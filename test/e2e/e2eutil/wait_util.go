package e2eutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/gocbmgr"

	"k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
)

var retryInterval = 10 * time.Second

type filterFunc func(*v1.Pod) bool
type filterFuncDaemonSet func(*v1beta1.DaemonSet) bool

func WaitUntilPodSizeReached(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, size int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, retryInterval, func() (done bool, err error) {
		podList, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(k8sutil.ClusterListOpt(couchbase.Name))
		if err != nil {
			return false, err
		}
		for _, pod := range podList.Items {
			if pod.Status.Phase != v1.PodRunning {
				return false, nil
			}
		}
		if len(podList.Items) != size {
			return false, nil
		}
		return true, nil
	})
}

func MustWaitUntilPodSizeReached(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, size int, timeout time.Duration) {
	if err := WaitUntilPodSizeReached(k8s, couchbase, size, timeout); err != nil {
		Die(t, err)
	}
}

func WaitUntilBucketsExists(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, buckets []string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// First ensure the buckets are created according to the operator.
	callback := func() error {
		currCluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, b := range buckets {
			found := false
			for _, bucket := range currCluster.Status.Buckets {
				if b == bucket.BucketName {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("bucket %s not present", b)
			}
		}
		return nil
	}
	if err := retryutil.RetryOnErr(ctx, retryInterval, callback); err != nil {
		return err
	}

	// Next wait until all nodes are warmed up before allowing tests to do I/O
	client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
	if err != nil {
		return err
	}
	defer cleanup()

	callback = func() error {
		info, err := client.ClusterInfo()
		if err != nil {
			return err
		}
		for _, node := range info.Nodes {
			if node.Status != "healthy" || node.Membership != "active" {
				return fmt.Errorf("node %s unhealthy, state %s:%s", node.HostName, node.Status, node.Membership)
			}
		}
		return nil
	}
	if err := retryutil.RetryOnErr(ctx, retryInterval, callback); err != nil {
		return err
	}

	return nil
}

func MustWaitUntilBucketsExists(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, buckets []string, timeout time.Duration) {
	if err := WaitUntilBucketsExists(k8s, couchbase, buckets, timeout); err != nil {
		Die(t, err)
	}
}

func WaitUntilBucketNotExists(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, retryInterval, func() (done bool, err error) {
		currCluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, b := range currCluster.Status.Buckets {
			if bucket == b.BucketName {
				return false, nil
			}
		}
		return true, nil
	})
}

func MustWaitUntilBucketNotExists(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := WaitUntilBucketNotExists(k8s, couchbase, bucket, timeout); err != nil {
		Die(t, err)
	}
}

// WaitClusterPhaseFailed expects the cluster to enter a failed state, useful for passing
// quickly rather than wating for a cluster to not become healthy
func WaitClusterPhaseFailed(t *testing.T, crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := retryutil.Retry(ctx, retryInterval, func() (bool, error) {
		cluster, err := GetCouchbaseCluster(crClient, cl.Name, cl.Namespace)
		if err != nil {
			return false, err
		}
		return cluster.Status.Phase == couchbasev2.ClusterPhaseFailed, nil
	})
	return err
}

func MustWaitClusterPhaseFailed(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := WaitClusterPhaseFailed(t, k8s.CRClient, cluster, timeout); err != nil {
		Die(t, err)
	}
}

