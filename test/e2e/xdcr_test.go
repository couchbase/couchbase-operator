package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mustCreateXDCRBuckets creates default buckets in the source and target clusters, ensuring
// we don't redefine if the same cluster is used for source and target couchbase instances.
func mustCreateXDCRBucketsWithScopes(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, scopes ...*couchbasev2.CouchbaseScope) metav1.Object {
	bucket := e2eutil.MustGetBucket(framework.Global.BucketType, framework.Global.CompressionMode)

	for _, scope := range scopes {
		e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	}

	e2eutil.MustNewBucket(t, kubernetes1, bucket)

	if kubernetes1.Config.Host != kubernetes2.Config.Host || kubernetes1.Namespace != kubernetes2.Namespace {
		e2eutil.MustNewBucket(t, kubernetes2, bucket)
	}

	return bucket
}

func mustCreateXDCRBuckets(t *testing.T, kubernetes1, kubernetes2 *types.Cluster) metav1.Object {
	return mustCreateXDCRBucketsWithScopes(t, kubernetes1, kubernetes2)
}

// ejectAllXDCRNodes removes each node from the cluster sequentially.
func ejectAllXDCRNodes(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) eventschema.Validatable {
	for i := 0; i < couchbase.Spec.TotalSize(); i++ {
		e2eutil.MustEjectMember(t, k8s, couchbase, i, 5*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, k8s, couchbase, e2eutil.RebalanceCompletedEvent(couchbase), 5*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, k8s, couchbase, 2*time.Minute)
	}

	return eventschema.Repeat{
		Times: couchbase.Spec.TotalSize(),
		Validator: eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
				// These tests fail intermitently due to something going screwy with server,
				// however it seems to rebalance again and save itself.  Would be nice to remove
				// this and run the test in a loop until it does fail and gather server logs...
				eventschema.Optional{
					Validator: eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
						},
					},
				},
			},
		},
	}
}

// killAllXDCRNodes kills each node from the cluster sequentially.
func killAllXDCRNodes(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) eventschema.Validatable {
	for i := 0; i < couchbase.Spec.TotalSize(); i++ {
		e2eutil.MustKillPodForMember(t, k8s, couchbase, i, false)
		e2eutil.MustWaitForClusterEvent(t, k8s, couchbase, e2eutil.RebalanceCompletedEvent(couchbase), 5*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, k8s, couchbase, 2*time.Minute)
	}

	return eventschema.Repeat{
		Times:     couchbase.Spec.TotalSize(),
		Validator: e2eutil.PodDownFailoverRecoverySequence(),
	}
}

// scaleDownXDCRCluster scales down to the specified size.
func scaleDownXDCRCluster(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, size int) eventschema.Validatable {
	e2eutil.MustResizeCluster(t, 0, size, k8s, couchbase, 5*time.Minute)

	return e2eutil.ClusterScaleDownSequence(couchbase.Spec.TotalSize() - size)
}

// xdcrCluster defines the cluster to operate on (source or target).
type xdcrCluster int

const (
	xdcrClusterSource xdcrCluster = iota
	xdcrClusterTarget xdcrCluster = iota
)

// xdcrOperation defines the operation to perform on the cluster (eject, delete or scale down nodes).
type xdcrOperation int

const (
	xdcrOperationEject     xdcrOperation = iota
	xdcrOperationDelete    xdcrOperation = iota
	xdcrOperationScaleDown xdcrOperation = iota
)

func createXDCRClusters(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy, clusterSize int) (*couchbasev2.CouchbaseCluster, *couchbasev2.CouchbaseCluster, metav1.Object) {
	// Create the clusters.
	// The kubernetes1 cluster optionally uses a custom DNS service to address the kubernetes2 cluster.
	// The kubernetes2 cluster optionally has TLS set.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithDNS(dns).MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(tls, policy).MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationWithTLS(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication, tls)

	return sourceCluster, targetCluster, bucket
}

