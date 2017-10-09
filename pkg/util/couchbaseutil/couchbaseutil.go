package couchbaseutil

import (
	"crypto/tls"
	"time"

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

func InitializeCluster(m *Member, username, password, name string, dataMemQuota, indexMemQuota,
	searchMemQuota int, services []string, indexStorageMode string) error {
	client, err := cbmgr.New(m.ClientURL())
	if err != nil {
		return err
	}

	svcs, err := cbmgr.ServiceListFromStringArray(services)
	if err != nil {
		return err
	}

	return retryutil.Retry(5 *time.Second, 36, func() (bool, error) {
		err := client.ClusterInitialize(username, password, name, dataMemQuota,
			indexMemQuota, searchMemQuota, 8091, svcs, cbmgr.IndexStorageMode(indexStorageMode))

		if err == nil {
			return true, nil
		}

		return false, nil
	})
}
