package cluster

import (
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"k8s.io/api/core/v1"
)

func (c *Cluster) updateMembers(known couchbaseutil.MemberSet) error {
	status, err := couchbaseutil.GetClusterStatus(known, c.username, c.password, c.cluster.Name)
	if err != nil {
		return err
	}
	members := couchbaseutil.MemberSet{}
	members.Append(status.ActiveNodes)
	members.Append(status.PendingAddNodes)
	members.Append(status.FailedAddNodes)
	members.Append(status.DownNodes)

	ct := members.GetHighestMemberCounter()
	if ct+1 > c.memberCounter {
		c.memberCounter = ct + 1
	}
	c.members = members
	return nil
}

func (c *Cluster) newMember(id int, serverSpecName string) *couchbaseutil.Member {
	name := couchbaseutil.CreateMemberName(c.cluster.Name, id)
	return &couchbaseutil.Member{
		Name:         name,
		Namespace:    c.cluster.Namespace,
		ServerConfig: serverSpecName,
		SecureClient: c.isSecureClient(),
	}
}

func podsToMemberSet(pods []*v1.Pod, sc bool) couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}
	for _, pod := range pods {
		config := ""
		labels := pod.GetLabels()
		if val, ok := labels["couchbase_node_conf"]; ok {
			config = val
		}

		m := &couchbaseutil.Member{
			Name:         pod.Name,
			Namespace:    pod.Namespace,
			ServerConfig: config,
			SecureClient: sc,
		}
		members.Add(m)
	}
	return members
}
