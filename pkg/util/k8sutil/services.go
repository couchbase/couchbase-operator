package k8sutil

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
)

const (
	// Used to annotate services with names which will get syncronized to a cloud DNS provider.
	ddnsAnnotation = "external-dns.alpha.kubernetes.io/hostname"
	// Used to annotate services which belong to an SRV record.
	srvAnnotaion = "external-dns.alpha.kubernetes.io/service"

	// Fixed port names (generate SRV records for the peer services)
	couchbaseSRVName    = "couchbase"
	couchbaseSRVNameTLS = "couchbases"

	couchbaseUIPortName    = "couchbase-ui"
	couchbaseUIPortNameTLS = couchbaseUIPortName + tlsPortNameSuffix

	// tlsBasePort is the port number relative to which TLS ports are (usually) translated
	tlsBasePort       = 10000
	tlsPortNameSuffix = "-tls"

	// Admin service constants
	adminServicePortName    = string(couchbasev1.AdminService)
	adminServicePortNameTLS = string(couchbasev1.AdminService) + tlsPortNameSuffix
	adminServicePort        = 8091
	adminServicePortTLS     = tlsBasePort + adminServicePort

	// Index service constants
	indexServicePortName    = string(couchbasev1.IndexService)
	indexServicePortNameTLS = string(couchbasev1.IndexService) + tlsPortNameSuffix
	indexServicePort        = 8092
	indexServicePortTLS     = tlsBasePort + indexServicePort

	// Query service constants
	queryServicePortName    = string(couchbasev1.QueryService)
	queryServicePortNameTLS = string(couchbasev1.QueryService) + tlsPortNameSuffix
	queryServicePort        = 8093
	queryServicePortTLS     = tlsBasePort + queryServicePort

	// Full text search service constants
	searchServicePortName    = string(couchbasev1.SearchService)
	searchServicePortNameTLS = string(couchbasev1.SearchService) + tlsPortNameSuffix
	searchServicePort        = 8094
	searchServicePortTLS     = tlsBasePort + searchServicePort

	// Analytics service constants
	analyticsServicePortName    = string(couchbasev1.AnalyticsService)
	analyticsServicePortNameTLS = string(couchbasev1.AnalyticsService) + tlsPortNameSuffix
	analyticsServicePort        = 8095
	analyticsServicePortTLS     = tlsBasePort + analyticsServicePort

	// Eventing service constants
	eventingServicePortName    = string(couchbasev1.EventingService)
	eventingServicePortNameTLS = string(couchbasev1.EventingService) + tlsPortNameSuffix
	eventingServicePort        = 8096
	eventingServicePortTLS     = tlsBasePort + eventingServicePort

	// Indexer service constants
	indexerAdminPortName     = "index-admin"
	indexerAdminPort         = 9100
	indexerScanPortName      = "index-scan"
	indexerScanPort          = 9101
	indexerHTTPPortName      = "index-http"
	indexerHTTPPort          = 9102
	indexerSTInitPortName    = "index-stinit"
	indexerSTInitPort        = 9103
	indexerSTCatchupPortName = "index-stcatchup"
	indexerSTCatchupPort     = 9104
	indexerSTMainPortName    = "index-stmaint"
	indexerSTMainPort        = 9105

	// Analytics service constants
	analyticsAdminPortName            = "cbas-admin"
	analyticsAdminPort                = 9110
	analyticsCCHTTPPortName           = "cbas-cc-http"
	analyticsCCHTTPPort               = 9111
	analyticsCCClusterPortName        = "cbas-cc-cluster"
	analyticsCCClusterPort            = 9112
	analyticsCCClientPortName         = "cbas-cc-client"
	analyticsCCClientPort             = 9113
	analyticsConsolePortName          = "cbas-client"
	analyticsConsolePort              = 9114
	analyticsClusterPortName          = "cbas-cluster"
	analyticsClusterPort              = 9115
	analyticsDataPortName             = "cbas-data"
	analyticsDataPort                 = 9116
	analyticsResultPortName           = "cbas-report"
	analyticsResultPort               = 9117
	analyticsMessagingPortName        = "cbas-messaging"
	analyticsMessagingPort            = 9118
	analyticsAuthPortName             = "cbas-auth"
	analyticsAuthPort                 = 9119
	analyticsReplicationPortName      = "cbas-repl"
	analyticsReplicationPort          = 9120
	analyticsMetadataPortName         = "cbas-meta"
	analyticsMetadataPort             = 9121
	analyticsMetadataCallbackPortName = "cbas-meta-cb"
	analyticsMetadataCallbackPort     = 9122

	// Data service constants
	dataServicePortName    = string(couchbasev1.DataService)
	dataServicePortNameTLS = string(couchbasev1.DataService) + tlsPortNameSuffix
	dataServicePort        = 11210
	dataServicePortTLS     = 11207
)

