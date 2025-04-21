package couchbasescopegroupresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseScopeGroupResourceNameNotProvided = errors.New("couchbase scope group resource name is not provided")
	ErrNamespaceNotProvided                       = errors.New("namespace is not provided")
	ErrCouchbaseScopeGroupResourceDoesNotExist    = errors.New("couchbase scope group resource does not exist")
	ErrNoCouchbaseScopeGroupResourcesInNamespace  = errors.New("no couchbase scope group resources in namespace")
)

// GetCouchbaseScopeGroupResourceNames returns a slice of strings containing names of all couchbase scope group resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseScopeGroupResourcesInNamespace.
func GetCouchbaseScopeGroupResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope group resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseScopeGroupResourceNamesOutput, err := kubectl.Get("CouchbaseScopeGroup").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope group resource names: %w", err)
	}

	if CouchbaseScopeGroupResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase scope group resource names: %w", ErrNoCouchbaseScopeGroupResourcesInNamespace)
	}

	CouchbaseScopeGroupResourceNames := strings.Split(CouchbaseScopeGroupResourceNamesOutput, "\n")
	for i := range CouchbaseScopeGroupResourceNames {
		// kubectl returns couchbase scope group resource names as couchbasescopegroups.couchbase.com/scope-group-name. We remove the prefix "couchbasescopegroups.couchbase.com/"
		CouchbaseScopeGroupResourceNames[i] = strings.TrimPrefix(CouchbaseScopeGroupResourceNames[i], "couchbasescopegroups.couchbase.com/")
	}

	return CouchbaseScopeGroupResourceNames, nil
}

// GetCouchbaseScopeGroup gets the couchbase scope group resource information of the couchbase scope group resource in the given namespace and returns *CouchbaseScopeGroupResource.
// Defined errors returned: ErrCouchbaseScopeGroupResourceDoesNotExist.
func GetCouchbaseScopeGroup(CouchbaseScopeGroupResourceName string, namespace string) (*CouchbaseScopeGroupResource, error) {
	if CouchbaseScopeGroupResourceName == "" {
		return nil, fmt.Errorf("get couchbase scope group: %w", ErrCouchbaseScopeGroupResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope group: %w", ErrNamespaceNotProvided)
	}

	CouchbaseScopeGroupResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseScopeGroup", CouchbaseScopeGroupResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasescopegroups.couchbase.com \"%s\" not found", CouchbaseScopeGroupResourceName) {
			return nil, fmt.Errorf("get couchbase scope group: %w", ErrCouchbaseScopeGroupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase scope group: %w", err)
	}

	var CouchbaseScopeGroupResource CouchbaseScopeGroupResource

	err = json.Unmarshal([]byte(CouchbaseScopeGroupResourceJSON), &CouchbaseScopeGroupResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope group: %w", err)
	}

	return &CouchbaseScopeGroupResource, nil
}

// GetCouchbaseScopeGroupResources gets the couchbase scope group resource information and returns the *CouchbaseScopeGroupResourceList containing the list of CouchbaseScopeGroupResources.
// If CouchbaseScopeGroupResourceNames = nil, then all the CouchbaseScopeGroupResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseScopeGroupResourceNamesDoesNotExist.
func GetCouchbaseScopeGroupResources(CouchbaseScopeGroupResourceNames []string, namespace string) (*CouchbaseScopeGroupResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope group resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseScopeGroupResourceNames == nil {
		CouchbaseScopeGroupResourceNamesList, err := GetCouchbaseScopeGroupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase scope group resources: %w", err)
		}

		CouchbaseScopeGroupResourceNames = CouchbaseScopeGroupResourceNamesList
	}

	var CouchbaseScopeGroupResourceList CouchbaseScopeGroupResourceList

	// When we execute `get CouchbaseScopeGroup <single-scope-group>`, then we receive a single couchbaseScopeGroup JSON instead of list of couchbaseScopeGroup JSONs.
	if len(CouchbaseScopeGroupResourceNames) == 1 {
		CouchbaseScopeGroupResource, err := GetCouchbaseScopeGroup(CouchbaseScopeGroupResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase scope group resources: %w", err)
		}

		CouchbaseScopeGroupResourceList.CouchbaseScopeGroupResources = append(CouchbaseScopeGroupResourceList.CouchbaseScopeGroupResources, CouchbaseScopeGroupResource)

		return &CouchbaseScopeGroupResourceList, nil
	}

	CouchbaseScopeGroupResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseScopeGroup", CouchbaseScopeGroupResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase scope group resources: %w", ErrCouchbaseScopeGroupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase scope group resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseScopeGroupResourcesJSON), &CouchbaseScopeGroupResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope group resources: json unmarshal: %w", err)
	}

	return &CouchbaseScopeGroupResourceList, nil
}

// GetCouchbaseScopeGroupsMap gets the couchbase scope group resource information and returns the map[string]*CouchbaseScopeGroupResource which has the *CouchbaseScopeGroupResource for each couchbase scope group resource names in given list.
// If CouchbaseScopeGroupResourceNames = nil, then all the couchbase scope group resources in the namespace are taken into account.
func GetCouchbaseScopeGroupResourcesMap(CouchbaseScopeGroupResourceNames []string, namespace string) (map[string]*CouchbaseScopeGroupResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope group resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseScopeGroupResourceNames == nil {
		CouchbaseScopeGroupResourceNamesList, err := GetCouchbaseScopeGroupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase scope group resources map: %w", err)
		}

		CouchbaseScopeGroupResourceNames = CouchbaseScopeGroupResourceNamesList
	}

	CouchbaseScopeGroupResourceMap := make(map[string]*CouchbaseScopeGroupResource)

	CouchbaseScopeGroupResourcesList, err := GetCouchbaseScopeGroupResources(CouchbaseScopeGroupResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope group resources map: %w", err)
	}

	for i := range CouchbaseScopeGroupResourcesList.CouchbaseScopeGroupResources {
		CouchbaseScopeGroupResourceMap[CouchbaseScopeGroupResourceNames[i]] = CouchbaseScopeGroupResourcesList.CouchbaseScopeGroupResources[i]
	}

	return CouchbaseScopeGroupResourceMap, nil
}
