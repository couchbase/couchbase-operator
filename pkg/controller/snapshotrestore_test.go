/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package controller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	couchbasefake "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned/fake"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	vtypes "github.com/couchbase/couchbase-operator/pkg/validator/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const restoreTestNamespace = "default"

// restoreTestRestore makes a restore of my-backup-run into cluster target, in the given phase.
func restoreTestRestore(phase couchbasev2.CouchbaseSnapshotBackupRestorePhase) *couchbasev2.CouchbaseSnapshotBackupRestore {
	return &couchbasev2.CouchbaseSnapshotBackupRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "restore", Namespace: restoreTestNamespace, UID: "restore-1"},
		Spec: couchbasev2.CouchbaseSnapshotBackupRestoreSpec{
			BackupName: "my-backup",
			RunName:    "my-backup-run",
			Target:     couchbasev2.CouchbaseSnapshotBackupRestoreTarget{Name: "target"},
		},
		Status: couchbasev2.CouchbaseSnapshotBackupRestoreStatus{Phase: phase},
	}
}

// restoreTestCluster makes a new 3 node target on 7.6.8 with a default volume, hibernated or not.
func restoreTestCluster(hibernated bool, size int) *couchbasev2.CouchbaseCluster {
	c := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: restoreTestNamespace},
		Spec: couchbasev2.ClusterSpec{
			Image:     "couchbase/server:7.6.8",
			Hibernate: hibernated,
			Servers: []couchbasev2.ServerConfig{{
				Name:         "all",
				Size:         size,
				VolumeMounts: &couchbasev2.VolumeMounts{DefaultClaim: "couchbase"},
			}},
			VolumeClaimTemplates: []couchbasev2.PersistentVolumeClaimTemplate{
				{ObjectMeta: couchbasev2.NamedObjectMeta{Name: "couchbase"}},
			},
		},
	}

	if hibernated {
		c.Status.SetHibernatingCondition("Cluster hibernating")
	}

	return c
}

// restoreTestRun makes a run of 3 nodes on 7.6.8, one default volume each.
func restoreTestRun(phase couchbasev2.CouchbaseSnapshotBackupRunPhase) *couchbasev2.CouchbaseSnapshotBackupRun {
	run := &couchbasev2.CouchbaseSnapshotBackupRun{
		ObjectMeta: metav1.ObjectMeta{Name: "my-backup-run", Namespace: restoreTestNamespace},
		Spec:       couchbasev2.CouchbaseSnapshotBackupRunSpec{SourceBackup: "my-backup", ServerVersion: "7.6.8", NodeCount: 3, ClusterUUID: "f50608c895e226375ef4a3a6fb76229f"},
		Status:     couchbasev2.CouchbaseSnapshotBackupRunStatus{Phase: phase},
	}

	for _, member := range []string{"target-0000", "target-0001", "target-0002"} {
		run.Status.Snapshots = append(run.Status.Snapshots, couchbasev2.CouchbaseSnapshotBackupRunSnapshot{
			Member:       member,
			SnapshotName: member + "-snapshot",
			MountPath:    "/opt/couchbase/var/lib/couchbase",
		})
	}

	return run
}

// restoreTestSnapshot makes a VolumeSnapshot, ready or not.
func restoreTestSnapshot(name string, ready bool) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetAPIVersion("snapshot.storage.k8s.io/v1")
	object.SetKind("VolumeSnapshot")
	object.SetNamespace(restoreTestNamespace)
	object.SetName(name)

	_ = unstructured.SetNestedField(object.Object, ready, "status", "readyToUse")

	return object
}

// restoreTestSetup is what one restore test case starts with.
type restoreTestSetup struct {
	restore   *couchbasev2.CouchbaseSnapshotBackupRestore
	cluster   *couchbasev2.CouchbaseCluster
	run       *couchbasev2.CouchbaseSnapshotBackupRun
	snapshots []runtime.Object
	volumes   []client.Object
}

// newRestoreTestReconciler builds a reconciler on fake clients holding the setup's objects.
func newRestoreTestReconciler(t *testing.T, setup restoreTestSetup) (*CouchbaseSnapshotBackupRestoreReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := couchbasev2.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}

	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}

	objects := append([]client.Object{setup.restore}, setup.volumes...)
	couchbaseObjects := []runtime.Object{}

	if setup.cluster != nil {
		objects = append(objects, setup.cluster)
		couchbaseObjects = append(couchbaseObjects, setup.cluster)
	}

	// The run is read through the controller's own client, and the validator's.
	if setup.run != nil {
		objects = append(objects, setup.run)
		couchbaseObjects = append(couchbaseObjects, setup.run)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&couchbasev2.CouchbaseSnapshotBackupRestore{}, &couchbasev2.CouchbaseCluster{}).
		Build()

	// Secrets are read through the typed client, so they go there.
	var kubeObjects []runtime.Object

	for _, object := range setup.volumes {
		if secret, ok := object.(*corev1.Secret); ok {
			kubeObjects = append(kubeObjects, secret)
		}

		if event, ok := object.(*corev1.Event); ok {
			kubeObjects = append(kubeObjects, event)
		}
	}

	// The snapshot API is installed, so the validator's add on check passes.
	kubeClient := k8sfake.NewSimpleClientset(kubeObjects...)
	kubeClient.Discovery().(*discoveryfake.FakeDiscovery).Resources = []*metav1.APIResourceList{{GroupVersion: "snapshot.storage.k8s.io/v1"}}

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{volumeSnapshotResource: "VolumeSnapshotList"},
		setup.snapshots...,
	)

	return &CouchbaseSnapshotBackupRestoreReconciler{
		client:           fakeClient,
		kubeClient:       kubeClient,
		couchbaseClient:  couchbasefake.NewSimpleClientset(couchbaseObjects...),
		validatorOptions: &vtypes.ValidatorOptions{},
		dynamic:          dynamicClient,
	}, fakeClient
}

// readySnapshots makes a ready snapshot for every member of restoreTestRun.
func readySnapshots() []runtime.Object {
	return []runtime.Object{
		restoreTestSnapshot("target-0000-snapshot", true),
		restoreTestSnapshot("target-0001-snapshot", true),
		restoreTestSnapshot("target-0002-snapshot", true),
	}
}

