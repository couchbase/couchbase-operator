package e2eutil

import (
	"fmt"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// dnsService is where the DNS service is expected to run.
	dnsNamespace = "kube-system"

	// dnsService is the name of the DNS service and endpoints.
	// This seems to hold for those using both kube-dns and coredns,
	// most likely for backwards compatibility.
	dnsService = "kube-dns"

	// resourceName is the common name for resources we will create.
	// TODO: We could generate these names to prevent collisions in
	// the event of some failure.
	resourceName = "test-coredns"

	// localDNSPort is the userspace the forwarding DNS will use to
	// serve queries.
	localDNSPort = 5353

	// coreFileTemplate is used to configure CoreDNS.
	coreFileTemplate = `%s.svc.cluster.local:%d {
  forward . %s
}
.:%d {
  forward . %s
}
`
)

// getSearchDomains returns a set of valid search domains for a local cluster
// using an intermediate forwarding proxy.
func getSearchDomains(local *types.Cluster) []string {
	return []string{
		local.Namespace + ".svc.cluster.local",
		"svc.cluster.local",
		"cluster.local",
	}
}

// provisionCoreDNS creates a CoreDNS instance that forwards requests to the remote
// namespace to the remote cluster's authoratative DNS server, and everything else
// to the local cluster's authoratative DNS server.  It returns the DNS service and
// a cleanup callback.
func provisionCoreDNS(local, remote *types.Cluster) (*corev1.Service, func(), error) {
	// Get a remote DNS endpoint.
	endpoints, err := remote.KubeClient.CoreV1().Endpoints(dnsNamespace).Get(dnsService, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	if len(endpoints.Subsets) != 1 {
		return nil, nil, fmt.Errorf("dns endpoints object contains unexpected number of subsets: %d", len(endpoints.Subsets))
	}

	if len(endpoints.Subsets[0].Addresses) == 0 {
		return nil, nil, fmt.Errorf("dns endpoints object contains no addresses")
	}

	remoteAddress := endpoints.Subsets[0].Addresses[0].IP
	if remoteAddress == "" {
		return nil, nil, fmt.Errorf("dns endpoint address is empty")
	}

	// Get the local DNS endpoint.
	localDNSService, err := local.KubeClient.CoreV1().Services(dnsNamespace).Get(dnsService, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	localAddress := localDNSService.Spec.ClusterIP
	if localAddress == "" {
		return nil, nil, fmt.Errorf("dns service cluster IP is empty")
	}

	// Create the resources.
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Data: map[string]string{
			"Corefile": fmt.Sprintf(coreFileTemplate, remote.Namespace, localDNSPort, remoteAddress, localDNSPort, localAddress),
		},
	}

	_ = local.KubeClient.CoreV1().ConfigMaps(local.Namespace).Delete(resourceName, metav1.NewDeleteOptions(0))

	if _, err = local.KubeClient.CoreV1().ConfigMaps(local.Namespace).Create(configMap); err != nil {
		return nil, nil, err
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
							Image: "coredns/coredns:1.6.6",
							Args: []string{
								"-conf",
								"/etc/coredns/Corefile",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "dns",
									ContainerPort: int32(localDNSPort),
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
	_ = local.KubeClient.AppsV1().Deployments(local.Namespace).Delete(resourceName, metav1.NewDeleteOptions(0))

	if _, err := local.KubeClient.AppsV1().Deployments(local.Namespace).Create(deployment); err != nil {
		return nil, nil, err
	}

	service := &corev1.Service{
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
					TargetPort: intstr.FromInt(localDNSPort),
				},
				{
					Name:       "dns-tcp",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(53),
					TargetPort: intstr.FromInt(localDNSPort),
				},
			},
		},
	}

	_ = local.KubeClient.CoreV1().Services(local.Namespace).Delete(resourceName, metav1.NewDeleteOptions(0))

	if service, err = local.KubeClient.CoreV1().Services(local.Namespace).Create(service); err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		_ = local.KubeClient.CoreV1().Services(local.Namespace).Delete(resourceName, metav1.NewDeleteOptions(0))
		_ = local.KubeClient.AppsV1().Deployments(local.Namespace).Delete(resourceName, metav1.NewDeleteOptions(0))
		_ = local.KubeClient.CoreV1().ConfigMaps(local.Namespace).Delete(resourceName, metav1.NewDeleteOptions(0))
	}

	return service, cleanup, nil
}

// MustProvisionCoreDNS reates a CoreDNS instance that forwards requests to the remote
// namespace to the remote cluster's authoratative DNS server, and everything else
// to the local cluster's authoratative DNS server.  It returns the DNS service and
// a cleanup callback.
func MustProvisionCoreDNS(t *testing.T, local, remote *types.Cluster) (*corev1.Service, func()) {
	svc, cleanup, err := provisionCoreDNS(local, remote)
	if err != nil {
		Die(t, err)
	}

	return svc, cleanup
}
