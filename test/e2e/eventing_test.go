package e2e

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	bucketNames = []string{
		sourceBucket.Name,
		destinationBucket.Name,
		metadataBucket.Name,
	}

	sourceBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "source",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(100),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	destinationBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "destination",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(100),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	metadataBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "metadata",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(100),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}
)

var (
	// dataServiceMemoryQuota is enough memory to support the bucket requirements
	dataServiceMemoryQuota = e2espec.NewResourceQuantityMi(300)
)

const (
	// function is a basic test eventing function that populates a destination
	// bucket with a test value when a document appears in a source bucket.  It
	// deletes it when the corresponding source document is deleted.
	function = `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`
)

// Create eventing enabled cluster
// Create 3 buckets for eventing to work
// Deploy eventing function to verify the results in destination bucket.
func TestEventingCreateEventingCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3
	numOfDocs := 10

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, sourceBucket)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, destinationBucket)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, metadataBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, couchbasev2.EventingService)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = dataServiceMemoryQuota
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, bucketNames, time.Minute)

	// When ready, deploy an eventing function to create documents in a destination
	// bucket based on source bucket documents. Populate the source and ensure the
	// documents appear in the destination.
	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, "test", sourceBucket.Name, metadataBucket.Name, destinationBucket.Name, function, time.Minute)
	e2eutil.MustInsertJSONDocsIntoBucket(t, targetKube, testCouchbase, sourceBucket.Name, 1, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, destinationBucket.Name, numOfDocs, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create eventing enabled couchbase cluster with eventing buckets.
// Resize the cluster when the eventing deployment is active and verify the results.
func TestEventingResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, sourceBucket)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, destinationBucket)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, metadataBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, couchbasev2.EventingService)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = dataServiceMemoryQuota
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, bucketNames, time.Minute)

	// When ready deploy the eventing function and generate workload.  Scale the cluster
	// up and down then stop the workload.  Expect the number of documents in the destination
	// bucket to equal those in the source.
	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, "test", sourceBucket.Name, metadataBucket.Name, destinationBucket.Name, function, time.Minute)
	stop := e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, sourceBucket.Name)
	defer stop()
	for _, newClusterSize := range []int{2, 3, 2} {
		testCouchbase = e2eutil.MustResizeCluster(t, 0, newClusterSize, targetKube, testCouchbase, 20*time.Minute)
	}
	stop()
	time.Sleep(time.Minute) // Wait for eventing to catch up
	itemCount := e2eutil.MustGetItemCount(t, targetKube, testCouchbase, sourceBucket.Name, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, destinationBucket.Name, int(itemCount), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets created
	// * Cluster scales from 1 -> 2
	// * Cluster scales from 2 -> 3
	// * Cluster scales from 3 -> 2
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create couchbase cluster with eventing service and required buckets.
// Kill all the eventing nodes one by one and check for eventing stability.
func TestEventingKillEventingPods(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, sourceBucket)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, destinationBucket)
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, metadataBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.Servers[0].Services = append(testCouchbase.Spec.Servers[0].Services, couchbasev2.EventingService)
	testCouchbase.Spec.ClusterSettings.DataServiceMemQuota = dataServiceMemoryQuota
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, targetKube.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, bucketNames, time.Minute)

	// When ready deploy the eventing function and generate workload.  Kill pods in sequence
	// then stop the workload.  Expect the number of documents in the destination bucket to
	// equal those in the source.
	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, "test", sourceBucket.Name, metadataBucket.Name, destinationBucket.Name, function, time.Minute)
	stop := e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, sourceBucket.Name)
	defer stop()
	for _, victim := range []int{0, 1, 2} {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, true)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 20*time.Minute)
	}
	stop()
	time.Sleep(time.Minute) // Wait for eventing to catch up
	itemCount := e2eutil.MustGetItemCount(t, targetKube, testCouchbase, sourceBucket.Name, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, destinationBucket.Name, int(itemCount), 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster goes down, fails over and recovers N times
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Repeat{Times: 3, Validator: e2eutil.PodDownFailoverRecoverySequence()},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
