package e2e

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/api/core/v1"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	serviceNum := 0

	// edit service size
	newSize := "2"
	t.Log("Changing cluster size")
	testCouchbase, err = e2eutil.UpdateServiceSpec(serviceNum, "Size", newSize, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 1)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatalf("Failed to add new member %d: %v", 1, err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	if err := e2eutil.VerifyClusterInfo(t, client, constants.Retries5, newSize, e2eutil.NumNodesVerifier); err != nil {
		t.Fatalf("failed to change service size: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests invalid editing of service spec
// 1. Create 1 node cluster
// 2. Attempt to change service size from 1 to -2
// 3. Verify change did not take hold via rest call
// 4. Verify cluster size of 1 via rest call
func TestNegEditServiceConfig(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	serviceNum := 0

	// edit service size
	newSize := "-2"
	oldSize := "1"
	t.Log("Changing cluster size to -2")
	if _, err := e2eutil.UpdateServiceSpec(serviceNum, "Size", newSize, targetKube.CRClient, testCouchbase, constants.Retries5); err == nil {
		t.Fatalf("failed to reject invalid service size: %v", err)
	} else if !strings.Contains(err.Error(), "spec.servers.size in body should be greater than or equal to 1") {
		t.Fatalf("failed to see expected error message: %v \n", err)
	}

	t.Log("Verify resize did not happen")
	if err := e2eutil.VerifyClusterInfo(t, client, constants.Retries5, newSize, e2eutil.NumNodesVerifier); err == nil {
		t.Fatalf("failed to reject invalid service size: %v", err)
	}

	t.Log("Verify cluster size is 1")
	if err := e2eutil.VerifyClusterInfo(t, client, constants.Retries5, oldSize, e2eutil.NumNodesVerifier); err != nil {
		t.Fatalf("failed to reject invalid service size: %v", err)
	}

	t.Log("Verify cluster balanced and healthy through rest api")
	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Log("Changing cluster size back to 1")
	testCouchbase, err = e2eutil.UpdateServiceSpec(serviceNum, "Size", oldSize, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Verify cluster size is 1")
	if err := e2eutil.VerifyClusterInfo(t, client, constants.Retries5, oldSize, e2eutil.NumNodesVerifier); err != nil {
		t.Fatalf("failed to reject invalid service size: %v", err)
	}

	t.Log("Verify cluster balanced and healthy through rest api")
	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// create 2 node cluster with admin console
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size2, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create a client to admin console
	testCouchbase, err = e2eutil.GetClusterCRD(targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

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
	event := k8sutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// healthy 2 node cluster
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	clusterSize := constants.Size1
	podToKillMemberId := 1

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, clusterSize, constants.WithBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSize = constants.Size5
	testCouchbase, err = e2eutil.ResizeClusterNoWait(t, 0, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	for memberId := 1; memberId < clusterSize; memberId++ {
		// wait for add member event
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)

		if memberId == clusterSize-2 {
			// kill pod 1
			if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId); err != nil {
				t.Fatal(err)
			}
		}
	}

	event := e2eutil.RebalanceIncompleteEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)

	event = e2eutil.FailedAddNodeEvent(testCouchbase, podToKillMemberId)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, podToKillMemberId)

	// wait for add member event
	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 150); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)

	// cluster should also be balanced
	if err := e2eutil.WaitForClusterBalancedCondition(t, targetKube.CRClient, testCouchbase, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	podToKillMemberId := 2

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// async scale up to 3 node cluster
	testCouchbase, err = e2eutil.ResizeClusterNoWait(t, 0, constants.Size3, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	// wait for add member event
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}

	for nodeIndex := 1; nodeIndex < constants.Size3; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)

	// kill pod that was just added
	if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, podToKillMemberId); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, podToKillMemberId)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	// cluster should also be balanced
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	clusterSize := constants.Size1

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, clusterSize, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("Unable to get Client for cluster: %v", err)
	}

	// resize to 3 member cluster
	clusterSize = constants.Size3
	testCouchbase, err = e2eutil.ResizeClusterNoWait(t, 0, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	// detect 3rd member add event
	newPodMemberId := clusterSize - 1
	event := e2eutil.NewMemberAddEvent(testCouchbase, newPodMemberId)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}

	for nodeIndex := 1; nodeIndex < clusterSize; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}

	// wait rebalance event
	event = e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	// kill 3rd member being rebalanced in
	if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, newPodMemberId); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, newPodMemberId)

	// waiting for 3rd member to be killed
	newPodMemberId = clusterSize
	event = e2eutil.NewMemberAddEvent(testCouchbase, newPodMemberId)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 180); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, newPodMemberId)

	if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, newPodMemberId); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, newPodMemberId)

	// 1/3 times we'll probably hit the dead node before the service is revoked so retry
	err = retryutil.RetryOnErr(e2eutil.Context, time.Second, 5, "reset failover", testCouchbase.Name, func() error {
		return client.ResetFailoverCounter()
	})
	if err != nil {
		t.Fatal(err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize+1)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize+1)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30); err != nil {
		t.Fatalf("Cluster failed to become healthy and balanced: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// 1. create 1 node cluster with admin console
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create a client to admin console
	testCouchbase, err = e2eutil.GetClusterCRD(targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

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
	testCouchbase, err = e2eutil.UpdateClusterSpec("Paused", "true", targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateMemberPod(targetKube.KubeClient, m, testCouchbase, testCouchbase.Name, f.Namespace); err != nil {
		t.Fatal(err)
	}
	defer e2eutil.KillMember(targetKube.KubeClient, f.Namespace, testCouchbase.Name, foreignNodeName)

	if err := e2eutil.AddNode(t, client, serverConfig.Services, username, password, m.ClientURLPlaintext()); err != nil {
		t.Fatal(err)
	}

	// Balanced cluster with foreign node
	ctx := e2eutil.Context
	err = retryutil.RetryOnErr(ctx, 5*time.Second, constants.Retries30, "rebalance", testCouchbase.GetName(),
		func() error {
			status, err := client.Rebalance([]string{""})
			if true && status != nil {
				return status.Wait()
			}
			return err
		})
	if err != nil {
		t.Fatal("Rebalance failed")
	}

	// Resuming operator
	testCouchbase, err = e2eutil.UpdateClusterSpec("Paused", "false", targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	// resize to 2 member cluster
	testCouchbase, err = e2eutil.ResizeCluster(t, 0, constants.Size2, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	// check that actual cluster size is only 2 nodes
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy")
	}

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
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(5, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	downEvent := e2eutil.NewMemberDownEvent(testCouchbase, 0)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, downEvent, 300); err != nil {
		t.Fatalf("failed to wait for down node: %v", err)
	}
	expectedEvents.AddMemberDownEvent(testCouchbase, 0)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, 0)

	event := e2eutil.NewMemberAddEvent(testCouchbase, 5)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 5)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 2 pods...")
	memberIdsToKill := []int{0, 1}
	for _, memberId := range memberIdsToKill {
		if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, memberId); err != nil {
			t.Fatal(err)
		}

		event := e2eutil.NewMemberDownEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, memberId)
	}

	// Manually failover nodes
	e2eutil.FailoverNodes(t, client, clusterSize, memberIdsToKill)
	for _, memberId := range memberIdsToKill {
		expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberId)
	}

	for memberId := clusterSize; memberId < clusterSize+len(memberIdsToKill); memberId++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, clusterSize); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	for _, memberId := range memberIdsToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	memberIdToKill := 0
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(5, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < constants.Size5; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 1 pod...")
	if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, memberIdToKill); err != nil {
		t.Fatal(err)
	}
	/*
		e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)
		if err != nil {
			t.Fatalf("failed to kill pods: %v", err)
		}
	*/

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	downEvent := e2eutil.NewMemberDownEvent(testCouchbase, memberIdToKill)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, downEvent, 60); err != nil {
		t.Fatalf("failed to wait for down node: %v", err)
	}

	expectedEvents.AddMemberDownEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

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

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 2 pods...")
	memberIdsToKill := []int{0, 1}
	memberIdsToKillLen := len(memberIdsToKill)
	for _, memberId := range memberIdsToKill {
		if err := e2eutil.KillPodForMember(targetKube.KubeClient, testCouchbase, memberId); err != nil {
			t.Fatal(err)
		}

		event := e2eutil.NewMemberDownEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberDownEvent(testCouchbase, memberId)
	}

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
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	for _, memberId := range memberIdsToKill {
		expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries5); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberId := 0; memberId < clusterSize; memberId++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)

	// Wait for the nodes to be reported as down before failing over, so we deterministically
	// see the down events
	event := e2eutil.NewMemberDownEvent(testCouchbase, podMemberIdToKill)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberDownEvent(testCouchbase, podMemberIdToKill)

	event = e2eutil.NewMemberFailedOverEvent(testCouchbase, podMemberIdToKill)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podMemberIdToKill)

	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 180); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)

	event = e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

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

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
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

	service, err := e2eutil.CreateService(targetKube.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteService(targetKube.KubeClient, f.Namespace, service.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

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

	// Manually failover nodes
	e2eutil.FailoverNodes(t, client, clusterSize, podMembersToKill)

	if _, err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, memberFailedOverEvents, 60); err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddParallelEvents(memDownEventValidator)
	expectedEvents.AddParallelEvents(memFailoverEventValidator)

	// event capture for new pod creation
	for memberId := clusterSize; memberId < clusterSize+len(podMembersToKill); memberId++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberId)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries120); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries5); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddParallelEvents(memRemovedEventValidator)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

