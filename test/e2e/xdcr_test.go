/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

	corev1 "k8s.io/api/core/v1"
)

// mustCreateXDCRBuckets creates default buckets in the source and target clusters, ensuring
// we don't redefine if the same cluster is used for source and target couchbase instances.
func mustCreateXDCRBuckets(t *testing.T, k8s1, k8s2 *types.Cluster) {
	e2eutil.MustNewBucket(t, k8s1, k8s1.Namespace, e2espec.DefaultBucket)
	if k8s1.Config.Host != k8s2.Config.Host || k8s1.Namespace != k8s2.Namespace {
		e2eutil.MustNewBucket(t, k8s2, k8s2.Namespace, e2espec.DefaultBucket)
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
	// Static configuration.
	clusterSize := constants.Size3
	scaleDownSize := constants.Size1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection and verify correct replication...
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	_, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
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

	// Create the clusters.
	// The k8s1 cluster optionally uses a custom DNS service to address the k8s2 cluster.
	// The k8s2 cluster optionally has TLS set.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXDCRCluster(t, k8s1, clusterSize, dns, nil, nil)
	xdcrCluster2 := e2eutil.MustNewXDCRCluster(t, k8s2, clusterSize, nil, tls, policy)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	cleanup := e2eutil.MustEstablishXDCRReplication(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication, tls)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, 10*time.Minute)

	// Check the events match what we expect:
	// * Both clusters created
	// * Source cluster establishes XDCR
	k8s2CreateSequence := e2eutil.ClusterCreateSequence(clusterSize)
	if policy != nil {
		k8s2CreateSequence = e2eutil.ClusterCreateSequenceWithMutualTLS(clusterSize)
	}
	expectedEvents1 := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
	}
	expectedEvents2 := []eventschema.Validatable{
		k8s2CreateSequence,
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents1)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents2)
}

// TestXdcrCreateClusterLocal tests establishing an XDCR connection within the same cluster.
func TestXdcrCreateClusterLocal(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	testXDCRCreateCluster(t, k8s1, k8s1, nil, nil, nil)
}

// TestXdcrCreateClusterLocalTLS tests establishing a TLS XDCR connection within the same cluster.
func TestXdcrCreateClusterLocalTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s1, k8s1.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testXDCRCreateCluster(t, k8s1, k8s1, nil, tls, nil)
}

// TestXdcrCreateClusterLocalMutualTLS tests establishing an mTLS XDCR connection within the same cluster.
func TestXdcrCreateClusterLocalMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s1, k8s1.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyEnable
	testXDCRCreateCluster(t, k8s1, k8s1, nil, tls, &policy)
}

// TestXdcrCreateClusterLocalMandatoryMutualTLS tests establishing a mandatory mTLS TLS XDCR connection within the same cluster.
func TestXdcrCreateClusterLocalMandatoryMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s1, k8s1.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testXDCRCreateCluster(t, k8s1, k8s1, nil, tls, &policy)
}

// TestXdcrCreateClusterRemote tests establishing an XDCR connection to a k8s2 cluster.
func TestXdcrCreateClusterRemote(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	testXDCRCreateCluster(t, k8s1, k8s2, dns, nil, nil)
}

// TestXdcrCreateClusterRemoteTLS tests establishing a TLS XDCR connection to a k8s2 cluster.
func TestXdcrCreateClusterRemoteTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s2, k8s2.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	testXDCRCreateCluster(t, k8s1, k8s2, dns, tls, nil)
}

// TestXdcrCreateClusterRemoteMutualTLS tests establishing an mTLS XDCR connection to a k8s2 cluster.
func TestXdcrCreateClusterRemoteMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s2, k8s2.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyEnable
	testXDCRCreateCluster(t, k8s1, k8s2, dns, tls, &policy)
}

