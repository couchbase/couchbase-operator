package cluster

import (
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"

	"k8s.io/api/core/v1"
)

func (c *Cluster) updateMembers(known couchbaseutil.MemberSet) error {
	// In the future we may need to keep some Couchbase specific information
	// for each node cached in the operator. If so, then that should be done
	// here.
	c.members = known
	return nil
}

func (c *Cluster) newMember(id int) *couchbaseutil.Member {
	name := couchbaseutil.CreateMemberName(c.cluster.Name, id)
	return &couchbaseutil.Member{
		Name:         name,
		Namespace:    c.cluster.Namespace,
		SecureClient: c.isSecureClient(),
	}
}

func podsToMemberSet(pods []*v1.Pod, sc bool) couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}
	for _, pod := range pods {
		m := &couchbaseutil.Member{Name: pod.Name, Namespace: pod.Namespace, SecureClient: sc}
		members.Add(m)
	}
	return members
}
