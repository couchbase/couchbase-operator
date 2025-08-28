package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
// reprovisioning them.
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
			log.Info("Unable to remove failed member", "cluster", c.namespacedName(), "error", rerr)
		}
	}()

	if c.isSGReschedulingEnabled() {
		return c.createPodWithRescheduling(ctx, m, serverSpec)
	}

	if _, err := k8sutil.CreateCouchbasePod(ctx, c.k8s, c.scheduler, c.cluster, m, serverSpec, c.config.GetPodReadinessConfig()); err != nil {
		return err
	}

	return c.waitForCreatePod(ctx, m)
}

type failedSchedulingServerGroupsTracker map[string]int

func (c *Cluster) getServerGroupsToAvoid() ([]string, error) {
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

		log.Info("Avoiding server groups", "cluster", c.namespacedName(), "serverGroups", serverGroupsToAvoid)
	}

	pod, err := k8sutil.CreateCouchbasePod(ctx, c.k8s, c.scheduler, c.cluster, m, serverSpec, c.config.GetPodReadinessConfig())
	if err != nil {
		return err
	}

	// Pod failed to schedule, add server group to avoid list.
	if err := c.waitForCreatePod(ctx, m); err != nil {
		serverGroup := pod.Spec.NodeSelector[constants.ServerGroupLabel]
		if err := c.addFailedSchedulingServerGroups(serverGroup); err != nil {
			log.Error(err, "Failed to add server group to avoid list", "cluster", c.namespacedName(), "serverGroup", serverGroup)
		}

		return err
	}

	return nil
}

// Remove Pod and any volumes associated with pod if requested
// or volumes are associated with default claim.
func (c *Cluster) removePod(name string, removeVolumes bool) error {
	if err := k8sutil.DeleteCouchbasePod(c.k8s, c.cluster.Namespace, name, c.config.GetDeleteOptions(), removeVolumes); err != nil {
		log.Error(err, "Pod deletion failed", "cluster", c.namespacedName())
		return err
	}

	log.Info("Pod deleted", "cluster", c.namespacedName(), "name", name)

	return nil
}

// Delete pod and create with same name.
// Persisted members will reuse volume mounts.
func (c *Cluster) recreatePod(m couchbaseutil.Member) error {
	config := c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		return fmt.Errorf("%w: config %s for pod does not exist", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), m.Config())
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

	// To get here the pod would need to be initialized and clustered, so this is
	// safe.
	return k8sutil.SetPodInitialized(c.k8s, m.Name())
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

// wait with context.
func (c *Cluster) waitForCreatePod(ctx context.Context, member couchbaseutil.Member) error {
	return k8sutil.WaitForPod(ctx, c.k8s.KubeClient, c.cluster.Namespace, member.Name(), member.GetHostPort())
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

	if err := k8sutil.CheckIfPodIsRecoverable(c.k8s, *config, m, targetSemVersion, checkReschedulability); err != nil {
		return false, err
	}

	return true, nil
}

// isPodRecoverable checks if a pod can be recovered after a failure.
func (c *Cluster) isPodRecoverable(m couchbaseutil.Member) bool {
	recoverable, err := c.checkPodRecoverability(m, false)
	if !recoverable {
		if err != nil {
			log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", m.Name(), "reason", err)
		} else {
			log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", m.Name())
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
			log.Info("Pod unschedulable", "cluster", c.namespacedName(), "name", m.Name(), "reason", err)
		} else {
			log.Info("Pod unschedulable", "cluster", c.namespacedName(), "name", m.Name())
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
			if group, ok := actual.Spec.NodeSelector[constants.ServerGroupLabel]; ok {
				serverGroup = group
			}
		}

		image := c.cluster.Spec.ServerClassCouchbaseImage(serverClass)

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
			if group, ok := actual.Spec.NodeSelector[constants.ServerGroupLabel]; ok {
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

// Updates the internal digest map, based on running pods.
// This is mostly used for when operator is recovering from a restart
// and has lost it's internal map.
// We update the image digest map early in reconciliation because it's
// used in c.IsAtLeastVersion().
func (c *Cluster) reconcilePodServerVersions() error {
	couchbaseImageToVersion := map[string]string{}
	couchbaseImageToVersion[c.cluster.Spec.CouchbaseImage()] = ""

	log.V(2).Info("requesting server version for image", "image", c.cluster.Spec.CouchbaseImage(), "cluster", c.cluster.Name)

	for _, member := range c.callableMembers {
		info := &couchbaseutil.PoolsInfo{}

		if err := couchbaseutil.GetPools(info).RetryFor(time.Minute).On(c.api, member); err != nil {
			return err
		}

		pod, found := c.k8s.Pods.Get(member.Name())
		if !found {
			continue
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

		if newVersion, updated := couchbaseutil.UpdateImageDigestMap(image, cbversion); newVersion != "" && updated {
			log.V(2).Info("found server version", "version", cbversion, "image", image, "cluster", c.cluster.Name)

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
