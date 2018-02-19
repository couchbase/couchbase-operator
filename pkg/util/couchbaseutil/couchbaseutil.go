package couchbaseutil

import (
	"context"
	"crypto/tls"
	"reflect"
	"strings"
	"time"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbaselabs/gocbmgr"
)

type ClusterStatus struct {
	ActiveNodes      MemberSet // status=healthy,   clusterMembership=active
	PendingAddNodes  MemberSet // status=healthy,   clusterMembership=inactiveAdded
	FailedAddNodes   MemberSet // status=unhealthy, clusterMembership=inactiveAdded
	DownNodes        MemberSet // status=unhealthy, clusterMembership=active
	FailedNodes      MemberSet // status=unhealthy, clusterMembership=inactiveFailed
	UnclusteredNodes MemberSet // Managed by Kubernetes, but not part of the cluster
	UnmanagedNodes   []string  // Not managed by Kubernetes
	IsRebalancing    bool
	NeedsRebalance   bool
}

type CouchbaseClient struct {
	ctx         context.Context
	clusterName string
	username    string
	password    string
	tls         *cbmgr.TLSAuth
}

func NewCouchbaseClient(ctx context.Context, clusterName, username, password string) *CouchbaseClient {
	return &CouchbaseClient{
		ctx:         ctx,
		clusterName: clusterName,
		username:    username,
		password:    password,
	}
}

func (c *CouchbaseClient) SetTLS(tls *cbmgr.TLSAuth) {
	c.tls = tls
}

// check the health of a particular Couchbase node.
func CheckHealth(url string, tc *tls.Config) (bool, error) {
	// TODO: check the health of a particular Couchbase node.
	return true, nil
}

func (c *CouchbaseClient) getClient(ms MemberSet) *cbmgr.Couchbase {
	return cbmgr.New(ms.ClientURLs(), c.username, c.password, c.tls)
}

func (c *CouchbaseClient) AddNode(ms MemberSet, hostname string, services []string) error {
	client := c.getClient(ms)
	svcs, err := cbmgr.ServiceListFromStringArray(services)
	if err != nil {
		return err
	}

	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "add node", c.clusterName,
		func() error {
			return client.AddNode(hostname, c.username, c.password, svcs)
		})
}

func (c *CouchbaseClient) CancelAddNode(ms MemberSet, hostname string) error {
	client := c.getClient(ms)
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "add node", c.clusterName,
		func() error {
			return client.CancelAddNode(hostname)
		})
}

func (c *CouchbaseClient) ClusterUUID(m *Member) (string, error) {
	ms := NewMemberSet(m)
	client := c.getClient(ms)

	var err error
	var uuid string
	err = retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "cluster uuid", c.clusterName,
		func() error {
			uuid, err = client.ClusterUUID()
			return err
		})

	return uuid, err
}

func (c *CouchbaseClient) GetClusterStatus(ms MemberSet) (*ClusterStatus, error) {
	client := c.getClient(ms)

	status := &ClusterStatus{
		ActiveNodes:      NewMemberSet(),
		PendingAddNodes:  NewMemberSet(),
		FailedAddNodes:   NewMemberSet(),
		DownNodes:        NewMemberSet(),
		FailedNodes:      NewMemberSet(),
		UnclusteredNodes: NewMemberSet(),
		UnmanagedNodes:   make([]string, 0),
	}
	err := retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "cluster status", c.clusterName, func() error {
		info, err := client.ClusterInfo()
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
				}
			} else if node.Status == "unhealthy" {
				if node.Membership == "active" {
					status.DownNodes.Add(member)
				} else if node.Membership == "inactiveAdded" {
					status.FailedAddNodes.Add(member)
				} else if node.Membership == "inactiveFailed" {
					status.FailedNodes.Add(member)
				}
			}
		}

		status.UnclusteredNodes = ms.Diff(managed)
		status.IsRebalancing = (info.RebalanceStatus == cbmgr.RebalanceStatusRunning)
		status.NeedsRebalance = !info.Balanced && len(info.Nodes) > 1
		return nil
	})

	return status, err
}

func (c *CouchbaseClient) InitializeCluster(m *Member, username, password, name string, dataMemQuota, indexMemQuota,
	searchMemQuota int, services []string, dataPath, indexPath, indexStorageMode string) error {
	ms := NewMemberSet(m)
	client := c.getClient(ms)

	err := retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "node init", name,
		func() error {
			return client.NodeInitialize(m.Addr(), dataPath, indexPath)
		})

	if err != nil {
		return err
	}

	svcs, err := cbmgr.ServiceListFromStringArray(services)
	if err != nil {
		return err
	}

	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "cluster init", name,
		func() error {
			return client.ClusterInitialize(username, password, name, dataMemQuota,
				indexMemQuota, searchMemQuota, 8091, svcs, cbmgr.IndexStorageMode(indexStorageMode))
		})
}

