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
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	vtypes "github.com/couchbase/couchbase-operator/pkg/validator/types"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// snapshotRestoreLabel marks the short lived pods a restore uses to bind its volumes, so the
// operator never takes them for cluster members.
const snapshotRestoreLabel = "couchbase_snapshot_restore"

// volumeSnapshotResource is the VolumeSnapshot API the restore reads snapshots from.
var volumeSnapshotResource = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}

// snapshotRestoreRecheck is how long to wait before checking a waiting restore again.
const snapshotRestoreRecheck = 10 * time.Second

// Conditions the restore sets, one while it waits for its target cluster to be hibernated, and
// one for warnings that don't stop it.
const (
	snapshotRestoreConditionTargetHibernated = "TargetHibernated"
	snapshotRestoreReasonTargetRunning       = "TargetRunning"
	snapshotRestoreConditionWarnings         = "Warnings"
	snapshotRestoreReasonWarnings            = "RestoreWarnings"
	snapshotRestoreConditionWaitingToStart   = "WaitingToStart"
	snapshotRestoreReasonWaitingForUser      = "WaitingForUser"
)

// CouchbaseSnapshotBackupRestoreReconciler carries out snapshot backup restores. It runs on its own,
// not inside the cluster's reconcile, because a hibernated cluster's reconcile stops early.
type CouchbaseSnapshotBackupRestoreReconciler struct {
	client client.Client
	// kubeClient, couchbaseClient and validatorOptions build a new validator for each check, since
	// the validator's cache isn't refreshed and is only meant to last one check.
	kubeClient       kubernetes.Interface
	couchbaseClient  versioned.Interface
	validatorOptions *vtypes.ValidatorOptions
	// dynamic reads VolumeSnapshots.
	dynamic dynamic.Interface
}

// newSnapshotBackupRestoreReconciler builds the restore reconciler, with a validator built the same
// way as the operator's own, and a client to read VolumeSnapshots.
func newSnapshotBackupRestoreReconciler(mgr manager.Manager) (*CouchbaseSnapshotBackupRestoreReconciler, error) {
	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	couchbaseClient, err := versioned.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	options := &vtypes.ValidatorOptions{
		// So the admin username check runs again before the restore starts.
		ValidateSecrets:        true,
		ValidateStorageClasses: false,
	}

	return &CouchbaseSnapshotBackupRestoreReconciler{
		client:           mgr.GetClient(),
		kubeClient:       kubeClient,
		couchbaseClient:  couchbaseClient,
		validatorOptions: options,
		dynamic:          dynamicClient,
	}, nil
}

// newValidator returns a validator for one check, so it never sees data loaded for an earlier one.
func (r *CouchbaseSnapshotBackupRestoreReconciler) newValidator() *vtypes.Validator {
	return vtypes.New(r.kubeClient, r.couchbaseClient, r.validatorOptions)
}

// getRun returns the run the restore names, or nil if it doesn't exist.
func (r *CouchbaseSnapshotBackupRestoreReconciler) getRun(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (*couchbasev2.CouchbaseSnapshotBackupRun, error) {
	run := &couchbasev2.CouchbaseSnapshotBackupRun{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Spec.RunName}, run); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return run, nil
}

// Reconcile moves a restore on to its next phase.
func (r *CouchbaseSnapshotBackupRestoreReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	restore := &couchbasev2.CouchbaseSnapshotBackupRestore{}
	if err := r.client.Get(ctx, req.NamespacedName, restore); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	phase := restore.Status.Phase

	result, err := r.reconcilePhase(ctx, restore)

	// Only once it's saved, so the log never shows a phase the restore didn't reach.
	if err == nil && restore.Status.Phase != phase {
		log.Info("Snapshot restore moved on", "restore", req.NamespacedName, "from", phase, "to", restore.Status.Phase)
	}

	return result, err
}

