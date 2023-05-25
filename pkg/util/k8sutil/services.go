package k8sutil

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/diff"

	"github.com/imdario/mergo"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("kubernetes")

const (
	// Fixed port names (generate SRV records for the peer services).
	couchbaseSRVName    = "couchbase"
	couchbaseSRVNameTLS = "couchbases"

	couchbaseUIPortName    = "couchbase-ui"
	couchbaseUIPortNameTLS = couchbaseUIPortName + tlsPortNameSuffix

	// tlsBasePort is the port number relative to which TLS ports are (usually) translated.
	tlsBasePort       = 10000
	tlsPortNameSuffix = "-tls"

	// Admin service constants.
	adminServicePortName    = string(couchbasev2.AdminService)
	adminServicePortNameTLS = string(couchbasev2.AdminService) + tlsPortNameSuffix
	AdminServicePort        = 8091
	AdminServicePortTLS     = tlsBasePort + AdminServicePort

	// Query service constants.
	queryServicePortName    = string(couchbasev2.QueryService)
	queryServicePortNameTLS = string(couchbasev2.QueryService) + tlsPortNameSuffix
	queryServicePort        = 8093
	queryServicePortTLS     = tlsBasePort + queryServicePort

	// Full text search service constants.
	searchServicePortName    = string(couchbasev2.SearchService)
	searchServicePortNameTLS = string(couchbasev2.SearchService) + tlsPortNameSuffix
	searchServicePort        = 8094
	searchServicePortTLS     = tlsBasePort + searchServicePort

	// Analytics service constants.
	analyticsServicePortName    = string(couchbasev2.AnalyticsService)
	analyticsServicePortNameTLS = string(couchbasev2.AnalyticsService) + tlsPortNameSuffix
	analyticsServicePort        = 8095
	analyticsServicePortTLS     = tlsBasePort + analyticsServicePort

	// Eventing service constants.
	eventingServicePortName    = string(couchbasev2.EventingService)
	eventingServicePortNameTLS = string(couchbasev2.EventingService) + tlsPortNameSuffix
	eventingServicePort        = 8096
	eventingServicePortTLS     = tlsBasePort + eventingServicePort

	// Index service constants - there are none!

	// Data service constants.
	dataServicePortName    = string(couchbasev2.DataService)
	dataServicePortNameTLS = string(couchbasev2.DataService) + tlsPortNameSuffix
	dataServicePort        = 11210
	dataServicePortTLS     = 11207

	// View service constants: part of the data service and not index service.
	viewService            = "view"
	viewServicePortName    = viewService
	viewServicePortNameTLS = viewService + tlsPortNameSuffix
	viewServicePort        = 8092
	viewServicePortTLS     = tlsBasePort + viewServicePort

	// stellar-nebula-gateway related constants.
	snDataPort = 18098
	snSdPort   = 18099
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
		{9130},
		{9140},
		{9999},
		{11207},
		{11209, 11210},
		{18091, 18096},
		{19130},
		{21100},
		{21150},
	}

	// uiServicePorts is the list of ports used to expose the Couchbase admin console.
	uiServicePorts = []v1.ServicePort{
		{
			Name:     couchbaseUIPortName,
			Port:     AdminServicePort,
			Protocol: v1.ProtocolTCP,
		},
		{
			Name:     couchbaseUIPortNameTLS,
			Port:     AdminServicePortTLS,
			Protocol: v1.ProtocolTCP,
		},
	}

	// clientBootstrapPorts are the data ports used for sdk bootstrapping.
	clientBootstrapPorts = []v1.ServicePort{
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
	// pod (overlay) network. Note that IndexService has no ports to map.
	servicePorts = map[couchbasev2.Service][]v1.ServicePort{
		couchbasev2.AdminService: {
			{
				Name:     adminServicePortName,
				Port:     AdminServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     adminServicePortNameTLS,
				Port:     AdminServicePortTLS,
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
			// The data service also exposes the view service:
			{
				Name:     viewServicePortName,
				Port:     viewServicePort,
				Protocol: v1.ProtocolTCP,
			},
			{
				Name:     viewServicePortNameTLS,
				Port:     viewServicePortTLS,
				Protocol: v1.ProtocolTCP,
			},
		},
	}
)

// dnsAdminConsoleName returns the DNS name for the admin console.
func dnsAdminConsoleName(cluster *couchbasev2.CouchbaseCluster) string {
	return "console." + cluster.Spec.Networking.DNS.Domain
}

// ConsoleServiceName generates the console service name.
func ConsoleServiceName(clusterName string) string {
	return clusterName + "-ui"
}

// getPeerServicePort gets a service port for a service.
// This is aware of istio and special services that need SRV records.
func getPeerServicePort(port int) v1.ServicePort {
	name := fmt.Sprintf("tcp-%d", port)

	switch {
	case port == dataServicePort:
		name = couchbaseSRVName
	case port == dataServicePortTLS:
		name = couchbaseSRVNameTLS
	}

	p := v1.ServicePort{
		Name:     name,
		Port:     int32(port),
		Protocol: v1.ProtocolTCP,
	}

	return p
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
			ports = append(ports, getPeerServicePort(rule[0]))
		case 2:
			for i := rule[0]; i <= rule[1]; i++ {
				ports = append(ports, getPeerServicePort(i))
			}
		default:
			return nil, fmt.Errorf("%w: illegal port rule: %v", errors.NewStackTracedError(errors.ErrInternalError), rule)
		}
	}

	return ports, nil
}