// xdcrClusterRemoveNode removes nodes from the selected cluster in numerous
// nefarious ways.
func xdcrClusterRemoveNode(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, cluster xdcrCluster, operation xdcrOperation) {
	// Skip the test if it utilises GenericNetworking with Istio enabled.
	framework.Requires(t, kubernetes1).IstioDisabled()

	// Static configuration.
	clusterSize := constants.Size3
	scaleDownSize := constants.Size1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection and verify correct replication...
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), framework.Global.DocsCount).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)

	// ...choose the correct victim cluster...
	var kubernetes *types.Cluster

	var targetCouchbase *couchbasev2.CouchbaseCluster

	switch cluster {
	case xdcrClusterSource:
		kubernetes = kubernetes1
		targetCouchbase = sourceCluster
	case xdcrClusterTarget:
		kubernetes = kubernetes2
		targetCouchbase = targetCluster
	}

	// ...and perform the necessary operation on it...
	var schema eventschema.Validatable

	switch operation {
	case xdcrOperationEject:
		schema = ejectAllXDCRNodes(t, kubernetes, targetCouchbase)
	case xdcrOperationDelete:
		schema = killAllXDCRNodes(t, kubernetes, targetCouchbase)
	case xdcrOperationScaleDown:
		schema = scaleDownXDCRCluster(t, kubernetes, targetCouchbase, scaleDownSize)
	}

	// ...before finally creating more documents and verifying replication still works.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	// * any cluster/operation specific things happened as expected
	switch cluster {
	case xdcrClusterSource:
		expectedEvents1 = append(expectedEvents1, schema)
	case xdcrClusterTarget:
		expectedEvents2 = append(expectedEvents2, schema)
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// testCreateXDCRCluster tests clusters creation using any combination of DNS/TLS.
func testCreateXDCRCluster(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := framework.Global.DocsCount

	sourceCluster, targetCluster, bucket := createXDCRClusters(t, kubernetes1, kubernetes2, dns, tls, policy, clusterSize)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRCreateClusterLocal tests establishing an XDCR connection within the same cluster.
func TestXDCRCreateClusterLocal(t *testing.T) {
	kubernetes1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	testCreateXDCRCluster(t, kubernetes1, kubernetes1, nil, nil, nil)
}

// TestXDCRCreateClusterLocalTLS tests establishing a TLS XDCR connection within the same cluster.
func TestXDCRCreateClusterLocalTLS(t *testing.T) {
	kubernetes1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes1, &e2eutil.TLSOpts{})

	testCreateXDCRCluster(t, kubernetes1, kubernetes1, nil, tls, nil)
}

// TestXDCRCreateClusterLocalMutualTLS tests establishing an mTLS XDCR connection within the same cluster.
func TestXDCRCreateClusterLocalMutualTLS(t *testing.T) {
	kubernetes1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes1, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testCreateXDCRCluster(t, kubernetes1, kubernetes1, nil, tls, &policy)
}

// TestXDCRCreateClusterLocalMandatoryMutualTLS tests establishing a mandatory mTLS TLS XDCR connection within the same cluster.
func TestXDCRCreateClusterLocalMandatoryMutualTLS(t *testing.T) {
	kubernetes1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes1, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testCreateXDCRCluster(t, kubernetes1, kubernetes1, nil, tls, &policy)
}

// TestXDCRCreateClusterRemote tests establishing an XDCR connection to a kubernetes2 cluster.
func TestXDCRCreateClusterRemote(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	testCreateXDCRCluster(t, kubernetes1, kubernetes2, dns, nil, nil)
}

// TestXDCRCreateClusterRemoteTLS tests establishing a TLS XDCR connection to a kubernetes2 cluster.
func TestXDCRCreateClusterRemoteTLS(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes2, &e2eutil.TLSOpts{})

	testCreateXDCRCluster(t, kubernetes1, kubernetes2, dns, tls, nil)
}

// TestXDCRCreateClusterRemoteMutualTLS tests establishing an mTLS XDCR connection to a kubernetes2 cluster.
func TestXDCRCreateClusterRemoteMutualTLS(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes2, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testCreateXDCRCluster(t, kubernetes1, kubernetes2, dns, tls, &policy)
}

// TestXDCRCreateClusterRemoteMandatoryMutualTLS tests establishing a mandatory mTLS TLS XDCR connection to a kubernetes2 cluster.
func TestXDCRCreateClusterRemoteMandatoryMutualTLS(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes2, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testCreateXDCRCluster(t, kubernetes1, kubernetes2, dns, tls, &policy)
}

