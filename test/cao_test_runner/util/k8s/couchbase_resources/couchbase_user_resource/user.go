package couchbaseuserresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseUserResourceNameNotProvided = errors.New("couchbase user resource name is not provided")
	ErrNamespaceNotProvided                 = errors.New("namespace is not provided")
	ErrCouchbaseUserResourceDoesNotExist    = errors.New("couchbase user resource does not exist")
	ErrNoCouchbaseUserResourcesInNamespace  = errors.New("no couchbase user resources in namespace")
)

// GetCouchbaseUserResourceNames returns a slice of strings containing names of all couchbase user resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseUserResourcesInNamespace.
func GetCouchbaseUserResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase user resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseUserResourceNamesOutput, err := kubectl.Get("CouchbaseUser").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase user resource names: %w", err)
	}

	if CouchbaseUserResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase user resource names: %w", ErrNoCouchbaseUserResourcesInNamespace)
	}

	CouchbaseUserResourceNames := strings.Split(CouchbaseUserResourceNamesOutput, "\n")
	for i := range CouchbaseUserResourceNames {
		// kubectl returns couchbase user resource names as couchbaseusers.couchbase.com/user-name. We remove the prefix "couchbaseusers.couchbase.com/"
		CouchbaseUserResourceNames[i] = strings.TrimPrefix(CouchbaseUserResourceNames[i], "couchbaseusers.couchbase.com/")
	}

	return CouchbaseUserResourceNames, nil
}

// GetCouchbaseUser gets the couchbase user resource information of the couchbase user resource in the given namespace and returns *CouchbaseUserResource.
// Defined errors returned: ErrCouchbaseUserResourceDoesNotExist.
func GetCouchbaseUser(CouchbaseUserResourceName string, namespace string) (*CouchbaseUserResource, error) {
	if CouchbaseUserResourceName == "" {
		return nil, fmt.Errorf("get couchbase user: %w", ErrCouchbaseUserResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase user: %w", ErrNamespaceNotProvided)
	}

	CouchbaseUserResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseUser", CouchbaseUserResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbaseusers.couchbase.com \"%s\" not found", CouchbaseUserResourceName) {
			return nil, fmt.Errorf("get couchbase user: %w", ErrCouchbaseUserResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase user: %w", err)
	}

	var CouchbaseUserResource CouchbaseUserResource

	err = json.Unmarshal([]byte(CouchbaseUserResourceJSON), &CouchbaseUserResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase user: %w", err)
	}

	return &CouchbaseUserResource, nil
}

// GetCouchbaseUserResources gets the couchbase user resource information and returns the *CouchbaseUserResourceList containing the list of CouchbaseUserResources.
// If CouchbaseUserResourceNames = nil, then all the CouchbaseUserResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseUserResourceNamesDoesNotExist.
func GetCouchbaseUserResources(CouchbaseUserResourceNames []string, namespace string) (*CouchbaseUserResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase user resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseUserResourceNames == nil {
		CouchbaseUserResourceNamesList, err := GetCouchbaseUserResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase user resources: %w", err)
		}

		CouchbaseUserResourceNames = CouchbaseUserResourceNamesList
	}

	var CouchbaseUserResourceList CouchbaseUserResourceList

	// When we execute `get CouchbaseUser <single-user>`, then we receive a single couchbaseUser JSON instead of list of couchbaseUser JSONs.
	if len(CouchbaseUserResourceNames) == 1 {
		CouchbaseUserResource, err := GetCouchbaseUser(CouchbaseUserResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase user resources: %w", err)
		}

		CouchbaseUserResourceList.CouchbaseUserResources = append(CouchbaseUserResourceList.CouchbaseUserResources, CouchbaseUserResource)

		return &CouchbaseUserResourceList, nil
	}

	CouchbaseUserResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseUser", CouchbaseUserResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase user resources: %w", ErrCouchbaseUserResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase user resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseUserResourcesJSON), &CouchbaseUserResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase user resources: json unmarshal: %w", err)
	}

	return &CouchbaseUserResourceList, nil
}

// GetCouchbaseUsersMap gets the couchbase user resource information and returns the map[string]*CouchbaseUserResource which has the *CouchbaseUserResource for each couchbase user resource names in given list.
// If CouchbaseUserResourceNames = nil, then all the couchbase user resources in the namespace are taken into account.
func GetCouchbaseUserResourcesMap(CouchbaseUserResourceNames []string, namespace string) (map[string]*CouchbaseUserResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase user resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseUserResourceNames == nil {
		CouchbaseUserResourceNamesList, err := GetCouchbaseUserResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase user resources map: %w", err)
		}

		CouchbaseUserResourceNames = CouchbaseUserResourceNamesList
	}

	CouchbaseUserResourceMap := make(map[string]*CouchbaseUserResource)

	CouchbaseUserResourcesList, err := GetCouchbaseUserResources(CouchbaseUserResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase user resources map: %w", err)
	}

	for i := range CouchbaseUserResourcesList.CouchbaseUserResources {
		CouchbaseUserResourceMap[CouchbaseUserResourceNames[i]] = CouchbaseUserResourcesList.CouchbaseUserResources[i]
	}

	return CouchbaseUserResourceMap, nil
}
