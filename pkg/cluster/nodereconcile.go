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
	goerrors "errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrReconcileInhibited = fmt.Errorf("reconcile was blocked from running")
var ErrFailoverStartCounterNotIncremented = fmt.Errorf("failover start counter not incremented")
var ErrFailoverSuccessCounterNotIncremented = fmt.Errorf("failover success counter not incremented")
var ErrUnexpectedCounterChange = fmt.Errorf("unexpected counter change")
var ErrNodeNotInCluster = fmt.Errorf("node not in the cluster: ")
var ErrNodeNotActive = fmt.Errorf("node not active: ")
var ErrSwapRebalanceSetupFailed = fmt.Errorf("swap rebalance setup failed")

// This is a temporary measure to maintain the interface.  This is all smell code
// and will probably get killed off fairly soon.
type MemberState struct {
	NodeStateMap           NodeStateMap
	managedNodes           couchbaseutil.MemberSet
	ActiveNodes            couchbaseutil.MemberSet
	PendingAddNodes        couchbaseutil.MemberSet
	AddBackNodes           couchbaseutil.MemberSet
	FailedAddNodes         couchbaseutil.MemberSet
	WarmupNodes            couchbaseutil.MemberSet
	DownNodes              couchbaseutil.MemberSet
	FailedNodes            couchbaseutil.MemberSet
	UnclusteredNodes       couchbaseutil.MemberSet
	IsRebalancing          bool
	NeedsRebalance         bool
	ServerRebalanceReasons []string
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

	// stabilizingMembers are replacement nodes that became Active post-rebalance
	// but haven't yet finished the upgrade stabilization period.
	stabilizingMembers couchbaseutil.MemberSet

	// unclusteredMembers members Couchbase knows about but we have no resource for
	// so they need ejecting and deleting.  They are treated separately from ejectMembers
	// because there is no book keeping to clean up.
	// TODO: is this true?  We reinitialize the cluster member state straight after this
	// FSM is called.
	unclusteredMembers couchbaseutil.MemberSet

	// pendingDNSMembers are nodes that have been CBS-added (PendingAddNodes state) but are not yet
	// ready to be rebalanced in because their external DNS has not propagated yet.
	// handleNodeServices manages DNS timeout ejection; canRebalance waits for DNS propagation.
	pendingDNSMembers couchbaseutil.MemberSet

	// pendingInitPods are members with PendingInitializationCondition, snapshotted at
	// FSM creation time (after updatePendingInitializationConditions has run).
	pendingInitPods couchbaseutil.MemberSet

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

	// rebalanceRetries is the number of times to retry rebalance operations when handling rebalances.
	rebalanceRetries uint
}

func (r *ReconcileMachine) logState() {
	log.V(0).Info("reconciler", "cluster", r.c.namespacedName(), "clustered", r.clusteredMembers.Names(), "running", r.runningMembers.Names(), "eject", r.ejectMembers.Names(), "unclustered", r.unclusteredMembers.Names(), "rebalance", r.needsRebalance, "pendingDNS", r.pendingDNSMembers.Names())
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
		NodeStateMap:           status.NodeStates,
		managedNodes:           c.members,
		ActiveNodes:            couchbaseutil.NewMemberSet(),
		PendingAddNodes:        couchbaseutil.NewMemberSet(),
		AddBackNodes:           couchbaseutil.NewMemberSet(),
		FailedAddNodes:         couchbaseutil.NewMemberSet(),
		WarmupNodes:            couchbaseutil.NewMemberSet(),
		DownNodes:              couchbaseutil.NewMemberSet(),
		FailedNodes:            couchbaseutil.NewMemberSet(),
		UnclusteredNodes:       couchbaseutil.NewMemberSet(),
		IsRebalancing:          status.Balancing,
		NeedsRebalance:         !status.Balanced,
		ServerRebalanceReasons: status.RebalanceReasons,
	}

	for name, nodeState := range status.NodeStates {
		member, ok := c.members[name]
		if !ok {
			continue
		}

		switch nodeState {
		case NodeStateActive:
			state.ActiveNodes.Add(member)
		case NodeStatePendingAdd:
			state.PendingAddNodes.Add(member)
			state.NeedsRebalance = true
		case NodeStateFailedAdd:
			state.FailedAddNodes.Add(member)
		case NodeStateWarmup:
			state.WarmupNodes.Add(member)
		case NodeStateDown:
			state.DownNodes.Add(member)
		case NodeStateFailed:
			state.FailedNodes.Add(member)
		case NodeStateAddBack:
			state.AddBackNodes.Add(member)
		}
	}

	for name, member := range c.members {
		if _, ok := status.NodeStates[name]; !ok {
			if pod, exists := c.k8s.Pods.Get(name); exists && k8sutil.HasPendingInitializationCondition(pod) {
				continue
			}
			state.UnclusteredNodes.Add(member)
		}
	}

	var rebalanceRetries = 1

	val, err := c.state.Get(persistence.RebalanceRetries)
	if err == nil {
		rr, err := strconv.Atoi(val)
		if err != nil {
			rebalanceRetries = 1
		} else {
			rebalanceRetries = rr
		}
	}

	pendingInit := podsToMemberSet(c.getPendingInitPods())

	fsm := &ReconcileMachine{
		// c.members contains all members we know about from Kubernetes or from
		// Couchbase server.  By removing the ones Couchbase doesn't know about
		// (UnclusteredNodes) and pods still awaiting async CBS initialization
		// (PendingInitializationCondition) we get the set of fully-clustered members.
		clusteredMembers: c.members.Diff(state.UnclusteredNodes).Diff(pendingInit),

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

		rebalanceRetries: uint(rebalanceRetries),

		stabilizingMembers: couchbaseutil.NewMemberSet(),

		pendingInitPods: pendingInit,

		pendingDNSMembers: c.members.Intersect(podsToMemberSet(c.getPendingDNSPods())),
	}

	// Reset any timeout counters if nodes have recovered.
	for name := range state.ActiveNodes {
		delete(c.recoveryTime, name)
	}

	c.deriveStabilizingFSMMembers(fsm, state)
	c.deriveEjectFSMMembers(fsm, state)

	return fsm, nil
}

// deriveStabilizingFSMMembers populates fsm.stabilizingMembers from active nodes that still carry the durable UpgradeTrackingAnnotation.
func (c *Cluster) deriveStabilizingFSMMembers(fsm *ReconcileMachine, state *MemberState) {
	for name := range state.ActiveNodes {
		pod, ok := c.k8s.Pods.Get(name)
		if !ok {
			continue
		}
		if process := k8sutil.GetPodUpgradeTracking(pod); process != "" {
			if member, ok := c.members[name]; ok {
				fsm.stabilizingMembers.Add(member)
			}
		}
	}
}

// deriveEjectFSMMembers populates fsm.ejectMembers from active nodes marked with
// PodPendingUpgradeBeforeEjectionCondition. Swap-rebalance marks old members for ejection durably so
// that the intent survives across reconcile cycles. The condition is NOT cleared here —
// it is cleared by handleRebalance after successful ejection.
//
// NOTE: The ejected member is also removed from clusteredMembers. In the migration path
// swapRebalanceMembers called removeMemberUser which did this in the same cycle. In the
// async path the swap is split across reconcile cycles, so clusteredMembers is rebuilt
// from CBS state each time and reincludes the old pod (still Active) plus the
// pending-init replacement. Without the removal, handleRemoveNode counts one extra node
// and scale-downs the wrong pod, and verifyRebalance fails because it expects the ejected
// node to remain Active after rebalance.
func (c *Cluster) deriveEjectFSMMembers(fsm *ReconcileMachine, state *MemberState) {
	for name := range state.ActiveNodes {
		pod, ok := c.k8s.Pods.Get(name)
		if !ok {
			continue
		}
		if k8sutil.IsPodPendingUpgradeBeforeEjection(pod) {
			if member, ok := c.members[name]; ok {
				fsm.ejectMembers.Add(member)
				fsm.clusteredMembers.Remove(name)
				fsm.needsRebalance = true
			}
		}
	}
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
		(*ReconcileMachine).handleServerServices,
		(*ReconcileMachine).handleMoveNodes,
		(*ReconcileMachine).handlePodHostname,
		(*ReconcileMachine).handleRemoveNode,
		(*ReconcileMachine).handleUpgradeNode,
		(*ReconcileMachine).handleBucketMigration,
		(*ReconcileMachine).handleAddNode,
		(*ReconcileMachine).handleServerGroups,
		(*ReconcileMachine).handleNodeServices,
		(*ReconcileMachine).handleAutoscaleServerConfigs,
		(*ReconcileMachine).handleRebalance,
		(*ReconcileMachine).handleEjectedMembers,
		(*ReconcileMachine).handleNotifyFinished,
	}

	for i := 0; i < len(reconcileFunctions); i++ {
		if err := reconcileFunctions[i](r, c); err != nil {
			return false, err
		}

		if r.abortReason != "" {
			c.log.Info("Aborting topology reconcile", "cluster", c.namespacedName(), "reason", r.abortReason)
			return true, nil
		}

		if err := c.updateCRStatus(); err != nil {
			c.log.Error(err, "Cluster status update failed", "cluster", c.namespacedName())
		}
	}

	return false, nil
}

func (r *ReconcileMachine) handlePodHostname(c *Cluster) error {
	var hostnameDNSRequired bool
	if c.cluster.Spec.Networking.ImprovedHostNetwork && c.cluster.Spec.Networking.InitPodsWithNodeHostname {
		hostnameDNSRequired = true
	}

	membersToSwapRebalance := couchbaseutil.MemberSet{}

	clusterInfo := &couchbaseutil.TerseClusterInfo{}
	if err := couchbaseutil.GetTerseClusterInfo(clusterInfo).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	orchestratorName := clusterInfo.Orchestrator

	for _, member := range r.clusteredMembers {
		pod, ok := c.k8s.Pods.Get(member.Name())
		if !ok {
			return errors.ErrPodNotFound
		}

		if hostnameDNSRequired {
			if member.GetDNSName() != pod.Spec.NodeName {
				membersToSwapRebalance.Add(member)
			}
		} else {
			if member.GetDNSName() == pod.Spec.NodeName {
				membersToSwapRebalance.Add(member)
			}
		}
	}

	constrained, err := c.selectUpgradeCandidates(membersToSwapRebalance, orchestratorName)
	if err != nil {
		return err
	}

	if membersToSwapRebalance.Size() > 0 {
		return r.swapRebalanceMembers(c, constrained)
	}

	return nil
}

