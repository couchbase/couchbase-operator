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
	"fmt"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
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

// Reasons recorded on a run when it's marked Invalid.
const (
	// Creating the snapshots failed.
	snapshotBackupRunReasonCaptureFailed = "CaptureFailed"
	// A snapshot the run needs no longer exists.
	snapshotBackupRunReasonSnapshotFailed = "SnapshotFailed"
	// The run has no snapshots, for example because the cluster has no persistent volumes.
	snapshotBackupRunReasonNoSnapshots = "NoSnapshots"
	// The cluster changed while the snapshots were being taken.
	snapshotBackupRunReasonClusterChanged = "ClusterChanged"
	// The snapshots were taken too far apart, more than maxSkew.
	snapshotBackupRunReasonSkewExceeded = "SkewExceeded"
	// The snapshots didn't become ready within readyTimeout.
	snapshotBackupRunReasonTimedOut = "TimedOut"
	// The cluster couldn't be checked soon enough after the snapshots were taken.
	snapshotBackupRunReasonClusterStateUnknown = "ClusterStateUnknown"
)

// System events that make a run Invalid if logged while its snapshots are being taken, since
// the members may no longer match each other. This matches EventIDsThatInvalidateSnapshots in
// tools-common (MB-74272). Event IDs are unique across services, each service has its own range.
var snapshotBackupRunEvents = map[int]struct{}{
	0:    {}, // Node joined the cluster.
	1:    {}, // Service started, which is how a crashed service coming back shows up.
	4:    {}, // Rebalance failed.
	5:    {}, // Rebalance interrupted.
	6:    {}, // Graceful failover initiated.
	7:    {}, // Graceful failover completed.
	8:    {}, // Graceful failover failed.
	9:    {}, // Graceful failover interrupted.
	10:   {}, // Hard failover initiated.
	11:   {}, // Hard failover completed.
	12:   {}, // Hard failover failed.
	13:   {}, // Hard failover interrupted.
	14:   {}, // Auto failover initiated.
	15:   {}, // Auto failover completed.
	16:   {}, // Auto failover failed.
	17:   {}, // Auto failover warning.
	19:   {}, // Service crashed.
	20:   {}, // Node down.
	8192: {}, // Bucket created.
	8193: {}, // Bucket deleted.
}

// snapshotBackupStateCheckLimit is how soon after the last snapshot is taken the cluster has to
// be read, for the check against the baseline to be trusted. The operator normally reads it
// within about 30 seconds.
const snapshotBackupStateCheckLimit = time.Minute

// isSnapshotBackupRunFor reports whether a run was taken for the named backup by the cluster.
// Two clusters in the same namespace can both pick up one backup, so matching on the backup
// name alone would make each cluster count the other's runs as its own.
func (c *Cluster) isSnapshotBackupRunFor(run *couchbasev2.CouchbaseSnapshotBackupRun, backupName string) bool {
	return run.Spec.SourceBackup == backupName && run.Labels[constants.LabelCluster] == c.cluster.Name
}

// hasInProgressSnapshotBackupRun reports whether the named backup already has a run sitting
// in the InProgress phase. Without this check, every reconcile pass would start a new run
// while an old one is still being captured.
func (c *Cluster) hasInProgressSnapshotBackupRun(backupName string) bool {
	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if c.isSnapshotBackupRunFor(run, backupName) && run.Status.Phase == couchbasev2.CouchbaseSnapshotBackupRunPhaseInProgress {
			return true
		}
	}

	return false
}

// mostRecentCompleteSnapshotBackupRun returns the most recently created Complete run for the
// named backup, or nil if none exists yet. Invalid runs don't count here, the retry rules in
// snapshotBackupRetryAllowed handle those.
func (c *Cluster) mostRecentCompleteSnapshotBackupRun(backupName string) *couchbasev2.CouchbaseSnapshotBackupRun {
	var latest *couchbasev2.CouchbaseSnapshotBackupRun

	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if !c.isSnapshotBackupRunFor(run, backupName) || run.Status.Phase != couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete {
			continue
		}

		if latest == nil || run.CreationTimestamp.After(latest.CreationTimestamp.Time) {
			latest = run
		}
	}

	return latest
}

