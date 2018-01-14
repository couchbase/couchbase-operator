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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	event := e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSizes := []int{2, 3, 2, 1}

	prevClusterSize := 1

	for _, clusterSize := range clusterSizes {
		service := 0
		err = e2eutil.ResizeCluster(t, service, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
		if err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			event := e2eutil.NewMemberAddEvent(testCouchbase, clusterSize-1)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)

			event = k8sutil.RebalanceEvent(testCouchbase)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddRebalanceEvent(testCouchbase)

		case clusterSize-prevClusterSize < 0:
			event = k8sutil.RebalanceEvent(testCouchbase)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddRebalanceEvent(testCouchbase)

			event = e2eutil.NewMemberRemoveEvent(testCouchbase, clusterSize)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
		}
		prevClusterSize = clusterSize
	}

	// verify events
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 3, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
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
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	event := e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = k8sutil.BucketCreateEvent("default", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := 1
	for _, clusterSize := range clusterSizes {
		service := 0
		err = e2eutil.ResizeCluster(t, service, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, clusterSize, 18)
		if err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			event := e2eutil.NewMemberAddEvent(testCouchbase, clusterSize-1)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)

			event = k8sutil.RebalanceEvent(testCouchbase)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddRebalanceEvent(testCouchbase)
		case clusterSize-prevClusterSize < 0:
			event = k8sutil.RebalanceEvent(testCouchbase)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddRebalanceEvent(testCouchbase)

			event = e2eutil.NewMemberRemoveEvent(testCouchbase, clusterSize)
			err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
			if err != nil {
				t.Fatal(err)
			}
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
		}
		prevClusterSize = clusterSize
	}

	// verify events
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 3, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}

	event := k8sutil.AdminConsoleSvcCreateEvent(testCouchbase.Name+"-ui", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")

	event = e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

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
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, 5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %+v", clusterInfo)

	// edit cluster dataServiceMemQuota
	newDataServiceMemQuota := "257"
	t.Log("Changing cluster data service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("DataServiceMemQuota", newDataServiceMemQuota, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, 6, "257", e2eutil.DataServiceMemQuotaVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster data service mem quota: %v", err)
	}

	// edit cluster indexServiceMemQuota
	newIndexServiceMemQuota := "257"
	t.Log("Changing cluster index service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexServiceMemQuota", newIndexServiceMemQuota, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, 6, "257", e2eutil.IndexServiceMemQuotaVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster index service mem quota: %v", err)
	}

	// edit cluster searchServiceMemQuota
	newSearchServiceMemQuota := "257"
	t.Log("Changing cluster search service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("SearchServiceMemQuota", newSearchServiceMemQuota, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, 6, "257", e2eutil.SearchServiceMemQuotaVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster search service mem quota: %v", err)
	}

	// edit cluster autoFailoverTimeout
	newAutoFailoverTimeout := "31"
	t.Log("Changing cluster autofailover timeout")
	testCouchbase, err = e2eutil.UpdateClusterSettings("AutoFailoverTimeout", newAutoFailoverTimeout, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyAutoFailoverInfo(t, client, 6, "31", e2eutil.AutoFailoverTimeoutVerifier)
	if err != nil {
		t.Fatalf("failed to change cluster autofailover timeout: %v", err)
	}

	// verify events
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, true)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}

	event := k8sutil.AdminConsoleSvcCreateEvent(testCouchbase.Name+"-ui", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")

	event = e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

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
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, 5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %+v", clusterInfo)

	// edit cluster dataServiceMemQuota
	newDataServiceMemQuota := "0"
	t.Log("Changing cluster data service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("DataServiceMemQuota", newDataServiceMemQuota, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, 6, "0", e2eutil.DataServiceMemQuotaVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster data service mem quota: %v", err)
	}

	// edit cluster indexServiceMemQuota
	newIndexServiceMemQuota := "0"
	t.Log("Changing cluster index service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexServiceMemQuota", newIndexServiceMemQuota, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyClusterInfo(t, client, 6, "0", e2eutil.IndexServiceMemQuotaVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster index service mem quota: %v", err)
	}

	// edit cluster searchServiceMemQuota
	newSearchServiceMemQuota := "0"
	t.Log("Changing cluster search service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("SearchServiceMemQuota", newSearchServiceMemQuota, f.CRClient, testCouchbase, 5)
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
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexStorageSetting", newIndexStorageSetting, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyIndexSettingInfo(t, client, 6, "0", e2eutil.IndexSettingVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster indexer storage setting: %v", err)
	}

	// edit cluster autoFailoverTimeout
	newAutoFailoverTimeout := "0"
	t.Log("Changing cluster autofailover timeout")
	testCouchbase, err = e2eutil.UpdateClusterSettings("AutoFailoverTimeout", newAutoFailoverTimeout, f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.VerifyAutoFailoverInfo(t, client, 6, "0", e2eutil.AutoFailoverTimeoutVerifier)
	if err == nil {
		t.Fatalf("failed to reject change for cluster autofailover timeout: %v", err)
	}

	//verify events
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "invalid-test-secret", configMap, false)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
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