func (r *ReconcileMachine) handleServerServices(c *Cluster) error {
	if r.needsRebalance {
		return nil
	}

	candidates, err := c.getNodeServiceMismatchCandidates()
	if err != nil {
		return err
	}

	if candidates.Empty() {
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionServicesMismatch)
		return nil
	}

	for _, candidate := range candidates {
		c.log.Info("Node services mismatch", "cluster", c.namespacedName(), "name", candidate.Name())
	}

	clusterInfo := &couchbaseutil.TerseClusterInfo{}
	if err := couchbaseutil.GetTerseClusterInfo(clusterInfo).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	orchestratorName := clusterInfo.Orchestrator

	constrained, err := c.selectUpgradeCandidates(candidates, orchestratorName)
	if err != nil {
		return err
	}

	if !constrained.Empty() {
		c.raiseEvent(k8sutil.EventReasonServicesMismatchEvent(c.cluster))
		c.cluster.Status.SetServicesMismatchCondition()
		return r.swapRebalanceMembers(c, constrained)
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionServicesMismatch)

	return nil
}

func (c *Cluster) getNodeServiceMismatchCandidates() (couchbaseutil.MemberSet, error) {
	candidates := couchbaseutil.NewMemberSet()

	for _, candidate := range c.readyMembers() {
		// Get the server configuration for the candidate
		serverConfig := c.cluster.Spec.GetServerConfigByName(candidate.Config())
		if serverConfig == nil {
			c.log.Info("Server configuration not found for candidate", "name", candidate.Name())
			continue
		}

		// Compare the candidate's services with the expected services
		nodesSelf := &couchbaseutil.NodeInfo{}
		err := couchbaseutil.GetNodesSelf(nodesSelf).On(c.api, candidate)
		if err != nil {
			return candidates, err
		}
		candidateServices := nodesSelf.Services
		expectedServices := serverConfig.Services
		expectedServicesString := make([]string, 0, len(expectedServices))
		for _, service := range expectedServices {
			if service.String() != "admin" {
				expectedServicesString = append(expectedServicesString, service.String())
			}
		}

		for i := 0; i < len(candidateServices); i++ {
			switch candidateServices[i] {
			case "kv":
				candidateServices[i] = "data"
			case "n1ql":
				candidateServices[i] = "query"
			case "fts":
				candidateServices[i] = "search"
			case "cbas":
				candidateServices[i] = "analytics"
			default:
				// no mapping needed
			}
		}

		sort.Strings(candidateServices)
		sort.Strings(expectedServicesString)

		if !couchbaseutil.EqualStringSlices(candidateServices, expectedServicesString) {
			candidates.Add(candidate)
		}
	}

	return candidates, nil
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

func (r *ReconcileMachine) checkRebalanceStarted(c *Cluster) (bool, error) {
	balancedConditionStatus := c.cluster.Status.GetCondition(couchbasev2.ClusterConditionBalanced).Status
	if balancedConditionStatus != "True" {
		r.needsRebalance = true

		ejectMembers, err := c.state.Get(persistence.RebalanceEjectMembers)
		if err != nil {
			return false, err
		}

		for _, eject := range strings.Split(ejectMembers, ",") {
			for _, clusterMember := range c.members {
				if eject == clusterMember.Name() {
					r.ejectMembers.Add(clusterMember)
				}
			}
		}

		clusteredMembers, err := c.state.Get(persistence.RebalanceClusteredMembers)
		if err != nil {
			return false, err
		}

		for _, clusteredMember := range strings.Split(clusteredMembers, ",") {
			for _, clusterMember := range c.members {
				if clusteredMember == clusterMember.Name() {
					r.clusteredMembers.Add(clusterMember)
				}
			}
		}

		return true, nil
	}

	return false, nil
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

	ok, err := r.checkRebalanceStarted(c)
	if err != nil {
		return err
	}

	if ok {
		return nil
	}

	if err := couchbaseutil.StopRebalance().On(c.api, c.readyMembers()); err != nil {
		c.log.Error(err, "Rebalance cancellation failed", "cluster", c.namespacedName())
		return err
	}

	c.log.Info("Rebalance cancelled", "cluster", c.namespacedName())

	return nil
}

// allDownNodesRecoveryTimedout looks at every down node, if we haven't seen it timeout yet,
// cache the time it should fail.  If all the nodes have been seen and their
// timeouts have expired, indicate "aggressive" recovery is allowed.
func (c *Cluster) allDownNodesRecoveryTimedout(members couchbaseutil.MemberSet) bool {
	recoverable := true

	for name := range members {
		r, timeUntilRecovery := c.hasMemberRecoveryTimeElapsed(name)
		if !r {
			c.log.Info("Pod down, waiting for auto-failover", "cluster", c.namespacedName(), "name", name, "recovery_in", timeUntilRecovery)

			recoverable = false
		}
	}

	return recoverable
}

// hasMemberRecoveryTimeElapsed checks if the recovery time has elapsed for a member.
// If the recovery timeout has not elapsed, it returns false and the time until the recovery timeout will be true.
// If the recovery timeout has elapsed, it returns true and nil.
func (c *Cluster) hasMemberRecoveryTimeElapsed(memberName string) (bool, *time.Duration) {
	recoverable := true

	recoveryTime, ok := c.recoveryTime[memberName]
	if !ok {
		timeout := c.cluster.Spec.ClusterSettings.AutoFailoverTimeout
		recoveryTime = time.Now().Add(timeout.Duration).Add(30 * time.Second)
		c.recoveryTime[memberName] = recoveryTime
	}

	var timeUntilRecovery *time.Duration

	if recoveryTime.After(time.Now()) {
		recoverable = false
		r := time.Until(recoveryTime)
		timeUntilRecovery = &r
	}

	return recoverable, timeUntilRecovery
}

// If any nodes are marked as down, we first and foremost, wait for Server to safely
// autofailover.
// nolint:gocognit
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
		r.abort("waiting for down node failover timeout")
		return nil
	}

	// Build a list of nodes that have exceeded the max recovery retry limit.
	// These will need to be failed over (PrioritizeUptime) or require manual action (PrioritizeDataIntegrity).
	nodesToRecover := []string{}

	for name, m := range r.couchbase.DownNodes {
		if c.isPodRecoverable(m) && c.hasExceededRecoveryMaxRetries(name) {
			c.log.Info("Pod recovery max retries exceeded",
				"cluster", c.namespacedName(), "name", name,
				"maxRetries", c.config.PodRecoveryMaxRetries,
				"attempts", c.getRecoveryAttempts(name))

			nodesToRecover = append(nodesToRecover, name)
		}
	}

	// If the recovery policy is set to prioritize data integrity (default) and any down nodes are
	// not recoverable or have exceeded retries, we need to abort here to wait for user action.
	// If the MirWatchdog is running, this should also trigger the MIR state.
	if c.cluster.GetRecoveryPolicy() == couchbasev2.PrioritizeDataIntegrity {
		manualActionNodes := []string{}

		for name, m := range r.couchbase.DownNodes {
			if !c.isPodRecoverable(m) {
				manualActionNodes = append(manualActionNodes, name)
			}
		}

		manualActionNodes = append(manualActionNodes, nodesToRecover...)

		if len(manualActionNodes) > 0 {
			c.log.Info("Node recovery policy set to prioritize data integrity and down nodes need manual action", "cluster", c.namespacedName(), "nodes", manualActionNodes)

			r.abort("waiting for manual action of down nodes")

			return nil
		}
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

		// Skip nodes that have exceeded recovery retries, they will be failed over below.
		if c.hasExceededRecoveryMaxRetries(name) {
			continue
		}

		c.logFailedMember("Node failed", name)

		// Timeout has expired, recreate the pod.
		// Guard: if the pod already exists and has the PendingInitializationCondition, it was
		// already recreated asynchronously in a previous cycle — skip to avoid a second recreate.
		pod, podExists := c.k8s.Pods.Get(name)
		if podExists && pod.DeletionTimestamp == nil && k8sutil.HasPendingInitializationCondition(pod) {
			c.log.V(1).Info("Down pod already recreated and pending initialization, skipping", "cluster", c.namespacedName(), "name", name)
			continue
		}

		c.lastRecoveryAttemptTime[name] = time.Now()
		metrics.PodTimeSinceLastRecoveryAttemptMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Set(0)
		c.persistRecoveryTimestamps()

		if err := c.recreatePod(m, true); err != nil {
			metrics.PodRecoveryFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Inc()

			return fmt.Errorf("pod recovery failed for member %s: %w", name, err)
		}

		c.log.Info("Pod recovering", "cluster", c.namespacedName(), "name", name)

		c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))
		delete(c.recoveryTime, name)

		metrics.PodRecoveriesMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, name})...).Inc()
		c.lastSuccessfulRecoveryTime[name] = time.Now()
		metrics.PodTimeSinceLastSuccessfulRecoveryMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, name})...).Set(0)
		c.persistRecoveryTimestamps()

		recovered++
	}

	if recovered != 0 {
		r.abort("waiting for cluster recovery")

		return nil
	}

	// Build the OTP failover list.
	// Nodes that exceeded max retries are always added
	// we only reach here under PrioritizeUptime since PrioritizeDataIntegrity returns above
	nodesToFailover := couchbaseutil.OTPNodeList{}
	failoverNames := []string{}

	for _, name := range nodesToRecover {
		m := r.couchbase.DownNodes[name]

		if err := c.resetRecoveryAttempts(name); err != nil {
			c.log.Error(err, "Failed to reset recovery attempts before failover", "cluster", c.namespacedName(), "name", name)
		}

		nodesToFailover = append(nodesToFailover, m.GetOTPNode())
		failoverNames = append(failoverNames, name)
	}

	// Add all remaining unrecoverable down/failed nodes when PrioritizeUptime is set.
	if c.cluster.GetRecoveryPolicy() == couchbasev2.PrioritizeUptime {
		for _, member := range r.couchbase.DownNodes {
			c.log.Info("Failing over node", "cluster", c.namespacedName(), "name", member.Name())

			nodesToFailover = append(nodesToFailover, member.GetOTPNode())
			failoverNames = append(failoverNames, member.Name())
		}

		for _, member := range r.couchbase.FailedNodes {
			nodesToFailover = append(nodesToFailover, member.GetOTPNode())
			failoverNames = append(failoverNames, member.Name())
		}
	}

	if len(nodesToFailover) > 0 {
		c.log.Info("Forcing failover of nodes", "cluster", c.namespacedName())

		if err := couchbaseutil.Failover(nodesToFailover, true).On(c.api, c.getMigratingReadyTarget()); err != nil {
			return err
		}

		for _, name := range failoverNames {
			c.raiseEventCached(k8sutil.MemberFailedOverEvent(name, c.cluster))
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
		// Skip pods that are already Terminating: they were CBS-ejected in the previous
		// reconcile cycle and MemberRemoved was already emitted from rebalanceWithRetriesOnVerifyFails.
		// Re-emitting here would produce duplicate events while Kubernetes finishes removing the pod.
		if pod, ok := c.k8s.Pods.Get(name); ok && pod.DeletionTimestamp != nil {
			if err := c.destroyMember(name, r.shouldRemoveVolumes(name)); err != nil {
				return fmt.Errorf("unable to remove unclustered node: %w", err)
			}

			continue
		}

		if err := c.destroyMember(name, r.shouldRemoveVolumes(name)); err != nil {
			return fmt.Errorf("unable to remove unclustered node: %w", err)
		}

		c.log.Info("Pod unclustered, deleting", "cluster", c.namespacedName(), "name", name)
	}

	return nil
}

