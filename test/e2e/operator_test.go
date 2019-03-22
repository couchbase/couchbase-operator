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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

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
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestKillOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// Kill the operator, wait for recovery and make sure nothing bad happened to the cluster.
	e2eutil.MustKillOperatorAndWaitForRecovery(t, targetKube, f.Namespace)
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
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, "default", jsonpatch.NewPatchSet().Replace("/EnableFlush", &flush), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, "default", jsonpatch.NewPatchSet().Test("/EnableFlush", &flush), time.Minute)
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
