package cluster

import (
	"fmt"
	"sort"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

var ErrReconcileInhibited = fmt.Errorf("reconcile was blocked from running")

// ReconcileState is an enumeration used to define the current
// node in the state machine.
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
	ReconcileUpgradeNode
	ReconcileRemoveNodes
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
// it must transition the state if necessary, or return an error.
type reconcileFunc func(*ReconcileMachine, *Cluster) error

// reconcileFuncMap maps from a reconciliation state to a reconcile function.
type reconcileFuncMap map[ReconcileState]reconcileFunc

// lookup finds the matching reconcile function for a specific state and returns
// a reference to it, or an error.
func (r reconcileFuncMap) lookup(state ReconcileState) (reconcileFunc, error) {
	f, ok := r[state]
	if !ok {
		return nil, fmt.Errorf("%w: invalid reconcile state", errors.NewStackTracedError(errors.ErrInternalError))
	}

	return f, nil
}

var (
	reconcileFunctions = reconcileFuncMap{
		ReconcileInit:             handleInit,
		ReconcileUnknownMembers:   handleUnknownMembers,
		ReconcileRebalanceCheck:   handleRebalanceCheck,
		ReconcileWarmupNodes:      handleWarmupNodes,
		ReconcileDownNodes:        handleDownNodes,
		ReconcileUnclusteredNodes: handleUnclusteredNodes,
		ReconcileFailedAddNodes:   handleFailedAddNodes,
		ReconcileAddBackNodes:     handleAddBackNodes,
		ReconcileFailedNodes:      handleFailedNodes,
		ReconcileServerConfigs:    handleUnknownServerConfigs,
		ReconcileUpgradeNode:      handleUpgradeNode,
		ReconcileRemoveNodes:      handleRemoveNode,
		ReconcileAddNodes:         handleAddNode,
		ReconcileServerGroups:     handleServerGroups,
		ReconcileNodeServices:     handleNodeServices,
		ReconcileRebalance:        handleRebalance,
		ReconcileDeadMembers:      handleDeadMembers,
		ReconcileNotifyFinished:   handleNotifyFinished,
	}
)

// This is a temporary measure to maintain the interface.  This is all smell code
// and will probably get killed off fairly soon.
type MemberState struct {
	NodeStateMap     NodeStateMap
	managedNodes     couchbaseutil.MemberSet
	ActiveNodes      couchbaseutil.MemberSet
	PendingAddNodes  couchbaseutil.MemberSet
	AddBackNodes     couchbaseutil.MemberSet
	FailedAddNodes   couchbaseutil.MemberSet
	WarmupNodes      couchbaseutil.MemberSet
	DownNodes        couchbaseutil.MemberSet
	FailedNodes      couchbaseutil.MemberSet
	UnclusteredNodes couchbaseutil.MemberSet
	IsRebalancing    bool
	NeedsRebalance   bool
}

func (m *MemberState) ClusterHealthy() bool {
	// All nodes must be active.
	badMembers := couchbaseutil.NewMemberSet()
	badMembers.Merge(m.PendingAddNodes)
	badMembers.Merge(m.AddBackNodes)
	badMembers.Merge(m.FailedAddNodes)
	badMembers.Merge(m.WarmupNodes)
	badMembers.Merge(m.DownNodes)
	badMembers.Merge(m.FailedNodes)
	badMembers.Merge(m.UnclusteredNodes)

	if !badMembers.Empty() {
		return false
	}

	// Must be balanced and not rebalancing.
	return !m.IsRebalancing && !m.NeedsRebalance
}

// nodeStatus is an intermediate data structured used to log node status information.
type nodeStatus struct {
	name    string
	version string
	class   string
	managed bool
	state   string
}

func (m *MemberState) LogStatus(cluster string) {
	// A cluster is either balanced or not
	balance := "balanced"
	if m.NeedsRebalance {
		balance = "unbalanced"
	}

	log.Info("Cluster status", "cluster", cluster, "balance", balance, "rebalancing", m.IsRebalancing)

	// Sort the names so it's easier to grok
	names := []string{}

	for name := range m.managedNodes {
		names = append(names, name)
	}

	sort.Strings(names)

	// Collect all the node statuses, as we process check the string lengths for
	// pretty tabulation
	statuses := []nodeStatus{}

	for _, name := range names {
		// All members are managed
		// And they will exist in one state
		state := m.NodeStateMap[name]

		// Buffer up the status entry
		class := m.managedNodes[name].Config()
		version := m.managedNodes[name].Version()

		status := nodeStatus{
			name:    name,
			version: version,
			class:   class,
			managed: true,
			state:   string(state),
		}
		statuses = append(statuses, status)
	}

	for _, status := range statuses {
		log.Info("Node status", "cluster", cluster, "name", status.name, "version", status.version, "class", status.class, "managed", status.managed, "status", status.state)
	}
}

type ReconcileMachine struct {
	runningPods   couchbaseutil.MemberSet
	knownNodes    couchbaseutil.MemberSet
	newNodes      couchbaseutil.MemberSet
	ejectNodes    couchbaseutil.MemberSet
	unknownNodes  couchbaseutil.MemberSet
	couchbase     *MemberState
	state         ReconcileState
	removeVolumes map[string]bool // map of nodes with volumes to remove if deleted
}

func (c *Cluster) newReconcileMachine() (*ReconcileMachine, error) {
	status, err := c.GetStatus()
	if err != nil {
		return nil, err
	}

	state := &MemberState{
		NodeStateMap:     status.NodeStates,
		managedNodes:     c.members,
		ActiveNodes:      couchbaseutil.NewMemberSet(),
		PendingAddNodes:  couchbaseutil.NewMemberSet(),
		AddBackNodes:     couchbaseutil.NewMemberSet(),
		FailedAddNodes:   couchbaseutil.NewMemberSet(),
		WarmupNodes:      couchbaseutil.NewMemberSet(),
		DownNodes:        couchbaseutil.NewMemberSet(),
		FailedNodes:      couchbaseutil.NewMemberSet(),
		UnclusteredNodes: couchbaseutil.NewMemberSet(),
		IsRebalancing:    status.Balancing,
		NeedsRebalance:   !status.Balanced,
	}

	for name, nodeState := range status.NodeStates {
		switch nodeState {
		case NodeStateActive:
			state.ActiveNodes.Add(c.members[name])
		case NodeStatePendingAdd:
			state.PendingAddNodes.Add(c.members[name])
		case NodeStateFailedAdd:
			state.FailedAddNodes.Add(c.members[name])
		case NodeStateWarmup:
			state.WarmupNodes.Add(c.members[name])
		case NodeStateDown:
			state.DownNodes.Add(c.members[name])
		case NodeStateFailed:
			state.FailedNodes.Add(c.members[name])
		case NodeStateAddBack:
			state.AddBackNodes.Add(c.members[name])
		}
	}

	for name, member := range c.members {
		if _, ok := status.NodeStates[name]; !ok {
			state.UnclusteredNodes.Add(member)
		}
	}

	fsm := &ReconcileMachine{
		runningPods:   podsToMemberSet(c.k8s.Pods.List()),
		knownNodes:    couchbaseutil.NewMemberSet(),
		newNodes:      couchbaseutil.NewMemberSet(),
		ejectNodes:    couchbaseutil.NewMemberSet(),
		couchbase:     state,
		state:         ReconcileInit,
		removeVolumes: make(map[string]bool),
	}

	return fsm, nil
}

func (r *ReconcileMachine) transitionState(to ReconcileState) {
	r.state = to
}

// step runs a single step of the state machine.
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

// done reports whether the reconciliation has completed.
func (r *ReconcileMachine) done() bool {
	return r.state == ReconcileFinished
}

// exec runs the state machine until a finished condition or error
// is encountered.
func (r *ReconcileMachine) exec(c *Cluster) error {
	for !r.done() {
		if err := r.step(c); err != nil {
			return err
		}

		if err := c.updateCRStatus(); err != nil {
			log.Error(err, "Cluster status update failed", "cluster", c.namespacedName())
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

	// If we have any server specs that are not properly sized then we need to
	// reconcile the nodes.
	serverSpecs := c.cluster.Spec.Servers
	for _, serverSpec := range serverSpecs {
		nodes := r.couchbase.ActiveNodes.GroupByServerConfig(serverSpec.Name)
		if nodes.Size() != serverSpec.Size {
			needsReconcile = true
		}
	}

	// If we have any running pods that are not part of one of the current
	// server configs then we need to reconcile so they can be removed.
	for _, m := range r.runningPods {
		found := false

		for _, serverSpec := range serverSpecs {
			if m.Config() == serverSpec.Name {
				found = true
			}
		}

		if !found {
			needsReconcile = true
		}
	}

	// Reset any timeout counters if nodes have recovered.
	for name := range r.couchbase.ActiveNodes {
		delete(c.recoveryTime, name)
	}

	// When nodes are being removed, the default behavior is to remove volumes
	// unless user is initiating the removal and only logs are mounted
	for name := range c.members {
		r.removeVolumes[name] = !c.memberHasLogVolumes(name)
	}

	if updated, err := c.wouldReconcileServerGroups(); err != nil {
		return err
	} else if updated {
		needsReconcile = true
	}

	if candidate, _, _, err := c.needsUpgrade(); err != nil {
		return err
	} else if candidate != nil {
		needsReconcile = true
	}
	// TEMPORARY HACK END

	if !needsReconcile {
		c.cluster.Status.SetBalancedCondition()
		r.transitionState(ReconcileFinished)

		return nil
	}

	// Catch all "needs rebalance" check handles things like auto-failover
	// which is outside of our control, or existing rebalance conditions
	// after a restart
	if r.couchbase.NeedsRebalance || r.couchbase.IsRebalancing {
		c.cluster.Status.SetUnbalancedCondition()

		if err := c.updateCRStatus(); err != nil {
			// TODO: we should handle errors properly
			r.transitionState(ReconcileFinished)
			return nil
		}
	}

	// We are performing an action log the cluster status
	c.logStatus(r.couchbase)

	r.knownNodes.Merge(r.couchbase.ActiveNodes)
	r.knownNodes.Merge(r.couchbase.PendingAddNodes)
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
		log.Info("Pods warming up, skipping", "cluster", c.namespacedName())
		r.transitionState(ReconcileNotifyFinished)

		return nil
	}

	r.transitionState(ReconcileRebalanceCheck)

	return nil
}

func handleRebalanceCheck(r *ReconcileMachine, c *Cluster) error {
	if r.couchbase.IsRebalancing {
		// If rebalance isn't actually active then we should stop it
		running, err := c.IsRebalanceActive(c.readyMembers())
		if err != nil {
			log.Error(err, "Rebalance status collection failed", "cluster", c.namespacedName())
		} else if !running {
			// stop rebalance
			if err := couchbaseutil.StopRebalance().On(c.api, c.readyMembers()); err != nil {
				log.Error(err, "Rebalance cancellation failed", "cluster", c.namespacedName())
				return err
			}

			log.Info("Rebalance cancelled", "cluster", c.namespacedName())
			r.transitionState(ReconcileDownNodes)

			return nil
		}

		return fmt.Errorf("%w: cluster is currently rebalancing", errors.NewStackTracedError(ErrReconcileInhibited))
	}

	r.transitionState(ReconcileDownNodes)

	return nil
}

func handleDownNodes(r *ReconcileMachine, c *Cluster) error {
	if r.couchbase.DownNodes.Size() > 0 {
		// Ensure the cluster is visibly unhealthy before triggering any events
		c.cluster.Status.SetUnavailableCondition(r.couchbase.DownNodes.Names())

		if err := c.updateCRStatus(); err != nil {
			return err
		}
	}

	// Alaways flag nodes down, the observed behaviour can change based on whether
	// a recover timer has expired or not.
	for name := range r.couchbase.DownNodes {
		c.raiseEventCached(k8sutil.MemberDownEvent(name, c.cluster))
	}

	pendingFailovers := 0

	// Get the duration that the node has been down from the status
	// and check if it has persistent volumes to be recovered
	for name, m := range r.couchbase.DownNodes {
		if _, ok := r.runningPods[name]; ok {
			// If the pod was created in the last minute then it may be a down node
			// that was restarted and is still coming back online. If this is the
			// case then we may be able to delta recover it in the near future.
			if k8sutil.GetPodUptime(c.k8s, name) < downNodeThreshold {
				log.Info("Recently created pod down, waiting", "cluster", c.namespacedName())
				continue
			}
		}

		// Ephemeral clusters are handled either automatically by server or
		// manually by the user.
		if !c.isPodRecoverable(m) && c.cluster.GetRecoveryPolicy() == couchbasev2.PrioritzeDataIntegrity {
			return fmt.Errorf("%w: pod down, waiting for auto-failover on cluster: %s, pod: %s", errors.NewStackTracedError(ErrReconcileInhibited), c.namespacedName(), m.Name())
		}

		// If we've not seen this node down yet, then take note of when we should
		// begin manual recovery.
		recoveryTime, ok := c.recoveryTime[name]
		if !ok {
			timeout := c.cluster.Spec.ClusterSettings.AutoFailoverTimeout
			recoveryTime = time.Now().Add(timeout.Duration).Add(30 * time.Second)
			c.recoveryTime[name] = recoveryTime
		}

		// If the timeout has yet to expire then continue.
		if recoveryTime.After(time.Now()) {
			log.Info("Pod down, waiting for auto-failover", "cluster", c.namespacedName(), "name", name, "recovery_in", time.Until(recoveryTime))

			pendingFailovers++

			continue
		}

		// If this is an ephemeral pod, then let volume backed ones take priority.
		if !c.isPodRecoverable(m) {
			continue
		}

		c.logFailedMember("Node failed", name)

		// Timeout has expired, recreate the pod.
		if err := c.recreatePod(m); err != nil {
			return fmt.Errorf("pod recovery failed for member %s: %w", name, err)
		}

		log.Info("Pod recovering", "cluster", c.namespacedName(), "name", name)

		c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))
		delete(c.recoveryTime, name)
		r.transitionState(ReconcileNotifyFinished)

		return nil
	}

	// We are still waiting for failovers to occur, don't allow the rest of the reconcile to
	// happen.
	if pendingFailovers != 0 {
		return fmt.Errorf("%w: waiting for pod failover", errors.NewStackTracedError(ErrReconcileInhibited))
	}

	// By this point we know:
	// * Things that cannot be recovered (have no PVC) and the user is demanding data
	//   integrity, and have been rejected.
	// * Things that haven't timed out yet, and have been rejected.
	// * Things that can be recovered (have a PVC) have been, these are enforced to
	//   be stateful services that require persistence e.g. data, index, analytics.
	// Leaving us with:
	// * Nothing to do
	// * Stuff that server thinks cannot be failed over e.g. a bunch of query nodes.
	// Give the system a helping hand...
	if len(r.couchbase.DownNodes) > 0 && c.cluster.GetRecoveryPolicy() == couchbasev2.PrioritzeUptime {
		log.Info("Forcing failover of unrecoverable nodes", "cluster", c.namespacedName())

		otpNodes := couchbaseutil.OTPNodeList{}

		for _, member := range r.couchbase.DownNodes {
			log.Info("Failing over node", "cluster", c.namespacedName(), "name", member.Name)

			otpNodes = append(otpNodes, member.GetOTPNode())
		}

		if err := couchbaseutil.Failover(otpNodes, true).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		r.transitionState(ReconcileNotifyFinished)

		return nil
	}

	c.cluster.Status.SetReadyCondition()
	r.transitionState(ReconcileUnclusteredNodes)

	return nil
}

