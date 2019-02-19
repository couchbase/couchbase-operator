package e2eutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/gocbmgr"

	"k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
)

const (
	IntMax = int(^uint(0) >> 1)
)

var retryInterval = 10 * time.Second

type acceptFunc func(*api.CouchbaseCluster) bool
type filterFunc func(*v1.Pod) bool
type filterFuncDaemonSet func(*v1beta1.DaemonSet) bool

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

func WaitUntilPodSizeReachedEtcd(t *testing.T, kubeClient kubernetes.Interface, size, retries int) ([]string, error) {
	var names []string
	err := retryutil.Retry(Context, retryInterval, retries, func() (done bool, err error) {
		podList, err := kubeClient.Core().Pods("default").List(metav1.ListOptions{LabelSelector: "app=etcd"})
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
		LogfWithTimestamp(t, "waiting size (%d), etcd pods: names (%v), nodes (%v)", size, names, nodeNames)
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
func WaitClusterPhaseFailed(t *testing.T, crClient versioned.Interface, cl *api.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := retryutil.Retry(ctx, retryInterval, IntMax, func() (bool, error) {
		cluster, err := GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
		if err != nil {
			return false, err
		}
		return cluster.Status.Phase == api.ClusterPhaseFailed, nil
	})
	return err
}

func MustWaitClusterPhaseFailed(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, timeout time.Duration) {
	if err := WaitClusterPhaseFailed(t, k8s.CRClient, cluster, timeout); err != nil {
		Die(t, err)
	}
}

func WaitClusterStatusHealthy(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	defer cancel()
	err := retryutil.Retry(ctx, retryInterval, IntMax, func() (done bool, err error) {
		cl, err := GetCouchbaseCluster(k8s.CRClient, cluster.Name, cluster.Namespace)
		if err != nil {
			LogfWithTimestamp(t, "could not get cluster: (%v)", err)
			return false, err
		}

		if cl.Status.Size != cluster.Spec.TotalSize() {
			LogfWithTimestamp(t, "Cluster nodes (%d) does not match expected nodes (%d) \n", cl.Status.Size, cluster.Spec.TotalSize())
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
			api.ClusterConditionUpgrading: {
				v1.ConditionFalse,
				"Cluster upgrading ...",
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

func MustWaitClusterStatusHealthy(t *testing.T, k8s *types.Cluster, cluster *api.CouchbaseCluster, timeout time.Duration) {
	if err := WaitClusterStatusHealthy(t, k8s, cluster, timeout); err != nil {
		Die(t, err)
	}
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
	undeletedPods, err := WaitPodsDeleted(kubeClient, namespace, 3, metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
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

// WaitUntilOperatorReady will wait until the first pod selected for couchbase-operator is ready.
func WaitUntilOperatorReady(kubecli kubernetes.Interface, namespace, label string) error {
	err := retryutil.Retry(Context, time.Second, 180, func() (bool, error) {
		podList, err := kubecli.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: label})
		if err != nil {
			return false, err
		}
		if len(podList.Items) > 0 {
			if k8sutil.IsPodReady(&podList.Items[0]) {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for pods with label=(%v) to become ready: %v", label, err)
	}
	return nil
}

func WaitUntilOperatorDeleted(kubecli kubernetes.Interface, namespace, label string) error {
	err := retryutil.Retry(Context, 10*time.Second, 6, func() (bool, error) {
		podList, err := kubecli.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: label})
		if err != nil {
			return false, err
		}
		if len(podList.Items) == 0 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for pods with label=(%v) to be deleted: %v", label, err)
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
func WaitForClusterEvent(kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, event *v1.Event, timeout time.Duration) error {
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
	timeoutChan := time.After(timeout)
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("Time out waiting for cluster event %s, %s:", event.Reason, event.Message)

		case watchEvent := <-resultChan:
			crdEvent := watchEvent.Object.(*v1.Event)
			// Watch() returns every event since the dawn of time, so ensure we
			// only return things after we started the wait.  This avoids matching
			// events that may have already occurred
			if crdEvent.LastTimestamp.Before(&now) {
				continue
			}
			if EqualEvent(event, crdEvent) {
				return nil
			}
		}
	}
}

func MustWaitForClusterEvent(t *testing.T, k8s *types.Cluster, cl *api.CouchbaseCluster, event *v1.Event, timeout time.Duration) {
	if err := WaitForClusterEvent(k8s.KubeClient, cl, event, timeout); err != nil {
		Die(t, err)
	}
}

func WaitForClusterEventsInParallel(kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, expectedEvents EventList, timeout time.Duration) (EventList, error) {
	receivedEvents := EventList{}
	eventChan := make(chan v1.Event)
	errChan := make(chan error)
	for _, event := range expectedEvents {
		// Creates go routines for each event
		go func(event v1.Event) {
			err := WaitForClusterEvent(kubeClient, cl, &event, timeout)
			eventChan <- event
			errChan <- err
		}(event)
	}

	var err error
	for _, _ = range expectedEvents {
		receivedEvents = append(receivedEvents, <-eventChan)
		if err = <-errChan; err != nil {
			break
		}
	}
	return receivedEvents, err
}

// waits until the provided condition type occurs with associated status
func WaitForListOfClusterEvents(kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, eventList EventList, maxExpectedEvents int, timeout time.Duration) (EventList, error) {
	opts := metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{Kind: api.CRDResourceKind},
	}
	occuredEvents := EventList{}
	watch, err := kubeClient.CoreV1().Events(cl.Namespace).Watch(opts)
	if err != nil {
		return occuredEvents, err
	}
	defer watch.Stop()

	now := metav1.Now()

	resultChan := watch.ResultChan()
	timeoutChan := time.After(timeout)
	for {
		select {
		case <-timeoutChan:
			return occuredEvents, fmt.Errorf("Time out waiting for %d cluster events from,\n%v \nEvents got:\n%v", maxExpectedEvents, eventList, occuredEvents)

		case watchEvent := <-resultChan:
			crdEvent := watchEvent.Object.(*v1.Event)
			// Watch() returns every event since the dawn of time, so ensure we
			// only return things after we started the wait.  This avoids matching
			// events that may have already occurred
			if crdEvent.FirstTimestamp.Before(&now) {
				continue
			}
			if EventExistsInEventList(crdEvent, eventList) {
				occuredEvents = append(occuredEvents, *crdEvent)
			}
		}
		if len(occuredEvents) == maxExpectedEvents {
			break
		}
	}
	return occuredEvents, nil
}

// waits until the provided condition type with associated status after specified timestamp
func WaitForClusterCondition(t *testing.T, crClient versioned.Interface, conditionType api.ClusterConditionType, status v1.ConditionStatus, cl *api.CouchbaseCluster, timeout time.Duration) error {
	after := time.Now()
	timeoutChan := time.After(timeout)
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("timed out waiting for condition %s with status: %s", conditionType, status)

		case <-tickChan:
			// Get current cluster condition
			cluster, err := GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
			if err != nil {
				return err
			}

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
		}
	}
}

func MustWaitForClusterCondition(t *testing.T, k8s *types.Cluster, conditionType api.ClusterConditionType, status v1.ConditionStatus, cl *api.CouchbaseCluster, timeout time.Duration) {
	if err := WaitForClusterCondition(t, k8s.CRClient, conditionType, status, cl, timeout); err != nil {
		Die(t, err)
	}
}

func nodeReady(node v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			return true
		}
	}
	return false
}

func allNodesReady(nodes []v1.Node) bool {
	for _, node := range nodes {
		if !nodeReady(node) {
			return false
		}
	}
	return true
}

func WaitForKubeNodesToBeReady(kubeClient kubernetes.Interface, requiredNodesInCluster, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out to get K8S node to ready state")

		case <-tickChan:
			nodesList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
			if err != nil {
				continue
			}
			if allNodesReady(nodesList.Items) && len(nodesList.Items) == requiredNodesInCluster {
				return nil
			}
		}
	}
}

func WaitForPodsReadyWithLabel(t *testing.T, kubeClient kubernetes.Interface, waitTimeInSec int, label string, namespace string) error {
	t.Logf("waiting for pods to be ready in namesapce %v with label %v", namespace, label)
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out waiting for pods to enter ready state")

		case <-tickChan:
			podReady := true
			podList, _ := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: label})
			for _, pod := range podList.Items {
				if pod.Status.Phase != v1.PodRunning {
					podReady = false
					break
				}
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "Ready" && condition.Status != v1.ConditionTrue {
						podReady = false
					}
				}
			}
			if podReady {
				t.Logf("pods with label %v ready \n", label)
				return nil
			}
		}
	}
}

