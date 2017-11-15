package e2eutil

import (
	"testing"

	"github.com/couchbaselabs/couchbase-operator/pkg/cluster"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateSecret(t *testing.T, kubeClient kubernetes.Interface, namespace string, secretSpec *v1.Secret) (*v1.Secret, error) {
	secret, err := kubeClient.CoreV1().Secrets(namespace).Create(secretSpec)
	if err != nil {
		return nil, err
	}
	t.Logf("created secret: %s", secret.Name)

	return secret, nil
}

func DeleteSecret(t *testing.T, kubeClient kubernetes.Interface, namespace string, secretName string, options *metav1.DeleteOptions) error {
	t.Logf("deleting secret: %s", secretName)
	return kubeClient.CoreV1().Secrets(namespace).Delete(secretName, options)
}

func GetSecret(t *testing.T, kubeClient kubernetes.Interface, namespace string, secretName string) (*v1.Secret, error) {
	t.Logf("get secret: %s", secretName)
	opts := metav1.GetOptions{}
	return kubeClient.CoreV1().Secrets(namespace).Get(secretName, opts)
}

// Use username and password from secret store
func GetClusterAuth(t *testing.T, kubeClient kubernetes.Interface, namespace string, secretName string) (error, string, string) {

	var username string
	var password string

	secret, err := GetSecret(t, kubeClient, namespace, secretName)
	if err != nil {
		return err, username, password
	}

	data := secret.Data
	if val, ok := data[constants.AuthSecretUsernameKey]; ok {
		username = string(val[:])
	} else {
		return cluster.ErrSecretMissingUsername(secretName), username, password
	}
	if val, ok := data[constants.AuthSecretPasswordKey]; ok {
		password = string(val[:])
	} else {
		return cluster.ErrSecretMissingPassword(secretName), username, password
	}

	return nil, username, password
}
