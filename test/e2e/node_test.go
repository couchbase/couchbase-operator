package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"

	"k8s.io/api/core/v1"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

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

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// edit service size
	newSize := 2
	t.Log("Changing cluster size")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Size", newSize), constants.Retries10)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 1), 120)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
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

	// create a client to admin console
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	// failover member
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	m := &couchbaseutil.Member{
		Name:         memberName,
		Namespace:    f.Namespace,
		ServerConfig: testCouchbase.Spec.ServerSettings[0].Name,
		SecureClient: false,
	}
	if err := client.Failover(m.HostURL()); err != nil {
		t.Fatalf("failed to failover host %s: %v", m.HostURL(), err)
	}

	// expect rebalance event to start
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.RebalanceStartedEvent(testCouchbase), 300)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// healthy 2 node cluster
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries30)

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests scenario where a third node is being added to a cluster, and a separate
// node goes down immediately after the add & before the rebalance.
//
// Expects: autofailover of down node occurs and a replacement node is added
// in order to reach desired cluster size
func TestNodeRecoveryAfterMemberAdd(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	clusterSize := constants.Size1
	podToKillMemberId := 1

	// create 1 node cluster
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSize = constants.Size5
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize, targetKube, testCouchbase)

	for memberId := 1; memberId < clusterSize; memberId++ {
		// wait for add member event
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberId), 120)
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)

		if memberId == clusterSize-2 {
			// kill pod 1
			e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podToKillMemberId, true)
		}
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceIncompleteEvent(testCouchbase), 60)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.FailedAddNodeEvent(testCouchbase, podToKillMemberId), 60)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, podToKillMemberId)

	// wait for add member event
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), 150)
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)

	// cluster should also be balanced
	if err := e2eutil.WaitForClusterBalancedCondition(t, targetKube.CRClient, testCouchbase, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 2), 300)

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
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries30)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests node recovery after killing during rebalance and then killing the newly added node
// 1. Create 1 node cluster
// 2. Scale cluster to 3 members
// 3. When rebalance starts kill 3rd member
// 4. Wait for autofailover to add a 4th member
// 5. Kill 4th member when added to cluster
// 6. Wait for resize to reach 3 nodes
// 7. Make sure cluster is healthy
//
// NOTE:
// There are potential race conditions here i.e. not our fault
// 1. When killing the first node, instead of failedAdd the node can be flagged as down
//    but NS server will prevent a failover as it thinks two nodes are down.  See comments
//    for K8S-497.
// 2. A rebalance can complete, but NS server reports that it needs a rebalance, this
//    will be cleared by the time we complete the next reconcile.
func TestKillNodesAfterRebalanceAndFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	clusterSize := constants.Size1

	// create 1 node cluster
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	// resize to 3 member cluster
	clusterSize = constants.Size3
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize, targetKube, testCouchbase)

	// detect 3rd member add event
	newPodMemberId := clusterSize - 1
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, newPodMemberId), 300)

	for nodeIndex := 1; nodeIndex < clusterSize; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}

	// wait rebalance event
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 60)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	// kill 3rd member being rebalanced in
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, newPodMemberId, true)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, newPodMemberId)

	// waiting for 3rd member to be killed
	newPodMemberId = clusterSize
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, newPodMemberId), 180)
	expectedEvents.AddMemberAddEvent(testCouchbase, newPodMemberId)

	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, newPodMemberId, true)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, newPodMemberId)

	// 1/3 times we'll probably hit the dead node before the service is revoked so retry
	err := retryutil.RetryOnErr(e2eutil.Context, time.Second, 5, "reset failover", testCouchbase.Name, func() error {
		return client.ResetFailoverCounter()
	})
	if err != nil {
		t.Fatal(err)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize+1), 120)
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize+1)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries30)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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

	// create a client to admin console
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	err, username, password := e2eutil.GetClusterAuth(t, targetKube.KubeClient, f.Namespace, targetKube.DefaultSecret.Name)
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
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), constants.Retries5)

	if _, err := e2eutil.CreateMemberPod(targetKube.KubeClient, m, testCouchbase, testCouchbase.Name, f.Namespace); err != nil {
		t.Fatal(err)
	}
	defer e2eutil.KillMember(targetKube.KubeClient, f.Namespace, testCouchbase.Name, foreignNodeName, true)

	if err := e2eutil.AddNode(t, client, serverConfig.Services, username, password, m.ClientURLPlaintext()); err != nil {
		t.Fatal(err)
	}

	// Balanced cluster with foreign node
	ctx := e2eutil.Context
	err = retryutil.RetryOnErr(ctx, 5*time.Second, constants.Retries30, "rebalance", testCouchbase.GetName(),
		func() error {
			err := client.Rebalance([]string{""})
			if err != nil {
				e2eutil.Die(t, err)
			}
			progress := client.NewRebalanceProgress()

		RebalanceWaitLoop:
			for {
				select {
				case _, ok := <-progress.Status():
					// Channel closed, rebalance complete.
					if !ok {
						break RebalanceWaitLoop
					}
				case err := <-progress.Error():
					return err
				}
			}

			return nil
		})
	if err != nil {
		t.Fatal("Rebalance failed")
	}

	// Resuming operator
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), constants.Retries5)

	// resize to 2 member cluster
	testCouchbase, err = e2eutil.ResizeCluster(t, 0, constants.Size2, targetKube, testCouchbase, constants.Retries30)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	// check that actual cluster size is only 2 nodes
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddEvent(*k8sutil.MemberRemoveEvent(foreignNodeName, testCouchbase))
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// None of the nodes should be the foreign member and
	// all should be healthy
	info, err := client.ClusterInfo()
	if err != nil {
		t.Fatalf("unable to poll cluster info")
	}
	for _, node := range info.Nodes {
		if node.Status != "healthy" {
			t.Fatalf("node %s is not healthy, status: %s", node.HostName, node.Status)
		}
	}
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
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, 0), 300)
	expectedEvents.AddMemberDownEvent(testCouchbase, 0)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, 0)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 5), 120)
	expectedEvents.AddMemberAddEvent(testCouchbase, 5)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
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
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	t.Logf("killing 2 pods...")
	memberIdsToKill := []int{0, 1}
	for _, memberId := range memberIdsToKill {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, memberId, true)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberId), 60)
		expectedEvents.AddMemberDownEvent(testCouchbase, memberId)
	}

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	// Manually failover nodes
	e2eutil.FailoverNodes(t, client, clusterSize, memberIdsToKill)
	for _, memberId := range memberIdsToKill {
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberId)
	}

	for memberId := clusterSize; memberId < clusterSize+len(memberIdsToKill); memberId++ {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberId), 120)
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)
	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, clusterSize); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

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
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

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
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberIdToKill), 60)

	expectedEvents.AddMemberDownEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

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

	service, err := e2eutil.CreateService(targetKube.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteService(targetKube.KubeClient, f.Namespace, service.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	t.Logf("killing 2 pods...")
	memberIdsToKill := []int{0, 1}
	memberIdsToKillLen := len(memberIdsToKill)
	for _, memberId := range memberIdsToKill {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, memberId, true)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberId), 60)
		expectedEvents.AddMemberDownEvent(testCouchbase, memberId)
	}

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	if err := e2eutil.WaitForUnhealthyNodes(t, client, constants.Retries5, memberIdsToKillLen); err != nil {
		t.Fatal(err)
	}

	// Manually failover nodes
	for _, memberId := range memberIdsToKill {
		memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, memberId)
		m := &couchbaseutil.Member{
			Name:         memberName,
			Namespace:    f.Namespace,
			ServerConfig: testCouchbase.Spec.ServerSettings[0].Name,
			SecureClient: false,
		}
		if err := client.Failover(m.HostURL()); err != nil {
			t.Fatalf("failed to failover host %s: %v", m.HostURL(), err)
		}
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberId)
	}

	for memberId := clusterSize; memberId < clusterSize+memberIdsToKillLen; memberId++ {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberId), 120)
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	for _, memberId := range memberIdsToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries5); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
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
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, podMemberIdToKill), 60)
	expectedEvents.AddMemberDownEvent(testCouchbase, podMemberIdToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberFailedOverEvent(testCouchbase, podMemberIdToKill), 60)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podMemberIdToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), 180)
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 120)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 300)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
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

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", "default")

	service, err := e2eutil.CreateService(targetKube.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteService(targetKube.KubeClient, f.Namespace, service.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	// For WaitForClusterEventsInParallel API
	memberDownEvents := e2eutil.EventList{}
	memberFailedOverEvents := e2eutil.EventList{}
	memberRemovedEvents := e2eutil.EventList{}

	// For validation purpose
	memDownEventValidator := e2eutil.EventValidator{}
	memFailoverEventValidator := e2eutil.EventValidator{}
	memRemovedEventValidator := e2eutil.EventValidator{}

	for _, podMemberToKill := range podMembersToKill {
		memberDownEvents = append(memberDownEvents, *e2eutil.NewMemberDownEvent(testCouchbase, podMemberToKill))
		memberFailedOverEvents = append(memberFailedOverEvents, *e2eutil.NewMemberFailedOverEvent(testCouchbase, podMemberToKill))
		memberRemovedEvents = append(memberRemovedEvents, *e2eutil.NewMemberRemoveEvent(testCouchbase, podMemberToKill))
	}
	memDownEventValidator.AddClusterPodEvent(testCouchbase, "MemberDown", podMembersToKill...)
	memFailoverEventValidator.AddClusterPodEvent(testCouchbase, "FailedOver", podMembersToKill...)
	memRemovedEventValidator.AddClusterPodEvent(testCouchbase, "MemberRemoved", podMembersToKill...)

	t.Logf("killing 2 pods...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 2)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}

	if _, err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, memberDownEvents, 30); err != nil {
		t.Fatal(err)
	}

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	// Manually failover nodes
	e2eutil.FailoverNodes(t, client, clusterSize, podMembersToKill)

	if _, err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, memberFailedOverEvents, 60); err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddParallelEvents(memDownEventValidator)
	expectedEvents.AddParallelEvents(memFailoverEventValidator)

	// event capture for new pod creation
	for memberId := clusterSize; memberId < clusterSize+len(podMembersToKill); memberId++ {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberId), 120)
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberId)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries120)

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries5); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddParallelEvents(memRemovedEventValidator)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	ValidateEvents(t, targetKube, f.Namespace, testCouchbase.Name, expectedEvents)
}

