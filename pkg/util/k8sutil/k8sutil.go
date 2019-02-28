package k8sutil

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/prettytable"
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/ghodss/yaml"

	"k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// TODO: This is deprecated in 1.11+, remove me next release and use PublishNotReadyAddresses
const TolerateUnreadyEndpointsAnnotation = "service.alpha.kubernetes.io/tolerate-unready-endpoints"

// applyBaseAnnotations adds common annotations to resources.
// All resources created should have this set of annotations for upgrade reasons.
func applyBaseAnnotations(object metav1.Object) {
	annotations := map[string]string{
		constants.ResourceVersionAnnotation: version.Version,
	}
	mergeLabels(annotations, object.GetAnnotations())
	object.SetAnnotations(annotations)
}

func SetCouchbaseVersion(pod *v1.Pod, version string) {
	pod.Annotations[constants.CouchbaseVersionAnnotationKey] = version
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

func createService(kubecli kubernetes.Interface, ns string, svc *v1.Service, owner metav1.OwnerReference) (*v1.Service, error) {
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	return kubecli.CoreV1().Services(ns).Create(svc)
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

func GetPod(kubecli kubernetes.Interface, ns, name string) (*v1.Pod, error) {
	return kubecli.CoreV1().Pods(ns).Get(name, metav1.GetOptions{})
}

func GetHostIP(kubecli kubernetes.Interface, ns, name string) (string, error) {
	pod, err := GetPod(kubecli, ns, name)
	if err != nil {
		return "", err
	}
	if pod.Status.HostIP == "" {
		return "", fmt.Errorf("host IP unset, pod not scheduled")
	}
	return pod.Status.HostIP, nil
}

func GetServerGroup(kubecli kubernetes.Interface, ns, name string) (string, error) {
	pod, err := GetPod(kubecli, ns, name)
	if err != nil {
		return "", err
	}
	if pod.Spec.NodeSelector == nil {
		return "", err
	}
	serverGroup, _ := pod.Spec.NodeSelector[constants.ServerGroupLabel]
	return serverGroup, nil
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
			return fmt.Errorf("%v: Pod Creation error - error creating pod %v", ctx.Err(), podName)
		// Process K8S events for our chosen pod
		case ev := <-events:
			if ev.Object == nil {
				return WaitForPod(ctx, kubeCli, namespace, podName, hostURL)
			}
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
								// Ignoring unbound errors because the scheduler will eventually
								// resolve them and actually schedule the Pod
								// as is the case with lazy binding
								if !strings.Contains(cond.Message, cberrors.ErrUnboundPersistedVolumeClaims) {
									return cberrors.ErrPodUnschedulable{Reason: cond.Message}
								}
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

func WaitForDeletePod(ctx context.Context, kubeCli kubernetes.Interface, namespace, podName string) error {
	_, err := kubeCli.Core().Pods(namespace).Get(podName, metav1.GetOptions{})
	if err != nil {
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

// Waits for a persistent volume claim belonging to be created with bounded volumes
func WaitForPersistentVolumeClaim(ctx context.Context, kubeCli kubernetes.Interface, namespace, claimName string) error {

	opts := metav1.ListOptions{
		FieldSelector: "metadata.name=" + claimName,
	}

	watcher, err := kubeCli.CoreV1().PersistentVolumeClaims(namespace).Watch(opts)
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
			return cberrors.ErrCreatingPersistentVolumeClaim{Reason: fmt.Sprintf("%v for %v", ctx.Err().Error(), claimName)}

		// Process K8S events of creation cycle
		case ev := <-events:
			if ev.Object == nil {
				continue
			}
			obj := ev.Object.(*v1.PersistentVolumeClaim)
			status := obj.Status

			conditions := []string{}
			for _, c := range status.Conditions {
				msg := fmt.Sprintf("%s: %s", c.Reason, c.Message)
				conditions = append(conditions, msg)
			}
			reason := strings.Join(conditions, ",")

			switch ev.Type {
			// check if any error occurred while creating
			case watch.Error:
				return cberrors.ErrCreatingPersistentVolumeClaim{Reason: reason}
			case watch.Deleted:
				return cberrors.ErrCreatingPersistentVolumeClaim{Reason: reason}
			case watch.Added, watch.Modified:
				// make sure claim is bound to a volume
				switch status.Phase {
				case v1.ClaimPending:
					continue
				case v1.ClaimBound:
					// Claim object is ready
					return nil
				case v1.ClaimLost:
					return cberrors.ErrCreatingPersistentVolumeClaim{Reason: reason}
				}
			}
		}
	}

	return nil
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
			err := fmt.Errorf("Unable to get version from Kubernetes API response")
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
		err := fmt.Errorf("Unable to get version from Kubernetes API response")
		return constants.KubernetesVersionUnknown, err
	}
	major, err := strconv.Atoi(rx.FindString(versionMajor))
	if err != nil {
		err := fmt.Errorf("Unable to get version from Kubernetes API response")
		return constants.KubernetesVersionUnknown, err
	}
	minor, err := strconv.Atoi(rx.FindString(versionMinor))
	if err != nil {
		err := fmt.Errorf("Unable to get version from Kubernetes API response")
		return constants.KubernetesVersionUnknown, err
	}
	return constants.KubernetesVersion(fmt.Sprintf("%02d%02d", major, minor)), nil
}

func GetStorageClass(kubeCli kubernetes.Interface, name string) (*storage.StorageClass, error) {
	return kubeCli.StorageV1().StorageClasses().Get(name, metav1.GetOptions{})
}

func GetPodUptime(kubecli kubernetes.Interface, ns, name string) int {
	pod, err := GetPod(kubecli, ns, name)
	if err != nil {
		return 0
	}
	return int(time.Now().Sub(pod.CreationTimestamp.Time).Seconds())
}

// LogPod returns ephemeral debug information about a failed pod, e.g. it's about to be
// deleted and we want to know why.
func LogPod(kubeCli kubernetes.Interface, namespace, name string) []string {
	result := []string{}

	pod, err := GetPod(kubeCli, namespace, name)
	if err == nil {
		if data, err := yaml.Marshal(pod); err == nil {
			result = append(result, "resource:")
			for _, line := range strings.Split(string(data), "\n") {
				result = append(result, line)
			}
		}
	}

	events, err := GetEventsForResource(kubeCli, namespace, "Pod", name)
	if err == nil {
		result = append(result, "events:")
		table := prettytable.Table{
			Header: prettytable.Row{"FirstTimestamp", "LastTimestamp", "Count", "Type", "Reason", "Message"},
		}
		for _, event := range events {
			row := prettytable.Row{
				event.FirstTimestamp.String(),
				event.LastTimestamp.String(),
				strconv.Itoa(int(event.Count)),
				event.Type,
				event.Reason,
				event.Message,
			}
			table.Rows = append(table.Rows, row)
		}
		data := &bytes.Buffer{}
		if err := table.Write(data); err == nil {
			for _, line := range strings.Split(string(data.Bytes()), "\n") {
				result = append(result, line)
			}
		}
	}

	return result
}
