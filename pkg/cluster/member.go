package cluster

import (
	"fmt"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"k8s.io/api/core/v1"
)

func (c *Cluster) updateMembers(known couchbaseutil.MemberSet) error {

	status, err := c.client.GetClusterStatus(known)
	if err != nil {
		return err
	}
	c.updateMemberStatusWithClusterInfo(status)

	// Establish initial members from running cluster Pods
	members := known

	// Sync additional members from available Persistent Volumes
	if pvm := c.pvcMembers(); !pvm.Empty() {
		members.Append(pvm)
	}

	// Return error in the case where cluster knows about nodes
	// that aren't identifed as members since these are either:
	//  1. A foreign node
	//  2. A node with both deleted Pod and Volume
	//  2. An ephemeral node with deleted Pod.
	//
	// In any of these cases nodes cannot be recovered and should
	// be manually failed over before we allow reconcile since
	// there's no way to determine which case we're dealing with.
	for _, node := range status.KnownNodes() {
		if !members.Contains(node) {
			return fmt.Errorf("Cluster contains node `%s` which cannot be managed. Failover/Rebalance is recommended.", node)
		}
	}

	if members.Empty() {
		return fmt.Errorf("Cluster does not have any Pods that are running or recoverable")
	}

	c.members = members
	return nil
}

// What would the next member name allocated by the cluster be?
func (c *Cluster) nextMemberName() string {
	index := c.getPodIndex()
	return couchbaseutil.CreateMemberName(c.cluster.Name, index)
}

func (c *Cluster) newMember(id int, serverSpecName, version string) *couchbaseutil.Member {
	name := couchbaseutil.CreateMemberName(c.cluster.Name, id)
	return &couchbaseutil.Member{
		Name:         name,
		Namespace:    c.cluster.Namespace,
		ServerConfig: serverSpecName,
		SecureClient: false,
		Version:      version,
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

		version := ""
		if val, ok := pod.Annotations[constants.CouchbaseVersionAnnotationKey]; ok {
			version = val
		}

		m := &couchbaseutil.Member{
			Name:         pod.Name,
			Namespace:    pod.Namespace,
			ServerConfig: config,
			SecureClient: sc,
			Version:      version,
		}
		members.Add(m)
	}
	return members
}
