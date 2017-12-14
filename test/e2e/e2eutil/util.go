package e2eutil

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func NewClusterBasic(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, size int, withBucket bool) (*api.CouchbaseCluster, *v1.Secret, error) {
	secret, err := CreateSecret(t, kubeClient, namespace, e2espec.NewBasicSecret(namespace))
	if err != nil {
		return nil, nil, err
	}
	testCouchbase, err := CreateCluster(t, crClient, namespace, e2espec.NewBasicCluster("test-couchbase-", secret.Name, size, withBucket))
	if err != nil {
		return nil, nil, err
	}
	_, err = WaitUntilSizeReached(t, crClient, size, 18, testCouchbase)
	if err != nil {
		return nil, nil, err
	}
	if withBucket == true {
		err = WaitUntilBucketsExists(t, crClient, []string{"default"}, 18, testCouchbase)
		if err != nil {
			return nil, nil, err
		}
	}
	return testCouchbase, secret, nil
}

func NewClusterMulti(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string,
	config map[string]map[string]string) (*api.CouchbaseCluster, *v1.Secret, error) {
	secret, err := CreateSecret(t, kubeClient, namespace, e2espec.NewBasicSecret(namespace))
	if err != nil {
		return nil, nil, err
	}
	clusterSpec := e2espec.NewMultiCluster("test-couchbase-", secret.Name, config)
	testCouchbase, err := CreateCluster(t, crClient, namespace, clusterSpec)
	if err != nil {
		return nil, nil, err
	}
	_, err = WaitUntilSizeReached(t, crClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		return nil, nil, err
	}
	buckets := testCouchbase.Spec.BucketNames()
	if len(buckets) > 0 {
		err = WaitUntilBucketsExists(t, crClient, buckets, 18, testCouchbase)
		if err != nil {
			return nil, nil, err
		}
	}
	return testCouchbase, secret, nil
}

func BucketsFromSpec(clusterSpec *api.CouchbaseCluster) []string {
	bucketNames := []string{}
	bucketSettings := clusterSpec.Spec.BucketSettings
	for _, setting := range bucketSettings {
		bucketNames = append(bucketNames, setting.BucketName)
	}
	return bucketNames
}

func UpdateClusterSpec(field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {

	updateFunc := func(cl *api.CouchbaseCluster) {}

	switch {
	case field == "Size":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[0].Size, _ = strconv.Atoi(value) }
	}

	return UpdateCluster(crClient, cl, maxRetries, updateFunc)

}

func UpdateBucketSpec(field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {

	updateFunc := func(cl *api.CouchbaseCluster) {}
	switch {
	case field == "BucketName":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[0].BucketName = value }
	case field == "BucketType":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[0].BucketType = value }
	case field == "BucketMemoryQuota":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[0].BucketMemoryQuota, _ = strconv.Atoi(value) }
	case field == "BucketReplicas":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[0].BucketReplicas, _ = strconv.Atoi(value) }
	case field == "IoPriority":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[0].IoPriority = value }
	case field == "EvictionPolicy":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[0].EvictionPolicy = &value }
	case field == "ConflictResolution":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[0].ConflictResolution = &value }
	case field == "EnableFlush":
		updateFunc = func(cl *api.CouchbaseCluster) {
			flush, _ := strconv.ParseBool(value)
			cl.Spec.BucketSettings[0].EnableFlush = &flush
		}
	case field == "EnableIndexReplica":
		updateFunc = func(cl *api.CouchbaseCluster) {
			enableReplicas, _ := strconv.ParseBool(value)
			cl.Spec.BucketSettings[0].EnableIndexReplica = &enableReplicas
		}
	}
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func DestroyCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cluster *api.CouchbaseCluster, secret *v1.Secret) {
	err1 := DeleteCluster(t, crClient, kubeClient, cluster, 10)
	if err1 != nil {
		t.Fatal(err1)
	}
	err2 := DeleteSecret(t, kubeClient, namespace, secret.Name, nil)
	if err2 != nil {
		t.Fatal(err2)
	}
}

func CleanUpCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cluster *api.CouchbaseCluster, secret *v1.Secret) {
	err1 := DeleteCluster(t, crClient, kubeClient, cluster, 10)
	if err1 != nil {
		t.Logf("Error: %v", err1)
	}
	err2 := DeleteSecret(t, kubeClient, namespace, secret.Name, nil)
	if err2 != nil {
		t.Logf("Error: %v", err2)
	}
}

func KillMembers(kubecli kubernetes.Interface, namespace string, names ...string) error {
	for _, name := range names {
		err := kubecli.CoreV1().Pods(namespace).Delete(name, metav1.NewDeleteOptions(0))
		if err != nil && !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

func LogfWithTimestamp(t *testing.T, format string, args ...interface{}) {
	t.Log(time.Now(), fmt.Sprintf(format, args...))
}

func printContainerStatus(buf *bytes.Buffer, ss []v1.ContainerStatus) {
	for _, s := range ss {
		if s.State.Waiting != nil {
			buf.WriteString(fmt.Sprintf("%s: Waiting: message (%s) reason (%s)\n", s.Name, s.State.Waiting.Message, s.State.Waiting.Reason))
		}
		if s.State.Terminated != nil {
			buf.WriteString(fmt.Sprintf("%s: Terminated: message (%s) reason (%s)\n", s.Name, s.State.Terminated.Message, s.State.Terminated.Reason))
		}
	}
}

func ResizeCluster(t *testing.T, clusterSize int, crClient versioned.Interface, cl *api.CouchbaseCluster) error {
	t.Logf("Changing Cluster Size To: %v...\n", strconv.Itoa(clusterSize))
	_, err := UpdateClusterSpec("Size", strconv.Itoa(clusterSize), crClient, cl, 10)
	if err != nil {
		return err
	}
	t.Logf("Waiting For Cluster Size To Be: %v...\n", strconv.Itoa(clusterSize))
	names, err := WaitUntilSizeReached(t, crClient, clusterSize, 30, cl)
	if err != nil {
		return err
	}
	t.Logf("Resize Success: %v...\n", names)
	return nil
}

func KillPodsAndWaitForRecovery(t *testing.T, kubeCli kubernetes.Interface, cl *api.CouchbaseCluster, numToKill int) {
	pods, err := kubeCli.CoreV1().Pods(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
	if err != nil {
		t.Fatalf("Error getting pods in cluster: %v", err)
	}

	if numToKill > len(pods.Items) {
		t.Fatalf("Trying to kill %d pods, but only %d exist")
	}

	killPods := []string{}
	for i := 0; i < numToKill; i++ {
		killPods = append(killPods, pods.Items[i].Name)
	}
	t.Logf("Killing pods: %v", killPods)

	KillMembers(kubeCli, cl.Namespace, killPods...)

	for _, pod := range killPods {
		WaitPodDeleted(t, kubeCli, pod, cl)
	}

	_, err = WaitUntilPodSizeReached(t, kubeCli, len(pods.Items), 20, cl)
	if err != nil {
		t.Fatalf("Failed to recover all nodes: %v", err)
	}
}