// reconcilePhase does the work for the restore's current phase.
func (r *CouchbaseSnapshotBackupRestoreReconciler) reconcilePhase(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (reconcile.Result, error) {
	switch restore.Status.Phase {
	case couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete, couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed:
		// Finished, nothing more to do.
		return reconcile.Result{}, nil
	case "", couchbasev2.CouchbaseSnapshotBackupRestorePhaseValidating:
		return r.validate(ctx, restore)
	case couchbasev2.CouchbaseSnapshotBackupRestorePhaseDestroying:
		return r.destroy(ctx, restore)
	case couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning:
		return r.provision(ctx, restore)
	case couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart:
		return r.readyToStart(ctx, restore)
	case couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping:
		return r.remap(ctx, restore)
	case couchbasev2.CouchbaseSnapshotBackupRestorePhaseWaitingForCluster:
		return r.waitForCluster(ctx, restore)
	}

	// A phase we don't know, so leave it alone.
	return reconcile.Result{}, nil
}

// validate checks the restore can go ahead. The target cluster must exist and be hibernated, since
// the operator would otherwise keep recreating its pods while the restore swaps their volumes.
// A running target isn't an error, the restore waits until the user hibernates it.
func (r *CouchbaseSnapshotBackupRestoreReconciler) validate(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (reconcile.Result, error) {
	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseValidating

	started := false
	if restore.Status.StartTime == nil {
		now := metav1.Now()
		restore.Status.StartTime = &now
		started = true
	}

	target := &couchbasev2.CouchbaseCluster{}

	if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Spec.Target.Name}, target); err != nil {
		if errors.IsNotFound(err) {
			return r.fail(ctx, restore, fmt.Sprintf("cluster %s doesn't exist", restore.Spec.Target.Name))
		}

		return reconcile.Result{}, err
	}

	if !target.Spec.Hibernate || !target.HasCondition(couchbasev2.ClusterConditionHibernating) {
		changed := meta.SetStatusCondition(&restore.Status.Conditions, metav1.Condition{
			Type:    snapshotRestoreConditionTargetHibernated,
			Status:  metav1.ConditionFalse,
			Reason:  snapshotRestoreReasonTargetRunning,
			Message: fmt.Sprintf("set spec.hibernate to true on cluster %s, the restore waits until it's hibernated", target.Name),
		})

		// We check again every few seconds while we wait, so only write when something changed.
		if changed || started {
			if err := r.client.Status().Update(ctx, restore); err != nil {
				return reconcile.Result{}, err
			}
		}

		return reconcile.Result{RequeueAfter: snapshotRestoreRecheck}, nil
	}

	meta.RemoveStatusCondition(&restore.Status.Conditions, snapshotRestoreConditionTargetHibernated)

	// Things may have changed since the restore was created, so check it again.
	warnings, err := validationv2.CheckConstraintsSnapshotBackupRestore(r.newValidator(), restore)
	if err != nil {
		return r.fail(ctx, restore, err.Error())
	}

	// Warnings don't stop the restore, but the user should see them.
	if len(warnings) > 0 {
		meta.SetStatusCondition(&restore.Status.Conditions, metav1.Condition{
			Type:    snapshotRestoreConditionWarnings,
			Status:  metav1.ConditionTrue,
			Reason:  snapshotRestoreReasonWarnings,
			Message: strings.Join(warnings, "; "),
		})
	}

	if err := r.checkSnapshotsReady(ctx, restore); err != nil {
		return r.fail(ctx, restore, err.Error())
	}

	// Destroying removes the target's old volumes, if it has any, before new ones are made.
	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseDestroying

	return reconcile.Result{}, r.client.Status().Update(ctx, restore)
}

// checkSnapshotsReady checks every snapshot the run recorded still exists and is ready to use, so
// the restore never deletes anything when a snapshot it needs is missing.
func (r *CouchbaseSnapshotBackupRestoreReconciler) checkSnapshotsReady(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) error {
	run, err := r.getRun(ctx, restore)
	if err != nil {
		return err
	}

	if run == nil {
		return fmt.Errorf("run %s doesn't exist", restore.Spec.RunName)
	}

	for _, snapshot := range run.Status.Snapshots {
		object, err := r.dynamic.Resource(volumeSnapshotResource).Namespace(restore.Namespace).Get(ctx, snapshot.SnapshotName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return fmt.Errorf("snapshot %s for member %s at %s doesn't exist", snapshot.SnapshotName, snapshot.Member, snapshot.MountPath)
		}

		if err != nil {
			return err
		}

		if ready, _, _ := unstructured.NestedBool(object.Object, "status", "readyToUse"); !ready {
			return fmt.Errorf("snapshot %s for member %s at %s isn't ready to use", snapshot.SnapshotName, snapshot.Member, snapshot.MountPath)
		}
	}

	return nil
}

