package e2e

import (
	"context"
	"strconv"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	pkgconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with no buckets
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions.
func TestResizeCluster(t *testing.T) {
	// Platform configuration.
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	serviceID := 0

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When the cluster is ready scale up to 3 nodes then down to 1 again.
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size3, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Test scaling a cluster with no buckets up and down
// 1. Create a one node cluster with one bucket
// 2. Resize the cluster  1 -> 2 -> 3 -> 2 -> 1
// 3. After each resize make sure the cluster is balanced and available
// 4. Check the events to make sure the operator took the correct actions.
func TestResizeClusterWithBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	serviceID := 0

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// When the cluster is ready scale up to 3 nodes then down to 1 again.
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size3, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size2, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeCluster(t, serviceID, constants.Size1, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests editing of cluster settings
// 1. Create a 1 node cluster
// 2. Change data service memory quota from 256 to 257 (verify via rest call to cluster)
// 3. Change index service memory quota from 256 to 257 (verify via rest call to cluster)
// 4. Change search service memory quota from 256 to 257 ( verify via rest call to cluster)
// 5. Change autofailover timeout from 30 to 31 ( verify via rest call to cluster).
func TestEditClusterSettings(t *testing.T) {
	// Platform configuration.
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	atLeast80, err := cluster.IsAtLeastVersion("8.0.0")
	if err != nil {
		e2eutil.Die(t, err)
	}

	// When ready change various cluster settings and ensure the changes are reflected
	// in the Couchbase API.
	patches := jsonpatch.NewPatchSet().
		Replace("/spec/cluster/dataServiceMemoryQuota", e2espec.NewResourceQuantityMi(257)).
		Replace("/spec/cluster/indexServiceMemoryQuota", e2espec.NewResourceQuantityMi(257)).
		Replace("/spec/cluster/searchServiceMemoryQuota", e2espec.NewResourceQuantityMi(257))
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patches, time.Minute)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.UpgradeFinishedEvent(cluster), 5*time.Minute)

	validationPatches := jsonpatch.NewPatchSet().
		Test("/DataMemoryQuotaMB", int64(257)).
		Test("/IndexMemoryQuotaMB", int64(257)).
		Test("/SearchMemoryQuotaMB", int64(257))
	e2eutil.MustPatchCouchbaseInfo(t, kubernetes, cluster, validationPatches, 5*time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", e2espec.NewDurationS(31)), time.Minute)
	e2eutil.MustPatchAutoFailoverInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/Timeout", int64(31)), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	patchCycles := 2

	if atLeast80 {
		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/allowFailoverEphemeralNoReplicas", true), time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

		allowFailoverEphemeralNoReplicas := true

		e2eutil.MustPatchAutoFailoverInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/AllowFailoverEphemeralNoReplicas", &allowFailoverEphemeralNoReplicas), 5*time.Minute)
		patchCycles++
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Upgraded due to new implicit memory requests
	// * All cluster edits are reported
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		eventschema.Repeat{Times: patchCycles, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestIndexerSettings changes the indexer settings and sees what happens.
func TestIndexerSettings(t *testing.T) {
	f := framework.Global

	// Platform configuration.
	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.  Use an empty indexer settings to get the DAC to fill in
	// the defaults for us.  Also remove the index service, the storage mode cannot be
	// modified if any nodes are running the index service (lol really?)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.Indexer = &couchbasev2.CouchbaseClusterIndexerSettings{}
	cluster.Spec.Servers[0].Services = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Edit the index settings so they aren't the defaults.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/threads", 8), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/Threads", 8), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/logLevel", couchbasev2.IndexerLogLevelDebug), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/LogLevel", couchbaseutil.IndexLogLevelDebug), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/maxRollbackPoints", 8), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/MaxRollbackPoints", 8), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/memorySnapshotInterval", &metav1.Duration{Duration: time.Second}), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/MemSnapInterval", 1000), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/stableSnapshotInterval", &metav1.Duration{Duration: time.Second}), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/StableSnapInterval", 1000), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/storageMode", couchbasev2.CouchbaseClusterIndexStorageSettingStandard), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/StorageMode", couchbaseutil.IndexStoragePlasma), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/numReplica", 1), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/NumberOfReplica", 1), time.Minute)
	// Check that we can set redistributeIndexes to both true and false
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/redistributeIndexes", true), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/RedistributeIndexes", true), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/redistributeIndexes", false), time.Minute)
	e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/RedistributeIndexes", false), time.Minute)

	patchCycles := 9

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.1.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		v := true

		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/enablePageBloomFilter", v), time.Minute)
		e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/EnablePageBloomFilter", &v), time.Minute)

		patchCycles++
	}

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		v := true

		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/enableShardAffinity", v), time.Minute)
		e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/EnableShardAffinity", &v), time.Minute)

		patchCycles++
	}

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "8.0.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		v := true

		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/deferBuild", v), time.Minute)
		e2eutil.MustPatchIndexSettingInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/DeferBuild", &v), time.Minute)

		patchCycles++
	}

	// Check that the user can see the cluster being edited.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: patchCycles, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestQuerySettings tests that query setting updates are accepted by the Couchbase API
// and the necessary events are raised.
func TestQuerySettings(t *testing.T) {
	f := framework.Global

	// Platform configuration.
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.Query = &couchbasev2.CouchbaseClusterQuerySettings{
		NumActiveTransactionRecords:  1024,
		CBOEnabled:                   true,
		CleanupClientAttemptsEnabled: true,
		CleanupLostAttemptsEnabled:   true,
		CompletedTrackingEnabled:     true,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Play with the settings and ensure they are as expected.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/temporarySpace", "2Gi"), time.Minute)
	tmpSpaceSize := resource.MustParse("2Gi")
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/TemporarySpaceSize", k8sutil.Megabytes(&tmpSpaceSize)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/temporarySpaceUnlimited", true), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/TemporarySpaceSize", int64(-1)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/backfillEnabled", false), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/TemporarySpaceSize", int64(0)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/pipelineBatch", 12), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/PipelineBatch", int32(12)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/pipelineCap", 1024), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/PipelineCap", int32(1024)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/scanCap", 1024), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/ScanCap", int32(1024)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/timeout", "1024ns"), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/Timeout", int64(1024)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/preparedLimit", 65536), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/PreparedLimit", int32(65536)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/completedLimit", 7000), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CompletedLimit", int32(7000)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/completedTrackingThreshold", e2espec.NewDurationS(1)).Replace("/spec/cluster/query/completedTrackingEnabled", true), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CompletedThreshold", int32(1000)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/completedTrackingAllRequests", true), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CompletedThreshold", int32(0)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/completedTrackingEnabled", false), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CompletedThreshold", int32(-1)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/logLevel", couchbasev2.QueryLogLevelWarn), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/LogLevel", couchbaseutil.QueryLogLevelWarn), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/maxParallelism", 3), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/MaxParallelism", int32(3)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/txTimeout", "10ms"), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/TxTimeout", "10000000ns"), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/memoryQuota", "4Mi"), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/MemoryQuota", int32(4)), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/cboEnabled", false), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CBOEnabled", false), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/cleanupClientAttemptsEnabled", false), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CleanupClientAttemptsEnabled", false), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/cleanupLostAttemptsEnabled", false), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CleanupLostAttemptsEnabled", false), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/cleanupWindow", "5us"), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CleanupWindow", "5000ns"), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/numActiveTransactionRecords", 512), time.Minute)
	e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/NumActiveTransactionRecords", int32(512)), time.Minute)

	patchCycles := 22
	upgradeCycles := 0

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		useReplica := couchbaseutil.QueryUseReplicaOn
		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/query/useReplica", true), time.Minute)
		e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/UseReplica", &useReplica), time.Minute)
		patchCycles++

		useReplica = couchbaseutil.QueryUseReplicaUnset
		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/useReplica", nil), time.Minute)
		e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/UseReplica", &useReplica), time.Minute)
		patchCycles++

		nodeQuotaVal := int32(11)
		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/nodeQuotaValPercent", 11), time.Minute)
		e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/NodeQuotaValPercent", &nodeQuotaVal), time.Minute)
		patchCycles++

		numCPU := int32(1)
		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/numCpus", 1), time.Minute)
		e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/NumCpus", &numCPU), time.Minute)
		patchCycles++

		compMaxPlanSize := int32(2883584)
		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/query/completedMaxPlanSize", "2883584"), time.Minute)
		e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/CompletedMaxPlanSize", &compMaxPlanSize), time.Minute)
		patchCycles++

		// QueryServiceMemQuota is a special case and actually causes a upgrade so give it more time
		nodeQuota := int32(700)
		cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/queryServiceMemoryQuota", "700Mi"), time.Minute)
		e2eutil.MustPatchQuerySettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/NodeQuota", &nodeQuota), 5*time.Minute)
		upgradeCycles++
	}

	// Check that the user can see the cluster being edited.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: patchCycles, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}}, // One optional one to make up for defaults
		eventschema.Repeat{Times: upgradeCycles, Validator: eventschema.Sequence{
			Validators: []eventschema.Validatable{
				RollingUpgradeSequence(clusterSize, clusterSize),
				eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
			},
		},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestInvalidBaseImage tests cluster with invalid image repos fail.
func TestInvalidBaseImage(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(1).Generate(kubernetes)
	cluster.Spec.Image = pkgconstants.InvalidBaseImage
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// When a pod has been created check it's event stream has an image pull error.  Also expect the
	// cluster to enter the failed state.
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, 0), 15*time.Minute)

	// Check the events match what we expect:
	// * First member creation failed
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestInvalidVersion tests cluster with invalid version repos fail.
func TestInvalidVersion(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(1).Generate(kubernetes)
	cluster.Spec.Image = "couchbase/server:enterprise-9.9.9"
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// When a pod has been created check it's event stream has an image pull error.  Also expect the
	// cluster to enter the failed state.
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, 0), 15*time.Minute)

	// Check the events match what we expect:
	// * First member creation failed
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// This is broken because the memory allocation stuff is inherently flawed.
	// There could be a prior cluster undergoing cleanup, thus making the allocations
	// at calculation time not tally with those at execution time.  Perhaps we need
	// to synchronize on namespace deletion??
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, kubernetes) + 1
	allocatableMemory := e2eutil.MustGetMinNodeMem(t, kubernetes)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(1).Generate(kubernetes)
	cluster.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(strconv.Itoa(int(allocatableMemory*0.7)) + "Mi"),
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Scales up the cluster exhausting memory, We expect the last node to not schedule. When the
	// policy is removed the last node will be created successfully and the rest of the cluster
	// upgraded to keep the spec synchronized.
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, clusterSize-2), 10*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/servers/0/resources"), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, clusterSize-1), 2*f.PodCreateTimeout)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Cluster recovers after node service goes down
// 1. Create 3 node cluster
// 2. stop couchbase on node-0000
// 3. Expect down node-0000 to be removed
// 4. Cluster should eventually reconcile as 3 nodes.
func TestNodeServiceDownRecovery(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3
	victimIndex := 0

	// Create the cluster
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Runtime configuration
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When ready kill the Couchbase Server process and await recovery
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustKillCouchbaseService(t, kubernetes, victimName, f.KubeType)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member goes down and fails over
	// * New member balanced in to replace the failed one
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Set{
			Validators: []eventschema.Validatable{
				eventschema.Optional{Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}}},
				eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName}},
			},
		},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestNodeServiceDownDuringRebalance tests killing a node during a scale down.