// TestSnapshotRestoreValidate checks how the Validating phase decides a restore, it fails when the
// target is missing, a check fails, or a snapshot is missing or not ready, waits while the target is
// running, shows warnings, and moves on to Destroying when everything is fine. A finished restore is
// left alone.
func TestSnapshotRestoreValidate(t *testing.T) {
	completeRun := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)

	testcases := []struct {
		name          string
		setup         restoreTestSetup
		expectedPhase couchbasev2.CouchbaseSnapshotBackupRestorePhase
		// expectedFailure, if set, must appear in the failure reason.
		expectedFailure string
		// expectWaiting means the restore should say it's waiting for the target to be hibernated.
		expectWaiting bool
		// expectedWarning, if set, must appear in the Warnings condition.
		expectedWarning string
	}{
		{
			name:            "target doesn't exist",
			setup:           restoreTestSetup{restore: restoreTestRestore("")},
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedFailure: "doesn't exist",
		},
		{
			name:          "target is running, so the restore waits",
			setup:         restoreTestSetup{restore: restoreTestRestore(""), cluster: restoreTestCluster(false, 3)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseValidating,
			expectWaiting: true,
		},
		{
			name:          "a finished restore is left alone",
			setup:         restoreTestSetup{restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete,
		},
		{
			name: "a check fails, so the restore fails",
			setup: restoreTestSetup{
				restore:   restoreTestRestore(""),
				cluster:   restoreTestCluster(true, 3),
				run:       restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid),
				snapshots: readySnapshots(),
			},
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedFailure: "only a Complete run can be restored",
		},
		{
			name: "a snapshot is missing, so the restore fails",
			setup: restoreTestSetup{
				restore:   restoreTestRestore(""),
				cluster:   restoreTestCluster(true, 3),
				run:       completeRun,
				snapshots: readySnapshots()[:2],
			},
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedFailure: "snapshot target-0002-snapshot for member target-0002 at /opt/couchbase/var/lib/couchbase doesn't exist",
		},
		{
			name: "a snapshot isn't ready, so the restore fails",
			setup: restoreTestSetup{
				restore:   restoreTestRestore(""),
				cluster:   restoreTestCluster(true, 3),
				run:       completeRun,
				snapshots: append(readySnapshots()[:2], restoreTestSnapshot("target-0002-snapshot", false)),
			},
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedFailure: "isn't ready to use",
		},
		{
			name: "a warning is shown and the restore goes ahead",
			setup: restoreTestSetup{
				restore:   restoreTestRestore(""),
				cluster:   restoreTestCluster(true, 5),
				run:       completeRun,
				snapshots: readySnapshots(),
			},
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseDestroying,
			expectedWarning: "the other 2 start empty",
		},
		{
			name: "everything is fine, so the restore moves on",
			setup: restoreTestSetup{
				restore:   restoreTestRestore(""),
				cluster:   restoreTestCluster(true, 3),
				run:       completeRun,
				snapshots: readySnapshots(),
			},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseDestroying,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			r, fakeClient := newRestoreTestReconciler(t, testcase.setup)
			key := types.NamespacedName{Namespace: restoreTestNamespace, Name: "restore"}

			if _, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: key}); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got := &couchbasev2.CouchbaseSnapshotBackupRestore{}
			if err := fakeClient.Get(t.Context(), key, got); err != nil {
				t.Fatalf("failed to get restore: %v", err)
			}

			checkRestoreTestResult(t, got, testcase.expectedPhase, testcase.expectedFailure, testcase.expectWaiting, testcase.expectedWarning)
		})
	}
}

// checkRestoreTestResult checks the restore ended up in the expected phase, with the expected
// failure reason, waiting condition and warning.
func checkRestoreTestResult(t *testing.T, got *couchbasev2.CouchbaseSnapshotBackupRestore, phase couchbasev2.CouchbaseSnapshotBackupRestorePhase, failure string, waiting bool, warning string) {
	t.Helper()

	if got.Status.Phase != phase {
		t.Errorf("expected phase %s, got %s (%s)", phase, got.Status.Phase, got.Status.FailureReason)
	}

	if !strings.Contains(got.Status.FailureReason, failure) {
		t.Errorf("expected the failure reason to contain %q, got %q", failure, got.Status.FailureReason)
	}

	if isWaiting := meta.IsStatusConditionFalse(got.Status.Conditions, snapshotRestoreConditionTargetHibernated); isWaiting != waiting {
		t.Errorf("expected waiting for hibernation %v, got conditions %v", waiting, got.Status.Conditions)
	}

	condition := meta.FindStatusCondition(got.Status.Conditions, snapshotRestoreConditionWarnings)

	switch {
	case warning == "" && condition != nil:
		t.Errorf("expected no warnings, got %s", condition.Message)
	case warning != "" && (condition == nil || !strings.Contains(condition.Message, warning)):
		t.Errorf("expected a warning containing %q, got %v", warning, condition)
	}
}

// restoreTestVolume makes a PVC of the given cluster. A member volume carries the member label, a
// backup volume doesn't.
func restoreTestVolume(name, cluster, member string) *corev1.PersistentVolumeClaim {
	labels := map[string]string{constants.LabelCluster: cluster}
	if member != "" {
		labels[constants.LabelNode] = member
	}

	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: restoreTestNamespace, Labels: labels}}
}

