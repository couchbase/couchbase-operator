package e2e

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Create 3 server groups
// Failover all pods in selected server group
// This should trigger server group failover in the cluster
func TestServerGroupAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	if len(availableServerGroupList) < 3 {
		t.Skip("couchbase server requires 3 or more availability zones")
	}

	// Create cluster spec for RZA feature
	clusterSize := e2eutil.MustNumNodes(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	clusterConfig["autoFailoverTimeout"] = "10"
	clusterConfig["autoFailoverMaxCount"] = "2"
	clusterConfig["autoFailoverServerGroup"] = "true"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"serverGroups": serverGroups,
	}

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Deploy couchbase cluster
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	victimGroup := 0
	victims := []int{}
	for i := 0; i < clusterSize; i++ {
		if i%len(availableServerGroupList) == victimGroup {
			victims = append(victims, i)
		}
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, true)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create 8 node couchbase server with default services over 3 server groups
// Create 1 node with search service to one server group
// kill all nodes in the server group where search pod is present
// server group failover should not be triggered
func TestServerGroupWithSingleServiceNodeInFailoverGroup(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	t.Skip("Server will not failover a service with a single instance")

	f := framework.Global
	targetKube := f.GetCluster(0)

	// Create cluster spec for RZA feature
	clusterSize := 9
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	clusterConfig["autoFailoverTimeout"] = "30"
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverServerGroup"] = "true"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize-1, "test_config_1", []string{"data", "query", "index"})
	serviceConfig2 := e2eutil.GetClassSpecificServiceConfigMap(1, "test_config_2", []string{"search"}, []string{availableServerGroupList[2]})
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"service2":     serviceConfig2,
		"serverGroups": serverGroups,
	}

	// Deploy couchbase cluster
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)

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
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, true)
		memDownParallelEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberToKill)
	}
	expectedEvents.AddParallelEvents(memDownParallelEvents)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberFailedOverEvent(testCouchbase, podMembersToKill[0]), 2*time.Minute)
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create 9 node couchbase cluster
// Failover multiple nodes at once and wait for multinode failover to trigger
func TestMultiNodeAutoFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 9
	victims := []int{2, 3, 4}
	victimCount := len(victims)

	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 3, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketThreeReplicas)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	// When ready, kill the victim nodes, expect a rebalance to happen eventually as they
	// are replaced and await healthy status.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, time.Minute)
	for _, victim := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, true)
		time.Sleep(31 * time.Second)
	}
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Nodes go down, and failover
	// * New members balanced in, kkilled members removed
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: victimCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)

}

// Create multinode couchbase cluster and bucket with replicas
// Load data into the bucket
// Remove bucket dir from the pod to simulate the disk write errors
func TestDiskFailureAutoFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 6
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"

	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", "default")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Get pod object for reading container name
	podName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	// Asynchronously insert data into the cluster so that Couchbase server
	// notices it cannot read/write to the bucket directory.
	go func() {
		cmd := []string{
			"/opt/couchbase/bin/cbworkloadgen",
			"-n", "127.0.0.1:8091",
			"-u", "Administrator",
			"-p", "password",
			"-b", "default",
			"-j",
			"-s", "100000",
			"-i", "10000",
		}
		// Note: we expect an error here as the directory gets deleted later.
		stdout, stderr, err := e2eutil.ExecCommandInPod(targetKube, f.Namespace, podName, cmd...)
		if err != nil {
			t.Logf("Error: %v", err)
			t.Logf("Command: %s", cmd)
			t.Logf("stdout: %s", stdout)
			t.Logf("stderr: %s", stderr)
		}
	}()

	t.Log("Entering sleep to load data")
	time.Sleep(time.Second * 20)
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, podName, "rm -vf /opt/couchbase/var/lib/couchbase/data/default/*")
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 2*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
