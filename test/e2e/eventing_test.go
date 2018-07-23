package e2e

import (
	"os"
	"strconv"
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
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
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	clusterSize := 1
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
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucket1["bucketName"])
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "analytics")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "data")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "eventing")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "index")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "query")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "search")
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucket2["bucketName"])
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucket3["bucketName"])

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	k8sMasterIp, err := f.GetKubeHostname(targetKubeName)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], 1, numOfDocs); err != nil {
		t.Fatal(err)
	}
	eventingNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	eventingPortStr := strconv.Itoa(int(testCouchbase.Status.ExposedPorts[eventingNodeName].EventingServicePort))

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	responseData, err := e2eutil.DeployEventingFunction(k8sMasterIp, eventingPortStr, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)
	if err != nil {
		t.Log(string(responseData))
		t.Fatal(err)
	}

	hostUrl := k8sMasterIp + ":" + testCouchbase.Status.AdminConsolePort
	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create eventing enabled couchbase cluster with eventing buckets
// Resize the cluster when the eventing deployment is active and verify the results
func TestEventingResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	nonEventingNodes := 2
	eventingNodes := 3
	clusterSize := nonEventingNodes + eventingNodes
	numOfDocs := 10
	configMap := createEventingConfigMap(nonEventingNodes, eventingNodes)

	// Creating cluster with eventing
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, configMap["bucket1"]["bucketName"])
	expectedEvents.AddBucketCreateEvent(testCouchbase, configMap["bucket2"]["bucketName"])
	expectedEvents.AddBucketCreateEvent(testCouchbase, configMap["bucket3"]["bucketName"])
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "analytics")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "data")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "eventing")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "index")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "query")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "search")

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	k8sMasterIp, err := f.GetKubeHostname(targetKubeName)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], 1, numOfDocs); err != nil {
		t.Fatal(err)
	}
	eventingNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 2)
	eventingPortStr := strconv.Itoa(int(testCouchbase.Status.ExposedPorts[eventingNodeName].EventingServicePort))

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	responseData, err := e2eutil.DeployEventingFunction(k8sMasterIp, eventingPortStr, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)
	if err != nil {
		t.Log(string(responseData))
		t.Fatal(err)
	}

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan bool)
	dataInsertionFunc := func(t *testing.T) {
		for {
			select {
			case <-stopDataInsertion:
				break
			default:
				numOfDocs++
				if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], numOfDocs, 1); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	go dataInsertionFunc(t)

	eventingClusterSizes := []int{2, 7, 4}
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
		if err := e2eutil.ResizeClusterNoWait(t, service, clusterSize, targetKube.CRClient, testCouchbase); err != nil {
			t.Fatal(err)
		}
		t.Logf("Waiting For Cluster Size To Be: %v...\n", strconv.Itoa(clusterSize))
		names, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, clusterSize, e2eutil.Retries120, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Resize Success: %v...\n", names)

		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
		if err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			for _, memberId := range membersAdded {
				expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
			}
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			for _, memberId := range membersRemoved {
				expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
			}
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
		}
		prevClusterSize = clusterSize
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}
	stopDataInsertion <- true

	// Cross check number of docs inserted reflected in eventing
	hostUrl := k8sMasterIp + ":" + testCouchbase.Status.AdminConsolePort
	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create couchbase cluster with eventing service and required buckets
// Kill all the eventing nodes one by one and check for eventing stability
func TestEventingKillEventingPods(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	nonEventingNodes := 2
	eventingNodes := 3
	clusterSize := nonEventingNodes + eventingNodes
	numOfDocs := 10
	configMap := createEventingConfigMap(nonEventingNodes, eventingNodes)

	// Creating cluster with eventing
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, configMap["bucket1"]["bucketName"])
	expectedEvents.AddBucketCreateEvent(testCouchbase, configMap["bucket2"]["bucketName"])
	expectedEvents.AddBucketCreateEvent(testCouchbase, configMap["bucket3"]["bucketName"])
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "analytics")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "data")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "eventing")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "index")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "query")
	expectedEvents.AddNodeServiceCreateEvent(testCouchbase, "search")

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	k8sMasterIp, err := f.GetKubeHostname(targetKubeName)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], 1, numOfDocs); err != nil {
		t.Fatal(err)
	}
	eventingNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 2)
	eventingPortStr := strconv.Itoa(int(testCouchbase.Status.ExposedPorts[eventingNodeName].EventingServicePort))

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	responseData, err := e2eutil.DeployEventingFunction(k8sMasterIp, eventingPortStr, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)
	if err != nil {
		t.Log(string(responseData))
		t.Fatal(err)
	}

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan bool)
	dataInsertionFunc := func(t *testing.T) {
		for {
			select {
			case <-stopDataInsertion:
				break
			default:
				numOfDocs++
				if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], numOfDocs, 1); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	go dataInsertionFunc(t)

	newMemberToBeAdded := clusterSize
	for _, memberId := range []int{2, 3, 4} {
		if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, memberId); err != nil {
			t.Fatal(err)
		}
		event := e2eutil.NewMemberDownEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 20); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, memberId)

		event = e2eutil.NewMemberFailedOverEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 40); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberId)

		event = e2eutil.NewMemberAddEvent(testCouchbase, newMemberToBeAdded)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)

		event = e2eutil.RebalanceStartedEvent(testCouchbase)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}

		event = e2eutil.RebalanceCompletedEvent(testCouchbase)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddRebalanceStartedEvent(testCouchbase)
		expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
		expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
		newMemberToBeAdded++
	}
	stopDataInsertion <- true

	// Cross check number of docs inserted reflected in eventing
	hostUrl := k8sMasterIp + ":" + testCouchbase.Status.AdminConsolePort
	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}
