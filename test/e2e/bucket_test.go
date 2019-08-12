package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	pkg_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/gocbmgr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestBucketAddRemoveBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration
	clusterSize := 3
	names := []string{
		"bucket1",
		"bucket2",
		"bucket3",
		"bucket4",
	}

	buckets := []runtime.Object{
		&couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[0],
			},
			Spec: couchbasev2.CouchbaseBucketSpec{
				MemoryQuota:        constants.Mem256Mb,
				Replicas:           pkg_constants.BucketReplicasOne,
				IoPriority:         pkg_constants.BucketIoPriorityHigh,
				EvictionPolicy:     pkg_constants.BucketEvictionPolicyFullEviction,
				ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
				EnableFlush:        constants.BucketFlushEnabled,
				EnableIndexReplica: constants.IndexReplicaEnabled,
				CompressionMode:    "passive",
			},
		},
		&couchbasev2.CouchbaseMemcachedBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[1],
			},
			Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
				MemoryQuota: constants.Mem256Mb,
				EnableFlush: constants.BucketFlushDisabled,
			},
		},
		&couchbasev2.CouchbaseEphemeralBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[2],
			},
			Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
				MemoryQuota:        101,
				Replicas:           pkg_constants.BucketReplicasOne,
				IoPriority:         pkg_constants.BucketIoPriorityHigh,
				EvictionPolicy:     pkg_constants.BucketEvictionPolicyNoEviction,
				ConflictResolution: pkg_constants.BucketConflictResolutionTimestamp,
				EnableFlush:        constants.BucketFlushEnabled,
				CompressionMode:    "passive",
			},
		},
		&couchbasev2.CouchbaseEphemeralBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: names[3],
			},
			Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
				MemoryQuota:        101,
				Replicas:           pkg_constants.BucketReplicasOne,
				IoPriority:         pkg_constants.BucketIoPriorityHigh,
				EvictionPolicy:     pkg_constants.BucketEvictionPolicyNRUEviction,
				ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
				EnableFlush:        constants.BucketFlushEnabled,
				CompressionMode:    "passive",
			},
		},
	}

	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = 1024
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.MustCreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	for i, bucket := range buckets {
		e2eutil.MustNewBucket(t, targetKube, f.Namespace, bucket)
		e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, names[:i+1], 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

		currentBuckets, err := client.GetBuckets()
		if err != nil && len(currentBuckets) != i+1 {
			e2eutil.Die(t, fmt.Errorf("failed to see all buckets from client"))
		}
	}

	for _, bucket := range buckets {
		e2eutil.MustDeleteBucket(t, targetKube, f.Namespace, bucket)
	}
	for _, name := range names {
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, name, 2*time.Minute)
	}
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	currentBuckets, err := client.GetBuckets()
	if err != nil && len(currentBuckets) != 0 {
		e2eutil.Die(t, fmt.Errorf("failed to see no buckets from client"))
	}

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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	bucketTypes := []string{"couchbase", "memcached", "ephemeral"}
	buckets := e2espec.GenerateValidBucketSettings(bucketTypes)
	for _, bucket := range buckets {
		name := e2eutil.MustGetBucketName(t, bucket)
		e2eutil.MustNewBucket(t, targetKube, f.Namespace, bucket)
		e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{name}, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
		e2eutil.MustDeleteBucket(t, targetKube, f.Namespace, bucket)
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
	kubernetes := f.GetCluster(0)

	// Constants
	bucketName := "default"
	enabled := true
	disabled := false

	// Create the cluster.
	bucket := e2eutil.MustNewBucket(t, kubernetes, f.Namespace, e2espec.DefaultBucket)
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, constants.Size1)
	e2eutil.MustWaitUntilBucketsExists(t, kubernetes, cluster, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Create a direct connection to a couchbase node.
	// When healthy change the memory quota, replicas, whether flushes are allowed and the compression mode.
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/MemoryQuota", 128), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", 128), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/MemoryQuota", 256), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", 256), time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/Replicas", 2), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 2), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/Replicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/EnableFlush", disabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", disabled), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/EnableFlush", enabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", enabled), time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/CompressionMode", cbmgr.CompressionModeActive), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModeActive), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/CompressionMode", cbmgr.CompressionModeOff), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModeOff), time.Minute)
	e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/CompressionMode", cbmgr.CompressionModePassive), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModePassive), time.Minute)

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
// 9. Check the events to make sure the operator took the correct actions
func TestRevertExternalBucketUpdates(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	bucketName := "default"

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Once ready, alter a few parameters and ensure they are reverted by the operator.
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/EnableFlush", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent(e2espec.DefaultBucket.Name, testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", true), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/BucketReplicas", 3), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent(e2espec.DefaultBucket.Name, testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/IoPriority", cbmgr.IoPriorityTypeLow), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent(e2espec.DefaultBucket.Name, testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/IoPriority", cbmgr.IoPriorityTypeHigh), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create a bucket.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)

	// Create a cluster with buckets unmanaged.
	couchbase := e2espec.NewBasicClusterSpec(clusterSize)
	couchbase.Spec.Buckets.Managed = false
	couchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, couchbase)

	// Ensure the bucket doesn't get created.
	if err := e2eutil.WaitUntilBucketsExists(targetKube, couchbase, []string{e2espec.DefaultBucket.Name}, time.Minute); err == nil {
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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3
	bucketName := "simba"
	labels := map[string]string{
		"loves": "nala",
	}

	// Create a default bucket and a labelled bucket.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	bucket := e2espec.DefaultBucket.DeepCopy()
	bucket.Name = bucketName
	bucket.Labels = labels
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, bucket)

	// Create a cluster that selects only labelled buckets.
	couchbase := e2espec.NewBasicClusterSpec(clusterSize)
	couchbase.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: labels,
	}
	couchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, couchbase)

	// Ensure the unlabelled bucket doesn't get created.
	if err := e2eutil.WaitUntilBucketsExists(targetKube, couchbase, []string{e2espec.DefaultBucket.Name}, time.Minute); err == nil {
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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3
	victim := 1
	foreignBucketName := "foreign"

	// Create the cluster
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = 1024
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustPopulateBucket(t, targetKube, testCouchbase, e2espec.DefaultBucket.Name, 10)

	// Pause the operator, failover the victim, then create a new bucket and populate it.
	// The operator - when restarted - should flag the node for delta recovery, but Server
	// will not allow this due to not all buckets being delta recoverable ("default" contains
	// partial data whereas "foreign" contains none).
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	e2eutil.MustFailoverNode(t, targetKube, testCouchbase, victim, 5*time.Minute)
	e2eutil.MustCreateBucket(t, targetKube, testCouchbase, foreignBucketName, time.Minute)
	// Wait for the active nodes to warm up. Operator behaves differently if this hasn't happened
	time.Sleep(10 * time.Second)
	e2eutil.MustPopulateBucket(t, targetKube, testCouchbase, foreignBucketName, 10)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)
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
