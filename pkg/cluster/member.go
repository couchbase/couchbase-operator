package cluster

import (
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	v1 "k8s.io/api/core/v1"
)

// updateMembers is the canonical source of truth for all membership information.
// It looks at Kubernetes for pods and pvcs (this is ostensibly free).
func (c *Cluster) updateMembers() error {
	// Get all pods these are running and ready for attempted API access.
	runningPods, _ := c.getClusterPodsByPhase()
	runningMembers := podsToMemberSet(runningPods)

	// All pods are members as are persistent volume claims.
	members := couchbaseutil.MemberSet{}
	members.Merge(runningMembers)
	members.Merge(c.pvcMembers())

	// The ready set is nodes that are in the active state.
	ready := couchbaseutil.MemberSet{}

	// The callable set is nodes that can be called with the Couchbase API.
	callable := couchbaseutil.MemberSet{}

	if !runningMembers.Empty() {
		// Try to find a Couchbase node that responds.
		status, err := c.GetStatusUnsafe(runningMembers)
		if err != nil {
			return err
		}

		// Add any nodes that Couchbase knows about that we do not.
		// Don't overwrite anything derived from PVCs as the member
		// metadata will be garbage from Couchbase.
		members.Merge(status.UnknownMembers)

		for name, member := range runningMembers {
			state, ok := status.NodeStates[name]
			if !ok {
				continue
			}

			if state == NodeStateActive {
				ready.Add(member)
			}

			if state.Callable() {
				callable.Add(member)
			}
		}
	}

	// The unready set is the boolean difference of all nodes with the ready set.
	unready := members.Diff(ready)

	// Update the cluster status
	c.updateMemberStatusWithClusterInfo(ready, unready)

	// Update the main cluster configuration
	c.members = members
	c.callableMembers = callable

	return nil
}

func (c *Cluster) newMember(id int, serverSpecName, image string) (couchbaseutil.Member, error) {
	version, err := k8sutil.CouchbaseVersion(image)
	if err != nil {
		return nil, err
	}

	name := couchbaseutil.CreateMemberName(c.cluster.Name, id)

	return couchbaseutil.NewMember(c.cluster.Namespace, c.cluster.Name, name, version, serverSpecName, c.cluster.IsTLSEnabled()), nil
}

func (c *Cluster) pvcMembers() couchbaseutil.MemberSet {
	return k8sutil.PVCToMemberset(c.k8s, c.cluster.Name, c.cluster.Namespace, c.cluster.IsTLSEnabled())
}

func podsToMemberSet(pods []*v1.Pod) couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}

	for _, pod := range pods {
		labels := pod.GetLabels()

		cluster := ""
		if val, ok := labels[constants.LabelCluster]; ok {
			cluster = val
		}

		config := ""
		if val, ok := labels[constants.LabelNodeConf]; ok {
			config = val
		}

		version := ""
		if val, ok := pod.Annotations[constants.CouchbaseVersionAnnotationKey]; ok {
			version = val
		}

		_, secure := pod.Annotations[constants.PodTLSAnnotation]

		members.Add(couchbaseutil.NewMember(pod.Namespace, cluster, pod.Name, version, config, secure))
	}

	return members
}
