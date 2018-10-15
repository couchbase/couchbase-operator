package couchbaseutil

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/prettytable"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/version"
	"github.com/couchbase/gocbmgr"
	"github.com/sirupsen/logrus"
)

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
	status := &ClusterStatus{}
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

const (
	RetryCount         = 5
	ExtendedRetryCount = 36
)

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

// SetUUID sets or updates the cluster UUID to be checked by new persistant
// connections being dialed
func (c *CouchbaseClient) SetUUID(uuid string) {
	c.client.SetUUID(uuid)
}

// nodeStatus is an intermediate data structured used to log node status information.
type nodeStatus struct {
	name    string
	version string
	class   string
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
func (cs *ClusterStatus) logClusterStatus(w io.Writer) error {
	states := []string{}

	// A cluster is either balanced or not
	if cs.NeedsRebalance {
		states = append(states, "unbalanced")
	} else {
		states = append(states, "balanced")
	}

	// A cluster may be rebalancing
	if cs.IsRebalancing {
		states = append(states, "rebalancing")
	}

	_, err := w.Write([]byte(fmt.Sprintf("Cluster status: %s\n", strings.Join(states, "+"))))
	return err
}

// logClusterNodeStatus logs the individual node statuses
func (cs *ClusterStatus) logClusterNodeStatus(w io.Writer) error {
	if _, err := w.Write([]byte("Node status:\n")); err != nil {
		return err
	}

	// Sort the names so it's easier to grok
	names := []string{}
	for name, _ := range cs.managedNodes {
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
		states := []string{"managed"}

		// And they will exist in one state
		state := cs.NodeStateMap[name]
		states = append(states, state.String())

		// Buffer up the status entry
		class := cs.managedNodes[name].ServerConfig
		version := cs.managedNodes[name].Version

		status := nodeStatus{
			name:    name,
			version: version,
			class:   class,
			state:   strings.Join(states, "+"),
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
	names = []string{}
	for _, name := range cs.UnmanagedNodes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		states := []string{"unmanaged"}

		// Ignore the error, if we've added the unmanaged node it has to exist in the node info.
		node, _ := cs.getNode(name)

		// Report the current state the unmanaged node is in, again ignore the error here as
		// it will return invalid if an error occurs.
		state, _ := getNodeState(node)
		states = append(states, state.String())

		// Buffer up the status entry
		status := nodeStatus{
			name:  name,
			state: strings.Join(states, "+"),
		}

		statuses = append(statuses, status)

		// Update the book keeping
		if len(name) > maxName {
			maxName = len(name)
		}
	}

	// Format our table
	table := prettytable.Table{
		Header: prettytable.Row{"Server", "Version", "Class", "Status"},
	}
	for _, status := range statuses {
		table.Rows = append(table.Rows, prettytable.Row{status.name, status.version, status.class, status.state})
	}
	return table.Write(w)
}

// Logs the cluster status
func (cs *ClusterStatus) LogStatus(w io.Writer) error {
	if err := cs.logClusterStatus(w); err != nil {
		return err
	}
	if err := cs.logClusterNodeStatus(w); err != nil {
		return err
	}
	return nil
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

func (c *CouchbaseClient) AddNode(ms MemberSet, hostname string, services cbapi.ServiceList) error {
	c.client.SetEndpoints(ms.ClientURLs())
	svcs, err := cbmgr.ServiceListFromStringArray(services.StringSlice())
	if err != nil {
		return err
	}

	return retryutil.RetryOnErr(c.ctx, 5*time.Second, ExtendedRetryCount, "add node", c.clusterName,
		func() error {
			return c.client.AddNode(hostname, c.username, c.password, svcs)
		})
}

func (c *CouchbaseClient) CancelAddNode(ms MemberSet, hostname string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, ExtendedRetryCount, "cancel add node", c.clusterName,
		func() error {
			return c.client.CancelAddNode(hostname)
		})
}

func (c *CouchbaseClient) CancelAddBackNode(ms MemberSet, hostname string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, ExtendedRetryCount, "cancel add back node", c.clusterName,
		func() error {
			return c.client.CancelAddBackNode(hostname)
		})
}

func (c *CouchbaseClient) ClusterUUID(m *Member) (string, error) {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())

	var err error
	var uuid string
	err = retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "cluster uuid", c.clusterName,
		func() error {
			uuid, err = c.client.ClusterUUID()
			return err
		})

	return uuid, err
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

	err := retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "cluster status", c.clusterName, func() error {
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
			status.addMemberToStateSet(state, member)
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

	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "node init", clusterName,
		func() error {
			return c.client.NodeInitialize(m.Addr(), dataPath, indexPath, analyticsPaths)
		})
}

