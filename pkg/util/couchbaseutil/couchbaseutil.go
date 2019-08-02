package couchbaseutil

import (
	"context"
	"crypto/tls"
	"fmt"
	"sort"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/version"
	"github.com/couchbase/gocbmgr"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
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

	// DefaultRetryPeriod is the default amount of time to wait for an API
	// operation to report success.
	DefaultRetryPeriod = 30 * time.Second

	// ExtendedRetryPeriod is an extended amount of time to wait for slow
	// API operations to report success.
	ExtendedRetryPeriod = 3 * time.Minute
)

// String converts a node state into a string representation
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
	info *cbmgr.ClusterInfo
	// NodeStateMap maps a known member to it's current state.  This is an
	// optimisation to avoid scanning the main sets below it determine the
	// state of a member.
	NodeStateMap NodeStateMap
	// managedNodes is a set of all known nodes.  This is used to iterate
	// over the set of all nodes.  This is analogous to the cluster members
	// set.
	managedNodes MemberSet

	ActiveNodes      MemberSet // status=healthy,   clusterMembership=active
	PendingAddNodes  MemberSet // status=healthy,   clusterMembership=inactiveAdded
	AddBackNodes     MemberSet // status=healthy,   clusterMembership=inactiveFailed
	FailedAddNodes   MemberSet // status=unhealthy, clusterMembership=inactiveAdded
	WarmupNodes      MemberSet // status=warmup,    clusterMembership=inactiveAdded
	DownNodes        MemberSet // status=unhealthy, clusterMembership=active
	FailedNodes      MemberSet // status=unhealthy, clusterMembership=inactiveFailed
	UnclusteredNodes MemberSet // Managed by Kubernetes, but not part of the cluster
	UnmanagedNodes   []string  // Not managed by Kubernetes
	IsRebalancing    bool
	NeedsRebalance   bool
}

// NewClusterStatus returns a cluster status object with all
// lists and maps initialized
func NewClusterStatus() *ClusterStatus {
	status := &ClusterStatus{
		info: &cbmgr.ClusterInfo{},
	}
	status.Reset()
	return status
}

// Reset replaces all sets in the cluster status with empty versions
// and returns scalars to the zero state
func (c *ClusterStatus) Reset() {
	c.NodeStateMap = NodeStateMap{}
	c.managedNodes = NewMemberSet()
	c.ActiveNodes = NewMemberSet()
	c.PendingAddNodes = NewMemberSet()
	c.AddBackNodes = NewMemberSet()
	c.FailedAddNodes = NewMemberSet()
	c.WarmupNodes = NewMemberSet()
	c.DownNodes = NewMemberSet()
	c.FailedNodes = NewMemberSet()
	c.UnclusteredNodes = NewMemberSet()
	c.UnmanagedNodes = []string{}
	c.IsRebalancing = false
	c.NeedsRebalance = false
}

// CouchbaseClient encapsulates Couchbase API operations.
type CouchbaseClient struct {
	// ctx is used to asynchronously cancel blocking or long operations.
	ctx context.Context
	// clusterName is used in logging to add context if multiple clusters are
	// defined
	clusterName string
	// username is used in HTTP basic authorization
	username string
	// password is used in HTTP basic authorization
	password string
	// client is ostensibly an http.Client.  It contains a pool of persistent
	// connections to target hosts for various host/port combinations.  When
	// client parameters are changed which affect HTTP or TLS this needs to be
	// refereshed e.g. a password is changed or the uuid is defined
	client *cbmgr.Couchbase
}

// NewCouchbaseClient allocates and initializes a new couchbase API client.
// Connections are persistent so we only expect to create a single client
// per cluster
func NewCouchbaseClient(ctx context.Context, clusterName, username, password string) *CouchbaseClient {
	c := &CouchbaseClient{
		ctx:         ctx,
		clusterName: clusterName,
		username:    username,
		password:    password,
	}
	c.client = cbmgr.New(username, password)

	// Make the User-Agent string more verbose about exactly who/what we are
	// to simplify support incidents
	userAgent := &cbmgr.UserAgent{
		Name:    version.Application,
		Version: version.Version,
		UUID:    revision.Revision(),
	}
	c.client.SetUserAgent(userAgent)

	return c
}

