package e2eutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func NewClusterBasic(t *testing.T, crClient versioned.Interface, namespace, secretName string, size int, withBucket bool) (*api.CouchbaseCluster, error) {
	crd := e2espec.NewBasicCluster("test-couchbase-", secretName, size, withBucket)
	return CreateClusterWithCRD(t, crClient, crd, namespace, secretName, size, withBucket)
}

func NewClusterExposed(t *testing.T, crClient versioned.Interface, namespace, secretName string, size int, withBucket bool) (*api.CouchbaseCluster, error) {
	crd := e2espec.NewClusterExposedSpec("test-couchbase-", secretName, size, withBucket)
	cl, err := CreateClusterWithCRD(t, crClient, crd, namespace, secretName, size, withBucket)
	if err != nil {
		return nil, err
	}
	// return with updated status so that admin console port is set
	return GetClusterCRD(crClient, cl)
}

func NewClusterMulti(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, secretName string,
	config map[string]map[string]string) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster("test-couchbase-", secretName, config)
	testCouchbase, err := CreateCluster(t, crClient, namespace, clusterSpec)
	if err != nil {
		return nil, err
	}
	_, err = WaitUntilSizeReached(t, crClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
	if err != nil {
		return nil, err
	}
	buckets := testCouchbase.Spec.BucketNames()
	if len(buckets) > 0 {
		err = WaitUntilBucketsExists(t, crClient, buckets, 18, testCouchbase)
		if err != nil {
			return nil, err
		}
	}
	return testCouchbase, nil
}

func UpdateClusterSpec(field string, value interface{}, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {

	updateFunc := func(cl *api.CouchbaseCluster) {}

	switch {
	case field == "Size":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[0].Size = ConvertToInt(value) }
	case field == "ExposeAdminConsole":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ExposeAdminConsole, _ = strconv.ParseBool(value.(string)) }
	case field == "DataServiceMemQuota":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ClusterSettings.DataServiceMemQuota = ConvertToInt(value) }
	}
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)

}

// convert interfaced value to int, note this can downcast i64
func ConvertToInt(value interface{}) int {
	switch value.(type) {
	case int, int64:
		return value.(int)
	case string:
		strInt, _ := strconv.Atoi(value.(string))
		return strInt
	}
	return 0
}

func UpdateBucketSpec(bucketName string, field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {
	bucketIndex := 0
	for i, bucket := range cl.Spec.BucketSettings {
		if bucketName == bucket.BucketName {
			bucketIndex = i
			break
		}
	}
	updateFunc := func(cl *api.CouchbaseCluster) {}
	switch {
	case field == "BucketName":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].BucketName = value }
	case field == "BucketType":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].BucketType = value }
	case field == "BucketMemoryQuota":
		updateFunc = func(cl *api.CouchbaseCluster) {
			cl.Spec.BucketSettings[bucketIndex].BucketMemoryQuota, _ = strconv.Atoi(value)
		}
	case field == "BucketReplicas":
		updateFunc = func(cl *api.CouchbaseCluster) {
			cl.Spec.BucketSettings[bucketIndex].BucketReplicas, _ = strconv.Atoi(value)
		}
	case field == "IoPriority":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].IoPriority = value }
	case field == "EvictionPolicy":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].EvictionPolicy = &value }
	case field == "ConflictResolution":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].ConflictResolution = &value }
	case field == "EnableFlush":
		updateFunc = func(cl *api.CouchbaseCluster) {
			flush, _ := strconv.ParseBool(value)
			cl.Spec.BucketSettings[bucketIndex].EnableFlush = &flush
		}
	case field == "EnableIndexReplica":
		updateFunc = func(cl *api.CouchbaseCluster) {
			enableReplicas, _ := strconv.ParseBool(value)
			cl.Spec.BucketSettings[bucketIndex].EnableIndexReplica = &enableReplicas
		}
	}
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func DestroyCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string, cluster *api.CouchbaseCluster) {
	err1 := DeleteCluster(t, crClient, kubeClient, cluster, 10)
	if err1 != nil {
		t.Fatal(err1)
	}
}

func CleanUpCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, logDir string, cluster *api.CouchbaseCluster) {
	err := WriteLogs(t, kubeClient, namespace, logDir)
	if err != nil {
		t.Logf("Error: %v", err)
	}

	err1 := DeleteCluster(t, crClient, kubeClient, cluster, 10)
	if err1 != nil {
		t.Logf("Error: %v", err1)
	}
}

func KillMembers(kubecli kubernetes.Interface, namespace string, names ...string) error {
	for _, name := range names {
		if err := KillMember(kubecli, namespace, name); err != nil {
			return err
		}
	}
	return nil
}

func KillMember(kubecli kubernetes.Interface, namespace string, name string) error {
	err := kubecli.CoreV1().Pods(namespace).Delete(name, metav1.NewDeleteOptions(0))
	if err != nil && !k8sutil.IsKubernetesResourceNotFoundError(err) {
		return err
	}
	return nil
}

func WriteLogs(t *testing.T, kubeClient kubernetes.Interface, namespace, logDir string) error {
	options := metav1.ListOptions{LabelSelector: "name=couchbase-operator"}
	pods, err := kubeClient.CoreV1().Pods(namespace).List(options)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		logOpts := &v1.PodLogOptions{}
		req := kubeClient.CoreV1().Pods(namespace).GetLogs(pod.Name, logOpts)
		data, err := req.DoRaw()
		if err != nil {
			return err
		}

		logFile := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", t.Name(), pod.Name))
		err = ioutil.WriteFile(logFile, data, 0644)
		if err != nil {
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

func KillPodForMember(kubeCli kubernetes.Interface, cl *api.CouchbaseCluster, memberId int) error {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return KillMember(kubeCli, cl.Namespace, name)
}

func CreateMemberPod(kubeCli kubernetes.Interface, m *couchbaseutil.Member, cl *api.CouchbaseCluster, clusterName, namespace string) (*v1.Pod, error) {

	for _, config := range cl.Spec.ServerSettings {
		if config.Name == m.ServerConfig {
			pod := k8sutil.CreateCouchbasePod(m, clusterName, cl.Spec, config, cl.AsOwner())
			pod, err := kubeCli.Core().Pods(namespace).Create(pod)
			if err != nil {
				return nil, err
			}
			err = k8sutil.WaitForPod(kubeCli, namespace, pod.Name)
			if err != nil {
				return nil, err
			}
			return kubeCli.Core().Pods(namespace).Get(pod.Name, metav1.GetOptions{})
		}
	}

	return nil, NewErrServerConfigNotFound(m.ServerConfig)
}
