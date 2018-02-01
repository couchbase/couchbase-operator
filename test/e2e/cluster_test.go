package e2e

import (
	"os"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
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

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSizes := []int{2, 3, 2, 1}

	prevClusterSize := e2eutil.Size1

	for _, clusterSize := range clusterSizes {
		service := 0
		err = e2eutil.ResizeCluster(t, service, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
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

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
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
	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := e2eutil.Size1
	for _, clusterSize := range clusterSizes {
		service := 0
		err = e2eutil.ResizeCluster(t, service, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, e2eutil.Retries10)
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

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
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

// Tests editing of cluster settings
// 1. Create a 1 node cluster
// 2. Change data service memory quota from 256 to 257 (verify via rest call to cluster)
// 3. Change index service memory quota from 256 to 257 (verify via rest call to cluster)
// 4. Change search service memory quota from 256 to 257 ( verify via rest call to cluster)
// 5. Change autofailover timeout from 30 to 31 ( verify via rest call to cluster)
func TestEditClusterSettings(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataN1qlIndex
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
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

	// edit cluster dataServiceMemQuota
	newDataServiceMemQuota := "257"
	t.Log("Changing cluster data service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("DataServiceMemQuota", newDataServiceMemQuota, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, "257", e2eutil.DataServiceMemQuotaVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster data service mem quota: %v", err)
	}

	// edit cluster indexServiceMemQuota
	newIndexServiceMemQuota := "257"
	t.Log("Changing cluster index service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexServiceMemQuota", newIndexServiceMemQuota, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, "257", e2eutil.IndexServiceMemQuotaVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster index service mem quota: %v", err)
	}

	// edit cluster searchServiceMemQuota
	newSearchServiceMemQuota := "257"
	t.Log("Changing cluster search service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("SearchServiceMemQuota", newSearchServiceMemQuota, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, "257", e2eutil.SearchServiceMemQuotaVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster search service mem quota: %v", err)
	}

	// edit cluster autoFailoverTimeout
	newAutoFailoverTimeout := "31"
	t.Log("Changing cluster autofailover timeout")
	testCouchbase, err = e2eutil.UpdateClusterSettings("AutoFailoverTimeout", newAutoFailoverTimeout, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyAutoFailoverInfo(t, client, e2eutil.Retries5, "31", e2eutil.AutoFailoverTimeoutVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster autofailover timeout: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
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

// Tests invalid editing of cluster settings (setting changes should not take hold)
// 1. Create a 1 node cluster
// 2. Change data service memory quota from 256 to 0 (verify via rest call to cluster)
// 3. Change index service memory quota from 256 to 0 (verify via rest call to cluster)
// 4. Change search service memory quota from 256 to 0 ( verify via rest call to cluster)
// 5. Change indexer storage mode from gsi to moi (verify via rest call to cluster)
// 6. Change autofailover timeout from 30 to 0 ( verify via rest call to cluster)
func TestNegEditClusterSettings(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataN1qlIndex
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminExposed)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

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

	// edit cluster dataServiceMemQuota
	newDataServiceMemQuota := "0"
	t.Log("Changing cluster data service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("DataServiceMemQuota", newDataServiceMemQuota, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, "0", e2eutil.DataServiceMemQuotaVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster data service mem quota: %v", err)
	}

	// edit cluster indexServiceMemQuota
	newIndexServiceMemQuota := "0"
	t.Log("Changing cluster index service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexServiceMemQuota", newIndexServiceMemQuota, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, e2eutil.Retries5, "0", e2eutil.IndexServiceMemQuotaVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster index service mem quota: %v", err)
	}

	// edit cluster searchServiceMemQuota
	newSearchServiceMemQuota := "0"
	t.Log("Changing cluster search service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("SearchServiceMemQuota", newSearchServiceMemQuota, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, 6, "0", e2eutil.SearchServiceMemQuotaVerifier)
	if err == nil {
		t.Fatalf("failed to reject change cluster for search service mem quota: %v", err)
	}

	// edit cluster indexStorageSetting
	newIndexStorageSetting := "plasma"
	t.Log("Changing cluster index storage setting")
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexStorageSetting", newIndexStorageSetting, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyIndexSettingInfo(t, client, e2eutil.Retries5, "0", e2eutil.IndexSettingVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster indexer storage setting: %v", err)
	}

	// edit cluster autoFailoverTimeout
	newAutoFailoverTimeout := "0"
	t.Log("Changing cluster autofailover timeout")
	testCouchbase, err = e2eutil.UpdateClusterSettings("AutoFailoverTimeout", newAutoFailoverTimeout, f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyAutoFailoverInfo(t, client, e2eutil.Retries5, "0", e2eutil.AutoFailoverTimeoutVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster autofailover timeout: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
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

// Tests if specs with invalid auth secrets will create a cluster (they should not)
// 1. Attempt to create a cluster with invalid auth secret
// 2. Wait until cluster creation fails
func TestInvalidAuthSecret(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataN1qlIndex
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "invalid-test-secret", configMap, e2eutil.AdminHidden)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
		t.Fatalf("failed to reject cluster creation: %v", err)
	}
}

// Tests if specs with invalid base image will create a cluster (they should not)
// 1. Attempt to create a cluster with invalid base image
// 2. Wait until cluster creation fails
func TestInvalidBaseImage(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataN1qlIndex
	otherConfig1 := map[string]string{
		"baseImageName": "basecouch/123",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminHidden)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
		t.Fatalf("failed to reject cluster creation: %v", err)
	}
}

// Tests if specs with invalid verison will create a cluster (they should not)
// 1. Attempt to create a cluster with invalid version
// 2. Wait until cluster creation fails
func TestInvalidVersion(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataN1qlIndex
	otherConfig1 := map[string]string{
		"versionNum": "enterprise-1.9.1",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, e2eutil.AdminHidden)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
		t.Fatalf("failed to reject cluster creation: %v", err)
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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true, false)
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
	// async start resize cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 0, 3, f.CRClient, testCouchbase)
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

// Cluster recovers after node service goes down
// 1. Create 3 node cluster
// 2. stop couchbase on node-0000
// 3. Expect down node-0000 to be removed
// 4. Cluster should eventually reconcile as 5 nodes:
func TestNodeServiceDownRecovery(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 3, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// kill couchbase service on pod 0
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	_, err = f.ExecShellInPod(memberName, "mv /etc/service/couchbase-server /tmp/")
	if err != nil {
		t.Fatal(err)
	}

	// expect down node to be removed from cluster
	event := e2eutil.NewMemberRemoveEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	// healthy 3 node cluster
	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, 3, 30)
	if err != nil {
		t.Fatal(err.Error())
	}
}

// Node service goes down while scaling down cluster
//
// TODO: (See K8S-113)
//     Test currently hangs since failure scenario leads to
//     possible datalos which currently requires manual user intervention
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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 5, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	// scale down to 4 node cluster
	echan := make(chan error)
	go func() {
		echan <- e2eutil.ResizeCluster(t, 0, 4, f.CRClient, testCouchbase)
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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 2, true, true)
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

// Tests basic MDS Scaling
// 1. Create 1 node cluster with data service only
// 2. Add n1ql service to cluster (verify via rest call to cluster)
// 3. Add index service to cluster (verify via rest call to cluster)
// 4. Add search service to cluster (verify via rest call to cluster)
// 5. Remove search service from cluster (verify via rest call to cluster)
// 6. Remove index service from cluster (verify via rest call to cluster)
// 7. Remove n1ql service from cluster (verify via rest call to cluster)
func TestBasicMDSScaling(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataNode
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
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

	// adding n1ql service
	t.Log("adding n1ql service")
	newService := api.ServerConfig{
		Size:      1,
		Name:      "test_config_2",
		Services:  []string{"n1ql"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add n1ql service: %v", err)
	}

	// adding index service
	t.Log("adding index service")
	newService = api.ServerConfig{
		Size:      1,
		Name:      "test_config_3",
		Services:  []string{"index"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries5, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add index service: %v", err)
	}

	// adding search service
	t.Log("adding fts service")
	newService = api.ServerConfig{
		Size:      1,
		Name:      "test_config_4",
		Services:  []string{"fts"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries5, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add fts service: %v", err)
	}

	// removing search service
	t.Log("removing fts service")
	removeServiceName := "test_config_4"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 3)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries5, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove fts service: %v", err)
	}

	// removing index service
	t.Log("removing index service")
	removeServiceName = "test_config_3"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 2)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries5, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove index service: %v", err)
	}

	// removing n1ql service
	t.Log("removing n1ql service")
	removeServiceName = "test_config_2"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  0,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries5, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove n1ql service: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
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

// Tests swapping nodes between services
// 1. Create 1 node cluster with data service only
// 2. Add n1ql service to cluster, 1 node (verify via rest call to cluster)
// 3. Add index service to cluster, 1 node (verify via rest call to cluster)
// 4. Add search service to cluster, 2 nodes (verify via rest call to cluster)
// 5. Swap node from search service to index service (verify via rest call to cluster)
// 6. Swap node from index service to n1ql service (verify via rest call to cluster)
// 7. Swap node from n1ql service to data service (verify via rest call to cluster)
func TestSwapNodesBetweenServices(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneDataNode
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

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

	// adding n1ql service
	t.Log("adding n1ql service")
	newService := api.ServerConfig{
		Size:      1,
		Name:      "test_config_2",
		Services:  []string{"n1ql"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add n1ql service: %v", err)
	}

	// adding index service
	t.Log("adding index service")
	newService = api.ServerConfig{
		Size:      1,
		Name:      "test_config_3",
		Services:  []string{"index"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add index service: %v", err)
	}

	// adding search services
	t.Log("adding fts service")
	newService = api.ServerConfig{
		Size:      2,
		Name:      "test_config_4",
		Services:  []string{"fts"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   2,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add fts service: %v", err)
	}

	// swapping nodes search - 1 and index + 1
	t.Log("swaping nodes: test_config_4--, test_config_3++")
	swapMap := map[string]int{
		"test_config_4": 1,
		"test_config_3": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(f.CRClient, testCouchbase, e2eutil.Retries10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 4)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 2,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_4--, test_config_3++: %v", err)
	}

	// swapping nodes index - 1 and n1ql + 1
	t.Log("swaping nodes: test_config_3--, test_config_2++")
	swapMap = map[string]int{
		"test_config_3": 1,
		"test_config_2": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(f.CRClient, testCouchbase, e2eutil.Retries10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 6)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 5)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  2,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_3--, test_config_2++: %v", err)
	}

	// swapping nodes n1ql - 1 and data + 1
	t.Log("swaping nodes: test_config_2--, test_config_1++")
	swapMap = map[string]int{
		"test_config_2": 1,
		"test_config_1": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(f.CRClient, testCouchbase, e2eutil.Retries10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 7)
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 6)

	event := e2eutil.NewMemberRemoveEvent(testCouchbase, 6)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	serviceMap = map[string]int{
		"Data":  2,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries10)
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

// Tests creating a cluster without data service
// 1. Attempt to create cluster without data service
// 2. Verify that the cluster could not be created
func TestCreateClusterWithoutDataService(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneN1qlIndexSearch
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	_, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminHidden)
	if err == nil {
		t.Fatal("failed to not create cluster")
	}
}

// Tests creating a cluster where the data service is the second service listed in the spec
// 1. Attempt to create a 2 node cluster with cluster spec order {[n1ql,fts,search], [data]}
// 2. Verify cluster was created via rest call
func TestCreateClusterDataServiceNotFirst(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneN1qlIndexSearch
	serviceConfig2 := e2eutil.BasicSecondaryServiceOneData
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal("failed to create cluster: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)


	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
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

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries5, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size2, e2eutil.Retries10)
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

func TestRemoveLastDataService(t *testing.T) {
	t.Skip("test not ready")
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceOneN1qlIndexSearch
	serviceConfig2 := e2eutil.BasicSecondaryServiceOneData
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal("failed to create cluster: %v", err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
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

	t.Log("removing last data service")
	removeServiceName := "test_config_2"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)

	serviceMap := map[string]int{
		"Data":  0,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, e2eutil.Retries5, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove data service: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
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

func TestManageMultipleClusters(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	t.Logf("Creating 3 Couchbase Clusters...\n")

	t.Logf("1: Creating New Couchbase Cluster...\n")
	testCouchbase1, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size2, e2eutil.WithoutBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase1)

	t.Logf("2: Creating New Couchbase Cluster...\n")
	testCouchbase2, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size2, e2eutil.WithoutBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase2)

	t.Logf("3: Creating New Couchbase Cluster...\n")
	testCouchbase3, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size2, e2eutil.WithoutBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase3)

	expectedEvents1 := e2eutil.EventList{}
	expectedEvents1.AddAdminConsoleSvcCreateEvent(testCouchbase1, testCouchbase1.Name+"-ui")
	expectedEvents1.AddMemberAddEvent(testCouchbase1, 0)
	expectedEvents1.AddMemberAddEvent(testCouchbase1, 1)
	expectedEvents1.AddRebalanceEvent(testCouchbase1)

	expectedEvents2 := e2eutil.EventList{}
	expectedEvents2.AddAdminConsoleSvcCreateEvent(testCouchbase2, testCouchbase2.Name+"-ui")
	expectedEvents2.AddMemberAddEvent(testCouchbase2, 0)
	expectedEvents2.AddMemberAddEvent(testCouchbase2, 1)
	expectedEvents2.AddRebalanceEvent(testCouchbase2)

	expectedEvents3 := e2eutil.EventList{}
	expectedEvents3.AddAdminConsoleSvcCreateEvent(testCouchbase3, testCouchbase3.Name+"-ui")
	expectedEvents3.AddMemberAddEvent(testCouchbase3, 0)
	expectedEvents3.AddMemberAddEvent(testCouchbase3, 1)
	expectedEvents3.AddRebalanceEvent(testCouchbase3)

	// create connection to couchbase nodes
	consoleURL1, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase1.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client1, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase1, []string{consoleURL1})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo1, err := e2eutil.GetClusterInfo(t, client1, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo1)

	// create connection to couchbase nodes
	consoleURL2, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase2.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client2, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase2, []string{consoleURL2})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo2, err := e2eutil.GetClusterInfo(t, client2, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo2)

	// create connection to couchbase nodes
	consoleURL3, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase3.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client3, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase3, []string{consoleURL3})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo3, err := e2eutil.GetClusterInfo(t, client3, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo3)

	// add bucket to cluster 1
	bucketSetting1 := api.BucketConfig{
		BucketName:         "default1",
		BucketType:         constants.BucketTypeCouchbase,
		BucketMemoryQuota:  256,
		BucketReplicas:     &constants.BucketReplicasOne,
		IoPriority:         &constants.BucketIoPriorityHigh,
		EvictionPolicy:     &constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: &constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: &constants.BucketIndexReplicasEnabled,
	}
	bucketConfig1 := []api.BucketConfig{bucketSetting1}
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig1)
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig1 }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase1, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase1, e2eutil.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Waiting For Bucket To Be Created \n")
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{bucketSetting1.BucketName}, e2eutil.Retries10, testCouchbase1)
	if err != nil {
		t.Fatalf("failed to create bucket %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client1, e2eutil.Retries5, "default1", "BucketMemoryQuota", "256", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}

	expectedEvents1.AddBucketCreateEvent(testCouchbase1, "default1")

	// add bucket to cluster 2
	bucketSetting2 := api.BucketConfig{
		BucketName:         "default2",
		BucketType:         constants.BucketTypeCouchbase,
		BucketMemoryQuota:  256,
		BucketReplicas:     &constants.BucketReplicasOne,
		IoPriority:         &constants.BucketIoPriorityHigh,
		EvictionPolicy:     &constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: &constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: &constants.BucketIndexReplicasEnabled,
	}
	bucketConfig2 := []api.BucketConfig{bucketSetting2}
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig2)
	updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig2 }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase2, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase2, e2eutil.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Waiting For Bucket To Be Created \n")
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{bucketSetting2.BucketName}, e2eutil.Retries10, testCouchbase2)
	if err != nil {
		t.Fatalf("failed to create bucket %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client2, e2eutil.Retries5, "default2", "BucketMemoryQuota", "256", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}

	expectedEvents2.AddBucketCreateEvent(testCouchbase2, "default2")

	// add bucket to cluster 3
	bucketSetting3 := api.BucketConfig{
		BucketName:         "default3",
		BucketType:         constants.BucketTypeCouchbase,
		BucketMemoryQuota:  256,
		BucketReplicas:     &constants.BucketReplicasOne,
		IoPriority:         &constants.BucketIoPriorityHigh,
		EvictionPolicy:     &constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: &constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: &constants.BucketIndexReplicasEnabled,
	}
	bucketConfig3 := []api.BucketConfig{bucketSetting3}
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig3)
	updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig3 }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase3, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase3, e2eutil.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Waiting For Bucket To Be Created \n")
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{bucketSetting3.BucketName}, e2eutil.Retries10, testCouchbase3)
	if err != nil {
		t.Fatalf("failed to create bucket %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client3, e2eutil.Retries5, "default3", "BucketMemoryQuota", "256", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}

	expectedEvents3.AddBucketCreateEvent(testCouchbase3, "default3")

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase1.Name, f.Namespace, e2eutil.Size2, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase2.Name, f.Namespace, e2eutil.Size2, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase3.Name, f.Namespace, e2eutil.Size2, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events1, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase1.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	events2, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase2.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	events3, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase3.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents1.Compare(events1) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents1, events1))
	}
	if !expectedEvents2.Compare(events2) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents2, events2))
	}
	if !expectedEvents3.Compare(events3) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents3, events3))
	}
}
