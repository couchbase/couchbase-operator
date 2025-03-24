package couchbasecollectiongroupresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseCollectionGroupResourceNameNotProvided = errors.New("couchbase collection group resource name is not provided")
	ErrNamespaceNotProvided                            = errors.New("namespace is not provided")
	ErrCouchbaseCollectionGroupResourceDoesNotExist    = errors.New("couchbase collection group resource does not exist")
	ErrNoCouchbaseCollectionGroupResourcesInNamespace  = errors.New("no couchbase collection group resources in namespace")
)

// GetCouchbaseCollectionGroupResourceNames returns a slice of strings containing names of all couchbase collection group resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseCollectionGroupResourcesInNamespace.
func GetCouchbaseCollectionGroupResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection group resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseCollectionGroupResourceNamesOutput, err := kubectl.Get("CouchbaseCollectionGroup").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection group resource names: %w", err)
	}

	if CouchbaseCollectionGroupResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase collection group resource names: %w", ErrNoCouchbaseCollectionGroupResourcesInNamespace)
	}

	CouchbaseCollectionGroupResourceNames := strings.Split(CouchbaseCollectionGroupResourceNamesOutput, "\n")
	for i := range CouchbaseCollectionGroupResourceNames {
		// kubectl returns couchbase collection group resource names as couchbasecollectiongroups.couchbase.com/collection-group-name. We remove the prefix "couchbasecollectiongroups.couchbase.com/"
		CouchbaseCollectionGroupResourceNames[i] = strings.TrimPrefix(CouchbaseCollectionGroupResourceNames[i], "couchbasecollectiongroups.couchbase.com/")
	}

	return CouchbaseCollectionGroupResourceNames, nil
}

// GetCouchbaseCollectionGroup gets the couchbase collection group resource information of the couchbase collection group resource in the given namespace and returns *CouchbaseCollectionGroupResource.
// Defined errors returned: ErrCouchbaseCollectionGroupResourceDoesNotExist.
func GetCouchbaseCollectionGroup(CouchbaseCollectionGroupResourceName string, namespace string) (*CouchbaseCollectionGroupResource, error) {
	if CouchbaseCollectionGroupResourceName == "" {
		return nil, fmt.Errorf("get couchbase collection group: %w", ErrCouchbaseCollectionGroupResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection group: %w", ErrNamespaceNotProvided)
	}

	CouchbaseCollectionGroupResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseCollectionGroup", CouchbaseCollectionGroupResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasecollectiongroups.couchbase.com \"%s\" not found", CouchbaseCollectionGroupResourceName) {
			return nil, fmt.Errorf("get couchbase collection group: %w", ErrCouchbaseCollectionGroupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase collection group: %w", err)
	}

	var CouchbaseCollectionGroupResource CouchbaseCollectionGroupResource

	err = json.Unmarshal([]byte(CouchbaseCollectionGroupResourceJSON), &CouchbaseCollectionGroupResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection group: %w", err)
	}

	return &CouchbaseCollectionGroupResource, nil
}

// GetCouchbaseCollectionGroupResources gets the couchbase collection group resource information and returns the *CouchbaseCollectionGroupResourceList containing the list of CouchbaseCollectionGroupResources.
// If CouchbaseCollectionGroupResourceNames = nil, then all the CouchbaseCollectionGroupResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseCollectionGroupResourceNamesDoesNotExist.
func GetCouchbaseCollectionGroupResources(CouchbaseCollectionGroupResourceNames []string, namespace string) (*CouchbaseCollectionGroupResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection group resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseCollectionGroupResourceNames == nil {
		CouchbaseCollectionGroupResourceNamesList, err := GetCouchbaseCollectionGroupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase collection group resources: %w", err)
		}

		CouchbaseCollectionGroupResourceNames = CouchbaseCollectionGroupResourceNamesList
	}

	var CouchbaseCollectionGroupResourceList CouchbaseCollectionGroupResourceList

	// When we execute `get CouchbaseCollectionGroup <single-collection-group>`, then we receive a single couchbaseCollectionGroup JSON instead of list of couchbaseCollectionGroup JSONs.
	if len(CouchbaseCollectionGroupResourceNames) == 1 {
		CouchbaseCollectionGroupResource, err := GetCouchbaseCollectionGroup(CouchbaseCollectionGroupResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase collection group resources: %w", err)
		}

		CouchbaseCollectionGroupResourceList.CouchbaseCollectionGroupResources = append(CouchbaseCollectionGroupResourceList.CouchbaseCollectionGroupResources, CouchbaseCollectionGroupResource)

		return &CouchbaseCollectionGroupResourceList, nil
	}

	CouchbaseCollectionGroupResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseCollectionGroup", CouchbaseCollectionGroupResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase collection group resources: %w", ErrCouchbaseCollectionGroupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase collection group resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseCollectionGroupResourcesJSON), &CouchbaseCollectionGroupResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection group resources: json unmarshal: %w", err)
	}

	return &CouchbaseCollectionGroupResourceList, nil
}

// GetCouchbaseCollectionGroupsMap gets the couchbase collection group resource information and returns the map[string]*CouchbaseCollectionGroupResource which has the *CouchbaseCollectionGroupResource for each couchbase collection group resource names in given list.
// If CouchbaseCollectionGroupResourceNames = nil, then all the couchbase collection group resources in the namespace are taken into account.
func GetCouchbaseCollectionGroupResourcesMap(CouchbaseCollectionGroupResourceNames []string, namespace string) (map[string]*CouchbaseCollectionGroupResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase collection group resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseCollectionGroupResourceNames == nil {
		CouchbaseCollectionGroupResourceNamesList, err := GetCouchbaseCollectionGroupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase collection group resources map: %w", err)
		}

		CouchbaseCollectionGroupResourceNames = CouchbaseCollectionGroupResourceNamesList
	}

	CouchbaseCollectionGroupResourceMap := make(map[string]*CouchbaseCollectionGroupResource)

	CouchbaseCollectionGroupResourcesList, err := GetCouchbaseCollectionGroupResources(CouchbaseCollectionGroupResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase collection group resources map: %w", err)
	}

	for i := range CouchbaseCollectionGroupResourcesList.CouchbaseCollectionGroupResources {
		CouchbaseCollectionGroupResourceMap[CouchbaseCollectionGroupResourceNames[i]] = CouchbaseCollectionGroupResourcesList.CouchbaseCollectionGroupResources[i]
	}

	return CouchbaseCollectionGroupResourceMap, nil
}
