package couchbaseutil

import (
	"context"
	"crypto/tls"
	"fmt"
	"reflect"
	"strings"
	"time"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
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
	NodeStateDown
	NodeStateFailed
	NodeStateAddBack
	NodeStateUnclustered
)

type ClusterStatus struct {
	ActiveNodes      MemberSet // status=healthy,   clusterMembership=active
	PendingAddNodes  MemberSet // status=healthy,   clusterMembership=inactiveAdded
	AddBackNodes     MemberSet // status=healthy, clusterMembership=inactiveFailed
	FailedAddNodes   MemberSet // status=unhealthy, clusterMembership=inactiveAdded
	DownNodes        MemberSet // status=unhealthy, clusterMembership=active
	FailedNodes      MemberSet // status=unhealthy, clusterMembership=inactiveFailed
	UnclusteredNodes MemberSet // Managed by Kubernetes, but not part of the cluster
	UnmanagedNodes   []string  // Not managed by Kubernetes
	IsRebalancing    bool
	NeedsRebalance   bool
}

const (
	RetryCount         = 5
	ExtendedRetryCount = 36
)

type ClusterStateMap map[string]NodeState

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

// Logs the cluster status
func (cs *ClusterStatus) LogStatus(logger *logrus.Entry) {
	if !cs.ActiveNodes.Empty() {
		logger.Infof("active nodes: %s", cs.ActiveNodes)
	}
	if !cs.PendingAddNodes.Empty() {
		logger.Infof("pending add nodes: %s", cs.PendingAddNodes)
	}
	if !cs.FailedAddNodes.Empty() {
		logger.Infof("failed add nodes: %s", cs.FailedAddNodes)
	}
	if !cs.DownNodes.Empty() {
		logger.Infof("down nodes: %s", cs.DownNodes)
	}
	if !cs.FailedNodes.Empty() {
		logger.Infof("failed nodes: %s", cs.FailedNodes)
	}
	if !cs.AddBackNodes.Empty() {
		logger.Infof("add back: %s", cs.AddBackNodes)
	}
	if !cs.UnclusteredNodes.Empty() {
		logger.Infof("unclustered nodes: %s", cs.UnclusteredNodes)
	}
	if len(cs.UnmanagedNodes) != 0 {
		logger.Infof("unmanaged nodes: %s", strings.Join(cs.UnmanagedNodes, ","))
	}
	logger.Infof("is rebalancing: %t", cs.IsRebalancing)
	logger.Infof("needs rebalance: %t", cs.NeedsRebalance)
}

// Are all managed nodes healthy? e.g. in the active state
func (cs *ClusterStatus) AllManagedNodesHealthy() bool {
	return cs.PendingAddNodes.Empty() &&
		cs.FailedAddNodes.Empty() &&
		cs.DownNodes.Empty() &&
		cs.FailedNodes.Empty() &&
		cs.AddBackNodes.Empty() &&
		cs.UnclusteredNodes.Empty()
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
	for _, state := range states {
		ok := false
		switch state {
		case NodeStateActive:
			_, ok = cs.ActiveNodes[name]
		case NodeStatePendingAdd:
			_, ok = cs.PendingAddNodes[name]
		case NodeStateFailedAdd:
			_, ok = cs.FailedAddNodes[name]
		case NodeStateDown:
			_, ok = cs.DownNodes[name]
		case NodeStateFailed:
			_, ok = cs.FailedNodes[name]
		case NodeStateAddBack:
			_, ok = cs.AddBackNodes[name]
		case NodeStateUnclustered:
			_, ok = cs.UnclusteredNodes[name]
		}
		if ok {
			return true
		}
	}
	return false
}

// Does the named node exist anywhere?
func (cs *ClusterStatus) ContainsNode(name string) bool {
	return cs.NodeInState(name,
		NodeStateActive,
		NodeStatePendingAdd,
		NodeStateFailedAdd,
		NodeStateDown,
		NodeStateFailed,
		NodeStateAddBack,
		NodeStateUnclustered)
}

// Convert a ClusterStatus object into a simple Name -> State mapping
func (cs *ClusterStatus) NewClusterStateMap() ClusterStateMap {
	stateCollectionMap := map[NodeState]MemberSet{
		NodeStateActive:     cs.ActiveNodes,
		NodeStatePendingAdd: cs.PendingAddNodes,
		NodeStateFailedAdd:  cs.FailedAddNodes,
		NodeStateDown:       cs.DownNodes,
		NodeStateFailed:     cs.FailedNodes,
		NodeStateAddBack:    cs.AddBackNodes,
	}

	states := ClusterStateMap{}
	for state, collection := range stateCollectionMap {
		for name, _ := range collection {
			states[name] = state
		}
	}
	return states
}

