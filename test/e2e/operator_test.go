package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestPauseControl tests the user can pause the operator from controlling
// an couchbase cluster.
func TestPauseOperator(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	memberIdToKill := 0

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	t.Logf("Pausing operator...")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Spec/Paused", true), time.Minute)

	t.Logf("Killing pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, constants.Size2, constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("failed to wait for killed member to die: %v", err)
	}

	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, constants.Size3, constants.Retries10, testCouchbase); err == nil {
		t.Fatalf("cluster should not be recovered: control is paused")
	}

	t.Logf("Resuming operator...")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Spec/Paused", false), time.Minute)

	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToKill)

	event := e2eutil.NewMemberAddEvent(testCouchbase, 3)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, 2*time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestKillOperator(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminHidden)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	if err := e2eutil.KillOperatorAndWaitForRecovery(t, targetKube.KubeClient, f.Namespace); err != nil {
		t.Fatal(err)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// TestKillOperatorAndUpdateClusterConfig ensures that manual changes to bucket
// configuration are reverted by the operator during a restart.
func TestKillOperatorAndUpdateClusterConfig(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1
	flush := false

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

	// When the cluster is ready, kill the operator, manually update the bucket.  Verify the
	// bucket was updated and wait for it to revert as the operator regains mastership.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustDeleteCouchbaseOperator(t, targetKube, f.Namespace)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, "default", jsonpatch.NewPatchSet().Replace("/EnableFlush", &flush), constants.Retries1)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, "default", jsonpatch.NewPatchSet().Test("/EnableFlush", &flush), constants.Retries1)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent("default", testCouchbase), 3*time.Minute)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited (reverted)
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