// snapshotBackupRunsFor returns the cluster's runs for the named backup that are in the
// given phase.
func (c *Cluster) snapshotBackupRunsFor(backupName string, phase couchbasev2.CouchbaseSnapshotBackupRunPhase) []*couchbasev2.CouchbaseSnapshotBackupRun {
	var runs []*couchbasev2.CouchbaseSnapshotBackupRun

	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if c.isSnapshotBackupRunFor(run, backupName) && run.Status.Phase == phase {
			runs = append(runs, run)
		}
	}

	return runs
}

// snapshotBackupDue reports whether a new snapshot backup run should be created for the
// given backup right now.
func (c *Cluster) snapshotBackupDue(backup *couchbasev2.CouchbaseBackup) (bool, error) {
	lastComplete := c.mostRecentCompleteSnapshotBackupRun(backup.Name)
	invalidRuns := c.snapshotBackupRunsFor(backup.Name, couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid)

	return snapshotBackupDueAt(backup, lastComplete, invalidRuns, time.Now())
}

// snapshotBackupDueAt reports whether a backup is due at the given time. A scheduled
// backup's next run is counted from its last Complete run, or from when the backup was
// created if there isn't one yet, so the first run waits for its scheduled time. A backup
// with no schedule runs once, straight away. If attempts for the current scheduled time end
// up Invalid, it's tried again up to maxRetries times, retryInterval apart, and after that it
// waits for the next scheduled time.
func snapshotBackupDueAt(backup *couchbasev2.CouchbaseBackup, lastComplete *couchbasev2.CouchbaseSnapshotBackupRun, invalidRuns []*couchbasev2.CouchbaseSnapshotBackupRun, now time.Time) (bool, error) {
	spec := backup.Spec.SnapshotBackup

	// The scheduled time the current attempt is for. Invalid runs from before it don't count.
	var slot time.Time

	if spec.Schedule == "" {
		// A backup with no schedule only runs once, so it's done once it has a Complete run.
		if lastComplete != nil {
			return false, nil
		}

		slot = backup.CreationTimestamp.Time
	} else {
		parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

		schedule, err := parser.Parse(spec.Schedule)
		if err != nil {
			return false, err
		}

		from := backup.CreationTimestamp.Time
		if lastComplete != nil {
			from = lastComplete.CreationTimestamp.Time
		}

		slot = schedule.Next(from)
		if now.Before(slot) {
			return false, nil
		}

		// Move on to the latest scheduled time that has already passed, so failed attempts
		// for an earlier one don't count against it.
		for next := schedule.Next(slot); !next.After(now); next = schedule.Next(next) {
			slot = next
		}
	}

	return snapshotBackupRetryAllowed(spec, invalidRuns, slot, now), nil
}

// defaultSnapshotBackupMaxRetries matches the CRD default, used if maxRetries isn't set.
const defaultSnapshotBackupMaxRetries = 3

// snapshotBackupRetryAllowed reports whether another attempt can be made for the given
// scheduled time, based on how many attempts since then ended up Invalid.
func snapshotBackupRetryAllowed(spec *couchbasev2.CouchbaseSnapshotBackupSpec, invalidRuns []*couchbasev2.CouchbaseSnapshotBackupRun, slot, now time.Time) bool {
	maxRetries := int32(defaultSnapshotBackupMaxRetries)
	if spec.MaxRetries != nil {
		maxRetries = *spec.MaxRetries
	}

	var failed int32

	var lastFailed time.Time

	for _, run := range invalidRuns {
		created := run.CreationTimestamp.Time
		if created.Before(slot) {
			continue
		}

		failed++

		if created.After(lastFailed) {
			lastFailed = created
		}
	}

	// The first attempt plus maxRetries retries, then wait for the next scheduled time.
	if failed > maxRetries {
		return false
	}

	return failed == 0 || !now.Before(lastFailed.Add(spec.RetryInterval.Duration))
}

// snapshotBackupClusterState is the cluster's state read immediately before capture begins.
// It gets recorded on the run and compared against the same signals read again after
// capture, to detect whether anything changed mid capture.
type snapshotBackupClusterState struct {
	orchestrator    string
	balanced        bool
	rebalancing     bool
	allMembersReady bool

	// events are the system events logged since the run started, only read after capture.
	events []couchbaseutil.SystemEvent
}

