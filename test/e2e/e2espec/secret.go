package e2espec

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	basicSecretData = map[string][]byte{
		"username": []byte("Administrator"),
		"password": []byte("password"),
	}
)

func NewDefaultSecret(namespace string) *v1.Secret {
	return NewSecret(namespace, "basic-test-secret", basicSecretData)
}

func NewSecret(namespace, name string, data map[string][]byte) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: data,
	}
}
