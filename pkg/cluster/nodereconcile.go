package cluster

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

// ReconcileState is an enumeration used to define the current
// node in the state machine
type ReconcileState int

const (
	ReconcileInit ReconcileState = iota
	ReconcileUnknownMembers
	ReconcileRebalanceCheck
	ReconcileWarmupNodes
	ReconcileDownNodes
	ReconcileUnclusteredNodes
	ReconcileFailedAddNodes
	ReconcileAddBackNodes
	ReconcileFailedAddBackNodes
	ReconcileFailedNodes
	ReconcileServerConfigs
	ReconcileRemoveNodes
	ReconcileRemoveUnmanaged
	ReconcileAddNodes
	ReconcileServerGroups
	ReconcileNodeServices
	ReconcileRebalance
	ReconcileDeadMembers
	ReconcileNotifyFinished
	ReconcileFinished
)

const downNodeThreshold int = 60

// reconcileFunc represents a function used to reconcile the cluster state,
// it must transition the state if necessary, or return an error
type reconcileFunc func(*ReconcileMachine, *Cluster) error

// reconcileFuncMap maps from a reconciliation state to a reconcile function
type reconcileFuncMap map[ReconcileState]reconcileFunc

// lookup finds the matching reconcile function for a specific state and returns
// a reference to it, or an error
func (r reconcileFuncMap) lookup(state ReconcileState) (reconcileFunc, error) {
	f, ok := r[state]
	if !ok {
		return nil, fmt.Errorf("Invalid reconcile state")
	}
	return f, nil
}

var (
	reconcileFunctions = reconcileFuncMap{
		ReconcileInit:               handleInit,
		ReconcileUnknownMembers:     handleUnknownMembers,
		ReconcileRebalanceCheck:     handleRebalanceCheck,
		ReconcileWarmupNodes:        handleWarmupNodes,
		ReconcileDownNodes:          handleDownNodes,
		ReconcileUnclusteredNodes:   handleUnclusteredNodes,
		ReconcileFailedAddNodes:     handleFailedAddNodes,
		ReconcileAddBackNodes:       handleAddBackNodes,
		ReconcileFailedAddBackNodes: handleFailedAddBackNodes,
		ReconcileFailedNodes:        handleFailedNodes,
		ReconcileServerConfigs:      handleUnknownServerConfigs,
		ReconcileRemoveNodes:        handleRemoveNode,
		ReconcileRemoveUnmanaged:    handleUnmanagedNodes,
		ReconcileAddNodes:           handleAddNode,
		ReconcileServerGroups:       handleServerGroups,
		ReconcileNodeServices:       handleNodeServices,
		ReconcileRebalance:          handleRebalance,
		ReconcileDeadMembers:        handleDeadMembers,
		ReconcileNotifyFinished:     handleNotifyFinished,
	}
)

type ReconcileMachine struct {
	runningPods  couchbaseutil.MemberSet
	knownNodes   couchbaseutil.MemberSet
	ejectNodes   couchbaseutil.MemberSet
	unknownNodes couchbaseutil.MemberSet
	couchbase    *couchbaseutil.ClusterStatus
	state        ReconcileState
}

func (r *ReconcileMachine) transitionState(to ReconcileState) {
	r.state = to
}

// step runs a single step of the state machine
func (r *ReconcileMachine) step(c *Cluster) error {
	f, err := reconcileFunctions.lookup(r.state)
	if err != nil {
		return err
	}
	if err := f(r, c); err != nil {
		return err
	}
	return nil
}

// done reports whether the reconciliation has completed
func (r *ReconcileMachine) done() bool {
	return r.state == ReconcileFinished
}

// exec runs the state machine until a finished condition or error
// is encountered
func (r *ReconcileMachine) exec(c *Cluster) error {
	for !r.done() {
		if err := r.step(c); err != nil {
			return err
		}
		if err := c.updateCRStatus(); err != nil {
			c.logger.Warnf("update CR status failed: %v", err)
		}
	}
	return nil
}

