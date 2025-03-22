package k8sclustervalidator

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrClusterKubeconfigDoesntExists = errors.New("cluster kubeconfig doesn't exists")
	ErrValueNotStruct                = errors.New("value is not a struct")
	ErrFieldNotNil                   = errors.New("field is not nil")
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
	out, _, err := kubectl.GetClusters().Exec(true, false)
	if err != nil {
		return fmt.Errorf("check if cluster exists in kubeconfig: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	if !contains(allClusters, clusterName) {
		return fmt.Errorf("check if cluster exists in kubeconfig: %w", ErrClusterKubeconfigDoesntExists)
	}

	return nil
}

func checkConfigIsNil(v interface{}) (bool, error) {
	val := reflect.ValueOf(v)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return false, fmt.Errorf("check config nil: %w", ErrValueNotStruct)
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)

		if field.Kind() == reflect.Ptr && field.IsNil() {
			continue
		}
		if field.Kind() == reflect.Slice && field.Len() == 0 {
			continue
		}

		return false, fmt.Errorf("check config nil: field %s is not nil: %w", val.Type().Field(i).Name, ErrFieldNotNil)
	}

	return true, nil
}
