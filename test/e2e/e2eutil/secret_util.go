package e2eutil

import (
	"testing"

	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateSecret(kubeClient kubernetes.Interface, namespace string, secretSpec *v1.Secret) (*v1.Secret, error) {
	return kubeClient.CoreV1().Secrets(namespace).Create(secretSpec)
}

func DeleteSecret(kubeClient kubernetes.Interface, namespace string, secretName string, options *metav1.DeleteOptions) error {
	return kubeClient.CoreV1().Secrets(namespace).Delete(secretName, options)
}

func GetSecret(kubeClient kubernetes.Interface, namespace string, secretName string) (*v1.Secret, error) {
	return kubeClient.CoreV1().Secrets(namespace).Get(secretName, metav1.GetOptions{})
}

// Use username and password from secret store
func GetClusterAuth(t *testing.T, kubeClient kubernetes.Interface, namespace string, secretName string) (error, string, string) {

	var username string
	var password string

	secret, err := GetSecret(kubeClient, namespace, secretName)
	if err != nil {
		return err, username, password
	}

	data := secret.Data
	if val, ok := data[constants.AuthSecretUsernameKey]; ok {
		username = string(val[:])
	} else {
		return cberrors.ErrSecretMissingUsername{Reason: secretName}, username, password
	}
	if val, ok := data[constants.AuthSecretPasswordKey]; ok {
		password = string(val[:])
	} else {
		return cberrors.ErrSecretMissingPassword{Reason: secretName}, username, password
	}

	return nil, username, password
}
