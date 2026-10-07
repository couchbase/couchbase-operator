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
	"sort"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	couchbasefake "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned/fake"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	groupsnapshotv1beta1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
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

// TestIsSnapshotBackupRunFor checks that a cluster only counts its own runs for a backup,
// even when another cluster in the same namespace has runs for a backup with the same name.
func TestIsSnapshotBackupRunFor(t *testing.T) {
	c := &Cluster{
		cluster: &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: fakeClusterNamespace}},
	}

	run := func(backupName, clusterName string) *couchbasev2.CouchbaseSnapshotBackupRun {
		return &couchbasev2.CouchbaseSnapshotBackupRun{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.LabelCluster: clusterName}},
			Spec:       couchbasev2.CouchbaseSnapshotBackupRunSpec{SourceBackup: backupName},
		}
	}

	testcases := []struct {
		name     string
		run      *couchbasev2.CouchbaseSnapshotBackupRun
		expected bool
	}{
		{name: "same backup, same cluster", run: run("backup", "cluster-a"), expected: true},
		{name: "same backup, other cluster", run: run("backup", "cluster-b"), expected: false},
		{name: "other backup, same cluster", run: run("other-backup", "cluster-a"), expected: false},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			if got := c.isSnapshotBackupRunFor(testcase.run, "backup"); got != testcase.expected {
				t.Errorf("expected %v, got %v", testcase.expected, got)
			}
		})
	}
}

