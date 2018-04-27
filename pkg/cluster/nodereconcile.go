package cluster

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

type ReconcileState int

const (
	ReconcileInit             ReconcileState = 0x01
	ReconcileUnknownMembers                  = 0x02
	ReconcileRebalanceCheck                  = 0x03
	ReconcileDownNodes                       = 0x04
	ReconcileUnclusteredNodes                = 0x05
	ReconcileFailedAddNodes                  = 0x06
	ReconcileAddBackNodes                    = 0x07
	ReconcileFailedNodes                     = 0x08
	ReconcileServerConfigs                   = 0x09
	ReconcileRemoveNodes                     = 0x0a
	ReconcileRemoveUnmanaged                 = 0x0b
	ReconcileAddNodes                        = 0x0c
	ReconcileRebalance                       = 0x0d
	ReconcileDeadMembers                     = 0x0e
	ReconcileFinished                        = 0xff
)

type ReconcileMachine struct {
	runningPods couchbaseutil.MemberSet
	knownNodes  couchbaseutil.MemberSet
	ejectNodes  couchbaseutil.MemberSet
	couchbase   *couchbaseutil.ClusterStatus
	state       ReconcileState
	errored     bool
}

func (r *ReconcileMachine) handleInit(c *Cluster) {
	needsReconcile := false

	// If we have any cluster related issues like failed nodes or if the cluster
	// is not balanced or is currently rebalancing then we need to run reconcile.
	if !r.couchbase.ClusterHealthy() || !r.runningPods.Equal(c.members) {
		needsReconcile = true
	}

	// If we have any server specs that are not properly sized then we need to
	// reconcile the nodes.
	serverSpecs := c.cluster.Spec.ServerSettings
	for _, serverSpec := range serverSpecs {
		nodes := r.couchbase.ActiveNodes.GroupByServerConfig(serverSpec.Name)
		if nodes.Size() != serverSpec.Size {
			needsReconcile = true
			c.logger.Infof("server config %s: %s", serverSpec.Name, nodes)
		}
	}

	// If we have any running pods that are not part of one of the current
	// server configs then we need to reconcile so they can be removed.
	for _, m := range r.runningPods {
		found := false
		for _, serverSpec := range serverSpecs {
			if m.ServerConfig == serverSpec.Name {
				found = true
			}
		}

		if !found {
			needsReconcile = true
		}
	}

	if !needsReconcile {
		c.status.SetBalancedCondition()
		r.transitionState(ReconcileFinished)
		return
	}

	// Catch all "needs rebalance" check handles things like auto-failover
	// which is outside of our control, or existing rebalance conditions
	// after a restart
	if r.couchbase.NeedsRebalance || r.couchbase.IsRebalancing {
		c.status.SetUnbalancedCondition()
		if err := c.updateCRStatus(); err != nil {
			// TODO: we should handle errors properly
			r.transitionState(ReconcileFinished)
			return
		}
	}

	c.logger.Infof("running members: %s", r.runningPods)
	c.logger.Infof("cluster membership: %s", c.members)

	r.couchbase.LogStatus(c.logger)

	r.knownNodes.Append(r.couchbase.ActiveNodes)
	r.knownNodes.Append(r.couchbase.PendingAddNodes)
	r.transitionState(ReconcileUnknownMembers)
}

// Unknown members are members who are currently running, but not part
// of the set of nodes the operator is tracking.
func (r *ReconcileMachine) handleUnknownMembers(c *Cluster) {
	var err error
	r.runningPods, err = c.removeUnknownMembers(r.runningPods)
	if err != nil {
		c.logger.Errorf("Removal of unknown members failed: %s", err.Error())
		r.errored = true
		r.transitionState(ReconcileFinished)
	} else {
		r.transitionState(ReconcileRebalanceCheck)
	}
}

func (r *ReconcileMachine) handleRebalanceCheck(c *Cluster) {
	if r.couchbase.IsRebalancing {
		c.logger.Infoln("Skipping reconcile loop because the cluster is currently rebalancing")
		r.errored = true
		r.transitionState(ReconcileFinished)
	} else {
		r.transitionState(ReconcileDownNodes)
	}
}

func (r *ReconcileMachine) handleDownNodes(c *Cluster) {
	if r.couchbase.DownNodes.Size() > 0 {
		c.status.SetUnavailableCondition(r.couchbase.DownNodes.ClientURLs())
		c.logger.Warnln("Unable to reconcile nodes, waiting for auto-failover to take place")
		r.errored = true
		r.transitionState(ReconcileFinished)
	} else {
		c.status.SetReadyCondition()
		r.transitionState(ReconcileUnclusteredNodes)
	}
}

func (r *ReconcileMachine) handleUnclusteredNodes(c *Cluster) {
	for name, _ := range r.couchbase.UnclusteredNodes {
		if err := c.destroyMember(name); err != nil {
			c.logger.Errorf("Unable to remove unclustered node: %s", err.Error())
		} else {
			c.logger.Infof("Removed unclustered node: %s", name)
			r.runningPods.Remove(name)

			// Nodes may be rebalanced out in a previous iteration (caused by
			// node failover) and thus miss out the ejection events from rebalance().
			//
			// TODO: it makes sense to unify handling all unclustered nodes *after*
			// reblancing has occurred, however tests will probably fail left right
			// and center due to the events occurring after the cluster is in a healthy
			// state.
			c.raiseEvent(k8sutil.MemberRemoveEvent(name, c.cluster))
		}
	}

	r.transitionState(ReconcileFailedAddNodes)
}

