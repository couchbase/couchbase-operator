package couchbaserolebindingresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseReplicationResourceNameNotProvided = errors.New("couchbase replication resource name is not provided")
	ErrNamespaceNotProvided                        = errors.New("namespace is not provided")
	ErrCouchbaseReplicationResourceDoesNotExist    = errors.New("couchbase replication resource does not exist")
	ErrNoCouchbaseReplicationResourcesInNamespace  = errors.New("no couchbase replication resources in namespace")
)

// GetCouchbaseReplicationResourceNames returns a slice of strings containing names of all couchbase replication resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseReplicationResourcesInNamespace.
func GetCouchbaseReplicationResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase replication resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseReplicationResourceNamesOutput, err := kubectl.Get("CouchbaseReplication").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase replication resource names: %w", err)
	}

	if CouchbaseReplicationResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase replication resource names: %w", ErrNoCouchbaseReplicationResourcesInNamespace)
	}

	CouchbaseReplicationResourceNames := strings.Split(CouchbaseReplicationResourceNamesOutput, "\n")
	for i := range CouchbaseReplicationResourceNames {
		// kubectl returns couchbase replication resource names as couchbasereplications.couchbase.com/replication-name. We remove the prefix "couchbasereplications.couchbase.com/"
		CouchbaseReplicationResourceNames[i] = strings.TrimPrefix(CouchbaseReplicationResourceNames[i], "couchbasereplications.couchbase.com/")
	}

	return CouchbaseReplicationResourceNames, nil
}

// GetCouchbaseReplication gets the couchbase replication resource information of the couchbase replication resource in the given namespace and returns *CouchbaseReplicationResource.
// Defined errors returned: ErrCouchbaseReplicationResourceDoesNotExist.
func GetCouchbaseReplication(CouchbaseReplicationResourceName string, namespace string) (*CouchbaseReplicationResource, error) {
	if CouchbaseReplicationResourceName == "" {
		return nil, fmt.Errorf("get couchbase replication: %w", ErrCouchbaseReplicationResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase replication: %w", ErrNamespaceNotProvided)
	}

	CouchbaseReplicationResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseReplication", CouchbaseReplicationResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasereplications.couchbase.com \"%s\" not found", CouchbaseReplicationResourceName) {
			return nil, fmt.Errorf("get couchbase user: %w", ErrCouchbaseReplicationResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase replication: %w", err)
	}

	var CouchbaseReplicationResource CouchbaseReplicationResource

	err = json.Unmarshal([]byte(CouchbaseReplicationResourceJSON), &CouchbaseReplicationResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase replication: %w", err)
	}

	return &CouchbaseReplicationResource, nil
}

// GetCouchbaseReplicationResources gets the couchbase replication resource information and returns the *CouchbaseReplicationResourceList containing the list of CouchbaseReplicationResources.
// If CouchbaseReplicationResourceNames = nil, then all the CouchbaseReplicationResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseReplicationResourceNamesDoesNotExist.
func GetCouchbaseReplicationResources(CouchbaseReplicationResourceNames []string, namespace string) (*CouchbaseReplicationResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase replication resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseReplicationResourceNames == nil {
		CouchbaseReplicationResourceNamesList, err := GetCouchbaseReplicationResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase replication resources: %w", err)
		}

		CouchbaseReplicationResourceNames = CouchbaseReplicationResourceNamesList
	}

	var CouchbaseReplicationResourceList CouchbaseReplicationResourceList

	// When we execute `get CouchbaseReplication <single-replication>`, then we receive a single couchbaseReplication JSON instead of list of couchbaseReplication JSONs.
	if len(CouchbaseReplicationResourceNames) == 1 {
		CouchbaseReplicationResource, err := GetCouchbaseReplication(CouchbaseReplicationResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase replication resources: %w", err)
		}

		CouchbaseReplicationResourceList.CouchbaseReplicationResources = append(CouchbaseReplicationResourceList.CouchbaseReplicationResources, CouchbaseReplicationResource)

		return &CouchbaseReplicationResourceList, nil
	}

	CouchbaseReplicationResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseReplication", CouchbaseReplicationResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase replication resources: %w", ErrCouchbaseReplicationResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase replication resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseReplicationResourcesJSON), &CouchbaseReplicationResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase replication resources: json unmarshal: %w", err)
	}

	return &CouchbaseReplicationResourceList, nil
}

// GetCouchbaseReplicationsMap gets the couchbase replication resource information and returns the map[string]*CouchbaseReplicationResource which has the *CouchbaseReplicationResource for each couchbase replication resource names in given list.
// If CouchbaseReplicationResourceNames = nil, then all the couchbase replication resources in the namespace are taken into account.
func GetCouchbaseReplicationResourcesMap(CouchbaseReplicationResourceNames []string, namespace string) (map[string]*CouchbaseReplicationResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase replication resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseReplicationResourceNames == nil {
		CouchbaseReplicationResourceNamesList, err := GetCouchbaseReplicationResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase replication resources map: %w", err)
		}

		CouchbaseReplicationResourceNames = CouchbaseReplicationResourceNamesList
	}

	CouchbaseReplicationResourceMap := make(map[string]*CouchbaseReplicationResource)

	CouchbaseReplicationResourcesList, err := GetCouchbaseReplicationResources(CouchbaseReplicationResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase replication resources map: %w", err)
	}

	for i := range CouchbaseReplicationResourcesList.CouchbaseReplicationResources {
		CouchbaseReplicationResourceMap[CouchbaseReplicationResourceNames[i]] = CouchbaseReplicationResourcesList.CouchbaseReplicationResources[i]
	}

	return CouchbaseReplicationResourceMap, nil
}