func handleUnclusteredNodes(r *ReconcileMachine, c *Cluster) error {
	for name := range r.couchbase.UnclusteredNodes {
		removeVolumes := shouldRemoveVolumes(r, c, name)
		if err := c.destroyMember(name, removeVolumes); err != nil {
			return fmt.Errorf("unable to remove unclustered node: %w", err)
		}

		log.Info("Pod unclustered, deleting", "cluster", c.namespacedName(), "name", name)

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
	for name, m := range r.couchbase.FailedAddNodes {
		c.logFailedMember("Node failed", name)

		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				log.Error(err, "Pending add pod cannot be recovered", "cluster", c.namespacedName(), "name", name)
				r.transitionState(ReconcileNotifyFinished)

				return nil
			}

			c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))

			return fmt.Errorf("%w: recovering pending add node %s", errors.NewStackTracedError(ErrReconcileInhibited), name)
		}

		err := c.cancelAddMember(m)
		if err != nil {
			return fmt.Errorf("unable to remove a failed pending add node: %w", err)
		}

		if err := c.clusterRemoveMember(name); err != nil {
			return err
		}

		r.runningPods.Remove(name)
	}

	r.transitionState(ReconcileAddBackNodes)

	return nil
}

// Add back failed nodes to cluster.
// Delta recover is performed for data nodes,
// otherwise a full recovery is performed.
func handleAddBackNodes(r *ReconcileMachine, c *Cluster) error {
	for name, m := range r.couchbase.AddBackNodes {
		err := c.verifyMemberVolumes(m)
		if err != nil {
			log.Error(err, "Failed pod cannot be recovered, volumes unhealthy", "cluster", c.namespacedName(), "name", name)

			r.ejectNodes.Add(m)
			r.runningPods.Remove(name)
			c.raiseEvent(k8sutil.FailedAddBackNodeEvent(name, c.cluster))

			break
		}

		// Set recovery type as delta for data nodes
		if sc := c.cluster.Spec.GetServerConfigByName(m.Config()); sc != nil {
			deltaRecovery := false

			for _, svc := range sc.Services {
				if svc == "data" {
					deltaRecovery = true
					break
				}
			}

			recoveryType := couchbaseutil.RecoveryTypeFull

			if deltaRecovery {
				recoveryType = couchbaseutil.RecoveryTypeDelta
			}

			log.Info("Setting recovery type", "cluster", c.namespacedName(), "name", name, "type", recoveryType)

			if err := couchbaseutil.SetRecoveryType(m.GetOTPNode(), recoveryType).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			r.couchbase.NeedsRebalance = true

			r.knownNodes.Add(m)
		} else {
			log.Info("Add back pod not in the specification, deleting", "cluster", c.namespacedName(), "name", name, "class", m.Config())

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
	for _, name := range r.couchbase.FailedNodes.Names() {
		c.raiseEventCached(k8sutil.MemberFailedOverEvent(name, c.cluster))
	}

	for name, m := range r.couchbase.FailedNodes {
		log.Info("Pods failed over", "cluster", c.namespacedName())

		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", name, "reason", err)

				r.transitionState(ReconcileNotifyFinished)

				return nil
			}

			c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))

			return fmt.Errorf("%w: recovering node %s", errors.NewStackTracedError(ErrReconcileInhibited), name)
		}

		log.Info("Pod failed, deleting", "cluster", c.namespacedName(), "name", name)

		r.ejectNodes.Add(m)
		r.runningPods.Remove(name)
	}

	r.transitionState(ReconcileServerConfigs)

	return nil
}

