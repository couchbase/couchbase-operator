package couchbaseutil

import (
	"crypto/tls"
	"github.com/couchbaselabs/gocbmgr"
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

// New client associated with member
func NewClientForMember(m *Member, username, password string) (*cbmgr.Couchbase, error) {
	memberUrl := m.ClientURL()
	return NewClient(memberUrl, username, password)
}

// New client associated with any member within a set
func NewClientForMemberSet(ms MemberSet, username, password string) (*cbmgr.Couchbase, error) {
	m := ms.PickOne()
	return NewClientForMember(m, username, password)
}

// check the health of a particular Couchbase node.
func CheckHealth(url string, tc *tls.Config) (bool, error) {
	// TODO: check the health of a particular Couchbase node.
	return true, nil
}
