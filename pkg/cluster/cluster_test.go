/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"errors"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	cbfake "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned/fake"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	fakeClusterName      = "cb-example"
	fakeClusterNamespace = "default"
)

// newStatusTestCluster builds a Cluster backed by a fake client. The server
// holds a cluster with storedSize, and the in-memory copy has desiredSize, so
// updateCRStatus() sees a difference and tries to write it.
func newStatusTestCluster(storedSize, desiredSize int) (*Cluster, *cbfake.Clientset) {
	name, namespace := fakeClusterName, fakeClusterNamespace

	stored := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status:     couchbasev2.ClusterStatus{Size: storedSize},
	}

	cbClient := cbfake.NewSimpleClientset(stored)

	c := &Cluster{
		k8s:     &client.Client{CouchbaseClient: cbClient},
		cluster: &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Status: couchbasev2.ClusterStatus{Size: desiredSize}},
		log:     logf.Log.WithName("test"),
	}

	return c, cbClient
}

func getStoredSize(t *testing.T, cbClient *cbfake.Clientset) int {
	t.Helper()

	got, err := cbClient.CouchbaseV2().CouchbaseClusters(fakeClusterNamespace).Get(t.Context(), fakeClusterName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to fetch cluster: %v", err)
	}

	return got.Status.Size
}

// TestUpdateCRStatusUsesStatusSubresource checks the normal case, status is
// written through the /status subresource, which the DAC doesn't see.
func TestUpdateCRStatusUsesStatusSubresource(t *testing.T) {
	c, cbClient := newStatusTestCluster(1, 3)

	var sawStatusUpdate bool

	cbClient.PrependReactor("update", "couchbaseclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "status" {
			sawStatusUpdate = true
		}
		// Fall through to the default tracker so the object is actually updated.
		return false, nil, nil
	})

	if err := c.updateCRStatus(); err != nil {
		t.Fatalf("updateCRStatus returned an error: %v", err)
	}

	if !sawStatusUpdate {
		t.Error("expected status to be written via the /status subresource, but it was not")
	}

	if got := getStoredSize(t, cbClient); got != 3 {
		t.Errorf("expected persisted status size 3, got %d", got)
	}
}

// TestUpdateCRStatusFallsBackWhenSubresourceMissing checks the upgrade case,
// when an old CRD has no /status endpoint, UpdateStatus() returns NotFound and
// we fall back to a status-only JSON Patch of the main resource so status
// still gets written.
func TestUpdateCRStatusFallsBackWhenSubresourceMissing(t *testing.T) {
	c, cbClient := newStatusTestCluster(1, 3)

	var sawStatusUpdate, sawFallbackPatch bool

	cbClient.PrependReactor("update", "couchbaseclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "status" {
			// Simulate an old CRD with no status subresource registered.
			sawStatusUpdate = true

			return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "couchbase.com", Resource: "couchbaseclusters"}, "cb-example")
		}

		return false, nil, nil
	})

	cbClient.PrependReactor("patch", "couchbaseclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		sawFallbackPatch = true

		// Fall through to the default tracker so the patch is actually applied.
		return false, nil, nil
	})

	if err := c.updateCRStatus(); err != nil {
		t.Fatalf("updateCRStatus returned an error: %v", err)
	}

	if !sawStatusUpdate {
		t.Error("expected an attempt on the /status subresource")
	}

	if !sawFallbackPatch {
		t.Error("expected a fallback status-only patch after the subresource returned NotFound")
	}

	if got := getStoredSize(t, cbClient); got != 3 {
		t.Errorf("expected persisted status size 3 after fallback, got %d", got)
	}
}

// TestUpdateCRStatusReturnsErrorWhenForbidden checks the missing RBAC case.
// When the operator lacks permission on couchbaseclusters/status,
// UpdateStatus() returns Forbidden. Forbidden (as opposed to NotFound) is
// only possible when this CRD version has a status subresource declared, in
// which case the API server strips `.status` out of any write sent to the
// main resource via Update, any Patch type, or Server-Side Apply. so a
// main-resource fallback could never actually persist the status write, it
// would just silently no-op.
func TestUpdateCRStatusReturnsErrorWhenForbidden(t *testing.T) {
	c, cbClient := newStatusTestCluster(1, 3)

	var sawStatusUpdate, sawFallbackPatch bool

	cbClient.PrependReactor("update", "couchbaseclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "status" {
			// Simulate the operator lacking couchbaseclusters/status RBAC.
			sawStatusUpdate = true

			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "couchbase.com", Resource: "couchbaseclusters/status"}, fakeClusterName, errors.New("forbidden"))
		}

		return false, nil, nil
	})

	cbClient.PrependReactor("patch", "couchbaseclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		sawFallbackPatch = true

		return false, nil, nil
	})

	err := c.updateCRStatus()
	if err == nil {
		t.Fatal("expected updateCRStatus to return an error when couchbaseclusters/status is forbidden, got nil")
	}

	if !sawStatusUpdate {
		t.Error("expected an attempt on the /status subresource")
	}

	if sawFallbackPatch {
		t.Error("expected no fallback patch of the main resource when forbidden, since it could never persist status and would silently no-op")
	}

	if got := getStoredSize(t, cbClient); got != 1 {
		t.Errorf("expected status to remain unchanged at 1 when forbidden, got %d", got)
	}
}

