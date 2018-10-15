package e2eutil

import (
	"fmt"
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net/url"
	"strconv"
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

func GetEventingIpAndPort(t *testing.T, eventingNodeName string, kubeClient kubernetes.Interface, namespace string, platformType string, cl *api.CouchbaseCluster) (string, string, error) {
	if platformType == "azure" {
		// need to create load balancer to forward this port to public ip
		serviceName := eventingNodeName + "-exposed-ports"
		service, _ := GetService(kubeClient, namespace, serviceName)
		service.Spec.Type = "LoadBalancer"
		service, _ = UpdateService(kubeClient, namespace, service)
		_ = WaitForExternalLoadBalancer(t, kubeClient, namespace, service.Name, 300)
		service, _ = GetService(kubeClient, namespace, serviceName)
		eventingHostUrl := service.Status.LoadBalancer.Ingress[0].IP
		eventingPortStr := "8096"
		return eventingHostUrl, eventingPortStr, nil
	} else {
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(eventingNodeName, metav1.GetOptions{})
		if err != nil {
			return "", "", err
		}
		eventingHostUrl := pod.Status.HostIP
		eventingPortStr := strconv.Itoa(int(cl.Status.ExposedPorts[eventingNodeName].EventingServicePort))
		return eventingHostUrl, eventingPortStr, nil
	}

}

func GetAnalyticsIpAndPort(t *testing.T, analyticsNodeName string, kubeClient kubernetes.Interface, namespace string, platformType string, cl *api.CouchbaseCluster) (string, string, error) {
	if platformType == "azure" {
		// need to create load balancer to forward this port to public ip
		serviceName := analyticsNodeName + "-exposed-ports"
		service, _ := GetService(kubeClient, namespace, serviceName)
		service.Spec.Type = "LoadBalancer"
		service, _ = UpdateService(kubeClient, namespace, service)
		_ = WaitForExternalLoadBalancer(t, kubeClient, namespace, service.Name, 300)
		service, _ = GetService(kubeClient, namespace, serviceName)
		analyticsHostUrl := service.Status.LoadBalancer.Ingress[0].IP
		analyticsNodePortStr := "8095"
		return analyticsHostUrl, analyticsNodePortStr, nil
	} else {
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(analyticsNodeName, metav1.GetOptions{})
		if err != nil {
			return "", "", err
		}
		analyticsHostUrl := pod.Status.HostIP
		analyticsNodePortStr := strconv.Itoa(int(cl.Status.ExposedPorts[analyticsNodeName].AnalyticsServicePort))
		return analyticsHostUrl, analyticsNodePortStr, nil
	}

}
