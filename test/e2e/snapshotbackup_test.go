/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	utilconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var volumeSnapshotGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}

var volumeGroupSnapshotGVR = schema.GroupVersionResource{Group: "groupsnapshot.storage.k8s.io", Version: "v1beta1", Resource: "volumegroupsnapshots"}

// snapshotBackupSpec returns a snapshot backup spec with every field set, since unset
// durations are sent as 0s and the CRD defaults don't apply. maxSkew is wide because the
// test driver can take over a minute between snapshots.
func snapshotBackupSpec(schedule string) *couchbasev2.CouchbaseSnapshotBackupSpec {
	maxRetries := int32(3)

	return &couchbasev2.CouchbaseSnapshotBackupSpec{
		Schedule:                schedule,
		VolumeSnapshotClassName: framework.Global.VolumeSnapshotClassName,
		MaxSkew:                 metav1.Duration{Duration: 5 * time.Minute},
		MaxRetries:              &maxRetries,
		RetryInterval:           metav1.Duration{Duration: time.Minute},
		ReadyTimeout:            metav1.Duration{Duration: 10 * time.Minute},
	}
}

// TestSnapshotBackupOnDemand checks that a snapshot backup with no schedule takes one run,
// which completes with a snapshot of every member's volumes, and no more runs after it.
func TestSnapshotBackupOnDemand(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	clusterSize := constants.Size3
	clusterOptions().WithPersistentTopology(clusterSize).MustCreate(t, kubernetes)

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, snapshotBackupSpec(""), 0)

	run := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)[0]

	members := map[string]bool{}
	for _, snapshot := range run.Status.Snapshots {
		members[snapshot.Member] = true
	}

	if len(members) != clusterSize {
		e2eutil.Die(t, fmt.Errorf("expected snapshots of %d members, got %v", clusterSize, run.Status.Snapshots))
	}

	if run.Status.MeasuredSkew == nil {
		e2eutil.Die(t, fmt.Errorf("expected skew to be recorded on run %s", run.Name))
	}

	// Give the operator time to take another run if it was going to.
	time.Sleep(2 * time.Minute)

	mustHaveSnapshotBackupRunCount(t, kubernetes, backup, 1)
}

// TestSnapshotBackupScheduled checks that a scheduled snapshot backup waits for its
// scheduled time before taking a run.
func TestSnapshotBackupScheduled(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	// Runs once an hour, a few minutes from now.
	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, snapshotBackupSpec(e2eutil.ScheduleIn(4*time.Minute)), 0)

	time.Sleep(2 * time.Minute)
	mustHaveSnapshotBackupRunCount(t, kubernetes, backup, 0)

	e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)
	mustHaveSnapshotBackupRunCount(t, kubernetes, backup, 1)
}

// TestSnapshotBackupInvalidRetries checks that a run that can't be taken ends Invalid, is
// retried maxRetries times, and then isn't tried again.
func TestSnapshotBackupInvalidRetries(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	maxRetries := int32(1)

	spec := snapshotBackupSpec("")
	spec.VolumeSnapshotClassName = "missing-snapshot-class"
	spec.MaxRetries = &maxRetries
	spec.ReadyTimeout = metav1.Duration{Duration: time.Minute}

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, spec, 0)

	runs := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid, 2, 10*time.Minute)

	for _, run := range runs {
		if run.Status.Reason != "TimedOut" || !strings.Contains(run.Status.Message, "not found") {
			e2eutil.Die(t, fmt.Errorf("expected run %s to time out on the missing class, got %s: %s", run.Name, run.Status.Reason, run.Status.Message))
		}
	}

	// No more attempts once the retries are used up.
	time.Sleep(3 * time.Minute)

	mustHaveSnapshotBackupRunCount(t, kubernetes, backup, 2)
}

// TestSnapshotBackupRetention checks that runs older than backupRetention are deleted
// along with their snapshots, and the latest Complete run is kept.
func TestSnapshotBackupRetention(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, snapshotBackupSpec("* * * * *"), 3*time.Minute)

	first := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)[0]

	mustWaitForSnapshotBackupRunDeleted(t, kubernetes, &first, 10*time.Minute)

	e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, time.Minute)
}

// TestSnapshotBackupSwitchFromArchive checks that switching an archive backup to snapshot
// removes its CronJob and keeps its PVC, and switching back brings the CronJob back using
// the same PVC.
func TestSnapshotBackupSwitchFromArchive(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster := clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	backup := e2eutil.NewFullBackup(e2eutil.DefaultSchedule()).MustCreate(t, kubernetes)
	e2eutil.MustWaitForBackup(t, kubernetes, backup, time.Minute)

	if err := e2eutil.WaitForCronjob(kubernetes, cluster, backup.Name+"-full", 5*time.Minute); err != nil {
		e2eutil.Die(t, err)
	}

	pvc, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).Get(t.Context(), backup.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	mustSetSnapshotBackup(t, kubernetes, backup.Name, snapshotBackupSpec("*/30 * * * *"))
	mustWaitForBackupCronJobs(t, kubernetes, backup.Name, 0)

	mustSetSnapshotBackup(t, kubernetes, backup.Name, nil)
	mustWaitForBackupCronJobs(t, kubernetes, backup.Name, 1)

	after, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).Get(t.Context(), backup.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if after.UID != pvc.UID {
		e2eutil.Die(t, fmt.Errorf("expected backup PVC %s to be kept, it was recreated", backup.Name))
	}
}

// TestSnapshotBackupGroup checks a backup with a group snapshot class takes every volume in one
// group snapshot, so they're all from the same moment.
func TestSnapshotBackupGroup(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots().VolumeGroupSnapshots()

	cluster := clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, groupSnapshotBackupSpec(), 0)
	run := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)[0]

	mustBeGroupSnapshotRun(t, &run)
	mustHaveSnapshotOfEveryVolume(t, kubernetes, cluster, &run)
}

// TestSnapshotBackupRetentionAfterBackupDeleted checks runs are still deleted once they're older
// than their backup's retention, even after the backup itself is gone. The latest Complete run is
// kept.
func TestSnapshotBackupRetentionAfterBackupDeleted(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, snapshotBackupSpec("* * * * *"), 3*time.Minute)
	runs := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 2, 15*time.Minute)

	e2eutil.MustDeleteBackup(t, kubernetes, backup)

	mustWaitForSnapshotBackupRunDeleted(t, kubernetes, &runs[0], 10*time.Minute)
	mustHaveSnapshotBackupRun(t, kubernetes, runs[len(runs)-1].Name)
}

// TestSnapshotBackupRunDeleted checks a run records what a restore needs, and that deleting the
// run also deletes its snapshots and its copy of the admin login.
func TestSnapshotBackupRunDeleted(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, snapshotBackupSpec(""), 24*time.Hour)
	run := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)[0]

	mustRecordRestoreDetails(t, kubernetes, cluster, bucket, &run, 24*time.Hour)

	mustDeleteSnapshotBackupRun(t, kubernetes, &run)
	mustWaitForSecretDeleted(t, kubernetes, run.AdminSecretCopyName(), 2*time.Minute)
}

// TestSnapshotBackupGroupRunDeleted checks deleting a group snapshot run deletes its group snapshot,
// and the snapshots the group snapshot made.
func TestSnapshotBackupGroupRunDeleted(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots().VolumeGroupSnapshots()

	clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, groupSnapshotBackupSpec(), 0)
	run := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)[0]

	mustBeGroupSnapshotRun(t, &run)

	if run.Status.GroupSnapshotName == nil {
		e2eutil.Die(t, fmt.Errorf("expected run %s to record its group snapshot", run.Name))
	}

	mustDeleteSnapshotBackupRun(t, kubernetes, &run)
	mustWaitForGroupSnapshotDeleted(t, kubernetes, *run.Status.GroupSnapshotName, 2*time.Minute)
}

