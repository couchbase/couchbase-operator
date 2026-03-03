/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

var ErrReconcileInhibited = fmt.Errorf("reconcile was blocked from running")

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
	// clusteredMembers is the set of members we have some resources for and Couchbase knows
	// about.  We add to/remove from this as the machine operates.  At any point in time this
	// reflects the members we want to be clustered when a rebalance is called.
	clusteredMembers couchbaseutil.MemberSet

	// runningMembers is the subset of clustered members with a running pod.
	runningMembers couchbaseutil.MemberSet

	// ejectMembers are the nodes that we wish to kick out of the cluster when we call
	// if and when we rebalance.
	ejectMembers couchbaseutil.MemberSet

	// unclusteredMembers members Couchbase knows about but we have no resource for
	// so they need ejecting and deleting.  They are treated separately from ejectMembers
	// because there is no book keeping to clean up.
	// TODO: is this true?  We reinitialize the cluster member state straight after this
	// FSM is called.
	unclusteredMembers couchbaseutil.MemberSet

	// needsRebalance records whether we think Couchbase Server will require a rebalance.
	// We could just let it report this fact and we take action in the next iteration, but
	// for historical reasons (and perhaps performance), do this manually.
	needsRebalance bool

	// couchbase records the per-member Couchbase node state aka its view of the world
	// e.g. are nodes down, failed etc.
	couchbase *MemberState

	// preserveVolumes is set if the member is down due to something out of the
	// user's control e.g. Server crashed, and it's using log volumes.
	preserveVolumes map[string]interface{}

	// abortReason when set stops the reconciler in its tracks and echos out the message.
	abortReason string

	// c caches the internal Couchbase cluster state for easy access.
	c *Cluster

	// logged is used as a flag to say we have already lazily logged the cluster state.
	// We only do this once to prevent spam, and only once we know we will take an action
	// that affects cluster topology.
	logged bool
}

func (r *ReconcileMachine) logState() {
	log.V(0).Info("reconciler", "clustered", r.clusteredMembers.Names(), "running", r.runningMembers.Names(), "eject", r.ejectMembers.Names(), "unclustered", r.unclusteredMembers.Names(), "rebalance", r.needsRebalance)
}

// addMember simulates creating and clustering a new member.
func (r *ReconcileMachine) addMember(m couchbaseutil.Member) {
	r.clusteredMembers.Add(m)
	r.runningMembers.Add(m)
	r.needsRebalance = true

	r.logState()
}

// removeMember simulates removing a current member.  This is called as the result
// of some external/environmental stimulus, in this case we need to preserve log
// volumes.
func (r *ReconcileMachine) removeMember(m couchbaseutil.Member) {
	r.clusteredMembers.Remove(m.Name())
	r.runningMembers.Remove(m.Name())
	r.ejectMembers.Add(m)
	r.needsRebalance = true

	if r.c.memberHasLogVolumes(m.Name()) {
		if r.preserveVolumes == nil {
			r.preserveVolumes = map[string]interface{}{}
		}

		r.preserveVolumes[m.Name()] = nil
	}

	r.logState()
}

// removeMemberUser simulates removing a current member.  This is called as the result
// of a user initiated action, e.g. scale down.  In this case we want to purge log volumes.
func (r *ReconcileMachine) removeMemberUser(m couchbaseutil.Member) {
	r.clusteredMembers.Remove(m.Name())
	r.runningMembers.Remove(m.Name())
	r.ejectMembers.Add(m)
	r.needsRebalance = true

	r.logState()
}

// removeMemberNoEject simulates removing a current member where no ejection is necessary.
func (r *ReconcileMachine) removeMemberNoEject(m couchbaseutil.Member) {
	r.clusteredMembers.Remove(m.Name())
	r.runningMembers.Remove(m.Name())

	r.logState()
}

// abort causes non-fatal termination from the runloop.
func (r *ReconcileMachine) abort(reason string) {
	r.abortReason = reason
}

