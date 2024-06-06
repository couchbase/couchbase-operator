package e2eutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
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
func getNodeServices(couchbase *couchbasev2.CouchbaseCluster) (*NodeServices, error) {
	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s.%s.svc:8091/pools/default/nodeServices", couchbase.Name, couchbase.Namespace), nil)
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

	body, err := io.ReadAll(response.Body)
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

	return k8s.KubeClient.CoreV1().Services(couchbase.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
}

func checkServiceExposesPorts(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, serviceName string, address *AlternateAddressExternal) error {
	if address == nil {
		return nil
	}

	service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for k, v := range address.Ports {
		found := false

		for _, servicePort := range service.Spec.Ports {
			if v == int(servicePort.NodePort) {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("port %d not found for service %s not found (expected for %q)", v, serviceName, k)
		}
	}

	return nil
}

func checkForExpectedPorts(service corev1.Service, expectedPorts []int32) error {
	for _, expectedPort := range expectedPorts {
		found := false

		for _, actualPort := range service.Spec.Ports {
			if actualPort.Port == expectedPort {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("missing required port %d for service %s", expectedPort, service.Name)
		}
	}

	return nil
}

func checkServicePorts(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		// Do a sanity check that we're exposing ports for NodePort types based on the Couchbase Service matrix.
		services, err := getKubernetesNodeServices(k8s, couchbase)
		if err != nil {
			return err
		}

		// We replicate the matrix here rather than reuse the one in services.go as we want to check for errors there.
		servicePorts := map[couchbasev2.Service][]int32{
			couchbasev2.AdminService:     {8091, 18091},
			couchbasev2.DataService:      {8092, 18092, 11210, 11207},
			couchbasev2.QueryService:     {8093, 18093},
			couchbasev2.SearchService:    {8094, 18094},
			couchbasev2.AnalyticsService: {8095, 18095},
			couchbasev2.EventingService:  {8096, 18096},
			couchbasev2.IndexService:     {9102, 19102},
		}

		// For the service, check its matching pod to confirm what type of Couchbase Service is running (data, index, etc.).
		// Using that confirm the appropriate ports are open on the service.
		for _, service := range services.Items {
			if service.Spec.Type != corev1.ServiceTypeNodePort {
				continue
			}

			// Get matching pod and find out what types are attached to it.
			podSelector := labels.Set(service.Spec.Selector)

			podList, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: podSelector.AsSelector().String()})
			if err != nil {
				return err
			}

			if podList == nil || len(podList.Items) < 1 {
				return fmt.Errorf("unable to find pod associated with service %q, selector: %q", service.Name, podSelector.AsSelector().String())
			}

			// Get the first pod (likely only) and check its annotations.
			pod := podList.Items[0]

			// Use the type to lookup in our maps above.
			for serviceType, expectedPorts := range servicePorts {
				searchLabel := constants.LabelServicePrefix + serviceType.String()
				labelValue, labelExists := pod.Labels[searchLabel]

				if labelExists && labelValue == constants.EnabledValue {
					err := checkForExpectedPorts(service, expectedPorts)
					if err != nil {
						return err
					}
				}
			}
		}

		return nil
	})
}

func MustCheckServicePorts(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := checkServicePorts(k8s, couchbase, timeout); err != nil {
		Die(t, err)
	}
}

// MustCheckConsolePorts requires successful check of expected ports on the Console service.
func MustCheckConsolePorts(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedPorts *[]int32, rejectedPorts *[]int32) {
	if err := checkConsolePorts(k8s, couchbase, expectedPorts, rejectedPorts); err != nil {
		Die(t, err)
	}
}

