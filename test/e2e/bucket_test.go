package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestBucketHistoryRetentionDefaultValues tests the default history retention settings for buckets.
func TestBucketHistoryRetentionRetentionSettingsInCRD(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.2.0")

	// Static configuration
	clusterSize := 1

	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bucket-no-annotations",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(1024),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
			StorageBackend:     couchbasev2.CouchbaseStorageBackendMagma,
			HistoryRetentionSettings: &couchbasev2.HistoryRetentionSettings{
				Seconds:           uint64(10),
				Bytes:             uint64(2147483648),
				CollectionDefault: &[]bool{false}[0],
			},
		},
	}

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(2048)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// check if mutation setting exists.
	e2eutil.MustVerifyBucketHistoryRetentionSettings(t, kubernetes, cluster, bucket.Name, 10, uint64(2147483648), false, 2*time.Minute)

	// validate whether magma block size settings are default as no related annotations where passed
	e2eutil.MustVerifyMagmaBucketBlockSizeSettings(t, kubernetes, cluster, bucket.Name, 4096, 4096, 2*time.Minute)
	// clean up.
	e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.Name, 2*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check events to verify no unnecessary events are emitted.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

/*
TestBucketHistoryRetentionWithAnnotations tests history retention server functionality in 7.2 and above.
Checks that setting the annotation results in the correct changes in Server.
*/
func TestBucketHistoryRetentionWithAnnotations(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.2.0")

	// Static configuration
	clusterSize := 1
	names := []string{
		"bucket1",
		"bucket2",
	}

	expected := []bool{
		false,
		true,
	}
	buckets := []metav1.Object{
		&couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[0],
				Annotations: map[string]string{
					"cao.couchbase.com/historyRetention.seconds":                  "100",
					"cao.couchbase.com/historyRetention.bytes":                    "2147483648",
					"cao.couchbase.com/historyRetention.collectionHistoryDefault": "false",
					"cao.couchbase.com/magmaSeqTreeDataBlockSize":                 "5555",
					"cao.couchbase.com/magmaKeyTreeDataBlockSize":                 "6666",
				},
			},
			Spec: couchbasev2.CouchbaseBucketSpec{
				MemoryQuota:        e2espec.NewResourceQuantityMi(1024),
				Replicas:           1,
				IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
				EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
				ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
				EnableFlush:        true,
				EnableIndexReplica: false,
				CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
				StorageBackend:     couchbasev2.CouchbaseStorageBackendMagma,
			},
		},
		&couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[1],
				Annotations: map[string]string{
					"cao.couchbase.com/historyRetention.seconds":                  "100",
					"cao.couchbase.com/historyRetention.bytes":                    "2147483648",
					"cao.couchbase.com/historyRetention.collectionHistoryDefault": "true",
					"cao.couchbase.com/magmaSeqTreeDataBlockSize":                 "5555",
					"cao.couchbase.com/magmaKeyTreeDataBlockSize":                 "6666",
				},
			},
			Spec: couchbasev2.CouchbaseBucketSpec{
				MemoryQuota:        e2espec.NewResourceQuantityMi(1024),
				Replicas:           1,
				IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
				EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
				ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
				EnableFlush:        true,
				EnableIndexReplica: false,
				CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
				StorageBackend:     couchbasev2.CouchbaseStorageBackendMagma,
			},
		},
	}

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(2048)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for _, bucket := range buckets {
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	}

	// check if mutation setting exists.
	for i := 1; i < len(buckets); i++ {
		e2eutil.MustVerifyBucketHistoryRetentionSettings(t, kubernetes, cluster, buckets[i].GetName(), 100, 2147483648, expected[i], 2*time.Minute)
		e2eutil.MustVerifyMagmaBucketBlockSizeSettings(t, kubernetes, cluster, buckets[i].GetName(), 5555, 6666, 2*time.Minute)
	}

	// clean up.
	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	}

	for _, name := range names {
		e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, name, 2*time.Minute)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
}

func TestBucketAddRemoveBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration
	clusterSize := 3
	names := []string{
		"bucket1",
		"bucket2",
		"bucket3",
		"bucket4",
	}

	buckets := []metav1.Object{
		&couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[0],
			},
			Spec: couchbasev2.CouchbaseBucketSpec{
				MemoryQuota:        e2espec.NewResourceQuantityMi(256),
				Replicas:           1,
				IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
				EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
				ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
				EnableFlush:        true,
				EnableIndexReplica: true,
				CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
			},
		},
		&couchbasev2.CouchbaseEphemeralBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[2],
			},
			Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
				MemoryQuota:        e2espec.NewResourceQuantityMi(101),
				Replicas:           1,
				IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
				EvictionPolicy:     couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNoEviction,
				ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionTimestamp,
				EnableFlush:        true,
				CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
			},
		},
	}

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	if memcachedBucketSupported, err := couchbaseutil.VersionBefore(cbVersion, "8.0.0"); err != nil {
		e2eutil.Die(t, err)
	} else if memcachedBucketSupported {
		buckets = append(buckets,
			&couchbasev2.CouchbaseMemcachedBucket{
				ObjectMeta: metav1.ObjectMeta{
					Name: names[1],
				},
				Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
					MemoryQuota: e2espec.NewResourceQuantityMi(256),
					EnableFlush: false,
				},
			})
	}

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(2048)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	for _, bucket := range buckets {
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	}

	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	}

	for _, name := range names {
		e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, name, 2*time.Minute)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets added seqentially
	// * Buckets removed
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted}},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestBucketAddRemoveExtended(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	bucketTypes := []string{"couchbase", "ephemeral"}

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	if memcachedBucketSupported, err := couchbaseutil.VersionBefore(cbVersion, "8.0.0"); err != nil {
		e2eutil.Die(t, err)
	} else if memcachedBucketSupported {
		bucketTypes = append(bucketTypes, "memcached")
	}

	buckets := e2espec.GenerateValidBucketSettings(bucketTypes)
	for _, bucket := range buckets {
		name := bucket.GetName()
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, name, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets added then removed
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times: len(buckets),
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
					eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
				},
			},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestEditBucket tests modifying various bucket parameters and reverting them.
func TestEditBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).CouchbaseBucket()

	// Constants
	enabled := true
	disabled := false

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(1).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Create a direct connection to a couchbase node.
	// When healthy change the memory quota, replicas, whether flushes are allowed and the compression mode.
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(128)), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", int64(128)), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", e2espec.NewResourceQuantityMi(256)), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", int64(256)), time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/replicas", 2), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/BucketReplicas", 2), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/replicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/enableFlush", disabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/EnableFlush", disabled), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/enableFlush", enabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/EnableFlush", enabled), time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionModeActive), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/CompressionMode", couchbaseutil.CompressionModeActive), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionModeOff), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/CompressionMode", couchbaseutil.CompressionModeOff), time.Minute)
	e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionModePassive), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/CompressionMode", couchbaseutil.CompressionModePassive), time.Minute)

	patchCycles := 9

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	if atleast76, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if atleast76 {
		rank := 100
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/rank", rank), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/Rank", &rank), time.Minute)

		rank = 0
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/rank", rank), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/Rank", &rank), time.Minute)

		enableCrossClusterVersioning := true
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/metadata/annotations", map[string]string{
			"cao.couchbase.com/enableCrossClusterVersioning": couchbaseutil.BoolToStr(enableCrossClusterVersioning),
		}), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/EnableCrossClusterVersioning", &enableCrossClusterVersioning), time.Minute)

		versionPruningWindowHrs := uint64(360)
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/metadata/annotations", map[string]string{
			"cao.couchbase.com/enableCrossClusterVersioning": couchbaseutil.BoolToStr(enableCrossClusterVersioning),
			"cao.couchbase.com/versionPruningWindowHrs":      couchbaseutil.IntToStr(int(versionPruningWindowHrs)),
		}), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/VersionPruningWindowHrs", &versionPruningWindowHrs), time.Minute)

		patchCycles += 4
	}

	if isAtleast80, err := couchbaseutil.VersionAfter(cbVersion, "8.0.0"); err != nil {
		e2eutil.Die(t, err)
	} else if isAtleast80 {
		accessScannerEnabled := false
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/accessScannerEnabled", accessScannerEnabled), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/AccessScannerEnabled", &accessScannerEnabled), time.Minute)

		expiryPagerSleepTimeSeconds := uint64(900)
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/expiryPagerSleepTime", k8sutil.NewDurationS(900)), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/ExpiryPagerSleepTime", &expiryPagerSleepTimeSeconds), time.Minute)

		warmupBehaviour := "blocking"
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/warmupBehavior", warmupBehaviour), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/WarmupBehavior", warmupBehaviour), time.Minute)

		memoryLowWatermark := 56
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryLowWatermark", memoryLowWatermark), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/MemoryLowWatermark", &memoryLowWatermark), time.Minute)

		memoryHighWatermark := 83
		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/memoryHighWatermark", memoryHighWatermark), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/MemoryHighWatermark", &memoryHighWatermark), time.Minute)

		bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/durabilityImpossibleFallback", couchbasev2.DurabilityImpossibleFallbackActive), time.Minute)
		e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/durabilityImpossibleFallback", couchbasev2.DurabilityImpossibleFallbackActive), time.Minute)

		patchCycles += 6
	}

	// Avoid a race where Couchbase has been updated but the event not raise yet.
	time.Sleep(10 * time.Second)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: patchCycles, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests that the operator reverts bucket edits not made by the operator
