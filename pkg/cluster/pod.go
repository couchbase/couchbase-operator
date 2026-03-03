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
	"fmt"
	"reflect"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	if _, err := k8sutil.CreateCouchbasePod(ctx, c.k8s, c.scheduler, c.cluster, m, serverSpec); err != nil {
		return err
	}

	return c.waitForCreatePod(ctx, m)
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
	podCreateTimeout, err := time.ParseDuration(c.config.PodCreateTimeout)
	if err != nil {
		return fmt.Errorf("PodCreateTimeout improperly formatted: %w", errors.NewStackTracedError(err))
	}

	ctx, cancel := context.WithTimeout(c.ctx, podCreateTimeout)
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

// wait with context.
func (c *Cluster) waitForCreatePod(ctx context.Context, member couchbaseutil.Member) error {
	return k8sutil.WaitForPod(ctx, c.k8s.KubeClient, c.cluster.Namespace, member.Name(), member.GetHostPort())
}

func (c *Cluster) waitForDeletePod(podName string, timeout int64) error {
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	return k8sutil.WaitForDeletePod(ctx, c.k8s.KubeClient, c.cluster.Namespace, podName)
}

func (c *Cluster) isPodRecoverable(m couchbaseutil.Member) bool {
	config := c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		return false
	}

	if err := k8sutil.IsPodRecoverable(c.k8s, *config, m); err != nil {
		log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", m.Name(), "reason", err)
		return false
	}

	return true
}

// reconcilePods updates pod metadata only, this is mutable.  All other changes are done
// with the upgrade mechanism, as these are immutable and need a replacement.  The assumption
// here is that topology changes, e.g upgrades, have been detected and done before this call.
// If that dodesn't hold, then we risk updating the pod spec annotation and ignoring changes.
func (c *Cluster) reconcilePods() error {
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

		image := c.cluster.Spec.CouchbaseImage()

		if pvcState != nil && pvcState.Image != "" {
			image = pvcState.Image
		}

		requested, err := k8sutil.CreateCouchbasePodSpec(c.k8s, member, c.cluster, *serverClass, serverGroup, pvcState, image)
		if err != nil {
			return err
		}

		// Preserve mutable metadata as this may be added and/or required by other tooling, e.g. Istio. Only enforce
		// what we are told to enforce.
		k8sutil.MaintainMutablePodConfiguration(actual, requested)

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
	image := c.cluster.Spec.CouchbaseImage()

	requested, err := k8sutil.CreateCouchbasePodSpec(c.k8s, member, c.cluster, *serverClass, serverGroup, pvcState, image)
	if err != nil {
		return nil, err
	}

	return requested, nil
}
