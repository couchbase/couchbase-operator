package k8sutil

import (
	//"fmt"
	"net"
	"os"

	cbapi "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	couchbaseVersionAnnotationKey = "couchbase.version"
	couchbaseVolumeName           = "couchbase-data"
	couchbaseVolumeMountDir       = "/opt/couchbase/var/lib/data"
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

func CreateCouchbasePod(m *couchbaseutil.Member, clusterName string, cs cbapi.ClusterSpec, owner metav1.OwnerReference) *v1.Pod {

	labels := createCouchbasePodLabels(m.Name, clusterName, cs)

	container := containerWithLivenessProbe(couchbaseContainer("", cs.BaseImage, cs.Version),
		couchbaseLivenessProbe())

	if cs.Pod != nil {
		container = containerWithRequirements(container, cs.Pod.Resources)
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

	applyPodPolicy(clusterName, pod, cs.Pod)

	SetCouchbaseVersion(pod, cs.Version)

	addOwnerRefToObject(pod.GetObjectMeta(), owner)
	return pod
}

func createCouchbasePodLabels(memberName, clusterName string, cs cbapi.ClusterSpec) map[string]string {
	labels := map[string]string{
		"app":               "couchbase",
		"couchbase_node":    memberName,
		"couchbase_cluster": clusterName,
	}
	if cs.ClusterSettings != nil {
		services := cs.ClusterSettings.ServicesArr()
		for _, s := range services {
			k := "couchbase_service_" + s
			labels[k] = "enabled"
		}
	}

	return labels
}

func createCouchbaseServiceManifest(svcName, clusterName, clusterIP string, ports []v1.ServicePort, portName string) *v1.Service {
	labels := map[string]string{
		"app":               "couchbase",
		"couchbase_cluster": clusterName,
		"couchbase_node":    portName,
	}
	if portName == "cb-admin" {
		labels = map[string]string{
			"app":               "couchbase",
			"couchbase_cluster": clusterName,
		}
	}
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   portName,
			Labels: labels,
			Annotations: map[string]string{
				TolerateUnreadyEndpointsAnnotation: "true",
			},
		},
		Spec: v1.ServiceSpec{
			Type: 	   "LoadBalancer",
			Ports:     ports,
			Selector:  labels,
		},
	}
	if portName == "cb-admin" {
		svc = &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:   svcName,
				Labels: labels,
				Annotations: map[string]string{
					TolerateUnreadyEndpointsAnnotation: "true",
				},
			},
			Spec: v1.ServiceSpec{
				Ports:     ports,
				Selector:  labels,
				ClusterIP: clusterIP,
			},
		}
	}
	return svc
}

// creates a service of Type ClusterIP which is only resolvable internally.
// futhermore the ClusterIP is "None" allowing the service to run "headless"
// (sans load balancing middleware) which allows the operator to resolve
// addresses of individual pods instead of a proxy
func CreatePeerService(kubecli kubernetes.Interface, clusterName, ns string, owner metav1.OwnerReference, memberName string) error {
	portName := "cb-admin"

	if memberName != "" {
		portName = memberName
	}

	ports := []v1.ServicePort{{
		Name:       portName,
		Port:       8091,
		TargetPort: intstr.FromInt(8091),
		Protocol:   v1.ProtocolTCP,
	}}
	return createService(kubecli, clusterName, clusterName, ns, v1.ClusterIPNone, ports, owner, portName)
}

func createService(kubecli kubernetes.Interface, svcName, clusterName, ns, clusterIP string, ports []v1.ServicePort, owner metav1.OwnerReference, portName string) error {
	svc := createCouchbaseServiceManifest(svcName, clusterName, clusterIP, ports, portName)
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	_, err := kubecli.CoreV1().Services(ns).Create(svc)
	return err
}

func ClusterListOpt(clusterName string) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(LabelsForCluster(clusterName)).String(),
	}
}

func LabelsForCluster(clusterName string) map[string]string {
	return map[string]string{
		"couchbase_cluster": clusterName,
		"app":               "couchbase",
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
