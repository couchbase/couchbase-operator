package k8sutil

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// Used to annotate services which belong to an SRV record.
	//srvAnnotaion = "external-dns.alpha.kubernetes.io/service"

	// Fixed port names (generate SRV records for the peer services)
	couchbaseSRVName    = "couchbase"
	couchbaseSRVNameTLS = "couchbases"

	couchbaseUIPortName    = "couchbase-ui"
	couchbaseUIPortNameTLS = couchbaseUIPortName + tlsPortNameSuffix

	// tlsBasePort is the port number relative to which TLS ports are (usually) translated
	tlsBasePort       = 10000
	tlsPortNameSuffix = "-tls"

	// Admin service constants
	adminServicePortName    = string(couchbasev2.AdminService)
	adminServicePortNameTLS = string(couchbasev2.AdminService) + tlsPortNameSuffix
	adminServicePort        = 8091
	adminServicePortTLS     = tlsBasePort + adminServicePort

	// Index service constants
	indexServicePortName    = string(couchbasev2.IndexService)
	indexServicePortNameTLS = string(couchbasev2.IndexService) + tlsPortNameSuffix
	indexServicePort        = 8092
	indexServicePortTLS     = tlsBasePort + indexServicePort

	// Query service constants
	queryServicePortName    = string(couchbasev2.QueryService)
	queryServicePortNameTLS = string(couchbasev2.QueryService) + tlsPortNameSuffix
	queryServicePort        = 8093
	queryServicePortTLS     = tlsBasePort + queryServicePort

	// Full text search service constants
	searchServicePortName    = string(couchbasev2.SearchService)
	searchServicePortNameTLS = string(couchbasev2.SearchService) + tlsPortNameSuffix
	searchServicePort        = 8094
	searchServicePortTLS     = tlsBasePort + searchServicePort

	// Analytics service constants
	analyticsServicePortName    = string(couchbasev2.AnalyticsService)
	analyticsServicePortNameTLS = string(couchbasev2.AnalyticsService) + tlsPortNameSuffix
	analyticsServicePort        = 8095
	analyticsServicePortTLS     = tlsBasePort + analyticsServicePort

	// Eventing service constants
	eventingServicePortName    = string(couchbasev2.EventingService)
	eventingServicePortNameTLS = string(couchbasev2.EventingService) + tlsPortNameSuffix
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
	dataServicePortName    = string(couchbasev2.DataService)
	dataServicePortNameTLS = string(couchbasev2.DataService) + tlsPortNameSuffix
	dataServicePort        = 11210
	dataServicePortTLS     = 11207
)