// TestSnapshotRestoreDestroy checks the Destroying phase deletes only the target's member volumes,
// keeps its backup volume and other clusters' volumes, waits while any are left, and moves on to
// Provisioning once there are none.
func TestSnapshotRestoreDestroy(t *testing.T) {
	testcases := []struct {
		name          string
		volumes       []client.Object
		expectedPhase couchbasev2.CouchbaseSnapshotBackupRestorePhase
		expectWait    bool
		expectedLeft  []string
		// woken means the user set hibernate to false on the target in the middle of the restore.
		woken bool
		// deleted means the target cluster was removed in the middle of the restore.
		deleted bool
	}{
		{
			name: "old member volumes are deleted, the backup volume is kept",
			volumes: []client.Object{
				restoreTestVolume("target-0000-default-00", "target", "target-0000"),
				restoreTestVolume("target-0001-default-00", "target", "target-0001"),
				restoreTestVolume("target-0002-default-00", "target", "target-0002"),
				restoreTestVolume("my-backup", "target", ""),
			},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseDestroying,
			expectWait:    true,
			expectedLeft:  []string{"my-backup"},
		},
		{
			name:          "a new target has no volumes, so the restore moves on",
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning,
		},
		{
			name:          "another cluster's volumes aren't touched",
			volumes:       []client.Object{restoreTestVolume("other-0000-default-00", "other", "other-0000")},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning,
			expectedLeft:  []string{"other-0000-default-00"},
		},
		{
			name:          "a target woken in the middle fails the restore, and its volumes are kept",
			volumes:       []client.Object{restoreTestVolume("target-0000-default-00", "target", "target-0000")},
			woken:         true,
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedLeft:  []string{"target-0000-default-00"},
		},
		{
			name:          "a target deleted in the middle fails the restore",
			volumes:       []client.Object{restoreTestVolume("target-0000-default-00", "target", "target-0000")},
			deleted:       true,
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedLeft:  []string{"target-0000-default-00"},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			cluster := restoreTestCluster(!testcase.woken, 3)
			if testcase.deleted {
				cluster = nil
			}

			r, fakeClient := newRestoreTestReconciler(t, restoreTestSetup{
				restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseDestroying),
				cluster: cluster,
				volumes: testcase.volumes,
			})
			key := types.NamespacedName{Namespace: restoreTestNamespace, Name: "restore"}

			result, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: key})
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if waited := result.RequeueAfter > 0; waited != testcase.expectWait {
				t.Errorf("expected waiting %v, got %v", testcase.expectWait, result.RequeueAfter)
			}

			got := &couchbasev2.CouchbaseSnapshotBackupRestore{}
			if err := fakeClient.Get(t.Context(), key, got); err != nil {
				t.Fatalf("failed to get restore: %v", err)
			}

			if got.Status.Phase != testcase.expectedPhase {
				t.Errorf("expected phase %s, got %s", testcase.expectedPhase, got.Status.Phase)
			}

			left := &corev1.PersistentVolumeClaimList{}
			if err := fakeClient.List(t.Context(), left); err != nil {
				t.Fatalf("failed to list volumes: %v", err)
			}

			var names []string
			for _, pvc := range left.Items {
				names = append(names, pvc.Name)
			}

			if strings.Join(names, ",") != strings.Join(testcase.expectedLeft, ",") {
				t.Errorf("expected volumes %v left, got %v", testcase.expectedLeft, names)
			}
		})
	}
}

// restoreTestBoundVolume makes a member's default volume that's already bound.
func restoreTestBoundVolume(member string) *corev1.PersistentVolumeClaim {
	volume := restoreTestVolume(member+"-default-00", "target", member)
	volume.Status.Phase = corev1.ClaimBound

	return volume
}

// restoreTestBinderPod makes a leftover binder pod for a member.
func restoreTestBinderPod(member string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      "restore-bind-" + member,
		Namespace: restoreTestNamespace,
		Labels:    map[string]string{snapshotRestoreLabel: "restore"},
	}}
}

// TestSnapshotRestoreProvision checks the Provisioning phase makes every member's volume from its
// snapshot, owned by the target, binds unbound ones with a short lived pod, removes those pods once
// everything is bound, and fails when a member has no server class to go into.
func TestSnapshotRestoreProvision(t *testing.T) {
	members := []string{"target-0000", "target-0001", "target-0002"}

	recordedClass := func(class string) *couchbasev2.CouchbaseSnapshotBackupRun {
		run := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)
		for i := range run.Status.Snapshots {
			run.Status.Snapshots[i].ServerClass = class
		}

		return run
	}

	testcases := []struct {
		name            string
		cluster         *couchbasev2.CouchbaseCluster
		run             *couchbasev2.CouchbaseSnapshotBackupRun
		volumes         []client.Object
		expectedPhase   couchbasev2.CouchbaseSnapshotBackupRestorePhase
		expectedFailure string
		// expectedBinders is how many binder pods should be left.
		expectedBinders int
		// expectSeeded means the target should now carry the backed up cluster's ID and saved state.
		expectSeeded bool
		// expectedPassword is the saved password the operator logs in with, empty if none is seeded.
		expectedPassword string
	}{
		{
			name:            "volumes are made from the snapshots and bound by a pod each",
			cluster:         restoreTestCluster(true, 3),
			run:             restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning,
			expectedBinders: 3,
		},
		{
			name:            "every member is restored even if the target asks for fewer",
			cluster:         restoreTestCluster(true, 2),
			run:             restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning,
			expectedBinders: 3,
		},
		{
			name:    "once every volume is bound, the binder pods go and the restore is ready to start",
			cluster: restoreTestCluster(true, 3),
			run:     restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete),
			volumes: []client.Object{
				restoreTestBoundVolume("target-0000"), restoreTestBoundVolume("target-0001"), restoreTestBoundVolume("target-0002"),
				restoreTestBinderPod("target-0000"), restoreTestBinderPod("target-0001"), restoreTestBinderPod("target-0002"),
				restoreTestState(nil),
			},
			expectSeeded:  true,
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart,
		},
		{
			name:    "the admin password from the backup is seeded, so the operator can log in",
			cluster: restoreTestCluster(true, 3),
			run:     restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete),
			volumes: []client.Object{
				restoreTestBoundVolume("target-0000"), restoreTestBoundVolume("target-0001"), restoreTestBoundVolume("target-0002"),
				restoreTestState(nil),
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "my-backup-run-admin", Namespace: restoreTestNamespace},
					Data:       map[string][]byte{"username": []byte("Administrator"), "password": []byte("backup-time-password")},
				},
			},
			expectSeeded:     true,
			expectedPassword: "backup-time-password",
			expectedPhase:    couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart,
		},
		{
			name:            "a member whose server class isn't in the target fails the restore, and its binder pods go",
			cluster:         restoreTestCluster(true, 3),
			run:             recordedClass("missing"),
			volumes:         []client.Object{restoreTestBinderPod("target-0000"), restoreTestBinderPod("target-0001")},
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedFailure: "server class missing, which cluster target doesn't have",
		},
		{
			name:            "a target deleted in the middle fails the restore before any volume is made",
			run:             restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedFailure: "was deleted before the restore finished",
		},
		{
			name:            "a target woken in the middle fails the restore before any volume is made",
			cluster:         restoreTestCluster(false, 3),
			run:             restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectedFailure: "was woken before the restore finished",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			r, fakeClient := newRestoreTestReconciler(t, restoreTestSetup{
				restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning),
				cluster: testcase.cluster,
				run:     testcase.run,
				volumes: testcase.volumes,
			})
			key := types.NamespacedName{Namespace: restoreTestNamespace, Name: "restore"}

			if _, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: key}); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got := &couchbasev2.CouchbaseSnapshotBackupRestore{}
			if err := fakeClient.Get(t.Context(), key, got); err != nil {
				t.Fatalf("failed to get restore: %v", err)
			}

			checkRestoreTestResult(t, got, testcase.expectedPhase, testcase.expectedFailure, false, "")

			pods := &corev1.PodList{}
			if err := fakeClient.List(t.Context(), pods, client.MatchingLabels{snapshotRestoreLabel: "restore"}); err != nil {
				t.Fatalf("failed to list pods: %v", err)
			}

			if len(pods.Items) != testcase.expectedBinders {
				t.Errorf("expected %d binder pods, got %d", testcase.expectedBinders, len(pods.Items))
			}

			if testcase.expectedPhase == couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed {
				return
			}

			checkRestoreTestVolumes(t, fakeClient, members)

			if testcase.expectSeeded {
				checkRestoreTestSeeded(t, r, fakeClient, testcase.expectedPassword)
			}
		})
	}
}

