package couchbaseutil

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("couchbaseutil")

type NodeState int

// Node states are powers of two, so can be logically ORed together e.g.
// we can check for nodes being in one of multiple states
const (
	NodeStateActive NodeState = iota
	NodeStatePendingAdd
	NodeStateFailedAdd
	NodeStateWarmup
	NodeStateDown
	NodeStateFailed
	NodeStateAddBack
	NodeStateUnclustered
	NodeStateInvalid
)

// String converts a node state into a string representation.
func (s NodeState) String() string {
	stateStrings := map[NodeState]string{
		NodeStateWarmup:      "warmup",
		NodeStateActive:      "active",
		NodeStatePendingAdd:  "pending_add",
		NodeStateAddBack:     "add_back",
		NodeStateDown:        "down",
		NodeStateFailedAdd:   "failed_add",
		NodeStateFailed:      "failed",
		NodeStateUnclustered: "unclustered",
	}

	if str, ok := stateStrings[s]; ok {
		return str
	}

	return "invalid"
}

// NodeStateMap maps a managed node (pod) name to its current state
type NodeStateMap map[string]NodeState

type ClusterStatus struct {
	// Cached /pools/default output.  From this we can derive the state of
	// foreign nodes added by an external party to provide better dubugging.
	info *ClusterInfo
	// NodeStateMap maps a known member to it's current state.  This is an
	// optimisation to avoid scanning the main sets below it determine the
	// state of a member.
	NodeStateMap NodeStateMap
	// managedNodes is a set of all known nodes.  This is used to iterate
	// over the set of all nodes.  This is analogous to the cluster members
	// set.
	managedNodes MemberSet

	ActiveNodes      MemberSet    // status=healthy,   clusterMembership=active
	PendingAddNodes  MemberSet    // status=healthy,   clusterMembership=inactiveAdded
	AddBackNodes     MemberSet    // status=healthy,   clusterMembership=inactiveFailed
	FailedAddNodes   MemberSet    // status=unhealthy, clusterMembership=inactiveAdded
	WarmupNodes      MemberSet    // status=warmup,    clusterMembership=inactiveAdded
	DownNodes        MemberSet    // status=unhealthy, clusterMembership=active
	FailedNodes      MemberSet    // status=unhealthy, clusterMembership=inactiveFailed
	UnclusteredNodes MemberSet    // Managed by Kubernetes, but not part of the cluster
	UnmanagedNodes   HostNameList // Not managed by Kubernetes
	IsRebalancing    bool
	NeedsRebalance   bool
}

// NewClusterStatus returns a cluster status object with all
// lists and maps initialized.
func NewClusterStatus() *ClusterStatus {
	status := &ClusterStatus{
		info: &ClusterInfo{},
	}

	status.Reset()

	return status
}

// Reset replaces all sets in the cluster status with empty versions
// and returns scalars to the zero state.
func (cs *ClusterStatus) Reset() {
	cs.NodeStateMap = NodeStateMap{}
	cs.managedNodes = NewMemberSet()
	cs.ActiveNodes = NewMemberSet()
	cs.PendingAddNodes = NewMemberSet()
	cs.AddBackNodes = NewMemberSet()
	cs.FailedAddNodes = NewMemberSet()
	cs.WarmupNodes = NewMemberSet()
	cs.DownNodes = NewMemberSet()
	cs.FailedNodes = NewMemberSet()
	cs.UnclusteredNodes = NewMemberSet()
	cs.UnmanagedNodes = HostNameList{}
	cs.IsRebalancing = false
	cs.NeedsRebalance = false
}

// nodeStatus is an intermediate data structured used to log node status information.
type nodeStatus struct {
	name    string
	version string
	class   string
	managed bool
	state   string
}

// GetNode looks up node based on Couchbase hostname.
func (cs *ClusterStatus) getNode(hostname HostName) (*NodeInfo, error) {
	for _, node := range cs.info.Nodes {
		if node.HostName == hostname {
			return &node, nil
		}
	}

	return nil, fmt.Errorf("node %s does not exist in cluster", hostname)
}

