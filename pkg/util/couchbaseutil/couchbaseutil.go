package couchbaseutil

import (
	"crypto/tls"
	"encoding/json"
	"time"

	cbapi "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbaselabs/gocbmgr"
)

var (
	RestTimeout = 30 * time.Second
)

// New client for managing couchbase node at <url>
func NewClient(url, username, password string) (*cbmgr.Couchbase, error) {
	client, err := cbmgr.New(url)
	if err != nil {
		return nil, err
	}
	client.Username = username
	client.Password = password
	return client, nil
}

// New client associated with any member within a set
func NewClientForMemberSet(ms MemberSet, username, password string) (*cbmgr.Couchbase, error) {
	m := ms.PickOne()
	return NewClient(m.ClientURL(), username, password)
}

// Create a client that ensures url is servicable
func NewReadyClient(url, username, password string) (*cbmgr.Couchbase, error) {

	// create new client
	couchbaseClient, err := NewClient(url, username, password)

	// make sure node is ready
	_, err = couchbaseClient.IsReady(url, RestTimeout)
	if err != nil {
		return nil, err
	}

	return couchbaseClient, nil
}

// check the health of a particular Couchbase node.
func CheckHealth(url string, tc *tls.Config) (bool, error) {
	// TODO: check the health of a particular Couchbase node.
	return true, nil
}

func AddNode(m *Member, clusterName, hostname, username, password string, services []string) error {
	client, err := cbmgr.New(m.ClientURL())
	if err != nil {
		return err
	}

	client.Username = username
	client.Password = password
	svcs, err := cbmgr.ServiceListFromStringArray(services)
	if err != nil {
		return err
	}

	return retryutil.RetryOnErr(5*time.Second, 36, func() (bool, error) {
		err := client.AddNode(hostname, username, password, svcs)
		return true, err
	}, "add node", clusterName)
}

func InitializeCluster(m *Member, username, password, name string, dataMemQuota, indexMemQuota,
	searchMemQuota int, services []string, dataPath, indexPath, indexStorageMode string) error {
	client, err := cbmgr.New(m.ClientURL())
	if err != nil {
		return err
	}

	err = retryutil.RetryOnErr(5*time.Second, 36, func() (bool, error) {
		err := client.NodeInitialize(m.Addr(), dataPath, indexPath)
		return true, err
	}, "node init", name)

	if err != nil {
		return err
	}

	svcs, err := cbmgr.ServiceListFromStringArray(services)
	if err != nil {
		return err
	}

	return retryutil.RetryOnErr(5*time.Second, 36, func() (bool, error) {
		err := client.ClusterInitialize(username, password, name, dataMemQuota,
			indexMemQuota, searchMemQuota, 8091, svcs, cbmgr.IndexStorageMode(indexStorageMode))
		return true, err
	}, "cluster init", name)
}

func Rebalance(m *Member, username, password, clusterName string, nodesToRemove []string, wait bool) error {
	client, err := cbmgr.New(m.ClientURL())
	if err != nil {
		return err
	}

	client.Username = username
	client.Password = password
	return retryutil.RetryOnErr(5*time.Second, 36, func() (bool, error) {
		status, err := client.Rebalance(nodesToRemove)
		if wait {
			status.SetLogger(retryutil.Log(clusterName))
			return true, status.Wait()
		}
		return true, err
	}, "rebalance", clusterName)
}

func CreateBucket(m *Member, username, password string, config *cbapi.BucketConfig) error {
	client, err := cbmgr.New(m.ClientURL())
	if err != nil {
		return err
	}

	client.Username = username
	client.Password = password

	// convert bucket config to cbmgr bucket type
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	bucket := cbmgr.Bucket{}
	if err := json.Unmarshal(data, &bucket); err != nil {
		return err
	}

	if err = client.CreateBucket(&bucket); err != nil {
		return err
	}

	// make sure bucket exists
	return retryutil.Retry(5*time.Second, 60,
		func() (bool, error) {
			return client.BucketReady(bucket.BucketName)
		})
}

func DeleteBucket(m *Member, username, password, bucketName string) error {

	client, err := cbmgr.New(m.ClientURL())
	if err != nil {
		return err
	}
	client.Username = username
	client.Password = password

	return client.DeleteBucket(bucketName)
}

func GetBucketNames(m *Member, username, password string) ([]string, error) {

	bucketNames := []string{}
	client, err := cbmgr.New(m.ClientURL())
	if err != nil {
		return bucketNames, err
	}
	client.Username = username
	client.Password = password

	buckets, err := client.GetBuckets()
	if err != nil {
		return bucketNames, err
	}

	for _, b := range buckets {
		bucketNames = append(bucketNames, b.BucketName)
	}

	return bucketNames, nil
}