// log prints out logs when we know for sure a topology change is required.
// This is a "singleton" and will only trigger once per iteration to avoid
// spamming the logs.
func (r *ReconcileMachine) log() {
	if r.logged {
		return
	}

	r.logged = true

	r.c.logStatus(r.couchbase)
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
		// c.members contains all members we know about from Kubernetes or from
		// Couchbase server.  By removing all the ones that Couchbase doesn't know
		// about we get the current set of things in the Couchbase cluster.
		clusteredMembers: c.members.Diff(state.UnclusteredNodes),

		// By intersecting all known members with the set of members pods we
		// get a set of members that we know are running (in some capacity) and
		// can be further interrogated for state.
		runningMembers: c.members.Intersect(podsToMemberSet(c.getClusterPods())),

		// This starts empty, we will populate it as we move through the manchine.
		ejectMembers: couchbaseutil.NewMemberSet(),

		unclusteredMembers: state.UnclusteredNodes.Copy(),

		needsRebalance: state.NeedsRebalance,

		couchbase: state,

		c: c,
	}

	// Reset any timeout counters if nodes have recovered.
	for name := range state.ActiveNodes {
		delete(c.recoveryTime, name)
	}

	return fsm, nil
}

// exec runs the state machine until a finished condition or error
// is encountered.
func (r *ReconcileMachine) exec(c *Cluster) (bool, error) {
	reconcileFunctions := []func(*ReconcileMachine, *Cluster) error{
		(*ReconcileMachine).handleRebalanceCheck,
		(*ReconcileMachine).handleWarmupNodes,
		(*ReconcileMachine).handleDownNodes,
		(*ReconcileMachine).handleUnclusteredNodes,
		(*ReconcileMachine).handleFailedAddNodes,
		(*ReconcileMachine).handleAddBackNodes,
		(*ReconcileMachine).handleFailedNodes,
		(*ReconcileMachine).handleUnknownServerConfigs,
		(*ReconcileMachine).handleVolumeExpansion,
		(*ReconcileMachine).handleUpgradeNode,
		(*ReconcileMachine).handleRemoveNode,
		(*ReconcileMachine).handleAddNode,
		(*ReconcileMachine).handleServerGroups,
		(*ReconcileMachine).handleNodeServices,
		(*ReconcileMachine).handleAutoscaleServerConfigs,
		(*ReconcileMachine).handleRebalance,
		(*ReconcileMachine).handleDeadMembers,
		(*ReconcileMachine).handleNotifyFinished,
	}

	for i := 0; i < len(reconcileFunctions); i++ {
		if err := reconcileFunctions[i](r, c); err != nil {
			return false, err
		}

		if r.abortReason != "" {
			log.Info("Aborting topology reconcile", "cluster", c.namespacedName(), "reason", r.abortReason)
			return true, nil
		}

		if err := c.updateCRStatus(); err != nil {
			log.Error(err, "Cluster status update failed", "cluster", c.namespacedName())
		}
	}

	return false, nil
}

// If we have nodes that are warming up then we need to wait for them to finish
// before doing any cluster operations.  The one exception is for down nodes
// where we let this through in the hope that pod recovery will save the day.
// Q: do we need a way to stop further reconcile, as this is likely to fail given
// the unstable nature of the cluster and litter the logs with stuff.
func (r *ReconcileMachine) handleWarmupNodes(_ *Cluster) error {
	if r.couchbase.WarmupNodes.Size() > 0 && r.couchbase.DownNodes.Empty() {
		// SM: So, this and a lot of others causes silent skipping of topology changes and
		// then we do other things... I'm of a mind that only allowing said things
		// after we have completed the topology changes is a good thing, and we are in
		// a known good state is a good assumption to make, meaning less code and fewer
		// hidden race condition bugs.
		r.abort("pods are warming up")
		return nil
	}

	return nil
}