var (
	// peerServicePorts is the list of ports to expose for the Couchbase cluster
	// headless service.  This generates DNS A records for all pods, and SRV records
	// pointing to all pods.
	peerServicePorts = []v1.ServicePort{
		{
			Name:     couchbaseSRVName,
			Port:     adminServicePort,
			Protocol: v1.ProtocolTCP,
		},
		{
			Name:     couchbaseSRVNameTLS,
			Port:     adminServicePortTLS,
			Protocol: v1.ProtocolTCP,
		},
	}

	// uiServicePorts is the list of ports used to expose the Couchbase admin console.
	uiServicePorts = []v1.ServicePort{
		{
			Name:     couchbaseUIPortName,
			Port:     adminServicePort,
			Protocol: v1.ProtocolTCP,
		},
		{
			Name:     couchbaseUIPortNameTLS,
			Port:     adminServicePortTLS,
			Protocol: v1.ProtocolTCP,
		},
	}

	// srvServicePorts is the list of ports used to create the Couchbase cluster SRV record
	// for service discovery.  The port names must be couchbase(s) and point at the data
	// service ports.
	srvServicePorts = []v1.ServicePort{
		{
			Name:     couchbaseSRVName,
			Port:     dataServicePort,
			Protocol: v1.ProtocolTCP,
		},
		{
			Name:     couchbaseSRVNameTLS,
			Port:     dataServicePortTLS,
			Protocol: v1.ProtocolTCP,
		},
	}

	// servicePorts maps a service type to it's set of ports.  These are used in
	// the generation of node ports to allow cluster access from outside of the
	// pod (overlay) network.
	servicePorts = map[couchbasev1.Service][]v1.ServicePort{
		couchbasev1.AdminService: []v1.ServicePort{
			{
				Name:     adminServicePortName,
				Port:     adminServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     adminServicePortNameTLS,
				Port:     adminServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
		couchbasev1.IndexService: []v1.ServicePort{
			{
				Name:     indexServicePortName,
				Port:     indexServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     indexServicePortNameTLS,
				Port:     indexServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
		couchbasev1.QueryService: []v1.ServicePort{
			{
				Name:     queryServicePortName,
				Port:     queryServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     queryServicePortNameTLS,
				Port:     queryServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
		couchbasev1.SearchService: []v1.ServicePort{
			{
				Name:     searchServicePortName,
				Port:     searchServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     searchServicePortNameTLS,
				Port:     searchServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
		couchbasev1.AnalyticsService: []v1.ServicePort{
			{
				Name:     analyticsServicePortName,
				Port:     analyticsServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     analyticsServicePortNameTLS,
				Port:     analyticsServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
		couchbasev1.EventingService: []v1.ServicePort{
			{
				Name:     eventingServicePortName,
				Port:     eventingServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     eventingServicePortNameTLS,
				Port:     eventingServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
		couchbasev1.DataService: []v1.ServicePort{
			{
				Name:     dataServicePortName,
				Port:     dataServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     dataServicePortNameTLS,
				Port:     dataServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
	}
)

// When we update a service we can return a status telling what happened.
type ReconcileStatus string

const (
	ReconcileStatusError     ReconcileStatus = "error"
	ReconcileStatusCreated                   = "created"
	ReconcileStatusDeleted                   = "deleted"
	ReconcileStatusUpdated                   = "updated"
	ReconcileStatusUnchanged                 = "unchanged"
)

// createServiceManifest creates a basic service with a type, ports, labels and pod selectors.
func createServiceManifest(svcName string, serviceType v1.ServiceType, ports []v1.ServicePort, labels, selector map[string]string) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   svcName,
			Labels: labels,
			Annotations: map[string]string{
				TolerateUnreadyEndpointsAnnotation: "true",
			},
		},
		Spec: v1.ServiceSpec{
			Type:     serviceType,
			Ports:    ports,
			Selector: selector,
		},
	}

	return svc
}

// dnsAdminConsoleName returns the DNS name for the admin console.
func dnsAdminConsoleName(cluster *couchbasev1.CouchbaseCluster) string {
	return "console." + cluster.Name + "." + cluster.Spec.DNS.Domain
}

// ConsoleServiceName generates the console service name.
func ConsoleServiceName(clusterName string) string {
	return clusterName + "-ui"
}

// labelsForNodeService returns a set of labels which identify a cluster service
// for a specific node.
func labelsForNodeService(clusterName, nodeName string) map[string]string {
	labels := LabelsForCluster(clusterName)
	labels[constants.LabelNode] = nodeName
	return labels
}

// creates a service of Type ClusterIP which is only resolvable internally.
// futhermore the ClusterIP is "None" allowing the service to run "headless"
// (sans load balancing middleware) which allows the operator to resolve
// addresses of individual pods instead of a proxy
func CreatePeerService(kubecli kubernetes.Interface, clusterName, ns string, owner metav1.OwnerReference) error {
	labels := LabelsForCluster(clusterName)

	// Create a service which defines A records for all pods, we use this internally
	// to address nodes via stable names (IPs are not fixed)
	svc := createServiceManifest(clusterName, v1.ServiceTypeClusterIP, peerServicePorts, labels, labels)
	svc.Spec.ClusterIP = v1.ClusterIPNone
	if _, err := createService(kubecli, ns, svc, owner); err != nil {
		return err
	}

	// Create a service which defines only data nodes.  This is the expected way clients
	// using SRV records will connect, meaning they will only ever bootstrap via memcached
	selectors := LabelsForCluster(clusterName)
	selectors["couchbase_service_data"] = "enabled"
	svc = createServiceManifest(clusterName+"-srv", v1.ServiceTypeClusterIP, srvServicePorts, labels, selectors)
	svc.Spec.ClusterIP = v1.ClusterIPNone
	if _, err := createService(kubecli, ns, svc, owner); err != nil {
		return err
	}

	return nil
}

// adminConsoleSelector generates a selector matching pods running all the requested services.
func adminConsoleSelector(cluster *couchbasev1.CouchbaseCluster) map[string]string {
	labels := LabelsForCluster(cluster.Name)
	for _, s := range cluster.Spec.AdminConsoleServices {
		k := "couchbase_service_" + s.String()
		labels[k] = "enabled"
	}
	return labels
}

// generateConsoleService creates a new Service resource based on the cluster specification.
func generateConsoleService(cluster *couchbasev1.CouchbaseCluster) *v1.Service {
	// If the service is public, remove non TLS ports.
	ports := filterInsecurePorts(uiServicePorts, cluster.Spec.IsAdminConsoleServiceTypePublic())

	// Label as belonging to the operator, and the specific cluster
	labels := LabelsForCluster(cluster.Name)

	// Select all nodes belonging to the cluster and with the specified services.
	selectors := adminConsoleSelector(cluster)

	// Create the basic service.
	service := createServiceManifest(ConsoleServiceName(cluster.Name), cluster.Spec.AdminConsoleServiceType, ports, labels, selectors)

	if cluster.Spec.IsAdminConsoleServiceTypePublic() {
		// If we are exposing publicly add a DNS name for use with TLS.
		service.Annotations[ddnsAnnotation] = dnsAdminConsoleName(cluster)

		// Also set the external trafic policy to Local which means the load balancer is
		// responsible for routing trafic to the destination and not Kubernetes.  For
		// ELB at least this appears to keep sessions open to the same console instance.
		service.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal

		// AWS ELB does not support IP based session stickiness, instead we rely on
		// the load balancer keeping TCP connection alive.  It's not perfect but we
		// can work until NS server get into gear.
		if cluster.Spec.Platform != couchbasev1.PlatformTypeAWS {
			service.Spec.SessionAffinity = v1.ServiceAffinityClientIP
		}
	} else {
		// Ensure client sessions are mantained by routing to the same backend node based
		// on the client IP address.
		service.Spec.SessionAffinity = v1.ServiceAffinityClientIP
	}

	return service
}

// getServiceNodePort returns the string representation of a NodePort for a named port.
func getServiceNodePort(svc *v1.Service, portName string) string {
	for _, port := range svc.Spec.Ports {
		if port.Name == portName {
			return strconv.Itoa(int(port.NodePort))
		}
	}
	return ""
}

// updateConsoleService updates the console service with the requested state returning
// true if anything happened.
func updateConsoleService(service, requested *v1.Service) bool {
	updated := false
	// This handles spec.ddns.domain updates.
	if !reflect.DeepEqual(service.Annotations, requested.Annotations) {
		service.Annotations = requested.Annotations
		updated = true
	}
	// This handles spec.adminConsoleServices updates.
	if !reflect.DeepEqual(service.Spec.Selector, requested.Spec.Selector) {
		service.Spec.Selector = requested.Spec.Selector
		updated = true
	}
	// This handles spec.adminConsoleServiceType updates.
	if service.Spec.Type != requested.Spec.Type {
		service.Spec.Type = requested.Spec.Type
		updated = true
	}
	return updated
}

// UpdateAdminConsole looks for the cluster's admin console service, creates it if not
// present but requested, deletes it if present but unrequested and performs udpdates
// to the configurable service parameters.
func UpdateAdminConsole(kubecli kubernetes.Interface, cluster *couchbasev1.CouchbaseCluster, status *couchbasev1.ClusterStatus) (ReconcileStatus, error) {
	// Lookup the console service.
	name := cluster.Name + "-ui"
	service, err := GetService(kubecli, name, cluster.Namespace, nil)

	// If it didn't exist but wants to, create it.
	if err != nil {
		switch {
		case IsKubernetesResourceNotFoundError(err):
			if !cluster.Spec.ExposeAdminConsole {
				return ReconcileStatusUnchanged, nil
			}
			service = generateConsoleService(cluster)
			if service, err = createService(kubecli, cluster.Namespace, service, cluster.AsOwner()); err != nil {
				return ReconcileStatusError, err
			}
			status.AdminConsolePort = getServiceNodePort(service, couchbaseUIPortName)
			status.AdminConsolePortSSL = getServiceNodePort(service, couchbaseUIPortNameTLS)
			return ReconcileStatusCreated, nil
		default:
			return ReconcileStatusError, err
		}
	}

	// If it exists but doesn't want to, delete it.
	if !cluster.Spec.ExposeAdminConsole {
		if err := DeleteService(kubecli, cluster.Namespace, name, nil); err != nil {
			return ReconcileStatusError, err
		}
		status.AdminConsolePort = ""
		status.AdminConsolePortSSL = ""
		return ReconcileStatusDeleted, nil
	}

	// Check for modifications and update if necessary.  We only allow dynamic
	// modification of the pod selector and the service type.
	if !updateConsoleService(service, generateConsoleService(cluster)) {
		return ReconcileStatusUnchanged, nil
	}

	if _, err := UpdateService(kubecli, cluster.Namespace, service); err != nil {
		return ReconcileStatusError, err
	}
	return ReconcileStatusUpdated, nil
}

// updateExposedPorts accepts a new or existing service and adds the allocated
// node ports to a port status structure.
func updateExposedPorts(portStatusMap couchbasev1.PortStatusMap, nodeName string, service *v1.Service) error {
	portStatus, ok := portStatusMap[nodeName]
	if !ok {
		portStatus = &couchbasev1.PortStatus{}
		portStatusMap[nodeName] = portStatus
	}
	for _, servicePort := range service.Spec.Ports {
		switch servicePort.Name {
		case adminServicePortName:
			portStatus.AdminServicePort = servicePort.NodePort
		case adminServicePortNameTLS:
			portStatus.AdminServicePortTLS = servicePort.NodePort
		case indexServicePortName:
			portStatus.IndexServicePort = servicePort.NodePort
		case indexServicePortNameTLS:
			portStatus.IndexServicePortTLS = servicePort.NodePort
		case queryServicePortName:
			portStatus.QueryServicePort = servicePort.NodePort
		case queryServicePortNameTLS:
			portStatus.QueryServicePortTLS = servicePort.NodePort
		case searchServicePortName:
			portStatus.SearchServicePort = servicePort.NodePort
		case searchServicePortNameTLS:
			portStatus.SearchServicePortTLS = servicePort.NodePort
		case analyticsServicePortName:
			portStatus.AnalyticsServicePort = servicePort.NodePort
		case analyticsServicePortNameTLS:
			portStatus.AnalyticsServicePortTLS = servicePort.NodePort
		case eventingServicePortName:
			portStatus.EventingServicePort = servicePort.NodePort
		case eventingServicePortNameTLS:
			portStatus.EventingServicePortTLS = servicePort.NodePort
		case dataServicePortName:
			portStatus.DataServicePort = servicePort.NodePort
		case dataServicePortNameTLS:
			portStatus.DataServicePortTLS = servicePort.NodePort
		default:
			return fmt.Errorf("unhandled port name %s", servicePort.Name)
		}
	}
	return nil
}

// GetExposedServiceName returns the service name generated for each service port group
func GetExposedServiceName(nodeName string) string {
	return nodeName + "-exposed-ports"
}

// exposedfeatureSets is a mapping from feature name to a list of host ports to expose
var exposedfeatureSets = map[string][]couchbasev1.Service{
	couchbasev1.FeatureAdmin: []couchbasev1.Service{
		couchbasev1.AdminService,
	},
	couchbasev1.FeatureXDCR: []couchbasev1.Service{
		couchbasev1.AdminService,
		couchbasev1.IndexService,
		couchbasev1.DataService,
	},
	couchbasev1.FeatureClient: []couchbasev1.Service{
		couchbasev1.IndexService,
		couchbasev1.QueryService,
		couchbasev1.SearchService,
		couchbasev1.AnalyticsService,
		couchbasev1.EventingService,
		couchbasev1.DataService,
	},
}

// exposedFeatureSetToServiceList takes a requested feature set and returns
// a list of unique service names
func exposedFeatureSetToServiceList(featureSet couchbasev1.ExposedFeatureList) (couchbasev1.ServiceList, error) {
	// Nothing to do, exit
	serviceList := couchbasev1.ServiceList{}
	serviceSet := map[couchbasev1.Service]interface{}{}
	if featureSet == nil || len(featureSet) == 0 {
		return serviceList, nil
	}

	// Accumulate services defined by features into a set
	for _, featureSet := range featureSet {
		featureSetPorts, ok := exposedfeatureSets[featureSet]
		if !ok {
			return nil, fmt.Errorf("feature set %s undefined", featureSet)
		}
		for _, featureSetPort := range featureSetPorts {
			serviceSet[featureSetPort] = nil
		}
	}

	// Map the set keys into a list
	for service, _ := range serviceSet {
		serviceList = append(serviceList, service)
	}
	return serviceList, nil
}

// listRequestedPorts return the set of ports requested by the specification.
func listRequestedPorts(serviceNames couchbasev1.ServiceList) []v1.ServicePort {
	ports := []v1.ServicePort{}
	for _, serviceName := range serviceNames {
		ports = append(ports, servicePorts[serviceName]...)
	}
	return ports
}

// serviceExists scans a list of services and returns true if the named
// service exists
func serviceExists(services []*v1.Service, name string) bool {
	for _, service := range services {
		if service.Name == name {
			return true
		}
	}
	return false
}

// portExists returns true if a named port exists
func portExists(ports []v1.ServicePort, name string) bool {
	for _, port := range ports {
		if port.Name == name {
			return true
		}
	}
	return false
}

// intersectPorts performs a boolean intersection of ports based on name.
// It returns a subset of a
func intersectPorts(a, b []v1.ServicePort) []v1.ServicePort {
	d := []v1.ServicePort{}
	for _, port := range a {
		if portExists(b, port.Name) {
			d = append(d, port)
		}
	}
	return d
}

// subtractPorts performs a boolean subtraction of ports based on name.
// It returns a subset of a
func subtractPorts(a, b []v1.ServicePort) []v1.ServicePort {
	d := []v1.ServicePort{}
	for _, port := range a {
		if !portExists(b, port.Name) {
			d = append(d, port)
		}
	}
	return d
}

// GetDNSName returns the public DNS name we expect to be created for the member.
func GetDNSName(cluster *couchbasev1.CouchbaseCluster, hostname string) string {
	return hostname + "." + cluster.Name + "." + cluster.Spec.DNS.Domain
}

// GetSRVName returns the public SRV name to create for service discovery.
func GetSRVName(cluster *couchbasev1.CouchbaseCluster) string {
	return "_couchbases._tcp." + cluster.Name + "." + cluster.Spec.DNS.Domain + " 0 0 " + strconv.Itoa(dataServicePortTLS)
}

// filterInsecurePorts takes a set of ports and removes any that are not allowed
// because they inscure.
func filterInsecurePorts(ports []v1.ServicePort, secure bool) []v1.ServicePort {
	p := []v1.ServicePort{}
	for _, port := range ports {
		if !secure || strings.HasSuffix(port.Name, tlsPortNameSuffix) {
			p = append(p, port)
		}
	}
	return p
}

// filterConfiguredPorts takes a set of ports and removes any that are not
// configured for a specific server class.
func filterConfiguredPorts(ports []v1.ServicePort, services couchbasev1.ServiceList) []v1.ServicePort {
	// Admin is not explicitly enabled, it is always there
	enabledPorts := listRequestedPorts(services)
	return intersectPorts(ports, enabledPorts)
}

// memberServices returns the enabled services for a specific member.
func memberServices(cluster *couchbasev1.CouchbaseCluster, members couchbaseutil.MemberSet, name string) (couchbasev1.ServiceList, error) {
	member, ok := members[name]
	if !ok {
		return nil, cberrors.NewErrUnknownMember(name)
	}
	class := cluster.Spec.GetServerConfigByName(member.ServerConfig)
	if class == nil {
		return nil, cberrors.NewErrUnknownServerClass(member.ServerConfig)
	}
	// Append the admin service, this is not explicitly enabled
	return append(class.Services, couchbasev1.AdminService), nil
}

// UpdateExposedFeatureStatus is used to communicate to clients which services have been
// added or created by UpdateExposedFeatureStatus
type UpdateExposedFeatureStatus struct {
	Added   couchbasev1.ServiceList
	Removed couchbasev1.ServiceList
}

// listExposedServices returns all services which are associated with specific pod ports.
func listExposedServices(kubecli kubernetes.Interface, cluster *couchbasev1.CouchbaseCluster) ([]*v1.Service, error) {
	// Get a list of all cluster services that belong to a specific nodes
	clusterRequirement, err := labels.NewRequirement(constants.LabelCluster, selection.Equals, []string{cluster.Name})
	if err != nil {
		return nil, err
	}
	nodeRequirement, err := labels.NewRequirement(constants.LabelNode, selection.Exists, []string{})
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector()
	selector = selector.Add(*clusterRequirement, *nodeRequirement)
	services, err := kubecli.CoreV1().Services(cluster.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	// Convert into pointers
	ret := []*v1.Service{}
	for _, service := range services.Items {
		// Shallow copy
		s := service
		ret = append(ret, &s)
	}

	return ret, nil
}

// generateExposedService creates a Kubernetes Service resource for the requested member.
func generateExposedService(name string, members couchbaseutil.MemberSet, cluster *couchbasev1.CouchbaseCluster, ports []v1.ServicePort) (*v1.Service, error) {
	// Define the new service name.
	exposedServiceName := GetExposedServiceName(name)

	// Attempt to lookup the enabled services on this pod.
	enabledServices, err := memberServices(cluster, members, name)
	if err != nil {
		return nil, err
	}

	// Remove any ports related to services not exposed on the node.
	allowedPorts := filterConfiguredPorts(ports, enabledServices)

	// Create the new service
	labels := labelsForNodeService(cluster.Name, name)
	selectors := getNodeServiceSelectors(cluster, name)
	service := createServiceManifest(exposedServiceName, cluster.Spec.ExposedFeatureServiceType, allowedPorts, labels, selectors)

	// Update external service definitions.
	if cluster.Spec.IsExposedFeatureServiceTypePublic() {
		// Annotate the service with DNS.
		service.Annotations[ddnsAnnotation] = GetDNSName(cluster, name)
		// TODO: data nodes only please
		// TODO: await input from external-dns
		//service.Annotations[srvAnnotaion] = GetSRVName(cluster)
	}

	// Enforce that traffic has to come directly to the k8s node avoiding the
	// penalty of an extra hop and SNAT.
	service.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal

	return service, nil
}

// createExposedServices creates external services for pods if required.  Don't create services if
// there are no ports to expose or the service already exists.
func createExposedServices(services []*v1.Service, members couchbaseutil.MemberSet, cluster *couchbasev1.CouchbaseCluster, ports []v1.ServicePort) (creations []*v1.Service, err error) {
	if !cluster.Spec.HasExposedFeatures() {
		return
	}

	for _, member := range members {
		// Generate the requested resource
		var service *v1.Service
		service, err = generateExposedService(member.Name, members, cluster, ports)
		if err != nil {
			return
		}

		// Ignore if the service already exists, this will get handled by the update code.
		if serviceExists(services, service.Name) {
			continue
		}

		// Only create if allowed
		if len(service.Spec.Ports) != 0 {
			creations = append(creations, service)
		}
	}

	return
}

// updateExposedService applies any modifications to the existing service and returns
// true if an update is required.
func updateExposedService(service, requested *v1.Service) bool {
	updated := false
	// This handles spec.ddns.domain updates.
	if !reflect.DeepEqual(service.Annotations, requested.Annotations) {
		service.Annotations = requested.Annotations
		updated = true
	}
	// This handles updates to spec.exposedFeatures
	existingPorts := intersectPorts(service.Spec.Ports, requested.Spec.Ports)
	requiredPorts := subtractPorts(requested.Spec.Ports, existingPorts)
	portsAdded := len(requiredPorts) != 0
	portsRemoved := len(existingPorts) != len(service.Spec.Ports)
	if portsAdded || portsRemoved {
		service.Spec.Ports = append(existingPorts, requiredPorts...)
		updated = true
	}
	return updated
}

// updateExposedServices examines existing external services.  It first filters the allowable ports on
// a per-member basis so as not to expose services not enabled or allowable by the specification.  It then
// returns an services which require an update or deletion.
func updateExposedServices(services []*v1.Service, members couchbaseutil.MemberSet, cluster *couchbasev1.CouchbaseCluster, ports []v1.ServicePort) (updates []*v1.Service, deletions []*v1.Service, err error) {
	for _, service := range services {
		// Extract metadata from the service
		memberName, ok := service.Labels[constants.LabelNode]
		if !ok {
			err = fmt.Errorf("exposed service %s missing node label", service.Name)
		}

		// Generate the requested resource
		var requested *v1.Service
		requested, err = generateExposedService(memberName, members, cluster, ports)

		// If the server class is missing then we allow for the node to balanced out.
		// If the member is missing we delete it.
		if err != nil {
			switch {
			case cberrors.IsErrUnknownServerClass(err):
				err = nil
				continue
			case cberrors.IsErrUnknownMember(err):
				deletions = append(deletions, service)
				err = nil
				continue
			default:
				return
			}
		}

		// Delete the service if nothing needs to be exposed.
		if len(requested.Spec.Ports) == 0 {
			deletions = append(deletions, service)
			continue
		}

		// Update if necessary.
		if updateExposedService(service, requested) {
			updates = append(updates, service)
		}
	}

	return
}

// UpdateExposedFeatures handles adding and removing per-pod node ports for all
// services.  Rather than specify individual ports we instead allow addition/deletion
// via feature sets.  There is some overlap between sets so we perform a boolean union
// before processing.  The function returns lists of added and removed services so
// client code can perform any notifications, these are lexically sorted for determinism.
func UpdateExposedFeatures(kubecli kubernetes.Interface, members couchbaseutil.MemberSet, cluster *couchbasev1.CouchbaseCluster, status *couchbasev1.ClusterStatus) (*UpdateExposedFeatureStatus, error) {
	// For each feature set accumulate a unique set of services to expose, then map these to
	// a set of ports we wish to expose.
	serviceNames, err := exposedFeatureSetToServiceList(cluster.Spec.ExposedFeatures)
	if err != nil {
		return nil, err
	}
	ports := listRequestedPorts(serviceNames)

	// Filter out any insecure ports if we need to
	ports = filterInsecurePorts(ports, cluster.Spec.IsExposedFeatureServiceTypePublic())

	// Get a list of pre-exising external services that belong to our members.
	services, err := listExposedServices(kubecli, cluster)
	if err != nil {
		return nil, err
	}

	// Calculate the services that need creating  based on the member status and
	// cluster specification.
	creations, err := createExposedServices(services, members, cluster, ports)
	if err != nil {
		return nil, err
	}

	// Calculate the services that need updating or deleting based on the member status
	// and cluster specification.
	updates, deletions, err := updateExposedServices(services, members, cluster, ports)
	if err != nil {
		return nil, err
	}

	// Buffer the port status as we go through
	portStatus := map[string]*couchbasev1.PortStatus{}

	// Create any required services, buffering exposed ports if we aren't using public
	// DNS based addressing.
	for _, service := range creations {
		if service, err = createService(kubecli, cluster.Namespace, service, cluster.AsOwner()); err != nil {
			return nil, err
		}
		if cluster.Spec.IsExposedFeatureServiceTypePublic() {
			continue
		}
		if err := updateExposedPorts(portStatus, service.Labels[constants.LabelNode], service); err != nil {
			return nil, err
		}
	}

	// Update any required services, buffering exposed ports if we aren't using public
	// DNS based addressing.
	for _, service := range updates {
		if service, err = UpdateService(kubecli, cluster.Namespace, service); err != nil {
			return nil, err
		}
		if cluster.Spec.IsExposedFeatureServiceTypePublic() {
			continue
		}
		if err := updateExposedPorts(portStatus, service.Labels[constants.LabelNode], service); err != nil {
			return nil, err
		}
	}

	// Delete any required services.
	for _, service := range deletions {
		if err := DeleteService(kubecli, cluster.Namespace, service.Name, nil); err != nil {
			return nil, err
		}
	}

	// Calculate which services have been added or removed
	prevServiceNames, err := exposedFeatureSetToServiceList(status.ExposedFeatures)
	if err != nil {
		return nil, err
	}
	ret := &UpdateExposedFeatureStatus{
		Added:   serviceNames.Sub(prevServiceNames),
		Removed: prevServiceNames.Sub(serviceNames),
	}
	sort.Sort(ret.Added)
	sort.Sort(ret.Removed)

	// Finally update the status
	status.ExposedFeatures = cluster.Spec.ExposedFeatures
	if len(portStatus) == 0 {
		portStatus = nil
	}
	status.ExposedPorts = portStatus

	return ret, nil
}

// TEMPORARY HACK
// Does exactly the same as above but tells us if we need to do anything
func WouldUpdateExposedFeatures(kubecli kubernetes.Interface, members couchbaseutil.MemberSet, cluster *couchbasev1.CouchbaseCluster) (bool, error) {
	// For each feature set accumulate a unique set of services to expose
	serviceNames, err := exposedFeatureSetToServiceList(cluster.Spec.ExposedFeatures)
	if err != nil {
		return false, err
	}

	// Create the list of ports each node should have
	ports := listRequestedPorts(serviceNames)

	// Filter out any insecure ports if we need to
	ports = filterInsecurePorts(ports, cluster.Spec.IsExposedFeatureServiceTypePublic())

	// Get a list of all cluster services that belong to a specific nodes
	services, err := listExposedServices(kubecli, cluster)
	if err != nil {
		return false, err
	}

	// Calculate the services that need creating  based on the member status and
	// cluster specification.
	creations, err := createExposedServices(services, members, cluster, ports)
	if err != nil {
		return false, err
	}

	// Calculate the services that need updating or deleting based on the member status
	// and cluster specification.
	updates, deletions, err := updateExposedServices(services, members, cluster, ports)
	if err != nil {
		return false, err
	}

	// Flag we need a reconcile if any services are created, updated or deleted.
	return len(creations)+len(updates)+len(deletions) != 0, nil
}
