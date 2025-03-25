package couchbasemigrationreplicationresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseMigrationReplicationResourceNameNotProvided = errors.New("couchbase migration replication resource name is not provided")
	ErrNamespaceNotProvided                                 = errors.New("namespace is not provided")
	ErrCouchbaseMigrationReplicationResourceDoesNotExist    = errors.New("couchbase migration replication resource does not exist")
	ErrNoCouchbaseMigrationReplicationResourcesInNamespace  = errors.New("no couchbase migration replication resources in namespace")
)

// GetCouchbaseMigrationReplicationResourceNames returns a slice of strings containing names of all couchbase migration replication resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseMigrationReplicationResourcesInNamespace.
func GetCouchbaseMigrationReplicationResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase migration replication resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseMigrationReplicationResourceNamesOutput, err := kubectl.Get("CouchbaseMigrationReplication").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase migration replication resource names: %w", err)
	}

	if CouchbaseMigrationReplicationResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase migration replication resource names: %w", ErrNoCouchbaseMigrationReplicationResourcesInNamespace)
	}

	CouchbaseMigrationReplicationResourceNames := strings.Split(CouchbaseMigrationReplicationResourceNamesOutput, "\n")
	for i := range CouchbaseMigrationReplicationResourceNames {
		// kubectl returns couchbase migration replication resource names as couchbasemigrationreplications.couchbase.com/migration-replication-name. We remove the prefix "couchbasemigrationreplications.couchbase.com/"
		CouchbaseMigrationReplicationResourceNames[i] = strings.TrimPrefix(CouchbaseMigrationReplicationResourceNames[i], "couchbasemigrationreplications.couchbase.com/")
	}

	return CouchbaseMigrationReplicationResourceNames, nil
}

// GetCouchbaseMigrationReplication gets the couchbase migration replication resource information of the couchbase migration replication resource in the given namespace and returns *CouchbaseMigrationReplicationResource.
// Defined errors returned: ErrCouchbaseMigrationReplicationResourceDoesNotExist.
func GetCouchbaseMigrationReplication(CouchbaseMigrationReplicationResourceName string, namespace string) (*CouchbaseMigrationReplicationResource, error) {
	if CouchbaseMigrationReplicationResourceName == "" {
		return nil, fmt.Errorf("get couchbase migration replication: %w", ErrCouchbaseMigrationReplicationResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase migration replication: %w", ErrNamespaceNotProvided)
	}

	CouchbaseMigrationReplicationResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseMigrationReplication", CouchbaseMigrationReplicationResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasemigrationreplications.couchbase.com \"%s\" not found", CouchbaseMigrationReplicationResourceName) {
			return nil, fmt.Errorf("get couchbase migration replication: %w", ErrCouchbaseMigrationReplicationResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase migration replication: %w", err)
	}

	var CouchbaseMigrationReplicationResource CouchbaseMigrationReplicationResource

	err = json.Unmarshal([]byte(CouchbaseMigrationReplicationResourceJSON), &CouchbaseMigrationReplicationResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase migration replication: %w", err)
	}

	return &CouchbaseMigrationReplicationResource, nil
}

// GetCouchbaseMigrationReplicationResources gets the couchbase migration replication resource information and returns the *CouchbaseMigrationReplicationResourceList containing the list of CouchbaseMigrationReplicationResources.
// If CouchbaseMigrationReplicationResourceNames = nil, then all the CouchbaseMigrationReplicationResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseMigrationReplicationResourceNamesDoesNotExist.
func GetCouchbaseMigrationReplicationResources(CouchbaseMigrationReplicationResourceNames []string, namespace string) (*CouchbaseMigrationReplicationResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase migration replication resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseMigrationReplicationResourceNames == nil {
		CouchbaseMigrationReplicationResourceNamesList, err := GetCouchbaseMigrationReplicationResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase migration replication resources: %w", err)
		}

		CouchbaseMigrationReplicationResourceNames = CouchbaseMigrationReplicationResourceNamesList
	}

	var CouchbaseMigrationReplicationResourceList CouchbaseMigrationReplicationResourceList

	// When we execute `get CouchbaseMigrationReplication <single-migration-replication>`, then we receive a single couchbaseMigrationReplication JSON instead of list of couchbaseMigrationReplication JSONs.
	if len(CouchbaseMigrationReplicationResourceNames) == 1 {
		CouchbaseMigrationReplicationResource, err := GetCouchbaseMigrationReplication(CouchbaseMigrationReplicationResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase migration replication resources: %w", err)
		}

		CouchbaseMigrationReplicationResourceList.CouchbaseMigrationReplicationResources = append(CouchbaseMigrationReplicationResourceList.CouchbaseMigrationReplicationResources, CouchbaseMigrationReplicationResource)

		return &CouchbaseMigrationReplicationResourceList, nil
	}

	CouchbaseMigrationReplicationResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseMigrationReplication", CouchbaseMigrationReplicationResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase migration replication resources: %w", ErrCouchbaseMigrationReplicationResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase migration replication resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseMigrationReplicationResourcesJSON), &CouchbaseMigrationReplicationResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase migration replication resources: json unmarshal: %w", err)
	}

	return &CouchbaseMigrationReplicationResourceList, nil
}

// GetCouchbaseMigrationReplicationsMap gets the couchbase migration replication resource information and returns the map[string]*CouchbaseMigrationReplicationResource which has the *CouchbaseMigrationReplicationResource for each couchbase migration replication resource names in given list.
// If CouchbaseMigrationReplicationResourceNames = nil, then all the couchbase migration replication resources in the namespace are taken into account.
func GetCouchbaseMigrationReplicationResourcesMap(CouchbaseMigrationReplicationResourceNames []string, namespace string) (map[string]*CouchbaseMigrationReplicationResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase migration replication resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseMigrationReplicationResourceNames == nil {
		CouchbaseMigrationReplicationResourceNamesList, err := GetCouchbaseMigrationReplicationResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase migration replication resources map: %w", err)
		}

		CouchbaseMigrationReplicationResourceNames = CouchbaseMigrationReplicationResourceNamesList
	}

	CouchbaseMigrationReplicationResourceMap := make(map[string]*CouchbaseMigrationReplicationResource)

	CouchbaseMigrationReplicationResourcesList, err := GetCouchbaseMigrationReplicationResources(CouchbaseMigrationReplicationResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase migration replication resources map: %w", err)
	}

	for i := range CouchbaseMigrationReplicationResourcesList.CouchbaseMigrationReplicationResources {
		CouchbaseMigrationReplicationResourceMap[CouchbaseMigrationReplicationResourceNames[i]] = CouchbaseMigrationReplicationResourcesList.CouchbaseMigrationReplicationResources[i]
	}

	return CouchbaseMigrationReplicationResourceMap, nil
}
