package e2eutil

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateService(t *testing.T, kubeClient kubernetes.Interface, namespace string, service *v1.Service) (*v1.Service, error) {
	service, err := kubeClient.CoreV1().Services(namespace).Create(service)
	if err != nil {
		return nil, err
	}
	t.Logf("created service: %s", service.Name)
	return service, nil
}

func DeleteService(t *testing.T, kubeClient kubernetes.Interface, namespace string, serviceName string, options *metav1.DeleteOptions) error {
	t.Logf("deleting service: %s", serviceName)
	return kubeClient.CoreV1().Services(namespace).Delete(serviceName, options)
}

// Node port service provides access to couchbase server via:
// http://<api_server>:<node_port>
func NodePortServiceClient(host string, service *v1.Service) (string, error) {
	port := fmt.Sprintf("%d", service.Spec.Ports[0].NodePort)
	apiUrl, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	ip := strings.Split(apiUrl.Host, ":")[0]
	client := fmt.Sprintf("http://%s:%s", ip, port)
	return client, nil
}