// generatePeerService returns an idealized peer service.  This is the main headless
// service for a couchbase cluster that establishes DNS addressing for all pods.
func generatePeerService(cluster *couchbasev2.CouchbaseCluster) (*v1.Service, error) {
	ports, err := getPeerServicePorts()
	if err != nil {
		return nil, err
	}

	service := &v1.Service{}

	// Fill in metadata.
	service.Name = cluster.Name
	service.Labels = mergeLabels(service.Labels, LabelsForClusterResource(cluster))
	service.OwnerReferences = []metav1.OwnerReference{
		cluster.AsOwner(),
	}

	// Fill in spec.
	service.Spec.ClusterIP = v1.ClusterIPNone
	service.Spec.PublishNotReadyAddresses = true
	service.Spec.Selector = SelectorForClusterResource(cluster)
	service.Spec.Ports = ports

	return service, nil
}

// generateDiscoveryService returns an idealized service for CCCP bootstrap via
// SRV records, the only real difference from the main peer service is that it selects
// only data nodes, which apparently isn't necessary from 6.5.0 onward...
func generateDiscoveryService(cluster *couchbasev2.CouchbaseCluster) *v1.Service {
	service := &v1.Service{}

	// Fill in metadata.
	service.Name = cluster.Name + "-srv"
	service.Labels = mergeLabels(service.Labels, LabelsForClusterResource(cluster))
	service.OwnerReferences = []metav1.OwnerReference{
		cluster.AsOwner(),
	}

	// Fill in spec.
	service.Spec.ClusterIP = v1.ClusterIPNone
	service.Spec.PublishNotReadyAddresses = true
	service.Spec.Selector = selectorForDataService(cluster)
	service.Spec.Ports = srvServicePorts

	return service
}

// ReconcilePeerServices creates/updates all cluster-wide services required for
// communication to and discovery of the cluster.
func ReconcilePeerServices(c *client.Client, cluster *couchbasev2.CouchbaseCluster) error {
	requested, err := generatePeerService(cluster)
	if err != nil {
		return err
	}

	if err := reconcileService(c, cluster, requested); err != nil {
		return err
	}

	requested = generateDiscoveryService(cluster)

	if err := reconcileService(c, cluster, requested); err != nil {
		return err
	}

	return nil
}

// adminConsoleSelector generates a selector matching pods running all the requested services.
// This is lagacy, apparently all nodes behave the same on 6.5.0.
func adminConsoleSelector(cluster *couchbasev2.CouchbaseCluster) map[string]string {
	labels := SelectorForClusterResource(cluster)

	for _, s := range cluster.Spec.Networking.AdminConsoleServices {
		k := constants.LabelServicePrefix + s.String()
		labels[k] = constants.EnabledValue
	}

	return labels
}

