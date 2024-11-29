package cbrestapi

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/buckets"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

var (
	ErrBucketNameNotFound     = errors.New("bucket name not found")
	ErrScopeNameNotFound      = errors.New("scope name not found")
	ErrCollectionNameNotFound = errors.New("collection name not found")
)

type BucketsAPI interface {
	// Scopes and Collections APIs.

	CreateScope(bucketName, scopeName string) error
	DeleteScope(bucketName, scopeName string) error

	CreateCollection(bucketName, scopeName, collectionName string, maxTTL int, history bool) error
	EditCollection(bucketName, scopeName, collectionName string, maxTTL int, history bool) error
	DeleteCollection(bucketName, scopeName, collectionName string) error
}

type Buckets struct {
	hostname   string
	port       int
	username   string
	password   string
	isSecure   bool // True for HTTPS request.
	reqTimeout time.Duration
	reqClient  *requestutils.Client
}

// NewBucketsAPI returns the BucketsAPI interface.
/*
 * If secretName is provided (along with namespace) then username and password will be taken from the K8S secret.
 */
func NewBucketsAPI(hostname string, port int, username, password, secretName, namespace string,
	requestTimeout time.Duration, isSecure bool) (BucketsAPI, error) {
	if hostname == "" {
		return nil, fmt.Errorf("new buckets api: %w", ErrHostnameNotFound)
	}

	if port <= 0 {
		return nil, fmt.Errorf("new buckets api: %w", ErrPortNotFound)
	}

	if secretName != "" {
		if namespace == "" {
			return nil, fmt.Errorf("new buckets api: %w", ErrNamespaceNotFound)
		}

		cbAuth, err := requestutils.GetCBClusterAuth(secretName, namespace)
		if err != nil {
			return nil, fmt.Errorf("new buckets api: %w", err)
		}

		username = cbAuth.Username
		password = cbAuth.Password
	}

	if username == "" {
		return nil, fmt.Errorf("new buckets api: %w", ErrUsernameNotFound)
	}

	if password == "" {
		return nil, fmt.Errorf("new buckets api: %w", ErrPasswordNotFound)
	}

	if requestTimeout <= 0 {
		return nil, fmt.Errorf("new buckets api: %w", ErrRequestTimeout)
	}

	// Checking the hostname
	// TODO split the hostname and port.
	// TODO update GetHTTPHostname to have isSecure parameter to support https.
	updatedHostname, err := requestutils.GetHTTPHostname(hostname, int64(port))
	if err != nil {
		return nil, fmt.Errorf("new buckets api: %w", err)
	}

	reqClient := requestutils.NewClient()
	reqClient.SetHTTPAuth(username, password)

	return &Buckets{
		hostname:   updatedHostname,
		port:       port,
		username:   username,
		password:   password,
		isSecure:   isSecure,
		reqTimeout: requestTimeout,
		reqClient:  reqClient,
	}, nil
}

// =======================================================================================
// =========================== Scopes and Collections APIs ===============================
// =======================================================================================

func (b *Buckets) CreateScope(bucketName, scopeName string) error {
	if bucketName == "" {
		return fmt.Errorf("create scope %s of bucket %s: %w", scopeName, bucketName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("create scope %s of bucket %s: %w", scopeName, bucketName, ErrScopeNameNotFound)
	}

	err := b.reqClient.Do(buckets.CreateScope(b.hostname, bucketName, scopeName), nil, b.reqTimeout)
	if err != nil {
		return fmt.Errorf("create scope: %w", err)
	}

	return nil
}

func (b *Buckets) DeleteScope(bucketName, scopeName string) error {
	if bucketName == "" {
		return fmt.Errorf("delete scope %s of bucket %s: %w", scopeName, bucketName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("delete scope %s of bucket %s: %w", scopeName, bucketName, ErrScopeNameNotFound)
	}

	err := b.reqClient.Do(buckets.DropScope(b.hostname, bucketName, scopeName), nil, b.reqTimeout)
	if err != nil {
		return fmt.Errorf("delete scope %s of bucket %s: %w", scopeName, bucketName, err)
	}

	return nil
}

func (b *Buckets) CreateCollection(bucketName, scopeName, collectionName string, maxTTL int, history bool) error {
	if bucketName == "" {
		return fmt.Errorf("create collection %s of scope %s: %w", collectionName, scopeName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("create collection %s of scope %s: %w", collectionName, scopeName, ErrScopeNameNotFound)
	}

	if collectionName == "" {
		return fmt.Errorf("create collection %s of scope %s: %w", collectionName, scopeName, ErrCollectionNameNotFound)
	}

	err := b.reqClient.Do(buckets.CreateCollection(b.hostname, bucketName, scopeName, collectionName, maxTTL, history), nil, b.reqTimeout)
	if err != nil {
		return fmt.Errorf("create collection %s for scope %s of bucket %s: %w", collectionName, scopeName, bucketName, err)
	}

	return nil
}

func (b *Buckets) EditCollection(bucketName, scopeName, collectionName string, maxTTL int, history bool) error {
	if bucketName == "" {
		return fmt.Errorf("edit collection %s of scope %s: %w", collectionName, scopeName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("edit collection %s of scope %s: %w", collectionName, scopeName, ErrScopeNameNotFound)
	}

	if collectionName == "" {
		return fmt.Errorf("edit collection %s of scope %s: %w", collectionName, scopeName, ErrCollectionNameNotFound)
	}

	err := b.reqClient.Do(buckets.EditCollection(b.hostname, bucketName, scopeName, collectionName, maxTTL, history), nil, b.reqTimeout)
	if err != nil {
		return fmt.Errorf("edit collection %s for scope %s of bucket %s: %w", collectionName, scopeName, bucketName, err)
	}

	return nil
}

func (b *Buckets) DeleteCollection(bucketName, scopeName, collectionName string) error {
	if bucketName == "" {
		return fmt.Errorf("delete collection %s of scope %s: %w", collectionName, scopeName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("delete collection %s of scope %s: %w", collectionName, scopeName, ErrScopeNameNotFound)
	}

	if collectionName == "" {
		return fmt.Errorf("delete collection %s of scope %s: %w", collectionName, scopeName, ErrCollectionNameNotFound)
	}

	err := b.reqClient.Do(buckets.DropCollection(b.hostname, bucketName, scopeName, collectionName), nil, b.reqTimeout)
	if err != nil {
		return fmt.Errorf("delete collection %s for scope %s of bucket %s: %w", collectionName, scopeName, bucketName, err)
	}

	return nil
}