// If the cluster is rebalancing, we need to let it continue before letting any more
// topology changes happen.  If Couchbase reports as rebalancing, but there is no
// active task, then stop the rebalance.
func (r *ReconcileMachine) handleRebalanceCheck(c *Cluster) error {
	if !r.couchbase.IsRebalancing {
		return nil
	}

	running, err := c.IsRebalanceActive(c.readyMembers())
	if err != nil {
		return fmt.Errorf("%w: Rebalance status collection failed", errors.NewStackTracedError(ErrReconcileInhibited))
	}

	if running {
		r.abort("cluster is currently rebalancing")
		return nil
	}

	if err := couchbaseutil.StopRebalance().On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Rebalance cancellation failed", "cluster", c.namespacedName())
		return err
	}

	log.Info("Rebalance cancelled", "cluster", c.namespacedName())

	return nil
}

// allDownNodesRecoveryTimedout looks at every down node, if we haven't seen it timeout yet,
// cache the time it should fail.  If all the nodes have been seen and their
// timeouts have expired, indicate "aggressive" recovery is allowed.
func (c *Cluster) allDownNodesRecoveryTimedout(members couchbaseutil.MemberSet) bool {
	recoverable := true

	for name := range members {
		recoveryTime, ok := c.recoveryTime[name]
		if !ok {
			timeout := c.cluster.Spec.ClusterSettings.AutoFailoverTimeout
			recoveryTime = time.Now().Add(timeout.Duration).Add(30 * time.Second)
			c.recoveryTime[name] = recoveryTime
			recoverable = false
		}

		if recoveryTime.After(time.Now()) {
			log.Info("Pod down, waiting for auto-failover", "cluster", c.namespacedName(), "name", name, "recovery_in", time.Until(recoveryTime))

			recoverable = false
		}
	}

	return recoverable
}

