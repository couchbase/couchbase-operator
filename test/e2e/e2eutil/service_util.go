package e2eutil

import (
	"fmt"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net/url"
	"strings"
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

// Derives admin console url from exposed port and api server
func AdminConsoleURL(apiServerHost, port string) (string, error) {
	apiUrl, err := url.Parse(apiServerHost)
	if err != nil {
		return "", err
	}
	ip := strings.Split(apiUrl.Host, ":")[0]
	consoleURL := fmt.Sprintf("http://%s:%s", ip, port)
	return consoleURL, nil
}

func GetEventingIpAndPort(t *testing.T, k8s *types.Cluster, namespace, pod string) (string, string, func()) {
	pf := &portforward.PortForwarder{
		Config:    k8s.Config,
		Client:    k8s.KubeClient,
		Namespace: namespace,
		Pod:       pod,
		Port:      "8096",
	}
	if err := pf.ForwardPorts(); err != nil {
		t.Fatal(err)
	}
	return "127.0.0.1", "8096", func() { pf.Close() }
}

func GetAnalyticsIpAndPort(t *testing.T, k8s *types.Cluster, namespace, pod string) (string, string, func()) {
	pf := &portforward.PortForwarder{
		Config:    k8s.Config,
		Client:    k8s.KubeClient,
		Namespace: namespace,
		Pod:       pod,
		Port:      "8095",
	}
	if err := pf.ForwardPorts(); err != nil {
		t.Fatal(err)
	}
	return "127.0.0.1", "8095", func() { pf.Close() }
}