// TestSnapshotBackupOperatorRestart checks a run isn't left InProgress when the operator restarts
// while it's being taken, and the backup still ends up with a Complete run.
func TestSnapshotBackupOperatorRestart(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, snapshotBackupSpec(""), 0)
	first := mustWaitForSnapshotBackupRunStarted(t, kubernetes, backup, 5*time.Minute)

	e2eutil.MustKillOperatorAndWaitForRecovery(t, kubernetes)

	mustWaitForSnapshotBackupRunFinished(t, kubernetes, first.Name, 15*time.Minute)
	e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)
}

// groupSnapshotBackupSpec is snapshotBackupSpec with the group snapshot class set too.
func groupSnapshotBackupSpec() *couchbasev2.CouchbaseSnapshotBackupSpec {
	spec := snapshotBackupSpec("")
	spec.VolumeGroupSnapshotClassName = &framework.Global.VolumeGroupSnapshotClassName

	return spec
}

// mustBeGroupSnapshotRun checks the run was taken as one group snapshot, so there's no gap between
// its volumes.
func mustBeGroupSnapshotRun(t *testing.T, run *couchbasev2.CouchbaseSnapshotBackupRun) {
	if !run.Status.GroupSnapshot {
		e2eutil.Die(t, fmt.Errorf("expected run %s to be taken as a group snapshot", run.Name))
	}

	if run.Status.MeasuredSkew == nil || run.Status.MeasuredSkew.Duration != 0 {
		e2eutil.Die(t, fmt.Errorf("expected no gap between the snapshots of run %s, got %v", run.Name, run.Status.MeasuredSkew))
	}
}

// mustHaveSnapshotOfEveryVolume checks the run has one snapshot for each of the cluster's volumes.
func mustHaveSnapshotOfEveryVolume(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, run *couchbasev2.CouchbaseSnapshotBackupRun) {
	selector := utilconstants.LabelCluster + "=" + cluster.Name + "," + utilconstants.LabelNode

	volumes, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if len(run.Status.Snapshots) != len(volumes.Items) {
		e2eutil.Die(t, fmt.Errorf("expected run %s to have a snapshot of each of the %d volumes, got %d", run.Name, len(volumes.Items), len(run.Status.Snapshots)))
	}
}

// mustWaitForSnapshotBackupRunDeleted waits until the run is deleted, and its snapshots with it,
// since they're owned by it.
func mustWaitForSnapshotBackupRunDeleted(t *testing.T, kubernetes *types.Cluster, run *couchbasev2.CouchbaseSnapshotBackupRun, timeout time.Duration) {
	err := retryutil.RetryFor(timeout, func() error {
		_, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(kubernetes.Namespace).Get(t.Context(), run.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("run %s not deleted yet: %w", run.Name, err)
	})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, snapshot := range run.Status.Snapshots {
		err := retryutil.RetryFor(2*time.Minute, func() error {
			_, err := kubernetes.DynamicClient.Resource(volumeSnapshotGVR).Namespace(kubernetes.Namespace).Get(t.Context(), snapshot.SnapshotName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}

			return fmt.Errorf("snapshot %s not deleted yet: %w", snapshot.SnapshotName, err)
		})
		if err != nil {
			e2eutil.Die(t, err)
		}
	}
}

// mustHaveSnapshotBackupRun fails the test if the run doesn't exist.
func mustHaveSnapshotBackupRun(t *testing.T, kubernetes *types.Cluster, name string) {
	if _, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(kubernetes.Namespace).Get(t.Context(), name, metav1.GetOptions{}); err != nil {
		e2eutil.Die(t, fmt.Errorf("expected run %s to be kept: %w", name, err))
	}
}

// mustHaveSnapshotBackupRunCount fails the test unless the backup has exactly count runs.
func mustHaveSnapshotBackupRunCount(t *testing.T, kubernetes *types.Cluster, backup *couchbasev2.CouchbaseBackup, count int) {
	runs, err := e2eutil.ListSnapshotBackupRuns(kubernetes, backup)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if len(runs) != count {
		e2eutil.Die(t, fmt.Errorf("expected backup %s to have %d runs, got %d", backup.Name, count, len(runs)))
	}
}

// mustSetSnapshotBackup sets or removes a backup's snapshotBackup.
func mustSetSnapshotBackup(t *testing.T, kubernetes *types.Cluster, name string, snapshotBackup *couchbasev2.CouchbaseSnapshotBackupSpec) {
	err := retryutil.RetryFor(time.Minute, func() error {
		backup, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackups(kubernetes.Namespace).Get(t.Context(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		backup.Spec.SnapshotBackup = snapshotBackup

		_, err = kubernetes.CRClient.CouchbaseV2().CouchbaseBackups(kubernetes.Namespace).Update(t.Context(), backup, metav1.UpdateOptions{})

		return err
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustWaitForBackupCronJobs waits until the backup has count CronJobs.
func mustWaitForBackupCronJobs(t *testing.T, kubernetes *types.Cluster, name string, count int) {
	err := retryutil.RetryFor(5*time.Minute, func() error {
		cronjobs, err := kubernetes.KubeClient.BatchV1().CronJobs(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		found := 0

		for _, cronjob := range cronjobs.Items {
			if strings.HasPrefix(cronjob.Name, name+"-") {
				found++
			}
		}

		if found != count {
			return fmt.Errorf("backup %s has %d cronjobs, waiting for %d", name, found, count)
		}

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// snapshotRestoreDocs is how many documents a restore test writes before and after its backup.
const snapshotRestoreDocs = 500

// TestSnapshotRestoreRollback checks a running cluster can be rolled back to a snapshot backup. It
// writes some documents, backs up, writes more, then restores, and only the backed up documents
// should be left. Each restored pod runs the node remap once before Couchbase starts.
func TestSnapshotRestoreRollback(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	e2eutil.NewDocumentSet(bucket, snapshotRestoreDocs).WithPrefix("after").MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, 2*snapshotRestoreDocs, time.Minute)

	cluster = mustHibernateCluster(t, kubernetes, cluster)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, cluster, true, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustHaveRemappedMembers(t, kubernetes, cluster, constants.Size3)
}

// TestSnapshotRestoreNewCluster checks a snapshot backup can be restored into a new cluster with the
// same name, created hibernated. Without startClusterWhenReady, the restore waits for the user to
// wake the cluster, and only then finishes.
func TestSnapshotRestoreNewCluster(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	cluster = mustRecreateClusterHibernated(t, kubernetes, cluster)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, cluster, false, false)
	restore := mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart, 20*time.Minute)

	if !meta.IsStatusConditionTrue(restore.Status.Conditions, "WaitingToStart") {
		e2eutil.Die(t, fmt.Errorf("expected the restore to say it's waiting for the cluster to be woken, got %v", restore.Status.Conditions))
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/hibernate", false), time.Minute)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustHaveRemappedMembers(t, kubernetes, cluster, constants.Size3)
}

// TestSnapshotRestorePasswordChanged checks a restore works after the admin password was changed
// since the backup. The restored data only accepts the old password, so the operator has to log in
// with it when the cluster wakes, then change it to the one in the admin secret.
func TestSnapshotRestorePasswordChanged(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	e2eutil.MustRotateClusterPassword(t, kubernetes)
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.EventReasonAdminPasswordChangedEvent(cluster), 5*time.Minute)

	cluster = mustHibernateCluster(t, kubernetes, cluster)
	restoreStart := time.Now()

	mustCreateSnapshotRestore(t, kubernetes, backup, run, cluster, true, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)

	// The operator changes the restored cluster's password to the one in the admin secret.
	mustHaveClusterEventSince(t, kubernetes, cluster, k8sutil.EventReasonAdminPasswordChanged, restoreStart)
}

// TestSnapshotRestoreServerGroups checks a restore puts each member back in the zone it was backed
// up in. A volume made from a snapshot could otherwise land in any zone.
func TestSnapshotRestoreServerGroups(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots().ServerGroups(3)

	spec := clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes)
	spec.Spec.ServerGroups = getAvailabilityZones(t, kubernetes)[:3]

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, spec)

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	zones := map[string]string{}

	for _, snapshot := range run.Status.Snapshots {
		if snapshot.ServerGroup == "" || snapshot.AvailabilityZone == "" {
			e2eutil.Die(t, fmt.Errorf("expected run %s to record member %s's server group and zone, got %+v", run.Name, snapshot.Member, snapshot))
		}

		zones[snapshot.Member] = snapshot.AvailabilityZone
	}

	cluster = mustRecreateClusterHibernated(t, kubernetes, cluster)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, cluster, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)

	for member, zone := range zones {
		pod, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).Get(t.Context(), member, metav1.GetOptions{})
		if err != nil {
			e2eutil.Die(t, err)
		}

		node, err := kubernetes.KubeClient.CoreV1().Nodes().Get(t.Context(), pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			e2eutil.Die(t, err)
		}

		if got := node.Labels[utilconstants.ServerGroupLabel]; got != zone {
			e2eutil.Die(t, fmt.Errorf("expected member %s back in zone %s, it's on node %s in zone %s", member, zone, node.Name, got))
		}
	}
}

// TestSnapshotRestoreNewName checks a backup can be restored into a cluster with a different name
// while the backed up cluster keeps running. The restored nodes get the new cluster's names and
// their own cluster UUID, and the backed up cluster isn't touched.
func TestSnapshotRestoreNewName(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	mustHaveRecordedHostnames(t, run, constants.Size3)

	// Written after the backup, so only the backed up cluster should have them.
	e2eutil.NewDocumentSet(bucket, snapshotRestoreDocs).WithPrefix("after").MustCreate(t, kubernetes, cluster)

	target := mustCreateHibernatedCluster(t, kubernetes, hibernatedCopy(cluster, cluster.Name+"-copy"))

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustHaveRemappedMembers(t, kubernetes, target, constants.Size3)
	mustHaveOwnNodeNames(t, e2eutil.MustCreateAdminConsoleClient(t, kubernetes, target), target)
	mustHaveOwnClusterUUID(t, kubernetes, cluster, target)

	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, 2*snapshotRestoreDocs, time.Minute)
}

// TestSnapshotRestoreGroup checks a group snapshot backup can be restored into a new cluster with a
// different name, and back into the cluster it came from.
func TestSnapshotRestoreGroup(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots().VolumeGroupSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, groupSnapshotBackupSpec(), 0)
	run := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)[0]

	mustBeGroupSnapshotRun(t, &run)

	// Into a new cluster first, while the backed up one keeps running.
	target := mustCreateHibernatedCluster(t, kubernetes, hibernatedCopy(cluster, cluster.Name+"-copy"))

	mustCreateSnapshotRestore(t, kubernetes, backup, &run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustHaveOwnNodeNames(t, e2eutil.MustCreateAdminConsoleClient(t, kubernetes, target), target)

	// Then back into the cluster it came from, after more documents were written.
	mustDeleteSnapshotRestore(t, kubernetes)

	e2eutil.NewDocumentSet(bucket, snapshotRestoreDocs).WithPrefix("after").MustCreate(t, kubernetes, cluster)

	cluster = mustHibernateCluster(t, kubernetes, cluster)

	mustCreateSnapshotRestore(t, kubernetes, backup, &run, cluster, true, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)
}

// TestSnapshotRestoreTLS checks a restore into a cluster with a different name works with TLS and
// node to node encryption. The restored volumes still hold the backed up cluster's certificate, so
// each node has to get the new cluster's certificate before Couchbase starts, or the operator can't
// talk to it.
func TestSnapshotRestoreTLS(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	encryption := couchbasev2.NodeToNodeAll

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	spec := clusterOptions().WithPersistentTopology(constants.Size3).WithTLS(tls).Generate(kubernetes)
	spec.Spec.Networking.TLS.NodeToNodeEncryption = &encryption

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, spec)

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	mustHaveTrustedCA(t, run, tls.CA)

	// Signed by the same CA, but only good for the new cluster's name.
	targetTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{ClusterName: cluster.Name + "-copy", CA: tls.CA})

	targetSpec := clusterOptions().WithPersistentTopology(constants.Size3).WithTLS(targetTLS).Generate(kubernetes)
	targetSpec.Spec.Networking.TLS.NodeToNodeEncryption = &encryption
	targetSpec.Spec.Hibernate = true

	target := mustCreateHibernatedCluster(t, kubernetes, targetSpec)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustCheckN2NEnabled(t, kubernetes, target, encryption, targetTLS, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustNotHaveSavedStateKey(t, kubernetes, target, persistence.ReloadTLS)
}