// readSnapshotBackupClusterState reads the cluster's current orchestrator and balance state.
// It's read before capture as the baseline, and again afterwards to tell whether anything
// changed while the snapshots were being taken.
func (c *Cluster) readSnapshotBackupClusterState() (*snapshotBackupClusterState, error) {
	var clusterInfo couchbaseutil.ClusterInfo
	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	var terseInfo couchbaseutil.TerseClusterInfo
	if err := couchbaseutil.GetTerseClusterInfo(&terseInfo).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	state := &snapshotBackupClusterState{
		orchestrator: terseInfo.Orchestrator,
		balanced:     clusterInfo.Balanced,
		rebalancing: clusterInfo.RebalanceStatus != couchbaseutil.RebalanceStatusNotRunning &&
			clusterInfo.RebalanceStatus != couchbaseutil.RebalanceStatusNone,
		allMembersReady: c.members.Diff(c.readyMembers()).Empty(),
	}

	return state, nil
}

// readSnapshotBackupBaseline reads the cluster's current state. A nil baseline with a nil
// error means the cluster isn't currently in a state safe to capture from (a member isn't
// ready, the cluster is unbalanced, or a rebalance is in progress) and this cycle should be
// skipped, not treated as a real error.
func (c *Cluster) readSnapshotBackupBaseline() (*snapshotBackupClusterState, error) {
	// A member that's down doesn't always make the cluster report itself as unbalanced,
	// and a backup missing one member's volumes can't be restored, so wait until every
	// member is ready.
	if !c.members.Diff(c.readyMembers()).Empty() {
		return nil, nil
	}

	state, err := c.readSnapshotBackupClusterState()
	if err != nil {
		return nil, err
	}

	if !state.balanced || state.rebalancing {
		return nil, nil
	}

	return state, nil
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
func (c *Cluster) createSnapshotBackupRun(backup *couchbasev2.CouchbaseBackup, baseline *snapshotBackupClusterState) (*couchbasev2.CouchbaseSnapshotBackupRun, error) {
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
			MaxSkew:         &backup.Spec.SnapshotBackup.MaxSkew,
			ReadyTimeout:    &backup.Spec.SnapshotBackup.ReadyTimeout,
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

// listSnapshotBackupRunSnapshots returns the VolumeSnapshots a run took. For a group snapshot
// these are the snapshots k8s creates under the group, one per volume. They only appear once
// the group snapshot has been picked up, so the list can be empty for a while.
func (c *Cluster) listSnapshotBackupRunSnapshots(run *couchbasev2.CouchbaseSnapshotBackupRun) ([]snapshotv1.VolumeSnapshot, error) {
	options := metav1.ListOptions{}

	// Snapshots k8s creates for a group don't carry the run label, so for a group snapshot
	// we list them all and keep the ones owned by the run's group snapshot.
	if !run.Status.GroupSnapshot {
		options.LabelSelector = constants.LabelSnapshotBackupRun + "=" + run.Name
	}

	list, err := c.k8s.DynamicClient.Resource(volumeSnapshotGVR).Namespace(c.cluster.Namespace).List(context.Background(), options)
	if err != nil {
		return nil, err
	}

	var snapshots []snapshotv1.VolumeSnapshot

	for _, item := range list.Items {
		if run.Status.GroupSnapshot && !isOwnedByGroupSnapshot(item.GetOwnerReferences(), *run.Status.GroupSnapshotName) {
			continue
		}

		var snapshot snapshotv1.VolumeSnapshot
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &snapshot); err != nil {
			return nil, err
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}

// getSnapshotBackupRunGroupSnapshot returns the run's VolumeGroupSnapshot. It returns nil if
// the group snapshot no longer exists.
func (c *Cluster) getSnapshotBackupRunGroupSnapshot(run *couchbasev2.CouchbaseSnapshotBackupRun) (*groupsnapshotv1beta1.VolumeGroupSnapshot, error) {
	object, err := c.k8s.DynamicClient.Resource(volumeGroupSnapshotGVR).Namespace(c.cluster.Namespace).Get(context.Background(), *run.Status.GroupSnapshotName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var groupSnapshot groupsnapshotv1beta1.VolumeGroupSnapshot
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &groupSnapshot); err != nil {
		return nil, err
	}

	return &groupSnapshot, nil
}

// isOwnedByGroupSnapshot reports whether one of the owner references is the named
// VolumeGroupSnapshot.
func isOwnedByGroupSnapshot(owners []metav1.OwnerReference, groupSnapshotName string) bool {
	for _, owner := range owners {
		if owner.Kind == "VolumeGroupSnapshot" && owner.Name == groupSnapshotName {
			return true
		}
	}

	return false
}

// snapshotBackupRunEntries turns a group snapshot's snapshots into the list recorded on the
// run. memberForPVC returns the member a PVC belongs to, from its node label.
func snapshotBackupRunEntries(snapshots []snapshotv1.VolumeSnapshot, memberForPVC func(string) string) []couchbasev2.CouchbaseSnapshotBackupRunSnapshot {
	var entries []couchbasev2.CouchbaseSnapshotBackupRunSnapshot

	for _, snapshot := range snapshots {
		pvc := ""
		if snapshot.Spec.Source.PersistentVolumeClaimName != nil {
			pvc = *snapshot.Spec.Source.PersistentVolumeClaimName
		}

		entries = append(entries, couchbasev2.CouchbaseSnapshotBackupRunSnapshot{
			Member:       memberForPVC(pvc),
			VolumeName:   pvc,
			SnapshotName: snapshot.Name,
		})
	}

	return entries
}

// snapshotBackupRunCheck is the outcome of checking a run. An empty phase means the run
// isn't decided yet and should be checked again next time.
type snapshotBackupRunCheck struct {
	phase   couchbasev2.CouchbaseSnapshotBackupRunPhase
	reason  string
	message string
	// skew is set once every snapshot has been taken. It's recorded on the run, so the skew
	// and cluster checks, which only make sense at that moment, are only done once.
	skew *metav1.Duration
}

// checkSnapshotBackupRun works out whether a run is Complete, Invalid, or not decided yet.
// readState reads the cluster's current state. It's only called once, at the moment every
// snapshot has been taken, since that's when the cluster needs to match the baseline.
func checkSnapshotBackupRun(run *couchbasev2.CouchbaseSnapshotBackupRun, snapshots []snapshotv1.VolumeSnapshot, groupSnapshot *groupsnapshotv1beta1.VolumeGroupSnapshot, readyTimeout, maxSkew time.Duration, readState func() (*snapshotBackupClusterState, error), now time.Time) (snapshotBackupRunCheck, error) {
	// The snapshot controller keeps retrying after an error, and clears it once a retry works,
	// so an error just means the snapshots aren't there yet. The last one is kept for the
	// message in case the run times out.
	lastError := snapshotBackupRunLastError(snapshots, groupSnapshot)

	// Still waiting, unless the run has been going longer than readyTimeout.
	waiting := func(check snapshotBackupRunCheck) snapshotBackupRunCheck {
		if now.Sub(run.CreationTimestamp.Time) > readyTimeout {
			message := fmt.Sprintf("the snapshots weren't ready within %s", readyTimeout)
			if lastError != "" {
				message += ", last error: " + lastError
			}

			return invalidSnapshotBackupRun(snapshotBackupRunReasonTimedOut, message)
		}

		return check
	}

	if run.Status.GroupSnapshot && groupSnapshot == nil {
		return invalidSnapshotBackupRun(snapshotBackupRunReasonSnapshotFailed, "the group snapshot no longer exists"), nil
	}

	if len(snapshots) == 0 {
		// A group snapshot's snapshots only appear once k8s has picked the group up, so
		// until the group is ready, none yet just means it's still early.
		if run.Status.GroupSnapshot && !isGroupSnapshotReady(groupSnapshot) {
			return waiting(snapshotBackupRunCheck{}), nil
		}

		return invalidSnapshotBackupRun(snapshotBackupRunReasonNoSnapshots, "the run has no snapshots, the cluster may have no persistent volumes"), nil
	}

	check := snapshotBackupRunCheck{skew: run.Status.MeasuredSkew}

	if check.skew == nil {
		earliest, latest, allTaken := snapshotCreationRange(snapshots)
		if !allTaken {
			return waiting(check), nil
		}

		captured, err := checkSnapshotBackupRunCapture(run, earliest, latest, maxSkew, readState, now)
		if err != nil || captured.phase != "" {
			return captured, err
		}

		check.skew = captured.skew
	}

	for _, snapshot := range snapshots {
		if snapshot.Status == nil || snapshot.Status.ReadyToUse == nil || !*snapshot.Status.ReadyToUse {
			return waiting(check), nil
		}
	}

	check.phase = couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete

	return check, nil
}

// invalidSnapshotBackupRun is the check result for a run that's Invalid.
func invalidSnapshotBackupRun(reason, message string) snapshotBackupRunCheck {
	return snapshotBackupRunCheck{phase: couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid, reason: reason, message: message}
}

// snapshotBackupRunLastError returns the last error reported on any of a run's snapshots, or
// on its group snapshot, or "" if there isn't one.
func snapshotBackupRunLastError(snapshots []snapshotv1.VolumeSnapshot, groupSnapshot *groupsnapshotv1beta1.VolumeGroupSnapshot) string {
	for _, snapshot := range snapshots {
		if snapshot.Status != nil && snapshot.Status.Error != nil {
			message := "snapshot " + snapshot.Name + " failed"
			if snapshot.Status.Error.Message != nil {
				message += ": " + *snapshot.Status.Error.Message
			}

			return message
		}
	}

	if groupSnapshot != nil && groupSnapshot.Status != nil && groupSnapshot.Status.Error != nil {
		message := "group snapshot " + groupSnapshot.Name + " failed"
		if groupSnapshot.Status.Error.Message != nil {
			message += ": " + *groupSnapshot.Status.Error.Message
		}

		return message
	}

	return ""
}

// checkSnapshotBackupRunCapture does the checks that only make sense at the moment every
// snapshot has been taken: how far apart they were, and whether the cluster changed from the
// baseline. If both pass, the result has no phase and just carries the measured skew.
func checkSnapshotBackupRunCapture(run *couchbasev2.CouchbaseSnapshotBackupRun, earliest, latest time.Time, maxSkew time.Duration, readState func() (*snapshotBackupClusterState, error), now time.Time) (snapshotBackupRunCheck, error) {
	skew := &metav1.Duration{Duration: latest.Sub(earliest)}

	if skew.Duration > maxSkew {
		result := invalidSnapshotBackupRun(snapshotBackupRunReasonSkewExceeded, fmt.Sprintf("the snapshots were taken %s apart, more than maxSkew %s", skew.Duration, maxSkew))
		result.skew = skew

		return result, nil
	}

	// A reading taken long after the snapshots can't be trusted, a later change could be
	// mistaken for one during capture, or a change during capture could have come and gone.
	if late := now.Sub(latest); late > snapshotBackupStateCheckLimit {
		result := invalidSnapshotBackupRun(snapshotBackupRunReasonClusterStateUnknown, fmt.Sprintf("the cluster couldn't be checked until %s after the snapshots were taken, so there's no way to tell if it changed", late.Round(time.Second)))
		result.skew = skew

		return result, nil
	}

	state, err := readState()
	if err != nil {
		return snapshotBackupRunCheck{}, err
	}

	if change := snapshotBackupClusterChange(run, state, latest); change != "" {
		result := invalidSnapshotBackupRun(snapshotBackupRunReasonClusterChanged, change)
		result.skew = skew

		return result, nil
	}

	return snapshotBackupRunCheck{skew: skew}, nil
}

// isGroupSnapshotReady reports whether a group snapshot has finished being taken.
func isGroupSnapshotReady(groupSnapshot *groupsnapshotv1beta1.VolumeGroupSnapshot) bool {
	return groupSnapshot.Status != nil && groupSnapshot.Status.ReadyToUse != nil && *groupSnapshot.Status.ReadyToUse
}

// snapshotCreationRange returns the earliest and latest time the snapshots were taken.
// allTaken is false if any of them hasn't been taken yet.
func snapshotCreationRange(snapshots []snapshotv1.VolumeSnapshot) (earliest, latest time.Time, allTaken bool) {
	for i, snapshot := range snapshots {
		if snapshot.Status == nil || snapshot.Status.CreationTime == nil {
			return time.Time{}, time.Time{}, false
		}

		created := snapshot.Status.CreationTime.Time

		if i == 0 || created.Before(earliest) {
			earliest = created
		}

		if i == 0 || created.After(latest) {
			latest = created
		}
	}

	return earliest, latest, true
}

// snapshotBackupClusterChange compares the cluster's state with the baseline recorded on the
// run, and returns what changed, or "" if nothing did.
func snapshotBackupClusterChange(run *couchbasev2.CouchbaseSnapshotBackupRun, state *snapshotBackupClusterState, latest time.Time) string {
	switch {
	case !state.allMembersReady:
		return "a member stopped being ready while the snapshots were being taken"
	case !state.balanced || state.rebalancing:
		return "the cluster became unbalanced or started rebalancing while the snapshots were being taken"
	case state.orchestrator != run.Status.BaselineOrchestrator:
		return fmt.Sprintf("the orchestrator changed from %s to %s while the snapshots were being taken", run.Status.BaselineOrchestrator, state.orchestrator)
	}

	if event := snapshotBackupRunEvent(run, state.events, latest); event != nil {
		return fmt.Sprintf("%s on %s at %s while the snapshots were being taken", event.Description, event.Node, event.Timestamp.Format(time.RFC3339))
	}

	return ""
}

// snapshotBackupRunEvent returns the first event from the list above that was logged between
// the run starting and its last snapshot being taken, or nil if there wasn't one.
func snapshotBackupRunEvent(run *couchbasev2.CouchbaseSnapshotBackupRun, events []couchbaseutil.SystemEvent, latest time.Time) *couchbaseutil.SystemEvent {
	start := snapshotBackupRunStart(run)

	for i := range events {
		event := &events[i]

		if _, ok := snapshotBackupRunEvents[event.EventID]; !ok {
			continue
		}

		// Snapshot times are only to the second, so allow for the rest of that second.
		if event.Timestamp.Before(start) || event.Timestamp.After(latest.Add(time.Second)) {
			continue
		}

		return event
	}

	return nil
}

// snapshotBackupRunStart returns when the run started, or when it was created if the start
// was never recorded.
func snapshotBackupRunStart(run *couchbasev2.CouchbaseSnapshotBackupRun) time.Time {
	if run.Status.StartTime != nil {
		return run.Status.StartTime.Time
	}

	return run.CreationTimestamp.Time
}

// Used for a run taken before the run recorded its own limits. They match the CRD defaults.
const (
	defaultSnapshotBackupReadyTimeout = 2 * time.Hour
	defaultSnapshotBackupMaxSkew      = 10 * time.Second
)

// validateSnapshotBackupRuns decides the outcome of each of the cluster's InProgress runs.
// It includes runs whose backup was deleted or switched back to archive, so none of them is
// left InProgress forever.
func (c *Cluster) validateSnapshotBackupRuns() error {
	var errs []error

	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if run.Labels[constants.LabelCluster] != c.cluster.Name || run.Status.Phase != couchbasev2.CouchbaseSnapshotBackupRunPhaseInProgress {
			continue
		}

		if err := c.validateSnapshotBackupRun(run.DeepCopy()); err != nil {
			errs = append(errs, fmt.Errorf("snapshot backup run %s: %w", run.Name, err))
		}
	}

	return errors.Join(errs...)
}

// validateSnapshotBackupRun checks one run and records the result on it.
func (c *Cluster) validateSnapshotBackupRun(run *couchbasev2.CouchbaseSnapshotBackupRun) error {
	readyTimeout := defaultSnapshotBackupReadyTimeout
	maxSkew := defaultSnapshotBackupMaxSkew

	if run.Spec.ReadyTimeout != nil {
		readyTimeout = run.Spec.ReadyTimeout.Duration
	}

	if run.Spec.MaxSkew != nil {
		maxSkew = run.Spec.MaxSkew.Duration
	}

	snapshots, err := c.listSnapshotBackupRunSnapshots(run)
	if err != nil {
		return err
	}

	var groupSnapshot *groupsnapshotv1beta1.VolumeGroupSnapshot

	if run.Status.GroupSnapshot {
		if groupSnapshot, err = c.getSnapshotBackupRunGroupSnapshot(run); err != nil {
			return err
		}
	}

	// After capture, also read the events logged since the run started.
	readState := func() (*snapshotBackupClusterState, error) {
		state, err := c.readSnapshotBackupClusterState()
		if err != nil {
			return nil, err
		}

		// The system event log only exists once every member is on 7.1 or later, so older
		// clusters rely on the other checks.
		if !c.SupportsVersionFeatures("7.1.0") {
			return state, nil
		}

		var events couchbaseutil.SystemEvents
		if err := couchbaseutil.GetSystemEvents(snapshotBackupRunStart(run), &events).On(c.api, c.readyMembers()); err != nil {
			return nil, err
		}

		state.events = events.Events

		return state, nil
	}

	check, err := checkSnapshotBackupRun(run, snapshots, groupSnapshot, readyTimeout, maxSkew, readState, time.Now())
	if err != nil {
		return err
	}

	// Nothing to record while it's still waiting, unless the skew has just been measured.
	skewJustMeasured := run.Status.MeasuredSkew == nil && check.skew != nil
	if check.phase == "" && !skewJustMeasured {
		return nil
	}

	run.Status.MeasuredSkew = check.skew

	switch check.phase {
	case couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete:
		run.Status.Phase = check.phase

		// A group snapshot's snapshots are only known now, so record them for restore.
		if run.Status.GroupSnapshot {
			run.Status.Snapshots = snapshotBackupRunEntries(snapshots, c.memberForPVC)
		}
	case couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid:
		run.Status.Phase = check.phase
		run.Status.Reason = check.reason
		run.Status.Message = check.message

		// Remove the snapshots, and clear the record of them, so the run doesn't point at
		// snapshots that no longer exist.
		c.cleanupFailedSnapshotCapture(run)
		run.Status.Snapshots = nil
		run.Status.GroupSnapshot = false
		run.Status.GroupSnapshotName = nil
	}

	if _, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(c.cluster.Namespace).UpdateStatus(context.Background(), run, metav1.UpdateOptions{}); err != nil {
		return err
	}

	switch check.phase {
	case couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete:
		c.raiseEvent(k8sutil.SnapshotBackupRunCompletedEvent(run.Name, c.cluster))
	case couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid:
		c.raiseEvent(k8sutil.SnapshotBackupRunInvalidEvent(run.Name, check.reason, check.message, c.cluster))
	}

	return nil
}

// memberForPVC returns the member a PVC belongs to, from its node label.
func (c *Cluster) memberForPVC(name string) string {
	if pvc, ok := c.k8s.PersistentVolumeClaims.Get(name); ok {
		return pvc.Labels[constants.LabelNode]
	}

	return ""
}

// cleanupOldSnapshotBackupRuns deletes the backup's runs that are older than its
// backupRetention. Deleting a run also deletes its snapshots, since they're owned by it.
func (c *Cluster) cleanupOldSnapshotBackupRuns(backup *couchbasev2.CouchbaseBackup) error {
	retention := backup.Spec.BackupRetention.Duration

	var runs []*couchbasev2.CouchbaseSnapshotBackupRun

	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if c.isSnapshotBackupRunFor(run, backup.Name) {
			runs = append(runs, run)
		}
	}

	for _, run := range snapshotBackupRunsToDelete(runs, retention, time.Now()) {
		if err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(c.cluster.Namespace).Delete(context.Background(), run.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}

		c.log.Info("Deleted snapshot backup run past its retention", "cluster", c.namespacedName(), "run", run.Name)
	}

	return nil
}

// snapshotBackupRunsToDelete returns the runs older than the retention period. The newest
// Complete run is always kept, so a long run of failures can't leave the backup with nothing
// to restore from, and InProgress runs are never deleted.
func snapshotBackupRunsToDelete(runs []*couchbasev2.CouchbaseSnapshotBackupRun, retention time.Duration, now time.Time) []*couchbasev2.CouchbaseSnapshotBackupRun {
	var newestComplete *couchbasev2.CouchbaseSnapshotBackupRun

	for _, run := range runs {
		if run.Status.Phase == couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete && (newestComplete == nil || run.CreationTimestamp.After(newestComplete.CreationTimestamp.Time)) {
			newestComplete = run
		}
	}

	var toDelete []*couchbasev2.CouchbaseSnapshotBackupRun

	for _, run := range runs {
		finished := run.Status.Phase == couchbasev2.CouchbaseSnapshotBackupRunPhaseComplete || run.Status.Phase == couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid
		if !finished || run == newestComplete {
			continue
		}

		if now.Sub(run.CreationTimestamp.Time) > retention {
			toDelete = append(toDelete, run)
		}
	}

	return toDelete
}

// reconcileSnapshotBackup drives CSI VolumeSnapshot based backups. It first decides the
// outcome of runs already taken, then deletes runs older than their backup's backupRetention,
// then takes a new run for each backup that's due. Archive
// based backups, ones without spec.snapshotBackup set, are untouched by this function
// entirely, they continue through reconcileBackup exactly as before.
func (c *Cluster) reconcileSnapshotBackup() error {
	var backups []couchbasev2.CouchbaseBackup

	// gatherBackups only returns the backups the cluster's selector picks, so two
	// clusters in the same namespace never capture each other's backups.
	if c.cluster.Spec.Backup.Managed {
		var err error
		if backups, err = c.gatherBackups(); err != nil {
			return err
		}
	}

	// Most clusters never use snapshot backups. Skip all of this unless a backup asks for one,
	// or the cluster still has runs to check or clean up.
	if !usesSnapshotBackup(backups) && !c.hasSnapshotBackupRuns() {
		return nil
	}

	// Decide the outcome of runs already taken first, so a run that's just finished doesn't
	// hold up the next one. This runs even if backups aren't managed, so a run that was
	// already going when that was turned off still gets an outcome.
	errs := []error{c.validateSnapshotBackupRuns()}

	// Each backup is handled on its own, so an error on one doesn't stop the others being
	// backed up this cycle.
	for i := range backups {
		if err := c.cleanupOldSnapshotBackupRuns(&backups[i]); err != nil {
			errs = append(errs, fmt.Errorf("snapshot backup %s: %w", backups[i].Name, err))
		}

		if err := c.reconcileSnapshotBackupFor(&backups[i]); err != nil {
			errs = append(errs, fmt.Errorf("snapshot backup %s: %w", backups[i].Name, err))
		}
	}

	// Errors are logged, not returned, so a snapshot problem doesn't stop the rest of the
	// reconcile. Anything that failed is tried again next time.
	if err := errors.Join(errs...); err != nil {
		c.log.Error(err, "Snapshot backup failed", "cluster", c.namespacedName())
	}

	return nil
}

// usesSnapshotBackup reports whether any of the backups asks for snapshot backups.
func usesSnapshotBackup(backups []couchbasev2.CouchbaseBackup) bool {
	for i := range backups {
		if backups[i].Spec.SnapshotBackup != nil {
			return true
		}
	}

	return false
}

// hasSnapshotBackupRuns reports whether the cluster has any snapshot backup runs, including ones
// whose backup was deleted or switched back to archive.
func (c *Cluster) hasSnapshotBackupRuns() bool {
	for _, run := range c.k8s.CouchbaseSnapshotBackupRuns.List() {
		if run.Labels[constants.LabelCluster] == c.cluster.Name {
			return true
		}
	}

	return false
}

// reconcileSnapshotBackupFor takes a snapshot backup run for one backup, if one is due.
func (c *Cluster) reconcileSnapshotBackupFor(backup *couchbasev2.CouchbaseBackup) error {
	if backup.Spec.SnapshotBackup == nil {
		return nil
	}

	if c.hasInProgressSnapshotBackupRun(backup.Name) {
		return nil
	}

	due, err := c.snapshotBackupDue(backup)
	if err != nil {
		return err
	}

	if !due {
		return nil
	}

	baseline, err := c.readSnapshotBackupBaseline()
	if err != nil {
		return err
	}

	if baseline == nil {
		// Cluster isn't in a state safe to capture from right now, try again next cycle.
		return nil
	}

	run, err := c.createSnapshotBackupRun(backup, baseline)
	if err != nil {
		return err
	}

	captureErr := c.captureSnapshots(backup, run)

	endTime := metav1.Now()
	run.Status.EndTime = &endTime

	if captureErr != nil {
		// Remove whatever the attempt did manage to create, and mark the run Invalid so
		// it no longer blocks the next attempt.
		c.cleanupFailedSnapshotCapture(run)
		run.Status.Snapshots = nil
		run.Status.GroupSnapshot = false
		run.Status.GroupSnapshotName = nil
		run.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRunPhaseInvalid
		run.Status.Reason = snapshotBackupRunReasonCaptureFailed
		run.Status.Message = captureErr.Error()
	}

	if _, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseSnapshotBackupRuns(c.cluster.Namespace).UpdateStatus(context.Background(), run, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return captureErr
}