func handleUnknownServerConfigs(r *ReconcileMachine, c *Cluster) error {
	// If a server configuration was deleted in a spec update then we will clean
	// up all of the nodes from that server config here.
	for name, m := range r.runningPods {
		found := false

		for _, serverSpec := range c.cluster.Spec.Servers {
			if m.Config() == serverSpec.Name {
				found = true
				break
			}
		}

		if !found {
			log.Info("Pod not in the specification, deleting", "cluster", c.namespacedName(), "name", name, "class", m.Config())

			r.couchbase.NeedsRebalance = true
			r.knownNodes.Remove(name)
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

	serverSpecs := c.cluster.Spec.Servers
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
				return fmt.Errorf("failed to schedule removal of member '%s': %w", serverSpec.Name, err)
			}

			r.knownNodes.Remove(server)
			r.ejectNodes.Add(c.members[server])

			if c.memberHasLogVolumes(server) {
				// Remove log volumes when user initiated scale down
				r.removeVolumes[server] = true
			}
		}
	}

	// If we are performing any scaling update the status and request a rebalance
	// to eject the scheduled servers
	if currentSize != desiredSize {
		c.cluster.Status.SetScalingDownCondition(currentSize, desiredSize)

		r.couchbase.NeedsRebalance = true
	}

	r.transitionState(ReconcileAddNodes)

	return nil
}

