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

	CreateScope(bucketName, scopeName string, portForward bool) error
	DeleteScope(bucketName, scopeName string, portForward bool) error

	CreateCollection(bucketName, scopeName, collectionName string, maxTTL int, history bool, portForward bool) error
	EditCollection(bucketName, scopeName, collectionName string, maxTTL int, history bool, portForward bool) error
	DeleteCollection(bucketName, scopeName, collectionName string, portForward bool) error
}

type Buckets struct {
	podName    string
	hostname   string
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
func NewBucketsAPI(podName, clusterName, username, password, secretName, namespace string,
	requestTimeout time.Duration, isSecure bool) (BucketsAPI, error) {
	if podName == "" {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrPodnameNotFound)
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

	hostname, err := requestutils.GetPodHostname(podName, clusterName, namespace)
	if err != nil {
		return nil, fmt.Errorf("new cluster nodes api: %w", err)
	}

	reqClient := requestutils.NewClient()
	reqClient.SetHTTPAuth(username, password)

	return &Buckets{
		podName:    podName,
		hostname:   hostname,
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

func (b *Buckets) CreateScope(bucketName, scopeName string, portForward bool) error {
	if bucketName == "" {
		return fmt.Errorf("create scope %s of bucket %s: %w", scopeName, bucketName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("create scope %s of bucket %s: %w", scopeName, bucketName, ErrScopeNameNotFound)
	}

	req := buckets.CreateScope(b.hostname, "", bucketName, scopeName)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: b.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := b.reqClient.Do(req, nil, b.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("create scope: %w", err)
	}

	return nil
}

func (b *Buckets) DeleteScope(bucketName, scopeName string, portForward bool) error {
	if bucketName == "" {
		return fmt.Errorf("delete scope %s of bucket %s: %w", scopeName, bucketName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("delete scope %s of bucket %s: %w", scopeName, bucketName, ErrScopeNameNotFound)
	}

	req := buckets.DropScope(b.hostname, "", bucketName, scopeName)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: b.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := b.reqClient.Do(req, nil, b.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("delete scope %s of bucket %s: %w", scopeName, bucketName, err)
	}

	return nil
}

func (b *Buckets) CreateCollection(bucketName, scopeName, collectionName string,
	maxTTL int, history bool, portForward bool) error {
	if bucketName == "" {
		return fmt.Errorf("create collection %s of scope %s: %w", collectionName, scopeName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("create collection %s of scope %s: %w", collectionName, scopeName, ErrScopeNameNotFound)
	}

	if collectionName == "" {
		return fmt.Errorf("create collection %s of scope %s: %w", collectionName, scopeName, ErrCollectionNameNotFound)
	}

	req := buckets.CreateCollection(b.hostname, "", bucketName, scopeName, collectionName, maxTTL, history)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: b.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := b.reqClient.Do(req, nil, b.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("create collection %s for scope %s of bucket %s: %w", collectionName, scopeName, bucketName, err)
	}

	return nil
}

func (b *Buckets) EditCollection(bucketName, scopeName, collectionName string,
	maxTTL int, history bool, portForward bool) error {
	if bucketName == "" {
		return fmt.Errorf("edit collection %s of scope %s: %w", collectionName, scopeName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("edit collection %s of scope %s: %w", collectionName, scopeName, ErrScopeNameNotFound)
	}

	if collectionName == "" {
		return fmt.Errorf("edit collection %s of scope %s: %w", collectionName, scopeName, ErrCollectionNameNotFound)
	}

	req := buckets.EditCollection(b.hostname, "", bucketName, scopeName, collectionName, maxTTL, history)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: b.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := b.reqClient.Do(req, nil, b.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("edit collection %s for scope %s of bucket %s: %w", collectionName, scopeName, bucketName, err)
	}

	return nil
}

func (b *Buckets) DeleteCollection(bucketName, scopeName, collectionName string, portForward bool) error {
	if bucketName == "" {
		return fmt.Errorf("delete collection %s of scope %s: %w", collectionName, scopeName, ErrBucketNameNotFound)
	}

	if scopeName == "" {
		return fmt.Errorf("delete collection %s of scope %s: %w", collectionName, scopeName, ErrScopeNameNotFound)
	}

	if collectionName == "" {
		return fmt.Errorf("delete collection %s of scope %s: %w", collectionName, scopeName, ErrCollectionNameNotFound)
	}

	req := buckets.DropCollection(b.hostname, "", bucketName, scopeName, collectionName)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: b.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := b.reqClient.Do(req, nil, b.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("delete collection %s for scope %s of bucket %s: %w", collectionName, scopeName, bucketName, err)
	}

	return nil
}
