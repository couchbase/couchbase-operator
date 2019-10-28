package e2e

import (
	"context"
	"fmt"
	"reflect"
	"sort"
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
)

// Labels k8s nodes based on the values provided from the ClusterInfo struct
func k8sNodesAddLabel(k8s *types.Cluster, nodes framework.ClusterInfo, nodeLabelName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		k8sNodeList, err := k8s.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to get k8s nodes: %v", err)
		}
		for _, k8sNode := range k8sNodeList.Items {
			labelChanged := false
			nodeLabels := k8sNode.GetLabels()
			nodeIPAddress := k8sNode.Status.Addresses[0].Address
			for _, node := range nodes.MasterNodeList {
				if node.IP == nodeIPAddress {
					nodeLabels[nodeLabelName] = node.NodeLabel
					labelChanged = true
					break
				}
			}
			for _, node := range nodes.WorkerNodeList {
				if node.IP == nodeIPAddress {
					nodeLabels[nodeLabelName] = node.NodeLabel
					labelChanged = true
					break
				}
			}
			if !labelChanged {
				return fmt.Errorf("unable to find node %v", nodeIPAddress)
			}
			k8sNode.SetLabels(nodeLabels)

			// Reset Taints and set schedulable property for nodes
			k8sNode.Spec.Unschedulable = false
			k8sNode.Spec.Taints = []v1.Taint{}

			if _, err := k8s.KubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
				return fmt.Errorf("failed to update label for node %v: %v", nodeIPAddress, err)
			}
		}
		return nil
	})
}

// rzaMap maps an availability zone to the number of pods scheduled in it.
type rzaMap map[string]int

// Note: Should be used only when using static server-group configuration
// Returns for map of expected ServerGroup names with the pod count in the group
// assuming the CRD is having static server-group configuration in it
func mustGetExpectedRzaResultMap(t *testing.T, cluster *types.Cluster, groupSize int) rzaMap {
	// Get the sorted list of AZs and the number of them.
	serverGroups := getAvailabilityZones(t, cluster)

	expected := rzaMap{}
	expected.accumulateRzaServerClass(groupSize, serverGroups)
	return expected
}

// accumulateRzaServerClass adds in class based scheduling to an rzaMap
func (expected rzaMap) accumulateRzaServerClass(groupSize int, serverGroups []string) {
	numServerGroups := len(serverGroups)

	// For each cluster member expect them to be striped, in order across
	// the available AZs
	for index := 0; index < groupSize; index++ {
		expected[serverGroups[index%numServerGroups]]++
	}
}

// mustValidateRzaMap accepts an expected scheduling and compares against reality
func (expected rzaMap) mustValidateRzaMap(t *testing.T, cluster *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	pods, err := cluster.KubeClient.CoreV1().Pods(couchbase.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		e2eutil.Die(t, err)
	}

	rzaMap := rzaMap{}
	for _, pod := range pods.Items {
		rzaMap[pod.Spec.NodeSelector[constants.FailureDomainZoneLabel]]++
	}

	if !reflect.DeepEqual(expected, rzaMap) {
		e2eutil.Die(t, fmt.Errorf("RZA scheduling mismatch: requested %v actual %v", expected, rzaMap))
	}
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

	// Sure the list is lexically ordered so that the scheduling emulation
	// works as desired.
	sort.Strings(output)
	return output
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
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// Create a expected RZA results map for verification
	expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	testCouchbase := e2espec.NewBasicClusterSpec(class1Size)
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
	expected := rzaMap{}
	expected.accumulateRzaServerClass(class1Size, class1ServerGroups)
	expected.accumulateRzaServerClass(class2Size, class2ServerGroups)
	expected.accumulateRzaServerClass(class3Size, class3ServerGroups)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

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

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// Create a map for server-groups based on deployed cb-server nodes
	expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	// Starting resize cluster test
	service := 0
	clusterSizes := []int{2, 7, 4}
	for _, clusterSize := range clusterSizes {
		// Resize cluster and wait for healthy cluster
		testCouchbase = e2eutil.MustResizeClusterNoWait(t, service, clusterSize, targetKube, testCouchbase)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

		// Update deployed server-groups based on new cluster size
		expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
		expected.mustValidateRzaMap(t, targetKube, testCouchbase)
	}

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleUpSequence(5),
		e2eutil.ClusterScaleDownSequence(3),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create cluster with AA-ON and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should fail
func TestRzaAntiAffinityOn(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	availableServerGroups := getAvailabilityZones(t, targetKube)
	// WARNING: this assumes all AZs have the same number of nodes
	clusterSize := e2eutil.MustNumNodes(t, targetKube)
	class := 0

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.AntiAffinity = true
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// When ready scale up, with AA on it should fail.
	expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	testCouchbase = e2eutil.MustResizeClusterNoWait(t, class, clusterSize+1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize), 2*f.PodCreateTimeout)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, class, clusterSize, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*f.PodCreateTimeout)

	// Create a map for server-groups based on deployed cb-server nodes
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create cluster with AA-OFF and deploy the çb cluster
// Add nodes beyond the number of available cluster nodes
// Expects pod creation beyond k8s cluster size should succeed
func TestRzaAntiAffinityOff(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	availableServerGroups := getAvailabilityZones(t, targetKube)
	// WARNING: this assumes all AZs have the same number of nodes
	clusterSize := e2eutil.MustNumNodes(t, targetKube)
	class := 0

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// When ready scale up, with AA on it should fail.
	expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	testCouchbase = e2eutil.MustResizeClusterNoWait(t, class, clusterSize+1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Create a map for server-groups based on deployed cb-server nodes
	expected = mustGetExpectedRzaResultMap(t, targetKube, testCouchbase.Spec.TotalSize())
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
