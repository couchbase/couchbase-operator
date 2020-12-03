package e2e

import (
	"strconv"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	pkgconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with no buckets
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions.
func TestResizeCluster(t *testing.T) {
	// Platform configuration.
	targetKube, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	serviceID := 0

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	// When the cluster is ready scale up to 3 nodes then down to 1 again.
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size3, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size1, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scales up and down
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with one bucket
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions.
func TestResizeClusterWithBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	serviceID := 0

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	// When the cluster is ready scale up to 3 nodes then down to 1 again.
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size3, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeCluster(t, serviceID, constants.Size1, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scales up and down
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleUpSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests editing of cluster settings
// 1. Create a 1 node cluster
// 2. Change data service memory quota from 256 to 257 (verify via rest call to cluster)
// 3. Change index service memory quota from 256 to 257 (verify via rest call to cluster)
// 4. Change search service memory quota from 256 to 257 ( verify via rest call to cluster)
// 5. Change autofailover timeout from 30 to 31 ( verify via rest call to cluster).
func TestEditClusterSettings(t *testing.T) {
	// Platform configuration.
	targetKube, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	// When ready change various cluster settings and ensure the changes are reflected
	// in the Couchbase API.
	patches := jsonpatch.NewPatchSet().
		Replace("/spec/cluster/dataServiceMemoryQuota", e2espec.NewResourceQuantityMi(257)).
		Replace("/spec/cluster/indexServiceMemoryQuota", e2espec.NewResourceQuantityMi(257)).
		Replace("/spec/cluster/searchServiceMemoryQuota", e2espec.NewResourceQuantityMi(257))
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, patches, time.Minute)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.UpgradeFinishedEvent(testCouchbase), 5*time.Minute)

	validationPatches := jsonpatch.NewPatchSet().
		Test("/DataMemoryQuotaMB", int64(257)).
		Test("/IndexMemoryQuotaMB", int64(257)).
		Test("/SearchMemoryQuotaMB", int64(257))
	e2eutil.MustPatchCouchbaseInfo(t, targetKube, testCouchbase, validationPatches, 5*time.Minute)

	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", e2espec.NewDurationS(31)), time.Minute)
	e2eutil.MustPatchAutoFailoverInfo(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Timeout", int64(31)), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgraded due to new implicit memory requests
	// * All cluster edits are reported
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestIndexerSettings changes the indexer settings and sees what happens.
func TestIndexerSettings(t *testing.T) {
	// Platform configuration.
	targetKube, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.  Use an empty inderer settings to get the DAC to fill in
	// the defaults for us.  Also remove the index service, the storage mode cannot be
	// modified if any nodes are running the index service (lol really?)
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.Indexer = &couchbasev2.CouchbaseClusterIndexerSettings{}
	testCouchbase.Spec.Servers[0].Services = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Edit the index settings so they aren't the defaults.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/threads", 8), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/Threads", 8), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/logLevel", couchbasev2.IndexerLogLevelDebug), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/LogLevel", couchbaseutil.IndexLogLevelDebug), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/maxRollbackPoints", 8), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/MaxRollbackPoints", 8), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/memorySnapshotInterval", &metav1.Duration{Duration: time.Second}), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/MemSnapInterval", 1000), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/stableSnapshotInterval", &metav1.Duration{Duration: time.Second}), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/StableSnapInterval", 1000), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/storageMode", couchbasev2.CouchbaseClusterIndexStorageSettingStandard), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/StorageMode", couchbaseutil.IndexStoragePlasma), time.Minute)

	// Check that the user can see the cluster being edited.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 6, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestQuerySettings tests that query setting updates are accepted by the Couchbase API
// and the necessary events are raised.
func TestQuerySettings(t *testing.T) {
	// Platform configuration.
	targetKube, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.Query = &couchbasev2.CouchbaseClusterQuerySettings{}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Play with the settings and ensure events are raised.
	op := e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, k8sutil.ClusterSettingsEditedEvent("query service", testCouchbase), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/temporarySpace", "2Gi"), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op)

	op = e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, k8sutil.ClusterSettingsEditedEvent("query service", testCouchbase), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/temporarySpaceUnlimited", true), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op)

	op = e2eutil.WaitForPendingClusterEvent(targetKube, testCouchbase, k8sutil.ClusterSettingsEditedEvent("query service", testCouchbase), time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/backfillEnabled", false), time.Minute)
	e2eutil.MustReceiveErrorValue(t, op)

	// Check that the user can see the cluster being edited.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestInvalidBaseImage tests cluster with invalid image repos fail.
func TestInvalidBaseImage(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(1).Generate(targetKube)
	testCouchbase.Spec.Image = pkgconstants.InvalidBaseImage
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, testCouchbase)

	// When a pod has been created check it's event stream has an image pull error.  Also expect the
	// cluster to enter the failed state.
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, 0), 15*time.Minute)

	// Check the events match what we expect:
	// * First member creation failed
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestInvalidVersion tests cluster with invalid version repos fail.
func TestInvalidVersion(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(1).Generate(targetKube)
	testCouchbase.Spec.Image = "couchbase/server:enterprise-9.9.9"
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, testCouchbase)

	// When a pod has been created check it's event stream has an image pull error.  Also expect the
	// cluster to enter the failed state.
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, 0), 15*time.Minute)

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

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// This is broken because the memory allocation stuff is inherently flawed.
	// There could be a prior cluster undergoing cleanup, thus making the allocations
	// at calculation time not tally with those at execution time.  Perhaps we need
	// to synchronize on namespace deletion??
	framework.Requires(t, targetKube).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube) + 1
	allocatableMemory := e2eutil.MustGetMinNodeMem(t, targetKube)

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(1).Generate(targetKube)
	testCouchbase.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(strconv.Itoa(int(allocatableMemory*0.7)) + "Mi"),
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Scales up the cluster exhausting memory, We expect the last node to not schedule. When the
	// policy is removed the last node will be created successfully and the rest of the cluster
	// upgraded to keep the spec synchronized.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize-2), 10*time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/spec/servers/0/resources"), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize-1), 2*f.PodCreateTimeout)
	e2eutil.MustWaitForClusterCondition(t, targetKube, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, testCouchbase, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 20*time.Minute)

	// Check the events match what we expect:
	// * All but one new nodes are created
	// * The last one fails creation
	// * A new node is created, after the memory constraints are removed
	// * Cluster rebalances
	expectedEvents := []eventschema.Validatable{
		eventschema.Repeat{Times: clusterSize - 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize - 1, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Cluster recovers after node service goes down
// 1. Create 3 node cluster
// 2. stop couchbase on node-0000
// 3. Expect down node-0000 to be removed
// 4. Cluster should eventually reconcile as 3 nodes.
func TestNodeServiceDownRecovery(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 0

	// Create the cluster
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	// Runtime configuration
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When ready kill the Couchbase Server process and await recovery
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustKillCouchbaseService(t, targetKube, victimName, f.KubeType)
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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size5
	victimIndex := 0

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

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
// 6. Verify cluster is balanced with 2 nodes.
func TestReplaceManuallyRemovedNode(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	removePodMemberID := 1
	clusterSize := 2

	// create 2 node cluster with admin console
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	// pause operator
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	e2eutil.MustEjectMember(t, targetKube, testCouchbase, removePodMemberID, 5*time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member removed
	// * Member replaced
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests basic MDS Scaling
// 1. Create 1 node cluster with data service only
// 2. Add query service to cluster (verify via rest call to cluster)
// 3. Add index service to cluster (verify via rest call to cluster)
// 4. Add search service to cluster (verify via rest call to cluster)
// 5. Remove search service from cluster (verify via rest call to cluster)
// 6. Remove index service from cluster (verify via rest call to cluster)
// 7. Remove query service from cluster (verify via rest call to cluster).
func TestBasicMDSScaling(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].Services = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// adding query service
	t.Log("adding query service")

	newService := couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_2",
		Services: couchbasev2.ServiceList{couchbasev2.QueryService},
	}
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// adding index service
	t.Log("adding index service")

	newService = couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_3",
		Services: couchbasev2.ServiceList{couchbasev2.IndexService},
	}
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// adding search service
	t.Log("adding search service")

	newService = couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_4",
		Services: couchbasev2.ServiceList{couchbasev2.SearchService},
	}
	testCouchbase = e2eutil.MustAddServices(t, targetKube, testCouchbase, newService, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// removing search service
	t.Log("removing search service")

	removeServiceName := "test_config_4"
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, removeServiceName, 2*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// removing index service
	t.Log("removing index service")

	removeServiceName = "test_config_3"
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, removeServiceName, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// removing query service
	t.Log("removing query service")

	removeServiceName = "test_config_2"
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, removeServiceName, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  0,
		"Index": 0,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * 3 members added
	// * 3 members removec
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: e2eutil.ClusterScaleUpSequence(1)},
		eventschema.Repeat{Times: 3, Validator: e2eutil.ClusterScaleDownSequence(1)},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests swapping nodes between services
// 1. Create 1 node cluster with data service only
// 2. Add query service to cluster, 1 node (verify via rest call to cluster)
// 3. Add index service to cluster, 1 node (verify via rest call to cluster)
// 4. Add search service to cluster, 2 nodes (verify via rest call to cluster)
// 5. Swap node from search service to index service (verify via rest call to cluster)
// 6. Swap node from index service to query service (verify via rest call to cluster)
// 7. Swap node from query service to data service (verify via rest call to cluster).
func TestSwapNodesBetweenServices(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].Services = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// adding query service
	newService := couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_2",
		Services: couchbasev2.ServiceList{couchbasev2.QueryService},
	}
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/spec/servers/-", newService), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// adding index service
	newService = couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_3",
		Services: couchbasev2.ServiceList{couchbasev2.IndexService},
	}
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/spec/servers/-", newService), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// adding search services
	newService = couchbasev2.ServerConfig{
		Size:     constants.Size2,
		Name:     "test_config_4",
		Services: couchbasev2.ServiceList{couchbasev2.SearchService},
	}
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/spec/servers/-", newService), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// swapping nodes search - 1 and index + 1
	patchset := jsonpatch.NewPatchSet().Replace("/spec/servers/2/size", constants.Size2).Replace("/spec/servers/3/size", constants.Size1)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, patchset, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// swapping nodes index - 1 and query + 1
	patchset = jsonpatch.NewPatchSet().Replace("/spec/servers/1/size", constants.Size2).Replace("/spec/servers/2/size", constants.Size1)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, patchset, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// swapping nodes query - 1 and data + 1
	patchset = jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", constants.Size2).Replace("/spec/servers/1/size", constants.Size1)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, patchset, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * 2 server classes added with one node, 1 server class with 2 nodes
	// * Server class scaled up while another is scaled down 3 times
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 2, Validator: e2eutil.ClusterScaleUpSequence(1)},
		e2eutil.ClusterScaleUpSequence(2),
		eventschema.Repeat{
			Times: 3,
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
					eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
				},
			},
		},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests creating a cluster where the data service is the second service listed in the spec
