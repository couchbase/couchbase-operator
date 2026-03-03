/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// AlternateAddressExternal is an external alternate address.
type AlternateAddressExternal struct {
	Hostname string         `json:"hostname"`
	Ports    map[string]int `json:"ports"`
}

// AlternateAddress is an alternate address entry.
type AlternateAddress struct {
	External *AlternateAddressExternal `json:"external"`
}

// NodeExt is a nodes external hostname and services.
type NodeExt struct {
	Hostname           string            `json:"hostname"`
	Services           map[string]int    `json:"services"`
	AlternateAddresses *AlternateAddress `json:"alternateAddresses"`
}

// NodeServices is a list of all nodes external addressability configuration.
type NodeServices struct {
	NodesExt []NodeExt `json:"nodesExt"`
}

// validateAlternateAddresses looks at the node services, verifying that the correct number
// exist and external alternate addresses are specified.
func (nodeServices *NodeServices) validateAlternateAddresses(couchbase *couchbasev2.CouchbaseCluster) error {
	if len(nodeServices.NodesExt) != couchbase.Spec.TotalSize() {
		return fmt.Errorf("found %d nodes, expected %d", len(nodeServices.NodesExt), couchbase.Spec.TotalSize())
	}

	for _, node := range nodeServices.NodesExt {
		if node.AlternateAddresses == nil || node.AlternateAddresses.External == nil {
			return fmt.Errorf("alternate addresses not set on node %s", node.Hostname)
		}
	}

	return nil
}

// getNodeServices polls the Couchbase API, gets and decodes external addressability configuration.
func getNodeServices(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) (*NodeServices, error) {
	host, cleanup, err := GetHostURL(k8s, couchbase, couchbasev2.AdminService)
	if err != nil {
		return nil, err
	}

	defer cleanup()

	request, err := http.NewRequest("GET", "http://"+host+"/pools/default/nodeServices", nil)
	if err != nil {
		return nil, err
	}

	request.SetBasicAuth("Administrator", "password")

	client := http.Client{}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	nodeServices := &NodeServices{}
	if err := json.Unmarshal(body, nodeServices); err != nil {
		return nil, err
	}

	return nodeServices, nil
}

// getKubernetesNodeServices lists the Kubernetes node services created by the operator.
func getKubernetesNodeServices(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) (*corev1.ServiceList, error) {
	appreq, err := labels.NewRequirement(constants.LabelApp, selection.Equals, []string{constants.App})
	if err != nil {
		return nil, err
	}

	clusterreq, err := labels.NewRequirement(constants.LabelCluster, selection.Equals, []string{couchbase.Name})
	if err != nil {
		return nil, err
	}

	nodereq, err := labels.NewRequirement(constants.LabelNode, selection.Exists, []string{})
	if err != nil {
		return nil, err
	}

	selector := labels.NewSelector()
	selector = selector.Add(*appreq, *clusterreq, *nodereq)

	return k8s.KubeClient.CoreV1().Services(couchbase.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
}

// CheckForIPAlternateAddresses gets external addressability configuration from the Couchbase API
// and checks that alternate addresses are defined and IPv4 addresses.
func CheckForIPAlternateAddresses(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		nodeServices, err := getNodeServices(k8s, couchbase)
		if err != nil {
			return err
		}

		if err := nodeServices.validateAlternateAddresses(couchbase); err != nil {
			return err
		}

		for _, node := range nodeServices.NodesExt {
			if net.ParseIP(node.AlternateAddresses.External.Hostname) == nil {
				return fmt.Errorf("node %s alternate address %s not an IP", node.Hostname, node.AlternateAddresses.External.Hostname)
			}
		}

		return nil
	})
}

func MustCheckForIPAlternateAddresses(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := CheckForIPAlternateAddresses(k8s, couchbase, timeout); err != nil {
		Die(t, err)
	}
}

// CheckForDNSAlternateAddresses gets external addressability configuration from the Couchbase API
// and checks that alternate addresses are defined and DNS names in the requested domain.
func CheckForDNSAlternateAddresses(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		nodeServices, err := getNodeServices(k8s, couchbase)
		if err != nil {
			return err
		}

		if err := nodeServices.validateAlternateAddresses(couchbase); err != nil {
			return err
		}

		for _, node := range nodeServices.NodesExt {
			if !strings.HasSuffix(node.AlternateAddresses.External.Hostname, domain) {
				return fmt.Errorf("node %s alternate address %s does not contain suffix %s", node.Hostname, node.AlternateAddresses.External.Hostname, domain)
			}
		}

		return nil
	})
}

func MustCheckForDNSAlternateAddresses(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) {
	if err := CheckForDNSAlternateAddresses(k8s, couchbase, domain, timeout); err != nil {
		Die(t, err)
	}
}