// checkRestoreTestVolumes checks each member's default volume exists, is owned by the target, and,
// if the restore made it, comes from that member's snapshot.
func checkRestoreTestVolumes(t *testing.T, fakeClient client.Client, members []string) {
	t.Helper()

	for _, member := range members {
		volume := &corev1.PersistentVolumeClaim{}
		if err := fakeClient.Get(t.Context(), types.NamespacedName{Namespace: restoreTestNamespace, Name: member + "-default-00"}, volume); err != nil {
			t.Errorf("expected a volume for %s, got %v", member, err)
			continue
		}

		// Volumes that were already there before this pass weren't made by it.
		if volume.Status.Phase == corev1.ClaimBound {
			continue
		}

		if volume.Spec.DataSource == nil || volume.Spec.DataSource.Name != member+"-snapshot" {
			t.Errorf("expected %s to come from snapshot %s, got %v", volume.Name, member+"-snapshot", volume.Spec.DataSource)
		}

		if len(volume.OwnerReferences) != 1 || volume.OwnerReferences[0].Name != "target" {
			t.Errorf("expected %s to be owned by cluster target, got %v", volume.Name, volume.OwnerReferences)
		}

		if volume.Labels[constants.LabelNode] != member || volume.Annotations[constants.AnnotationVolumeMountPath] != "/opt/couchbase/var/lib/couchbase" {
			t.Errorf("expected %s to carry the operator's member label and mount path, got %v %v", volume.Name, volume.Labels, volume.Annotations)
		}

		if _, ok := volume.Annotations[constants.AnnotationSnapshotRestoreRemap]; !ok {
			t.Errorf("expected %s to be marked for a node remap, got %v", volume.Name, volume.Annotations)
		}

		if id := volume.Annotations[constants.AnnotationSnapshotRestoreRemapID]; id != "restore-1" {
			t.Errorf("expected %s to carry the restore's UID for the node remap, got %q", volume.Name, id)
		}
	}
}

// restoreTestState makes the target's saved state Secret, with the given saved values.
func restoreTestState(data map[string]string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: restoreTestNamespace},
		Data:       map[string][]byte{},
	}

	for key, value := range data {
		secret.Data[key] = []byte(value)
	}

	return secret
}

// checkRestoreTestSeeded checks the target now carries the backed up cluster's ID, and its saved
// state has that ID, the backed up server version, a next member number past the restored ones,
// and the admin password from the backup, if the run saved one.
func checkRestoreTestSeeded(t *testing.T, r *CouchbaseSnapshotBackupRestoreReconciler, fakeClient client.Client, password string) {
	t.Helper()

	target := &couchbasev2.CouchbaseCluster{}
	if err := fakeClient.Get(t.Context(), types.NamespacedName{Namespace: restoreTestNamespace, Name: "target"}, target); err != nil {
		t.Fatalf("failed to get target: %v", err)
	}

	if target.Status.ClusterID != "f50608c895e226375ef4a3a6fb76229f" {
		t.Errorf("expected the target to carry the backed up cluster ID, got %q", target.Status.ClusterID)
	}

	state, err := r.kubeClient.CoreV1().Secrets(restoreTestNamespace).Get(t.Context(), "target", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get saved state: %v", err)
	}

	expected := map[string]string{"uuid": "f50608c895e226375ef4a3a6fb76229f", "version": "7.6.8", "podIndex": "3", "password": password}
	for key, value := range expected {
		if string(state.Data[key]) != value {
			t.Errorf("expected saved %s %q, got %q", key, value, state.Data[key])
		}
	}
}

// TestRestorePodIndex checks the next member number is past every restored member, and never goes
// below what the cluster already used.
func TestRestorePodIndex(t *testing.T) {
	run := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)

	testcases := []struct {
		name     string
		saved    map[string]string
		expected int
	}{
		{name: "nothing saved, the next is past the restored members", expected: 3},
		{name: "a higher saved number is kept", saved: map[string]string{"podIndex": "7"}, expected: 7},
		{name: "a lower saved number is raised past the restored members", saved: map[string]string{"podIndex": "1"}, expected: 3},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			got, err := restorePodIndex(restoreTestState(testcase.saved), run)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != testcase.expected {
				t.Errorf("expected %d, got %d", testcase.expected, got)
			}
		})
	}
}