// destroy deletes the target cluster's old volumes, so new ones can be made from the snapshots
// under the same names. Validating has already made sure the user agreed to lose that data.
// A new cluster has no volumes, so it goes straight to Provisioning.
func (r *CouchbaseSnapshotBackupRestoreReconciler) destroy(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (reconcile.Result, error) {
	// Waking the cluster now isn't safe, the operator would start pods on volumes we're replacing.
	if reason, err := r.targetNotHibernated(ctx, restore); err != nil || reason != "" {
		if err != nil {
			return reconcile.Result{}, err
		}

		return r.fail(ctx, restore, reason)
	}

	volumes, err := r.targetVolumes(ctx, restore)
	if err != nil {
		return reconcile.Result{}, err
	}

	for i := range volumes {
		// Already being deleted, just wait for it to go.
		if volumes[i].DeletionTimestamp != nil {
			continue
		}

		if err := r.client.Delete(ctx, &volumes[i]); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
	}

	// A new volume can't use a name until the old one is fully gone, so wait until they all are.
	if len(volumes) > 0 {
		return reconcile.Result{RequeueAfter: snapshotRestoreRecheck}, nil
	}

	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseProvisioning

	return reconcile.Result{}, r.client.Status().Update(ctx, restore)
}

// targetVolumes returns the target cluster's member volumes. They carry the cluster label and the
// member they belong to. A backup volume carries the cluster label but no member, so it's kept.
// targetNotHibernated returns why the restore can't go on with its target, if the target was woken
// or deleted while its volumes were being replaced, or an empty string if it's still hibernated.
// It checks spec.hibernate rather than the condition, since the spec changes as soon as the user
// edits it.
func (r *CouchbaseSnapshotBackupRestoreReconciler) targetNotHibernated(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (string, error) {
	name := restore.Spec.Target.Name
	target := &couchbasev2.CouchbaseCluster{}

	err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: name}, target)
	if errors.IsNotFound(err) {
		return fmt.Sprintf("cluster %s was deleted before the restore finished replacing its volumes", name), nil
	}

	if err != nil {
		return "", err
	}

	if !target.Spec.Hibernate {
		return fmt.Sprintf("cluster %s was woken before the restore finished replacing its volumes, hibernate it and create a new restore", name), nil
	}

	return "", nil
}

func (r *CouchbaseSnapshotBackupRestoreReconciler) targetVolumes(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) ([]corev1.PersistentVolumeClaim, error) {
	list := &corev1.PersistentVolumeClaimList{}

	if err := r.client.List(ctx, list, client.InNamespace(restore.Namespace), client.MatchingLabels{constants.LabelCluster: restore.Spec.Target.Name}); err != nil {
		return nil, err
	}

	var volumes []corev1.PersistentVolumeClaim

	for _, pvc := range list.Items {
		if _, ok := pvc.Labels[constants.LabelNode]; ok {
			volumes = append(volumes, pvc)
		}
	}

	return volumes, nil
}

// provision makes the target's volumes again from the run's snapshots, with the same names, labels
// and annotations the operator gives its own, so the operator takes them as its members when the
// cluster wakes. The operator only does that for bound volumes, so a volume still waiting for a
// pod is bound by a short lived pod first.
func (r *CouchbaseSnapshotBackupRestoreReconciler) provision(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (reconcile.Result, error) {
	// Waking the cluster now isn't safe, the operator would start pods on volumes we're replacing.
	if reason, err := r.targetNotHibernated(ctx, restore); err != nil || reason != "" {
		if err != nil {
			return reconcile.Result{}, err
		}

		return r.fail(ctx, restore, reason)
	}

	run, err := r.getRun(ctx, restore)
	if err != nil {
		return reconcile.Result{}, err
	}

	if run == nil {
		return r.fail(ctx, restore, fmt.Sprintf("run %s doesn't exist", restore.Spec.RunName))
	}

	target := &couchbasev2.CouchbaseCluster{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Spec.Target.Name}, target); err != nil {
		return reconcile.Result{}, err
	}

	volumes, err := restoreVolumes(target, run, string(restore.UID))
	if err != nil {
		return r.fail(ctx, restore, err.Error())
	}

	unbound, err := r.createVolumes(ctx, volumes)
	if err != nil {
		return reconcile.Result{}, err
	}

	if len(unbound) > 0 {
		if err := r.bindVolumes(ctx, restore, target, unbound); err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{RequeueAfter: snapshotRestoreRecheck}, nil
	}

	// Every volume is bound, so the pods that bound them aren't needed any more.
	if err := r.deleteBinderPods(ctx, restore); err != nil {
		return reconcile.Result{}, err
	}

	// The restored volumes belong to the backed up cluster, so the target has to carry its ID and
	// saved state too, otherwise the operator sets it up as a brand new cluster when it wakes.
	if err := r.seedTargetState(ctx, target, run); err != nil {
		return reconcile.Result{}, err
	}

	// Nodes named after the k8s nodes they run on get their new names from
	// a list we fill in once their pods are placed. It starts empty, so the
	// restored pods wait for it.
	if k8sutil.UsesNodeHostnames(target) {
		if err := r.resetRemapList(ctx, restore, target); err != nil {
			return reconcile.Result{}, err
		}
	}

	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseReadyToStart

	return reconcile.Result{}, r.client.Status().Update(ctx, restore)
}

