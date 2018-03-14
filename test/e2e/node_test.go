package e2e

import (
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
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

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataNode
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	serviceNum := 0

	// edit service size
	newSize := "2"
	t.Log("Changing cluster size")
	testCouchbase, err = e2eutil.UpdateServiceSpec(serviceNum, "Size", newSize, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, newSize, e2eutil.NumNodesVerifier)
	if err != nil {
		t.Fatalf("failed to change service size: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size2, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
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

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataN1qlIndex
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	serviceNum := 0

	// edit service size
	newSize := "-2"
	oldSize := "1"
	t.Log("Changing cluster size to -2")
	testCouchbase, err = e2eutil.UpdateServiceSpec(serviceNum, "Size", newSize, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Verify resize did not happen")
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, newSize, e2eutil.NumNodesVerifier)
	if err == nil {
		t.Fatalf("failed to reject invalid service size: %v", err)
	}

	t.Log("Verify cluster size is 1")
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, oldSize, e2eutil.NumNodesVerifier)
	if err != nil {
		t.Fatalf("failed to reject invalid service size: %v", err)
	}

	t.Log("Verify cluster balanced and healthy through rest api")
	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Log("Changing cluster size back to 1")
	testCouchbase, err = e2eutil.UpdateServiceSpec(serviceNum, "Size", oldSize, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Verify cluster size is 1")
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, oldSize, e2eutil.NumNodesVerifier)
	if err != nil {
		t.Fatalf("failed to reject invalid service size: %v", err)
	}

	t.Log("Verify cluster balanced and healthy through rest api")
	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}

	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
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

	// create 2 node cluster with admin console
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, 2, true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create a client to admin console
	testCouchbase, err = e2eutil.GetClusterCRD(f.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
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
	err = client.Failover(m.HostURL())
	if err != nil {
		t.Fatalf("failed to failover host %s: %v", m.HostURL(), err)
	}

	// expect rebalance event to start
	event := k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddRebalanceEvent(testCouchbase)

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(t, f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}

	// expect member add for node being replaced
	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 2)

	// expect operator to rebalance in the node
	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddRebalanceEvent(testCouchbase)

	// healthy 2 node cluster
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, 2, 18)
	if err != nil {
		t.Fatal(err.Error())
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size2, e2eutil.Retries30)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
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

	// create 2 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// async scale up to 3 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 0, 3, f.CRClient, testCouchbase)
	}()

	// wait for add member event
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	// kill pod 1
	err = e2eutil.KillPodForMember(f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatal(err)
	}

	// check response from resize request
	err = <-echan
	if err != nil {
		t.Fatal(err)
	}

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(t, f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
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

	// create 2 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// async scale up to 3 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 0, 3, f.CRClient, testCouchbase)
	}()

	// wait for add member event
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	// kill pod that was just added
	err = e2eutil.KillPodForMember(f.KubeClient, testCouchbase, 2)
	if err != nil {
		t.Fatal(err)
	}

	// check response from resize request
	err = <-echan
	if err != nil {
		t.Fatal(err)
	}

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(t, f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
}

