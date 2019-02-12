package e2e

import (
	"fmt"
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
		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, 5*time.Minute)

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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
		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, 5*time.Minute)

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/DataServiceMemQuota", newDataServiceMemQuota), time.Minute)
	e2eutil.MustPatchCouchbaseInfo(t, client, jsonpatch.NewPatchSet().Test("/DataMemoryQuotaMB", newDataServiceMemQuota), time.Minute)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster indexServiceMemQuota
	newIndexServiceMemQuota := uint64(257)
	t.Log("Changing cluster index service mem quota")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexServiceMemQuota", newIndexServiceMemQuota), time.Minute)
	e2eutil.MustPatchCouchbaseInfo(t, client, jsonpatch.NewPatchSet().Test("/IndexMemoryQuotaMB", newIndexServiceMemQuota), time.Minute)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster searchServiceMemQuota
	newSearchServiceMemQuota := uint64(257)
	t.Log("Changing cluster search service mem quota")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/SearchServiceMemQuota", newSearchServiceMemQuota), time.Minute)
	e2eutil.MustPatchCouchbaseInfo(t, client, jsonpatch.NewPatchSet().Test("/SearchMemoryQuotaMB", newSearchServiceMemQuota), time.Minute)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "memory quota")

	// edit cluster autoFailoverTimeout
	newAutoFailoverTimeout := uint64(31)
	t.Log("Changing cluster autofailover timeout")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoFailoverTimeout", newAutoFailoverTimeout), time.Minute)
	e2eutil.MustPatchAutoFailoverInfo(t, client, jsonpatch.NewPatchSet().Test("/Timeout", newAutoFailoverTimeout), time.Minute)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "autofailover")

	// edit cluster indexStorageSetting
	// TODO: Need to make the API version typed on the underlying library
	newIndexStorageSetting := "plasma"
	t.Log("Changing cluster index storage setting")
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexStorageSetting", newIndexStorageSetting), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, client, jsonpatch.NewPatchSet().Test("/StorageMode", cbmgr.IndexStoragePlasma), time.Minute)
	expectedEvents.AddClusterSettingsEditedEvent(testCouchbase, "index service")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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
	e2eutil.MustWaitForFirstPodContainerWaiting(t, targetKube, testCouchbase, time.Minute, "ErrImagePull", "ImagePullBackOff")
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, 10*time.Minute)

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
	e2eutil.MustWaitForFirstPodContainerWaiting(t, targetKube, testCouchbase, time.Minute, "ErrImagePull", "ImagePullBackOff")
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, 10*time.Minute)

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
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize-1), 5*time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/Spec/ServerSettings/0/Pod"), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 0

	// Create the cluster
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden)

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
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize-1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 30*time.Second)
	e2eutil.MustWaitForRebalanceProgress(t, targetKube, testCouchbase, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName}},
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
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)

	// remove node
	e2eutil.MustEjectMember(t, targetKube, testCouchbase, removePodMemberId, 5*time.Minute)

	// resume operator
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)

	// expect an add member event to occur
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, newPodMemberId), time.Minute)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, removePodMemberId)
	expectedEvents.AddMemberAddEvent(testCouchbase, newPodMemberId)

	// cluster should also be balanced
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// check that actual cluster size is only 2 nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()
	info, err := client.ClusterInfo()
	if err != nil {
		e2eutil.Die(t, err)
	}
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
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, removeServiceName, 2*time.Minute)
	if err != nil {
		t.Fatalf("remove service failed: %v", err)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, removeServiceName, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, removeServiceName, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, 2*time.Minute)
	if err != nil {
		t.Fatalf("add service failed: %v", err)
	}

	for memeberId := 3; memeberId <= 4; memeberId++ {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memeberId), 2*time.Minute)
		expectedEvents.AddMemberAddEvent(testCouchbase, memeberId)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustScaleServices(t, targetKube, testCouchbase, swapMap, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustScaleServices(t, targetKube, testCouchbase, swapMap, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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
	testCouchbase = e2eutil.MustScaleServices(t, targetKube, testCouchbase, swapMap, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	expectedEvents.AddMemberAddEvent(testCouchbase, 7)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, 6)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberRemoveEvent(testCouchbase, 6), 5*time.Minute)

	serviceMap = map[string]int{
		"Data":  2,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	if err := e2eutil.VerifyServices(t, client, constants.Retries10, serviceMap, e2eutil.NodeServicesVerifier); err != nil {
		t.Fatalf("failed to scale test_config_2--, test_config_1++: %v", err)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
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
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, removeServiceName, 2*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

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

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// TestManageMultipleClusters tests that multiple clusters can be managed independantly
// within the same namespace.
func TestManageMultipleClusters(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size2

	// Create the clusters.
	clusters := []*api.CouchbaseCluster{}
	for index := 0; index < 3; index++ {
		clusters = append(clusters, e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminExposed))
	}

	for index, testCouchbase := range clusters {
		// When each cluster is ready create a uniquely named bucket and verify it appears in the
		// cluster status.
		bucketName := fmt.Sprintf("default%d", index)
		bucket := api.BucketConfig{
			BucketName:         bucketName,
			BucketType:         pkg_constants.BucketTypeCouchbase,
			BucketMemoryQuota:  constants.Mem256Mb,
			BucketReplicas:     pkg_constants.BucketReplicasOne,
			IoPriority:         pkg_constants.BucketIoPriorityHigh,
			EvictionPolicy:     pkg_constants.BucketEvictionPolicyFullEviction,
			ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
			EnableFlush:        constants.BucketFlushEnabled,
			EnableIndexReplica: constants.IndexReplicaEnabled,
		}
		testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/Spec/BucketSettings/-", bucket), time.Minute)
		testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test(fmt.Sprintf("/Status/Buckets/%s/BucketName", bucketName), bucketName), time.Minute)

		// Verify the bucket also appears in the Couchbase API.
		client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
		defer cleanup()

		e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", constants.Mem256Mb), time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

		// Check the events match what we expect:
		// * Cluster created
		// * Bucket created
		expectedEvents := []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
			e2eutil.ClusterCreateSequence(clusterSize),
			eventschema.Event{Reason: k8sutil.EventReasonBucketCreated, FuzzyMessage: bucketName},
		}
		ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
	}
}