// deleteBinderPods removes the pods that bound the restore's volumes. They're deleted one at a
// time, since the operator isn't allowed to delete pods in bulk.
func (r *CouchbaseSnapshotBackupRestoreReconciler) deleteBinderPods(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) error {
	pods := &corev1.PodList{}
	if err := r.client.List(ctx, pods, client.InNamespace(restore.Namespace), client.MatchingLabels{snapshotRestoreLabel: restore.Name}); err != nil {
		return err
	}

	for i := range pods.Items {
		if err := r.client.Delete(ctx, &pods.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// seedTargetState gives the target the backed up cluster's ID and saved state, so the operator
// recovers its members from the restored volumes instead of setting it up from scratch. The
// operator keeps this state in a Secret named after the cluster.
func (r *CouchbaseSnapshotBackupRestoreReconciler) seedTargetState(ctx context.Context, target *couchbasev2.CouchbaseCluster, run *couchbasev2.CouchbaseSnapshotBackupRun) error {
	// Secrets are read straight from the API server, they're only needed once per restore and
	// caching them would keep every Secret in the namespace in memory.
	state, err := r.kubeClient.CoreV1().Secrets(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		// The operator makes this when it first sees the cluster, so wait for it.
		return err
	}

	podIndex, err := restorePodIndex(state, run)
	if err != nil {
		return err
	}

	if state.Data == nil {
		state.Data = map[string][]byte{}
	}

	state.Data[string(persistence.UUID)] = []byte(run.Spec.ClusterUUID)
	state.Data[string(persistence.Version)] = []byte(run.Spec.ServerVersion)
	state.Data[string(persistence.PodIndex)] = []byte(strconv.Itoa(podIndex))

	// A cluster with a new name gets a new UUID from the node remap tool, so the operator reads it
	// from the cluster once it's up.
	if restoreSourceCluster(run) != target.Name {
		state.Data[string(persistence.RefreshUUID)] = []byte("true")
	}

	// The restored volumes still list the backed up server certificate as loaded, so the
	// operator reloads the cluster's own one once it's up.
	if target.IsTLSEnabled() {
		state.Data[string(persistence.ReloadTLS)] = []byte("true")
	}

	// The restored data only accepts the password from when the backup was taken. The operator
	// logs in with it, then changes it to the target's admin secret like any password change.
	admin, err := r.kubeClient.CoreV1().Secrets(run.Namespace).Get(ctx, run.AdminSecretCopyName(), metav1.GetOptions{})
	switch {
	case err == nil:
		state.Data[string(persistence.Password)] = admin.Data[constants.AuthSecretPasswordKey]
	case !errors.IsNotFound(err):
		return err
	}

	if _, err := r.kubeClient.CoreV1().Secrets(state.Namespace).Update(ctx, state, metav1.UpdateOptions{}); err != nil {
		return err
	}

	target.Status.ClusterID = run.Spec.ClusterUUID

	return r.client.Status().Update(ctx, target)
}

// restorePodIndex returns the number the operator gives the next member it adds. It must be past
// every restored member, so a new member never takes a restored one's name, and it never goes
// below what the cluster already used.
func restorePodIndex(state *corev1.Secret, run *couchbasev2.CouchbaseSnapshotBackupRun) (int, error) {
	next := 0

	if saved, ok := state.Data[string(persistence.PodIndex)]; ok {
		index, err := strconv.Atoi(string(saved))
		if err != nil {
			return 0, err
		}

		next = index
	}

	for _, snapshot := range run.Status.Snapshots {
		index, err := couchbaseutil.GetIndexFromMemberName(snapshot.Member)
		if err != nil {
			return 0, err
		}

		if index+1 > next {
			next = index + 1
		}
	}

	return next, nil
}

// restoreVolumes returns every volume the restore has to make, one set per member in the run, each
// made from the snapshot taken of it. Each member keeps its number and takes the target's cluster
// name. All of the run's members are restored, even if the target asks for fewer, the operator
// scales down afterwards.
func restoreVolumes(target *couchbasev2.CouchbaseCluster, run *couchbasev2.CouchbaseSnapshotBackupRun, restoreID string) ([]*corev1.PersistentVolumeClaim, error) {
	members, err := k8sutil.SnapshotRestoreMembers(target, run)
	if err != nil {
		return nil, err
	}

	// Each volume lists every backed up member's hostname, so the remap
	// knows the old names.
	pairs := make([]string, 0, len(members))
	for i := range members {
		pairs = append(pairs, members[i].Name+"="+run.Spec.Hostnames[members[i].Name])
	}

	remap := strings.Join(pairs, ",")

	var volumes []*corev1.PersistentVolumeClaim

	for i := range members {
		memberVolumes, err := restoreMemberVolumes(target, &members[i], remap, restoreID)
		if err != nil {
			return nil, err
		}

		volumes = append(volumes, memberVolumes...)
	}

	return volumes, nil
}

// restoreMemberVolumes returns the volumes for one backed up member, each pointing at its snapshot
// and owned by the target cluster, like the operator's own volumes.
func restoreMemberVolumes(target *couchbasev2.CouchbaseCluster, restored *k8sutil.SnapshotRestoreMember, remap, restoreID string) ([]*corev1.PersistentVolumeClaim, error) {
	name, err := restoreMemberName(target, restored.Name)
	if err != nil {
		return nil, err
	}

	config := restored.Config
	image := target.Spec.ServerClassCouchbaseImage(config)

	version, err := k8sutil.CouchbaseVersion(image)
	if err != nil {
		return nil, err
	}

	member := couchbaseutil.NewMember(target.Namespace, target.Name, name, version, config.Name, target.IsTLSEnabled(), image)

	volumes, err := k8sutil.MemberPersistentVolumeClaims(target, member, *config)
	if err != nil {
		return nil, err
	}

	snapshotGroup := "snapshot.storage.k8s.io"

	for _, volume := range volumes {
		path := volume.Annotations[constants.AnnotationVolumeMountPath]

		snapshot, ok := restored.Snapshots[path]
		if !ok {
			return nil, fmt.Errorf("the run has no snapshot for member %s at %s", restored.Name, path)
		}

		volume.Namespace = target.Namespace
		volume.OwnerReferences = []metav1.OwnerReference{target.AsOwner()}
		volume.Spec.DataSource = &corev1.TypedLocalObjectReference{APIGroup: &snapshotGroup, Kind: "VolumeSnapshot", Name: snapshot}

		// The member's pod runs a node remap before Couchbase starts, and the restore removes the
		// mark once it's done. If the target has a different name, the mark holds every backed up
		// member's name, because each node needs the new names of all the others.
		volume.Annotations[constants.AnnotationSnapshotRestoreRemap] = remap
		volume.Annotations[constants.AnnotationSnapshotRestoreRemapID] = restoreID
	}

	// Give the volumes the same server group and zone the operator would, so recovery puts the
	// member back where it was. Older runs didn't record these, so their volumes are left as they are.
	if restored.ServerGroup != "" {
		for _, volume := range volumes {
			k8sutil.AddServerGroupAnnotations(volume, restored.ServerGroup, restored.AvailabilityZone, target)
		}
	}

	return volumes, nil
}

// restoreMemberName returns the target's name for a backed up member. It keeps the member's number
// and uses the target's cluster name.
func restoreMemberName(target *couchbasev2.CouchbaseCluster, member string) (string, error) {
	index, err := couchbaseutil.GetIndexFromMemberName(member)
	if err != nil {
		return "", err
	}

	return couchbaseutil.CreateMemberName(target.Name, index), nil
}

// restoreSourceCluster returns the name of the cluster the run was taken from, from the run's label,
// or for a run without it, from its member names.
func restoreSourceCluster(run *couchbasev2.CouchbaseSnapshotBackupRun) string {
	if name := run.Labels[constants.LabelCluster]; name != "" {
		return name
	}

	for _, snapshot := range run.Status.Snapshots {
		if len(snapshot.Member) > 5 {
			return snapshot.Member[:len(snapshot.Member)-5]
		}
	}

	return ""
}

// createVolumes makes any volume that doesn't exist yet, and returns the ones that aren't bound yet.
// A volume that already exists is left as it is, so a retry after a restart is safe.
func (r *CouchbaseSnapshotBackupRestoreReconciler) createVolumes(ctx context.Context, volumes []*corev1.PersistentVolumeClaim) ([]*corev1.PersistentVolumeClaim, error) {
	var unbound []*corev1.PersistentVolumeClaim

	for _, volume := range volumes {
		existing := &corev1.PersistentVolumeClaim{}

		err := r.client.Get(ctx, types.NamespacedName{Namespace: volume.Namespace, Name: volume.Name}, existing)
		if errors.IsNotFound(err) {
			if err := r.client.Create(ctx, volume); err != nil && !errors.IsAlreadyExists(err) {
				return nil, err
			}

			unbound = append(unbound, volume)

			continue
		}

		if err != nil {
			return nil, err
		}

		if existing.Status.Phase != corev1.ClaimBound {
			unbound = append(unbound, existing)
		}
	}

	return unbound, nil
}

// bindVolumes starts one short lived pod per member that just mounts its unbound volumes. Some
// storage only makes a volume once a pod uses it, and the operator only recovers members from
// bound volumes. The pod runs sleep in the target's own image, so nothing new is pulled.
func (r *CouchbaseSnapshotBackupRestoreReconciler) bindVolumes(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore, target *couchbasev2.CouchbaseCluster, unbound []*corev1.PersistentVolumeClaim) error {
	memberVolumes := map[string][]string{}
	memberClass := map[string]*couchbasev2.ServerConfig{}
	memberZone := map[string]string{}

	for _, volume := range unbound {
		member := volume.Labels[constants.LabelNode]
		memberVolumes[member] = append(memberVolumes[member], volume.Name)
		memberClass[member] = target.Spec.GetServerConfigByName(volume.Annotations[constants.AnnotationVolumeNodeConf])
		memberZone[member] = k8sutil.PVCAvailabilityZone(volume)
	}

	for member, names := range memberVolumes {
		pod := restoreBinderPod(restore, target, memberClass[member], member, names, memberZone[member])

		if err := r.client.Create(ctx, pod); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

// restoreBinderPod is a pod that does nothing but mount a member's volumes, so they get bound. It's
// owned by the restore, so it goes if the restore is deleted. It uses the member's server class image
// and placement, so it can pull the image and land where the member's own pod would.
func restoreBinderPod(restore *couchbasev2.CouchbaseSnapshotBackupRestore, target *couchbasev2.CouchbaseCluster, config *couchbasev2.ServerConfig, member string, volumeNames []string, zone string) *corev1.Pod {
	gracePeriod := int64(0)
	no := false

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-bind-%s", restore.Name, member),
			Namespace:       restore.Namespace,
			Labels:          map[string]string{snapshotRestoreLabel: restore.Name},
			OwnerReferences: []metav1.OwnerReference{restore.AsOwner()},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &gracePeriod,
			SecurityContext:               binderPodSecurity(target),
			Containers: []corev1.Container{{
				Name:    "bind",
				Image:   target.Spec.ServerClassCouchbaseImage(config),
				Command: []string{"sleep", "3600"},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &no,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
				},
			}},
		},
	}

	// Pull secrets, tolerations and node selector come from the member's server class, so the pod
	// can pull the image and run on the same nodes as the member's own pod.
	if config != nil && config.Pod != nil {
		pod.Spec.ImagePullSecrets = config.Pod.Spec.ImagePullSecrets
		pod.Spec.Tolerations = config.Pod.Spec.Tolerations
		pod.Spec.NodeSelector = maps.Clone(config.Pod.Spec.NodeSelector)
	}

	// Make the volume in the zone the backed up volume was in. A volume made from a snapshot can
	// otherwise end up in any zone, and the member's pod couldn't use it.
	if zone != "" {
		if pod.Spec.NodeSelector == nil {
			pod.Spec.NodeSelector = map[string]string{}
		}

		pod.Spec.NodeSelector[constants.ServerGroupLabel] = zone
	}

	for i, name := range volumeNames {
		volume := fmt.Sprintf("volume-%d", i)

		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name:         volume,
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}},
		})
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: volume, MountPath: "/" + volume})
	}

	return pod
}