// TestXDCRCreateCluster tests establishing an XDCR connection.
func TestXDCRCreateCluster(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRPauseReplication tests a replication can be paused and restarted again.
func TestXDCRPauseReplication(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := 1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.  Pause the replication and add in
	// some new documents, these shouldn't be replicated until we remove the
	// pause.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	info := e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)
	replication = info.Replication

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, time.Minute)
	replication = e2eutil.MustPatchReplication(t, kubernetes1, replication, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	time.Sleep(time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	time.Sleep(time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, time.Minute)
	_ = e2eutil.MustPatchReplication(t, kubernetes1, replication, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source cluster paused and unpaused
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRSourceNodeDown tests killing a node in the source cluster of an
// XDCR replication.
func TestXDCRSourceNodeDown(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := 3
	nodeToKill := 1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and verify they
	// have been replicated.  Kill a pod in the source cluster.  Add some more documents.
	// When healthy verify the documents have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes1, sourceCluster, nodeToKill, true)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes1, sourceCluster, e2eutil.RebalanceStartedEvent(sourceCluster), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes1, sourceCluster, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source recovers after a pod is taken down
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRSourceNodeAdd tests adding a node into the source cluster of an XDCR
// replication.
func TestXDCRSourceNodeAdd(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := constants.Size1
	clusterScaledSize := constants.Size3
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Scale up the source cluster, add some more documents
	// and ensure they are (eventually - after 5 whole minutes) replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)
	sourceCluster = e2eutil.MustResizeCluster(t, 0, constants.Size3, kubernetes1, sourceCluster, 5*time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source cluster is scaled up
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		e2eutil.ClusterScaleUpSequence(clusterScaledSize - clusterSize),
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRTargetNodeServiceDelete tests deleting the node-port services of the
// target cluster required by an XDCR replication.
func TestXDCRTargetNodeServiceDelete(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := constants.Size1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Delete the pod services in the destination cluster
	// to circuit break the connection.  Add some more documents and verify they are
	// replicated when the connection is reestablished.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)
	e2eutil.MustDeletePodServices(t, kubernetes2, targetCluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source cluster is scaled up
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the source bucket cluster are rebalanced out.
// one by one until there is only one node in cluster.
func TestXDCRRebalanceOutSourceClusterNodes(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1")

	xdcrClusterRemoveNode(t, kubernetes1, kubernetes2, xdcrClusterSource, xdcrOperationEject)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the destination bucket cluster are rebalanced out.
// one by one until there is only one node in cluster.
func TestXDCRRebalanceOutTargetClusterNodes(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1")

	xdcrClusterRemoveNode(t, kubernetes1, kubernetes2, xdcrClusterTarget, xdcrOperationEject)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the source bucket cluster are killed one by one.
// At the end all nodes are replaced by new nodes in the cluster.
func TestXDCRRemoveSourceClusterNodes(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, kubernetes1, kubernetes2, xdcrClusterSource, xdcrOperationDelete)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the destination bucket cluster are killed one by one.
// At the end all nodes are replaced by new nodes in the cluster.
func TestXDCRRemoveTargetClusterNodes(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, kubernetes1, kubernetes2, xdcrClusterTarget, xdcrOperationDelete)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes of source bucket cluster is resized to single node cluster.
func TestXDCRResizedOutSourceClusterNodes(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, kubernetes1, kubernetes2, xdcrClusterSource, xdcrOperationScaleDown)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes of destination bucket cluster is resized to single node cluster.
func TestXDCRResizedOutTargetClusterNodes(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, kubernetes1, kubernetes2, xdcrClusterTarget, xdcrOperationScaleDown)
}

// TestXDCRDeleteReplication tests a replication can be deleted.
func TestXDCRDeleteReplication(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := 1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	info := e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)
	replication = info.Replication

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, time.Minute)

	// Now we delete the replication, add some documents in the source bucket and
	// verify that the doc count of destination bucket is same as its old value
	e2eutil.MustDeleteXDCRReplication(t, kubernetes1, sourceCluster, targetCluster, replication, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes1, sourceCluster, 2*time.Minute)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	time.Sleep(time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes1, sourceCluster, bucket.GetName(), 2*numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Replication from Source to destination deleted
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationRemoved},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRFilterExp checks that the filter expressions when applied to XDCR cluster
// is behaving as expected.
func TestXDCRFilterExp(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := 1
	numOfDocs := f.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection with the specified filter expression
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())
	replication.Spec.FilterExpression = `REGEXP_CONTAINS(META().id, "^doc.*$")`

	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	// Insert documents with DocId following the template: "random%d"
	// which won't be matched by the filter and will not get replicated.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 0, time.Minute)

	// Insert documents with DocId following the template: "`doc`%d"
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).WithPrefix("doc").MustCreate(t, kubernetes1, sourceCluster)

	// Now we check that the new documents inserted in the source bucket is replicated
	// since the filter expression applied allows documents with document id starting with `doc` to get replicated.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes1, sourceCluster, bucket.GetName(), 2*numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRRotatePassword tests that when the password is rotated on the XDCR target cluster
// it can also be updated on the source cluster without having to teardown and recreate
// the connection.
func TestXDCRRotatePassword(t *testing.T) {
	// Platform configuration.
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket().NotVersion("6.5.1").IstioDisabled()

	// Static configuration.
	clusterSize := 1

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, kubernetes1, kubernetes2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes1)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes2)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes1, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes2, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	info := e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes1, kubernetes2, sourceCluster, targetCluster, replication)

	numOfDocs := framework.Global.DocsCount
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)

	e2eutil.MustRotateClusterPassword(t, kubernetes2)
	e2eutil.MustRotateXDCRReplicationPassword(t, kubernetes1, kubernetes2, info.SecretName)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterUpdated},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

func testXDCRRotateClient(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	clusterSize := 1

	sourceCluster, targetCluster, bucket := createXDCRClusters(t, kubernetes1, kubernetes2, dns, tls, policy, clusterSize)

	numOfDocs := framework.Global.DocsCount

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)

	e2eutil.MustRotateClientCertificate(t, tls)
	e2eutil.MustObserveClusterEvent(t, kubernetes2, targetCluster, k8sutil.ClientTLSUpdatedEvent(targetCluster, k8sutil.ClientTLSUpdateReasonUpdateClientAuth), 5*time.Minute)
	e2eutil.MustRotateXDCRReplicationTLS(t, kubernetes1, targetCluster, tls)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterUpdated},
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRRotateClientMutualTLS rotates the client certificate while using mutual TLS on the
// XDCR target cluster, and ensures that the source cluster's cert is updated too (without recreating).
func TestXDCRRotateClientMutualTLS(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyEnable
	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	testXDCRRotateClient(t, kubernetes1, kubernetes2, dns, tls, &policy)
}

// TestXDCRRotateClientMandatoryMutualTLS rotates the client certificate while using mandatory mutual
// TLS on the XDCR target cluster, and ensures that the source cluster's cert is updated too (without recreating).
func TestXDCRRotateClientMandatoryMutualTLS(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyMandatory
	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	testXDCRRotateClient(t, kubernetes1, kubernetes2, dns, tls, &policy)
}

func testXDCRRotateCA(t *testing.T, kubernetes1, kubernetes2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	clusterSize := 1

	sourceCluster, targetCluster, bucket := createXDCRClusters(t, kubernetes1, kubernetes2, dns, tls, policy, clusterSize)

	sourceCluster = e2eutil.MustPatchCluster(t, kubernetes1, sourceCluster, jsonpatch.NewPatchSet().Add("/metadata/annotations", map[string]string{
		"cao.couchbase.com/xdcr.disablePrechecks": "true",
	}), time.Minute)

	numOfDocs := framework.Global.DocsCount

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), numOfDocs, 10*time.Minute)

	e2eutil.MustRotateServerCertificateClientCertificateAndCA(t, tls)
	e2eutil.MustObserveClusterEvent(t, kubernetes2, targetCluster, k8sutil.ClientTLSUpdatedEvent(targetCluster, k8sutil.ClientTLSUpdateReasonUpdateCA), 5*time.Minute)
	e2eutil.MustRotateXDCRReplicationTLS(t, kubernetes1, targetCluster, tls)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes1, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes2, targetCluster, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterUpdated},
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		// Race condition updating secrets.
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonTLSInvalid},
		},
		eventschema.Event{Reason: k8sutil.EventReasonTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateCA)},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonUpdateClientAuth)},
	}

	ValidateEvents(t, kubernetes1, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes2, targetCluster, expectedEvents2)
}

