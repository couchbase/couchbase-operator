package e2eutil

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// resourceName is the common name for resources we will create.
	// TODO: We could generate these names to prevent collisions in
	// the event of some failure.
	resourceName = "test-coredns"

	// localDNSPort is the userspace the forwarding DNS will use to
	// serve queries.
	localDNSPort = 5353

	// coreFileTemplate is used to configure CoreDNS.
	coreFileTemplate = `%s.svc.cluster.local:%d {
	log
	errors
  forward . %s
}
.:%d {
	log
	errors
  forward . %s
}
`

	// coreFileTemplateForExternalDNSCheck is used to configure CoreDNS to pass external DNS checks against certain pods.
	coreFileTemplateForExternalDNSCheck = `%s:%d {
		hosts {
			fallthrough
		}
			forward . %s
	}

	.:%d {
		log
		errors
		reload 10s
  	forward . %s
	}
	`
)

var dnsServices = map[string]string{
	"kube-system":   "kube-dns",
	"openshift-dns": "dns-default",
}

// getSearchDomains returns a set of valid search domains for a local cluster
// using an intermediate forwarding proxy.
func getSearchDomains(local *types.Cluster) []string {
	return []string{
		local.Namespace + ".svc.cluster.local",
		"svc.cluster.local",
		"cluster.local",
	}
}

func findDNSService(k8s *types.Cluster) (string, string, error) {
	for namespace, service := range dnsServices {
		if _, err := k8s.KubeClient.CoreV1().Services(namespace).Get(context.Background(), service, metav1.GetOptions{}); err == nil {
			return namespace, service, nil
		}
	}

	return "", "", fmt.Errorf("dns service not found")
}

// provisionCoreDNS creates a CoreDNS instance that forwards requests to the remote
// namespace to the remote cluster's authoratative DNS server, and everything else
// to the local cluster's authoratative DNS server.  It returns the DNS service and
// a cleanup callback.
func provisionCoreDNS(local, remote *types.Cluster) (*corev1.Service, error) {
	namespace, service, err := findDNSService(remote)
	if err != nil {
		return nil, err
	}

	// List EndpointSlices for the DNS service using the standard label selector.
	slices, err := remote.KubeClient.DiscoveryV1().EndpointSlices(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "kubernetes.io/service-name=" + service,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list endpoint slices for service %s/%s: %w", namespace, service, err)
	}

	if len(slices.Items) == 0 {
		return nil, fmt.Errorf("no EndpointSlice objects found for DNS service %s/%s", namespace, service)
	}

	// Find first ready endpoint with an address.
	var address string
	var dnsPort int32

	for _, slice := range slices.Items {
		// Find DNS port from slice ports (typically port 53).
		for _, p := range slice.Ports {
			if p.Name != nil && *p.Name == "dns" && p.Port != nil {
				dnsPort = *p.Port
				break
			}
		}

		// Find a ready endpoint with an address.
		for _, ep := range slice.Endpoints {
			if len(ep.Addresses) > 0 {
				address = ep.Addresses[0]
				break
			}
		}

		if address != "" && dnsPort > 0 {
			break
		}
	}

	if address == "" {
		return nil, fmt.Errorf("no ready endpoint addresses found for DNS service %s/%s", namespace, service)
	}

	if dnsPort == 0 {
		return nil, fmt.Errorf("no dns port found in EndpointSlices for service %s/%s", namespace, service)
	}

	remoteAddress := fmt.Sprintf("%s:%d", address, dnsPort)

	// Get the local DNS service cluster IP.
	localDNSService, err := local.KubeClient.CoreV1().Services(namespace).Get(context.Background(), service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	localAddress := localDNSService.Spec.ClusterIP
	if localAddress == "" {
		return nil, fmt.Errorf("dns service cluster IP is empty")
	}

	return provisionDNS(local, resourceName, localDNSPort, fmt.Sprintf(coreFileTemplate, remote.Namespace, localDNSPort, remoteAddress, localDNSPort, localAddress))
}

func provisionCoreDNSForExternalDNSCheck(local *types.Cluster, dnsPath string) (*corev1.Service, error) {
	namespace, service, err := findDNSService(local)
	if err != nil {
		return nil, err
	}

	// Get the local DNS endpoint.
	localDNSService, err := local.KubeClient.CoreV1().Services(namespace).Get(context.Background(), service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Get the local DNS endpoint to use as a fallback.
	localAddress := localDNSService.Spec.ClusterIP
	if localAddress == "" {
		return nil, fmt.Errorf("dns service cluster IP is empty")
	}

	return provisionDNS(local, resourceName, 53, fmt.Sprintf(coreFileTemplateForExternalDNSCheck, dnsPath, 53, localAddress, 53, localAddress))
}

// MustProvisionCoreDNS reates a CoreDNS instance that forwards requests to the remote
// namespace to the remote cluster's authoratative DNS server, and everything else
// to the local cluster's authoratative DNS server.  It returns the DNS service and
// a cleanup callback.
func MustProvisionCoreDNS(t *testing.T, local, remote *types.Cluster) *corev1.Service {
	svc, err := provisionCoreDNS(local, remote)
	if err != nil {
		Die(t, err)
	}

	return svc
}

func MustProvisionCoreDNSForExternalDNSCheck(t *testing.T, local *types.Cluster, dnsPath string) *corev1.Service {
	svc, err := provisionCoreDNSForExternalDNSCheck(local, dnsPath)
	if err != nil {
		Die(t, err)
	}

	return svc
}

func MustAddPodsForDNSCheck(t *testing.T, local *types.Cluster, svcName, dnsPath string, pods []corev1.Pod) {
	cm, err := local.KubeClient.CoreV1().ConfigMaps(local.Namespace).Get(context.Background(), svcName, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	var newEntries string
	for _, pod := range pods {
		newEntries += fmt.Sprintf("%s %s.%s\n", pod.Status.HostIP, pod.Name, dnsPath)
	}

	lines := strings.Split(cm.Data["Corefile"], "\n")

	var updated []string

	for _, line := range lines {
		if strings.TrimSpace(line) == "fallthrough" {
			updated = append(updated, newEntries)
		}

		updated = append(updated, line)
	}

	cm.Data["Corefile"] = strings.Join(updated, "\n")

	if _, err := local.KubeClient.CoreV1().ConfigMaps(local.Namespace).Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		Die(t, err)
	}
}

func provisionDNS(local *types.Cluster, resourceName string, targetPort int, corefile string) (*corev1.Service, error) {
	// Create the resources.
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Data: map[string]string{
			"Corefile": corefile,
		},
	}

	if _, err := local.KubeClient.CoreV1().ConfigMaps(local.Namespace).Create(context.Background(), configMap, metav1.CreateOptions{}); err != nil {
		return nil, err
	}

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": resourceName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": resourceName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "coredns",
							Image: "coredns/coredns:1.12.2",
							Args: []string{
								"-conf",
								"/etc/coredns/Corefile",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "dns",
									ContainerPort: int32(targetPort),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/etc/coredns",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: resourceName,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := local.KubeClient.AppsV1().Deployments(local.Namespace).Create(context.Background(), deployment, metav1.CreateOptions{}); err != nil {
		return nil, err
	}

	coreService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": resourceName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "dns",
					Protocol:   corev1.ProtocolUDP,
					Port:       int32(53),
					TargetPort: intstr.FromInt(targetPort),
				},
				{
					Name:       "dns-tcp",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(53),
					TargetPort: intstr.FromInt(targetPort),
				},
			},
		},
	}

	return local.KubeClient.CoreV1().Services(local.Namespace).Create(context.Background(), coreService, metav1.CreateOptions{})
}
