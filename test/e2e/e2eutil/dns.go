/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"fmt"
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
	// Get a remote DNS endpoint.
	endpoints, err := remote.KubeClient.CoreV1().Endpoints(namespace).Get(context.Background(), service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if len(endpoints.Subsets) != 1 {
		return nil, fmt.Errorf("dns endpoints object contains unexpected number of subsets: %d", len(endpoints.Subsets))
	}

	if len(endpoints.Subsets[0].Addresses) == 0 {
		return nil, fmt.Errorf("dns endpoints object contains no addresses")
	}

	remoteAddress := endpoints.Subsets[0].Addresses[0].IP
	if remoteAddress == "" {
		return nil, fmt.Errorf("dns endpoint address is empty")
	}

	// Add the dns port
	for _, port := range endpoints.Subsets[0].Ports {
		if port.Name == "dns" {
			remoteAddress = fmt.Sprintf("%s:%d", remoteAddress, port.Port)
			break
		}
	}

	// Get the local DNS endpoint.
	localDNSService, err := local.KubeClient.CoreV1().Services(namespace).Get(context.Background(), service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	localAddress := localDNSService.Spec.ClusterIP
	if localAddress == "" {
		return nil, fmt.Errorf("dns service cluster IP is empty")
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
							Image: "coredns/coredns:1.9.0",
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

	svc, err := local.KubeClient.CoreV1().Services(local.Namespace).Create(context.Background(), coreService, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return svc, nil
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