func WaitDaemonSetsDeleted(kubecli kubernetes.Interface, namespace string, retries int, lo metav1.ListOptions) ([]*v1beta1.DaemonSet, error) {
	f := func(ds *v1beta1.DaemonSet) bool { return ds.DeletionTimestamp != nil }
	return waitDaemonSetsDeleted(kubecli, namespace, retries, lo, f)
}

func waitDaemonSetsDeleted(kubecli kubernetes.Interface, namespace string, retries int, lo metav1.ListOptions, filters ...filterFuncDaemonSet) ([]*v1beta1.DaemonSet, error) {
	var dss []*v1beta1.DaemonSet
	err := retryutil.Retry(Context, retryInterval, retries, func() (bool, error) {
		dsList, err := kubecli.ExtensionsV1beta1().DaemonSets(namespace).List(lo)
		if err != nil {
			return false, err
		}
		dss = nil
		for i := range dsList.Items {
			ds := &dsList.Items[i]
			filtered := false
			for _, filter := range filters {
				if filter(ds) {
					filtered = true
				}
			}
			if !filtered {
				dss = append(dss, ds)
			}
		}
		return len(dss) == 0, nil
	})
	return dss, err
}

func WaitForExternalLoadBalancer(t *testing.T, kubeClient kubernetes.Interface, namespace string, loadBalancerServiceName string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out to get K8S node to ready state")

		case <-tickChan:
			service, err := GetService(kubeClient, namespace, loadBalancerServiceName)
			if err != nil {
				continue
			}

			if len(service.Status.LoadBalancer.Ingress) > 0 {
				if service.Status.LoadBalancer.Ingress[0].IP != "" {
					t.Logf("load balancer ingress: %+v", service.Status.LoadBalancer.Ingress)
					return nil
				}
			}
		}
	}
}