// checkConsolePorts ensures that Console service advertises the expected data port, but not the rejected ports.
func checkConsolePorts(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, expectedPorts *[]int32, rejectedPorts *[]int32) error {
	// Get Console Service.
	service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(context.Background(), couchbase.Name+"-ui", metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Check service for expected ports.
	if expectedPorts != nil {
		err := checkForExpectedPorts(*service, *expectedPorts)
		if err != nil {
			return err
		}
	}

	// Expect failure when looking up each rejected port
	if rejectedPorts != nil {
		for _, port := range *rejectedPorts {
			err = checkForExpectedPorts(*service, []int32{port})
			if err == nil {
				return fmt.Errorf("port %d was not expected to be exposed on service %s", port, service.Name)
			}
		}
	}

	return nil
}

// CheckForIPAlternateAddresses gets external addressability configuration from the Couchbase API
// and checks that alternate addresses are defined and IPv4 addresses.
func CheckForIPAlternateAddresses(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		nodeServices, err := getNodeServices(couchbase)
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

			serviceName := node.Hostname[:strings.IndexByte(node.Hostname, '.')]
			err := checkServiceExposesPorts(k8s, couchbase, serviceName, node.AlternateAddresses.External)
			if err != nil {
				return err
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
func CheckForDNSAlternateAddresses(couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		nodeServices, err := getNodeServices(couchbase)
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

func MustCheckForDNSAlternateAddresses(t *testing.T, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) {
	if err := CheckForDNSAlternateAddresses(couchbase, domain, timeout); err != nil {
		Die(t, err)
	}
}

// CheckForDNSServiceAnnotations gets all node services defined for the cluster and
// checks that the DDNS annotations exist and in the requested domain.
func CheckForDNSServiceAnnotations(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, domain string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
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
	return retryutil.RetryFor(timeout, func() error {
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
	return retryutil.RetryFor(timeout, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(context.Background(), couchbase.Name+"-ui", metav1.GetOptions{})
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
	return retryutil.RetryFor(timeout, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(context.Background(), couchbase.Name+"-ui", metav1.GetOptions{})
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
	return retryutil.RetryFor(timeout, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(context.Background(), couchbase.Name+"-ui", metav1.GetOptions{})
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

// CheckConsoleServiceLoadBalancerSourceRanges checks that load balancer source ranges
// are as we expect.
func CheckConsoleServiceLoadBalancerSourceRanges(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, sourceRanges []string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		service, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).Get(context.Background(), couchbase.Name+"-ui", metav1.GetOptions{})
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

// exposePorts checks if the server ports are exposed only on the requested protocols.
// Works with 7.0.2+ only.  Don't be tempted to actually probe the ports themselves,
// you'll find it practically impossible to actually test this on 99% of platforms.
func exposePorts(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, protocol couchbasev2.AddressFamily, unique bool, timeout time.Duration) error {
	// Get the expected number of pods (with kubernetes as the source of truth, we expect Couchbase to match).
	options := metav1.ListOptions{
		LabelSelector: labels.Set(k8sutil.SelectorForClusterResource(couchbase)).String(),
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), options)
	if err != nil {
		return fmt.Errorf("unable to get couchbase pods: %w", err)
	}

	// Translate between the API idea of an address family and that Couchbase returns.
	family := couchbaseutil.AddressFamilyOutInet

	if protocol == couchbasev2.AFInet6 {
		family = couchbaseutil.AddressFamilyOutInet6
	}

	// Accumulate tests to run against the /pools/default endpoint.
	// All nodes in the cluster should be the same.
	patchset := jsonpatch.NewPatchSet()

	for i := range pods.Items {
		patchset.Test(fmt.Sprintf("/Nodes/%d/AddressFamily", i), family)
		patchset.Test(fmt.Sprintf("/Nodes/%d/AddressFamilyOnly", i), unique)
	}

	callback := func() error {
		return PatchCouchbaseInfo(k8s, couchbase, patchset, timeout)
	}

	return retryutil.RetryFor(timeout, callback)
}

func MustExposePorts(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, protocol couchbasev2.AddressFamily, unique bool, timeout time.Duration) {
	if err := exposePorts(k8s, couchbase, protocol, unique, timeout); err != nil {
		Die(t, err)
	}
}

// get the IP Address of this container.
func GetHostAddress(t *testing.T) string {
	containerHostname, err := os.Hostname()
	if err != nil {
		Die(t, err)
	}

	addrs, err := net.LookupHost(containerHostname)
	if err != nil {
		Die(t, err)
	}

	// there must be at least one IP
	if len(addrs) == 0 {
		Die(t, fmt.Errorf("expected at least one IP address"))
	}

	return addrs[0]
}

// generates address with a random port between min - max.
func GetHostAddressWithPort(t *testing.T, min, max int) string {
	address := GetHostAddress(t)
	port := rand.Intn(max-min) + min

	return address + ":" + strconv.Itoa(port)
}