// Tests node recovery after killing during rebalance and then killing the newly added node
// 1. Create 1 node cluster
// 2. Scale cluster to 3 members
// 3. When rebalance starts kill 3rd member
// 4. Wait for autofailover to add a 4th member
// 5. Kill 4th member when added to cluster
// 6. Wait for resize to reach 3 nodes
// 7. Make sure cluster is healthy
func TestKillNodesAfterRebalanceAndFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// async kill a pod while cluster is scaling to 3rd member
	doneCh := make(chan bool)
	go func() {

		// detect 3rd member add event
		event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
		err := e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
		if err != nil {
			t.Fatal(err)
		}

		// wait rebalance event
		event = k8sutil.RebalanceEvent(testCouchbase)
		err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
		if err != nil {
			t.Fatal(err)
		}

		// kill 3rd member being rebalanced in
		err = e2eutil.KillPodForMember(f.KubeClient, testCouchbase, 2)
		if err != nil {
			t.Fatal(err)
		}
		doneCh <- true
	}()

	// async watch for 4th member event
	go func() {

		// waiting for 3rd membr to be killed
		<-doneCh

		event := e2eutil.NewMemberAddEvent(testCouchbase, 3)
		err := e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.KillPodForMember(f.KubeClient, testCouchbase, 3)
		if err != nil {
			t.Fatal(err)
		}
	}()

	// resize to 3 member cluster
	err = e2eutil.ResizeCluster(t, 0, 3, f.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(t, f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
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

	// create 1 node cluster with admin console
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true, true)
	if err != nil {
		t.Fatal(err)
	}

	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// create a client to admin console
	testCouchbase, err = e2eutil.GetClusterCRD(f.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}
	err, username, password := e2eutil.GetClusterAuth(t, f.KubeClient, f.Namespace, f.DefaultSecret.Name)
	if err != nil {
		t.Fatal(err)
	}

	// create a foreign member to be added to the cluster
	serverConfig := testCouchbase.Spec.ServerSettings[0]
	m := &couchbaseutil.Member{
		Name:         "test-member",
		Namespace:    f.Namespace,
		ServerConfig: serverConfig.Name,
		SecureClient: false,
	}
	pod, err := e2eutil.CreateMemberPod(f.KubeClient, m, testCouchbase, "unknown-cluster", f.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.KillMember(f.KubeClient, f.Namespace, "test-member")

	externalPodIP := pod.Status.PodIP + ":8091"
	err = e2eutil.AddNode(t, client, serverConfig.Services, username, password, externalPodIP)
	if err != nil {
		t.Fatal(err)
	}

	// resize to 2 member cluster
	err = e2eutil.ResizeCluster(t, 0, 2, f.CRClient, testCouchbase)
	// check that actual cluster size is only 2 nodes
	info, err := client.ClusterInfo()
	numNodes := len(info.Nodes)
	if numNodes != 2 {
		t.Fatalf("expected 2 nodes, found: %d", numNodes)
	}
	// None of the nodes should be the foreign member and
	// all should be healthy
	for _, node := range info.Nodes {
		if node.HostName == externalPodIP {
			t.Fatalf("node %s should not be in cluster", node.HostName)
		}
		if node.Status != "healthy" {
			t.Fatalf("node %s is not healthy, status: %s", node.HostName, node.Status)
		}
	}
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

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	e2eutil.KillPodsAndWaitForRecovery(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pod and recover: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
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

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 2 pods...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 2)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 3: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 2)
	if err != nil {
		t.Fatalf("failed to wait for 2 unhealthy nodes: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}
	nodesToFailover := []string{}
	for _, node := range clusterNodes {
		t.Logf("node status: %v", node.Status)
		if node.Status == "unhealthy" {
			nodesToFailover = append(nodesToFailover, node.HostName)
		}
	}

	t.Logf("failing over nodes: %v", nodesToFailover)
	for _, nodeName := range nodesToFailover {
		err = e2eutil.FailoverNode(t, client, e2eutil.Retries5, nodeName)
		if err != nil {
			t.Fatalf("failed to failover node: %v with error: %v", nodeName, err)
		}
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddMemberAddEvent(testCouchbase, 6)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
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
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 4, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 1)
	if err != nil {
		t.Fatalf("failed to wait for 1 unhealthy node: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
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
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	service, err := e2eutil.CreateService(t, f.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteService(t, f.KubeClient, f.Namespace, service.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()
	// create connection to couchbase nodes
	serviceUrl, err := e2eutil.NodePortServiceClient(f.ApiServerHost(), service)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{serviceUrl})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 2 pods...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 2)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 3: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 2)
	if err != nil {
		t.Fatalf("failed to wait for 2 unhealthy nodes: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 3 nodes in the cluster")
	}

	nodesToFailover := []string{}
	for _, node := range clusterNodes {
		t.Logf("node status: %v", node.Status)
		if node.Status == "unhealthy" {
			nodesToFailover = append(nodesToFailover, node.HostName)
		}
	}

	t.Logf("failing over nodes: %v", nodesToFailover)
	for _, nodeName := range nodesToFailover {
		err = e2eutil.FailoverNode(t, client, e2eutil.Retries10, nodeName)
		if err != nil {
			t.Fatalf("failed to failover node: %v with error: %v", nodeName, err)
		}
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 3: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddMemberAddEvent(testCouchbase, 6)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries120)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
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
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	bucketConfig1 := e2eutil.BasicTwoReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 4, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 1)
	if err != nil {
		t.Fatalf("failed to wait for 1 unhealthy node: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
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
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	bucketConfig1 := e2eutil.BasicTwoReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}

	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	service, err := e2eutil.CreateService(t, f.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteService(t, f.KubeClient, f.Namespace, service.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()
	// create connection to couchbase nodes
	serviceUrl, err := e2eutil.NodePortServiceClient(f.ApiServerHost(), service)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{serviceUrl})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	/*
		// create connection to couchbase nodes
		consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
		if err != nil {
			t.Fatalf("failed to get cluster url %v", err)
		}
		client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
		if err != nil {
			t.Fatalf("failed to create cluster client %v", err)
		}
	*/

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// NewClusterMulti only waits for cluster size to be accurate (e.g. a node could still be pending-add),
	// wait for the cluster to be fully balanced and healthy before killing things
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries20)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	t.Logf("killing 2 pods...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 2)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 3: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 2)
	if err != nil {
		t.Fatalf("failed to wait for 2 unhealthy nodes: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}
	nodesToFailover := []string{}
	for _, node := range clusterNodes {
		t.Logf("node status: %v", node.Status)
		if node.Status == "unhealthy" {
			nodesToFailover = append(nodesToFailover, node.HostName)
		}
	}

	t.Logf("failing over nodes: %v", nodesToFailover)
	for _, nodeName := range nodesToFailover {
		err = e2eutil.FailoverNode(t, client, e2eutil.Retries10, nodeName)
		if err != nil {
			t.Fatalf("failed to failover node: %v with error: %v", nodeName, err)
		}
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddMemberAddEvent(testCouchbase, 6)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries120)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestRecoveryAfterOneNsServerFailureBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	configMap := map[string]map[string]string{"cluster": clusterConfig, "service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// TODO kill pod via pkill beam.smp
	// ***
	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}
	// ***

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 4, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 1)
	if err != nil {
		t.Fatalf("failed to wait for 1 unhealthy node: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestRecoveryAfterOneNodeUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// TODO bring up iptables to disable networking on the node
	// ***
	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}
	// ***

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 4, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 1)
	if err != nil {
		t.Fatalf("failed to wait for 1 unhealthy node: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestRecoveryNodeTmpUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFiveDataN1qlIndex
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// TODO bring up iptables to disable networking on the node
	// ***
	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}
	// ***

	// TODO revert changes to iptables to reenable networking on the node
	// ***
	t.Logf("killing 1 pod...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pods: %v", err)
	}
	// ***

	t.Logf("waiting for pods to die...")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 4, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 4: %v", err)
	}

	t.Logf("waiting for unhealthy nodes from cluster...")
	err = e2eutil.WaitForUnhealthyNodes(t, client, e2eutil.Retries5, 1)
	if err != nil {
		t.Fatalf("failed to wait for 1 unhealthy node: %v", err)
	}

	t.Logf("getting cluster nodes...")
	clusterNodes, err := e2eutil.GetNodesFromCluster(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get nodes from cluster: %v", err)
	}
	if len(clusterNodes) != 5 {
		t.Logf("clusterNodes: %v", clusterNodes)
		t.Fatal("failed to see 5 nodes in the cluster")
	}

	t.Logf("waiting for cluster size to be 5")
	_, err = e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to reach cluster size of 5: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	err = e2eutil.VerifyClusterBalancedAndHealthy(t, client, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("cluster failed to become healthy and balanced: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}
