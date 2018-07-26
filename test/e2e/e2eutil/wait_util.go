package e2eutil

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"

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
	err := retryutil.Retry(Context, retryInterval, retries, func() (done bool, err error) {
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
	err := retryutil.Retry(Context, retryInterval, retries, func() (done bool, err error) {
		currCluster, err := crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, accept := range accepts {
			if !accept(currCluster) {
				return false, nil
			}
		}

		names = currCluster.Status.Members.Ready.Names()
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
	err := retryutil.Retry(Context, retryInterval, retries, func() (done bool, err error) {
		currCluster, err := crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
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
				LogfWithTimestamp(t, "bucket (%v), not ready: (%v)", b, currCluster.Status.Buckets[b])
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
	err := retryutil.Retry(Context, retryInterval, retries, func() (done bool, err error) {
		currCluster, err := crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
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

// WaitClusterPhaseFailed expects the cluster to enter a failed state, useful for passing
// quickly rather than wating for a cluster to not become healthy
func WaitClusterPhaseFailed(t *testing.T, crClient versioned.Interface, name, namespace string, retries int) error {
	err := retryutil.Retry(Context, retryInterval, retries, func() (bool, error) {
		cluster, err := GetCouchbaseCluster(crClient, name, namespace)
		if err != nil {
			return false, err
		}
		return cluster.Status.Phase == api.ClusterPhaseFailed, nil
	})
	return err
}

func WaitClusterStatusHealthy(t *testing.T, crClient versioned.Interface, name, namespace string, expectedNodes, retries int) error {
	err := retryutil.Retry(Context, retryInterval, retries, func() (done bool, err error) {
		cl, err := GetCouchbaseCluster(crClient, name, namespace)
		if err != nil {
			LogfWithTimestamp(t, "could not get cluster: (%v)", err)
			return false, err
		}

		if cl.Status.Size != expectedNodes {
			LogfWithTimestamp(t, "Cluster nodes (%d) does not match expected nodes (%d) \n", cl.Status.Size, expectedNodes)
			return false, nil
		}

		LogfWithTimestamp(t, "Cluster Status Conditions: (%v)", cl.Status.Conditions)

		healthyConditions := map[api.ClusterConditionType]struct {
			healthyCondition v1.ConditionStatus
			message          string
		}{
			api.ClusterConditionAvailable: {
				v1.ConditionTrue,
				"Cluster not available ...",
			},
			api.ClusterConditionBalanced: {
				v1.ConditionTrue,
				"Cluster unbalanced ...",
			},
			api.ClusterConditionScaling: {
				v1.ConditionFalse,
				"Cluster scaling ...",
			},
		}

		for kind, cond := range cl.Status.Conditions {
			healthyCondition, ok := healthyConditions[kind]
			if !ok {
				continue
			}

			if cond.Status != healthyCondition.healthyCondition {
				LogfWithTimestamp(t, "%s", healthyCondition.message)
				return false, nil
			}
		}
		LogfWithTimestamp(t, "Cluster healthy")
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("fail to wait for cluster status to be healthy: %v \n", err)
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

	err = retryutil.Retry(Context, retryInterval, 3, func() (done bool, err error) {
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

func WaitPodDeleted(t *testing.T, kubeClient kubernetes.Interface, podName string, cl *api.CouchbaseCluster) error {
	undeletedPods, err := WaitPodsDeleted(kubeClient, cl.Namespace, 3, k8sutil.NodeListOpt(cl.Name, podName))
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
	return nil
}

func WaitUntilPodDeleted(t *testing.T, kubeClient kubernetes.Interface, namespace string) error {
	undeletedPods, err := WaitPodsDeleted(kubeClient, namespace, 3, metav1.ListOptions{LabelSelector: "app=couchbase"})
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
	return nil
}

func WaitPodsDeleted(kubecli kubernetes.Interface, namespace string, retries int, lo metav1.ListOptions) ([]*v1.Pod, error) {
	f := func(p *v1.Pod) bool { return p.DeletionTimestamp != nil }
	return waitPodsDeleted(kubecli, namespace, retries, lo, f)
}

func waitPodsDeleted(kubecli kubernetes.Interface, namespace string, retries int, lo metav1.ListOptions, filters ...filterFunc) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	err := retryutil.Retry(Context, retryInterval, retries, func() (bool, error) {
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
		LabelSelector: labels.SelectorFromSet(NameLabelSelector("app", name)).String(),
	}
	err := retryutil.Retry(Context, time.Second, 180, func() (bool, error) {
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

func WaitUntilOperatorDeleted(kubecli kubernetes.Interface, namespace, name string) error {
	lo := metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(NameLabelSelector("app", name)).String(),
	}
	err := retryutil.Retry(Context, 10*time.Second, 6, func() (bool, error) {
		podList, err := kubecli.CoreV1().Pods(namespace).List(lo)
		if err != nil {
			return false, err
		}
		if len(podList.Items) == 0 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for pods with label=(%v) to be deleted: %v", name, err)
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
	err = retryutil.Retry(Context, interval, int(timeout/(interval)), func() (bool, error) {
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

// waits until the provided condition type occurs with associated status
func WaitForClusterEvent(kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, event *v1.Event, seconds int) error {
	opts := metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{Kind: api.CRDResourceKind},
	}
	watch, err := kubeClient.CoreV1().Events(cl.Namespace).Watch(opts)
	if err != nil {
		return err
	}
	defer watch.Stop()

	now := metav1.Now()

	resultChan := watch.ResultChan()
	duration := time.Duration(seconds) * time.Second
	timeoutChan := time.After(duration)
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("Time out waiting for cluster event %s, %s:", event.Reason, event.Message)

		case watchEvent := <-resultChan:
			crdEvent := watchEvent.Object.(*v1.Event)
			// Watch() returns every event since the dawn of time, so ensure we
			// only return things after we started the wait.  This avoids matching
			// events that may have already occurred
			if crdEvent.FirstTimestamp.Before(&now) {
				continue
			}
			if EqualEvent(event, crdEvent) {
				return nil
			}
		}
	}
}

func WaitForClusterEventsInParallel(kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, expectedEvents []*v1.Event, seconds int) error {
	errs := make(chan error)
	for _, event := range expectedEvents {
		// Creates go routines for each event
		go func(event *v1.Event) {
			errs <- WaitForClusterEvent(kubeClient, cl, event, seconds)
		}(event)
	}

	// Wait for events to complete
	var eventErr error
	for _, _ = range expectedEvents {
		if err := <-errs; err != nil {
			eventErr = err
			break
		}
	}
	return eventErr
}

func WaitForManagedConfigCondition(t *testing.T, crClient versioned.Interface, cl *api.CouchbaseCluster, status v1.ConditionStatus, wait int) error {
	return WaitForClusterCondition(t, crClient, api.ClusterConditionBalanced, status, cl, time.Now(), wait)
}

// waits until the cluter's scaling condition
func WaitForClusterScalingCondition(t *testing.T, crClient versioned.Interface, cl *api.CouchbaseCluster, wait int) error {
	return WaitForClusterCondition(t, crClient, api.ClusterConditionScaling, v1.ConditionTrue, cl, time.Now(), wait)
}

// waits until the cluter's balanced condition is set
func WaitForClusterBalancedCondition(t *testing.T, crClient versioned.Interface, cl *api.CouchbaseCluster, wait int) error {
	return WaitForClusterCondition(t, crClient, api.ClusterConditionBalanced, v1.ConditionTrue, cl, time.Now(), wait)
}

// waits until the cluter's balanced condition is false
func WaitForClusterUnBalancedCondition(t *testing.T, crClient versioned.Interface, cl *api.CouchbaseCluster, wait int) error {
	return WaitForClusterCondition(t, crClient, api.ClusterConditionBalanced, v1.ConditionFalse, cl, time.Now(), wait)
}

// waits until the provided condition type with associated status after specified timestamp
func WaitForClusterCondition(t *testing.T, crClient versioned.Interface, conditionType api.ClusterConditionType, status v1.ConditionStatus, cl *api.CouchbaseCluster, after time.Time, wait int) error {
	cluster, err := GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
	if err != nil {
		return err
	}

	timeOutChan := time.NewTimer(time.Duration(wait) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return fmt.Errorf("timed out waiting for condition %s with status: %s", conditionType, status)

		case <-tickChan:
			// compare cluster conditions to desired condition
			t.Logf("cluster status: %s", cluster.Status.Conditions)
			if condition, ok := cluster.Status.Conditions[conditionType]; ok {
				if condition.Status == status {
					conditionTime, err := time.Parse(time.RFC3339, condition.LastUpdateTime)
					if err != nil {
						return err
					}
					if conditionTime.After(after) {
						return nil
					}
				}
			}
			// update cluster
			cluster, err = GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
			if err != nil {
				return err
			}
		}
	}
}

func WaitForClusterStatus(t *testing.T, crClient versioned.Interface, statusType string, statusValue string, cl *api.CouchbaseCluster, wait int) error {
	cluster, err := GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
	if err != nil {
		return err
	}

	timeOutChan := time.NewTimer(time.Duration(wait) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return fmt.Errorf("timed out waiting for status %s with status: %s", statusType, statusValue)

		case <-tickChan:
			// compare cluster conditions to desired condition
			if statusType == "ControlPaused" {
				t.Logf("cluster paused: %t", cluster.Status.ControlPaused)
				desiredStatusBool, _ := strconv.ParseBool(statusValue)
				if desiredStatusBool == cluster.Status.ControlPaused {
					return nil
				}
			}
			// update cluster
			cluster, err = GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
			if err != nil {
				return err
			}
		}
	}
}

func WaitUntilAccepts(t *testing.T, crClient versioned.Interface, retries int, cl *api.CouchbaseCluster, accepts ...acceptFunc) error {
	interval := 5 * time.Second
	err := retryutil.Retry(Context, interval, retries, func() (done bool, err error) {
		currCluster, err := crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, accept := range accepts {
			if !accept(currCluster) {
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

func WaitUntilEventsCompare(t *testing.T, kubecli kubernetes.Interface, retries int, cl *api.CouchbaseCluster, compareEvents EventList, namespace string) error {
	interval := 5 * time.Second
	err := retryutil.Retry(Context, interval, retries, func() (done bool, err error) {
		events, err := GetCouchbaseEvents(kubecli, cl.Name, namespace)
		if err != nil {
			return false, fmt.Errorf("failed to get coucbase cluster events: %v", err)
		}
		if !compareEvents.Compare(events) {
			return false, errors.New(EventListCompareFailedString(compareEvents, events))
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	return nil
}

func WaitForConditionMessage(t *testing.T, crClient versioned.Interface, retries int, cl *api.CouchbaseCluster, conditionType api.ClusterConditionType, message string) error {
	interval := 5 * time.Second
	err := retryutil.Retry(Context, interval, retries, func() (done bool, err error) {
		cluster, err := GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
		if err != nil {
			return false, err
		}
		for kind, condition := range cluster.Status.Conditions {
			if kind == conditionType && strings.Contains(condition.Message, message) {
				return true, nil
			}
			t.Logf("condition: %v, message: %v", conditionType, condition.Message)
		}
		t.Logf("conditions: %v", cluster.Status.Conditions)
		return false, nil

	})
	if err != nil {
		return err
	}
	return nil
}

func WaitForKubeNodesToBeReady(t *testing.T, kubeClient kubernetes.Interface, waitTimeInSec int) error {

	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out to get K8S node to ready state")

		case <-tickChan:
			nodesList, _ := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})

			allNodesReady := true
			var nodeConditionReady v1.NodeCondition
			for _, node := range nodesList.Items {
				for _, nodeCondition := range node.Status.Conditions {
					if nodeCondition.Type == "Ready" {
						nodeConditionReady = nodeCondition
						break
					}
				}
			}
			if nodeConditionReady.Status != "True" {
				allNodesReady = false
				break
			}
			if allNodesReady {
				return nil
			}
		}
	}
}