// TestSnapshotRestoreDifferentCA checks a TLS backup can be restored into a cluster whose
// certificate comes from another CA. The operator loads the new CA once the nodes are up.
func TestSnapshotRestoreDifferentCA(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).WithTLS(tls).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	// No CA given, so this gets a new one.
	targetTLS := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{ClusterName: cluster.Name + "-copy"})

	targetSpec := clusterOptions().WithPersistentTopology(constants.Size3).WithTLS(targetTLS).Generate(kubernetes)
	targetSpec.Spec.Hibernate = true

	target := mustCreateHibernatedCluster(t, kubernetes, targetSpec)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
}

// TestSnapshotRestoreBinderPods checks a restore works on storage that only makes a
// volume once a pod uses it. The restore starts binder pods to get the volumes made,
// and removes them after.
func TestSnapshotRestoreBinderPods(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	storage := mustCreateWaitingStorageClass(t, kubernetes)
	defer func() {
		_ = kubernetes.KubeClient.StorageV1().StorageClasses().Delete(t.Context(), storage, metav1.DeleteOptions{})
	}()

	spec := clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes)
	for i := range spec.Spec.VolumeClaimTemplates {
		spec.Spec.VolumeClaimTemplates[i].Spec.StorageClassName = &storage
	}

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, spec)

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	target := mustCreateHibernatedCluster(t, kubernetes, hibernatedCopy(cluster, cluster.Name+"-copy"))

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustSeeBinderPods(t, kubernetes)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	mustHaveNoBinderPods(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
}

// TestSnapshotRestoreScopesCollections checks a restore brings back a scope and
// collection with their documents.
func TestSnapshotRestoreScopesCollections(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	collection := e2eutil.NewCollection("hotels").MustCreate(t, kubernetes)
	scope := e2eutil.NewScope("inventory").WithCollections(collection).MustCreate(t, kubernetes)

	bucket := e2eutil.MustGetBucket(framework.Global.BucketType, framework.Global.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithPersistentTopology(constants.Size3).MustCreate(t, kubernetes)

	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope("inventory").WithCollections("hotels")
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, 2*time.Minute)

	e2eutil.NewDocumentSet(bucket.GetName(), snapshotRestoreDocs).IntoScopeAndCollection("inventory", "hotels").MustCreate(t, kubernetes, cluster)

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	target := mustCreateHibernatedCluster(t, kubernetes, hibernatedCopy(cluster, cluster.Name+"-copy"))

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, target, bucket, expected, 2*time.Minute)
	e2eutil.MustVerifyDocCountInCollectionSum(t, kubernetes, target, bucket.GetName(), "inventory", "hotels", snapshotRestoreDocs, 5*time.Minute)
}

// TestSnapshotBackupVolumelessClass checks a snapshot backup is refused for a cluster with a
// server class that keeps no data on a volume, since a restore couldn't bring those nodes back.
func TestSnapshotBackupVolumelessClass(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	clusterOptions().WithMixedTopologyNoEventing(constants.Size2).MustCreate(t, kubernetes)

	maxRetries := int32(0)
	spec := snapshotBackupSpec("")
	spec.MaxRetries = &maxRetries

	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, spec, 0)
	run := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid, 1, 10*time.Minute)[0]

	if !strings.Contains(run.Status.Message, "has no default volume") {
		e2eutil.Die(t, fmt.Errorf("expected run %s to be refused for the class without a volume, got %s: %s", run.Name, run.Status.Reason, run.Status.Message))
	}
}

