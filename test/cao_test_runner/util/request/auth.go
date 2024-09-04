package requestutils

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/secrets"
)

var (
	ErrCBSecretNameNotProvided = errors.New("cb cluster secret name not provided")
)

// GetCBClusterAuth retrieves the CB cluster auth details from the k8s secret.
func GetCBClusterAuth(cbSecretName, namespace string) (*HTTPAuth, error) {
	if cbSecretName == "" {
		return nil, fmt.Errorf("get cb cluster auth: %w", ErrCBSecretNameNotProvided)
	}

	if namespace == "" {
		return nil, fmt.Errorf("get cb cluster auth: %w", secrets.ErrNamespaceNotProvided)
	}

	cbSecret, err := secrets.GetSecret(cbSecretName, namespace)
	if err != nil {
		return nil, fmt.Errorf("get cb cluster auth: %w", err)
	}

	username, err := base64.StdEncoding.DecodeString(cbSecret.Data["username"])
	if err != nil {
		return nil, fmt.Errorf("decode cb cluster auth: %w", err)
	}

	password, err := base64.StdEncoding.DecodeString(cbSecret.Data["password"])
	if err != nil {
		return nil, fmt.Errorf("decode cb cluster auth: %w", err)
	}

	return &HTTPAuth{
		Username: string(username),
		Password: string(password),
	}, nil
}
