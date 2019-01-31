package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	pkg_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/gocbmgr"
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
	targetKube := f.GetCluster(0)

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithoutBucket, constants.AdminHidden)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := constants.Size1
	for _, clusterSize := range clusterSizes {
		service := 0
		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, constants.Retries30)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminHidden)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	clusterSizes := []int{2, 3, 2, 1}
	prevClusterSize := constants.Size1
	for _, clusterSize := range clusterSizes {
		service := 0
		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, constants.Retries30)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

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

	// edit cluster dataServiceMemQuota
	newDataServiceMemQuota := uint64(257)
	t.Log("Changing cluster data service mem quota")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/DataServiceMemQuota", newDataServiceMemQuota), constants.Retries5)
	e2eutil.MustPatchCouchbaseInfo(t, client, jsonpatch.NewPatchSet().Test("/DataMemoryQuotaMB", newDataServiceMemQuota), constants.Retries5)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster indexServiceMemQuota
	newIndexServiceMemQuota := uint64(257)
	t.Log("Changing cluster index service mem quota")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexServiceMemQuota", newIndexServiceMemQuota), constants.Retries5)
	e2eutil.MustPatchCouchbaseInfo(t, client, jsonpatch.NewPatchSet().Test("/IndexMemoryQuotaMB", newIndexServiceMemQuota), constants.Retries5)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster searchServiceMemQuota
	newSearchServiceMemQuota := uint64(257)
	t.Log("Changing cluster search service mem quota")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/SearchServiceMemQuota", newSearchServiceMemQuota), constants.Retries5)
	e2eutil.MustPatchCouchbaseInfo(t, client, jsonpatch.NewPatchSet().Test("/SearchMemoryQuotaMB", newSearchServiceMemQuota), constants.Retries5)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster autoFailoverTimeout
	newAutoFailoverTimeout := uint64(31)
	t.Log("Changing cluster autofailover timeout")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoFailoverTimeout", newAutoFailoverTimeout), constants.Retries5)
	e2eutil.MustPatchAutoFailoverInfo(t, client, jsonpatch.NewPatchSet().Test("/Timeout", newAutoFailoverTimeout), constants.Retries5)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "autofailover")

	// edit cluster indexStorageSetting
	// TODO: Need to make the API version typed on the underlying library
	newIndexStorageSetting := "plasma"
	t.Log("Changing cluster index storage setting")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexStorageSetting", newIndexStorageSetting), constants.Retries5)
	e2eutil.MustPatchIndexSettingInfo(t, client, jsonpatch.NewPatchSet().Test("/StorageMode", cbmgr.IndexStoragePlasma), constants.Retries5)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "index service")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)

}

