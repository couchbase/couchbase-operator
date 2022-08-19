package e2e

import (
	"sort"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create 3 server groups.
// Failover all pods in selected server group.
// This should trigger server group failover in the cluster.
func TestServerGroupAutoFailover(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().ServerGroups(3)

	availableServerGroupList := getAvailabilityZones(t, kubernetes)

	// Create cluster spec for RZA feature.  Size the cluster such that
	// there are 2 nodes per available server group/AZ.
	clusterSize := len(availableServerGroupList) * 2

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(10)
	cluster.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	cluster.Spec.ClusterSettings.AutoFailoverServerGroup = true
	cluster.Spec.ServerGroups = availableServerGroupList
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroupList)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	victimGroup := 0
	victims := []int{}

	for i := 0; i < clusterSize; i++ {
		if i%len(availableServerGroupList) == victimGroup {
			victims = append(victims, i)
		}
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, podMemberToKill, true)
	}

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestMultiNodeAutoFailover tests Couchbase's ability to handle multiple failover
// events in series, and the Operator's ability to recover from it.
func TestMultiNodeAutoFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 9
	victims := []int{2, 3, 4}
	victimCount := len(victims)
	autoFailoverTimeout := 10 * time.Second

	e2eutil.MustNewBucket(t, kubernetes, e2espec.DefaultBucketThreeReplicas())
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = &metav1.Duration{Duration: autoFailoverTimeout}
	cluster.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, e2espec.DefaultBucketThreeReplicas(), time.Minute)

	// When ready, kill the victim nodes, waiting long enough for server to perform
	// auto failover, then expect recovery.  Yes to test this we have to be not running
	// in order for the condition to manifest.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status/controlPaused", true), time.Minute)

	for _, victim := range victims {
		e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim, true)
		time.Sleep(2 * autoFailoverTimeout)
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Nodes go down, and failover
	// * New members balanced in, killed members removed
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
