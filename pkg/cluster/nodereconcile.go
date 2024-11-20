package cluster

import (
	goerrors "errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrReconcileInhibited = fmt.Errorf("reconcile was blocked from running")
var ErrOrchestratorNotUpgraded = fmt.Errorf("orchestrator not upgraded yet")
var ErrFailoverStartCounterNotIncremented = fmt.Errorf("failover start counter not incremented")
var ErrFailoverSuccessCounterNotIncremented = fmt.Errorf("failover success counter not incremented")
var ErrUnexpectedCounterChange = fmt.Errorf("unexpected counter change")
var ErrNodeNotInCluster = fmt.Errorf("node not in the cluster: ")
var ErrNodeNotActive = fmt.Errorf("node not active: ")

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

	// rebalanceRetries is the number of times to retry rebalance operations when handling rebalances.
	rebalanceRetries uint
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

		rebalanceRetries: 1,
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
		(*ReconcileMachine).handleMoveNodes,
		(*ReconcileMachine).handleUpgradeNode,
		(*ReconcileMachine).handleBucketStorageBackendMigration,
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
			metrics.PodRecoveryFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Inc()

			return fmt.Errorf("pod recovery failed for member %s: %w", name, err)
		}

		log.Info("Pod recovering", "cluster", c.namespacedName(), "name", name)

		c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))
		delete(c.recoveryTime, name)

		metrics.PodRecoveriesMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, name})...).Inc()

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
				r.abort("unable to recover pod pending addition")

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
		if terminating, err := c.isPodTerminating(m); err != nil {
			return err
		} else if terminating {
			log.Info("Add back node is terminating", "cluster", c.namespacedName(), "name", name)
			r.abort("add back node is terminating")
			return nil
		}

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

				metrics.PodRecoveryFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Inc()

				r.abort("unable to recover pod")

				return nil
			}

			c.raiseEventCached(k8sutil.MemberRecoveredEvent(name, c.cluster))

			metrics.PodRecoveriesMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, m.Name()})...).Inc()

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

type allMetrics struct {
	idx, data, view couchbaseutil.StatsRangeMetrics
}

// getMetrics retrives metrics for data, index and views services.
func getMetrics(c *Cluster) (allMetrics, error) {
	idxMetrics := couchbaseutil.StatsRangeMetrics{}
	if err := couchbaseutil.GetStatsRangeAvgMetrics("index", &idxMetrics).On(c.api, c.readyMembers()); err != nil {
		return allMetrics{}, fmt.Errorf("%w: error getting index metrics", err)
	}

	dataMetrics := couchbaseutil.StatsRangeMetrics{}
	if err := couchbaseutil.GetStatsRangeAvgMetrics("data", &dataMetrics).On(c.api, c.readyMembers()); err != nil {
		return allMetrics{}, fmt.Errorf("%w: error getting data metrics", err)
	}

	viewsMetrics := couchbaseutil.StatsRangeMetrics{}
	if err := couchbaseutil.GetStatsRangeAvgMetrics("views", &viewsMetrics).On(c.api, c.readyMembers()); err != nil {
		return allMetrics{}, fmt.Errorf("%w: error getting views metrics", err)
	}

	return allMetrics{idx: idxMetrics, data: dataMetrics, view: viewsMetrics}, nil
}

func parseSizePerMember(memberName string, allmetrices allMetrics) (float64, error) {
	sizeByService := func(sm couchbaseutil.StatsRangeMetrics) (float64, error) {
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
						return 0, fmt.Errorf("%w: error converting %s to float64 format", err, s)
					}

					size += i
				}
			}
		}

		return size, nil
	}

	idxSize, err := sizeByService(allmetrices.idx)
	if err != nil {
		return 0, err
	}

	dataSize, err := sizeByService(allmetrices.data)
	if err != nil {
		return 0, err
	}

	viewSize, err := sizeByService(allmetrices.view)
	if err != nil {
		return 0, err
	}

	return idxSize + dataSize + viewSize, nil
}

