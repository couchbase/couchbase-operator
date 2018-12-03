package e2e

import (
	"os"
	"strconv"
	"strings"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	pkg_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithoutBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := constants.Size1
	for _, clusterSize := range clusterSizes {
		service := 0
		testCouchbase, err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
		}
		prevClusterSize = clusterSize
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := constants.Size1
	for _, clusterSize := range clusterSizes {
		service := 0
		testCouchbase, err = e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}

		if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
			t.Fatal(err.Error())
		}

		switch {
		case clusterSize-prevClusterSize > 0:
			expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
		case clusterSize-prevClusterSize < 0:
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, clusterSize)
			expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
		}
		prevClusterSize = clusterSize
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
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

	// edit cluster dataServiceMemQuota
	newDataServiceMemQuota := "257"
	t.Log("Changing cluster data service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("DataServiceMemQuota", newDataServiceMemQuota, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.VerifyClusterInfo(t, client, constants.Retries5, newDataServiceMemQuota, e2eutil.DataServiceMemQuotaVerifier); err != nil {
		t.Fatalf("failed to change cluster data service mem quota: %v", err)
	}
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster indexServiceMemQuota
	newIndexServiceMemQuota := "257"
	t.Log("Changing cluster index service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexServiceMemQuota", newIndexServiceMemQuota, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.VerifyClusterInfo(t, client, constants.Retries5, newIndexServiceMemQuota, e2eutil.IndexServiceMemQuotaVerifier); err != nil {
		t.Fatalf("failed to change cluster index service mem quota: %v", err)
	}
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster searchServiceMemQuota
	newSearchServiceMemQuota := "257"
	t.Log("Changing cluster search service mem quota")
	testCouchbase, err = e2eutil.UpdateClusterSettings("SearchServiceMemQuota", newSearchServiceMemQuota, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.VerifyClusterInfo(t, client, constants.Retries5, newSearchServiceMemQuota, e2eutil.SearchServiceMemQuotaVerifier); err != nil {
		t.Fatalf("failed to change cluster search service mem quota: %v", err)
	}
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster autoFailoverTimeout
	newAutoFailoverTimeout := "31"
	t.Log("Changing cluster autofailover timeout")
	testCouchbase, err = e2eutil.UpdateClusterSettings("AutoFailoverTimeout", newAutoFailoverTimeout, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.VerifyAutoFailoverInfo(t, client, constants.Retries5, newAutoFailoverTimeout, e2eutil.AutoFailoverTimeoutVerifier); err != nil {
		t.Fatalf("failed to change cluster autofailover timeout: %v", err)
	}
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "autofailover")

	// edit cluster indexStorageSetting
	newIndexStorageSetting := "plasma"
	t.Log("Changing cluster index storage setting")
	testCouchbase, err = e2eutil.UpdateClusterSettings("IndexStorageSetting", newIndexStorageSetting, targetKube.CRClient, testCouchbase, constants.Retries5)
	if err != nil {
		t.Fatal(err)
	}
	if err := e2eutil.VerifyIndexSettingInfo(t, client, constants.Retries5, newIndexStorageSetting, e2eutil.IndexSettingVerifier); err != nil {
		t.Fatalf("failed to change cluster indexer storage setting: %v", err)
	}
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "index service")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)

}

