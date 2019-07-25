package e2e

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
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
		"exposedFeatures": {"featureNames": "client"},
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AdminService, api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	// Can get stuck on rebalancing.
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 0, numOfDocs)
	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.MustGetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsDataset := "testDataset1"

	queries := []string{
		"CREATE DATASET " + analyticsDataset + " ON `" + bucketName + "`",
		"CONNECT LINK Local",
	}

	// TODO: Kill me ASAP as it's DP code.
	version, err := couchbaseutil.NewVersion(f.CouchbaseServerVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}
	if version.Major() < 6 {
		analyticsBucketName := "testAnalyticsBucket"
		queries = []string{
			`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
			`create dataset ` + analyticsDataset + ` on ` + analyticsBucketName,
			`connect bucket ` + analyticsBucketName,
		}
	}

	for _, query := range queries {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Verify data set doc count
	e2eutil.MustVerifyDocCountInAnalyticsDataset(t, analyticsHostUrl, analyticsNodePortStr, analyticsDataset, constants.CbClusterUsername, constants.CbClusterPassword, numOfDocs, 5*time.Minute)
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func insertAnalyticsDocument(k8s *types.Cluster, cluster *api.CouchbaseCluster, bucket, docID, docType string) error {
	// Establishing a client connection is essentially random so if we tried
	// to reuse this one there is a high probability that the target pod will
	// be killed and this will constantly error.
	client, cleanup, err := e2eutil.CreateAdminConsoleClient(k8s, cluster)
	if err != nil {
		return err
	}
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
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AdminService, api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 0, numOfDocs)

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.MustGetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"

	queries := []string{
		"CREATE DATASET " + analyticsDataset1 + " ON `" + bucketName + "`",
		"CREATE DATASET " + analyticsDataset2 + " ON `" + bucketName + "` WHERE `valueType` = \"type1\"",
		"CREATE DATASET " + analyticsDataset3 + " ON `" + bucketName + "` WHERE `valueType` = \"type2\"",
		"CONNECT LINK Local",
	}

	// TODO: Kill me ASAP as it's DP code.
	version, err := couchbaseutil.NewVersion(f.CouchbaseServerVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}
	if version.Major() < 6 {
		analyticsBucketName := "testAnalyticsBucket"
		queries = []string{
			`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
			`create dataset ` + analyticsDataset1 + ` on ` + analyticsBucketName,
			`create dataset ` + analyticsDataset2 + ` on ` + analyticsBucketName + ` where valueType="type1"`,
			`create dataset ` + analyticsDataset3 + ` on ` + analyticsBucketName + ` where valueType="type2"`,
			`connect bucket ` + analyticsBucketName,
		}
	}

	// Load default data set into couchbase bucket
	for _, query := range queries {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Function to insert data with two types of valueType variables
	dataInsertionErrChan := make(chan error)
	stopDataInsertionChan := make(chan interface{})
	numOfType1Docs := 0
	numOfType2Docs := 0

	// Clone the cluster here to avoid read/write races
	// Don't not use any Must* calls as it will cause a hang
	go func(cluster *api.CouchbaseCluster) {
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
				if err = insertAnalyticsDocument(targetKube, cluster, bucketName, docID, docType); err != nil {
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
	}(testCouchbase.DeepCopy())

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

	// To stop background data insertion and wait for function to complete.
	// Wait for a short while so that the load generator has a chance to successfully
	// commit documents to only pods that exist now.  Note that the port forward has
	// a timeout of 1 minute, so wait for two to be certain.
	time.Sleep(2 * time.Minute)
	close(stopDataInsertionChan)
	stopped = true
	if err := <-dataInsertionErrChan; err != nil {
		e2eutil.Die(t, err)
	}

	// Verify total docs in data sets
	datasetNames := []string{analyticsDataset1, analyticsDataset2, analyticsDataset3}
	dataSetDocCount := []int{numOfDocs, numOfType1Docs, numOfType2Docs}
	for index, datasetName := range datasetNames {
		e2eutil.MustVerifyDocCountInAnalyticsDataset(t, analyticsHostUrl, analyticsNodePortStr, datasetName, constants.CbClusterUsername, constants.CbClusterPassword, dataSetDocCount[index], 5*time.Minute)
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy analytics enabled couchbase cluster and populate data
// Kill analytics enabled node and check the cluster status
func TestAnalyticsKillPods(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	version, err := couchbaseutil.NewVersion(f.CouchbaseServerVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}
	if version.Major() < 6 {
		t.Skip("cbas broken in 5.5.x")
	}

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
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AdminService, api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	// Load default data set into couchbase bucket
	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 0, numOfDocs)

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.MustGetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"

	queries := []string{
		"CREATE DATASET " + analyticsDataset1 + " ON `" + bucketName + "`",
		"CREATE DATASET " + analyticsDataset2 + " ON `" + bucketName + "` WHERE `valueType` = \"type1\"",
		"CREATE DATASET " + analyticsDataset3 + " ON `" + bucketName + "` WHERE `valueType` = \"type2\"",
		"CONNECT LINK Local",
	}

	// Load default data set into couchbase bucket
	for _, query := range queries {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Wait until analytics service is fully functional
	e2eutil.MustVerifyDocCountInAnalyticsDataset(t, analyticsHostUrl, analyticsNodePortStr, analyticsDataset1, constants.CbClusterUsername, constants.CbClusterPassword, numOfDocs, 5*time.Minute)

	// Function to insert data with two types of valueType variables
	dataInsertionErrChan := make(chan error)
	stopDataInsertionChan := make(chan interface{})
	numOfType1Docs := 0
	numOfType2Docs := 0

	// Clone the cluster here to avoid read/write races
	// Don't not use any Must* calls as it will cause a hang
	go func(cluster *api.CouchbaseCluster) {
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
				if err = insertAnalyticsDocument(targetKube, cluster, bucketName, docID, docType); err != nil {
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
	}(testCouchbase.DeepCopy())

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
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberId, true)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, podMemberId), time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberId)

		e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, constants.Size1, time.Minute)

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

	// To stop background data insertion and wait for function to complete.
	// Wait for a short while so that the load generator has a chance to successfully
	// commit documents to only pods that exist now.  Note that the port forward has
	// a timeout of 1 minute, so wait for two to be certain.
	time.Sleep(2 * time.Minute)
	close(stopDataInsertionChan)
	stopped = true
	if err := <-dataInsertionErrChan; err != nil {
		e2eutil.Die(t, err)
	}

	// Verify document counts for each dataset in analytics bucket
	dataSetNames := []string{analyticsDataset1, analyticsDataset2, analyticsDataset3}
	dataSetCount := []int{numOfDocs, numOfType1Docs, numOfType2Docs}
	for index, dataSetName := range dataSetNames {
		e2eutil.MustVerifyDocCountInAnalyticsDataset(t, analyticsHostUrl, analyticsNodePortStr, dataSetName, constants.CbClusterUsername, constants.CbClusterPassword, dataSetCount[index], 5*time.Minute)
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy analytics enabled couchbase cluster over PVC and populate data
// Kill analytics enabled node and check the cluster and PVC status
// Kill all analytics nodes at once and check for node recovery
func TestAnalyticsKillPodsWithPVC(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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

	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec)

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AdminService, api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	e2eutil.MustInsertJsonDocsIntoBucket(t, targetKube, testCouchbase, bucketName, 1, numOfDocs)

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	analyticsHostUrl, analyticsNodePortStr, cleanup := e2eutil.MustGetAnalyticsIpAndPort(t, targetKube, f.Namespace, analyticsNodeName)
	defer cleanup()

	analyticsDataset1 := "testDataset1"
	analyticsDataset2 := "testDataset2"
	analyticsDataset3 := "testDataset3"

	queries := []string{
		"CREATE DATASET " + analyticsDataset1 + " ON `" + bucketName + "`",
		"CREATE DATASET " + analyticsDataset2 + " ON `" + bucketName + "` WHERE `valueType` = \"type1\"",
		"CREATE DATASET " + analyticsDataset3 + " ON `" + bucketName + "` WHERE `valueType` = \"type2\"",
		"CONNECT LINK Local",
	}

	// TODO: Kill me ASAP as it's DP code.
	version, err := couchbaseutil.NewVersion(f.CouchbaseServerVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}
	if version.Major() < 6 {
		analyticsBucketName := "testAnalyticsBucket"
		queries = []string{
			`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
			`create dataset ` + analyticsDataset1 + ` on ` + analyticsBucketName,
			`create dataset ` + analyticsDataset2 + ` on ` + analyticsBucketName + ` where valueType="type1"`,
			`create dataset ` + analyticsDataset3 + ` on ` + analyticsBucketName + ` where valueType="type2"`,
			`connect bucket ` + analyticsBucketName,
		}
	}

	// Load default data set into couchbase bucket
	for _, query := range queries {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(analyticsHostUrl, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Wait till anlytics service become functional
	e2eutil.MustVerifyDocCountInAnalyticsDataset(t, analyticsHostUrl, analyticsNodePortStr, analyticsDataset1, constants.CbClusterUsername, constants.CbClusterPassword, numOfDocs, 5*time.Minute)

	// Loop to kill the pod containers
	for podMemberId := 0; podMemberId < clusterSize; podMemberId++ {
		podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
		// Deletes only the pod leaving the pvc active
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, podMemberName, f.Namespace); err != nil {
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
		e2eutil.MustVerifyDocCountInAnalyticsDataset(t, analyticsHostUrl, analyticsNodePortStr, dataSetName, constants.CbClusterUsername, constants.CbClusterPassword, dataSetDocCount[index], 5*time.Minute)
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