// SetTLS sets or updates the TLS configuration for a client
func (c *CouchbaseClient) SetTLS(tls *cbmgr.TLSAuth) {
	c.client.SetTLS(tls)
}

// GetTLS returns the TLS configuration for the client.
func (c *CouchbaseClient) GetTLS() *cbmgr.TLSAuth {
	return c.client.GetTLS()
}

// SetUUID sets or updates the cluster UUID to be checked by new persistent
// connections being dialed
func (c *CouchbaseClient) SetUUID(uuid string) {
	c.client.SetUUID(uuid)
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
func (cs *ClusterStatus) getNode(hostname string) (*cbmgr.NodeInfo, error) {
	for _, node := range cs.info.Nodes {
		if node.HostName == hostname {
			return &node, nil
		}
	}
	return nil, fmt.Errorf("node %s does not exist in cluster", hostname)
}

// KnownNodes returns all nodes that the cluster is tracking
func (cs *ClusterStatus) KnownNodes() []string {

	knownNodes := []string{}
	for _, node := range cs.info.Nodes {
		memberName := strings.Split(node.HostName, ".")[0]
		knownNodes = append(knownNodes, memberName)
	}

	return knownNodes
}

// getNodeState looks up node status based on Couchbase hostname.
func getNodeState(node *cbmgr.NodeInfo) (state NodeState, err error) {
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

// logClusterStatus logs the overal cluster status e.g. balanced condition
func (cs *ClusterStatus) logClusterStatus(cluster string) {
	// A cluster is either balanced or not
	balance := "balanced"
	if cs.NeedsRebalance {
		balance = "unbalanced"
	}

	log.Info("Cluster status", "cluster", cluster, "balance", balance, "rebalancing", cs.IsRebalancing)
}

// logClusterNodeStatus logs the individual node statuses
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

	// Sort the names so it's easier to grok
	sort.Strings(cs.UnmanagedNodes)

	for _, name := range cs.UnmanagedNodes {
		// Ignore the error, if we've added the unmanaged node it has to exist in the node info.
		node, _ := cs.getNode(name)

		// Report the current state the unmanaged node is in, again ignore the error here as
		// it will return invalid if an error occurs.
		state, _ := getNodeState(node)

		// Buffer up the status entry
		status := nodeStatus{
			name:    name,
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

// Logs the cluster status
func (cs *ClusterStatus) LogStatus(cluster string) {
	cs.logClusterStatus(cluster)
	cs.logClusterNodeStatus(cluster)
}

// Are all managed nodes healthy? e.g. in the active state
func (cs *ClusterStatus) AllManagedNodesHealthy() bool {
	for _, member := range cs.managedNodes {
		if cs.NodeStateMap[member.Name] != NodeStateActive {
			return false
		}
	}
	return true
}

// Do any nodes exist that we aren't managing
func (cs *ClusterStatus) AnyUnmanagedNodes() bool {
	return len(cs.UnmanagedNodes) != 0
}

// Is the cluster as a whole healthy
func (cs *ClusterStatus) ClusterHealthy() bool {
	return cs.AllManagedNodesHealthy() &&
		!cs.AnyUnmanagedNodes() &&
		!cs.IsRebalancing &&
		!cs.NeedsRebalance
}

// Is the named node in any of the states set in the bitmap
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

// Filter named nodes from a cluster status map
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

// check the health of a particular Couchbase node.
func CheckHealth(url string, tc *tls.Config) (bool, error) {
	// TODO: check the health of a particular Couchbase node.
	return true, nil
}

func (c *CouchbaseClient) AddNode(ms MemberSet, hostname string, services couchbasev2.ServiceList) error {
	c.client.SetEndpoints(ms.ClientURLs())
	svcs, err := cbmgr.ServiceListFromStringArray(services.StringSlice())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.ctx, ExtendedRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.AddNode(hostname, c.username, c.password, svcs)
	})
}

func (c *CouchbaseClient) CancelAddNode(ms MemberSet, hostname string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	ctx, cancel := context.WithTimeout(c.ctx, ExtendedRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.CancelAddNode(hostname)
	})
}

func (c *CouchbaseClient) CancelAddBackNode(ms MemberSet, hostname string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	ctx, cancel := context.WithTimeout(c.ctx, ExtendedRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.CancelAddBackNode(hostname)
	})
}