func handleInit(r *ReconcileMachine, c *Cluster) error {
	needsReconcile := false

	// If we have any cluster related issues like failed nodes or if the cluster
	// is not balanced or is currently rebalancing then we need to run reconcile.
	if !r.couchbase.ClusterHealthy() || !r.runningPods.Equal(c.members) {
		needsReconcile = true
	}

	// Update ready and unready members at end of reconciliation
	defer c.updateMemberStatusWithClusterInfo(r.couchbase)

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

	// TEMPORARY HACK
	if updated, err := k8sutil.WouldUpdateExposedFeatures(c.config.KubeCli, c.members, c.cluster); err != nil {
		return err
	} else if updated {
		needsReconcile = true
	}

	if updated, err := c.wouldReconcileMemberAlternateAddresses(); err != nil {
		return err
	} else if updated {
		needsReconcile = true
	}

	if updated, err := c.wouldReconcileServerGroups(); err != nil {
		return err
	} else if updated {
		needsReconcile = true
	}
	// TEMPORARY HACK END

	if !needsReconcile {
		c.status.SetBalancedCondition()
		r.transitionState(ReconcileFinished)
		return nil
	}

	// Catch all "needs rebalance" check handles things like auto-failover
	// which is outside of our control, or existing rebalance conditions
	// after a restart
	if r.couchbase.NeedsRebalance || r.couchbase.IsRebalancing {
		c.status.SetUnbalancedCondition()
		if err := c.updateCRStatus(); err != nil {
			// TODO: we should handle errors properly
			r.transitionState(ReconcileFinished)
			return nil
		}
	}

	// We are performing an action log the cluster status
	c.logStatus(r.couchbase)

	r.knownNodes.Append(r.couchbase.ActiveNodes)
	r.knownNodes.Append(r.couchbase.PendingAddNodes)
	r.transitionState(ReconcileUnknownMembers)

	return nil
}

// Unknown members are members who are currently running, but not part
// of the set of nodes the operator is tracking.
func handleUnknownMembers(r *ReconcileMachine, c *Cluster) error {
	// Safely balance out these illegal members
	r.unknownNodes = r.runningPods.Diff(c.members)
	r.runningPods = r.runningPods.Diff(r.unknownNodes)
	r.transitionState(ReconcileWarmupNodes)
	return nil
}

// If we have nodes that are warming up then we need to wait for them to finish
// before doing any cluster operations.
func handleWarmupNodes(r *ReconcileMachine, c *Cluster) error {
	if r.couchbase.WarmupNodes.Size() > 0 && r.couchbase.DownNodes.Empty() {
		c.logger.Info("Skipping reconcile loop because some nodes are warming up")
		r.transitionState(ReconcileNotifyFinished)
		return nil
	}
	r.transitionState(ReconcileRebalanceCheck)
	return nil
}

func handleRebalanceCheck(r *ReconcileMachine, c *Cluster) error {
	if r.couchbase.IsRebalancing {
		return fmt.Errorf("Skipping reconcile loop because the cluster is currently rebalancing")
	}
	r.transitionState(ReconcileDownNodes)
	return nil
}

func handleDownNodes(r *ReconcileMachine, c *Cluster) error {

	if r.couchbase.DownNodes.Size() > 0 {
		// Ensure the cluster is visibly unhealthy before triggering any events
		c.status.SetUnavailableCondition(r.couchbase.DownNodes.ClientURLs())
		c.updateCRStatus()

		// Get the duration that the node has been down from the status
		// and check if it has persistent volumes to be recovered
		for _, m := range r.couchbase.DownNodes {
			c.raiseEventCached(k8sutil.MemberDownEvent(m.Name, c.cluster))
			if _, ok := r.runningPods[m.Name]; ok {
				// If the pod was created in the last minute then it may be a down node
				// that was restarted and is still coming back online. If this is the
				// case then we may be able to delta recover it in the near future.
				if k8sutil.GetPodUptime(c.config.KubeCli, m.Namespace, m.Name) < downNodeThreshold {
					c.logger.Warnf("Down nodes `%s` pod was recently created, waiting to see if it rejoins the cluster, ", m.Name)
					continue
				}
			}
			if c.status.Members.Unready.Contains(m.Name) {
				if c.isPodRecoverable(m) {
					ts := c.status.Members.Unready.GetMember(m.Name).Ts()

					// Recover node if it has been down longer than auto-failover time
					elapsed, remainingTs := c.elapsedRecoveryDuration(ts)
					if elapsed {
						if err := c.recreatePod(m); err != nil {
							c.logger.Errorf("Node %s could not be recovered: %s", m.ClientURL(), err.Error())
						} else {
							c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name, c.cluster))
							c.logger.Infof("Recovering node %s", m.ClientURL())
							r.transitionState(ReconcileNotifyFinished)
							return nil
						}
					} else {
						c.logger.Errorf("Waiting for auto-failover of down node `%s`.  Automated recovery will begin after (%s) if auto-failover cannot be performed", m.Name, remainingTs)
					}
				} else {
					c.logger.Warnf("Waiting for auto-failover of down node `%s`", m.Name)
				}
			} else {
				c.logger.Errorf("Waiting for status of down node `%s` to become unready")
			}
		}

		return fmt.Errorf("Unable to reconcile cluster because some nodes are down")
	}

	c.status.SetReadyCondition()
	r.transitionState(ReconcileUnclusteredNodes)
	return nil
}

