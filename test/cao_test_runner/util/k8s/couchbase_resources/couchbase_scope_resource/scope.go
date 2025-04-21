package couchbasescoperesource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseScopeResourceNameNotProvided = errors.New("couchbase scope resource name is not provided")
	ErrNamespaceNotProvided                  = errors.New("namespace is not provided")
	ErrCouchbaseScopeResourceDoesNotExist    = errors.New("couchbase scope resource does not exist")
	ErrNoCouchbaseScopeResourcesInNamespace  = errors.New("no couchbase scope resources in namespace")
)

// GetCouchbaseScopeResourceNames returns a slice of strings containing names of all couchbase scope resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseScopeResourcesInNamespace.
func GetCouchbaseScopeResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseScopeResourceNamesOutput, err := kubectl.Get("CouchbaseScope").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope resource names: %w", err)
	}

	if CouchbaseScopeResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase scope resource names: %w", ErrNoCouchbaseScopeResourcesInNamespace)
	}

	CouchbaseScopeResourceNames := strings.Split(CouchbaseScopeResourceNamesOutput, "\n")
	for i := range CouchbaseScopeResourceNames {
		// kubectl returns couchbase scope resource names as couchbasescopes.couchbase.com/scope-name. We remove the prefix "couchbasescopes.couchbase.com/"
		CouchbaseScopeResourceNames[i] = strings.TrimPrefix(CouchbaseScopeResourceNames[i], "couchbasescopes.couchbase.com/")
	}

	return CouchbaseScopeResourceNames, nil
}

// GetCouchbaseScope gets the couchbase scope resource information of the couchbase scope resource in the given namespace and returns *CouchbaseScopeResource.
// Defined errors returned: ErrCouchbaseScopeResourceDoesNotExist.
func GetCouchbaseScope(CouchbaseScopeResourceName string, namespace string) (*CouchbaseScopeResource, error) {
	if CouchbaseScopeResourceName == "" {
		return nil, fmt.Errorf("get couchbase scope: %w", ErrCouchbaseScopeResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope: %w", ErrNamespaceNotProvided)
	}

	CouchbaseScopeResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseScope", CouchbaseScopeResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasescopes.couchbase.com \"%s\" not found", CouchbaseScopeResourceName) {
			return nil, fmt.Errorf("get couchbase scope: %w", ErrCouchbaseScopeResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase scope: %w", err)
	}

	var CouchbaseScopeResource CouchbaseScopeResource

	err = json.Unmarshal([]byte(CouchbaseScopeResourceJSON), &CouchbaseScopeResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope: %w", err)
	}

	return &CouchbaseScopeResource, nil
}

// GetCouchbaseScopeResources gets the couchbase scope resource information and returns the *CouchbaseScopeResourceList containing the list of CouchbaseScopeResources.
// If CouchbaseScopeResourceNames = nil, then all the CouchbaseScopeResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseScopeResourceNamesDoesNotExist.
func GetCouchbaseScopeResources(CouchbaseScopeResourceNames []string, namespace string) (*CouchbaseScopeResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseScopeResourceNames == nil {
		CouchbaseScopeResourceNamesList, err := GetCouchbaseScopeResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase scope resources: %w", err)
		}

		CouchbaseScopeResourceNames = CouchbaseScopeResourceNamesList
	}

	var CouchbaseScopeResourceList CouchbaseScopeResourceList

	// When we execute `get CouchbaseScope <single-scope>`, then we receive a single couchbaseScope JSON instead of list of couchbaseScope JSONs.
	if len(CouchbaseScopeResourceNames) == 1 {
		CouchbaseScopeResource, err := GetCouchbaseScope(CouchbaseScopeResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase scope resources: %w", err)
		}

		CouchbaseScopeResourceList.CouchbaseScopeResources = append(CouchbaseScopeResourceList.CouchbaseScopeResources, CouchbaseScopeResource)

		return &CouchbaseScopeResourceList, nil
	}

	CouchbaseScopeResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseScope", CouchbaseScopeResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase scope resources: %w", ErrCouchbaseScopeResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase scope resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseScopeResourcesJSON), &CouchbaseScopeResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope resources: json unmarshal: %w", err)
	}

	return &CouchbaseScopeResourceList, nil
}

// GetCouchbaseScopesMap gets the couchbase scope resource information and returns the map[string]*CouchbaseScopeResource which has the *CouchbaseScopeResource for each couchbase scope resource names in given list.
// If CouchbaseScopeResourceNames = nil, then all the couchbase scope resources in the namespace are taken into account.
func GetCouchbaseScopeResourcesMap(CouchbaseScopeResourceNames []string, namespace string) (map[string]*CouchbaseScopeResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase scope resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseScopeResourceNames == nil {
		CouchbaseScopeResourceNamesList, err := GetCouchbaseScopeResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase scope resources map: %w", err)
		}

		CouchbaseScopeResourceNames = CouchbaseScopeResourceNamesList
	}

	CouchbaseScopeResourceMap := make(map[string]*CouchbaseScopeResource)

	CouchbaseScopeResourcesList, err := GetCouchbaseScopeResources(CouchbaseScopeResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase scope resources map: %w", err)
	}

	for i := range CouchbaseScopeResourcesList.CouchbaseScopeResources {
		CouchbaseScopeResourceMap[CouchbaseScopeResourceNames[i]] = CouchbaseScopeResourcesList.CouchbaseScopeResources[i]
	}

	return CouchbaseScopeResourceMap, nil
}
