package e2e

import (
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"testing"
)

// Tests creation of a 3 node cluster with persistence
// 1. Create a 3 node Couchbase cluster
// 2. Check cluster persistence type is stateful
// 3. Verify that expected volumes exist
// 4. Verify that the cluster is balanced and all data is available
func TestCreateStatefulCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := constants.Size3
	testCouchbase := e2eutil.MustNewStatefulCluster(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// Volumes should exist for each pods
	claimTemplate := testCouchbase.Spec.VolumeClaimTemplates[0].Name
	for i := 0; i < clusterSize; i++ {
		memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, i)
		_, err := e2eutil.GetMemberPVC(targetKube.KubeClient, f.Namespace, claimTemplate, memberName, 0, "default")
		if err != nil {
			t.Fatalf("could not find persistent volume for member: %s, %v", memberName, err)
		}
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10)

	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}
