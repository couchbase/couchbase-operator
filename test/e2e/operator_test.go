package e2e

import (
	"fmt"
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
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Pause the operator, kill a pod, ensure nothing comes back from the dead, then reenable the operator.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Spec/Paused", true), time.Minute)
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)
	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, constants.Size2, 2*time.Minute)

	if err := e2eutil.WaitUntilPodSizeReached(targetKube, testCouchbase, constants.Size3, 2*time.Minute); err == nil {
		e2eutil.Die(t, fmt.Errorf("cluster expectedly recovered"))
	}

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Spec/Paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member failed over
	// * Member replaced
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestKillOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Kill the operator, wait for recovery and make sure nothing bad happened to the cluster.
	e2eutil.MustKillOperatorAndWaitForRecovery(t, targetKube)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestKillOperatorAndUpdateClusterConfig ensures that manual changes to bucket
// configuration are reverted by the operator during a restart.
func TestKillOperatorAndUpdateClusterConfig(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucket.GetName()}, time.Minute)

	// When the cluster is ready, pause the operator, and await aknowledgement.  Manually update the bucket, then kill
	// and unpause the operator.  Verify the bucket was updated and wait for it to revert as the operator regains mastership.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Status/ControlPaused", true), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/EnableFlush", false), time.Minute)
	e2eutil.MustDeleteCouchbaseOperator(t, targetKube)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent(bucket.GetName(), testCouchbase), time.Minute)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited (reverted)
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
