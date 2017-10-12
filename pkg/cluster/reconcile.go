package cluster

import (
	"fmt"

	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"k8s.io/api/core/v1"
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

	err := c.reconcileBuckets()
	if err != nil {
		return err
	}

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

	// add node to cluster
	if err := c.addClusterNode(newMember); err != nil {
		return err
	}

	// rebalance if this is last node to add to cluster
	if len(c.members) == c.cluster.Spec.Size {
		//TODO: watch for rebalance complete and handle failures
		return c.rebalance([]string{})
	}

	return nil
}

// Rebalance nodes in the cluster
func (c *Cluster) rebalance(nodesToRemove []string) error {
	m := c.members.First(c.cluster.Name, c.memberCounter)
	username, password := c.cluster.Auth()
	return couchbaseutil.Rebalance(m, username, password, c.cluster.Name, nodesToRemove, true)
}

// adds a node to the cluster
func (c *Cluster) addClusterNode(m *couchbaseutil.Member) error {

	// create client for node currently active in cluster
	// TODO: orchestrator api
	activeMember := c.members.First(c.cluster.Name, c.memberCounter)
	username, password := c.cluster.Auth()
	services := c.cluster.Spec.ClusterSettings.ServicesArr()
	return couchbaseutil.AddNode(activeMember, c.cluster.Name, m.ClientURL(), username, password, services)
}

// reconcile buckets by adding or removing
// buckets one at a time based on comparison
// of existing buckets to cluster spec
func (c *Cluster) reconcileBuckets() error {

	// set status buckets to existing buckets
	username, password := c.cluster.Auth()
	activeMember := c.members.First(c.cluster.Name, c.memberCounter)
	existingBuckets, err := couchbaseutil.
		GetBucketNames(activeMember, username, password)
	if err != nil {
		return err
	}
	c.status.Buckets = existingBuckets

	// when reconciling buckets, any buckets in cluster
	// created outside of operator are removed,
	// and any buckets removed by user are recreated
	// if still present in active spec
	spec := c.cluster.Spec
	bucketsToAdd, bucketsToRemove := spec.BucketDiff(existingBuckets)
	if len(bucketsToRemove) > 0 {
		bucketName := bucketsToRemove[0]
		err := c.deleteClusterBucket(bucketName)
		if err != nil {
			return err
		}
		c.status.RemoveBucket(bucketName)
	} else if len(bucketsToAdd) > 0 {
		bucketName := bucketsToAdd[0]
		err := c.createClusterBucket(bucketName)
		if err != nil {
			return err
		}
		c.status.AddBucket(bucketName)
	}

	return nil
}

// create bucket on cluster
func (c *Cluster) createClusterBucket(bucketName string) error {

	// establish cluster connection
	activeMember := c.members.First(c.cluster.Name, c.memberCounter)
	username, password := c.cluster.Auth()
	config := c.cluster.Spec.GetBucketByName(bucketName)
	return couchbaseutil.CreateBucket(activeMember, username, password, config)
}

func (c *Cluster) deleteClusterBucket(bucketName string) error {

	// establish cluster connection
	activeMember := c.members.First(c.cluster.Name, c.memberCounter)
	username, password := c.cluster.Auth()
	return couchbaseutil.DeleteBucket(activeMember, username, password, bucketName)
}

// initializes member with cluster settings
func (c *Cluster) initMember(m *couchbaseutil.Member) error {
	username, password := c.cluster.Auth()
	settings := c.cluster.Spec.ClusterSettings
	err := couchbaseutil.InitializeCluster(m, username, password, c.cluster.Name,
		settings.DataServiceMemQuota, settings.IndexServiceMemQuota, settings.SearchServiceMemQuota,
		settings.ServicesArr(), settings.DataPath, settings.IndexPath, settings.IndexStorageSetting)
	if err != nil {
		return err
	}

	// enables autofailover by default
	return couchbaseutil.SetAutoFailoverTimeout(m, username, password, c.cluster.Name, true, settings.AutoFailoverTimeout)
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
	nodeName := toRemove.Name
	err := c.rebalance([]string{toRemove.Addr() + ":8091"})
	if err != nil {
		return err
	}

	// remove member from operator
	c.members.Remove(nodeName)
	if err := c.removePod(nodeName); err != nil {
		return err
	}
	c.logger.Infof("removed member (%v)", nodeName)
	return nil
}
