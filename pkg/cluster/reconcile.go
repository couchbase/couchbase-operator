package cluster

import (
	"encoding/json"
	"fmt"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"
	"time"

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

	bucketsToAdd, bucketsToRemove := c.cluster.BucketDiff()
	if len(bucketsToAdd) > 0 {
		// create one of the buckets to add
		bucketName := bucketsToAdd[0]
		err := c.createClusterBucket(bucketName)
		if err != nil {
			// TODO: eventually may not want to retry bucket create
			// TODO: check if error is 'bucket-exists' (ie created same name?)
			return err
		}

		// update status
		c.status.AddBucket(bucketName)
	} else if len(bucketsToRemove) > 0 {
		// TODO: rm bucket
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
	if c.memberCounter == c.cluster.Spec.Size {
		//TODO: watch for rebalance complete and handle failures
		return c.rebalanceInNodes()
	}

	return nil
}

// Rebalance in nodes added to cluster.  This method will collect
// all the otpNodes as known nodes and run the rebalance on cluster
func (c *Cluster) rebalanceInNodes() error {

	// TODO: orchestrator api
	m := c.members.First(c.cluster.Name)
	username, password := c.cluster.Auth()
	couchbaseClient, err := couchbaseutil.NewClient(m.ClientURL(), username, password)
	if err != nil {
		return nil
	}

	// get known nodes being added to cluster
	knownNodes, err := couchbaseClient.KnownOTPNodes()
	if err != nil {
		return nil
	}
	return couchbaseClient.Rebalance(knownNodes, []string{})
}

// initializes node with cluster settings
func (c *Cluster) initClusterNode(m *couchbaseutil.Member) error {

	// create client
	username, password := c.cluster.Auth()
	couchbaseClient, err := couchbaseutil.NewReadyClient(m.ClientURL(), username, password)
	if err != nil {
		return err
	}

	// init node
	services := c.cluster.Spec.ClusterSettings.ServicesArr()
	settings := c.cluster.Spec.ClusterSettings
	return couchbaseClient.Initialize(m.Addr(),
		services,
		settings.DataServiceMemQuota,
		settings.IndexServiceMemQuota,
		settings.SearchServiceMemQuota)
}

// adds a node to the cluster
func (c *Cluster) addClusterNode(m *couchbaseutil.Member) error {

	// create client for node currently active in cluster
	// TODO: orchestrator api
	activeMember := c.members.First(c.cluster.Name)
	username, password := c.cluster.Auth()
	currentNodeClient, err := couchbaseutil.NewReadyClient(activeMember.ClientURL(), username, password)
	if err != nil {
		return err
	}

	// make sure node is ready to be added
	newNodeClient, err := couchbaseutil.NewReadyClient(m.ClientURL(), username, password)
	if err != nil {
		return err
	}

	// add node to cluster
	services := c.cluster.Spec.ClusterSettings.ServicesArr()
	f := func() (bool, error) {
		return true, currentNodeClient.AddNode(m.Addr(), username, password, services, c.cluster.Namespace)
	}
	err = retryutil.RetryOnErr(3*time.Second, 5, f, "add-node", c.cluster.Name)
	if err != nil {
		return err
	}

	// make sure node being added is healthy
	return newNodeClient.Healthy(couchbaseutil.RestTimeout)
}

// create bucket on cluster
func (c *Cluster) createClusterBucket(bucketName string) error {

	// establish cluster connection
	activeMember := c.members.First(c.cluster.Name)
	username, password := c.cluster.Auth()
	client, err :=
		couchbaseutil.NewReadyClient(activeMember.ClientURL(), username, password)
	if err != nil {
		return err
	}

	// marshalling spec into json
	config := c.cluster.Spec.GetBucketByName(bucketName)
	js, err := json.Marshal(config)
	if err != nil {
		return err
	}

	// create bucket
	if err = client.CreateBucketFromSpec(js); err != nil {
		return err
	}

	// make sure bucket exists
	f := func() (bool, error) {
		return client.BucketReady(bucketName)
	}
	return retryutil.RetryOnErr(5*time.Second, 60, f, "check-bucket-ready", c.cluster.Name)
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