// 1. Create a one node cluster with one bucket
// 2. Create a node port service so we can access the cluster externally
// 3. Use an external client to update the bucket flush enabled parameter to false
// 4. Verify that the operator reverts the change
// 5. Use an external client to update the bucket replicas parameter to 3
// 6. Verify that the operator reverts the change
// 7. Use an external client to update the bucket IO priority parameter to default
// 8. Verify that the operator reverts the change
// 9. Check the events to make sure the operator took the correct actions.
func TestRevertExternalBucketUpdates(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).CouchbaseBucket()

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(1).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Once ready, alter a few parameters and ensure they are reverted by the operator.
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/EnableFlush", false), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/EnableFlush", true), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/BucketReplicas", 3), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/IoPriority", couchbaseutil.IoPriorityTypeLow), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/IoPriority", couchbaseutil.IoPriorityTypeHigh), time.Minute)
	time.Sleep(10 * time.Second) // Wait for event to become visible

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestBucketUnmanaged ensures the operator doesn't touch buckets when they are
// unmanaged.
func TestBucketUnmanaged(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create a bucket.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create a cluster with buckets unmanaged.
	couchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	couchbase.Spec.Buckets.Managed = false
	couchbase = e2eutil.MustNewClusterFromSpec(t, kubernetes, couchbase)

	// Ensure the bucket doesn't get created.
	if err := e2eutil.WaitUntilBucketExists(kubernetes, couchbase, bucket, time.Minute); err == nil {
		e2eutil.Die(t, fmt.Errorf("bucket created unexpectedly"))
	}

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, couchbase, expectedEvents)
}

// TestBucketSelection ensures the operator only touches buckets that match the
// label selector.
func TestBucketSelection(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	bucketName := "simba"
	labels := map[string]string{
		"loves": "nala",
	}

	// Create a default bucket and a labelled bucket.
	e2eutil.MustNewBucket(t, kubernetes, e2espec.DefaultBucket())
	bucket := e2espec.DefaultBucket()
	bucket.Name = bucketName
	bucket.Labels = labels
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create a cluster that selects only labelled buckets.
	couchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	couchbase.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: labels,
	}
	couchbase = e2eutil.MustNewClusterFromSpec(t, kubernetes, couchbase)

	// Ensure the unlabelled bucket doesn't get created.
	if err := e2eutil.WaitUntilBucketExists(kubernetes, couchbase, e2espec.DefaultBucket(), time.Minute); err == nil {
		e2eutil.Die(t, fmt.Errorf("bucket created unexpectedly"))
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Only the labelled bucket is created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucketName},
	}

	ValidateEvents(t, kubernetes, couchbase, expectedEvents)
}

