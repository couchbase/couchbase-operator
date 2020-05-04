package e2eutil

import (
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NewLDAPServer creates a new LDAP server.
func NewLDAPServer(kubeClient kubernetes.Interface, namespace string, pod *v1.Pod) (*v1.Pod, error) {
	return kubeClient.CoreV1().Pods(namespace).Create(pod)
}

// MustNewLDAPServer create LDAP server or dies trying.
func MustNewLDAPServer(t *testing.T, kubeClient kubernetes.Interface, namespace string, pod *v1.Pod) *v1.Pod {
	pod, err := NewLDAPServer(kubeClient, namespace, pod)
	if err != nil {
		Die(t, err)
	}
	return pod
}

// NewLDAPService creates headless service for accessing LDAP server.
func NewLDAPService(kubeClient kubernetes.Interface, namespace string, service *v1.Service) (*v1.Service, error) {
	return kubeClient.CoreV1().Services(namespace).Create(service)
}

// MustNewLDAPService creates LDAP service or dies trying.
func MustNewLDAPService(t *testing.T, kubeClient kubernetes.Interface, namespace string, service *v1.Service) *v1.Service {
	service, err := NewLDAPService(kubeClient, namespace, service)
	if err != nil {
		Die(t, err)
	}
	return service
}

// Delete LDAP Service.
func DeleteLDAPService(kubeClient kubernetes.Interface, namespace string) error {
	opts := metav1.ListOptions{LabelSelector: "group=" + constants.LDAPLabelSelector}
	svcList, err := kubeClient.CoreV1().Services(namespace).List(opts)
	if err != nil {
		return err
	}
	for _, service := range svcList.Items {
		err := kubeClient.CoreV1().Services(namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
		if (err != nil) && !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

// Delete LDAP Server.
func DeleteLDAPServer(kubeClient kubernetes.Interface, namespace string) error {
	opts := metav1.ListOptions{LabelSelector: "group=" + constants.LDAPLabelSelector}
	podList, err := kubeClient.CoreV1().Pods(namespace).List(opts)
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		err := kubeClient.CoreV1().Pods(namespace).Delete(pod.Name, metav1.NewDeleteOptions(0))
		if (err != nil) && !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

// Delete LDAP Secret.
func DeleteLDAPSecret(kubeClient kubernetes.Interface, namespace string) error {
	opts := metav1.ListOptions{LabelSelector: "group=" + constants.LDAPLabelSelector}
	secretList, err := kubeClient.CoreV1().Secrets(namespace).List(opts)
	if err != nil {
		return err
	}
	for _, secret := range secretList.Items {
		err := kubeClient.CoreV1().Secrets(namespace).Delete(secret.Name, metav1.NewDeleteOptions(0))
		if (err != nil) && !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

// Clean up ldap service and server.
func CleanLDAPResources(kubeClient kubernetes.Interface, namespace string) error {
	if err := DeleteLDAPServer(kubeClient, namespace); err != nil {
		return nil
	}
	if err := DeleteLDAPSecret(kubeClient, namespace); err != nil {
		return nil
	}
	return DeleteLDAPService(kubeClient, namespace)
}