// generateConsoleService creates a new Service resource based on the cluster specification.
func generateConsoleService(cluster *couchbasev2.CouchbaseCluster) *v1.Service {
	// Gather admin console ports.
	ports := listConsolePorts(cluster)

	// Insecure ports are filtered out when using public networking.
	ports = filterInsecurePorts(ports, cluster.Spec.IsAdminConsoleServiceTypePublic())

	// Use either a blank service or the user specified template.
	service := &v1.Service{}

	if cluster.Spec.Networking.AdminConsoleServiceTemplate != nil {
		service.Labels = cluster.Spec.Networking.AdminConsoleServiceTemplate.Labels
		service.Annotations = cluster.Spec.Networking.AdminConsoleServiceTemplate.Annotations

		if cluster.Spec.Networking.AdminConsoleServiceTemplate.Spec != nil {
			service.Spec = *cluster.Spec.Networking.AdminConsoleServiceTemplate.Spec
		}
	} else {
		service.Spec.Type = cluster.Spec.Networking.AdminConsoleServiceType
	}

	// Apply deprecated fields, these have precedence for backwards compatibility,
	// but only apply if they have been set e.g. not the zero value.
	if cluster.Spec.Networking.ServiceAnnotations != nil {
		service.Annotations = mergeLabels(service.Annotations, cluster.Spec.Networking.ServiceAnnotations)
	}

	if cluster.Spec.Networking.LoadBalancerSourceRanges != nil {
		service.Spec.LoadBalancerSourceRanges = cluster.Spec.Networking.LoadBalancerSourceRanges.StringSlice()
	}

	// Fill in metadata.
	service.Name = ConsoleServiceName(cluster.Name)
	service.Labels = mergeLabels(service.Labels, LabelsForClusterResource(cluster))
	service.OwnerReferences = []metav1.OwnerReference{
		cluster.AsOwner(),
	}

	// Fill in spec.
	service.Spec.PublishNotReadyAddresses = true
	service.Spec.Selector = adminConsoleSelector(cluster)
	service.Spec.Ports = ports

	// If a DNS domain is specified add a DNS name for use with TLS.
	if cluster.Spec.Networking.DNS != nil {
		if service.Annotations == nil {
			service.Annotations = map[string]string{}
		}

		service.Annotations[constants.DNSAnnotation] = dnsAdminConsoleName(cluster)
	}

	// Also set the external traffic policy to Local which means the load balancer is
	// responsible for routing traffic to the destination and not Kubernetes.  For
	// ELB at least this appears to keep sessions open to the same console instance.
	if cluster.Spec.IsAdminConsoleServiceTypePublic() {
		service.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	}

	// Ensure client sessions are maintained by routing to the same backend node based
	// on the client IP address, server's authentication cookies are only valid for the
	// host they were created on. AWS ELB does not support IP based session stickiness,
	// instead we rely on the load balancer keeping TCP connection alive.  It's not perfect
	// but we can work until NS server get into gear.
	if !cluster.Spec.IsAdminConsoleServiceTypePublic() || cluster.Spec.Platform != couchbasev2.PlatformTypeAWS {
		service.Spec.SessionAffinity = v1.ServiceAffinityClientIP
	}

	return service
}

// ReconcileAdminConsole looks for the cluster's admin console service, creates it if not
// present but requested, deletes it if present but unrequested and performs udpdates
// to the configurable service parameters.
func ReconcileAdminConsole(c *client.Client, cluster *couchbasev2.CouchbaseCluster) error {
	requested := generateConsoleService(cluster)

	return reconcileService(c, cluster, requested)
}

// depthFirstPrune takes a current object and recursively removes any scalars
// that are defined in the original, but no longer in the requested.  This means
// a managed field has been unmanaged and configuration must be removed.
func depthFirstPrune(current, original, requested map[string]interface{}) map[string]interface{} {
	pruned := map[string]interface{}{}

	for key, value := range current {
		switch cc := value.(type) {
		case map[string]interface{}:
			// Recursively descend through maps depth first, only adding the
			// key to the pruned object if the returned child is not empty.
			oc, _ := original[key].(map[string]interface{})
			rc, _ := requested[key].(map[string]interface{})

			uc := depthFirstPrune(cc, oc, rc)

			if len(uc) == 0 {
				break
			}

			pruned[key] = uc
		default:
			// Scalars and arrays should be pruned if they were in the original, and
			// are not in the requested object.
			_, ook := original[key]
			_, rok := requested[key]

			if ook && !rok {
				break
			}

			pruned[key] = value
		}
	}

	return pruned
}

// prune removes from the current object any items defined in the original but not
// in the current.
func prune(currentJSON, originalJSON, requestedJSON []byte) ([]byte, error) {
	current := map[string]interface{}{}
	if err := json.Unmarshal(currentJSON, &current); err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	original := map[string]interface{}{}
	if err := json.Unmarshal(originalJSON, &original); err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	requested := map[string]interface{}{}
	if err := json.Unmarshal(requestedJSON, &requested); err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	updated := depthFirstPrune(current, original, requested)

	updatedJSON, err := json.Marshal(updated)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	return updatedJSON, nil
}

