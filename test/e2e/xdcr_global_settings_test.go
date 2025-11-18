package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestXDCRGlobalSettingsBeforeReplication ensures that global XDCR settings are reconciled before
// replications are processed, validates that all XDCR global settings can be set correctly,
// and that the expected events are emitted.
func TestXDCRGlobalSettingsBeforeReplication(t *testing.T) {
	f := framework.Global

	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := f.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().IstioDisabled()

	// Static configuration.
	clusterSize := 1
	numOfDocs := f.DocsCount

	// Create the clusters and buckets.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// Enable operator-managed XDCR with initial global settings.
	gs := couchbasev2.XDCRGlobalSettings{}
	docBatch := int32(128)
	gs.DocBatchSizeKb = &docBatch
	ct := "Auto"
	gs.CompressionType = &ct

	_ = e2eutil.MustPatchCluster(t, kubernetes1, sourceCluster, jsonpatch.NewPatchSet().
		Add("/spec/xdcr", couchbasev2.XDCR{Managed: true}).
		Add("/spec/xdcr/globalSettings", gs), time.Minute)

	// Wait for the global settings event before proceeding.
	e2eutil.MustObserveClusterEvent(t, kubernetes1, sourceCluster, k8sutil.ClusterSettingsEditedEvent("xdcr global settings", sourceCluster), 2*time.Minute)

	// Get Couchbase version for version-specific settings
	cbVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Define all expected values as variables for clarity and consistency
	checkpointInterval := int32(120)
	collectionsOSOMode := false
	compressionType := "None"
	desiredLatency := int32(100)
	docBatchSizeKb := int32(512)
	failureRestartInterval := int32(30)
	filterBypassExpiry := true
	filterBinary := true
	filterBypassUncommittedTxn := true
	filterDeletion := true
	filterExpiration := true
	jsFunctionTimeoutMs := int32(30000)
	logLevel := "Debug"
	networkUsageLimit := int32(100)
	optimisticReplicationThreshold := int32(512)
	priority := "Medium"
	retryOnRemoteAuthErr := false
	retryOnRemoteAuthErrMaxWaitSec := int32(180)
	sourceNozzlePerNode := int32(4)
	statsInterval := int32(2000)
	targetNozzlePerNode := int32(4)
	workerBatchSize := int32(1000)
	goGC := int32(80)
	goMaxProcs := int32(8)

	// Version-specific values
	mobile := "Off"
	hlvPruningWindowSec := int32(7200)

	// Create a comprehensive patch with all XDCR global settings
	settingsPatch := jsonpatch.NewPatchSet().
		// Global/per-replication settings
		Replace("/spec/xdcr/globalSettings/checkpointInterval", checkpointInterval).
		Replace("/spec/xdcr/globalSettings/collectionsOSOMode", collectionsOSOMode).
		Replace("/spec/xdcr/globalSettings/compressionType", compressionType).
		Replace("/spec/xdcr/globalSettings/desiredLatency", desiredLatency).
		Replace("/spec/xdcr/globalSettings/docBatchSizeKb", docBatchSizeKb).
		Replace("/spec/xdcr/globalSettings/failureRestartInterval", failureRestartInterval).
		Replace("/spec/xdcr/globalSettings/filterBypassExpiry", filterBypassExpiry).
		Replace("/spec/xdcr/globalSettings/filterBinary", filterBinary).
		Replace("/spec/xdcr/globalSettings/filterBypassUncommittedTxn", filterBypassUncommittedTxn).
		Replace("/spec/xdcr/globalSettings/filterDeletion", filterDeletion).
		Replace("/spec/xdcr/globalSettings/filterExpiration", filterExpiration).
		Replace("/spec/xdcr/globalSettings/jsFunctionTimeoutMs", jsFunctionTimeoutMs).
		Replace("/spec/xdcr/globalSettings/logLevel", logLevel).
		Replace("/spec/xdcr/globalSettings/networkUsageLimit", networkUsageLimit).
		Replace("/spec/xdcr/globalSettings/optimisticReplicationThreshold", optimisticReplicationThreshold).
		Replace("/spec/xdcr/globalSettings/priority", priority).
		Replace("/spec/xdcr/globalSettings/retryOnRemoteAuthErr", retryOnRemoteAuthErr).
		Replace("/spec/xdcr/globalSettings/retryOnRemoteAuthErrMaxWaitSec", retryOnRemoteAuthErrMaxWaitSec).
		Replace("/spec/xdcr/globalSettings/sourceNozzlePerNode", sourceNozzlePerNode).
		Replace("/spec/xdcr/globalSettings/statsInterval", statsInterval).
		Replace("/spec/xdcr/globalSettings/targetNozzlePerNode", targetNozzlePerNode).
		Replace("/spec/xdcr/globalSettings/workerBatchSize", workerBatchSize).
		// Global-only settings
		Replace("/spec/xdcr/globalSettings/goGC", goGC).
		Replace("/spec/xdcr/globalSettings/goMaxProcs", goMaxProcs)

	// Add version-specific settings
	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		// Mobile (Active/Off, default Off) - supported in 7.6.0+
		settingsPatch = settingsPatch.Replace("/spec/xdcr/globalSettings/mobile", mobile)
	}

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if !ok {
		// HlvPruningWindowSec (seconds, minimum 1) - supported in versions < 7.6.0
		settingsPatch = settingsPatch.Replace("/spec/xdcr/globalSettings/hlvPruningWindowSec", hlvPruningWindowSec)
	}

	// Apply all settings in a single batch operation
	sourceCluster = e2eutil.MustPatchCluster(t, kubernetes1, sourceCluster, settingsPatch, time.Minute)

	// Create validation patch with all expected values
	validationPatch := jsonpatch.NewPatchSet().
		// Global/per-replication settings
		Test("/CheckpointInterval", &checkpointInterval).
		Test("/CollectionsOSOMode", &collectionsOSOMode).
		Test("/CompressionType", &compressionType).
		Test("/DesiredLatency", &desiredLatency).
		Test("/DocBatchSizeKb", &docBatchSizeKb).
		Test("/FailureRestartInterval", &failureRestartInterval).
		Test("/FilterBypassExpiry", &filterBypassExpiry).
		Test("/FilterBinary", &filterBinary).
		Test("/FilterBypassUncommittedTxn", &filterBypassUncommittedTxn).
		Test("/FilterDeletion", &filterDeletion).
		Test("/FilterExpiration", &filterExpiration).
		Test("/JSFunctionTimeoutMs", &jsFunctionTimeoutMs).
		Test("/LogLevel", &logLevel).
		Test("/NetworkUsageLimit", &networkUsageLimit).
		Test("/OptimisticReplicationThreshold", &optimisticReplicationThreshold).
		Test("/Priority", &priority).
		Test("/RetryOnRemoteAuthErr", &retryOnRemoteAuthErr).
		Test("/RetryOnRemoteAuthErrMaxWaitSec", &retryOnRemoteAuthErrMaxWaitSec).
		Test("/SourceNozzlePerNode", &sourceNozzlePerNode).
		Test("/StatsInterval", &statsInterval).
		Test("/TargetNozzlePerNode", &targetNozzlePerNode).
		Test("/WorkerBatchSize", &workerBatchSize).
		// Global-only settings
		Test("/GoGC", &goGC).
		Test("/GoMaxProcs", &goMaxProcs)

	// Add version-specific validations
	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		validationPatch = validationPatch.Test("/Mobile", &mobile)
	}

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "7.6.0"); err != nil {
		e2eutil.Die(t, err)
	} else if !ok {
		validationPatch = validationPatch.Test("/HlvPruningWindowSec", &hlvPruningWindowSec)
	}

	if ok, err := couchbaseutil.VersionAfter(cbVersion, "8.0.0"); err != nil {
		e2eutil.Die(t, err)
	} else if ok {
		conflictLogging := &couchbaseutil.ConflictLoggingSettings{}
		validationPatch = validationPatch.Test("/ConflictLogging", conflictLogging)
	}

	// Validate all settings in one operation
	e2eutil.MustPatchXDCRGlobalSettings(t, kubernetes1, sourceCluster, validationPatch, time.Minute)

	// Now establish the XDCR replication and verify it works.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)

	// Assert event ordering on the source cluster: global settings edited occurs before replication added.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		// Allow one or more global settings edits before replication is established.
		eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents)
}

