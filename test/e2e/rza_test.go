package e2e

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/clustercapabilities"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Labels k8s nodes based on the values provided from the ClusterInfo struct
func k8sNodesAddLabel(k8s *types.Cluster, nodes framework.ClusterInfo, nodeLabelName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, e2eutil.IntMax, "", "", func() error {
		k8sNodeList, err := k8s.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return errors.New("Failed to get k8s nodes " + err.Error())
		}
		for _, k8sNode := range k8sNodeList.Items {
			labelChanged := false
			nodeLabels := k8sNode.GetLabels()
			nodeIpAddress := k8sNode.Status.Addresses[0].Address
			for _, node := range nodes.MasterNodeList {
				if node.Ip == nodeIpAddress {
					nodeLabels[nodeLabelName] = node.NodeLabel
					labelChanged = true
					break
				}
			}
			for _, node := range nodes.WorkerNodeList {
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

			if _, err := k8s.KubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
				return errors.New("Failed to update label for node " + nodeIpAddress + ": " + err.Error())
			}
		}
		return nil
	})
}

// Note: Should be used only when using static server-group configuration
// Returns for map of expected ServerGroup names with the pod count in the group
// assuming the CRD is having static server-group configuration in it
func getExpectedRzaResultMap(clusterSize int, availableServerGroups []string) map[string]int {
	expectedRzaResultMap := map[string]int{}
	availableServerGroupsLen := len(availableServerGroups)
	for index := 0; index < clusterSize; index++ {
		currRzaGroup := availableServerGroups[index%availableServerGroupsLen]
		if _, keyPresent := expectedRzaResultMap[currRzaGroup]; keyPresent {
			expectedRzaResultMap[currRzaGroup]++
		} else {
			expectedRzaResultMap[currRzaGroup] = 1
		}
	}
	return expectedRzaResultMap
}

// Returns for map of ServerGroup names with the pod count in the group
func getDeployedRzaMap(kubeClient kubernetes.Interface, namespace string) (map[string]int, error) {
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

// getAvailabilityZones returns a sorted list of configured availability zones from the cluster.
// These zones will be pre-provisioned by Kops etc. or added via a cluster decorator.
func getAvailabilityZones(t *testing.T, cluster *types.Cluster) clustercapabilities.ZoneList {
	capabilities := clustercapabilities.MustNewCapabilities(t, cluster.KubeClient)
	if !capabilities.ZonesSet {
		t.Skip("cluster availability zones unset")
	}
	sort.Strings(capabilities.AvailabilityZones)
	return capabilities.AvailabilityZones
}

// mustNumAvailabilityZones returns the number of availability zones defined in the target cluster.
func mustNumAvailabilityZones(t *testing.T, cluster *types.Cluster) int {
	return len(getAvailabilityZones(t, cluster))
}

// Generic function to test AntiAffinity test case with values on / off
func rzaAntiAffinity(t *testing.T, antiAffinity bool) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	availableServerGroups := getAvailabilityZones(t, targetKube)

	getClusterSizeForAntiAffinity := func() int {
		maxNodesPossibleforAaOn := 0
		sort.Strings(availableServerGroups)
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
			for _, serverGroup := range availableServerGroups {
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

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize, constants.AdminHidden)
	testCouchbase.Spec.AntiAffinity = antiAffinity
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	if clusterSize > 1 {
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceIndex := 0
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, serviceIndex, clusterSize+newPodsToAdd, targetKube, testCouchbase)

	if antiAffinity {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize), 2*time.Minute)
		expectedEvents.AddClusterPodEvent(testCouchbase, "CreationFailed", clusterSize)
		// Revert back to original cluster size
		testCouchbase = e2eutil.MustResizeClusterNoWait(t, serviceIndex, clusterSize, targetKube, testCouchbase)
	} else {
		// Updated new clusterSize
		clusterSize += newPodsToAdd
		for memberIndex := clusterSize - newPodsToAdd; memberIndex < clusterSize; memberIndex++ {
			e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberIndex), 2*time.Minute)
			expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
		}

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := getExpectedRzaResultMap(clusterSize, availableServerGroups)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := getDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Define Static ServersGroups in the CRD
// Deploy the cluster through operator and verify the server groups are balanced
func TestRzaCreateClusterWithStaticConfig(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroups := getAvailabilityZones(t, targetKube)

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize, constants.AdminHidden)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := getExpectedRzaResultMap(clusterSize, availableServerGroups)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", "default")

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := getDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// chooseServerGroups deterministically chooses a set of server groups to use based
// on a seed, and a requested number of server groups.
func chooseServerGroups(groups []string, seed string, max int) []string {
	// Cap the maximum requested groups to the set actually provided by the
	// underlying platform.
	if max > len(groups) {
		max = len(groups)
	}

	// Convert the seed into an integer index into our groups, this pseudo-
	// randomizes so we don't start at zero all the time.
	index := 0
	for i := 0; i < len(seed); i++ {
		index += int(seed[i])
	}

	// Return a contiguous (with wrap) set of server groups from the pseudo-
	// random index.
	output := []string{}
	for i := 0; i < max; i++ {
		output = append(output, groups[(i+index)%len(groups)])
	}

	return output
}

