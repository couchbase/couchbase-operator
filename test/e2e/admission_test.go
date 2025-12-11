package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCAODacValidation(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	skipDacValidation := make(map[string]string)
	skipDacValidation[constants.AnnotationDisableAdmissionController] = "true"

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/metadata/annotations", skipDacValidation), 1*time.Minute)
	// Patch an invalid configuration with DAC validation skipped.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", targetVersionIllegalDowngrade), 1*time.Minute)

	// Must be caught by operator validation (i.e. pod version is not updated).
	e2eutil.MustCheckPodsForVersion(t, kubernetes, cluster, f.CouchbaseServerImage, e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, ""))
}

func TestCAODacValidationDisabled(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	skipDacValidation := make(map[string]string)
	skipDacValidation[constants.AnnotationSkipDACValidation] = "true"

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/metadata/annotations", skipDacValidation), 1*time.Minute)
	// Must be caught by DAC.
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", targetVersionIllegalDowngrade))
}

func TestCAOValidationUnreconcilable(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Create an invalid bucket.
	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-admission",
			Annotations: map[string]string{
				constants.AnnotationDisableAdmissionController: "true",
			},
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(90),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityLow,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: true,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
			StorageBackend:     couchbasev2.CouchbaseStorageBackendMagma,
		},
	}

	apiBucket, _ := e2eutil.NewBucketOld(kubernetes, bucket)

	// Check the bucket has had the unreconcilable annotation added.
	annotations := apiBucket.GetAnnotations()
	if value, found := annotations[constants.AnnotationUnreconcilable]; found {
		if !strings.EqualFold(value, "true") {
			t.Errorf("Unreconcilable annotation not set.")
			t.FailNow()
		}
	}

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionError, v1.ConditionTrue, cluster, 5*time.Minute)
}

func TestDisableAllValidation(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Create an invalid bucket.
	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-admission",
			Annotations: map[string]string{
				constants.AnnotationDisableAdmissionController: "true",
				constants.AnnotationSkipDACValidation:          "true",
			},
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(90),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityLow,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: true,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
			StorageBackend:     couchbasev2.CouchbaseStorageBackendMagma,
		},
	}

	e2eutil.MustNewBucket(t, kubernetes, bucket)

	e2eutil.MustObserveClusterEventIgnoringMessage(t, kubernetes, cluster, e2eutil.ReconcileFailedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionError, v1.ConditionTrue, cluster, 5*time.Minute)
}

// TestCAOValidationOperatorRestart tests that a cluster which is invalid (dac disabled, validationRunner is invalid) will still be considered
// invalid after an Operator is restarted and therefore has to recreate it's state to compare last vs existing cluster.
func TestCAOValidationOperatorRestart(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Disable DAC validation
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/metadata/annotations", map[string]string{
		constants.AnnotationDisableAdmissionController: "true",
	}), 1*time.Minute)

	oldSpecJSON, err := json.Marshal(cluster.Spec)
	if err != nil {
		t.Fatalf("Failed to marshal cluster spec: %v", err)
	}

	// Replace the default memory_optimized index storage setting with an invalid value (plasma).
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexStorageSetting", couchbasev2.CouchbaseClusterIndexStorageSettingStandard), time.Minute)
	// Wait for the oeprator to register the change is invalid.
	e2eutil.MustWaitForClusterWithErrorMessage(t, kubernetes, "spec.cluster.indexStorageSetting/spec.cluster.indexer.storageMode in body cannot be modified if there are any nodes in the cluster running the index service", cluster, 2*time.Minute)
	// Check that the last reconciled spec is the same as the old spec.
	e2eutil.MustWaitForClusterWithAnnotation(t, kubernetes, cluster, constants.AnnotationLastReconciledSpec, string(oldSpecJSON), 2*time.Minute)
	e2eutil.MustKillOperatorAndWaitForRecovery(t, kubernetes)

	// Update the size of the cluster and sleep so reconciliation occurs. We do not expect the operator to reconcile this change or updated the last reconciled spec annotation..
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", clusterSize+1), time.Minute)

	time.Sleep(30 * time.Second)
	// Check we still have the error message and the last reconciled spec is the same as the old spec.
	e2eutil.MustWaitForClusterWithErrorMessage(t, kubernetes, "spec.cluster.indexStorageSetting/spec.cluster.indexer.storageMode in body cannot be modified if there are any nodes in the cluster running the index service", cluster, 2*time.Minute)
	e2eutil.MustWaitForClusterWithAnnotation(t, kubernetes, cluster, constants.AnnotationLastReconciledSpec, string(oldSpecJSON), 2*time.Minute)

	// Patch the index storage setting back to a valid value.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexStorageSetting", couchbasev2.CouchbaseClusterIndexStorageSettingMemoryOptimized), time.Minute)

	newSpecJSON, err := json.Marshal(cluster.Spec)
	if err != nil {
		t.Fatalf("Failed to marshal cluster spec: %v", err)
	}

	// Wait for the error to be removed, the spec annotation to be updated and the cluster goes into a healthy state.
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 2*time.Minute, couchbasev2.ClusterConditionError)
	e2eutil.MustWaitForClusterWithAnnotation(t, kubernetes, cluster, constants.AnnotationLastReconciledSpec, string(newSpecJSON), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the cluster scales correctly once the spec is valid.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleUpSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