// TestSnapshotBackupRejected checks snapshot backups that can't work are refused when created.
func TestSnapshotBackupRejected(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	backup := func(name string, change func(*couchbasev2.CouchbaseBackupSpec)) *couchbasev2.CouchbaseBackup {
		b := &couchbasev2.CouchbaseBackup{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       couchbasev2.CouchbaseBackupSpec{SnapshotBackup: snapshotBackupSpec("")},
		}
		change(&b.Spec)

		return b
	}

	cases := []struct {
		name    string
		backup  *couchbasev2.CouchbaseBackup
		message string
	}{
		{"bad schedule", backup("bad-schedule", func(s *couchbasev2.CouchbaseBackupSpec) { s.SnapshotBackup.Schedule = "every day" }), "spec.snapshotBackup.schedule"},
		{"no snapshot class", backup("no-class", func(s *couchbasev2.CouchbaseBackupSpec) { s.SnapshotBackup.VolumeSnapshotClassName = "" }), "volumeSnapshotClassName"},
		{"no ready timeout", backup("no-timeout", func(s *couchbasev2.CouchbaseBackupSpec) { s.SnapshotBackup.ReadyTimeout = metav1.Duration{} }), "readyTimeout"},
		{"name too long", backup(strings.Repeat("a", 60), func(*couchbasev2.CouchbaseBackupSpec) {}), "cannot be longer than"},
		{"with worm", backup("with-worm", func(s *couchbasev2.CouchbaseBackupSpec) {
			s.Worm = &couchbasev2.CouchbaseBackupWorm{CompliancePeriod: metav1.Duration{Duration: 24 * time.Hour}}
		}), "spec.worm cannot be used with spec.snapshotBackup"},
	}

	for _, c := range cases {
		_, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackups(kubernetes.Namespace).Create(t.Context(), c.backup, metav1.CreateOptions{})
		if err == nil {
			e2eutil.Die(t, fmt.Errorf("%s: expected backup %s to be refused", c.name, c.backup.Name))
		}

		if !strings.Contains(err.Error(), c.message) {
			e2eutil.Die(t, fmt.Errorf("%s: expected backup %s to be refused with %q, got %w", c.name, c.backup.Name, c.message, err))
		}
	}
}

// TestSnapshotRestoreRejected checks restores that can't work are refused when they're created, so
// nothing on the target is touched. The targets are created hibernated with no data, and nothing
// waits for them to start, since a refused restore never gets that far.
func TestSnapshotRestoreRejected(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, _ := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	// A target that's fine, for the cases where only the restore itself is wrong.
	good := e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, hibernatedCopy(cluster, cluster.Name+"-good"))

	older := hibernatedCopy(cluster, cluster.Name+"-older")
	older.Spec.Image = "couchbase/server:7.6.2"

	disks := hibernatedCopy(cluster, cluster.Name+"-disks")
	disks.Spec.Servers[0].VolumeMounts.DataClaim = disks.Spec.Servers[0].VolumeMounts.DefaultClaim

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{ClusterName: cluster.Name + "-tls"})
	withTLS := clusterOptions().WithPersistentTopology(constants.Size3).WithTLS(tls).Generate(kubernetes)
	withTLS.Spec.Hibernate = true

	index := hibernatedCopy(cluster, cluster.Name+"-index")
	index.Spec.ClusterSettings.Indexer = &couchbasev2.CouchbaseClusterIndexerSettings{StorageMode: otherIndexStorageMode(cluster)}

	noBucket := hibernatedCopy(cluster, cluster.Name+"-no-bucket")
	noBucket.Spec.Buckets.Selector = &couchbasev2.ObjectSelector{MatchLabels: map[string]string{"bucket": "none"}}

	// A run that failed, since it uses a snapshot class that doesn't exist.
	failed := snapshotBackupSpec("")
	failed.VolumeSnapshotClassName = "missing-class"
	failed.ReadyTimeout = metav1.Duration{Duration: time.Minute}
	failedBackup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, failed, 0)
	failedRun := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, failedBackup, couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid, 1, 10*time.Minute)[0]

	// A target whose admin login has another username.
	otherUser := hibernatedCopy(cluster, cluster.Name+"-other-user")
	otherUser.Spec.Security.AdminSecret = mustCreateAdminSecret(t, kubernetes, "other-login", "someone-else")

	// Paused, so it doesn't try to migrate anything.
	moving := hibernatedCopy(cluster, cluster.Name+"-moving")
	moving.Spec.Hibernate = false
	moving.Spec.Paused = true
	moving.Spec.Migration = &couchbasev2.ClusterAssimilationSpec{UnmanagedClusterHost: "other-cluster.example.com"}

	for _, spec := range []*couchbasev2.CouchbaseCluster{older, disks, withTLS, index, noBucket, moving, otherUser} {
		e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, spec)
	}

	yes := true

	cases := []struct {
		name    string
		restore *couchbasev2.CouchbaseSnapshotBackupRestore
		message string
	}{
		{"run doesn't exist", snapshotRestore("missing-run", backup.Name, "missing", good.Name, nil), "doesn't exist in namespace"},
		{"run from another backup", snapshotRestore("other-backup", "other", run.Name, good.Name, nil), "was taken by backup"},
		{"target doesn't exist", snapshotRestore("missing-target", backup.Name, run.Name, "missing", nil), "create the cluster first"},
		{"target has data", snapshotRestore("has-data", backup.Name, run.Name, cluster.Name, nil), "acknowledgeDataLoss must be true"},
		{"target is too old", snapshotRestore("too-old", backup.Name, run.Name, older.Name, nil), "a snapshot restore needs"},
		{"target has other disks", snapshotRestore("other-disks", backup.Name, run.Name, disks.Name, nil), "but server class"},
		{"target uses TLS", snapshotRestore("uses-tls", backup.Name, run.Name, withTLS.Name, nil), "uses TLS but the backed up cluster didn't"},
		{"target has another index storage mode", snapshotRestore("index-mode", backup.Name, run.Name, index.Name, nil), "the target must use the same one"},
		{"target doesn't pick up the bucket", snapshotRestore("no-bucket", backup.Name, run.Name, noBucket.Name, nil), "add CRs for them"},
		{"target is still migrating", snapshotRestore("still-migrating", backup.Name, run.Name, moving.Name, nil), "is still migrating"},
		{"run failed", snapshotRestore("failed-run", failedBackup.Name, failedRun.Name, good.Name, nil), "only a Complete run can be restored"},
		{"target has another admin user", snapshotRestore("other-user", backup.Name, run.Name, otherUser.Name, nil), "the backup was taken with admin user"},
	}

	for _, c := range cases {
		mustRejectSnapshotRestore(t, kubernetes, c.restore, c.message)
	}

	// With acknowledgeBucketGap set, the restore into the target without the bucket is allowed.
	allowed := snapshotRestore("bucket-gap-ok", backup.Name, run.Name, noBucket.Name, nil)
	allowed.Spec.Target.AcknowledgeBucketGap = &yes
	mustAcceptSnapshotRestore(t, kubernetes, allowed)

	// A restore into the running cluster is let in, but waits there for it to be hibernated, so
	// nothing is deleted. A second one into the same cluster is refused while it waits.
	waiting := snapshotRestore("waiting", backup.Name, run.Name, cluster.Name, &yes)
	if _, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace).Create(t.Context(), waiting, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	mustRejectSnapshotRestore(t, kubernetes, snapshotRestore("second", backup.Name, run.Name, cluster.Name, &yes), "is still restoring into cluster")

	// A restore's spec can't be changed once it's created.
	mustRejectSnapshotRestoreChange(t, kubernetes, waiting.Name, "spec can't be changed")
}

