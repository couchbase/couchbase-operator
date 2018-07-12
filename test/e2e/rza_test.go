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

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

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

		if _, err = kubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
			return errors.New("Failed to update label for node " + nodeIpAddress + ": " + err.Error())
		}
	}
	return nil
}

// Remove specified label from all k8s nodes identified by kubeName
func K8SNodesRemoveLabel(nodeLabelName string, kubeClient kubernetes.Interface) error {
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to get k8s nodes " + err.Error())
	}
	for _, k8sNode := range k8sNodeList.Items {
		nodeLabels := k8sNode.GetLabels()
		delete(nodeLabels, nodeLabelName)
		k8sNode.SetLabels(nodeLabels)
		if _, err = kubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
			return errors.New("Failed to delete label for node " + k8sNode.Name + ": " + err.Error())
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
		if _, err = kubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
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
	couchbasePodList, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	if err != nil {
		return nil, err
	}

	deployedRzaGroupsMap := map[string]int{}
	for _, cbPod := range couchbasePodList.Items {
		currRzaGroup := cbPod.Spec.NodeSelector["server-group.couchbase.com/zone"]
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
	couchbasePodList, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	if err != nil {
		return nil, err
	}

	deployedRzaGroupsMap := map[string]string{}
	for _, cbPod := range couchbasePodList.Items {
		deployedRzaGroupsMap[cbPod.Name] = cbPod.Spec.NodeSelector["server-group.couchbase.com/zone"]
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

// Wrapper function to Label the K8S nodes and
// remove the labels after the test case execution
func rzaNodeLabeller(testFunc framework.TestFunc, args framework.DecoratorArgs) framework.TestFunc {
	wrapperFunc := func(t *testing.T) {
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

		// Adding retry loop because sometimes node label update is failing. On retry, it will succeed
		for retryCount := 0; retryCount < 3; retryCount++ {
			t.Logf("Retry node label update: %d", retryCount)
			// Label K8S nodes based on the labels present in the cluster conf yaml file
			if err = K8SNodesAddLabel("server-group.couchbase.com/zone", targetKube.KubeClient, k8sNodesData[0]); err == nil {
				break
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		defer K8SNodesRemoveLabel("server-group.couchbase.com/zone", targetKube.KubeClient)

		testFunc(t)
	}
	return wrapperFunc
}

// Generic function to test AntiAffinity test case with values on / off
func RzaAntiAffinity(t *testing.T, antiAffinity string) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Get number of available nodes from K8S cluster
	clusterSize, err := e2eutil.NumK8Nodes(targetKube.KubeClient)
	if err != nil {
		t.Fatalf("failed to get number of kubernetes nodes: %v", err)
	}

	// Create cluster spec for RZA feature
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
	availableServerGroups := strings.Join(availableServerGroupList, ",")

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

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	t.Logf("AntiAffinity=%s ... \n attempting to create %d pod cluster with %d nodes", antiAffinity, clusterSize, clusterSize)
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	for i := 0; i < clusterSize; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	service := 0
	err = e2eutil.ResizeCluster(t, service, clusterSize+1, targetKube.CRClient, testCouchbase)
	if antiAffinity == "on" {
		if err == nil {
			t.Fatal("Cluster resized more than number of nodes with AntiAffinity On..")
		}
		expectedEvents.AddMemberCreationFailedEvent(testCouchbase, clusterSize)
	} else if antiAffinity == "off" {
		if err != nil {
			t.Fatalf("Cluster failed to scale up with antiaffinity off: %v", err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)
		clusterSize++
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Generic test cases to update K8S node's server group labels
func RzaK8SNodeLabelEdit(t *testing.T, editType string) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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
		nodeUpdateErrChan <- UpdateServerGroupLabel("server-group.couchbase.com/zone", availableServerGroupList[0], "NewRzaGroup-1", targetKube.KubeClient)
	}

	if strings.Contains(editType, "InParallel") {
		go k8sNodeLabelUpdateFunc()
	} else {
		k8sNodeLabelUpdateFunc()
	}

	if strings.Contains(editType, "update") {
		if strings.Contains(editType, "WithDelay") {
			t.Log("Entering sleep to add delay before CRD update")
			time.Sleep(time.Second * 120)
		}
		// Updating CRD to add new server-group in CRD
		availableServerGroupList = append(availableServerGroupList, "NewRzaGroup-1")
		testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", availableServerGroups, targetKube.CRClient, testCouchbase, 3)
		if err != nil {
			t.Fatalf("Failed to update server groups: %v", err)
		}
	}

	err = <-nodeUpdateErrChan
	if err != nil {
		t.Fatal(err)
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Define Static ServersGroups in the CRD
// Deploy the cluster through operator and verify the server groups are balanced
func TestRzaCreateClusterWithStaticConfig(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Define Class based ServersGroups config in the CRD
// Deploy the cb cluster and verify the server groups are balanced as specified in the CRD
func TestRzaCreateClusterWithClassBasedConfig(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 7
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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
		testCouchbase.Name + "-0000": availableServerGroupList[0],
		testCouchbase.Name + "-0001": availableServerGroupList[2],
		testCouchbase.Name + "-0002": availableServerGroupList[0],
		testCouchbase.Name + "-0003": availableServerGroupList[1],
		testCouchbase.Name + "-0004": availableServerGroupList[0],
		testCouchbase.Name + "-0005": availableServerGroupList[2],
		testCouchbase.Name + "-0006": availableServerGroupList[0],
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
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Scale up the couchbase nodes both general scalling and service based scalling
func TestRzaResizeCluster(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Remove one of the rack zones from the CRD definition
// Expects pods to redistribute to available groups
func TestRzaServerGroupRemoval(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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
	testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", availableServerGroups, targetKube.CRClient, testCouchbase, 3)
	if err != nil {
		t.Fatalf("Failed to update server groups: %v", err)
	}

	event := e2eutil.NewMemberRemoveEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatalf("Failed to remove pod from removed server-group failed: %v", err)
	}

	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 3)
	err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatalf("Failed to add new pods to replace removed server group node: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Add a new server group using the CRD update
// Expected new pods scaled up is added to new groups to balance the pods
func TestRzaServerGroupAddition(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])

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

	sort.Strings(serverGroupsUsed)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, serverGroupsUsed)

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
	testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", availableServerGroups, targetKube.CRClient, testCouchbase, 3)
	if err != nil {
		t.Fatalf("Failed to update server groups: %v", err)
	}

	clusterSize = clusterSize + 1
	service := 0

	// Resize cluster and wait for healthy cluster
	err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
	if err != nil {
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
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Kill nodes across server groups
// Expects pods to be spawed in the same server-groups again
func TestRzaKillServerPods(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 7
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Deploy couchbase cluster
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
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

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("Unable to get Client for cluster: %v", err)
	}

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	// Killing multiple pods to see that they are added in proper server groups in balanced manner
	podsToKill := []int{2, 3, 6}
	for _, podToKillMemberId := range podsToKill {
		if err = e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId); err != nil {
			t.Fatalf("Failed to kill pod member %d: %v", podToKillMemberId, err)
		}
		expectedEvents.AddFailedAddNodeEvent(testCouchbase, podToKillMemberId)
	}

	// Waiting for failover to happen for all pods
	for _, podToKillMemberId := range podsToKill {
		event := e2eutil.NewMemberFailedOverEvent(testCouchbase, podToKillMemberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 180); err != nil {
			t.Fatalf("Fail over didn't occur for killed pod %d: %v", podToKillMemberId, err)
		}
		if err := client.ResetFailoverCounter(); err != nil {
			t.Fatal(err)
		}
	}

	// Waiting for add new nodes to replace failed-over node
	for memberIndex := 7; memberIndex < 10; memberIndex++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberIndex)
		err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300)
		if err != nil {
			t.Fatalf("Failed to add pod to replace killed pods: %v", err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}

	err = e2eutil.WaitForClusterBalancedCondition(t, targetKube.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err.Error())
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

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy Couchbase cluster over valid server-groups
// Update CRD to scale up nodes using invalid server-group name
// New pod creation should fail because of unavailable server group
func TestRzaNegScaleupCluster(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Deploy couchbase cluster
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
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

	availableServerGroupList = append(availableServerGroupList, "InvalidGroup-1")
	availableServerGroups = strings.Join(availableServerGroupList, ",")

	testCouchbase, err = e2eutil.UpdateClusterSpec("ServerGroups", availableServerGroups, targetKube.CRClient, testCouchbase, 3)
	if err != nil {
		t.Fatalf("Failed to update server groups: %v", err)
	}

	service := 0
	if err = e2eutil.ResizeCluster(t, service, clusterSize+1, targetKube.CRClient, testCouchbase); err == nil {
		t.Fatalf("Cluster resize succussful with invalid server group")
	}

	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, clusterSize)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	if err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase); err == nil {
		t.Fatalf("Cluster resize succussful with invalid server group")
	}

	err = e2eutil.WaitForClusterBalancedCondition(t, targetKube.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Server-group is brought down so the communication with K8S node is down
// Expects recration of new pods should fail due to the server group down
func TestRzaServerGroupDown(t *testing.T) {
	f := framework.Global
	targetKubeName := "NewCluster1"
	targetKube := f.ClusterSpec[targetKubeName]

	k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{targetKubeName})
	if err != nil {
		t.Fatalf("Failed to read cluster yaml data: %v", err)
	}

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroupList := GetAvailableServerGroupsFromYmlData(k8sNodesData[0])
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

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Deploy couchbase cluster
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
	podTaint := v1.Taint{
		Key:    "noExecKey",
		Value:  "noExecVal",
		Effect: "NoExecute",
	}
	podTaintList := []v1.Taint{podTaint}

	nodeIndex := 2
	memberIdToGoDown := 1

	if err = e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, true, podTaintList, nodeIndex); err != nil {
		t.Fatalf("Failed to set node taint and schedulable property: %v", err)
	}
	defer e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex)

	expectedEvents.AddMemberDownEvent(testCouchbase, memberIdToGoDown)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToGoDown)
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(targetKubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("Unable to get Client for cluster: %v", err)
	}

	if err = e2eutil.WaitForUnhealthyNodes(t, client, 5, 1); err != nil {
		t.Fatalf("No unhealthy nodes in cluster: %v", err)
	}

	if err = e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex); err != nil {
		t.Fatalf("Failed to unset node taint and schedulable property: %v", err)
	}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 3)
	if err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Failed to add pod after removing the taint: %v", err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, memberIdToGoDown)
	if err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Failed to remove unclusteres pod: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToGoDown)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	if err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, 3, e2eutil.Retries30); err != nil {
		t.Fatalf("Cluster failed to become healthy: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create cluster with AA-ON and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should fail
func TestRzaAntiAffinityOn(t *testing.T) {
	RzaAntiAffinity(t, "on")
}

// Create cluster with AA-OFF and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should succeed
func TestRzaAntiAffinityOff(t *testing.T) {
	RzaAntiAffinity(t, "off")
}

// Deploy couchbase cluster using multiple server groups
// Update existing K8S node with different label value
// in parallel with CRD update
// Expects, the new nodes to get spawed in new group
func TestRzaUpdateK8SNodeLabelAndCrd(t *testing.T) {
	RzaK8SNodeLabelEdit(t, "updateNodeCrdInParallel")
}

// Deploy couchbase cluster using multiple server groups
// Update existing K8S node with different label value
// And update the CRD with some delay
// Expects, the new nodes to get spawed in new group only after CRD update
func TestRzaUpdateK8SNodeLabelAndCrdWithDelay(t *testing.T) {
	RzaK8SNodeLabelEdit(t, "updateNodeCrdWithDelay")
}

// Deploy couchbase cluster using server groups turned on
// Remove particular node label from cluster nodes
// Operator should kill the pods in the removed server group and redistribute
// to other groups uniformly
func TestRzaRemoveK8SNodeLabel(t *testing.T) {
	RzaK8SNodeLabelEdit(t, "remove")
}