// populateRemovalQueuePerServerClass enqueues pod(server) names which are version 7.0+.
func populateRemovalQueuePerServerClass(serverClass string, clusteredMembers couchbaseutil.MemberSet, c *Cluster) error {
	serverConf := c.cluster.Spec.GetServerConfigByName(serverClass)
	if serverConf == nil {
		return fmt.Errorf("server class not found %s: %w", serverClass, errors.NewStackTracedError(errors.ErrServerClassNotFound))
	}

	targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.ServerClassCouchbaseImage(serverConf))
	if err != nil {
		return err
	}

	var queueMembers []string

	// appending the member names which are NOT already on target version.
	membersNamesNotOnTargetVersion := clusteredMembers.GroupBy(
		func(m couchbaseutil.Member) bool {
			return m.Version() != targetVersion && m.Config() == serverClass
		}).Names()

	// added as the first members to get culled.
	queueMembers = append(queueMembers, membersNamesNotOnTargetVersion...)

	membersOnTargetVersion := clusteredMembers.GroupBy(
		func(m couchbaseutil.Member) bool {
			return m.Version() == targetVersion
		})

	// map used to avoid any chance of duplicate member names.
	memberToSize := map[string]float64{}
	memNames := make([]string, 0, len(memberToSize))

	allm, err := getMetrics(c)
	if err != nil {
		return err
	}

	for name := range membersOnTargetVersion {
		size, err := parseSizePerMember(name, allm)
		if err != nil {
			return fmt.Errorf("%w: error calculating member disk size", err)
		}

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
		whereEqualsServerConfig := func(m couchbaseutil.Member) bool {
			return m.Config() == serverSpec.Name
		}
		existingNodes := r.clusteredMembers.GroupBy(whereEqualsServerConfig).Size()

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
		} else if pvcState == nil {
			continue
		}

		for _, pvc := range pvcState.List() {
			switch {
			// When online resize failed for any of the member volumes then proceed with
			// normal rolling upgrade since the Pod must be recreated.
			case pvcState.IsResizeFailed(pvc.Name):
				err := pvcState.GetReasonForResizeFailed(pvc.Name)
				log.Info("Unable to expand volume in place, falling back to rolling upgrade", "cluster", c.namespacedName(), "volume", pvc.Name, "error", err)
				c.raiseEvent(k8sutil.ExpandVolumeFallbackEvent(pvc.Name, c.cluster, err.Error()))

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

				metrics.VolumeExpansionMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, pvc.Name})...).Inc()

				// Done for now. Not going to upgrade all volumes at once.
				r.abort("persistent volumes expanding")

				return nil
			}
			// volume expansion state is not set
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
func (c *Cluster) selectUpgradeCandidates(candidates couchbaseutil.MemberSet, orchestrator string) (couchbaseutil.MemberSet, error) {
	// Rolling upgrade defaults to a single node at a time, however this can
	// be increased to an absolute number or a relative size of the cluster.
	if c.cluster.GetUpgradeStrategy() == couchbasev2.RollingUpgrade {
		// Default to one at a time
		upgradeLimit := 1

		// Remove orchestrator from list if rolling upgrade
		if len(candidates) != 1 {
			candidates, _ = separateCandidatesAndOrchestrator(candidates, orchestrator)
		}

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
			log.Info(candidates[name].Name())
		}

		candidates = constrained
	}

	return candidates, nil
}

