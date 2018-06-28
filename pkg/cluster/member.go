package cluster

import (
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/api/core/v1"
)

func (c *Cluster) updateMembers(known couchbaseutil.MemberSet) error {

	known.Append(c.pvcMembers())
	status, err := c.client.GetClusterStatus(known)
	if err != nil {
		return err
	}
	c.updateMemberStatusWithClusterInfo(status)

	members := couchbaseutil.MemberSet{}
	members.Append(status.ActiveNodes)
	members.Append(status.PendingAddNodes)
	members.Append(status.FailedAddNodes)
	members.Append(status.DownNodes)

	c.members = members
	return nil
}

// What would the next member name allocated by the cluster be?
func (c *Cluster) nextMemberName() string {
	index, err := c.getPodIndex()
	if err != nil {
		// TODO: propagate an error
		return ""
	}
	return couchbaseutil.CreateMemberName(c.cluster.Name, index)
}

func (c *Cluster) newMember(id int, serverSpecName string) *couchbaseutil.Member {
	name := couchbaseutil.CreateMemberName(c.cluster.Name, id)
	return &couchbaseutil.Member{
		Name:         name,
		Namespace:    c.cluster.Namespace,
		ServerConfig: serverSpecName,
		SecureClient: false,
	}
}

func (c *Cluster) pvcMembers() couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}
	pvcMembers, err := k8sutil.PVCToMemberset(c.config.KubeCli,
		c.cluster.Namespace,
		c.cluster.Name,
		c.isSecureClient())
	if err != nil {
		c.logger.Warningf("Error occured listing members: %v", err)
	} else {
		members = pvcMembers
	}
	return members
}

func podsToMemberSet(pods []*v1.Pod, sc bool) couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}
	for _, pod := range pods {
		config := ""
		labels := pod.GetLabels()
		if val, ok := labels[constants.LabelNodeConf]; ok {
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
