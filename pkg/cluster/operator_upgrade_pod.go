/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"context"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podUpgradeFunc is a function that applies an upgrade to a pod resource.
type podUpgradeFunc func(*Cluster, *corev1.Pod) error

// podUpgradeAction is an action to perform on a pod resource when its version
// is within the specified upgrade range.
type podUpgradeAction struct {
	upgradeRange upgradeRange
	action       podUpgradeFunc
}

// podUpgradeActionList is an ordered list of actions to try performing on a
// pod resource.
type podUpgradeActionList []podUpgradeAction

// podUpgradableResource implements the upgradableResource interface for pods.
type podUpgradableResource struct {
	// cluster is a reference to the cluster for client and namespace access.
	cluster *Cluster
	// pods is a local cache of fetched resource items.
	pods []*corev1.Pod
	// actions is the list of possible upgrade actions to perform on a resource item.
	actions podUpgradeActionList
}

func newPodUpgradableResource(c *Cluster) upgradableResource {
	return &podUpgradableResource{
		cluster: c,
		actions: podUpgradeActionList{
			{upgradeRange: upgradeRange{"2.0.0", "2.1.0"}, action: upgradePodFrom020000To020100},
		},
	}
}

func (r *podUpgradableResource) kind() string {
	return "pod"
}

func (r *podUpgradableResource) name(item int) string {
	return r.pods[item].Name
}

func (r *podUpgradableResource) fetch() error {
	r.pods = r.cluster.getClusterPods()
	return nil
}

func (r *podUpgradableResource) lenItems() int {
	return len(r.pods)
}

func (r *podUpgradableResource) itemVersion(item int) string {
	version := "0.0.0"

	if v, ok := r.pods[item].Annotations[constants.ResourceVersionAnnotation]; ok {
		version = v
	}

	return version
}

func (r *podUpgradableResource) lenActions() int {
	return len(r.actions)
}

func (r *podUpgradableResource) actionVersionRange(action int) upgradeRange {
	return r.actions[action].upgradeRange
}

func (r *podUpgradableResource) perform(item, action int) error {
	pod := r.pods[item]

	upgrade := r.actions[action].action
	if err := upgrade(r.cluster, pod); err != nil {
		return err
	}

	return nil
}

func (r *podUpgradableResource) commit(item int) error {
	pod := r.pods[item]
	if _, err := r.cluster.k8s.KubeClient.CoreV1().Pods(r.cluster.cluster.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return nil
}

// upgradePodFrom020000To020100 performs pod upgrades to 2.1.0 from 2.0.0.
// * The couchbase_server label was added and needs to be present on upgrade for the peer service.
func upgradePodFrom020000To020100(cluster *Cluster, pod *corev1.Pod) error {
	// Don't bother with updating the version, it will all be upgraded as it is...
	pod.Labels[constants.LabelServer] = "true"

	return nil
}