// Tests invalid editing of cluster settings (setting changes should not take hold)
func TestNegEditClusterSettings(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// edit cluster indexStorageSetting
	newIndexStorageSetting := "plasma"
	t.Log("Changing cluster index storage setting")
	if _, err := e2eutil.UpdateClusterSettings("IndexStorageSetting", newIndexStorageSetting, targetKube.CRClient, testCouchbase, constants.Retries5); err == nil {
		t.Fatal("successful update to index storage mode when index service enabled")
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests if specs with invalid auth secrets will create a cluster (they should not)
// 1. Attempt to create a cluster with invalid auth secret
// 2. Wait until cluster creation fails
func TestInvalidAuthSecret(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "query", "index"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

	retries := &e2eutil.ClusterReadyRetries{
		Size:    constants.Retries5,
		Bucket:  constants.Retries1,
		Service: constants.Retries1,
	}
	testCouchbase, err := e2eutil.NewClusterMultiQuick(t, targetKube, f.Namespace, configMap, constants.AdminHidden, retries)
	if err == nil {
		t.Fatalf("failed to reject cluster creation: %v", err)
	}

	expectedEvents := e2eutil.EventList{}

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if len(pods.Items) != 0 {
		t.Fatalf("more than zero pods: %+v", pods.Items)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests if specs with invalid base image will create a cluster (they should not)
// 1. Attempt to create a cluster with invalid base image
// 2. Wait until cluster creation fails
func TestInvalidBaseImage(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	couchbaseBaseImage := "basecouch/123"
	couchbaseVerString := "enterprise-5.5.0"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "query", "index"})
	otherConfig1 := map[string]string{
		"baseImageName": couchbaseBaseImage,
		"versionNum":    couchbaseVerString,
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}

	retries := &e2eutil.ClusterReadyRetries{
		Size:    constants.Retries5,
		Bucket:  constants.Retries1,
		Service: constants.Retries1,
	}
	testCouchbase, err := e2eutil.NewClusterMultiQuick(t, targetKube, f.Namespace, configMap, constants.AdminHidden, retries)
	if err == nil {
		t.Fatalf("failed to reject cluster creation: %v", err)
	}

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if len(pods.Items) != 1 {
		t.Fatalf("more than one pod: %v", pods.Items)
	}

	reason := pods.Items[0].Status.ContainerStatuses[0].State.Waiting.Reason
	if reason != "ErrImagePull" && reason != "ImagePullBackOff" && !strings.Contains(reason, "image "+couchbaseBaseImage+":"+couchbaseVerString+" not found") {
		t.Fatalf("container status error: %s", reason)
	}

	acceptableMessages := []string{
		"Back-off pulling image \"" + couchbaseBaseImage + ":" + couchbaseVerString + "\"",
		"rpc error: code = 2 desc = Error: image " + couchbaseBaseImage + ":" + couchbaseVerString + " not found",
		"rpc error: code = Unknown desc = repository docker.io/" + couchbaseBaseImage + " not found: does not exist or no pull access",
		// For openshift v3.9.33, kubernetes v1.9.1+a0ce1bc657
		"rpc error: code = Unknown desc = Error: image " + couchbaseBaseImage + ":" + couchbaseVerString + " not found",
		"rpc error: code = Unknown desc = Error response from daemon: repository " + couchbaseBaseImage + " not found: does not exist or no pull access",
	}

	containerMsg := pods.Items[0].Status.ContainerStatuses[0].State.Waiting.Message
	correctMsg := false
	for _, acceptableMsg := range acceptableMessages {
		if containerMsg == acceptableMsg {
			correctMsg = true
			break
		}
	}

	if !correctMsg {
		t.Fatalf("incorrect msg, container status error: %+v", containerMsg)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, e2eutil.EventList{})
}

// Tests if specs with invalid version will create a cluster (they should not)
// 1. Attempt to create a cluster with invalid version
// 2. Wait until cluster creation fails
func TestInvalidVersion(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	couchbaseBaseImage := "couchbase/server"
	couchbaseVerString := "enterprise-9.9.9"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "query", "index"})
	otherConfig1 := map[string]string{
		"versionNum":    couchbaseVerString,
		"baseImageName": couchbaseBaseImage,
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}

	retries := &e2eutil.ClusterReadyRetries{
		Size:    constants.Retries5,
		Bucket:  constants.Retries1,
		Service: constants.Retries1,
	}
	testCouchbase, err := e2eutil.NewClusterMultiQuick(t, targetKube, f.Namespace, configMap, constants.AdminHidden, retries)
	if err == nil {
		t.Fatalf("failed to reject cluster creation: %v", err)
	}

	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if len(pods.Items) != 1 {
		t.Fatalf("more than one pod: %v", pods.Items)
	}

	reason := pods.Items[0].Status.ContainerStatuses[0].State.Waiting.Reason
	if reason != "ErrImagePull" && reason != "ImagePullBackOff" && !strings.Contains(reason, "image "+couchbaseBaseImage+":"+couchbaseVerString+" not found") {
		t.Fatalf("container status error: %s", reason)
	}

	acceptableMessages := []string{
		"Back-off pulling image \"" + couchbaseBaseImage + ":" + couchbaseVerString + "\"",
		"rpc error: code = 2 desc = Tag " + couchbaseVerString + " not found in repository docker.io/",
		"rpc error: code = Unknown desc = manifest for docker.io/" + couchbaseBaseImage + ":" + couchbaseVerString + " not found",
		// For openshift v3.9.33, kubernetes v1.9.1+a0ce1bc657
		"rpc error: code = Unknown desc = Tag " + couchbaseVerString + " not found in repository docker.io/" + couchbaseBaseImage,
	}

	containerMsg := pods.Items[0].Status.ContainerStatuses[0].State.Waiting.Message
	correctMsg := false
	for _, acceptableMsg := range acceptableMessages {
		if containerMsg == acceptableMsg {
			correctMsg = true
			break
		}
	}

	if !correctMsg {
		t.Fatalf("incorrect msg, container status error: %+v", containerMsg)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, e2eutil.EventList{})
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	clusterSize := constants.Size1

	// create 1 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// Get max from k8s cluster allocatable memory
	allocatableMemory, err := e2eutil.GetK8SMaxAllocatableMemory(targetKube.KubeClient)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Allocatable Memory: %d", allocatableMemory)
	testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, func(cl *api.CouchbaseCluster) {
		cl.Spec.ServerSettings[0].Pod = e2espec.CreateMemoryPodPolicy(allocatableMemory*7/10, allocatableMemory)
	})
	nodeList, err := targetKube.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	//first node wont change request and last node should be unschedulable and minus out the master node
	clusterSize = len(nodeList.Items) + 1
	serviceId := 0
	testCouchbase, err = e2eutil.ResizeClusterNoWait(t, serviceId, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for each member add event
	for memberId := 1; memberId < clusterSize-1; memberId++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 180); err != nil {
			t.Fatal(err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	// expect unbalanced condition
	if err := e2eutil.WaitForClusterUnBalancedCondition(t, targetKube.CRClient, testCouchbase, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, clusterSize-1)

	// drop limits so that pod can be scheduled
	testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, func(cl *api.CouchbaseCluster) {
		cl.Spec.ServerSettings[0].Pod = nil
	})
	if err != nil {
		t.Fatal(err)
	}

	event := e2eutil.NewMemberAddEvent(testCouchbase, clusterSize-1)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 180); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)

	// Wait for cluster balanced condition
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries20); err != nil {
		t.Fatal(err.Error())
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Cluster recovers after node service goes down
// 1. Create 3 node cluster
// 2. stop couchbase on node-0000
// 3. Expect down node-0000 to be removed
// 4. Cluster should eventually reconcile as 3 nodes:
func TestNodeServiceDownRecovery(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	removePodMemberId := 0
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// create 3 node cluster
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithBucket, constants.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	for nodeIndex := 0; nodeIndex < constants.Size3; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// kill couchbase service on pod 0
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	if f.KubeType == "kubernetes" {
		if _, err := f.ExecShellInPod(kubeName, memberName, "mv /etc/service/couchbase-server /tmp/"); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, memberName, f.Namespace); err != nil {
			t.Fatal(err)
		}
	}

	expectedEvents.AddMemberDownEvent(testCouchbase, 0)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, 0)

	// expect down node to be removed from cluster
	event := e2eutil.NewMemberRemoveEvent(testCouchbase, removePodMemberId)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300); err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, removePodMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// healthy 3 node cluster
	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	// create 5 node cluster
	clusterSize := constants.Size5
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)
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

	clusterSize--
	// scale down to a node in cluster
	testCouchbase, err = e2eutil.ResizeClusterNoWait(t, 0, clusterSize, targetKube.CRClient, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	// when cluster starts scaling kill couchbase service on pod 0
	if err := e2eutil.WaitForClusterScalingCondition(t, targetKube.CRClient, testCouchbase, 300); err != nil {
		t.Fatal(err)
	}

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	if f.KubeType == "kubernetes" {
		if _, err := f.ExecShellInPod(kubeName, memberName, "mv /etc/service/couchbase-server /tmp/"); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, memberName, f.Namespace); err != nil {
			t.Fatal(err)
		}
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceIncomplete")
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", 0)
	expectedEvents.AddClusterPodEvent(testCouchbase, "FailedOver", 0)

	// Add possible outcomes for this scenario into new event list
	multipleOutcomeEvents := e2eutil.EventValidator{}
	multipleOutcomeEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", 0)
	multipleOutcomeEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", clusterSize)

	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddAnyOfEvents(multipleOutcomeEvents)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, 50); err != nil {
		t.Logf("status: %+v", testCouchbase.Status)
		t.Fatal(err)
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
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
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	removePodMemberId := 1
	newPodMemberId := 2

	// create 2 node cluster with admin console
	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for nodeIndex := 0; nodeIndex < constants.Size2; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// pause operator
	testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = true
	})
	if err != nil {
		t.Fatal(err)
	}

	// create node port service for node 0
	clusterNodeName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	nodePortService := e2espec.NewNodePortService(f.Namespace)
	nodePortService.Spec.Selector["couchbase_node"] = clusterNodeName
	service, err := e2eutil.CreateService(targetKube.KubeClient, f.Namespace, nodePortService)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.DeleteService(targetKube.KubeClient, f.Namespace, service.Name, nil)

	serviceUrl, err := e2eutil.NodePortServiceClient(f.ApiServerHost(kubeName), service)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, targetKube.KubeClient, testCouchbase, []string{serviceUrl})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// remove node
	if err := e2eutil.RebalanceOutMember(t, client, testCouchbase.Name, testCouchbase.Namespace, removePodMemberId, true); err != nil {
		t.Fatal(err)
	}

	// resume operator
	testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = false
	})
	if err != nil {
		t.Fatal(err)
	}

	// expect an add member event to occur
	event := e2eutil.NewMemberAddEvent(testCouchbase, newPodMemberId)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 60); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberRemoveEvent(testCouchbase, removePodMemberId)
	expectedEvents.AddMemberAddEvent(testCouchbase, newPodMemberId)

	// cluster should also be balanced
	if err := e2eutil.WaitForClusterBalancedCondition(t, targetKube.CRClient, testCouchbase, 300); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// check that actual cluster size is only 2 nodes
	info, err := client.ClusterInfo()
	numNodes := len(info.Nodes)
	if numNodes != 2 {
		t.Fatalf("expected 2 nodes, found: %d", numNodes)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests basic MDS Scaling
// 1. Create 1 node cluster with data service only
// 2. Add query service to cluster (verify via rest call to cluster)
// 3. Add index service to cluster (verify via rest call to cluster)
// 4. Add search service to cluster (verify via rest call to cluster)
// 5. Remove search service from cluster (verify via rest call to cluster)
// 6. Remove index service from cluster (verify via rest call to cluster)
// 7. Remove query service from cluster (verify via rest call to cluster)
func TestBasicMDSScaling(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}
	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
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

	// adding query service
	t.Log("adding query service")
	newService := api.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_2",
		Services: api.ServiceList{api.QueryService},
	}
	testCouchbase, err = e2eutil.AddServices(targetKube.CRClient, testCouchbase, newService, constants.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	if _, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries10, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to add query service: %v", err)
	}

	// adding index service
	t.Log("adding index service")
	newService = api.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_3",
		Services: api.ServiceList{api.IndexService},
	}
	testCouchbase, err = e2eutil.AddServices(targetKube.CRClient, testCouchbase, newService, constants.Retries5)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	if _, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries5, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to add index service: %v", err)
	}

	// adding search service
	t.Log("adding search service")
	newService = api.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_4",
		Services: api.ServiceList{api.SearchService},
	}
	testCouchbase, err = e2eutil.AddServices(targetKube.CRClient, testCouchbase, newService, constants.Retries5)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	if _, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries5, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to add search service: %v", err)
	}

	// removing search service
	t.Log("removing search service")
	removeServiceName := "test_config_4"
	testCouchbase, err = e2eutil.RemoveServices(targetKube.CRClient, testCouchbase, removeServiceName, constants.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}

	if _, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries5, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to remove search service: %v", err)
	}

	// removing index service
	t.Log("removing index service")
	removeServiceName = "test_config_3"
	testCouchbase, err = e2eutil.RemoveServices(targetKube.CRClient, testCouchbase, removeServiceName, constants.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	if _, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries5, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to remove index service: %v", err)
	}

	// removing query service
	t.Log("removing query service")
	removeServiceName = "test_config_2"
	testCouchbase, err = e2eutil.RemoveServices(targetKube.CRClient, testCouchbase, removeServiceName, constants.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}
	if _, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  0,
		"Index": 0,
		"FTS":   0,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries5, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to remove query service: %v", err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests swapping nodes between services