// attemptSilentReAdd tries to cancel a FailedAdd state in CBS and re-add the node
// without raising events or recreating the pod. Returns true if the re-add succeeded.
func (c *Cluster) attemptSilentReAdd(m couchbaseutil.Member) bool {
	config := c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		return false
	}
	url := m.GetDNSName()
	if _, ok := c.cluster.Annotations[constants.AddNodeInsecureAnnotation]; ok {
		url = m.GetHostURLPlaintext()
	}
	services, err := couchbaseutil.ServiceListFromStringArray(
		couchbasev2.ServiceList(config.Services).StringSlice())
	if err != nil {
		return false
	}
	if cancelErr := couchbaseutil.CancelAddNode(m.GetOTPNode()).RetryFor(extendedRetryPeriod).On(c.api, c.getMigratingReadyTarget()); cancelErr != nil {
		return false
	}
	return c.AddNodeWithPodReadyCheck(m, url, services, c.readyMembers(), silentReAddRetryPeriod) == nil
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
			// Guard: only skip if the pod was already async-recreated (has PendingInit).
			// A pod that was successfully AddNode'd (condition cleared) but then crashed
			// into FailedAdd does not have PendingInit and must be recreated.
			pod, podExists := c.k8s.Pods.Get(name)
			if podExists && pod.DeletionTimestamp == nil && k8sutil.HasPendingInitializationCondition(pod) {
				c.log.V(1).Info("Pending add pod already recreated and pending initialization, skipping", "cluster", c.namespacedName(), "name", name)
				continue
			}

			// If the pod is still running, attempt a silent cancel-and-retry before
			// falling back to pod recreation.
			if podExists && pod.DeletionTimestamp == nil && c.attemptSilentReAdd(m) {
				r.abort("FailedAdd node silently re-added after retry: " + name)
				return nil
			}

			if err := c.recreatePod(m, false); err != nil {
				c.log.Error(err, "Pending add pod cannot be recovered", "cluster", c.namespacedName(), "name", name)
				r.abort("unable to recover pod pending addition")

				return nil
			}

			c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))

			r.abort("pending add pod recreated asynchronously, waiting for initialization: " + name)

			return nil
		}

		// For stateless (non-recoverable) pods, attempt a silent cancel-and-retry
		// before escalating to cancelAddMember which fires a FailedAddNode event.
		if pod, podExists := c.k8s.Pods.Get(name); podExists && pod.DeletionTimestamp == nil {
			if c.attemptSilentReAdd(m) {
				r.abort("FailedAdd stateless node silently re-added after retry: " + name)
				return nil
			}
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

	ready := c.readyMembers()
	if ready.Empty() {
		return nil
	}

	r.log()

	for name, m := range r.couchbase.AddBackNodes {
		if terminating, err := c.isPodTerminating(m); err != nil {
			return err
		} else if terminating {
			c.log.Info("Add back node is terminating", "cluster", c.namespacedName(), "name", name)
			r.abort("add back node is terminating")
			return nil
		}

		err := c.verifyMemberVolumes(m)
		if err != nil {
			c.log.Error(err, "Failed pod cannot be recovered, volumes unhealthy", "cluster", c.namespacedName(), "name", name)

			r.removeMember(m)
			c.raiseEvent(k8sutil.FailedAddBackNodeEvent(name, c.cluster))

			break
		}

		// Set recovery type as delta for data nodes
		sc := c.cluster.Spec.GetServerConfigByName(m.Config())
		if sc == nil {
			c.log.Info("Add back pod not in the specification, deleting", "cluster", c.namespacedName(), "name", name, "class", m.Config())

			r.ejectMembers.Add(m)

			break
		}

		recoveryType := couchbaseutil.RecoveryTypeFull

		if couchbasev2.ServiceList(sc.Services).Contains(couchbasev2.DataService) {
			recoveryType = couchbaseutil.RecoveryTypeDelta
		}

		c.log.Info("Setting recovery type", "cluster", c.namespacedName(), "name", name, "type", recoveryType)

		if err := couchbaseutil.SetRecoveryType(m.GetOTPNode(), recoveryType).On(c.api, ready); err != nil {
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
		c.log.Info("Pods failed over", "cluster", c.namespacedName())

		if c.isPodRecoverable(m) && !c.hasExceededRecoveryMaxRetries(name) {
			pod, podExists := c.k8s.Pods.Get(name)
			if podExists && pod.DeletionTimestamp == nil && k8sutil.HasPendingInitializationCondition(pod) {
				c.log.V(1).Info("Failed pod already recreated and pending initialization, skipping", "cluster", c.namespacedName(), "name", name)
				continue
			}

			c.lastRecoveryAttemptTime[name] = time.Now()
			metrics.PodTimeSinceLastRecoveryAttemptMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Set(0)
			c.persistRecoveryTimestamps()

			if err := c.recreatePod(m, true); err != nil {
				c.log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", name, "reason", err)

				metrics.PodRecoveryFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Inc()

				r.abort("unable to recover pod")

				return nil
			}

			c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))

			metrics.PodRecoveriesMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Inc()
			c.lastSuccessfulRecoveryTime[name] = time.Now()
			metrics.PodTimeSinceLastSuccessfulRecoveryMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Set(0)
			c.persistRecoveryTimestamps()

			r.abort("failed pod recreated asynchronously, waiting for initialization: " + name)

			return nil
		}

		if c.hasExceededRecoveryMaxRetries(name) {
			c.log.Info("Pod recovery max retries exceeded, removing member",
				"cluster", c.namespacedName(), "name", name,
				"maxRetries", c.config.PodRecoveryMaxRetries,
				"attempts", c.getRecoveryAttempts(name))
		}

		c.log.Info("Pod failed, deleting", "cluster", c.namespacedName(), "name", name)

		r.removeMember(m)
	}

	return nil
}

func (r *ReconcileMachine) handleUnknownServerConfigs(c *Cluster) error {
	// If a server configuration was deleted in a spec update then we will clean
	// up all of the nodes from that server config here.
	for name, m := range r.clusteredMembers {
		r.removeMemberIfUnknownServerConfig(c, name, m)
	}

	return nil
}

func (r *ReconcileMachine) removeMemberIfUnknownServerConfig(c *Cluster, name string, m couchbaseutil.Member) {
	if c.cluster.Spec.GetServerConfigByName(m.Config()) == nil {
		c.log.Info("Node does not match any server config in the specification. Ejecting from cluster", "cluster", c.namespacedName(), "name", name, "class", m.Config())

		// Check the node is actually active before we attempt to delete the log volumes.
		info := &couchbaseutil.PoolsInfo{}
		if err := couchbaseutil.GetPools(info).RetryFor(10*time.Second).On(c.api, m); err != nil {
			r.abort("unknown node is going down")
		}

		r.removeMemberUser(m)
	}
}

type allMetrics struct {
	idx, data, view couchbaseutil.StatsRangeMetrics
}

// getMetrics retrives metrics for data, index and views services.

func getMetrics(c *Cluster) allMetrics {
	getServiceMetrics := func(serviceName string) couchbaseutil.StatsRangeMetrics {
		metrics := couchbaseutil.StatsRangeMetrics{}
		if err := couchbaseutil.GetStatsRangeAvgMetrics(serviceName, &metrics).On(c.api, c.readyMembers()); err != nil {
			c.log.V(1).Info("Error getting "+serviceName+" metrics when server class removal queue is being populated", "cluster", c.namespacedName(), "error", err)
		}
		if serviceName == "index" {
			metrics = couchbaseutil.StatsRangeMetrics{}
		}
		return metrics
	}

	return allMetrics{
		idx:  getServiceMetrics("index"),
		data: getServiceMetrics("data"),
		view: getServiceMetrics("views"),
	}
}

func parseSizePerMember(memberName string, allmetrices allMetrics) float64 {
	sizeByService := func(sm couchbaseutil.StatsRangeMetrics) float64 {
		var size float64

		for _, data := range sm.Data {
			if len(data.Metric.Nodes) > 0 {
				if !strings.Contains(data.Metric.Nodes[0], memberName) {
					continue
				}
			}

			// there will be two values and we are just interested in one of thems.
			if len(data.Values) > 0 {
				value := data.Values[0]

				s, ok := value[1].(string)
				if ok {
					i, err := strconv.ParseFloat(s, 64)
					if err != nil {
						log.V(1).Info("Error parsing size to float64 format", "error", err)
					}

					size += i
				}
			}
		}

		return size
	}

	return sizeByService(allmetrices.idx) + sizeByService(allmetrices.data) + sizeByService(allmetrices.view)
}

// populateRemovalQueuePerServerClass enqueues pod(server) names which are version 7.0+.
func populateRemovalQueuePerServerClass(serverClass string, clusteredMembers couchbaseutil.MemberSet, c *Cluster) error {
	serverConf := c.cluster.Spec.GetServerConfigByName(serverClass)
	if serverConf == nil {
		return fmt.Errorf("server class not found %s: %w", serverClass, errors.NewStackTracedError(errors.ErrServerClassNotFound))
	}

	prioritizedRemoveCandidates := couchbaseutil.NewMemberSet()

	getCandidatesFuncs := []func() (couchbaseutil.MemberSet, error){
		func() (couchbaseutil.MemberSet, error) {
			candidates, _, err := c.needsUpgrade()
			return candidates, err
		},
		func() (couchbaseutil.MemberSet, error) {
			candidates, _, _, _, err := c.getBucketMigrationCandidates()
			return candidates, err
		},
		c.getNodeServiceMismatchCandidates,
	}

	for _, getCandidatesFunc := range getCandidatesFuncs {
		// Get any candidates that need to be removed.
		candidates, err := getCandidatesFunc()
		if err != nil {
			return err
		}

		// Only keep candidates that are in the server class.
		candidates = candidates.Intersect(clusteredMembers)

		// Add the candidates to the list of candidates to be removed.
		prioritizedRemoveCandidates.Merge(candidates)
	}

	// Add the candidates to the list of candidates to be removed.
	var queueMembers []string
	queueMembers = append(queueMembers, prioritizedRemoveCandidates.Names()...)

	// Get remaining members that aren't in the above list.
	remainingMembers := clusteredMembers.Diff(prioritizedRemoveCandidates)

	// map used to avoid any chance of duplicate member names.
	memberToSize := map[string]float64{}
	memNames := make([]string, 0, len(memberToSize))

	allm := getMetrics(c)

	for name := range remainingMembers {
		size := parseSizePerMember(name, allm)

		memberToSize[name] = size

		memNames = append(memNames, name)
	}

	// sort the slice of member names based on the size.
	sort.Slice(memNames, func(i, j int) bool {
		return memberToSize[memNames[i]] < memberToSize[memNames[j]]
	})

	queueMembers = append(queueMembers, memNames...)

	c.scheduler.EnQueueRemovals(serverClass, queueMembers)

	return nil
}

