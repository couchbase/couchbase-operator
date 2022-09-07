package cluster

import (
	"context"
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/version"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// pvcUpgradableResource implements the upgradableResource interface for pvcs.
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
			{upgradeRange: upgradeRange{"0.0.0", "2.3.0"}, action: upgradePersistentVolumeClaimFrom000000To020300},
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
	if _, err := r.cluster.k8s.KubeClient.CoreV1().PersistentVolumeClaims(r.cluster.cluster.Namespace).Update(context.Background(), pvc, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return nil
}

// upgradePersistentVolumeClaimFrom000000To020300 performs pvc upgrades to 2.3.0 from any version.
//   - The server group label was changed from failure-domain.beta.kubernetes.io/zone
//     to topology.kubernetes.io/zone.
//   - It got a new image label.
func upgradePersistentVolumeClaimFrom000000To020300(cluster *Cluster, pvc *corev1.PersistentVolumeClaim) error {
	// If the PVC doesn't have a server config annotation then
	// there isn't any need to add the server image annotation
	// since the additional request for a node label will fail.
	if _, ok := pvc.Annotations[constants.AnnotationVolumeNodeConf]; !ok {
		return nil
	}

	pvc.Annotations[constants.ResourceVersionAnnotation] = version.Version

	if zone, ok := pvc.Annotations[corev1.LabelFailureDomainBetaZone]; ok {
		pvc.Annotations[corev1.LabelTopologyZone] = zone

		delete(pvc.Annotations, corev1.LabelFailureDomainBetaZone)
	}

	pod, ok := cluster.k8s.Pods.Get(pvc.Labels[constants.LabelNode])
	if !ok {
		return fmt.Errorf("%w: pod for volume missing", errors.NewStackTracedError(errors.ErrResourceRequired))
	}

	container, err := k8sutil.GetCouchbaseContainer(pod)
	if err != nil {
		return err
	}

	pvc.Annotations[constants.PVCImageAnnotation] = container.Image

	return nil
}