// TestSnapshotRestoreNewerVersion checks a backup can be restored into a cluster on a newer server
// version. The restored nodes start on the new version straight from the backed up data.
func TestSnapshotRestoreNewerVersion(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots().Upgradable()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptionsUpgrade().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	spec := hibernatedCopy(cluster, cluster.Name+"-copy")
	spec.Spec.Image = framework.Global.CouchbaseServerImage

	target := mustCreateHibernatedCluster(t, kubernetes, spec)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustRunVersion(t, kubernetes, target, framework.Global.CouchbaseServerImage)
}

// TestSnapshotRestoreMoreNodes checks a backup of three nodes can be restored into a cluster that
// asks for two. All three are restored, so no data is missing, and then the cluster scales down.
func TestSnapshotRestoreMoreNodes(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	spec := hibernatedCopy(cluster, cluster.Name+"-copy")
	spec.Spec.Servers[0].Size = constants.Size2

	target := mustCreateHibernatedCluster(t, kubernetes, spec)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitForClusterPods(t, kubernetes, target, constants.Size2, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
}

// TestSnapshotRestoreFewerNodes checks a backup of two nodes can be restored into a cluster that
// asks for three. Both are restored, and the third is added as a new node.
func TestSnapshotRestoreFewerNodes(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size2).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	spec := hibernatedCopy(cluster, cluster.Name)
	spec.Spec.Servers[0].Size = constants.Size3

	e2eutil.MustDeleteCluster(t, kubernetes, cluster)
	e2eutil.MustWaitForResourceDeletion(t, kubernetes, cluster, 5*time.Minute)

	target := mustCreateHibernatedCluster(t, kubernetes, spec)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitForClusterPods(t, kubernetes, target, constants.Size3, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
}

// TestSnapshotRestoreIndexes checks a GSI index comes back with the restored data and can be used
// straight away, without being built again.
func TestSnapshotRestoreIndexes(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	spec := clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes)
	spec.Spec.Servers[0].Services = append(spec.Spec.Servers[0].Services, couchbasev2.QueryService)

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, spec)

	e2eutil.MustExecuteIndexQuery(t, kubernetes, cluster, fmt.Sprintf("CREATE PRIMARY INDEX ON `%s` USING GSI", bucket), time.Minute)
	mustCountDocsWithIndex(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	e2eutil.NewDocumentSet(bucket, snapshotRestoreDocs).WithPrefix("after").MustCreate(t, kubernetes, cluster)

	cluster = mustHibernateCluster(t, kubernetes, cluster)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, cluster, true, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	if e2eutil.MustGetIndexCount(t, kubernetes, cluster, 2*time.Minute) == 0 {
		e2eutil.Die(t, fmt.Errorf("expected the primary index on bucket %s to come back", bucket))
	}

	mustCountDocsWithIndex(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)
}

// TestSnapshotRestoreOperatorRestart checks a restore carries on when the operator restarts, once
// while it waits for the cluster to be woken, and again while the nodes are being remapped.
func TestSnapshotRestoreOperatorRestart(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	cluster = mustRecreateClusterHibernated(t, kubernetes, cluster)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, cluster, false, false)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart, 20*time.Minute)

	e2eutil.MustKillOperatorAndWaitForRecovery(t, kubernetes)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/hibernate", false), time.Minute)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping, 10*time.Minute)

	e2eutil.MustKillOperatorAndWaitForRecovery(t, kubernetes)

	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustHaveRemappedMembers(t, kubernetes, cluster, constants.Size3)
}

// TestSnapshotRestoreRunDeleted checks a restore fails with a clear reason when its run is deleted
// before it starts, and leaves the cluster as it was, so it wakes up with all its data.
func TestSnapshotRestoreRunDeleted(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket, backup, run := mustCreateWaitingSnapshotRestore(t, kubernetes)

	mustDeleteSnapshotBackupRun(t, kubernetes, run)

	mustFailSnapshotRestoreAndKeepCluster(t, kubernetes, cluster, bucket, backup, "spec.runName "+run.Name+" doesn't exist")
}

// TestSnapshotRestoreSnapshotMissing checks a restore fails with a clear reason when one of the
// run's snapshots is gone, and leaves the cluster as it was, so it wakes up with all its data.
func TestSnapshotRestoreSnapshotMissing(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket, backup, run := mustCreateWaitingSnapshotRestore(t, kubernetes)

	missing := run.Status.Snapshots[0].SnapshotName
	if err := kubernetes.DynamicClient.Resource(volumeSnapshotGVR).Namespace(kubernetes.Namespace).Delete(t.Context(), missing, metav1.DeleteOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	mustFailSnapshotRestoreAndKeepCluster(t, kubernetes, cluster, bucket, backup, "snapshot "+missing+" for member")
}

// TestSnapshotRestoreBackupAgain checks a restored cluster works like any other. It's restored
// under a new name, grows by a node, is backed up, and is then rolled back to that backup.
func TestSnapshotRestoreBackupAgain(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().VolumeSnapshots()

	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	target := mustCreateHibernatedCluster(t, kubernetes, hibernatedCopy(cluster, cluster.Name+"-copy"))

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, false, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)

	// The backup has no selector, so with the backed up cluster gone, its next run is of the copy.
	e2eutil.MustDeleteCluster(t, kubernetes, cluster)
	e2eutil.MustWaitForResourceDeletion(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustDeleteBackup(t, kubernetes, backup)
	mustDeleteSnapshotRestore(t, kubernetes)

	// A new node gets the next free number, with no clash with the restored ones.
	target = e2eutil.MustResizeCluster(t, 0, constants.Size4, kubernetes, target, 10*time.Minute)
	mustHavePod(t, kubernetes, target.Name+"-0003")

	backup, run = mustTakeSnapshotBackupRun(t, kubernetes)

	e2eutil.NewDocumentSet(bucket, snapshotRestoreDocs).WithPrefix("after").MustCreate(t, kubernetes, target)

	target = mustHibernateCluster(t, kubernetes, target)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, target, true, true)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, 20*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, target, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, target, bucket, snapshotRestoreDocs, 5*time.Minute)
	mustHaveRemappedMembers(t, kubernetes, target, constants.Size4)
}

// mustCreateSnapshotRestoreCluster creates the cluster and a bucket, and writes the documents the
// backup should keep.
func mustCreateSnapshotRestoreCluster(t *testing.T, kubernetes *types.Cluster, spec *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, string) {
	cluster := e2eutil.MustNewClusterFromSpec(t, kubernetes, spec)

	bucket := e2eutil.MustGetBucket(framework.Global.BucketType, framework.Global.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, 2*time.Minute)

	e2eutil.NewDocumentSet(bucket.GetName(), snapshotRestoreDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), snapshotRestoreDocs, time.Minute)

	return cluster, bucket.GetName()
}

// mustTakeSnapshotBackupRun takes one snapshot backup and waits for its run to be Complete.
func mustTakeSnapshotBackupRun(t *testing.T, kubernetes *types.Cluster) (*couchbasev2.CouchbaseBackup, *couchbasev2.CouchbaseSnapshotBackupRun) {
	backup := e2eutil.MustCreateSnapshotBackup(t, kubernetes, snapshotBackupSpec(""), 0)
	run := e2eutil.MustWaitForSnapshotBackupRuns(t, kubernetes, backup, couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, 1, 15*time.Minute)[0]

	return backup, &run
}

// mustHibernateCluster hibernates the cluster and waits until it's done.
func mustHibernateCluster(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/hibernate", true), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 5*time.Minute)

	return cluster
}

