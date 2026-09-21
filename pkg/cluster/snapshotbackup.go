/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"context"
	"encoding/json"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	groupsnapshotv1beta1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/robfig/cron/v3"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// volumeSnapshotGVR and volumeGroupSnapshotGVR name the two actual snapshot kinds this
// reconciler creates. Both are namespaced.
var (
	volumeSnapshotGVR = schema.GroupVersionResource{
		Group:    "snapshot.storage.k8s.io",
		Version:  "v1",
		Resource: "volumesnapshots",
	}
	// v1beta1, not v1. The v1 type needs k8s.io v0.36.1, the repo is pinned to v0.30.14,
	// and bumping that breaks a lot of other things. Sticking with v1beta1 for now.
	volumeGroupSnapshotGVR = schema.GroupVersionResource{
		Group:    "groupsnapshot.storage.k8s.io",
		Version:  "v1beta1",
		Resource: "volumegroupsnapshots",
	}
)

// hasInProgressSnapshotBackupRun reports whether the named backup already has a run sitting
// in the InProgress phase. Without this check, every reconcile pass would start a new run
// while an old one is still being captured.
func (c *Cluster) hasInProgressSnapshotBackupRun(backupName string) bool {
	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if run.Spec.SourceBackup == backupName && run.Status.Phase == couchbasev2.CouchbaseSnapshotBackupRunPhaseInProgress {
			return true
		}
	}

	return false
}

// mostRecentCompleteSnapshotBackupRun returns the most recently created Complete run for the
// named backup, or nil if none exists yet. An Invalid run doesn't count here, so a
// discarded attempt doesn't delay the next try.
func (c *Cluster) mostRecentCompleteSnapshotBackupRun(backupName string) *couchbasev2.CouchbaseSnapshotBackupRun {
	var latest *couchbasev2.CouchbaseSnapshotBackupRun

	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if run.Spec.SourceBackup != backupName || run.Status.Phase != couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete {
			continue
		}

		if latest == nil || run.CreationTimestamp.After(latest.CreationTimestamp.Time) {
			latest = run
		}
	}

	return latest
}

// snapshotBackupDue reports whether a new snapshot backup run should be created for the
// given backup right now, based on its cron schedule and the most recent Complete run.
func (c *Cluster) snapshotBackupDue(backup *couchbasev2.CouchbaseBackup) (bool, error) {
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	schedule, err := parser.Parse(backup.Spec.SnapshotBackup.Schedule)
	if err != nil {
		return false, err
	}

	lastRun := c.mostRecentCompleteSnapshotBackupRun(backup.Name)
	if lastRun == nil {
		// No prior successful run at all, due immediately.
		return true, nil
	}

	next := schedule.Next(lastRun.CreationTimestamp.Time)

	return !time.Now().Before(next), nil
}

// snapshotBackupBaseline is the cluster's state read immediately before capture begins.
// It gets recorded on the run and compared against the same signals read again after
// capture, to detect whether anything changed mid capture.
type snapshotBackupBaseline struct {
	orchestrator string
	balanced     bool
	chronicleRev *int64
}

// readSnapshotBackupBaseline reads the cluster's current state. A nil baseline with a nil
// error means the cluster isn't currently in a state safe to capture from (a member isn't
// ready, the cluster is unbalanced, or a rebalance is in progress) and this cycle should be
// skipped, not treated as a real error.
func (c *Cluster) readSnapshotBackupBaseline() (*snapshotBackupBaseline, error) {
	// A member that's down doesn't always make the cluster report itself as unbalanced,
	// and a backup missing one member's volumes can't be restored, so wait until every
	// member is ready.
	if !c.members.Diff(c.readyMembers()).Empty() {
		return nil, nil
	}

	var clusterInfo couchbaseutil.ClusterInfo
	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	rebalanceSafe := clusterInfo.RebalanceStatus == couchbaseutil.RebalanceStatusNotRunning ||
		clusterInfo.RebalanceStatus == couchbaseutil.RebalanceStatusNone

	if !clusterInfo.Balanced || !rebalanceSafe {
		return nil, nil
	}

	var terseInfo couchbaseutil.TerseClusterInfo
	if err := couchbaseutil.GetTerseClusterInfo(&terseInfo).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	baseline := &snapshotBackupBaseline{
		orchestrator: terseInfo.Orchestrator,
		balanced:     true,
	}

	if c.SupportsVersionFeatures("8.5.0") {
		rev := clusterInfo.ChronicleRev
		baseline.chronicleRev = &rev
	}

	return baseline, nil
}

