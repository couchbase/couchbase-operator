package e2eutil

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

var retryInterval = 10 * time.Second

// ResourceCheckFunc is a function type consumed by ResourceConstraints.
type resourceCheckFunc func(*unstructured.Unstructured, error) error

// resourceExists checks that a resource exists.
func resourceExists(resource *unstructured.Unstructured, lookupError error) error {
	if lookupError != nil {
		return fmt.Errorf("resource must exist: %w", lookupError)
	}

	return nil
}

// resourceNotExists checks that a resource does not exist.
func resourceNotExists(resource *unstructured.Unstructured, lookupError error) error {
	if lookupError == nil {
		return fmt.Errorf("resource nust not exist")
	}

	return nil
}

// resourceConditionExists checks that a resource has a condition in the correct state.
func resourceConditionExists(conditionType, conditionStatus string) resourceCheckFunc {
	return func(resource *unstructured.Unstructured, lookupError error) error {
		conditions, ok, _ := unstructured.NestedSlice(resource.Object, "status", "conditions")
		if !ok {
			return fmt.Errorf("resource has no conditions")
		}

		for _, c := range conditions {
			condition, ok := c.(map[string]interface{})
			if !ok {
				return fmt.Errorf("malformed condition %v", c)
			}

			concreteType, ok, _ := unstructured.NestedString(condition, "type")
			if !ok {
				return fmt.Errorf("condition has no type %v", condition)
			}

			if concreteType != conditionType {
				continue
			}

			concreteStatus, ok, _ := unstructured.NestedString(condition, "status")
			if !ok {
				return fmt.Errorf("condition has no status %v", condition)
			}

			if concreteStatus != conditionStatus {
				return fmt.Errorf("condition status mismatch, expected %v, got %v", conditionStatus, concreteStatus)
			}

			return nil
		}

		return fmt.Errorf("condition %v missing", conditionType)
	}
}

// resourceConditionNotExists checks that a resource does not have a condition.
func resourceConditionNotExists(conditionType string) resourceCheckFunc {
	return func(resource *unstructured.Unstructured, lookupError error) error {
		conditions, ok, _ := unstructured.NestedSlice(resource.Object, "status", "conditions")
		if !ok {
			return fmt.Errorf("resource has no conditions")
		}

		for _, c := range conditions {
			condition, ok := c.(map[string]interface{})
			if !ok {
				return fmt.Errorf("malformed condition %v", c)
			}

			concreteType, ok, _ := unstructured.NestedString(condition, "type")
			if !ok {
				return fmt.Errorf("condition has no type %v", condition)
			}

			if concreteType == conditionType {
				return fmt.Errorf("condition %s must not exist", conditionType)
			}
		}

		return nil
	}
}

// couchbaseClusterScaled checks that the requested cluster size matched the reported size.
func couchbaseClusterScaled(resource *unstructured.Unstructured, lookupError error) error {
	classes, ok, _ := unstructured.NestedSlice(resource.Object, "spec", "servers")
	if !ok {
		return fmt.Errorf("resource has no server classes")
	}

	var requested int64

	for _, c := range classes {
		class, ok := c.(map[string]interface{})
		if !ok {
			return fmt.Errorf("malformed class %v", c)
		}

		size, ok, _ := unstructured.NestedInt64(class, "size")
		if !ok {
			return fmt.Errorf("class has no size %v", class)
		}

		requested += size
	}

	actual, ok, _ := unstructured.NestedInt64(resource.Object, "status", "size")
	if !ok {
		return fmt.Errorf("status has no size")
	}

	if requested != actual {
		return fmt.Errorf("size does not match, wanted %v, got %v", requested, actual)
	}

	return nil
}

// ResourceConstraints gets a resource and then applies constraints to it, if any fail, so
// does this.
func ResourceConstraints(k8s *types.Cluster, resource runtime.Object, constraints ...resourceCheckFunc) func() error {
	return func() error {
		// Map from object to dynamic API mapping... "e.g. couchbase.com/v2/couchbaseclusters"
		mapping, err := getRESTMapping(k8s, resource)
		if err != nil {
			return err
		}

		unstructuredResource, lookupError := metaGet(k8s, mapping, resource)

		// Check all constraints return with no error.
		for _, constraint := range constraints {
			if err := constraint(unstructuredResource, lookupError); err != nil {
				return err
			}
		}

		return nil
	}
}

// ResourceDeleted checks if a given resource has been deleted.  This returns a closure
// that should be used with RetryFor().
func ResourceDeleted(k8s *types.Cluster, resource runtime.Object) func() error {
	return ResourceConstraints(k8s, resource, resourceNotExists)
}

