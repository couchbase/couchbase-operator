/*
Copyright 2023-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testBucket(bucketName string, backend couchbasev2.CouchbaseStorageBackend) *couchbasev2.CouchbaseBucket {
	return &couchbasev2.CouchbaseBucket{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: bucketName},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:    e2espec.NewResourceQuantityMi(1024),
			Replicas:       1,
			EvictionPolicy: couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			StorageBackend: backend,
		}}
}

func testCouchstoreBucket(bucketName string) *couchbasev2.CouchbaseBucket {
	return testBucket(bucketName, couchbasev2.CouchbaseStorageBackendCouchstore)
}

func testMagmaBucket(bucketName string) *couchbasev2.CouchbaseBucket {
	return testBucket(bucketName, couchbasev2.CouchbaseStorageBackendMagma)
}

func TestMagmaBucketToCouchstoreMigration(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0").CouchbaseBucket()

	clusterSize := 3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(1152))
	cluster.Spec.Buckets.EnableBucketMigrationRoutines = true

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := testMagmaBucket(e2e_constants.DefaultBucket)

	couchbaseutil.AddAnnotation(&bucket.ObjectMeta, "cao.couchbase.com/historyRetention.collectionHistoryDefault", "false")

	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().
		Replace("/spec/storageBackend", couchbasev2.CouchbaseStorageBackendCouchstore).
		Remove("/metadata/annotations"),
		time.Minute)

	e2eutil.MustWaitUntilAllNodeStorageBackendCouchstore(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		eventschema.Repeat{Times: clusterSize, Validator: e2eutil.SwapRebalanceSequence},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// This test validates that if multiple buckets are updated in a relatively short time
// the operator will only perform round of swap rebalances, migrating multiple buckets.
func TestMultipleCouchstoreBucketsToMagmaMigration(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0").CouchbaseBucket()

	clusterSize := 2

	cluster := clusterOptions().WithDataOnlyEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2048))
	cluster.Spec.Buckets.EnableBucketMigrationRoutines = true

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket1 := testCouchstoreBucket("bucket1")

	bucket2 := testCouchstoreBucket("bucket2")

	bucket1Obj := e2eutil.MustNewBucket(t, kubernetes, bucket1)
	bucket2Obj := e2eutil.MustNewBucket(t, kubernetes, bucket2)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket1, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket2, time.Minute)

	e2eutil.MustPatchBucket(t, kubernetes, bucket1Obj, jsonpatch.NewPatchSet().
		Replace("/spec/storageBackend", couchbasev2.CouchbaseStorageBackendMagma),
		time.Minute)

	e2eutil.MustPatchBucket(t, kubernetes, bucket2Obj, jsonpatch.NewPatchSet().
		Replace("/spec/storageBackend", couchbasev2.CouchbaseStorageBackendMagma),
		time.Minute)

	e2eutil.MustWaitUntilAllNodeStorageBackendMagma(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		eventschema.Repeat{Times: clusterSize, Validator: e2eutil.SwapRebalanceSequence},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// This test validates that the buckets.maxConcurrentPodSwaps annotation will be used to determine the number of pods
// that should be migrated each swap-rebalance, excl. the orchestrator.
func TestCouchstoreBucketsToMagmaMigrationWithMultiMigration(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0").CouchbaseBucket()

	clusterSize := 3

	cluster := clusterOptions().WithDataOnlyEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(2048))
	cluster.Spec.Buckets.EnableBucketMigrationRoutines = true

	couchbaseutil.AddAnnotation(&cluster.ObjectMeta, "cao.couchbase.com/buckets.maxConcurrentPodSwaps", "2")

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := testCouchstoreBucket("bucket")

	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().
		Replace("/spec/storageBackend", couchbasev2.CouchbaseStorageBackendMagma),
		time.Minute)

	e2eutil.MustWaitUntilAllNodeStorageBackendMagma(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// With a cluster size of 3 and buckets.maxConcurrentPodSwaps set to 2, we should
	// only see 2 rebalances where the non-orchestrator pods are rebalanced at the same time, followed by the orchestrator
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Repeat{Times: 1, Validator: e2eutil.SwapRebalanceSequence},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCouchstoreBucketToMagmaMigrationUnmanagedBucket(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0").CouchbaseBucket()

	clusterSize := 1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.Managed = false
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(1152))
	cluster.Spec.Buckets.EnableBucketMigrationRoutines = true
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	bucket := e2eutil.NewBucket(e2eutil.BucketTypeCouchbase).
		WithMemoryQuota(1024).
		WithEvictionPolicy(string(couchbasev2.CouchbaseBucketEvictionPolicyFullEviction)).
		WithStorageBackend(couchbasev2.CouchbaseStorageBackendCouchstore)
	bucket.MustCreateManually(t, kubernetes, cluster, e2e_constants.DefaultBucket)

	e2eutil.MustWaitUntilUnmanagedBucketExists(t, kubernetes, cluster, e2e_constants.DefaultBucket, time.Minute)

	bucket.
		WithStorageBackend(couchbasev2.CouchbaseStorageBackendMagma).
		MustUpdateManually(t, kubernetes, cluster, e2e_constants.DefaultBucket, time.Minute)

	e2eutil.MustWaitUntilAllNodeStorageBackendMagma(t, kubernetes, cluster, 10*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: clusterSize, Validator: e2eutil.SwapRebalanceSequence},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMagmaBucketToCouchstoreMigrationFromDefault(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0").CouchbaseBucket()

	clusterSize := 1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(1152))
	cluster.Spec.Buckets.EnableBucketMigrationRoutines = true

	couchbaseutil.AddAnnotation(&cluster.ObjectMeta, "cao.couchbase.com/buckets.defaultStorageBackend", "couchstore")

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := testMagmaBucket(e2e_constants.DefaultBucket)

	couchbaseutil.AddAnnotation(&bucket.ObjectMeta, "cao.couchbase.com/historyRetention.collectionHistoryDefault", "false")

	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().
		Remove("/spec/storageBackend"),
		time.Minute)

	e2eutil.MustWaitUntilAllNodeStorageBackendCouchstore(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		eventschema.Repeat{Times: clusterSize, Validator: e2eutil.SwapRebalanceSequence},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCouchstoreBucketToMagmaUpdateUnmanagedBucket(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0").CouchbaseBucket()

	clusterSize := 1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.Managed = false
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(int64(1152))
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	bucket := e2eutil.NewBucket(e2eutil.BucketTypeCouchbase).
		WithMemoryQuota(1024).
		WithEvictionPolicy(string(couchbasev2.CouchbaseBucketEvictionPolicyFullEviction)).
		WithStorageBackend(couchbasev2.CouchbaseStorageBackendCouchstore)
	bucket.MustCreateManually(t, kubernetes, cluster, e2e_constants.DefaultBucket)

	e2eutil.MustWaitUntilUnmanagedBucketExists(t, kubernetes, cluster, e2e_constants.DefaultBucket, time.Minute)

	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/metadata/annotations", map[string]string{
		"cao.couchbase.com/buckets.targetUnmanagedBucketStorageBackend": "magma",
		"cao.couchbase.com/buckets.enableBucketMigrationRoutines":       "true",
	}), time.Minute)

	e2eutil.MustWaitUntilAllNodeStorageBackendMagma(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: clusterSize, Validator: e2eutil.SwapRebalanceSequence},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestEvictionPolicyOnlineChange(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("8.0.0").CouchbaseBucket()

	clusterSize := 3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.EnableBucketMigrationRoutines = true

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := testCouchstoreBucket(e2e_constants.DefaultBucket)

	bucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(int64(100))
	bucket.Spec.OnlineEvictionPolicyChange = true
	bucket.Spec.EvictionPolicy = couchbasev2.CouchbaseBucketEvictionPolicyValueOnly

	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().
		Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyFullEviction),
		time.Minute)

	e2eutil.MustWaitUntilAllNodeEvictionPolicyMatch(t, kubernetes, cluster, 10*time.Minute, string(couchbasev2.CouchbaseBucketEvictionPolicyFullEviction))
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		eventschema.Repeat{Times: clusterSize, Validator: e2eutil.SwapRebalanceSequence},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestEvictionPolicyOnlineChangeMigrationDisabled(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("8.0.0").CouchbaseBucket()

	clusterSize := 3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	skipDacValidation := make(map[string]string)
	skipDacValidation[constants.AnnotationDisableAdmissionController] = "true"

	cluster.Annotations = skipDacValidation

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Create a bucket with eviction policy valueOnly
	bucket := testCouchstoreBucket(e2e_constants.DefaultBucket)

	bucket.Annotations = skipDacValidation

	bucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(int64(100))
	bucket.Spec.OnlineEvictionPolicyChange = true
	bucket.Spec.EvictionPolicy = couchbasev2.CouchbaseBucketEvictionPolicyValueOnly

	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Wait for everything to be healthy and ready
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Change the eviction policy to fullEviction
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().
		Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyFullEviction),
		time.Minute)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.BucketEditedEvent(cluster, bucket.Name), 5*time.Minute)

	e2eutil.NodesMustNotHaveClusterEvictionPolicy(t, kubernetes, cluster, 3*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Change the online eviction policy change to false and expect the eviction policy to change through
	// bucket restarts
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().
		Replace("/spec/onlineEvictionPolicyChange", false),
		time.Minute)

	e2eutil.MustWaitUntilAllNodeEvictionPolicyMatch(t, kubernetes, cluster, 10*time.Minute, string(couchbasev2.CouchbaseBucketEvictionPolicyFullEviction))
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