func (c *CouchbaseClient) ClusterUUID(m *Member) (string, error) {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())

	var err error
	var uuid string
	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	err = retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		uuid, err = c.client.ClusterUUID()
		return err
	})

	return uuid, err
}

// IsEnterprise returns whether the member is Couchbase Enterprise Edition or not.
func (c *CouchbaseClient) IsEnterprise(m *Member) (bool, error) {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())

	var err error
	var isEnterprise bool
	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	err = retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		isEnterprise, err = c.client.IsEnterprise()
		return err
	})

	return isEnterprise, err
}

func (c *CouchbaseClient) GetClusterStatus(ms MemberSet) (*ClusterStatus, error) {
	status := &ClusterStatus{}
	if err := c.UpdateClusterStatus(ms, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (c *CouchbaseClient) UpdateClusterStatus(ms MemberSet, status *ClusterStatus) error {
	c.client.SetEndpoints(ms.ClientURLs())

	status.Reset()

	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	err := retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		// Get the cluster information from Couchbase server
		var err error
		status.info, err = c.client.ClusterInfo()
		if err != nil {
			return err
		}

		// Cache the managed nodes in the status
		status.managedNodes.Append(ms)

		// Collect the node known to Couchbase server as we iterate through the cluster map
		knownNodes := NewMemberSet()

		// Iterate over all of the nodes known to Couchbase server
		for _, node := range status.info.Nodes {
			// The node name should be in the form cb-pod.cb-cluster.namespace.svc:8091.
			// By extracting the first field we can derive the pod name.  If it is not
			// know to the operator we flag it as unmanaged.
			member := ms[strings.Split(node.HostName, ".")[0]]
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

		status.IsRebalancing = (status.info.RebalanceStatus == string(cbmgr.RebalanceStatusRunning))
		status.NeedsRebalance = !status.info.Balanced && len(status.info.Nodes) > 1
		return nil
	})

	return err
}

func (c *CouchbaseClient) NodeInitialize(m *Member, clusterName string, dataPath string, indexPath string, analyticsPaths []string) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())

	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.NodeInitialize(m.Addr(), dataPath, indexPath, analyticsPaths)
	})
}

func (c *CouchbaseClient) InitializeCluster(m *Member, username, password string, defaults *cbmgr.PoolsDefaults,
	services couchbasev2.ServiceList, dataPath string, indexPath string, analyticsPaths []string, indexStorageMode string) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())

	err := c.NodeInitialize(m, defaults.ClusterName, dataPath, indexPath, analyticsPaths)
	if err != nil {
		return err
	}

	svcs, err := cbmgr.ServiceListFromStringArray(services.StringSlice())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.ClusterInitialize(username, password, defaults, 8091, svcs, cbmgr.IndexStorageMode(indexStorageMode))
	})
}

func (c *CouchbaseClient) Rebalance(ms MemberSet, nodesToRemove []string, wait bool, cluster string) error {
	c.client.SetEndpoints(ms.ClientURLs())

	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()

	// Try rebalance a few times before giving up.
	callback := func() error {
		return c.client.Rebalance(nodesToRemove)
	}
	if err := retryutil.RetryOnErr(ctx, 5*time.Second, callback); err != nil {
		return err
	}

	if !wait {
		return nil
	}

	// Wait for a rebalance to commence, retry this a few times, we expect to witness
	// the rebalance.
	seenRebalance := false
	callback = func() error {
		progress := c.client.NewRebalanceProgress()
		for {
			status, ok := <-progress.Status()
			if !ok {
				if !seenRebalance {
					return fmt.Errorf("rebalance task not observed as running: %v", progress.Error())
				}
				return progress.Error()
			}
			seenRebalance = true
			switch status.Status {
			case cbmgr.RebalanceStatusUnknown:
				log.Info("Rebalancing", "cluster", cluster, "progress", "unknown")
			case cbmgr.RebalanceStatusRunning:
				log.Info("Rebalancing", "cluster", cluster, "progress", status.Progress)
			}
		}
	}
	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		return err
	}

	return nil
}

func (c *CouchbaseClient) StopRebalance(ms MemberSet) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.StopRebalance()
}

// Check that cluster is actively rebalancing with status 'running'
func (c *CouchbaseClient) IsRebalanceActive(ms MemberSet) (bool, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.CompareRebalanceStatus(cbmgr.RebalanceStatusRunning)
}

