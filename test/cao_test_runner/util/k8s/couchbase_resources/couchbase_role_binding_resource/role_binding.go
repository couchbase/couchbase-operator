package couchbaserolebindingresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseRoleBindingResourceNameNotProvided = errors.New("couchbase role binding resource name is not provided")
	ErrNamespaceNotProvided                        = errors.New("namespace is not provided")
	ErrCouchbaseRoleBindingResourceDoesNotExist    = errors.New("couchbase role binding resource does not exist")
	ErrNoCouchbaseRoleBindingResourcesInNamespace  = errors.New("no couchbase role binding resources in namespace")
)

// GetCouchbaseRoleBindingResourceNames returns a slice of strings containing names of all couchbase role binding resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseRoleBindingResourcesInNamespace.
func GetCouchbaseRoleBindingResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase role binding resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseRoleBindingResourceNamesOutput, err := kubectl.Get("CouchbaseRoleBinding").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase role binding resource names: %w", err)
	}

	if CouchbaseRoleBindingResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase role binding resource names: %w", ErrNoCouchbaseRoleBindingResourcesInNamespace)
	}

	CouchbaseRoleBindingResourceNames := strings.Split(CouchbaseRoleBindingResourceNamesOutput, "\n")
	for i := range CouchbaseRoleBindingResourceNames {
		// kubectl returns couchbase role binding resource names as couchbaserolebindings.couchbase.com/role-binding-name. We remove the prefix "couchbaserolebindings.couchbase.com/"
		CouchbaseRoleBindingResourceNames[i] = strings.TrimPrefix(CouchbaseRoleBindingResourceNames[i], "couchbaserolebindings.couchbase.com/")
	}

	return CouchbaseRoleBindingResourceNames, nil
}

// GetCouchbaseRoleBinding gets the couchbase role binding resource information of the couchbase role binding resource in the given namespace and returns *CouchbaseRoleBindingResource.
// Defined errors returned: ErrCouchbaseRoleBindingResourceDoesNotExist.
func GetCouchbaseRoleBinding(CouchbaseRoleBindingResourceName string, namespace string) (*CouchbaseRoleBindingResource, error) {
	if CouchbaseRoleBindingResourceName == "" {
		return nil, fmt.Errorf("get couchbase role binding: %w", ErrCouchbaseRoleBindingResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase role binding: %w", ErrNamespaceNotProvided)
	}

	CouchbaseRoleBindingResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseRoleBinding", CouchbaseRoleBindingResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbaserolebindings.couchbase.com \"%s\" not found", CouchbaseRoleBindingResourceName) {
			return nil, fmt.Errorf("get couchbase user: %w", ErrCouchbaseRoleBindingResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase role binding: %w", err)
	}

	var CouchbaseRoleBindingResource CouchbaseRoleBindingResource

	err = json.Unmarshal([]byte(CouchbaseRoleBindingResourceJSON), &CouchbaseRoleBindingResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase role binding: %w", err)
	}

	return &CouchbaseRoleBindingResource, nil
}

// GetCouchbaseRoleBindingResources gets the couchbase role binding resource information and returns the *CouchbaseRoleBindingResourceList containing the list of CouchbaseRoleBindingResources.
// If CouchbaseRoleBindingResourceNames = nil, then all the CouchbaseRoleBindingResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseRoleBindingResourceNamesDoesNotExist.
func GetCouchbaseRoleBindingResources(CouchbaseRoleBindingResourceNames []string, namespace string) (*CouchbaseRoleBindingResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase role binding resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseRoleBindingResourceNames == nil {
		CouchbaseRoleBindingResourceNamesList, err := GetCouchbaseRoleBindingResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase role binding resources: %w", err)
		}

		CouchbaseRoleBindingResourceNames = CouchbaseRoleBindingResourceNamesList
	}

	var CouchbaseRoleBindingResourceList CouchbaseRoleBindingResourceList

	// When we execute `get CouchbaseRoleBinding <single-role-binding>`, then we receive a single couchbaseRoleBinding JSON instead of list of couchbaseRoleBinding JSONs.
	if len(CouchbaseRoleBindingResourceNames) == 1 {
		CouchbaseRoleBindingResource, err := GetCouchbaseRoleBinding(CouchbaseRoleBindingResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase role binding resources: %w", err)
		}

		CouchbaseRoleBindingResourceList.CouchbaseRoleBindingResources = append(CouchbaseRoleBindingResourceList.CouchbaseRoleBindingResources, CouchbaseRoleBindingResource)

		return &CouchbaseRoleBindingResourceList, nil
	}

	CouchbaseRoleBindingResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseRoleBinding", CouchbaseRoleBindingResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase role binding resources: %w", ErrCouchbaseRoleBindingResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase role binding resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseRoleBindingResourcesJSON), &CouchbaseRoleBindingResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase role binding resources: json unmarshal: %w", err)
	}

	return &CouchbaseRoleBindingResourceList, nil
}

// GetCouchbaseRoleBindingsMap gets the couchbase role binding resource information and returns the map[string]*CouchbaseRoleBindingResource which has the *CouchbaseRoleBindingResource for each couchbase role binding resource names in given list.
// If CouchbaseRoleBindingResourceNames = nil, then all the couchbase role binding resources in the namespace are taken into account.
func GetCouchbaseRoleBindingResourcesMap(CouchbaseRoleBindingResourceNames []string, namespace string) (map[string]*CouchbaseRoleBindingResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase role binding resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseRoleBindingResourceNames == nil {
		CouchbaseRoleBindingResourceNamesList, err := GetCouchbaseRoleBindingResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase role binding resources map: %w", err)
		}

		CouchbaseRoleBindingResourceNames = CouchbaseRoleBindingResourceNamesList
	}

	CouchbaseRoleBindingResourceMap := make(map[string]*CouchbaseRoleBindingResource)

	CouchbaseRoleBindingResourcesList, err := GetCouchbaseRoleBindingResources(CouchbaseRoleBindingResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase role binding resources map: %w", err)
	}

	for i := range CouchbaseRoleBindingResourcesList.CouchbaseRoleBindingResources {
		CouchbaseRoleBindingResourceMap[CouchbaseRoleBindingResourceNames[i]] = CouchbaseRoleBindingResourcesList.CouchbaseRoleBindingResources[i]
	}

	return CouchbaseRoleBindingResourceMap, nil
}