func TestRecoveryAfterOneNsServerFailureBucketOneReplica(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1, "bucket1": bucketConfig1}
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for nodeIndex := 0; nodeIndex < clusterSize; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	memberToKill := 0
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, memberToKill)

	if f.KubeType == "kubernetes" {
		if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "mv /etc/service/couchbase-server /tmp/"); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, memberName, f.Namespace); err != nil {
			t.Fatal(err)
		}
	}

	autofailoverTimeout, err := strconv.Atoi(e2eutil.BasicClusterConfig["autoFailoverTimeout"])
	if err != nil {
		t.Fatal(err)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, memberToKill), 30)
	expectedEvents.AddMemberDownEvent(testCouchbase, memberToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberFailedOverEvent(testCouchbase, memberToKill), autofailoverTimeout+30)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), 120)
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 60)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 300)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries5)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries1); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT; iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT")

	autofailoverTimeout, err := strconv.Atoi(e2eutil.BasicClusterConfig["autoFailoverTimeout"])
	time.Sleep(time.Duration(autofailoverTimeout) * time.Second)

	t.Logf("waiting for pods to die...")
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, 4, constants.Retries30, testCouchbase); err != nil {
		t.Logf("status: %v+", testCouchbase)
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	t.Logf("waiting for unhealthy nodes from cluster...")
	if err := e2eutil.WaitForUnhealthyNodes(t, client, constants.Retries5, 1); err != nil {
		t.Fatalf("failed to wait for 1 unhealthy node: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}

	t.Logf("waiting for cluster size to be 5")
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, 5, constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
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
	if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT"); err != nil {
		t.Fatal(err)
	}

	// wait half of autofailover timeout
	time.Sleep(time.Duration(int64(autofailoverTimeout/2)) * time.Second)

	//revert iptable changes, allow all incoming and outgoing traffic
	if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -F"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -X"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -P INPUT DROP"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -P OUTPUT DROP"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "iptables -P FORWARD DROP"); err != nil {
		t.Fatal(err)
	}

	t.Logf("waiting for pods to die...")
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, 4, constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	t.Logf("waiting for unhealthy nodes from cluster...")
	if err := e2eutil.WaitForUnhealthyNodes(t, client, constants.Retries5, 1); err != nil {
		t.Fatalf("failed to wait for 1 unhealthy node: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}

	t.Logf("waiting for cluster size to be 5")
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, 5, constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
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
	defer e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex)

	expectedEvents.AddMemberDownEvent(testCouchbase, memberIdToGoDown)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToGoDown)

	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	if err := e2eutil.WaitForUnhealthyNodes(t, client, 5, 1); err != nil {
		t.Fatalf("No unhealthy nodes in cluster: %v", err)
	}

	if err := e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex); err != nil {
		t.Fatalf("Failed to unset node taint and schedulable property: %v", err)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, 3), 300)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberRemoveEvent(testCouchbase, memberIdToGoDown), 300)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToGoDown)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries30)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}
