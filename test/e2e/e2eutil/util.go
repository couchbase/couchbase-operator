package e2eutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

var (
	Size1 = 1
	Size2 = 2
	Size3 = 3
	Size4 = 4
	Size5 = 5
)

var (
	WithBucket    = true
	WithoutBucket = false
	AdminExposed  = true
	AdminHidden   = false
)

var (
	Retries5   = 5
	Retries10  = 10
	Retries20  = 20
	Retries30  = 30
	Retries120 = 120
)

var (
	BasicClusterConfig = map[string]string{
		"dataServiceMemQuota":   "256",
		"indexServiceMemQuota":  "256",
		"searchServiceMemQuota": "256",
		"indexStorageSetting":   "memory_optimized",
		"autoFailoverTimeout":   "10"}

	BasicClusterConfig2 = map[string]string{
		"dataServiceMemQuota":   "1024",
		"indexServiceMemQuota":  "256",
		"searchServiceMemQuota": "256",
		"indexStorageSetting":   "memory_optimized",
		"autoFailoverTimeout":   "10"}

	BasicServiceOneDataNode = map[string]string{
		"size":      "1",
		"name":      "test_config_1",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicServiceOneDataN1qlIndex = map[string]string{
		"size":      "1",
		"name":      "test_config_1",
		"services":  "data,n1ql,index",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicServiceOneN1qlIndexSearch = map[string]string{
		"size":      "1",
		"name":      "test_config_1",
		"services":  "n1ql,index,fts",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicSecondaryServiceOneData = map[string]string{
		"size":      "1",
		"name":      "test_config_2",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicServiceThreeDataN1qlIndex = map[string]string{
		"size":      "3",
		"name":      "test_config_1",
		"services":  "data,n1ql,index",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicServiceThreeDataNode = map[string]string{
		"size":      "3",
		"name":      "test_config_1",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicServiceFourDataNode = map[string]string{
		"size":      "4",
		"name":      "test_config_1",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicServiceFiveDataN1qlIndex = map[string]string{
		"size":      "5",
		"name":      "test_config_1",
		"services":  "data,n1ql,index",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}

	BasicOneReplicaBucket = map[string]string{
		"bucketName":         "default",
		"bucketType":         "couchbase",
		"bucketMemoryQuota":  "100",
		"bucketReplicas":     "1",
		"ioPriority":         "high",
		"evictionPolicy":     "fullEviction",
		"conflictResolution": "seqno",
		"enableFlush":        "true",
		"enableIndexReplica": "false"}

	BasicTwoReplicaBucket = map[string]string{
		"bucketName":         "default",
		"bucketType":         "couchbase",
		"bucketMemoryQuota":  "100",
		"bucketReplicas":     "2",
		"ioPriority":         "high",
		"evictionPolicy":     "fullEviction",
		"conflictResolution": "seqno",
		"enableFlush":        "true",
		"enableIndexReplica": "false"}
)

func NewClusterBasicQuick(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, secretName string, size int, withBucket bool, exposed bool, sizeChecks int, bucketChecks int) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster("test-couchbase-", secretName, size, withBucket, exposed)
	testCouchbase, err1 := CreateCluster(t, crClient, namespace, clusterSpec)
	if err1 != nil {
		return testCouchbase, err1
	}
	_, err2 := WaitUntilSizeReached(t, crClient, testCouchbase.Spec.TotalSize(), sizeChecks, testCouchbase)
	if err2 != nil {
		return testCouchbase, err2
	}
	buckets := testCouchbase.Spec.BucketNames()
	if withBucket == true {
		err := WaitUntilBucketsExists(t, crClient, buckets, bucketChecks, testCouchbase)
		if err != nil {
			return testCouchbase, err
		}
	}
	return GetClusterCRD(crClient, testCouchbase)
}

func NewClusterMultiQuick(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, secretName string,
	config map[string]map[string]string, exposed bool, sizeChecks int, bucketChecks int) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster("test-couchbase-", secretName, config, exposed)
	testCouchbase, err1 := CreateCluster(t, crClient, namespace, clusterSpec)
	if err1 != nil {
		return testCouchbase, err1
	}
	_, err2 := WaitUntilSizeReached(t, crClient, testCouchbase.Spec.TotalSize(), sizeChecks, testCouchbase)
	if err2 != nil {
		return testCouchbase, err2
	}
	buckets := testCouchbase.Spec.BucketNames()
	if len(buckets) > 0 {
		err := WaitUntilBucketsExists(t, crClient, buckets, bucketChecks, testCouchbase)
		if err != nil {
			return testCouchbase, err
		}
	}
	return GetClusterCRD(crClient, testCouchbase)
}

func NewClusterBasic(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, secretName string, size int, withBucket bool, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewBasicCluster("test-couchbase-", secretName, size, withBucket, exposed)

	return newClusterFromSpec(t, kubeClient, crClient, clusterSpec, namespace, secretName, size, withBucket, exposed)
}

func NewStatefulCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, secretName string, size int, withBucket bool, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewStatefulCluster("test-couchbase-", secretName, size, withBucket, exposed)
	return newClusterFromSpec(t, kubeClient, crClient, clusterSpec, namespace, secretName, size, withBucket, exposed)
}

func newClusterFromSpec(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, clusterSpec *api.CouchbaseCluster, namespace, secretName string, size int, withBucket bool, exposed bool) (*api.CouchbaseCluster, error) {
	retries := 2
	for i := 0; i < retries; i++ {
		time.Sleep(10 * time.Second)
		testCouchbase, err1 := CreateCluster(t, crClient, namespace, clusterSpec)
		if err1 != nil {
			crClient.CouchbaseV1beta1().CouchbaseClusters(namespace).Delete(testCouchbase.Name, nil)
			if i == retries-1 {
				return nil, err1
			}
		} else {
			_, err2 := WaitUntilSizeReached(t, crClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
			if err2 != nil {
				DeleteCluster(t, crClient, kubeClient, testCouchbase, 5)
				if i == retries-1 {
					return nil, err2
				}
			} else {
				buckets := testCouchbase.Spec.BucketNames()
				if withBucket == true {
					err := WaitUntilBucketsExists(t, crClient, buckets, 18, testCouchbase)
					if err != nil {
						DeleteCluster(t, crClient, kubeClient, testCouchbase, 5)
						if i == retries-1 {
							return nil, err
						}
					} else {
						return GetClusterCRD(crClient, testCouchbase)
					}
				} else {
					return GetClusterCRD(crClient, testCouchbase)
				}
			}
		}
	}
	return nil, fmt.Errorf("failed to create cluster")
}

func NewClusterMulti(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, secretName string,
	config map[string]map[string]string, exposed bool) (*api.CouchbaseCluster, error) {
	clusterSpec := e2espec.NewMultiCluster("test-couchbase-", secretName, config, exposed)
	retries := 2
	for i := 0; i < retries; i++ {
		time.Sleep(10 * time.Second)
		testCouchbase, err1 := CreateCluster(t, crClient, namespace, clusterSpec)
		if err1 != nil {
			crClient.CouchbaseV1beta1().CouchbaseClusters(namespace).Delete(testCouchbase.Name, nil)
			if i == retries-1 {
				return nil, err1
			}
		} else {
			_, err2 := WaitUntilSizeReached(t, crClient, testCouchbase.Spec.TotalSize(), 18, testCouchbase)
			if err2 != nil {
				DeleteCluster(t, crClient, kubeClient, testCouchbase, 5)
				if i == retries-1 {
					return nil, err2
				}
			} else {
				buckets := testCouchbase.Spec.BucketNames()
				if len(buckets) > 0 {
					err := WaitUntilBucketsExists(t, crClient, buckets, 18, testCouchbase)
					if err != nil {
						DeleteCluster(t, crClient, kubeClient, testCouchbase, 5)
						if i == retries-1 {
							return nil, err
						}
					} else {
						return GetClusterCRD(crClient, testCouchbase)
					}
				} else {
					return GetClusterCRD(crClient, testCouchbase)
				}
			}
		}
	}
	return nil, fmt.Errorf("failed to create cluster")
}

func UpdateClusterSpec(field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {

	updateFunc := func(cl *api.CouchbaseCluster) {}

	switch {
	case field == "Paused":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.Paused, _ = strconv.ParseBool(value) }
	case field == "ExposeAdminConsole":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ExposeAdminConsole, _ = strconv.ParseBool(value) }
	}

	return UpdateCluster(crClient, cl, maxRetries, updateFunc)

}

func UpdateClusterSettings(field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {

	updateFunc := func(cl *api.CouchbaseCluster) {}

	switch {
	case field == "DataServiceMemQuota":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ClusterSettings.DataServiceMemQuota, _ = strconv.Atoi(value) }
	case field == "IndexServiceMemQuota":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ClusterSettings.IndexServiceMemQuota, _ = strconv.Atoi(value) }
	case field == "SearchServiceMemQuota":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ClusterSettings.SearchServiceMemQuota, _ = strconv.Atoi(value) }
	case field == "IndexStorageSetting":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ClusterSettings.IndexStorageSetting = value }
	case field == "AutoFailoverTimeout":
		updateFunc = func(cl *api.CouchbaseCluster) {
			newTimeout, _ := strconv.Atoi(value)
			cl.Spec.ClusterSettings.AutoFailoverTimeout = uint64(newTimeout)
		}
	}

	return UpdateCluster(crClient, cl, maxRetries, updateFunc)

}

func UpdateServiceSpec(service int, field string, value string, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int) (*api.CouchbaseCluster, error) {

	updateFunc := func(cl *api.CouchbaseCluster) {}

	switch {
	case field == "Size":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[0].Size = ConvertToInt(value) }
	case field == "Name":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[service].Name = value }
	case field == "Services":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[service].Services = strings.Split(value, ",") }
	case field == "DataPath":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[service].DataPath = value }
	case field == "IndexPath":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings[service].IndexPath = value }
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
func AddServices(crClient versioned.Interface, cl *api.CouchbaseCluster, newService api.ServerConfig, maxRetries int) (*api.CouchbaseCluster, error) {
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings = append(cl.Spec.ServerSettings, newService) }
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func RemoveServices(crClient versioned.Interface, cl *api.CouchbaseCluster, removeServiceName string, maxRetries int) (*api.CouchbaseCluster, error) {
	newServiceConfig := []api.ServerConfig{}
	for _, service := range cl.Spec.ServerSettings {
		if service.Name != removeServiceName {
			newServiceConfig = append(newServiceConfig, service)
		}
	}
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings = newServiceConfig }
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
}

