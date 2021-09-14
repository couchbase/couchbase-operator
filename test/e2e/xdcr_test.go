package e2e

import (
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
func mustCreateXDCRBucketsWithScopes(t *testing.T, k8s1, k8s2 *types.Cluster, scopes ...*couchbasev2.CouchbaseScope) metav1.Object {
	bucket := e2eutil.MustGetBucket(t, framework.Global.BucketType, framework.Global.CompressionMode)

	for _, scope := range scopes {
		e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	}

	e2eutil.MustNewBucket(t, k8s1, bucket)

	if k8s1.Config.Host != k8s2.Config.Host || k8s1.Namespace != k8s2.Namespace {
		e2eutil.MustNewBucket(t, k8s2, bucket)
	}

	return bucket
}

func mustCreateXDCRBuckets(t *testing.T, k8s1, k8s2 *types.Cluster) metav1.Object {
	return mustCreateXDCRBucketsWithScopes(t, k8s1, k8s2)
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

func XDCRCreateCluster(t *testing.T, k8s1, k8s2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy, clusterSize int) (*couchbasev2.CouchbaseCluster, *couchbasev2.CouchbaseCluster, metav1.Object, *couchbasev2.CouchbaseReplication) {
	// Create the clusters.
	// The k8s1 cluster optionally uses a custom DNS service to address the k8s2 cluster.
	// The k8s2 cluster optionally has TLS set.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithDNS(dns).MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithMutualTLS(tls, policy).MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication, tls)

	return xdcrCluster1, xdcrCluster2, bucket, replication
}

// xdcrClusterRemoveNode removes nodes from the selected cluster in numerous
// nefarious ways.
func xdcrClusterRemoveNode(t *testing.T, k8s1, k8s2 *types.Cluster, cluster xdcrCluster, operation xdcrOperation) {
	// Static configuration.
	clusterSize := constants.Size3
	scaleDownSize := constants.Size1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection and verify correct replication...
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), framework.Global.DocsCount).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)

	// ...choose the correct victim cluster...
	var targetKubernetes *types.Cluster

	var targetCouchbase *couchbasev2.CouchbaseCluster

	switch cluster {
	case xdcrClusterSource:
		targetKubernetes = k8s1
		targetCouchbase = xdcrCluster1
	case xdcrClusterTarget:
		targetKubernetes = k8s2
		targetCouchbase = xdcrCluster2
	}

	// ...and perform the necessary operation on it...
	var schema eventschema.Validatable

	switch operation {
	case xdcrOperationEject:
		schema = ejectAllXDCRNodes(t, targetKubernetes, targetCouchbase)
	case xdcrOperationDelete:
		schema = killAllXDCRNodes(t, targetKubernetes, targetCouchbase)
	case xdcrOperationScaleDown:
		schema = scaleDownXDCRCluster(t, targetKubernetes, targetCouchbase, scaleDownSize)
	}

	// ...before finally creating more documents and verifying replication still works.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// testXDCRCreateCluster tests cluster creation using any combination of DNS/TLS.
func testXDCRCreateCluster(t *testing.T, k8s1, k8s2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := framework.Global.DocsCount

	xdcrCluster1, xdcrCluster2, bucket, _ := XDCRCreateCluster(t, k8s1, k8s2, dns, tls, policy, clusterSize)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRCreateClusterLocal tests establishing an XDCR connection within the same cluster.
func TestXDCRCreateClusterLocal(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	testXDCRCreateCluster(t, k8s1, k8s1, nil, nil, nil)
}

// TestXDCRCreateClusterLocalTLS tests establishing a TLS XDCR connection within the same cluster.
func TestXDCRCreateClusterLocalTLS(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, k8s1, &e2eutil.TLSOpts{})

	testXDCRCreateCluster(t, k8s1, k8s1, nil, tls, nil)
}

// TestXDCRCreateClusterLocalMutualTLS tests establishing an mTLS XDCR connection within the same cluster.
func TestXDCRCreateClusterLocalMutualTLS(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, k8s1, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testXDCRCreateCluster(t, k8s1, k8s1, nil, tls, &policy)
}

// TestXDCRCreateClusterLocalMandatoryMutualTLS tests establishing a mandatory mTLS TLS XDCR connection within the same cluster.
func TestXDCRCreateClusterLocalMandatoryMutualTLS(t *testing.T) {
	k8s1, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, k8s1, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testXDCRCreateCluster(t, k8s1, k8s1, nil, tls, &policy)
}