func (c *CouchbaseClient) ListBuckets(ms MemberSet) ([]cbmgr.Bucket, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	buckets, err := c.client.GetBuckets()
	if err != nil {
		return nil, err
	}

	// TODO: Standardize on value/pointer
	res := []cbmgr.Bucket{}
	for _, bucket := range buckets {
		res = append(res, *bucket)
	}
	return res, nil
}

func (c *CouchbaseClient) CreateBucket(ms MemberSet, bucket cbmgr.Bucket) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.CreateBucket(&bucket)
}

func (c *CouchbaseClient) UpdateBucket(ms MemberSet, bucket cbmgr.Bucket) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.EditBucket(&bucket)
}

func (c *CouchbaseClient) DeleteBucket(ms MemberSet, bucket cbmgr.Bucket) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.DeleteBucket(bucket.BucketName)
}

func (c *CouchbaseClient) ListUsers(ms MemberSet) ([]cbmgr.User, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	users, err := c.client.GetUsers()
	if err != nil {
		return nil, err
	}

	res := []cbmgr.User{}
	for _, user := range users {
		res = append(res, *user)
	}
	return res, nil
}

func (c *CouchbaseClient) CreateUser(ms MemberSet, user cbmgr.User) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.CreateUser(&user)
}

func (c *CouchbaseClient) DeleteUser(ms MemberSet, user cbmgr.User) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.DeleteUser(&user)
}

func (c *CouchbaseClient) SetAutoFailoverSettings(ms MemberSet, settings *cbmgr.AutoFailoverSettings) error {
	c.client.SetEndpoints(ms.ClientURLs())
	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.SetAutoFailoverSettings(settings)
	})
}

func (c *CouchbaseClient) GetAutoFailoverSettings(ms MemberSet) (*cbmgr.AutoFailoverSettings, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	var settings *cbmgr.AutoFailoverSettings

	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	return settings, retryutil.RetryOnErr(ctx, 5*time.Second, func() (e error) {
		settings, e = c.client.GetAutoFailoverSettings()
		return e
	})
}

func (c *CouchbaseClient) GetClusterInfo(ms MemberSet) (*cbmgr.ClusterInfo, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.ClusterInfo()
}

func (c *CouchbaseClient) SetPoolsDefault(ms MemberSet, defaults *cbmgr.PoolsDefaults) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.SetPoolsDefault(defaults)
}

func (c *CouchbaseClient) UploadClusterCACert(m *Member, pem []byte) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.UploadClusterCACert(pem)
	})
}

func (c *CouchbaseClient) GetClusterCACert(m *Member) ([]byte, error) {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.GetClusterCACert()
}

func (c *CouchbaseClient) ReloadNodeCert(m *Member) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	// This may take ages if the CA was previously updated.
	ctx, cancel := context.WithTimeout(c.ctx, ExtendedRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.ReloadNodeCert()
	})
}

// GetClientCertAuth/SetClientCertAuth allow reconciliation of TLS settings.  The ordering constraints
// when using mTLS make the process very messy, so this is always done over HTTP.
func (c *CouchbaseClient) GetClientCertAuth(ms MemberSet) (*cbmgr.ClientCertAuth, error) {
	c.client.SetEndpoints(ms.ClientURLsPlaintext())
	return c.client.GetClientCertAuth()
}

func (c *CouchbaseClient) CloseIdleConnections() {
	c.client.CloseIdleConnections()
}

func (c *CouchbaseClient) SetClientCertAuth(ms MemberSet, settings *cbmgr.ClientCertAuth) error {
	c.client.SetEndpoints(ms.ClientURLsPlaintext())
	return c.client.SetClientCertAuth(settings)
}

func (c *CouchbaseClient) GetUpdatesEnabled(ms MemberSet) (bool, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.GetUpdatesEnabled()
}

func (c *CouchbaseClient) SetUpdatesEnabled(ms MemberSet, enabled bool) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.SetUpdatesEnabled(enabled)
}

func (c *CouchbaseClient) GetIndexSettings(ms MemberSet, username, password string) (*cbmgr.IndexSettings, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.GetIndexSettings()
}

func (c *CouchbaseClient) SetIndexSettings(ms MemberSet, username, password, storageMode string, settings *cbmgr.IndexSettings) error {
	c.client.SetEndpoints(ms.ClientURLs())
	settings.StorageMode = cbmgr.IndexStorageMode(storageMode)
	return c.client.SetIndexSettings(settings)
}