// TestSnapshotBackupDueAt checks that a new backup waits for its first scheduled time instead
// of running the moment it's created, that later runs follow on from the last Complete run,
// and that Invalid runs are retried up to maxRetries times, retryInterval apart.
func TestSnapshotBackupDueAt(t *testing.T) {
	// Every case is checked at 3pm on 25 September. The scheduled ones use a daily at 2am schedule.
	now := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)

	at := func(day, hour int) metav1.Time {
		return metav1.NewTime(time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC))
	}

	atMinute := func(day, hour, minute int) metav1.Time {
		return metav1.NewTime(time.Date(2026, 9, day, hour, minute, 0, 0, time.UTC))
	}

	backup := func(created metav1.Time) *couchbasev2.CouchbaseBackup {
		return &couchbasev2.CouchbaseBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "backup", CreationTimestamp: created},
			Spec: couchbasev2.CouchbaseBackupSpec{
				SnapshotBackup: &couchbasev2.CouchbaseSnapshotBackupSpec{
					Schedule:      "0 2 * * *",
					RetryInterval: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		}
	}

	unscheduled := func(created metav1.Time) *couchbasev2.CouchbaseBackup {
		b := backup(created)
		b.Spec.SnapshotBackup.Schedule = ""

		return b
	}

	noRetries := func(b *couchbasev2.CouchbaseBackup) *couchbasev2.CouchbaseBackup {
		zero := int32(0)
		b.Spec.SnapshotBackup.MaxRetries = &zero

		return b
	}

	run := func(created metav1.Time) *couchbasev2.CouchbaseSnapshotBackupRun {
		return &couchbasev2.CouchbaseSnapshotBackupRun{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: created}}
	}

	runs := func(created ...metav1.Time) []*couchbasev2.CouchbaseSnapshotBackupRun {
		var list []*couchbasev2.CouchbaseSnapshotBackupRun

		for _, c := range created {
			list = append(list, run(c))
		}

		return list
	}

	testcases := []struct {
		name        string
		backup      *couchbasev2.CouchbaseBackup
		lastRun     *couchbasev2.CouchbaseSnapshotBackupRun
		invalidRuns []*couchbasev2.CouchbaseSnapshotBackupRun
		expected    bool
	}{
		{
			name:     "new backup created at 2pm waits for 2am tomorrow",
			backup:   backup(at(25, 14)),
			expected: false,
		},
		{
			name:     "backup created yesterday has passed its first 2am",
			backup:   backup(at(24, 1)),
			expected: true,
		},
		{
			name:     "last run at 2am today means the next one is 2am tomorrow",
			backup:   backup(at(20, 1)),
			lastRun:  run(at(25, 2)),
			expected: false,
		},
		{
			name:     "last run yesterday means today's 2am is due",
			backup:   backup(at(20, 1)),
			lastRun:  run(at(24, 2)),
			expected: true,
		},
		{
			name:     "backup with no schedule and no runs is due straight away",
			backup:   unscheduled(at(25, 14)),
			expected: true,
		},
		{
			name:     "backup with no schedule is not due again once it has a Complete run",
			backup:   unscheduled(at(25, 14)),
			lastRun:  run(at(25, 14)),
			expected: false,
		},
		{
			name:        "a failure less than retryInterval ago isn't retried yet",
			backup:      backup(at(20, 1)),
			lastRun:     run(at(24, 2)),
			invalidRuns: runs(atMinute(25, 14, 58)),
			expected:    false,
		},
		{
			name:        "a failure more than retryInterval ago is retried",
			backup:      backup(at(20, 1)),
			lastRun:     run(at(24, 2)),
			invalidRuns: runs(atMinute(25, 14, 50)),
			expected:    true,
		},
		{
			name:        "once the retries are used up it waits for the next scheduled time",
			backup:      backup(at(20, 1)),
			lastRun:     run(at(24, 2)),
			invalidRuns: runs(atMinute(25, 2, 0), atMinute(25, 2, 5), atMinute(25, 2, 10), atMinute(25, 2, 15)),
			expected:    false,
		},
		{
			name:        "failures for yesterday's scheduled time don't count today",
			backup:      backup(at(20, 1)),
			lastRun:     run(at(23, 2)),
			invalidRuns: runs(atMinute(24, 2, 0), atMinute(24, 2, 5), atMinute(24, 2, 10), atMinute(24, 2, 15)),
			expected:    true,
		},
		{
			name:        "backup with no schedule is retried after a failure",
			backup:      unscheduled(at(25, 14)),
			invalidRuns: runs(atMinute(25, 14, 30)),
			expected:    true,
		},
		{
			name:        "backup with no schedule stops once its retries are used up",
			backup:      unscheduled(at(25, 14)),
			invalidRuns: runs(atMinute(25, 14, 0), atMinute(25, 14, 5), atMinute(25, 14, 10), atMinute(25, 14, 15)),
			expected:    false,
		},
		{
			name:        "maxRetries of 0 means a failure isn't retried",
			backup:      noRetries(backup(at(20, 1))),
			lastRun:     run(at(24, 2)),
			invalidRuns: runs(atMinute(25, 2, 0)),
			expected:    false,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			due, err := snapshotBackupDueAt(testcase.backup, testcase.lastRun, testcase.invalidRuns, now)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if due != testcase.expected {
				t.Errorf("expected due to be %v, got %v", testcase.expected, due)
			}
		})
	}
}