// snapshotBackupResourceNames returns the names of every bucket, scope, and collection
// currently declared on the cluster. Recorded on a run so a restore can later detect one
// with no matching CR, rather than silently losing it to ordinary reconciliation right
// after the restore completes.
func (c *Cluster) snapshotBackupResourceNames() (bucketNames, scopeNames, collectionNames []string, err error) {
	buckets, err := c.gatherBuckets()
	if err != nil {
		return nil, nil, nil, err
	}

	for _, bucket := range buckets {
		bucketNames = append(bucketNames, bucket.BucketName)
	}

	scopedBuckets, err := c.listScopedBuckets()
	if err != nil {
		return nil, nil, nil, err
	}

	for _, bucket := range scopedBuckets {
		scopes, err := c.gatherScopes(bucket)
		if err != nil {
			return nil, nil, nil, err
		}

		for _, scope := range scopes {
			scopeNames = append(scopeNames, scope.CouchbaseName())

			collections, err := c.gatherCollections(scope)
			if err != nil {
				return nil, nil, nil, err
			}

			for _, collection := range collections {
				collectionNames = append(collectionNames, collection.CouchbaseName())
			}
		}
	}

	return bucketNames, scopeNames, collectionNames, nil
}

// createSnapshotBackupRun creates a new CouchbaseSnapshotBackupRun for the given backup,
// in the InProgress phase, before any snapshot API call is made. If the operator crashes
// midway through, this run stays sitting there marked InProgress instead of quietly
// looking like it finished.
// CouchbaseSnapshotBackupRun has a status subresource, so this is deliberately two calls,
// Create only ever writes spec, the phase, start time, and baseline fields are all written
// afterward with a separate UpdateStatus call.
func (c *Cluster) createSnapshotBackupRun(backup *couchbasev2.CouchbaseBackup, baseline *snapshotBackupBaseline) (*couchbasev2.CouchbaseSnapshotBackupRun, error) {
	specJSON, err := json.Marshal(c.cluster.Spec)
	if err != nil {
		return nil, err
	}

	bucketNames, scopeNames, collectionNames, err := c.snapshotBackupResourceNames()
	if err != nil {
		return nil, err
	}

	run := &couchbasev2.CouchbaseSnapshotBackupRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: backup.Name + "-",
			Namespace:    c.cluster.Namespace,
			Labels: map[string]string{
				constants.LabelCluster: c.cluster.Name,
			},
		},
		Spec: couchbasev2.CouchbaseSnapshotBackupRunSpec{
			SourceBackup:    backup.Name,
			ClusterSpec:     runtime.RawExtension{Raw: specJSON},
			ServerVersion:   c.cluster.Status.CurrentVersion,
			NodeCount:       len(c.readyMembers()),
			ClusterUUID:     c.cluster.Status.ClusterID,
			AdminSecretName: c.cluster.Spec.Security.AdminSecret,
			Buckets:         bucketNames,
			Scopes:          scopeNames,
			Collections:     collectionNames,
		},
	}

	created, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(c.cluster.Namespace).Create(context.Background(), run, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	startTime := metav1.Now()

	// Phase and start time are set here, not left to a default or set later, so a
	// crash right after this still leaves a clear record that the run started.
	created.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRunPhaseInProgress
	created.Status.StartTime = &startTime
	created.Status.BaselineOrchestrator = baseline.orchestrator
	created.Status.BaselineBalanced = baseline.balanced
	created.Status.BaselineChronicleRev = baseline.chronicleRev

	return c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(c.cluster.Namespace).UpdateStatus(context.Background(), created, metav1.UpdateOptions{})
}

// backfillDataVolumeLabels makes sure every non log PVC for this cluster carries the label
// a group snapshot selector needs, including ones created before that label existed. A
// label update never requires recreating the PVC.
func (c *Cluster) backfillDataVolumeLabels() error {
	for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
		if pvc.Labels[constants.LabelCluster] != c.cluster.Name {
			continue
		}

		if k8sutil.IsLogPVC(pvc) {
			continue
		}

		// The backup job's own PVC shares the cluster label too, but only a real
		// member volume has a node label, so this keeps the backup job's PVC out.
		if _, ok := pvc.Labels[constants.LabelNode]; !ok {
			continue
		}

		if _, ok := pvc.Labels[constants.LabelDataVolume]; ok {
			continue
		}

		// Patch just this one label instead of a full update.
		patch := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]string{
					constants.LabelDataVolume: "true",
				},
			},
		}

		patchBytes, err := json.Marshal(patch)
		if err != nil {
			return err
		}

		if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Patch(context.Background(), pvc.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	return nil
}