func (r *ReconcileMachine) handleRemoveNode(c *Cluster) error {
	// Do not remove any node while other nodes are being recovered or pending rebalance.
	// This covers the three states a recovering node can be in:
	//   FailedNodes    (unhealthy+inactiveFailed): pod recreated from PVC, CBS not yet responsive
	//   AddBackNodes   (healthy+inactiveFailed):   pod responsive, SetRecoveryType not yet called
	//   PendingAddNodes (healthy+inactiveAdded):   SetRecoveryType called, awaiting rebalance
	// Removing a node in any of these states risks data loss and confuses the topology
	// count (e.g. temporary upgrade-replacement pods appear as permanent members, causing
	// the scheduler to eject an original node instead of the replacement).
	if len(r.couchbase.FailedNodes) > 0 || len(r.couchbase.AddBackNodes) > 0 || len(r.couchbase.PendingAddNodes) > 0 {
		return nil
	}

	var deletions []couchbasev2.ServerConfig

	var scheduledScaling couchbasev2.ScalingMessageList

	for _, serverSpec := range c.cluster.Spec.Servers {
		// Check to see if we need to remove anything
		members := r.clusteredMembers.GroupByServerConfig(serverSpec.Name)

		// falls back to the old way of removing node, if version is below 7.0.
		if cbVersionOver7, err := c.IsAtLeastVersion("7.0.0"); cbVersionOver7 && err == nil {
			err := populateRemovalQueuePerServerClass(serverSpec.Name, members, c)
			if err != nil {
				return fmt.Errorf("failed to populate removal queue for server class '%s': %w", serverSpec.Name, err)
			}
		}

		existingNodes := members.Size()
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

	// If we need to preserve ready CNG containers, check that the number of pods to remove won't breach the limit.
	// We abort the scale down wholesale as we can't determine at this point which members will be removed.
	if preserveReadyCNG := c.cluster.Spec.PreserveCNGReadyInstances(); preserveReadyCNG > 0 {
		readyCNGInstances := c.getReadyCNGMembers()
		if len(readyCNGInstances) > 0 && len(deletions) > len(readyCNGInstances)-preserveReadyCNG {
			c.log.Info("Aborting scale down to maintain CNG availability", "cluster", c.namespacedName(), "deletions", len(deletions), "readyCNGInstances", len(readyCNGInstances))
			return nil
		}
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

	arbiterNodesSupported, err := couchbaseutil.VersionAfter(c.cluster.Status.CurrentVersion, "7.6.0")
	if err != nil {
		return err
	}

	for _, serverSpec := range c.cluster.Spec.Servers {
		whereEqualsServerConfig := func(m couchbaseutil.Member) bool {
			return m.Config() == serverSpec.Name
		}
		// clusteredMembers excludes pending-init pods; add them back so we don't create duplicates.
		existingNodes := r.clusteredMembers.GroupBy(whereEqualsServerConfig).Size()
		existingNodes += r.pendingInitPods.GroupBy(whereEqualsServerConfig).Size()

		nodesToCreate := serverSpec.Size - existingNodes
		if nodesToCreate <= 0 {
			continue
		}

		if (len(serverSpec.Services) == 0 || serverSpec.Services[0] == couchbasev2.AdminService) && !arbiterNodesSupported {
			c.log.Info("[WARN] Arbiter nodes are not supported for this cluster version, skipping node addition", "cluster", c.namespacedName(), "server", serverSpec.Name)
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

	// During a forward upgrade with previousVersionPodCount, some new pods may need to be
	// created with the old (baseline) version. This ensures that scaling up respects
	// the user's intent to keep a certain number of pods on the previous version.
	if err := c.applyPreviousVersionToNewPods(additions); err != nil {
		c.log.Error(err, "Failed to apply previous version to new pods, all new pods will use the current spec image", "cluster", c.namespacedName())
	}

	r.log()

	// Set the scaling status *before* we start adding any nodes.
	// This means an external observer can wait for a node addition and the
	// cluster will already report as scaling (aka not fully healthy)
	c.cluster.Status.SetScalingUpCondition(scheduledScaling.BuildMessage())

	if err := c.updateCRStatus(); err != nil {
		c.log.Error(err, "Cluster status update failed", "cluster", c.namespacedName())
	}

	memberResults, err := c.addMembers(additions...)
	if err != nil {
		c.log.Error(err, "Pod addition to cluster failed", "cluster", c.namespacedName())
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
			c.log.Error(result.Err, "Pod addition to cluster failed", "cluster", c.namespacedName(), "pod", result.Member.Name())
		} else if c.cluster.IsMigrationCluster() {
			// Migration clusters perform CBS add-node synchronously inside addMembersToTarget,
			// so the pod is already known to Couchbase and it is safe to mark it as clustered
			// and set needsRebalance=true here.
			r.addMember(result.Member)
		}
		// Non-migration (async): the pod has PendingInitializationCondition and has NOT been
		// CBS-added yet. Calling r.addMember() would set needsRebalance=true and cause
		// handleRebalance to attempt a premature rebalance.
	}

	if len(errs) == 0 {
		if !c.cluster.IsMigrationCluster() {
			r.abort("scale-up pods created, waiting for async initialization")
		}
		return nil
	}

	return errors.Join(errs...)
}

// handleVolumeExpansion attempts to perform online expansion of Persistent Volumes.
func (r *ReconcileMachine) handleVolumeExpansion(c *Cluster) error {
	if !c.cluster.Spec.HasAnyVolumeExpansionEnabled() {
		return nil
	}

	// When using swap rebalance, skip volume expansion for nodes that will be replaced.
	// Pods whose only spec change is a PVC update are excluded from the skip set so that online volume expansion handles them instead of swap rebalance unnecessarily replacing the pod.
	upgradeCandidatesSet := couchbaseutil.MemberSet{}
	if c.cluster.GetUpgradeProcess() == couchbasev2.SwapRebalance {
		changes, err := c.detectChangeSets()
		if err != nil {
			c.log.Error(err, "Failed to check upgrade candidates, proceeding with volume expansion", "cluster", c.namespacedName())
		} else {
			upgradeCandidatesSet.Merge(changes.VersionOnly)
			upgradeCandidatesSet.Merge(changes.Both)
			for name, member := range changes.SpecOnly {
				if _, hasPVCChange := changes.ChangedPVCs[name]; !hasPVCChange {
					upgradeCandidatesSet[name] = member
				}
			}
		}
	}

	// Online upgrade of Persistent volumes for each member
	for _, name := range c.members.Names() {
		member := c.members[name]

		// Get member config
		serverClass := c.cluster.Spec.GetServerConfigByName(member.Config())
		if serverClass == nil {
			continue
		}

		// Skip volume expansion if this member needs upgrading and we're using swap rebalance
		if !upgradeCandidatesSet.Empty() && upgradeCandidatesSet.Contains(member.Name()) {
			c.log.V(2).Info("Skipping volume expansion for node that will be upgraded via Swap Rebalance", "cluster", c.namespacedName(), "node", member.Name())
			continue
		}

		// Get state of persistent volumes for the member
		pvcState, err := k8sutil.GetPodVolumes(c.k8s, member, c.cluster, *serverClass)
		if err != nil {
			return err
		} else if pvcState == nil {
			continue
		}

		for _, pvc := range pvcState.List() {
			switch {
			// When online resize failed for any of the member volumes then proceed with
			// normal rolling upgrade since the Pod must be recreated.
			case pvcState.IsResizeFailed(pvc.Name):
				err := pvcState.GetReasonForResizeFailed(pvc.Name)
				c.log.Info("Volume expansion failed, falling back to rolling upgrade", "cluster", c.namespacedName(), "node", member.Name(), "volume", pvc.Name, "error", err)
				c.raiseEvent(k8sutil.ExpandVolumeFallbackEvent(pvc.Name, c.cluster, err.Error()))
				c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionExpandingVolume)

				return nil
			// Check if a volume expansion is already in progress and end reconciliation loop if so.
			case pvcState.IsExpanding(pvc.Name):
				c.log.Info("Volume expansion is in progress", "cluster", c.namespacedName(), "node", member.Name(), "volume", pvc.Name)

				return nil
			// Check if volume spec has been updated and apply changes.
			case pvcState.IsUpdated(pvc.Name):
				requestedClaim, err := pvcState.Update(c.k8s, pvc.Name)
				if err != nil {
					return err
				}

				// Flag that a volume expansion is occurring.
				c.cluster.Status.SetExpandingVolumeCondition()

				// Log and raise event that expansion started.
				currentSize := k8sutil.GetVolumeStorageSize(pvc)
				requestedSize := k8sutil.GetVolumeStorageSize(requestedClaim)
				c.raiseEvent(k8sutil.ExpandVolumeStartedEvent(pvc.Name, currentSize, requestedSize, c.cluster))
				c.log.Info("Volume expansion started", "cluster", c.namespacedName(), "node", member.Name(), "volume", pvc.Name, "current", currentSize, "requested", requestedSize)

				metrics.VolumeExpansionMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, pvc.Name})...).Inc()

				return nil
			}
		}
	}

	// We can't raise expansion completed events for each PVC as there's no reliable way to determine when a specific volume has "completed" it's expansion.
	// Instead, if we get to this point in the method, we can assume there are no more PVC's that are expanding/failed expanding/updated, so we'll raise a single event
	// if the condition still exists.
	if c.cluster.HasCondition(couchbasev2.ClusterConditionExpandingVolume) {
		c.log.Info("Volume expansion completed", "cluster", c.namespacedName())
		c.raiseEvent(k8sutil.ExpandVolumeSucceededEvent(c.cluster))
	}

	// Clear the condition regardless of whether we raised an event or not for safety.
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionExpandingVolume)

	return nil
}

func (c *Cluster) selectUpgradeCandidatesIgnoringOrchestrator(
	candidates couchbaseutil.MemberList,
	zoneChanges map[string]couchbaseutil.Member,
	pvcChanges map[string]couchbaseutil.Member,
) (couchbaseutil.MemberSet, bool, bool, error) {
	zoneChange := false
	pvcChange := false

	candidateList, err := c.selectOrderedUpgradeCandidates(candidates, "")
	if err != nil {
		return nil, false, false, err
	}

	for _, candidate := range candidateList {
		if _, ok := zoneChanges[candidate.Name()]; ok {
			zoneChange = true
		}
		if _, ok := pvcChanges[candidate.Name()]; ok {
			pvcChange = true
		}
	}

	return candidateList, zoneChange, pvcChange, nil
}

// selectUpgradeCandidates returns the candidates that can be upgraded this turn, determined by the upgrade strategy and
// the maximum number of candidates that can be upgraded at once.
func (c *Cluster) selectUpgradeCandidates(candidates couchbaseutil.MemberSet, orchestrator string) (couchbaseutil.MemberSet, error) {
	return c.selectOrderedUpgradeCandidates(candidates.ToList(), orchestrator)
}

