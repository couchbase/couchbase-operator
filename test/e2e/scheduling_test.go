// Scheduling tests are pretty extreme and test what happens when the platform
// starts evacuating regions etc.
package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/clustercapabilities"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// mustGetNonMasterAvailabilityZone selects a random availability zone, that isn't a
// master (can't go killing Kubernetes can we).  This means we can pin the cluster to
// that zone and do bad things to it, while allowing the operator to keep functioning,
// and thus we can observe its behaviour.

//nolint:unused,nolintlint
func mustGetNonMasterAvailabilityZone(t *testing.T, kubernetes *types.Cluster) string {
	caps := clustercapabilities.MustNewCapabilities(t, kubernetes.KubeClient)

	serverGroups := []string{}

Next:
	for _, serverGroup := range caps.AvailabilityZones {
		for _, master := range caps.MasterZones {
			if serverGroup == master {
				continue Next
			}
		}

		serverGroups = append(serverGroups, serverGroup)
	}

	if len(serverGroups) < 2 {
		t.Skip("need two or more")
	}

	return serverGroups[0]
}

// TestScheduleEvacuateAllPersistent creates a PVC backed cluster on a single
// availability zone.  This zone is then evacuated.  We wait for the operator
// to fail to recover due to a scheduling error (tests this event is actually
// raised), then we remove the tainst and let nature take its course.
func TestScheduleEvacuateAllPersistent(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	mdsSize := 2
	clusterSize := mdsSize * 2
	recoveryPolicy := couchbasev2.PrioritizeUptime

	// Dynamic configuration.
	victim := mustGetNonMasterAvailabilityZone(t, kubernetes)

	// Create the cluster.  Place all pods in the same availability zone.  We will
	// evacuate this, and leave the Operator running in another.
	cluster := clusterOptions().WithMixedTopology(mdsSize).Generate(kubernetes)
	cluster.Spec.RecoveryPolicy = &recoveryPolicy
	cluster.Spec.ServerGroups = []string{
		victim,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Ensure the nodes are cleaned up afterwards whatever happens.  This can
	// still fail and leave the cluster in a bad state, but what to do eh?
	defer e2eutil.MustUntaintAll(t, kubernetes)

	// Kill the cluster unceremoniously, then expect a member creation failure as
	// the pod is forced onto an unschedulable zone.  Untaint and recover.
	e2eutil.MustEvacuateZone(t, kubernetes, victim)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, 0), 15*time.Minute)
	e2eutil.MustUntaintAll(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster comes up
	// * Member recovery fails
	// * Cluster fully recovers
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
		e2eutil.PodDownWithPVCRecoverySequenceWithEphemeral(t, clusterSize, mdsSize, mdsSize, f.CouchbaseServerImage),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScheduleCleanupUninitializedPod kicks everything off by scheduling a cluster in an
// unusable AZ, then offs the Operator and removes the taint.  This ensures that the Operator
// spots a pod is unintialized, cannot be talked to, and thus needs sorting out, with a
// cudgel sorting out.
func TestScheduleCleanupUninitializedPod(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Rethink()

	clusterSize := 3

	// Dynamic configuration.
	// Pick an AZ to evacuate and do it... The wait is to let the DAC get back
	// up and running again on the off chance it's evicted.
	victim := mustGetNonMasterAvailabilityZone(t, kubernetes)

	e2eutil.MustEvacuateZone(t, kubernetes, victim)
	defer e2eutil.MustUntaintAll(t, kubernetes)

	time.Sleep(10 * time.Second)

	// Create the cluster in the evacuated AZ...
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = []string{
		victim,
	}
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// ... when the pod is pending, exists but isn't scheduled or initialized, then we
	// want to kill the Operator, remove the taints to allow the pod to be scheduled.
	// The Operator should spot it's crap and kill it off before bringing the cluster up.
	e2eutil.MustWaitForPendingPod(t, kubernetes, couchbaseutil.CreateMemberName(cluster.Name, 0), time.Minute)
	e2eutil.MustDeleteCouchbaseOperator(t, kubernetes)
	e2eutil.MustUntaintAll(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster comes up... eventually
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
