package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestXDCRGlobalSettingsBeforeReplication ensures that global XDCR settings are reconciled before
// replications are processed, and that the expected event is emitted prior to replication creation.
func TestXDCRGlobalSettingsBeforeReplication(t *testing.T) {
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
	// We expect the operator to reconcile these settings and emit a ClusterSettingsEdited event
	// prior to creating the replication.
	// Note: only non-nil fields are sent, others remain untouched on Server.
	gs := couchbasev2.XDCRGlobalSettings{}
	// Set a few representative fields to ensure the settings payload is non-empty.
	docBatch := int32(128)
	gs.DocBatchSizeKb = &docBatch
	ct := "Auto"
	gs.CompressionType = &ct

	_ = e2eutil.MustPatchCluster(t, kubernetes1, sourceCluster, jsonpatch.NewPatchSet().
		Add("/spec/xdcr", couchbasev2.XDCR{Managed: true}).
		Add("/spec/xdcr/globalSettings", gs), time.Minute)

	// Wait for the global settings event before proceeding.
	e2eutil.MustObserveClusterEvent(t, kubernetes1, sourceCluster, k8sutil.ClusterSettingsEditedEvent("xdcr global settings", sourceCluster), 2*time.Minute)

	// Now establish the XDCR replication and verify ordering of events.
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
		// Allow further global settings edits after replication creation.
		eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
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