func ScaleServices(crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int, servicesMap map[string]int) (*api.CouchbaseCluster, error) {
	newServiceConfig := []api.ServerConfig{}
	for _, service := range cl.Spec.ServerSettings {
		for serviceName, size := range servicesMap {
			if serviceName == service.Name {
				service.Size = size
			}
		}
		newServiceConfig = append(newServiceConfig, service)
	}
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.ServerSettings = newServiceConfig }
	return UpdateCluster(crClient, cl, maxRetries, updateFunc)
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
			replicas, _ := strconv.Atoi(value)
			cl.Spec.BucketSettings[bucketIndex].BucketReplicas = &replicas
		}
	case field == "IoPriority":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].IoPriority = &value }
	case field == "EvictionPolicy":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].EvictionPolicy = &value }
	case field == "ConflictResolution":
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings[bucketIndex].ConflictResolution = &value }
	case field == "EnableFlush":
		updateFunc = func(cl *api.CouchbaseCluster) {
			flush, _ := strconv.ParseBool(value)
			cl.Spec.BucketSettings[bucketIndex].EnableFlush = flush
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

func CleanUpCluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace, logDir string) {
	err := WriteLogs(t, kubeClient, namespace, logDir)
	if err != nil {
		t.Logf("Error: %v", err)
	}
	CleanK8Cluster(t, kubeClient, crClient, namespace)

}

