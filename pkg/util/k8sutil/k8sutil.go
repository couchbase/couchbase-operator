package k8sutil

import (
	//"fmt"
	"net"
	"os"

	"github.com/couchbaselabs/couchbase-operator/pkg/spec"
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
	// See https://github.com/coreos/etcd-operator/issues/731#issuecomment-283804819
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

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

func CreateCouchbasePod(m *couchbaseutil.Member, clusterName string, cs spec.ClusterSpec, owner metav1.OwnerReference) *v1.Pod {
	labels := map[string]string{
		"app":               "couchbase",
		"couchbase_node":    m.Name,
		"couchbase_cluster": clusterName,
	}

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

func createCouchbaseServiceManifest(svcName, clusterName, clusterIP string, ports []v1.ServicePort) *v1.Service {
	labels := map[string]string{
		"app":               "couchbase",
		"couchbase_cluster": clusterName,
	}
	svc := &v1.Service{
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
	return svc
}

func CreateCouchbaseService(kubecli kubernetes.Interface, clusterName, ns string, owner metav1.OwnerReference) error {
	ports := []v1.ServicePort{{
		Name:       "cb-admin",
		Port:       8091,
		TargetPort: intstr.FromInt(8091),
		Protocol:   v1.ProtocolTCP,
	}}

	svc := createCouchbaseServiceManifest(ClientServiceName(clusterName), clusterName, "", ports)
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