// binderPodSecurity uses the cluster's own pod security, so the binder pod is allowed wherever the
// cluster's pods are. Anything it doesn't set gets the safest value, so the pod also passes restricted
// pod security. User 1000 is the couchbase user in the server image.
func binderPodSecurity(target *couchbasev2.CouchbaseCluster) *corev1.PodSecurityContext {
	security := target.Spec.Security.PodSecurityContext
	if security == nil {
		security = target.Spec.SecurityContext
	}

	podSecurity := &corev1.PodSecurityContext{}
	if security != nil {
		podSecurity = security.DeepCopy()
	}

	couchbaseUser := int64(1000)
	yes := true

	if podSecurity.RunAsUser == nil {
		podSecurity.RunAsUser = &couchbaseUser
	}

	if podSecurity.RunAsNonRoot == nil {
		podSecurity.RunAsNonRoot = &yes
	}

	if podSecurity.SeccompProfile == nil {
		podSecurity.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	}

	return podSecurity
}

// readyToStart waits for the target cluster to be woken. The user does that by setting
// spec.hibernate to false, unless they asked the restore to do it with startClusterWhenReady.
// The restore only changes that one field, and only once, since it moves on straight after.
func (r *CouchbaseSnapshotBackupRestoreReconciler) readyToStart(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (reconcile.Result, error) {
	target := &couchbasev2.CouchbaseCluster{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Spec.Target.Name}, target); err != nil {
		return reconcile.Result{}, err
	}

	if target.Spec.Hibernate {
		if !restore.Spec.StartClusterWhenReady {
			changed := meta.SetStatusCondition(&restore.Status.Conditions, metav1.Condition{
				Type:    snapshotRestoreConditionWaitingToStart,
				Status:  metav1.ConditionTrue,
				Reason:  snapshotRestoreReasonWaitingForUser,
				Message: fmt.Sprintf("the volumes are restored, set spec.hibernate to false on cluster %s to start it", target.Name),
			})

			// We check again every few seconds while we wait, so only write when something changed.
			if changed {
				if err := r.client.Status().Update(ctx, restore); err != nil {
					return reconcile.Result{}, err
				}
			}

			return reconcile.Result{RequeueAfter: snapshotRestoreRecheck}, nil
		}

		// Only spec.hibernate changes, so nothing else the user set is touched.
		original := target.DeepCopy()
		target.Spec.Hibernate = false

		if err := r.client.Patch(ctx, target, client.MergeFrom(original)); err != nil {
			return reconcile.Result{}, err
		}
	}

	meta.RemoveStatusCondition(&restore.Status.Conditions, snapshotRestoreConditionWaitingToStart)
	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseRemapping

	return reconcile.Result{}, r.client.Status().Update(ctx, restore)
}