// TestSnapshotRestoreStart checks a restore waits for the user to wake the target, wakes it itself
// only when asked to, and finishes once the restored cluster is available and balanced.
func TestSnapshotRestoreStart(t *testing.T) {
	healthy := func() *couchbasev2.CouchbaseCluster {
		c := restoreTestCluster(false, 3)
		c.Status.SetReadyCondition()
		c.Status.SetBalancedCondition()

		return c
	}

	withStart := func(phase couchbasev2.CouchbaseSnapshotBackupRestorePhase) *couchbasev2.CouchbaseSnapshotBackupRestore {
		restore := restoreTestRestore(phase)
		restore.Spec.StartClusterWhenReady = true

		return restore
	}

	testcases := []struct {
		name          string
		restore       *couchbasev2.CouchbaseSnapshotBackupRestore
		cluster       *couchbasev2.CouchbaseCluster
		expectedPhase couchbasev2.CouchbaseSnapshotBackupRestorePhase
		// expectHibernated is whether the target should still be hibernated afterwards.
		expectHibernated bool
		// expectWaitingForUser means the restore should say the user has to wake the target.
		expectWaitingForUser bool
	}{
		{
			name:                 "target still hibernated and the restore isn't asked to wake it, so it waits",
			restore:              restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart),
			cluster:              restoreTestCluster(true, 3),
			expectedPhase:        couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart,
			expectHibernated:     true,
			expectWaitingForUser: true,
		},
		{
			name:          "the restore is asked to wake the target, so it does",
			restore:       withStart(couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart),
			cluster:       restoreTestCluster(true, 3),
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping,
		},
		{
			name:          "the user already woke the target",
			restore:       restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart),
			cluster:       restoreTestCluster(false, 3),
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping,
		},
		{
			name:          "the restored cluster is available and balanced, so the restore is complete",
			restore:       restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseWaitingForCluster),
			cluster:       healthy(),
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete,
		},
		{
			name:          "the restored cluster isn't up yet, so the restore keeps waiting",
			restore:       restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseWaitingForCluster),
			cluster:       restoreTestCluster(false, 3),
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseWaitingForCluster,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			r, fakeClient := newRestoreTestReconciler(t, restoreTestSetup{restore: testcase.restore, cluster: testcase.cluster})
			key := types.NamespacedName{Namespace: restoreTestNamespace, Name: "restore"}

			if _, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: key}); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got := &couchbasev2.CouchbaseSnapshotBackupRestore{}
			if err := fakeClient.Get(t.Context(), key, got); err != nil {
				t.Fatalf("failed to get restore: %v", err)
			}

			checkRestoreTestResult(t, got, testcase.expectedPhase, "", false, "")

			if waiting := meta.IsStatusConditionTrue(got.Status.Conditions, snapshotRestoreConditionWaitingToStart); waiting != testcase.expectWaitingForUser {
				t.Errorf("expected waiting for the user %v, got conditions %v", testcase.expectWaitingForUser, got.Status.Conditions)
			}

			if testcase.expectedPhase == couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete && got.Status.CompletionTime == nil {
				t.Errorf("expected a completion time on a complete restore")
			}

			target := &couchbasev2.CouchbaseCluster{}
			if err := fakeClient.Get(t.Context(), types.NamespacedName{Namespace: restoreTestNamespace, Name: "target"}, target); err != nil {
				t.Fatalf("failed to get target: %v", err)
			}

			if target.Spec.Hibernate != testcase.expectHibernated {
				t.Errorf("expected the target hibernated %v, got %v", testcase.expectHibernated, target.Spec.Hibernate)
			}
		})
	}
}

// TestSnapshotRestoreWaitWritesOnce checks a restore that's waiting, for its target to be hibernated
// or for the user to wake it, writes its status once and not again on every recheck.
func TestSnapshotRestoreWaitWritesOnce(t *testing.T) {
	testcases := []struct {
		name    string
		restore *couchbasev2.CouchbaseSnapshotBackupRestore
		cluster *couchbasev2.CouchbaseCluster
	}{
		{
			name:    "waiting for the target to be hibernated",
			restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseValidating),
			cluster: restoreTestCluster(false, 3),
		},
		{
			name:    "waiting for the user to wake the target",
			restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart),
			cluster: restoreTestCluster(true, 3),
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			r, fakeClient := newRestoreTestReconciler(t, restoreTestSetup{restore: testcase.restore, cluster: testcase.cluster})
			key := types.NamespacedName{Namespace: restoreTestNamespace, Name: "restore"}

			versions := []string{}

			for range 2 {
				if _, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: key}); err != nil {
					t.Fatalf("expected no error, got %v", err)
				}

				got := &couchbasev2.CouchbaseSnapshotBackupRestore{}
				if err := fakeClient.Get(t.Context(), key, got); err != nil {
					t.Fatalf("failed to get restore: %v", err)
				}

				versions = append(versions, got.ResourceVersion)
			}

			if versions[0] != versions[1] {
				t.Errorf("expected no status write on the second check, resource version went from %s to %s", versions[0], versions[1])
			}
		})
	}
}

// TestRestoreBinderPod checks the binder pod passes restricted pod security, keeps the cluster's own
// pod security, and takes its image and placement from the member's server class.
func TestRestoreBinderPod(t *testing.T) {
	restore := restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning)

	t.Run("no pod security set on the cluster gets the safest values", func(t *testing.T) {
		pod := restoreBinderPod(restore, restoreTestCluster(true, 3), nil, "target-0000", []string{"target-0000-default-00"}, "")

		security := pod.Spec.SecurityContext
		if security.RunAsNonRoot == nil || !*security.RunAsNonRoot || security.RunAsUser == nil || *security.RunAsUser != 1000 {
			t.Errorf("expected the pod to run as the non root couchbase user, got %+v", security)
		}

		if security.SeccompProfile == nil || security.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
			t.Errorf("expected the RuntimeDefault seccomp profile, got %+v", security.SeccompProfile)
		}

		container := pod.Spec.Containers[0]
		if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
			t.Errorf("expected no privilege escalation")
		}

		if len(container.SecurityContext.Capabilities.Drop) != 1 || container.SecurityContext.Capabilities.Drop[0] != "ALL" {
			t.Errorf("expected every capability dropped, got %v", container.SecurityContext.Capabilities.Drop)
		}

		if container.Resources.Requests.Cpu().IsZero() || container.Resources.Limits.Memory().IsZero() {
			t.Errorf("expected resource requests and limits, got %+v", container.Resources)
		}
	})

	t.Run("the cluster's own pod security and the server class placement are kept", func(t *testing.T) {
		user := int64(2000)
		group := int64(3000)

		cluster := restoreTestCluster(true, 3)
		cluster.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsUser: &user, FSGroup: &group}

		config := &couchbasev2.ServerConfig{
			Name:  "all",
			Image: "couchbase/server:8.0.1",
			Pod: &couchbasev2.PodTemplate{Spec: corev1.PodSpec{
				ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry"}},
				Tolerations:      []corev1.Toleration{{Key: "dedicated", Value: "couchbase"}},
				NodeSelector:     map[string]string{"pool": "couchbase"},
			}},
		}

		pod := restoreBinderPod(restore, cluster, config, "target-0000", []string{"target-0000-default-00"}, "zone-b")

		if *pod.Spec.SecurityContext.RunAsUser != 2000 || *pod.Spec.SecurityContext.FSGroup != 3000 {
			t.Errorf("expected the cluster's user and fsGroup, got %+v", pod.Spec.SecurityContext)
		}

		if pod.Spec.Containers[0].Image != "couchbase/server:8.0.1" {
			t.Errorf("expected the server class image, got %s", pod.Spec.Containers[0].Image)
		}

		if len(pod.Spec.ImagePullSecrets) != 1 || len(pod.Spec.Tolerations) != 1 || pod.Spec.NodeSelector["pool"] != "couchbase" {
			t.Errorf("expected the server class pull secrets, tolerations and node selector, got %+v", pod.Spec)
		}

		if pod.Spec.NodeSelector[constants.ServerGroupLabel] != "zone-b" {
			t.Errorf("expected the pod to be pinned to the backed up volume's zone, got %v", pod.Spec.NodeSelector)
		}

		if _, ok := config.Pod.Spec.NodeSelector[constants.ServerGroupLabel]; ok {
			t.Errorf("expected the server class's own node selector to be left alone, got %v", config.Pod.Spec.NodeSelector)
		}
	})
}