var (
	// Istio needs every port to be defined in a service in order for them to be
	// picked up by the control plane and handled by the data plane.  Obviously
	// Couchbase makes this particularly hard...
	allTheThings = [][]int{
		{4369},
		{8091, 8096},
		{9100, 9105},
		{9110, 9118},
		{9120, 9122},
		{9999},
		{11207},
		{11209, 11211},
		{11213},
		{18091, 18096},
		{21100, 21299},
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
	servicePorts = map[couchbasev2.Service][]v1.ServicePort{
		couchbasev2.AdminService: {
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
		couchbasev2.IndexService: {
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
		couchbasev2.QueryService: {
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
		couchbasev2.SearchService: {
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
		couchbasev2.AnalyticsService: {
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
		couchbasev2.EventingService: {
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
		couchbasev2.DataService: {
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
	ReconcileStatusCreated   ReconcileStatus = "created"
	ReconcileStatusDeleted   ReconcileStatus = "deleted"
	ReconcileStatusUpdated   ReconcileStatus = "updated"
	ReconcileStatusUnchanged ReconcileStatus = "unchanged"
)

// createServiceManifest creates a basic service with a type, ports, labels and pod selectors.
func createServiceManifest(svcName string, serviceType v1.ServiceType, ports []v1.ServicePort, labels, selector map[string]string) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   svcName,
			Labels: labels,
		},
		Spec: v1.ServiceSpec{
			Type:                     serviceType,
			Ports:                    ports,
			Selector:                 selector,
			PublishNotReadyAddresses: true,
		},
	}

	ApplyBaseAnnotations(svc)

	return svc
}

func createService(c *client.Client, ns string, svc *v1.Service, owner metav1.OwnerReference) (*v1.Service, error) {
	addOwnerRefToObject(svc, owner)
	return c.KubeClient.CoreV1().Services(ns).Create(svc)
}

func deleteService(c *client.Client, ns, name string, opts *metav1.DeleteOptions) error {
	return c.KubeClient.CoreV1().Services(ns).Delete(name, opts)
}

func updateService(c *client.Client, ns string, svc *v1.Service) (*v1.Service, error) {
	return c.KubeClient.CoreV1().Services(ns).Update(svc)
}

// dnsAdminConsoleName returns the DNS name for the admin console.
func dnsAdminConsoleName(cluster *couchbasev2.CouchbaseCluster) string {
	return "console." + cluster.Spec.Networking.DNS.Domain
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

// getPeerServicePorts returns the set of all possible ports.  When running in a
// service mesh it will deny access unless they are referenced by at least one
// service.
func getPeerServicePorts() ([]v1.ServicePort, error) {
	// Create a service which defines A records for all pods, we use this internally
	// to address nodes via stable names (IPs are not fixed)
	ports := []v1.ServicePort{}

	for _, rule := range allTheThings {
		switch len(rule) {
		case 1:
			ports = append(ports, v1.ServicePort{
				Name: fmt.Sprintf("tcp-%v", rule[0]),
				Port: int32(rule[0]),
			})
		case 2:
			for i := rule[0]; i <= rule[1]; i++ {
				ports = append(ports, v1.ServicePort{
					Name: fmt.Sprintf("tcp-%v", i),
					Port: int32(i),
				})
			}
		default:
			return nil, fmt.Errorf("illegal port rule: %v", rule)
		}
	}

	return ports, nil
}

func updatePeerService(current, requested *v1.Service) bool {
	updated := false

	if !reflect.DeepEqual(current.Labels, requested.Labels) {
		current.Labels = requested.Labels
		updated = true
	}

	if !reflect.DeepEqual(current.Annotations, requested.Annotations) {
		current.Annotations = requested.Annotations
		updated = true
	}

	if current.Spec.PublishNotReadyAddresses != requested.Spec.PublishNotReadyAddresses {
		current.Spec.PublishNotReadyAddresses = requested.Spec.PublishNotReadyAddresses
		updated = true
	}

	if !reflect.DeepEqual(current.Spec.LoadBalancerSourceRanges, requested.Spec.LoadBalancerSourceRanges) {
		current.Spec.LoadBalancerSourceRanges = requested.Spec.LoadBalancerSourceRanges
		updated = true
	}

	// Filled in by the API, so reset to what we generate
	for i := range current.Spec.Ports {
		current.Spec.Ports[i].TargetPort = intstr.IntOrString{}
	}

	// Unlike NodePort services we don't need to preserve TargetPorts so
	// can just copy over the whole Spec.
	if !reflect.DeepEqual(current.Spec, requested.Spec) {
		current.Spec = requested.Spec
		updated = true
	}

	return updated
}

// reconcilePeerService either creates the peer service if it doesn't exist
// or updates an existing version if it has been edited or has been upgraded.
func reconcilePeerService(c *client.Client, namespace, name string, owner metav1.OwnerReference) error {
	serviceName := name

	ports, err := getPeerServicePorts()
	if err != nil {
		return err
	}

	labels := LabelsForCluster(name)
	requested := createServiceManifest(serviceName, v1.ServiceTypeClusterIP, ports, labels, labels)
	requested.Spec.ClusterIP = v1.ClusterIPNone

	// Create if it doesn't exist.
	current, found := c.Services.Get(serviceName)
	if !found {
		_, err := createService(c, namespace, requested, owner)
		return err
	}

	// Update an existing service if it does exist.
	if !updatePeerService(current, requested) {
		return nil
	}

	if _, err := updateService(c, namespace, current); err != nil {
		return err
	}

	return nil
}

// reconcileDiscoveryService either creates the discovery service if it doesn't exist
// or updates an existing version if it has been edited or has been upgraded.
func reconcileDiscoveryService(c *client.Client, namespace, name string, owner metav1.OwnerReference) error {
	serviceName := name + "-srv"

	labels := LabelsForCluster(name)

	// Create a service which defines only data nodes.  This is the expected way clients
	// using SRV records will connect, meaning they will only ever bootstrap via memcached
	selectors := LabelsForCluster(name)
	selectors["couchbase_service_data"] = "enabled"

	requested := createServiceManifest(serviceName, v1.ServiceTypeClusterIP, srvServicePorts, labels, selectors)
	requested.Spec.ClusterIP = v1.ClusterIPNone

	// Create if it doesn't exist.
	current, found := c.Services.Get(serviceName)
	if !found {
		_, err := createService(c, namespace, requested, owner)
		return err
	}

	// Update an existing service if it does exist.
	if !updatePeerService(current, requested) {
		return nil
	}

	if _, err := updateService(c, namespace, current); err != nil {
		return err
	}

	return nil
}

// ReconcilePeerServices creates/updates all cluster-wide services required for
// communication to and discovery of the cluster.
func ReconcilePeerServices(c *client.Client, namespace, name string, owner metav1.OwnerReference) error {
	if err := reconcilePeerService(c, namespace, name, owner); err != nil {
		return err
	}

	if err := reconcileDiscoveryService(c, namespace, name, owner); err != nil {
		return err
	}

	return nil
}

// adminConsoleSelector generates a selector matching pods running all the requested services.
func adminConsoleSelector(cluster *couchbasev2.CouchbaseCluster) map[string]string {
	labels := LabelsForCluster(cluster.Name)

	for _, s := range cluster.Spec.Networking.AdminConsoleServices {
		k := "couchbase_service_" + s.String()
		labels[k] = "enabled"
	}

	return labels
}

// generateConsoleService creates a new Service resource based on the cluster specification.
func generateConsoleService(cluster *couchbasev2.CouchbaseCluster) *v1.Service {
	// If the service is public, remove non TLS ports.
	ports := filterInsecurePorts(uiServicePorts, cluster.Spec.IsAdminConsoleServiceTypePublic())

	// Label as belonging to the operator, and the specific cluster
	labels := LabelsForCluster(cluster.Name)

	// Select all nodes belonging to the cluster and with the specified services.
	selectors := adminConsoleSelector(cluster)

	// Create the basic service.
	service := createServiceManifest(ConsoleServiceName(cluster.Name), cluster.Spec.Networking.AdminConsoleServiceType, ports, labels, selectors)
	service.Annotations = mergeLabels(service.Annotations, cluster.Spec.Networking.ServiceAnnotations)

	// If a DNS domain is specified add a DNS name for use with TLS.
	if cluster.Spec.Networking.DNS != nil {
		service.Annotations[constants.DNSAnnotation] = dnsAdminConsoleName(cluster)
	}

	if cluster.Spec.IsAdminConsoleServiceTypePublic() {
		// Also set the external traffic policy to Local which means the load balancer is
		// responsible for routing traffic to the destination and not Kubernetes.  For
		// ELB at least this appears to keep sessions open to the same console instance.
		service.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal

		// AWS ELB does not support IP based session stickiness, instead we rely on
		// the load balancer keeping TCP connection alive.  It's not perfect but we
		// can work until NS server get into gear.
		if cluster.Spec.Platform != couchbasev2.PlatformTypeAWS {
			service.Spec.SessionAffinity = v1.ServiceAffinityClientIP
		}

		service.Spec.LoadBalancerSourceRanges = cluster.Spec.Networking.LoadBalancerSourceRanges.StringSlice()
	} else {
		// Ensure client sessions are maintained by routing to the same backend node based
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

	if service.Spec.PublishNotReadyAddresses != requested.Spec.PublishNotReadyAddresses {
		service.Spec.PublishNotReadyAddresses = requested.Spec.PublishNotReadyAddresses
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

	// sync session affinity from generated service since this value
	// may also change when Type  or Platform changes
	if service.Spec.SessionAffinity != requested.Spec.SessionAffinity {
		service.Spec.SessionAffinity = requested.Spec.SessionAffinity
		updated = true
	}

	return updated
}

// UpdateAdminConsole looks for the cluster's admin console service, creates it if not
// present but requested, deletes it if present but unrequested and performs udpdates
// to the configurable service parameters.
func UpdateAdminConsole(c *client.Client, cluster *couchbasev2.CouchbaseCluster, status *couchbasev2.ClusterStatus) (ReconcileStatus, error) {
	// Lookup the console service.
	name := cluster.Name + "-ui"

	service, found := c.Services.Get(name)
	if !found {
		if !cluster.Spec.Networking.ExposeAdminConsole {
			return ReconcileStatusUnchanged, nil
		}

		service = generateConsoleService(cluster)

		var err error

		if service, err = createService(c, cluster.Namespace, service, cluster.AsOwner()); err != nil {
			return ReconcileStatusError, err
		}

		status.AdminConsolePort = getServiceNodePort(service, couchbaseUIPortName)
		status.AdminConsolePortSSL = getServiceNodePort(service, couchbaseUIPortNameTLS)

		return ReconcileStatusCreated, nil
	}

	// If it exists but doesn't want to, delete it.
	if !cluster.Spec.Networking.ExposeAdminConsole {
		if err := deleteService(c, cluster.Namespace, name, nil); err != nil {
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

	if _, err := updateService(c, cluster.Namespace, service); err != nil {
		return ReconcileStatusError, err
	}

	return ReconcileStatusUpdated, nil
}

// GetExposedServiceName returns the service name generated for each service port group.
func GetExposedServiceName(nodeName string) string {
	return nodeName
}

// exposedfeatureSets is a mapping from feature name to a list of host ports to expose
var exposedfeatureSets = map[couchbasev2.ExposedFeature][]couchbasev2.Service{
	couchbasev2.FeatureAdmin: {
		couchbasev2.AdminService,
	},
	couchbasev2.FeatureXDCR: {
		couchbasev2.AdminService,
		couchbasev2.IndexService,
		couchbasev2.DataService,
	},
	couchbasev2.FeatureClient: {
		couchbasev2.AdminService,
		couchbasev2.IndexService,
		couchbasev2.QueryService,
		couchbasev2.SearchService,
		couchbasev2.AnalyticsService,
		couchbasev2.EventingService,
		couchbasev2.DataService,
	},
}

// exposedFeatureSetToServiceList takes a requested feature set and returns
// a list of unique service names.
func exposedFeatureSetToServiceList(featureSet couchbasev2.ExposedFeatureList) (couchbasev2.ServiceList, error) {
	// Nothing to do, exit
	serviceList := couchbasev2.ServiceList{}
	serviceSet := map[couchbasev2.Service]interface{}{}

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
	for service := range serviceSet {
		serviceList = append(serviceList, service)
	}

	return serviceList, nil
}

// listRequestedPorts return the set of ports requested by the specification.
func listRequestedPorts(serviceNames couchbasev2.ServiceList) []v1.ServicePort {
	ports := []v1.ServicePort{}

	for _, serviceName := range serviceNames {
		ports = append(ports, servicePorts[serviceName]...)
	}

	return ports
}

// serviceExists scans a list of services and returns true if the named
// service exists.
func serviceExists(services []*v1.Service, name string) bool {
	for _, service := range services {
		if service.Name == name {
			return true
		}
	}

	return false
}

// portExists returns true if a named port exists.
func portExists(ports []v1.ServicePort, name string) bool {
	for _, port := range ports {
		if port.Name == name {
			return true
		}
	}

	return false
}

// intersectPorts performs a boolean intersection of ports based on name.
// It returns a subset of a.
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
// It returns a subset of a.
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
func GetDNSName(cluster *couchbasev2.CouchbaseCluster, hostname string) string {
	return hostname + "." + cluster.Spec.Networking.DNS.Domain
}

// GetSRVName returns the public SRV name to create for service discovery.
func GetSRVName(cluster *couchbasev2.CouchbaseCluster) string {
	return "_couchbases._tcp." + cluster.Spec.Networking.DNS.Domain + " 0 0 " + strconv.Itoa(dataServicePortTLS)
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
func filterConfiguredPorts(ports []v1.ServicePort, services couchbasev2.ServiceList) []v1.ServicePort {
	// Admin is not explicitly enabled, it is always there
	enabledPorts := listRequestedPorts(services)
	return intersectPorts(ports, enabledPorts)
}

// memberServices returns the enabled services for a specific member.
func memberServices(cluster *couchbasev2.CouchbaseCluster, members couchbaseutil.MemberSet, name string) (couchbasev2.ServiceList, error) {
	member, ok := members[name]
	if !ok {
		return nil, cberrors.NewErrUnknownMember(name)
	}

	class := cluster.Spec.GetServerConfigByName(member.ServerConfig)
	if class == nil {
		return nil, cberrors.NewErrUnknownServerClass(member.ServerConfig)
	}

	// Append the admin service, this is not explicitly enabled
	return append(class.Services, couchbasev2.AdminService), nil
}

// UpdateExposedFeatureStatus is used to communicate to clients which services have been
// added or created by UpdateExposedFeatureStatus
type UpdateExposedFeatureStatus struct {
	Added   couchbasev2.ServiceList
	Removed couchbasev2.ServiceList
}

// listExposedServices returns all services which are associated with specific pod ports.
func listExposedServices(c *client.Client) (services []*v1.Service) {
	// Filter out non-node services
	for _, service := range c.Services.List() {
		if _, ok := service.Labels[constants.LabelNode]; ok {
			services = append(services, service)
		}
	}

	return
}

// generateExposedService creates a Kubernetes Service resource for the requested member.
func generateExposedService(name string, members couchbaseutil.MemberSet, cluster *couchbasev2.CouchbaseCluster, ports []v1.ServicePort) (*v1.Service, error) {
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
	service := createServiceManifest(exposedServiceName, cluster.Spec.Networking.ExposedFeatureServiceType, allowedPorts, labels, selectors)
	service.Annotations = mergeLabels(service.Annotations, cluster.Spec.Networking.ServiceAnnotations)

	if cluster.Spec.IsExposedFeatureServiceTypePublic() {
		service.Spec.LoadBalancerSourceRanges = cluster.Spec.Networking.LoadBalancerSourceRanges.StringSlice()
	}

	// If a DNS domain is specified annotate with the pod DNS name.
	if cluster.Spec.Networking.DNS != nil {
		// TODO: data nodes only please
		// TODO: await input from external-dns
		//service.Annotations[srvAnnotaion] = GetSRVName(cluster)
		service.Annotations[constants.DNSAnnotation] = GetDNSName(cluster, name)
	}

	// Enforce that traffic has to come directly to the k8s node avoiding the
	// penalty of an extra hop and SNAT.
	service.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	if cluster.Spec.Networking.ExposedFeatureTrafficPolicy != nil {
		service.Spec.ExternalTrafficPolicy = *cluster.Spec.Networking.ExposedFeatureTrafficPolicy
	}

	return service, nil
}

// createExposedServices creates external services for pods if required.  Don't create services if
// there are no ports to expose or the service already exists.
func createExposedServices(services []*v1.Service, members couchbaseutil.MemberSet, cluster *couchbasev2.CouchbaseCluster, ports []v1.ServicePort) (creations []*v1.Service, err error) {
	if !cluster.Spec.HasExposedFeatures() {
		return
	}

	for _, member := range members {
		// Generate the requested resource
		var service *v1.Service

		service, err = generateExposedService(member.Name, members, cluster, ports)

		// If the server class is missing then we allow for the node to balanced out.
		if err != nil {
			switch {
			case cberrors.IsErrUnknownServerClass(err):
				err = nil
				continue
			default:
				return
			}
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

	// This handles updates to the service type
	if service.Spec.Type != requested.Spec.Type {
		service.Spec.Type = requested.Spec.Type
		service.Spec.Type = requested.Spec.Type
		updated = true
	}

	// This handles traffic policy updates
	if service.Spec.ExternalTrafficPolicy != requested.Spec.ExternalTrafficPolicy {
		service.Spec.Type = requested.Spec.Type
		service.Spec.ExternalTrafficPolicy = requested.Spec.ExternalTrafficPolicy
		updated = true
	}

	if !reflect.DeepEqual(service.Spec.LoadBalancerSourceRanges, requested.Spec.LoadBalancerSourceRanges) {
		service.Spec.LoadBalancerSourceRanges = requested.Spec.LoadBalancerSourceRanges
		updated = true
	}

	// This handles updates to spec.exposedFeatures
	existingPorts := intersectPorts(service.Spec.Ports, requested.Spec.Ports)
	requiredPorts := subtractPorts(requested.Spec.Ports, existingPorts)

	portsAdded := len(requiredPorts) != 0
	portsRemoved := len(existingPorts) != len(service.Spec.Ports)

	if portsAdded || portsRemoved {
		updatedPorts := existingPorts
		updatedPorts = append(updatedPorts, requiredPorts...)

		service.Spec.Type = requested.Spec.Type
		service.Spec.Ports = updatedPorts
		updated = true
	}

	return updated
}

// updateExposedServices examines existing external services.  It first filters the allowable ports on
// a per-member basis so as not to expose services not enabled or allowable by the specification.  It then
// returns an services which require an update or deletion.
func updateExposedServices(services []*v1.Service, members couchbaseutil.MemberSet, cluster *couchbasev2.CouchbaseCluster, ports []v1.ServicePort) (updates, deletions []*v1.Service, err error) {
	for _, service := range services {
		// Extract metadata from the service
		memberName, ok := service.Labels[constants.LabelNode]
		if !ok {
			err = fmt.Errorf("exposed service %s missing node label", service.Name)
			return
		}

		// Generate the requested resource
		var requested *v1.Service

		// If the server class is missing then we allow for the node to balanced out.
		// If the member is missing we delete it.
		requested, err = generateExposedService(memberName, members, cluster, ports)
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
func UpdateExposedFeatures(c *client.Client, members couchbaseutil.MemberSet, cluster *couchbasev2.CouchbaseCluster, status *couchbasev2.ClusterStatus) (*UpdateExposedFeatureStatus, error) {
	// For each feature set accumulate a unique set of services to expose, then map these to
	// a set of ports we wish to expose.
	serviceNames, err := exposedFeatureSetToServiceList(cluster.Spec.Networking.ExposedFeatures)
	if err != nil {
		return nil, err
	}

	ports := listRequestedPorts(serviceNames)

	// Filter out any insecure ports if we need to
	ports = filterInsecurePorts(ports, cluster.Spec.IsExposedFeatureServiceTypePublic())

	// Get a list of pre-existing external services that belong to our members.
	services := listExposedServices(c)

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

	// Create any required services, buffering exposed ports if we aren't using public
	// DNS based addressing.
	for _, service := range creations {
		if _, err := createService(c, cluster.Namespace, service, cluster.AsOwner()); err != nil {
			return nil, err
		}

		if cluster.Spec.IsExposedFeatureServiceTypePublic() {
			continue
		}
	}

	// Update any required services, buffering exposed ports if we aren't using public
	// DNS based addressing.
	for _, service := range updates {
		if _, err := updateService(c, cluster.Namespace, service); err != nil {
			return nil, err
		}

		if cluster.Spec.IsExposedFeatureServiceTypePublic() {
			continue
		}
	}

	// Delete any required services.
	for _, service := range deletions {
		if err := deleteService(c, cluster.Namespace, service.Name, nil); err != nil {
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
	status.ExposedFeatures = cluster.Spec.Networking.ExposedFeatures

	return ret, nil
}

// TEMPORARY HACK
// Does exactly the same as above but tells us if we need to do anything.
func WouldUpdateExposedFeatures(c *client.Client, members couchbaseutil.MemberSet, cluster *couchbasev2.CouchbaseCluster) (bool, error) {
	// For each feature set accumulate a unique set of services to expose
	serviceNames, err := exposedFeatureSetToServiceList(cluster.Spec.Networking.ExposedFeatures)
	if err != nil {
		return false, err
	}

	// Create the list of ports each node should have
	ports := listRequestedPorts(serviceNames)

	// Filter out any insecure ports if we need to
	ports = filterInsecurePorts(ports, cluster.Spec.IsExposedFeatureServiceTypePublic())

	// Get a list of all cluster services that belong to a specific nodes
	services := listExposedServices(c)

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

// GetAlternateAddressExternalPorts polls the pod service for any alternate ports
// that may have been set and returns a structure ready for submission to the
// Couchbase API.
func GetAlternateAddressExternalPorts(c *client.Client, namespace, name string) (*couchbaseutil.AlternateAddressesExternalPorts, error) {
	svc, found := c.Services.Get(name)

	// Not created yet.
	if !found {
		return nil, nil
	}

	// Only NodePorts do DNAT
	if svc.Spec.Type != v1.ServiceTypeNodePort {
		return nil, nil
	}

	ports := &couchbaseutil.AlternateAddressesExternalPorts{}

	for _, port := range svc.Spec.Ports {
		switch port.Name {
		case adminServicePortName:
			ports.AdminServicePort = port.NodePort
		case adminServicePortNameTLS:
			ports.AdminServicePortTLS = port.NodePort
		case indexServicePortName:
			ports.IndexServicePort = port.NodePort
		case indexServicePortNameTLS:
			ports.IndexServicePortTLS = port.NodePort
		case queryServicePortName:
			ports.QueryServicePort = port.NodePort
		case queryServicePortNameTLS:
			ports.QueryServicePortTLS = port.NodePort
		case searchServicePortName:
			ports.SearchServicePort = port.NodePort
		case searchServicePortNameTLS:
			ports.SearchServicePortTLS = port.NodePort
		case analyticsServicePortName:
			ports.AnalyticsServicePort = port.NodePort
		case analyticsServicePortNameTLS:
			ports.AnalyticsServicePortTLS = port.NodePort
		case eventingServicePortName:
			ports.EventingServicePort = port.NodePort
		case eventingServicePortNameTLS:
			ports.EventingServicePortTLS = port.NodePort
		case dataServicePortName:
			ports.DataServicePort = port.NodePort
		case dataServicePortNameTLS:
			ports.DataServicePortTLS = port.NodePort
		default:
			return nil, fmt.Errorf("unexpected port name %s", port.Name)
		}
	}

	return ports, nil
}
