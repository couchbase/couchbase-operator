package e2e

import (
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with no buckets
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions
func TestResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	expectedEvents := e2eutil.EventList{}
	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := 1
	for _, clusterSize := range clusterSizes {
		err = e2eutil.ResizeCluster(t, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
		if err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceEvent(testCouchbase)
		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
		}
		prevClusterSize = clusterSize
	}
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with one bucket
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions
func TestResizeClusterWithBucket(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	expectedEvents := e2eutil.EventList{}
	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := 1
	for _, clusterSize := range clusterSizes {
		err = e2eutil.ResizeCluster(t, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
		if err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceEvent(testCouchbase)
		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
		}
		prevClusterSize = clusterSize
	}
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

// Tests scenerio where a third node is being added to a cluster, and a separate
// node goes down immediately after the add & before the rebalance.
//
// Expects: autofailover of down node occurs and a replacement node is added
// in order to reach desired cluster size
func TestNodeRecoveryAfterMemberAdd(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 2 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// async scale up to 3 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 3, f.CRClient, testCouchbase)
	}()

	// wait for add member event
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	// kill pod 1
	err = e2eutil.KillPodForMember(f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatal(err)
	}

	// check response from resize request
	err = <-echan
	if err != nil {
		t.Fatal(err)
	}

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
}

// Tests scenerio where the node being added to is killed before it can be
// rebalanced in.
//
// Expects: autofailover of down node occurs and a replacement node is added
// in order to reach desired cluster size
func TestNodeRecoveryKilledNewMember(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 2 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// async scale up to 3 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 3, f.CRClient, testCouchbase)
	}()

	// wait for add member event
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	// kill pod that was just added
	err = e2eutil.KillPodForMember(f.KubeClient, testCouchbase, 2)
	if err != nil {
		t.Fatal(err)
	}

	// check response from resize request
	err = <-echan
	if err != nil {
		t.Fatal(err)
	}

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNegResizeCluster(t *testing.T) {

}