// WaitForPVCDeletion is used as synchronization between runs, especially in the cloud
// as PVC reclaim is not instant.
func WaitForPVCDeletion(k8s *types.Cluster, namespace string, ctx context.Context) error {
	requirements := []labels.Requirement{}
	req, err := labels.NewRequirement(operator_constants.LabelApp, selection.Equals, []string{operator_constants.App})
	if err != nil {
		return err
	}
	requirements = append(requirements, *req)

	selector := labels.NewSelector()
	selector = selector.Add(requirements...)

	return retryutil.Retry(ctx, 10*time.Second, IntMax, func() (bool, error) {
		pvcs, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		if len(pvcs.Items) != 0 {
			return false, nil
		}
		return true, nil
	})
}

// DeleteAndWaitForPVCDeletion deletes all PVCs in the cluster and waits for them
// to be deleted.  If this operation fails we retry again and again until it does
// work or the context timeout triggers.
func DeleteAndWaitForPVCDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Select all Couchbase PVCs in the namespace.
	requirements := []labels.Requirement{}
	req, err := labels.NewRequirement(operator_constants.LabelApp, selection.Equals, []string{operator_constants.App})
	if err != nil {
		return err
	}
	requirements = append(requirements, *req)

	selector := labels.NewSelector()
	selector = selector.Add(requirements...)

	// Retry deletion until success or the timeout context fires.
	return retryutil.Retry(ctx, time.Second, IntMax, func() (bool, error) {
		pvcs, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Nothing to do, move along
		if len(pvcs.Items) == 0 {
			return true, nil
		}

		// Temporarily report that stuff needs to be deleted synchronously
		pvcNames := []string{}
		for _, pvc := range pvcs.Items {
			pvcNames = append(pvcNames, pvc.Name)
		}
		fmt.Println("Waiting for deletion of:", strings.Join(pvcNames, ", "))

		for _, pvc := range pvcs.Items {
			if err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, metav1.NewDeleteOptions(0)); err != nil {
				return false, retryutil.RetryOkError(err)
			}
		}

		// Wait for upto a minute for the PVCs to be deleted before we retry the deletion.
		waitContext, waitCancel := context.WithTimeout(ctx, time.Minute)
		defer waitCancel()

		if err := WaitForPVCDeletion(k8s, namespace, waitContext); err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
}