// TestListSnapshotBackupRunSnapshots checks that a run's snapshots are found by the run label
// for individual snapshots, and by the owning group snapshot for a group snapshot, and that
// snapshots belonging to anything else are left out.
func TestListSnapshotBackupRunSnapshots(t *testing.T) {
	volumeSnapshot := func(name, runLabel, groupOwner string) *unstructured.Unstructured {
		object := &unstructured.Unstructured{}
		object.SetAPIVersion(volumeSnapshotGVR.GroupVersion().String())
		object.SetKind("VolumeSnapshot")
		object.SetNamespace(fakeClusterNamespace)
		object.SetName(name)

		if runLabel != "" {
			object.SetLabels(map[string]string{constants.LabelSnapshotBackupRun: runLabel})
		}

		if groupOwner != "" {
			object.SetOwnerReferences([]metav1.OwnerReference{{
				APIVersion: volumeGroupSnapshotGVR.GroupVersion().String(),
				Kind:       "VolumeGroupSnapshot",
				Name:       groupOwner,
				UID:        "group-uid",
			}})
		}

		return object
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{volumeSnapshotGVR: "VolumeSnapshotList"},
		volumeSnapshot("run-snapshot-1", "test-run", ""),
		volumeSnapshot("run-snapshot-2", "test-run", ""),
		volumeSnapshot("other-run-snapshot", "other-run", ""),
		volumeSnapshot("group-member-1", "", "test-run-group"),
		volumeSnapshot("group-member-2", "", "test-run-group"),
		volumeSnapshot("other-group-member", "", "other-group"),
	)

	c := &Cluster{
		k8s:     &client.Client{DynamicClient: dynamicClient},
		cluster: &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace}},
		log:     logf.Log.WithName("test"),
	}

	groupName := "test-run-group"

	testcases := []struct {
		name     string
		status   couchbasev2.CouchbaseSnapshotBackupRunStatus
		expected []string
	}{
		{
			name:     "individual snapshots are found by the run label",
			expected: []string{"run-snapshot-1", "run-snapshot-2"},
		},
		{
			name:     "group snapshot members are found by their owning group snapshot",
			status:   couchbasev2.CouchbaseSnapshotBackupRunStatus{GroupSnapshot: true, GroupSnapshotName: &groupName},
			expected: []string{"group-member-1", "group-member-2"},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			run := &couchbasev2.CouchbaseSnapshotBackupRun{
				ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: fakeClusterNamespace},
				Status:     testcase.status,
			}

			snapshots, err := c.listSnapshotBackupRunSnapshots(run)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			var names []string

			for _, snapshot := range snapshots {
				names = append(names, snapshot.Name)
			}

			sort.Strings(names)

			if !reflect.DeepEqual(names, testcase.expected) {
				t.Errorf("expected %v, got %v", testcase.expected, names)
			}
		})
	}
}

// TestSnapshotBackupRunEntries checks that a group snapshot's snapshots are recorded on the
// run with the member, volume, snapshot name, and mount path for each one.
func TestSnapshotBackupRunEntries(t *testing.T) {
	snapshot := func(name, pvc string) snapshotv1.VolumeSnapshot {
		return snapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: snapshotv1.VolumeSnapshotSpec{
				Source: snapshotv1.VolumeSnapshotSource{PersistentVolumeClaimName: &pvc},
			},
		}
	}

	members := map[string]string{
		"cb-0000-default-00": "cb-0000",
		"cb-0000-data-00":    "cb-0000",
	}

	mountPaths := map[string]string{
		"cb-0000-default-00": "/opt/couchbase/var/lib/couchbase",
		"cb-0000-data-00":    "/mnt/data",
	}

	entries := snapshotBackupRunEntries(
		[]snapshotv1.VolumeSnapshot{snapshot("member-1", "cb-0000-default-00"), snapshot("member-2", "cb-0000-data-00")},
		func(pvc string) string { return members[pvc] },
		func(pvc string) string { return mountPaths[pvc] },
	)

	expected := []couchbasev2.CouchbaseSnapshotBackupRunSnapshot{
		{Member: "cb-0000", VolumeName: "cb-0000-default-00", SnapshotName: "member-1", MountPath: "/opt/couchbase/var/lib/couchbase"},
		{Member: "cb-0000", VolumeName: "cb-0000-data-00", SnapshotName: "member-2", MountPath: "/mnt/data"},
	}

	if !reflect.DeepEqual(entries, expected) {
		t.Errorf("expected %+v, got %+v", expected, entries)
	}
}

