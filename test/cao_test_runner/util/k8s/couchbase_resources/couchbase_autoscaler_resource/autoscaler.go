package couchbaseautoscalerresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCouchbaseAutoscalerResourceNameNotProvided = errors.New("couchbase autoscaler resource name is not provided")
	ErrNamespaceNotProvided                       = errors.New("namespace is not provided")
	ErrCouchbaseAutoscalerResourceDoesNotExist    = errors.New("couchbase autoscaler resource does not exist")
	ErrNoCouchbaseAutoscalerResourcesInNamespace  = errors.New("no couchbase autoscaler resources in namespace")
)

// GetCouchbaseAutoscalerResourceNames returns a slice of strings containing names of all couchbase autoscaler resource in the given namespace.
// Defined errors returned: ErrNoCouchbaseAutoscalerResourcesInNamespace.
func GetCouchbaseAutoscalerResourceNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase autoscaler resource names: %w", ErrNamespaceNotProvided)
	}

	couchbaseAutoscalerResourceNamesOutput, err := kubectl.Get("CouchbaseAutoscaler").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get couchbase autoscaler resource names: %w", err)
	}

	if couchbaseAutoscalerResourceNamesOutput == "" {
		return nil, fmt.Errorf("get couchbase autoscaler resource names: %w", ErrNoCouchbaseAutoscalerResourcesInNamespace)
	}

	couchbaseAutoscalerResourceNames := strings.Split(couchbaseAutoscalerResourceNamesOutput, "\n")
	for i := range couchbaseAutoscalerResourceNames {
		// kubectl returns couchbase autoscaler resource names as couchbaseautoscalers.couchbase.com/autoscaler-name. We remove the prefix "couchbaseautoscalers.couchbase.com/"
		couchbaseAutoscalerResourceNames[i] = strings.TrimPrefix(couchbaseAutoscalerResourceNames[i], "couchbaseautoscalers.couchbase.com/")
	}

	return couchbaseAutoscalerResourceNames, nil
}

// GetCouchbaseAutoscaler gets the couchbase autoscaler resource information of the couchbase autoscaler resource in the given namespace and returns *CouchbaseAutoScalerResource.
// Defined errors returned: ErrCouchbaseAutoScalerResourceDoesNotExist.
func GetCouchbaseAutoscaler(couchbaseAutoscalerResourceName string, namespace string) (*CouchbaseAutoScalerResource, error) {
	if couchbaseAutoscalerResourceName == "" {
		return nil, fmt.Errorf("get couchbase autoscaler: %w", ErrCouchbaseAutoscalerResourceNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get couchbase autoscaler: %w", ErrNamespaceNotProvided)
	}

	couchbaseAutoscalerResourceJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseAutoscaler", couchbaseAutoscalerResourceName).
		FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): couchbaseautoscalers.couchbase.com \"%s\" not found", couchbaseAutoscalerResourceName) {
			return nil, fmt.Errorf("get couchbase autoscaler: %w", ErrCouchbaseAutoscalerResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase autoscaler: %w", err)
	}

	var couchbaseAutoscalerResource CouchbaseAutoScalerResource

	err = json.Unmarshal([]byte(couchbaseAutoscalerResourceJSON), &couchbaseAutoscalerResource)
	if err != nil {
		return nil, fmt.Errorf("get couchbase autoscaler: %w", err)
	}

	return &couchbaseAutoscalerResource, nil
}

// GetCouchbaseAutoscalerResources gets the couchbase autoscaler resource information and returns the *CouchbaseAutoscalerResourceList containing the list of CouchbaseAutoscalerResources.
// If couchbaseAutoscalerResourceNames = nil, then all the couchbaseAutoscalerResources in the namespace are taken into account.
// Defined errors returned: ErrCouchbaseAutoscalerResourceNamesDoesNotExist.
func GetCouchbaseAutoscalerResources(couchbaseAutoscalerResourceNames []string, namespace string) (*CouchbaseAutoScalerResourceList, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase autoscaler resources: %w", ErrNamespaceNotProvided)
	}

	if couchbaseAutoscalerResourceNames == nil {
		couchbaseAutoscalerResourceNamesList, err := GetCouchbaseAutoscalerResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase autoscaler resources: %w", err)
		}

		couchbaseAutoscalerResourceNames = couchbaseAutoscalerResourceNamesList
	}

	var couchbaseAutoscalerResourceList CouchbaseAutoScalerResourceList

	// When we execute `get CouchbaseAutoscaler <single-autoscaler>`, then we receive a single couchbaseAutoscaler JSON instead of list of couchbaseAutoscaler JSONs.
	if len(couchbaseAutoscalerResourceNames) == 1 {
		couchbaseAutoscalerResource, err := GetCouchbaseAutoscaler(couchbaseAutoscalerResourceNames[0], namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase autoscaler resources: %w", err)
		}

		couchbaseAutoscalerResourceList.CouchbaseAutoScalerResources = append(couchbaseAutoscalerResourceList.CouchbaseAutoScalerResources, couchbaseAutoscalerResource)

		return &couchbaseAutoscalerResourceList, nil
	}

	couchbaseAutoscalerResourcesJSON, stderr, err := kubectl.GetByTypeAndName("CouchbaseAutoscaler", couchbaseAutoscalerResourceNames...).FormatOutput("json").InNamespace(namespace).Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get couchbase autoscaler resources: %w", ErrCouchbaseAutoscalerResourceDoesNotExist)
		}

		return nil, fmt.Errorf("get couchbase autoscaler resources: %w", err)
	}

	err = json.Unmarshal([]byte(couchbaseAutoscalerResourcesJSON), &couchbaseAutoscalerResourceList)
	if err != nil {
		return nil, fmt.Errorf("get couchbase autoscaler resources: json unmarshal: %w", err)
	}

	return &couchbaseAutoscalerResourceList, nil
}

// GetCouchbaseAutoscalersMap gets the couchbase autoscaler resource information and returns the map[string]*CouchbaseAutoScalerResource which has the *CouchbaseAutoScalerResource for each couchbase autoscaler resource names in given list.
// If couchbaseAutoscalerResourceNames = nil, then all the couchbase autoscaler resources in the namespace are taken into account.
func GetCouchbaseAutoscalerResourcesMap(couchbaseAutoscalerResourceNames []string, namespace string) (map[string]*CouchbaseAutoScalerResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get couchbase autoscaler resources map: %w", ErrNamespaceNotProvided)
	}

	if couchbaseAutoscalerResourceNames == nil {
		couchbaseAutoscalerResourceNamesList, err := GetCouchbaseAutoscalerResourceNames(namespace)
		if err != nil {
			return nil, fmt.Errorf("get couchbase autoscaler resources map: %w", err)
		}

		couchbaseAutoscalerResourceNames = couchbaseAutoscalerResourceNamesList
	}

	couchbaseAutoscalerResourceMap := make(map[string]*CouchbaseAutoScalerResource)

	couchbaseAutoscalerResourcesList, err := GetCouchbaseAutoscalerResources(couchbaseAutoscalerResourceNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get couchbase autoscaler resources map: %w", err)
	}

	for i := range couchbaseAutoscalerResourcesList.CouchbaseAutoScalerResources {
		couchbaseAutoscalerResourceMap[couchbaseAutoscalerResourceNames[i]] = couchbaseAutoscalerResourcesList.CouchbaseAutoScalerResources[i]
	}

	return couchbaseAutoscalerResourceMap, nil

}