// generateCloudNativeGatewayService returns a service for
// exposing the Cloud Native Gateway which could be accessed form
// outside the cluster with the help of an Ingress or Openshift Route.
func generateCloudNativeGatewayService(cluster *couchbasev2.CouchbaseCluster) *v1.Service {
	service := &v1.Service{}

	service.Name = cluster.Name + "-cloud-native-gateway-service"
	service.Labels = mergeLabels(service.Labels, LabelsForClusterResource(cluster))
	service.OwnerReferences = []metav1.OwnerReference{
		cluster.AsOwner(),
	}

	svcPort := v1.ServicePort{
		Name:       "cloud-native-gateway-http",
		Protocol:   v1.ProtocolTCP,
		Port:       80,
		TargetPort: intstr.FromInt(snDataPort),
	}

	// When Enpoint Proxy TLS enabled
	if cluster.Spec.Networking.CloudNativeGateway.TLS != nil {
		svcPort = v1.ServicePort{
			Name:       "cloud-native-gateway-https",
			Protocol:   v1.ProtocolTCP,
			Port:       443,
			TargetPort: intstr.FromInt(snDataPort),
		}
	}

	service.Spec.Selector = selectorForCloudNativeGatewayService(cluster)
	service.Spec.Ports = []v1.ServicePort{svcPort}

	return service
}

// ReconcileCloudNativeGatewayService creates/updates k8s services for exposing the data and service discovery services
// on the gRPC based Cloud Native Gateway proxying to the couchbase cluster.
func ReconcileCloudNativeGatewayService(c *client.Client, cluster *couchbasev2.CouchbaseCluster) error {
	requested := generateCloudNativeGatewayService(cluster)

	if err := reconcileService(c, cluster, requested); err != nil {
		return err
	}

	return nil
}