// CheckForDNSServiceAnnotations gets all node services defined for the cluster and
// checks that the DDNS annotations exist and in the requested domain.
func CheckForDNSServiceAnnotations(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		services, err := getKubernetesNodeServices(k8s, couchbase)
		if err != nil {
			return err
		}

		if len(services.Items) != couchbase.Spec.TotalSize() {
			return fmt.Errorf("found %d nodes, expected %d", len(services.Items), couchbase.Spec.TotalSize())
		}

		for _, service := range services.Items {
			annotation, ok := service.Annotations[constants.DNSAnnotation]
			if !ok {
				return fmt.Errorf("ddns annotation missing on service %s", service.Name)
			}

			if !strings.HasSuffix(annotation, domain) {
				return fmt.Errorf("service %s annotation %s does not contain the suffix %s", service.Name, annotation, domain)
			}
		}

		return nil
	})
}

func MustCheckForDNSServiceAnnotations(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) {
	if err := CheckForDNSServiceAnnotations(k8s, couchbase, domain, timeout); err != nil {
		Die(t, err)
	}
}

// CheckForNodeServiceType checks that the node service type is as expected.
func CheckForNodeServiceType(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, serviceType corev1.ServiceType, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		services, err := getKubernetesNodeServices(k8s, couchbase)
		if err != nil {
			return err
		}

		if len(services.Items) != couchbase.Spec.TotalSize() {
			return fmt.Errorf("found %d nodes, expected %d", len(services.Items), couchbase.Spec.TotalSize())
		}

		for _, service := range services.Items {
			if service.Spec.Type != serviceType {
				return fmt.Errorf("service %s type %v is not of type %v", service.Name, service.Spec.Type, serviceType)
			}
		}

		return nil
	})
}

func MustCheckForNodeServiceType(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, serviceType corev1.ServiceType, timeout time.Duration) {
	if err := CheckForNodeServiceType(k8s, couchbase, serviceType, timeout); err != nil {
		Die(t, err)
	}
}

// CheckForDNSAdminAnnotation checks that a DNS annotaion is added to the console service.
func CheckForDNSAdminAnnotation(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(couchbase.Name+"-ui", metav1.GetOptions{})
		if err != nil {
			return err
		}

		annotation, ok := service.Annotations[constants.DNSAnnotation]
		if !ok {
			return fmt.Errorf("ddns annotation missing on service %s", service.Name)
		}

		if !strings.HasSuffix(annotation, domain) {
			return fmt.Errorf("service %s annotation %s does not contain the suffix %s", service.Name, annotation, domain)
		}

		return nil
	})
}

func MustCheckForDNSAdminAnnotation(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) {
	if err := CheckForDNSAdminAnnotation(k8s, couchbase, domain, timeout); err != nil {
		Die(t, err)
	}
}

// CheckForConsoleServiceType checks that the console service is of the epxected type.
func CheckForConsoleServiceType(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, serviceType corev1.ServiceType, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(couchbase.Name+"-ui", metav1.GetOptions{})
		if err != nil {
			return err
		}

		if service.Spec.Type != serviceType {
			return fmt.Errorf("service %s type %v is not of type %v", service.Name, service.Spec.Type, serviceType)
		}

		return nil
	})
}

func MustCheckForConsoleServiceType(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, serviceType corev1.ServiceType, timeout time.Duration) {
	if err := CheckForConsoleServiceType(k8s, couchbase, serviceType, timeout); err != nil {
		Die(t, err)
	}
}

// CheckConsoleServiceStatus checks that if Console service type is LoadBalancer then an ingress route is established.
func CheckConsoleServiceStatus(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(couchbase.Name+"-ui", metav1.GetOptions{})
		if err != nil {
			return err
		}

		if service.Spec.Type == corev1.ServiceTypeLoadBalancer {
			// IP is set for load-balancer ingress points that are IP based
			// (typically GCE or OpenStack load-balancers)
			// Hostname is set for load-balancer ingress points that are DNS based
			// (typically AWS load-balancers)
			for _, ingress := range service.Status.LoadBalancer.Ingress {
				if ingress.IP != "" || ingress.Hostname != "" {
					return nil
				}
			}

			return fmt.Errorf("loadbalancer service %s failed to create an ingress route", service.Name)
		}

		return nil
	})
}

func MustCheckConsoleServiceStatus(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := CheckConsoleServiceStatus(k8s, couchbase, timeout); err != nil {
		Die(t, err)
	}
}

// CheckConsoleServiceLoadBalancerSourceRanges checks that loda balancer source ranges
// are as we expect.
func CheckConsoleServiceLoadBalancerSourceRanges(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, sourceRanges []string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(couchbase.Name+"-ui", metav1.GetOptions{})
		if err != nil {
			return err
		}

		if !reflect.DeepEqual(service.Spec.LoadBalancerSourceRanges, sourceRanges) {
			return fmt.Errorf("wanted %v, has %v", sourceRanges, service.Spec.LoadBalancerSourceRanges)
		}

		return nil
	})
}

func MustCheckConsoleServiceLoadBalancerSourceRanges(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, sourceRanges []string, timeout time.Duration) {
	if err := CheckConsoleServiceLoadBalancerSourceRanges(k8s, couchbase, sourceRanges, timeout); err != nil {
		Die(t, err)
	}
}
