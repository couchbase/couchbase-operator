package couchbasebackuprestoreresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseBackupRestoreResourceNameNotProvided = errors.New("couchbase backup restore resource name is not provided")
	ErrNamespaceNotProvided                          = errors.New("namespace is not provided")
	ErrCouchbaseBackupRestoreResourceDoesNotExist    = errors.New("couchbase backup restore resource does not exist")
	ErrNoCouchbaseBackupRestoreResourcesInNamespace  = errors.New("no couchbase backup restore resources in namespace")
)

// GetCouchbaseBackupRestoreResourceNames returns a slice of strings containing names of all couchbase backup restore resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseBackupRestoreResourcesInNamespace.
func GetCouchbaseBackupRestoreResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup restore resource names: %w", ErrNamespaceNotProvided)
	}

	CouchbaseBackupRestoreResourceNamesOutput, err := kubectl.Get("CouchbaseBackupRestore").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup restore resource names: %w", err)
	}

	if CouchbaseBackupRestoreResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase backup restore resource names: %w", ErrNoCouchbaseBackupRestoreResourcesInNamespace)
	}

	CouchbaseBackupRestoreResourceNames := strings.Split(CouchbaseBackupRestoreResourceNamesOutput, "\n")
	for i := range CouchbaseBackupRestoreResourceNames {
		// kubectl returns couchbase backup restore resource names as couchbasebackuprestores.couchbase.com/backup-restore-name. We remove the prefix "couchbasebackuprestores.couchbase.com/"
		CouchbaseBackupRestoreResourceNames[i] = strings.TrimPrefix(CouchbaseBackupRestoreResourceNames[i], "couchbasebackuprestores.couchbase.com/")
	}

	return CouchbaseBackupRestoreResourceNames, nil
}

// GetCouchbaseBackupRestore gets the couchbase backup restore resource information of the couchbase backup restore resource in the given namespace and returns *CouchbaseBackupRestoreResource.
// Defined errors returned: ErrCouchbaseBackupRestoreResourceDoesNotExist.
func GetCouchbaseBackupRestore(CouchbaseBackupRestoreResourceName string, namespace string) (*CouchbaseBackupRestoreResource, error) {
	if CouchbaseBackupRestoreResourceName == "" {
		return nil, fmt.Errorf("get couchbase backup restore: %w", ErrCouchbaseBackupRestoreResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup restore: %w", ErrNamespaceNotProvided)
	}

	CouchbaseBackupRestoreResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseBackupRestore", CouchbaseBackupRestoreResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbasebackuprestores.couchbase.com \"%s\" not found", CouchbaseBackupRestoreResourceName) {
			return nil, fmt.Errorf("get couchbase backup restore: %w", ErrCouchbaseBackupRestoreResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase backup restore: %w", err)
	}

	var CouchbaseBackupRestoreResource CouchbaseBackupRestoreResource

	err = json.Unmarshal([]byte(CouchbaseBackupRestoreResourceJSON), &CouchbaseBackupRestoreResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup restore: %w", err)
	}

	return &CouchbaseBackupRestoreResource, nil
}

// GetCouchbaseBackupRestoreResources gets the couchbase backup restore resource information and returns the *CouchbaseBackupRestoreResourceList containing the list of CouchbaseBackupRestoreResources.
// If CouchbaseBackupRestoreResourceNames = nil, then all the CouchbaseBackupRestoreResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseBackupRestoreResourceNamesDoesNotExist.
func GetCouchbaseBackupRestoreResources(CouchbaseBackupRestoreResourceNames []string, namespace string) (*CouchbaseBackupRestoreResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup restore resources: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseBackupRestoreResourceNames == nil {
		CouchbaseBackupRestoreResourceNamesList, err := GetCouchbaseBackupRestoreResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase backup restore resources: %w", err)
		}

		CouchbaseBackupRestoreResourceNames = CouchbaseBackupRestoreResourceNamesList
	}

	var CouchbaseBackupRestoreResourceList CouchbaseBackupRestoreResourceList

	// When we execute `get CouchbaseBackupRestore <single-backup-restore>`, then we receive a single couchbaseBackupRestore JSON instead of list of couchbaseBackupRestore JSONs.
	if len(CouchbaseBackupRestoreResourceNames) == 1 {
		CouchbaseBackupRestoreResource, err := GetCouchbaseBackupRestore(CouchbaseBackupRestoreResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase backup restore resources: %w", err)
		}

		CouchbaseBackupRestoreResourceList.CouchbaseBackupRestoreResources = append(CouchbaseBackupRestoreResourceList.CouchbaseBackupRestoreResources, CouchbaseBackupRestoreResource)

		return &CouchbaseBackupRestoreResourceList, nil
	}

	CouchbaseBackupRestoreResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseBackupRestore", CouchbaseBackupRestoreResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase backup restore resources: %w", ErrCouchbaseBackupRestoreResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase backup restore resources: %w", err)
	}

	err = json.Unmarshal([]byte(CouchbaseBackupRestoreResourcesJSON), &CouchbaseBackupRestoreResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup restore resources: json unmarshal: %w", err)
	}

	return &CouchbaseBackupRestoreResourceList, nil
}

// GetCouchbaseBackupRestoresMap gets the couchbase backup restore resource information and returns the map[string]*CouchbaseBackupRestoreResource which has the *CouchbaseBackupRestoreResource for each couchbase backup restore resource names in given list.
// If CouchbaseBackupRestoreResourceNames = nil, then all the couchbase backup restore resources in the namespace are taken into account.
func GetCouchbaseBackupRestoreResourcesMap(CouchbaseBackupRestoreResourceNames []string, namespace string) (map[string]*CouchbaseBackupRestoreResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase backup restore resources map: %w", ErrNamespaceNotProvided)
	}

	if CouchbaseBackupRestoreResourceNames == nil {
		CouchbaseBackupRestoreResourceNamesList, err := GetCouchbaseBackupRestoreResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase backup restore resources map: %w", err)
		}

		CouchbaseBackupRestoreResourceNames = CouchbaseBackupRestoreResourceNamesList
	}

	CouchbaseBackupRestoreResourceMap := make(map[string]*CouchbaseBackupRestoreResource)

	CouchbaseBackupRestoreResourcesList, err := GetCouchbaseBackupRestoreResources(CouchbaseBackupRestoreResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase backup restore resources map: %w", err)
	}

	for i := range CouchbaseBackupRestoreResourcesList.CouchbaseBackupRestoreResources {
		CouchbaseBackupRestoreResourceMap[CouchbaseBackupRestoreResourceNames[i]] = CouchbaseBackupRestoreResourcesList.CouchbaseBackupRestoreResources[i]
	}

	return CouchbaseBackupRestoreResourceMap, nil

}
