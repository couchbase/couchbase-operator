package cluster

import (
	"fmt"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"

	"k8s.io/api/core/v1"
	"time"
)

func (c *Cluster) reconcile(pods []*v1.Pod) error {
	c.logger.Infoln("Start reconciling")
	defer c.logger.Infoln("Finish reconciling")

	defer func() {
		c.status.Size = c.members.Size()
	}()

	// TODO: We should check to see if the cluster is rebalancing here

	// TODO: We should check to see if there are any Couchbase nodes marked as
	// failed here. If so we should return and reconcile the cluster later once
	// the failure has been handled by the Couchbase cluster manager.

	// TODO: We should update any cluster confiugration parameters here.

	// Ensure that the actual cluster size matches the desired cluster size. Also
	// clean up and failed pods.
	sp := c.cluster.Spec
	running := podsToMemberSet(pods, c.isSecureClient())
	if !running.IsEqual(c.members) || c.members.Size() != sp.Size {
		return c.reconcileMembers(running)
	}

	// TODO: We should add, delete or edit buckets here.

	// TODO: We should upgrade any nodes in the cluster here.

	c.status.SetVersion(sp.Version)
	c.status.SetReadyCondition()

	return nil
}

// reconcileMembers reconciles
// - running pods on k8s and cluster membership
// - cluster membership and expected size of couchbase cluster
// Steps:
// 1. Remove all pods from running set that does not belong to member set.
// 2. L consist of remaining pods of runnings
// 3. If L = members, the current state matches the membership state. END.
// 4. Add one missing member. END.
func (c *Cluster) reconcileMembers(running couchbaseutil.MemberSet) error {
	c.logger.Infof("running members: %s", running)
	c.logger.Infof("cluster membership: %s", c.members)

	unknownMembers := running.Diff(c.members)
	if unknownMembers.Size() > 0 {
		c.logger.Infof("removing unexpected pods: %v", unknownMembers)
		for _, m := range unknownMembers {
			if err := c.removePod(m.Name); err != nil {
				return err
			}
		}
	}
	L := running.Diff(unknownMembers)

	if L.Size() == c.members.Size() {
		return c.resize()
	}

	c.logger.Infof("removing one dead member")
	// remove dead members that doesn't have any running pods before doing resizing.
	return c.removeDeadMember(c.members.Diff(L).PickOne())
}

func (c *Cluster) resize() error {
	if c.members.Size() == c.cluster.Spec.Size {
		return nil
	}

	if c.members.Size() < c.cluster.Spec.Size {
		return c.addOneMember()
	}

	return c.removeOneMember()
}

func (c *Cluster) addOneMember() error {
	c.status.AppendScalingUpCondition(c.members.Size(), c.cluster.Spec.Size)

	newMember := c.newMember(c.memberCounter)
	c.members.Add(newMember)

	if err := c.createPod(c.members, newMember); err != nil {
		return fmt.Errorf("fail to create member's pod (%s): %v", newMember.Name, err)
	}
	c.memberCounter++
	c.logger.Infof("added member (%s)", newMember.Name)
	return nil
}

// initializes member with cluster settings
func (c *Cluster) initMember(m *couchbaseutil.Member) error {
	// create client
	username, password := c.cluster.Auth()
	couchbaseClient, err := couchbaseutil.
		NewClientForMember(m, username, password)

	// make sure node is ready
	_, err = couchbaseClient.IsReady(m.ClientURL(), 30*time.Second)
	if err != nil {
		return err
	}

	// init node
	services := c.cluster.Spec.ClusterSettings.ServicesArr()
	return couchbaseClient.Initialize(m.Addr(), services, c.cluster.Namespace)
}

func (c *Cluster) removeOneMember() error {
	c.status.AppendScalingDownCondition(c.members.Size(), c.cluster.Spec.Size)

	return c.removeMember(c.members.PickOne())
}

func (c *Cluster) removeDeadMember(toRemove *couchbaseutil.Member) error {
	c.logger.Infof("removing dead member %q", toRemove.Name)
	c.status.AppendRemovingDeadMember(toRemove.Name)

	return c.removeMember(toRemove)
}

func (c *Cluster) removeMember(toRemove *couchbaseutil.Member) error {
	// TODO - Add the Couchbase API calls to remove nodes from the cluster

	c.members.Remove(toRemove.Name)
	if err := c.removePod(toRemove.Name); err != nil {
		return err
	}
	c.logger.Infof("removed member (%v)", toRemove.Name)
	return nil
}