// TestCheckSnapshotBackupRun checks how a run gets decided, errors, missing snapshots, skew,
// the cluster changing or logging a system event while the snapshots were taken, waiting,
// timing out, and completing.
func TestCheckSnapshotBackupRun(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	start := now.Add(-30 * time.Second)

	readyTimeout := 2 * time.Hour
	maxSkew := 10 * time.Second

	// snapshot makes a snapshot taken at start plus the given offset, or not taken yet if
	// offset is negative.
	snapshot := func(name string, offset time.Duration, ready bool) snapshotv1.VolumeSnapshot {
		status := &snapshotv1.VolumeSnapshotStatus{ReadyToUse: &ready}

		if offset >= 0 {
			created := metav1.NewTime(start.Add(offset))
			status.CreationTime = &created
		}

		return snapshotv1.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: status}
	}

	// takenAt makes a ready snapshot taken at the given time.
	takenAt := func(name string, at time.Time) snapshotv1.VolumeSnapshot {
		taken := snapshot(name, 0, true)
		created := metav1.NewTime(at)
		taken.Status.CreationTime = &created

		return taken
	}

	// failed makes a snapshot whose creation failed, so it has an error and no creation time.
	failed := func(name, message string) snapshotv1.VolumeSnapshot {
		failed := snapshot(name, -1, false)
		failed.Status.Error = &snapshotv1.VolumeSnapshotError{Message: &message}

		return failed
	}

	run := func(created time.Time) *couchbasev2.CouchbaseSnapshotBackupRun {
		return &couchbasev2.CouchbaseSnapshotBackupRun{
			ObjectMeta: metav1.ObjectMeta{Name: "test-run", CreationTimestamp: metav1.NewTime(created)},
			Status: couchbasev2.CouchbaseSnapshotBackupRunStatus{
				BaselineOrchestrator: "ns_1@cb-0000",
				BaselineBalanced:     true,
			},
		}
	}

	groupRun := run(start)
	groupRun.Status.GroupSnapshot = true

	groupSnapshot := func(ready bool, errorMessage string) *groupsnapshotv1beta1.VolumeGroupSnapshot {
		group := &groupsnapshotv1beta1.VolumeGroupSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: "test-run-group"},
			Status:     &groupsnapshotv1beta1.VolumeGroupSnapshotStatus{ReadyToUse: &ready},
		}

		if errorMessage != "" {
			group.Status.Error = &snapshotv1.VolumeSnapshotError{Message: &errorMessage}
		}

		return group
	}

	skewRecorded := run(start)
	skewRecorded.Status.MeasuredSkew = &metav1.Duration{Duration: 2 * time.Second}

	unchanged := func() *snapshotBackupClusterState {
		return &snapshotBackupClusterState{orchestrator: "ns_1@cb-0000", balanced: true, allMembersReady: true}
	}

	changed := func(change func(*snapshotBackupClusterState)) *snapshotBackupClusterState {
		state := unchanged()
		change(state)

		return state
	}

	// withEvent is the unchanged state with one system event logged at the given time.
	withEvent := func(id int, component, description string, at time.Time) *snapshotBackupClusterState {
		return changed(func(s *snapshotBackupClusterState) {
			s.events = []couchbaseutil.SystemEvent{{EventID: id, Component: component, Description: description, Node: "cb-0001", Timestamp: at}}
		})
	}

	testcases := []struct {
		name           string
		run            *couchbasev2.CouchbaseSnapshotBackupRun
		snapshots      []snapshotv1.VolumeSnapshot
		groupSnapshot  *groupsnapshotv1beta1.VolumeGroupSnapshot
		state          *snapshotBackupClusterState
		expectedPhase  couchbasev2.CouchbaseSnapshotBackupRunPhase
		expectedReason string
		// expectedMessage, if set, must appear in the run's message.
		expectedMessage string
		// expectStateRead is whether the cluster's state should have been read.
		expectStateRead bool
	}{
		{
			name:      "a snapshot error is still waiting, since the snapshot controller retries",
			run:       run(start),
			snapshots: []snapshotv1.VolumeSnapshot{snapshot("a", 0, true), failed("b", "disk full")},
		},
		{
			name:            "an error still there past readyTimeout makes the run Invalid",
			run:             run(now.Add(-3 * time.Hour)),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true), failed("b", "disk full")},
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason:  snapshotBackupRunReasonTimedOut,
			expectedMessage: "disk full",
		},
		{
			name:           "individual snapshots with none at all makes the run Invalid",
			run:            run(start),
			expectedPhase:  couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason: snapshotBackupRunReasonNoSnapshots,
		},
		{
			name:          "a group snapshot that isn't ready with none yet is still waiting",
			run:           groupRun,
			groupSnapshot: groupSnapshot(false, ""),
		},
		{
			name:          "a group snapshot error is still waiting, since the snapshot controller retries",
			run:           groupRun,
			groupSnapshot: groupSnapshot(false, "driver error"),
		},
		{
			name:           "a group snapshot that no longer exists makes the run Invalid",
			run:            groupRun,
			expectedPhase:  couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason: snapshotBackupRunReasonSnapshotFailed,
		},
		{
			name:           "a ready group snapshot with no snapshots makes the run Invalid",
			run:            groupRun,
			groupSnapshot:  groupSnapshot(true, ""),
			expectedPhase:  couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason: snapshotBackupRunReasonNoSnapshots,
		},
		{
			name:      "not every snapshot taken yet is still waiting",
			run:       run(start),
			snapshots: []snapshotv1.VolumeSnapshot{snapshot("a", 0, false), snapshot("b", -1, false)},
		},
		{
			name:           "still waiting past readyTimeout makes the run Invalid",
			run:            run(now.Add(-3 * time.Hour)),
			snapshots:      []snapshotv1.VolumeSnapshot{snapshot("a", 0, false), snapshot("b", -1, false)},
			expectedPhase:  couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason: snapshotBackupRunReasonTimedOut,
		},
		{
			name:           "snapshots taken too far apart make the run Invalid",
			run:            run(start),
			snapshots:      []snapshotv1.VolumeSnapshot{snapshot("a", 0, true), snapshot("b", 20*time.Second, true)},
			expectedPhase:  couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason: snapshotBackupRunReasonSkewExceeded,
		},
		{
			name:            "orchestrator changing makes the run Invalid",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			state:           changed(func(s *snapshotBackupClusterState) { s.orchestrator = "ns_1@cb-0001" }),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason:  snapshotBackupRunReasonClusterChanged,
			expectStateRead: true,
		},
		{
			name:            "a service crashing while the snapshots were taken makes the run Invalid",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true), snapshot("b", 2*time.Second, true)},
			state:           withEvent(19, "ns_server", "Service crashed", start.Add(time.Second)),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason:  snapshotBackupRunReasonClusterChanged,
			expectedMessage: "Service crashed on cb-0001",
			expectStateRead: true,
		},
		{
			name:            "a bucket being deleted while the snapshots were taken makes the run Invalid",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true), snapshot("b", 2*time.Second, true)},
			state:           withEvent(8193, "data", "Bucket deleted", start.Add(time.Second)),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason:  snapshotBackupRunReasonClusterChanged,
			expectedMessage: "Bucket deleted on cb-0001",
			expectStateRead: true,
		},
		{
			name:            "an event from before the run started is ignored",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			state:           withEvent(19, "ns_server", "Service crashed", start.Add(-time.Minute)),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete,
			expectStateRead: true,
		},
		{
			name:            "an event after the last snapshot was taken is ignored",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			state:           withEvent(19, "ns_server", "Service crashed", start.Add(10*time.Second)),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete,
			expectStateRead: true,
		},
		{
			name:            "an event not in the list is ignored",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			state:           withEvent(2, "ns_server", "Rebalance initiated", start.Add(time.Second)),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete,
			expectStateRead: true,
		},
		{
			name:            "a member stopping being ready makes the run Invalid",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			state:           changed(func(s *snapshotBackupClusterState) { s.allMembersReady = false }),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason:  snapshotBackupRunReasonClusterChanged,
			expectStateRead: true,
		},
		{
			name:            "the cluster starting to rebalance makes the run Invalid",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			state:           changed(func(s *snapshotBackupClusterState) { s.rebalancing = true }),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason:  snapshotBackupRunReasonClusterChanged,
			expectStateRead: true,
		},
		{
			name:           "a cluster reading long after the snapshots makes the run Invalid",
			run:            run(now.Add(-10 * time.Minute)),
			snapshots:      []snapshotv1.VolumeSnapshot{takenAt("a", now.Add(-5*time.Minute))},
			state:          unchanged(),
			expectedPhase:  couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid,
			expectedReason: snapshotBackupRunReasonClusterStateUnknown,
		},
		{
			name:            "all taken but not ready yet is still waiting",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, false), snapshot("b", 2*time.Second, false)},
			state:           unchanged(),
			expectStateRead: true,
		},
		{
			name:            "all taken, nothing changed and all ready makes the run Complete",
			run:             run(start),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true), snapshot("b", 2*time.Second, true)},
			state:           unchanged(),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete,
			expectStateRead: true,
		},
		{
			name:            "ready past readyTimeout is still Complete",
			run:             run(now.Add(-3 * time.Hour)),
			snapshots:       []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			state:           unchanged(),
			expectedPhase:   couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete,
			expectStateRead: true,
		},
		{
			name:          "with the skew already recorded the cluster isn't read again",
			run:           skewRecorded,
			snapshots:     []snapshotv1.VolumeSnapshot{snapshot("a", 0, true)},
			expectedPhase: couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			stateRead := false

			readState := func() (*snapshotBackupClusterState, error) {
				stateRead = true

				return testcase.state, nil
			}

			check, err := checkSnapshotBackupRun(testcase.run, testcase.snapshots, testcase.groupSnapshot, readyTimeout, maxSkew, readState, now)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if check.phase != testcase.expectedPhase {
				t.Errorf("expected phase %q, got %q (%s)", testcase.expectedPhase, check.phase, check.message)
			}

			if check.reason != testcase.expectedReason {
				t.Errorf("expected reason %q, got %q", testcase.expectedReason, check.reason)
			}

			if !strings.Contains(check.message, testcase.expectedMessage) {
				t.Errorf("expected the message to contain %q, got %q", testcase.expectedMessage, check.message)
			}

			if stateRead != testcase.expectStateRead {
				t.Errorf("expected the cluster state read to be %v, got %v", testcase.expectStateRead, stateRead)
			}
		})
	}
}

