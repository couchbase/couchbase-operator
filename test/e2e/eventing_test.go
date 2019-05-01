package e2e

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
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
			MemoryQuota:        100,
			Replicas:           1,
			IoPriority:         "high",
			EvictionPolicy:     "fullEviction",
			ConflictResolution: "seqno",
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    "passive",
		},
	}

	destinationBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "destination",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        100,
			Replicas:           1,
			IoPriority:         "high",
			EvictionPolicy:     "fullEviction",
			ConflictResolution: "seqno",
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    "passive",
		},
	}

	metadataBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: "metadata",
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        100,
			Replicas:           1,
			IoPriority:         "high",
			EvictionPolicy:     "fullEviction",
			ConflictResolution: "seqno",
			EnableFlush:        true,
			EnableIndexReplica: false,
			CompressionMode:    "passive",
		},
	}
)

// Creates config map of 2 service groups and required bucket data
func createEventingConfigMap(nonEventingNodes, eventingNodes int) map[string]map[string]string {
	clusterConfig := e2eutil.BasicClusterConfig2
	serviceConfig1 := e2eutil.GetServiceConfigMap(nonEventingNodes, "test_config_1", []string{"data", "query", "index"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(eventingNodes, "test_config_2", []string{"eventing"})
	return map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
	}
}

// Create eventing enabled cluster
// Create 3 buckets for eventing to work
// Deploy eventing function to verify the results in destination bucket
func TestEventingCreateEventingCluster(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 3
	numOfDocs := 10
	clusterConfig := e2eutil.BasicClusterConfig2
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverTimeout"] = "10"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "eventing"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

	// Creating cluster with eventing
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, sourceBucket)
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, destinationBucket)
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, metadataBucket)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, bucketNames, time.Minute)

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, sourceBucket.Name, 1, numOfDocs)

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := sourceBucket.Name
	eventingMetaBucketName := metadataBucket.Name
	eventingDstBucketName := destinationBucket.Name
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)

	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create eventing enabled couchbase cluster with eventing buckets
// Resize the cluster when the eventing deployment is active and verify the results
func TestEventingResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	nonEventingNodes := 2
	eventingNodes := 3
	clusterSize := nonEventingNodes + eventingNodes
	numOfDocs := 10
	configMap := createEventingConfigMap(nonEventingNodes, eventingNodes)

	// Creating cluster with eventing
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, sourceBucket)
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, destinationBucket)
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, metadataBucket)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, bucketNames, time.Minute)

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, sourceBucket.Name, 0, numOfDocs)

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := sourceBucket.Name
	eventingMetaBucketName := metadataBucket.Name
	eventingDstBucketName := destinationBucket.Name
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)

	// Cross check number of docs inserted reflected in eventing
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan interface{})
	dataInsertionError := make(chan error)

	// Clone the cluster here to avoid read/write races
	// Don't not use any Must* calls as it will cause a hang
	go func(cluster *couchbasev2.CouchbaseCluster) {
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertion:
				break OuterLoop
			default:
				if err = e2eutil.InsertJsonDocsIntoBucket(targetKube, cluster, sourceBucket.Name, numOfDocs, 1); err == nil {
					numOfDocs++
				}
			}
		}
		dataInsertionError <- err
	}(testCouchbase.DeepCopy())

	// Ensure the routine is shut down properly in the event of a fatality.
	stopped := false
	defer func() {
		if !stopped {
			close(stopDataInsertion)
			<-dataInsertionError
		}
	}()

	// Don't scale this too high or it will pwn your laptop and the cluster will go unresponsive
	for _, eventingNodes = range []int{2, 4, 3} {
		testCouchbase = e2eutil.MustResizeCluster(t, 1, eventingNodes, targetKube, testCouchbase, 20*time.Minute)
	}

	// To stop background data insertion and wait for function to complete.
	// Wait for a short while so that the load generator has a chance to successfully
	// commit documents to only pods that exist now.  Note that the port forward has
	// a timeout of 1 minute, so wait for two to be certain.
	time.Sleep(2 * time.Minute)
	close(stopDataInsertion)
	stopped = true
	if err := <-dataInsertionError; err != nil {
		t.Fatal(err)
	}

	// Cross check number of docs inserted reflected in eventing
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleUpSequence(2),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create couchbase cluster with eventing service and required buckets
// Kill all the eventing nodes one by one and check for eventing stability
func TestEventingKillEventingPods(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	nonEventingNodes := 2
	eventingNodes := 3
	clusterSize := nonEventingNodes + eventingNodes
	numOfDocs := 10
	configMap := createEventingConfigMap(nonEventingNodes, eventingNodes)

	// Creating cluster with eventing
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, sourceBucket)
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, destinationBucket)
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, metadataBucket)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, bucketNames, time.Minute)

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, sourceBucket.Name, 0, numOfDocs)

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := sourceBucket.Name
	eventingMetaBucketName := metadataBucket.Name
	eventingDstBucketName := destinationBucket.Name
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)

	// Cross check number of docs inserted reflected in eventing
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan interface{})
	dataInsertionErr := make(chan error)

	// Clone the cluster here to avoid read/write races
	// Don't not use any Must* calls as it will cause a hang
	go func(cluster *couchbasev2.CouchbaseCluster) {
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertion:
				break OuterLoop
			default:
				if err = e2eutil.InsertJsonDocsIntoBucket(targetKube, cluster, sourceBucket.Name, numOfDocs, 1); err == nil {
					numOfDocs++
				}
			}
		}
		dataInsertionErr <- err
	}(testCouchbase.DeepCopy())

	// Ensure the routine is shut down properly in the event of a fatality.
	stopped := false
	defer func() {
		if !stopped {
			close(stopDataInsertion)
			<-dataInsertionErr
		}
	}()

	newMemberToBeAdded := clusterSize
	for _, memberId := range []int{2, 3, 4} {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, memberId, true)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberId), 30*time.Second)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, newMemberToBeAdded), 3*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
		newMemberToBeAdded++
	}

	// To stop background data insertion and wait for function to complete.
	// Wait for a short while so that the load generator has a chance to successfully
	// commit documents to only pods that exist now.  Note that the port forward has
	// a timeout of 1 minute, so wait for two to be certain.
	time.Sleep(2 * time.Minute)
	close(stopDataInsertion)
	stopped = true
	if err := <-dataInsertionErr; err != nil {
		t.Fatal(err)
	}

	// Cross check number of docs inserted reflected in eventing
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Repeat{Times: 3, Validator: e2eutil.PodDownFailoverRecoverySequence()},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
