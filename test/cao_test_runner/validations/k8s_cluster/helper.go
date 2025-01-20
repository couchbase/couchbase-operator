package k8sclustervalidator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrClusterKubeconfigDoesntExists = errors.New("cluster kubeconfig doesn't exists")
)

func contains(array []string, str string) bool {
	for _, item := range array {
		if item == str {
			return true
		}
	}

	return false
}

func containsStrPtr(array []*string, str *string) bool {
	for _, item := range array {
		if *item == *str {
			return true
		}
	}

	return false
}

func checkIfClusterExistsInKubeconfig(clusterName string) error {
	out, _, err := kubectl.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("check if cluster exists in kubeconfig: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	if !contains(allClusters, clusterName) {
		return fmt.Errorf("check if cluster exists in kubeconfig: %w", ErrClusterKubeconfigDoesntExists)
	}

	return nil
}
