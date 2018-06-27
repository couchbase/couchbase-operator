package e2e

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/gocbmgr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Create 3 server groups and 9 node homogeneous cb cluster
// Failover all pods in selected server group
// This should trigger server group failover in the cluster
func TestServerGroupAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 9
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.GetBucketConfigMap("testBucket", "couchbase", "high", 100, 1, true, false)
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Deploy couchbase cluster
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	event := e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatalf("Rebalance not started after server group assignment: %v", err)
	}

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Rebalance failed after server group assignment: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	podMembersToKill := []int{2, 5, 8}
	for _, podMemberToKill := range podMembersToKill {
		err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill)
		if err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, podMemberToKill)
	}

	for memberIndex := clusterSize; memberIndex < clusterSize+3; memberIndex++ {
		event = e2eutil.NewMemberAddEvent(testCouchbase, memberIndex)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
			t.Fatalf("Failed to create replacement pod: %v", err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	for _, podMemberToKill := range podMembersToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberToKill)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create 8 node couchbase server with default services over 3 server groups
// Create 1 node with search service to one server group
// kill all nodes in the server group where search pod is present
// server group failover should not be triggered
func TestServerGroupWithSingleServiceNodeInFailoverGroup(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 9
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize-1, "test_config_1", []string{"data", "query", "index"})
	serviceConfig2 := e2eutil.GetClassSpecificServiceConfigMap(1, "test_config_2", []string{"search"}, []string{availableServerGroupList[2]})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"service2":     serviceConfig2,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	sort.Strings(availableServerGroupList)

	// Deploy couchbase cluster
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	// Creating expected RZA server groups pod maps
	expectedRzaResultMap := map[string]int{
		availableServerGroupList[0]: 4,
		availableServerGroupList[1]: 1,
		availableServerGroupList[2]: 2,
	}

	expectedRzaPodNodeSelectorMap := map[string]string{
		testCouchbase.Name + "0000": availableServerGroupList[0],
		testCouchbase.Name + "0001": availableServerGroupList[2],
		testCouchbase.Name + "0002": availableServerGroupList[0],
		testCouchbase.Name + "0003": availableServerGroupList[1],
		testCouchbase.Name + "0004": availableServerGroupList[0],
		testCouchbase.Name + "0005": availableServerGroupList[2],
		testCouchbase.Name + "0006": availableServerGroupList[0],
	}

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	event := e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatalf("Rebalance not started after server group assignment: %v", err)
	}

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Rebalance failed after server group assignment: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	deployedRzaPodMap, err := GetDeployedRzaPodMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	t.Log(deployedRzaPodMap)

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaPodNodeSelectorMap, deployedRzaPodMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaPodNodeSelectorMap, deployedRzaPodMap)
	}

	autoFailoverSettings := cbmgr.AutoFailoverSettings{
		Enabled: true,
		Timeout: 30,
		Count:   3,
		//FailoverOnDataDiskIssues FailoverOnDiskFailureSettings
		FailoverServerGroup: true,
		MaxCount:            2,
	}

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("Unable to get Client for cluster: %v", err)
	}

	if err := client.SetAutoFailoverSettings(&autoFailoverSettings); err != nil {
		t.Fatal(err)
	}

	podMembersToKill := []int{2, 5, 8}
	for _, podMemberToKill := range podMembersToKill {
		err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill)
		if err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, podMemberToKill)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err == nil {
		t.Fatal("Failover trigger despite search service is present only in failure group")
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create 9 node couchbase cluster
// Failover multiple nodes at once and wait for multinode failover to trigger
func TestMultiNodeAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	clusterSize := 9
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	podMembersToKill := []int{2, 3, 4}
	for _, podMemberToKill := range podMembersToKill {
		err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill)
		if err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, podMemberToKill)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create multinode couchbase cluster and bucket with replicas
// Load data into the bucket
// Remove bucket dir from the pod to simulate the disk write errors
func TestDiskFailureAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := "BasicCluster"
	targetKube := f.ClusterSpec[targetKubeName]

	clusterSize := 6
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"

	bucketName := "testBucket"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	//defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Get pod object for reading container name
	podName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	pod, err := f.PodClient(targetKubeName).Get(podName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get pod data: %v", err)
	}

	cbWorkLoadGoRoutine := func() {
		podCmdStr := []string{"/opt/couchbase/bin/cbworkloadgen", "-n", "127.0.0.1:8091", "-u", "Administrator", "-p", "password", "-b", bucketName, "-j", "-s", "100000", "-i", "10000"}
		podCmdOptions := framework.ExecOptions{
			Command:            podCmdStr,
			Namespace:          f.Namespace,
			PodName:            podName,
			ContainerName:      pod.Spec.Containers[0].Name,
			Stdin:              nil,
			CaptureStdout:      true,
			CaptureStderr:      true,
			PreserveWhitespace: true,
		}

		// start loading data
		stdOut, _, err := f.ExecWithOptions(targetKubeName, podCmdOptions)
		if err != nil {
			t.Logf("Stdout: %v", stdOut)
			t.Logf("Execution failed: %v", err)
		}
	}

	deleteBucketDirErrChan := make(chan error)
	deleteBucketDirGoRoutine := func() {
		podCmdStr := "rm -vf /opt/couchbase/var/lib/couchbase/data/" + bucketName + "/*"

		t.Log("Entering sleep to load data")
		time.Sleep(time.Second * 20)

		stdOut, err := f.ExecShellInPod(targetKubeName, podName, podCmdStr)
		if err != nil {
			t.Logf("Stdout: %v", stdOut)
		}
		deleteBucketDirErrChan <- err
	}

	go cbWorkLoadGoRoutine()
	go deleteBucketDirGoRoutine()

	if err := <-deleteBucketDirErrChan; err != nil {
		t.Fatalf("Failed to delete bucket dir: %v", err)
	}

	event := e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatalf("Rebalance not started after failover: %v", err)
	}

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Rebalance failed after failover: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}