// TestXDCRCreateClusterRemote tests establishing an XDCR connection to a k8s2 cluster.
func TestXDCRCreateClusterRemote(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	testXDCRCreateCluster(t, k8s1, k8s2, dns, nil, nil)
}

// TestXDCRCreateClusterRemoteTLS tests establishing a TLS XDCR connection to a k8s2 cluster.
func TestXDCRCreateClusterRemoteTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})

	testXDCRCreateCluster(t, k8s1, k8s2, dns, tls, nil)
}

// TestXDCRCreateClusterRemoteMutualTLS tests establishing an mTLS XDCR connection to a k8s2 cluster.
func TestXDCRCreateClusterRemoteMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testXDCRCreateCluster(t, k8s1, k8s2, dns, tls, &policy)
}

// TestXDCRCreateClusterRemoteMandatoryMutualTLS tests establishing a mandatory mTLS TLS XDCR connection to a k8s2 cluster.
func TestXDCRCreateClusterRemoteMandatoryMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testXDCRCreateCluster(t, k8s1, k8s2, dns, tls, &policy)
}

// TestXDCRCreateCluster tests establishing an XDCR connection.
func TestXDCRCreateCluster(t *testing.T) {
	// Platform configuration.
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := constants.Size3
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRPauseReplication tests a replication can be paused and restarted again.
func TestXDCRPauseReplication(t *testing.T) {
	// Platform configuration.
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := 1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.  Pause the replication and add in
	// some new documents, these shouldn't be replicated until we remove the
	// pause.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	info := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
	replication = info.Replication

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, time.Minute)
	replication = e2eutil.MustPatchReplication(t, k8s1, replication, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	time.Sleep(time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	time.Sleep(time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, time.Minute)
	_ = e2eutil.MustPatchReplication(t, k8s1, replication, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRSourceNodeDown tests killing a node in the source cluster of an
// XDCR replication.
func TestXDCRSourceNodeDown(t *testing.T) {
	// Platform configuration.
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := 3
	nodeToKill := 1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and verify they
	// have been replicated.  Kill a pod in the source cluster.  Add some more documents.
	// When healthy verify the documents have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)
	e2eutil.MustKillPodForMember(t, k8s1, xdcrCluster1, nodeToKill, true)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustWaitForClusterEvent(t, k8s1, xdcrCluster1, e2eutil.RebalanceStartedEvent(xdcrCluster1), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, k8s1, xdcrCluster1, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRSourceNodeAdd tests adding a node into the source cluster of an XDCR
// replication.
func TestXDCRSourceNodeAdd(t *testing.T) {
	// Platform configuration.
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := constants.Size1
	clusterScaledSize := constants.Size3
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Scale up the source cluster, add some more documents
	// and ensure they are (eventually - after 5 whole minutes) replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)
	xdcrCluster1 = e2eutil.MustResizeCluster(t, 0, constants.Size3, k8s1, xdcrCluster1, 5*time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRTargetNodeServiceDelete tests deleting the node-port services of the
// target cluster required by an XDCR replication.
func TestXDCRTargetNodeServiceDelete(t *testing.T) {
	// Platform configuration.
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := constants.Size1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Delete the pod services in the destination cluster
	// to circuit break the connection.  Add some more documents and verify they are
	// replicated when the connection is reestablished.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)
	e2eutil.MustDeletePodServices(t, k8s2, xdcrCluster2)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the source bucket cluster are rebalanced out.
// one by one until there is only one node in cluster.
func TestXDCRRebalanceOutSourceClusterNodes(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterSource, xdcrOperationEject)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the destination bucket cluster are rebalanced out.
// one by one until there is only one node in cluster.
func TestXDCRRebalanceOutTargetClusterNodes(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterTarget, xdcrOperationEject)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the source bucket cluster are killed one by one.
// At the end all nodes are replaced by new nodes in the cluster.
func TestXDCRRemoveSourceClusterNodes(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterSource, xdcrOperationDelete)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes from the destination bucket cluster are killed one by one.
// At the end all nodes are replaced by new nodes in the cluster.
func TestXDCRRemoveTargetClusterNodes(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterTarget, xdcrOperationDelete)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes of source bucket cluster is resized to single node cluster.
func TestXDCRResizedOutSourceClusterNodes(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterSource, xdcrOperationScaleDown)
}

// Create two clusters and while trying to configure XDCR.
// Cluster nodes of destination bucket cluster is resized to single node cluster.
func TestXDCRResizedOutTargetClusterNodes(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterTarget, xdcrOperationScaleDown)
}

// TestXDCRDeleteReplication tests a replication can be deleted.
func TestXDCRDeleteReplication(t *testing.T) {
	// Platform configuration.
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := 1
	numOfDocs := framework.Global.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	info := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
	replication = info.Replication

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, time.Minute)

	// Now we delete the replication, add some documents in the source bucket and
	// verify that the doc count of destination bucket is same as its old value
	e2eutil.MustDeleteXDCRReplication(t, k8s1, xdcrCluster1, replication, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, k8s1, xdcrCluster1, e2eutil.ReplicationRemovedEvent(xdcrCluster1, xdcrCluster2.Name, string(replication.Spec.Bucket), string(replication.Spec.RemoteBucket)), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, k8s1, xdcrCluster1, 2*time.Minute)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	time.Sleep(time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s1, xdcrCluster1, bucket.GetName(), 2*numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRFilterExp checks that the filter expressions when applied to XDCR cluster
// is behaving as expected.
func TestXDCRFilterExp(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := 1
	numOfDocs := f.DocsCount

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection with the specified filter expression
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())
	replication.Spec.FilterExpression = `REGEXP_CONTAINS(META().id, "^doc.*$")`

	e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)

	// Insert documents with DocId following the template: "random%d"
	// which won't be matched by the filter and will not get replicated.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 0, time.Minute)

	// Insert documents with DocId following the template: "`doc`%d"
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).WithPrefix("doc").MustCreate(t, k8s1, xdcrCluster1)

	// Now we check that the new documents inserted in the source bucket is replicated
	// since the filter expression applied allows documents with document id starting with `doc` to get replicated.
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s1, xdcrCluster1, bucket.GetName(), 2*numOfDocs, time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRRotatePassword tests that when the password is rotated on the XDCR target cluster
// it can also be updated on the source cluster without having to teardown and recreate
// the connection.
func TestXDCRRotatePassword(t *testing.T) {
	// Platform configuration.
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket().NotVersion("6.5.1")

	// Static configuration.
	clusterSize := 1

	// Create the clusters.
	bucket := mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s1)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, k8s2)
	e2eutil.MustWaitUntilBucketExists(t, k8s1, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, k8s2, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	info := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)

	numOfDocs := framework.Global.DocsCount
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)

	e2eutil.MustRotateClusterPassword(t, k8s2)
	e2eutil.MustRotateXDCRReplicationPassword(t, k8s1, k8s2, info.SecretName)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

func testXDCRRotateClient(t *testing.T, k8s1, k8s2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	clusterSize := 1

	xdcrCluster1, xdcrCluster2, bucket, _ := XDCRCreateCluster(t, k8s1, k8s2, dns, tls, policy, clusterSize)

	numOfDocs := framework.Global.DocsCount

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)

	e2eutil.MustRotateClientCertificate(t, tls)
	e2eutil.MustObserveClusterEvent(t, k8s2, xdcrCluster2, k8sutil.ClientTLSUpdatedEvent(xdcrCluster2, k8sutil.ClientTLSUpdateReasonUpdateClientAuth), 5*time.Minute)
	e2eutil.MustRotateXDCRReplicationTLS(t, k8s1, k8s2, xdcrCluster2, tls)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRRotateClientMutualTLS rotates the client certificate while using mutual TLS on the
// XDCR target cluster, and ensures that the source cluster's cert is updated too (without recreating).
func TestXDCRRotateClientMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyEnable
	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	testXDCRRotateClient(t, k8s1, k8s2, dns, tls, &policy)
}

// TestXDCRRotateClientMandatoryMutualTLS rotates the client certificate while using mandatory mutual
// TLS on the XDCR target cluster, and ensures that the source cluster's cert is updated too (without recreating).
func TestXDCRRotateClientMandatoryMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyMandatory
	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	testXDCRRotateClient(t, k8s1, k8s2, dns, tls, &policy)
}

func testXDCRRotateCA(t *testing.T, k8s1, k8s2 *types.Cluster, dns *corev1.Service, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	clusterSize := 1

	xdcrCluster1, xdcrCluster2, bucket, _ := XDCRCreateCluster(t, k8s1, k8s2, dns, tls, policy, clusterSize)

	numOfDocs := framework.Global.DocsCount

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), numOfDocs, 10*time.Minute)

	e2eutil.MustRotateServerCertificateClientCertificateAndCA(t, tls)
	e2eutil.MustObserveClusterEvent(t, k8s2, xdcrCluster2, k8sutil.ClientTLSUpdatedEvent(xdcrCluster2, k8sutil.ClientTLSUpdateReasonUpdateCA), 5*time.Minute)
	e2eutil.MustRotateXDCRReplicationTLS(t, k8s1, k8s2, xdcrCluster2, tls)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, k8s1, xdcrCluster1)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, bucket.GetName(), 2*numOfDocs, 10*time.Minute)

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

	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRRotateCAMandatoryMutualTLS rotates the CA while using mandatory mutual TLS on the
