package couchbasegroupresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseGroupResourceNameNotProvided = errors.New("couchbase group resource name is not provided")
	ErrNamespaceNotProvided                  = errors.New("namespace is not provided")
	ErrCouchbaseGroupResourceDoesNotExist    = errors.New("couchbase group resource does not exist")
	ErrNoCouchbaseGroupResourcesInNamespace  = errors.New("no couchbase group resources in namespace")
)

// GetCouchbaseGroupResourceNames returns a slice of strings containing names of all couchbase group resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseGroupResourcesInNamespace.
func GetCouchbaseGroupResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase group resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseGroupResourceNamesOutput, err := kubectl.Get("CouchbaseGroup").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase group resource names: %w", err)
	}

	if CouchbaseGroupResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase group resource names: %w", ErrNoCouchbaseGroupResourcesInNamespace)
	}

	CouchbaseGroupResourceNames := strings.Split(CouchbaseGroupResourceNamesOutput, "\n")
	for i := range CouchbaseGroupResourceNames {
		// kubectl returns couchbase group resource names as couchbasegroups.couchbase.com/group-name. We remove the prefix "couchbasegroups.couchbase.com/"
		CouchbaseGroupResourceNames[i] = strings.TrimPrefix(CouchbaseGroupResourceNames[i], "couchbasegroups.couchbase.com/")
	}

	return CouchbaseGroupResourceNames, nil
}

// GetCouchbaseGroup gets the couchbase group resource information of the couchbase group resource in the given namespace and returns *CouchbaseGroupResource.
// Defined errors returned: ErrCouchbaseGroupResourceDoesNotExist.
func GetCouchbaseGroup(CouchbaseGroupResourceName string, namespace string) (*CouchbaseGroupResource, error) {
	if CouchbaseGroupResourceName == "" {
		return nil, fmt.Errorf("get couchbase group: %w", ErrCouchbaseGroupResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase group: %w", ErrNamespaceNotProvided)
	}

	CouchbaseGroupResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseGroup", CouchbaseGroupResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasegroups.couchbase.com \"%s\" not found", CouchbaseGroupResourceName) {
			return nil, fmt.Errorf("get couchbase group: %w", ErrCouchbaseGroupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase group: %w", err)
	}

	var CouchbaseGroupResource CouchbaseGroupResource

	err = json.Unmarshal([]byte(CouchbaseGroupResourceJSON), &CouchbaseGroupResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase group: %w", err)
	}

	return &CouchbaseGroupResource, nil
}

// GetCouchbaseGroupResources gets the couchbase group resource information and returns the *CouchbaseGroupResourceList containing the list of CouchbaseGroupResources.
// If CouchbaseGroupResourceNames = nil, then all the CouchbaseGroupResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseGroupResourceNamesDoesNotExist.
func GetCouchbaseGroupResources(CouchbaseGroupResourceNames []string, namespace string) (*CouchbaseGroupResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase group resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseGroupResourceNames == nil {
		CouchbaseGroupResourceNamesList, err := GetCouchbaseGroupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase group resources: %w", err)
		}

		CouchbaseGroupResourceNames = CouchbaseGroupResourceNamesList
	}

	var CouchbaseGroupResourceList CouchbaseGroupResourceList

	// When we execute `get CouchbaseGroup <single-group>`, then we receive a single couchbaseGroup JSON instead of list of couchbaseGroup JSONs.
	if len(CouchbaseGroupResourceNames) == 1 {
		CouchbaseGroupResource, err := GetCouchbaseGroup(CouchbaseGroupResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase group resources: %w", err)
		}

		CouchbaseGroupResourceList.CouchbaseGroupResources = append(CouchbaseGroupResourceList.CouchbaseGroupResources, CouchbaseGroupResource)

		return &CouchbaseGroupResourceList, nil
	}

	CouchbaseGroupResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseGroup", CouchbaseGroupResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase group resources: %w", ErrCouchbaseGroupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase group resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseGroupResourcesJSON), &CouchbaseGroupResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase group resources: json unmarshal: %w", err)
	}

	return &CouchbaseGroupResourceList, nil
}

// GetCouchbaseGroupsMap gets the couchbase group resource information and returns the map[string]*CouchbaseGroupResource which has the *CouchbaseGroupResource for each couchbase group resource names in given list.
// If CouchbaseGroupResourceNames = nil, then all the couchbase group resources in the namespace are taken into account.
func GetCouchbaseGroupResourcesMap(CouchbaseGroupResourceNames []string, namespace string) (map[string]*CouchbaseGroupResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase group resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseGroupResourceNames == nil {
		CouchbaseGroupResourceNamesList, err := GetCouchbaseGroupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase group resources map: %w", err)
		}

		CouchbaseGroupResourceNames = CouchbaseGroupResourceNamesList
	}

	CouchbaseGroupResourceMap := make(map[string]*CouchbaseGroupResource)

	CouchbaseGroupResourcesList, err := GetCouchbaseGroupResources(CouchbaseGroupResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase group resources map: %w", err)
	}

	for i := range CouchbaseGroupResourcesList.CouchbaseGroupResources {
		CouchbaseGroupResourceMap[CouchbaseGroupResourceNames[i]] = CouchbaseGroupResourcesList.CouchbaseGroupResources[i]
	}

	return CouchbaseGroupResourceMap, nil
}
