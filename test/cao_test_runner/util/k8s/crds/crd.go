package crds

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrCRDNameNotProvided = errors.New("crd name is not provided")
	ErrCRDDoesNotExist    = errors.New("crd does not exist")
	ErrNoCRDsInCluster    = errors.New("no crds in cluster")
)

// GetCRDNames returns a slice of strings containing names of all crds in the given cluster.
// Defined errors returned: ErrNoCRDsInCluster.
func GetCRDNames() ([]string, error) {
	crdNamesOutput, err := kubectl.Get("crds").FormatOutput("name").Output()
	if err != nil {
		return nil, fmt.Errorf("get crd names: %w", err)
	}

	if crdNamesOutput == "" {
		return nil, fmt.Errorf("get crd names: %w", ErrNoCRDsInCluster)
	}

	crdNames := strings.Split(crdNamesOutput, "\n")
	for i := range crdNames {
		// kubectl returns crd names as customresourcedefinition.apiextensions.k8s.io/crd-name.
		// We remove the prefix "customresourcedefinition.apiextensions.k8s.io/"
		crdNames[i] = strings.TrimPrefix(crdNames[i], "customresourcedefinition.apiextensions.k8s.io/")
	}

	return crdNames, nil
}

// GetCRD gets the crd information of the crd in the given cluster and returns *CRD.
// Defined errors returned: ErrCRDDoesNotExist.
func GetCRD(crdName string) (*CRD, error) {
	if crdName == "" {
		return nil, fmt.Errorf("get crd: %w", ErrCRDNameNotProvided)
	}

	crdJSON, stderr, err := kubectl.GetByTypeAndName("crds", crdName).FormatOutput("json").Exec(true, false)
	if err != nil {
		if strings.TrimSpace(stderr) == fmt.Sprintf("Error from server (NotFound): customresourcedefinitions.apiextensions.k8s.io \"%s\" not found", crdName) {
			return nil, fmt.Errorf("get crd: %w", ErrCRDDoesNotExist)
		}

		return nil, fmt.Errorf("get crd: %w", err)
	}

	var crd CRD

	err = json.Unmarshal([]byte(crdJSON), &crd)
	if err != nil {
		return nil, fmt.Errorf("get crd: %w", err)
	}

	return &crd, nil
}

// GetCRDs gets the crd information and returns the *CRDList containing the list of CRDs.
// If crdNames = nil, then all the crds in the cluster are taken into account.
// Defined errors returned: ErrCRDDoesNotExist.
func GetCRDs(crdNames []string) (*CRDList, error) {
	if len(crdNames) == 0 {
		crdNamesList, err := GetCRDNames()
		if err != nil {
			return nil, fmt.Errorf("get crds: %w", err)
		}

		crdNames = crdNamesList
	}

	var crdList CRDList

	// When we execute `get crds <single-crd>`, then we receive a single CRD JSON instead of list of CRD JSONs.
	if len(crdNames) == 1 {
		crd, err := GetCRD(crdNames[0])
		if err != nil {
			return nil, fmt.Errorf("get crds: %w", err)
		}

		crdList.CRDs = append(crdList.CRDs, crd)

		return &crdList, nil
	}

	crdsJSON, stderr, err := kubectl.GetByTypeAndName("crds", crdNames...).FormatOutput("json").Exec(true, false)
	if err != nil {
		if strings.Contains(stderr, "Error from server (NotFound)") {
			return nil, fmt.Errorf("get crds: %w", ErrCRDDoesNotExist)
		}

		return nil, fmt.Errorf("get crds: %w", err)
	}

	err = json.Unmarshal([]byte(crdsJSON), &crdList)
	if err != nil {
		return nil, fmt.Errorf("get crds: json unmarshal: %w", err)
	}

	return &crdList, nil
}

// GetCRDsMap gets the crd information and returns the map[string]*CRD which has the *CRD for each crd names in given list.
// If crdNames = nil, then all the crds in the cluster are taken into account.
func GetCRDsMap(crdNames []string) (map[string]*CRD, error) {
	if len(crdNames) == 0 {
		crdNamesList, err := GetCRDNames()
		if err != nil {
			return nil, fmt.Errorf("get crds map: %w", err)
		}

		crdNames = crdNamesList
	}

	crdMap := make(map[string]*CRD)

	crdsList, err := GetCRDs(crdNames)
	if err != nil {
		return nil, fmt.Errorf("get crds map: %w", err)
	}

	for i := range crdsList.CRDs {
		crdMap[crdNames[i]] = crdsList.CRDs[i]
	}

	return crdMap, nil
}
