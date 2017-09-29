package couchbaseutil

import (
	"crypto/tls"
)

func CheckHealth(url string, tc *tls.Config) (bool, error) {
	// TODO - Add Couchbase API calls here to check the health of a particular
	// Couchbase node.
	return true, nil
}