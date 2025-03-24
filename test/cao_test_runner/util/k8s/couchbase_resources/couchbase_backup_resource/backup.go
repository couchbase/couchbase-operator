package couchbasebackupresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseBackupResourceNameNotProvided = errors.New("couchbase backup resource name is not provided")
	ErrNamespaceNotProvided                   = errors.New("namespace is not provided")
	ErrCouchbaseBackupResourceDoesNotExist    = errors.New("couchbase backup resource does not exist")
	ErrNoCouchbaseBackupResourcesInNamespace  = errors.New("no couchbase backup resources in namespace")
)

// GetCouchbaseBackupResourceNames returns a slice of strings containing names of all couchbase backup resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseBackupResourcesInNamespace.
func GetCouchbaseBackupResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseBackupResourceNamesOutput, err := kubectl.Get("CouchbaseBackup").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup resource names: %w", err)
	}

	if CouchbaseBackupResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase backup resource names: %w", ErrNoCouchbaseBackupResourcesInNamespace)
	}

	CouchbaseBackupResourceNames := strings.Split(CouchbaseBackupResourceNamesOutput, "\n")
	for i := range CouchbaseBackupResourceNames {
		// kubectl returns couchbase backup resource names as couchbasebackups.couchbase.com/backup-name. We remove the prefix "couchbasebackups.couchbase.com/"
		CouchbaseBackupResourceNames[i] = strings.TrimPrefix(CouchbaseBackupResourceNames[i], "couchbasebackups.couchbase.com/")
	}

	return CouchbaseBackupResourceNames, nil
}

// GetCouchbaseBackup gets the couchbase backup resource information of the couchbase backup resource in the given namespace and returns *CouchbaseBackupResource.
// Defined errors returned: ErrCouchbaseBackupResourceDoesNotExist.
func GetCouchbaseBackup(CouchbaseBackupResourceName string, namespace string) (*CouchbaseBackupResource, error) {
	if CouchbaseBackupResourceName == "" {
		return nil, fmt.Errorf("get couchbase backup: %w", ErrCouchbaseBackupResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup: %w", ErrNamespaceNotProvided)
	}

	CouchbaseBackupResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseBackup", CouchbaseBackupResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasebackups.couchbase.com \"%s\" not found", CouchbaseBackupResourceName) {
			return nil, fmt.Errorf("get couchbase backup: %w", ErrCouchbaseBackupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase backup: %w", err)
	}

	var CouchbaseBackupResource CouchbaseBackupResource

	err = json.Unmarshal([]byte(CouchbaseBackupResourceJSON), &CouchbaseBackupResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup: %w", err)
	}

	return &CouchbaseBackupResource, nil
}

// GetCouchbaseBackupResources gets the couchbase backup resource information and returns the *CouchbaseBackupResourceList containing the list of CouchbaseBackupResources.
// If CouchbaseBackupResourceNames = nil, then all the CouchbaseBackupResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseBackupResourceNamesDoesNotExist.
func GetCouchbaseBackupResources(CouchbaseBackupResourceNames []string, namespace string) (*CouchbaseBackupResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseBackupResourceNames == nil {
		CouchbaseBackupResourceNamesList, err := GetCouchbaseBackupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase backup resources: %w", err)
		}

		CouchbaseBackupResourceNames = CouchbaseBackupResourceNamesList
	}

	var CouchbaseBackupResourceList CouchbaseBackupResourceList

	// When we execute `get CouchbaseBackup <single-backup>`, then we receive a single couchbaseBackup JSON instead of list of couchbaseBackup JSONs.
	if len(CouchbaseBackupResourceNames) == 1 {
		CouchbaseBackupResource, err := GetCouchbaseBackup(CouchbaseBackupResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase backup resources: %w", err)
		}

		CouchbaseBackupResourceList.CouchbaseBackupResources = append(CouchbaseBackupResourceList.CouchbaseBackupResources, CouchbaseBackupResource)

		return &CouchbaseBackupResourceList, nil
	}

	CouchbaseBackupResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseBackup", CouchbaseBackupResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase backup resources: %w", ErrCouchbaseBackupResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase backup resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseBackupResourcesJSON), &CouchbaseBackupResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup resources: json unmarshal: %w", err)
	}

	return &CouchbaseBackupResourceList, nil
}

// GetCouchbaseBackupsMap gets the couchbase backup resource information and returns the map[string]*CouchbaseBackupResource which has the *CouchbaseBackupResource for each couchbase backup resource names in given list.
// If CouchbaseBackupResourceNames = nil, then all the couchbase backup resources in the namespace are taken into account.
func GetCouchbaseBackupResourcesMap(CouchbaseBackupResourceNames []string, namespace string) (map[string]*CouchbaseBackupResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseBackupResourceNames == nil {
		CouchbaseBackupResourceNamesList, err := GetCouchbaseBackupResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase backup resources map: %w", err)
		}

		CouchbaseBackupResourceNames = CouchbaseBackupResourceNamesList
	}

	CouchbaseBackupResourceMap := make(map[string]*CouchbaseBackupResource)

	CouchbaseBackupResourcesList, err := GetCouchbaseBackupResources(CouchbaseBackupResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup resources map: %w", err)
	}

	for i := range CouchbaseBackupResourcesList.CouchbaseBackupResources {
		CouchbaseBackupResourceMap[CouchbaseBackupResourceNames[i]] = CouchbaseBackupResourcesList.CouchbaseBackupResources[i]
	}

	return CouchbaseBackupResourceMap, nil
}