func (c *CouchbaseClient) InitializeCluster(m *Member, username, password string, defaults *cbmgr.PoolsDefaults,
	services cbapi.ServiceList, dataPath string, indexPath string, analyticsPaths []string, indexStorageMode string) error {
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

	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "cluster init", defaults.ClusterName,
		func() error {
			return c.client.ClusterInitialize(username, password, defaults,
				8091, svcs, cbmgr.IndexStorageMode(indexStorageMode))
		})
}

func (c *CouchbaseClient) Rebalance(ms MemberSet, nodesToRemove []string, wait bool) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "rebalance", c.clusterName,
		func() error {
			status, err := c.client.Rebalance(nodesToRemove)
			if wait && status != nil {
				logger := c.ctx.Value("logger").(*logrus.Entry)
				status.SetLogger(logger)
				return status.Wait()
			}
			return err
		})
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

func (c *CouchbaseClient) CreateBucket(ms MemberSet, config *cbapi.BucketConfig) error {
	c.client.SetEndpoints(ms.ClientURLs())

	bucket := ApiBucketToCbmgr(config)
	if err := c.client.CreateBucket(bucket); err != nil {
		return err
	}

	// make sure bucket exists
	return retryutil.Retry(c.ctx, 5*time.Second, ExtendedRetryCount, func() (bool, error) {
		_, err := c.client.BucketReady(bucket.BucketName)
		// Bucket doesn't exist, someone deleted it.
		if cbmgr.IsServerError(err, 404) {
			return false, fmt.Errorf("Bucket %s does not exist", bucket.BucketName)
		}
		return err == nil, nil
	})
}

func (c *CouchbaseClient) DeleteBucket(ms MemberSet, bucketName string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return c.client.DeleteBucket(bucketName)
}

func (c *CouchbaseClient) EditBucket(ms MemberSet, config *cbapi.BucketConfig) error {
	c.client.SetEndpoints(ms.ClientURLs())
	bucket := ApiBucketToCbmgr(config)

	return c.client.EditBucket(bucket)
}

func (c *CouchbaseClient) GetBuckets(ms MemberSet) ([]*cbapi.BucketConfig, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	buckets, err := c.client.GetBuckets()
	if err != nil {
		return nil, err
	}

	rv := make([]*cbapi.BucketConfig, len(buckets))
	for i, bucket := range buckets {
		rv[i] = CbmgrBucketToApiBucket(bucket)
	}

	return rv, nil
}

func (c *CouchbaseClient) GetBucketNames(ms MemberSet) ([]string, error) {

	bucketNames := []string{}

	c.client.SetEndpoints(ms.ClientURLs())
	buckets, err := c.client.GetBuckets()
	if err == nil {
		for _, b := range buckets {
			bucketNames = append(bucketNames, b.BucketName)
		}
	}

	return bucketNames, err
}

// compare spec buckets to couchbase buckets and add to list of buckets
// that need editing
func (c *CouchbaseClient) GetBucketsToEdit(ms MemberSet, spec *cbapi.ClusterSpec) ([]string, error) {
	bucketNames := []string{}
	clusterBuckets, err := c.GetBuckets(ms)
	if err != nil {
		return nil, err
	}

	for _, clusterBucket := range clusterBuckets {
		specBucket := spec.GetBucketByName(clusterBucket.BucketName)
		if specBucket != nil {
			if !specBucket.Equals(clusterBucket) {
				bucketNames = append(bucketNames, specBucket.BucketName)
			}
		}
	}

	return bucketNames, nil
}

