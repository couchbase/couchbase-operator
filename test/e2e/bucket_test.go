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

	clusterConfig := e2eutil.BasicClusterConfig2
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)

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
		t.Fatalf("failed to see no buckets from client")
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

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.AdminHidden)

	bucketTypes := []string{"couchbase", "memcached", "ephemeral"}
	buckets := e2espec.GenerateValidBucketSettings(bucketTypes)
	for _, bucket := range buckets {
		e2eutil.MustNewBucket(t, targetKube, f.Namespace, bucket)
		e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
		e2eutil.MustDeleteBucket(t, targetKube, f.Namespace, bucket)
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, "default", 2*time.Minute)
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
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, constants.Size1, constants.AdminExposed)

	// Create a direct connection to a couchbase node.
	// When healthy change the memory quota, replicas, whether flushes are allowed and the compression mode.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
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
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
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
	enabled := true
	disabled := false

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.AdminExposed)

	// Once ready, alter a few parameters and ensure they are reverted by the operator.
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/EnableFlush", disabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", disabled), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent("default", testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", enabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/BucketReplicas", 3), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 3), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent("default", testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/IoPriority", cbmgr.IoPriorityTypeLow), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/IoPriority", cbmgr.IoPriorityTypeLow), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent("default", testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/IoPriority", cbmgr.IoPriorityTypeHigh), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