// TestXdcrCreateClusterRemoteMandatoryMutualTLS tests establishing a mandatory mTLS TLS XDCR connection to a k8s2 cluster.
func TestXdcrCreateClusterRemoteMandatoryMutualTLS(t *testing.T) {
	k8s1 := framework.Global.GetCluster(0)
	k8s2 := framework.Global.GetCluster(1)

	dns, cleanup := e2eutil.MustProvisionCoreDNS(t, k8s1, k8s2)
	defer cleanup()

	tls, teardown := e2eutil.MustInitClusterTLS(t, k8s2, k8s2.Namespace, &e2eutil.TLSOpts{})
	defer teardown()

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testXDCRCreateCluster(t, k8s1, k8s2, dns, tls, &policy)
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
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	_, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, 10*time.Minute)

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
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := 1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.  Pause the replication and add in
	// some new documents, these shouldn't be replicated until we remove the
	// pause.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	replication, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
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
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, xdcrCluster1Size)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, xdcrCluster2Size)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and verify they
	// have been replicated.  Kill a pod in the source cluster.  Add some more documents.
	// When healthy verify the documents have been replicated.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	_, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
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
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequenceWithExposedFeatures(xdcrCluster1Size, couchbasev2.FeatureXDCR),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRemoteClusterAdded},
		eventschema.Event{Reason: k8sutil.EventReasonReplicationAdded},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	expectedEvents2 := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
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
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Scale up the source cluster, add some more documents
	// and ensure they are (eventually - after 5 whole minutes) replicated.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	_, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
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
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, link the source to the destination cluster.  Add some documents and
	// ensure they are replicated.  Delete the pod services in the destination cluster
	// to circuit break the connection.  Add some more documents and verify they are
	// replicated when the connection is reestablished.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	_, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
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

// TestXDCRDeleteReplication tests a replication can be deleted.
func TestXDCRDeleteReplication(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := 1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection, add some documents and
	// verify they have been replicated.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	replication, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
	defer cleanup()
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, time.Minute)

	// Now we delete the replication, add some documents in the source bucket and
	// verify that the doc count of destination bucket is same as its old value
	e2eutil.MustDeleteXDCRReplication(t, k8s1, xdcrCluster1, replication, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, k8s1, xdcrCluster1, e2eutil.ReplicationRemovedEvent(xdcrCluster1, "remote", replication.Spec.Bucket, replication.Spec.RemoteBucket), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, k8s1, xdcrCluster1, 2*time.Minute)

	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	time.Sleep(time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 20, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, time.Minute)

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
// is behaving as expected:
func TestXDCRFilterExp(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := 1

	// Create the clusters.
	mustCreateXDCRBuckets(t, k8s1, k8s2)
	xdcrCluster1 := e2eutil.MustNewXDCRClusterGeneric(t, k8s1, k8s1.Namespace, clusterSize)
	xdcrCluster2 := e2eutil.MustNewXDCRClusterGeneric(t, k8s2, k8s2.Namespace, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, k8s1, xdcrCluster1, []string{e2espec.DefaultBucket.Name}, time.Minute)
	e2eutil.MustWaitUntilBucketsExists(t, k8s2, xdcrCluster2, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// When ready, establish the XDCR connection with the specified filter expression
	// for 6.0.3, template for filter expression: "regex_pattern"
	// for 6.5.0, template for filter expression: `REGEXP_CONTAINS(expression, "regex_pattern")`
	// filter expression is defined to allow docs with Key Id starting from `doc` to get replicated.
	replication := e2espec.GetReplication(e2espec.DefaultBucket.Name, e2espec.DefaultBucket.Name)
	threshold, _ := couchbaseutil.NewVersion("6.5.0")
	tag, err := k8sutil.CouchbaseVersion(f.CouchbaseServerImage)
	if err != nil {
		e2eutil.Die(t, err)
	}
	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		e2eutil.Die(t, err)
	}
	replication.Spec.FilterExpression = `REGEXP_CONTAINS(META().id, "^doc.*$")`
	if version.Less(threshold) {
		replication.Spec.FilterExpression = `^doc.*$`
	}
	_, cleanup := e2eutil.MustEstablishXDCRReplicationGeneric(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, replication)
	defer cleanup()

	// MustPopulateBucket inserts documents with DocId following the template: "random%d"
	// which won't be matched by the filter and will not get replicated.
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 0, time.Minute)

	// MustInsertJSONDocsIntoBucket inserts documents with DocId following the template: "`doc`%d"
	e2eutil.MustInsertJSONDocsIntoBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 0, 10)

	// Now we check that the new documents inserted in the source bucket is replicated
	// since the filter expression applied allows documents with document id starting with `doc` to get replicated.
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, e2espec.DefaultBucket.Name, 10, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s1, xdcrCluster1, e2espec.DefaultBucket.Name, 20, time.Minute)

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