func (c *CouchbaseClient) SetAutoFailoverSettings(ms MemberSet, settings *cbmgr.AutoFailoverSettings) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "set autofailover timeout", c.clusterName,
		func() error {
			return c.client.SetAutoFailoverSettings(settings)
		})
}

func (c *CouchbaseClient) GetAutoFailoverSettings(ms MemberSet) (*cbmgr.AutoFailoverSettings, error) {
	c.client.SetEndpoints(ms.ClientURLs())
	var settings *cbmgr.AutoFailoverSettings

	return settings, retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "get autofailover settings", c.clusterName,
		func() (e error) {
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
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "upload CA cert", c.clusterName,
		func() error {
			return c.client.UploadClusterCACert(pem)
		})
}

func (c *CouchbaseClient) ReloadNodeCert(m *Member) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "reload server cert", c.clusterName,
		func() error {
			return c.client.ReloadNodeCert()
		})
}

func (c *CouchbaseClient) SetClientCertAuth(m *Member, settings *cbmgr.ClientCertAuth) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())
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
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "set delta recovery", c.clusterName,
		func() error {
			return c.client.SetRecoveryType(hostname, cbmgr.RecoveryTypeDelta)
		})
}

func (c *CouchbaseClient) SetRecoveryTypeFull(ms MemberSet, hostname string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "set full recovery", c.clusterName,
		func() error {
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

	return cbmgr.RecoveryTypeFull, fmt.Errorf("No member exists for %s", m.Name)
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

func ApiBucketToCbmgr(config *cbapi.BucketConfig) *cbmgr.Bucket {
	rv := &cbmgr.Bucket{
		BucketName:        config.BucketName,
		BucketType:        config.BucketType,
		BucketMemoryQuota: config.BucketMemoryQuota,
		EnableFlush:       &config.EnableFlush,
	}

	if rv.BucketType == constants.BucketTypeMemcached {
		return rv
	}

	// Required ephemeral/couchbase fields
	rv.BucketReplicas = config.BucketReplicas
	rv.IoPriority = cbmgr.IoPriorityType(config.IoPriority)
	rv.EvictionPolicy = &config.EvictionPolicy
	rv.ConflictResolution = &config.ConflictResolution

	// Required couchbase only fields
	if rv.BucketType == constants.BucketTypeMembase || rv.BucketType == constants.BucketTypeCouchbase {
		rv.BucketType = constants.BucketTypeCouchbase
		rv.EnableIndexReplica = &config.EnableIndexReplica
	}

	return rv
}

func CbmgrBucketToApiBucket(bucket *cbmgr.Bucket) *cbapi.BucketConfig {
	rv := &cbapi.BucketConfig{
		BucketName:        bucket.BucketName,
		BucketType:        bucket.BucketType,
		BucketMemoryQuota: bucket.BucketMemoryQuota,
	}

	rv.EnableFlush = bucket.EnableFlush != nil && *bucket.EnableFlush

	if rv.BucketType == constants.BucketTypeMemcached {
		return rv
	}

	// Required ephemeral/couchbase fields
	rv.BucketReplicas = bucket.BucketReplicas
	rv.IoPriority = string(bucket.IoPriority)
	rv.EvictionPolicy = *bucket.EvictionPolicy
	rv.ConflictResolution = *bucket.ConflictResolution

	// Required couchbase only fields
	if rv.BucketType == constants.BucketTypeMembase || rv.BucketType == constants.BucketTypeCouchbase {
		rv.BucketType = constants.BucketTypeCouchbase
		rv.EnableIndexReplica = *bucket.EnableIndexReplica
	}

	return rv
}

// transforms bucket status into bucketConfig type and compares the two
func bucketStatusEqualsConfig(statusConfig *cbmgr.Bucket, specConfig *cbmgr.Bucket) bool {

	// consider type couchbase = membase
	if specConfig.BucketType == constants.BucketTypeCouchbase && statusConfig.BucketType == constants.BucketTypeMembase {
		statusConfig.BucketType = constants.BucketTypeCouchbase
	}

	return reflect.DeepEqual(statusConfig, specConfig)
}