func CleanK8Cluster(t *testing.T, kubeClient kubernetes.Interface, crClient versioned.Interface, namespace string) {
	services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	for _, service := range services.Items {
		kubeClient.CoreV1().Services(namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
	}

	clusters, err := crClient.CouchbaseV1beta1().CouchbaseClusters(namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Logf("Error: %v", err)
	}
	clusterNameList := []string{}
	for _, cluster := range clusters.Items {
		clusterNameList = append(clusterNameList, cluster.Name)
	}

	t.Logf("Deleteing clusters: [%v]", clusterNameList)
	for _, cluster := range clusters.Items {
		t.Logf("Attempting to delete: [%v]", cluster.Name)
		err := k8sutil.DeleteCouchbaseCluster(crClient, &cluster)
		if err != nil {
			t.Logf("Error: %v", err)
		} else {
			t.Logf("Successfully deleted: [%v]", cluster.Name)
		}

	}
	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	killPods := []string{}
	for _, pod := range pods.Items {
		killPods = append(killPods, pod.Name)
	}
	t.Logf("Killing pods: %v", killPods)

	KillMembers(kubeClient, namespace, killPods...)

	for _, pod := range killPods {
		t.Logf("Waiting for deletion of pod: %v", pod)
	}
	WaitUntilPodDeleted(t, kubeClient, namespace)
}

func KillMembers(kubecli kubernetes.Interface, namespace string, names ...string) error {
	for _, name := range names {
		if err := KillMember(kubecli, namespace, name); err != nil {
			return err
		}
	}
	return nil
}

func KillMember(kubecli kubernetes.Interface, namespace, name string) error {
	return k8sutil.DeleteCouchbasePod(kubecli, namespace, name, metav1.NewDeleteOptions(0))
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

		testName := t.Name()
		if strings.Contains(testName, "/") {
			testName = strings.Split(t.Name(), "/")[1]
		}

		logFile := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", testName, pod.Name))
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

func isMasterNode(nodeMap map[string]string) bool {
	_, isK8SMaster := nodeMap["node-role.kubernetes.io/master"]
	_, isOpenshiftMaster := nodeMap["openshift-infra"]
	if isK8SMaster || isOpenshiftMaster {
		return true
	}
	return false
}

func ResizeCluster(t *testing.T, service int, clusterSize int, crClient versioned.Interface, cl *api.CouchbaseCluster) error {
	t.Logf("Changing Cluster Size To: %v...\n", strconv.Itoa(clusterSize))
	_, err := UpdateServiceSpec(service, "Size", strconv.Itoa(clusterSize), crClient, cl, 10)
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

func KillPods(t *testing.T, kubeCli kubernetes.Interface, cl *api.CouchbaseCluster, numToKill int) {
	pods, err := kubeCli.CoreV1().Pods(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
	if err != nil {
		t.Fatalf("Error getting pods in cluster: %v", err)
	}

	items := len(pods.Items)
	if numToKill > items {
		t.Fatalf("Trying to kill %d pods, but only %d exist", numToKill, items)
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
}

func KillPodsAndWaitForRecovery(t *testing.T, kubeCli kubernetes.Interface, cl *api.CouchbaseCluster, numToKill int) {
	pods, err := kubeCli.CoreV1().Pods(cl.Namespace).List(k8sutil.ClusterListOpt(cl.Name))
	KillPods(t, kubeCli, cl, numToKill)
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
			pod, err := k8sutil.CreateCouchbasePod(kubeCli, namespace, clusterName, m, cl.Spec, cl.Status.CurrentVersion, config, cl.AsOwner())
			if err != nil {
				return nil, err
			}
			pod, err = kubeCli.Core().Pods(namespace).Create(pod)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			err = k8sutil.WaitForPod(ctx, kubeCli, namespace, pod.Name, "")
			if err != nil {
				return nil, err
			}
			return kubeCli.Core().Pods(namespace).Get(pod.Name, metav1.GetOptions{})
		}
	}

	return nil, NewErrServerConfigNotFound(m.ServerConfig)
}

func DeleteCouchbaseOperator(kubeCli kubernetes.Interface, namespace string) error {
	name, err := GetOperatorName(kubeCli, namespace)
	if err != nil {
		return err
	}
	return kubeCli.CoreV1().Pods(namespace).Delete(name, metav1.NewDeleteOptions(0))
}

func GetOperatorName(kubeCli kubernetes.Interface, namespace string) (string, error) {
	selector := labels.SelectorFromSet(labels.Set(map[string]string{"name": "couchbase-operator"}))
	pods, err := kubeCli.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return "couchbase-operator", err
	}
	operatorPods := []string{}
	for i := 0; i < len(pods.Items); i++ {
		operatorPods = append(operatorPods, pods.Items[i].Name)
	}
	if len(operatorPods) > 1 {
		return "couchbase-operator", errors.New("too many couchbase operators")
	}
	return operatorPods[0], nil
}

func GetNodeNames(kubeCli kubernetes.Interface, namespace string) (string, error) {
	selector := labels.SelectorFromSet(labels.Set(map[string]string{"name": "couchbase-operator"}))
	pods, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return "couchbase-operator", err
	}
	operatorPods := []string{}
	for i := 0; i < len(pods.Items); i++ {
		operatorPods = append(operatorPods, pods.Items[i].Name)
	}
	if len(operatorPods) > 1 {
		return "couchbase-operator", errors.New("too many couchbase operators")
	}
	return operatorPods[0], nil
}

func NumK8Nodes(kubeCli kubernetes.Interface) (int, error) {
	nodeList, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return -1, err
	}
	return len(nodeList.Items), nil
}

