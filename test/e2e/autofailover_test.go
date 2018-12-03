package e2e

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create 3 server groups and 9 node homogeneous cb cluster
// Failover all pods in selected server group
// This should trigger server group failover in the cluster
func TestServerGroupAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "NewCluster1"
	targetKube := f.ClusterSpec[kubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{kubeName})
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
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

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
	memberDownEvents := e2eutil.EventList{}
	memberFailedOverEvents := e2eutil.EventList{}

	memDownParallelEvents := e2eutil.EventValidator{}
	memFailoverParallelEvents := e2eutil.EventValidator{}
	memRemovedParallelEvents := e2eutil.EventValidator{}

	// Loop to kill the nodes
	for _, podMemberToKill := range podMembersToKill {
		err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill)
		if err != nil {
			t.Fatal(err)
		}
		memberDownEvents = append(memberDownEvents, *e2eutil.NewMemberDownEvent(testCouchbase, podMemberToKill))
		memberFailedOverEvents = append(memberFailedOverEvents, *e2eutil.NewMemberFailedOverEvent(testCouchbase, podMemberToKill))

		memDownParallelEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberToKill)
		memFailoverParallelEvents.AddClusterPodEvent(testCouchbase, "FailedOver", podMemberToKill)
		memRemovedParallelEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", podMemberToKill)
	}

	if _, err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, memberDownEvents, 30); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, memberFailedOverEvents, 60); err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddParallelEvents(memDownParallelEvents)
	expectedEvents.AddParallelEvents(memFailoverParallelEvents)

	// Event check for new member add
	for memberIndex := clusterSize; memberIndex < clusterSize+3; memberIndex++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberIndex)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatalf("Failed to create replacement pod: %v", err)
		}
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}

	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddParallelEvents(memRemovedParallelEvents)

	event := e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
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
	kubeName := "NewCluster1"
	targetKube := f.ClusterSpec[kubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{kubeName})
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
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

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

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", "default")

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

	memDownParallelEvents := e2eutil.EventValidator{}
	for _, podMemberToKill := range podMembersToKill {
		err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill)
		if err != nil {
			t.Fatal(err)
		}
		memDownParallelEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberToKill)
	}
	expectedEvents.AddParallelEvents(memDownParallelEvents)

	event := e2eutil.NewMemberFailedOverEvent(testCouchbase, podMembersToKill[0])
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err == nil {
		t.Fatal("Member failed over in single node service scenario")
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Create 9 node couchbase cluster
// Failover multiple nodes at once and wait for multinode failover to trigger
func TestMultiNodeAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "NewCluster1"
	targetKube := f.ClusterSpec[kubeName]

	clusterSize := 9
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 3, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.GetBucketConfigMap("default", "couchbase", "high", constants.Mem256Mb, constants.Size3, constants.BucketFlushEnabled, constants.IndexReplicaDisabled)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", "default")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	memDownParallelEvents := e2eutil.EventValidator{}
	memFailoverParallelEvents := e2eutil.EventValidator{}
	memRemoveParallelEvents := e2eutil.EventValidator{}

	podMembersToKill := []int{2, 3, 4}
	podMembersToKillLen := len(podMembersToKill)
	for _, podMemberToKill := range podMembersToKill {
		if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podMemberToKill); err != nil {
			t.Fatal(err)
		}
		memDownParallelEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberToKill)
		memFailoverParallelEvents.AddClusterPodEvent(testCouchbase, "FailedOver", podMemberToKill)
		memRemoveParallelEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", podMemberToKill)
		time.Sleep(time.Second * 31)
	}
	expectedEvents.AddParallelEvents(memDownParallelEvents)
	expectedEvents.AddParallelEvents(memFailoverParallelEvents)

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("Unable to get Client for cluster: %v", err)
	}
	if err := e2eutil.WaitForUnhealthyNodes(t, client, constants.Retries10, podMembersToKillLen); err != nil {
		t.Fatal(err)
	}

	for memberId := clusterSize; memberId < clusterSize+podMembersToKillLen; memberId++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 180); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberId)
	}

	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddParallelEvents(memRemoveParallelEvents)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries60); err != nil {
		t.Fatal(err.Error())
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Create multinode couchbase cluster and bucket with replicas
// Load data into the bucket
// Remove bucket dir from the pod to simulate the disk write errors
func TestDiskFailureAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "NewCluster1"
	targetKube := f.ClusterSpec[kubeName]

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

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	// Get pod object for reading container name
	podName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	pod, err := f.PodClient(kubeName).Get(podName, metav1.GetOptions{})
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
		stdOut, _, err := f.ExecWithOptions(kubeName, podCmdOptions)
		if err != nil {
			t.Logf("Stdout: %v", stdOut)
			t.Logf("Execution failed: %v", err)
		}
	}

	deleteBucketDirErrChan := make(chan error)

	// this function doesnt work
	deleteBucketDirGoRoutine := func() {
		podCmdStr := "rm -vf /opt/couchbase/var/lib/couchbase/data/" + bucketName + "/*"

		t.Log("Entering sleep to load data")
		time.Sleep(time.Second * 20)

		stdOut, err := f.ExecShellInPod(kubeName, podName, podCmdStr)
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
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}