// TestUpdateCRStatusNoChangeSkipsWrite checks that no update is sent when the
// status already matches what's stored.
func TestUpdateCRStatusNoChangeSkipsWrite(t *testing.T) {
	c, cbClient := newStatusTestCluster(3, 3)

	var sawUpdate bool

	cbClient.PrependReactor("update", "couchbaseclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		sawUpdate = true

		return false, nil, nil
	})

	if err := c.updateCRStatus(); err != nil {
		t.Fatalf("updateCRStatus returned an error: %v", err)
	}

	if sawUpdate {
		t.Error("expected no update to be issued when status is unchanged")
	}
}

// newReplacedClusterTest builds a Cluster for a cluster that was deleted, and a new cluster with the
// same name but a different UID on the server, as if it was made again while the old one was still
// being reconciled.
func newReplacedClusterTest() (*Cluster, *cbfake.Clientset) {
	stored := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace, UID: "new-cluster"},
		Spec:       couchbasev2.ClusterSpec{Image: "couchbase/server:7.6.8"},
	}

	cbClient := cbfake.NewSimpleClientset(stored)

	c := &Cluster{
		k8s: &client.Client{CouchbaseClient: cbClient},
		cluster: &couchbasev2.CouchbaseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace, UID: "old-cluster"},
			Spec:       couchbasev2.ClusterSpec{Image: "couchbase/server:7.6.11"},
			Status:     couchbasev2.ClusterStatus{ClusterID: "f50608c895e226375ef4a3a6fb76229f", Size: 3},
		},
		log: logf.Log.WithName("test"),
	}

	return c, cbClient
}

// TestUpdateCRStatusSkipsReplacedCluster checks the old cluster's status, like its cluster ID, isn't
// written onto a new cluster with the same name.
func TestUpdateCRStatusSkipsReplacedCluster(t *testing.T) {
	c, cbClient := newReplacedClusterTest()

	var sawWrite bool

	cbClient.PrependReactor("*", "couchbaseclusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetVerb() == "update" || action.GetVerb() == "patch" {
			sawWrite = true
		}

		return false, nil, nil
	})

	if err := c.updateCRStatus(); err != nil {
		t.Fatalf("updateCRStatus returned an error: %v", err)
	}

	if sawWrite {
		t.Error("expected no write to the new cluster")
	}

	got, err := cbClient.CouchbaseV2().CouchbaseClusters(fakeClusterNamespace).Get(t.Context(), fakeClusterName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to fetch cluster: %v", err)
	}

	if got.Status.ClusterID != "" {
		t.Errorf("expected the new cluster to have no cluster ID, got %q", got.Status.ClusterID)
	}
}

// TestUpdateCRSpecAnnotationSkipsReplacedCluster checks the old cluster's spec isn't written into a new
// cluster's lastReconciledSpec annotation.
func TestUpdateCRSpecAnnotationSkipsReplacedCluster(t *testing.T) {
	c, cbClient := newReplacedClusterTest()

	if err := c.UpdateCRSpecAnnotation(); err != nil {
		t.Fatalf("UpdateCRSpecAnnotation returned an error: %v", err)
	}

	got, err := cbClient.CouchbaseV2().CouchbaseClusters(fakeClusterNamespace).Get(t.Context(), fakeClusterName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to fetch cluster: %v", err)
	}

	if _, ok := got.Annotations[constants.AnnotationLastReconciledSpec]; ok {
		t.Errorf("expected the new cluster to have no lastReconciledSpec annotation, got %v", got.Annotations)
	}
}

// TestCanAddNodeToClusterNoExposedFeatures checks that a cluster without
// external addressing is never held back by the external DNS pre check, the
// node is always allowed to join.
func TestCanAddNodeToClusterNoExposedFeatures(t *testing.T) {
	c := &Cluster{
		cluster: &couchbasev2.CouchbaseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace},
			// No ExposedFeatures set, so HasExposedFeatures() is false.
		},
		log: logf.Log.WithName("test"),
	}

	// The pod and member are not looked at on this path, so nil is fine.
	canAddNode, err := c.canAddNodeToCluster(nil, nil)
	if err != nil {
		t.Fatalf("canAddNodeToCluster returned an error: %v", err)
	}

	if !canAddNode {
		t.Error("expected the node to be allowed to join when the cluster has no exposed features")
	}
}

// TestCanAddNodeToClusterAllowUnreachable checks that with
// allowExternallyUnreachablePods set, the join is not gated on DNS at all, the
// node is allowed to join straight away, The rebalance is still held back
// separately by canRebalance.
func TestCanAddNodeToClusterAllowUnreachable(t *testing.T) {
	allow := true

	c := &Cluster{
		cluster: &couchbasev2.CouchbaseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace},
			Spec: couchbasev2.ClusterSpec{
				Networking: couchbasev2.CouchbaseClusterNetworkingSpec{
					// Exposed features are on, so the DNS check would normally run.
					ExposedFeatures:                []couchbasev2.ExposedFeature{couchbasev2.FeatureClient},
					AllowExternallyUnreachablePods: &allow,
				},
			},
		},
		log: logf.Log.WithName("test"),
	}

	// The pod and member are not looked at on this path, so nil is fine.
	canAddNode, err := c.canAddNodeToCluster(nil, nil)
	if err != nil {
		t.Fatalf("canAddNodeToCluster returned an error: %v", err)
	}

	if !canAddNode {
		t.Error("expected the node to be allowed to join when allowExternallyUnreachablePods is set")
	}
}
