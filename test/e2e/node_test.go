package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"

	"k8s.io/api/core/v1"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestDenyCommunityEdition tries installing with a CE version of Couchbase server
// and expects it not to work.
func TestDenyCommunityEdition(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Don't run this on Openshift etc. as there is no community edition.
	skipEnterpriseOnlyPlatform(t)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize, constants.WithoutBucket, constants.AdminHidden)
	testCouchbase.Spec.Version = constants.CommunityEditionVersion
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, f.Namespace, testCouchbase)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, 5*time.Minute)
}

// Tests editing service spec
// 1. Create 1 node cluster with single service spec
// 2. Update service spec size from 1 to 2 (verify via rest call to cluster)
func TestEditServiceConfig(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// edit service size
	newSize := 2
	t.Log("Changing cluster size")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Size", newSize), time.Minute)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 1), 2*time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests manual failover and operator recovery of cluster
// 1. Create 2 node cluster
// 2. Manually failover 1 member
// 3. Wait for operator to rebalance out failed node
// 4. Expect operator to replace failed node with new node
func TestNodeManualFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	// create 2 node cluster with admin console
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// failover member
	e2eutil.MustFailoverNode(t, targetKube, testCouchbase, 0, time.Minute)

	// expect rebalance event to start
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// healthy 2 node cluster
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// TestNodeRecoveryAfterMemberAdd tests killing a node during a scale up.
func TestNodeRecoveryAfterMemberAdd(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1
	scaleSize := constants.Size5
	triggerIndex := 3
	victimIndex := 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When the cluster is ready begin scaling up.  When the third new member is added
	// kill the victim node.  Expect the cluster to become healthy again.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, scaleSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, triggerIndex), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * New nodes added
	// * Rebalance starts and fails
	// * Victim failed add
	// * New node added and rebalanced in
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: scaleSize - clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests scenario where the node being added to is killed before it can be
// rebalanced in.
//
// Expects: autofailover of down node occurs and a replacement node is added
// in order to reach desired cluster size
func TestNodeRecoveryKilledNewMember(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	podToKillMemberId := 2

	// create 1 node cluster
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminHidden)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// async scale up to 3 node cluster
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, constants.Size3, targetKube, testCouchbase)

	// wait for add member event
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 2), 5*time.Minute)

	for nodeIndex := 1; nodeIndex < constants.Size3; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)

	// kill pod that was just added
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podToKillMemberId, true)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	// cluster should also be balanced
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// TestKillNodesAfterRebalanceAndFailover tests repeated pod termination at
// different points in a scale up operation.
func TestKillNodesAfterRebalanceAndFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1
	scaledClusterSize := constants.Size3
	victim1Index := scaledClusterSize - 1
	victim2Index := scaledClusterSize

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden)

	// Runtime configuration.
	victim1Name := couchbaseutil.CreateMemberName(testCouchbase.Name, victim1Index)
	victim2Name := couchbaseutil.CreateMemberName(testCouchbase.Name, victim2Index)

	// When the cluster is healthy, resize to the target size.  When the first victim starts balancing in
	// kill it.  When the second victim is created to replace the dead node, kill it too.  Cluster should
	// end up healthy.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, scaledClusterSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, targetKube, testCouchbase, 25.0, 2*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim1Index, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, victim2Index), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim2Index, true)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Scale up begins
	// * Rebalance fails
	// * First victim node goes down and fails over
	// * New node is created to replace the first victim
	// * Rebalance fails
	// * Second victim fails to add
	// * Another new node is created and balanced in and the first victim is removed.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: scaledClusterSize - clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		// The operator may miss seeing this due to network timeouts
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victim1Name}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victim1Name},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victim2Name},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victim2Name},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victim1Name},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Test that a foreign node is removed from cluster
//
// Expects: only nodes added by operator to be in cluster
// 1. Create 1 node cluster
// 2. Manually add 1 external member to cluster
// 3. Request cluster resize to 2 members
// 4. Verify that actual cluster size is 2 nodes
// 5. Verify that external member was removed
// 6. Verify that the 2 cluster nodes are healthy
func TestRemoveForeignNode(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	// 1. create 1 node cluster with admin console
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	username, password, err := e2eutil.GetClusterAuth(targetKube.KubeClient, f.Namespace, targetKube.DefaultSecret.Name)
	if err != nil {
		t.Fatal(err)
	}

	// 2. create a foreign member to be added to the cluster
	foreignNodeName := testCouchbase.Name + "-foreignnode"
	serverConfig := testCouchbase.Spec.ServerSettings[0]
	m := &couchbaseutil.Member{
		Name:         foreignNodeName,
		Namespace:    f.Namespace,
		ServerConfig: serverConfig.Name,
		SecureClient: false,
	}

	// Pause operator to avoid foreign pod deletion as soon as added
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	e2eutil.MustAddNode(t, targetKube, testCouchbase, testCouchbase.Spec.ServerSettings[0].Services, username, password, m)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)

	// resize to 2 member cluster
	testCouchbase = e2eutil.MustResizeCluster(t, 0, constants.Size2, targetKube, testCouchbase, 5*time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddEvent(*k8sutil.MemberRemoveEvent(foreignNodeName, testCouchbase))
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests one node failing in a cluster with no buckets
// 1. Create a 5 node cluster with no buckets
// 2. Kill a single node
// 3. Wait for autofailover, rebalance, and healthy
func TestRecoveryAfterOnePodFailureNoBucket(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(5, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, 0), 5*time.Minute)
	expectedEvents.AddMemberDownEvent(testCouchbase, 0)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, 0)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 5), 2*time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, 5)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests two nodes failing in a cluster with no buckets
