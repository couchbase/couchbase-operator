package e2eutil

import (
	"testing"

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