// TestInvalidBaseImage tests cluster with invalid image repos fail
func TestInvalidBaseImage(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterMultiNoWait(t, targetKube, f.Namespace, configMap)

	// When a pod has been created check it's event stream has an image pull error.  Also expect the
	// cluster to enter the failed state.
	e2eutil.MustWaitForFirstPodContainerWaiting(t, targetKube, testCouchbase, constants.Retries10, "ErrImagePull", "ImagePullBackOff")
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, constants.Retries120)

	// Check the events match what we expect:
	// * First member creation failed
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestInvalidBaseImage tests cluster with invalid version repos fail
func TestInvalidVersion(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterMultiNoWait(t, targetKube, f.Namespace, configMap)

	// When a pod has been created check it's event stream has an image pull error.  Also expect the
	// cluster to enter the failed state.
	e2eutil.MustWaitForFirstPodContainerWaiting(t, targetKube, testCouchbase, constants.Retries10, "ErrImagePull", "ImagePullBackOff")
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, constants.Retries60)

	// Check the events match what we expect:
	// * First member creation failed
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestNodeUnschedulable tests running out of allocatable memory and then updates
// to the pod memory requests are reflected in new pods.
//
// NOTE: The memory policy applies to the operator template only, and pods
// created from it.  Changes will not and cannot be reflected on the running
// pod.
func TestNodeUnschedulable(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube) + 1
	allocatableMemory := e2eutil.MustGetMinNodeMem(t, targetKube)

	// Create the cluster.
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"resourceMemRequest": strconv.Itoa(int(allocatableMemory * 0.7)),
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, false)

	// Scal up the cluster enhauting memory, We expect the last node to not schedule. When the
	// policy is removed the last node will be created successfully and the cluster rebalanced
	// into a healthy state.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize-1), 300)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/Spec/ServerSettings/0/Pod"), constants.Retries5)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries20)

	// Check the events match what we expect:
	// * All but one new nodes are created
	// * The last one fails creation
	// * A new node is created, after the memory constaints are removed
	// * Cluster rebalances
	expectedEvents := []eventschema.Validatable{
		eventschema.Repeat{Times: clusterSize - 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	targetKube := f.GetCluster(0)

	// create 3 node cluster
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithBucket, constants.AdminHidden)

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
		e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "mv /etc/service/couchbase-server /tmp/")
	} else {
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, memberName, f.Namespace); err != nil {
			t.Fatal(err)
		}
	}

	expectedEvents.AddMemberDownEvent(testCouchbase, 0)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, 0)

	// expect down node to be removed from cluster
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberRemoveEvent(testCouchbase, removePodMemberId), 300)

	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, removePodMemberId)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// healthy 3 node cluster
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries30)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// TestNodeServiceDownDuringRebalance tests killing a node during a scale down.
func TestNodeServiceDownDuringRebalance(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size5
	victimIndex := 0

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When healthy scale down the cluster and terminate the victim during the rebalance,
	// we expect the cluster to end up healthy.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 30)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize-1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 30)
	e2eutil.MustWaitForRebalanceProgress(t, targetKube, testCouchbase, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 300)

	// Check the events match what we expect:
	// * Cluster created
	// * Scale down starts
	// * Rebalance incomplete
	// * Rebalance retried, down node removed.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	targetKube := f.GetCluster(0)
	removePodMemberId := 1
	newPodMemberId := 2

	// create 2 node cluster with admin console
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for nodeIndex := 0; nodeIndex < constants.Size2; nodeIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, nodeIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// pause operator
	testCouchbase = e2eutil.MustUpdateCluster(t, targetKube.CRClient, testCouchbase, constants.Retries5, func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = true
	})

	// create node port service for node 0
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	// remove node
	if err := e2eutil.RebalanceOutMember(t, client, testCouchbase.Name, testCouchbase.Namespace, removePodMemberId, true); err != nil {
		t.Fatal(err)
	}

	// resume operator
	var err error
	testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = false
	})
	if err != nil {
		t.Fatal(err)
	}

	// expect an add member event to occur
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, newPodMemberId), 60)
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
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}
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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data"})

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

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
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memeberId), 120)
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

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberRemoveEvent(testCouchbase, 6), 300)

	serviceMap = map[string]int{
		"Data":  2,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries10, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Tests creating a cluster without data service
// 1. Attempt to create cluster without data service
// 2. Verify that the cluster could not be created
func TestCreateClusterWithoutDataService(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

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
	targetKube := f.GetCluster(0)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"query", "index", "search"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(1, "test_config_2", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestRemoveLastDataService(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(1, "test_config_1", []string{"data", "query", "index"})
	serviceConfig2 := e2eutil.GetServiceConfigMap(1, "test_config_2", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestManageMultipleClusters(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	t.Logf("Creating New Couchbase Cluster-1...\n")
	testCouchbase1 := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithoutBucket, constants.AdminExposed)

	t.Logf("Creating New Couchbase Cluster-2...\n")
	testCouchbase2 := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithoutBucket, constants.AdminExposed)

	t.Logf("Creating New Couchbase Cluster-3...\n")
	testCouchbase3 := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size2, constants.WithoutBucket, constants.AdminExposed)

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
	client1, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase1)
	defer cleanup()

	clusterInfo1, err := e2eutil.GetClusterInfo(t, client1, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo1)

	// create connection to couchbase nodes
	client2, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase2)
	defer cleanup()

	clusterInfo2, err := e2eutil.GetClusterInfo(t, client2, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo2)

	// create connection to couchbase nodes
	client3, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase3)
	defer cleanup()

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase1, constants.Retries10)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase2, constants.Retries10)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase3, constants.Retries10)

	ValidateClusterEvents(t, targetKube, testCouchbase1.Name, f.Namespace, expectedEvents1)
	ValidateClusterEvents(t, targetKube, testCouchbase2.Name, f.Namespace, expectedEvents2)
	ValidateClusterEvents(t, targetKube, testCouchbase3.Name, f.Namespace, expectedEvents3)
}
