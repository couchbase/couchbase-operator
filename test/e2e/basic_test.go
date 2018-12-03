package e2e

import (
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithoutBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", 0, 1, 2)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size3, constants.WithBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", 0, 1, 2)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", "default")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}