// WaitForRebalanceProgress waits until a rebalance is running and the progress is greater
// than or eual to the defined threshold.  This allows us to kill pods during a rebalance
// with greater confidence that some vbuckets have migrated to the new master.
func WaitForRebalanceProgress(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, threshold float64, timeout time.Duration) error {
	timeoutChan := time.After(timeout)

RetryLabel:
	for {
		client, cleanup := MustCreateAdminConsoleClient(t, k8s, couchbase)
		defer cleanup()

		progress := client.NewRebalanceProgress()
		defer progress.Cancel()

		for {
			select {
			case <-timeoutChan:
				return fmt.Errorf("timeout")
			case status, ok := <-progress.Status():
				if !ok {
					return fmt.Errorf("rebalance status terminated: %v", progress.Error())
				}
				switch status.Status {
				case cbmgr.RebalanceStatusUnknown:
					// Don't fail on unknown, just free up resources and try again with
					// another random pod until we get something that definitively succeeeds
					// or fails.
					cleanup()
					progress.Cancel()
					continue RetryLabel
				case cbmgr.RebalanceStatusRunning:
					if status.Progress >= threshold {
						return nil
					}
				}
			}
		}
	}
}

func MustWaitForRebalanceProgress(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, threshold float64, timeout time.Duration) {
	if err := WaitForRebalanceProgress(t, k8s, couchbase, threshold, timeout); err != nil {
		Die(t, err)
	}
}

// WaitForFirstPodContainerWaiting waits for the first pods's container to enter a waiting state
// with optional reasons to validate.
func WaitForFirstPodContainerWaiting(k8s *types.Cluster, couchbase *api.CouchbaseCluster, timeout time.Duration, reasons ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, retryInterval, IntMax, func() (bool, error) {
		pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
		if err != nil {
			return false, err
		}

		if len(pods.Items) == 0 {
			return false, nil
		}

		pod := pods.Items[0]
		if len(pod.Status.ContainerStatuses) == 0 {
			return false, retryutil.RetryOkError(fmt.Errorf("pod has no container status"))
		}

		if pod.Status.ContainerStatuses[0].State.Waiting == nil {
			return false, retryutil.RetryOkError(fmt.Errorf("pod is not waiting"))
		}

		if len(reasons) == 0 {
			return true, nil
		}

		waitReason := pod.Status.ContainerStatuses[0].State.Waiting.Reason
		for _, reason := range reasons {
			if waitReason == reason {
				return true, nil
			}
		}

		return false, retryutil.RetryOkError(fmt.Errorf("pod waiting reason is %s", waitReason))
	})
}

func MustWaitForFirstPodContainerWaiting(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, timeout time.Duration, reasons ...string) {
	if err := WaitForFirstPodContainerWaiting(k8s, couchbase, timeout, reasons...); err != nil {
		Die(t, err)
	}
}