// snapshotRestoreRemapContainer is the init container that runs the node remap on a restored pod.
const snapshotRestoreRemapContainer = constants.CouchbaseContainerName + "-remap"

// remap waits for every restored member's pod to finish its node remap, then removes the mark from
// the volumes, so pods made later don't run the remap step again.
func (r *CouchbaseSnapshotBackupRestoreReconciler) remap(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (reconcile.Result, error) {
	target := &couchbasev2.CouchbaseCluster{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Spec.Target.Name}, target); err != nil {
		return reconcile.Result{}, err
	}

	if k8sutil.UsesNodeHostnames(target) {
		filled, err := r.fillRemapList(ctx, restore, target)
		if err != nil {
			return reconcile.Result{}, err
		}

		if !filled {
			return reconcile.Result{RequeueAfter: snapshotRestoreRecheck}, nil
		}
	}

	volumes := &corev1.PersistentVolumeClaimList{}
	if err := r.client.List(ctx, volumes, client.InNamespace(restore.Namespace), client.MatchingLabels{constants.LabelCluster: restore.Spec.Target.Name}); err != nil {
		return reconcile.Result{}, err
	}

	marked := []*corev1.PersistentVolumeClaim{}
	waiting := false
	checked := map[string]bool{}

	for i := range volumes.Items {
		volume := &volumes.Items[i]

		if _, ok := volume.Annotations[constants.AnnotationSnapshotRestoreRemap]; !ok {
			continue
		}

		marked = append(marked, volume)

		member := volume.Labels[constants.LabelNode]
		if checked[member] {
			continue
		}

		checked[member] = true

		pod := &corev1.Pod{}
		if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: member}, pod); err != nil {
			if !errors.IsNotFound(err) {
				return reconcile.Result{}, err
			}

			// The operator hasn't made the pod yet.
			waiting = true

			continue
		}

		done, failure := remapState(pod)
		if failure != "" {
			return r.fail(ctx, restore, fmt.Sprintf("node remap failed on pod %s: %s", member, failure))
		}

		if !done {
			waiting = true
		}
	}

	if waiting {
		return reconcile.Result{RequeueAfter: snapshotRestoreRecheck}, nil
	}

	for _, volume := range marked {
		original := volume.DeepCopy()
		delete(volume.Annotations, constants.AnnotationSnapshotRestoreRemap)
		delete(volume.Annotations, constants.AnnotationSnapshotRestoreRemapID)

		if err := r.client.Patch(ctx, volume, client.MergeFrom(original)); err != nil {
			return reconcile.Result{}, err
		}
	}

	// The list isn't needed once every node has its new name.
	if k8sutil.UsesNodeHostnames(target) {
		list := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: restore.Namespace, Name: k8sutil.SnapshotRestoreRemapConfigMap(target)}}
		if err := r.client.Delete(ctx, list); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
	}

	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseWaitingForCluster

	return reconcile.Result{}, r.client.Status().Update(ctx, restore)
}

