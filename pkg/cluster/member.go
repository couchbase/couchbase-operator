package cluster

import (
	"fmt"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"k8s.io/api/core/v1"
)

func (c *Cluster) updateMembers(known couchbaseutil.MemberSet) error {
	// If we have no known members, default to an empty status
	status := couchbaseutil.NewClusterStatus()
	if !known.Empty() {
		var err error
		if status, err = c.client.GetClusterStatus(known); err != nil {
			return err
		}
	}

	// Establish initial members from running cluster Pods
	members := known

	// Collect additional members from available Persistent Volumes
	members.Append(c.getManagedPersistedMembers(status.KnownNodes()))

	// Return error in the case where cluster knows about nodes
	// that aren't identifed as members since these are either:
	//  1. A foreign node
	//  2. A node with both deleted Pod and Volume
	//  2. An ephemeral node with deleted Pod.
	//
	// In any of these cases nodes cannot be recovered and should
	// be manually failed over before we allow reconcile since
	// there's no way to determine if this node is managed by the operator.
	for _, node := range status.KnownNodes() {
		if !members.Contains(node) {
			return fmt.Errorf("cluster contains node `%s` which cannot be managed. Failover/Rebalance is recommended", node)
		}
	}

	if members.Empty() {
		return fmt.Errorf("cluster does not have any Pods that are running or recoverable")
	}

	c.members = members

	if err := c.updateMemberStatusWithClusterInfo(status); err != nil {
		return err
	}

	return nil
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
		log.Error(err, "Member discovery failed", "cluster", c.cluster.Name)
	} else {
		members = pvcMembers
	}
	return members
}

// getManagedPersistedMembers gets list of members associated with
// persisted volumes that are actually managed by active cluster
func (c *Cluster) getManagedPersistedMembers(knownNodes []string) couchbaseutil.MemberSet {
	managedMembers := couchbaseutil.MemberSet{}
	pvcMembers := c.pvcMembers()

	// When knownNodes are empty then we'll only rely on
	// members that can be retrieved from persisted volumes
	if len(knownNodes) == 0 {
		return pvcMembers
	}

	if !pvcMembers.Empty() {
		// Only include persisted members that are part of knownNodes
		for _, node := range knownNodes {
			if m, ok := pvcMembers[node]; ok {
				managedMembers.Add(m)
			}
		}
	}
	return managedMembers
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