// XDCR target cluster, and ensures that the source cluster's CA is updated too (without recreating).
func TestXDCRRotateCAMandatoryMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyMandatory
	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	testXDCRRotateCA(t, k8s1, k8s2, dns, tls, &policy)
}

// TestXDCRRotateCAMutualTLS rotates the CA while using mutual TLS on the
// XDCR target cluster, and ensures that the source cluster's CA is updated too (without recreating).
func TestXDCRRotateCAMutualTLS(t *testing.T) {
	k8s1, k8s2, cleanup := framework.Global.SetupTestRemote(t)
	defer cleanup()

	framework.Requires(t, k8s1).CouchbaseBucket()

	tls := e2eutil.MustInitClusterTLS(t, k8s2, &e2eutil.TLSOpts{})
	policy := couchbasev2.ClientCertificatePolicyEnable
	dns := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)

	testXDCRRotateCA(t, k8s1, k8s2, dns, tls, &policy)
}

func TestXDCRReplicateLocalScopesAndCollections(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

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
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster2, bucket, time.Minute)

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
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster1, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster2, bucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, xdcrCluster1, xdcrCluster2, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, xdcrCluster1)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster1, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, bucket.GetName(), scopeName, collectionName, numOfDocs, 5*time.Minute)

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

	ValidateEvents(t, kubernetes, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, kubernetes, xdcrCluster2, expectedEvents2)
}