// TestXDCRRotateCAMandatoryMutualTLS rotates the CA while using mandatory mutual TLS on the
// XDCR target cluster, and ensures that the source cluster's CA is updated too (without recreating).
func TestXDCRRotateCAMandatoryMutualTLS(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyMandatory
	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	testXDCRRotateCA(t, kubernetes1, kubernetes2, dns, tls, &policy)
}

// TestXDCRRotateCAMutualTLS rotates the CA while using mutual TLS on the
// XDCR target cluster, and ensures that the source cluster's CA is updated too (without recreating).
func TestXDCRRotateCAMutualTLS(t *testing.T) {
	kubernetes1, kubernetes2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, kubernetes1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyEnable
	dns := e2eutil.MustProvisionCoreDNS(t, kubernetes1, kubernetes2)

	testXDCRRotateCA(t, kubernetes1, kubernetes2, dns, tls, &policy)
}

func TestXDCRReplicateLocalScopesAndCollections(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := framework.Global.DocsCount
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Create the clusters.
	bucket := mustCreateXDCRBucketsWithScopes(t, kubernetes, kubernetes, scope)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	// Add explicit mappings - currently we only use the default bucket collection
	replication.ExplicitMapping = couchbasev2.CouchbaseExplicitMappingSpec{
		AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName),
				},
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName),
				},
			},
		},
		DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{},
	}

	// Wait for the scope and collection to be created on both clusters
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, sourceCluster, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, bucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, sourceCluster, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucket.GetName(), scopeName, collectionName, numOfDocs, 5*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRReplicateLocalScopesAndCollectionsWithDeny tests that replication deny rules are correctly applied.
func TestXDCRReplicateLocalScopesAndCollectionsWithDeny(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := framework.Global.DocsCount
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	deniedCollectionName := "larry"

	// Create two collections - one to be denied replication.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	deniedCollection := e2eutil.NewCollection(deniedCollectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, deniedCollection).MustCreate(t, kubernetes)

	// Create the clusters.
	bucket := mustCreateXDCRBucketsWithScopes(t, kubernetes, kubernetes, scope)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	// Add explicit mappings, including denying one of the collections - currently we only use the default bucket collection.
	replication.ExplicitMapping = couchbasev2.CouchbaseExplicitMappingSpec{
		AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName),
				},
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName),
				},
			},
		},
		DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(deniedCollectionName),
				},
			},
		},
	}

	// Wait for the scope and collection to be created on both clusters.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, deniedCollectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, sourceCluster, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, bucket, expected, time.Minute)

	// Set up replication.
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs to both collections on the source bucket.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, deniedCollectionName).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, sourceCluster, bucket.GetName(), 2*numOfDocs, time.Minute)
	// Now wait until only the allowed target ones appear.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, targetCluster, bucket.GetName(), numOfDocs, 5*time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRReplicateLocalScopesAndCollectionsReuseSpec is much the same as ...WithDeny, but