// If any nodes are marked as down, we first and foremost, wait for Server to safely
// autofailover.
func (r *ReconcileMachine) handleDownNodes(c *Cluster) error {
	if r.couchbase.DownNodes.Empty() {
		return nil
	}

	r.log()

	// Ensure the cluster is visibly unhealthy before triggering any events
	c.cluster.Status.SetUnavailableCondition(r.couchbase.DownNodes.Names())

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	// Alaways flag nodes down, the observed behaviour can change based on whether
	// a recover timer has expired or not.
	for name := range r.couchbase.DownNodes {
		c.raiseEventCached(k8sutil.MemberDownEvent(name, c.cluster))
	}

	// If the recovery timeouts haven't all expired, wait...
	// Note that we do a hard abort here.  This allows the cluster to get back into a
	// stable state before we do "dangerous" things like reconcile TLS, which needs to
	// be done one-shot.
	if !c.allDownNodesRecoveryTimedout(r.couchbase.DownNodes) {
		return fmt.Errorf("%w: waiting for pod failover", errors.NewStackTracedError(ErrReconcileInhibited))
	}

	// Get the duration that the node has been down from the status
	// and check if it has persistent volumes to be recovered
	recovered := 0

	for name, m := range r.couchbase.DownNodes {
		// Ephemeral clusters are handled either automatically by server or
		// manually by the user.
		if !c.isPodRecoverable(m) && c.cluster.GetRecoveryPolicy() == couchbasev2.PrioritizeDataIntegrity {
			return nil
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

		recovered++
	}

	if recovered != 0 {
		r.abort("waiting for cluster recovery")

		return nil
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
	if c.cluster.GetRecoveryPolicy() == couchbasev2.PrioritizeUptime {
		log.Info("Forcing failover of unrecoverable nodes", "cluster", c.namespacedName())

		otpNodes := couchbaseutil.OTPNodeList{}

		for _, member := range r.couchbase.DownNodes {
			log.Info("Failing over node", "cluster", c.namespacedName(), "name", member.Name())

			otpNodes = append(otpNodes, member.GetOTPNode())
		}

		if err := couchbaseutil.Failover(otpNodes, true).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		r.abort("pods are failing over")

		return nil
	}

	return nil
}

func (r *ReconcileMachine) handleUnclusteredNodes(c *Cluster) error {
	if r.unclusteredMembers.Empty() {
		return nil
	}

	r.log()

	for name := range r.unclusteredMembers {
		if err := c.destroyMember(name, r.shouldRemoveVolumes(name)); err != nil {
			return fmt.Errorf("unable to remove unclustered node: %w", err)
		}

		log.Info("Pod unclustered, deleting", "cluster", c.namespacedName(), "name", name)

		// Nodes may be rebalanced out in a previous iteration (caused by
		// node failover) and thus miss out the ejection events from rebalance().
		//
		// TODO: it makes sense to unify handling all unclustered nodes *after*
		// reblancing has occurred, however tests will probably fail left right
		// and center due to the events occurring after the cluster is in a healthy
		// state.
		c.raiseEvent(k8sutil.MemberRemoveEvent(name, c.cluster))
	}

	return nil
}

func (r *ReconcileMachine) handleFailedAddNodes(c *Cluster) error {
	if r.couchbase.FailedAddNodes.Empty() {
		return nil
	}

	r.log()

	// These nodes have been added, but the node failed before a rebalance could
	// start. If the node is configured to use volumes then we will recreate it,
	// otherwise, we will remove these nodes and re-add them in other pods later.
	for name, m := range r.couchbase.FailedAddNodes {
		c.logFailedMember("Node failed", name)

		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				log.Error(err, "Pending add pod cannot be recovered", "cluster", c.namespacedName(), "name", name)
				r.abort("unaable to recover pod pending addition")

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

		// This is a special snowflake case, and the member is ejected by the cancel.
		r.removeMemberNoEject(m)
	}

	return nil
}

// Add back failed nodes to cluster.
// Delta recover is performed for data nodes,
// otherwise a full recovery is performed.
func (r *ReconcileMachine) handleAddBackNodes(c *Cluster) error {
	if r.couchbase.AddBackNodes.Empty() {
		return nil
	}

	r.log()

	for name, m := range r.couchbase.AddBackNodes {
		err := c.verifyMemberVolumes(m)
		if err != nil {
			log.Error(err, "Failed pod cannot be recovered, volumes unhealthy", "cluster", c.namespacedName(), "name", name)

			r.removeMember(m)
			c.raiseEvent(k8sutil.FailedAddBackNodeEvent(name, c.cluster))

			break
		}

		// Set recovery type as delta for data nodes
		sc := c.cluster.Spec.GetServerConfigByName(m.Config())
		if sc == nil {
			log.Info("Add back pod not in the specification, deleting", "cluster", c.namespacedName(), "name", name, "class", m.Config())

			r.ejectMembers.Add(m)

			break
		}

		recoveryType := couchbaseutil.RecoveryTypeFull

		if couchbasev2.ServiceList(sc.Services).Contains(couchbasev2.DataService) {
			recoveryType = couchbaseutil.RecoveryTypeDelta
		}

		log.Info("Setting recovery type", "cluster", c.namespacedName(), "name", name, "type", recoveryType)

		if err := couchbaseutil.SetRecoveryType(m.GetOTPNode(), recoveryType).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		r.needsRebalance = true
	}

	return nil
}

// Failed nodes can be recovered if the pod's volumes are healthy,
// otherwise the node is ejected from the cluster and it's node deleted.
func (r *ReconcileMachine) handleFailedNodes(c *Cluster) error {
	if r.couchbase.FailedNodes.Empty() {
		return nil
	}

	r.log()

	for _, name := range r.couchbase.FailedNodes.Names() {
		c.raiseEventCached(k8sutil.MemberFailedOverEvent(name, c.cluster))
	}

	for name, m := range r.couchbase.FailedNodes {
		log.Info("Pods failed over", "cluster", c.namespacedName())

		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", name, "reason", err)

				r.abort("unable to recover pod")

				return nil
			}

			c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))

			return fmt.Errorf("%w: recovering node %s", errors.NewStackTracedError(ErrReconcileInhibited), name)
		}

		log.Info("Pod failed, deleting", "cluster", c.namespacedName(), "name", name)

		r.removeMember(m)
	}

	return nil
}

