/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
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

	e2eutil.MustObserveClusterEventIgnoringMessage(t, kubernetes, cluster, e2eutil.ReconcileFailedEvent(cluster), 2*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionError, v1.ConditionTrue, cluster, 5*time.Minute)
}
