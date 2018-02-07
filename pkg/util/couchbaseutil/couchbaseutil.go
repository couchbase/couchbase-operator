package couchbaseutil

import (
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

// check the health of a particular Couchbase node.
func CheckHealth(url string, tc *tls.Config) (bool, error) {
	// TODO: check the health of a particular Couchbase node.
	return true, nil
}

func AddNode(ms MemberSet, clusterName, hostname, username, password string, services []string) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	svcs, err := cbmgr.ServiceListFromStringArray(services)
	if err != nil {
		return err
	}

	return retryutil.RetryOnErr(5*time.Second, 36, "add node", clusterName,
		func() error {
			return client.AddNode(hostname, username, password, svcs)
		})
}

func CancelAddNode(ms MemberSet, clusterName, hostname, username, password string) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return retryutil.RetryOnErr(5*time.Second, 36, "add node", clusterName,
		func() error {
			return client.CancelAddNode(hostname)
		})
}

func ClusterUUID(m *Member, username, password, clusterName string) (string, error) {
	client := cbmgr.New([]string{m.ClientURL()}, username, password)

	var err error
	var uuid string
	err = retryutil.RetryOnErr(5*time.Second, 36, "cluster uuid", clusterName,
		func() error {
			uuid, err = client.ClusterUUID()
			return err
		})

	return uuid, err
}

func GetClusterStatus(ms MemberSet, username, password, clusterName string) (*ClusterStatus, error) {
	client := cbmgr.New(ms.ClientURLs(), username, password)

	status := &ClusterStatus{
		ActiveNodes:      NewMemberSet(),
		PendingAddNodes:  NewMemberSet(),
		FailedAddNodes:   NewMemberSet(),
		DownNodes:        NewMemberSet(),
		FailedNodes:      NewMemberSet(),
		UnclusteredNodes: NewMemberSet(),
		UnmanagedNodes:   make([]string, 0),
	}
	err := retryutil.RetryOnErr(5*time.Second, 36, "cluster status", clusterName, func() error {
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

func InitializeCluster(m *Member, username, password, name string, dataMemQuota, indexMemQuota,
	searchMemQuota int, services []string, dataPath, indexPath, indexStorageMode string) error {
	client := cbmgr.New([]string{m.ClientURL()}, "", "")

	err := retryutil.RetryOnErr(5*time.Second, 36, "node init", name,
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

	return retryutil.RetryOnErr(5*time.Second, 36, "cluster init", name,
		func() error {
			return client.ClusterInitialize(username, password, name, dataMemQuota,
				indexMemQuota, searchMemQuota, 8091, svcs, cbmgr.IndexStorageMode(indexStorageMode))
		})
}

func Rebalance(ms MemberSet, username, password, clusterName string, nodesToRemove []string, wait bool) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return retryutil.RetryOnErr(5*time.Second, 36, "rebalance", clusterName,
		func() error {
			status, err := client.Rebalance(nodesToRemove)
			if wait && status != nil {
				status.SetLogger(retryutil.Log(clusterName))
				return status.Wait()
			}
			return err
		})
}

func CreateBucket(ms MemberSet, clusterName, username, password string, config *cbapi.BucketConfig) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)

	bucket := ApiBucketToCbmgr(config)
	if err := client.CreateBucket(bucket); err != nil {
		return err
	}

	// make sure bucket exists
	return retryutil.RetryOnErr(5*time.Second, 60, "create bucket", clusterName,
		func() error {
			_, err := client.BucketReady(bucket.BucketName)
			return err
		})
}

func DeleteBucket(ms MemberSet, username, password, bucketName string) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return client.DeleteBucket(bucketName)
}

func EditBucket(ms MemberSet, username, password string, config *cbapi.BucketConfig) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	bucket := ApiBucketToCbmgr(config)

	return client.EditBucket(bucket)
}

func GetBuckets(ms MemberSet, username, password string) ([]*cbapi.BucketConfig, error) {
	client := cbmgr.New(ms.ClientURLs(), username, password)
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

func GetBucketNames(ms MemberSet, username, password string) ([]string, error) {

	bucketNames := []string{}

	client := cbmgr.New(ms.ClientURLs(), username, password)
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
func GetBucketsToEdit(ms MemberSet, username, password string, spec *cbapi.ClusterSpec) ([]string, error) {
	bucketNames := []string{}
	clusterBuckets, err := GetBuckets(ms, username, password)
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

func SetAutoFailoverTimeout(ms MemberSet, username, password, clusterName string, enabled bool, timeout uint64) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return retryutil.RetryOnErr(5*time.Second, 5, "set autofailover timeout", clusterName,
		func() error {
			return client.SetAutoFailoverTimeout(enabled, timeout)
		})
}

func GetAutoFailoverSettings(ms MemberSet, username, password, clusterName string) (*cbmgr.AutoFailoverSettings, error) {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	var settings *cbmgr.AutoFailoverSettings

	return settings, retryutil.RetryOnErr(5*time.Second, 10, "get autofailover settings", clusterName,
		func() (e error) {
			settings, e = client.GetAutoFailoverSettings()
			return e
		})
}

func GetClusterInfo(ms MemberSet, username, password string) (*cbmgr.ClusterInfo, error) {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return client.ClusterInfo()
}

func SetPoolsDefault(ms MemberSet, username, password, clusterName string, dataServiceMemQuota, indexServiceMemQuota, searchServiceMemQuota int) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return client.SetPoolsDefault(clusterName, dataServiceMemQuota, indexServiceMemQuota, searchServiceMemQuota)
}

func SetDataMemoryQuota(ms MemberSet, username, password string, quota int) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return client.SetDataMemoryQuota(quota)
}

func SetIndexMemoryQuota(ms MemberSet, username, password string, quota int) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return client.SetIndexMemoryQuota(quota)
}

func SetSearchMemoryQuota(ms MemberSet, username, password string, quota int) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return client.SetSearchMemoryQuota(quota)
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