// reuses the replication mapping for 2 separate replications.
func TestXDCRReplicateLocalScopesAndCollectionsReuseSpec(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := f.DocsCount
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	deniedCollectionName := "larry"

	// Create two collections - one to be denied replication.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	deniedCollection := e2eutil.NewCollection(deniedCollectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, deniedCollection).MustCreate(t, kubernetes)

	// Create the buckets & clusters.
	bucket := e2eutil.NewBucket(f.BucketType).WithScopes(scope).MustCreate(t, kubernetes)
	bucket2 := e2eutil.NewBucket(f.BucketType).WithScopes(scope).MustCreate(t, kubernetes)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucket2, time.Minute)

	// Set up replication spec; allow one collection, deny the other.
	mapping := couchbasev2.CouchbaseExplicitMappingSpec{
		AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName),
				},
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName),
				},
			},
		},
		DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(deniedCollectionName),
				},
			},
		},
	}

	replication1 := e2espec.GetReplication(bucket.GetName(), bucket.GetName())
	replication1.ExplicitMapping = mapping
	replication2 := e2espec.GetReplication(bucket.GetName(), bucket2.GetName())
	replication2.ExplicitMapping = mapping

	// Wait for the scope and collection to be created on both clusters.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, deniedCollectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, sourceCluster, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, bucket2, expected, time.Minute)

	// Set up replications.
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication1)
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication2)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs to both collections in both source buckets.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, deniedCollectionName).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, sourceCluster, bucket.GetName(), 2*numOfDocs, time.Minute)
	// Now wait until only the allowed target ones appear.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, targetCluster, bucket.GetName(), numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, targetCluster, bucket2.GetName(), numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucket2.GetName(), scopeName, collectionName, numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRReplicateLocalScopesAndCollectionsMultipleRules tests that multiple rules can be combined and applied.