func (c *Cluster) selectOrderedUpgradeCandidates(candidates couchbaseutil.MemberList, orchestrator string) (couchbaseutil.MemberSet, error) {
	if c.cluster.GetUpgradeStrategy() != couchbasev2.RollingUpgrade {
		return candidates.ToSet(), nil
	}

	// Remove orchestrator from list if rolling upgrade and orchestrator is specified.
	if len(candidates) != 1 && orchestrator != "" {
		candidates, _ = separateOrderedCandidatesAndOrchestrator(candidates, orchestrator)
	}

	maxUpgradable, err := c.cluster.GetMaxUpgradable()
	if err != nil {
		return nil, err
	}

	// Apply the max upgradable constraint
	// The ordering of the candidates does not matter as the upgrade process will be done in parallel.
	constrained := couchbaseutil.MemberSet{}
	for _, candidate := range candidates {
		if constrained.Size() >= maxUpgradable {
			break
		}

		constrained.Add(candidate)
	}

	// If using CNG, preserve a minimum number of ready instances by removing upgrade candidates.
	if c.cluster.Spec.Networking.CloudNativeGateway != nil && len(candidates) > 0 {
		filteredCNGCandidates := c.filterUpgradeCandidatesToPreserveCNG(constrained)
		preservedCandidates := constrained.Diff(filteredCNGCandidates)
		for _, preserved := range preservedCandidates {
			c.log.Info("Preserving candidate to maintain CNG availability", "cluster", c.namespacedName(), "candidate", preserved.Name())
		}
		constrained = filteredCNGCandidates
	}

	return constrained, nil
}

func separateOrderedCandidatesAndOrchestrator(candidates couchbaseutil.MemberList, orchestratorName string) (couchbaseutil.MemberList, couchbaseutil.Member) {
	var candidatesNoOrchestrator = couchbaseutil.MemberList{}

	var orchestrator couchbaseutil.Member

	if orchestratorName == "undefined" {
		return candidates, nil
	}

	for _, candidate := range candidates {
		if strings.Contains(orchestratorName, candidate.Name()) {
			orchestratorCandidate := candidate
			orchestrator = orchestratorCandidate

			continue
		}

		candidatesNoOrchestrator = append(candidatesNoOrchestrator, candidate)
	}

	return candidatesNoOrchestrator, orchestrator
}

func separateCandidatesAndOrchestrator(candidates couchbaseutil.MemberSet, orchestratorName string) (couchbaseutil.MemberSet, couchbaseutil.Member) {
	candidatesNoOrchestrator, orchestrator := separateOrderedCandidatesAndOrchestrator(candidates.ToList(), orchestratorName)
	return candidatesNoOrchestrator.ToSet(), orchestrator
}

func (r *ReconcileMachine) startGracefulFailover(candidate couchbaseutil.Member, c *Cluster) error {
	otpNodeList := couchbaseutil.OTPNodeList{candidate.GetOTPNode()}

	// We can retry on 500 and 503 codes.
	return retryutil.RetryUntilErrorOrSuccess(time.Minute, 5*time.Second, func() (error, bool) {
		if err := couchbaseutil.GracefulFailover(otpNodeList).On(c.api, candidate); err != nil {
			var failedReqErr couchbaseutil.FailedRequestError
			if goerrors.As(err, &failedReqErr) {
				switch failedReqErr.StatusCode {
				case http.StatusServiceUnavailable, http.StatusInternalServerError:
					return nil, false
				}
			}

			return err, false
		}
		return nil, true
	})
}

//nolint:gocognit
func (r *ReconcileMachine) gracefullyFailoverNode(candidate couchbaseutil.Member, c *Cluster) error {
	clusterInfoInitial := &couchbaseutil.ClusterInfo{}
	if err := couchbaseutil.GetPoolsDefault(clusterInfoInitial).On(c.api, candidate); err != nil {
		return err
	}

	for _, node := range clusterInfoInitial.Nodes {
		if node.HostName != candidate.GetHostName() {
			continue
		}

		if !c.members.Contains(node.HostName.GetMemberName()) {
			return fmt.Errorf("%w, %s", ErrNodeNotInCluster, node.HostName)
		}

		if strings.Compare("active", node.Membership) != 0 {
			return fmt.Errorf("%w %s", ErrNodeNotActive, node.HostName)
		}

		if node.Membership == "inactiveFailed" || node.Membership == "inactiveAdded" {
			return fmt.Errorf("%w %s", ErrNodeNotActive, node.HostName)
		}
	}

	initialCounters := clusterInfoInitial.Counters

	if err := r.startGracefulFailover(candidate, c); err != nil {
		return err
	}

	// Wait for the graceful failover to complete
	err := retryutil.RetryUntilErrorOrSuccess(30*time.Minute, time.Second, func() (error, bool) {
		clusterInfo := couchbaseutil.ClusterInfo{}

		if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(c.api, candidate); err != nil {
			return err, false
		}

		// Ensure the graceful failover start counter is incremented
		if clusterInfo.Counters["graceful_failover_start"] != (initialCounters["graceful_failover_start"] + 1) {
			return ErrFailoverStartCounterNotIncremented, false
		}

		// Compare the counters to see if anything has changed
		if len(initialCounters) > len(clusterInfo.Counters) {
			return ErrUnexpectedCounterChange, false
		}

		// We track the counters to ensure that the rebalance completes and graceful failover completes.
		// If the rebalance status completes, but another one starts (e.g. auto-failover or user intervention)
		// then we will fail because we detect that some other counter has changed.
		for name, curVal := range clusterInfo.Counters {
			oldVal := initialCounters[name]
			switch name {
			case "graceful_failover_start", "graceful_failover_success":
				continue
			// We can expect the failover counters to increment by 1 because these get incremented by server
			// before the graceful_failover_success.
			// The order is graceful_failover_start, failover, failover_complete, graceful_failover_success.
			case "failover", "failover_complete":
				if curVal > oldVal+1 {
					return ErrUnexpectedCounterChange, false
				}
				continue
			}

			if curVal != oldVal {
				return ErrUnexpectedCounterChange, false
			}
		}

		// If the rebalance is complete check that the graceful failover success counter is incremented
		if clusterInfo.RebalanceStatus == couchbaseutil.RebalanceStatusNone {
			if clusterInfo.Counters["graceful_failover_success"] != (initialCounters["graceful_failover_success"] + 1) {
				return ErrFailoverSuccessCounterNotIncremented, false
			}
		}

		if clusterInfo.Counters["graceful_failover_success"] == (initialCounters["graceful_failover_success"] + 1) {
			return nil, true
		}

		return nil, false
	})

	if err != nil {
		return fmt.Errorf("graceful failover failed: %w", err)
	}

	return nil
}

// failoverNodeForDeltaRecovery will gracefully failover data nodes and hardfailover other nodes.
func (r *ReconcileMachine) failoverNodeForInPlaceUpgrade(candidate couchbaseutil.Member, c *Cluster) (bool, error) {
	serverClass := c.cluster.Spec.GetServerConfigByName(candidate.Config())
	services := serverClass.Services
	dataNode := false

	for _, service := range services {
		if service == couchbasev2.DataService {
			dataNode = true
			break
		}
	}

	if dataNode {
		// Graceful failover for data nodes
		if err := r.gracefullyFailoverNode(candidate, c); err != nil {
			return false, err
		}

		return true, nil
	}

	// Hard failover for other nodes
	c.log.Info("Unable to perform graceful failover on node. Reverting to hard failover.", "cluster", c.namespacedName(), "name", candidate.Name())

	otpNodeList := couchbaseutil.OTPNodeList{candidate.GetOTPNode()}

	if err := couchbaseutil.Failover(otpNodeList, false).RetryFor(1*time.Minute).On(c.api, candidate); err != nil {
		return false, err
	}

	return true, nil
}

func (r *ReconcileMachine) recreateNode(c *Cluster, candidate couchbaseutil.Member, canDeltaRecover bool) error {
	if len(c.members) > 1 {
		ready := c.readyMembers()
		if ready.Empty() {
			c.log.Info("No ready members to set recovery type, yielding", "cluster", c.namespacedName())
			return fmt.Errorf("%w: no ready members to set recovery type", errors.NewStackTracedError(ErrReconcileInhibited))
		}

		if canDeltaRecover {
			if err := couchbaseutil.SetRecoveryType(candidate.GetOTPNode(), couchbaseutil.RecoveryTypeDelta).On(c.api, ready); err != nil {
				return err
			}
		} else {
			c.log.Info("Unable to set delta recovery type. Reverting to full recovery.", "cluster", c.namespacedName())

			if err := couchbaseutil.SetRecoveryType(candidate.GetOTPNode(), couchbaseutil.RecoveryTypeFull).On(c.api, ready); err != nil {
				return err
			}
		}
	}

	if err := c.recreatePod(candidate, false); err != nil {
		return err
	}

	// Mark the new pod with the durable upgrade tracking annotation so later async
	// phases can attribute metrics and gate the stabilization period.
	if err := k8sutil.SetPodUpgradeTracking(c.k8s, candidate.Name(), string(couchbasev2.InPlaceUpgrade)); err != nil {
		c.log.Error(err, "Failed to mark pod with upgrade tracking; upgrade metrics may not fire",
			"cluster", c.namespacedName(), "pod", candidate.Name())
	}

	return nil
}

func (c *Cluster) multipleInPlaceUpgradesSupported(candidates couchbaseutil.MemberSet) (bool, error) {
	if c.cluster.Spec.Upgrade == nil || (c.cluster.Spec.Upgrade != nil && c.cluster.Spec.Upgrade.UpgradeOrderType != couchbasev2.UpgradeOrderTypeServerGroups) {
		return false, nil
	}

	if len(candidates) >= len(c.callableMembers)/2 {
		c.log.Info("Unable to perform multiple in-place upgrades at once without losing quorum", "cluster", c.namespacedName())
		return false, nil
	}

	buckets, err := c.gatherBuckets()
	if err != nil {
		return false, err
	}

	for _, bucket := range buckets {
		if bucket.BucketReplicas <= len(candidates) {
			c.log.Info("Unable to perform multiple in-place upgrades at once due to insufficient bucket replicas", "cluster", c.namespacedName(), "bucket", bucket.BucketName)
			return false, nil
		}
	}

	return true, nil
}

