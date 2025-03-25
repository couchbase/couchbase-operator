package couchbaseephemeralbucketresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseEphemeralBucketResourceNameNotProvided = errors.New("couchbase ephemeral bucket resource name is not provided")
	ErrNamespaceNotProvided                            = errors.New("namespace is not provided")
	ErrCouchbaseEphemeralBucketResourceDoesNotExist    = errors.New("couchbase ephemeral bucket resource does not exist")
	ErrNoCouchbaseEphemeralBucketResourcesInNamespace  = errors.New("no couchbase ephemeral bucket resources in namespace")
)

// GetCouchbaseEphemeralBucketResourceNames returns a slice of strings containing names of all couchbase ephemeral bucket resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseEphemeralBucketResourcesInNamespace.
func GetCouchbaseEphemeralBucketResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase ephemeral bucket resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseEphemeralBucketResourceNamesOutput, err := kubectl.Get("CouchbaseEphemeralBucket").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase ephemeral bucket resource names: %w", err)
	}

	if CouchbaseEphemeralBucketResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase ephemeral bucket resource names: %w", ErrNoCouchbaseEphemeralBucketResourcesInNamespace)
	}

	CouchbaseEphemeralBucketResourceNames := strings.Split(CouchbaseEphemeralBucketResourceNamesOutput, "\n")
	for i := range CouchbaseEphemeralBucketResourceNames {
		// kubectl returns couchbase ephemeral bucket resource names as couchbaseephemeralbuckets.couchbase.com/ephemeral-bucket-name. We remove the prefix "couchbaseephemeralbuckets.couchbase.com/"
		CouchbaseEphemeralBucketResourceNames[i] = strings.TrimPrefix(CouchbaseEphemeralBucketResourceNames[i], "couchbaseephemeralbuckets.couchbase.com/")
	}

	return CouchbaseEphemeralBucketResourceNames, nil
}

// GetCouchbaseEphemeralBucket gets the couchbase ephemeral bucket resource information of the couchbase ephemeral bucket resource in the given namespace and returns *CouchbaseEphemeralBucketResource.
// Defined errors returned: ErrCouchbaseEphemeralBucketResourceDoesNotExist.
func GetCouchbaseEphemeralBucket(CouchbaseEphemeralBucketResourceName string, namespace string) (*CouchbaseEphemeralBucketResource, error) {
	if CouchbaseEphemeralBucketResourceName == "" {
		return nil, fmt.Errorf("get couchbase ephemeral bucket: %w", ErrCouchbaseEphemeralBucketResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase ephemeral bucket: %w", ErrNamespaceNotProvided)
	}

	CouchbaseEphemeralBucketResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseEphemeralBucket", CouchbaseEphemeralBucketResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbaseephemeralbuckets.couchbase.com \"%s\" not found", CouchbaseEphemeralBucketResourceName) {
			return nil, fmt.Errorf("get couchbase ephemeral bucket: %w", ErrCouchbaseEphemeralBucketResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase ephemeral bucket: %w", err)
	}

	var CouchbaseEphemeralBucketResource CouchbaseEphemeralBucketResource

	err = json.Unmarshal([]byte(CouchbaseEphemeralBucketResourceJSON), &CouchbaseEphemeralBucketResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase ephemeral bucket: %w", err)
	}

	return &CouchbaseEphemeralBucketResource, nil
}

// GetCouchbaseEphemeralBucketResources gets the couchbase ephemeral bucket resource information and returns the *CouchbaseEphemeralBucketResourceList containing the list of CouchbaseEphemeralBucketResources.
// If CouchbaseEphemeralBucketResourceNames = nil, then all the CouchbaseEphemeralBucketResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseEphemeralBucketResourceNamesDoesNotExist.
func GetCouchbaseEphemeralBucketResources(CouchbaseEphemeralBucketResourceNames []string, namespace string) (*CouchbaseEphemeralBucketResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase ephemeral bucket resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseEphemeralBucketResourceNames == nil {
		CouchbaseEphemeralBucketResourceNamesList, err := GetCouchbaseEphemeralBucketResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase ephemeral bucket resources: %w", err)
		}

		CouchbaseEphemeralBucketResourceNames = CouchbaseEphemeralBucketResourceNamesList
	}

	var CouchbaseEphemeralBucketResourceList CouchbaseEphemeralBucketResourceList

	// When we execute `get CouchbaseEphemeralBucket <single-ephemeral-bucket>`, then we receive a single couchbaseEphemeralBucket JSON instead of list of couchbaseEphemeralBucket JSONs.
	if len(CouchbaseEphemeralBucketResourceNames) == 1 {
		CouchbaseEphemeralBucketResource, err := GetCouchbaseEphemeralBucket(CouchbaseEphemeralBucketResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase ephemeral bucket resources: %w", err)
		}

		CouchbaseEphemeralBucketResourceList.CouchbaseEphemeralBucketResources = append(CouchbaseEphemeralBucketResourceList.CouchbaseEphemeralBucketResources, CouchbaseEphemeralBucketResource)

		return &CouchbaseEphemeralBucketResourceList, nil
	}

	CouchbaseEphemeralBucketResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseEphemeralBucket", CouchbaseEphemeralBucketResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase ephemeral bucket resources: %w", ErrCouchbaseEphemeralBucketResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase ephemeral bucket resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseEphemeralBucketResourcesJSON), &CouchbaseEphemeralBucketResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase ephemeral bucket resources: json unmarshal: %w", err)
	}

	return &CouchbaseEphemeralBucketResourceList, nil
}

// GetCouchbaseEphemeralBucketsMap gets the couchbase ephemeral bucket resource information and returns the map[string]*CouchbaseEphemeralBucketResource which has the *CouchbaseEphemeralBucketResource for each couchbase ephemeral bucket resource names in given list.
// If CouchbaseEphemeralBucketResourceNames = nil, then all the couchbase ephemeral bucket resources in the namespace are taken into account.
func GetCouchbaseEphemeralBucketResourcesMap(CouchbaseEphemeralBucketResourceNames []string, namespace string) (map[string]*CouchbaseEphemeralBucketResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase ephemeral bucket resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseEphemeralBucketResourceNames == nil {
		CouchbaseEphemeralBucketResourceNamesList, err := GetCouchbaseEphemeralBucketResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase ephemeral bucket resources map: %w", err)
		}

		CouchbaseEphemeralBucketResourceNames = CouchbaseEphemeralBucketResourceNamesList
	}

	CouchbaseEphemeralBucketResourceMap := make(map[string]*CouchbaseEphemeralBucketResource)

	CouchbaseEphemeralBucketResourcesList, err := GetCouchbaseEphemeralBucketResources(CouchbaseEphemeralBucketResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase ephemeral bucket resources map: %w", err)
	}

	for i := range CouchbaseEphemeralBucketResourcesList.CouchbaseEphemeralBucketResources {
		CouchbaseEphemeralBucketResourceMap[CouchbaseEphemeralBucketResourceNames[i]] = CouchbaseEphemeralBucketResourcesList.CouchbaseEphemeralBucketResources[i]
	}

	return CouchbaseEphemeralBucketResourceMap, nil
}