// TestValidateSnapshotBackupRunUsesRunLimits checks that a run is judged against the maxSkew
// it was taken with, even once its backup is gone, and falls back to the default when the
// run has none.
func TestValidateSnapshotBackupRunUsesRunLimits(t *testing.T) {
	now := time.Now()

	testcases := []struct {
		name     string
		maxSkew  *metav1.Duration
		expected string
	}{
		{
			name:     "run's own maxSkew is used",
			maxSkew:  &metav1.Duration{Duration: 5 * time.Minute},
			expected: snapshotBackupRunReasonClusterStateUnknown,
		},
		{
			name:     "default maxSkew is used when the run has none",
			expected: snapshotBackupRunReasonSkewExceeded,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			snapshot := func(name string, takenAt time.Time) *unstructured.Unstructured {
				object := &unstructured.Unstructured{}
				object.SetAPIVersion(volumeSnapshotGVR.GroupVersion().String())
				object.SetKind("VolumeSnapshot")
				object.SetNamespace(fakeClusterNamespace)
				object.SetName(name)
				object.SetLabels(map[string]string{constants.LabelSnapshotBackupRun: "test-run"})

				_ = unstructured.SetNestedField(object.Object, takenAt.UTC().Format(time.RFC3339), "status", "creationTime")

				return object
			}

			// Taken 1m apart, and long enough ago that the cluster state isn't read.
			dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
				runtime.NewScheme(),
				map[schema.GroupVersionResource]string{
					volumeSnapshotGVR:      "VolumeSnapshotList",
					volumeGroupSnapshotGVR: "VolumeGroupSnapshotList",
				},
				snapshot("test-run-a", now.Add(-3*time.Minute)),
				snapshot("test-run-b", now.Add(-2*time.Minute)),
			)

			run := &couchbasev2.CouchbaseSnapshotBackupRun{
				ObjectMeta: metav1.ObjectMeta{Name: "test-run", Namespace: fakeClusterNamespace},
				Spec:       couchbasev2.CouchbaseSnapshotBackupRunSpec{SourceBackup: "deleted-backup", MaxSkew: testcase.maxSkew},
				Status:     couchbasev2.CouchbaseSnapshotBackupRunStatus{Phase: couchbasev2.CouchbaseSnapshotBackupRunPhaseInProgress},
			}

			couchbaseClient := couchbasefake.NewSimpleClientset(run)

			c := &Cluster{
				k8s: &client.Client{
					DynamicClient:   dynamicClient,
					CouchbaseClient: couchbaseClient,
					KubeClient:      kubefake.NewSimpleClientset(),
				},
				cluster: &couchbasev2.CouchbaseCluster{ObjectMeta: metav1.ObjectMeta{Name: fakeClusterName, Namespace: fakeClusterNamespace}},
				log:     logf.Log.WithName("test"),
			}

			if err := c.validateSnapshotBackupRun(run.DeepCopy()); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got, err := couchbaseClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(fakeClusterNamespace).Get(t.Context(), "test-run", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get run: %v", err)
			}

			if got.Status.Reason != testcase.expected {
				t.Errorf("expected reason %s, got %s (%s)", testcase.expected, got.Status.Reason, got.Status.Message)
			}
		})
	}
}