// nolint:gocognit
func (r *ReconcileMachine) handleInPlaceUpgrade(c *Cluster, candidates couchbaseutil.MemberSet, targetVersion string) error {
	upgraded := len(c.members) - len(candidates)

	status := &couchbasev2.UpgradeStatus{
		TargetCount: upgraded,
		TotalCount:  len(c.members),
	}

	// Flag that an upgrade is in action, validation will use this to control what
	// resource modifications are allowed.
	if err := c.reportUpgrade(status); err != nil {
		metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

		return err
	}

	// When multipleInPlaceUpgradesSupported returns true (server-group upgrade),
	// the entire candidates set is processed in one cycle: all are failover'd and
	// recreated asynchronously, then handleReadyPendingPod / handleRebalance
	// complete the upgrade across subsequent cycles.
	// When false (quorum or replica safety check failed), constrain to one
	// candidate even if selectUpgradeCandidates passed multiple through, matching
	// the original sync one-at-a-time behavior that guarded against quorum loss.
	canDoMultipleInPlaceUpgrades, err := c.multipleInPlaceUpgradesSupported(candidates)
	if err != nil {
		return err
	}

	if !canDoMultipleInPlaceUpgrades && len(candidates) > 1 {
		// Safety check failed — limit to a single candidate this cycle.
		// The remaining candidates will be picked up in subsequent cycles.
		for _, first := range candidates {
			candidates = couchbaseutil.NewMemberSet(first)
			break
		}
	}

	for _, candidate := range candidates {
		if err := c.scheduler.Upgrade(candidate.Config(), candidate.Name()); err != nil {
			metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

			return err
		}

		serverClass := c.cluster.Spec.GetServerConfigByName(candidate.Config())
		if serverClass == nil {
			continue
		}

		// Member already has correct version and image from getUpgradeCandidates()
		// Update PVC annotations to match
		if c.k8s.PersistentVolumeClaims != nil {
			// Update volumes
			pvcState, err := k8sutil.GetPodVolumes(c.k8s, candidate, c.cluster, *serverClass)
			if err != nil {
				metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

				return err
			} else if pvcState != nil {
				for _, volume := range pvcState.List() {
					// Use candidate's version/image (set by getUpgradeCandidates)
					volume.Annotations[constants.PVCImageAnnotation] = candidate.GetImage()
					volume.Annotations[constants.CouchbaseVersionAnnotationKey] = candidate.Version()
					_, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(c.ctx, volume, metav1.UpdateOptions{})

					if err != nil {
						metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

						return err
					}
				}
			}
		}

		canInPlaceUpgrade := false

		if len(c.members) > 1 {
			var err error
			canInPlaceUpgrade, err = r.failoverNodeForInPlaceUpgrade(candidate, c)

			if err != nil {
				return err
			}
		}

		canInPlaceUpgrade = canInPlaceUpgrade && c.cluster.Spec.ConfigHasStatefulService(candidate.Config())

		if err := r.recreateNode(c, candidate, canInPlaceUpgrade); err != nil {
			metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
			metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

			return err
		}
	}

	// All candidates failover'd + recreated. Pods are pending async initialization
	// via handleReadyPendingPod. Yield the FSM — subsequent cycles will CBS-add
	// the pods and handleRebalance will rebalance them back in.
	r.abort("in-place upgrade pods recreated, waiting for async initialization")

	return nil
}

func (r *ReconcileMachine) handleMoveNodes(c *Cluster) error {
	// If the cluster needs a rebalance, let's do that first
	if r.needsRebalance {
		return nil
	}

	r.log()

	// Check which pods need moving
	candidates := c.needsMove()

	if candidates.Empty() {
		return nil
	}

	// Get the max upgradeable candidates
	clusterInfo := &couchbaseutil.TerseClusterInfo{}
	if err := couchbaseutil.GetTerseClusterInfo(clusterInfo).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	orchestratorName := clusterInfo.Orchestrator

	constrained, err := c.selectUpgradeCandidates(candidates, orchestratorName)

	if err != nil {
		return err
	}

	candidates = constrained

	// Is it possible to do InPlaceUpgrade if that's what they asked for?
	canDoInPlaceReschedule := true
	hasIndexCandidates := false
	indexCandidates := couchbaseutil.MemberSet{}
	nonIndexCandidates := couchbaseutil.MemberSet{}

	var targetVersion string

	for _, candidate := range candidates {
		// The target version is going to stay the same as the current version
		targetVersion = candidate.Version()

		c.log.Info("Moving node", "cluster", c.namespacedName(), "candidate", candidate.Name())

		if c.isPodReschedulable(candidate) == false {
			canDoInPlaceReschedule = false
			break
		}

		if c.cluster.ShouldRevertCandidateToSwapRebalance(candidate) {
			hasIndexCandidates = true
			indexCandidates.Add(candidate)
		} else {
			nonIndexCandidates.Add(candidate)
		}
	}

	if c.cluster.GetUpgradeProcess() == couchbasev2.DeltaRecovery {
		inPlaceUpgrade := couchbasev2.InPlaceUpgrade
		if c.cluster.Spec.Upgrade != nil {
			c.cluster.Spec.Upgrade.UpgradeProcess = inPlaceUpgrade
		} else {
			c.cluster.Spec.UpgradeProcess = &inPlaceUpgrade
		}
	}

	// Carry out the move
	// We can use the upgrade methods to do this (even though we're not changing the version)
	if c.cluster.GetUpgradeProcess() == couchbasev2.InPlaceUpgrade && canDoInPlaceReschedule {
		if hasIndexCandidates {
			c.log.Info("Reschedule candidates with index service will use SwapRebalance, non-index candidates will use InPlaceUpgrade",
				"cluster", c.namespacedName(),
				"swapRebalance", indexCandidates.Names(),
				"inPlaceUpgrade", nonIndexCandidates.Names())

			if nonIndexCandidates.Size() > 0 {
				if err := r.handleInPlaceUpgrade(c, nonIndexCandidates, targetVersion); err != nil {
					return err
				}
			}

			return r.swapRebalanceMembers(c, indexCandidates)
		}

		err := r.handleInPlaceUpgrade(c, candidates, targetVersion)
		if err != nil {
			return err
		}
	} else {
		if c.cluster.GetUpgradeProcess() == couchbasev2.InPlaceUpgrade && !canDoInPlaceReschedule {
			c.log.Info("InPlaceUpgrade not possible. Reverting to SwapRebalance.", "cluster", c.namespacedName())
		}

		return r.swapRebalanceMembers(c, candidates)
	}

	return nil
}

func (r *ReconcileMachine) checkIfValidUpgradePath() error {
	currentVersion, err := r.c.state.Get(persistence.Version)
	if err != nil {
		return err
	}

	newVersionImage := r.c.cluster.Spec.CouchbaseImage()

	newVersion, err := couchbaseutil.NewVersionFromImage(newVersionImage)
	if err != nil {
		return err
	}

	// We use 9.9.9 to represent an unknown version, so we don't need to check the upgrade path.
	if newVersion.String() == "9.9.9" {
		return nil
	}

	return couchbaseutil.CheckUpgradePath(currentVersion, newVersion.Version())
}

// handleUpgradeStabilization manages the stabilization-period guards for upgrade.
// Returns true if the upgrade should be blocked (waiting for stabilization).
func (r *ReconcileMachine) handleUpgradeStabilization(c *Cluster) bool {
	// If stabilizingMembers exist but WBU hasn't been set yet, either set it
	// (stabilization configured) or let it fall through (no stabilization).
	if len(r.stabilizingMembers) > 0 && !c.cluster.HasCondition(couchbasev2.ClusterConditionWaitingBetweenUpgrades) {
		upgradeSpec := r.c.cluster.Spec.Upgrade
		if upgradeSpec != nil && upgradeSpec.StabilizationPeriod != nil {
			r.c.cluster.Status.SetWaitingBetweenUpgrades()
		}
	}

	if c.cluster.UpgradeWaitingForStabilizationPeriod() || c.cluster.HasCondition(couchbasev2.ClusterConditionExpandingVolume) {
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionUpgrading)
		return true
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionWaitingBetweenUpgrades)

	// Stabilization period expired — clear the durable annotation so
	// stabilizingMembers won't be repopulated on the next cycle.
	for _, member := range r.stabilizingMembers {
		if err := k8sutil.ClearPodUpgradeTracking(c.k8s, member.Name()); err != nil {
			c.log.Error(err, "Failed to clear upgrade tracking annotation", "cluster", c.namespacedName(), "pod", member.Name())
		}
	}

	return false
}

//nolint:gocyclo
func (r *ReconcileMachine) handleUpgradeNode(c *Cluster) error {
	// Block upgrades while topology is unsettled or pods are pending async CBS init.
	if r.needsRebalance || len(r.couchbase.PendingAddNodes) > 0 || r.pendingInitPods.Size() > 0 {
		return nil
	}

	if r.handleUpgradeStabilization(c) {
		return nil
	}

	arbiterNodesSupported, err := couchbaseutil.VersionAfter(c.cluster.Status.CurrentVersion, "7.6.0")
	if err != nil {
		return nil
	}

	// Abort if we need to create nodes, as we can't continue the upgrade until we have the right number of nodes.
	if CheckNodesToCreate(c.cluster, r.clusteredMembers, arbiterNodesSupported) {
		return nil
	}

	// check if the upgrade is a valid upgrade path

	if err := r.checkIfValidUpgradePath(); err != nil {
		return err
	}

	blockers, err := c.getUpgradeBlockers()
	if err != nil {
		return err
	}

	if len(blockers) > 0 {
		c.log.Info("[WARN] Cluster can't be upgraded", "cluster", c.namespacedName(), "blockers", blockers)
		return nil
	}

	orderedCandidates, zoneChanges, pvcChanges, err := c.getUpgradeCandidates(true)
	if err != nil {
		return err
	}

	// Nothing to do, move along.
	if len(orderedCandidates) == 0 {
		return nil
	}

	r.log()

	// We filter out the orchestrator when appropriate earlier so we don't need to do it here
	constrainedCandidates, zoneChangeDetected, pvcChangeDetected, err := c.selectUpgradeCandidatesIgnoringOrchestrator(orderedCandidates, zoneChanges, pvcChanges)
	if err != nil {
		return err
	}

	if len(constrainedCandidates) == 0 {
		return nil
	}

	// Calculate the number of nodes already in the target state before we
	// potentially mutate the candidates.
	upgraded := len(c.members) - len(constrainedCandidates)

	// Do any events/conditions that make the upgrade observable.
	status := &couchbasev2.UpgradeStatus{
		TargetCount: upgraded,
		TotalCount:  len(c.members),
	}

	// Flag that an upgrade is in action, validation will use this to control what
	// resource modifications are allowed.
	if err := c.reportUpgrade(status); err != nil {
		return err
	}

	if c.cluster.GetUpgradeProcess() == couchbasev2.DeltaRecovery {
		inPlaceUpgrade := couchbasev2.InPlaceUpgrade
		c.cluster.Spec.UpgradeProcess = &inPlaceUpgrade
	}

	targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return err
	}

	if c.cluster.GetUpgradeProcess() == couchbasev2.InPlaceUpgrade {
		if handled, err := r.tryInPlaceUpgrade(c, constrainedCandidates, zoneChangeDetected, pvcChangeDetected, targetVersion); err != nil || handled {
			return err
		}
	}

	c.log.Info("Upgrading pods with SwapRebalance", "cluster", c.namespacedName(), "names", constrainedCandidates.Names(), "target-version", targetVersion)
	return r.swapRebalanceMembers(c, constrainedCandidates)
}

