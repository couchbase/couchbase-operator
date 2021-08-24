package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBucketAddRemoveBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
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
		&couchbasev2.CouchbaseMemcachedBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[1],
			},
			Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
				MemoryQuota: e2espec.NewResourceQuantityMi(256),
				EnableFlush: false,
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
		&couchbasev2.CouchbaseEphemeralBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[3],
			},
			Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
				MemoryQuota:        e2espec.NewResourceQuantityMi(101),
				Replicas:           1,
				IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
				EvictionPolicy:     couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction,
				ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
				EnableFlush:        true,
				CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
			},
		},
	}

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(1024)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	for _, bucket := range buckets {
		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	}

	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, targetKube, bucket)
	}

	for _, name := range names {
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, name, 2*time.Minute)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets added seqentially
	// * Buckets removed
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Repeat{Times: len(buckets), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted}},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBucketAddRemoveExtended(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	bucketTypes := []string{"couchbase", "memcached", "ephemeral"}

	buckets := e2espec.GenerateValidBucketSettings(bucketTypes)
	for _, bucket := range buckets {
		name := bucket.GetName()
		e2eutil.MustNewBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
		e2eutil.MustDeleteBucket(t, targetKube, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, name, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	// Avoid a race where Couchbase has been updated but the event not raise yet.
	time.Sleep(10 * time.Second)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 9, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited}},
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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).CouchbaseBucket()

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	testCouchbase := clusterOptions().WithEphemeralTopology(1).MustCreate(t, targetKube)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	// Once ready, alter a few parameters and ensure they are reverted by the operator.
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/EnableFlush", false), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucket.GetName(), jsonpatch.NewPatchSet().Test("/EnableFlush", true), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/BucketReplicas", 3), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucket.GetName(), jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucket.GetName(), jsonpatch.NewPatchSet().Replace("/IoPriority", couchbaseutil.IoPriorityTypeLow), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucket.GetName(), jsonpatch.NewPatchSet().Test("/IoPriority", couchbaseutil.IoPriorityTypeHigh), time.Minute)
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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestBucketUnmanaged ensures the operator doesn't touch buckets when they are
// unmanaged.
func TestBucketUnmanaged(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create a bucket.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	// Create a cluster with buckets unmanaged.
	couchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	couchbase.Spec.Buckets.Managed = false
	couchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, couchbase)

	// Ensure the bucket doesn't get created.
	if err := e2eutil.WaitUntilBucketExists(targetKube, couchbase, bucket, time.Minute); err == nil {
		e2eutil.Die(t, fmt.Errorf("bucket created unexpectedly"))
	}

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, targetKube, couchbase, expectedEvents)
}

// TestBucketSelection ensures the operator only touches buckets that match the
// label selector.
func TestBucketSelection(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	bucketName := "simba"
	labels := map[string]string{
		"loves": "nala",
	}

	// Create a default bucket and a labelled bucket.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket())
	bucket := e2espec.DefaultBucket()
	bucket.Name = bucketName
	bucket.Labels = labels
	e2eutil.MustNewBucket(t, targetKube, bucket)

	// Create a cluster that selects only labelled buckets.
	couchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	couchbase.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: labels,
	}
	couchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, couchbase)

	// Ensure the unlabelled bucket doesn't get created.
	if err := e2eutil.WaitUntilBucketExists(targetKube, couchbase, e2espec.DefaultBucket(), time.Minute); err == nil {
		e2eutil.Die(t, fmt.Errorf("bucket created unexpectedly"))
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Only the labelled bucket is created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucketName},
	}

	ValidateEvents(t, targetKube, couchbase, expectedEvents)
}

// TestDeltaRecoveryImpossible ensures that the operator handles the situation where
// it can attempt a delta node recovery, however Couchbase server prevents it.  This
// is an esoteric case that shouldn't happen in reality, but it can, because users.
func TestDeltaRecoveryImpossible(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	victim := 1
	foreignBucketName := "foreign"

	// Create the cluster
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityMi(1024)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).MustCreate(t, targetKube, testCouchbase)

	// Pause the operator, failover the victim, then create a new bucket and populate it.
	// The operator - when restarted - should flag the node for delta recovery, but Server
	// will not allow this due to not all buckets being delta recoverable ("default" contains
	// partial data whereas "foreign" contains none).
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	e2eutil.MustFailoverNode(t, targetKube, testCouchbase, victim, 5*time.Minute)
	e2eutil.MustCreateBucket(t, targetKube, testCouchbase, foreignBucketName, time.Minute)
	// Wait for the active nodes to warm up. Operator behaves differently if this hasn't happened
	time.Sleep(10 * time.Second)
	e2eutil.NewDocumentSet(foreignBucketName, f.DocsCount).MustCreate(t, targetKube, testCouchbase)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Failed node added back, fails delta rebalance
	// * Failed node added back, succeeds full rebalance
	// * Foreign bucket is deleted
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
