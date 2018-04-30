package k8sutil

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	couchbaseVersionAnnotationKey = "couchbase.version"

	// Fixed port names (generate SRV records for the peer services)
	couchbaseSRVName    = "couchbase"
	couchbaseSRVNameTLS = "couchbases"

	// tlsBasePort is the port number relative to which TLS ports are (usually) translated
	tlsBasePort = 10000

	// Admin service constants
	AdminService            = "admin"
	adminServicePortName    = "admin"
	adminServicePortNameTLS = "admin-tls"
	adminServicePort        = 8091
	adminServicePortTLS     = tlsBasePort + adminServicePort

	// View service constants
	ViewService            = "view"
	viewServicePortName    = "view"
	viewServicePortNameTLS = "view-tls"
	viewServicePort        = 8092
	viewServicePortTLS     = tlsBasePort + viewServicePort

	// Query service constants
	QueryService            = "query"
	queryServicePortName    = "query"
	queryServicePortNameTLS = "query-tls"
	queryServicePort        = 8093
	queryServicePortTLS     = tlsBasePort + queryServicePort

	// Full text search service constants
	FtsService            = "fts"
	ftsServicePortName    = "fts"
	ftsServicePortNameTLS = "fts-tls"
	ftsServicePort        = 8094
	ftsServicePortTLS     = tlsBasePort + ftsServicePort

	// Analytics service constants
	AnalyticsService            = "analytics"
	analyticsServicePortName    = "analytics"
	analyticsServicePortNameTLS = "analytics-tls"
	analyticsServicePort        = 8095
	analyticsServicePortTLS     = tlsBasePort + analyticsServicePort

	// Eventing service constants
	EventingService            = "eventing"
	eventingServicePortName    = "eventing"
	eventingServicePortNameTLS = "eventing-tls"
	eventingServicePort        = 8096
	eventingServicePortTLS     = tlsBasePort + eventingServicePort

	// Data service constants
	DataService            = "data"
	dataServicePortName    = "data"
	dataServicePortNameTLS = "data-tls"
	dataServicePort        = 11210
	dataServicePortTLS     = 11207

	// Labels
	labelApp      = "app"
	labelCluster  = "couchbase_cluster"
	labelNode     = "couchbase_node"
	labelNodeConf = "couchbase_node_conf"
)

const TolerateUnreadyEndpointsAnnotation = "service.alpha.kubernetes.io/tolerate-unready-endpoints"

func SetCouchbaseVersion(pod *v1.Pod, version string) {
	pod.Annotations[couchbaseVersionAnnotationKey] = version
}

func ClientServiceName(clusterName string) string {
	return clusterName + "-client"
}

func InClusterConfig() (*rest.Config, error) {
	// Work around https://github.com/kubernetes/kubernetes/issues/40973
	if len(os.Getenv("KUBERNETES_SERVICE_HOST")) == 0 {
		addrs, err := net.LookupHost("kubernetes.default.svc")
		if err != nil {
			panic(err)
		}
		os.Setenv("KUBERNETES_SERVICE_HOST", addrs[0])
	}
	if len(os.Getenv("KUBERNETES_SERVICE_PORT")) == 0 {
		os.Setenv("KUBERNETES_SERVICE_PORT", "443")
	}
	return rest.InClusterConfig()
}

func MustNewKubeClient() kubernetes.Interface {
	cfg, err := InClusterConfig()
	if err != nil {
		panic(err)
	}
	return kubernetes.NewForConfigOrDie(cfg)
}

func GetPodNames(pods []*v1.Pod) []string {
	if len(pods) == 0 {
		return nil
	}
	res := []string{}
	for _, p := range pods {
		res = append(res, p.Name)
	}
	return res
}

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

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

// creates a service of Type ClusterIP which is only resolvable internally.
// futhermore the ClusterIP is "None" allowing the service to run "headless"
// (sans load balancing middleware) which allows the operator to resolve
// addresses of individual pods instead of a proxy
func CreatePeerService(kubecli kubernetes.Interface, clusterName, ns string, owner metav1.OwnerReference) error {
	ports := peerServicePorts
	labels := LabelsForCluster(clusterName)
	svc := createServiceManifest(clusterName, v1.ServiceTypeClusterIP, ports, labels, labels)
	svc.Spec.ClusterIP = v1.ClusterIPNone
	_, err := createService(kubecli, ns, svc, owner)
	return err
}