// TestBucketWithExplicitName checks that overriding the resource name works.
func TestBucketWithExplicitName(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Constants
	clusterSize := 1
	bucketName := "Sweet_Carabine_Bah_Bah_Bah"

	// Create the cluster.
	bucketTyped := e2espec.DefaultBucket()
	bucketTyped.Spec.Name = couchbasev2.BucketName(bucketName)
	e2eutil.MustNewBucket(t, kubernetes, bucketTyped)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucketTyped, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket added with the correct name
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucketName},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestBucketWithSameExplicitNameAndDifferentType checks that buckets can have different
// types but the same name in the same namespace.
func TestBucketWithSameExplicitNameAndDifferentType(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Constants
	clusterSize := 1
	bucketName := "Sweet_Carabine_Bah_Bah_Bah"
	labels1 := map[string]string{
		"name": "thor",
	}
	labels2 := map[string]string{
		"name": "scarlet-witch",
	}

	// Create the first cluster with a couchbase bucket.
	bucketTyped1 := e2espec.DefaultBucket()
	bucketTyped1.Name = ""
	bucketTyped1.GenerateName = "bucket-"
	bucketTyped1.Labels = labels1
	bucketTyped1.Spec.Name = couchbasev2.BucketName(bucketName)

	var bucketUntyped1 metav1.Object = bucketTyped1

	bucketUntyped1 = e2eutil.MustNewBucket(t, kubernetes, bucketUntyped1)

	cluster1 := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster1.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: labels1,
	}
	cluster1 = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster1)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster1, bucketUntyped1, time.Minute)

	// Create the second cluster with an ephemeral bucket.
	bucketTyped2 := e2espec.DefaultEphemeralBucket()
	bucketTyped2.Name = ""
	bucketTyped2.GenerateName = "bucket-"
	bucketTyped2.Labels = labels2
	bucketTyped2.Spec.Name = couchbasev2.BucketName(bucketName)

	var bucketUntyped2 metav1.Object = bucketTyped2

	bucketUntyped2 = e2eutil.MustNewBucket(t, kubernetes, bucketUntyped2)

	cluster2 := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster2.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: labels2,
	}
	cluster2 = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster2)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster2, bucketUntyped2, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Bucket added with the correct name
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucketName},
	}

	ValidateEvents(t, kubernetes, cluster1, expectedEvents)
	ValidateEvents(t, kubernetes, cluster2, expectedEvents)
}

// TestCouchbaseBucketStorageBackendMagmaInvalidForFtsAnalyticsEventing tests whether the dac will correctly accept or reject
// magma buckets where the server version is above or below 7.1.2, which is a requirement for FTS, analytics and eventing services.
func TestCouchbaseBucketStorageBackendMagmaInvalidForFtsAnalyticsEventing(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration
	clusterSize := constants.Size1

	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bucket-magma",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(1024),
			StorageBackend:     couchbasev2.CouchbaseStorageBackendMagma,
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(1431)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.SearchService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	magmaServicesSupported, err := couchbaseutil.VersionAfter(cbVersion, "7.2.1")
	if err != nil {
		e2eutil.Die(t, err)
	}

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	if magmaServicesSupported {
		e2eutil.MustNewBucket(t, kubernetes, bucket)
		e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonBucketCreated})
	} else {
		// Bucket should not be created if we don't have a high enough cb version when running the services
		e2eutil.MustGetAdmissionFailureOnCreateBucket(t, kubernetes, bucket, "search, eventing or analytics services cannot be used with magma buckets below CB Server 7.1.2.")
		e2eutil.MustWaitUntilBucketNotExists(t, kubernetes, cluster, bucket.GetName(), 2*time.Minute)
	}

	// Check the cluster is still healthy after trying to add a bucket.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestSampleBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Static configuration
	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "beer-sample",
			Annotations: map[string]string{"cao.couchbase.com/sampleBucket": "true"},
		},
	}

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	e2eutil.MustVerifyDocCountInBucketGreaterThan(t, kubernetes, cluster, bucket.GetName(), 7300, time.Minute)
}

// TestUpdateSampleBucket will check that sample buckets aren't updated to match the CRD spec until the sampleBucket annotation is false.
func TestUpdateSampleBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// TODO: Remove this and fix the test once we implement magma vbucket config support.
	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0").BeforeVersion("8.0.0")

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Static configuration
	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "beer-sample",
			Annotations: map[string]string{"cao.couchbase.com/sampleBucket": "true"},
		},
	}

	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)
	e2eutil.MustVerifyDocCountInBucketGreaterThan(t, kubernetes, cluster, bucket.GetName(), 7300, time.Minute)

	beerSampleDefaultMemoryQuota := e2espec.NewResourceQuantityMi(200)
	updatedMemoryQuota := e2espec.NewResourceQuantityMi(256)

	// Update the memory quota with a value that is different to the sample bucket default
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().Add("/spec/memoryQuota", updatedMemoryQuota), time.Minute)

	// Validate that the sample bucket's memory quota has not been updated
	e2eutil.MustVerifyCouchbaseBucketMemoryQuota(t, kubernetes, cluster, bucketObj.GetName(), beerSampleDefaultMemoryQuota, time.Minute)

	// Set the sampleBucket annotation to false
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().Replace("/metadata/annotations", map[string]string{"cao.couchbase.com/sampleBucket": "false"}), time.Minute)

	// We now expect the memory quota to be updated
	e2eutil.MustVerifyCouchbaseBucketMemoryQuota(t, kubernetes, cluster, bucketObj.GetName(), updatedMemoryQuota, time.Minute)

	// Check the events contain a single bucket edited event where the memory quota was updated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucket.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited, FuzzyMessage: bucket.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestCreateEditDeleteCouchbaseBucketAutoCompactionSettingsCouchstoreBackend tests that auto-compaction settings configured at the bucket level are set on the couchbase bucket
