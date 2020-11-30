package e2eutil

import (
	"context"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateService(k8s *types.Cluster, service *v1.Service) (*v1.Service, error) {
	service, err := k8s.KubeClient.CoreV1().Services(k8s.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return service, nil
}

func UpdateService(k8s *types.Cluster, service *v1.Service) (*v1.Service, error) {
	service, err := k8s.KubeClient.CoreV1().Services(k8s.Namespace).Update(context.Background(), service, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return service, nil
}

func DeleteService(k8s *types.Cluster, serviceName string, options *metav1.DeleteOptions) error {
	return k8s.KubeClient.CoreV1().Services(k8s.Namespace).Delete(context.Background(), serviceName, *options)
}

func GetService(k8s *types.Cluster, serviceName string) (*v1.Service, error) {
	service, err := k8s.KubeClient.CoreV1().Services(k8s.Namespace).Get(context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return service, nil
}

func GetServices(k8s *types.Cluster) ([]v1.Service, error) {
	serviceList, err := k8s.KubeClient.CoreV1().Services(k8s.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return serviceList.Items, nil
}

func GetEventingIPAndPort(k8s *types.Cluster, pod string) (string, string, func(), error) {
	port, cleanup, err := forwardPort(k8s, k8s.Namespace, pod, "8096")
	if err != nil {
		return "", "", nil, err
	}

	// TODO: return a host string e.g. "127.0.0.1:8096"
	return "127.0.0.1", port, cleanup, nil
}

func MustGetEventingIPAndPort(t *testing.T, k8s *types.Cluster, pod string) (string, string, func()) {
	host, port, cleanup, err := GetEventingIPAndPort(k8s, pod)
	if err != nil {
		Die(t, err)
	}

	return host, port, cleanup
}

func GetAnalyticsIPAndPort(k8s *types.Cluster, pod string) (string, string, func(), error) {
	port, cleanup, err := forwardPort(k8s, k8s.Namespace, pod, "8095")
	if err != nil {
		return "", "", nil, err
	}

	// TODO: return a host string e.g. "127.0.0.1:8095"
	return "127.0.0.1", port, cleanup, nil
}

func MustGetAnalyticsIPAndPort(t *testing.T, k8s *types.Cluster, pod string) (string, string, func()) {
	host, port, cleanup, err := GetAnalyticsIPAndPort(k8s, pod)
	if err != nil {
		Die(t, err)
	}

	return host, port, cleanup
}