// TestSnapshotBackupRunsToDelete checks that runs past the retention period are deleted,
// except the newest Complete run and any run still InProgress.
func TestSnapshotBackupRunsToDelete(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	retention := 30 * 24 * time.Hour

	run := func(name string, phase couchbasev2.CouchbaseSnapshotBackupRunPhase, age time.Duration) *couchbasev2.CouchbaseSnapshotBackupRun {
		return &couchbasev2.CouchbaseSnapshotBackupRun{
			ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(now.Add(-age))},
			Status:     couchbasev2.CouchbaseSnapshotBackupRunStatus{Phase: phase},
		}
	}

	complete := couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete
	invalid := couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid
	inProgress := couchbasev2.CouchbaseSnapshotBackupRunPhaseInProgress
	day := 24 * time.Hour

	testcases := []struct {
		name     string
		runs     []*couchbasev2.CouchbaseSnapshotBackupRun
		expected []string
	}{
		{
			name:     "an old Complete run is deleted when there's a newer one",
			runs:     []*couchbasev2.CouchbaseSnapshotBackupRun{run("old", complete, 40*day), run("new", complete, day)},
			expected: []string{"old"},
		},
		{
			name: "the newest Complete run is kept even when it's old",
			runs: []*couchbasev2.CouchbaseSnapshotBackupRun{run("oldest", complete, 50*day), run("newest", complete, 40*day)},
			// Only the older of the two goes, the newest Complete one stays.
			expected: []string{"oldest"},
		},
		{
			name: "an old InProgress run is kept",
			runs: []*couchbasev2.CouchbaseSnapshotBackupRun{run("running", inProgress, 40*day)},
		},
		{
			name:     "an old Invalid run is deleted",
			runs:     []*couchbasev2.CouchbaseSnapshotBackupRun{run("failed", invalid, 40*day), run("good", complete, day)},
			expected: []string{"failed"},
		},
		{
			name: "recent runs are kept",
			runs: []*couchbasev2.CouchbaseSnapshotBackupRun{run("recent-complete", complete, day), run("recent-invalid", invalid, day)},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			var names []string

			for _, run := range snapshotBackupRunsToDelete(testcase.runs, retention, now) {
				names = append(names, run.Name)
			}

			sort.Strings(names)

			if !reflect.DeepEqual(names, testcase.expected) {
				t.Errorf("expected %v to be deleted, got %v", testcase.expected, names)
			}
		})
	}
}