// TestRestoreVolumesPlacement checks restored volumes get the server group and zone recorded on the
// run, the same way the operator annotates its own volumes, and older runs without them are left as
// they are.
func TestRestoreVolumesPlacement(t *testing.T) {
	target := restoreTestCluster(true, 3)
	target.Spec.Platform = couchbasev2.PlatformTypeAWS

	placed := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)
	for i := range placed.Status.Snapshots {
		placed.Status.Snapshots[i].ServerGroup = "group-" + placed.Status.Snapshots[i].Member
		placed.Status.Snapshots[i].AvailabilityZone = "zone-" + placed.Status.Snapshots[i].Member
	}

	for name, run := range map[string]*couchbasev2.CouchbaseSnapshotBackupRun{
		"recorded":  placed,
		"older run": restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete),
	} {
		t.Run(name, func(t *testing.T) {
			volumes, err := restoreVolumes(target, run, "restore-1")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			for _, volume := range volumes {
				member := volume.Labels[constants.LabelNode]

				group, zone, csiZone := "", "", ""
				if run == placed {
					group, zone, csiZone = "group-"+member, "zone-"+member, "zone-"+member
				}

				if volume.Annotations[constants.AnnotationServerGroup] != group || volume.Annotations[constants.AnnotationAvailabilityZone] != zone || volume.Annotations[constants.EKSTopologyLabel] != csiZone {
					t.Errorf("expected %s in group %q zone %q, got %v", volume.Name, group, zone, volume.Annotations)
				}
			}
		})
	}
}

// restoreTestRemapPod makes a member's pod with the given state for its node remap init container.
func restoreTestRemapPod(member string, status corev1.ContainerStatus) *corev1.Pod {
	status.Name = snapshotRestoreRemapContainer

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: member, Namespace: restoreTestNamespace},
		Status:     corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{status}},
	}
}

// restoreTestExited is an init container state for one that exited with the given code.
func restoreTestExited(code int32) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: code, Reason: "Error"}}
}

// TestRemapState checks how a pod's node remap is read, running, finished, failed, and waiting to
// run again after a failure.
func TestRemapState(t *testing.T) {
	testcases := []struct {
		name            string
		status          corev1.ContainerStatus
		expectedDone    bool
		expectedFailure string
	}{
		{
			name:   "still running",
			status: corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		},
		{
			name:         "finished",
			status:       corev1.ContainerStatus{State: restoreTestExited(0)},
			expectedDone: true,
		},
		{
			name:            "failed",
			status:          corev1.ContainerStatus{State: restoreTestExited(1)},
			expectedFailure: "exit code 1",
		},
		{
			name: "waiting to run again after a failure",
			status: corev1.ContainerStatus{
				State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				LastTerminationState: restoreTestExited(2),
			},
			expectedFailure: "exit code 2",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			done, failure := remapState(restoreTestRemapPod("target-0000", testcase.status))

			if done != testcase.expectedDone {
				t.Errorf("expected done %v, got %v", testcase.expectedDone, done)
			}

			if !strings.Contains(failure, testcase.expectedFailure) || (testcase.expectedFailure == "" && failure != "") {
				t.Errorf("expected failure %q, got %q", testcase.expectedFailure, failure)
			}
		})
	}
}

// TestSnapshotRestoreRemap checks the Remapping phase waits for each restored pod's node remap,
// fails the restore if one fails, and once all are done removes the marks and moves on.
func TestSnapshotRestoreRemap(t *testing.T) {
	marked := func(member string) *corev1.PersistentVolumeClaim {
		volume := restoreTestVolume(member+"-default-00", "target", member)
		volume.Annotations = map[string]string{constants.AnnotationSnapshotRestoreRemap: "true"}

		return volume
	}

	finished := corev1.ContainerStatus{State: restoreTestExited(0)}

	testcases := []struct {
		name          string
		objects       []client.Object
		expectedPhase couchbasev2.CouchbaseSnapshotBackupRestorePhase
		expectFailure string
		// expectMarked is whether the volumes should still be marked afterwards.
		expectMarked bool
	}{
		{
			name:          "the operator hasn't made a pod yet, so it waits",
			objects:       []client.Object{marked("target-0000"), marked("target-0001"), restoreTestRemapPod("target-0000", finished)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping,
			expectMarked:  true,
		},
		{
			name:          "every node remap finished, so the marks are removed",
			objects:       []client.Object{marked("target-0000"), marked("target-0001"), restoreTestRemapPod("target-0000", finished), restoreTestRemapPod("target-0001", finished)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseWaitingForCluster,
		},
		{
			name:          "a node remap failed, so the restore fails",
			objects:       []client.Object{marked("target-0000"), restoreTestRemapPod("target-0000", corev1.ContainerStatus{State: restoreTestExited(1)})},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed,
			expectFailure: "node remap failed on pod target-0000",
			expectMarked:  true,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			r, fakeClient := newRestoreTestReconciler(t, restoreTestSetup{
				restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping),
				cluster: restoreTestCluster(false, 3),
				volumes: testcase.objects,
			})
			key := types.NamespacedName{Namespace: restoreTestNamespace, Name: "restore"}

			if _, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: key}); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got := &couchbasev2.CouchbaseSnapshotBackupRestore{}
			if err := fakeClient.Get(t.Context(), key, got); err != nil {
				t.Fatalf("failed to get restore: %v", err)
			}

			checkRestoreTestResult(t, got, testcase.expectedPhase, testcase.expectFailure, false, "")

			volume := &corev1.PersistentVolumeClaim{}
			if err := fakeClient.Get(t.Context(), types.NamespacedName{Namespace: restoreTestNamespace, Name: "target-0000-default-00"}, volume); err != nil {
				t.Fatalf("failed to get volume: %v", err)
			}

			if _, ok := volume.Annotations[constants.AnnotationSnapshotRestoreRemap]; ok != testcase.expectMarked {
				t.Errorf("expected the volume marked %v, got %v", testcase.expectMarked, volume.Annotations)
			}
		})
	}
}