func TestXDCRReplicateLocalScopesAndCollectionsMultipleRules(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := framework.Global.DocsCount
	clusterSize := 1
	scopeName1 := "pinky"
	scopeName2 := "billie"
	collectionName1 := "brain"
	collectionName2 := "snowball"
	deniedCollectionName := "larry"

	// Create two collections - one to be denied replication.
	collection1 := e2eutil.NewCollection(collectionName1).MustCreate(t, kubernetes)
	collection2 := e2eutil.NewCollection(collectionName2).MustCreate(t, kubernetes)
	deniedCollection := e2eutil.NewCollection(deniedCollectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope1 := e2eutil.NewScope(scopeName1).WithCollections(collection1, collection2, deniedCollection).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scopeName2).WithCollections(collection1, collection2, deniedCollection).MustCreate(t, kubernetes)

	// Create the clusters.
	bucket := mustCreateXDCRBucketsWithScopes(t, kubernetes, kubernetes, scope1, scope2)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	// Add multiple mappings, across many scopes and collections, both allow and deny.
	replication.ExplicitMapping = couchbasev2.CouchbaseExplicitMappingSpec{
		AllowRules: []couchbasev2.CouchbaseAllowReplicationMapping{
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName1),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName1),
				},
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName1),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName2),
				},
			},
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName1),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName2),
				},
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName1),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName1),
				},
			},
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope: couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName2),
				},
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope: couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName2),
				},
			},
		},
		DenyRules: []couchbasev2.CouchbaseDenyReplicationMapping{
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName1),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(deniedCollectionName),
				},
			},
			{
				SourceKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName2),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(deniedCollectionName),
				},
			},
		},
	}

	// Wait for the scope and collection to be created on both clusters.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName1).WithCollections(collectionName1, collectionName2, deniedCollectionName)
	expected.WithScope(scopeName2).WithCollections(collectionName1, collectionName2, deniedCollectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, sourceCluster, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, bucket, expected, time.Minute)

	// Set up replication.
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs to various scopes and collections collections on the source bucket.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName1, collectionName1).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.NewDocumentSet(bucket.GetName(), 2*numOfDocs).IntoScopeAndCollection(scopeName1, collectionName2).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName1, deniedCollectionName).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName2, collectionName1).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName2, deniedCollectionName).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first.
	e2eutil.MustVerifyDocCountInScope(t, kubernetes, sourceCluster, bucket.GetName(), scopeName1, 4*numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInScope(t, kubernetes, sourceCluster, bucket.GetName(), scopeName2, 2*numOfDocs, time.Minute)
	// Now wait until only the allowed target ones appear.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, targetCluster, bucket.GetName(), 4*numOfDocs, 5*time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucket.GetName(), scopeName1, collectionName1, 2*numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucket.GetName(), scopeName1, collectionName2, numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInScope(t, kubernetes, targetCluster, bucket.GetName(), scopeName2, numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRReplicateLocalScopesAndCollectionsImplicit tests replication using implicit rules.
func TestXDCRReplicateLocalScopesAndCollectionsImplicit(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := f.DocsCount
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Create the clusters.
	bucket := e2eutil.NewBucket(f.BucketType).WithScopes(scope).MustCreate(t, kubernetes)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	// Wait for the scope and collection to be created on both clusters
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, sourceCluster, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, bucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, sourceCluster, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucket.GetName(), scopeName, collectionName, numOfDocs, 5*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRMigrationLocalScopesAndCollections tests the migration functionality fo XDCR.
func TestXDCRMigrationLocalScopesAndCollections(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := framework.Global.DocsCount
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Create the clusters each selecting a different bucket using labels.
	sourceLabels := map[string]string{
		"source": "true",
	}
	targetLabels := map[string]string{
		"target": "true",
	}

	// Our source bucket has no scope and collection.
	sourceBucket := e2espec.DefaultBucket()
	sourceBucket.Name = "source"
	sourceBucket.Labels = sourceLabels
	sourceBucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(128)
	e2eutil.MustNewBucket(t, kubernetes, sourceBucket)

	// Create a target bucket with the scope and collection we want.
	targetBucket := e2espec.DefaultBucket()
	targetBucket.Name = "target"
	targetBucket.Labels = targetLabels
	targetBucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(128)
	e2eutil.LinkBucketToScopesExplicit(targetBucket, scope)
	e2eutil.MustNewBucket(t, kubernetes, targetBucket)

	// Now create our clusters which should select their appropriate buckets
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().Generate(kubernetes)
	sourceCluster.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: sourceLabels,
	}
	sourceCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, sourceCluster)

	// And the target cluster
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().Generate(kubernetes)
	targetCluster.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: targetLabels,
	}
	targetCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, targetCluster)

	// Confirm we have both
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, sourceBucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, targetBucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetMigrationReplication(sourceBucket.GetName(), targetBucket.GetName())

	replication.MigrationMapping = couchbasev2.CouchbaseMigrationMappingSpec{
		Mappings: []couchbasev2.CouchbaseMigrationMapping{
			{
				Filter: "_default._default",
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scopeName),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collectionName),
				},
			},
		},
	}

	// Wait for the scope and collection to be created on both clusters
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, targetBucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRMigrationReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(sourceBucket.GetName(), numOfDocs).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, sourceCluster, sourceBucket.GetName(), numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, targetBucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRMigrationLocalScopesAndCollectionsMultipleRules tests migration with multiple rules, and checks docs are appropriately not migrated.
func TestXDCRMigrationLocalScopesAndCollectionsMultipleRules(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := framework.Global.DocsCount
	clusterSize := 1
	scope1Name := "pinky"
	scope2Name := "snowball"
	collection1Name := "brain"
	collection2Name := "billie"
	key := "test"
	value1 := "qwerty"
	value2 := "asdf"

	// Create a collection and collection group.
	collection1 := e2eutil.NewCollection(collection1Name).MustCreate(t, kubernetes)
	collection2 := e2eutil.NewCollection(collection2Name).MustCreate(t, kubernetes)

	// Create a scope.
	scope1 := e2eutil.NewScope(scope1Name).WithCollections(collection1, collection2).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scope2Name).WithCollections(collection2).MustCreate(t, kubernetes)

	// Create the clusters each selecting a different bucket using labels.
	sourceLabels := map[string]string{
		"source": "true",
	}
	targetLabels := map[string]string{
		"target": "true",
	}

	// Our source bucket has no scope and collection.
	sourceBucket := e2espec.DefaultBucket()
	sourceBucket.Name = "source"
	sourceBucket.Labels = sourceLabels
	sourceBucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(128)
	e2eutil.MustNewBucket(t, kubernetes, sourceBucket)

	// Create a target bucket with the scope and collection we want.
	targetBucket := e2espec.DefaultBucket()
	targetBucket.Name = "target"
	targetBucket.Labels = targetLabels
	targetBucket.Spec.MemoryQuota = e2espec.NewResourceQuantityMi(128)
	e2eutil.LinkBucketToScopesExplicit(targetBucket, scope1, scope2)
	e2eutil.MustNewBucket(t, kubernetes, targetBucket)

	// Now create our clusters which should select their appropriate buckets
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().Generate(kubernetes)
	sourceCluster.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: sourceLabels,
	}
	sourceCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, sourceCluster)

	// And the target cluster
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().Generate(kubernetes)
	targetCluster.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: targetLabels,
	}
	targetCluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, targetCluster)

	// Confirm we have both
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, sourceBucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, targetBucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetMigrationReplication(sourceBucket.GetName(), targetBucket.GetName())

	replication.MigrationMapping = couchbasev2.CouchbaseMigrationMappingSpec{
		Mappings: []couchbasev2.CouchbaseMigrationMapping{
			{
				Filter: fmt.Sprintf("%s=\"%s\"", key, value2),
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scope2Name),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collection2Name),
				},
			},
			{
				Filter: fmt.Sprintf("%s=\"%s\"", key, value1),
				TargetKeyspace: couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      couchbasev2.ScopeOrCollectionNameIncludingDefault(scope1Name),
					Collection: couchbasev2.ScopeOrCollectionNameIncludingDefault(collection2Name),
				},
			},
		},
	}

	// Wait for the scope and collection to be created on both clusters
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scope1Name).WithCollections(collection1Name, collection2Name)
	expected.WithScope(scope2Name).WithCollections(collection2Name)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, targetCluster, targetBucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRMigrationReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(sourceBucket.GetName(), numOfDocs).WithValue(key, value1).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.NewDocumentSet(sourceBucket.GetName(), numOfDocs).WithValue(key, value2).MustCreate(t, kubernetes, sourceCluster)
	// Add docs that shouldn't be migrated.
	e2eutil.NewDocumentSet(sourceBucket.GetName(), numOfDocs).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, sourceCluster, sourceBucket.GetName(), 3*numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, targetBucket.GetName(), scope1Name, collection2Name, numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, targetBucket.GetName(), scope2Name, collection2Name, numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRReplicateLocalScopesAndCollectionsToUnmanaged replicates from a managed collection into an unmanaged one