// resetRemapList creates an empty hostname list for the restored pods to wait on.
// The restore owns it, so deleting the restore deletes it too.
func (r *CouchbaseSnapshotBackupRestoreReconciler) resetRemapList(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore, target *couchbasev2.CouchbaseCluster) error {
	list := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            k8sutil.SnapshotRestoreRemapConfigMap(target),
			Namespace:       restore.Namespace,
			Labels:          map[string]string{snapshotRestoreLabel: restore.Name},
			OwnerReferences: []metav1.OwnerReference{restore.AsOwner()},
		},
	}

	err := r.client.Create(ctx, list)
	if !errors.IsAlreadyExists(err) {
		return err
	}

	// One left from an earlier restore of the cluster is emptied and taken over.
	existing := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, client.ObjectKeyFromObject(list), existing); err != nil {
		return err
	}

	existing.Data = nil
	existing.Labels = list.Labels
	existing.OwnerReferences = list.OwnerReferences

	return r.client.Update(ctx, existing)
}

// fillRemapList writes each restored member's old and new hostname into the list.
// The new hostname is the name of the k8s node the pod runs on, so it waits until
// every pod is placed. It returns true once the list is written.
func (r *CouchbaseSnapshotBackupRestoreReconciler) fillRemapList(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore, target *couchbasev2.CouchbaseCluster) (bool, error) {
	list := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: k8sutil.SnapshotRestoreRemapConfigMap(target)}, list); err != nil {
		return false, err
	}

	if list.Data[k8sutil.SnapshotRestoreRemapKey] != "" {
		return true, nil
	}

	run, err := r.getRun(ctx, restore)
	if err != nil {
		return false, err
	}

	members, err := k8sutil.SnapshotRestoreMembers(target, run)
	if err != nil {
		return false, err
	}

	lines := make([]string, 0, len(members))

	for i := range members {
		name, err := restoreMemberName(target, members[i].Name)
		if err != nil {
			return false, err
		}

		pod := &corev1.Pod{}
		if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: name}, pod); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}

			return false, err
		}

		if pod.Spec.NodeName == "" {
			return false, nil
		}

		lines = append(lines, run.Spec.Hostnames[members[i].Name]+" "+pod.Spec.NodeName)
	}

	list.Data = map[string]string{k8sutil.SnapshotRestoreRemapKey: strings.Join(lines, "\n") + "\n"}

	return true, r.client.Update(ctx, list)
}

