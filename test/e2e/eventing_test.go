package e2e

import (
	"os"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Creates config map of 2 service groups and required bucket data
func createEventingConfigMap(nonEventingNodes, eventingNodes int) map[string]map[string]string {
	clusterConfig := e2eutil.BasicClusterConfig2
	serviceConfig1 := e2eutil.GetServiceConfigMap(nonEventingNodes, "test_config_1", []string{"data", "query", "index"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(eventingNodes, "test_config_2", []string{"eventing"})
	bucket1 := e2eutil.GetBucketConfigMap("eventingSrc", "couchbase", "high", 100, 1, true, false)
	bucket2 := e2eutil.GetBucketConfigMap("eventingMetaBucket", "couchbase", "high", 100, 1, true, false)
	bucket3 := e2eutil.GetBucketConfigMap("eventingDst", "couchbase", "high", 100, 1, true, false)
	return map[string]map[string]string{
		"cluster":              clusterConfig,
		"service1":             serviceConfig1,
		"service2":             serviceConfig2,
		"bucket1":              bucket1,
		"bucket2":              bucket2,
		"bucket3":              bucket3,
		"exposedFeatures":      map[string]string{"featureNames": "client"},
		"adminConsoleServices": map[string]string{"services": "data,eventing"},
	}
}

// Create eventing enabled cluster
// Create 3 buckets for eventing to work
// Deploy eventing function to verify the results in destination bucket
func TestEventingCreateEventingCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 3
	numOfDocs := 10
	clusterConfig := e2eutil.BasicClusterConfig2
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverTimeout"] = "10"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "eventing"})
	bucket1 := e2eutil.GetBucketConfigMap("eventingSrc", "couchbase", "high", 100, 1, true, false)
	bucket2 := e2eutil.GetBucketConfigMap("eventingMetaBucket", "couchbase", "high", 100, 1, true, false)
	bucket3 := e2eutil.GetBucketConfigMap("eventingDst", "couchbase", "high", 100, 1, true, false)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucket1,
		"bucket2":         bucket2,
		"bucket3":         bucket3,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	// Creating cluster with eventing
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AdminService, api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucket1["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucket2["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucket3["bucketName"])

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, configMap["bucket1"]["bucketName"], 1, numOfDocs)

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)

	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

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
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AdminService, api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket1"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket2"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket3"]["bucketName"])

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, configMap["bucket1"]["bucketName"], 0, numOfDocs)

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)

	// Cross check number of docs inserted reflected in eventing
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan interface{})
	dataInsertionError := make(chan error)

	// Clone the cluster here to avoid read/write races
	// Don't not use any Must* calls as it will cause a hang
	go func(cluster *api.CouchbaseCluster) {
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertion:
				break OuterLoop
			default:
				if err = e2eutil.InsertJsonDocsIntoBucket(targetKube, cluster, configMap["bucket1"]["bucketName"], numOfDocs, 1); err == nil {
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
	eventingClusterSizes := []int{2, 4, 3}
	prevClusterSize := clusterSize
	memberToBeAdded := clusterSize
	for _, eventingNodes = range eventingClusterSizes {
		membersAdded := []int{}
		membersRemoved := []int{}
		clusterSize = nonEventingNodes + eventingNodes
		sizeDiff := clusterSize - prevClusterSize
		if sizeDiff > 0 {
			for memberId := prevClusterSize; memberId < clusterSize; memberId++ {
				membersAdded = append(membersAdded, memberToBeAdded)
				memberToBeAdded++
			}
		} else {
			for memberId := memberToBeAdded + sizeDiff; memberId < memberToBeAdded; memberId++ {
				membersRemoved = append(membersRemoved, memberId)
			}
		}

		// Resize cluster and wait for healthy cluster
		service := 1
		testCouchbase = e2eutil.MustResizeCluster(t, service, eventingNodes, targetKube, testCouchbase, 20*time.Minute)

		switch {
		case clusterSize-prevClusterSize > 0:
			for _, memberIndex := range membersAdded {
				expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
			}
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
			for _, memberIndex := range membersRemoved {
				expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", memberIndex)
			}
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
		}
		prevClusterSize = clusterSize
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
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AdminService, api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket1"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket2"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket3"]["bucketName"])

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, configMap["bucket1"]["bucketName"], 0, numOfDocs)

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	e2eutil.MustDeployEventingFunction(t, targetKube, testCouchbase, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)

	// Cross check number of docs inserted reflected in eventing
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, eventingDstBucketName, numOfDocs, 2*time.Minute)

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan interface{})
	dataInsertionErr := make(chan error)

	// Clone the cluster here to avoid read/write races
	// Don't not use any Must* calls as it will cause a hang
	go func(cluster *api.CouchbaseCluster) {
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertion:
				break OuterLoop
			default:
				if err = e2eutil.InsertJsonDocsIntoBucket(targetKube, cluster, configMap["bucket1"]["bucketName"], numOfDocs, 1); err == nil {
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
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", memberId)
		expectedEvents.AddClusterPodEvent(testCouchbase, "FailedOver", memberId)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, newMemberToBeAdded), 3*time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", newMemberToBeAdded)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", memberId)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
