package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// mustCreateXDCRBuckets creates default buckets in the source and target clusters, ensuring
// we don't redefine if the same cluster is used for source and target couchbase instances.
func mustCreateXDCRBuckets(t *testing.T, k8s1, k8s2 *types.Cluster) {
	f := framework.Global

	e2eutil.MustNewBucket(t, k8s1, f.Namespace, e2espec.DefaultBucket)
	if k8s1.Config.Host != k8s2.Config.Host {
		e2eutil.MustNewBucket(t, k8s2, f.Namespace, e2espec.DefaultBucket)
	}
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

// xdcrCluster defines the cluster to operate on (source or target)
type xdcrCluster int

const (
	xdcrClusterSource xdcrCluster = iota
	xdcrClusterTarget xdcrCluster = iota
)

// xdcrOperation defines the operation to perform on the cluster (eject, delete or scale down nodes)
type xdcrOperation int

const (
	xdcrOperationEject     xdcrOperation = iota
	xdcrOperationDelete    xdcrOperation = iota
	xdcrOperationScaleDown xdcrOperation = iota
)

// xdcrClusterRemoveNode removes nodes from the selected cluster in numerous
// nefarious ways.
func xdcrClusterRemoveNode(t *testing.T, k8s1, k8s2 *types.Cluster, cluster xdcrCluster, operation xdcrOperation) {
	// Platform configuration.
	f := framework.Global

	// Static configuration.
	clusterSize := constants.Size3
	scaleDownSize := constants.Size1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection and verify correct replication...
	_, cleanup := e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, 10*time.Minute)

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
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 20, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
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

// TestXdcrCreateCluster tests establishing an XDCR connection.
func TestXdcrCreateCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	_, cleanup := e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXDCRPauseReplication tests a replication can be paused and restarted again.
func TestXDCRPauseReplication(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := 1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.  Pause the replication and add in
	// some new documents, these shouldn't be replicated until we remove the
	// pause.
	replication, cleanup := e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, time.Minute)
	replication = e2eutil.MustPatchReplication(t, k8s1, replication, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	time.Sleep(time.Minute)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	time.Sleep(time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, time.Minute)
	_ = e2eutil.MustPatchReplication(t, k8s1, replication, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 20, time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source cluster paused and unpaused
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXdcrSourceNodeDown tests killing a node in the source cluster of an
// XDCR replication.
func TestXdcrSourceNodeDown(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	xdcrCluster1Size := constants.Size5
	xdcrCluster2Size := constants.Size2
	nodeToKill := 1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, xdcrCluster1Size)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, xdcrCluster2Size)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and verify they
	// have been replicated.  Kill a pod in the source cluster.  Add some more documents.
	// When healthy verify the documents have been replicated.
	_, cleanup := e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, 10*time.Minute)
	e2eutil.MustKillPodForMember(t, k8s1, xdcrCluster1, nodeToKill, true)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustWaitClusterStatusHealthy(t, k8s1, xdcrCluster1, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 20, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source recovers after a pod is taken down
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(xdcrCluster1Size, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(xdcrCluster2Size, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXdcrSourceNodeAdd tests adding a node into the source cluster of an XDCR
// replication.
func TestXdcrSourceNodeAdd(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := constants.Size1
	clusterScaledSize := constants.Size3

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Scale up the source cluster, add some more documents
	// and ensure they are (eventually - after 5 whole minutes) replicated.
	_, cleanup := e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, 10*time.Minute)
	xdcrCluster1 = e2eutil.MustResizeCluster(t, 0, constants.Size3, k8s1, xdcrCluster1, 5*time.Minute)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 20, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source cluster is scaled up
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		e2eutil.ClusterScaleUpSequence(clusterScaledSize - clusterSize),
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXdcrTargetNodeServiceDelete tests deleting the node-port services of the
// target cluster required by an XDCR replication.
func TestXdcrTargetNodeServiceDelete(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Delete the pod services in the destination cluster
	// to circuit break the connection.  Add some more documents and verify they are
	// replicated when the connection is reestablished.
	_, cleanup := e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, 10*time.Minute)
	e2eutil.MustDeletePodServices(t, k8s2, xdcrCluster2)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 20, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	// * Source cluster is scaled up
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithExposedFeatures(clusterSize, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// skipTLSXDCRCheck doesn't run these tests if Couchbase server version
// is less than 5.5.3 or 6.0.1 respectively due to a bug in GoXDCR that
// prevented a full handshake over TLS.
func skipTLSXDCRCheck(t *testing.T) {
	f := framework.Global

	rawVersion, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}
	version, err := couchbaseutil.NewVersion(rawVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}

	switch version.Major() {
	case 5:
		minVersion, err := couchbaseutil.NewVersion("5.5.3")
		if err != nil {
			e2eutil.Die(t, err)
		}
		if version.Less(minVersion) {
			t.Skip("Test requires Couchbase 5.5.3 or greater")
		}
	case 6:
		minVersion, err := couchbaseutil.NewVersion("6.0.1")
		if err != nil {
			e2eutil.Die(t, err)
		}
		if version.Less(minVersion) {
			t.Skip("Test requires Couchbase 6.0.1 or greater")
		}
	}
}

// Create cb clusters on top of TLS certificates
func TestXDCRCreateTLSCluster(t *testing.T) {
	skipTLSXDCRCheck(t)

	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := constants.Size1

	tls1, teardown1 := e2eutil.MustInitClusterTLS(t, k8s1, f.Namespace, &e2eutil.TLSOpts{})
	defer teardown1()
	tls2, teardown2 := e2eutil.MustInitClusterTLS(t, k8s2, f.Namespace, &e2eutil.TLSOpts{})
	defer teardown2()

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewTLSXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize, tls1)
	xdcrCluster2 := e2eutil.MustNewTLSXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize, tls2)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Create the XDCR connection.  Ensure TLS is enabled.
	cleanup := e2eutil.MustEstablishXDCRReplicationTLS(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name, tls2)
	defer cleanup()
	e2eutil.MustCheckClusterTLS(t, k8s1, f.Namespace, tls1)
	e2eutil.MustCheckClusterTLS(t, k8s1, f.Namespace, tls2)

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
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// testXDCRCreateMututalTLSCluster creates a TLS enabled XDCR connection with client authentication.
func testXDCRCreateMututalTLSCluster(t *testing.T, policy couchbasev2.ClientCertificatePolicy) {
	skipTLSXDCRCheck(t)
	skipMutualTLSCheck(t)

	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := constants.Size1

	tls1, teardown1 := e2eutil.MustInitClusterTLS(t, k8s1, f.Namespace, &e2eutil.TLSOpts{})
	defer teardown1()
	tls2, teardown2 := e2eutil.MustInitClusterTLS(t, k8s2, f.Namespace, &e2eutil.TLSOpts{})
	defer teardown2()

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewMutualTLSXDCRClusterBasic(t, k8s1, f.Namespace, clusterSize, tls1, policy)
	xdcrCluster2 := e2eutil.MustNewMutualTLSXDCRClusterBasic(t, k8s2, f.Namespace, clusterSize, tls2, policy)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Create the XDCR connection.  Ensure TLS is enabled.
	cleanup := e2eutil.MustEstablishXDCRReplicationTLS(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name, tls2)
	defer cleanup()
	e2eutil.MustCheckClusterTLS(t, k8s1, f.Namespace, tls1)
	e2eutil.MustCheckClusterTLS(t, k8s1, f.Namespace, tls2)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

func TestXDCRCreateMututalTLSCluster(t *testing.T) {
	testXDCRCreateMututalTLSCluster(t, couchbasev2.ClientCertificatePolicyEnable)
}

func TestXDCRCreateMandatoryMututalTLSCluster(t *testing.T) {
	testXDCRCreateMututalTLSCluster(t, couchbasev2.ClientCertificatePolicyMandatory)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutSourceClusterNodes(t *testing.T) {
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterSource, xdcrOperationEject)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutTargetClusterNodes(t *testing.T) {
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterTarget, xdcrOperationEject)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveSourceClusterNodes(t *testing.T) {
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterSource, xdcrOperationDelete)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveTargetClusterNodes(t *testing.T) {
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterTarget, xdcrOperationDelete)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of source bucket cluster is resized to single node cluster
func TestXdcrResizedOutSourceClusterNodes(t *testing.T) {
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterSource, xdcrOperationScaleDown)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of destination bucket cluster is resized to single node cluster
func TestXdcrResizedOutTargetClusterNodes(t *testing.T) {
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	xdcrClusterRemoveNode(t, k8s1, k8s2, xdcrClusterTarget, xdcrOperationScaleDown)
}