func (c *CouchbaseClient) GetAlternateAddressesExternal(m *Member) (*cbmgr.AlternateAddressesExternal, error) {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.GetAlternateAddressesExternal()
}

func (c *CouchbaseClient) SetAlternateAddressesExternal(m *Member, addresses *cbmgr.AlternateAddressesExternal) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.SetAlternateAddressesExternal(addresses)
}

func (c *CouchbaseClient) DeleteAlternateAddressesExternal(m *Member) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.DeleteAlternateAddressesExternal()
}

func (c *CouchbaseClient) SetRecoveryTypeDelta(ms MemberSet, hostname string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.SetRecoveryType(hostname, cbmgr.RecoveryTypeDelta)
	})
}

func (c *CouchbaseClient) SetRecoveryTypeFull(ms MemberSet, hostname string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	ctx, cancel := context.WithTimeout(c.ctx, DefaultRetryPeriod)
	defer cancel()
	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		return c.client.SetRecoveryType(hostname, cbmgr.RecoveryTypeFull)
	})
}

// Get RecoveryType of node if it has been set.
// The default type is 'full'
func (c *CouchbaseClient) GetRecoveryType(m *Member) (cbmgr.RecoveryType, error) {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	info, err := c.client.ClusterInfo()
	if err != nil {
		return cbmgr.RecoveryTypeFull, err
	}
	for _, node := range info.Nodes {
		member := ms[strings.Split(node.HostName, ".")[0]]
		if member != nil && member.Name == m.Name {
			return cbmgr.RecoveryType(node.RecoveryType), nil
		}
	}

	return cbmgr.RecoveryTypeFull, fmt.Errorf("no member exists for %s", m.Name)
}

func (c *CouchbaseClient) IsRecoveryTypeDelta(m *Member) (bool, error) {
	recoveryType, err := c.GetRecoveryType(m)
	if err != nil {
		return false, err
	}
	return recoveryType == cbmgr.RecoveryTypeDelta, nil
}

func (c *CouchbaseClient) GetServerGroups(ms MemberSet) (*cbmgr.ServerGroups, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.GetServerGroups()
}

func (c *CouchbaseClient) CreateServerGroup(ms MemberSet, name string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.CreateServerGroup(name)
}

func (c *CouchbaseClient) UpdateServerGroups(ms MemberSet, revision string, groups *cbmgr.ServerGroupsUpdate) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.UpdateServerGroups(revision, groups)
}

func (c *CouchbaseClient) GetAutoCompactionSettings(ms MemberSet) (*cbmgr.AutoCompactionSettings, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.GetAutoCompactionSettings()
}

func (c *CouchbaseClient) SetAutoCompactionSettings(ms MemberSet, r *cbmgr.AutoCompactionSettings) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.SetAutoCompactionSettings(r)
}

func (c *CouchbaseClient) ListRemoteClusters(ms MemberSet) (cbmgr.RemoteClusters, error) {
	return c.client.ListRemoteClusters()
}

func (c *CouchbaseClient) CreateRemoteCluster(ms MemberSet, r *cbmgr.RemoteCluster) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.CreateRemoteCluster(r)
}

func (c *CouchbaseClient) DeleteRemoteCluster(ms MemberSet, r *cbmgr.RemoteCluster) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.DeleteRemoteCluster(r)
}

func (c *CouchbaseClient) ListReplications(ms MemberSet) ([]cbmgr.Replication, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.ListReplications()
}

func (c *CouchbaseClient) CreateReplication(ms MemberSet, r *cbmgr.Replication) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.CreateReplication(r)
}

func (c *CouchbaseClient) UpdateReplication(ms MemberSet, r *cbmgr.Replication) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.UpdateReplication(r)
}

func (c *CouchbaseClient) DeleteReplication(ms MemberSet, r *cbmgr.Replication) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.DeleteReplication(r)
}

func (c *CouchbaseClient) SetLDAPSettings(ms MemberSet, settings *cbmgr.LDAPSettings) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.SetLDAPSettings(settings)
}

func (c *CouchbaseClient) GetLDAPSettings(ms MemberSet) (*cbmgr.LDAPSettings, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.GetLDAPSettings()
}
