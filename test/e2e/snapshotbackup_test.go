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
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"k8s.io/apimachinery/pkg/api/errors"
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
