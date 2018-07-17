package e2e

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type queryResult struct {
	Results []map[string]int
}

// Create cluster with Analytics service enabled
// Deploy analytics bucket and verify for bucket creation and data replication
func TestAnalyticsCreateDataSet(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	clusterSize := 3
	numOfDocs := 50
	bucketName := "defBucket"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", e2eutil.Mem256Mb, 1, e2eutil.BucketFlushEnabled, e2eutil.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "analytics")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "data")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "eventing")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "index")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "query")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "search")

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	analyticsNodePortStr := strconv.Itoa(int(testCouchbase.Status.ExposedPorts[analyticsNodeName].AnalyticsServicePort))

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	k8sMasterIp, err := f.GetKubeHostname(targetKubeName)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, bucketName, 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

	analyticsBucketName := "testAnalyticsBucket"
	analyticsDataset := "testDataset1"

	queryMap := []string{
		`create bucket ` + analyticsBucketName + ` with {"name": "` + bucketName + `"}`,
		`create dataset ` + analyticsDataset + ` on ` + analyticsBucketName,
		`connect bucket ` + analyticsBucketName,
	}
	for _, query := range queryMap {
		t.Log(query)
		if response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Time to populate data to analytics bucket
	time.Sleep(time.Second * 30)

	query := "select count(*) as count from " + analyticsDataset
	t.Log(query)
	response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}

	queryRes := queryResult{}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfDocs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create analytics enabled couchbase cluster
// Create analytics bucket with data sets and connect with couchbase bucket
// Resize cluster with analytics nodes and check for data and functional consistency
func TestAnalyticsResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	clusterSize := 1
	numOfDocs := 10
	bucketName := "defBucket"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index", "analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", e2eutil.Mem256Mb, 1, e2eutil.BucketFlushEnabled, e2eutil.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "analytics")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "data")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "eventing")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "index")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "query")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "search")

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	analyticsNodePortStr := strconv.Itoa(int(testCouchbase.Status.ExposedPorts[analyticsNodeName].AnalyticsServicePort))

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	k8sMasterIp, err := f.GetKubeHostname(targetKubeName)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, bucketName, 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

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
		if response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Function to insert data with two types of valueType varaibles
	dataInsertionStopChan := make(chan bool)
	stopDataInsertion := false
	numOfType1Docs := 0
	numOfType2Docs := 0
	docInsertFunc := func() {
		docIndex := numOfDocs
		// Get bucket Obj
		bucketObj, err := client.GetBucket(bucketName)
		if err != nil {
			t.Fatalf("Failed to retrieve couchbase bucket %s: %v ", bucketName, err)
		}

		for {
			if stopDataInsertion {
				break
			}
			docIndex++
			docKey := "doc" + strconv.Itoa(docIndex)
			docMap := map[string]string{}
			docMap["name"] = "docName " + strconv.Itoa(docIndex)
			docMap["value"] = "dummy Value " + strconv.Itoa(docIndex)
			if docIndex%2 == 0 {
				docMap["valueType"] = "type1"
				numOfType1Docs++
			} else {
				docMap["valueType"] = "type2"
				numOfType2Docs++
			}

			// Convert map data to byte array
			docData, err := json.Marshal(docMap)
			if err != nil {
				t.Fatalf("Failed to marshal map into bytes: %v", err)
			}
			docData = append([]byte("value="), docData...)

			// Inserts document using client
			if err := client.InsertDoc(bucketObj, docKey, docData); err != nil {
				t.Fatalf("Failed to insert doc %s: %v", docKey, err)
			}
		}
		numOfDocs = docIndex
		dataInsertionStopChan <- true
	}

	//Run doc populator in backgroud while cluster resize is happening
	go docInsertFunc()

	// Resize cluster
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := clusterSize
	for _, clusterSize = range clusterSizes {
		service := 0
		err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
		if err != nil {
			t.Fatal(err)
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
		}
		prevClusterSize = clusterSize
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries120); err != nil {
		t.Fatal(err)
	}

	// To stop background data insertion and wait for function to complete
	stopDataInsertion = true
	_ = <-dataInsertionStopChan

	// Verify total docs in data set - 1
	query := "select count(*) as count from " + analyticsDataset1
	t.Log(query)
	response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	queryRes := queryResult{}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfDocs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	// Verify total docs in data set - 2
	query = "select count(*) as count from " + analyticsDataset2
	t.Log(query)
	response, err = e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfType1Docs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	// Verify total docs in data set - 3
	query = "select count(*) as count from " + analyticsDataset3
	t.Log(query)
	response, err = e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfType2Docs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy analyitcs enabled couchbase cluster and populate data
// Kill analytics enabled node and check the cluster status
func TestAnalyticsKillPods(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	clusterSizeWoAnalytics := 1
	clusterSizeOfAnalytics := 3
	clusterSize := clusterSizeWoAnalytics + clusterSizeOfAnalytics + 1
	numOfDocs := 50
	bucketName := "defBucket"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "analytics"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(clusterSizeWoAnalytics, "test_config_2", []string{"data", "query", "index"})
	serviceConfig3 := e2eutil.GetServiceConfigMap(clusterSizeOfAnalytics, "test_config_3", []string{"analytics"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", e2eutil.Mem256Mb, 1, e2eutil.BucketFlushEnabled, e2eutil.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"service2":        serviceConfig2,
		"service3":        serviceConfig3,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "analytics")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "data")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "eventing")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "index")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "query")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "search")

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	analyticsNodePortStr := strconv.Itoa(int(testCouchbase.Status.ExposedPorts[analyticsNodeName].AnalyticsServicePort))

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	k8sMasterIp, err := f.GetKubeHostname(targetKubeName)
	if err != nil {
		t.Fatal(err)
	}

	// Load default data set into couchbase bucket
	if err := e2eutil.InsertJsonDocsIntoBucket(client, bucketName, 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

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
		if response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
	}

	// Function to insert data with two types of valueType varaibles
	dataInsertionStopChan := make(chan bool)
	stopDataInsertion := false
	numOfType1Docs := 0
	numOfType2Docs := 0
	docInsertFunc := func() {
		docIndex := numOfDocs
		// Get bucket Obj
		bucketObj, err := client.GetBucket(bucketName)
		if err != nil {
			t.Fatalf("Failed to retrieve couchbase bucket %s: %v ", bucketName, err)
		}

		for {
			if stopDataInsertion {
				break
			}
			docIndex++
			docKey := "doc" + strconv.Itoa(docIndex)
			docMap := map[string]string{}
			docMap["name"] = "docName " + strconv.Itoa(docIndex)
			docMap["value"] = "dummy Value " + strconv.Itoa(docIndex)
			if docIndex%2 == 0 {
				docMap["valueType"] = "type1"
				numOfType1Docs++
			} else {
				docMap["valueType"] = "type2"
				numOfType2Docs++
			}

			// Convert map data to byte array
			docData, err := json.Marshal(docMap)
			if err != nil {
				t.Fatalf("Failed to marshal map into bytes: %v", err)
			}
			docData = append([]byte("value="), docData...)

			// Inserts document using client
			if err := client.InsertDoc(bucketObj, docKey, docData); err != nil {
				t.Fatalf("Failed to insert doc %s: %v", docKey, err)
			}
		}
		numOfDocs = docIndex
		dataInsertionStopChan <- true
	}

	//Run doc populator in backgroud while cluster resize is happening
	go docInsertFunc()

	podMemberIdsToKill := []int{4, 5, 6}
	newMemberIdToBeAdded := clusterSize
	for _, podMemberId := range podMemberIdsToKill {
		podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
		if err := e2eutil.KillMember(targetKube.KubeClient, f.Namespace, testCouchbase.Name, podMemberName); err != nil {
			t.Fatal(err)
		}

		event := e2eutil.NewMemberDownEvent(testCouchbase, podMemberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
			t.Fatal(err)
		} else {
			expectedEvents.AddMemberDownEvent(testCouchbase, podMemberId)
		}

		if err := e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, e2eutil.Size1); err != nil {
			t.Fatalf("Mismatch in unhealthy nodes count: %v", err)
		}

		// Manual failover to recover the pod
		member := &couchbaseutil.Member{
			Name:         podMemberName,
			Namespace:    f.Namespace,
			ServerConfig: testCouchbase.Spec.ServerSettings[1].Name,
			SecureClient: false,
		}
		if err := e2eutil.FailoverNode(t, client, e2eutil.Retries5, member.HostURL()); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, podMemberId)

		event = e2eutil.NewMemberAddEvent(testCouchbase, newMemberIdToBeAdded)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 90); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, newMemberIdToBeAdded)

		event = e2eutil.RebalanceStartedEvent(testCouchbase)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddRebalanceStartedEvent(testCouchbase)

		event = e2eutil.RebalanceCompletedEvent(testCouchbase)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberId)
		expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

		newMemberIdToBeAdded++
	}

	// To stop background data insertion and wait for function to complete
	stopDataInsertion = true
	_ = <-dataInsertionStopChan

	// Verify total docs in data set - 1
	query := "select count(*) as count from " + analyticsDataset1
	t.Log(query)
	response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	queryRes := queryResult{}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfDocs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	// Verify total docs in data set - 2
	query = "select count(*) as count from " + analyticsDataset2
	t.Log(query)
	response, err = e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfType1Docs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	// Verify total docs in data set - 3
	query = "select count(*) as count from " + analyticsDataset3
	t.Log(query)
	response, err = e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfType2Docs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy analyitcs enabled couchbase cluster over PVC and populate data
