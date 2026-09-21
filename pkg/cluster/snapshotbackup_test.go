/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"reflect"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// TestCleanupFailedSnapshotCapture checks that cleanup finds a failed run's snapshots
// by its label, not by the names recorded on the run, so it still removes ones the run
// never managed to record, and leaves other runs snapshots alone.
func TestCleanupFailedSnapshotCapture(t *testing.T) {
	snapshot := func(gvr schema.GroupVersionResource, kind, name, runName string) *unstructured.Unstructured {
		object := &unstructured.Unstructured{}
		object.SetAPIVersion(gvr.GroupVersion().String())
		object.SetKind(kind)
		object.SetNamespace(fakeClusterNamespace)
		object.SetName(name)
		object.SetLabels(map[string]string{constants.LabelSnapshotBackupRun: runName})

		return object
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			volumeSnapshotGVR:      "VolumeSnapshotList",
			volumeGroupSnapshotGVR: "VolumeGroupSnapshotList",
		},
		snapshot(volumeSnapshotGVR, "VolumeSnapshot", "failed-run-a", "failed-run"),
		snapshot(volumeSnapshotGVR, "VolumeSnapshot", "failed-run-b", "failed-run"),
		snapshot(volumeGroupSnapshotGVR, "VolumeGroupSnapshot", "failed-run-group", "failed-run"),
		snapshot(volumeSnapshotGVR, "VolumeSnapshot", "other-snapshot", "other-run"),
	)

	c := &Cluster{
		k8s:     &client.Client{DynamicClient: dynamicClient},
		cluster: &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace}},
		log:     logf.Log.WithName("test"),
	}

	// The run records nothing, the same as when one snapshot fails before any results
	// are written back to it.
	run := &couchbasev2.CouchbaseSnapshotBackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-run", Namespace: fakeClusterNamespace},
	}

	c.cleanupFailedSnapshotCapture(run)

	remaining := func(gvr schema.GroupVersionResource) []string {
		list, err := dynamicClient.Resource(gvr).Namespace(fakeClusterNamespace).List(t.Context(), metav1.ListOptions{})
		if err != nil {
			t.Fatalf("failed to list %s: %v", gvr.Resource, err)
		}

		var names []string

		for _, item := range list.Items {
			names = append(names, item.GetName())
		}

		return names
	}

	if got := remaining(volumeSnapshotGVR); !reflect.DeepEqual(got, []string{"other-snapshot"}) {
		t.Errorf("expected only other-snapshot to remain, got %v", got)
	}

	if got := remaining(volumeGroupSnapshotGVR); len(got) != 0 {
		t.Errorf("expected the group snapshot to be deleted, got %v", got)
	}
}

// TestReadSnapshotBackupBaselineSkipsWhenMemberNotReady checks that no backup is taken while
// any member isn't ready, since a backup missing one member's volumes can't be restored.
func TestReadSnapshotBackupBaselineSkipsWhenMemberNotReady(t *testing.T) {
	member := func(name string) couchbaseutil.Member {
		return couchbaseutil.NewMember(fakeClusterNamespace, fakeClusterName, name, "", "", false, "")
	}

	// No API client is set, so this only passes if we skip before calling the cluster.
	c := &Cluster{
		members:         couchbaseutil.NewMemberSet(member("cb-0000"), member("cb-0001"), member("cb-0002")),
		callableMembers: couchbaseutil.NewMemberSet(member("cb-0000"), member("cb-0001")),
	}

	baseline, err := c.readSnapshotBackupBaseline()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if baseline != nil {
		t.Errorf("expected no baseline while cb-0002 isn't ready, got %+v", baseline)
	}
}

// TestCreateVolumeSnapshotOwnedByRun checks that a snapshot is owned by the run that captured
// it, so deleting the run deletes the snapshot too.
func TestCreateVolumeSnapshotOwnedByRun(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{volumeSnapshotGVR: "VolumeSnapshotList"},
	)

	c := &Cluster{
		k8s:     &client.Client{DynamicClient: dynamicClient},
		cluster: &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace}},
		log:     logf.Log.WithName("test"),
	}

	run := &couchbasev2.CouchbaseSnapshotBackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: fakeClusterNamespace, UID: "test-run-uid"},
	}

	if _, err := c.createVolumeSnapshot(t.Context(), run, "test-class", "cb-0000-default-00"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	list, err := dynamicClient.Resource(volumeSnapshotGVR).Namespace(fakeClusterNamespace).List(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list snapshots: %v", err)
	}

	if len(list.Items) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(list.Items))
	}

	owners := list.Items[0].GetOwnerReferences()
	if len(owners) != 1 || !reflect.DeepEqual(owners[0], run.AsOwner()) {
		t.Errorf("expected the snapshot to be owned by test-run, got %+v", owners)
	}
}