func (c *CouchbaseClient) Rebalance(ms MemberSet, nodesToRemove []string, wait bool) error {
	client := c.getClient(ms)
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "rebalance", c.clusterName,
		func() error {
			status, err := client.Rebalance(nodesToRemove)
			if wait && status != nil {
				status.SetLogger(retryutil.Log(c.clusterName))
				return status.Wait()
			}
			return err
		})
}

func (c *CouchbaseClient) CreateBucket(ms MemberSet, config *cbapi.BucketConfig) error {
	client := c.getClient(ms)

	bucket := ApiBucketToCbmgr(config)
	if err := client.CreateBucket(bucket); err != nil {
		return err
	}

	// make sure bucket exists
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 60, "create bucket", c.clusterName,
		func() error {
			_, err := client.BucketReady(bucket.BucketName)
			return err
		})
}

func (c *CouchbaseClient) DeleteBucket(ms MemberSet, bucketName string) error {
	client := c.getClient(ms)
	return client.DeleteBucket(bucketName)
}

func (c *CouchbaseClient) EditBucket(ms MemberSet, config *cbapi.BucketConfig) error {
	client := c.getClient(ms)
	bucket := ApiBucketToCbmgr(config)

	return client.EditBucket(bucket)
}

func (c *CouchbaseClient) GetBuckets(ms MemberSet) ([]*cbapi.BucketConfig, error) {
	client := c.getClient(ms)
	buckets, err := client.GetBuckets()
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

	client := c.getClient(ms)
	buckets, err := client.GetBuckets()
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

func (c *CouchbaseClient) SetAutoFailoverTimeout(ms MemberSet, enabled bool, timeout uint64) error {
	client := c.getClient(ms)
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 5, "set autofailover timeout", c.clusterName,
		func() error {
			return client.SetAutoFailoverTimeout(enabled, timeout)
		})
}

func (c *CouchbaseClient) GetAutoFailoverSettings(ms MemberSet) (*cbmgr.AutoFailoverSettings, error) {
	client := c.getClient(ms)
	var settings *cbmgr.AutoFailoverSettings

	return settings, retryutil.RetryOnErr(c.ctx, 5*time.Second, 10, "get autofailover settings", c.clusterName,
		func() (e error) {
			settings, e = client.GetAutoFailoverSettings()
			return e
		})
}

func (c *CouchbaseClient) GetClusterInfo(ms MemberSet) (*cbmgr.ClusterInfo, error) {
	client := c.getClient(ms)
	return client.ClusterInfo()
}

func (c *CouchbaseClient) SetPoolsDefault(ms MemberSet, dataServiceMemQuota, indexServiceMemQuota, searchServiceMemQuota int) error {
	client := c.getClient(ms)
	return client.SetPoolsDefault(c.clusterName, dataServiceMemQuota, indexServiceMemQuota, searchServiceMemQuota)
}

func (c *CouchbaseClient) SetDataMemoryQuota(ms MemberSet, quota int) error {
	client := c.getClient(ms)
	return client.SetDataMemoryQuota(quota)
}

func (c *CouchbaseClient) SetIndexMemoryQuota(ms MemberSet, quota int) error {
	client := c.getClient(ms)
	return client.SetIndexMemoryQuota(quota)
}

func (c *CouchbaseClient) SetSearchMemoryQuota(ms MemberSet, quota int) error {
	client := c.getClient(ms)
	return client.SetSearchMemoryQuota(quota)
}

func (c *CouchbaseClient) UploadClusterCACert(m *Member, pem []byte) error {
	ms := NewMemberSet(m)
	client := c.getClient(ms)
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "upload CA cert", c.clusterName,
		func() error {
			return client.UploadClusterCACert(pem)
		})
}

func (c *CouchbaseClient) ReloadNodeCert(m *Member) error {
	ms := NewMemberSet(m)
	client := c.getClient(ms)
	return retryutil.RetryOnErr(c.ctx, 5*time.Second, 36, "reload server cert", c.clusterName,
		func() error {
			return client.ReloadNodeCert()
		})
}

func (c *CouchbaseClient) SetClientCertAuth(m *Member, settings *cbmgr.ClientCertAuth) error {
	ms := NewMemberSet(m)
	client := c.getClient(ms)
	return client.SetClientCertAuth(settings)
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
