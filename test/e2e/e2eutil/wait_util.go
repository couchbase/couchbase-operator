package e2eutil

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"github.com/go-openapi/errors"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
)

var retryInterval = 10 * time.Second

type filterFunc func(*v1.Pod) bool

// WaitForBackupCreation waits for a backup resources associated with a cluster to be created.
func WaitForBackup(k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		// TODO: you can check more than presence, the schedule for example can allow you to wait for updates...
		if _, err := k8s.KubeClient.BatchV1beta1().CronJobs(backup.Namespace).Get(backup.Name+"-full", metav1.GetOptions{}); err != nil {
			return err
		}

		if backup.Spec.Strategy != couchbasev2.FullIncremental {
			return nil
		}

		// TODO: you can check more than presence, the schedule for example can allow you to wait for updates...
		if _, err := k8s.KubeClient.BatchV1beta1().CronJobs(backup.Namespace).Get(backup.Name+"-incremental", metav1.GetOptions{}); err != nil {
			return err
		}

		return nil
	}

	return retryutil.RetryOnErr(ctx, retryInterval, callback)
}

func MustWaitForBackup(t *testing.T, k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup, timeout time.Duration) {
	if err := WaitForBackup(k8s, backup, timeout); err != nil {
		Die(t, err)
	}
}

func WaitForCronjob(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, retryInterval, func() (done bool, err error) {
		listOptions := metav1.ListOptions{}
		cronjobs, err := k8s.KubeClient.BatchV1beta1().CronJobs(couchbase.Namespace).List(listOptions)
		if err != nil {
			return false, err
		}

		for _, cronjob := range cronjobs.Items {
			if strings.HasSuffix(cronjob.Name, name) {
				return true, nil
			}
		}

		return false, nil
	})
}

func MustWaitForCronjob(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, name string, timeout time.Duration) {
	if err := WaitForCronjob(k8s, couchbase, name, timeout); err != nil {
		Die(t, err)
	}
}