// mustRecreateClusterHibernated deletes the cluster and creates it again with the same name and
// spec, hibernated, so it's a new cluster with no data for a restore to go into.
func mustRecreateClusterHibernated(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	spec := hibernatedCopy(cluster, cluster.Name)

	e2eutil.MustDeleteCluster(t, kubernetes, cluster)
	e2eutil.MustWaitForResourceDeletion(t, kubernetes, cluster, 5*time.Minute)

	return mustCreateHibernatedCluster(t, kubernetes, spec)
}

// hibernatedCopy returns a hibernated cluster with the same spec as the given one, under the given
// name.
func hibernatedCopy(cluster *couchbasev2.CouchbaseCluster, name string) *couchbasev2.CouchbaseCluster {
	spec := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cluster.Namespace, Labels: cluster.Labels},
		Spec:       *cluster.Spec.DeepCopy(),
	}
	spec.Spec.Hibernate = true

	return spec
}

// mustCreateHibernatedCluster creates the cluster and waits until it's hibernated.
func mustCreateHibernatedCluster(t *testing.T, kubernetes *types.Cluster, spec *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	cluster := e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, spec)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 5*time.Minute)

	return cluster
}

// snapshotRestoreName is the name every restore test uses for its restore.
const snapshotRestoreName = "restore"

// mustCreateSnapshotRestore restores the run into the target cluster.
func mustCreateSnapshotRestore(t *testing.T, kubernetes *types.Cluster, backup *couchbasev2.CouchbaseBackup, run *couchbasev2.CouchbaseSnapshotBackupRun, target *couchbasev2.CouchbaseCluster, acknowledgeDataLoss, startClusterWhenReady bool) {
	restore := &couchbasev2.CouchbaseSnapshotBackupRestore{
		ObjectMeta: metav1.ObjectMeta{Name: snapshotRestoreName},
		Spec: couchbasev2.CouchbaseSnapshotBackupRestoreSpec{
			BackupName:            backup.Name,
			RunName:               run.Name,
			StartClusterWhenReady: startClusterWhenReady,
			Target:                couchbasev2.CouchbaseSnapshotBackupRestoreTarget{Name: target.Name},
		},
	}

	if acknowledgeDataLoss {
		restore.Spec.Target.AcknowledgeDataLoss = &acknowledgeDataLoss
	}

	if _, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace).Create(t.Context(), restore, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}
}

