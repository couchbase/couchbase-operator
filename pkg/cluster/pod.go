/*
Copyright 2021-Present Couchbase, Inc.

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
	"reflect"
	"strconv"
	"time"

	goerrors "errors"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServerGroupAvoidDelimiter is used to separate server groups in the
// failed scheduling server groups list stored in persistent state.
const ServerGroupAvoidDelimiter = ","

// createPod is used to create EVERY Couchbase server pod, either provisioning or
// reprovisioning them.  Pod creation is non-blocking: the pod is flagged with a
// PendingInitializationCondition and initialization will be completed asynchronously
// by updatePendingInitializationConditions() in the next reconciliation cycle.
func (c *Cluster) createPod(ctx context.Context, m couchbaseutil.Member, serverSpec couchbasev2.ServerConfig, deleteVolumes bool) (err error) {
	// In the event of an error, dump out all information we know about
	// and raise an event.  Delete all resources
	defer func() {
		if err == nil {
			return
		}

		c.logFailedMember("Member creation failed", m.Name())
		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(m.Name(), c.cluster))

		if rerr := c.removePod(m.Name(), deleteVolumes); rerr != nil {
			c.log.Info("Unable to remove failed member", "cluster", c.namespacedName(), "error", rerr)
		}
	}()

	if c.isSGReschedulingEnabled() {
		return c.createPodWithRescheduling(ctx, m, serverSpec)
	}

	pod, err := k8sutil.CreateCouchbasePod(ctx, c.k8s, c.scheduler, c.cluster, m, serverSpec, c.config.GetPodReadinessConfig())
	if err != nil {
		return err
	}

	if err := k8sutil.FlagPodPendingInitialization(c.k8s, pod, "pod created, waiting for readiness"); err != nil {
		c.log.Error(err, "Failed to flag pod pending initialization", "cluster", c.namespacedName(), "pod", m.Name())
		return err
	}

	c.log.V(1).Info("Pod flagged for async initialization", "cluster", c.namespacedName(), "pod", pod.Name)
	return nil
}

type failedSchedulingServerGroupsTracker map[string]int

func (c *Cluster) getServerGroupsToAvoid() ([]string, error) {
	// Protect read to ensure we see the latest writes from concurrent
	// pod creation goroutines that may be updating the tracker.
	c.failedGroupsMu.RLock()
	defer c.failedGroupsMu.RUnlock()

	failedGroupsTracker, err := c.getFailedServerGroupsTracker()
	if err != nil {
		return nil, err
	}

	// Filter and return only server groups that have failed more than twice
	var result []string

	for group, count := range failedGroupsTracker {
		if count > 1 {
			result = append(result, group)
		}
	}

	return result, nil
}

func (c *Cluster) getFailedServerGroupsTracker() (failedSchedulingServerGroupsTracker, error) {
	failedGroupsTracker := failedSchedulingServerGroupsTracker{}

	trackerString, err := c.state.Get(persistence.FailedSchedulingServerGroupsTracker)
	if err != nil {
		// If the key doesn't exist, just return an empty tracker
		if goerrors.Is(err, persistence.ErrKeyError) {
			return failedGroupsTracker, nil
		}

		return nil, err
	}

	if err := json.Unmarshal([]byte(trackerString), &failedGroupsTracker); err != nil {
		return nil, err
	}

	return failedGroupsTracker, nil
}

func (c *Cluster) createPodWithRescheduling(ctx context.Context, m couchbaseutil.Member, serverSpec couchbasev2.ServerConfig) error {
	if serverGroupsToAvoid, err := c.getServerGroupsToAvoid(); err == nil && len(serverGroupsToAvoid) > 0 {
		c.scheduler.AvoidGroups(serverGroupsToAvoid...)

		c.log.Info("Avoiding server groups", "cluster", c.namespacedName(), "serverGroups", serverGroupsToAvoid)
	}

	pod, err := k8sutil.CreateCouchbasePod(ctx, c.k8s, c.scheduler, c.cluster, m, serverSpec, c.config.GetPodReadinessConfig())
	if err != nil {
		return err
	}

	if err := k8sutil.FlagPodPendingInitialization(c.k8s, pod, "pod created with rescheduling, waiting for readiness"); err != nil {
		c.log.Error(err, "Failed to flag pod pending initialization", "cluster", c.namespacedName(), "pod", m.Name())
		return err
	}

	c.log.V(1).Info("Pod flagged for async initialization", "cluster", c.namespacedName(), "pod", pod.Name)
	return nil
}

// Remove Pod and any volumes associated with pod if requested
// or volumes are associated with default claim.
func (c *Cluster) removePod(name string, removeVolumes bool) error {
	if err := k8sutil.DeleteCouchbasePod(c.k8s, c.cluster.Namespace, name, c.config.GetDeleteOptions(), removeVolumes); err != nil {
		c.log.Error(err, "Pod deletion failed", "cluster", c.namespacedName())
		return err
	}

	c.log.Info("Pod deleted", "cluster", c.namespacedName(), "name", name)

	return nil
}

// Delete pod and create with same name.
// Persisted members will reuse volume mounts.
// When recovery is true, the recovery attempt counter on the member's PVCs is
// incremented before recreation and reset on success.
func (c *Cluster) recreatePod(m couchbaseutil.Member, recovery bool) error {
	config := c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		return fmt.Errorf("%w: config %s for pod does not exist", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), m.Config())
	}

	if recovery {
		if err := c.incrementRecoveryAttempts(m.Name()); err != nil {
			c.log.Error(err, "Failed to increment recovery attempts", "cluster", c.namespacedName(), "name", m.Name())
		}
	}

	if err := k8sutil.DeletePod(c.k8s, c.cluster.Namespace, m.Name(), c.config.GetDeleteOptions()); err != nil {
		return err
	}

	if err := c.waitForDeletePod(m.Name(), 120); err != nil {
		return err
	}

	// The pod creation timeout is global across this operation e.g. PVCs, pods, the lot.
	ctx, cancel := context.WithTimeout(c.ctx, c.config.PodCreateTimeout)
	defer cancel()

	// Don't delete the volumes here, we need them to recover from, and they
	// contain precious customer data.
	if err := c.createPod(ctx, m, *config, false); err != nil {
		return err
	}

	// createPod now flags the pod with PendingInitializationCondition.
	// The pod will be initialized asynchronously. SetPodInitialized will
	// be called after the pending initialization is cleared.
	c.log.V(1).Info("Pod recreated with pending initialization", "cluster", c.namespacedName(), "pod", m.Name())

	// To get here the pod would need to be initialized and clustered, so this is
	// safe.
	if err := k8sutil.SetPodInitialized(c.k8s, m.Name()); err != nil {
		return err
	}

	if recovery {
		if err := c.resetRecoveryAttempts(m.Name()); err != nil {
			c.log.Error(err, "Failed to reset recovery attempts", "cluster", c.namespacedName(), "name", m.Name())
		}
	}

	return nil
}

// waitForPodAdded waits for a pod to be added to the cluster.
// The pod will be inactive until rebalanced back in to the cluster.
func (c *Cluster) waitForPodAdded(ctx context.Context, member couchbaseutil.Member) error {
	callback := func() error {
		nodeInfo := couchbaseutil.NodeInfo{}

		if err := couchbaseutil.GetNodesSelf(&nodeInfo).On(c.api, member); err != nil {
			return err
		}

		if nodeInfo.Membership == "inactiveAdded" || nodeInfo.Membership == "active" {
			return nil
		}

		return errors.ErrNodeNotAdded
	}

	return retryutil.Retry(ctx, time.Second, callback)
}

func (c *Cluster) waitForDeletePod(podName string, timeout int64) error {
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	return k8sutil.WaitForDeletePod(ctx, c.k8s.KubeClient, c.cluster.Namespace, podName)
}

// checkPodRecoverability checks if a pod can be recovered or rescheduled based on the checkReschedulability parameter.
// If checkReschedulability is true, it will also verify that the pod can be rescheduled (e.g., no LPVs).
// Returns true if the pod is recoverable/reschedulable, false otherwise.
func (c *Cluster) checkPodRecoverability(m couchbaseutil.Member, checkReschedulability bool) (bool, error) {
	config := c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		return false, nil
	}

	targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.ServerClassCouchbaseImage(config))
	if err != nil {
		return false, err
	}

	targetSemVersion, err := couchbaseutil.NewVersion(targetVersion)
	if err != nil {
		return false, err
	}

	restrictedGroups := c.getRestrictedServerGroupsForConfig(config)
	restrictedZone := c.getRestrictedZoneForConfig(config)

	if err := k8sutil.CheckIfPodIsRecoverable(c.k8s, *config, m, targetSemVersion, checkReschedulability, restrictedGroups, restrictedZone); err != nil {
		return false, err
	}

	return true, nil
}

// getRestrictedZoneForConfig returns the availability zone a server class is pinned to, ONLY when a
// server-group label override is active (placement groups). Under an override the server group is a
// placement group, not an AZ, so the volume's recovery constraint is the zone: a PVC in a different
// AZ cannot be recovered in place, whereas a PVC whose only change is its placement group can. The
// zone is the per-class pod-template zone selector (required under an override; see the override
// validator). Empty when no override is set.
func (c *Cluster) getRestrictedZoneForConfig(config *couchbasev2.ServerConfig) string {
	if c.cluster.ServerGroupLabel() == constants.ServerGroupLabel {
		return ""
	}

	if config.Pod != nil && config.Pod.Spec.NodeSelector != nil {
		return config.Pod.Spec.NodeSelector[constants.ServerGroupLabel]
	}

	return ""
}

// getRestrictedServerGroupsForConfig returns the server groups that are restricted for a given server config.
// If this method returns an empty list, then any server groups are allowed.
// If this method returns a non-empty list, then only the server groups in the list are allowed, as determined by the cluster spec.
func (c *Cluster) getRestrictedServerGroupsForConfig(config *couchbasev2.ServerConfig) []string {
	restrictedGroups, _ := scheduler.GetServerGroupsForClass(c.cluster, config)

	if len(restrictedGroups) == 0 && config.Pod != nil {
		if group, ok := config.Pod.Spec.NodeSelector[c.cluster.ServerGroupLabel()]; ok {
			restrictedGroups = append(restrictedGroups, group)
		}
	}

	return restrictedGroups
	// return nil
}

// isPodRecoverable checks if a pod can be recovered after a failure.
func (c *Cluster) isPodRecoverable(m couchbaseutil.Member) bool {
	recoverable, err := c.checkPodRecoverability(m, false)
	if !recoverable {
		if err != nil {
			c.log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", m.Name(), "reason", err)
		} else {
			c.log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", m.Name())
		}
	}

	return recoverable
}

// isPodReschedulable checks if a pod can be rescheduled to a different node.
// This includes all recoverability checks plus additional checks for rescheduling constraints
// such as Local Persistent Volumes.
func (c *Cluster) isPodReschedulable(m couchbaseutil.Member) bool {
	recoverable, err := c.checkPodRecoverability(m, true)
	if !recoverable {
		if err != nil {
			c.log.Info("Pod unschedulable", "cluster", c.namespacedName(), "name", m.Name(), "reason", err)
		} else {
			c.log.Info("Pod unschedulable", "cluster", c.namespacedName(), "name", m.Name())
		}
	}

	return recoverable
}

// reconcilePods updates pod metadata only, this is mutable.  All other changes are done
// with the upgrade mechanism, as these are immutable and need a replacement.  The assumption
// here is that topology changes, e.g upgrades, have been detected and done before this call.
// If that dodesn't hold, then we risk updating the pod spec annotation and ignoring changes.
func (c *Cluster) reconcilePods() error {
	var memoryUnderManagement resource.Quantity

	var cpuUnderManagement resource.Quantity

	for name, member := range c.members {
		actual, exists := c.k8s.Pods.Get(name)
		if !exists {
			continue
		}

		// Get what the member should look like.
		serverClass := c.cluster.Spec.GetServerConfigByName(member.Config())
		if serverClass == nil {
			continue
		}

		pvcState, err := k8sutil.GetPodVolumes(c.k8s, member, c.cluster, *serverClass)
		if err != nil {
			return err
		}

		serverGroup := ""

		// Checks existing NodeSelectors on the pod
		if actual.Spec.NodeSelector != nil {
			if group, ok := actual.Spec.NodeSelector[c.cluster.ServerGroupLabel()]; ok {
				serverGroup = group
			}
		}

		image := extractCouchbaseImage(actual)
		if image == "" {
			image = c.cluster.Spec.ServerClassCouchbaseImage(serverClass)
		}

		if pvcState != nil && pvcState.Image != "" {
			image = pvcState.Image
		}

		requested, err := k8sutil.CreateCouchbasePodSpec(member, c.cluster, *serverClass, serverGroup, pvcState, image, c.config.GetPodReadinessConfig())
		if err != nil {
			return err
		}

		// Preserve mutable metadata as this may be added and/or required by other tooling, e.g. Istio. Only enforce
		// what we are told to enforce.
		k8sutil.MaintainMutablePodConfiguration(actual, requested)

		memoryUnderManagement.Add(k8sutil.GetResourceRequestQuantity(actual, v1.ResourceMemory))
		cpuUnderManagement.Add(k8sutil.GetResourceRequestQuantity(actual, v1.ResourceCPU))

		if reflect.DeepEqual(actual.Labels, requested.Labels) && reflect.DeepEqual(actual.Annotations, requested.Annotations) {
			continue
		}

		// Don't modify the cache!!
		updated := actual.DeepCopy()
		updated.Labels = requested.Labels
		updated.Annotations = requested.Annotations

		if _, err := c.k8s.KubeClient.CoreV1().Pods(c.cluster.Namespace).Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	metrics.MemoryUnderManagementBytesMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Set(memoryUnderManagement.AsApproximateFloat64())
	metrics.CPUUnderManagementMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Set(cpuUnderManagement.AsApproximateFloat64())

	return nil
}

func (c *Cluster) regeneratePod(member couchbaseutil.Member, actual *v1.Pod, serverClass *couchbasev2.ServerConfig, pvcState *k8sutil.PersistentVolumeClaimState, moves []scheduler.Move) (*v1.Pod, error) {
	// For server groups, if off, then leave it blank.  If it's enabled, default to
	// what was there originally, unless overridden by a resceduling move.
	serverGroup := ""

	if c.cluster.Spec.ServerGroupsEnabled() {
		// Keep the existing selector if one exists.
		if actual.Spec.NodeSelector != nil {
			if group, ok := actual.Spec.NodeSelector[c.cluster.ServerGroupLabel()]; ok {
				serverGroup = group
			}
		}

		// Check the rescheduling information for any overrides.
		for _, move := range moves {
			if move.Name == member.Name() {
				serverGroup = move.To

				break
			}
		}
	}

	// Regeneration is used for upgrades, so the CRD is the source of truth here.
	image := c.cluster.Spec.ServerClassCouchbaseImage(serverClass)

	requested, err := k8sutil.CreateCouchbasePodSpec(member, c.cluster, *serverClass, serverGroup, pvcState, image, c.config.GetPodReadinessConfig())
	if err != nil {
		return nil, err
	}

	return requested, nil
}

// Allows patching a members version AFTER creation.
// This involves not only updating the member, but the Pod
// and PVC as well.
func (c *Cluster) updateMemberVersion(member couchbaseutil.Member, version string) error {
	if version == "" { // won't upgrade to empty version
		return nil
	}

	if member.Version() == version {
		return nil
	}

	member.SetVersion(version)

	pod, found := c.k8s.Pods.Get(member.Name())
	if !found {
		return fmt.Errorf("failed to find pod by name %s %w", member.Name(), errors.ErrResourceRequired)
	}

	if pod.Annotations[constants.CouchbaseVersionAnnotationKey] == version {
		return nil
	}

	pod.Annotations[constants.CouchbaseVersionAnnotationKey] = version

	if _, err := c.k8s.KubeClient.CoreV1().Pods(c.cluster.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		return err
	}

	for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
		if name, ok := pvc.Labels[constants.LabelNode]; ok && name == member.Name() {
			// update the annotation
			if pvc.Annotations[constants.CouchbaseVersionAnnotationKey] == version {
				continue
			}

			pvc.Annotations[constants.CouchbaseVersionAnnotationKey] = version

			if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(context.Background(),
				pvc, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
	}

	return nil
}

// pvcImageToStamp returns the image to write onto a PVC's image annotation, or
// "" when there is nothing to do. The image annotation was added after the
// version annotation, so PVCs created by older operators carry a version but no
// image, recovering such a member from its PVC would give it an empty image and
// an invalid pod. The image is taken from the member's running pod, not the
// spec, so it stays correct during mixed mode PVPC upgrades where a member can still
// run the old image while the spec already points at the new one.
func pvcImageToStamp(pvc *v1.PersistentVolumeClaim, pod *v1.Pod) string {
	// Already has an image, nothing to do.
	if _, ok := pvc.Annotations[constants.PVCImageAnnotation]; ok {
		return ""
	}

	// No running pod to read the real image from, so try again on a later reconcile.
	if pod == nil || pod.DeletionTimestamp != nil {
		return ""
	}

	return extractCouchbaseImage(pod)
}

// reconcilePVCImages adds the image annotation to PVCs that don't have it, using
// the image from each member's running pod. Old PVCs made before this annotation
// existed would otherwise recover with an empty image and an invalid pod.
// A PVC is only fixed while its pod is running. If all pods are lost before that
// happens, those PVCs still recover with an empty image, but they get fixed on any
// later reconcile that sees a running pod.
func (c *Cluster) reconcilePVCImages() error {
	for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
		// log volumes are never recovered into members, so don't bother.
		if k8sutil.IsLogPVC(pvc) {
			continue
		}

		name, ok := pvc.Labels[constants.LabelNode]
		if !ok {
			continue
		}

		pod, _ := c.k8s.Pods.Get(name)

		image := pvcImageToStamp(pvc, pod)
		if image == "" {
			continue
		}

		// List() hands back pointers into the shared cache, so copy before mutating.
		updated := pvc.DeepCopy()
		if updated.Annotations == nil {
			updated.Annotations = map[string]string{}
		}
		updated.Annotations[constants.PVCImageAnnotation] = image

		if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(context.Background(),
			updated, metav1.UpdateOptions{}); err != nil {
			return err
		}

		c.log.Info("stamped missing image annotation on PVC", "pvc", pvc.Name, "member", name, "image", image)
	}

	return nil
}

// Updates the internal digest map, based on running pods.
// This is mostly used for when operator is recovering from a restart
// and has lost it's internal map.
// We update the image digest map early in reconciliation because it's
// used in c.IsAtLeastVersion().
func (c *Cluster) reconcilePodServerVersions() error {
	couchbaseImageToVersion := map[string]string{}
	couchbaseImageToVersion[c.cluster.Spec.CouchbaseImage()] = ""

	c.log.V(2).Info("requesting server version for image", "image", c.cluster.Spec.CouchbaseImage(), "cluster", c.namespacedName())

	for _, member := range c.callableMembers {
		pod, found := c.k8s.Pods.Get(member.Name())
		if !found {
			continue
		}

		if pod.DeletionTimestamp != nil {
			continue
		}

		info := &couchbaseutil.PoolsInfo{}

		if err := couchbaseutil.GetPools(info).RetryFor(time.Minute).On(c.api, member); err != nil {
			return err
		}

		config := c.cluster.Spec.GetServerConfigByName(member.Config())
		image := c.cluster.Spec.ServerClassCouchbaseImage(config)

		for _, container := range pod.Spec.Containers {
			if container.Image == image {
				if version, exists := couchbaseImageToVersion[image]; !exists || version == "" {
					couchbaseImageToVersion[image] = info.Version
				}
			}
		}
	}

	for image, cbversion := range couchbaseImageToVersion {
		version := couchbaseutil.GetVersionTag(image)
		// check if we know about this image.
		if _, ok := constants.ImageDigests[version]; ok {
			continue
		}

		if newVersion, updated := couchbaseutil.UpdateImageDigestMap(image, cbversion, c.log); newVersion != "" && updated {
			c.log.V(2).Info("found server version", "version", cbversion, "image", image, "cluster", c.namespacedName())

			err := c.updatePersistenceVersion(newVersion)

			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Only update persistence version if
// we aren't upgrading, since the status is used
// for rollback recovery.
func (c *Cluster) updatePersistenceVersion(version string) error {
	upgrading, _ := c.isUpgrading()
	if upgrading {
		return nil
	}

	return c.state.Update(persistence.Version, version)
}

// getRecoveryAttempts returns the number of recovery attempts tracked on a member's PVCs.
// If multiple PVCs exist for the member, returns the maximum count to handle cases where
// PVC updates may have partially failed, causing counts to diverge.
func (c *Cluster) getRecoveryAttempts(memberName string) int {
	maxAttempts := 0

	for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
		if name, ok := pvc.Labels[constants.LabelNode]; ok && name == memberName {
			if pvc.Annotations == nil {
				continue
			}

			if attemptsStr, ok := pvc.Annotations[constants.PodRecoveryAttemptsAnnotation]; ok {
				if attempts, err := strconv.Atoi(attemptsStr); err == nil {
					if attempts > maxAttempts {
						maxAttempts = attempts
					}
				}
			}
		}
	}

	return maxAttempts
}

// incrementRecoveryAttempts increments the recovery attempt counter on all PVCs belonging to a member.
func (c *Cluster) incrementRecoveryAttempts(memberName string) error {
	for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
		if name, ok := pvc.Labels[constants.LabelNode]; ok && name == memberName {
			if pvc.Annotations == nil {
				pvc.Annotations = map[string]string{}
			}

			attempts := 0
			if attemptsStr, ok := pvc.Annotations[constants.PodRecoveryAttemptsAnnotation]; ok {
				if parsed, err := strconv.Atoi(attemptsStr); err == nil {
					attempts = parsed
				}
			}

			pvc.Annotations[constants.PodRecoveryAttemptsAnnotation] = strconv.Itoa(attempts + 1)

			if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(
				context.Background(), pvc, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
	}

	return nil
}

// resetRecoveryAttempts resets the recovery attempt counter on all PVCs belonging to a member.
func (c *Cluster) resetRecoveryAttempts(memberName string) error {
	for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
		if name, ok := pvc.Labels[constants.LabelNode]; ok && name == memberName {
			if pvc.Annotations == nil {
				continue
			}

			if _, ok := pvc.Annotations[constants.PodRecoveryAttemptsAnnotation]; !ok {
				continue
			}

			delete(pvc.Annotations, constants.PodRecoveryAttemptsAnnotation)

			if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(
				context.Background(), pvc, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
	}

	return nil
}

// hasExceededRecoveryMaxRetries checks if the member has exceeded the configured maximum number of recovery retries.
// Returns false if max retries is 0 (infinite).
func (c *Cluster) hasExceededRecoveryMaxRetries(memberName string) bool {
	maxRetries := c.config.PodRecoveryMaxRetries
	if maxRetries == 0 {
		return false
	}

	return c.getRecoveryAttempts(memberName) >= maxRetries
}