func handleAddNode(r *ReconcileMachine, c *Cluster) error {
	serverSpecs := c.cluster.Spec.Servers
	for _, serverSpec := range serverSpecs {
		addCount := 0

		nodes := r.runningPods.GroupByServerConfig(serverSpec.Name)
		for nodes.Size()+addCount < serverSpec.Size {
			originalSize := r.couchbase.ActiveNodes.Size() + r.couchbase.PendingAddNodes.Size()

			c.cluster.Status.SetScalingUpCondition(originalSize, c.cluster.Spec.TotalSize())

			if err := c.updateCRStatus(); err != nil {
				log.Error(err, "Cluster status update failed", "cluster", c.namespacedName())
			}

			r.couchbase.NeedsRebalance = true

			m, err := c.addMember(serverSpec)
			if err != nil {
				log.Error(err, "Pod addition to cluster failed", "cluster", c.namespacedName())
				return err
			}

			r.knownNodes.Add(m)
			r.runningPods.Add(m)
			r.newNodes.Add(m)
			addCount++
		}
	}

	r.transitionState(ReconcileUpgradeNode)

	return nil
}

func handleUpgradeNode(r *ReconcileMachine, c *Cluster) error {
	// Something is broken, let that get fixed up first.
	if r.couchbase.NeedsRebalance {
		r.transitionState(ReconcileServerGroups)
		return nil
	}

	// Nothing to do, move along.
	candidate, targetCount, diff, err := c.needsUpgrade()
	if err != nil {
		return err
	}

	if candidate == nil {
		r.transitionState(ReconcileServerGroups)
		return nil
	}

	log.Info("Pod upgrading", "cluster", c.namespacedName(), "name", candidate.Name(), "diff", diff)

	status := &couchbasev2.UpgradeStatus{
		TargetCount: targetCount,
		TotalCount:  len(c.members),
	}

	// Flag that an upgrade is in action, validation will use this to control what
	// resource modifications are allowed.
	if err := c.reportUpgrade(status); err != nil {
		return err
	}

	// Remove the candidate from the scheduler.
	if err := c.scheduler.Upgrade(candidate.Config(), candidate.Name()); err != nil {
		return err
	}

	// Grab the server class.
	class := c.cluster.Spec.GetServerConfigByName(candidate.Config())
	if class == nil {
		return fmt.Errorf("upgrade unable to determine server class %s for member %s: %w", candidate.Name(), candidate.Config(), errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// Add the new member.
	member, err := c.addMember(*class)
	if err != nil {
		return fmt.Errorf("upgrade failed to add new node to cluster: %w", err)
	}

	// Update book keeping
	r.knownNodes.Add(member)
	r.runningPods.Add(member)
	r.newNodes.Add(member)
	r.knownNodes.Remove(candidate.Name())
	r.ejectNodes.Add(candidate)
	r.couchbase.NeedsRebalance = true

	if c.memberHasLogVolumes(candidate.Name()) {
		r.removeVolumes[candidate.Name()] = true
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
	if err := c.reconcilePodServices(); err != nil {
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
		// Eject nodes that we want to discard.
		eject := r.ejectNodes.OTPNodes()

		if err := c.rebalance(r.knownNodes, eject); err != nil {
			// If rebalance error occurred due to a node that could not be delta
			// recovered then it should be set to a full recovery type.  The state
			// will have changed from add-back to pending-add, so we won't loop
			// forever.
			addNodes := r.couchbase.AddBackNodes.Copy()
			addNodes.Merge(r.couchbase.PendingAddNodes)

			deltaNodes := couchbaseutil.NewMemberSet()

			for _, m := range addNodes {
				info := &couchbaseutil.ClusterInfo{}
				if err := couchbaseutil.GetPoolsDefault(info).On(c.api, c.readyMembers()); err != nil {
					log.Error(err, "Pod add-back failed, unable to determine recovery type", "cluster", c.namespacedName(), "name", m.Name)
					return err
				}

				node, err := info.GetNode(m.GetHostName())
				if err != nil {
					log.Error(err, "Pod add-back failed, unable to determine recovery type", "cluster", c.namespacedName(), "name", m.Name)
					return err
				}

				if node.RecoveryType == couchbaseutil.RecoveryTypeDelta {
					deltaNodes.Add(m)
				}
			}

			if len(deltaNodes) != 0 {
				log.Info("Pod add-back failed, forcing full recovery")

				for name, m := range deltaNodes {
					if err := couchbaseutil.SetRecoveryType(m.GetOTPNode(), couchbaseutil.RecoveryTypeFull).On(c.api, c.readyMembers()); err != nil {
						log.Error(err, "Pod add-back, recovery type update failed", "cluster", c.namespacedName(), "name", name)

						c.raiseEvent(k8sutil.FailedAddBackNodeEvent(name, c.cluster))

						return err
					}
				}

				return fmt.Errorf("%w: rebalance failed, forcing full recovery", errors.NewStackTracedError(ErrReconcileInhibited))
			}

			return fmt.Errorf("failed to rebalance: %w", err)
		}

		for name := range r.ejectNodes {
			r.ejectNodes.Remove(name)
			r.runningPods.Remove(name)
		}
	}

	r.transitionState(ReconcileDeadMembers)

	return nil
}

// Dead members are members that the operator is tracking, but do not have a
// corresponding running pod.
func handleDeadMembers(r *ReconcileMachine, c *Cluster) error {
	dead := c.members.Diff(r.runningPods)

	for name := range r.couchbase.FailedNodes {
		c.logFailedMember("Node failed", name)
	}

	for name := range dead {
		removeVolumes := shouldRemoveVolumes(r, c, name)
		if err := c.destroyMember(name, removeVolumes); err != nil {
			return fmt.Errorf("failed to remove dead members: %w", err)
		}
	}

	for name := range r.unknownNodes {
		removeVolumes := shouldRemoveVolumes(r, c, name)
		if err := c.removePod(name, removeVolumes); err != nil {
			return fmt.Errorf("failed to remove unknown member: %w", err)
		}
	}

	r.transitionState(ReconcileNotifyFinished)

	return nil
}

func handleNotifyFinished(r *ReconcileMachine, c *Cluster) error {
	log.Info("Reconcile completed", "cluster", c.namespacedName())

	r.transitionState(ReconcileFinished)

	return nil
}

// Check if volumes should be removed with Pod based on reconcile status.
func shouldRemoveVolumes(r *ReconcileMachine, c *Cluster, server string) bool {
	removeVolumes, ok := r.removeVolumes[server]
	if !ok {
		// If decision to remove volume is unset then only
		// remove if it's not a log volume
		return !c.memberHasLogVolumes(server)
	}

	return removeVolumes
}