// KnownNodes returns all nodes that the cluster is tracking.
func (cs *ClusterStatus) KnownNodes() []string {
	knownNodes := []string{}

	for _, node := range cs.info.Nodes {
		knownNodes = append(knownNodes, node.HostName.GetMemberName())
	}

	return knownNodes
}

// getNodeState looks up node status based on Couchbase hostname.
func getNodeState(node *NodeInfo) (state NodeState, err error) {
	// Set default return values
	state = NodeStateInvalid

	// Select and return the correct status type
	switch node.Status {
	case "warmup":
		state = NodeStateWarmup
	case "healthy":
		switch node.Membership {
		case "active":
			state = NodeStateActive
		case "inactiveAdded":
			state = NodeStatePendingAdd
		case "inactiveFailed":
			state = NodeStateAddBack
		default:
			err = fmt.Errorf("cluster status: status=%s membership=%s", node.Status, node.Membership)
		}
	case "unhealthy":
		switch node.Membership {
		case "active":
			state = NodeStateDown
		case "inactiveAdded":
			state = NodeStateFailedAdd
		case "inactiveFailed":
			state = NodeStateFailed
		default:
			err = fmt.Errorf("cluster status: status=%s membership=%s", node.Status, node.Membership)
		}
	default:
		err = fmt.Errorf("cluster status: status=%s membership=%s", node.Status, node.Membership)
	}

	return
}

// addMemberToStateSet adds the member to the correct set based on state.
func (cs *ClusterStatus) addMemberToStateSet(state NodeState, member *Member) error {
	switch state {
	case NodeStateActive:
		cs.ActiveNodes.Add(member)
	case NodeStatePendingAdd:
		cs.PendingAddNodes.Add(member)
	case NodeStateFailedAdd:
		cs.FailedAddNodes.Add(member)
	case NodeStateWarmup:
		cs.WarmupNodes.Add(member)
	case NodeStateDown:
		cs.DownNodes.Add(member)
	case NodeStateFailed:
		cs.FailedNodes.Add(member)
	case NodeStateAddBack:
		cs.AddBackNodes.Add(member)
	case NodeStateUnclustered:
		cs.UnclusteredNodes.Add(member)
	default:
		return fmt.Errorf("unhandled node state %v", state)
	}

	return nil
}

// logClusterStatus logs the overal cluster status e.g. balanced condition.
func (cs *ClusterStatus) logClusterStatus(cluster string) {
	// A cluster is either balanced or not
	balance := "balanced"
	if cs.NeedsRebalance {
		balance = "unbalanced"
	}

	log.Info("Cluster status", "cluster", cluster, "balance", balance, "rebalancing", cs.IsRebalancing)
}

// logClusterNodeStatus logs the individual node statuses.
func (cs *ClusterStatus) logClusterNodeStatus(cluster string) {
	// Sort the names so it's easier to grok
	names := []string{}

	for name := range cs.managedNodes {
		names = append(names, name)
	}

	sort.Strings(names)

	// Collect all the node statuses, as we process check the string lengths for
	// pretty tabulation
	statuses := []nodeStatus{}
	maxName := 0
	maxClass := 0

	for _, name := range names {
		// All members are managed
		// And they will exist in one state
		state := cs.NodeStateMap[name]

		// Buffer up the status entry
		class := cs.managedNodes[name].ServerConfig
		version := cs.managedNodes[name].Version

		status := nodeStatus{
			name:    name,
			version: version,
			class:   class,
			managed: true,
			state:   state.String(),
		}
		statuses = append(statuses, status)

		// Update the book keeping
		if len(name) > maxName {
			maxName = len(name)
		}

		if len(class) > maxClass {
			maxClass = len(class)
		}
	}

	for _, name := range cs.UnmanagedNodes {
		// Ignore the error, if we've added the unmanaged node it has to exist in the node info.
		node, _ := cs.getNode(name)

		// Report the current state the unmanaged node is in, again ignore the error here as
		// it will return invalid if an error occurs.
		state, _ := getNodeState(node)

		// Buffer up the status entry
		status := nodeStatus{
			name:    string(name),
			version: "unknown",
			class:   "unknown",
			managed: false,
			state:   state.String(),
		}

		statuses = append(statuses, status)

		// Update the book keeping
		if len(name) > maxName {
			maxName = len(name)
		}
	}

	for _, status := range statuses {
		log.Info("Node status", "cluster", cluster, "name", status.name, "version", status.version, "class", status.class, "managed", status.managed, "status", status.state)
	}
}

