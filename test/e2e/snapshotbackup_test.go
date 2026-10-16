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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	utilconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var volumeSnapshotGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}

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

	err := retryutil.RetryFor(10*time.Minute, func() error {
		_, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(kubernetes.Namespace).Get(context.Background(), first.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("run %s not deleted yet: %w", first.Name, err)
	})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Its snapshots are owned by it, so they're deleted too.
	for _, snapshot := range first.Status.Snapshots {
		err := retryutil.RetryFor(2*time.Minute, func() error {
			_, err := kubernetes.DynamicClient.Resource(volumeSnapshotGVR).Namespace(kubernetes.Namespace).Get(context.Background(), snapshot.SnapshotName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}

			return fmt.Errorf("snapshot %s not deleted yet: %w", snapshot.SnapshotName, err)
		})
		if err != nil {
			e2eutil.Die(t, err)
		}
	}

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

	pvc, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).Get(context.Background(), backup.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	mustSetSnapshotBackup(t, kubernetes, backup.Name, snapshotBackupSpec("*/30 * * * *"))
	mustWaitForBackupCronJobs(t, kubernetes, backup.Name, 0)

	mustSetSnapshotBackup(t, kubernetes, backup.Name, nil)
	mustWaitForBackupCronJobs(t, kubernetes, backup.Name, 1)

	after, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).Get(context.Background(), backup.Name, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if after.UID != pvc.UID {
		e2eutil.Die(t, fmt.Errorf("expected backup PVC %s to be kept, it was recreated", backup.Name))
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
		backup, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBackups(kubernetes.Namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		backup.Spec.SnapshotBackup = snapshotBackup

		_, err = kubernetes.CRClient.CouchbaseV2().CouchbaseBackups(kubernetes.Namespace).Update(context.Background(), backup, metav1.UpdateOptions{})

		return err
	})
	if err != nil {
		e2eutil.Die(t, err)
	}
}

// mustWaitForBackupCronJobs waits until the backup has count CronJobs.
func mustWaitForBackupCronJobs(t *testing.T, kubernetes *types.Cluster, name string, count int) {
	err := retryutil.RetryFor(5*time.Minute, func() error {
		cronjobs, err := kubernetes.KubeClient.BatchV1().CronJobs(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{})
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
		pod, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).Get(context.Background(), member, metav1.GetOptions{})
		if err != nil {
			e2eutil.Die(t, err)
		}

		node, err := kubernetes.KubeClient.CoreV1().Nodes().Get(context.Background(), pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			e2eutil.Die(t, err)
		}

		if got := node.Labels[utilconstants.ServerGroupLabel]; got != zone {
			e2eutil.Die(t, fmt.Errorf("expected member %s back in zone %s, it's on node %s in zone %s", member, zone, node.Name, got))
		}
	}
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
	spec := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace, Labels: cluster.Labels},
		Spec:       *cluster.Spec.DeepCopy(),
	}
	spec.Spec.Hibernate = true

	e2eutil.MustDeleteCluster(t, kubernetes, cluster)
	e2eutil.MustWaitForResourceDeletion(t, kubernetes, cluster, 5*time.Minute)

	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, spec)
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

	if _, err := kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace).Create(context.Background(), restore, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}
}

// mustWaitForSnapshotRestorePhase waits for the restore to reach the phase, and fails straight away
// if it fails instead.
func mustWaitForSnapshotRestorePhase(t *testing.T, kubernetes *types.Cluster, phase couchbasev2.CouchbaseSnapshotBackupRestorePhase, timeout time.Duration) *couchbasev2.CouchbaseSnapshotBackupRestore {
	var restore *couchbasev2.CouchbaseSnapshotBackupRestore

	err := retryutil.RetryFor(timeout, func() error {
		var err error

		restore, err = kubernetes.CRClient.CouchbaseV2().CouchbaseSnapshotBackupRestores(kubernetes.Namespace).Get(context.Background(), snapshotRestoreName, metav1.GetOptions{})
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

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector + "," + utilconstants.LabelNode})
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

	volumes, err := kubernetes.KubeClient.CoreV1().PersistentVolumeClaims(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
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
		events, err := kubernetes.KubeClient.CoreV1().Events(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{FieldSelector: "involvedObject.name=" + cluster.Name + ",reason=" + reason})
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