// TestRestoreVolumesRename checks a restore into a differently named cluster gives each member the
// target's name with the same number, makes its volumes from the backed up member's snapshots, and
// marks every volume with all the backed up members and their hostnames for the node remap.
func TestRestoreVolumesRename(t *testing.T) {
	target := restoreTestCluster(true, 3)

	run := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)
	run.Labels = map[string]string{constants.LabelCluster: "src"}

	for i := range run.Status.Snapshots {
		run.Status.Snapshots[i].Member = strings.Replace(run.Status.Snapshots[i].Member, "target", "src", 1)
		run.Status.Snapshots[i].SnapshotName = strings.Replace(run.Status.Snapshots[i].SnapshotName, "target", "src", 1)
	}

	run.Spec.Hostnames = map[string]string{
		"src-0000": "src-0000.src.default.svc",
		"src-0001": "src-0001.src.default.svc",
		"src-0002": "src-0002.src.default.svc",
	}

	volumes, err := restoreVolumes(target, run, "restore-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(volumes) != 3 {
		t.Fatalf("expected 3 volumes, got %d", len(volumes))
	}

	for i, volume := range volumes {
		member := fmt.Sprintf("target-%04d", i)

		if volume.Name != member+"-default-00" || volume.Labels[constants.LabelNode] != member {
			t.Errorf("expected volume %s-default-00 for member %s, got %s with labels %v", member, member, volume.Name, volume.Labels)
		}

		if snapshot := fmt.Sprintf("src-%04d-snapshot", i); volume.Spec.DataSource == nil || volume.Spec.DataSource.Name != snapshot {
			t.Errorf("expected %s to come from snapshot %s, got %v", volume.Name, snapshot, volume.Spec.DataSource)
		}

		if mark := volume.Annotations[constants.AnnotationSnapshotRestoreRemap]; mark != "src-0000=src-0000.src.default.svc,src-0001=src-0001.src.default.svc,src-0002=src-0002.src.default.svc" {
			t.Errorf("expected %s to list every backed up member for the remap, got %q", volume.Name, mark)
		}
	}
}

// TestRestoreVolumesHostnames checks each volume carries every backed up member's
// recorded hostname for the node remap, when the run recorded them.
func TestRestoreVolumesHostnames(t *testing.T) {
	run := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)
	run.Spec.Hostnames = map[string]string{
		"target-0000": "node-1.example.com",
		"target-0001": "node-2.example.com",
		"target-0002": "node-3.example.com",
	}

	volumes, err := restoreVolumes(restoreTestCluster(true, 3), run, "restore-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	want := "target-0000=node-1.example.com,target-0001=node-2.example.com,target-0002=node-3.example.com"

	for _, volume := range volumes {
		if mark := volume.Annotations[constants.AnnotationSnapshotRestoreRemap]; mark != want {
			t.Errorf("expected %s marked %q, got %q", volume.Name, want, mark)
		}
	}
}

// TestSnapshotRestoreRemapNodeHostnames checks that for nodes named after the k8s
// nodes they run on, the restore waits until every pod is placed, then writes each
// old and new hostname, and removes the list once every node remap has finished.
func TestSnapshotRestoreRemapNodeHostnames(t *testing.T) {
	target := restoreTestCluster(false, 2)
	target.Spec.Networking.InitPodsWithNodeHostname = true
	target.Spec.Networking.ImprovedHostNetwork = true

	run := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)
	run.Status.Snapshots = run.Status.Snapshots[:2]
	run.Spec.Hostnames = map[string]string{"target-0000": "node-1.example.com", "target-0001": "node-2.example.com"}

	marked := func(member string) *corev1.PersistentVolumeClaim {
		volume := restoreTestVolume(member+"-default-00", "target", member)
		volume.Annotations = map[string]string{constants.AnnotationSnapshotRestoreRemap: member + "=" + run.Spec.Hostnames[member]}

		return volume
	}

	placed := func(member, node string, status corev1.ContainerStatus) *corev1.Pod {
		pod := restoreTestRemapPod(member, status)
		pod.Spec.NodeName = node

		return pod
	}

	list := func(data string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "target-snapshot-restore-remap", Namespace: restoreTestNamespace},
			Data:       map[string]string{k8sutil.SnapshotRestoreRemapKey: data},
		}
	}

	waiting := corev1.ContainerStatus{}
	finished := corev1.ContainerStatus{State: restoreTestExited(0)}
	filled := "node-1.example.com node-3.example.com\nnode-2.example.com node-4.example.com\n"

	testcases := []struct {
		name          string
		objects       []client.Object
		expectedPhase couchbasev2.CouchbaseSnapshotBackupRestorePhase
		// expectedList is what the list should hold afterwards, "-" if it should be gone.
		expectedList string
	}{
		{
			name:          "a pod isn't placed yet, so the list stays empty",
			objects:       []client.Object{marked("target-0000"), marked("target-0001"), list(""), placed("target-0000", "node-3.example.com", waiting), placed("target-0001", "", waiting)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping,
		},
		{
			name:          "every pod is placed, so the list is written",
			objects:       []client.Object{marked("target-0000"), marked("target-0001"), list(""), placed("target-0000", "node-3.example.com", waiting), placed("target-0001", "node-4.example.com", waiting)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping,
			expectedList:  filled,
		},
		{
			name:          "every node remap finished, so the list is removed",
			objects:       []client.Object{marked("target-0000"), marked("target-0001"), list(filled), placed("target-0000", "node-3.example.com", finished), placed("target-0001", "node-4.example.com", finished)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRestorePhaseWaitingForCluster,
			expectedList:  "-",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			r, fakeClient := newRestoreTestReconciler(t, restoreTestSetup{
				restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping),
				cluster: target,
				run:     run,
				volumes: testcase.objects,
			})
			key := types.NamespacedName{Namespace: restoreTestNamespace, Name: "restore"}

			if _, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: key}); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got := &couchbasev2.CouchbaseSnapshotBackupRestore{}
			if err := fakeClient.Get(t.Context(), key, got); err != nil {
				t.Fatalf("failed to get restore: %v", err)
			}

			checkRestoreTestResult(t, got, testcase.expectedPhase, "", false, "")

			saved := &corev1.ConfigMap{}
			err := fakeClient.Get(t.Context(), types.NamespacedName{Namespace: restoreTestNamespace, Name: "target-snapshot-restore-remap"}, saved)

			switch {
			case testcase.expectedList == "-":
				if !errors.IsNotFound(err) {
					t.Errorf("expected the list removed, got %v", err)
				}
			case err != nil:
				t.Fatalf("failed to get the list: %v", err)
			case saved.Data[k8sutil.SnapshotRestoreRemapKey] != testcase.expectedList:
				t.Errorf("expected the list %q, got %q", testcase.expectedList, saved.Data[k8sutil.SnapshotRestoreRemapKey])
			}
		})
	}
}

