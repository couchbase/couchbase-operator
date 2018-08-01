package e2e

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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
	bucketName := "testBucket"
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	clusterConfig["autoFailoverTimeout"] = "10"
	clusterConfig["autoFailoverMaxCount"] = "2"
	clusterConfig["autoFailoverServerGroup"] = "true"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 1, true, false)
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
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)

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

	memberDownEvents := []*corev1.Event{}
	memberFailedOverEvents := []*corev1.Event{}

	// Loop to kill the nodes
	for _, podMemberToKill := range podMembersToKill {
		err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill)
		if err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, podMemberToKill)
		memberDownEvents = append(memberDownEvents, e2eutil.NewMemberDownEvent(testCouchbase, podMemberToKill))
		memberFailedOverEvents = append(memberFailedOverEvents, e2eutil.NewMemberFailedOverEvent(testCouchbase, podMemberToKill))
	}

	if err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, memberDownEvents, 30); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, memberFailedOverEvents, 60); err != nil {
		t.Fatal(err)
	}
	for _, podMemberToKill := range podMembersToKill {
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, podMemberToKill)
	}

	// Event check for new member add
	for memberIndex := clusterSize; memberIndex < clusterSize+3; memberIndex++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberIndex)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatalf("Failed to create replacement pod: %v", err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}

	// Event check for rebalance events
	event := e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	for _, podMemberToKill := range podMembersToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberToKill)
	}

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
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
	clusterConfig["autoFailoverTimeout"] = "30"
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverServerGroup"] = "true"
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

	// Deploy couchbase cluster
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	sort.Strings(availableServerGroupList)

	// Creating expected RZA server groups pod maps
	expectedRzaResultMap := map[string]int{
		availableServerGroupList[0]: 3,
		availableServerGroupList[1]: 3,
		availableServerGroupList[2]: 3,
	}

	expectedRzaPodNodeSelectorMap := map[string]string{
		testCouchbase.Name + "-0000": availableServerGroupList[0],
		testCouchbase.Name + "-0001": availableServerGroupList[1],
		testCouchbase.Name + "-0002": availableServerGroupList[2],
		testCouchbase.Name + "-0003": availableServerGroupList[0],
		testCouchbase.Name + "-0004": availableServerGroupList[1],
		testCouchbase.Name + "-0005": availableServerGroupList[2],
		testCouchbase.Name + "-0006": availableServerGroupList[0],
		testCouchbase.Name + "-0007": availableServerGroupList[1],
		testCouchbase.Name + "-0008": availableServerGroupList[2],
	}

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

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

	podMembersToKill := []int{2, 5, 8}
	var podEventErrChan [3]chan error
	for index := range podEventErrChan {
		podEventErrChan[index] = make(chan error)
	}

	for _, podMemberToKill := range podMembersToKill {
		err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill)
		if err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, podMemberToKill)
	}

	event := e2eutil.NewMemberFailedOverEvent(testCouchbase, podMembersToKill[0])
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err == nil {
		t.Fatal("Member failed over in single node service scenario")
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
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 10, 3, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.GetBucketConfigMap("default", "couchbase", "high", e2eutil.Mem256Mb, e2eutil.Size3, e2eutil.BucketFlushEnabled, e2eutil.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	podMembersToKill := []int{2, 3, 4}
	podMembersToKillLen := len(podMembersToKill)
	for _, podMemberToKill := range podMembersToKill {
		if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, podMemberToKill)
		time.Sleep(time.Second * 15)
	}

	for _, podMemberToKill := range podMembersToKill {
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, podMemberToKill)
	}

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("Unable to get Client for cluster: %v", err)
	}
	if err := e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries10, podMembersToKillLen); err != nil {
		t.Fatal(err)
	}

	for memberId := clusterSize; memberId < clusterSize+podMembersToKillLen; memberId++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
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
	for _, podMemberToKill := range podMembersToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberToKill)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries5); err != nil {
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
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

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
