package cluster

import (
	"time"

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
	ReconcileFailedNodes                     = 0x07
	ReconcileServerConfigs                   = 0x08
	ReconcileRemoveNodes                     = 0x09
	ReconcileRemoveUnmanaged                 = 0x0a
	ReconcileAddNodes                        = 0x0b
	ReconcileRebalance                       = 0x0c
	ReconcileDeadMembers                     = 0x0d
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
	if !r.couchbase.PendingAddNodes.Empty() || !r.couchbase.FailedNodes.Empty() ||
		!r.couchbase.DownNodes.Empty() || !r.couchbase.FailedAddNodes.Empty() ||
		len(r.couchbase.UnmanagedNodes) > 0 || !r.runningPods.Equal(c.members) ||
		!r.couchbase.UnclusteredNodes.Empty() || r.couchbase.IsRebalancing ||
		r.couchbase.NeedsRebalance {
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

	if len(r.couchbase.UnclusteredNodes) > 0 {
		c.logger.Infof("unclustered nodes: %s", r.couchbase.UnclusteredNodes)
	}

	if len(r.couchbase.UnmanagedNodes) > 0 {
		c.logger.Infof("unmanaged nodes: %s", r.couchbase.UnmanagedNodes)
	}

	if r.couchbase.NeedsRebalance {
		c.status.SetUnbalancedCondition()
	}

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
	for _, m := range r.couchbase.UnclusteredNodes {
		if err := c.removePod(m.Name); err != nil {
			c.logger.Errorf("Unable to remove unclustered node: %s", err.Error())
		} else {
			c.logger.Errorf("Removed unclustered node: %s", m.Name)
			r.runningPods.Remove(m.Name)
			c.members.Remove(m.Name)
		}
	}

	r.transitionState(ReconcileFailedAddNodes)
}

func (r *ReconcileMachine) handleFailedAddNodes(c *Cluster) {
	// These nodes have been added, but the node failed before a rebalance could
	// start. We will remove these nodes and re-add them in other pods later.
	for _, m := range r.couchbase.FailedAddNodes {
		err := couchbaseutil.CancelAddNode(r.knownNodes, c.cluster.Name, m.HostURL(), c.username, c.password)
		if err != nil {
			c.logger.Errorf("Unable to removed a failed pending add node: %s", err.Error())
			r.errored = true
			r.transitionState(ReconcileFinished)
			return
		}

		_, err = c.eventsCli.Create(k8sutil.FailedAddNodeEvent(m.Name, c.cluster))
		if err != nil {
			c.logger.Errorf("failed to create failed add node event: %v", err)
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
			m, err := c.addOneMember(serverSpec)
			if err != nil {
				c.logger.Warnf("Failed to add new node to cluster: %v", err)
				r.errored = true
				r.transitionState(ReconcileFinished)
				return
			}
			r.knownNodes.Add(m)
			r.runningPods.Add(m)
			addCount++

			_, err = c.eventsCli.Create(k8sutil.MemberAddEvent(m.Name, c.cluster))
			if err != nil {
				c.logger.Errorf("failed to create new member add event: %v", err)
			}
		}
	}

	r.transitionState(ReconcileRebalance)
}

func (r *ReconcileMachine) handleRebalance(c *Cluster) {
	if r.couchbase.NeedsRebalance {
		c.status.SetUnbalancedCondition()
		removeNodes := append(r.ejectNodes.HostURLs(), r.couchbase.UnmanagedNodes...)
		if err := c.rebalance(removeNodes); err != nil {
			c.logger.Warnf("Failed to start rebalance: %s", err.Error())
			r.errored = true
			r.transitionState(ReconcileFinished)
			return
		}

		_, err := c.eventsCli.Create(k8sutil.RebalanceEvent(c.cluster))
		if err != nil {
			c.logger.Errorf("failed to create rebalance event: %v", err)
		}

		for {
			toRemove := r.ejectNodes.Highest()
			if toRemove == nil {
				break
			}

			// This ensures events don't happen at roughly the same time. It looks
			// like Kubernetes tracks events at second resolution and this causes
			// our test verification to fail. Sleeping here isn't a big deal, but
			// we should find a more permanent solution that doesn't sleep in the
			// future.
			time.Sleep(1 * time.Second)

			_, err := c.eventsCli.Create(k8sutil.MemberRemoveEvent(toRemove.Name, c.cluster))
			if err != nil {
				c.logger.Errorf("failed to create member remove event: %v", err)
			}
			r.ejectNodes.Remove(toRemove.Name)
			r.runningPods.Remove(toRemove.Name)
		}
	}

	c.status.SetReadyCondition()
	c.status.ClearCondition(api.ClusterConditionScaling)

	newState, err := couchbaseutil.GetClusterStatus(r.knownNodes, c.username, c.password, c.cluster.Name)
	if err != nil {
		c.status.SetUnknownBalancedCondition()
	} else if !newState.NeedsRebalance {
		c.status.SetBalancedCondition()
	}

	r.transitionState(ReconcileDeadMembers)
}

// Dead members are members that the operator is tracking, but do not have a
// corresponding running pod.
func (r *ReconcileMachine) handleDeadMembers(c *Cluster) {
	c.updateMemberStatus(c.members)

	dead := c.members.Diff(r.runningPods)
	if !dead.Empty() {
		c.logger.Infof("removing one dead member")
		err := c.removeDeadMember(c.members.Diff(r.runningPods).PickOne())
		if err != nil {
			c.logger.Errorf("Failed to remove dead members: %s", err.Error())
			r.errored = true
		}
	}

	r.transitionState(ReconcileFinished)
}

func (r *ReconcileMachine) handleFinished(c *Cluster) {
	c.updateMemberStatus(c.members)
}

func (r *ReconcileMachine) transitionState(to ReconcileState) {
	r.state = to
}
