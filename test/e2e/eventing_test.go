package e2e

import (
	"os"
	"strconv"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

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
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucket1["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucket2["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucket3["bucketName"])

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

	eventingNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	eventingHostUrl, eventingPortStr, err := e2eutil.GetEventingIpAndPort(t, eventingNodeName, targetKube.KubeClient, f.Namespace, f.PlatformType, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	responseData, err := e2eutil.DeployEventingFunction(eventingHostUrl, eventingPortStr, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)
	if err != nil {
		t.Log(string(responseData))
		t.Fatal(err)
	}

	k8sMasterIp, err := f.GetKubeHostname(kubeName)
	if err != nil {
		t.Fatal(err)
	}
	hostUrl, err := e2eutil.GetAdminConsoleHostURL(k8sMasterIp, f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Create eventing enabled couchbase cluster with eventing buckets
// Resize the cluster when the eventing deployment is active and verify the results
func TestEventingResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	nonEventingNodes := 2
	eventingNodes := 3
	clusterSize := nonEventingNodes + eventingNodes
	numOfDocs := 10
	configMap := createEventingConfigMap(nonEventingNodes, eventingNodes)

	// Creating cluster with eventing
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket1"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket2"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket3"]["bucketName"])

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

	// Provide the pod index for the eventing node
	// Here nonEventingNodes will be equal to eventing pod's index
	eventingNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, nonEventingNodes)
	eventingHostUrl, eventingPortStr, err := e2eutil.GetEventingIpAndPort(t, eventingNodeName, targetKube.KubeClient, f.Namespace, f.PlatformType, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	responseData, err := e2eutil.DeployEventingFunction(eventingHostUrl, eventingPortStr, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)
	if err != nil {
		t.Log(string(responseData))
		t.Fatal(err)
	}

	// Cross check number of docs inserted reflected in eventing
	k8sMasterIp, err := f.GetKubeHostname(kubeName)
	if err != nil {
		t.Fatal(err)
	}
	hostUrl, err := e2eutil.GetAdminConsoleHostURL(k8sMasterIp, f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan bool)
	dataInsertionError := make(chan error)
	dataInsertionFunc := func(t *testing.T) {
		// Return an error here, don't call Fatal() as this will trigger a race condition
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertion:
				break OuterLoop
			default:
				numOfDocs++
				if err = e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], numOfDocs, 1); err != nil {
					break OuterLoop
				}
			}
		}
		dataInsertionError <- err
	}
	go dataInsertionFunc(t)

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
		testCouchbase, err = e2eutil.ResizeClusterNoWait(t, service, eventingNodes, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Waiting For Cluster Size To Be: %v...\n", strconv.Itoa(clusterSize))
		names, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, clusterSize, constants.Retries120, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Resize Success: %v...\n", names)

		if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
			t.Fatal(err.Error())
		}

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

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	// Stop the insertion and wait for it to exit, checking for any errors encountered
	stopDataInsertion <- true
	if err := <-dataInsertionError; err != nil {
		t.Fatal(err)
	}

	// Cross check number of docs inserted reflected in eventing
	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Create couchbase cluster with eventing service and required buckets
// Kill all the eventing nodes one by one and check for eventing stability
func TestEventingKillEventingPods(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	nonEventingNodes := 2
	eventingNodes := 3
	clusterSize := nonEventingNodes + eventingNodes
	numOfDocs := 10
	configMap := createEventingConfigMap(nonEventingNodes, eventingNodes)

	// Creating cluster with eventing
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.AnalyticsService, api.DataService, api.EventingService)
	expectedEvents.AddClusterNodeServiceEvent(testCouchbase, "Create", api.IndexService, api.QueryService, api.SearchService)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket1"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket2"]["bucketName"])
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", configMap["bucket3"]["bucketName"])

	// Creates the client with exposed admin port
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	if err := e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], 1, numOfDocs); err != nil {
		t.Fatal(err)
	}

	// Provide the pod index for the eventing node
	// Here nonEventingNodes will be equal to eventing pod's index
	eventingNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, nonEventingNodes)
	eventingHostUrl, eventingPortStr, err := e2eutil.GetEventingIpAndPort(t, eventingNodeName, targetKube.KubeClient, f.Namespace, f.PlatformType, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}
	eventingFuncName := "eventingFunc"
	eventingSrcBucketName := "eventingSrc"
	eventingMetaBucketName := "eventingMetaBucket"
	eventingDstBucketName := "eventingDst"
	eventingJsFunc := `function OnUpdate(doc, meta) {\n    var doc_id = meta.id;\n    dst_bucket[doc_id] = \"test value\";\n}\nfunction OnDelete(meta) {\n  delete dst_bucket[meta.id];\n}`

	responseData, err := e2eutil.DeployEventingFunction(eventingHostUrl, eventingPortStr, eventingFuncName, eventingSrcBucketName, eventingMetaBucketName, eventingDstBucketName, eventingJsFunc)
	if err != nil {
		t.Log(string(responseData))
		t.Fatal(err)
	}

	// Cross check number of docs inserted reflected in eventing
	k8sMasterIp, err := f.GetKubeHostname(kubeName)
	if err != nil {
		t.Fatal(err)
	}
	hostUrl, err := e2eutil.GetAdminConsoleHostURL(k8sMasterIp, f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	// Code to insert data in parallel with cluster resize
	stopDataInsertion := make(chan bool)
	dataInsertionErr := make(chan error)
	dataInsertionFunc := func(t *testing.T) {
		var err error
	OuterLoop:
		for {
			select {
			case <-stopDataInsertion:
				break OuterLoop
			default:
				numOfDocs++
				if err = e2eutil.InsertJsonDocsIntoBucket(client, configMap["bucket1"]["bucketName"], numOfDocs, 1); err != nil {
					break OuterLoop
				}
			}
		}
		dataInsertionErr <- err
	}
	go dataInsertionFunc(t)

	newMemberToBeAdded := clusterSize
	for _, memberId := range []int{2, 3, 4} {
		if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, memberId); err != nil {
			t.Fatal(err)
		}
		event := e2eutil.NewMemberDownEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 30); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", memberId)
		expectedEvents.AddClusterPodEvent(testCouchbase, "FailedOver", memberId)

		event = e2eutil.NewMemberAddEvent(testCouchbase, newMemberToBeAdded)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 150); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", newMemberToBeAdded)

		event = e2eutil.RebalanceCompletedEvent(testCouchbase)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", memberId)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
		newMemberToBeAdded++
	}
	stopDataInsertion <- true
	if err := <-dataInsertionErr; err != nil {
		t.Fatal(err)
	}

	// Cross check number of docs inserted reflected in eventing
	if err := e2eutil.VerifyDocCountInBucket(hostUrl, eventingDstBucketName, string(e2espec.BasicSecretData["username"]), string(e2espec.BasicSecretData["password"]), numOfDocs, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}