// Kill analytics enabled node and check the cluster and PVC status
// Kill all analytics nodes at once and check for node recovery
func TestAnalyticsKillPodsWithPVC(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

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

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", e2eutil.Mem256Mb, 1, e2eutil.BucketFlushEnabled, e2eutil.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":         clusterConfig,
		"service1":        serviceConfig1,
		"bucket1":         bucketConfig1,
		"exposedFeatures": map[string]string{"featureNames": "client"},
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec("standard", pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}

	testCouchbase, err := e2eutil.CreateClusterFromSpec(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, e2eutil.AdminExposed, clusterSpec)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "analytics")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "data")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "eventing")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "index")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "query")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "search")

	analyticsNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	analyticsNodePortStr := strconv.Itoa(int(testCouchbase.Status.ExposedPorts[analyticsNodeName].AnalyticsServicePort))

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	k8sMasterIp, err := f.GetKubeHostname(targetKubeName)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, bucketName, 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

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
		if response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query); err != nil {
			t.Fatal(err.Error() + "-" + string(response))
		}
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
		event := e2eutil.NewMemberDownEvent(testCouchbase, podMemberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		} else {
			expectedEvents.AddMemberDownEvent(testCouchbase, podMemberId)
		}
	}

	// Loop to wait for pod recovery events to occur
	for podMemberId := 0; podMemberId < clusterSize; podMemberId++ {
		if podMemberId == 0 {
			continue
		}
		event := e2eutil.MemberRecoveredEvent(testCouchbase, podMemberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberRecoveredEvent(testCouchbase, podMemberId)
	}

	// Event checks for rebalance to start and complete successfully
	event := e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries5); err != nil {
		t.Fatal(err)
	}

	numOfType1Docs := 0
	numOfType2Docs := 0
	// Verify total docs in data set - 1
	query := "select count(*) as count from " + analyticsDataset1
	t.Log(query)
	response, err := e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	queryRes := queryResult{}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfDocs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	// Verify total docs in data set - 2
	query = "select count(*) as count from " + analyticsDataset2
	t.Log(query)
	response, err = e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfType1Docs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}

	// Verify total docs in data set - 3
	query = "select count(*) as count from " + analyticsDataset3
	t.Log(query)
	response, err = e2eutil.ExecuteAnalyticsQuery(k8sMasterIp, analyticsNodePortStr, query)
	if err != nil {
		t.Fatal(err.Error() + "-" + string(response))
	}
	json.Unmarshal(response, &queryRes)

	if queryRes.Results[0]["count"] != numOfType2Docs {
		t.Fatalf("Mismatch in number of docs. Expected %d, got %d", numOfDocs, queryRes.Results[0]["count"])
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}