// Logs the cluster status.
func (cs *ClusterStatus) LogStatus(cluster string) {
	cs.logClusterStatus(cluster)
	cs.logClusterNodeStatus(cluster)
}

// Are all managed nodes healthy? e.g. in the active state.
func (cs *ClusterStatus) AllManagedNodesHealthy() bool {
	for _, member := range cs.managedNodes {
		if cs.NodeStateMap[member.Name] != NodeStateActive {
			return false
		}
	}

	return true
}

// Do any nodes exist that we aren't managing.
func (cs *ClusterStatus) AnyUnmanagedNodes() bool {
	return len(cs.UnmanagedNodes) != 0
}

// Is the cluster as a whole healthy.
func (cs *ClusterStatus) ClusterHealthy() bool {
	return cs.AllManagedNodesHealthy() &&
		!cs.AnyUnmanagedNodes() &&
		!cs.IsRebalancing &&
		!cs.NeedsRebalance
}

// Is the named node in any of the states set in the bitmap.
func (cs *ClusterStatus) NodeInState(name string, states ...NodeState) bool {
	// Doesn't exist, so not in that state
	s, ok := cs.NodeStateMap[name]
	if !ok {
		return false
	}

	// Iterate through the requested states and return true if any match
	for _, state := range states {
		if s == state {
			return true
		}
	}

	return false
}

// Does the named node exist anywhere?
func (cs *ClusterStatus) ContainsNode(name string) bool {
	return cs.managedNodes.Contains(name)
}

// RebalanceSucceeded looks at the state and determines whether what we inteded to happen
// actually happened in light of Server not behaving sensibly.
func (cs *ClusterStatus) RebalanceSucceeded(members MemberSet) (bool, error) {
	// For all managed nodes, we expect them to either be active (after being balanced in)
	// or unclustered after being ejected.
	for name := range cs.managedNodes {
		if state, ok := cs.NodeStateMap[name]; !ok || state != NodeStateActive {
			return false, fmt.Errorf("node %s not found or not active", name)
		}
	}

	return true, nil
}

// Filter named nodes from a cluster status map.
func (nsm NodeStateMap) Exclude(excludes ...string) NodeStateMap {
	states := NodeStateMap{}

	for name, state := range nsm {
		found := false

		for _, exclude := range excludes {
			if name == exclude {
				found = true
				break
			}
		}

		if !found {
			states[name] = state
		}
	}

	return states
}

// Are all cluster statuses active?
func (nsm NodeStateMap) AllActive() bool {
	for _, state := range nsm {
		if state != NodeStateActive {
			return false
		}
	}

	return true
}

// CouchbaseClient encapsulates Couchbase API operations.
type CouchbaseClient struct {
	// ctx is used to asynchronously cancel blocking or long operations.
	ctx context.Context

	// api provides API calls
	api *Client
}

// NewCouchbaseClient allocates and initializes a new couchbase API client.
// Connections are persistent so we only expect to create a single client
// per cluster.
func NewCouchbaseClient(ctx context.Context, api *Client) *CouchbaseClient {
	return &CouchbaseClient{
		ctx: ctx,
		api: api,
	}
}

func (c *CouchbaseClient) GetClusterStatus(ms MemberSet) (*ClusterStatus, error) {
	status := &ClusterStatus{}
	if err := c.UpdateClusterStatus(ms, status); err != nil {
		return nil, err
	}

	return status, nil
}