// ResourceCondition checks if a given resource has the given condition. This returns a closure
// that should be used with RetryFor().
func ResourceCondition(k8s *types.Cluster, resource runtime.Object, conditionType, conditionStatus string) func() error {
	return ResourceConstraints(k8s, resource, resourceExists, resourceConditionExists(conditionType, conditionStatus))
}

// waitForResourceEvent watches event streams for a given resource and returns when the requested
// event has been seen.  The context allows for timeouts (everything should have one that does this),
// the ready channel, if not nil, is closed when the routine is up and running, or on error, so it can
// unblock a caller that needs to wait until this is running before continuing.
func waitForResourceEvent(ctx context.Context, ready chan struct{}, k8s *types.Cluster, resource runtime.Object, event *v1.Event, epoch time.Time) error {
	// If the ready channel is valid, and we've not closed it, do so.
	// This will occur if some error happened before the point where we
	// normally close to indicate we're watching, and unblock the caller.
	defer func() {
		if ready == nil {
			return
		}

		select {
		case <-ready:
		default:
			close(ready)
		}
	}()

	// Map from object to dynamic API mapping... "e.g. couchbase.com/v2/couchbaseclusters"
	kinds, _, err := scheme.Scheme.ObjectKinds(resource)
	if err != nil {
		return err
	}

	gvk := kinds[0]

	// Up cast the resource into a meta object so we can interrogate name and namespace.
	metaResource, ok := resource.(metav1.Object)
	if !ok {
		return fmt.Errorf("unable to convert from runtime to meta resource")
	}

	selector := map[string]string{
		"involvedObject.apiVersion": gvk.GroupVersion().String(),
		"involvedObject.kind":       gvk.Kind,
		"involvedObject.name":       metaResource.GetName(),
	}

	watch, err := k8s.KubeClient.CoreV1().Events(metaResource.GetNamespace()).Watch(context.Background(), metav1.ListOptions{FieldSelector: labels.FormatLabels(selector)})
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

	resultChan := watch.ResultChan()

	if ready != nil {
		close(ready)
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: failed to wait for event %v/%v", ctx.Err(), event.Reason, event.Message)

		case watchEvent := <-resultChan:
			ev := watchEvent.Object.(*v1.Event)
			// Watch() returns every event since the dawn of time, so ensure we
			// only return things after we started the wait.  This avoids matching
			// events that may have already occurred
			if ev.LastTimestamp.Before(&metav1.Time{Time: epoch}) {
				continue
			}

			if EqualEvent(event, ev) {
				return nil
			}
		}
	}
}

func waitForResourceEventFromNow(ctx context.Context, ready chan struct{}, k8s *types.Cluster, resource runtime.Object, event *v1.Event) error {
	return waitForResourceEvent(ctx, ready, k8s, resource, event, time.Now())
}

func waitForResourceEventEver(ctx context.Context, k8s *types.Cluster, resource runtime.Object, event *v1.Event) error {
	// Tonight we're gonna party like it's 1999... you must have royally screwed up if this
	// isn't far enough in the past to see every event!
	return waitForResourceEvent(ctx, nil, k8s, resource, event, time.Date(1999, time.December, 31, 23, 59, 59, 0, time.UTC))
}

// mustWaitForResourceEventFromNow watches event streams for a given resource and returns when the requested
// event has been seen.
func mustWaitForResourceEventFromNow(t *testing.T, k8s *types.Cluster, resource runtime.Object, event *v1.Event, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := waitForResourceEventFromNow(ctx, nil, k8s, resource, event); err != nil {
		Die(t, err)
	}
}

// WaitForBackupCreation waits for a backup resources associated with a cluster to be created.
func WaitForBackup(k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup, timeout time.Duration) error {
	callback := func() error {
		// TODO: you can check more than presence, the schedule for example can allow you to wait for updates...
		if _, err := k8s.KubeClient.BatchV1beta1().CronJobs(backup.Namespace).Get(context.Background(), backup.Name+"-full", metav1.GetOptions{}); err != nil {
			return err
		}

		if backup.Spec.Strategy != couchbasev2.FullIncremental {
			return nil
		}

		// TODO: you can check more than presence, the schedule for example can allow you to wait for updates...
		if _, err := k8s.KubeClient.BatchV1beta1().CronJobs(backup.Namespace).Get(context.Background(), backup.Name+"-incremental", metav1.GetOptions{}); err != nil {
			return err
		}

		return nil
	}

	return retryutil.RetryFor(timeout, callback)
}