// Filter named nodes from a cluster status map
func (csm ClusterStateMap) Exclude(excludes ...string) ClusterStateMap {
	states := ClusterStateMap{}
	for name, state := range csm {
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
func (csm ClusterStateMap) AllActive() bool {
	for _, state := range csm {
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

func (c *CouchbaseClient) AddNode(ms MemberSet, hostname string, services []string) error {
	c.client.SetEndpoints(ms.ClientURLs())
	svcs, err := cbmgr.ServiceListFromStringArray(services)
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
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, ExtendedRetryCount, "add node", c.clusterName,
		func() error {
			return c.client.CancelAddNode(hostname)
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

	status.ActiveNodes = NewMemberSet()
	status.PendingAddNodes = NewMemberSet()
	status.FailedAddNodes = NewMemberSet()
	status.DownNodes = NewMemberSet()
	status.FailedNodes = NewMemberSet()
	status.AddBackNodes = NewMemberSet()
	status.UnclusteredNodes = NewMemberSet()
	status.UnmanagedNodes = []string{}

	err := retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "cluster status", c.clusterName, func() error {
		info, err := c.client.ClusterInfo()
		if err != nil {
			return err
		}

		managed := NewMemberSet()
		for _, node := range info.Nodes {
			member := ms[strings.Split(node.HostName, ".")[0]]
			if member == nil {
				status.UnmanagedNodes = append(status.UnmanagedNodes, node.HostName)
				continue
			}

			managed.Add(member)
			if node.Status == "healthy" || node.Status == "warmup" {
				if node.Membership == "active" {
					status.ActiveNodes.Add(member)
				} else if node.Membership == "inactiveAdded" {
					status.PendingAddNodes.Add(member)
				} else if node.Membership == "inactiveFailed" {
					status.AddBackNodes.Add(member) // Caused by manual failover
				} else {
					return fmt.Errorf("cluster status: status=%s membership=%s", node.Status, node.Membership)
				}
			} else if node.Status == "unhealthy" {
				if node.Membership == "active" {
					status.DownNodes.Add(member)
				} else if node.Membership == "inactiveAdded" {
					status.FailedAddNodes.Add(member)
				} else if node.Membership == "inactiveFailed" {
					status.FailedNodes.Add(member)
				} else {
					return fmt.Errorf("cluster status: status=%s membership=%s", node.Status, node.Membership)
				}
			} else {
				return fmt.Errorf("cluster status: status=%s membership=%s", node.Status, node.Membership)
			}
		}

		status.UnclusteredNodes = ms.Diff(managed)
		status.IsRebalancing = (info.RebalanceStatus == cbmgr.RebalanceStatusRunning)
		status.NeedsRebalance = !info.Balanced && len(info.Nodes) > 1
		return nil
	})

	return err
}

func (c *CouchbaseClient) InitializeCluster(m *Member, username, password string, defaults *cbmgr.PoolsDefaults,
	services []string, dataPath, indexPath, indexStorageMode string) error {
	ms := NewMemberSet(m)
	c.client.SetEndpoints(ms.ClientURLs())

	err := retryutil.RetryOnErr(c.ctx, 5*time.Second, RetryCount, "node init", defaults.ClusterName,
		func() error {
			return c.client.NodeInitialize(m.Addr(), dataPath, indexPath)
		})

	if err != nil {
		return err
	}

	svcs, err := cbmgr.ServiceListFromStringArray(services)
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
				status.SetLogger(retryutil.Log(c.clusterName))
				return status.Wait()
			}
			return err
		})
}

func (c *CouchbaseClient) CreateBucket(ms MemberSet, config *cbapi.BucketConfig) error {
	c.client.SetEndpoints(ms.ClientURLs())

	bucket := ApiBucketToCbmgr(config)
	if err := c.client.CreateBucket(bucket); err != nil {
		return err
	}

	// make sure bucket exists
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, ExtendedRetryCount, "create bucket", c.clusterName,
		func() error {
			_, err := c.client.BucketReady(bucket.BucketName)
			return err
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
	return c.client.SetRecoveryType(hostname, cbmgr.RecoveryTypeDelta)
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

	if config.BucketReplicas != nil {
		rv.BucketReplicas = *config.BucketReplicas
	}

	if config.IoPriority != nil {
		rv.IoPriority = cbmgr.IoPriorityType(*config.IoPriority)
	}

	rv.ConflictResolution = config.ConflictResolution
	rv.EvictionPolicy = config.EvictionPolicy

	if rv.BucketType == constants.BucketTypeMembase || rv.BucketType == constants.BucketTypeCouchbase {
		rv.BucketType = constants.BucketTypeCouchbase
		rv.EnableIndexReplica = config.EnableIndexReplica
	}

	return rv
}

func CbmgrBucketToApiBucket(bucket *cbmgr.Bucket) *cbapi.BucketConfig {
	rv := &cbapi.BucketConfig{
		BucketName:        bucket.BucketName,
		BucketType:        bucket.BucketType,
		BucketMemoryQuota: bucket.BucketMemoryQuota,
	}

	if bucket.EnableFlush == nil || *bucket.EnableFlush == false {
		rv.EnableFlush = false
	} else {
		rv.EnableFlush = true
	}

	if rv.BucketType == constants.BucketTypeMemcached {
		return rv
	}

	ioPriority := string(bucket.IoPriority)
	rv.BucketReplicas = &bucket.BucketReplicas
	rv.IoPriority = &ioPriority
	rv.ConflictResolution = bucket.ConflictResolution
	rv.EvictionPolicy = bucket.EvictionPolicy

	if rv.BucketType == constants.BucketTypeMembase || rv.BucketType == constants.BucketTypeCouchbase {
		rv.BucketType = constants.BucketTypeCouchbase
		rv.EnableIndexReplica = bucket.EnableIndexReplica
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
