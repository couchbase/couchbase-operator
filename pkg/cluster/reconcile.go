package cluster

import (
	"fmt"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"k8s.io/api/core/v1"
)

func (c *Cluster) reconcile(pods []*v1.Pod) error {
	c.logger.Infoln("Start reconciling")
	defer c.logger.Infoln("Finish reconciling")

	defer func() {
		c.status.Size = c.members.Size()
	}()

	sp := c.cluster.Spec

	cluster, err := c.getClusterStatus()
	if err != nil {
		c.logger.Warnf("Unable to get cluster state, skiping reconcile loop: %s", err.Error())
		return nil
	}

	state := &ReconcileMachine{
		runningPods: podsToMemberSet(pods, c.isSecureClient()),
		knownNodes:  couchbaseutil.NewMemberSet(),
		ejectNodes:  couchbaseutil.NewMemberSet(),
		couchbase:   cluster,
		state:       ReconcileInit,
	}

	c.reconcileClusterSettings()

	c.reconcileMembers(state)

	if err := c.reconcileBuckets(); err != nil {
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
// 1. Remove running pods that we didn't create explicitly (unknown members)
// 2. If we are currently in a rebalance then we should finish it.
// 3. If any nodes are down then wait for them to be failed over.
// 4. Decide which nodes should be removed and whether we need to add nodes.
//    - Nodes added to the cluster that are failed, but not rebalanced should
//      be removed.
//    - Nodes that have been failed over should be removed from the cluster
//      and rebalanced out.
//    - Nodes that are pending addition, healthy, but not yet rebalanced in
//      should be fully added in.
//    - Healthy active nodes should remain in the cluster.
// 5. We will now know what the current cluster would look like if we handled
//    any issues with the current nodes in the cluster. We now need to either
//    remove healthy nodes if we're scaling down, or add noew nodes if we need
//    to scale up.
// 6. Run a rebalance if neccessary.
// 7. Remove any nodes from the cached member set that are not part actually
//    part of the cluster.
func (c *Cluster) reconcileMembers(rm *ReconcileMachine) {
	done := false
	for !done {
		switch rm.state {
		case ReconcileInit:
			rm.handleInit(c)
		case ReconcileUnknownMembers:
			rm.handleUnknownMembers(c)
		case ReconcileRebalanceCheck:
			rm.handleRebalanceCheck(c)
		case ReconcileDownNodes:
			rm.handleDownNodes(c)
		case ReconcileFailedAddNodes:
			rm.handleFailedAddNodes(c)
		case ReconcileFailedNodes:
			rm.handleFailedNodes(c)
		case ReconcileServerConfigs:
			rm.handleUnknownServerConfigs(c)
		case ReconcileRemoveNodes:
			rm.handleRemoveNode(c)
		case ReconcileAddNodes:
			rm.handleAddNode(c)
		case ReconcileRebalance:
			rm.handleRebalance(c)
		case ReconcileFinished:
			rm.handleFinished(c)
			done = true
		default:
			panic("Invalid state\n")
		}

		if err := c.updateCRStatus(); err != nil {
			c.logger.Warnf("update CR status failed: %v", err)
		}
	}
}

func (c *Cluster) addOneMember(serverSpec api.ServerConfig) (*couchbaseutil.Member, error) {
	newMember := c.newMember(c.memberCounter, serverSpec.Name)
	c.members.Add(newMember)

	if err := c.createPod(c.members, newMember, serverSpec); err != nil {
		c.members.Remove(newMember.Name)
		return nil, fmt.Errorf("fail to create member's pod (%s): %v", newMember.Name, err)
	}
	c.memberCounter++
	c.logger.Infof("added member (%s)", newMember.Name)

	return newMember, couchbaseutil.AddNode(c.members.Diff(couchbaseutil.NewMemberSet(newMember)),
		c.cluster.Name, newMember.ClientURL(), c.username, c.password, serverSpec.Services)
}

func (c *Cluster) getClusterStatus() (*couchbaseutil.ClusterStatus, error) {
	return couchbaseutil.GetClusterStatus(c.members, c.username, c.password, c.cluster.Name)
}

// Rebalance nodes in the cluster
func (c *Cluster) rebalance(nodesToRemove []string) error {
	return couchbaseutil.Rebalance(c.members, c.username, c.password, c.cluster.Name, nodesToRemove, true)
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

		_, err = c.eventsCli.Create(k8sutil.BucketCreateEvent(bucketName, c.cluster))
		if err != nil {
			c.logger.Errorf("failed to create new bucket event: %v", err)
		}
	}
	return err
}

func (c *Cluster) deleteClusterBucket(bucketName string) error {
	err := couchbaseutil.DeleteBucket(c.members, c.username, c.password, bucketName)
	if err == nil {
		c.status.RemoveBucket(bucketName)

		_, err = c.eventsCli.Create(k8sutil.BucketDeleteEvent(bucketName, c.cluster))
		if err != nil {
			c.logger.Errorf("failed to create delete bucket event: %v", err)
		}
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

		//_, err = c.eventsCli.Create(k8sutil.BucketEditEvent(bucketName, c.cluster))
		//if err != nil {
		//	c.logger.Errorf("failed to create edit bucket event: %v", err)
		//}
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
func (c *Cluster) initMember(m *couchbaseutil.Member, serverSpec api.ServerConfig) error {
	settings := c.cluster.Spec.ClusterSettings
	err := couchbaseutil.InitializeCluster(m, c.username, c.password, c.cluster.Name,
		settings.DataServiceMemQuota, settings.IndexServiceMemQuota, settings.SearchServiceMemQuota,
		serverSpec.Services, serverSpec.DataPath, serverSpec.IndexPath, settings.IndexStorageSetting)
	if err != nil {
		return err
	}

	// enables autofailover by default
	return couchbaseutil.SetAutoFailoverTimeout(c.members, c.username, c.password,
		c.cluster.Name, true, settings.AutoFailoverTimeout)
}

func (c *Cluster) removeDeadMember(toRemove *couchbaseutil.Member) error {
	c.logger.Infof("removing dead member %q", toRemove.Name)
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