// creates a service of Type NodePort which allows external clients to
// access the web ui
func CreateUIService(kubecli kubernetes.Interface, clusterName, ns string, services []string, owner metav1.OwnerReference) (*v1.Service, error) {
	ports := peerServicePorts
	selectors := LabelsForAdminConsole(clusterName, services)
	svc := createServiceManifest(AdminServiceName(clusterName), v1.ServiceTypeNodePort, ports, selectors, LabelsForCluster(clusterName))
	svc.Spec.SessionAffinity = v1.ServiceAffinityClientIP
	return createService(kubecli, ns, svc, owner)
}

func createService(kubecli kubernetes.Interface, ns string, svc *v1.Service, owner metav1.OwnerReference) (*v1.Service, error) {
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	return kubecli.CoreV1().Services(ns).Create(svc)
}

func GetAdminConsolePorts(svc *v1.Service) (string, string) {
	return getAdminConsolePort(svc, couchbaseSRVName), getAdminConsolePort(svc, couchbaseSRVNameTLS)
}

func getAdminConsolePort(svc *v1.Service, portName string) string {
	for _, port := range svc.Spec.Ports {
		if port.Name == portName {
			return strconv.Itoa(int(port.NodePort))
		}
	}
	return ""
}

func GetService(kubecli kubernetes.Interface, name, ns string, opts *metav1.GetOptions) (*v1.Service, error) {
	if opts == nil {
		opts = &metav1.GetOptions{}
	}
	return kubecli.CoreV1().Services(ns).Get(name, *opts)
}

func DeleteService(kubecli kubernetes.Interface, ns, name string, opts *metav1.DeleteOptions) error {
	return kubecli.CoreV1().Services(ns).Delete(name, opts)
}

func UpdateService(kubecli kubernetes.Interface, ns string, svc *v1.Service) (*v1.Service, error) {
	return kubecli.CoreV1().Services(ns).Update(svc)
}