// reconcileService creates or updates a service.
func reconcileService(c *client.Client, cluster *couchbasev2.CouchbaseCluster, requested *v1.Service) error {
	requestedJSON, err := json.Marshal(requested)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	// Check to see if the console service exists.
	// If it doesn't and its being managed, then create it.
	current, ok := c.Services.Get(requested.Name)
	if !ok {
		log.V(2).Info("Creating service", "cluster", cluster.NamespacedName(), "name", requested.Name, "requested", string(requestedJSON))

		// Annotate the service with the current specification.
		if requested.Annotations == nil {
			requested.Annotations = map[string]string{}
		}

		requested.Annotations[constants.SVCSpecAnnotation] = string(requestedJSON)

		// Create the service.
		if _, err := c.KubeClient.CoreV1().Services(cluster.Namespace).Create(context.Background(), requested, metav1.CreateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	// Annotation does not exist, upgrade it to have the current requested spec.
	// This makes the assumption that people are not bold enough to modify while
	// performing the upgrade.
	originalJSONString, ok := current.Annotations[constants.SVCSpecAnnotation]
	if !ok {
		log.V(2).Info("Upgrading service", "cluster", cluster.NamespacedName(), "name", requested.Name, "requested", string(requestedJSON))

		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}

		current.Annotations[constants.SVCSpecAnnotation] = string(requestedJSON)

		if _, err := c.KubeClient.CoreV1().Services(cluster.Namespace).Update(context.Background(), current, metav1.UpdateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	originalJSON := []byte(originalJSONString)

	original := &v1.Service{}
	if err := json.Unmarshal(originalJSON, original); err != nil {
		return errors.NewStackTracedError(err)
	}

	// Consider the current resource without the annotation.
	// Beware modifying the cache entry, it may have unintended side effects
	// next time around e.g. no annotation, upgrade with the wrong spec,
	// ignore a change.
	current = current.DeepCopy()

	delete(current.Annotations, constants.SVCSpecAnnotation)

	currentJSON, err := json.Marshal(current)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	log.V(2).Info("Merging service", "cluster", cluster.NamespacedName(), "name", requested.Name, "requested", string(requestedJSON), "current", string(currentJSON))

	// First pass merges the requested specification on top of the current
	// specification, this will add new struct and map fields, and update
	// any ones that have been modified either by the specification or an
	// external third party.  Third party modification to unamanged attributes
	// are preserved by this operation. Lists (i.e. the ports) cannot be merged
	// as they preserve ordering, and are just copied over verbatim.  This will
	// be handled specially later, it does however delete any ports that are
	// undefined by the required resource.
	merged := current.DeepCopy()
	if err := mergo.Merge(merged, requested, mergo.WithOverride); err != nil {
		return errors.NewStackTracedError(err)
	}

	// Mergo dosn't like the meta/v1.Time type, it overwrites with null even though
	// it's a zero value.
	merged.CreationTimestamp = current.CreationTimestamp

	// Second pass prunes values that were managed, but are now not defined.
	// To do this we use the original resource annotation to identify things
	// that were managed but are now not when compared to the requesed resource.
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	log.V(2).Info("Pruning service", "cluster", cluster.NamespacedName(), "name", requested.Name, "merged", string(mergedJSON), "original", string(originalJSON), "requested", string(requestedJSON))

	prunedJSON, err := prune(mergedJSON, originalJSON, requestedJSON)
	if err != nil {
		return err
	}

	pruned := &v1.Service{}
	if err := json.Unmarshal(prunedJSON, pruned); err != nil {
		return errors.NewStackTracedError(err)
	}

	// Third pass should be unnecessary, but too many people insist on using
	// node port addressing, so preserve port numbers, lest people's clients
	// stop working.  To be frank, I consider this their own fault for not
	// using stable and highly-available network addressing in the first place.
	for i, port := range pruned.Spec.Ports {
		for _, old := range current.Spec.Ports {
			if port.Name == old.Name {
				pruned.Spec.Ports[i].TargetPort = old.TargetPort
				pruned.Spec.Ports[i].NodePort = old.NodePort

				break
			}
		}
	}

	updatedJSON, err := json.Marshal(pruned)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	log.V(2).Info("Comparing service", "cluster", cluster.NamespacedName(), "name", requested.Name, "original", string(currentJSON), "updated", string(updatedJSON))

	// Check for updates, updating the resource if required.
	if reflect.DeepEqual(current, pruned) {
		return nil
	}

	d, err := diff.Diff(current, pruned)
	if err != nil {
		return err
	}

	// Deep equal isn't 100% accurate, and defines an nil list and
	// an empty list as different.
	if d == "" {
		return nil
	}

	log.V(1).Info("Updating service", "cluster", cluster.NamespacedName(), "diff", d)

	if pruned.Annotations == nil {
		pruned.Annotations = map[string]string{}
	}

	pruned.Annotations[constants.SVCSpecAnnotation] = string(requestedJSON)

	if _, err := c.KubeClient.CoreV1().Services(cluster.Namespace).Update(context.Background(), pruned, metav1.UpdateOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}

// GetExposedServiceName returns the service name generated for each service port group.
func GetExposedServiceName(nodeName string) string {
	return nodeName
}

// exposedfeatureSets is a mapping from feature name to a list of host ports to expose.
var exposedfeatureSets = map[couchbasev2.ExposedFeature][]couchbasev2.Service{
	couchbasev2.FeatureAdmin: {
		couchbasev2.AdminService,
	},
	couchbasev2.FeatureXDCR: {
		couchbasev2.AdminService,
		couchbasev2.DataService,
	},
	couchbasev2.FeatureClient: {
		couchbasev2.AdminService,
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
			return nil, fmt.Errorf("%w: feature set %s undefined", errors.NewStackTracedError(errors.ErrInternalError), featureSet)
		}

		for _, featureSetPort := range featureSetPorts {
			serviceSet[featureSetPort] = nil
		}
	}

	// Map the set keys into a list
	for service := range serviceSet {
		serviceList = append(serviceList, service)
	}

	// Make the port ordering deterministic.
	sort.Sort(serviceList)

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

// listConsolePorts returns the set of ports to expose on the Admin console service.
func listConsolePorts(cluster *couchbasev2.CouchbaseCluster) []v1.ServicePort {
	// The console service always exposes 8091/18091,
	ports := uiServicePorts

	// The console service exposes data ports for bootstrapping
	// when 'client' feature set is exposed
	if cluster.Spec.IsClientFeatureExposed() {
		ports = append(ports, clientBootstrapPorts...)
	}

	return ports
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
func memberServices(cluster *couchbasev2.CouchbaseCluster, member couchbaseutil.Member) (couchbasev2.ServiceList, error) {
	class := cluster.Spec.GetServerConfigByName(member.Config())
	if class == nil {
		return nil, fmt.Errorf("%w: server class %s missing for member %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), member.Config(), member.Name())
	}

	// Always allow the admin service, this is not explicitly enabled
	// per server class.
	services := couchbasev2.ServiceList{
		couchbasev2.AdminService,
	}

	// Build up the node services based on enabled services, but don't duplicate,
	// it's probably harmless...
	for _, service := range class.Services {
		if services.Contains(service) {
			continue
		}

		services = append(services, service)
	}

	return services, nil
}

// UpdateExposedFeatureStatus is used to communicate to clients which services have been
// added or created by UpdateExposedFeatureStatus.
type UpdateExposedFeatureStatus struct {
	Added   couchbasev2.ServiceList
	Removed couchbasev2.ServiceList
}

// generateExposedService creates a Kubernetes Service resource for the requested member.
func generateExposedService(member couchbaseutil.Member, cluster *couchbasev2.CouchbaseCluster) (*v1.Service, error) {
	// The ports to expose are first determined by the exposed feature set,
	// then they are filtered based on the services the member is exposing,
	// then finally based on whether we are enforcing use of TLS.
	serviceNames, err := exposedFeatureSetToServiceList(cluster.Spec.Networking.ExposedFeatures)
	if err != nil {
		return nil, err
	}

	enabledServices, err := memberServices(cluster, member)
	if err != nil {
		return nil, err
	}

	ports := listRequestedPorts(serviceNames)
	ports = filterConfiguredPorts(ports, enabledServices)
	ports = filterInsecurePorts(ports, cluster.Spec.IsExposedFeatureServiceTypePublic())

	// Use either a blank service or the user specified template.
	service := &v1.Service{}

	if cluster.Spec.Networking.ExposedFeatureServiceTemplate != nil {
		serviceTemplate := cluster.Spec.Networking.ExposedFeatureServiceTemplate.DeepCopy()
		service.Labels = serviceTemplate.Labels
		service.Annotations = serviceTemplate.Annotations

		if serviceTemplate.Spec != nil {
			service.Spec = *serviceTemplate.Spec
		}
	} else {
		service.Spec.Type = cluster.Spec.Networking.ExposedFeatureServiceType
	}

	// Apply deprecated fields, these have precedence for backwards compatibility,
	// but only apply if they have been set e.g. not the zero value.
	if cluster.Spec.Networking.ServiceAnnotations != nil {
		service.Annotations = mergeLabels(service.Annotations, cluster.Spec.Networking.ServiceAnnotations)
	}

	if cluster.Spec.Networking.LoadBalancerSourceRanges != nil {
		service.Spec.LoadBalancerSourceRanges = cluster.Spec.Networking.LoadBalancerSourceRanges.StringSlice()
	}

	// Fill in the metadata.
	service.Name = member.Name()
	service.Labels = mergeLabels(service.Labels, LabelsForNodeResource(cluster, member.Name()))
	service.OwnerReferences = []metav1.OwnerReference{
		cluster.AsOwner(),
	}

	// Fill in the spec.
	service.Spec.PublishNotReadyAddresses = true
	service.Spec.Selector = getNodeServiceSelectors(cluster, member.Name())
	service.Spec.Ports = ports

	// If a DNS domain is specified annotate with the pod DNS name.
	if cluster.Spec.Networking.DNS != nil {
		if service.Annotations == nil {
			service.Annotations = map[string]string{}
		}

		service.Annotations[constants.DNSAnnotation] = GetDNSName(cluster, member.Name())
	}

	// Enforce that traffic has to come directly to the k8s node avoiding the
	// penalty of an extra hop and SNAT.
	if service.Spec.ExternalTrafficPolicy == "" {
		service.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	}

	return service, nil
}

func ReconcilePodService(c *client.Client, cluster *couchbasev2.CouchbaseCluster, member couchbaseutil.Member) error {
	requested, err := generateExposedService(member, cluster)
	if err != nil {
		return err
	}

	return reconcileService(c, cluster, requested)
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
		case viewServicePortName:
			ports.ViewAndXDCRServicePort = port.NodePort
		case viewServicePortNameTLS:
			ports.ViewAndXDCRServicePortTLS = port.NodePort
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
			return nil, fmt.Errorf("%w: unexpected port name %s", errors.NewStackTracedError(errors.ErrInternalError), port.Name)
		}
	}

	return ports, nil
}
