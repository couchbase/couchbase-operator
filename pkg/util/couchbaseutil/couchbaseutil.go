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
	TotalNodes        int
	NumHealthyNodes   int
	NumUnhealthyNodes int
	NumWarmupNodes    int
	AutoFailedNodes   MemberSet
	IsRebalancing     bool
	NeedsRebalance    bool
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

		status.TotalNodes = 0
		status.NumHealthyNodes = 0
		status.NumUnhealthyNodes = 0
		status.NumWarmupNodes = 0
		status.AutoFailedNodes = NewMemberSet()
		status.IsRebalancing = (info.RebalanceStatus == cbmgr.RebalanceStatusRunning)
		status.NeedsRebalance = !info.Balanced

		for _, node := range info.Nodes {
			status.TotalNodes++
			if node.Status == "healthy" {
				status.NumHealthyNodes++
			} else if node.Status == "warmup" {
				status.NumWarmupNodes++
			} else if node.Status == "unhealthy" {
				status.NumUnhealthyNodes++
				if node.Membership == "inactiveFailed" {
					host := strings.Split(node.HostName, ".")[0]
					m := ms[host]
					status.AutoFailedNodes.Add(m)
				}
			}
		}

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

func CreateBucket(ms MemberSet, username, password string, config *cbapi.BucketConfig) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)

	bucket, err := apiBucketToCbmgr(config)
	if err != nil {
		return err
	}

	if err = client.CreateBucket(bucket); err != nil {
		return err
	}

	// make sure bucket exists
	return retryutil.Retry(5*time.Second, 60,
		func() (bool, error) {
			return client.BucketReady(bucket.BucketName)
		})
}

func DeleteBucket(ms MemberSet, username, password, bucketName string) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return client.DeleteBucket(bucketName)
}

func EditBucket(ms MemberSet, username, password string, config *cbapi.BucketConfig) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	bucket, err := apiBucketToCbmgr(config)
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
			if !bucketStatusEqualsConfig(b, config) {
				bucketNames = append(bucketNames, config.BucketName)
			}
		}
	}

	return bucketNames, nil
}

func SetAutoFailoverTimeout(ms MemberSet, username, password, clusterName string, enabled bool, timeout uint64) error {
	client := cbmgr.New(ms.ClientURLs(), username, password)
	return retryutil.RetryOnErr(5*time.Second, 36, "set autofailover timeout", clusterName,
		func() error {
			return client.SetAutoFailoverTimeout(enabled, timeout)
		})
}

func apiBucketToCbmgr(config *cbapi.BucketConfig) (*cbmgr.Bucket, error) {

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
func bucketStatusEqualsConfig(statusConfig *cbmgr.Bucket, specConfig *cbapi.BucketConfig) bool {

	// consider type couchbase = membase
	if specConfig.BucketType == "couchbase" && statusConfig.BucketType == "membase" {
		statusConfig.BucketType = "couchbase"
	}
	// TODO: status doesn't seem to return conflict resolution
	//statusConfig.ConflictResolution = specConfig.ConflictResolution

	return reflect.DeepEqual(statusConfig, specConfig)
}