func handleUnclusteredNodes(r *ReconcileMachine, c *Cluster) error {
	for name, _ := range r.couchbase.UnclusteredNodes {
		if err := c.destroyMember(name); err != nil {
			return fmt.Errorf("Unable to remove unclustered node: %s", err.Error())
		}

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

	r.transitionState(ReconcileFailedAddNodes)
	return nil
}

func handleFailedAddNodes(r *ReconcileMachine, c *Cluster) error {
	// These nodes have been added, but the node failed before a rebalance could
	// start. If the node is configured to use volumes then we will recreate it,
	// otherwise, we will remove these nodes and re-add them in other pods later.
	for _, m := range r.couchbase.FailedAddNodes {
		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				c.logger.Errorf("Failed pending add node `%s` could not be recovered: %s", m.Name, err.Error())
			} else {
				c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name, c.cluster))
				return fmt.Errorf("recovering pending  add node %s", m.ClientURL())
			}
		}
		err := c.cancelAddMember(r.knownNodes, m)
		if err != nil {
			return fmt.Errorf("Unable to removed a failed pending add node: %s", err.Error())
		}
		c.clusterRemoveMember(m.Name)
		r.runningPods.Remove(m.Name)
	}

	r.transitionState(ReconcileAddBackNodes)
	return nil
}

// Add back failed nodes to cluster.
// Delta recover is performed for data nodes,
// otherwise a full recovery is performed
func handleAddBackNodes(r *ReconcileMachine, c *Cluster) error {
	for _, m := range r.couchbase.AddBackNodes {

		err := c.verifyMemberVolumes(m)
		if err != nil {
			c.logger.Warnf("Failed over node `%s` cannot be added back since it has unhealthy volumes: %v, removing, ", m.Name, err)
			r.ejectNodes.Add(m)
			r.runningPods.Remove(m.Name)
			c.raiseEvent(k8sutil.FailedAddBackNodeEvent(m.Name, c.cluster))
			break
		}

		// Set recovery type as delta for data nodes
		if sc := c.cluster.Spec.GetServerConfigByName(m.ServerConfig); sc != nil {
			deltaRecovery := false
			for _, svc := range sc.Services {
				if svc == "data" {
					deltaRecovery = true
					break
				}
			}

			var err error
			if deltaRecovery {
				c.logger.Infof("Add back node `%s` is being marked for delta recovery", m.Name)
				err = c.client.SetRecoveryTypeDelta(r.couchbase.ActiveNodes, m.HostURLPlaintext())
			} else {
				c.logger.Infof("Add back node `%s` is being marked for full recovery since it is not running the data service", m.Name)
				err = c.client.SetRecoveryTypeFull(r.couchbase.ActiveNodes, m.HostURLPlaintext())
			}

			if err != nil {
				c.logger.Infof("Unable to set recovery type for node %s,  node will be rebalanced out since it cannot be added back %v", m.Name, err)
				r.ejectNodes.Add(m)
				break
			} else {
				r.couchbase.NeedsRebalance = true
			}
		} else {
			c.logger.Infof("Add back node `%s` is missing from cluster spec: %s, removing", m.Name, m.ServerConfig)
			r.ejectNodes.Add(m)
			break
		}
	}

	r.transitionState(ReconcileFailedNodes)
	return nil
}

