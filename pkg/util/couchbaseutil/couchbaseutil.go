package couchbaseutil

import (
	"crypto/tls"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	cbapi "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbaselabs/gocbmgr"
)

type ClusterStatus struct {
	ActiveNodes     MemberSet // status=healthy,   clusterMembership=active
	PendingAddNodes MemberSet // status=healthy,   clusterMembership=inactiveAdded
	FailedAddNodes  MemberSet // status=unhealthy, clusterMembership=inactiveAdded
	DownNodes       MemberSet // status=unhealthy, clusterMembership=active
	FailedNodes     MemberSet // status=unhealthy, clusterMembership=inactiveFailed
	UnknownNodes    MemberSet // not part of the cluster
	IsRebalancing   bool
	NeedsRebalance  bool
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

	status := &ClusterStatus{}
	err := retryutil.RetryOnErr(5*time.Second, 36, "cluster status", clusterName, func() error {
		info, err := client.ClusterInfo()
		if err != nil {
			return err
		}

		status.ActiveNodes = NewMemberSet()
		status.PendingAddNodes = NewMemberSet()
		status.FailedAddNodes = NewMemberSet()
		status.DownNodes = NewMemberSet()
		status.FailedNodes = NewMemberSet()
		status.IsRebalancing = (info.RebalanceStatus == cbmgr.RebalanceStatusRunning)
		status.NeedsRebalance = !info.Balanced

		all := NewMemberSet()
		for _, node := range info.Nodes {
			member := ms[strings.Split(node.HostName, ".")[0]]
			if member == nil {
				continue
			}
			all.Add(member)
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

		status.UnknownNodes = ms.Diff(all)
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
			if wait {
				status.SetLogger(retryutil.Log(clusterName))
				return status.Wait()
			}
			return err
		})
}

func CreateBucket(ms MemberSet, clusterName, username, password string, config *cbapi.BucketConfig) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)

	bucket, err := ApiBucketToCbmgr(config)
	if err != nil {
		return err
	}

	if err = client.CreateBucket(bucket); err != nil {
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
	bucket, err := ApiBucketToCbmgr(config)
	if err != nil {
		return err
	}

	return client.EditBucket(bucket)
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
	client := cbmgr.New(ms.ClientURLs(), username, password)

	buckets, err := client.GetBuckets()
	if err != nil {
		return bucketNames, err
	}
	for _, b := range buckets {
		config := spec.GetBucketByName(b.BucketName)
		if config != nil {

			cbConfigBucket, err := ApiBucketToCbmgr(config)
			if err != nil {
				return bucketNames, err
			}
			if !bucketStatusEqualsConfig(b, cbConfigBucket) {
				bucketNames = append(bucketNames, config.BucketName)
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

func ApiBucketToCbmgr(config *cbapi.BucketConfig) (*cbmgr.Bucket, error) {

	// convert bucket config to cbmgr bucket type
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	bucket := cbmgr.Bucket{}
	if err := json.Unmarshal(data, &bucket); err != nil {
		return nil, err
	}

	return &bucket, nil
}

// transforms bucket status into bucketConfig type and compares the two
func bucketStatusEqualsConfig(statusConfig *cbmgr.Bucket, specConfig *cbmgr.Bucket) bool {

	// consider type couchbase = membase
	if specConfig.BucketType == "couchbase" && statusConfig.BucketType == "membase" {
		statusConfig.BucketType = "couchbase"
	}

	return reflect.DeepEqual(statusConfig, specConfig)
}