// (created directly through the UI).
func TestXDCRReplicateLocalScopesAndCollectionsToUnmanaged(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket().IstioDisabled()

	// Static configuration.
	numOfDocs := f.DocsCount
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Create the clusters.
	bucket := e2eutil.NewBucket(f.BucketType).WithScopes(scope).MustCreate(t, kubernetes)
	bucketNoScopes := e2eutil.NewBucket(f.BucketType).MustCreate(t, kubernetes)
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucketNoScopes, time.Minute)

	// Manually create unmanaged scope & collection in target bucket.
	e2eutil.MustCreateScopeManually(t, kubernetes, targetCluster, bucketNoScopes.GetName(), scopeName)
	e2eutil.MustCreateCollectionManually(t, kubernetes, targetCluster, bucketNoScopes.GetName(), scopeName, collectionName)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucketNoScopes.GetName())

	// Wait for the scope and collection to be created on both clusters
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, sourceCluster, bucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, sourceCluster)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, sourceCluster, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, targetCluster, bucketNoScopes.GetName(), scopeName, collectionName, numOfDocs, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated},
	}

	ValidateEvents(t, kubernetes, sourceCluster, expectedEvents1)
	ValidateEvents(t, kubernetes, targetCluster, expectedEvents2)
}

// TestXDCRWithMandatoryTLSAndMultipleCAs tests that we can provision a working
// XDCR stream when the remote end is using multiple CAs and the client PKI is
// different from that of the server.
func TestXDCRWithMandatoryTLSAndMultipleCAs(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.1.0")

	// Static configuration.
	clusterSize := 1
	mtlsPolicy := couchbasev2.ClientCertificatePolicyMandatory

	// Create a source and target bucket.
	bucket := mustCreateXDCRBuckets(t, kubernetes, kubernetes)

	// The XDCR remote will need server PKI and a client PKI.
	serverTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})
	clientTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{Source: e2eutil.TLSSourceKubernetesSecret})

	// Create a basic source cluster, and an mTLS enabled target cluster, with multiple
	// CAs for server and client certification.
	sourceCluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	targetCluster := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(serverTLS, &mtlsPolicy).WithClientTLS(clientTLS).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, sourceCluster, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, targetCluster, bucket, time.Minute)

	// Establish the XDCR connection.  Use the server PKI to validate against the endpoint,
	// and the client PKI to provide authentication.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())
	e2eutil.MustEstablishXDCRReplicationWithMultipleCAs(t, kubernetes, kubernetes, sourceCluster, targetCluster, replication, serverTLS, clientTLS)

	// Inject some documents into the source, and expect them to appear in the target.
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).MustCreate(t, kubernetes, sourceCluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, targetCluster, bucket.GetName(), f.DocsCount, 10*time.Minute)
}