func (c *CouchbaseClient) UpdateClusterStatus(ms MemberSet, status *ClusterStatus) error {
	status.Reset()

	cluster := &ClusterInfo{}
	if err := GetPoolsDefault(cluster).On(c.api, ms); err != nil {
		return err
	}

	status.info = cluster

	// Cache the managed nodes in the status
	status.managedNodes.Append(ms)

	// Collect the node known to Couchbase server as we iterate through the cluster map
	knownNodes := NewMemberSet()

	// Iterate over all of the nodes known to Couchbase server
	for i := range status.info.Nodes {
		node := status.info.Nodes[i]

		// The node name should be in the form cb-pod.cb-cluster.namespace.svc:8091.
		// By extracting the first field we can derive the pod name.  If it is not
		// know to the operator we flag it as unmanaged.
		member := ms[node.HostName.GetMemberName()]
		if member == nil {
			status.UnmanagedNodes = append(status.UnmanagedNodes, node.HostName)
			continue
		}

		// Add to the list of nodes known to Couchbase
		knownNodes.Add(member)

		// Attempt to get the internal state of the node from the cluster info
		state, err := getNodeState(&node)
		if err != nil {
			return err
		}

		// Map the member to its state
		status.NodeStateMap[member.Name] = state

		// Add the member to the relevant state based set
		if err := status.addMemberToStateSet(state, member); err != nil {
			return err
		}
	}

	// Any managed nodes not known to the cluster are unclustered (have been ejected).
	status.UnclusteredNodes = ms.Diff(knownNodes)
	for _, member := range status.UnclusteredNodes {
		status.NodeStateMap[member.Name] = NodeStateUnclustered
	}

	status.IsRebalancing = (status.info.RebalanceStatus == string(RebalanceStatusRunning))
	status.NeedsRebalance = !status.info.Balanced && len(status.info.Nodes) > 1

	return nil
}

// RebalanceProgressEntry is the type communicated to clients periodically
// over a channel.
type RebalanceStatusEntry struct {
	// Status is the status of a rebalance.
	Status RebalanceStatus
	// Progress is how far the rebalance has progressed, only valid when
	// the status is RebalanceStatusRunning.
	Progress float64
}

// RebalanceProgress is a type used to monitor rebalance status.
type RebalanceProgress interface {
	// Status is a channel that is periodically updated with the rebalance
	// status and progress.  This channel is closed once the rebalance task
	// is no longer running or an error was detected.
	Status() <-chan *RebalanceStatusEntry
	// Error is used to check the error status when the Status channel has
	// been closed.
	Error() error
	// Cancel allows the client to stop the go routine before a rebalance has
	// completed.
	Cancel()
}

// rebalanceProgressImpl implements the RebalanceProgress interface.
type rebalanceProgressImpl struct {
	// statusChan is the main channel for communicating status to the client.
	statusChan chan *RebalanceStatusEntry
	// err is used to return any error encountered
	err error
	// context is a context used to cancel the rebalance progress
	context context.Context
	// cancel is used to cancel the rebalance progress
	cancel context.CancelFunc
}

// NewRebalanceProgress creates a new RebalanceProgress object and starts a go routine to
// periodically poll for updates.
func (c *CouchbaseClient) NewRebalanceProgress(ms MemberSet) RebalanceProgress {
	progress := &rebalanceProgressImpl{
		statusChan: make(chan *RebalanceStatusEntry),
	}

	progress.context, progress.cancel = context.WithCancel(context.Background())

	go func() {
	RoutineRunloop:
		for {
			tasks := &TaskList{}
			if err := ListTasks(tasks).On(c.api, ms); err != nil {
				progress.err = err

				close(progress.statusChan)

				break RoutineRunloop
			}

			task, err := tasks.GetTask(TaskTypeRebalance)
			if err != nil {
				progress.err = err

				close(progress.statusChan)

				break RoutineRunloop
			}

			status := getRebalanceStatus(task)

			// If the task is no longer running then terminate the routine.
			if status == RebalanceStatusNotRunning {
				close(progress.statusChan)

				break RoutineRunloop
			}

			// Otherwise return the status to the client
			progress.statusChan <- &RebalanceStatusEntry{
				Status:   status,
				Progress: task.Progress,
			}

			// Wait for a period of time or for the client to close the
			// progress.  Do this in the loop tail to maintain compatibility
			// with the old code.
			select {
			case <-time.After(4 * time.Second):
			case <-progress.context.Done():
				break RoutineRunloop
			}
		}
	}()

	return progress
}

