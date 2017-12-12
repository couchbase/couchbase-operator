package e2e

import (
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"testing"
)

// This test will create single node basic cluster and attempt to scale it one node at a time up then down
func TestResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	expectedEvents := e2eutil.EventList{}
	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, secret, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase, secret)
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := 1
	for _, clusterSize := range clusterSizes {
		err = e2eutil.ResizeCluster(t, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
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

// This test will create single node basic cluster with a bucket and attempt to scale it one node at a time up then down
func TestResizeClusterWithBucket(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	expectedEvents := e2eutil.EventList{}
	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, secret, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase, secret)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := 1
	for _, clusterSize := range clusterSizes {
		err = e2eutil.ResizeCluster(t, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
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

func TestNegResizeCluster(t *testing.T) {

}
