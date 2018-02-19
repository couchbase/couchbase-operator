package k8sutil

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	couchbaseVersionAnnotationKey = "couchbase.version"
	couchbaseVolumeName           = "couchbase-data"
	couchbaseVolumeMountDir       = "/opt/couchbase/var/lib/data"
	consoleAdminPortName          = "cb-admin"
	consoleAdminPortNameSSL       = "cb-admin-ssl"
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

func CreateCouchbasePod(m *couchbaseutil.Member, clusterName string, cs cbapi.ClusterSpec, ns cbapi.ServerConfig, owner metav1.OwnerReference) (*v1.Pod, error) {

	labels := createCouchbasePodLabels(m.Name, clusterName, ns)

	container := containerWithLivenessProbe(couchbaseContainer("", cs.BaseImage, cs.Version),
		couchbaseLivenessProbe())

	if ns.Pod != nil {
		container = containerWithRequirements(container, ns.Pod.Resources)
	}

	volumes := []v1.Volume{
		{Name: "couchbase-data", VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}}},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        m.Name,
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Spec: v1.PodSpec{
			Containers:    []v1.Container{container},
			RestartPolicy: v1.RestartPolicyNever,
			Volumes:       volumes,
			Hostname:      m.Name,
			Subdomain:     clusterName,
		},
	}

	if cs.AntiAffinity {
		pod = PodWithAntiAffinity(pod, clusterName)
	}

	applyPodPolicy(clusterName, pod, ns.Pod)

	if err := applyPodTlsConfiguration(cs, pod); err != nil {
		return nil, err
	}

	SetCouchbaseVersion(pod, cs.Version)

	addOwnerRefToObject(pod.GetObjectMeta(), owner)
	return pod, nil
}

func createCouchbasePodLabels(memberName, clusterName string, ns cbapi.ServerConfig) map[string]string {
	labels := map[string]string{
		"app":                 "couchbase",
		"couchbase_node":      memberName,
		"couchbase_node_conf": ns.Name,
		"couchbase_cluster":   clusterName,
	}

	for _, s := range ns.Services {
		k := "couchbase_service_" + s
		labels[k] = "enabled"
	}

	return labels
}

func createServiceManifest(svcName string, serviceType v1.ServiceType, ports []v1.ServicePort, selector, labels map[string]string) *v1.Service {

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
	labels := LabelsForCluster(clusterName)
	svc := createServiceManifest(clusterName, v1.ServiceTypeClusterIP, adminServicePorts(), labels, labels)
	svc.Spec.ClusterIP = v1.ClusterIPNone
	_, err := createService(kubecli, svc, ns, owner)
	return err
}

// creates a service of Type NodePort which allows external clients to
// access the web ui
func CreateUIService(kubecli kubernetes.Interface, clusterName, ns string, services []string, owner metav1.OwnerReference) (*v1.Service, error) {
	selectors := LabelsForAdminConsole(clusterName, services)
	svc := createServiceManifest(AdminServiceName(clusterName), v1.ServiceTypeNodePort, adminServicePorts(), selectors, LabelsForCluster(clusterName))
	svc.Spec.SessionAffinity = v1.ServiceAffinityClientIP
	return createService(kubecli, svc, ns, owner)
}

func createService(kubecli kubernetes.Interface, svc *v1.Service, ns string, owner metav1.OwnerReference) (*v1.Service, error) {
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	return kubecli.CoreV1().Services(ns).Create(svc)
}

func GetAdminConsolePorts(svc *v1.Service) (string, string) {
	return getAdminConsolePort(svc, consoleAdminPortName), getAdminConsolePort(svc, consoleAdminPortNameSSL)
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

func DeleteService(kubecli kubernetes.Interface, name, ns string, opts *metav1.DeleteOptions) error {
	return kubecli.CoreV1().Services(ns).Delete(name, opts)
}

func UpdateService(kubecli kubernetes.Interface, ns string, svc *v1.Service) error {
	_, err := kubecli.CoreV1().Services(ns).Update(svc)
	return err
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

func LabelsForAdminConsole(clusterName string, services []string) map[string]string {
	labels := LabelsForCluster(clusterName)
	for _, s := range services {
		k := "couchbase_service_" + s
		labels[k] = "enabled"
	}
	return labels
}

func LabelsForCluster(clusterName string) map[string]string {
	return map[string]string{
		"couchbase_cluster": clusterName,
		"app":               "couchbase",
	}
}

func AdminServiceName(clusterName string) string {
	return clusterName + "-ui"
}

func adminServicePorts() []v1.ServicePort {
	return []v1.ServicePort{{
		Name:       consoleAdminPortName,
		Port:       8091,
		TargetPort: intstr.FromInt(8091),
		Protocol:   v1.ProtocolTCP,
	}, {
		Name:       consoleAdminPortNameSSL,
		Port:       18091,
		TargetPort: intstr.FromInt(18091),
		Protocol:   v1.ProtocolTCP,
	}}
}

func NodeListOpt(clusterName, memberName string) metav1.ListOptions {
	l := LabelsForCluster(clusterName)
	l["couchbase_node"] = memberName
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
	for ; !done; {
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
				return cberrors.ErrCreatingPod{status.Reason}
			case watch.Deleted:
				return cberrors.ErrCreatingPod{status.Reason}
			case watch.Added, watch.Modified:

				// make sure created pod is now running
				switch status.Phase {
				case v1.PodRunning:
					done = true
				case v1.PodPending:
					for _, cond := range status.Conditions {
						if cond.Type == v1.PodScheduled {
							if cond.Status == v1.ConditionFalse && cond.Reason == v1.PodReasonUnschedulable {
								return cberrors.ErrPodUnschedulable{cond.Message}
							}
						}
					}
				default:
					return cberrors.ErrRunningPod{status.Reason}
				}
			}
		}
	}

	// Wait for the admin port to come up, avoids unnecessary spam while trying to
	// run commands against it (e.g. initialisation and adding new nodes)
	if err := netutil.WaitForHostPort(ctx, hostURL); err != nil {
		return err
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
		rx := regexp.MustCompile("^v[0-9]{1,2}.[0-9]{1,2}.[0-9]{1,2}$")
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
