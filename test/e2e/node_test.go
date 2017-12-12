package e2e

import (
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Tests one node failing in a cluster with 0 buckets
// 1. Create a 3 node Couchbase cluster with no buckets
// 2. Delete one of the pods in the cluster
// 3. Wait for Couchbase to failover the dead node and rebalance in a new one
// 4. Check the cluster status to make sure the cluster is healthy
// 5. Check the events to make sure the operator took the correct actions
func TestSingleNodeFailureNoBuckets(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	clusterSize := 3
	testCouchbase, secret, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, clusterSize, false)
	if err != nil {
		t.Fatal(err)
	}

	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase, secret)
	e2eutil.KillPodsAndWaitForRecovery(t, f.KubeClient, testCouchbase, 1)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, testCouchbase.Namespace, clusterSize, 36)
	if err != nil {
		t.Fatalf("Failed to wait for cluster to be healthy: %v", err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}

	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestNodeConfig(t *testing.T) {

}

func TestNodeConfigNegative(t *testing.T) {

}

func TestNodeFailure(t *testing.T) {

}
