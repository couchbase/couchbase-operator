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
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/clustercapabilities"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// rzaMap maps an availability zone to the number of pods scheduled in it.
type rzaMap map[string]int

// Note: Should be used only when using static server-group configuration
// Returns for map of expected ServerGroup names with the pod count in the group
// assuming the CRD is having static server-group configuration in it.
func getExpectedRzaResultMap(groupSize int, serverGroups []string) rzaMap {
	expected := rzaMap{}
	expected.accumulateRzaServerClass(groupSize, serverGroups)

	return expected
}

// accumulateRzaServerClass adds in class based scheduling to an rzaMap.
func (expected rzaMap) accumulateRzaServerClass(groupSize int, serverGroups []string) {
	numServerGroups := len(serverGroups)

	// For each cluster member expect them to be striped, in order across
	// the available AZs
	for index := 0; index < groupSize; index++ {
		expected[serverGroups[index%numServerGroups]]++
	}
}

// mustValidateRzaMap accepts an expected scheduling and compares against reality.
func (expected rzaMap) mustValidateRzaMap(t *testing.T, cluster *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	pods, err := cluster.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
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

	sort.Strings(capabilities.AvailabilityZones)

	return capabilities.AvailabilityZones
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

// Define Static ServersGroups in the CRD.
// Deploy the cluster through operator and verify the server groups are balanced.
func TestRzaCreateClusterWithStaticConfig(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroups := getAvailabilityZones(t, kubernetes)

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = availableServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Create a expected RZA results map for verification
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Define Class based ServersGroups config in the CRD.
// Deploy the cb cluster and verify the server groups are balanced as specified in the CRD.
func TestRzaCreateClusterWithClassBasedConfig(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	serverGroups := getAvailabilityZones(t, kubernetes)

	class1Size := 3
	class2Size := 1
	class3Size := 3

	class1ServerGroups := chooseServerGroups(serverGroups, "class1", 2)
	class2ServerGroups := chooseServerGroups(serverGroups, "class2", 1)
	class3ServerGroups := chooseServerGroups(serverGroups, "class3", 2)

	// Create cluster spec for RZA feature
	clusterSize := 7

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().Generate(kubernetes)
	cluster.Spec.Servers = []couchbasev2.ServerConfig{
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
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Creating expected RZA server groups pod maps
	expected := rzaMap{}
	expected.accumulateRzaServerClass(class1Size, class1ServerGroups)
	expected.accumulateRzaServerClass(class2Size, class2ServerGroups)
	expected.accumulateRzaServerClass(class3Size, class3ServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Deploy couchbase cluster over multiple server-groups.
// Scale up the couchbase nodes both general scalling and service based scalling.
func TestRzaResizeCluster(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	// Create cluster spec for RZA feature
	clusterSize := 3
	availableServerGroups := getAvailabilityZones(t, kubernetes)

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = availableServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Create a map for server-groups based on deployed cb-server nodes
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	// Starting resize cluster test
	service := 0

	clusterSizes := []int{2, 7, 4}
	for _, clusterSize := range clusterSizes {
		// Resize cluster and wait for healthy cluster
		cluster = e2eutil.MustResizeClusterNoWait(t, service, clusterSize, kubernetes, cluster)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

		// Update deployed server-groups based on new cluster size
		expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
		expected.mustValidateRzaMap(t, kubernetes, cluster)
	}

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleUpSequence(5),
		e2eutil.ClusterScaleDownSequence(3),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create cluster with AA-ON and deploy the çb cluster.
// Add nodes beyond the number of available cluster nodes.
// Expects pod creation beyond k8s cluster size should fail.
func TestRzaAntiAffinityOn(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// This is broken because it doesn't take into account memory allocation.
	framework.Requires(t, kubernetes).StaticCluster().ServerGroups(2).Rethink()

	availableServerGroups := getAvailabilityZones(t, kubernetes)
	// WARNING: this assumes all AZs have the same number of nodes
	clusterSize := e2eutil.MustNumNodes(t, kubernetes)
	class := 0

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.AntiAffinity = true
	cluster.Spec.ServerGroups = availableServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When ready scale up, with AA on it should fail.
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	cluster = e2eutil.MustResizeClusterNoWait(t, class, clusterSize+1, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, clusterSize), 2*f.PodCreateTimeout)
	cluster = e2eutil.MustResizeClusterNoWait(t, class, clusterSize, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*f.PodCreateTimeout)

	// Create a map for server-groups based on deployed cb-server nodes
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Create cluster with AA-OFF and deploy the çb cluster.
// Add nodes beyond the number of available cluster nodes.
// Expects pod creation beyond k8s cluster size should succeed.
func TestRzaAntiAffinityOff(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().ServerGroups(2)

	availableServerGroups := getAvailabilityZones(t, kubernetes)
	// WARNING: this assumes all AZs have the same number of nodes
	clusterSize := e2eutil.MustNumNodes(t, kubernetes)
	class := 0

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = availableServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When ready scale up, with AA on it should fail.
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	cluster = e2eutil.MustResizeClusterNoWait(t, class, clusterSize+1, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Create a map for server-groups based on deployed cb-server nodes
	expected = getExpectedRzaResultMap(cluster.Spec.TotalSize(), availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestServerGroupEnable tests server groups can be enabled on a running cluster.
func TestServerGroupEnable(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	// Dynamic configuration.
	availableServerGroups := getAvailabilityZones(t, kubernetes)
	clusterSize := len(availableServerGroups)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Enable server groups, expecting an upgrade as the pods specification
	// is augmented with scheduling information.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/serverGroups", availableServerGroups), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check things are spread out as we expect.
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestServerGroupDisable tests server groups can be removed from a running cluster...
// the ultimate "get out of jail free" card for support.
func TestServerGroupDisable(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	// Dynamic configuration.
	availableServerGroups := getAvailabilityZones(t, kubernetes)
	clusterSize := len(availableServerGroups)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = availableServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Disable server groups, expecting an upgrade as the pod specification
	// is deprived of scheduling information.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/serverGroups"), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestServerGroupAddGroup starts with one fewer server groups than the cluster can support
// then adds the remaining one.  Possible use cases are rebalancing the cluster over more
// server groups for better fault tolerance.
func TestServerGroupAddGroup(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	// Dynamic configuration.
	availableServerGroups := getAvailabilityZones(t, kubernetes)
	initialServerGroups := availableServerGroups[:len(availableServerGroups)-1]
	clusterSize := len(availableServerGroups)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = initialServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Replace the initial server group set with the total available to scale
	// this parameter up.  The cluster should upgrade as the scheduling constraints
	// are adapted.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/serverGroups", availableServerGroups), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check things are spread out as we expect.
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestServerGroupRemoveGroup removes a server group.  Possible use cases are reducing the
// number of server groups in order to scale down the cluster while maintaining equal balance
// across the server groups.
func TestServerGroupRemoveGroup(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	// Dynamic configuration.
	availableServerGroups := getAvailabilityZones(t, kubernetes)
	finalServerGroups := availableServerGroups[:len(availableServerGroups)-1]
	clusterSize := len(availableServerGroups)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = availableServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Replace the initial server group set with the total available to scale
	// this parameter up.  The cluster should upgrade as the scheduling constraints
	// are adapted.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/serverGroups", finalServerGroups), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check things are spread out as we expect.
	expected := getExpectedRzaResultMap(clusterSize, finalServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestServerGroupReplaceGroup tests migrating a server group from one availability zone
// to another, possibly because the provider needs to perform maintenance or something...
// if it can be done a user will find a way to use it!
func TestServerGroupReplaceGroup(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	// Dynamic configuration.
	availableServerGroups := getAvailabilityZones(t, kubernetes)
	initialServerGroups := availableServerGroups[:len(availableServerGroups)-1]
	finalServerGroups := availableServerGroups[1:]
	clusterSize := len(initialServerGroups)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = initialServerGroups
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Replace the initial server group set with the total available to scale
	// this parameter up.  The cluster should upgrade as the scheduling constraints
	// are adapted.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/serverGroups", finalServerGroups), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check things are spread out as we expect.
	expected := getExpectedRzaResultMap(clusterSize, finalServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
