package e2e

import (
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"testing"
)

// Tests creation of a 3 node cluster with 0 buckets
// 1. Create a 3 node Couchbase cluster with no buckets
// 2. Check the events to make sure the operator took the correct actions
// 3. Verifies that the cluster is balanced and all data is available
func TestCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterSize := 3
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, clusterSize, false, false)
	if err != nil {
		t.Fatal(err)
	}

	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
	if err != nil {
		t.Fatal(err.Error())
	}
}

// Tests creation of a 3 node cluster with 1 bucket
// 1. Create a 3 node Couchbase cluster with 1 bucket
// 2. Check the events to make sure the operator took the correct actions
// 3. Verifies that the cluster is balanced and all data is available
func TestCreateBucketCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterSize := 3
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, clusterSize, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	event = k8sutil.BucketCreateEvent("default", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
	if err != nil {
		t.Fatal(err.Error())
	}
}