// Failed nodes can be recovered if the pod's volumes are healthy,
// otherwise the node is ejected from the cluster and it's node deleted.
func handleFailedNodes(r *ReconcileMachine, c *Cluster) error {
	for _, m := range r.couchbase.FailedNodes {
		c.logger.Infof("An auto-failover has taken place")
		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				c.logger.Errorf("node %s could not be recovered: %s", m.ClientURL(), err.Error())
			} else {
				c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name, c.cluster))
				return fmt.Errorf("recovering node %s", m.ClientURL())
			}
		}

		c.logger.Infof("planning removal of %s", m.ClientURL())
		r.ejectNodes.Add(m)
		r.runningPods.Remove(m.Name)
	}

	for _, name := range r.couchbase.FailedNodes.Names() {
		c.raiseEventCached(k8sutil.MemberFailedOverEvent(name, c.cluster))
	}

	r.transitionState(ReconcileServerConfigs)
	return nil
}

func handleUnknownServerConfigs(r *ReconcileMachine, c *Cluster) error {
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
	return nil
}

func handleRemoveNode(r *ReconcileMachine, c *Cluster) error {
	// Bookkeep the scaling information for only the affected server classes
	currentSize := 0
	desiredSize := 0

	serverSpecs := c.cluster.Spec.ServerSettings
	for _, serverSpec := range serverSpecs {
		// Check to see if we need to remove anything
		nodes := r.runningPods.GroupByServerConfig(serverSpec.Name)
		nodesToRemove := nodes.Size() - serverSpec.Size
		if nodesToRemove <= 0 {
			continue
		}

		// Update global scaling information
		currentSize += nodes.Size()
		desiredSize += serverSpec.Size

		// Schedule deletion based on server class
		for i := 0; i < nodesToRemove; i++ {
			server, err := c.scheduler.Delete(serverSpec.Name)
			if err != nil {
				return fmt.Errorf("Failed to schedule removal of member '%s': %v", serverSpec.Name, err)
			}
			r.knownNodes.Remove(server)
			r.ejectNodes.Add(c.members[server])
		}

	}

	// If we are performing any scaling update the status and request a rebalance
	// to eject the scheduled servers
	if currentSize != desiredSize {
		c.status.SetScalingDownCondition(currentSize, desiredSize)
		r.couchbase.NeedsRebalance = true
	}

	r.transitionState(ReconcileRemoveUnmanaged)
	return nil
}

func handleUnmanagedNodes(r *ReconcileMachine, c *Cluster) error {
	if len(r.couchbase.UnmanagedNodes) > 0 {
		r.couchbase.NeedsRebalance = true
	}

	r.transitionState(ReconcileAddNodes)
	return nil
}

func handleAddNode(r *ReconcileMachine, c *Cluster) error {
	serverSpecs := c.cluster.Spec.ServerSettings
	for _, serverSpec := range serverSpecs {
		addCount := 0
		nodes := r.runningPods.GroupByServerConfig(serverSpec.Name)
		for nodes.Size()+addCount < serverSpec.Size {
			originalSize := r.couchbase.ActiveNodes.Size() + r.couchbase.PendingAddNodes.Size()

			c.status.SetScalingUpCondition(originalSize, c.cluster.Spec.TotalSize())
			if err := c.updateCRStatus(); err != nil {
				c.logger.Warnf("Failed to update scale up condition")
			}

			r.couchbase.NeedsRebalance = true
			m, err := c.addMember(serverSpec)
			if err != nil {
				return fmt.Errorf("Failed to add new node to cluster: %v", err)
			}
			r.knownNodes.Add(m)
			r.runningPods.Add(m)
			addCount++
		}
	}

	r.transitionState(ReconcileServerGroups)
	return nil
}