func (r *ReconcileMachine) handleUnknownServerConfigs(c *Cluster) error {
	// If a server configuration was deleted in a spec update then we will clean
	// up all of the nodes from that server config here.
	for name, m := range r.clusteredMembers {
		if c.cluster.Spec.GetServerConfigByName(m.Config()) == nil {
			log.Info("Pod not in the specification, deleting", "cluster", c.namespacedName(), "name", name, "class", m.Config())

			r.removeMemberUser(m)
		}
	}

	return nil
}

func (r *ReconcileMachine) handleRemoveNode(c *Cluster) error {
	var deletions []couchbasev2.ServerConfig

	var scheduledScaling couchbasev2.ScalingMessageList

	for _, serverSpec := range c.cluster.Spec.Servers {
		// Check to see if we need to remove anything
		existingNodes := r.clusteredMembers.GroupByServerConfig(serverSpec.Name).Size()
		nodesToRemove := existingNodes - serverSpec.Size

		if nodesToRemove <= 0 {
			continue
		}

		for i := 0; i < nodesToRemove; i++ {
			deletions = append(deletions, serverSpec)
		}

		scheduledScaling = append(scheduledScaling, couchbasev2.ScalingMessage{Server: serverSpec.Name, From: existingNodes, To: serverSpec.Size})
	}

	if len(deletions) == 0 {
		return nil
	}

	r.log()

	c.cluster.Status.SetScalingDownCondition(scheduledScaling.BuildMessage())

	for _, serverSpec := range deletions {
		server, err := c.scheduler.Delete(serverSpec.Name)
		if err != nil {
			return fmt.Errorf("failed to schedule removal of member '%s': %w", serverSpec.Name, err)
		}

		r.removeMemberUser(c.members[server])
	}

	return nil
}