// tryInPlaceUpgrade attempts to upgrade candidates using InPlaceUpgrade.
// Returns (true, nil) if the upgrade was handled, (true, err) on error,
// or (false, nil) if the caller should fall through to SwapRebalance.
func (r *ReconcileMachine) tryInPlaceUpgrade(c *Cluster, candidates couchbaseutil.MemberSet, zoneChangeDetected bool, pvcChangeDetected bool, targetVersion string) (bool, error) {
	allCandidatesRecoverable := true
	swapRebalanceIndexNodes := false
	indexCandidates := couchbaseutil.MemberSet{}
	nonIndexCandidates := couchbaseutil.MemberSet{}

	for _, candidate := range candidates {
		if !c.isPodRecoverable(candidate) {
			allCandidatesRecoverable = false
			break
		}

		if c.cluster.ShouldRevertCandidateToSwapRebalance(candidate) {
			swapRebalanceIndexNodes = true
			indexCandidates.Add(candidate)
		} else {
			nonIndexCandidates.Add(candidate)
		}
	}

	switch {
	case allCandidatesRecoverable && !zoneChangeDetected && !swapRebalanceIndexNodes && !pvcChangeDetected:
		c.log.Info("Upgrading pods with InPlaceUpgrade", "cluster", c.namespacedName(), "names", candidates.Names(), "target-version", targetVersion)
		return true, r.handleInPlaceUpgrade(c, candidates, targetVersion)
	case allCandidatesRecoverable && zoneChangeDetected:
		c.log.Info("Pods with PVCs need zone changes. InPlaceUpgrade cannot change zones. Reverting to SwapRebalance.", "cluster", c.namespacedName())
	case allCandidatesRecoverable && pvcChangeDetected:
		c.log.Info("Pods have PVC changes and online volume expansion is disabled. Reverting to SwapRebalance.", "cluster", c.namespacedName())
	case allCandidatesRecoverable && swapRebalanceIndexNodes:
		c.log.Info("Upgrade candidates with index service will use SwapRebalance, non-index candidates will use InPlaceUpgrade",
			"cluster", c.namespacedName(),
			"swapRebalance", indexCandidates.Names(),
			"inPlaceUpgrade", nonIndexCandidates.Names())

		if nonIndexCandidates.Size() > 0 {
			if err := r.handleInPlaceUpgrade(c, nonIndexCandidates, targetVersion); err != nil {
				return true, err
			}
		}

		return true, r.swapRebalanceMembers(c, indexCandidates)
	default:
		c.log.Info("Not all pods are recoverable from persistent volumes. Reverting to SwapRebalance.", "cluster", c.namespacedName())
	}

	return false, nil
}

// CheckNodesToCreate checks if any nodes need to be created based on the desired and existing node counts.
func CheckNodesToCreate(cluster *couchbasev2.CouchbaseCluster, clusteredMembers couchbaseutil.MemberSet, arbiterNodesSupported bool) bool {
	for _, serverSpec := range cluster.Spec.Servers {
		if (len(serverSpec.Services) == 0 || serverSpec.Services[0] == couchbasev2.AdminService) && !arbiterNodesSupported {
			continue
		}

		existingNodes := clusteredMembers.GroupByServerConfig(serverSpec.Name).Size()
		nodesToCreate := serverSpec.Size - existingNodes

		if nodesToCreate > 0 {
			return true
		}
	}

	return false
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
	if err != nil {
		return err
	}

	// Update the DNS-pending members after reconciling alternative addresses.
	r.pendingDNSMembers = c.members.Intersect(podsToMemberSet(c.getPendingDNSPods()))

	// If at this point a pod is still pending DNS propagation and the timeout
	// has elapsed, we should remove the member if it has not been rebalanced (activated) into the cluster
	// If it has already been activated, we will continue to attempt to set the alternate address,
	// but we should not remove the member from the cluster.
	for _, member := range r.pendingDNSMembers.Intersect(r.couchbase.PendingAddNodes) {
		pod, found := c.k8s.Pods.Get(member.Name())
		if !found {
			continue
		}

		if c.hasDNSCheckTimeoutElapsed(pod) {
			r.removeMember(member)
			r.pendingDNSMembers.Remove(member.Name())
		}
	}

	return nil
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

//nolint:gocognit
func (r *ReconcileMachine) handleRebalance(c *Cluster) error {
	if shouldRebalance(c, r) {
		// Defer rebalance while any pod is still pending CBS initialization (not yet
		// addNode'd). Waiting ensures all replacement pods are known to CBS before
		// rebalancing, which batches add+eject operations into a single rebalance and
		// avoids a follow-up rebalance after each pod initializes individually.
		if r.pendingInitPods.Size() > 0 {
			c.log.V(1).Info("Deferring rebalance until pending pods are CBS-initialized",
				"cluster", c.namespacedName(), "pending", r.pendingInitPods.Names())
			return nil
		}

		if len(r.couchbase.ServerRebalanceReasons) > 0 {
			c.log.Info("Rebalancing Cluster", "cluster", r.c.namespacedName(), "rebalance_reasons", r.couchbase.ServerRebalanceReasons)
		}

		if err := c.state.Upsert(persistence.RebalanceClusteredMembers, strings.Join(r.clusteredMembers.Names(), ",")); err != nil {
			return err
		}

		if err := c.state.Upsert(persistence.RebalanceEjectMembers, strings.Join(r.ejectMembers.Names(), ",")); err != nil {
			return err
		}

		for _, member := range r.ejectMembers {
			if err := k8sutil.FlagPodUnready(r.c.k8s, member.Name(), "pod is being ejected"); err != nil {
				return err
			}
		}

		metrics.RebalancesTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

		if err := c.rebalanceWithRetriesOnVerifyFails(r.clusteredMembers, r.ejectMembers, r.rebalanceRetries); err != nil {
			metrics.RebalancesFailedTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

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
					c.log.Error(err, "Pod add-back failed, unable to determine recovery type", "cluster", c.namespacedName(), "name", m.Name())
					return err
				}

				node, err := info.GetNode(m.GetHostName())
				if err != nil {
					c.log.Error(err, "Pod add-back failed, unable to determine recovery type", "cluster", c.namespacedName(), "name", m.Name())
					return err
				}

				if node.RecoveryType == couchbaseutil.RecoveryTypeDelta {
					deltaNodes.Add(m)
				}
			}

			if len(deltaNodes) != 0 {
				c.log.Info("Pod add-back failed, forcing full recovery", "cluster", c.cluster.NamespacedName())

				for name, m := range deltaNodes {
					if err := couchbaseutil.SetRecoveryType(m.GetOTPNode(), couchbaseutil.RecoveryTypeFull).On(c.api, c.readyMembers()); err != nil {
						c.log.Error(err, "Pod add-back, recovery type update failed", "cluster", c.namespacedName(), "name", name)

						c.raiseEvent(k8sutil.FailedAddBackNodeEvent(name, c.cluster))

						return err
					}
				}

				// Full recovery type is now set; yield the FSM so the next cycle
				// retries the rebalance with the updated recovery type.
				r.abort("rebalance failed, retrying with full recovery")
				return nil
			}

			// Permanent rebalance failure (no delta nodes to recover from).
			// Fire upgrade failure metrics for any in-place upgrade pods waiting to be
			// rebalanced in — equivalent to sync rebalanceAfterInPlaceUpgrade failure path.
			for name := range addNodes {
				pod, ok := c.k8s.Pods.Get(name)
				if !ok {
					continue
				}
				if k8sutil.GetPodUpgradeTracking(pod) == string(couchbasev2.InPlaceUpgrade) {
					metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
					metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
				}
			}

			return fmt.Errorf("failed to rebalance: %w", err)
		}
	}

	// Clear PodPendingUpgradeBeforeEjectionCondition on members that were successfully ejected.
	// This is done here (rather than in newReconcileMachine) so that if the FSM
	// aborts before reaching handleRebalance, the durable condition survives and
	// the ejection is retried on the next cycle.
	for _, member := range r.ejectMembers {
		if err := k8sutil.ClearPodPendingUpgradeBeforeEjection(c.k8s, member.Name()); err != nil {
			c.log.Error(err, "Failed to clear pending ejection condition", "cluster", c.namespacedName(), "pod", member.Name())
		}
	}

	metrics.VolumeSizeUnderManagementBytesMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Set(float64(k8sutil.GetTotalPVCMemoryByApp(c.k8s.PersistentVolumeClaims.List(), constants.App)))

	return nil
}

// shouldRebalance checks that conditions are satisfied to rebalance the cluster.
func shouldRebalance(c *Cluster, r *ReconcileMachine) bool {
	if !r.needsRebalance {
		return false
	}

	return canRebalance(c, r)
}

// canRebalance checks if the cluster can be rebalanced. Pending members are ones that have been added to the cluster but not balanced in. We may want to avoid rebalancing the cluster if these pending members are still waiting for checks to pass.
func canRebalance(c *Cluster, r *ReconcileMachine) bool {
	// If the allowExternallyUnreachablePods flag is set, we will rebalance the cluster if all the pending members have had their DNS check delay elapsed.
	// If it is not set, we will only rebalance if there are no pending members that have not been activated in the cluster.
	if c.cluster.Spec.Networking.AllowExternallyUnreachablePods != nil && *c.cluster.Spec.Networking.AllowExternallyUnreachablePods {
		for _, member := range r.pendingDNSMembers {
			pod, found := c.k8s.Pods.Get(member.Name())
			if !found {
				continue
			}

			if !c.hasDNSCheckDelayElapsed(pod) {
				return false
			}
		}

		return true
	}

	return r.pendingDNSMembers.Diff(r.couchbase.ActiveNodes).Empty()
}

// handleEjectedMembers cleans up members that have been CBS-ejected but whose
// K8s pods are still running. The pod is only deleted after CBS ejection is
// confirmed (PodPendingUpgradeBeforeEjection condition cleared by handleRebalance).
func (r *ReconcileMachine) handleEjectedMembers(c *Cluster) error {
	// If we can't rebalance, we should not destroy eject members as they won't be ejected safely from the cluster yet.
	if !canRebalance(c, r) {
		return nil
	}

	for name := range r.ejectMembers {
		// Do not delete a pod that still carries PodPendingUpgradeBeforeEjection — the CBS
		// eject-rebalance (handleRebalance) has not yet run for it.
		pod, ok := c.k8s.Pods.Get(name)
		if ok && k8sutil.IsPodPendingUpgradeBeforeEjection(pod) {
			continue
		}

		if err := c.destroyMember(name, r.shouldRemoveVolumes(name)); err != nil {
			return fmt.Errorf("failed to remove dead members: %w", err)
		}
	}

	return nil
}