func NumK8Workers(kubeCli kubernetes.Interface) (int, error) {
	nodeList, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return -1, err
	}
	numWorkers := 0
	for _, value := range nodeList.Items {
		node := &value
		nodeMap := node.GetLabels()
		if isMasterNode(nodeMap) {
			continue
		}
		numWorkers = numWorkers + 1
	}
	return numWorkers, nil
}

func GetMinNodeMem(kubeCli kubernetes.Interface) (float64, error) {
	minMem := math.Inf(+1)
	nodeList, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return 0.0, err
	}
	if len(nodeList.Items) > 0 {
		for _, value := range nodeList.Items {
			node := &value
			nodeMap := node.GetLabels()
			if isMasterNode(nodeMap) {
				continue
			}
			//kilobytes
			memQuantity := node.Status.Allocatable[v1.ResourceMemory]
			//megabytes
			newMem := float64(memQuantity.Value() >> 20)
			if newMem < minMem {
				minMem = newMem
			}
		}
		if minMem == math.Inf(+1) {
			return 0.0, fmt.Errorf("no minimum found")
		}

	} else {
		return 0.0, fmt.Errorf("no nodes in the cluster")
	}
	return minMem, nil
}

func GetMaxNodeMem(kubeCli kubernetes.Interface) (float64, error) {
	maxMem := 0.0
	nodeList, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return 0.0, err
	}
	if len(nodeList.Items) > 0 {
		for _, value := range nodeList.Items {
			node := &value
			nodeMap := node.GetLabels()
			if isMasterNode(nodeMap) {
				continue
			}
			//kilobytes
			memQuantity := node.Status.Allocatable[v1.ResourceMemory]
			//megabytes
			newMem := float64(memQuantity.Value() >> 20)
			if newMem > maxMem {
				maxMem = newMem
			}
		}
		if maxMem == 0.0 {
			return 0.0, fmt.Errorf("no maximum found")
		}

	} else {
		return 0.0, fmt.Errorf("no nodes in the cluster")
	}
	return maxMem, nil
}