func TestNodeServiceDownDuringRebalance(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size5
	victimIndex := 0

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When healthy scale down the cluster and terminate the victim during the rebalance,
	// we expect the cluster to end up healthy.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize-1, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 30*time.Second)
	e2eutil.MustWaitForRebalanceProgress(t, kubernetes, cluster, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Scale down starts
	// * Rebalance incomplete
	// * Rebalance retried, down node removed.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Sequence{Validators: []eventschema.Validatable{eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete}}}},
		eventschema.Optional{Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}}},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName}},
		eventschema.AnyOf{Validators: []eventschema.Validatable{eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName}}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	removePodMemberID := 1
	clusterSize := 2

	// create 2 node cluster with admin console
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// pause operator
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	e2eutil.MustEjectMember(t, kubernetes, cluster, removePodMemberID, 5*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

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
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// adding query service
	t.Log("adding query service")

	newService := couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_2",
		Services: couchbasev2.ServiceList{couchbasev2.QueryService},
	}
	cluster = e2eutil.MustAddServices(t, kubernetes, cluster, newService, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// adding index service
	t.Log("adding index service")

	newService = couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_3",
		Services: couchbasev2.ServiceList{couchbasev2.IndexService},
	}
	cluster = e2eutil.MustAddServices(t, kubernetes, cluster, newService, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// adding search service
	t.Log("adding search service")

	newService = couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_4",
		Services: couchbasev2.ServiceList{couchbasev2.SearchService},
	}
	cluster = e2eutil.MustAddServices(t, kubernetes, cluster, newService, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// removing search service
	t.Log("removing search service")

	removeServiceName := "test_config_4"
	cluster = e2eutil.MustRemoveServices(t, kubernetes, cluster, removeServiceName, 2*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// removing index service
	t.Log("removing index service")

	removeServiceName = "test_config_3"
	cluster = e2eutil.MustRemoveServices(t, kubernetes, cluster, removeServiceName, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 0,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	// removing query service
	t.Log("removing query service")

	removeServiceName = "test_config_2"
	cluster = e2eutil.MustRemoveServices(t, kubernetes, cluster, removeServiceName, 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	serviceMap = map[string]int{
		"Data":  1,
		"N1QL":  0,
		"Index": 0,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * 3 members added
	// * 3 members removec
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: e2eutil.ClusterScaleUpSequence(1)},
		eventschema.Repeat{Times: 3, Validator: e2eutil.ClusterScaleDownSequence(1)},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// adding query service
	newService := couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_2",
		Services: couchbasev2.ServiceList{couchbasev2.QueryService},
	}
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/servers/-", newService), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// adding index service
	newService = couchbasev2.ServerConfig{
		Size:     constants.Size1,
		Name:     "test_config_3",
		Services: couchbasev2.ServiceList{couchbasev2.IndexService},
	}
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/servers/-", newService), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// adding search services
	newService = couchbasev2.ServerConfig{
		Size:     constants.Size2,
		Name:     "test_config_4",
		Services: couchbasev2.ServiceList{couchbasev2.SearchService},
	}
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/servers/-", newService), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// swapping nodes search - 1 and index + 1
	patchset := jsonpatch.NewPatchSet().Replace("/spec/servers/2/size", constants.Size2).Replace("/spec/servers/3/size", constants.Size1)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// swapping nodes index - 1 and query + 1
	patchset = jsonpatch.NewPatchSet().Replace("/spec/servers/1/size", constants.Size2).Replace("/spec/servers/2/size", constants.Size1)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// swapping nodes query - 1 and data + 1
	patchset = jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", constants.Size2).Replace("/spec/servers/1/size", constants.Size1)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * 2 server classes added with one node, 1 server class with 2 nodes
	// * Server class scaled up while another is scaled down 3 times
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 2, Validator: e2eutil.ClusterScaleUpSequence(1)},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
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
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests creating a cluster where the data service is the second service listed in the spec
// 1. Attempt to create a 2 node cluster with cluster spec order {[query,search,search], [data]}
// 2. Verify cluster was created via rest call.
func TestCreateClusterDataServiceNotFirst(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroup1Size := 1
	mdsGroup2Size := 1
	clusterSize := mdsGroup1Size + mdsGroup2Size

	// Create the cluster.
	cluster := clusterOptions().Generate(kubernetes)
	cluster.Spec.Servers = []couchbasev2.ServerConfig{
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
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   1,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestRemoveLastDataService(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroup1Size := 1
	mdsGroup2Size := 1
	clusterSize := mdsGroup1Size + mdsGroup2Size

	// Create the cluster.
	cluster := clusterOptions().Generate(kubernetes)
	cluster.Spec.Servers = []couchbasev2.ServerConfig{
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
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// create connection to couchbase nodes
	cluster = e2eutil.MustRemoveServices(t, kubernetes, cluster, "service2", 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	serviceMap := map[string]int{
		"Data":  1,
		"N1QL":  1,
		"Index": 1,
		"FTS":   0,
	}
	e2eutil.MustVerifyServices(t, kubernetes, cluster, time.Minute, serviceMap, e2eutil.NodeServicesVerifier)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scales down
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleDownSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestRemoveServerClassWithNodeService tests removing a server class with external features
// enabled.  Due to all the per-service filtering stuff that occurs we could get into trouble
// when the server class disappears.
func TestRemoveServerClassWithNodeService(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize1 := 1
	mdsGroupSize2 := 1
	clusterSize := mdsGroupSize1 + mdsGroupSize2

	// Create the cluster with two server classes, and exposed features.
	cluster := clusterOptions().WithGenericNetworking().Generate(kubernetes)
	cluster.Spec.Servers = []couchbasev2.ServerConfig{
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
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Remove a service and ensure things still work.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/servers/1"), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Service added
	// * Cluster created
	// * XDCR service created (admin, data, index)
	// * Server class successfully removed.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		eventschema.Repeat{Times: clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		e2eutil.ClusterScaleDownSequence(mdsGroupSize2),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestManageMultipleClusters tests that multiple clusters can be managed independently
// within the same namespace.
func TestManageMultipleClusters(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size2

	// Create the clusters.
	clusters := []*couchbasev2.CouchbaseCluster{}

	for index := 0; index < 3; index++ {
		clusters = append(clusters, clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes))
	}

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	for _, cluster := range clusters {
		// When each cluster is ready create a bucket and verify it appears in the
		// cluster status.
		e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/status/buckets/0/name", "default"), time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

		// Check the events match what we expect:
		// * Cluster created
		// * Bucket created
		expectedEvents := []eventschema.Validatable{
			e2eutil.ClusterCreateSequence(clusterSize),
			eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		}
		ValidateEvents(t, kubernetes, cluster, expectedEvents)
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

	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	after80, err := couchbaseutil.VersionAfter(cbVersion, "8.0.0")
	if err != nil {
		e2eutil.Die(t, err)
	}

	defaultSetting := intstr.Parse("default")
	if after80 {
		defaultSetting = intstr.Parse("balanced")
	}

	diskOptimized := intstr.Parse("disk_io_optimized")
	fixedVal := func(value int) *intstr.IntOrString {
		val := intstr.FromInt(value)
		return &val
	}

	e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, nil, nil, nil, nil, time.Minute)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{ReaderThreads: fixedVal(4)}), time.Minute)
	e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, fixedVal(4), &defaultSetting, nil, nil, time.Minute)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data/writerThreads", diskOptimized), time.Minute)
	e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, fixedVal(4), &diskOptimized, nil, nil, time.Minute)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{ReaderThreads: nil, WriterThreads: nil, NonIOThreads: nil, AuxIOThreads: nil}), time.Minute)
	e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, &defaultSetting, &defaultSetting, nil, nil, time.Minute)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{ReaderThreads: &defaultSetting, WriterThreads: &diskOptimized}), time.Minute)
	e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, &defaultSetting, &diskOptimized, nil, nil, time.Minute)

	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{ReaderThreads: &defaultSetting}), time.Minute)
	e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, &defaultSetting, &defaultSetting, nil, nil, time.Minute)

	numPatches := 5

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.1.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		nonIOThreads := 12
		auxIOThreads := 15

		e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data/nonIOThreads", nonIOThreads), time.Minute)
		e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, &defaultSetting, &defaultSetting, &nonIOThreads, nil, time.Minute)

		e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data/auxIOThreads", auxIOThreads), time.Minute)
		e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, &defaultSetting, &defaultSetting, &nonIOThreads, &auxIOThreads, time.Minute)

		numPatches += 2
	}

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		minReplicaCounts := 1
		cluster := e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data/minReplicasCount", minReplicaCounts), time.Minute)
		e2eutil.MustPatchDataServiceSettings(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/MinReplicasCount", minReplicaCounts), time.Minute)

		numPatches++
	}

	if after80 {
		e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data/diskUsageLimit", &couchbasev2.DiskUsageLimit{Enabled: util.BoolPtr(true), Percent: util.IntPtr(80)}), time.Minute)
		e2eutil.MustVerifyDiskUsageLimit(t, kubernetes, cluster, time.Minute, true, 80)
		// Check removing the field resets it to the default value
		e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/cluster/data/diskUsageLimit"), time.Minute)
		e2eutil.MustVerifyDiskUsageLimit(t, kubernetes, cluster, time.Minute, false, 85)

		e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{ReaderThreads: &defaultSetting, WriterThreads: &defaultSetting}), time.Minute)
		e2eutil.MustVerifyDataServerSettingsMemcachedThreads(t, kubernetes, cluster, &defaultSetting, &defaultSetting, nil, nil, time.Minute)

		numPatches += 3
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Settings updated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.RepeatAtLeast{
			Times:     numPatches,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestMovePod(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	var annotations = make(map[string]string)
	annotations["cao.couchbase.com/reschedule"] = "true"

	listOptions := metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + cluster.Name,
	}

	var podsList = corev1.PodList{}

	var podsCall = func() error {
		pods, err := kubernetes.KubeClient.CoreV1().Pods(cluster.Namespace).List(context.Background(), listOptions)
		if err != nil {
			return err
		}
		podsList = *pods
		return nil
	}

	err := retryutil.Retry(context.Background(), 5*time.Minute, podsCall)

	if err != nil {
		t.Error(err)
		t.Fail()
	}

	e2eutil.MustAddCustomAnnotationAndLabelsSinglePod(t, kubernetes, annotations, nil, podsList.Items[0])

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestServicelessClass(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0")

	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralAndServicelessTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize * 2),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