func separateCandidatesAndOrchestrator(candidates couchbaseutil.MemberSet, orchestratorName string) (couchbaseutil.MemberSet, couchbaseutil.Member) {
	var candidatesNoOrchestrator = couchbaseutil.MemberSet{}

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

		candidatesNoOrchestrator.Add(candidate)
	}

	return candidatesNoOrchestrator, orchestrator
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
					fmt.Println("Retrying graceful failover")
					fmt.Println("failedReqErr.StatusCode: ", failedReqErr.StatusCode)
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
	log.Info("Unable to perform graceful failover on node. Reverting to hard failover.", "cluster", c.namespacedName(), "name", candidate.Name())

	otpNodeList := couchbaseutil.OTPNodeList{candidate.GetOTPNode()}

	if err := couchbaseutil.Failover(otpNodeList, false).On(c.api, candidate); err != nil {
		return false, err
	}

	return true, nil
}

func (r *ReconcileMachine) checkOrchestratorOnLatestVersion(c *Cluster, targetVersion string) error {
	callback := func() error {
		clusterInfo := &couchbaseutil.TerseClusterInfo{}
		if err := couchbaseutil.GetTerseClusterInfo(clusterInfo).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		if couchbaseutil.MemberOnVersion(c.members, clusterInfo.Orchestrator, targetVersion) {
			return nil
		}

		return ErrOrchestratorNotUpgraded
	}

	return retryutil.RetryFor(1*time.Minute, callback)
}

