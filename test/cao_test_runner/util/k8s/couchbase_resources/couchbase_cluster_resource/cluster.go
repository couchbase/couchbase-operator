package couchbaseclusterresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseClusterResourceNameNotProvided = errors.New("couchbase cluster resource name is not provided")
	ErrNamespaceNotProvided                    = errors.New("namespace is not provided")
	ErrCouchbaseClusterResourceDoesNotExist    = errors.New("couchbase cluster resource does not exist")
	ErrNoCouchbaseClusterResourcesInNamespace  = errors.New("no couchbase cluster resources in namespace")
)

// GetCouchbaseClusterResourceNames returns a slice of strings containing names of all couchbase cluster resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseClusterResourcesInNamespace.
func GetCouchbaseClusterResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase cluster resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseClusterResourceNamesOutput, err := kubectl.Get("CouchbaseCluster").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase cluster resource names: %w", err)
	}

	if CouchbaseClusterResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase cluster resource names: %w", ErrNoCouchbaseClusterResourcesInNamespace)
	}

	CouchbaseClusterResourceNames := strings.Split(CouchbaseClusterResourceNamesOutput, "\n")
	for i := range CouchbaseClusterResourceNames {
		// kubectl returns couchbase cluster resource names as couchbaseclusters.couchbase.com/group-name. We remove the prefix "couchbaseclusters.couchbase.com/"
		CouchbaseClusterResourceNames[i] = strings.TrimPrefix(CouchbaseClusterResourceNames[i], "couchbaseclusters.couchbase.com/")
	}

	return CouchbaseClusterResourceNames, nil
}

// GetCouchbaseCluster gets the couchbase cluster resource information of the couchbase cluster resource in the given namespace and returns *CouchbaseClusterResource.
// Defined errors returned: ErrCouchbaseClusterResourceDoesNotExist.
func GetCouchbaseCluster(CouchbaseClusterResourceName string, namespace string) (*CouchbaseClusterResource, error) {
	if CouchbaseClusterResourceName == "" {
		return nil, fmt.Errorf("get couchbase cluster: %w", ErrCouchbaseClusterResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase cluster: %w", ErrNamespaceNotProvided)
	}

	CouchbaseClusterResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseCluster", CouchbaseClusterResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbaseclusters.couchbase.com \"%s\" not found", CouchbaseClusterResourceName) {
			return nil, fmt.Errorf("get couchbase cluster: %w", ErrCouchbaseClusterResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase cluster: %w", err)
	}

	var CouchbaseClusterResource CouchbaseClusterResource

	err = json.Unmarshal([]byte(CouchbaseClusterResourceJSON), &CouchbaseClusterResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase cluster: %w", err)
	}

	return &CouchbaseClusterResource, nil
}

// GetCouchbaseClusterResources gets the couchbase cluster resource information and returns the *CouchbaseClusterResourceList containing the list of CouchbaseClusterResources.
// If CouchbaseClusterResourceNames = nil, then all the CouchbaseClusterResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseClusterResourceNamesDoesNotExist.
func GetCouchbaseClusterResources(CouchbaseClusterResourceNames []string, namespace string) (*CouchbaseClusterResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase cluster resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseClusterResourceNames == nil {
		CouchbaseClusterResourceNamesList, err := GetCouchbaseClusterResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase cluster resources: %w", err)
		}

		CouchbaseClusterResourceNames = CouchbaseClusterResourceNamesList
	}

	var CouchbaseClusterResourceList CouchbaseClusterResourceList

	// When we execute `get CouchbaseCluster <single-cluster>`, then we receive a single couchbaseCluster JSON instead of list of couchbaseCluster JSONs.
	if len(CouchbaseClusterResourceNames) == 1 {
		CouchbaseClusterResource, err := GetCouchbaseCluster(CouchbaseClusterResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase cluster resources: %w", err)
		}

		CouchbaseClusterResourceList.CouchbaseClusterResources = append(CouchbaseClusterResourceList.CouchbaseClusterResources, CouchbaseClusterResource)

		return &CouchbaseClusterResourceList, nil
	}

	CouchbaseClusterResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseCluster", CouchbaseClusterResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase cluster resources: %w", ErrCouchbaseClusterResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase cluster resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseClusterResourcesJSON), &CouchbaseClusterResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase cluster resources: json unmarshal: %w", err)
	}

	return &CouchbaseClusterResourceList, nil
}

// GetCouchbaseClustersMap gets the couchbase cluster resource information and returns the map[string]*CouchbaseClusterResource which has the *CouchbaseClusterResource for each couchbase cluster resource names in given list.
// If CouchbaseClusterResourceNames = nil, then all the couchbase cluster resources in the namespace are taken into account.
func GetCouchbaseClusterResourcesMap(CouchbaseClusterResourceNames []string, namespace string) (map[string]*CouchbaseClusterResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase cluster resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseClusterResourceNames == nil {
		CouchbaseClusterResourceNamesList, err := GetCouchbaseClusterResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase cluster resources map: %w", err)
		}

		CouchbaseClusterResourceNames = CouchbaseClusterResourceNamesList
	}

	CouchbaseClusterResourceMap := make(map[string]*CouchbaseClusterResource)

	CouchbaseClusterResourcesList, err := GetCouchbaseClusterResources(CouchbaseClusterResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase cluster resources map: %w", err)
	}

	for i := range CouchbaseClusterResourcesList.CouchbaseClusterResources {
		CouchbaseClusterResourceMap[CouchbaseClusterResourceNames[i]] = CouchbaseClusterResourcesList.CouchbaseClusterResources[i]
	}

	return CouchbaseClusterResourceMap, nil
}