// Node service goes down while scaling down cluster
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
	serviceConfig1 := e2eutil.BasicServiceOneDataN1qlIndex
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}

	event := k8sutil.AdminConsoleSvcCreateEvent(testCouchbase.Name+"-ui", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")

	event = e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

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

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, 5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %+v", clusterInfo)

	// adding n1ql service
	t.Log("adding n1ql service")
	newService := api.ServerConfig{
		Size:      1,
		Name:      "test_config_2",
		Services:  []string{"n1ql"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, 10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
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
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, 10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
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
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, 10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 3)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add fts service: %v", err)
	}

	// removing search service
	t.Log("removing fts service")
	removeServiceName := "test_config_4"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, 10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, 3)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 3)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove fts service: %v", err)
	}

	// removing index service
	t.Log("removing index service")
	removeServiceName = "test_config_3"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, 10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 2)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove index service: %v", err)
	}

	// removing n1ql service
	t.Log("removing n1ql service")
	removeServiceName = "test_config_2"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, 10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  0,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove n1ql service: %v", err)
	}

	// verify events
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, true)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}

	event := k8sutil.AdminConsoleSvcCreateEvent(testCouchbase.Name+"-ui", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")

	event = e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

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
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, 5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %+v", clusterInfo)

	// adding n1ql service
	t.Log("adding n1ql service")
	newService := api.ServerConfig{
		Size:      1,
		Name:      "test_config_2",
		Services:  []string{"n1ql"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, 10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
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
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, 10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
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
	testCouchbase, err = e2eutil.AddServices(f.CRClient, testCouchbase, newService, 10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 3)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 4)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 4)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   2,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add fts service: %v", err)
	}

	// swapping nodes search - 1 and index + 1
	t.Log("swaping nodes: test_config_4--, test_config_3++")
	swapMap := map[string]int{
		"test_config_4": 1,
		"test_config_3": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(f.CRClient, testCouchbase, 10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 5)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 5)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, 4)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 4)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 2,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, 18, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_4--, test_config_3++: %v", err)
	}

	// swapping nodes index - 1 and n1ql + 1
	t.Log("swaping nodes: test_config_3--, test_config_2++")
	swapMap = map[string]int{
		"test_config_3": 1,
		"test_config_2": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(f.CRClient, testCouchbase, 10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 6)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 6)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, 5)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 5)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  2,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, 18, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_3--, test_config_2++: %v", err)
	}

	// swapping nodes n1ql - 1 and data + 1
	t.Log("swaping nodes: test_config_2--, test_config_1++")
	swapMap = map[string]int{
		"test_config_2": 1,
		"test_config_1": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(f.CRClient, testCouchbase, 10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 7)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 7)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	event = e2eutil.NewMemberRemoveEvent(testCouchbase, 6)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 6)

	serviceMap = map[string]int{
		"Data":  2,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	// verify events
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
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
	serviceConfig1 := e2eutil.BasicServiceThreeDataN1qlIndex
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	_, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, false)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, true)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal("failed to create cluster: %v", err)
	}

	expectedEvents := e2eutil.EventList{}

	event := k8sutil.AdminConsoleSvcCreateEvent(testCouchbase.Name+"-ui", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")

	event = e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

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
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, 5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %+v", clusterInfo)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
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

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, true)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal("failed to create cluster: %v", err)
	}

	expectedEvents := e2eutil.EventList{}

	event := k8sutil.AdminConsoleSvcCreateEvent(testCouchbase.Name+"-ui", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")

	event = e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

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
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, 5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %+v", clusterInfo)

	t.Log("removing last data service")
	removeServiceName := "test_config_2"
	testCouchbase, err = e2eutil.RemoveServices(f.CRClient, testCouchbase, removeServiceName, 10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
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
	err = e2eutil.VerifyServices(t, client, 6, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to remove data service: %v", err)
	}
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
	}

}