// and that editing and removing settings will be correctly reflected by the bucket when it has a couchstore storage backend.
func TestCreateEditDeleteCouchbaseBucketAutoCompactionSettingsCouchstoreBackend(t *testing.T) {
	// Platform configuration
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0")

	clusterSize := constants.Size1

	// Configure initial bucket auto-compaction settings
	// Initial bucket auto-compaction settings
	crdAutoCompactionSettings := couchbasev2.AutoCompactionSpecBucket{DatabaseFragmentationThreshold: &couchbasev2.DatabaseFragmentationThresholdBucket{Percent: to.Ptr(15), Size: k8sutil.NewResourceQuantityMi(int64(512))},
		ViewFragmentationThreshold: &couchbasev2.ViewFragmentationThresholdBucket{Percent: to.Ptr(45), Size: k8sutil.NewResourceQuantityMi(int64(256))},
		TombstonePurgeInterval:     &metav1.Duration{Duration: time.Duration(48) * time.Hour},
		TimeWindow: &couchbasev2.TimeWindow{AbortCompactionOutsideWindow: true,
			Start: to.Ptr("10:00"),
			End:   to.Ptr("12:30")}}

	// Expected bucket details after creation on couchbase server
	expectedAutoCompactionSettings := couchbaseutil.BucketAutoCompactionSettings{
		Enabled: true,
		Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
			DatabaseFragmentationThreshold: couchbaseutil.AutoCompactionDatabaseFragmentationThreshold{Percentage: 15, Size: e2eutil.MiToByteQuantity(512)},
			ViewFragmentationThreshold:     couchbaseutil.AutoCompactionViewFragmentationThreshold{Percentage: 45, Size: e2eutil.MiToByteQuantity(256)},
			ParallelDBAndViewCompaction:    false,
			AllowedTimePeriod: &couchbaseutil.AutoCompactionAllowedTimePeriod{
				AbortOutside: true,
				FromMinute:   0,
				FromHour:     10,
				ToMinute:     30,
				ToHour:       12,
			},
		}}

	// Initial requested bucket as CRD
	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "couchbase-bucket-autocompaction-couchstore",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			AutoCompaction: &crdAutoCompactionSettings,
			StorageBackend: couchbasev2.CouchbaseStorageBackendCouchstore,
		},
	}

	expectedPurgeInterval := to.Ptr(float64(2))

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Create the bucket
	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Verify the initial settings are correct
	e2eutil.MustVerifyCouchbaseBucketAutoCompactionSettings(t, kubernetes, cluster, bucket.Name, expectedAutoCompactionSettings, expectedPurgeInterval, time.Minute)

	// Mutate the bucket auto-compaction setting
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().Remove("/spec/autoCompaction/databaseFragmentationThreshold/percent").Remove("/spec/autoCompaction/viewFragmentationThreshold/size").Replace("/spec/autoCompaction/viewFragmentationThreshold/percent", 65).Replace("/spec/autoCompaction/timeWindow/end", "17:30").Remove("/spec/autoCompaction/tombstonePurgeInterval"), time.Minute)

	// Update the expected settings to the new values. Values removed from the CRD should not be changed on the bucket
	expectedAutoCompactionSettings.Settings.DatabaseFragmentationThreshold.Percentage = 0 // Removed databaseFragmentationThreshold/percent
	expectedAutoCompactionSettings.Settings.ViewFragmentationThreshold.Size = 0           // Removed viewFragmentationThreshold/size
	expectedAutoCompactionSettings.Settings.ViewFragmentationThreshold.Percentage = 65    // Replaced viewFragmentationThreshold/percent with 65
	expectedAutoCompactionSettings.Settings.AllowedTimePeriod.ToHour = 17                 // Replaced timeWindow/end with 17:30
	expectedPurgeInterval = to.Ptr(float64(3))                                            // Removed tombstonePurgeInterval, value will be set to cluster default

	// Verify the settings have been correctly updated, with those that are removed inheriting values from the cluster
	e2eutil.MustVerifyCouchbaseBucketAutoCompactionSettings(t, kubernetes, cluster, bucket.Name, expectedAutoCompactionSettings, expectedPurgeInterval, time.Minute)

	// Delete the auto-compaction settings from the bucket CRD and expected settings
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().Remove("/spec/autoCompaction"), time.Minute)

	expectedAutoCompactionSettings.Enabled = false
	expectedAutoCompactionSettings.Settings = nil

	// Verify the auto-compaction settings have been removed from the cb bucket
	e2eutil.MustVerifyCouchbaseBucketAutoCompactionSettings(t, kubernetes, cluster, bucket.Name, expectedAutoCompactionSettings, nil, time.Minute)

	// Check the events match what we expect
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucket.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited, FuzzyMessage: bucket.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited, FuzzyMessage: bucket.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestCreateEditDeleteCouchbaseBucketAutoCompactionSettingsMagmaBackend tests that auto-compaction settings configured at the bucket level are set on the couchbase bucket
// and that editing and removing settings will be correctly reflected by the bucket when it has a magma storage backend.
func TestCreateEditDeleteCouchbaseBucketAutoCompactionSettingsMagmaBackend(t *testing.T) {
	// Platform configuration
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	clusterSize := constants.Size1

	// Expected bucket details after creation on couchbase server
	expectedAutoCompactionSettings := couchbaseutil.BucketAutoCompactionSettings{
		Enabled: true,
		Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
			MagmaFragmentationThresholdPercentage: 15,
		}}

	// Initial requested bucket as CRD
	bucket := &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "couchbase-bucket-autocompaction-magma",
			Annotations: map[string]string{
				"cao.couchbase.com/autoCompaction.magmaFragmentationPercentage": "15",
			},
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:    e2espec.NewResourceQuantityMi(1024),
			EvictionPolicy: couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			StorageBackend: couchbasev2.CouchbaseStorageBackendMagma,
		},
	}

	// The default purge interval is 3 days.
	expectedPurgeInterval := to.Ptr(float64(3))

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(1024)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Create the bucket
	bucketObj := e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Verify the initial settings are correct
	e2eutil.MustVerifyCouchbaseBucketAutoCompactionSettings(t, kubernetes, cluster, bucket.Name, expectedAutoCompactionSettings, expectedPurgeInterval, time.Minute)

	// Mutate the bucket auto-compaction setting
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().
		Replace("/metadata/annotations", map[string]string{"cao.couchbase.com/autoCompaction.magmaFragmentationPercentage": "45"}).
		Add("/spec/autoCompaction", &couchbasev2.AutoCompactionSpecBucket{TombstonePurgeInterval: &metav1.Duration{Duration: time.Duration(24) * time.Hour}}), time.Minute)

	// Update the expected settings to the new values.
	expectedAutoCompactionSettings.Settings.MagmaFragmentationThresholdPercentage = 45 // Updated the expected magma fragmentation percentage to the new expected value
	expectedPurgeInterval = to.Ptr(float64(1))                                         // Updated the expected purge interval to the new expected value

	// Verify the settings have been correctly updated, with those that are removed inheriting values from the cluster
	e2eutil.MustVerifyCouchbaseBucketAutoCompactionSettings(t, kubernetes, cluster, bucket.Name, expectedAutoCompactionSettings, expectedPurgeInterval, time.Minute)

	// Delete the auto-compaction settings from the bucket CRD and expected settings
	e2eutil.MustPatchBucket(t, kubernetes, bucketObj, jsonpatch.NewPatchSet().Remove("/spec/autoCompaction").Remove("/metadata/annotations"), time.Minute)

	expectedAutoCompactionSettings.Enabled = false
	expectedAutoCompactionSettings.Settings = nil

	// Verify the auto-compaction settings have been removed from the cb bucket
	e2eutil.MustVerifyCouchbaseBucketAutoCompactionSettings(t, kubernetes, cluster, bucket.Name, expectedAutoCompactionSettings, nil, time.Minute)

	// Check the events match what we expect
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucket.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited, FuzzyMessage: bucket.Name},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited, FuzzyMessage: bucket.Name},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
