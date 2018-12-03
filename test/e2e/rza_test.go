package e2e

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/clustercapabilities"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Labels k8s nodes based on the values provided from the ClusterInfo struct
func K8SNodesAddLabel(nodeLabelName string, kubeClient kubernetes.Interface, k8sNodesData framework.ClusterInfo) error {
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to get k8s nodes " + err.Error())
	}
	for _, k8sNode := range k8sNodeList.Items {
		labelChanged := false
		nodeLabels := k8sNode.GetLabels()
		nodeIpAddress := k8sNode.Status.Addresses[0].Address
		for _, node := range k8sNodesData.MasterNodeList {
			if node.Ip == nodeIpAddress {
				nodeLabels[nodeLabelName] = node.NodeLabel
				labelChanged = true
				break
			}
		}
		for _, node := range k8sNodesData.WorkerNodeList {
			if node.Ip == nodeIpAddress {
				nodeLabels[nodeLabelName] = node.NodeLabel
				labelChanged = true
				break
			}
		}
		if !labelChanged {
			return errors.New("Unable to find node " + nodeIpAddress)
		}
		k8sNode.SetLabels(nodeLabels)

		// Reset Taints and set schedulable property for nodes
		k8sNode.Spec.Unschedulable = false
		k8sNode.Spec.Taints = []v1.Taint{}

		if _, err := kubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
			return errors.New("Failed to update label for node " + nodeIpAddress + ": " + err.Error())
		}
	}
	return nil
}

// Updates ServerGroup labels for the nodes with matches the oldLabelVal
// and replaces with the newLabelVal
func UpdateServerGroupLabel(nodeLabelName, oldLabelVal, newLabelVal string, kubeClient kubernetes.Interface) error {
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to get k8s nodes " + err.Error())
	}
	for _, k8sNode := range k8sNodeList.Items {
		nodeLabels := k8sNode.GetLabels()
		if nodeLabels[nodeLabelName] == oldLabelVal {
			nodeLabels[nodeLabelName] = newLabelVal
		}
		k8sNode.SetLabels(nodeLabels)
		if _, err := kubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
			return errors.New("Failed to update label for node " + k8sNode.Name + ": " + err.Error())
		}
	}
	return nil
}

// Function to check an element exists in the array
func checkElementExists(element string, elementList []string) bool {
	for _, temElement := range elementList {
		if temElement == element {
			return true
		}
	}
	return false
}

// Note: Should be used only when using static server-group configuration
// Returns for map of expected ServerGroup names with the pod count in the group
// assuming the CRD is having static server-group configuration in it
func GetExpectedRzaResultMap(clusterSize int, availableServerGroupList []string) map[string]int {
	expectedRzaResultMap := map[string]int{}
	availableServerGroupsLen := len(availableServerGroupList)
	for index := 0; index < clusterSize; index++ {
		currRzaGroup := availableServerGroupList[index%availableServerGroupsLen]
		if _, keyPresent := expectedRzaResultMap[currRzaGroup]; keyPresent {
			expectedRzaResultMap[currRzaGroup]++
		} else {
			expectedRzaResultMap[currRzaGroup] = 1
		}
	}
	return expectedRzaResultMap
}

// Returns for map of ServerGroup names with the pod count in the group
func GetDeployedRzaMap(kubeClient kubernetes.Interface, namespace string) (map[string]int, error) {
	// Get all couchbase pods
	couchbasePodList, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return nil, err
	}

	deployedRzaGroupsMap := map[string]int{}
	for _, cbPod := range couchbasePodList.Items {
		currRzaGroup := cbPod.Spec.NodeSelector[constants.FailureDomainZoneLabel]
		if _, keyPresent := deployedRzaGroupsMap[currRzaGroup]; keyPresent {
			deployedRzaGroupsMap[currRzaGroup]++
		} else {
			deployedRzaGroupsMap[currRzaGroup] = 1
		}
	}
	return deployedRzaGroupsMap, err
}