// updateExposedPorts accepts a new or existing service and adds the allocated
// node ports to a port status structure.
func updateExposedPorts(portStatusMap cbapi.PortStatusMap, nodeName string, service *v1.Service) error {
	portStatus, ok := portStatusMap[nodeName]
	if !ok {
		portStatus = &cbapi.PortStatus{}
		portStatusMap[nodeName] = portStatus
	}
	for _, servicePort := range service.Spec.Ports {
		switch servicePort.Name {
		case adminServicePortName:
			portStatus.AdminServicePort = servicePort.NodePort
		case adminServicePortNameTLS:
			portStatus.AdminServicePortTLS = servicePort.NodePort
		case viewServicePortName:
			portStatus.ViewServicePort = servicePort.NodePort
		case viewServicePortNameTLS:
			portStatus.ViewServicePortTLS = servicePort.NodePort
		case queryServicePortName:
			portStatus.QueryServicePort = servicePort.NodePort
		case queryServicePortNameTLS:
			portStatus.QueryServicePortTLS = servicePort.NodePort
		case ftsServicePortName:
			portStatus.FtsServicePort = servicePort.NodePort
		case ftsServicePortNameTLS:
			portStatus.FtsServicePortTLS = servicePort.NodePort
		case analyticsServicePortName:
			portStatus.AnalyticsServicePort = servicePort.NodePort
		case analyticsServicePortNameTLS:
			portStatus.AnalyticsServicePortTLS = servicePort.NodePort
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

// ServiceList contains a set of services
type ServiceList []string

// Contains returns true if a sevice is part of a service list
func (sl ServiceList) Contains(service string) bool {
	for _, s := range sl {
		if s == service {
			return true
		}
	}
	return false
}

// Sub removes members from 'other' from a ServiceList
func (sl ServiceList) Sub(other ServiceList) ServiceList {
	out := ServiceList{}
	for _, service := range sl {
		if other.Contains(service) {
			continue
		}
		out = append(out, service)
	}
	return out
}

// GetExposedServiceName returns the service name generated for each service port group
func GetExposedServiceName(nodeName string) string {
	return nodeName + "-exposed-ports"
}

// exposedfeatureSets is a mapping from feature name to a list of host ports to expose
var exposedfeatureSets = map[string][]string{
	cbapi.FeatureAdmin: []string{
		AdminService,
	},
	cbapi.FeatureXDCR: []string{
		AdminService,
		ViewService,
		DataService,
	},
	cbapi.FeatureClient: []string{
		ViewService,
		QueryService,
		FtsService,
		AnalyticsService,
		EventingService,
		DataService,
	},
}

// exposedFeatureSetToServiceList takes a requested feature set and returns
// a list of unique service names
func exposedFeatureSetToServiceList(featureSet cbapi.ExposedFeatureList) (ServiceList, error) {
	// Nothing to do, exit
	serviceList := ServiceList{}
	serviceSet := map[string]interface{}{}
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

// serviceExists scans a list of services and returns true if the named
// service exists
func serviceExists(services *v1.ServiceList, name string) bool {
	for _, service := range services.Items {
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

// UpdateExposedFeatureStatus is used to communicate to clients which services have been
// added or created by UpdateExposedFeatureStatus
type UpdateExposedFeatureStatus struct {
	Added   ServiceList
	Removed ServiceList
}

// UpdateExposedFeatures handles adding and removing per-pod node ports for all
// services.  Rather than specify individual ports we instead allow addition/deletion
// via feature sets.  There is some overlap between sets so we perform a boolean union
// before processing.  The function returns lists of added and removed services so
// client code can perform any notifications, these are lexically sorted for determinism.
func UpdateExposedFeatures(kubecli kubernetes.Interface, cluster *cbapi.CouchbaseCluster, status *cbapi.ClusterStatus) (*UpdateExposedFeatureStatus, error) {
	// For each feature set accumulate a unique set of services to expose
	serviceNames, err := exposedFeatureSetToServiceList(cluster.Spec.ExposedFeatures)
	if err != nil {
		return nil, err
	}
	servicesDefined := len(serviceNames) != 0

	// Create the list of ports each node should have
	ports := []v1.ServicePort{}
	for _, serviceName := range serviceNames {
		ports = append(ports, servicePorts[serviceName]...)
	}

	// Get a list of all cluster services that belong to a specific nodes
	clusterRequirement, err := labels.NewRequirement(labelCluster, selection.Equals, []string{cluster.Name})
	if err != nil {
		return nil, err
	}
	nodeRequirement, err := labels.NewRequirement(labelNode, selection.Exists, []string{})
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector()
	selector = selector.Add(*clusterRequirement, *nodeRequirement)
	services, err := kubecli.CoreV1().Services(cluster.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	// Buffer the port status as we go through
	portStatus := map[string]*cbapi.PortStatus{}

	// Remove any node services that shouldn't be defined or perform any updates
	for _, service := range services.Items {
		// Extract metadata from the service
		nodeName := service.Labels[labelNode]

		// Node no longer exists or no ports are defined so delete the service
		if !servicesDefined || !status.Members.Ready.Contains(nodeName) {
			if err := DeleteService(kubecli, cluster.Namespace, service.Name, nil); err != nil {
				return nil, err
			}
			continue
		}

		// Get pre-existing requested ports (with associated node ports), and requested ports that
		// aren't defined
		existingPorts := intersectPorts(service.Spec.Ports, ports)
		newPorts := subtractPorts(ports, existingPorts)

		// Perform an update if ports have been deleted or added
		updatedService := &service
		if len(existingPorts) != len(service.Spec.Ports) || len(newPorts) != 0 {
			service.Spec.Ports = append(existingPorts, newPorts...)
			if updatedService, err = UpdateService(kubecli, cluster.Namespace, &service); err != nil {
				return nil, err
			}
		}

		// Update the port status with the new node ports
		if err := updateExposedPorts(portStatus, nodeName, updatedService); err != nil {
			return nil, err
		}
	}

	// Add any services to nodes which aren't already defined
	if servicesDefined {
		for _, nodeName := range status.Members.Ready {
			// Define the new service name
			exposedServiceName := GetExposedServiceName(nodeName)

			// Service already exists, ignore
			if serviceExists(services, exposedServiceName) {
				continue
			}

			// Add the new service
			labels := labelsForNodeService(cluster.Name, nodeName)
			selectors := getNodeServiceSelectors(cluster, nodeName)
			service := createServiceManifest(exposedServiceName, v1.ServiceTypeNodePort, ports, labels, selectors)

			// Enforce that traffic has to come directly to the k8s node avoiding the
			// penalty of an extra hop and SNAT
			service.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal

			if service, err = createService(kubecli, cluster.Namespace, service, cluster.AsOwner()); err != nil {
				return nil, err
			}

			// Update the port status with the new node ports
			if err := updateExposedPorts(portStatus, nodeName, service); err != nil {
				return nil, err
			}
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
	sort.Strings(ret.Added)
	sort.Strings(ret.Removed)

	// Finally update the status
	status.ExposedFeatures = cluster.Spec.ExposedFeatures
	status.ExposedPorts = portStatus

	return ret, nil
}

func GetHostIP(kubecli kubernetes.Interface, ns, name string) (string, error) {
	pod, err := kubecli.CoreV1().Pods(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if pod.Status.HostIP == "" {
		return "", fmt.Errorf("host IP unset, pod not scheduled")
	}
	return pod.Status.HostIP, nil
}

func GetSecret(kubecli kubernetes.Interface, name, ns string, opts *metav1.GetOptions) (*v1.Secret, error) {
	if opts == nil {
		opts = &metav1.GetOptions{}
	}
	return kubecli.CoreV1().Secrets(ns).Get(name, *opts)
}

func ClusterListOpt(clusterName string) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(LabelsForCluster(clusterName)).String(),
	}
}

// getNodeServiceSelectors returns a key/value map to identify a specific node
// in the cluster.
// In general all services are applied to all nodes in the Couchbase cluster.
// If it is the admin port however we may apply it to only nodes with the
// specified list of services installed (thus limiting the kinds of operations
// that can be performed via the UI).
func getNodeServiceSelectors(cluster *cbapi.CouchbaseCluster, nodeName string) map[string]string {
	// Apply to a specific couchbase pod within the named cluster
	selectors := LabelsForCluster(cluster.Name)
	selectors[labelNode] = nodeName
	return selectors
}

func LabelsForAdminConsole(clusterName string, services []string) map[string]string {
	labels := LabelsForCluster(clusterName)
	for _, s := range services {
		k := "couchbase_service_" + s
		labels[k] = "enabled"
	}
	return labels
}

// LabelsForCluster returns a basic set of labels which will identify a couchbase
// pod within a specific cluster.
func LabelsForCluster(clusterName string) map[string]string {
	return map[string]string{
		labelCluster: clusterName,
		labelApp:     "couchbase",
	}
}

func AdminServiceName(clusterName string) string {
	return clusterName + "-ui"
}

// labelsForNodeService returns a set of labels which identify a cluster service
// for a specific node.
func labelsForNodeService(clusterName, nodeName string) map[string]string {
	labels := LabelsForCluster(clusterName)
	labels[labelNode] = nodeName
	return labels
}

// peerServicePorts is the list of ports to expose for the Couchbase cluster
// headless service.  This generates DNS A records for all pods, and SRV records
// pointing to all pods.
var peerServicePorts = []v1.ServicePort{
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

// servicePorts maps a service type to it's set of ports.  These are used in
// the generation of node ports to allow cluster access from outside of the
// pod (overlay) network.
var servicePorts = map[string][]v1.ServicePort{
	AdminService: []v1.ServicePort{
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
	ViewService: []v1.ServicePort{
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
	QueryService: []v1.ServicePort{
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
	FtsService: []v1.ServicePort{
		{
			Name:     ftsServicePortName,
			Port:     ftsServicePort,
			Protocol: v1.ProtocolTCP,
		},
		{
			Name:     ftsServicePortNameTLS,
			Port:     ftsServicePortTLS,
			Protocol: v1.ProtocolTCP,
		},
	},
	AnalyticsService: []v1.ServicePort{
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
	EventingService: []v1.ServicePort{
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
	DataService: []v1.ServicePort{
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

func NodeListOpt(clusterName, memberName string) metav1.ListOptions {
	l := LabelsForCluster(clusterName)
	l[labelNode] = memberName
	return metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(l).String(),
	}
}

func IsKubernetesResourceAlreadyExistError(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func IsKubernetesResourceNotFoundError(err error) bool {
	return apierrors.IsNotFound(err)
}

func CascadeDeleteOptions(gracePeriodSeconds int64) *metav1.DeleteOptions {
	return &metav1.DeleteOptions{
		GracePeriodSeconds: func(t int64) *int64 { return &t }(gracePeriodSeconds),
		PropagationPolicy: func() *metav1.DeletionPropagation {
			foreground := metav1.DeletePropagationForeground
			return &foreground
		}(),
	}
}

// mergeLables merges l2 into l1. Conflicting label will be skipped.
func mergeLabels(l1, l2 map[string]string) {
	for k, v := range l2 {
		if _, ok := l1[k]; ok {
			continue
		}
		l1[k] = v
	}
}

func DeletePod(kubeCli kubernetes.Interface, namespace, podName string, opts *metav1.DeleteOptions) error {
	err := kubeCli.Core().Pods(namespace).Delete(podName, opts)
	if err != nil {
		if !IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

func CreatePod(kubeCli kubernetes.Interface, namespace string, pod *v1.Pod) (*v1.Pod, error) {
	return kubeCli.Core().Pods(namespace).Create(pod)
}

// Waits for a pod to be created and for it to respond to TCP connections on
// the admin port.  Accepts a context, typically with a timeout, which can
// abort the operation.  The any timeout duration covers the whole process.
func WaitForPod(ctx context.Context, kubeCli kubernetes.Interface, namespace, podName, hostURL string) error {

	opts := metav1.ListOptions{
		LabelSelector: "couchbase_node=" + podName,
	}

	watcher, err := kubeCli.CoreV1().Pods(namespace).Watch(opts)
	if err != nil {
		return err
	}
	events := watcher.ResultChan()

	// Loop until success
	done := false
	for !done {
		select {
		// Handle timeout and cancellation events
		case <-ctx.Done():
			return ctx.Err()
		// Process K8S events for our chosen pod
		case ev := <-events:
			obj := ev.Object.(*v1.Pod)
			status := obj.Status

			switch ev.Type {

			// check if any error occurred creating pod
			case watch.Error:
				return cberrors.ErrCreatingPod{Reason: status.Reason}
			case watch.Deleted:
				return cberrors.ErrCreatingPod{Reason: status.Reason}
			case watch.Added, watch.Modified:

				// make sure created pod is now running
				switch status.Phase {
				case v1.PodRunning:
					done = true
				case v1.PodPending:
					for _, cond := range status.Conditions {
						if cond.Type == v1.PodScheduled {
							if cond.Status == v1.ConditionFalse && cond.Reason == v1.PodReasonUnschedulable {
								return cberrors.ErrPodUnschedulable{Reason: cond.Message}
							}
						}
					}
				default:
					return cberrors.ErrRunningPod{Reason: status.Reason}
				}
			}
		}
	}

	if hostURL != "" {
		// Wait for the admin port to come up, avoids unnecessary spam while trying to
		// run commands against it (e.g. initialisation and adding new nodes)
		if err := netutil.WaitForHostPort(ctx, hostURL); err != nil {
			return err
		}
	}

	return nil
}

func GetKubernetesVersion(kubeCli kubernetes.Interface) (constants.KubernetesVersion, error) {
	version, err := kubeCli.Discovery().ServerVersion()
	if err != nil {
		return constants.KubernetesVersionUnknown, err
	}

	// Sometimes the Major and Minor values are not set so we need to parse them
	// from the GitVersion field.
	if version.Major == "" || version.Minor == "" {
		rx := regexp.MustCompile("^v[0-9]{1,2}.[0-9]{1,2}.[0-9]{1,2}(\\+[0-9abcdef]*)?$")
		if !rx.MatchString(version.GitVersion) {
			err := fmt.Errorf("Unable to get version from Kubernetes API response")
			return constants.KubernetesVersionUnknown, err
		}

		parts := strings.Split(version.GitVersion[1:], ".")
		version.Major = parts[0]
		version.Minor = parts[1]
	}

	major, _ := strconv.Atoi(version.Major)
	minor, _ := strconv.Atoi(version.Minor)
	return constants.KubernetesVersion(fmt.Sprintf("%02d%02d", major, minor)), nil
}