// this function waits until expected non-empty values appear in backup status fields.
func WaitForStatusUpdate(k8s *types.Cluster, backupName, statusField string, timeout time.Duration) (reflect.Value, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var statusFieldValue reflect.Value

	return statusFieldValue, retryutil.RetryOnErr(ctx, retryInterval, func() (err error) {
		backup, err := k8s.CRClient.CouchbaseV2().CouchbaseBackups(k8s.Namespace).Get(backupName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		statusFieldValue = reflect.ValueOf(backup.Status).FieldByName(statusField)
		if reflect.DeepEqual(statusFieldValue, reflect.Zero(reflect.TypeOf(statusFieldValue)).Interface()) { // nil zero value
			return fmt.Errorf("empty value panic, not found")
		}

		switch statusFieldValue.Type().Kind() {
		case reflect.String:
			if len(statusFieldValue.String()) == 0 {
				err = fmt.Errorf("string value is empty")
			}
		case reflect.Bool:
			if !statusFieldValue.Bool() {
				err = fmt.Errorf("boolean value is false")
			}
		default:
			if statusFieldValue.String() == "<*v1.Time Value>" {
				timeValueStr := fmt.Sprintf("%s", statusFieldValue.Interface())
				goTimeFmt := "2006-01-02 15:04:05 -0700 MST"

				newTime, err := time.Parse(goTimeFmt, timeValueStr)
				if err != nil {
					return err
				}

				if newTime.IsZero() {
					return fmt.Errorf("time value is the 0 value")
				}
			}

			// isValid checks that v has a value, returns false if it is the 0 value
			if !statusFieldValue.IsValid() {
				err = fmt.Errorf("value is not valid or the 0 value")
			}
		}
		return err
	})
}

func MustWaitStatusUpdate(t *testing.T, k8s *types.Cluster, backupName, statusField string, timeout time.Duration) reflect.Value {
	s, err := WaitForStatusUpdate(k8s, backupName, statusField, timeout)
	if err != nil {
		Die(t, err)
	}

	return s
}

func WaitUntilPodSizeReached(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, size int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, retryInterval, func() (done bool, err error) {
		podList, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(ClusterListOpt(couchbase))
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
		info := &couchbaseutil.ClusterInfo{}
		if err := couchbaseutil.GetPoolsDefault(info).On(client.client, client.host); err != nil {
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

func WaitClusterStatusHealthy(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		cl, err := GetCouchbaseCluster(k8s.CRClient, cluster)
		if err != nil {
			return err
		}

		if cl.Status.Size != cluster.Spec.TotalSize() {
			return fmt.Errorf("requested size %d, reported size %d", cluster.Spec.TotalSize(), cl.Status.Size)
		}

		requiredConditions := map[couchbasev2.ClusterConditionType]v1.ConditionStatus{
			couchbasev2.ClusterConditionAvailable: v1.ConditionTrue,
			couchbasev2.ClusterConditionBalanced:  v1.ConditionTrue,
		}

		optionalConditions := map[couchbasev2.ClusterConditionType]v1.ConditionStatus{
			couchbasev2.ClusterConditionScaling:   v1.ConditionFalse,
			couchbasev2.ClusterConditionUpgrading: v1.ConditionFalse,
		}

	NextCondition:
		for typ, status := range requiredConditions {
			for _, condition := range cl.Status.Conditions {
				if condition.Type == typ {
					if condition.Status == status {
						continue NextCondition
					}

					return fmt.Errorf("required condition %v is %v", typ, condition.Status)
				}
			}

			return fmt.Errorf("required condition %v not defined", typ)
		}

		for _, condition := range cl.Status.Conditions {
			status, ok := optionalConditions[condition.Type]
			if !ok {
				continue
			}

			if condition.Status != status {
				return fmt.Errorf("optional condition %v is %v", condition.Type, condition.Status)
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

func WaitPodDeleted(t *testing.T, kubeClient kubernetes.Interface, podName string, cl *couchbasev2.CouchbaseCluster) error {
	_, err := WaitPodsDeleted(kubeClient, cl.Namespace, NodeListOpt(cl, podName))
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
func WaitUntilOperatorReady(k8s *types.Cluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		deployment, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Get(k8s.OperatorDeployment.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable {
				if condition.Status == v1.ConditionTrue {
					return nil
				}

				return fmt.Errorf("operator deployment not ready")
			}
		}

		return fmt.Errorf("operator deployment missing Available condition")
	}

	return retryutil.RetryOnErr(ctx, time.Second, callback)
}

// waits until the provided condition type occurs with associated status.
func WaitForClusterEvent(ctx context.Context, kubeClient kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, event *v1.Event) error {
	opts := metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{Kind: couchbasev2.ClusterCRDResourceKind},
	}

	watch, err := kubeClient.CoreV1().Events(cl.Namespace).Watch(opts)
	if err != nil {
		return err
	}

	defer func() {
		// There is a race when you call stop, but the watcher is trying to send
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

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: failed to wait for event %v/%v", ctx.Err(), event.Reason, event.Message)

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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := WaitForClusterEvent(ctx, k8s.KubeClient, cl, event); err != nil {
		Die(t, err)
	}
}

// MustObserveClusterEvent differs from MustWaitForClusterEvent in that the latter waits for
// an event after the time the function was called.  The former however expects that the event
// has or will happen e.g. is less racy.  This requires that the event is unique within a test
// run as multiple events of the same type will cause this to trigger.
func MustObserveClusterEvent(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, event *v1.Event, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	opts := metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{Kind: couchbasev2.ClusterCRDResourceKind},
	}

	callback := func() error {
		events, err := k8s.KubeClient.CoreV1().Events(cluster.Namespace).List(opts)
		if err != nil {
			return err
		}

		for i := range events.Items {
			if EqualEvent(&events.Items[i], event) {
				return nil
			}
		}

		return fmt.Errorf("unable to locate event %v", event)
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}
}

func WaitForBackupEvent(kubeClient kubernetes.Interface, b *couchbasev2.CouchbaseBackup, event *v1.Event, timeout time.Duration) error {
	opts := metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{Kind: couchbasev2.BackupCRDResourceKind},
	}

	watch, err := kubeClient.CoreV1().Events(b.Namespace).Watch(opts)
	if err != nil {
		return err
	}

	defer func() {
		// There is a race when you call stop, but the watcher is trying to send
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
			return fmt.Errorf("time out waiting for backup event %s, %s", event.Reason, event.Message)

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

func MustWaitForBackupEvent(t *testing.T, k8s *types.Cluster, b *couchbasev2.CouchbaseBackup, event *v1.Event, timeout time.Duration) {
	if err := WaitForBackupEvent(k8s.KubeClient, b, event, timeout); err != nil {
		Die(t, err)
	}
}

// waits until the provided condition type with associated status after specified timestamp.
func WaitForClusterCondition(t *testing.T, crClient versioned.Interface, conditionType couchbasev2.ClusterConditionType, status v1.ConditionStatus, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		cluster, err := GetCouchbaseCluster(crClient, cl)
		if err != nil {
			return err
		}

		// compare cluster conditions to desired condition
		for _, condition := range cluster.Status.Conditions {
			if condition.Type == conditionType {
				if condition.Status == status {
					return nil
				}

				return fmt.Errorf("condition exists, expected status %v, got %v", status, condition.Status)
			}
		}

		return fmt.Errorf("condition does not exist")
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		return err
	}

	return nil
}

func MustWaitForClusterCondition(t *testing.T, k8s *types.Cluster, conditionType couchbasev2.ClusterConditionType, status v1.ConditionStatus, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := WaitForClusterCondition(t, k8s.CRClient, conditionType, status, cl, timeout); err != nil {
		Die(t, err)
	}
}

func WaitForPVC(k8s *types.Cluster, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		_, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).Get(name, metav1.GetOptions{})
		return err
	}

	return retryutil.RetryOnErr(ctx, retryInterval, callback)
}

func MustWaitForPVC(t *testing.T, k8s *types.Cluster, name string, timeout time.Duration) {
	if err := WaitForPVC(k8s, name, timeout); err != nil {
		Die(t, err)
	}
}

// WaitForPVCDeletion is used as synchronization between runs, especially in the cloud
// as PVC reclaim is not instant.
func WaitForPVCDeletion(ctx context.Context, k8s *types.Cluster) error {
	requirements := []labels.Requirement{}

	req, err := labels.NewRequirement(operator_constants.LabelApp, selection.Equals, []string{operator_constants.App})
	if err != nil {
		return err
	}

	requirements = append(requirements, *req)

	selector := labels.NewSelector()
	selector = selector.Add(requirements...)

	return retryutil.Retry(ctx, 10*time.Second, func() (bool, error) {
		pvcs, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		if len(pvcs.Items) != 0 {
			return false, nil
		}

		return true, nil
	})
}

// DeleteAndWaitForPVCDeletionSingle deletes a specific PVC in the cluster and waits for it
// to be deleted.  If this operation fails we retry again and again until it does
// work or the context timeout triggers.
func DeleteAndWaitForPVCDeletionSingle(k8s *types.Cluster, pvcName string, timeout time.Duration) error {
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
		pvcs, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		found := false

		// If there are any finalizers make sure they aren't there, this causes most hangs
		for i := range pvcs.Items {
			pvc := pvcs.Items[i]

			if len(pvc.Finalizers) == 0 {
				continue
			}

			pvc.Finalizers = []string{}

			if _, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).Update(&pvc); err != nil {
				return false, retryutil.RetryOkError(err)
			}

			if pvc.Name == pvcName {
				found = true
			}
		}

		if !found {
			return true, nil
		}

		if err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).Delete(pvcName, metav1.NewDeleteOptions(0)); err != nil {
			return false, retryutil.RetryOkError(err)
		}

		// Wait for upto a minute for the PVCs to be deleted before we retry the deletion.
		waitContext, waitCancel := context.WithTimeout(ctx, time.Minute)
		defer waitCancel()

		if err := WaitForPVCDeletion(waitContext, k8s); err != nil {
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

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				tasks := &couchbaseutil.TaskList{}
				if err := couchbaseutil.ListTasks(tasks).On(client.client, client.host); err != nil {
					return err
				}

				task, err := tasks.GetTask(couchbaseutil.TaskTypeRebalance)
				if err != nil {
					return err
				}

				if task.Status != "running" {
					return fmt.Errorf("rebalance status %s", task.Status)
				}

				if task.Progress < threshold {
					return fmt.Errorf("rebalance %.2f%% waiting for %.2f%%", task.Progress, threshold)
				}

				return nil
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

		u := &couchbaseutil.User{}
		err = couchbaseutil.GetUser(user.Name, couchbaseutil.AuthDomain(user.Spec.AuthDomain), u).On(client.client, client.host)

		return err == nil, err
	})
}

func MustWaitUntilUserExists(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, user *couchbasev2.CouchbaseUser, timeout time.Duration) {
	if err := WaitUntilUserExists(k8s, couchbase, user, timeout); err != nil {
		Die(t, err)
	}
}

// WaitForClusterUserDeletion waits user to be deleted
// from couchbase cluster.
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
		users := &couchbaseutil.UserList{}
		if err := couchbaseutil.ListUsers(users).On(client.client, client.host); err != nil {
			return true, err
		}

		found := false

		for _, user := range *users {
			if user.ID == userName {
				found = true
				break
			}
		}

		if found {
			err := fmt.Errorf("waiting for couchbase user `%s` to be deleted", userName)
			return false, retryutil.RetryOkError(err)
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

// WaitForCRDDeletion waits until CRD is deleted.
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

// AsyncOperation wraps up an asynchronous operation, in a generic way.
type AsyncOperation struct {
	// ctx is a cancellable context e.g. must be initialized with either
	// WithCancel or WithTimeout.
	ctx context.Context

	// cancel allows the operation to be cancelled.
	cancel func()

	// err is the channel used to propagate errors from the operation.
	err chan error
}

// NewAsyncOperationWithTimeout creates an asynchronous operation with a
// finite life span.
func NewAsyncOperationWithTimeout(timeout time.Duration) *AsyncOperation {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	op := &AsyncOperation{
		ctx:    ctx,
		cancel: cancel,
		err:    make(chan error),
	}

	return op
}

// Run executes the specified function with the operation's cancellable context.
func (a *AsyncOperation) Run(f func(context.Context) error) {
	go func() {
		a.err <- f(a.ctx)
	}()
}

// WaitForCompletion blocks waiting for the operation to complete.
// Once finished it will close the error channel.
func (a *AsyncOperation) WaitForCompletion() error {
	err := <-a.err

	close(a.err)

	return err
}

// Cancel must be defered at the call site to shutdown the operation in
// all circumstances.
func (a *AsyncOperation) Cancel() {
	// Stop the operation.
	a.cancel()

	// If the error channel has been closed, then do nothing.  If it has not
	// then wait for the operation to shutdown, consume from the channel to
	// unblock the sender, and close the channel.
	if _, ok := <-a.err; ok {
		close(a.err)
	}
}

// WaitForPendingClusterEvent returns a channel to be
// populated with result of a pending cluster event.
func WaitForPendingClusterEvent(kubeClient kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, event *v1.Event, timeout time.Duration) *AsyncOperation {
	f := func(ctx context.Context) error {
		return WaitForClusterEvent(ctx, kubeClient, cl, event)
	}

	op := NewAsyncOperationWithTimeout(timeout)

	op.Run(f)

	return op
}

// MustReceiveErrorValue requires error from channel is nil.
func MustReceiveErrorValue(t *testing.T, op *AsyncOperation) {
	if err := op.WaitForCompletion(); err != nil {
		Die(t, err)
	}
}

func WaitForBackupDeletion(k8s *types.Cluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		backups, err := k8s.CRClient.CouchbaseV2().CouchbaseBackups(k8s.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return err
		}

		if len(backups.Items) != 0 {
			return fmt.Errorf("waiting for %v backups to delete", len(backups.Items))
		}

		return nil
	}

	return retryutil.RetryOnErr(ctx, retryInterval, callback)
}

func MustWaitForBackupDeletion(t *testing.T, k8s *types.Cluster, timeout time.Duration) {
	if err := WaitForBackupDeletion(k8s, timeout); err != nil {
		Die(t, err)
	}
}

func WaitForPrometheusReady(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, retryInterval, func() (bool, error) {
		listOptions := &metav1.ListOptions{
			LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
		}

		pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(*listOptions)
		if err != nil {
			return false, err
		}

		ready := map[string]bool{}

		for _, pod := range pods.Items {
			for _, status := range pod.Status.ContainerStatuses {
				if status.Name == k8sutil.MetricsContainerName && status.Image == couchbase.Spec.Monitoring.Prometheus.Image {
					ready[pod.Name] = status.Ready
				}
			}
		}

		errs := []error{}

		for s, b := range ready {
			if !b {
				errs = append(errs, fmt.Errorf("prometheus container belonging to pod %s is not ready", s))
			}
		}

		if len(errs) > 0 {
			return false, errors.CompositeValidationError(errs...)
		}

		return true, nil
	})
}

func MustWaitForPrometheusReady(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	err := WaitForPrometheusReady(k8s, couchbase, timeout)
	if err != nil {
		Die(t, err)
	}
}