func TestXDCRReplicateLocalScopesAndCollectionsWithDeny(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

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
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster2, bucket, time.Minute)

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
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, deniedCollectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster1, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster2, bucket, expected, time.Minute)

	// Set up replication.
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, xdcrCluster1, xdcrCluster2, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs to both collections on the source bucket.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, xdcrCluster1)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, deniedCollectionName).MustCreate(t, kubernetes, xdcrCluster1)
	// Ensure we have the source docs first.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, xdcrCluster1, bucket.GetName(), 2*numOfDocs, time.Minute)
	// Now wait until only the allowed target ones appear.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, xdcrCluster2, bucket.GetName(), numOfDocs, 5*time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)

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

	ValidateEvents(t, kubernetes, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, kubernetes, xdcrCluster2, expectedEvents2)
}

// TestXDCRReplicateLocalScopesAndCollectionsReuseSpec is much the same as ...WithDeny, but
// reuses the replication mapping for 2 separate replications.
func TestXDCRReplicateLocalScopesAndCollectionsReuseSpec(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

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
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster2, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster2, bucket2, time.Minute)

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
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, deniedCollectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster1, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster2, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster2, bucket2, expected, time.Minute)

	// Set up replications.
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, xdcrCluster1, xdcrCluster2, replication1)
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, xdcrCluster1, xdcrCluster2, replication2)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs to both collections in both source buckets.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, xdcrCluster1)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, deniedCollectionName).MustCreate(t, kubernetes, xdcrCluster1)
	// Ensure we have the source docs first.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, xdcrCluster1, bucket.GetName(), 2*numOfDocs, time.Minute)
	// Now wait until only the allowed target ones appear.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, xdcrCluster2, bucket.GetName(), numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, xdcrCluster2, bucket2.GetName(), numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, bucket2.GetName(), scopeName, collectionName, numOfDocs, time.Minute)

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

	ValidateEvents(t, kubernetes, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, kubernetes, xdcrCluster2, expectedEvents2)
}