// 1. Attempt to create a 2 node cluster with cluster spec order {[query,search,search], [data]}
// 2. Verify cluster was created via rest call.
func TestCreateClusterDataServiceNotFirst(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroup1Size := 1
	mdsGroup2Size := 1
	clusterSize := mdsGroup1Size + mdsGroup2Size

	// Create the cluster.
	testCouchbase := clusterOptions().Generate(targetKube)
	testCouchbase.Spec.Servers = []couchbasev2.ServerConfig{
		{
			Name: "service1",
			Size: mdsGroup1Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.QueryService,
				couchbasev2.IndexService,
				couchbasev2.SearchService,
			},
		},
		{
			Name: "service2",
			Size: mdsGroup2Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
			},
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestRemoveLastDataService(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroup1Size := 1
	mdsGroup2Size := 1
	clusterSize := mdsGroup1Size + mdsGroup2Size

	// Create the cluster.
	testCouchbase := clusterOptions().Generate(targetKube)
	testCouchbase.Spec.Servers = []couchbasev2.ServerConfig{
		{
			Name: "service1",
			Size: mdsGroup1Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
				couchbasev2.QueryService,
			},
		},
		{
			Name: "service2",
			Size: mdsGroup2Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
			},
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// create connection to couchbase nodes
	testCouchbase = e2eutil.MustRemoveServices(t, targetKube, testCouchbase, "service2", 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, targetKube, testCouchbase, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scales down
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleDownSequence(1),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestRemoveServerClassWithNodeService tests removing a server class with external features
// enabled.  Due to all the per-service filtering stuff that occurs we could get into trouble
// when the server class disappears.
func TestRemoveServerClassWithNodeService(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize1 := 1
	mdsGroupSize2 := 1
	clusterSize := mdsGroupSize1 + mdsGroupSize2

	// Create the cluster with two server classes, and exposed features.
	testCouchbase := clusterOptions().Generate(targetKube)
	testCouchbase.Spec.Servers = []couchbasev2.ServerConfig{
		{
			Name: "data",
			Size: mdsGroupSize1,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
		},
		{
			Name: "query",
			Size: mdsGroupSize2,
			Services: couchbasev2.ServiceList{
				couchbasev2.QueryService,
			},
		},
	}
	testCouchbase.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureXDCR,
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Remove a service and ensure things still work.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/spec/servers/1"), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * XDCR service created (admin, data, index)
	// * Server class successfully removed.
	expectedEvents := []eventschema.Validatable{
		eventschema.Repeat{Times: clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		e2eutil.ClusterScaleDownSequence(mdsGroupSize2),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestManageMultipleClusters tests that multiple clusters can be managed independently
// within the same namespace.
func TestManageMultipleClusters(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size2

	// Create the clusters.
	clusters := []*couchbasev2.CouchbaseCluster{}

	for index := 0; index < 3; index++ {
		clusters = append(clusters, clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube))
	}

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	for _, testCouchbase := range clusters {
		// When each cluster is ready create a bucket and verify it appears in the
		// cluster status.
		e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Test("/status/buckets/0/name", "default"), time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

		// Check the events match what we expect:
		// * Cluster created
		// * Bucket created
		expectedEvents := []eventschema.Validatable{
			e2eutil.ClusterCreateSequence(clusterSize),
			eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		}
		ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
	}
}

// TestModifyDataServiceSettings checks that couchbase behaves as we expect (badly)
// and that it's updated when we tell it to.
func TestModifyDataServiceSettings(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Check that the starting state is correct (aka totally unknown).
	// The check that adding configuration shows up.
	e2eutil.MustVerifyReaderWriterThreads(t, kubernetes, cluster, 0, 0, time.Minute)
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{ReaderThreads: 6}), time.Minute)
	e2eutil.MustVerifyReaderWriterThreads(t, kubernetes, cluster, 6, 0, time.Minute)
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data/writerThreads", 9), time.Minute)
	e2eutil.MustVerifyReaderWriterThreads(t, kubernetes, cluster, 6, 9, time.Minute) // ... dude!

	// Check the events match what we expect:
	// * Cluster created
	// * Settings updated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