func (r *ReconcileMachine) handleFailedAddNodes(c *Cluster) {
	// These nodes have been added, but the node failed before a rebalance could
	// start. We will remove these nodes and re-add them in other pods later.
	for _, m := range r.couchbase.FailedAddNodes {
		err := c.cancelAddMember(r.knownNodes, m)
		if err != nil {
			c.logger.Errorf("Unable to removed a failed pending add node: %s", err.Error())
			r.errored = true
			r.transitionState(ReconcileFinished)
			return
		}

		r.runningPods.Remove(m.Name)
	}

	r.transitionState(ReconcileAddBackNodes)
}

func (r *ReconcileMachine) handleAddBackNodes(c *Cluster) {
	// These nodes are failed over, but are responding and can be added back into the cluster,
	// but for now we will just consider them failed and remove them.
	for _, m := range r.couchbase.AddBackNodes {
		c.logger.Infof("Removing healthy node %s because it is failed over", m.ClientURL())
		r.ejectNodes.Add(m)
	}

	r.transitionState(ReconcileFailedNodes)
}

func (r *ReconcileMachine) handleFailedNodes(c *Cluster) {
	for _, m := range r.couchbase.FailedNodes {
		c.logger.Infof("An auto-failover has taken place on %s, planning removal", m.ClientURL())
		r.ejectNodes.Add(m)
	}

	r.transitionState(ReconcileServerConfigs)
}

func (r *ReconcileMachine) handleUnknownServerConfigs(c *Cluster) {
	// If a server configuration was deleted in a spec update then we will clean
	// up all of the nodes from that server config here.
	for _, m := range r.runningPods {
		found := false
		for _, serverSpec := range c.cluster.Spec.ServerSettings {
			if m.ServerConfig == serverSpec.Name {
				found = true
				break
			}
		}

		if !found {
			c.logger.Infof("Member %s is no longer part of any server config, removing", m.Name)
			r.couchbase.NeedsRebalance = true
			r.knownNodes.Remove(m.Name)
			r.ejectNodes.Add(m)
		}
	}

	r.transitionState(ReconcileRemoveNodes)
}

func (r *ReconcileMachine) handleRemoveNode(c *Cluster) {
	serverSpecs := c.cluster.Spec.ServerSettings
	for _, serverSpec := range serverSpecs {
		removeCount := 0
		nodes := r.runningPods.GroupByServerConfig(serverSpec.Name)
		for (nodes.Size() - removeCount) > serverSpec.Size {
			originalSize := r.couchbase.ActiveNodes.Size() + r.couchbase.PendingAddNodes.Size()
			c.status.SetScalingDownCondition(originalSize, c.cluster.Spec.TotalSize())

			r.couchbase.NeedsRebalance = true
			toRemove := nodes.Highest()
			r.knownNodes.Remove(toRemove.Name)
			r.ejectNodes.Add(toRemove)
			removeCount++
		}
	}

	r.transitionState(ReconcileRemoveUnmanaged)
}

func (r *ReconcileMachine) handleUnmanagedNodes(c *Cluster) {
	if len(r.couchbase.UnmanagedNodes) > 0 {
		r.couchbase.NeedsRebalance = true
	}

	r.transitionState(ReconcileAddNodes)
}

func (r *ReconcileMachine) handleAddNode(c *Cluster) {
	serverSpecs := c.cluster.Spec.ServerSettings
	for _, serverSpec := range serverSpecs {
		addCount := 0
		nodes := r.runningPods.GroupByServerConfig(serverSpec.Name)
		for nodes.Size()+addCount < serverSpec.Size {
			originalSize := r.couchbase.ActiveNodes.Size() + r.couchbase.PendingAddNodes.Size()
			c.status.SetScalingUpCondition(originalSize, c.cluster.Spec.TotalSize())

			r.couchbase.NeedsRebalance = true
			m, err := c.addMember(serverSpec)
			if err != nil {
				c.logger.Warnf("Failed to add new node to cluster: %v", err)
				r.errored = true
				r.transitionState(ReconcileFinished)
				return
			}
			r.knownNodes.Add(m)
			r.runningPods.Add(m)
			addCount++
		}
	}

	r.transitionState(ReconcileRebalance)
}

func (r *ReconcileMachine) handleRebalance(c *Cluster) {
	if r.couchbase.NeedsRebalance {
		if err := c.rebalance(r.ejectNodes, r.couchbase.UnmanagedNodes); err != nil {
			c.logger.Warnf("Failed to rebalance: %s", err.Error())
			r.errored = true
			r.transitionState(ReconcileFinished)
			return
		}

		for _, toRemove := range r.ejectNodes {
			r.ejectNodes.Remove(toRemove.Name)
			r.runningPods.Remove(toRemove.Name)
		}
	}

	c.status.SetReadyCondition()
	c.status.ClearCondition(api.ClusterConditionScaling)

	r.transitionState(ReconcileDeadMembers)
}

// Dead members are members that the operator is tracking, but do not have a
// corresponding running pod.
func (r *ReconcileMachine) handleDeadMembers(c *Cluster) {
	dead := c.members.Diff(r.runningPods)
	for name, _ := range dead {
		if err := c.destroyMember(name); err != nil {
			c.logger.Errorf("Failed to remove dead members: %s", err.Error())
			r.errored = true
		}
	}

	r.transitionState(ReconcileFinished)
}

func (r *ReconcileMachine) handleFinished(c *Cluster) {
}

func (r *ReconcileMachine) transitionState(to ReconcileState) {
	r.state = to
}
