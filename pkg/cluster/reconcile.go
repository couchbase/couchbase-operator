package cluster

import (
	"fmt"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"k8s.io/api/core/v1"
)

func (c *Cluster) reconcile(pods []*v1.Pod) error {
	c.logger.Infoln("Start reconciling")
	defer c.logger.Infoln("Finish reconciling")

	defer func() {
		c.status.Size = c.members.Size()
	}()

	sp := c.cluster.Spec
	running := podsToMemberSet(pods, c.isSecureClient())
	running, err := c.removeUnknownMembers(running)
	if err != nil {
		return err
	}

	status, err := c.getClusterStatus()
	if err != nil {
		return err
	}

	if status.IsRebalancing {
		c.logger.Infoln("Skipping reconcile loop because the cluster is currently rebalancing")
		// TODO - Should track the status of rebalance here
		return nil
	}

	if status.NumUnhealthyNodes > status.AutoFailedNodes.Size() {
		c.logger.Infoln("Skipping reconcile loop to allow autofilover to take place")
		return nil
	}

	if status.AutoFailedNodes.Size() > 0 {
		c.logger.Infoln("An autofailover has taken place, rebalancing nodes")
		for _, m := range status.AutoFailedNodes {
			c.removeMember(m)
			running.Remove(m.Name)
		}
	}

	if status.NeedsRebalance {
		c.logger.Infoln("The cluster is unbalanced, starting rebalance")
		if err := c.rebalance([]string{}); err != nil {
			return err
		}
	}

	// TODO: We should update any cluster confiugration parameters here.
	c.reconcileClusterSettings()

	// Ensure that the actual cluster size matches the desired cluster size. Also
	// clean up and failed pods.

	if !running.IsEqual(c.members) || c.members.Size() != sp.Size {
		return c.reconcileMembers(running)
	}

	err = c.reconcileBuckets()
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

	if running.Size() == c.members.Size() {
		return c.resize()
	}

	c.logger.Infof("removing one dead member")
	// remove dead members that doesn't have any running pods before doing resizing.
	return c.removeDeadMember(c.members.Diff(running).PickOne())
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
	c.status.SetScalingUpCondition(c.members.Size(), c.cluster.Spec.Size)

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
	
	if err := c.setupServices(newMember.Name); err != nil {
		return fmt.Errorf("cluster create: fail to create services: %v", err)
	}

	// rebalance if this is last node to add to cluster
	if len(c.members) == c.cluster.Spec.Size {
		return c.rebalance([]string{})
	}

	return nil
}

func (c *Cluster) getClusterStatus() (*couchbaseutil.ClusterStatus, error) {
	return couchbaseutil.GetClusterStatus(c.members, c.username, c.password, c.cluster.Name)
}

// Rebalance nodes in the cluster
func (c *Cluster) rebalance(nodesToRemove []string) error {
	return couchbaseutil.Rebalance(c.members, c.username, c.password, c.cluster.Name, nodesToRemove, true)
}

// adds a node to the cluster
func (c *Cluster) addClusterNode(m *couchbaseutil.Member) error {
	services := c.cluster.Spec.ClusterSettings.ServicesArr()
	return couchbaseutil.AddNode(c.members.Diff(couchbaseutil.NewMemberSet(m)),
		c.cluster.Name, m.ClientURL(), c.username, c.password, services)
}

// reconcile buckets by adding or removing
// buckets one at a time based on comparison
// of existing buckets to cluster spec
func (c *Cluster) reconcileBuckets() error {
	existingBuckets, err := couchbaseutil.GetBucketNames(c.members, c.username, c.password)
	if err != nil {
		return err
	}

	// when reconciling buckets, any buckets in cluster
	// created outside of operator are removed,
	// and any buckets removed by user are recreated
	// if still present in active spec
	spec := c.cluster.Spec
	bucketsToAdd, bucketsToRemove := spec.BucketDiff(existingBuckets)
	bucketsToEdit, err := couchbaseutil.GetBucketsToEdit(c.members, c.username, c.password, &spec)
	if err != nil {
		return err
	}

	if len(bucketsToRemove) > 0 {
		bucketName := bucketsToRemove[0]
		err := c.deleteClusterBucket(bucketName)
		if err != nil {
			return err
		}
	} else if len(bucketsToEdit) > 0 {
		bucketName := bucketsToEdit[0]
		err := c.editClusterBucket(bucketName)
		if err != nil {
			return err
		}
	} else if len(bucketsToAdd) > 0 {
		bucketName := bucketsToAdd[0]
		err := c.createClusterBucket(bucketName)
		if err != nil {
			return err
		}
	}

	return nil
}