func WaitClusterStatusHealthy(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		cl, err := GetCouchbaseCluster(k8s.CRClient, cluster.Name, cluster.Namespace)
		if err != nil {
			return err
		}

		if cl.Status.Size != cluster.Spec.TotalSize() {
			return fmt.Errorf("requested size %d, reported size %d", cluster.Spec.TotalSize(), cl.Status.Size)
		}

		healthyConditions := map[couchbasev2.ClusterConditionType]v1.ConditionStatus{
			couchbasev2.ClusterConditionAvailable: v1.ConditionTrue,
			couchbasev2.ClusterConditionBalanced:  v1.ConditionTrue,
			couchbasev2.ClusterConditionScaling:   v1.ConditionFalse,
			couchbasev2.ClusterConditionUpgrading: v1.ConditionFalse,
		}

		for _, cond := range cl.Status.Conditions {
			healthyCondition, ok := healthyConditions[cond.Type]
			if !ok {
				continue
			}

			if cond.Status != healthyCondition {
				return fmt.Errorf("healthy condition %v is %v", cond.Type, cond.Status)
			}
		}
		return nil
	}

	if err := retryutil.RetryOnErr(ctx, retryInterval, callback); err != nil {
		return fmt.Errorf("fail to wait for cluster status to be healthy: %v", err)
	}
	return nil
}

func MustWaitClusterStatusHealthy(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := WaitClusterStatusHealthy(t, k8s, cluster, timeout); err != nil {
		Die(t, err)
	}
}