func (r *ReconcileMachine) handleAddNode(c *Cluster) error {
	// Accumulate the server classes that need scaling up...
	var additions []couchbasev2.ServerConfig

	var scheduledScaling couchbasev2.ScalingMessageList

	for _, serverSpec := range c.cluster.Spec.Servers {
		existingNodes := r.clusteredMembers.GroupByServerConfig(serverSpec.Name).Size()

		nodesToCreate := serverSpec.Size - existingNodes
		if nodesToCreate <= 0 {
			continue
		}

		for i := 0; i < nodesToCreate; i++ {
			additions = append(additions, serverSpec)
		}

		scheduledScaling = append(scheduledScaling, couchbasev2.ScalingMessage{Server: serverSpec.Name, From: existingNodes, To: serverSpec.Size})
	}

	if len(additions) == 0 {
		return nil
	}

	r.log()

	// Set the scaling status *before* we start adding any nodes.
	// This means an external observer can wait for a node addition and the
	// cluster will already report as scaling (aka not fully healthy)
	c.cluster.Status.SetScalingUpCondition(scheduledScaling.BuildMessage())

	if err := c.updateCRStatus(); err != nil {
		log.Error(err, "Cluster status update failed", "cluster", c.namespacedName())
	}

	memberResults, err := c.addMembers(additions...)
	if err != nil {
		log.Error(err, "Pod addition to cluster failed", "cluster", c.namespacedName())
	}

	// count how many errors we actually have
	numErrors := 0

	for _, result := range memberResults {
		if result.Err != nil {
			numErrors++
		}
	}

	errs := make([]error, 0, numErrors)

	for _, result := range memberResults {
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("failed to create new node for cluster: %w", result.Err))
			log.Error(result.Err, "Pod addition to cluster failed", "cluster", c.namespacedName(), "pod", result.Member.Name())
		} else {
			r.addMember(result.Member)
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

// handleVolumeExpansion attempts to perform online expansion of Persistent Volumes.
func (r *ReconcileMachine) handleVolumeExpansion(c *Cluster) error {
	if !c.cluster.Spec.EnableOnlineVolumeExpansion {
		// currently only volumes are allowed for online upgrade
		return nil
	}

	// Online upgrade of Persistent volumes for each member
	for _, name := range c.members.Names() {
		member := c.members[name]

		// Get member config
		serverClass := c.cluster.Spec.GetServerConfigByName(member.Config())
		if serverClass == nil {
			continue
		}

		// Get state of persistent volumes
		pvcState, err := k8sutil.GetPodVolumes(c.k8s, member, c.cluster, *serverClass)
		if err != nil {
			return err
		}

		for _, pvc := range pvcState.List() {
			switch {
			// When online resize failed for any of the member volumes then proceed with
			// normal rolling upgrade since the Pod must be recreated.
			case pvcState.IsResizeFailed(pvc.Name):
				log.Info("Unable to expand volume in place, falling back to rolling upgrade", "cluster", c.namespacedName(), "volume", pvc.Name)
				c.raiseEvent(k8sutil.ExpandVolumeFallbackEvent(pvc.Name, c.cluster))

				// Remove volume expansion flag.
				if c.checkVolumeExpansionState() {
					return nil
				}

				return c.setVolumeExpansionState(false)

			// Check if a volume expansion is already in progress and end reconciliation loop if so.
			case pvcState.IsExpanding(pvc.Name):
				// NOTE: might consider a timeout here, but since we've already sent the request
				// to the storageclass there isn't really a good action to take while volumes
				// are in an unstable state.
				log.Info("Pod volume expansion is in progress", "cluster", c.namespacedName(), "volume", pvc.Name)
				r.abort("persisitent volumes expanding")

				return nil
			// Check if volume spec has been updated and apply changes.
			case pvcState.IsUpdated(pvc.Name):
				requestedClaim, err := pvcState.Update(c.k8s, pvc.Name)
				if err != nil {
					return err
				}

				// Flag that a volume expansion is occurring.
				if err := c.setVolumeExpansionState(true); err != nil {
					return err
				}

				// Log and raise event that expansion started.
				currentSize := k8sutil.GetVolumeStorageSize(pvc)
				requestedSize := k8sutil.GetVolumeStorageSize(requestedClaim)
				c.raiseEvent(k8sutil.ExpandVolumeStartedEvent(pvc.Name, currentSize, requestedSize, c.cluster))
				log.Info("Volume expanding", "cluster", c.namespacedName(), "name", pvc.Name, "current", currentSize, "requested", requestedSize)

				// Done for now. Not going to upgrade all volumes at once.
				r.abort("persistent volumes expanding")

				return nil
			}

			if !c.checkVolumeExpansionState() {
				continue
			}

			// At this point the volume matches our desired state.
			// If this is a result of a volume expansion then
			// send an event, otherwise carry on.
			if err := c.setVolumeExpansionState(false); err != nil {
				return err
			}

			c.raiseEvent(k8sutil.ExpandVolumeSucceededEvent(pvc.Name, c.cluster))
		}
	}

	return nil
}

// selectUpgradeCandidates applies an upgrade heuristic to the set of all upgradable pods
// and filters this down into a set of pods that will be upgraded this turn.
func (c *Cluster) selectUpgradeCandidates(candidates couchbaseutil.MemberSet) (couchbaseutil.MemberSet, error) {
	// Rolling upgrade defaults to a single node at a time, however this can
	// be increased to an absolute number or a relative size of the cluster.
	if c.cluster.GetUpgradeStrategy() == couchbasev2.RollingUpgrade {
		// Default to one at a time
		upgradeLimit := 1

		if c.cluster.Spec.RollingUpgrade != nil {
			// Start with a big number and pick the smallest of any
			// explicitly stated number...
			explicitNumber := constants.IntMax

			// Absolute number is first, so just set it if defined.  A zero value
			// means it's unset and is pruned from the CR JSON.
			if c.cluster.Spec.RollingUpgrade.MaxUpgradable != 0 {
				explicitNumber = c.cluster.Spec.RollingUpgrade.MaxUpgradable
			}

			if c.cluster.Spec.RollingUpgrade.MaxUpgradablePercent != "" {
				// Strip the percentage and convert into an interger in the
				// range 1-100.
				maxUpgradableRaw := c.cluster.Spec.RollingUpgrade.MaxUpgradablePercent
				maxUpgradableRaw = maxUpgradableRaw[:len(maxUpgradableRaw)-1]

				percentage, err := strconv.Atoi(maxUpgradableRaw)
				if err != nil {
					return nil, errors.NewStackTracedError(err)
				}

				// Yield a number in the range 0->cluster size>.  When zero, we'll
				// do nothing, so set a lower bound of 1.
				maxUpgradable := (c.cluster.Spec.TotalSize() * percentage) / 100
				if maxUpgradable <= 0 {
					maxUpgradable = 1
				}

				// Select this value if it's smaller than enything already set.
				if maxUpgradable < explicitNumber {
					explicitNumber = maxUpgradable
				}
			}

			// If we have an explicit value, update the number of candidates.
			if explicitNumber != constants.IntMax {
				upgradeLimit = explicitNumber
			}
		}

		// Cap the number of upgrades at the number of candidates.
		maxUpgradable := len(candidates)
		if upgradeLimit < maxUpgradable {
			maxUpgradable = upgradeLimit
		}

		constrained := couchbaseutil.MemberSet{}

		for _, name := range candidates.Names()[:maxUpgradable] {
			constrained.Add(candidates[name])
		}

		candidates = constrained
	}

	return candidates, nil
}

func (r *ReconcileMachine) handleUpgradeNode(c *Cluster) error {
	// Something is broken, let that get fixed up first.
	if r.needsRebalance {
		return nil
	}

	// Nothing to do, move along.
	candidates, err := c.needsUpgrade()
	if err != nil {
		return err
	}

	if candidates.Empty() {
		return nil
	}

	r.log()

	constrained, err := c.selectUpgradeCandidates(candidates)
	if err != nil {
		return err
	}

	candidates = constrained

	// Calculate the number of nodes already in the target state before we
	// potentially mutate the candidates.
	upgraded := len(c.members) - len(candidates)

	// Do any events/conditions that make the upgrade observable.
	targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return err
	}

	status := &couchbasev2.UpgradeStatus{
		TargetCount: upgraded,
		TotalCount:  len(c.members),
	}

	// Flag that an upgrade is in action, validation will use this to control what
	// resource modifications are allowed.
	if err := c.reportUpgrade(status); err != nil {
		return err
	}

	candidatesSlice := make([]couchbaseutil.Member, 0, len(candidates))
	toCreate := make([]couchbasev2.ServerConfig, 0, len(candidates))

	for _, candidate := range candidates {
		log.Info("Pod upgrading", "cluster", c.namespacedName(), "name", candidate.Name(), "source", candidate.Version(), "target", targetVersion)

		// Remove the candidate from the scheduler.
		if err := c.scheduler.Upgrade(candidate.Config(), candidate.Name()); err != nil {
			return err
		}

		// Grab the server class.
		class := c.cluster.Spec.GetServerConfigByName(candidate.Config())
		if class == nil {
			return fmt.Errorf("upgrade unable to determine server class %s for member %s: %w", candidate.Name(), candidate.Config(), errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		toCreate = append(toCreate, *class)
		candidatesSlice = append(candidatesSlice, candidate)
	}

	// Add the new members.
	memberResults, err := c.addMembers(toCreate...)
	if err != nil {
		return fmt.Errorf("upgrade failed to add new nodes to cluster: %w", err)
	}

	numErrors := 0

	for _, result := range memberResults {
		if result.Err != nil {
			numErrors++
		}
	}

	errs := make([]error, 0, numErrors)

	for index, result := range memberResults {
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("upgrade failed to add new node to cluster: %w", result.Err))
			log.Error(result.Err, "Pod addition to cluster failed", "cluster", c.namespacedName(), "pod", result.Member.Name())
		} else { // Update book keeping
			r.addMember(result.Member)
			r.removeMemberUser(candidatesSlice[index])
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

// handleServerGroups moves nodes from their current server group into the one
// the pod is labelled as by the scheduler.  It occurs after node addition and
// balance in as alterations would trigger an additional rebalance otherwise.
func (r *ReconcileMachine) handleServerGroups(c *Cluster) error {
	if updated, err := c.reconcileServerGroups(); err != nil {
		return err
	} else if updated {
		r.needsRebalance = true
	}

	return nil
}

// handleNodeServices creates any services required to provide external connectivity
// before the node is balanaced in and starts serving bucket shards.  This is for the
// benefit of external clients such as xdcr which would not function until the
// balance in has completed otherwise.
func (r *ReconcileMachine) handleNodeServices(c *Cluster) error {
	if err := c.reconcilePodServices(); err != nil {
		return err
	}

	err := c.reconcileMemberAlternateAddresses()

	return err
}

// When cluster is under topology changes that are outside of
// HorizontalPodAutoscaler or due to pending changes via restart/crash,
// incoming requests from the HorizontalPodAutoscaler are suspended.
func (r *ReconcileMachine) handleAutoscaleServerConfigs(c *Cluster) error {
	// Check if cluster is rebalancing (or needs to be rebalanced), and pause HPA activity
	// by entering 'maintenance-mode' to prevent any intermediate scaling requests.
	if c.cluster.Spec.AutoscaleStabilizationPeriod != nil {
		if r.couchbase.IsRebalancing || r.needsRebalance {
			if err := c.startAutoscalingMaintenanceMode(); err != nil {
				return err
			}
		} else if c.autoscalingReady() {
			if err := c.endAutoscalingMaintenanceMode(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ReconcileMachine) handleRebalance(c *Cluster) error {
	if r.needsRebalance {
		// Eject nodes that we want to discard.
		eject := r.ejectMembers.OTPNodes()

		if err := c.rebalance(r.clusteredMembers, eject); err != nil {
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
					log.Error(err, "Pod add-back failed, unable to determine recovery type", "cluster", c.namespacedName(), "name", m.Name())
					return err
				}

				node, err := info.GetNode(m.GetHostName())
				if err != nil {
					log.Error(err, "Pod add-back failed, unable to determine recovery type", "cluster", c.namespacedName(), "name", m.Name())
					return err
				}

				if node.RecoveryType == couchbaseutil.RecoveryTypeDelta {
					deltaNodes.Add(m)
				}
			}

			if len(deltaNodes) != 0 {
				log.Info("Pod add-back failed, forcing full recovery", "cluster", c.cluster.NamespacedName())

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
	}

	return nil
}

// Dead members are members that the operator is tracking, but do not have a
// corresponding running pod.
func (r *ReconcileMachine) handleDeadMembers(c *Cluster) error {
	for name := range r.ejectMembers {
		if err := c.destroyMember(name, r.shouldRemoveVolumes(name)); err != nil {
			return fmt.Errorf("failed to remove dead members: %w", err)
		}
	}

	return nil
}

func (r *ReconcileMachine) handleNotifyFinished(c *Cluster) error {
	log.V(1).Info("Reconcile completed", "cluster", c.namespacedName())

	return nil
}

// Check if volumes should be removed with Pod based on reconcile status.
func (r *ReconcileMachine) shouldRemoveVolumes(server string) bool {
	if r.preserveVolumes == nil {
		return true
	}

	if _, ok := r.preserveVolumes[server]; !ok {
		return true
	}

	return false
}
