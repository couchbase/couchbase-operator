package e2e

import (
	"os"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with no buckets
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions
func TestResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	expectedEvents := e2eutil.EventList{}
	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := 1
	for _, clusterSize := range clusterSizes {
		err = e2eutil.ResizeCluster(t, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
		if err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceEvent(testCouchbase)
		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
		}
		prevClusterSize = clusterSize
	}
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with one bucket
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions
func TestResizeClusterWithBucket(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	expectedEvents := e2eutil.EventList{}
	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := 1
	for _, clusterSize := range clusterSizes {
		err = e2eutil.ResizeCluster(t, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
		if err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceEvent(testCouchbase)
		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
		}
		prevClusterSize = clusterSize
	}
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

// Tests scenerio where a third node is being added to a cluster, and a separate
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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// async scale up to 3 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 3, f.CRClient, testCouchbase)
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
	err = e2eutil.WaitForClusterBalancedCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
}

// Tests scenerio where the node being added to is killed before it can be
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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// async scale up to 3 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 3, f.CRClient, testCouchbase)
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
	err = e2eutil.WaitForClusterBalancedCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
}

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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
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
	err = e2eutil.ResizeCluster(t, 3, f.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(f.CRClient, testCouchbase, 300)
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
// 4. Verify that actuall cluster size is 2 nodes
// 5. Verify that external member was removed
// 6. Verify that the 2 cluster nodes are healthy
func TestRemoveForeignNode(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 1 node cluster with admin console
	testCouchbase, err := e2eutil.NewClusterExposed(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
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
	err = e2eutil.ResizeCluster(t, 2, f.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

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

// 1. create 1 node cluster
// 2. get max allocatable memory of a k8s node
// 3. update resource limits to 70% allocatable memory
// 4. attempt to scale up as 3 node cluster
// 5. wait for unbalanced condition
// 6. remove resource limits
// 7. expect scale up to complete
func TestNodeUnschedulable(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// request 70% of allocatable memory
	allocatableMemory, err := e2eutil.GetK8SAllocatableMemory(f.KubeClient, f.Namespace, nil)
	if err != nil {
		t.Fatal(err)
	}
	testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 5, func(cl *api.CouchbaseCluster) {
		cl.Spec.ServerSettings[0].Pod = e2espec.CreateMemoryPodPolicy(allocatableMemory*7/10, allocatableMemory)
	})
	if err != nil {
		t.Fatal(err)
	}

	// async start resize cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 3, f.CRClient, testCouchbase)
	}()

	// expect unbalanced condition
	err = e2eutil.WaitForClusterUnBalancedCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}

	// drop limits so that pod can be scheduled
	testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 5, func(cl *api.CouchbaseCluster) {
		cl.Spec.ServerSettings[0].Pod = nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// verify result from cluster resize ok
	err = <-echan
	if err != nil {
		t.Fatal(err)
	}

	// cluster should be healthy with 3 nodes
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, 3, 18)
	if err != nil {
		t.Fatal(err.Error())
	}

}

// Node service goes down while scaling down cluster
//
// 1. Create 5 node cluster
// 2. Scale down to 4 nodes
// 3. When rebalance starts, stop couchbase on node-0000
// 4. Expect down node-0000 to be removed
// 5. Cluster should eventually reconcile as 4 nodes:
//      a. either by not continuing to scale down since down node was removed
//      b. continuing to scale down and replacing down node
func TestNodeServiceDownDuringRebalance(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 5 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// scale down to 4 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 4, f.CRClient, testCouchbase)
	}()

	// when cluster starts scaling kill couchbase service on pod 0
	err = e2eutil.WaitForClusterScalingCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	_, err = f.ExecShellInPod(memberName, "mv /etc/service/couchbase-server /tmp/")
	if err != nil {
		t.Fatal(err)
	}

	// expect down node to be removed from cluster
	event := e2eutil.NewMemberRemoveEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 60)
	if err != nil {
		t.Fatal(err)
	}

	// resize cluster should complete ok
	err = <-echan
	if err != nil {
		t.Fatal(err)
	}

	// healthy 4 node cluster
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, 4, 30)
	if err != nil {
		t.Fatal(err.Error())
	}
}

// Test that a node is added back when operator is resumed
//
// 1. Create 2 node cluster
// 2. Pause operator
// 3. Externally remove a node
// 4. Resume operator
// 5. Expect operator to add another node
// 6. Verify cluster is balanced with 2 nodes
func TestReplaceManuallyRemovedNode(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 2 node cluster with admin console
	testCouchbase, err := e2eutil.NewClusterExposed(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 2, true)
	if err != nil {
		t.Fatal(err)
	}

	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// pause operator
	testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 5, func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = true
	})
	if err != nil {
		t.Fatal(err)
	}

	// create node port service for node 0
	clusterNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	nodePortService := e2espec.NewNodePortService(f.Namespace)
	nodePortService.Spec.Selector["couchbase_node"] = clusterNodeName
	service, err := e2eutil.CreateService(t, f.KubeClient, f.Namespace, nodePortService)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.DeleteService(t, f.KubeClient, f.Namespace, service.Name, nil)

	serviceUrl, err := e2eutil.NodePortServiceClient(f.ApiServerHost(), service)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{serviceUrl})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// remove node
	err = e2eutil.RebalanceOutMember(t, client, testCouchbase.Name, testCouchbase.Namespace, 1, true)
	if err != nil {
		t.Fatal(err)
	}

	// resume operator
	testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 5, func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = false
	})
	if err != nil {
		t.Fatal(err)
	}

	// expect an add member event to occur
	event := e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 60)
	if err != nil {
		t.Fatal(err)
	}

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}

	// check that actual cluster size is only 2 nodes
	info, err := client.ClusterInfo()
	numNodes := len(info.Nodes)
	if numNodes != 2 {
		t.Fatalf("expected 2 nodes, found: %d", numNodes)
	}

}

func TestNegResizeCluster(t *testing.T) {

}