// Returns for map of pod name with the ServerGroup name on which they are deployed on
func GetDeployedRzaPodMap(kubeClient kubernetes.Interface, namespace string) (map[string]string, error) {
	// Get all couchbase pods
	couchbasePodList, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return nil, err
	}

	deployedRzaGroupsMap := map[string]string{}
	for _, cbPod := range couchbasePodList.Items {
		deployedRzaGroupsMap[cbPod.Name] = cbPod.Spec.NodeSelector[constants.FailureDomainZoneLabel]
	}
	return deployedRzaGroupsMap, err
}

// Returns the array of available server groups for the particular cluster given by k8sNodesData
func GetAvailableServerGroupsFromYmlData(k8sNodesData framework.ClusterInfo) []string {
	serverGroups := []string{}
	for _, node := range k8sNodesData.MasterNodeList {
		if node.NodeLabel != "" {
			if !checkElementExists(node.NodeLabel, serverGroups) {
				serverGroups = append(serverGroups, node.NodeLabel)
			}
		}
	}
	for _, node := range k8sNodesData.WorkerNodeList {
		if node.NodeLabel != "" {
			if !checkElementExists(node.NodeLabel, serverGroups) {
				serverGroups = append(serverGroups, node.NodeLabel)
			}
		}
	}
	return serverGroups
}

// GetAvailabilityZones returns a sorted list of configured availability zones from the cluster.
// These zones will be pre-provisioned by Kops etc. or added via a cluster decorator.
func GetAvailabilityZones(t *testing.T, cluster *types.Cluster) clustercapabilities.ZoneList {
	capabilities := clustercapabilities.MustNewCapabilities(t, cluster.KubeClient)
	if !capabilities.ZonesSet {
		t.Skip("cluster availability zones unset")
	}
	sort.Strings(capabilities.AvailabilityZones)
	return capabilities.AvailabilityZones
}