// mustWaitForSnapshotRestorePhase waits for the restore to reach the phase, and fails straight away
// if it fails instead.
func mustWaitForSnapshotRestorePhase(t *testing.T, kubernetes *types.Cluster, phase couchbasev2.CouchbaseSnapshotBackupRestorePhase, timeout time.Duration) *couchbasev2.CouchbaseSnapshotBackupRestore {
	var restore *couchbasev2.CouchbaseSnapshotBackupRestore

	err := retryutil.RetryFor(timeout, func() error {
		var err error

		restore, err = kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace).Get(t.Context(), snapshotRestoreName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// A failed restore never moves on, so stop waiting and fail below.
		if restore.Status.Phase == couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed {
			return nil
		}

		if restore.Status.Phase != phase {
			return fmt.Errorf("restore is %s, waiting for %s", restore.Status.Phase, phase)
		}

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if restore.Status.Phase != phase {
		e2eutil.Die(t, fmt.Errorf("restore failed: %s", restore.Status.FailureReason))
	}

	return restore
}

// mustHaveRemappedMembers checks every restored member's pod ran the node remap and it finished,
// and the restore removed its mark from every volume afterwards.
func mustHaveRemappedMembers(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, members int) {
	selector := utilconstants.LabelCluster + "=" + cluster.Name

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{LabelSelector: selector + "," + utilconstants.LabelNode})
	if err != nil {
		e2eutil.Die(t, err)
	}

	remapped := 0

	for _, pod := range pods.Items {
		for _, status := range pod.Status.InitContainerStatuses {
			if status.Name == utilconstants.CouchbaseContainerName+"-remap" && status.State.Terminated != nil && status.State.Terminated.ExitCode == 0 {
				remapped++
			}
		}
	}

	if remapped != members {
		e2eutil.Die(t, fmt.Errorf("expected %d pods to have run the node remap, %d did", members, remapped))
	}

	volumes, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, volume := range volumes.Items {
		if _, ok := volume.Annotations[utilconstants.AnnotationSnapshotRestoreRemap]; ok {
			e2eutil.Die(t, fmt.Errorf("expected the restore to remove the remap mark from volume %s", volume.Name))
		}
	}
}

// mustHaveClusterEventSince checks the cluster raised an event with the reason after the given time.
func mustHaveClusterEventSince(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, reason string, since time.Time) {
	err := retryutil.RetryFor(2*time.Minute, func() error {
		events, err := kubernetes.KubeClient.CoreV1().Events(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{FieldSelector: "involvedObject.name=" + cluster.Name + ",reason=" + reason})
		if err != nil {
			return err
		}

		for _, event := range events.Items {
			if event.LastTimestamp.After(since) || event.EventTime.After(since) {
				return nil
			}
		}

		return fmt.Errorf("no %s event on cluster %s since %s", reason, cluster.Name, since)
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustHaveOwnNodeNames checks every Couchbase node is named after the cluster's own pods, so none
// is left with a name from the backed up cluster.
func mustHaveOwnNodeNames(t *testing.T, client *e2eutil.CouchbaseClient, cluster *couchbasev2.CouchbaseCluster) {
	info, err := e2eutil.GetClusterInfo(client)
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, node := range info.Nodes {
		name := string(node.OTPNode)

		if !strings.HasPrefix(name, "ns_1@"+cluster.Name+"-") || !strings.HasSuffix(name, "."+cluster.Name+"."+cluster.Namespace+".svc") {
			e2eutil.Die(t, fmt.Errorf("expected node %s to be named after cluster %s", name, cluster.Name))
		}
	}
}

// mustHaveOwnClusterUUID checks the target has its own cluster UUID, not the backed up cluster's,
// and the restore's request to read it from Couchbase again has been done.
func mustHaveOwnClusterUUID(t *testing.T, kubernetes *types.Cluster, cluster, target *couchbasev2.CouchbaseCluster) {
	err := retryutil.RetryFor(2*time.Minute, func() error {
		source, err := kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(kubernetes.Namespace).Get(t.Context(), cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		restored, err := kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(kubernetes.Namespace).Get(t.Context(), target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if restored.Status.ClusterID == "" || restored.Status.ClusterID == source.Status.ClusterID {
			return fmt.Errorf("cluster %s has ID %q, waiting for its own, not %q", target.Name, restored.Status.ClusterID, source.Status.ClusterID)
		}

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}

	mustNotHaveSavedStateKey(t, kubernetes, target, persistence.RefreshUUID)
}

// mustNotHaveSavedStateKey waits until the key is gone from the cluster's saved state.
func mustNotHaveSavedStateKey(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, key persistence.PersistentKind) {
	err := retryutil.RetryFor(2*time.Minute, func() error {
		state, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Get(t.Context(), cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if _, ok := state.Data[string(key)]; ok {
			return fmt.Errorf("cluster %s still has %s in its saved state", cluster.Name, key)
		}

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustDeleteSnapshotRestore deletes the restore and waits until it's gone, so another one can use
// the same name.
func mustDeleteSnapshotRestore(t *testing.T, kubernetes *types.Cluster) {
	restores := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace)

	if err := restores.Delete(t.Context(), snapshotRestoreName, metav1.DeleteOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	err := retryutil.RetryFor(time.Minute, func() error {
		_, err := restores.Get(t.Context(), snapshotRestoreName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("restore %s not deleted yet: %w", snapshotRestoreName, err)
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// snapshotRestore returns a restore of the run into the target, with the given name.
func snapshotRestore(name, backup, run, target string, acknowledgeDataLoss *bool) *couchbasev2.CouchbaseSnapshotBackupRestore {
	return &couchbasev2.CouchbaseSnapshotBackupRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: couchbasev2.CouchbaseSnapshotBackupRestoreSpec{
			BackupName: backup,
			RunName:    run,
			Target:     couchbasev2.CouchbaseSnapshotBackupRestoreTarget{Name: target, AcknowledgeDataLoss: acknowledgeDataLoss},
		},
	}
}

// mustRejectSnapshotRestore checks the restore is refused, with the message.
func mustRejectSnapshotRestore(t *testing.T, kubernetes *types.Cluster, restore *couchbasev2.CouchbaseSnapshotBackupRestore, message string) {
	_, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace).Create(t.Context(), restore, metav1.CreateOptions{})
	if err == nil {
		e2eutil.Die(t, fmt.Errorf("expected restore %s to be refused", restore.Name))
	}

	if !strings.Contains(err.Error(), message) {
		e2eutil.Die(t, fmt.Errorf("expected restore %s to be refused with %q, got %w", restore.Name, message, err))
	}
}

// mustHaveRecordedHostnames checks the run saved a hostname for every node.
func mustHaveRecordedHostnames(t *testing.T, run *couchbasev2.CouchbaseSnapshotBackupRun, nodes int) {
	if len(run.Spec.Hostnames) != nodes {
		e2eutil.Die(t, fmt.Errorf("expected run %s to save %d hostnames, got %v", run.Name, nodes, run.Spec.Hostnames))
	}

	for member, hostname := range run.Spec.Hostnames {
		if !strings.HasPrefix(hostname, member+".") {
			e2eutil.Die(t, fmt.Errorf("expected run %s to save a hostname for %s, got %s", run.Name, member, hostname))
		}
	}
}

// mustHaveTrustedCA checks the run saved the cluster's CA.
func mustHaveTrustedCA(t *testing.T, run *couchbasev2.CouchbaseSnapshotBackupRun, ca *e2eutil.CertificateAuthority) {
	for _, pem := range run.Spec.TrustedCAs {
		if strings.TrimSpace(pem) == strings.TrimSpace(string(ca.Certificate)) {
			return
		}
	}

	e2eutil.Die(t, fmt.Errorf("expected run %s to save the cluster's CA, it saved %d others", run.Name, len(run.Spec.TrustedCAs)))
}

// mustCreateWaitingStorageClass copies the test storage class, but only makes a volume
// once a pod uses it.
func mustCreateWaitingStorageClass(t *testing.T, kubernetes *types.Cluster) string {
	classes := kubernetes.KubeClient.StorageV1().StorageClasses()

	base, err := classes.Get(t.Context(), framework.Global.SharedTestFlags.StorageClassName, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	waiting := storagev1.VolumeBindingWaitForFirstConsumer
	class := &storagev1.StorageClass{
		ObjectMeta:        metav1.ObjectMeta{Name: "wait-for-pod"},
		Provisioner:       base.Provisioner,
		Parameters:        base.Parameters,
		VolumeBindingMode: &waiting,
	}

	if _, err := classes.Create(t.Context(), class, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		e2eutil.Die(t, err)
	}

	return class.Name
}

// binderPods lists the pods the restore starts to get its volumes made.
func binderPods(t *testing.T, kubernetes *types.Cluster) []v1.Pod {
	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{LabelSelector: "couchbase_snapshot_restore=" + snapshotRestoreName})
	if err != nil {
		e2eutil.Die(t, err)
	}

	return pods.Items
}

// mustSeeBinderPods checks the restore starts its binder pods.
func mustSeeBinderPods(t *testing.T, kubernetes *types.Cluster) {
	err := retryutil.RetryFor(10*time.Minute, func() error {
		if len(binderPods(t, kubernetes)) == 0 {
			return fmt.Errorf("no binder pods yet")
		}

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustHaveNoBinderPods checks the restore removed its binder pods.
func mustHaveNoBinderPods(t *testing.T, kubernetes *types.Cluster) {
	if pods := binderPods(t, kubernetes); len(pods) != 0 {
		e2eutil.Die(t, fmt.Errorf("expected the binder pods to be removed, %d are left", len(pods)))
	}
}

// mustCreateAdminSecret makes an admin login secret and returns its name.
func mustCreateAdminSecret(t *testing.T, kubernetes *types.Cluster, name, username string) string {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Data:       map[string][]byte{"username": []byte(username), "password": []byte("password")},
	}

	if _, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Create(t.Context(), secret, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	return name
}

// mustAcceptSnapshotRestore checks the restore would be let in, without creating it.
func mustAcceptSnapshotRestore(t *testing.T, kubernetes *types.Cluster, restore *couchbasev2.CouchbaseSnapshotBackupRestore) {
	_, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace).Create(t.Context(), restore, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		e2eutil.Die(t, fmt.Errorf("expected restore %s to be let in, got %w", restore.Name, err))
	}
}

// mustRejectSnapshotRestoreChange checks a change to the restore's run is refused, with the message.
func mustRejectSnapshotRestoreChange(t *testing.T, kubernetes *types.Cluster, name, message string) {
	restores := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace)

	restore, err := restores.Get(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	restore.Spec.RunName = "another-run"

	_, err = restores.Update(t.Context(), restore, metav1.UpdateOptions{})
	if err == nil {
		e2eutil.Die(t, fmt.Errorf("expected a change to restore %s to be refused", name))
	}

	if !strings.Contains(err.Error(), message) {
		e2eutil.Die(t, fmt.Errorf("expected a change to restore %s to be refused with %q, got %w", name, message, err))
	}
}

// otherIndexStorageMode returns an index storage mode the cluster doesn't use.
func otherIndexStorageMode(cluster *couchbasev2.CouchbaseCluster) couchbasev2.CouchbaseClusterIndexStorageSetting {
	if cluster.IndexStorageMode() == couchbasev2.CouchbaseClusterIndexStorageSettingStandard {
		return couchbasev2.CouchbaseClusterIndexStorageSettingMemoryOptimized
	}

	return couchbasev2.CouchbaseClusterIndexStorageSettingStandard
}

// mustRecordRestoreDetails checks the run has what a restore needs, which also lets it be restored
// after the backed up cluster is gone.
func mustRecordRestoreDetails(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, run *couchbasev2.CouchbaseSnapshotBackupRun, retention time.Duration) {
	current, err := kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(kubernetes.Namespace).Get(t.Context(), cluster.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	spec := run.Spec

	switch {
	case len(spec.ClusterSpec.Raw) == 0:
		e2eutil.Die(t, fmt.Errorf("expected run %s to record the cluster spec", run.Name))
	case spec.ServerVersion == "":
		e2eutil.Die(t, fmt.Errorf("expected run %s to record the server version", run.Name))
	case spec.ClusterUUID != current.Status.ClusterID:
		e2eutil.Die(t, fmt.Errorf("expected run %s to record cluster ID %s, got %s", run.Name, current.Status.ClusterID, spec.ClusterUUID))
	case spec.NodeCount != constants.Size3:
		e2eutil.Die(t, fmt.Errorf("expected run %s to record %d nodes, got %d", run.Name, constants.Size3, spec.NodeCount))
	case !slices.Contains(spec.Buckets, bucket):
		e2eutil.Die(t, fmt.Errorf("expected run %s to record bucket %s, got %v", run.Name, bucket, spec.Buckets))
	case spec.AdminSecretName != cluster.Spec.Security.AdminSecret:
		e2eutil.Die(t, fmt.Errorf("expected run %s to record admin secret %s, got %s", run.Name, cluster.Spec.Security.AdminSecret, spec.AdminSecretName))
	case spec.BackupRetention == nil || spec.BackupRetention.Duration != retention:
		e2eutil.Die(t, fmt.Errorf("expected run %s to record retention %s, got %v", run.Name, retention, spec.BackupRetention))
	}

	admin, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Get(t.Context(), run.AdminSecretCopyName(), metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, fmt.Errorf("expected run %s to keep a copy of the admin login: %w", run.Name, err))
	}

	if len(admin.OwnerReferences) != 1 || admin.OwnerReferences[0].UID != run.UID {
		e2eutil.Die(t, fmt.Errorf("expected secret %s to be owned by run %s", admin.Name, run.Name))
	}

	// The restore renames the nodes using these, so every node needs one.
	mustHaveRecordedHostnames(t, run, constants.Size3)
}

// mustDeleteSnapshotBackupRun deletes the run and waits until it's gone, along with its snapshots.
func mustDeleteSnapshotBackupRun(t *testing.T, kubernetes *types.Cluster, run *couchbasev2.CouchbaseSnapshotBackupRun) {
	if err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(kubernetes.Namespace).Delete(t.Context(), run.Name, metav1.DeleteOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	mustWaitForSnapshotBackupRunDeleted(t, kubernetes, run, 5*time.Minute)
}

// mustWaitForSecretDeleted waits until the secret is gone.
func mustWaitForSecretDeleted(t *testing.T, kubernetes *types.Cluster, name string, timeout time.Duration) {
	err := retryutil.RetryFor(timeout, func() error {
		_, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Get(t.Context(), name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("secret %s not deleted yet: %w", name, err)
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustWaitForGroupSnapshotDeleted waits until the group snapshot is gone.
func mustWaitForGroupSnapshotDeleted(t *testing.T, kubernetes *types.Cluster, name string, timeout time.Duration) {
	err := retryutil.RetryFor(timeout, func() error {
		_, err := kubernetes.DynamicClient.Resource(volumeGroupSnapshotGVR).Namespace(kubernetes.Namespace).Get(t.Context(), name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("group snapshot %s not deleted yet: %w", name, err)
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustWaitForSnapshotBackupRunStarted waits for the backup's first run to be created, whatever its
// phase.
func mustWaitForSnapshotBackupRunStarted(t *testing.T, kubernetes *types.Cluster, backup *couchbasev2.CouchbaseBackup, timeout time.Duration) *couchbasev2.CouchbaseSnapshotBackupRun {
	var run *couchbasev2.CouchbaseSnapshotBackupRun

	err := retryutil.RetryFor(timeout, func() error {
		runs, err := e2eutil.ListSnapshotBackupRuns(kubernetes, backup)
		if err != nil {
			return err
		}

		if len(runs) == 0 {
			return fmt.Errorf("backup %s has no run yet", backup.Name)
		}

		run = &runs[0]

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}

	return run
}

// mustWaitForSnapshotBackupRunFinished waits until the run is Complete or Invalid.
func mustWaitForSnapshotBackupRunFinished(t *testing.T, kubernetes *types.Cluster, name string, timeout time.Duration) {
	err := retryutil.RetryFor(timeout, func() error {
		run, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(kubernetes.Namespace).Get(t.Context(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		switch run.Status.Phase {
		case couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete, couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid:
			return nil
		}

		return fmt.Errorf("run %s is %s, waiting for it to finish", name, run.Status.Phase)
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustRunVersion checks every pod runs the image, and the cluster reports its version.
func mustRunVersion(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, image string) {
	version, err := couchbaseutil.CouchbaseImageVersion(image)
	if err != nil {
		e2eutil.Die(t, err)
	}

	err = retryutil.RetryFor(5*time.Minute, func() error {
		current, err := kubernetes.CRClient.CouchbaseV2().CouchbaseClusters(kubernetes.Namespace).Get(t.Context(), cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if current.Status.CurrentVersion != version {
			return fmt.Errorf("cluster %s is on %s, waiting for %s", cluster.Name, current.Status.CurrentVersion, version)
		}

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{LabelSelector: utilconstants.LabelCluster + "=" + cluster.Name + "," + utilconstants.LabelNode})
	if err != nil {
		e2eutil.Die(t, err)
	}

	for _, pod := range pods.Items {
		if got := pod.Spec.Containers[0].Image; got != image {
			e2eutil.Die(t, fmt.Errorf("expected pod %s to run %s, got %s", pod.Name, image, got))
		}
	}
}

// mustCountDocsWithIndex counts the bucket's documents with a query that has to scan the primary
// index, so it only works once the index is up, and waits for the count to match.
func mustCountDocsWithIndex(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, count int, timeout time.Duration) {
	query := fmt.Sprintf("SELECT COUNT(*) AS count FROM `%s` WHERE META().id IS NOT MISSING", bucket)

	err := retryutil.RetryFor(timeout, func() error {
		data, err := e2eutil.ExecuteIndexQuery(kubernetes, cluster, query, time.Minute)
		if err != nil {
			return err
		}

		result := struct {
			Results []struct {
				Count int `json:"count"`
			} `json:"results"`
		}{}

		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}

		if len(result.Results) == 0 || result.Results[0].Count != count {
			return fmt.Errorf("index query on bucket %s found %v documents, waiting for %d", bucket, result.Results, count)
		}

		return nil
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustCreateWaitingSnapshotRestore makes a cluster with data and a backup of it, writes more data,
// and starts a restore into the running cluster. The restore waits there until the cluster is
// hibernated, which gives a test time to break something first.
func mustCreateWaitingSnapshotRestore(t *testing.T, kubernetes *types.Cluster) (*couchbasev2.CouchbaseCluster, string, *couchbasev2.CouchbaseBackup, *couchbasev2.CouchbaseSnapshotBackupRun) {
	cluster, bucket := mustCreateSnapshotRestoreCluster(t, kubernetes, clusterOptions().WithPersistentTopology(constants.Size3).Generate(kubernetes))

	backup, run := mustTakeSnapshotBackupRun(t, kubernetes)

	e2eutil.NewDocumentSet(bucket, snapshotRestoreDocs).WithPrefix("after").MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, 2*snapshotRestoreDocs, time.Minute)

	mustCreateSnapshotRestore(t, kubernetes, backup, run, cluster, true, false)
	mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseValidating, 5*time.Minute)

	return cluster, bucket, backup, run
}

// mustFailSnapshotRestoreAndKeepCluster hibernates the cluster so the waiting restore goes ahead,
// checks it fails with the reason, then checks the cluster still has the same volumes and wakes up
// with all its data.
func mustFailSnapshotRestoreAndKeepCluster(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, backup *couchbasev2.CouchbaseBackup, reason string) {
	volumes := mustGetVolumeUIDs(t, kubernetes, cluster)

	cluster = mustHibernateCluster(t, kubernetes, cluster)

	restore := mustWaitForSnapshotRestorePhase(t, kubernetes, couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed, 10*time.Minute)
	if !strings.Contains(restore.Status.FailureReason, reason) {
		e2eutil.Die(t, fmt.Errorf("expected the restore to fail with %q, got %q", reason, restore.Status.FailureReason))
	}

	if after := mustGetVolumeUIDs(t, kubernetes, cluster); !maps.Equal(volumes, after) {
		e2eutil.Die(t, fmt.Errorf("expected cluster %s to keep its volumes, before %v, after %v", cluster.Name, volumes, after))
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/hibernate", false), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket, 2*snapshotRestoreDocs, 5*time.Minute)
}

// mustGetVolumeUIDs returns the UID of each of the cluster's volumes, by name.
func mustGetVolumeUIDs(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) map[string]string {
	volumes, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).List(t.Context(), metav1.ListOptions{LabelSelector: utilconstants.LabelCluster + "=" + cluster.Name})
	if err != nil {
		e2eutil.Die(t, err)
	}

	uids := map[string]string{}
	for _, volume := range volumes.Items {
		uids[volume.Name] = string(volume.UID)
	}

	return uids
}

// mustHavePod fails the test if the pod doesn't exist.
func mustHavePod(t *testing.T, kubernetes *types.Cluster, name string) {
	if _, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).Get(t.Context(), name, metav1.GetOptions{}); err != nil {
		e2eutil.Die(t, fmt.Errorf("expected pod %s: %w", name, err))
	}
}