// 1. Create 5 node cluster with no buckets
// 2. Kill two nodes
// 3. Manually failover the killed nodes
// 4. Wait for rebalance and healthy cluster
func TestRecoveryAfterTwoPodFailureNoBucket(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	t.Logf("killing 2 pods...")
	memberIdsToKill := []int{0, 1}
	for _, memberId := range memberIdsToKill {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, memberId, true)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberId), time.Minute)
		expectedEvents.AddMemberDownEvent(testCouchbase, memberId)
	}

	// Manually failover nodes
	for _, memberId := range memberIdsToKill {
		e2eutil.MustFailoverNode(t, targetKube, testCouchbase, memberId, time.Minute)
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberId)
	}

	for memberId := clusterSize; memberId < clusterSize+len(memberIdsToKill); memberId++ {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberId), 2*time.Minute)
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	for _, memberId := range memberIdsToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests one nodes failing in a cluster with one bucket with one replica
// 1. Create 5 node cluster with one bucket with 1 replica
// 2. Kill one node
// 3. Wait for rebalance and healthy cluster
func TestRecoveryAfterOnePodFailureBucketOneReplica(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	memberIdToKill := 0
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(5, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < constants.Size5; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	t.Logf("killing 1 pod...")
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, memberIdToKill, true)
	/*
		e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)
		if err != nil {
			t.Fatalf("failed to kill pods: %v", err)
		}
	*/

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberIdToKill), time.Minute)

	expectedEvents.AddMemberDownEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)

	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Tests two nodes failing in a cluster with one bucket with one replica
// 1. Create 5 node cluster with one bucket with 1 replica
// 2. Kill two nodes
// 3. Manually failover the two killed nodes
// 4. Wait for rebalance and healthy cluster
func TestRecoveryAfterTwoPodFailureBucketOneReplica(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for nodeIndex := 0; nodeIndex < clusterSize; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	t.Logf("killing 2 pods...")
	memberIdsToKill := []int{0, 1}
	memberIdsToKillLen := len(memberIdsToKill)
	for _, memberId := range memberIdsToKill {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, memberId, true)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberId), time.Minute)
		expectedEvents.AddMemberDownEvent(testCouchbase, memberId)
	}

	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, memberIdsToKillLen, time.Minute)

	// Manually failover nodes
	for _, memberId := range memberIdsToKill {
		e2eutil.MustFailoverNode(t, targetKube, testCouchbase, memberId, time.Minute)
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberId)
	}

	for memberId := clusterSize; memberId < clusterSize+memberIdsToKillLen; memberId++ {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberId), 2*time.Minute)
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	for _, memberId := range memberIdsToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests one node failing in a cluster with one bucket with two replicas
// 1. Create 5 node cluster with one bucket with two replicas
// 2. Kill one node
// 3. Wait for rebalance and healthy cluster
func TestRecoveryAfterOnePodFailureBucketTwoReplica(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	podMemberIdToKill := 0

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicTwoReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, podMemberIdToKill), time.Minute)
	expectedEvents.AddMemberDownEvent(testCouchbase, podMemberIdToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberFailedOverEvent(testCouchbase, podMemberIdToKill), time.Minute)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podMemberIdToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), 3*time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 2*time.Minute)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests two nodes failing in a cluster with one bucket with two replicas