func (r *ReconcileMachine) recreateAndRebalanceNode(c *Cluster, candidate couchbaseutil.Member, targetVersion string, canDeltaRecover bool) error {
	if len(c.members) > 1 {
		if canDeltaRecover {
			if err := couchbaseutil.SetRecoveryType(candidate.GetOTPNode(), couchbaseutil.RecoveryTypeDelta).On(c.api, c.readyMembers()); err != nil {
				return err
			}
		} else {
			log.Info("Unable to set delta recovery type. Reverting to full recovery.")

			if err := couchbaseutil.SetRecoveryType(candidate.GetOTPNode(), couchbaseutil.RecoveryTypeFull).On(c.api, c.readyMembers()); err != nil {
				return err
			}
		}
	}

	if err := c.recreatePod(candidate); err != nil {
		return err
	}

	if err := c.waitForPodAdded(c.ctx, candidate); err != nil {
		return err
	}

	// Rebalance failed. Time to set recovery type as full.
	if err := c.rebalanceWithRetriesOnVerifyFails(c.members, nil, 2); err != nil {
		log.Info(fmt.Sprintf("Rebalance failed, reverting to full recovery: %s", err.Error()))

		if err := couchbaseutil.SetRecoveryType(candidate.GetOTPNode(), couchbaseutil.RecoveryTypeFull).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		if err := r.checkOrchestratorOnLatestVersion(c, targetVersion); err != nil {
			if !goerrors.Is(err, ErrOrchestratorNotUpgraded) {
				return err
			}
		}

		return c.rebalance(c.members, nil)
	}

	return nil
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

	for _, candidate := range candidates {
		if err := c.scheduler.Upgrade(candidate.Config(), candidate.Name()); err != nil {
			metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

			return err
		}

		serverClass := c.cluster.Spec.GetServerConfigByName(candidate.Config())
		if serverClass == nil {
			continue
		}

		// Update candidate version
		candidate.SetVersion(targetVersion)

		if c.k8s.PersistentVolumeClaims != nil {
			// Update volumes
			pvcState, err := k8sutil.GetPodVolumes(c.k8s, candidate, c.cluster, *serverClass)
			if err != nil {
				metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

				return err
			} else if pvcState != nil {
				for _, volume := range pvcState.List() {
					volume.Annotations[constants.PVCImageAnnotation] = c.cluster.Spec.Image
					volume.Annotations[constants.CouchbaseVersionAnnotationKey] = targetVersion
					_, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(c.ctx, volume, v1.UpdateOptions{})

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

		if err := r.recreateAndRebalanceNode(c, candidate, targetVersion, canInPlaceUpgrade); err != nil {
			metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
			metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

			return err
		}

		metrics.InPlaceUpgradeTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		metrics.PodReplacementsMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
	}

	return nil
}

func (r *ReconcileMachine) handleMoveNodes(c *Cluster) error {
	// Don't do anything if the cluster is currently upgrading
	if upgrading, err := c.isUpgrading(); upgrading && err == nil {
		return nil
	} else if err != nil {
		return err
	}

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
	pvcPresent := true

	var targetVersion string

	for _, candidate := range candidates {
		// The target version is going to stay the same as the current version
		targetVersion = candidate.Version()

		if c.isPodRecoverable(candidate) == false {
			pvcPresent = false
			break
		}
	}

	if c.cluster.Spec.UpgradeProcess != nil && *c.cluster.Spec.UpgradeProcess == couchbasev2.DeltaRecovery {
		inPlaceUpgrade := couchbasev2.InPlaceUpgrade
		c.cluster.Spec.UpgradeProcess = &inPlaceUpgrade
	}

	// Carry out the move
	// We can use the upgrade methods to do this (even though we're not changing the version)
	if c.cluster.Spec.UpgradeProcess != nil && *c.cluster.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade && pvcPresent {
		err := r.handleInPlaceUpgrade(c, candidates, targetVersion)
		if err != nil {
			return err
		}
	} else {
		if c.cluster.Spec.UpgradeProcess != nil && *c.cluster.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade && !pvcPresent {
			log.Info("No persistent volumes in cluster. Reverting to SwapRebalance.", "cluster", c.namespacedName())
		}

		return r.swapRebalanceMembers(c, candidates)
	}

	return nil
}

func (r *ReconcileMachine) handleUpgradeNode(c *Cluster) error {
	// Something is broken, let that get fixed up first.
	if r.needsRebalance || len(r.couchbase.PendingAddNodes) > 0 {
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

	pvcPresent := true

	for _, candidate := range candidates {
		if !c.isPodRecoverable(candidate) {
			pvcPresent = false
			break
		}
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

	candidates = constrained

	// Calculate the number of nodes already in the target state before we
	// potentially mutate the candidates.
	upgraded := len(c.members) - len(candidates)

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

	if c.cluster.Spec.UpgradeProcess != nil && *c.cluster.Spec.UpgradeProcess == couchbasev2.DeltaRecovery {
		inPlaceUpgrade := couchbasev2.InPlaceUpgrade
		c.cluster.Spec.UpgradeProcess = &inPlaceUpgrade
	}

	groupedCandidates := candidates.GroupByServerConfigs()
	for serverConfigName, serverCandidates := range groupedCandidates {
		serverConf := c.cluster.Spec.GetServerConfigByName(serverConfigName)

		targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.ServerClassCouchbaseImage(serverConf))
		if err != nil {
			return err
		}

		if c.cluster.Spec.UpgradeProcess != nil && *c.cluster.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade && pvcPresent {
			log.Info("Upgrading pods with InPlaceUpgrade", "cluster", c.namespacedName(), "names", serverCandidates.Names(), "target-version", targetVersion)

			err = r.handleInPlaceUpgrade(c, serverCandidates, targetVersion)
		} else {
			if c.cluster.Spec.UpgradeProcess != nil && *c.cluster.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade && !pvcPresent {
				log.Info("No persistent volumes in cluster. Reverting to SwapRebalance.", "cluster", c.namespacedName())
			}

			log.Info("Upgrading pods with SwapRebalance", "cluster", c.namespacedName(), "names", serverCandidates.Names(), "target-version", targetVersion)

			err = r.swapRebalanceMembers(c, serverCandidates)
		}

		if err != nil {
			return err
		}
	}

	return nil
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

//nolint:gocognit
func (r *ReconcileMachine) handleRebalance(c *Cluster) error {
	if r.needsRebalance {
		if len(r.couchbase.ServerRebalanceReasons) > 0 {
			log.Info("Rebalancing Cluster", "cluster", r.c.namespacedName(), "rebalance_reasons", r.couchbase.ServerRebalanceReasons)
		}

		// Eject nodes that we want to discard.
		eject := r.ejectMembers.OTPNodes()

		pods, err := c.GetK8sClient().KubeClient.CoreV1().Pods(c.cluster.Namespace).List(c.ctx, v1.ListOptions{LabelSelector: constants.LabelNode})
		if err != nil {
			return err
		}

		upgrading, err := c.isUpgrading()
		if err != nil {
			return err
		}

		// We don't want to do this while upgrading as this will be done as part of the upgrade process.
		if !upgrading && !c.isMigrating() {
			clusterInfo := couchbaseutil.ClusterInfo{}
			if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			for _, nodeInfo := range clusterInfo.Nodes {
				// We only need to failover if the node is active and is in the eject list
				if nodeInfo.Membership != "active" || !slices.Contains(eject, nodeInfo.OTPNode) {
					continue
				}

				failoverList := couchbaseutil.OTPNodeList{}
				failoverList = append(failoverList, nodeInfo.OTPNode)

				for _, pod := range pods.Items {
					if strings.EqualFold(pod.Name, nodeInfo.HostName.GetMemberName()) {
						continue
					}

					for _, member := range c.members {
						if member.Name() == nodeInfo.HostName.GetMemberName() {
							if err := r.gracefullyFailoverNode(member, c); err != nil {
								if err := couchbaseutil.Failover(failoverList, false).On(c.api, c.readyMembers()); err != nil {
									return err
								}
							}

							break
						}
					}

					break
				}
			}
		}

		if err := c.rebalanceWithRetriesOnVerifyFails(r.clusteredMembers, eject, r.rebalanceRetries); err != nil {
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

	metrics.VolumeSizeUnderManagementBytesMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Set(float64(k8sutil.GetTotalPVCMemoryByApp(c.k8s.PersistentVolumeClaims.List(), constants.App)))

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

func (r *ReconcileMachine) handleBucketStorageBackendMigration(c *Cluster) error {
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

	// We can only migrate the nodes if CB server is >= 7.6 and bucket migration routines are enabled
	if !atleast76 || !c.cluster.Spec.Buckets.EnableBucketMigrationRoutines {
		return nil
	}

	// Get all buckets
	clusterBuckets := couchbaseutil.BucketStatusList{}
	if err = couchbaseutil.ListBucketStatuses(&clusterBuckets).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	var candidates = couchbaseutil.MemberSet{}

	for _, bucket := range clusterBuckets {
		for _, node := range bucket.Nodes {
			// node.StorageBackend is empty if it matches the bucket storageMode
			if node.StorageBackend != "" {
				candidates.Add(c.members[node.HostName.GetMemberName()])
			}
		}
	}

	candidatesNoOrchestrator, orchestrator := separateCandidatesAndOrchestrator(candidates, clusterInfo.Orchestrator)

	if len(candidatesNoOrchestrator) == 0 && orchestrator == nil {
		return nil
	}

	migrationCandidates := couchbaseutil.MemberSet{}

	explicitNumber := min(max(1, int(c.cluster.Spec.Buckets.MaxMigratableBuckets)), len(candidatesNoOrchestrator))

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
		log.Info("Swap-Rebalancing pod ", "cluster", c.namespacedName(), "name", candidate.Name(), "source-version", candidate.Version())

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

		toCreate = append(toCreate, *class)
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
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("swap rebalance failed to add new node to cluster: %w", result.Err))
			log.Error(result.Err, "Pod addition to cluster failed", "cluster", c.namespacedName(), "pod", result.Member.Name())

			metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		} else { // Update book keeping
			r.addMember(result.Member)
			r.removeMemberUser(candidatesSlice[index])

			metrics.SwapRebalancesTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
			metrics.PodReplacementsMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		}
	}

	if len(errs) == 0 {
		return nil
	}

	metrics.SwapRebalanceFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()

	return errors.Join(errs...)
}