// Generic function to test AntiAffinity test case with values on / off
func RzaAntiAffinity(t *testing.T, antiAffinity string) {
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")

	getClusterSizeForAntiAffinity := func() int {
		maxNodesPossibleforAaOn := 0
		sort.Strings(availableServerGroupList)
		k8sNodes, err := targetKube.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		serverGroupNodeCountMap := map[string]int{}

		// Populate the server-group node count map
		for _, node := range k8sNodes.Items {
			nodeLabels := node.GetLabels()
			if serverGroup, zoneLabelOk := nodeLabels[constants.FailureDomainZoneLabel]; zoneLabelOk {
				serverGroupNodeCountMap[serverGroup] += 1
			}
		}

		// Simulate the nodes scheduling across server groups which is done in lexical order of group names
		// When the next server group to be allocated from is empty then terminate
		// and return the number of allocations that succeeded
		for {
			for _, serverGroup := range availableServerGroupList {
				if serverGroupNodeCountMap[serverGroup] == 0 {
					return maxNodesPossibleforAaOn
				}
				maxNodesPossibleforAaOn++
				serverGroupNodeCountMap[serverGroup]--
			}
		}
	}

	// TODO: so while we do force removal taints at the moment, we cannot rely on
	// this forever, perhaps the clustercapabilities can point out how many nodes
	// are schedulable...
	clusterSize := getClusterSizeForAntiAffinity()
	newPodsToAdd := constants.Size3

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":     strconv.Itoa(clusterSize),
		"name":     "test_config_1",
		"services": "data",
	}
	otherConfig1 := map[string]string{"antiAffinity": antiAffinity}
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"other1":       otherConfig1,
		"serverGroups": serverGroups,
	}

	t.Logf("AntiAffinity=%s ... \n attempting to create %d pod cluster with %d nodes", antiAffinity, clusterSize, clusterSize)
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	if clusterSize > 1 {
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	serviceIndex := 0
	testCouchbase, err = e2eutil.ResizeClusterNoWait(t, serviceIndex, clusterSize+newPodsToAdd, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	if antiAffinity == "on" {
		event := e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(testCouchbase, "CreationFailed", clusterSize)
		// Revert back to original cluster size
		testCouchbase, err = e2eutil.ResizeClusterNoWait(t, serviceIndex, clusterSize, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}
	} else if antiAffinity == "off" {
		// Updated new clusterSize
		clusterSize += newPodsToAdd
		for memberIndex := clusterSize - newPodsToAdd; memberIndex < clusterSize; memberIndex++ {
			event := e2eutil.NewMemberAddEvent(testCouchbase, memberIndex)
			if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
		}

		event := e2eutil.RebalanceCompletedEvent(testCouchbase)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries5); err != nil {
		t.Fatal(err.Error())
	}

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Generic test cases to update K8S node's server group labels
func RzaK8SNodeLabelEdit(t *testing.T, editType string) {
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

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

	nodeUpdateErrChan := make(chan error)
	k8sNodeLabelUpdateFunc := func() {
		// Rename node labels for particular server-group
		// Node label get updated in both update / remove scenario
		nodeUpdateErrChan <- UpdateServerGroupLabel(constants.FailureDomainZoneLabel, availableServerGroupList[0], "NewRzaGroup-1", targetKube.KubeClient)
		// TODO: you MUST revert the label if you are changing it or any subsequent
		// persistent volume tests will fail if using EBS for example.
	}

	if strings.Contains(editType, "InParallel") {
		go k8sNodeLabelUpdateFunc()
	} else {
		k8sNodeLabelUpdateFunc()
	}
	defer UpdateServerGroupLabel(constants.FailureDomainZoneLabel, "NewRzaGroup-1", availableServerGroupList[0], targetKube.KubeClient)

	newAvailableServerGroupList := []string{}
	if strings.Contains(editType, "update") {
		if strings.Contains(editType, "WithDelay") {
			t.Log("Entering sleep to add delay before CRD update")
			time.Sleep(time.Second * 60)
		}
		// Updating CRD to add new server-group in CRD
		newAvailableServerGroupList = append(availableServerGroupList, "NewRzaGroup-1")
		newAvailableServerGroups := strings.Join(newAvailableServerGroupList, ",")
		testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", newAvailableServerGroups, targetKube.CRClient, testCouchbase, constants.Retries5)
		if err != nil {
			t.Fatalf("Failed to update server groups: %v", err)
		}
	}

	if err := <-nodeUpdateErrChan; err != nil {
		t.Fatal(err)
	}

	service := 0
	prevClusterSize := clusterSize
	clusterSize += 1
	testCouchbase, err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	for memberId := prevClusterSize; memberId < clusterSize; memberId++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberId)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	// Create a expected RZA results map for verification
	sort.Strings(newAvailableServerGroupList)
	expectedRzaResultMap = GetExpectedRzaResultMap(clusterSize, newAvailableServerGroupList)
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

// Define Static ServersGroups in the CRD
// Deploy the cluster through operator and verify the server groups are balanced
func TestRzaCreateClusterWithStaticConfig(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

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
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Define Class based ServersGroups config in the CRD
// Deploy the cb cluster and verify the server groups are balanced as specified in the CRD
func TestRzaCreateClusterWithClassBasedConfig(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	clusterSize := 7
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetClassSpecificServiceConfigMap(3, "test_config_1", []string{"data", "index"}, []string{availableServerGroupList[0], availableServerGroupList[2]})
	serviceConfig2 := e2eutil.GetClassSpecificServiceConfigMap(1, "test_config_2", []string{"query"}, []string{availableServerGroupList[1]})
	serviceConfig3 := e2eutil.GetClassSpecificServiceConfigMap(3, "test_config_3", []string{"search"}, []string{availableServerGroupList[0], availableServerGroupList[2]})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"service2":     serviceConfig2,
		"service3":     serviceConfig3,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	// Deploy couchbase cluster
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	// Creating expected RZA server groups pod maps
	expectedRzaResultMap := map[string]int{
		availableServerGroupList[0]: 4,
		availableServerGroupList[1]: 1,
		availableServerGroupList[2]: 2,
	}

	expectedRzaPodNodeSelectorMap := map[string]string{
		testCouchbase.Name + "-0000": availableServerGroupList[0],
		testCouchbase.Name + "-0001": availableServerGroupList[2],
		testCouchbase.Name + "-0002": availableServerGroupList[0],
		testCouchbase.Name + "-0003": availableServerGroupList[1],
		testCouchbase.Name + "-0004": availableServerGroupList[0],
		testCouchbase.Name + "-0005": availableServerGroupList[2],
		testCouchbase.Name + "-0006": availableServerGroupList[0],
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
		t.Errorf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	deployedRzaPodMap, err := GetDeployedRzaPodMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaPodNodeSelectorMap, deployedRzaPodMap) == false {
		t.Errorf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaPodNodeSelectorMap, deployedRzaPodMap)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Scale up the couchbase nodes both general scalling and service based scalling
func TestRzaResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

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

	// Starting resize cluster test
	service := 0
	clusterSizes := []int{2, 7, 4}
	prevClusterSize := clusterSize
	memberToBeAdded := clusterSize

	for _, clusterSize := range clusterSizes {
		membersAdded := []int{}
		membersRemoved := []int{}
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

		// Update the expected RZA results map for verification
		expectedRzaResultMap = GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

		// Resize cluster and wait for healthy cluster
		testCouchbase, err = e2eutil.ResizeClusterNoWait(t, service, clusterSize, targetKube.CRClient, testCouchbase)
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

		// Update deployed server-groups based on new cluster size
		deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
		if err != nil {
			t.Fatalf("Failed to get deployed Rza map: %v", err)
		}

		// Cross check it matches the expected values
		if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
			t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			for _, memberId := range membersAdded {
				expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberId)
			}
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
			for _, memberId := range membersRemoved {
				expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", memberId)
			}
			expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
		}
		prevClusterSize = clusterSize
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Remove one of the rack zones from the CRD definition
// Expects pods to redistribute to available groups
func TestRzaServerGroupRemoval(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

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

	availableServerGroupList = []string{availableServerGroupList[0], availableServerGroupList[2]}
	availableServerGroups = strings.Join(availableServerGroupList, ",")
	testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", availableServerGroups, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatalf("Failed to update server groups: %v", err)
	}

	event := e2eutil.NewMemberRemoveEvent(testCouchbase, 1)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Failed to remove pod from removed server-group failed: %v", err)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", 1)

	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Failed to add new pods to replace removed server group node: %v", err)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", clusterSize)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Add a new server group using the CRD update
// Expected new pods scaled up is added to new groups to balance the pods
func TestRzaServerGroupAddition(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailabilityZones(t, targetKube)

	serverGroupsUsed := []string{availableServerGroupList[0], availableServerGroupList[2]}
	availableServerGroups := strings.Join(serverGroupsUsed, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, serverGroupsUsed)

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

	availableServerGroups = strings.Join(availableServerGroupList, ",")
	testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", availableServerGroups, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatalf("Failed to update server groups: %v", err)
	}

	clusterSize = clusterSize + 1
	service := 0

	// Resize cluster and wait for healthy cluster
	testCouchbase, err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", clusterSize-1)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	// Create a expected RZA results map for verification
	expectedRzaResultMap = GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

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

// Deploy Couchbase cluster over valid server-groups
// Update CRD to scale up nodes using invalid server-group name
// New pod creation should fail because of unavailable server group
func TestRzaNegScaleupCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	clusterSize := len(availableServerGroupList)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Deploy couchbase cluster
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

	newAvailableServerGroupList := append(availableServerGroupList, "InvalidGroup-1")
	newAvailableServerGroups := strings.Join(newAvailableServerGroupList, ",")

	testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", newAvailableServerGroups, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatalf("Failed to update server groups: %v", err)
	}

	service := 0
	clusterSize++
	// Add one more node to cluster
	testCouchbase, err = e2eutil.ResizeClusterNoWait(t, service, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatalf("Cluster update for server size spec failed")
	}

	event := e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize-1)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "CreationFailed", clusterSize-1)

	// Revert server group addition
	testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", availableServerGroups, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatalf("Failed to update server groups: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize-1)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", clusterSize-1)

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	// Update expected RZA results map for verification
	expectedRzaResultMap = GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Server-group is brought down so the communication with K8S node is down