// accumulateExpectedPods populates a map of server group to pod count based on
// the number of servers in a class and the allowed server groups.
func accumulateExpectedPods(expected map[string]int, count int, serverGroups []string) {
	sort.Strings(serverGroups)
	for i := 0; i < count; i++ {
		expected[serverGroups[i%len(serverGroups)]]++
	}
}

// Define Class based ServersGroups config in the CRD
// Deploy the cb cluster and verify the server groups are balanced as specified in the CRD
func TestRzaCreateClusterWithClassBasedConfig(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	serverGroups := getAvailabilityZones(t, targetKube)

	class1Size := 3
	class2Size := 1
	class3Size := 3

	class1ServerGroups := chooseServerGroups(serverGroups, "class1", 2)
	class2ServerGroups := chooseServerGroups(serverGroups, "class2", 1)
	class3ServerGroups := chooseServerGroups(serverGroups, "class3", 2)

	// Create cluster spec for RZA feature
	clusterSize := 7

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(class1Size, constants.AdminHidden)
	testCouchbase.Spec.Servers = []couchbasev2.ServerConfig{
		{
			Name: "service1",
			Size: class1Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
			ServerGroups: class1ServerGroups,
		},
		{
			Name: "service2",
			Size: class2Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.QueryService,
			},
			ServerGroups: class2ServerGroups,
		},
		{
			Name: "service3",
			Size: class3Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.SearchService,
			},
			ServerGroups: class3ServerGroups,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// Creating expected RZA server groups pod maps
	expectedRzaResultMap := map[string]int{}
	accumulateExpectedPods(expectedRzaResultMap, class1Size, class1ServerGroups)
	accumulateExpectedPods(expectedRzaResultMap, class2Size, class2ServerGroups)
	accumulateExpectedPods(expectedRzaResultMap, class3Size, class3ServerGroups)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := getDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}

	// Cross check it matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Errorf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups
// Scale up the couchbase nodes both general scalling and service based scalling
func TestRzaResizeCluster(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroups := getAvailabilityZones(t, targetKube)
	expectedRzaResultMap := getExpectedRzaResultMap(clusterSize, availableServerGroups)

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize, constants.AdminHidden)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", "default")

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := getDeployedRzaMap(targetKube.KubeClient, f.Namespace)
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
		expectedRzaResultMap = getExpectedRzaResultMap(clusterSize, availableServerGroups)

		// Resize cluster and wait for healthy cluster
		testCouchbase = e2eutil.MustResizeClusterNoWait(t, service, clusterSize, targetKube, testCouchbase)
		t.Logf("Waiting For Cluster Size To Be: %v...\n", strconv.Itoa(clusterSize))
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

		// Update deployed server-groups based on new cluster size
		deployedRzaGroupsMap, err = getDeployedRzaMap(targetKube.KubeClient, f.Namespace)
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
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create cluster with AA-ON and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should fail
func TestRzaAntiAffinityOn(t *testing.T) {
	rzaAntiAffinity(t, true)
}

// Create cluster with AA-OFF and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should succeed
func TestRzaAntiAffinityOff(t *testing.T) {
	rzaAntiAffinity(t, false)
}