func MustWaitForBackup(t *testing.T, k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup, timeout time.Duration) {
	if err := WaitForBackup(k8s, backup, timeout); err != nil {
		Die(t, err)
	}
}

func WaitForCronjob(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, name string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		listOptions := metav1.ListOptions{}
		cronjobs, err := k8s.KubeClient.BatchV1beta1().CronJobs(couchbase.Namespace).List(context.Background(), listOptions)
		if err != nil {
			return err
		}

		for _, cronjob := range cronjobs.Items {
			if strings.HasSuffix(cronjob.Name, name) {
				return nil
			}
		}

		return fmt.Errorf("no cronjob with suffix %s found", name)
	})
}

func MustWaitForCronjob(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, name string, timeout time.Duration) {
	if err := WaitForCronjob(k8s, couchbase, name, timeout); err != nil {
		Die(t, err)
	}
}

// this function waits until expected non-empty values appear in backup status fields.
func WaitForStatusUpdate(k8s *types.Cluster, backupName, statusField string, timeout time.Duration) (reflect.Value, error) {
	var statusFieldValue reflect.Value

	return statusFieldValue, retryutil.RetryFor(timeout, func() (err error) {
		backup, err := k8s.CRClient.CouchbaseV2().CouchbaseBackups(k8s.Namespace).Get(context.Background(), backupName, metav1.GetOptions{})
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

func WaitUntilBucketsExist(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, buckets []string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// First ensure the buckets are created according to the operator.
	callback := func() error {
		currCluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(context.Background(), couchbase.Name, metav1.GetOptions{})
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
	if err := retryutil.Retry(ctx, retryInterval, callback); err != nil {
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
	if err := retryutil.Retry(ctx, retryInterval, callback); err != nil {
		return err
	}

	return nil
}

func MustWaitUntilBucketsExist(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, buckets []string, timeout time.Duration) {
	if err := WaitUntilBucketsExist(k8s, couchbase, buckets, timeout); err != nil {
		Die(t, err)
	}
}

func getBucketName(bucket interface{}) (string, error) {
	switch typed := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		name := typed.Name

		if typed.Spec.Name != "" {
			name = typed.Spec.Name
		}

		return name, nil
	case *couchbasev2.CouchbaseEphemeralBucket:
		name := typed.Name

		if typed.Spec.Name != "" {
			name = typed.Spec.Name
		}

		return name, nil
	case *couchbasev2.CouchbaseMemcachedBucket:
		name := typed.Name

		if typed.Spec.Name != "" {
			name = typed.Spec.Name
		}

		return name, nil
	case string:
		return typed, nil
	}

	return "", fmt.Errorf("unhandled bucket type")
}

func WaitUntilBucketExists(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket interface{}, timeout time.Duration) error {
	name, err := getBucketName(bucket)
	if err != nil {
		return err
	}

	return WaitUntilBucketsExist(k8s, couchbase, []string{name}, timeout)
}

func MustWaitUntilBucketExists(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket interface{}, timeout time.Duration) {
	name, err := getBucketName(bucket)
	if err != nil {
		Die(t, err)
	}

	if err := WaitUntilBucketsExist(k8s, couchbase, []string{name}, timeout); err != nil {
		Die(t, err)
	}
}

func WaitUntilBucketNotExists(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		currCluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(context.Background(), couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, b := range currCluster.Status.Buckets {
			if bucket == b.BucketName {
				return fmt.Errorf("bucket %s still exists", bucket)
			}
		}

		return nil
	})
}

func MustWaitUntilBucketNotExists(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := WaitUntilBucketNotExists(k8s, couchbase, bucket, timeout); err != nil {
		Die(t, err)
	}
}

func WaitClusterStatusHealthy(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	// Bit complex, probably a strong sign that our conditions are wrong!
	// Cluster needs to be available, balanced, not scaling and not upgrading.
	constraints := []resourceCheckFunc{
		resourceExists,
		couchbaseClusterScaled,
		resourceConditionExists(string(couchbasev2.ClusterConditionAvailable), string(v1.ConditionTrue)),
		resourceConditionExists(string(couchbasev2.ClusterConditionBalanced), string(v1.ConditionTrue)),
		resourceConditionNotExists(string(couchbasev2.ClusterConditionScaling)),
		resourceConditionNotExists(string(couchbasev2.ClusterConditionUpgrading)),
		resourceConditionNotExists(string(couchbasev2.ClusterConditionError)),
	}

	return retryutil.RetryFor(timeout, ResourceConstraints(k8s, cluster, constraints...))
}

func MustWaitClusterStatusHealthy(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := WaitClusterStatusHealthy(t, k8s, cluster, timeout); err != nil {
		Die(t, err)
	}
}

// WaitUntilOperatorReady will wait until the first pod selected for couchbase-operator is ready.
func WaitUntilOperatorReady(k8s *types.Cluster, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, ResourceCondition(k8s, k8s.OperatorDeployment, "Available", "True"))
}

// MustWaitForClusterEvent waits for the specified event to be raised.
func MustWaitForClusterEvent(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, event *v1.Event, timeout time.Duration) {
	mustWaitForResourceEventFromNow(t, k8s, cluster, event, timeout)
}

// MustObserveClusterEvent differs from MustWaitForClusterEvent in that the latter waits for
// an event after the time the function was called.  The former however expects that the event
// has or will happen e.g. is less racy.  This requires that the event is unique within a test
// run as multiple events of the same type will cause this to trigger.
func MustObserveClusterEvent(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, event *v1.Event, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := waitForResourceEventEver(ctx, k8s, cluster, event); err != nil {
		Die(t, err)
	}
}

func MustWaitForBackupEvent(t *testing.T, k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup, event *v1.Event, timeout time.Duration) {
	mustWaitForResourceEventFromNow(t, k8s, backup, event, timeout)
}

// waits until the provided condition type with associated status.
func MustWaitForClusterCondition(t *testing.T, k8s *types.Cluster, conditionType couchbasev2.ClusterConditionType, status v1.ConditionStatus, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	if err := retryutil.RetryFor(timeout, ResourceCondition(k8s, cl, string(conditionType), string(status))); err != nil {
		Die(t, err)
	}
}

func fieldEqualQuantity(size *resource.Quantity, fields ...string) resourceCheckFunc {
	return func(u *unstructured.Unstructured, lookupError error) error {
		raw, ok, _ := unstructured.NestedString(u.Object, fields...)
		if !ok {
			return fmt.Errorf("no value found within resource")
		}

		quantity, err := resource.ParseQuantity(raw)
		if err != nil {
			return err
		}

		if quantity.Value() != size.Value() {
			return fmt.Errorf("quantity does not match size")
		}

		return nil
	}
}

func MustWaitForPVCSize(t *testing.T, k8s *types.Cluster, name string, size *resource.Quantity, timeout time.Duration) {
	pvc, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		Die(t, err)
	}

	if err := retryutil.RetryFor(timeout, ResourceConstraints(k8s, pvc, fieldEqualQuantity(size, "status", "capacity", "storage"))); err != nil {
		Die(t, err)
	}
}

func MustDeletePVC(t *testing.T, k8s *types.Cluster, pvc *v1.PersistentVolumeClaim, timeout time.Duration) {
	if err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).Delete(context.Background(), pvc.Name, metav1.DeleteOptions{}); err != nil {
		Die(t, err)
	}

	if err := retryutil.RetryFor(timeout, ResourceDeleted(k8s, pvc)); err != nil {
		Die(t, err)
	}
}