// Expects recration of new pods should fail due to the server group down
func TestRzaServerGroupDown(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKubeName := f.TestClusters[0]
	targetKube := f.ClusterSpec[targetKubeName]

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Deploy couchbase cluster
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

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	// Set taint to
	nodeTaint := v1.Taint{
		Key:    "noExecKey",
		Value:  "noExecVal",
		Effect: "NoExecute",
	}
	nodeTaintList := []v1.Taint{nodeTaint}

	operatorPodName, err := e2eutil.GetOperatorName(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	operatorNodeIndex, err := e2eutil.GetTargetNodeIndexForPod(targetKube.KubeClient, f.Namespace, operatorPodName)
	if err != nil {
		t.Fatal(err)
	}

	var nodeIndex int
	memberIdToGoDown := 1
	for ; memberIdToGoDown < clusterSize; memberIdToGoDown++ {
		memberNameToGoDown := couchbaseutil.CreateMemberName(testCouchbase.Name, memberIdToGoDown)
		nodeIndex, err = e2eutil.GetTargetNodeIndexForPod(targetKube.KubeClient, f.Namespace, memberNameToGoDown)
		if err != nil {
			t.Fatal(err)
		}
		if nodeIndex != operatorNodeIndex {
			break
		}
	}

	t.Logf("Selected member id %d running on node %d\n", memberIdToGoDown, nodeIndex)
	if err = e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, true, nodeTaintList, nodeIndex); err != nil {
		t.Fatalf("Failed to set node taint and schedulable property: %v", err)
	}
	defer e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex)

	// Wait till pod creation fail due to the Server Group unavailable to schedule a new pod
	event := e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 180); err != nil {
		t.Fatalf("Requested node didn't go down as expected: %v", event)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", memberIdToGoDown)
	expectedEvents.AddClusterPodEvent(testCouchbase, "FailedOver", memberIdToGoDown)
	expectedEvents.AddClusterPodEvent(testCouchbase, "CreationFailed", clusterSize)

	// Remove the taint from the node to allow pod schedulling
	if err := e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex); err != nil {
		t.Fatalf("Failed to unset node taint and schedulable property: %v", err)
	}

	// Wait for pod member to add back to the cluster
	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Failed to add pod after removing the taint: %v", err)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", clusterSize)

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30); err != nil {
		t.Fatalf("Cluster failed to become healthy: %v", err)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", memberIdToGoDown)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Create cluster with AA-ON and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should fail
func TestRzaAntiAffinityOn(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	RzaAntiAffinity(t, "on")
}

// Create cluster with AA-OFF and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should succeed
func TestRzaAntiAffinityOff(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	RzaAntiAffinity(t, "off")
}

// Deploy couchbase cluster using multiple server groups
// Update existing K8S node with different label value
// in parallel with CRD update
// Expects, the new nodes to get spawed in new group
func TestRzaUpdateK8SNodeLabelAndCrd(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	RzaK8SNodeLabelEdit(t, "updateNodeCrdInParallel")
}

// Deploy couchbase cluster using multiple server groups
// Update existing K8S node with different label value
// And update the CRD with some delay
// Expects, the new nodes to get spawed in new group only after CRD update
func TestRzaUpdateK8SNodeLabelAndCrdWithDelay(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	RzaK8SNodeLabelEdit(t, "updateNodeCrdWithDelay")
}

// Deploy couchbase cluster using server groups turned on
// Remove particular node label from cluster nodes
// Operator should kill the pods in the removed server group and redistribute
// to other groups uniformly
func TestRzaRemoveK8SNodeLabel(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	RzaK8SNodeLabelEdit(t, "remove")
}