// handleServerGroups moves nodes from their current server group into the one
// the pod is labelled as by the scheduler.  It occurs after node addition and
// balance in as alterations would trigger an additional rebalance otherwise.
func handleServerGroups(r *ReconcileMachine, c *Cluster) error {
	if updated, err := c.reconcileServerGroups(); err != nil {
		return err
	} else if updated {
		r.couchbase.NeedsRebalance = true
	}
	r.transitionState(ReconcileNodeServices)
	return nil
}

// handleNodeServices creates any services required to provide external connectivity
// before the node is balanaced in and starts serving bucket shards.  This is for the
// benefit of external clients such as xdcr which would not function until the
// balance in has completed otherwise.
func handleNodeServices(r *ReconcileMachine, c *Cluster) error {
	if err := c.reconcileExposedFeatures(); err != nil {
		return err
	}
	if err := c.reconcileMemberAlternateAddresses(); err != nil {
		return err
	}
	r.transitionState(ReconcileRebalance)
	return nil
}

func handleRebalance(r *ReconcileMachine, c *Cluster) error {
	if r.couchbase.NeedsRebalance {
		if err := c.rebalance(r.ejectNodes, r.couchbase.UnmanagedNodes); err != nil {
			// If rebalance error occured due to a node that could not be delta
			// recovered then it should be reconciled with FailedAddBack nodes
			if c.didDeltaRecoveryFail(err) {
				// TODO: K8S-530: verify that it is not actually possible add nodes
				//       or change server groups while nodes are in delta recovery
				c.logger.Errorf("Could not Rebalance because requested delta recovery is not possible. You probably added more nodes to the cluster or changed server groups configuration: %v", err)
				r.transitionState(ReconcileFailedAddBackNodes)
				return nil
			} else {
				return fmt.Errorf("Failed to rebalance: %s", err.Error())
			}
		}

		for _, toRemove := range r.ejectNodes {
			r.ejectNodes.Remove(toRemove.Name)
			r.runningPods.Remove(toRemove.Name)
		}
	}

	r.transitionState(ReconcileDeadMembers)
	return nil
}

// Attempt to add back nodes that cannot be rebalanced back into the
// cluster using delta recovery by changing to full recovery type
func handleFailedAddBackNodes(r *ReconcileMachine, c *Cluster) error {

	addNodes := r.couchbase.AddBackNodes.Copy()
	addNodes.Append(r.couchbase.PendingAddNodes)
	for _, m := range addNodes {
		isDelta, err := c.client.IsRecoveryTypeDelta(m)
		if err != nil {
			c.logger.Errorf("Failed to add back node `%s` because recovery type could not be determined: %v", m.Name, err)
			r.transitionState(ReconcileNotifyFinished)
			return nil
		}
		if isDelta {
			c.logger.Warnf("Changing recovery type from `delta` to `full` to recover node `%s`", m.Name)
			err := c.client.SetRecoveryTypeFull(r.couchbase.ActiveNodes, m.HostURLPlaintext())
			if err != nil {
				c.logger.Warnf("Unable to set failed add back node `%s` recovery type to `full`: %v", m.Name, err)
				c.raiseEvent(k8sutil.FailedAddBackNodeEvent(m.Name, c.cluster))
				return err
			}
		} else {
			c.logger.Warnf("Failed add back node `%s` recovery mode is already set to `full`", m.Name)
		}
	}

	r.transitionState(ReconcileDeadMembers)
	return nil
}

// Dead members are members that the operator is tracking, but do not have a
// corresponding running pod.
func handleDeadMembers(r *ReconcileMachine, c *Cluster) error {
	dead := c.members.Diff(r.runningPods)
	for name, _ := range dead {
		if err := c.destroyMember(name); err != nil {
			return fmt.Errorf("Failed to remove dead members: %s", err.Error())
		}
	}

	for name, _ := range r.unknownNodes {
		if err := c.removePod(name); err != nil {
			return fmt.Errorf("Failed to remove unknown member: %s", err.Error())
		}
	}

	r.transitionState(ReconcileNotifyFinished)
	return nil
}

func handleNotifyFinished(r *ReconcileMachine, c *Cluster) error {
	c.logger.Info("reconcile finished")
	r.transitionState(ReconcileFinished)
	return nil
}
