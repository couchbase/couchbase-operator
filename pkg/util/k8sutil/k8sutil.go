package k8sutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/ghodss/yaml"

	"k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ApplyBaseAnnotations adds common annotations to resources.
// All resources created should have this set of annotations for upgrade reasons.
func ApplyBaseAnnotations(object metav1.Object) {
	annotations := map[string]string{
		constants.ResourceVersionAnnotation: version.Version,
	}
	annotations = mergeLabels(object.GetAnnotations(), annotations)
	object.SetAnnotations(annotations)
}

func CouchbaseVersion(image string) (string, error) {
	parts := strings.Split(image, ":")

	lenParts := len(parts)
	if lenParts < 2 {
		return "", fmt.Errorf("invalid image string: %s", image)
	}

	version := parts[lenParts-1]

	// lookup version associated with sha256 digest
	if couchbaseutil.IsSHA256Version(version) {
		return couchbaseutil.GetSHA256Version(version)
	}

	return version, nil
}

func SetCouchbaseVersion(pod *v1.Pod, image string) error {
	version, err := CouchbaseVersion(image)
	if err != nil {
		return err
	}
	pod.Annotations[constants.CouchbaseVersionAnnotationKey] = version
	return nil
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

func GetHostIP(client *client.Client, name string) (string, error) {
	pod, found := client.Pods.Get(name)
	if !found {
		return "", fmt.Errorf("pod %s not found", name)
	}
	if pod.Status.HostIP == "" {
		return "", fmt.Errorf("host IP unset, pod not scheduled")
	}
	return pod.Status.HostIP, nil
}

func GetServerGroup(client *client.Client, name string) (string, error) {
	pod, found := client.Pods.Get(name)
	if !found {
		return "", fmt.Errorf("pod %s not found", name)
	}
	if pod.Spec.NodeSelector == nil {
		return "", fmt.Errorf("pod %s has no node selector", name)
	}
	serverGroup := pod.Spec.NodeSelector[constants.ServerGroupLabel]
	return serverGroup, nil
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
func getNodeServiceSelectors(cluster *couchbasev2.CouchbaseCluster, nodeName string) map[string]string {
	// Apply to a specific couchbase pod within the named cluster
	selectors := LabelsForCluster(cluster.Name)
	selectors[constants.LabelNode] = nodeName
	return selectors
}

// LabelsForCluster returns a basic set of labels which will identify a couchbase
// pod within a specific cluster.
func LabelsForCluster(clusterName string) map[string]string {
	return map[string]string{
		constants.LabelCluster: clusterName,
		constants.LabelApp:     constants.App,
	}
}

func NodeListOpt(clusterName, memberName string) metav1.ListOptions {
	l := LabelsForCluster(clusterName)
	l[constants.LabelNode] = memberName
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
func mergeLabels(l1, l2 map[string]string) map[string]string {
	if l1 == nil {
		return l2
	}

	m := map[string]string{}
	for k, v := range l1 {
		m[k] = v
	}
	for k, v := range l2 {
		m[k] = v
	}
	return m
}

func DeletePod(client *client.Client, namespace, podName string, opts *metav1.DeleteOptions) error {
	err := client.KubeClient.CoreV1().Pods(namespace).Delete(podName, opts)
	if err != nil {
		if !IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

func CreatePod(client *client.Client, namespace string, pod *v1.Pod) (*v1.Pod, error) {
	return client.KubeClient.CoreV1().Pods(namespace).Create(pod)
}

// Waits for a pod to be created and for it to respond to TCP connections on
// the admin port.  Accepts a context, typically with a timeout, which can
// abort the operation.  The any timeout duration covers the whole process.
func WaitForPod(ctx context.Context, kubeCli kubernetes.Interface, namespace, podName, hostURL string) error {
	// Do the simplest check possible here.  Kubernetes will reconcile continually until the
	// pod is successfully scheduled and all dependencies e.g. persistent volumes.  Don't ever
	// short cut this, honour context the timeout!
	callback := func() error {
		// TODO: cache me, used my cbopinfo :/
		pod, err := kubeCli.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if pod.Status.Phase != v1.PodRunning {
			return cberrors.ErrCreatingPod{Reason: pod.Status.Reason}
		}

		return nil
	}
	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		return err
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

func WaitForDeletePod(ctx context.Context, kubeCli kubernetes.Interface, namespace, podName string) error {
	if _, err := kubeCli.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{}); err != nil {
		if IsKubernetesResourceNotFoundError(err) {
			return nil
		} else {
			return err
		}
	}

	// watch for pod deletion event
	opts := metav1.ListOptions{
		LabelSelector: "couchbase_node=" + podName,
	}

	watcher, err := kubeCli.CoreV1().Pods(namespace).Watch(opts)
	if err != nil {
		return err
	}
	events := watcher.ResultChan()

	done := false
	for !done {
		select {
		// Handle timeout and cancellation events
		case <-ctx.Done():
			return fmt.Errorf("%v: Pod Deletion error - error deleting pod %v", ctx.Err(), podName)
		// Process K8S events for our chosen pod
		case ev := <-events:
			if ev.Object == nil {
				continue
			}
			obj := ev.Object.(*v1.Pod)
			status := obj.Status

			switch ev.Type {
			// check if any error occurred creating pod
			case watch.Error:
				return cberrors.ErrDeletingPod{Reason: status.Reason}
			case watch.Deleted:
				return nil
			default:
				continue
			}
		}
	}

	return fmt.Errorf("failed to wait for pod to delete: %s", podName)
}

func GetKubernetesVersion(kubeCli kubernetes.Interface) (constants.KubernetesVersion, error) {
	version, err := kubeCli.Discovery().ServerVersion()
	if err != nil {
		return constants.KubernetesVersionUnknown, err
	}
	return ParseKubernetesVersion(version.Major, version.Minor, version.GitVersion)
}

func ParseKubernetesVersion(versionMajor, versionMinor, gitVersion string) (constants.KubernetesVersion, error) {
	// Sometimes the Major and Minor values are not set so we need to parse them
	// from the GitVersion field.
	if versionMajor == "" || versionMinor == "" {
		rx := regexp.MustCompile("^v[0-9]{1,2}.[0-9]{1,2}.[0-9]{1,2}")
		if !rx.MatchString(gitVersion) {
			err := fmt.Errorf("unable to get version from Kubernetes API response")
			return constants.KubernetesVersionUnknown, err
		}
		gitVersion = rx.FindString(gitVersion)
		parts := strings.Split(gitVersion[1:], ".")
		versionMajor = parts[0]
		versionMinor = parts[1]
	}

	rx := regexp.MustCompile("^[0-9]{1,2}")
	// simply require that version starts with a number to be valid
	if !rx.MatchString(versionMajor) || !rx.MatchString(versionMinor) {
		err := fmt.Errorf("unable to get version from Kubernetes API response")
		return constants.KubernetesVersionUnknown, err
	}
	major, err := strconv.Atoi(rx.FindString(versionMajor))
	if err != nil {
		err := fmt.Errorf("unable to get version from Kubernetes API response")
		return constants.KubernetesVersionUnknown, err
	}
	minor, err := strconv.Atoi(rx.FindString(versionMinor))
	if err != nil {
		err := fmt.Errorf("unable to get version from Kubernetes API response")
		return constants.KubernetesVersionUnknown, err
	}
	return constants.KubernetesVersion(fmt.Sprintf("%02d%02d", major, minor)), nil
}

func GetStorageClass(kubeCli kubernetes.Interface, name string) (*storage.StorageClass, error) {
	return kubeCli.StorageV1().StorageClasses().Get(name, metav1.GetOptions{})
}

func GetPodUptime(client *client.Client, name string) int {
	pod, found := client.Pods.Get(name)
	if !found {
		return 0
	}
	return int(time.Since(pod.CreationTimestamp.Time).Seconds())
}

// LogPod returns ephemeral debug information about a failed pod, e.g. it's about to be
// deleted and we want to know why.
func LogPod(client *client.Client, namespace, name string) (output string) {
	pod, found := client.Pods.Get(name)
	if found {
		// Dump the pod
		data, err := yaml.Marshal(pod)
		if err == nil {
			output += string(data) + "\n"
		}

		// Dump the pod's logs
		for _, container := range pod.Spec.Containers {
			logs := client.KubeClient.CoreV1().Pods(namespace).GetLogs(pod.Name, &v1.PodLogOptions{Container: container.Name})

			readCloser, err := logs.Stream()
			if err != nil {
				continue
			}

			buffer := &bytes.Buffer{}
			if _, err := io.Copy(buffer, readCloser); err != nil {
				readCloser.Close()
				continue
			}
			readCloser.Close()

			output += buffer.String() + "\n"
		}
	}

	// Dump the pods's events
	events, err := GetEventsForResource(client.KubeClient, namespace, "Pod", name)
	if err == nil {
		for _, event := range events {
			data, err := yaml.Marshal(event)
			if err != nil {
				continue
			}
			output += string(data) + "\n"
		}
	}

	for _, pvc := range client.PersistentVolumeClaims.List() {
		node, ok := pvc.Labels[constants.LabelNode]
		if !ok || node != name {
			continue
		}

		// Dump the pod's PVC.
		data, err := yaml.Marshal(pvc)
		if err == nil {
			output += string(data) + "\n"
		}

		// Dump the pod's PVC events.
		events, err := GetEventsForResource(client.KubeClient, namespace, "PersistentVolumeClaim", pvc.Name)
		if err == nil {
			for _, event := range events {
				data, err := yaml.Marshal(event)
				if err != nil {
					continue
				}
				output += string(data) + "\n"
			}
		}
	}

	return
}

// NewResourceQuantityMi accepts an integral value representing megabytes (2^20)
// and returns a quantity.
func NewResourceQuantityMi(value int64) *resource.Quantity {
	return resource.NewQuantity(value<<20, resource.BinarySI)
}

// NewDurationS accepts an integral value representing seconds and returns
// a duration.
func NewDurationS(value int64) *metav1.Duration {
	return &metav1.Duration{Duration: time.Duration(value) * time.Second}
}

// Megabytes accepts a quantity and returns an integral value representing megabytes.
func Megabytes(quantity *resource.Quantity) int64 {
	return quantity.Value() >> 20
}

// Seconds accepts a duration and returns an integral value representing seconds.
func Seconds(duration *metav1.Duration) int64 {
	return int64(duration.Seconds())
}
