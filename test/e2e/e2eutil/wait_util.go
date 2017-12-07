package e2eutil

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

var retryInterval = 10 * time.Second

type acceptFunc func(*api.CouchbaseCluster) bool
type filterFunc func(*v1.Pod) bool

func WaitUntilPodSizeReached(t *testing.T, kubeClient kubernetes.Interface, size, retries int, cl *api.CouchbaseCluster) ([]string, error) {
	var names []string
	err := retryutil.Retry(retryInterval, retries, func() (done bool, err error) {
		podList, err := kubeClient.Core().Pods(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
		if err != nil {
			return false, err
		}
		names = nil
		var nodeNames []string
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase != v1.PodRunning {
				continue
			}
			names = append(names, pod.Name)
			nodeNames = append(nodeNames, pod.Spec.NodeName)
		}
		LogfWithTimestamp(t, "waiting size (%d), couchbase pods: names (%v), nodes (%v)", size, names, nodeNames)
		if len(names) != size {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func WaitUntilSizeReached(t *testing.T, crClient versioned.Interface, size, retries int, cl *api.CouchbaseCluster) ([]string, error) {
	return waitSizeReachedWithAccept(t, crClient, size, retries, cl)
}

func waitSizeReachedWithAccept(t *testing.T, crClient versioned.Interface, size, retries int, cl *api.CouchbaseCluster, accepts ...acceptFunc) ([]string, error) {
	var names []string
	err := retryutil.Retry(retryInterval, retries, func() (done bool, err error) {
		currCluster, err := crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, accept := range accepts {
			if !accept(currCluster) {
				return false, nil
			}
		}

		names = currCluster.Status.Members.Ready
		LogfWithTimestamp(t, "waiting size (%d), healthy couchbase members: names (%v)", size, names)
		if len(names) != size {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func WaitUntilBucketsExists(t *testing.T, crClient versioned.Interface, buckets []string, retries int, cl *api.CouchbaseCluster, accepts ...acceptFunc) error {
	err := retryutil.Retry(retryInterval, retries, func() (done bool, err error) {
		currCluster, err := crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, accept := range accepts {
			if !accept(currCluster) {
				return false, nil
			}
		}

		LogfWithTimestamp(t, "waiting for buckets to be ready (%v)", buckets)
		for _, b := range buckets {
			if _, ok := currCluster.Status.Buckets[b]; !ok {
				return false, nil
			}
		}
		return true, nil
	})

	if err != nil {
		return err
	}
	return nil

}

func WaitUntilBucketsNotExists(t *testing.T, crClient versioned.Interface, buckets []string, retries int, cl *api.CouchbaseCluster, accepts ...acceptFunc) error {
	err := retryutil.Retry(retryInterval, retries, func() (done bool, err error) {
		currCluster, err := crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, accept := range accepts {
			if accept(currCluster) {
				return false, nil
			}
		}

		LogfWithTimestamp(t, "waiting for buckets to be deleted (%v)", buckets)
		for _, b := range buckets {
			if _, ok := currCluster.Status.Buckets[b]; ok {
				return false, nil
			}
		}
		return true, nil
	})

	if err != nil {
		return err
	}
	return nil

}

func waitResourcesDeleted(t *testing.T, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, retries int) error {
	undeletedPods, err := WaitPodsDeleted(kubeClient, cl.Namespace, retries, k8sutil.ClusterListOpt(cl.Name))
	if err != nil {
		if retryutil.IsRetryFailure(err) && len(undeletedPods) > 0 {
			p := undeletedPods[0]
			LogfWithTimestamp(t, "waiting pod (%s) to be deleted.", p.Name)

			buf := bytes.NewBuffer(nil)
			buf.WriteString("init container status:\n")
			printContainerStatus(buf, p.Status.InitContainerStatuses)
			buf.WriteString("container status:\n")
			printContainerStatus(buf, p.Status.ContainerStatuses)
			t.Logf("pod (%s) status.phase is (%s): %v", p.Name, p.Status.Phase, buf.String())
		}

		return fmt.Errorf("fail to wait pods deleted: %v", err)
	}

	err = retryutil.Retry(retryInterval, 3, func() (done bool, err error) {
		list, err := kubeClient.CoreV1().Services(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
		if err != nil {
			return false, err
		}
		if len(list.Items) > 0 {
			LogfWithTimestamp(t, "waiting service (%s) to be deleted", list.Items[0].Name)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("fail to wait services deleted: %v", err)
	}
	return nil
}

func WaitPodsDeleted(kubecli kubernetes.Interface, namespace string, retries int, lo metav1.ListOptions) ([]*v1.Pod, error) {
	f := func(p *v1.Pod) bool { return p.DeletionTimestamp != nil }
	return waitPodsDeleted(kubecli, namespace, retries, lo, f)
}

func waitPodsDeleted(kubecli kubernetes.Interface, namespace string, retries int, lo metav1.ListOptions, filters ...filterFunc) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	err := retryutil.Retry(retryInterval, retries, func() (bool, error) {
		podList, err := kubecli.CoreV1().Pods(namespace).List(lo)
		if err != nil {
			return false, err
		}
		pods = nil
		for i := range podList.Items {
			p := &podList.Items[i]
			filtered := false
			for _, filter := range filters {
				if filter(p) {
					filtered = true
				}
			}
			if !filtered {
				pods = append(pods, p)
			}
		}
		return len(pods) == 0, nil
	})
	return pods, err
}

// WaitUntilOperatorReady will wait until the first pod selected for the label name=couchbase-operator is ready.
func WaitUntilOperatorReady(kubecli kubernetes.Interface, namespace, name string) error {
	var podName string
	lo := metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(NameLabelSelector(name)).String(),
	}
	err := retryutil.Retry(10*time.Second, 6, func() (bool, error) {
		podList, err := kubecli.CoreV1().Pods(namespace).List(lo)
		if err != nil {
			return false, err
		}
		if len(podList.Items) > 0 {
			podName = podList.Items[0].Name
			if k8sutil.IsPodReady(&podList.Items[0]) {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for pod (%v) to become ready: %v", podName, err)
	}
	return nil
}

func CreateAndWaitPod(kubecli kubernetes.Interface, ns string, pod *v1.Pod, timeout time.Duration) (*v1.Pod, error) {
	_, err := kubecli.CoreV1().Pods(ns).Create(pod)
	if err != nil {
		return nil, err
	}

	interval := 5 * time.Second
	var retPod *v1.Pod
	err = retryutil.Retry(interval, int(timeout/(interval)), func() (bool, error) {
		retPod, err = kubecli.CoreV1().Pods(ns).Get(pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		switch retPod.Status.Phase {
		case v1.PodRunning:
			return true, nil
		case v1.PodPending:
			return false, nil
		default:
			return false, fmt.Errorf("unexpected pod status.phase: %v", retPod.Status.Phase)
		}
	})

	if err != nil {
		if retryutil.IsRetryFailure(err) {
			return nil, fmt.Errorf("failed to wait pod running, it is still pending: %v", err)
		}
		return nil, fmt.Errorf("failed to wait pod running: %v", err)
	}

	return retPod, nil
}