func TestRecoveryAfterOneNsServerFailureBucketOneReplica(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterSize := constants.Size5
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1, "bucket1": bucketConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for nodeIndex := 0; nodeIndex < clusterSize; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	memberToKill := 0
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, memberToKill)

	if f.KubeType == "kubernetes" {
		if _, err := f.ExecShellInPod(kubeName, memberName, "mv /etc/service/couchbase-server /tmp/"); err != nil {
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

	event := e2eutil.NewMemberDownEvent(testCouchbase, memberToKill)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberDownEvent(testCouchbase, memberToKill)

	event = e2eutil.NewMemberFailedOverEvent(testCouchbase, memberToKill)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, autofailoverTimeout+30); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberToKill)

	event = e2eutil.NewMemberAddEvent(testCouchbase, clusterSize)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)

	event = e2eutil.RebalanceStartedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	event = e2eutil.RebalanceCompletedEvent(testCouchbase)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries5); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries1); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestRecoveryAfterOneNodeUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(5, "test_config_1", []string{"data", "query", "index"})
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

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

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	f.ExecShellInPod(kubeName, memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT; iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT")

	autofailoverTimeout, err := strconv.Atoi(e2eutil.BasicClusterConfig["autoFailoverTimeout"])
	time.Sleep(time.Duration(autofailoverTimeout) * time.Second)

	t.Logf("waiting for pods to die...")
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, 4, constants.Retries30, testCouchbase); err != nil {
		t.Logf("status: %v+", testCouchbase)
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

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

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestRecoveryNodeTmpUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, "basic-test-secret", configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

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

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	//block all incoming and outgoing traffic expect ssh on port 22
	if _, err := f.ExecShellInPod(kubeName, memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(kubeName, memberName, "iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT"); err != nil {
		t.Fatal(err)
	}

	// wait half of autofailover timeout
	time.Sleep(time.Duration(int64(autofailoverTimeout/2)) * time.Second)

	//revert iptable changes, allow all incoming and outgoing traffic
	if _, err := f.ExecShellInPod(kubeName, memberName, "iptables -F"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(kubeName, memberName, "iptables -X"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(kubeName, memberName, "iptables -P INPUT DROP"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(kubeName, memberName, "iptables -P OUTPUT DROP"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.ExecShellInPod(kubeName, memberName, "iptables -P FORWARD DROP"); err != nil {
		t.Fatal(err)
	}

	t.Logf("waiting for pods to die...")
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, 4, constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

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

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	if err := e2eutil.VerifyClusterBalancedAndHealthy(t, client, constants.Retries10); err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestTaintK8SNodeAndRemoveTaint(t *testing.T) {
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

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
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

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
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("Unable to get Client for cluster: %v", err)
	}

	if err := e2eutil.WaitForUnhealthyNodes(t, client, 5, 1); err != nil {
		t.Fatalf("No unhealthy nodes in cluster: %v", err)
	}

	if err := e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex); err != nil {
		t.Fatalf("Failed to unset node taint and schedulable property: %v", err)
	}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 3)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Failed to remove pod from tainted node: %v", err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, memberIdToGoDown)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatalf("Failed to remove failed pod: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToGoDown)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30); err != nil {
		t.Fatalf("Cluster failed to become healthy: %v", err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}