// 1. Create 1 node cluster with data service only
// 2. Add query service to cluster, 1 node (verify via rest call to cluster)
// 3. Add index service to cluster, 1 node (verify via rest call to cluster)
// 4. Add search service to cluster, 2 nodes (verify via rest call to cluster)
// 5. Swap node from search service to index service (verify via rest call to cluster)
// 6. Swap node from index service to query service (verify via rest call to cluster)
// 7. Swap node from query service to data service (verify via rest call to cluster)
func TestSwapNodesBetweenServices(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
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

	// adding query service
	t.Log("adding query service")
	newService := api.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_2",
		Services: api.ServiceList{api.QueryService},
	}
	testCouchbase, err = e2eutil.AddServices(targetKube.CRClient, testCouchbase, newService, constants.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, constants.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add query service: %v", err)
	}

	// adding index service
	t.Log("adding index service")
	newService = api.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_3",
		Services: api.ServiceList{api.IndexService},
	}
	testCouchbase, err = e2eutil.AddServices(targetKube.CRClient, testCouchbase, newService, constants.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	err = e2eutil.VerifyServices(t, client, constants.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add index service: %v", err)
	}

	// adding search services
	t.Log("adding search service")
	newService = api.ServerConfig{
		Size:     constants.Size2,
		Name:     "test_config_4",
		Services: api.ServiceList{api.SearchService},
	}
	testCouchbase, err = e2eutil.AddServices(targetKube.CRClient, testCouchbase, newService, constants.Retries10)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}

	for memeberId := 3; memeberId <= 4; memeberId++ {
		event := e2eutil.NewMemberAddEvent(testCouchbase, memeberId)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
			t.Fatalf("Failed to add new member %d: %v", 1, err)
		}
		expectedEvents.AddMemberAddEvent(testCouchbase, memeberId)
	}

	if _, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase); err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   2,
	}
	err = e2eutil.VerifyServices(t, client, constants.Retries10, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to add search service: %v", err)
	}

	// swapping nodes search - 1 and index + 1
	t.Log("swaping nodes: test_config_4--, test_config_3++")
	swapMap := map[string]int{
		"test_config_4": 1,
		"test_config_3": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(targetKube.CRClient, testCouchbase, constants.Retries10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 2,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, constants.Retries20, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_4--, test_config_3++: %v", err)
	}

	// swapping nodes index - 1 and query + 1
	t.Log("swaping nodes: test_config_3--, test_config_2++")
	swapMap = map[string]int{
		"test_config_3": 1,
		"test_config_2": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(targetKube.CRClient, testCouchbase, constants.Retries10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 6)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 5)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  2,
		"Index": 1,
		"FTS":   1,
	}
	err = e2eutil.VerifyServices(t, client, constants.Retries20, serviceMap, e2eutil.NodeServicesVerifier)
	if err != nil {
		t.Fatalf("failed to scale test_config_3--, test_config_2++: %v", err)
	}

	// swapping nodes query - 1 and data + 1
	t.Log("swaping nodes: test_config_2--, test_config_1++")
	swapMap = map[string]int{
		"test_config_2": 1,
		"test_config_1": 2,
	}
	testCouchbase, err = e2eutil.ScaleServices(targetKube.CRClient, testCouchbase, constants.Retries10, swapMap)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}
	_, err = e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddMemberAddEvent(testCouchbase, 7)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 6)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	event := e2eutil.NewMemberRemoveEvent(testCouchbase, 6)
	err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	serviceMap = map[string]int{
		"Data":  2,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries10, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests creating a cluster without data service
// 1. Attempt to create cluster without data service
// 2. Verify that the cluster could not be created
func TestCreateClusterWithoutDataService(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"query", "index", "search"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	retries := &e2eutil.ClusterReadyRetries{
		Size:    constants.Retries5,
		Bucket:  constants.Retries1,
		Service: constants.Retries1,
	}
	if _, err := e2eutil.NewClusterMultiQuick(t, targetKube, f.Namespace, configMap, constants.AdminHidden, retries); err == nil {
		t.Fatalf("failed to reject cluster creation: %v", err)
	}
}

// Tests creating a cluster where the data service is the second service listed in the spec
// 1. Attempt to create a 2 node cluster with cluster spec order {[query,search,search], [data]}
// 2. Verify cluster was created via rest call
func TestCreateClusterDataServiceNotFirst(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"query", "index", "search"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(1, "test_config_2", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

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

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries5, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestRemoveLastDataService(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "query", "index"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(1, "test_config_2", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2}

	testCouchbase, err := e2eutil.NewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

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

	t.Log("removing last data service")
	removeServiceName := "test_config_2"
	testCouchbase, err = e2eutil.RemoveServices(targetKube.CRClient, testCouchbase, removeServiceName, constants.Retries10)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}

	_, err = e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, testCouchbase.Spec.TotalSize(), constants.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("cluster resize failed: %v", err)
	}

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries5, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to remove data service: %v", err)
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestManageMultipleClusters(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	t.Logf("Creating New Couchbase Cluster-1...\n")
	testCouchbase1, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithoutBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Creating New Couchbase Cluster-2...\n")
	testCouchbase2, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithoutBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Creating New Couchbase Cluster-3...\n")
	testCouchbase3, err := e2eutil.NewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithoutBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents1 := e2eutil.EventList{}
	expectedEvents1.AddAdminConsoleSvcCreateEvent(testCouchbase1)
	expectedEvents1.AddMemberAddEvent(testCouchbase1, 0)
	expectedEvents1.AddMemberAddEvent(testCouchbase1, 1)
	expectedEvents1.AddRebalanceStartedEvent(testCouchbase1)
	expectedEvents1.AddRebalanceCompletedEvent(testCouchbase1)

	expectedEvents2 := e2eutil.EventList{}
	expectedEvents2.AddAdminConsoleSvcCreateEvent(testCouchbase2)
	expectedEvents2.AddMemberAddEvent(testCouchbase2, 0)
	expectedEvents2.AddMemberAddEvent(testCouchbase2, 1)
	expectedEvents2.AddRebalanceStartedEvent(testCouchbase2)
	expectedEvents2.AddRebalanceCompletedEvent(testCouchbase2)

	expectedEvents3 := e2eutil.EventList{}
	expectedEvents3.AddAdminConsoleSvcCreateEvent(testCouchbase3)
	expectedEvents3.AddMemberAddEvent(testCouchbase3, 0)
	expectedEvents3.AddMemberAddEvent(testCouchbase3, 1)
	expectedEvents3.AddRebalanceStartedEvent(testCouchbase3)
	expectedEvents3.AddRebalanceCompletedEvent(testCouchbase3)

	// create connection to couchbase nodes
	client1, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase1)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo1, err := e2eutil.GetClusterInfo(t, client1, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo1)

	// create connection to couchbase nodes
	client2, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase2)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo2, err := e2eutil.GetClusterInfo(t, client2, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo2)

	// create connection to couchbase nodes
	client3, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase3)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo3, err := e2eutil.GetClusterInfo(t, client3, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo3)

	// add bucket to cluster 1
	bucketSetting1 := api.BucketConfig{
		BucketName:         "default1",
		BucketType:         pkg_constants.BucketTypeCouchbase,
		BucketMemoryQuota:  constants.Mem256Mb,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: constants.IndexReplicaEnabled,
	}
	bucketConfig1 := []api.BucketConfig{bucketSetting1}
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig1)
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig1 }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase1, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase1, constants.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Waiting For Bucket To Be Created \n")
	if err := e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{bucketSetting1.BucketName}, constants.Retries10, testCouchbase1); err != nil {
		t.Fatalf("failed to create bucket %v", err)
	}

	if err := e2eutil.VerifyBucketInfo(t, client1, constants.Retries5, "default1", "BucketMemoryQuota", strconv.Itoa(constants.Mem256Mb), e2eutil.BucketInfoVerifier); err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}
	expectedEvents1.AddBucketCreateEvent(testCouchbase1, "default1")

	// add bucket to cluster 2
	bucketSetting2 := api.BucketConfig{
		BucketName:         "default2",
		BucketType:         pkg_constants.BucketTypeCouchbase,
		BucketMemoryQuota:  constants.Mem256Mb,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: constants.IndexReplicaEnabled,
	}
	bucketConfig2 := []api.BucketConfig{bucketSetting2}
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig2)
	updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig2 }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase2, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase2, constants.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Waiting For Bucket To Be Created \n")
	if err := e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{bucketSetting2.BucketName}, constants.Retries10, testCouchbase2); err != nil {
		t.Fatalf("failed to create bucket %v", err)
	}

	if err := e2eutil.VerifyBucketInfo(t, client2, constants.Retries5, "default2", "BucketMemoryQuota", strconv.Itoa(constants.Mem256Mb), e2eutil.BucketInfoVerifier); err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}
	expectedEvents2.AddBucketCreateEvent(testCouchbase2, "default2")

	// add bucket to cluster 3
	bucketSetting3 := api.BucketConfig{
		BucketName:         "default3",
		BucketType:         pkg_constants.BucketTypeCouchbase,
		BucketMemoryQuota:  constants.Mem256Mb,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: constants.IndexReplicaEnabled,
	}
	bucketConfig3 := []api.BucketConfig{bucketSetting3}
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig3)
	updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig3 }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase3, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase3, constants.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Waiting For Bucket To Be Created \n")
	err = e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{bucketSetting3.BucketName}, constants.Retries10, testCouchbase3)
	if err != nil {
		t.Fatalf("failed to create bucket %v", err)
	}

	if err := e2eutil.VerifyBucketInfo(t, client3, constants.Retries5, "default3", "BucketMemoryQuota", strconv.Itoa(constants.Mem256Mb), e2eutil.BucketInfoVerifier); err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}

	expectedEvents3.AddBucketCreateEvent(testCouchbase3, "default3")

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase1, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase2, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase3, constants.Retries10); err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase1.Name, f.Namespace, expectedEvents1)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase2.Name, f.Namespace, expectedEvents2)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase3.Name, f.Namespace, expectedEvents3)
}