// WaitForRebalanceProgress waits until a rebalance is running and the progress is greater
// than or eual to the defined threshold.  This allows us to kill pods during a rebalance
// with greater confidence that some vbuckets have migrated to the new master.
func WaitForRebalanceProgress(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, threshold float64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.Retry(ctx, time.Second, func() error {
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

func WaitUntilUserExists(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, user *couchbasev2.CouchbaseUser, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		currCluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(context.Background(), couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// find user in cluster status
		_, found := couchbasev2.HasItem(user.Name, currCluster.Status.Users)
		if !found {
			return fmt.Errorf("waiting for user `%s` to be created", user.Name)
		}

		// user must also be in couchbase
		client, cleanup, err := CreateAdminConsoleClient(k8s, currCluster)
		if err != nil {
			return err
		}

		defer cleanup()

		u := &couchbaseutil.User{}
		return couchbaseutil.GetUser(user.Name, couchbaseutil.AuthDomain(user.Spec.AuthDomain), u).On(client.client, client.host)
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
	callback := func() error {
		client, cleanup, err := CreateAdminConsoleClient(k8s, couchbase)
		if err != nil {
			return err
		}

		defer cleanup()

		// we should get an error attempting to get user
		users := &couchbaseutil.UserList{}
		if err := couchbaseutil.ListUsers(users).On(client.client, client.host); err != nil {
			return err
		}

		for _, user := range *users {
			if user.ID == userName {
				return fmt.Errorf("waiting for couchbase user `%s` to be deleted", userName)
			}
		}

		return nil
	}

	return retryutil.RetryFor(timeout, callback)
}

func MustWaitForClusterUserDeletion(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, userName string, timeout time.Duration) error {
	err := WaitForClusterUserDeletion(k8s, couchbase, userName, timeout)
	if err != nil {
		Die(t, err)
	}

	return nil
}

func WaitUntilCouchbaseAutoscalerExists(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, autoscalerName string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		currCluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(context.Background(), couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// find autoscalers in cluster status
		_, found := couchbasev2.HasItem(autoscalerName, currCluster.Status.Autoscalers)
		if !found {
			return fmt.Errorf("waiting for autoscaler `%s` to be created", autoscalerName)
		}

		// get autoscaler from k8s
		_, err = k8s.CRClient.CouchbaseV2().CouchbaseAutoscalers(couchbase.Namespace).Get(context.Background(), autoscalerName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		return nil
	})
}

func MustWaitUntilCouchbaseAutoscalerExists(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, autoscalerName string, timeout time.Duration) {
	if err := WaitUntilCouchbaseAutoscalerExists(k8s, couchbase, autoscalerName, timeout); err != nil {
		Die(t, err)
	}
}

// MustWaitForCouchbaseAutoscalerDeletion waits for autoscaler to be deleted.
func MustWaitForCouchbaseAutoscalerDeletion(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, autoscalerName string, timeout time.Duration) {
	autoscaler := &couchbasev2.CouchbaseAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscalerName,
			Namespace: couchbase.Namespace,
		},
	}

	if err := retryutil.RetryFor(timeout, ResourceDeleted(k8s, autoscaler)); err != nil {
		Die(t, err)
	}
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
func (a *AsyncOperation) Run(f func(context.Context, chan struct{}) error) {
	ready := make(chan struct{})

	go func() {
		a.err <- f(a.ctx, ready)
	}()

	// All async operations must indicate that they have started to do what they
	// are meant to do (or errored), before we continue on our merry way.
	<-ready
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
func WaitForPendingClusterEvent(k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, event *v1.Event, timeout time.Duration) *AsyncOperation {
	f := func(ctx context.Context, ready chan struct{}) error {
		return waitForResourceEventFromNow(ctx, ready, k8s, cl, event)
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

func MustWaitForBackupDeletion(t *testing.T, k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup, timeout time.Duration) {
	if err := retryutil.RetryFor(timeout, ResourceDeleted(k8s, backup)); err != nil {
		Die(t, err)
	}
}

func WaitForPrometheusReady(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		listOptions := metav1.ListOptions{
			LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
		}

		pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), listOptions)
		if err != nil {
			return err
		}

		for _, pod := range pods.Items {
			found := false

			for _, status := range pod.Status.ContainerStatuses {
				if status.Name != k8sutil.MetricsContainerName {
					continue
				}

				if !status.Ready {
					return fmt.Errorf("prometheus not ready on pod %s", pod.Name)
				}

				found = true
			}

			if !found {
				return fmt.Errorf("unable to find prometheus container status in pod %s", pod.Name)
			}
		}

		return nil
	})
}

func MustWaitForPrometheusReady(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) {
	err := WaitForPrometheusReady(k8s, couchbase, timeout)
	if err != nil {
		Die(t, err)
	}
}

// MustWaitForStableResourceVersion checks the resource's resource version.  It expects this to
// remain stable for the given period.  If this fails, the risk is the DAC running all the time
// and flooding the API with requests (scaling linearly...)
func MustWaitForStableResourceVersion(t *testing.T, k8s *types.Cluster, resource runtime.Object, period, timeout time.Duration) {
	mapping, err := getRESTMapping(k8s, resource)
	if err != nil {
		Die(t, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// The outer retry loop loads up the current resource and reads out its generation...
	callback := func() error {
		resource, err := metaGet(k8s, mapping, resource)
		if err != nil {
			return err
		}

		version := resource.GetResourceVersion()

		// The inner retry loop checks that it doesn't change for the duration of the
		callback := func() error {
			resource, err := metaGet(k8s, mapping, resource)
			if err != nil {
				return err
			}

			currentVersion := resource.GetResourceVersion()

			if currentVersion != version {
				return fmt.Errorf("resource version mismatch %v vs %v", currentVersion, version)
			}

			return nil
		}

		innerCtx, cancel := context.WithTimeout(ctx, period)
		defer cancel()

		if err := retryutil.Assert(innerCtx, time.Second, callback); err != nil {
			return fmt.Errorf("resource version is unstable")
		}

		return nil
	}

	if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}
}
