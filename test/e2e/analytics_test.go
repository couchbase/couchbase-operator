package e2e

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create cluster with Analytics service enabled
// Deploy analytics bucket and verify for bucket creation and data replication
func TestAnalyticsCreateDataSet(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 3
	numOfDocs := 50
	bucketName := "defBucket"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 1, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	if err := e2eutil.InsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 1, numOfDocs); err != nil {
		t.Fatal(err)
	}
	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.GetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsBucketName := "testAnalyticsBucket"
	analyticsDataset := "testDataset1"

	queryMap := []string{
		`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
		`create dataset ` + analyticsDataset + ` on ` + analyticsBucketName,
		`connect bucket ` + analyticsBucketName,
	}
	for _, query := range queryMap {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Verify data set doc count
	if err := e2eutil.VerifyDocCountInAnalyticsDataset(analyticsHostUrl, analyticsNodePortStr, analyticsDataset, constants.CbClusterUsername, constants.CbClusterPassword, numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func insertAnalyticsDocument(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, bucket, docID, docType string) error {
	// Establishing a client connection is essentially random so if we tried
	// to reuse this one there is a high probability that the target pod will
	// be killed and this will constantly error.
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, k8s, cluster)
	defer cleanup()

	b, err := client.GetBucket(bucket)
	if err != nil {
		return err
	}

	docKey := "doc" + docID
	docMap := map[string]string{
		"name":      "docName " + docID,
		"value":     "dummy Value " + docID,
		"valueType": docType,
	}

	// Convert map data to byte array
	docData, err := json.Marshal(docMap)
	if err != nil {
		return err
	}
	docData = append([]byte("value="), docData...)

	// Inserts document using client
	if err := client.InsertDoc(b, docKey, docData); err != nil {
		return err
	}
	return nil
}

// Create analytics enabled couchbase cluster
// Create analytics bucket with data sets and connect with couchbase bucket
// Resize cluster with analytics nodes and check for data and functional consistency
func TestAnalyticsResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 1
	numOfDocs := 10
	bucketName := "defBucket"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 1, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	if err := e2eutil.InsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 0, numOfDocs); err != nil {
		t.Fatal(err)
	}

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.GetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsBucketName := "testAnalyticsBucket"
	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"

	queryMap := []string{
		`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
		`create dataset ` + analyticsDataset1 + ` on ` + analyticsBucketName,
		`create dataset ` + analyticsDataset2 + ` on ` + analyticsBucketName + ` where valueType="type1"`,
		`create dataset ` + analyticsDataset3 + ` on ` + analyticsBucketName + ` where valueType="type2"`,
		`connect bucket ` + analyticsBucketName,
	}

	// Load default data set into couchbase bucket
	for _, query := range queryMap {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Function to insert data with two types of valueType varaibles
	dataInsertionErrChan := make(chan error)
	stopDataInsertionChan := make(chan interface{})
	numOfType1Docs := 0
	numOfType2Docs := 0

	docInsertFunc := func() {
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertionChan:
				break OuterLoop
			default:
				docID := strconv.Itoa(numOfDocs)
				docType := "type1"
				if numOfDocs%2 != 0 {
					docType = "type2"
				}
				if err = insertAnalyticsDocument(t, targetKube, testCouchbase, bucketName, docID, docType); err != nil {
					break
				}
				if numOfDocs%2 == 0 {
					numOfType1Docs++
				} else {
					numOfType2Docs++
				}
				numOfDocs++
			}
		}
		dataInsertionErrChan <- err
	}

	//Run doc populator in backgroud while cluster resize is happening
	go docInsertFunc()

	// Ensure the routine is shut down properly in the event of a fatality.
	stopped := false
	defer func() {
		if !stopped {
			close(stopDataInsertionChan)
			<-dataInsertionErrChan
		}
	}()

	// Resize cluster
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := clusterSize
	for _, clusterSize = range clusterSizes {
		service := 0
		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, 20*time.Minute)

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", clusterSize-1)
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
			expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", clusterSize)
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
		}
		prevClusterSize = clusterSize
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// To stop background data insertion and wait for function to complete
	close(stopDataInsertionChan)
	stopped = true
	if err := <-dataInsertionErrChan; err != nil {
		t.Fatal(err)
	}

	// Verify total docs in data sets
	datasetNames := []string{analyticsDataset1, analyticsDataset2, analyticsDataset3}
	dataSetDocCount := []int{numOfDocs, numOfType1Docs, numOfType2Docs}
	for index, datasetName := range datasetNames {
		if err := e2eutil.VerifyDocCountInAnalyticsDataset(analyticsHostUrl, analyticsNodePortStr, datasetName, constants.CbClusterUsername, constants.CbClusterPassword, dataSetDocCount[index], constants.Retries10); err != nil {
			t.Fatal(err)
		}
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy analyitcs enabled couchbase cluster and populate data
// Kill analytics enabled node and check the cluster status
func TestAnalyticsKillPods(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	t.Skip("cbas broken in 5.5.x, syntax broken in 6.0.x")

	clusterSizeWoAnalytics := 3
	clusterSizeOfAnalytics := 3
	clusterSize := clusterSizeWoAnalytics + clusterSizeOfAnalytics + 1
	numOfDocs := 10
	bucketName := "defBucket"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "analytics"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(clusterSizeWoAnalytics, "test_config_2", []string{"data", "query", "index"})
	serviceConfig3 := e2eutil.GetServiceConfigMap(clusterSizeOfAnalytics, "test_config_3", []string{"analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 1, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"service2":        serviceConfig2,
		"service3":        serviceConfig3,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	// Creates the client with exposed admin port
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	// Load default data set into couchbase bucket
	if err := e2eutil.InsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 0, numOfDocs); err != nil {
		t.Fatal(err)
	}

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.GetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsBucketName := "testAnalyticsBucket"
	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"

	queryMap := []string{
		`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
		`create dataset ` + analyticsDataset1 + ` on ` + analyticsBucketName,
		`create dataset ` + analyticsDataset2 + ` on ` + analyticsBucketName + ` where valueType="type1"`,
		`create dataset ` + analyticsDataset3 + ` on ` + analyticsBucketName + ` where valueType="type2"`,
		`connect bucket ` + analyticsBucketName,
	}

	for _, query := range queryMap {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Wait until analytics service is fully functional
	if err := e2eutil.VerifyDocCountInAnalyticsDataset(analyticsHostUrl, analyticsNodePortStr, analyticsDataset1, constants.CbClusterUsername, constants.CbClusterPassword, numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	// Function to insert data with two types of valueType varaibles
	dataInsertionErrChan := make(chan error)
	stopDataInsertionChan := make(chan interface{})
	numOfType1Docs := 0
	numOfType2Docs := 0

	docInsertFunc := func() {
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertionChan:
				break OuterLoop
			default:
				docID := strconv.Itoa(numOfDocs)
				docType := "type1"
				if numOfDocs%2 != 0 {
					docType = "type2"
				}
				if err = insertAnalyticsDocument(t, targetKube, testCouchbase, bucketName, docID, docType); err != nil {
					break
				}
				if numOfDocs%2 == 0 {
					numOfType1Docs++
				} else {
					numOfType2Docs++
				}
				numOfDocs++
			}
		}
		dataInsertionErrChan <- err
	}

	// Run doc populator in backgroud while cluster resize is happening
	go docInsertFunc()

	// Ensure the routine is shut down properly in the event of a fatality.
	stopped := false
	defer func() {
		if !stopped {
			close(stopDataInsertionChan)
			<-dataInsertionErrChan
		}
	}()

	podMemberIdsToKill := []int{4, 5, 6}
	newMemberIdToBeAdded := clusterSize
	for _, podMemberId := range podMemberIdsToKill {
		podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
		if err := e2eutil.KillMember(targetKube.KubeClient, f.Namespace, testCouchbase.Name, podMemberName, true); err != nil {
			t.Fatal(err)
		}

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, podMemberId), time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberId)

		if err := e2eutil.WaitForUnhealthyNodes(t, client, constants.Retries5, constants.Size1); err != nil {
			t.Fatalf("Mismatch in unhealthy nodes count: %v", err)
		}

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberFailedOverEvent(testCouchbase, podMemberId), time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "FailedOver", podMemberId)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, newMemberIdToBeAdded), 2*time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", newMemberIdToBeAdded)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 2*time.Minute)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", podMemberId)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

		newMemberIdToBeAdded++
	}

	// To stop background data insertion and wait for function to complete
	close(stopDataInsertionChan)
	stopped = true
	if err := <-dataInsertionErrChan; err != nil {
		t.Fatal(err)
	}

	// Verify document counts for each dataset in analytics bucket
	dataSetNames := []string{analyticsDataset1, analyticsDataset2, analyticsDataset3}
	dataSetCount := []int{numOfDocs, numOfType1Docs, numOfType2Docs}
	for index, dataSetName := range dataSetNames {
		if err := e2eutil.VerifyDocCountInAnalyticsDataset(analyticsHostUrl, analyticsNodePortStr, dataSetName, constants.CbClusterUsername, constants.CbClusterPassword, dataSetCount[index], constants.Retries5); err != nil {
			t.Fatal(err)
		}
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy analyitcs enabled couchbase cluster over PVC and populate data
// Kill analytics enabled node and check the cluster and PVC status
// Kill all analytics nodes at once and check for node recovery
func TestAnalyticsKillPodsWithPVC(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	numOfDocs := 100
	clusterSize := 3
	bucketName := "PVBucket"
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "analytics"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["analyticsVolMnt"] = pvcName + "," + pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", constants.Mem256Mb, 1, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec, f.PlatformType)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	if err := e2eutil.InsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.GetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsBucketName := "testAnalyticsBucket"
	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"

	queryMap := []string{
		`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
		`create dataset ` + analyticsDataset1 + ` on ` + analyticsBucketName,
		`create dataset ` + analyticsDataset2 + ` on ` + analyticsBucketName + ` where valueType="type1"`,
		`create dataset ` + analyticsDataset3 + ` on ` + analyticsBucketName + ` where valueType="type2"`,
		`connect bucket ` + analyticsBucketName,
	}

	// Load default data set into couchbase bucket
	for _, query := range queryMap {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Wait till anlytics service become functional
	if err := e2eutil.VerifyDocCountInAnalyticsDataset(analyticsHostUrl, analyticsNodePortStr, analyticsDataset1, constants.CbClusterUsername, constants.CbClusterPassword, numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	// Loop to kill the pod containers
	for podMemberId := 0; podMemberId < clusterSize; podMemberId++ {
		podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
		// Deletes only the pod leaving the pvc active
		if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberName, &metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	// Loop to wait for Member down events for pods
	for podMemberId := 0; podMemberId < clusterSize; podMemberId++ {
		if podMemberId == 0 {
			continue
		}
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, podMemberId), 2*time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberId)
	}

	// Loop to wait for pod recovery events to occur
	for podMemberId := 0; podMemberId < clusterSize; podMemberId++ {
		if podMemberId == 0 {
			continue
		}
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.MemberRecoveredEvent(testCouchbase, podMemberId), time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRecovered", podMemberId)
	}

	// Event checks for rebalance to start and complete successfully
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), time.Minute)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	dataSetNames := []string{analyticsDataset1, analyticsDataset2, analyticsDataset3}
	dataSetDocCount := []int{numOfDocs, 0, 0}
	for index, dataSetName := range dataSetNames {
		if err := e2eutil.VerifyDocCountInAnalyticsDataset(analyticsHostUrl, analyticsNodePortStr, dataSetName, constants.CbClusterUsername, constants.CbClusterPassword, dataSetDocCount[index], constants.Retries5); err != nil {
			t.Fatal(err)
		}
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
