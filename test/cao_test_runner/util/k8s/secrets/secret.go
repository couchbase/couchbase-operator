package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

var (
	ErrSecretNameNotProvided = errors.New("secret name is not provided")
	ErrNamespaceNotProvided  = errors.New("namespace is not provided")
)

func GetSecretNames(namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("get secret names: %w", ErrNamespaceNotProvided)
	}

	secretNamesOutput, err := kubectl.Get("secrets").FormatOutput("name").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get secret names: %w", err)
	}

	secretNames := strings.Split(secretNamesOutput, "\n")
	for i := range secretNames {
		// kubectl returns secret names as secret/secret-name. We remove the prefix "secret/"
		secretNames[i] = strings.TrimPrefix(secretNames[i], "secret/")
	}

	return secretNames, nil
}

// GetSecret gets the k8s secret information in the given namespace and returns *Secret.
func GetSecret(secretName string, namespace string) (*Secret, error) {
	if secretName == "" {
		return nil, fmt.Errorf("get secret: %w", ErrSecretNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get secret: %w", ErrNamespaceNotProvided)
	}

	secretJSON, err := kubectl.GetByTypeAndName("secrets", secretName).FormatOutput("json").InNamespace(namespace).Output()
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	var secret Secret

	err = json.Unmarshal([]byte(secretJSON), &secret)
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	return &secret, nil
}
