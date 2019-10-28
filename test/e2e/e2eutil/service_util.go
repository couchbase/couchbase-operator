package e2eutil

import (
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"testing"
)

func CreateService(kubeClient kubernetes.Interface, namespace string, service *v1.Service) (*v1.Service, error) {
	service, err := kubeClient.CoreV1().Services(namespace).Create(service)
	if err != nil {
		return nil, err
	}
	return service, nil
}

func UpdateService(kubeClient kubernetes.Interface, namespace string, service *v1.Service) (*v1.Service, error) {
	service, err := kubeClient.CoreV1().Services(namespace).Update(service)
	if err != nil {
		return nil, err
	}
	return service, nil
}

func DeleteService(kubeClient kubernetes.Interface, namespace string, serviceName string, options *metav1.DeleteOptions) error {
	return kubeClient.CoreV1().Services(namespace).Delete(serviceName, options)
}

func GetService(kubeClient kubernetes.Interface, namespace string, serviceName string) (*v1.Service, error) {
	service, err := kubeClient.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

func GetServices(kubeClient kubernetes.Interface, namespace string) ([]v1.Service, error) {
	serviceList, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return serviceList.Items, nil
}

func GetEventingIPAndPort(k8s *types.Cluster, namespace, pod string) (string, string, func(), error) {
	port, cleanup, err := forwardPort(k8s, namespace, pod, "8096")
	if err != nil {
		return "", "", nil, err
	}
	// TODO: return a host string e.g. "127.0.0.1:8096"
	return "127.0.0.1", port, cleanup, nil
}

func MustGetEventingIPAndPort(t *testing.T, k8s *types.Cluster, namespace, pod string) (string, string, func()) {
	host, port, cleanup, err := GetEventingIPAndPort(k8s, namespace, pod)
	if err != nil {
		Die(t, err)
	}
	return host, port, cleanup
}

func GetAnalyticsIPAndPort(k8s *types.Cluster, namespace, pod string) (string, string, func(), error) {
	port, cleanup, err := forwardPort(k8s, namespace, pod, "8095")
	if err != nil {
		return "", "", nil, err
	}
	// TODO: return a host string e.g. "127.0.0.1:8095"
	return "127.0.0.1", port, cleanup, nil
}

func MustGetAnalyticsIPAndPort(t *testing.T, k8s *types.Cluster, namespace, pod string) (string, string, func()) {
	host, port, cleanup, err := GetAnalyticsIPAndPort(k8s, namespace, pod)
	if err != nil {
		Die(t, err)
	}
	return host, port, cleanup
}
