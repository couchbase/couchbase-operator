package cluster

import (
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	corev1 "k8s.io/api/core/v1"
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

// podUpgradableResource implments the upgradableResource interface for pods.
type podUpgradableResource struct {
	// cluster is a reference to the cluster for client and namespace access.
	cluster *Cluster
	// pods is a local cache of fetched resource items.
	pods *corev1.PodList
	// actions is the list of possible upgrade actions to perform on a resource item.
	actions podUpgradeActionList
}

func newPodUpgradableResource(c *Cluster) upgradableResource {
	return &podUpgradableResource{
		cluster: c,
		actions: podUpgradeActionList{
			{upgradeRange: upgradeRange{"0.0.0", "1.2.0"}, action: upgradePodFrom000000To010200},
			{upgradeRange: upgradeRange{"1.2.0", "2.0.0"}, action: upgradePodFrom010200To020000},
		},
	}
}

func (r *podUpgradableResource) kind() string {
	return "pod"
}

func (r *podUpgradableResource) name(item int) string {
	return r.pods.Items[item].Name
}

func (r *podUpgradableResource) fetch() error {
	var err error
	r.pods, err = r.cluster.kubeClient.CoreV1().Pods(r.cluster.cluster.Namespace).List(k8sutil.ClusterListOpt(r.cluster.cluster.Name))
	if err != nil {
		return err
	}
	return nil
}

func (r *podUpgradableResource) lenItems() int {
	return len(r.pods.Items)
}

func (r *podUpgradableResource) itemVersion(item int) string {
	version := "0.0.0"
	if v, ok := r.pods.Items[item].Annotations[constants.ResourceVersionAnnotation]; ok {
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
	pod := &r.pods.Items[item]
	upgrade := r.actions[action].action
	if err := upgrade(r.cluster, pod); err != nil {
		return err
	}
	return nil
}

func (r *podUpgradableResource) commit(item int) error {
	pod := &r.pods.Items[item]
	if _, err := r.cluster.kubeClient.CoreV1().Pods(r.cluster.cluster.Namespace).Update(pod); err != nil {
		return err
	}
	return nil
}

// upgradePodFrom000000To010200 performs pod upgrades to 1.2.0 from all prior versions.
// * The "couchbase.version" annotation was changed to "server.couchbase.com/version".
func upgradePodFrom000000To010200(cluster *Cluster, pod *corev1.Pod) error {
	// Update the version annotation
	pod.Annotations[constants.ResourceVersionAnnotation] = "1.2.0"

	// Add the server version annotation from the cluster's current version.
	pod.Annotations[constants.CouchbaseVersionAnnotationKey] = cluster.cluster.Status.CurrentVersion
	delete(pod.Annotations, "couchbase.version")

	return nil
}

// upgradePodFrom010200To020000 performs pod upgrades to 2.0.0 from 1.2.0.
// * The "pod.couchbase.com/tls" annotation was added.
// * The "pod.couchbase.com/spec" annotation was added (very hard, just let upgrade happen).
func upgradePodFrom010200To020000(cluster *Cluster, pod *corev1.Pod) error {
	// Update the version annotation
	pod.Annotations[constants.ResourceVersionAnnotation] = "2.0.0"

	// Previously we allowed TLS to be on or off during the life cycle of a cluster.
	// Now we can allow upgrade and downgrade we must explicitly monitor which pods
	// have TLS secrets mounted.
	for _, container := range pod.Spec.Containers {
		if container.Name == constants.CouchbaseContainerName {
			for _, mount := range container.VolumeMounts {
				if mount.Name == constants.CouchbaseTLSVolumeName {
					pod.Annotations[constants.PodTLSAnnotation] = "enabled"
					break
				}
			}
			break
		}
	}

	return nil
}