// 1. Create 5 node cluster with one bucket with two replicas
// 2. Kill two nodes
// 3. Manually failover the two killed nodes
// 4. Wait for rebalance and healthy cluster
func TestRecoveryAfterTwoPodFailureBucketTwoReplica(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicTwoReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}
	podMembersToKill := []int{0, 1}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	for _, podMemberToKill := range podMembersToKill {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, true)
	}
	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, len(podMembersToKill), time.Minute)
	for _, podMemberToKill := range podMembersToKill {
		e2eutil.MustFailoverNode(t, targetKube, testCouchbase, podMemberToKill, time.Minute)
	}
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)
	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)

	// Events:
	// Create 5 node cluster
	// Kill 2 nodes
	// Manually failover the 2 nodes
	// Wait for rebalance.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		eventschema.Repeat{Times: clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestRecoveryAfterOneNsServerFailureBucketOneReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size5
	victimIndex := 0

	// Create the cluster.
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1, "bucket1": bucketConfig1}
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	// Runtime configuration
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When ready kill the Couchbase Server process and await recovery
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustKillCouchbaseService(t, targetKube, f.Namespace, victimName, f.KubeType)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member goes down and fails over
	// * New member balanced in to replace the failed one
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestRecoveryAfterOneNodeUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(5, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT; iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT")

	autofailoverTimeout, _ := strconv.Atoi(e2eutil.BasicClusterConfig["autoFailoverTimeout"])
	time.Sleep(time.Duration(autofailoverTimeout) * time.Second)

	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 4, 5*time.Minute)
	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 5, 5*time.Minute)

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestRecoveryNodeTmpUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	autofailoverTimeout := 30

	clusterConfig := map[string]string{
		"dataServiceMemQuota":   "256",
		"indexServiceMemQuota":  "256",
		"searchServiceMemQuota": "256",
		"indexStorageSetting":   "memory_optimized",
		"autoFailoverTimeout":   strconv.Itoa(autofailoverTimeout)}
	serviceConfig1 := e2eutil.GetServiceConfigMap(5, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	//block all incoming and outgoing traffic expect ssh on port 22
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT")
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT")

	// wait half of autofailover timeout
	time.Sleep(time.Duration(int64(autofailoverTimeout/2)) * time.Second)

	//revert iptable changes, allow all incoming and outgoing traffic
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -F")
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -X")
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -P INPUT DROP")
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -P OUTPUT DROP")
	e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "iptables -P FORWARD DROP")

	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 4, 5*time.Minute)
	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 5, 5*time.Minute)

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	e2eutil.MustVerifyClusterBalancedAndHealthy(t, targetKube, testCouchbase, time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestTaintK8SNodeAndRemoveTaint(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Create cluster spec for RZA feature
	clusterSize := 3
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	// Deploy couchbase cluster
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// Set taint properties
	podTaint := v1.Taint{
		Key:    "noExecKey",
		Value:  "noExecVal",
		Effect: "NoExecute",
	}
	podTaintList := []v1.Taint{podTaint}

	nodeIndex := 2
	memberIdToGoDown := 1
	if err := e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, true, podTaintList, nodeIndex); err != nil {
		t.Fatalf("Failed to set node taint and schedulable property: %v", err)
	}
	defer func() {
		_ = e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex)
	}()

	expectedEvents.AddMemberDownEvent(testCouchbase, memberIdToGoDown)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToGoDown)

	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, 1, time.Minute)

	if err := e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex); err != nil {
		t.Fatalf("Failed to unset node taint and schedulable property: %v", err)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 3), 5*time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberRemoveEvent(testCouchbase, memberIdToGoDown), 5*time.Minute)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToGoDown)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}
