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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	availableServerGroupList := getAvailabilityZones(t, targetKube)
	if len(availableServerGroupList) < 3 {
		t.Skip("couchbase server requires 3 or more availability zones")
	}

	// Create cluster spec for RZA feature.  Ensure that data is correctly
	// balanced by rounding down the largest multiple of number of AZs that
	// will fit.
	clusterSize := e2eutil.MustNumNodes(t, targetKube) / len(availableServerGroupList) * len(availableServerGroupList)

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(10)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	testCouchbase.Spec.ClusterSettings.AutoFailoverServerGroup = true
	testCouchbase.Spec.ServerGroups = availableServerGroupList
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, time.Minute)

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	victimGroup := 0
	victims := []int{}

	for i := 0; i < clusterSize; i++ {
		if i%len(availableServerGroupList) == victimGroup {
			victims = append(victims, i)
		}
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, true)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestMultiNodeAutoFailover tests Couchbase's ability to handle multiple failover
// events in series, and the Operator's ability to recover from it.
func TestMultiNodeAutoFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 9
	victims := []int{2, 3, 4}
	victimCount := len(victims)
	autoFailoverTimeout := 10 * time.Second

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketThreeReplicas())
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = &metav1.Duration{Duration: autoFailoverTimeout}
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucketThreeReplicas().Name}, time.Minute)

	// When ready, kill the victim nodes, waiting long enough for server to perform
	// auto failover, then expect recovery.  Yes to test this we have to be not running
	// in order for the condition to manifest.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Status/ControlPaused", true), time.Minute)

	for _, victim := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, true)
		time.Sleep(2 * autoFailoverTimeout)
	}

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