// remapState says if a pod's node remap has finished, or why it failed.
func remapState(pod *corev1.Pod) (bool, string) {
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name != snapshotRestoreRemapContainer {
			continue
		}

		if exited := status.State.Terminated; exited != nil {
			if exited.ExitCode == 0 {
				return true, ""
			}

			return false, fmt.Sprintf("exit code %d, %s", exited.ExitCode, exited.Reason)
		}

		// k8s restarts a failed init container, so it may be waiting to run again after a failure.
		if exited := status.LastTerminationState.Terminated; exited != nil && exited.ExitCode != 0 {
			return false, fmt.Sprintf("exit code %d, %s", exited.ExitCode, exited.Reason)
		}
	}

	return false, ""
}

// waitForCluster waits for the restored cluster to be up, available and balanced, then marks the
// restore Complete.
func (r *CouchbaseSnapshotBackupRestoreReconciler) waitForCluster(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore) (reconcile.Result, error) {
	target := &couchbasev2.CouchbaseCluster{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: restore.Namespace, Name: restore.Spec.Target.Name}, target); err != nil {
		return reconcile.Result{}, err
	}

	if !target.HasCondition(couchbasev2.ClusterConditionAvailable) || !target.HasCondition(couchbasev2.ClusterConditionBalanced) {
		return reconcile.Result{RequeueAfter: snapshotRestoreRecheck}, nil
	}

	now := metav1.Now()
	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseComplete
	restore.Status.CompletionTime = &now

	return reconcile.Result{}, r.client.Status().Update(ctx, restore)
}

// fail marks the restore as Failed with the reason, and stops working on it.
func (r *CouchbaseSnapshotBackupRestoreReconciler) fail(ctx context.Context, restore *couchbasev2.CouchbaseSnapshotBackupRestore, reason string) (reconcile.Result, error) {
	now := metav1.Now()

	restore.Status.Phase = couchbasev2.CouchbaseSnapshotBackupRestorePhaseFailed
	restore.Status.FailureReason = reason
	restore.Status.CompletionTime = &now

	log.Info("Snapshot restore failed", "restore", types.NamespacedName{Namespace: restore.Namespace, Name: restore.Name}, "reason", reason)

	// The pods that bind volumes aren't needed once the restore has failed, and they'd hold the
	// target's volumes for up to an hour. If removing them fails, it's logged, the restore still
	// fails, and deleting the restore removes them too since it owns them.
	if err := r.deleteBinderPods(ctx, restore); err != nil {
		log.Info("[WARN] Failed to remove the restore's binder pods", "restore", types.NamespacedName{Namespace: restore.Namespace, Name: restore.Name}, "error", err)
	}

	return reconcile.Result{}, r.client.Status().Update(ctx, restore)
}