func (r *ReconcileMachine) handleNotifyFinished(c *Cluster) error {
	c.log.V(1).Info("Reconcile completed", "cluster", c.namespacedName())

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

// getBucketMigrationCandidates returns nodes that have in-progress backend or
// eviction-policy migrations, and whether any of those nodes actually require a
// swap-rebalance to converge to spec.
func (c *Cluster) getBucketMigrationCandidates() (candidates couchbaseutil.MemberSet, swapsNeeded bool, hasStorageBackendOverrides bool, hasEvictionOverrides bool, err error) {
	atleast76, err := couchbaseutil.VersionAfter(c.cluster.Status.CurrentVersion, "7.6.0")
	if err != nil {
		return nil, false, false, false, err
	}

	// Node-level migration fields are only reported by CB server >= 7.6.
	if !atleast76 {
		return nil, false, false, false, nil
	}

	bucketSpecs, err := c.gatherBuckets()
	if err != nil {
		return nil, false, false, false, err
	}

	bucketMap := make(map[string]couchbaseutil.Bucket, len(bucketSpecs))

	for _, bucketSpec := range bucketSpecs {
		bucketMap[bucketSpec.BucketName] = bucketSpec
	}

	clusterBuckets := couchbaseutil.BucketStatusList{}
	if err = couchbaseutil.ListBucketStatuses(&clusterBuckets).On(c.api, c.readyMembers()); err != nil {
		return nil, false, false, false, err
	}

	candidates = couchbaseutil.MemberSet{}
	managedBuckets := c.cluster.Spec.Buckets.Managed
	for _, bucket := range clusterBuckets {
		var spec couchbaseutil.Bucket
		if managedBuckets {
			spec = bucketMap[bucket.BucketName]
		}

		for _, node := range bucket.Nodes {
			storageBackendMigrating := node.StorageBackend != ""
			evictionPolicyMigrating := node.EvictionPolicy != ""
			if !storageBackendMigrating && !evictionPolicyMigrating {
				continue
			}

			candidates.Add(c.members[node.HostName.GetMemberName()])

			// Track whether any node still carries an old storage backend. Only
			// storage-backend migrations require the restricted bucket API, so the
			// BucketMigrating condition is gated on this (eviction-only migrations
			// must not set it).
			if storageBackendMigrating {
				hasStorageBackendOverrides = true
			}

			if evictionPolicyMigrating {
				hasEvictionOverrides = true
			}

			// If not managing buckets, any ongoing migration requires a swap.
			if !managedBuckets {
				swapsNeeded = true
				continue
			}

			// If managing buckets, check if the ongoing migration mismatches the spec and therefore requires a swap to align.
			backendMismatch := storageBackendMigrating && node.StorageBackend != string(spec.BucketStorageBackend)
			evictionMismatch := evictionPolicyMigrating && node.EvictionPolicy != spec.EvictionPolicy

			if backendMismatch || evictionMismatch {
				swapsNeeded = true
			}
		}
	}

	return candidates, swapsNeeded, hasStorageBackendOverrides, hasEvictionOverrides, nil
}

// handleBucketMigration detects and manages per-node storage backend and eviction policy overrides.
// When EnableBucketMigrationRoutines is true and overrides require convergence,
// this function schedules swap-rebalances to move vBuckets to the new format.
func (r *ReconcileMachine) handleBucketMigration(c *Cluster) error {
	// Something is broken, let that get fixed up first.
	if r.needsRebalance {
		return nil
	}

	// Let's finish upgrading before we try to migrate the buckets
	if upgrading, err := c.isUpgrading(); upgrading && err == nil {
		return nil
	} else if err != nil {
		return err
	}

	clusterInfo := couchbaseutil.TerseClusterInfo{}
	if err := couchbaseutil.GetTerseClusterInfo(&clusterInfo).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	atleast76, err := couchbaseutil.VersionAfter(c.cluster.Status.CurrentVersion, "7.6.0")
	if err != nil {
		return nil
	}

	// Node-level migration state is only reported by CB server >= 7.6.
	if !atleast76 {
		return nil
	}

	candidates, swapsNeeded, hasStorageBackendOverrides, hasEvictionOverrides, err := c.getBucketMigrationCandidates()
	if err != nil {
		return err
	}

	candidatesNoOrchestrator, orchestrator := separateCandidatesAndOrchestrator(candidates, clusterInfo.Orchestrator)

	if len(candidatesNoOrchestrator) == 0 && orchestrator == nil {
		r.c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionBucketMigration)
		r.c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionBucketEvictionMigration)
		return nil
	}

	// Only advertise the migration condition when there are storage-backend overrides
	// AND the operator is actually driving the migration. The operator is driving it
	// when the buckets are managed — reconcileBuckets pushes corrective updates via the
	// restricted migration-safe API even when swap routines are off — or when migration
	// routines are enabled, in which case the operator swap-rebalances even unmanaged buckets.
	operatorDrivingMigration := c.cluster.Spec.Buckets.Managed || c.cluster.Spec.Buckets.EnableBucketMigrationRoutines

	if hasStorageBackendOverrides && operatorDrivingMigration {
		// Persist before any swap pod is created so an external observer (and the
		// CouchbaseCluster DAC's upgrade-during-migration block) sees the condition.
		// Done above the EnableBucketMigrationRoutines guard because reconcileBuckets
		// also reads the condition to pick the migration-safe REST API.
		if !c.cluster.HasCondition(couchbasev2.ClusterConditionBucketMigration) {
			c.cluster.Status.SetBucketMigrationCondition()
			if err := c.updateCRStatus(); err != nil {
				return err
			}
		}
	} else {
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionBucketMigration)
	}

	if hasEvictionOverrides {
		r.c.cluster.Status.SetBucketEvictionMigrationCondition()
	} else {
		r.c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionBucketEvictionMigration)
	}

	// Swap-rebalances are skipped when either:
	// * Migration routines are disabled (operator not permitted to eject pods), or
	// * Every overridden node already carries the vBucket format spec desires
	//   (the bucket setting drifted then was reverted before any data movement —
	//   reconcileBuckets will push the corrective REST update this tick).
	if !c.cluster.Spec.Buckets.EnableBucketMigrationRoutines || !swapsNeeded {
		return nil
	}

	migrationCandidates := couchbaseutil.MemberSet{}

	explicitNumber := min(max(1, int(c.cluster.Spec.Buckets.MaxConcurrentPodSwaps)), len(candidatesNoOrchestrator))

	// Add candidates up to the explicitNumber or the orchestrator if no others are available.
	for _, candidateName := range candidatesNoOrchestrator.Names()[:explicitNumber] {
		migrationCandidates.Add(candidatesNoOrchestrator[candidateName])
	}

	if (migrationCandidates.Size() == 0) && orchestrator != nil {
		migrationCandidates.Add(orchestrator)
	}

	if migrationCandidates.Size() > 0 {
		return r.swapRebalanceMembers(c, migrationCandidates)
	}

	return nil
}

// nolint:gocognit
func (r *ReconcileMachine) swapRebalanceMembers(c *Cluster, members couchbaseutil.MemberSet) error {
	candidatesSlice := make([]couchbaseutil.Member, 0, len(members))
	toCreate := make([]couchbasev2.ServerConfig, 0, len(members))

	for _, candidate := range members {
		c.log.Info("Swap-Rebalancing pod ", "cluster", c.namespacedName(), "name", candidate.Name(), "source-version", candidate.Version())

		// Remove the candidate from the scheduler.
		if err := c.scheduler.Upgrade(candidate.Config(), candidate.Name()); err != nil {
			metrics.SwapRebalanceFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
			metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

			return err
		}

		// Grab the server class.
		class := c.cluster.Spec.GetServerConfigByName(candidate.Config())
		if class == nil {
			metrics.SwapRebalanceFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
			metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

			return fmt.Errorf("swap rebalance unable to determine server class %s for member %s: %w", candidate.Name(), candidate.Config(), errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		// Member already has correct image from getUpgradeCandidates()
		// Create a copy and use candidate's image (spec class has latest image)
		configToUse := *class
		configToUse.Image = candidate.GetImage()
		toCreate = append(toCreate, configToUse)
		candidatesSlice = append(candidatesSlice, candidate)
	}

	// Add the new members.
	memberResults, err := c.addMembers(toCreate...)
	if err != nil {
		metrics.SwapRebalanceFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

		return fmt.Errorf("swap rebalance failed to add new nodes to cluster: %w", err)
	}

	numErrors := 0

	for _, result := range memberResults {
		if result.Err != nil {
			numErrors++
		}
	}

	errs := make([]error, 0, numErrors)

	for index, result := range memberResults {
		switch {
		case result.Err != nil:
			errs = append(errs, fmt.Errorf("swap rebalance failed to add new node to cluster: %w", result.Err))
			c.log.Error(result.Err, "Pod addition to cluster failed", "cluster", c.namespacedName(), "pod", result.Member.Name())

			metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		case c.cluster.IsMigrationCluster():
			// Migration: addMembersToTarget blocked until CBS-add. Bookkeeping
			// and metrics happen at the correct point (same as original sync).
			r.addMember(result.Member)
			r.removeMemberUser(candidatesSlice[index])
			r.stabilizingMembers.Add(result.Member)

			metrics.SwapRebalancesTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
			metrics.PodReplacementsMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		default:
			// New pods are pending init. Defer metrics to handleReadyPendingPod.
			setupOK := true

			if err := k8sutil.SetPodUpgradeTracking(c.k8s, result.Member.Name(), string(couchbasev2.SwapRebalance)); err != nil {
				c.log.Error(err, "Failed to mark replacement pod with upgrade tracking",
					"cluster", c.namespacedName(), "pod", result.Member.Name())
				setupOK = false
			}

			// Only mark the old member for ejection if the annotation succeeded.
			// Setting the condition without the annotation is dangerous: the old
			// pod gets ejected but stabilizingMembers stays empty, skipping the
			// stabilization period.
			if setupOK {
				if err := k8sutil.SetPodPendingUpgradeBeforeEjection(c.k8s, candidatesSlice[index].Name()); err != nil {
					c.log.Error(err, "Failed to mark old pod for ejection",
						"cluster", c.namespacedName(), "pod", candidatesSlice[index].Name())
					setupOK = false
				}
			}

			if !setupOK {
				c.log.Info("Deleting replacement pod due to failed swap-rebalance setup",
					"cluster", c.namespacedName(), "pod", result.Member.Name())
				if delErr := k8sutil.DeletePod(c.k8s, c.cluster.Namespace, result.Member.Name(), c.config.GetDeleteOptions()); delErr != nil {
					c.log.Error(delErr, "Failed to delete replacement pod during cleanup",
						"cluster", c.namespacedName(), "pod", result.Member.Name())
				}
				errs = append(errs, fmt.Errorf("%w: candidate %s, replacement pod cleaned up",
					ErrSwapRebalanceSetupFailed, candidatesSlice[index].Name()))
			}
		}
	}

	if len(errs) == 0 {
		// For async (non-migration) clusters, yield the FSM. The replacement
		// pods need CBS-add via handleReadyPendingPod, then rebalance ejects
		// the old members (via PodPendingUpgradeBeforeEjectionCondition) and finalizes the swap.
		if !c.cluster.IsMigrationCluster() {
			r.abort("swap rebalance pods created, waiting for async initialization")
		}

		return nil
	}

	metrics.SwapRebalanceFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

	return errors.Join(errs...)
}