// Status returns the RebalanceProgress status channel.
func (r *rebalanceProgressImpl) Status() <-chan *RebalanceStatusEntry {
	return r.statusChan
}

// Error returns the RebalanceProgress error channel.
func (r *rebalanceProgressImpl) Error() error {
	return r.err
}

// Cancel terminates the RebalanceProgress routine.
func (r *rebalanceProgressImpl) Cancel() {
	r.cancel()
}

// getRebalanceStatus transforms a task status into our simplified rebalance status.
func getRebalanceStatus(task *Task) RebalanceStatus {
	// We treat stale or timed out tasks as unknown, no status
	// is treated as not running.
	status := RebalanceStatus(task.Status)

	switch {
	case task.Stale || task.Timeout:
		status = RebalanceStatusUnknown
	case status == RebalanceStatusNone:
		status = RebalanceStatusNotRunning
	}

	return status
}

// witnessRebalance waits until we can start streaming progress from the rebalance task.
func (c *CouchbaseClient) witnessRebalance(ms MemberSet) (RebalanceProgress, *RebalanceStatusEntry, error) {
	ctx, cancel := context.WithTimeout(c.ctx, time.Minute)
	defer cancel()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

WitnessLoop:
	for {
		progress := c.NewRebalanceProgress(ms)
		status, ok := <-progress.Status()
		if ok {
			return progress, status, nil
		}

		select {
		case <-tick.C:
		case <-ctx.Done():
			break WitnessLoop
		}
	}

	return nil, nil, errors.RebalanceNotObservedError
}

func (c *CouchbaseClient) Rebalance(ms MemberSet, eject OTPNodeList, cluster string) error {
	// The rebalance API is crap, rather than accepting a list of host names to eject
	// it requires a list of nodes that it already knows about as well.  On top of that
	// the names need translating into erlang rather than the hostnames that all the
	// clients and uses know and use.
	info := &ClusterInfo{}
	if err := GetPoolsDefault(info).On(c.api, ms); err != nil {
		return err
	}

	known := make(OTPNodeList, len(info.Nodes))

	for i, node := range info.Nodes {
		known[i] = node.OTPNode
	}

	if err := Rebalance(known, eject).On(c.api, ms); err != nil {
		return err
	}

	// Ensure we see the rebalance happen, if we don't do this then we can delete
	// pods prematurely.
	progress, status, err := c.witnessRebalance(ms)
	if err != nil {
		return err
	}

	// Stream out rebalance status.
	for {
		switch status.Status {
		case RebalanceStatusUnknown:
			log.Info("Rebalancing", "cluster", cluster, "progress", "unknown")
		case RebalanceStatusRunning:
			log.Info("Rebalancing", "cluster", cluster, "progress", status.Progress)
		}

		var ok bool

		status, ok = <-progress.Status()
		if !ok {
			return progress.Error()
		}
	}
}

// Check that cluster is actively rebalancing with status 'running'.
func (c *CouchbaseClient) IsRebalanceActive(ms MemberSet) (bool, error) {
	tasks := &TaskList{}
	if err := ListTasks(tasks).On(c.api, ms); err != nil {
		return false, err
	}

	task, err := tasks.GetTask(TaskTypeRebalance)
	if err != nil {
		return false, err
	}

	return getRebalanceStatus(task) == RebalanceStatusRunning, nil
}

func (c *CouchbaseClient) CloseIdleConnections() {
	c.api.CloseIdleConnections()
}