// createVolumeSnapshot creates a single VolumeSnapshot for the given PVC, returning its
// name once created. ctx lets the call be cancelled when another snapshot in the same
// run has already failed.
func (c *Cluster) createVolumeSnapshot(ctx context.Context, run *couchbasev2.CouchbaseSnapshotBackupRun, className, pvcName string) (string, error) {
	snapshot := &snapshotv1.VolumeSnapshot{
		TypeMeta: metav1.TypeMeta{
			APIVersion: volumeSnapshotGVR.GroupVersion().String(),
			Kind:       "VolumeSnapshot",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: run.Name + "-",
			Namespace:    c.cluster.Namespace,
			Labels: map[string]string{
				constants.LabelSnapshotBackupRun: run.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				run.AsOwner(),
			},
		},
		Spec: snapshotv1.VolumeSnapshotSpec{
			VolumeSnapshotClassName: &className,
			Source: snapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &pvcName,
			},
		},
	}

	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(snapshot)
	if err != nil {
		return "", err
	}

	created, err := c.k8s.DynamicClient.Resource(volumeSnapshotGVR).Namespace(c.cluster.Namespace).Create(ctx, &unstructured.Unstructured{Object: object}, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	return created.GetName(), nil
}

// captureIndividualSnapshots creates one VolumeSnapshot per data volume across every member.
// This is the universally available path, used whenever group snapshots aren't configured
// or aren't usable. Each snapshot's name is known as soon as it's created, so
// run.Status.Snapshots is filled in directly here.
// A large cluster can have many volumes, so these are created in parallel with goroutines
// rather than one at a time.
func (c *Cluster) captureIndividualSnapshots(backup *couchbasev2.CouchbaseBackup, run *couchbasev2.CouchbaseSnapshotBackupRun) error {
	className := backup.Spec.SnapshotBackup.VolumeSnapshotClassName

	type volumeToSnapshot struct {
		member string
		pvc    string
	}

	var volumes []volumeToSnapshot

	for _, member := range c.readyMembers() {
		for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
			if pvc.Labels[constants.LabelNode] != member.Name() {
				continue
			}

			if k8sutil.IsLogPVC(pvc) {
				continue
			}

			volumes = append(volumes, volumeToSnapshot{member: member.Name(), pvc: pvc.Name})
		}
	}

	// Each goroutine writes to its own slot, so no lock is needed to collect the results.
	snapshots := make([]couchbasev2.CouchbaseSnapshotBackupRunSnapshot, len(volumes))

	// If one snapshot fails, the shared context is cancelled so the rest stop early
	// instead of carrying on for a run that has already failed.
	g, ctx := errgroup.WithContext(context.Background())

	for i, volume := range volumes {
		g.Go(func() error {
			snapshotName, err := c.createVolumeSnapshot(ctx, run, className, volume.pvc)
			if err != nil {
				return err
			}

			snapshots[i] = couchbasev2.CouchbaseSnapshotBackupRunSnapshot{
				Member:       volume.member,
				VolumeName:   volume.pvc,
				SnapshotName: snapshotName,
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	run.Status.Snapshots = append(run.Status.Snapshots, snapshots...)

	return nil
}

// captureGroupSnapshot creates a single VolumeGroupSnapshot covering every data volume
// across the whole cluster, selected by label rather than an explicit list. Its member
// snapshots are created asynchronously by k8s, so this only records the group
// object's own name, run.Status.Snapshots is filled in by a later phase once it's ready.
func (c *Cluster) captureGroupSnapshot(backup *couchbasev2.CouchbaseBackup, run *couchbasev2.CouchbaseSnapshotBackupRun) error {
	if err := c.backfillDataVolumeLabels(); err != nil {
		return err
	}

	// The controller that reads this selector only understands a plain list of
	// label/value pairs that must all match. It has no way to check that a label
	// is missing, which is why we mark the volumes we want (constants.LabelDataVolume)
	// rather than the one we don't.
	groupSnapshot := &groupsnapshotv1beta1.VolumeGroupSnapshot{
		TypeMeta: metav1.TypeMeta{
			APIVersion: volumeGroupSnapshotGVR.GroupVersion().String(),
			Kind:       "VolumeGroupSnapshot",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: run.Name + "-",
			Namespace:    c.cluster.Namespace,
			Labels: map[string]string{
				constants.LabelSnapshotBackupRun: run.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				run.AsOwner(),
			},
		},
		Spec: groupsnapshotv1beta1.VolumeGroupSnapshotSpec{
			VolumeGroupSnapshotClassName: backup.Spec.SnapshotBackup.VolumeGroupSnapshotClassName,
			Source: groupsnapshotv1beta1.VolumeGroupSnapshotSource{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						constants.LabelCluster:    c.cluster.Name,
						constants.LabelDataVolume: "true",
					},
				},
			},
		},
	}

	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(groupSnapshot)
	if err != nil {
		return err
	}

	created, err := c.k8s.DynamicClient.Resource(volumeGroupSnapshotGVR).Namespace(c.cluster.Namespace).Create(context.Background(), &unstructured.Unstructured{Object: object}, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	name := created.GetName()
	run.Status.GroupSnapshot = true
	run.Status.GroupSnapshotName = &name

	return nil
}

// captureSnapshots decides between the group snapshot path and the individual snapshot
// path, and performs the capture. We don't check upfront whether either named class
// actually exists, that would need a permission tied to the whole cluster rather than to
// one namespace, which conflicts with running the operator scoped to a single namespace.
// If a class name is wrong, the snapshot it creates simply never becomes ready, and the
// later validation phase catches that the same way it catches any other capture that
// didn't turn out usable.
func (c *Cluster) captureSnapshots(backup *couchbasev2.CouchbaseBackup, run *couchbasev2.CouchbaseSnapshotBackupRun) error {
	if backup.Spec.SnapshotBackup.VolumeGroupSnapshotClassName != nil {
		return c.captureGroupSnapshot(backup, run)
	}

	return c.captureIndividualSnapshots(backup, run)
}

// cleanupFailedSnapshotCapture removes everything a failed capture attempt created. Snapshots
// are owned by their run, so deleting a run deletes them, but a failed run is kept as a record
// of the failure, so its snapshots have to be deleted here instead. They're found by the run's
// label, not by the names recorded on the run, because a snapshot can still get created even
// when its call failed or was cancelled, and then we will never know its name.
// Deleting the VolumeGroupSnapshot is enough for the group path, its members are owned by it
// and are removed automatically. If anything fails here, we just log it rather than return
// it, so it doesn't hide the real error from the capture itself.
func (c *Cluster) cleanupFailedSnapshotCapture(run *couchbasev2.CouchbaseSnapshotBackupRun) {
	selector := metav1.ListOptions{LabelSelector: constants.LabelSnapshotBackupRun + "=" + run.Name}

	for _, gvr := range []schema.GroupVersionResource{volumeSnapshotGVR, volumeGroupSnapshotGVR} {
		client := c.k8s.DynamicClient.Resource(gvr).Namespace(c.cluster.Namespace)

		list, err := client.List(context.Background(), selector)
		if err != nil {
			// NotFound here means this kind isn't installed on the cluster, so there's
			// nothing of it to clean up.
			if !apierrors.IsNotFound(err) {
				c.log.Info("[WARN] Failed to list snapshots to clean up after a failed capture", "cluster", c.namespacedName(), "resource", gvr.Resource, "error", err)
			}

			continue
		}

		for _, item := range list.Items {
			if err := client.Delete(context.Background(), item.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				c.log.Info("[WARN] Failed to clean up snapshot after a failed capture", "cluster", c.namespacedName(), "resource", gvr.Resource, "name", item.GetName(), "error", err)
			}
		}
	}
}

// reconcileSnapshotBackup drives CSI VolumeSnapshot based backup capture. Archive based
// backups, ones without spec.snapshotBackup set, are untouched by this function entirely,
// they continue through reconcileBackup exactly as before.
// This only ever gets a run as far as InProgress. It doesn't decide whether a run actually
// succeeded, mark it Complete or Invalid, or clean up old runs.
func (c *Cluster) reconcileSnapshotBackup() error {
	if !c.cluster.Spec.Backup.Managed {
		return nil
	}

	// gatherBackups only returns the backups the cluster's selector picks, so two
	// clusters in the same namespace never capture each other's backups.
	backups, err := c.gatherBackups()
	if err != nil {
		return err
	}

	for i := range backups {
		backup := &backups[i]

		if backup.Spec.SnapshotBackup == nil {
			continue
		}

		if c.hasInProgressSnapshotBackupRun(backup.Name) {
			continue
		}

		due, err := c.snapshotBackupDue(backup)
		if err != nil {
			return err
		}

		if !due {
			continue
		}

		baseline, err := c.readSnapshotBackupBaseline()
		if err != nil {
			return err
		}

		if baseline == nil {
			// Cluster isn't in a state safe to capture from right now, try again next cycle.
			continue
		}

		run, err := c.createSnapshotBackupRun(backup, baseline)
		if err != nil {
			return err
		}

		captureErr := c.captureSnapshots(backup, run)

		endTime := metav1.Now()
		run.Status.EndTime = &endTime

		if captureErr != nil {
			// remove whatever the attempt did manage to create, then clear
			// the record of it, so a failed run honestly reads as nothing captured
			// rather than pointing at snapshots that no longer exist.
			c.cleanupFailedSnapshotCapture(run)
			run.Status.Snapshots = nil
			run.Status.GroupSnapshot = false
			run.Status.GroupSnapshotName = nil
		}

		if _, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(c.cluster.Namespace).UpdateStatus(context.Background(), run, metav1.UpdateOptions{}); err != nil {
			return err
		}

		if captureErr != nil {
			return captureErr
		}
	}

	return nil
}
