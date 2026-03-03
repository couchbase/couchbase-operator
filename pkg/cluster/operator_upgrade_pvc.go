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
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	corev1 "k8s.io/api/core/v1"
)

// pvcUpgradeFunc is a function that applies an upgrade to a pvc resource.
type pvcUpgradeFunc func(*Cluster, *corev1.PersistentVolumeClaim) error

// pvcUpgradeAction is an action to perform on a pvc resource when its version
// is within the specified upgrade range.
type pvcUpgradeAction struct {
	upgradeRange upgradeRange
	action       pvcUpgradeFunc
}

// pvcUpgradeActionList is an ordered list of actions to try performing on a
// pvc resource.
type pvcUpgradeActionList []pvcUpgradeAction

// pvcUpgradableResource implments the upgradableResource interface for pvcs.
type pvcUpgradableResource struct {
	// cluster is a reference to the cluster for client and namespace access.
	cluster *Cluster
	// pvcs is a local cache of fetched resource items.
	pvcs []*corev1.PersistentVolumeClaim
	// actions is the list of possible upgrade actions to perform on a resource item.
	actions pvcUpgradeActionList
}

func newPVCUpgradableResource(c *Cluster) upgradableResource {
	return &pvcUpgradableResource{
		cluster: c,
		actions: pvcUpgradeActionList{
			{upgradeRange: upgradeRange{"0.0.0", "1.2.0"}, action: upgradePVCFrom000000To010200},
		},
	}
}

func (r *pvcUpgradableResource) kind() string {
	return "pvc"
}

func (r *pvcUpgradableResource) name(item int) string {
	return r.pvcs[item].Name
}

func (r *pvcUpgradableResource) fetch() error {
	r.pvcs = r.cluster.k8s.PersistentVolumeClaims.List()
	return nil
}

func (r *pvcUpgradableResource) lenItems() int {
	return len(r.pvcs)
}

func (r *pvcUpgradableResource) itemVersion(item int) string {
	version := "0.0.0"

	if v, ok := r.pvcs[item].Annotations[constants.ResourceVersionAnnotation]; ok {
		version = v
	}

	return version
}

func (r *pvcUpgradableResource) lenActions() int {
	return len(r.actions)
}

func (r *pvcUpgradableResource) actionVersionRange(action int) upgradeRange {
	return r.actions[action].upgradeRange
}

func (r *pvcUpgradableResource) perform(item, action int) error {
	pvc := r.pvcs[item]

	upgrade := r.actions[action].action
	if err := upgrade(r.cluster, pvc); err != nil {
		return err
	}

	return nil
}

func (r *pvcUpgradableResource) commit(item int) error {
	pvc := r.pvcs[item]
	if _, err := r.cluster.k8s.KubeClient.CoreV1().PersistentVolumeClaims(r.cluster.cluster.Namespace).Update(pvc); err != nil {
		return err
	}

	return nil
}

// upgradePVCFrom000000To010200 performs pvc upgrades to 1.2.0 from all prior versions.
// * The "server.couchbase.com/version" annotation was added.
// * The "failure-domain.beta.kubernetes.io/zone" annotation was added.
func upgradePVCFrom000000To010200(cluster *Cluster, pvc *corev1.PersistentVolumeClaim) error {
	// Update the version annotation
	pvc.Annotations[constants.ResourceVersionAnnotation] = "1.2.0"

	// Add the server version annotation from the cluster's current version.
	pvc.Annotations[constants.CouchbaseVersionAnnotationKey] = cluster.cluster.Status.CurrentVersion

	// Attempt to add in the PVC zone from the associated Pod.
	// TODO: Pod may not be alive by the time we run the upgrade actions

	return nil
}