func waitResourcesDeleted(t *testing.T, kubeClient kubernetes.Interface, cl *couchbasev2.CouchbaseCluster) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := retryutil.Retry(ctx, retryInterval, func() (done bool, err error) {
		list, err := kubeClient.CoreV1().Services(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
		if err != nil {
			return false, err
		}
		if len(list.Items) > 0 {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("fail to wait services deleted: %v", err)
	}
	return nil
}

func WaitPodDeleted(t *testing.T, kubeClient kubernetes.Interface, podName string, cl *couchbasev2.CouchbaseCluster) error {
	_, err := WaitPodsDeleted(kubeClient, cl.Namespace, k8sutil.NodeListOpt(cl.Name, podName))
	if err != nil {
		return fmt.Errorf("fail to wait pods deleted: %v", err)
	}
	return nil
}

func WaitUntilPodDeleted(kubeClient kubernetes.Interface, namespace string) error {
	_, err := WaitPodsDeleted(kubeClient, namespace, metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return fmt.Errorf("fail to wait pods deleted: %v", err)
	}
	return nil
}

func WaitPodsDeleted(kubecli kubernetes.Interface, namespace string, lo metav1.ListOptions) ([]*v1.Pod, error) {
	f := func(p *v1.Pod) bool { return p.DeletionTimestamp != nil }
	return waitPodsDeleted(kubecli, namespace, lo, f)
}

func waitPodsDeleted(kubecli kubernetes.Interface, namespace string, lo metav1.ListOptions, filters ...filterFunc) ([]*v1.Pod, error) {
	var pods []*v1.Pod

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := retryutil.Retry(ctx, retryInterval, func() (bool, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := retryutil.Retry(ctx, time.Second, func() (bool, error) {
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

// waits until the provided condition type occurs with associated status
func WaitForClusterEvent(kubeClient kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, event *v1.Event, timeout time.Duration) error {
	opts := metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{Kind: couchbasev2.ClusterCRDResourceKind},
	}
	watch, err := kubeClient.CoreV1().Events(cl.Namespace).Watch(opts)
	if err != nil {
		return err
	}
	defer func() {
		// There is a race when you call stop, but the watcher is tring to send
		// on the result channel, so drain any events to cause the routine to exit
		// cleanly.
		watch.Stop()
		for {
			if _, ok := <-watch.ResultChan(); !ok {
				break
			}
		}
	}()

	now := metav1.Now()

	resultChan := watch.ResultChan()
	timeoutChan := time.After(timeout)
	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("time out waiting for cluster event %s, %s", event.Reason, event.Message)

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

func MustWaitForClusterEvent(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, event *v1.Event, timeout time.Duration) {
	if err := WaitForClusterEvent(k8s.KubeClient, cl, event, timeout); err != nil {
		Die(t, err)
	}
}

// waits until the provided condition type with associated status after specified timestamp
func WaitForClusterCondition(t *testing.T, crClient versioned.Interface, conditionType couchbasev2.ClusterConditionType, status v1.ConditionStatus, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
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
			for _, condition := range cluster.Status.Conditions {
				if condition.Type == conditionType && condition.Status == status {
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

func MustWaitForClusterCondition(t *testing.T, k8s *types.Cluster, conditionType couchbasev2.ClusterConditionType, status v1.ConditionStatus, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) {
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
			return fmt.Errorf("timed out to get K8S node to ready state")

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
			return fmt.Errorf("timed out waiting for pods to enter ready state")

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

func WaitDaemonSetsDeleted(kubecli kubernetes.Interface, namespace string, lo metav1.ListOptions) ([]*v1beta1.DaemonSet, error) {
	f := func(ds *v1beta1.DaemonSet) bool { return ds.DeletionTimestamp != nil }
	return waitDaemonSetsDeleted(kubecli, namespace, lo, f)
}

func waitDaemonSetsDeleted(kubecli kubernetes.Interface, namespace string, lo metav1.ListOptions, filters ...filterFuncDaemonSet) ([]*v1beta1.DaemonSet, error) {
	var dss []*v1beta1.DaemonSet
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := retryutil.Retry(ctx, retryInterval, func() (bool, error) {
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
			return fmt.Errorf("timed out to get K8S node to ready state")

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

	return retryutil.Retry(ctx, 10*time.Second, func() (bool, error) {
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
	return retryutil.Retry(ctx, time.Second, func() (bool, error) {
		pvcs, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Nothing to do, move along
		if len(pvcs.Items) == 0 {
			return true, nil
		}

		// If there are any finalizers make sure they aren't there, this causes most hangs
		for _, pvc := range pvcs.Items {
			if len(pvc.Finalizers) == 0 {
				continue
			}
			pvc.Finalizers = []string{}
			if _, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).Update(&pvc); err != nil {
				return false, retryutil.RetryOkError(err)
			}
		}

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
func WaitForRebalanceProgress(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, threshold float64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 1*time.Second, func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}
		defer cleanup()

		progress := client.NewRebalanceProgress()
		defer progress.Cancel()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case status, ok := <-progress.Status():
				if !ok {
					return fmt.Errorf("rebalance status terminated: %v", progress.Error())
				}
				switch status.Status {
				case cbmgr.RebalanceStatusUnknown:
					return fmt.Errorf("rebalance status unknown")
				case cbmgr.RebalanceStatusRunning:
					if status.Progress >= threshold {
						return nil
					}
				}
			}
		}
	})
}

func MustWaitForRebalanceProgress(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, threshold float64, timeout time.Duration) {
	if err := WaitForRebalanceProgress(t, k8s, couchbase, threshold, timeout); err != nil {
		Die(t, err)
	}
}

// WaitForFirstPodContainerWaiting waits for the first pods's container to enter a waiting state
// with optional reasons to validate.
func WaitForFirstPodContainerWaiting(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, reasons ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, retryInterval, func() (bool, error) {
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

func MustWaitForFirstPodContainerWaiting(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration, reasons ...string) {
	if err := WaitForFirstPodContainerWaiting(k8s, couchbase, timeout, reasons...); err != nil {
		Die(t, err)
	}
}

func WaitForBucketDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		buckets, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(buckets.Items) != 0 {
			return fmt.Errorf("waiting for %v buckets to delete", len(buckets.Items))
		}
		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

func WaitForEphemeralBucketDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		buckets, err := k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(buckets.Items) != 0 {
			return fmt.Errorf("waiting for %v ephemeral buckets to delete", len(buckets.Items))
		}
		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

func WaitForMemcachedBucketDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		buckets, err := k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(buckets.Items) != 0 {
			return fmt.Errorf("waiting for %v memcached buckets to delete", len(buckets.Items))
		}
		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

func WaitForReplicationDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	callback := func() error {
		replications, err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(replications.Items) != 0 {
			return fmt.Errorf("waiting for %v replications to delete", len(replications.Items))
		}
		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

func WaitUntilUserExists(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, user *couchbasev2.CouchbaseUser, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 10*time.Second, func() (bool, error) {
		currCluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		// find user in cluster status
		_, found := couchbasev2.HasItem(user.Name, currCluster.Status.Users)
		if !found {
			return false, fmt.Errorf("waiting for user `%s` to be created", user.Name)
		}
		// user must also be in couchbase
		client, cleanup, err := CreateAdminConsoleClient(k8s, currCluster)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		defer cleanup()
		_, err = client.GetUser(user.Name, cbmgr.AuthDomain(user.Spec.AuthDomain))
		return err == nil, err

	})
}

func MustWaitUntilUserExists(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, user *couchbasev2.CouchbaseUser, timeout time.Duration) {
	if err := WaitUntilUserExists(k8s, couchbase, user, timeout); err != nil {
		Die(t, err)
	}
}

// WaitForClusterUserDeletion waits user to be deleted
// from couchbase cluster
func WaitForClusterUserDeletion(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, userName string, timeout time.Duration) error {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() (bool, error) {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		defer cleanup()

		// we should get an error attempting to get user
		users, err := client.GetUsers()
		if err != nil {
			return true, err
		}

		found := false
		for _, user := range users {
			if user.ID == userName {
				found = true
				break
			}
		}
		if found {
			return true, fmt.Errorf("waiting for couchbase user `%s` to be deleted", userName)
		}
		return true, nil
	}

	return retryutil.Retry(ctx, retryInterval, callback)
}

func MustWaitForClusterUserDeletion(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, userName string, timeout time.Duration) error {
	err := WaitForClusterUserDeletion(k8s, couchbase, userName, timeout)
	if err != nil {
		Die(t, err)
	}
	return nil
}

func WaitForUserDeletion(k8s *types.Cluster, namespace string, userName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		users, err := k8s.CRClient.CouchbaseV2().CouchbaseUsers(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}

		found := false
		for _, u := range users.Items {
			if u.Name == userName {
				found = true
				break
			}
		}

		if found {
			return fmt.Errorf("waiting for couchbase user `%s` to be deleted", userName)
		}
		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

func WaitForAllUserDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		users, err := k8s.CRClient.CouchbaseV2().CouchbaseUsers(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(users.Items) != 0 {
			return fmt.Errorf("waiting for %v couchbase users to delete", len(users.Items))
		}

		// and none of the users are remaining in status
		for _, user := range users.Items {
			err := WaitForUserDeletion(k8s, namespace, user.Name, timeout)
			if err != nil {
				return err
			}
		}

		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

func MustWaitForUserDeletion(t *testing.T, k8s *types.Cluster, namespace string, userName string, timeout time.Duration) {
	err := WaitForUserDeletion(k8s, namespace, userName, timeout)
	if err != nil {
		Die(t, err)
	}
}

func WaitForRoleDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		roles, err := k8s.CRClient.CouchbaseV2().CouchbaseRoles(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(roles.Items) != 0 {
			return fmt.Errorf("waiting for %v couchbase user roles to delete", len(roles.Items))
		}
		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

func WaitForRoleBindingDeletion(k8s *types.Cluster, namespace string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		bindings, err := k8s.CRClient.CouchbaseV2().CouchbaseRoleBindings(namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(bindings.Items) != 0 {
			return fmt.Errorf("waiting for %v couchbase user role bindings to delete", len(bindings.Items))
		}
		return nil
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

// WaitForCRDDeletion waits until CRD is deleted
func WaitForCRDDeletion(cs *clientset.Clientset, crdName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, 1*time.Second, func() (bool, error) {
		if _, err := cs.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crdName, metav1.GetOptions{}); err != nil {
			if k8sutil.IsKubernetesResourceNotFoundError(err) {
				// api reported crd deleted ok
				return true, nil
			}
			// crd doesn't exists, but for unknown reason
			return false, retryutil.RetryOkError(err)
		}
		// crd still exists, retry
		return false, nil
	})
}
