package couchbasebucketresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseBucketResourceNameNotProvided = errors.New("couchbase bucket resource name is not provided")
	ErrNamespaceNotProvided                   = errors.New("namespace is not provided")
	ErrCouchbaseBucketResourceDoesNotExist    = errors.New("couchbase bucket resource does not exist")
	ErrNoCouchbaseBucketResourcesInNamespace  = errors.New("no couchbase bucket resources in namespace")
)

// GetCouchbaseBucketResourceNames returns a slice of strings containing names of all couchbase bucket resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseBucketResourcesInNamespace.
func GetCouchbaseBucketResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase bucket resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseBucketResourceNamesOutput, err := kubectl.Get("CouchbaseBucket").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase bucket resource names: %w", err)
	}

	if CouchbaseBucketResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase bucket resource names: %w", ErrNoCouchbaseBucketResourcesInNamespace)
	}

	CouchbaseBucketResourceNames := strings.Split(CouchbaseBucketResourceNamesOutput, "\n")
	for i := range CouchbaseBucketResourceNames {
		// kubectl returns couchbase bucket resource names as couchbasebuckets.couchbase.com/bucket-name. We remove the prefix "couchbasebuckets.couchbase.com/"
		CouchbaseBucketResourceNames[i] = strings.TrimPrefix(CouchbaseBucketResourceNames[i], "couchbasebuckets.couchbase.com/")
	}

	return CouchbaseBucketResourceNames, nil
}

// GetCouchbaseBucket gets the couchbase bucket resource information of the couchbase bucket resource in the given namespace and returns *CouchbaseBucketResource.
// Defined errors returned: ErrCouchbaseBucketResourceDoesNotExist.
func GetCouchbaseBucket(CouchbaseBucketResourceName string, namespace string) (*CouchbaseBucketResource, error) {
	if CouchbaseBucketResourceName == "" {
		return nil, fmt.Errorf("get couchbase bucket: %w", ErrCouchbaseBucketResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase bucket: %w", ErrNamespaceNotProvided)
	}

	CouchbaseBucketResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseBucket", CouchbaseBucketResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasebuckets.couchbase.com \"%s\" not found", CouchbaseBucketResourceName) {
			return nil, fmt.Errorf("get couchbase bucket: %w", ErrCouchbaseBucketResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase bucket: %w", err)
	}

	var CouchbaseBucketResource CouchbaseBucketResource

	err = json.Unmarshal([]byte(CouchbaseBucketResourceJSON), &CouchbaseBucketResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase bucket: %w", err)
	}

	return &CouchbaseBucketResource, nil
}

// GetCouchbaseBucketResources gets the couchbase bucket resource information and returns the *CouchbaseBucketResourceList containing the list of CouchbaseBucketResources.
// If CouchbaseBucketResourceNames = nil, then all the CouchbaseBucketResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseBucketResourceNamesDoesNotExist.
func GetCouchbaseBucketResources(CouchbaseBucketResourceNames []string, namespace string) (*CouchbaseBucketResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase bucket resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseBucketResourceNames == nil {
		CouchbaseBucketResourceNamesList, err := GetCouchbaseBucketResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase bucket resources: %w", err)
		}

		CouchbaseBucketResourceNames = CouchbaseBucketResourceNamesList
	}

	var CouchbaseBucketResourceList CouchbaseBucketResourceList

	// When we execute `get CouchbaseBucket <single-bucket>`, then we receive a single couchbaseBucket JSON instead of list of couchbaseBucket JSONs.
	if len(CouchbaseBucketResourceNames) == 1 {
		CouchbaseBucketResource, err := GetCouchbaseBucket(CouchbaseBucketResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase bucket resources: %w", err)
		}

		CouchbaseBucketResourceList.CouchbaseBucketResources = append(CouchbaseBucketResourceList.CouchbaseBucketResources, CouchbaseBucketResource)

		return &CouchbaseBucketResourceList, nil
	}

	CouchbaseBucketResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseBucket", CouchbaseBucketResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase bucket resources: %w", ErrCouchbaseBucketResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase bucket resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseBucketResourcesJSON), &CouchbaseBucketResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase bucket resources: json unmarshal: %w", err)
	}

	return &CouchbaseBucketResourceList, nil
}

// GetCouchbaseBucketsMap gets the couchbase bucket resource information and returns the map[string]*CouchbaseBucketResource which has the *CouchbaseBucketResource for each couchbase bucket resource names in given list.
// If CouchbaseBucketResourceNames = nil, then all the couchbase bucket resources in the namespace are taken into account.
func GetCouchbaseBucketResourcesMap(CouchbaseBucketResourceNames []string, namespace string) (map[string]*CouchbaseBucketResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase bucket resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseBucketResourceNames == nil {
		CouchbaseBucketResourceNamesList, err := GetCouchbaseBucketResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase bucket resources map: %w", err)
		}

		CouchbaseBucketResourceNames = CouchbaseBucketResourceNamesList
	}

	CouchbaseBucketResourceMap := make(map[string]*CouchbaseBucketResource)

	CouchbaseBucketResourcesList, err := GetCouchbaseBucketResources(CouchbaseBucketResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase bucket resources map: %w", err)
	}

	for i := range CouchbaseBucketResourcesList.CouchbaseBucketResources {
		CouchbaseBucketResourceMap[CouchbaseBucketResourceNames[i]] = CouchbaseBucketResourcesList.CouchbaseBucketResources[i]
	}

	return CouchbaseBucketResourceMap, nil
}
