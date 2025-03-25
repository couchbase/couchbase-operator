package couchbasememcachedbucketresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseMemcachedBucketResourceNameNotProvided = errors.New("couchbase memcached bucket resource name is not provided")
	ErrNamespaceNotProvided                            = errors.New("namespace is not provided")
	ErrCouchbaseMemcachedBucketResourceDoesNotExist    = errors.New("couchbase memcached bucket resource does not exist")
	ErrNoCouchbaseMemcachedBucketResourcesInNamespace  = errors.New("no couchbase memcached bucket resources in namespace")
)

// GetCouchbaseMemcachedBucketResourceNames returns a slice of strings containing names of all couchbase memcached bucket resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseMemcachedBucketResourcesInNamespace.
func GetCouchbaseMemcachedBucketResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase memcached bucket resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseMemcachedBucketResourceNamesOutput, err := kubectl.Get("CouchbaseMemcachedBucket").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase memcached bucket resource names: %w", err)
	}

	if CouchbaseMemcachedBucketResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase memcached bucket resource names: %w", ErrNoCouchbaseMemcachedBucketResourcesInNamespace)
	}

	CouchbaseMemcachedBucketResourceNames := strings.Split(CouchbaseMemcachedBucketResourceNamesOutput, "\n")
	for i := range CouchbaseMemcachedBucketResourceNames {
		// kubectl returns couchbase memcached bucket resource names as couchbasememcachedbuckets.couchbase.com/memcached-bucket-name. We remove the prefix "couchbasememcachedbuckets.couchbase.com/"
		CouchbaseMemcachedBucketResourceNames[i] = strings.TrimPrefix(CouchbaseMemcachedBucketResourceNames[i], "couchbasememcachedbuckets.couchbase.com/")
	}

	return CouchbaseMemcachedBucketResourceNames, nil
}

// GetCouchbaseMemcachedBucket gets the couchbase memcached bucket resource information of the couchbase memcached bucket resource in the given namespace and returns *CouchbaseMemcachedBucketResource.
// Defined errors returned: ErrCouchbaseMemcachedBucketResourceDoesNotExist.
func GetCouchbaseMemcachedBucket(CouchbaseMemcachedBucketResourceName string, namespace string) (*CouchbaseMemcachedBucketResource, error) {
	if CouchbaseMemcachedBucketResourceName == "" {
		return nil, fmt.Errorf("get couchbase memcached bucket: %w", ErrCouchbaseMemcachedBucketResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase memcached bucket: %w", ErrNamespaceNotProvided)
	}

	CouchbaseMemcachedBucketResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseMemcachedBucket", CouchbaseMemcachedBucketResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasememcachedbuckets.couchbase.com \"%s\" not found", CouchbaseMemcachedBucketResourceName) {
			return nil, fmt.Errorf("get couchbase memcached bucket: %w", ErrCouchbaseMemcachedBucketResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase memcached bucket: %w", err)
	}

	var CouchbaseMemcachedBucketResource CouchbaseMemcachedBucketResource

	err = json.Unmarshal([]byte(CouchbaseMemcachedBucketResourceJSON), &CouchbaseMemcachedBucketResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase memcached bucket: %w", err)
	}

	return &CouchbaseMemcachedBucketResource, nil
}

// GetCouchbaseMemcachedBucketResources gets the couchbase memcached bucket resource information and returns the *CouchbaseMemcachedBucketResourceList containing the list of CouchbaseMemcachedBucketResources.
// If CouchbaseMemcachedBucketResourceNames = nil, then all the CouchbaseMemcachedBucketResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseMemcachedBucketResourceNamesDoesNotExist.
func GetCouchbaseMemcachedBucketResources(CouchbaseMemcachedBucketResourceNames []string, namespace string) (*CouchbaseMemcachedBucketResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase memcached bucket resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseMemcachedBucketResourceNames == nil {
		CouchbaseMemcachedBucketResourceNamesList, err := GetCouchbaseMemcachedBucketResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase memcached bucket resources: %w", err)
		}

		CouchbaseMemcachedBucketResourceNames = CouchbaseMemcachedBucketResourceNamesList
	}

	var CouchbaseMemcachedBucketResourceList CouchbaseMemcachedBucketResourceList

	// When we execute `get CouchbaseMemcachedBucket <single-memcached-bucket>`, then we receive a single couchbaseMemcachedBucket JSON instead of list of couchbaseMemcachedBucket JSONs.
	if len(CouchbaseMemcachedBucketResourceNames) == 1 {
		CouchbaseMemcachedBucketResource, err := GetCouchbaseMemcachedBucket(CouchbaseMemcachedBucketResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase memcached bucket resources: %w", err)
		}

		CouchbaseMemcachedBucketResourceList.CouchbaseMemcachedBucketResources = append(CouchbaseMemcachedBucketResourceList.CouchbaseMemcachedBucketResources, CouchbaseMemcachedBucketResource)

		return &CouchbaseMemcachedBucketResourceList, nil
	}

	CouchbaseMemcachedBucketResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseMemcachedBucket", CouchbaseMemcachedBucketResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase memcached bucket resources: %w", ErrCouchbaseMemcachedBucketResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase memcached bucket resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseMemcachedBucketResourcesJSON), &CouchbaseMemcachedBucketResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase memcached bucket resources: json unmarshal: %w", err)
	}

	return &CouchbaseMemcachedBucketResourceList, nil
}

// GetCouchbaseMemcachedBucketsMap gets the couchbase memcached bucket resource information and returns the map[string]*CouchbaseMemcachedBucketResource which has the *CouchbaseMemcachedBucketResource for each couchbase memcached bucket resource names in given list.
// If CouchbaseMemcachedBucketResourceNames = nil, then all the couchbase memcached bucket resources in the namespace are taken into account.
func GetCouchbaseMemcachedBucketResourcesMap(CouchbaseMemcachedBucketResourceNames []string, namespace string) (map[string]*CouchbaseMemcachedBucketResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase memcached bucket resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseMemcachedBucketResourceNames == nil {
		CouchbaseMemcachedBucketResourceNamesList, err := GetCouchbaseMemcachedBucketResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase memcached bucket resources map: %w", err)
		}

		CouchbaseMemcachedBucketResourceNames = CouchbaseMemcachedBucketResourceNamesList
	}

	CouchbaseMemcachedBucketResourceMap := make(map[string]*CouchbaseMemcachedBucketResource)

	CouchbaseMemcachedBucketResourcesList, err := GetCouchbaseMemcachedBucketResources(CouchbaseMemcachedBucketResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase memcached bucket resources map: %w", err)
	}

	for i := range CouchbaseMemcachedBucketResourcesList.CouchbaseMemcachedBucketResources {
		CouchbaseMemcachedBucketResourceMap[CouchbaseMemcachedBucketResourceNames[i]] = CouchbaseMemcachedBucketResourcesList.CouchbaseMemcachedBucketResources[i]
	}

	return CouchbaseMemcachedBucketResourceMap, nil
}