// TestXDCRGlobalSettingsDoNotUpdateExistingReplications ensures that changing global settings does not
// modify settings of existing replications.
func TestXDCRGlobalSettingsDoNotUpdateExistingReplications(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().IstioDisabled()

	// Static configuration.
	clusterSize := 1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters and buckets.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// Enable operator-managed XDCR and set a couple of global settings BEFORE establishing replication.
	gs := couchbasev2.XDCRGlobalSettings{}
	docBatch := int32(64)
	gs.DocBatchSizeKb = &docBatch
	_ = e2eutil.MustPatchCluster(t, kubernetes1, sourceCluster, jsonpatch.NewPatchSet().
		Add("/spec/xdcr", couchbasev2.XDCR{Managed: true}).
		Add("/spec/xdcr/globalSettings", gs), time.Minute)

	// Wait until global settings are applied (event observed) before creating replication.
	e2eutil.MustObserveClusterEvent(t, kubernetes1, sourceCluster, k8sutil.ClusterSettingsEditedEvent("xdcr global settings", sourceCluster), 2*time.Minute)

	// Establish replication.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())
	info := e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)
	replication = info.Replication

	// Explicitly set a per-replication setting (override) and verify it takes effect.
	// Pick docBatchSizeKb as it is easy to validate and present in both APIs.
	perRepDocBatch := int32(256)
	patch := jsonpatch.NewPatchSet().Replace("/DocBatchSizeKb", &perRepDocBatch)
	e2eutil.MustPatchReplicationSettings(t, kubernetes1, sourceCluster, replication, &sourceCluster.Spec.XDCR.RemoteClusters[0], patch, time.Minute)

	before := e2eutil.MustGetReplicationSettings(t, kubernetes1, sourceCluster, replication, &sourceCluster.Spec.XDCR.RemoteClusters[0])

	// Change global settings to a different value; existing replication should not change.
	newDocBatch := int32(128)
	newGS := couchbasev2.XDCRGlobalSettings{DocBatchSizeKb: &newDocBatch}
	_ = e2eutil.MustPatchCluster(t, kubernetes1, sourceCluster, jsonpatch.NewPatchSet().
		Add("/spec/xdcr/globalSettings", newGS), time.Minute)

	// Wait for global settings reconciliation (should NOT update the existing replication's override).
	e2eutil.MustObserveClusterEvent(t, kubernetes1, sourceCluster, k8sutil.ClusterSettingsEditedEvent("xdcr global settings", sourceCluster), 2*time.Minute)

	after := e2eutil.MustGetReplicationSettings(t, kubernetes1, sourceCluster, replication, &sourceCluster.Spec.XDCR.RemoteClusters[0])

	// Assert the per-replication override remains intact.
	if before.DocBatchSizeKb == nil || after.DocBatchSizeKb == nil {
		t.Fatalf("docBatchSizeKb not set on replication settings: before=%v after=%v", before.DocBatchSizeKb, after.DocBatchSizeKb)
	}

	if *before.DocBatchSizeKb != *after.DocBatchSizeKb {
		t.Fatalf("replication settings changed after global update: before=%d after=%d", *before.DocBatchSizeKb, *after.DocBatchSizeKb)
	}

	// Sanity - replication still functions.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)
}