// create bucket on cluster
func (c *Cluster) createClusterBucket(bucketName string) error {
	config := c.cluster.Spec.GetBucketByName(bucketName)
	err := couchbaseutil.CreateBucket(c.members, c.username, c.password, config)
	if err == nil {
		c.status.UpdateBuckets(bucketName, config)
	}
	return err
}

func (c *Cluster) deleteClusterBucket(bucketName string) error {
	err := couchbaseutil.DeleteBucket(c.members, c.username, c.password, bucketName)
	if err == nil {
		c.status.RemoveBucket(bucketName)
	}
	return err
}

// edit bucket on cluster
func (c *Cluster) editClusterBucket(bucketName string) error {
	config := c.cluster.Spec.GetBucketByName(bucketName)

	if err := c.validateEditBucket(config); err != nil {
		return err
	}
	err := couchbaseutil.EditBucket(c.members, c.username, c.password, config)
	if err == nil {
		c.status.UpdateBuckets(bucketName, config)
	}
	return err
}

// Validate edit bucket returns error on attempts
// to change immutable attributes
func (c *Cluster) validateEditBucket(config *api.BucketConfig) error {

	bucketName := config.BucketName
	if statusBucket, ok := c.status.Buckets[bucketName]; ok {
		if config.ConflictResolution != statusBucket.ConflictResolution {
			return ErrInvalidBucketParamChange{
				bucketName,
				"conflictResolution",
				statusBucket.ConflictResolution,
				config.ConflictResolution}
		}
		if config.BucketType != statusBucket.BucketType {
			return ErrInvalidBucketParamChange{
				bucketName,
				"type",
				statusBucket.BucketType,
				config.BucketType}
		}
	}

	return nil
}

// initializes member with cluster settings
func (c *Cluster) initMember(m *couchbaseutil.Member) error {
	settings := c.cluster.Spec.ClusterSettings
	err := couchbaseutil.InitializeCluster(m, c.username, c.password, c.cluster.Name,
		settings.DataServiceMemQuota, settings.IndexServiceMemQuota, settings.SearchServiceMemQuota,
		settings.ServicesArr(), settings.DataPath, settings.IndexPath, settings.IndexStorageSetting)
	if err != nil {
		return err
	}

	// enables autofailover by default
	return couchbaseutil.SetAutoFailoverTimeout(c.members, c.username, c.password,
		c.cluster.Name, true, settings.AutoFailoverTimeout)
}

func (c *Cluster) removeOneMember() error {
	c.status.SetScalingDownCondition(c.members.Size(), c.cluster.Spec.Size)

	return c.removeMember(c.members.PickOne())
}

func (c *Cluster) removeDeadMember(toRemove *couchbaseutil.Member) error {
	c.logger.Infof("removing dead member %q", toRemove.Name)

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

// Remove all pods from running set that does not belong to member set.
func (c *Cluster) removeUnknownMembers(running couchbaseutil.MemberSet) (couchbaseutil.MemberSet, error) {
	unknownMembers := running.Diff(c.members)
	if unknownMembers.Size() > 0 {
		c.logger.Infof("removing unexpected pods: %v", unknownMembers)
		for _, m := range unknownMembers {
			if err := c.removePod(m.Name); err != nil {
				return running, err
			}
		}
	}
	return running.Diff(unknownMembers), nil
}

func (c *Cluster) reconcileClusterSettings() error {

	err := c.reconcileAutoFailoverSettings()

	//TODO: reconcile other cluster settings

	return err
}

// ensure autofailover timeout matches spec setting
func (c *Cluster) reconcileAutoFailoverSettings() error {
	failoverSettings, err := couchbaseutil.GetAutoFailoverSettings(c.members, c.username, c.password, c.cluster.Name)
	if err != nil {
		return err
	}

	clusterSettings := c.cluster.Spec.ClusterSettings
	if (failoverSettings.Timeout != clusterSettings.AutoFailoverTimeout) ||
		(failoverSettings.Enabled != true) {

		// reset autofailover timeout
		return couchbaseutil.SetAutoFailoverTimeout(c.members, c.username, c.password,
			c.cluster.Name, true, clusterSettings.AutoFailoverTimeout)
	}

	return nil
}