func GetMaxScale(kubeCli kubernetes.Interface, minMem float64) (int, error) {
	scaleNum := 0
	nodeList, err := kubeCli.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	if len(nodeList.Items) > 0 {
		for _, value := range nodeList.Items {
			node := &value
			nodeMap := node.GetLabels()
			if isMasterNode(nodeMap) {
				continue
			}
			//kilobytes
			memQuantity := node.Status.Allocatable[v1.ResourceMemory]
			//megabytes
			nodeMem := float64(memQuantity.Value() >> 20)
			scaleNum = scaleNum + int(math.Floor(nodeMem/minMem))
		}
		if minMem == math.Inf(+1) {
			return 0, fmt.Errorf("unable to calculate scale number")
		}

	} else {
		return 0, fmt.Errorf("no nodes in the cluster")
	}
	return scaleNum, nil
}

// Construct expected name for the PersistentVolumeClaim which belongs to member
// where 'index' specifies the Nth claim generated from the specs template.
// Only specs with multiple VolumeMounts should return volumes with index > 0
func GetMemberPVC(kubeCli kubernetes.Interface, namespace string, claimName string, memberName string, index int) (*v1.PersistentVolumeClaim, error) {
	name := k8sutil.NameForPersistentVolumeClaim(claimName, memberName, index)
	return kubeCli.CoreV1().PersistentVolumeClaims(namespace).Get(name, metav1.GetOptions{})
}
