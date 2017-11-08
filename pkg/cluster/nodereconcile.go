package cluster

import (
	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
)

type ReconcileState int

const (
	ReconcileInit           ReconcileState = 0x01
	ReconcileUnknownMembers                = 0x02
	ReconcileRebalanceCheck                = 0x03
	ReconcileDownNodes                     = 0x04
	ReconcileFailedAddNodes                = 0x05
	ReconcileFailedNodes                   = 0x06
	ReconcileRemoveNodes                   = 0x07
	ReconcileAddNodes                      = 0x08
	ReconcileRebalance                     = 0x09
	ReconcileFinished                      = 0xff
)

type ReconcileMachine struct {
	runningPods couchbaseutil.MemberSet
	knownNodes  couchbaseutil.MemberSet
	ejectNodes  couchbaseutil.MemberSet
	couchbase   *couchbaseutil.ClusterStatus
	state       ReconcileState
}

func (r *ReconcileMachine) handleInit(c *Cluster) {
	if r.couchbase.PendingAddNodes.Empty() && r.couchbase.FailedNodes.Empty() &&
		r.couchbase.DownNodes.Empty() && r.couchbase.FailedAddNodes.Empty() &&
		r.couchbase.ActiveNodes.Size() == c.cluster.Spec.TotalSize() &&
		r.couchbase.UnknownNodes.Empty() && r.runningPods.Equal(c.members) &&
		!r.couchbase.IsRebalancing && !r.couchbase.NeedsRebalance {
		r.transitionState(ReconcileFinished)
		return
	}

	c.logger.Infof("running members: %s", r.runningPods)
	c.logger.Infof("cluster membership: %s", c.members)
	c.logger.Infof("active nodes: %s", r.couchbase.ActiveNodes)

	if !r.couchbase.PendingAddNodes.Empty() {
		c.logger.Infof("pending add nodes: %s", r.couchbase.PendingAddNodes)
	}

	if !r.couchbase.FailedAddNodes.Empty() {
		c.logger.Infof("failed add nodes: %s", r.couchbase.FailedAddNodes)
	}

	if !r.couchbase.DownNodes.Empty() {
		c.logger.Infof("down nodes: %s", r.couchbase.DownNodes)
	}

	if !r.couchbase.FailedNodes.Empty() {
		c.logger.Infof("failed nodes: %s", r.couchbase.FailedNodes)
	}

	if !r.couchbase.UnknownNodes.Empty() {
		c.logger.Infof("unknown nodes: %s", r.couchbase.UnknownNodes)
	}

	if r.couchbase.NeedsRebalance {
		c.status.SetUnbalancedCondition()
	}

	r.knownNodes.Append(r.couchbase.ActiveNodes)
	r.knownNodes.Append(r.couchbase.PendingAddNodes)
	r.transitionState(ReconcileUnknownMembers)
}

func (r *ReconcileMachine) handleUnknownMembers(c *Cluster) {
	var err error
	r.runningPods, err = c.removeUnknownMembers(r.runningPods)
	if err != nil {
		c.logger.Errorf("Removal of unknown members failed: %s", err.Error())
		r.transitionState(ReconcileFinished)
	} else {
		r.transitionState(ReconcileRebalanceCheck)
	}
}

func (r *ReconcileMachine) handleRebalanceCheck(c *Cluster) {
	if r.couchbase.IsRebalancing {
		c.logger.Infoln("Skipping reconcile loop because the cluster is currently rebalancing")
		r.transitionState(ReconcileFinished)
	} else {
		r.transitionState(ReconcileDownNodes)
	}
}

func (r *ReconcileMachine) handleDownNodes(c *Cluster) {
	if r.couchbase.DownNodes.Size() > 0 {
		c.status.SetUnavailableCondition(r.couchbase.DownNodes.ClientURLs())
		c.logger.Warnln("Unable to reconcile nodes, waiting for auto-failover to take place")
		r.transitionState(ReconcileFinished)
	} else {
		c.status.SetReadyCondition()
		r.transitionState(ReconcileFailedAddNodes)
	}
}