// TestResetRemapList checks a list left from an earlier restore of the cluster is emptied and
// taken over by the new restore.
func TestResetRemapList(t *testing.T) {
	leftover := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "target-snapshot-restore-remap", Namespace: restoreTestNamespace},
		Data:       map[string]string{k8sutil.SnapshotRestoreRemapKey: "node-1.example.com node-3.example.com\n"},
	}

	restore := restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning)
	r, fakeClient := newRestoreTestReconciler(t, restoreTestSetup{restore: restore, cluster: restoreTestCluster(true, 3), volumes: []client.Object{leftover}})

	if err := r.resetRemapList(t.Context(), restore, restoreTestCluster(true, 3)); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got := &corev1.ConfigMap{}
	if err := fakeClient.Get(t.Context(), client.ObjectKeyFromObject(leftover), got); err != nil {
		t.Fatalf("failed to get the list: %v", err)
	}

	if len(got.Data) != 0 || len(got.OwnerReferences) != 1 || got.OwnerReferences[0].Name != restore.Name {
		t.Errorf("expected an empty list owned by restore %s, got data %v owners %v", restore.Name, got.Data, got.OwnerReferences)
	}
}

// TestSeedTargetStateRefreshUUID checks the restore asks the operator to read the cluster's UUID
// again only when the target has a new name, since only then does the node remap tool change it.
func TestSeedTargetStateRefreshUUID(t *testing.T) {
	for _, source := range []string{"target", "src"} {
		t.Run("backed up from "+source, func(t *testing.T) {
			target := restoreTestCluster(true, 3)
			r, _ := newRestoreTestReconciler(t, restoreTestSetup{
				restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning),
				cluster: target,
				volumes: []client.Object{restoreTestState(nil)},
			})

			run := restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)
			run.Labels = map[string]string{constants.LabelCluster: source}

			if err := r.seedTargetState(t.Context(), target, run); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			state, err := r.kubeClient.CoreV1().Secrets(restoreTestNamespace).Get(t.Context(), "target", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get saved state: %v", err)
			}

			_, refresh := state.Data[string(persistence.RefreshUUID)]
			if refresh != (source != "target") {
				t.Errorf("expected a refresh %v for a run backed up from %s, got %v", source != "target", source, refresh)
			}
		})
	}
}

// TestSeedTargetStateReloadTLS checks the restore asks the operator to reload the server
// certificate only when the target has TLS.
func TestSeedTargetStateReloadTLS(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%v", tls), func(t *testing.T) {
			target := restoreTestCluster(true, 3)
			if tls {
				target.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
					SecretSource: &couchbasev2.TLSSecretSource{ServerSecretName: "target-tls"},
				}
			}

			r, _ := newRestoreTestReconciler(t, restoreTestSetup{
				restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning),
				cluster: target,
				volumes: []client.Object{restoreTestState(nil)},
			})

			if err := r.seedTargetState(t.Context(), target, restoreTestRun(couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete)); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			state, err := r.kubeClient.CoreV1().Secrets(restoreTestNamespace).Get(t.Context(), "target", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get saved state: %v", err)
			}

			if _, reload := state.Data[string(persistence.ReloadTLS)]; reload != tls {
				t.Errorf("expected a reload %v, got %v", tls, reload)
			}
		})
	}
}

// TestStuckVolume checks the restore only gives up on a volume once it's been unbound for longer
// than the limit, and says what the storage said when there's a message.
func TestStuckVolume(t *testing.T) {
	now := time.Now()

	volume := func(name string, age time.Duration) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         restoreTestNamespace,
			CreationTimestamp: metav1.NewTime(now.Add(-age)),
		}}
	}

	failed := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "stuck-volume-failed", Namespace: restoreTestNamespace},
		InvolvedObject: corev1.ObjectReference{Kind: "PersistentVolumeClaim", Name: "stuck-volume"},
		Reason:         "ProvisioningFailed",
		Message:        "volume source not found",
		LastTimestamp:  metav1.NewTime(now),
	}

	r, _ := newRestoreTestReconciler(t, restoreTestSetup{
		restore: restoreTestRestore(couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning),
		volumes: []client.Object{failed},
	})

	cases := []struct {
		name     string
		volumes  []*corev1.PersistentVolumeClaim
		expected string
	}{
		{"volume made a minute ago", []*corev1.PersistentVolumeClaim{volume("recent-volume", time.Minute)}, ""},
		{"volume made in this pass", []*corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data-volume"}}}, ""},
		{"stuck volume with a message", []*corev1.PersistentVolumeClaim{volume("stuck-volume", time.Hour)}, "the storage said: volume source not found"},
		{"stuck volume without a message", []*corev1.PersistentVolumeClaim{volume("other-volume", time.Hour)}, "volume other-volume still isn't bound"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason := r.stuckVolume(t.Context(), c.volumes, now)

			if (c.expected == "") != (reason == "") || !strings.Contains(reason, c.expected) {
				t.Errorf("expected %q, got %q", c.expected, reason)
			}
		})
	}
}
