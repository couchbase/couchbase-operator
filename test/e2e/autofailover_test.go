package e2e

import (
	"reflect"
	"sort"
	"testing"
	"time"

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
	f := framework.Global
	targetKube := f.GetCluster(0)

	availableServerGroupList := getAvailabilityZones(t, targetKube)
	if len(availableServerGroupList) < 3 {
		t.Skip("couchbase server requires 3 or more availability zones")
	}

	// Create cluster spec for RZA feature
	clusterSize := e2eutil.MustNumNodes(t, targetKube)

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize, constants.AdminHidden)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 10
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	testCouchbase.Spec.ClusterSettings.AutoFailoverServerGroup = true
	testCouchbase.Spec.ServerGroups = availableServerGroupList
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	sort.Strings(availableServerGroupList)

	// Create a expected RZA results map for verification
	expectedRzaResultMap := getExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := getDeployedRzaMap(targetKube.KubeClient, f.Namespace)
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
	deployedRzaGroupsMap, err = getDeployedRzaMap(targetKube.KubeClient, f.Namespace)
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

// Create 9 node couchbase cluster
// Failover multiple nodes at once and wait for multinode failover to trigger
// NOTE: this is a shonky racy test, please make me better.
func TestMultiNodeAutoFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 9
	victims := []int{2, 3, 4}
	victimCount := len(victims)

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketThreeReplicas)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize, constants.AdminHidden)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 30
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.ClusterSettings.AutoFailoverServerGroup = true
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

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
