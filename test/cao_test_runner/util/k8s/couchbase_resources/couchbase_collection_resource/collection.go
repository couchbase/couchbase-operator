package couchbasecollectionresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseCollectionResourceNameNotProvided = errors.New("couchbase collection resource name is not provided")
	ErrNamespaceNotProvided                       = errors.New("namespace is not provided")
	ErrCouchbaseCollectionResourceDoesNotExist    = errors.New("couchbase collection resource does not exist")
	ErrNoCouchbaseCollectionResourcesInNamespace  = errors.New("no couchbase collection resources in namespace")
)

// GetCouchbaseCollectionResourceNames returns a slice of strings containing names of all couchbase collection resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseCollectionResourcesInNamespace.
func GetCouchbaseCollectionResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseCollectionResourceNamesOutput, err := kubectl.Get("CouchbaseCollection").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection resource names: %w", err)
	}

	if CouchbaseCollectionResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase collection resource names: %w", ErrNoCouchbaseCollectionResourcesInNamespace)
	}

	CouchbaseCollectionResourceNames := strings.Split(CouchbaseCollectionResourceNamesOutput, "\n")
	for i := range CouchbaseCollectionResourceNames {
		// kubectl returns couchbase collection resource names as couchbasecollections.couchbase.com/collection-name. We remove the prefix "couchbasecollections.couchbase.com/"
		CouchbaseCollectionResourceNames[i] = strings.TrimPrefix(CouchbaseCollectionResourceNames[i], "couchbasecollections.couchbase.com/")
	}

	return CouchbaseCollectionResourceNames, nil
}

// GetCouchbaseCollection gets the couchbase collection resource information of the couchbase collection resource in the given namespace and returns *CouchbaseCollectionResource.
// Defined errors returned: ErrCouchbaseCollectionResourceDoesNotExist.
func GetCouchbaseCollection(CouchbaseCollectionResourceName string, namespace string) (*CouchbaseCollectionResource, error) {
	if CouchbaseCollectionResourceName == "" {
		return nil, fmt.Errorf("get couchbase collection: %w", ErrCouchbaseCollectionResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection: %w", ErrNamespaceNotProvided)
	}

	CouchbaseCollectionResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseCollection", CouchbaseCollectionResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasecollections.couchbase.com \"%s\" not found", CouchbaseCollectionResourceName) {
			return nil, fmt.Errorf("get couchbase collection: %w", ErrCouchbaseCollectionResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase collection: %w", err)
	}

	var CouchbaseCollectionResource CouchbaseCollectionResource

	err = json.Unmarshal([]byte(CouchbaseCollectionResourceJSON), &CouchbaseCollectionResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection: %w", err)
	}

	return &CouchbaseCollectionResource, nil
}

// GetCouchbaseCollectionResources gets the couchbase collection resource information and returns the *CouchbaseCollectionResourceList containing the list of CouchbaseCollectionResources.
// If CouchbaseCollectionResourceNames = nil, then all the CouchbaseCollectionResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseCollectionResourceNamesDoesNotExist.
func GetCouchbaseCollectionResources(CouchbaseCollectionResourceNames []string, namespace string) (*CouchbaseCollectionResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseCollectionResourceNames == nil {
		CouchbaseCollectionResourceNamesList, err := GetCouchbaseCollectionResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase collection resources: %w", err)
		}

		CouchbaseCollectionResourceNames = CouchbaseCollectionResourceNamesList
	}

	var CouchbaseCollectionResourceList CouchbaseCollectionResourceList

	// When we execute `get CouchbaseCollection <single-collection>`, then we receive a single couchbaseCollection JSON instead of list of couchbaseCollection JSONs.
	if len(CouchbaseCollectionResourceNames) == 1 {
		CouchbaseCollectionResource, err := GetCouchbaseCollection(CouchbaseCollectionResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase collection resources: %w", err)
		}

		CouchbaseCollectionResourceList.CouchbaseCollectionResources = append(CouchbaseCollectionResourceList.CouchbaseCollectionResources, CouchbaseCollectionResource)

		return &CouchbaseCollectionResourceList, nil
	}

	CouchbaseCollectionResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseCollection", CouchbaseCollectionResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase collection resources: %w", ErrCouchbaseCollectionResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase collection resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseCollectionResourcesJSON), &CouchbaseCollectionResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection resources: json unmarshal: %w", err)
	}

	return &CouchbaseCollectionResourceList, nil
}

// GetCouchbaseCollectionsMap gets the couchbase collection resource information and returns the map[string]*CouchbaseCollectionResource which has the *CouchbaseCollectionResource for each couchbase collection resource names in given list.
// If CouchbaseCollectionResourceNames = nil, then all the couchbase collection resources in the namespace are taken into account.
func GetCouchbaseCollectionResourcesMap(CouchbaseCollectionResourceNames []string, namespace string) (map[string]*CouchbaseCollectionResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseCollectionResourceNames == nil {
		CouchbaseCollectionResourceNamesList, err := GetCouchbaseCollectionResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase collection resources map: %w", err)
		}

		CouchbaseCollectionResourceNames = CouchbaseCollectionResourceNamesList
	}

	CouchbaseCollectionResourceMap := make(map[string]*CouchbaseCollectionResource)

	CouchbaseCollectionResourcesList, err := GetCouchbaseCollectionResources(CouchbaseCollectionResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection resources map: %w", err)
	}

	for i := range CouchbaseCollectionResourcesList.CouchbaseCollectionResources {
		CouchbaseCollectionResourceMap[CouchbaseCollectionResourceNames[i]] = CouchbaseCollectionResourcesList.CouchbaseCollectionResources[i]
	}

	return CouchbaseCollectionResourceMap, nil
}