func TestXDCRReplicateLocalScopesAndCollectionsMultipleRules(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

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
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster2, bucket, time.Minute)

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
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName1).WithCollections(collectionName1, collectionName2, deniedCollectionName)
	expected.WithScope(scopeName2).WithCollections(collectionName1, collectionName2, deniedCollectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster1, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster2, bucket, expected, time.Minute)

	// Set up replication.
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, xdcrCluster1, xdcrCluster2, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs to both collections on the source bucket.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName1, collectionName1).MustCreate(t, kubernetes, xdcrCluster1)
	e2eutil.NewDocumentSet(bucket.GetName(), 2*numOfDocs).IntoScopeAndCollection(scopeName1, collectionName2).MustCreate(t, kubernetes, xdcrCluster1)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName1, deniedCollectionName).MustCreate(t, kubernetes, xdcrCluster1)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName2, collectionName1).MustCreate(t, kubernetes, xdcrCluster1)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName2, deniedCollectionName).MustCreate(t, kubernetes, xdcrCluster1)
	// Ensure we have the source docs first.
	e2eutil.MustVerifyDocCountInScope(t, kubernetes, xdcrCluster1, bucket.GetName(), scopeName1, 4*numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInScope(t, kubernetes, xdcrCluster1, bucket.GetName(), scopeName2, 2*numOfDocs, time.Minute)
	// Now wait until only the allowed target ones appear.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, xdcrCluster2, bucket.GetName(), 4*numOfDocs, 5*time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, bucket.GetName(), scopeName1, collectionName1, 2*numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, bucket.GetName(), scopeName1, collectionName2, numOfDocs, time.Minute)
	e2eutil.MustVerifyDocCountInScope(t, kubernetes, xdcrCluster2, bucket.GetName(), scopeName2, numOfDocs, time.Minute)

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

	ValidateEvents(t, kubernetes, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, kubernetes, xdcrCluster2, expectedEvents2)
}

func TestXDCRReplicateLocalScopesAndCollectionsImplicit(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

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
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster1, bucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster2, bucket, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(bucket.GetName(), bucket.GetName())

	// Wait for the scope and collection to be created on both clusters
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster1, bucket, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster2, bucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRReplicationGeneric(t, kubernetes, kubernetes, xdcrCluster1, xdcrCluster2, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, xdcrCluster1)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster1, bucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, bucket.GetName(), scopeName, collectionName, numOfDocs, 5*time.Minute)

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

	ValidateEvents(t, kubernetes, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, kubernetes, xdcrCluster2, expectedEvents2)
}

func TestXDCRMigrationLocalScopesAndCollections(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

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
	xdcrCluster1 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().Generate(kubernetes)
	xdcrCluster1.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: sourceLabels,
	}
	xdcrCluster1 = e2eutil.MustNewClusterFromSpec(t, kubernetes, xdcrCluster1)

	// And the target cluster
	xdcrCluster2 := clusterOptions().WithEphemeralTopology(clusterSize).WithGenericNetworking().Generate(kubernetes)
	xdcrCluster2.Spec.Buckets.Selector = &metav1.LabelSelector{
		MatchLabels: targetLabels,
	}
	xdcrCluster2 = e2eutil.MustNewClusterFromSpec(t, kubernetes, xdcrCluster2)

	// Confirm we have both
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster1, sourceBucket, time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, xdcrCluster2, targetBucket, time.Minute)

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
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, xdcrCluster2, targetBucket, expected, time.Minute)

	// Set up replication
	e2eutil.MustEstablishXDCRMigrationReplicationGeneric(t, kubernetes, kubernetes, xdcrCluster1, xdcrCluster2, replication)
	// TODO: verify the rules are correct (they are according to a check in the web UI on cluster 1)

	// Add docs and ensure they get replicated
	e2eutil.NewDocumentSet(sourceBucket.GetName(), numOfDocs).MustCreate(t, kubernetes, xdcrCluster1)
	// Ensure we have the source docs first
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, xdcrCluster1, sourceBucket.GetName(), numOfDocs, time.Minute)
	// Now wait until all the target ones appear
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, xdcrCluster2, targetBucket.GetName(), scopeName, collectionName, numOfDocs, time.Minute)

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

	ValidateEvents(t, kubernetes, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, kubernetes, xdcrCluster2, expectedEvents2)
}
