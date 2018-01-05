package e2e

import (
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Tests one node failing in a cluster with 0 buckets
// 1. Create a 5 node Couchbase cluster with no buckets
// 2. Delete one of the pods in the cluster
// 3. Wait for Couchbase to failover the dead node and rebalance in a new one
// 4. Check the cluster status to make sure the cluster is healthy
// 5. Check the events to make sure the operator took the correct actions
func TestSingleNodeFailureNoBuckets(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := map[string]string{
		"dataServiceMemQuota":   "256",
		"indexServiceMemQuota":  "256",
		"searchServiceMemQuota": "256",
		"indexStorageSetting":   "memory_optimized",
		"autoFailoverTimeout":   "30"}
	serviceConfig1 := map[string]string{
		"size":      "5",
		"name":      "test_config_1",
		"services":  "data,n1ql,index",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	e2eutil.KillPodsAndWaitForRecovery(t, f.KubeClient, testCouchbase, 1)
	if err != nil {
		t.Fatalf("failed to kill pod and recover: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 0)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, 5, 18)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}

	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

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
	testCouchbase, err := e2eutil.NewClusterExposed(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 2, true)
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

	// cluster should also be balanced
	err = e2eutil.WaitForClusterBalancedCondition(f.CRClient, testCouchbase, 300)
	if err != nil {
		t.Fatal(err)
	}

	// expect member add for node being replaced
	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	// expect operator to rebalance in the node
	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	// healthy 2 node cluster
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, 2, 18)
	if err != nil {
		t.Fatal(err.Error())
	}

}

func TestNodeConfig(t *testing.T) {

}

func TestNodeConfigNegative(t *testing.T) {

}

func TestNodeFailure(t *testing.T) {

}
