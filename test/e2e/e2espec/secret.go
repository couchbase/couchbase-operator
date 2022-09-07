package e2espec

import (
	"github.com/couchbase/couchbase-operator/test/e2e/constants"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var BasicSecretData = map[string][]byte{
	constants.SecretUsernameKey: []byte(constants.CbClusterUsername),
	constants.SecretPasswordKey: []byte(constants.CbClusterPassword),
}

func NewDefaultSecret(namespace string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      constants.KubeTestSecretName,
		},
		Data: BasicSecretData,
	}
}