func (r *ReconcileMachine) handleFailedAddNodes(c *Cluster) {
	// These nodes have been added, but the node failed before a rebalance could
	// start. We will remove these nodes and re-add them in other pods later.
	for _, m := range r.couchbase.FailedAddNodes {
		err := couchbaseutil.CancelAddNode(r.knownNodes, c.cluster.Name, m.Addr(), c.username, c.password)
		if err != nil {
			c.logger.Errorf("Unable to removed a failed pending add node: %s", err.Error())
			r.transitionState(ReconcileFinished)
			return
		}
		r.runningPods.Remove(m.Name)
	}

	r.transitionState(ReconcileFailedNodes)
}

func (r *ReconcileMachine) handleFailedNodes(c *Cluster) {
	for _, m := range r.couchbase.FailedNodes {
		c.logger.Infof("An auto-failover has taken place on %s, planning removal", m.ClientURL())
		r.ejectNodes.Add(m)
	}

	r.transitionState(ReconcileRemoveNodes)
}

func (r *ReconcileMachine) handleRemoveNode(c *Cluster) {
	if r.knownNodes.Size() > c.cluster.Spec.TotalSize() {
		originalSize := r.couchbase.ActiveNodes.Size() + r.couchbase.PendingAddNodes.Size()
		c.status.SetScalingDownCondition(originalSize, c.cluster.Spec.TotalSize())

		r.couchbase.NeedsRebalance = true
		toRemove := r.knownNodes.PickOne()
		r.knownNodes.Remove(toRemove.Name)
		r.ejectNodes.Add(toRemove)
	} else {
		r.transitionState(ReconcileAddNodes)
	}
}

func (r *ReconcileMachine) handleAddNode(c *Cluster) {
	if r.knownNodes.Size() < c.cluster.Spec.TotalSize() {
		originalSize := r.couchbase.ActiveNodes.Size() + r.couchbase.PendingAddNodes.Size()
		c.status.SetScalingUpCondition(originalSize, c.cluster.Spec.TotalSize())

		r.couchbase.NeedsRebalance = true
		m, err := c.addOneMember()
		if err != nil {
			c.logger.Warnf("Failed to add new node to cluster: %v", err)
			r.transitionState(ReconcileFinished)
			return
		}
		r.knownNodes.Add(m)
		r.runningPods.Add(m)
	} else {
		r.transitionState(ReconcileRebalance)
	}
}

func (r *ReconcileMachine) handleRebalance(c *Cluster) {
	if r.couchbase.NeedsRebalance {
		c.status.SetUnbalancedCondition()
		if err := c.rebalance(r.ejectNodes.ClientURLs()); err != nil {
			c.logger.Warnf("Failed to start rebalance: %s", err.Error())
			r.transitionState(ReconcileFinished)
			return
		}
		r.runningPods = r.runningPods.Diff(r.ejectNodes)
	}

	c.status.SetReadyCondition()
	c.status.ClearCondition(api.ClusterConditionScaling)

	newState, err := c.getClusterStatus()
	if err != nil {
		c.status.SetUnknownBalancedCondition()
	} else if !newState.NeedsRebalance {
		c.status.SetBalancedCondition()
	}

	r.transitionState(ReconcileFinished)
}

func (r *ReconcileMachine) handleFinished(c *Cluster) {
	c.updateMemberStatus(c.members)

	dead := c.members.Diff(r.runningPods)
	if !dead.Empty() {
		// remove dead members that don't have any running pods.
		c.logger.Infof("removing one dead member")
		err := c.removeDeadMember(c.members.Diff(r.runningPods).PickOne())
		if err != nil {
			c.logger.Errorf("Failed to remove dead members: %s", err.Error())
		}
	}
}

func (r *ReconcileMachine) transitionState(to ReconcileState) {
	r.state = to
}
