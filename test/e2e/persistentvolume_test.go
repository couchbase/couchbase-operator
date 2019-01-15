package e2e

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// This will create a Persistent volume claim data
// for adding into the cluster CRD
func createPersistentVolumeClaimSpec(storageClass, pvcName string, resourceQtyVal int64) corev1.PersistentVolumeClaim {
	resourceQuantity := apiresource.NewQuantity(resourceQtyVal*1024*1024*1024, apiresource.BinarySI)
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: pvcName},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": *resourceQuantity,
				},
			},
		},
	}
}

func createPodSecurityContext(fsGroup int, clusterSpec *v1.ClusterSpec) {
	if framework.Global.KubeType == "kubernetes" {
		fsGroupVal := int64(fsGroup)
		sc := corev1.PodSecurityContext{FSGroup: &fsGroupVal}
		clusterSpec.SecurityContext = &sc
	}
}

// Verifies actual PVC wrt to server pods matches the expected PVC mapping given by user
func VerifyPvcMappingForPods(t *testing.T, kubeClient kubernetes.Interface, namespace string, expectedPvcMap map[string]int, platformType string) (errToReturn error) {
	pvcMappingVerify := func() error {
		for memberName, pvcCount := range expectedPvcMap {
			pvcList, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: "couchbase_node=" + memberName})
			if err != nil {
				return err
			} else if len(pvcList.Items) != pvcCount {
				t.Logf("Persistent volume claims not created as expected for %s. Has %d volume, expected %d", memberName, len(pvcList.Items), pvcCount)
				return errors.New("PVC mapping verification failed")
			}
		}
		return nil
	}

	maxRetires := constants.Retries5
	if platformType == "azure" {
		maxRetires = constants.Retries30
	}
	for retryCount := 0; retryCount < maxRetires; retryCount++ {
		// Sleep before next poll
		time.Sleep(time.Second * 5)
		if errToReturn = pvcMappingVerify(); errToReturn == nil {
			return
		}
	}
	return
}

func MustVerifyPvcMappingForPods(t *testing.T, kubeClient kubernetes.Interface, namespace string, expectedPvcMap map[string]int, platformType string) {
	if err := VerifyPvcMappingForPods(t, kubeClient, namespace, expectedPvcMap, platformType); err != nil {
		t.Fatal(err)
	}
}

// Generic function to test the cb-server down and pod remove scenarios
func PersistentVolumeNodeFailoverGeneric(t *testing.T, clusterSize int, podMembersToKill []int, autoFailoverWillOccur bool) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	bucketName := "PVBucket"
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverTimeout"] = "10"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	// For validation purpose only
	eventsExpected := e2eutil.EventList{}

	// For event validation scheme
	memberDownEvents := e2eutil.EventValidator{}
	memberRecoveredEvents := e2eutil.EventValidator{}

	// Kill couchbase server pods in cluster and test auto failover
	for _, podMemberId := range podMembersToKill {
		podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, podMemberName, f.Namespace); err != nil {
			t.Fatal(err)
		}
		memberDownEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberId)
		memberRecoveredEvents.AddClusterPodEvent(testCouchbase, "MemberRecovered", podMemberId)

		eventsExpected = append(eventsExpected, *e2eutil.MemberRecoveredEvent(testCouchbase, podMemberId))
	}

	// For validation purpose only
	eventsExpected = append(eventsExpected, *e2eutil.RebalanceStartedEvent(testCouchbase))
	eventsExpected = append(eventsExpected, *e2eutil.RebalanceCompletedEvent(testCouchbase))
	if _, err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, eventsExpected, 600); err != nil {
		t.Fatal(err)
	}

	// For event schema validation
	expectedEvents.AddParallelEvents(memberDownEvents)
	expectedEvents.AddParallelEvents(memberRecoveredEvents)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	// Execute this test code only in case of kubernetes cluster,
	// since openshift container does not have permissions to execute in privileged mode
	if f.KubeType == "kubernetes" {
		// For validation purpose only
		eventsExpected = e2eutil.EventList{}

		// Kill couchbase server process in target pods
		for _, podMemberId := range podMembersToKill {
			memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
			if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "pkill beam.smp"); err != nil {
				t.Fatal(err)
			}
			eventsExpected = append(eventsExpected, *e2eutil.NewMemberDownEvent(testCouchbase, podMemberId))
		}
		if _, err := e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, eventsExpected, 100); err != nil {
			t.Fatal(err)
		}

		expectedEvents.AddParallelEvents(memberDownEvents)
		expectedEvents.AddParallelEvents(memberRecoveredEvents)

		e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 600)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries5)
	// K8S-537 - Will fail due to Pod recovery event not generated
	//ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Generic function to kill pods with operator
// Wait for recovery to happen on killed pods and reuse the volumes claims
func PersistentVolumeKillNodesWithOperatorGeneric(t *testing.T, clusterSize int, podMembersToKill []int) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	platformTimingMultiplier := e2eutil.GetPlatformTimingMultiplier(f.PlatformType)

	autofailoverTimeout := 30
	totalTimeToRecover := 0
	bucketName := "PVBucket"
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec, f.PlatformType)

	if _, err := e2eutil.CreateAdminConsoleClient(t, targetKube.APIHost(), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase); err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	memberDownEvents := e2eutil.EventValidator{}
	memberRecoveredEvents := e2eutil.EventValidator{}

	// Kill couchbase server pods in cluster and test auto failover
	killPodsErrChan := make(chan error)
	go func() {
		for _, podMemberId := range podMembersToKill {
			podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
			if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberName, &metav1.DeleteOptions{}); err != nil {
				killPodsErrChan <- err
			}
			totalTimeToRecover += autofailoverTimeout + 30
			memberDownEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberId)
			memberRecoveredEvents.AddClusterPodEvent(testCouchbase, "MemberRecovered", podMemberId)
		}
		killPodsErrChan <- nil
	}()

	// kill couchbase operator
	operatorPodList, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseOperatorLabel})
	if err != nil {
		t.Fatal(err)
	}
	for _, operatorPod := range operatorPodList.Items {
		if err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).Delete(operatorPod.Name, &metav1.DeleteOptions{}); err != nil {
			t.Fatalf("Failed to kill operator pod %s: %v", operatorPod.Name, err)
		}
	}

	// Wait to cb-server pods kill to complete
	if err := <-killPodsErrChan; err != nil {
		t.Fatalf("Unable to kill requested pods: %v", err)
	}

	podMembersToKillLen := len(podMembersToKill)
	if podMembersToKillLen == clusterSize {
		// All cluster pods are killed, should get one less than all events since first pod
		// event will not have the repective event, but recovered event will be registered
		expectedEvents.AddAnyOfEvents(memberRecoveredEvents)
		for index := 1; index < podMembersToKillLen; index++ {
			expectedEvents.AddAnyOfEvents(memberDownEvents)
		}
		for index := 1; index < podMembersToKillLen; index++ {
			expectedEvents.AddAnyOfEvents(memberRecoveredEvents)
		}
	} else {
		// If some pods are killed, should get all member down and recovered events
		expectedEvents.AddAnyOfEvents(memberDownEvents)
		expectedEvents.AddAnyOfEvents(memberRecoveredEvents)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), totalTimeToRecover+(60*platformTimingMultiplier))

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 300*platformTimingMultiplier)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Generic proc to create cluster given by server configs passed
// Kill the pods and remove the PVC and verify for pod recovery
func PersistentVolumeForSingleNodeServiceGeneric(t *testing.T, serviceConfig1, serviceConfig2, serviceConfig3 map[string]string) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	platformTimingMultiplier := e2eutil.GetPlatformTimingMultiplier(f.PlatformType)

	clusterSizeWithOutPvc, _ := strconv.Atoi(serviceConfig1["size"])
	clusterSizeWithPvc1, _ := strconv.Atoi(serviceConfig2["size"])
	clusterSizeWithPvc2, _ := strconv.Atoi(serviceConfig3["size"])
	clusterSize := clusterSizeWithOutPvc + clusterSizeWithPvc1 + clusterSizeWithPvc2
	autofailoverTimeout := 30
	bucketName := "PVBucket"
	pvc1Name := serviceConfig2["defaultVolMnt"]
	pvc2Name := serviceConfig3["defaultVolMnt"]
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverTimeout"] = strconv.Itoa(autofailoverTimeout)

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"service3": serviceConfig3,
		"bucket1":  bucketConfig1,
	}

	// Pod member with singe node service to kill
	podMemberIdToKill := clusterSize - 1

	// Define multiple volume claim template spec
	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvc1Name, 2)
	pvcTemplate2 := createPersistentVolumeClaimSpec(f.StorageClassName, pvc2Name, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1, pvcTemplate2}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec, f.PlatformType)

	client, err := e2eutil.CreateAdminConsoleClient(t, targetKube.APIHost(), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// To cross check number of persistent vol claims matches the defined spec
	var expectedPvcMap map[string]int
	switch serviceConfig3["services"] {
	case "analytics", "data", "index":
		expectedPvcMap = map[string]int{
			couchbaseutil.CreateMemberName(testCouchbase.Name, 0): 0,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 1): 0,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 2): 3,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 3): 3,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 4): 3,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 5): 2,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 6): 0,
		}
	default:
		expectedPvcMap = map[string]int{
			couchbaseutil.CreateMemberName(testCouchbase.Name, 0): 0,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 1): 0,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 2): 4,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 3): 4,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 4): 4,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 5): 1,
			couchbaseutil.CreateMemberName(testCouchbase.Name, 6): 0,
		}
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	podMemberNameToKill := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberIdToKill)

	// Kill single service pod and wait for recovery
	if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberNameToKill, &metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberIdToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.MemberRecoveredEvent(testCouchbase, podMemberIdToKill), autofailoverTimeout+(90*platformTimingMultiplier))
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRecovered", podMemberIdToKill)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries5*platformTimingMultiplier)

	// To cross check number of persistent vol claims matches the defined spec
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap, f.PlatformType); err != nil {
		t.Error(err)
	}

	// Kill pod along with its PVC
	if err := e2eutil.RemovePersistentVolumesOfPod(targetKube.KubeClient, f.Namespace, testCouchbase.Name, podMemberIdToKill); err != nil {
		t.Fatal(err)
	}

	if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberNameToKill, &metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberVolumeUnhealthyEvent(testCouchbase, podMemberIdToKill, "Missing PersistentVolumeClaim for path /opt/couchbase/var/lib/couchbase")

	// Sleep for autofailover to occur
	time.Sleep(time.Second*time.Duration(autofailoverTimeout) + 120)

	if err := e2eutil.WaitForUnhealthyNodes(t, client, constants.Retries5, constants.Size1); err != nil {
		t.Fatalf("Mismatch in unhealthy nodes count: %v", err)
	}

	// Manual failover to recover the pod
	member := &couchbaseutil.Member{
		Name:         podMemberNameToKill,
		Namespace:    f.Namespace,
		ServerConfig: testCouchbase.Spec.ServerSettings[2].Name,
		SecureClient: false,
	}
	if err := e2eutil.FailoverNode(t, client, constants.Retries5, member.HostURL()); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddClusterPodEvent(testCouchbase, "FailedOver", podMemberIdToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), 180)
	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", clusterSize)

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.NewMemberRemoveEvent(testCouchbase, podMemberIdToKill), 300)

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 400)

	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", podMemberIdToKill)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
	e2eutil.DeleteCbCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, testCouchbase)
}

// Create multi-node couchbase cluster with volumeClaimTemplates
// Create test bucket and verify
func TestPersistentVolumeCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2

	// Create a basic supportable cluster with 2 stateful and 2 stateless nodes
	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes.CRClient, cluster, constants.Retries30)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes.KubeClient, f.Namespace, cluster.Name, expectedEvents)

	// Check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(cluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cluster.Name, 3): 1,
	}

	// To cross check number of persistent vol claims matches the defined spec
	if err := VerifyPvcMappingForPods(t, kubernetes.KubeClient, f.Namespace, expectedPvcMap, f.PlatformType); err != nil {
		t.Error(err)
	}
}

// Create PV enabled couchbase cluster
// First kill multiple cb-server process and then kill multiple cb pods
// Both the cases, failover will be triggered and recovery pod will be created
// replacing the old one with same pod name and reuses the persistent volume claims
func TestPersistentVolumeAutoFailover(t *testing.T) {
	clusterSize := 3
	podMembersToKill := []int{1}
	autoFailoverWillOccur := true
	PersistentVolumeNodeFailoverGeneric(t, clusterSize, podMembersToKill, autoFailoverWillOccur)
}

// Create PV enabled couchbase cluster
// First kill multiple cb-server process and then kill multiple cb pods
// Both the cases failover will not be triggered, so new pod should spawed replacing the
// old ones using the same pod name and reuse the persistent volume claims
func TestPersistentVolumeNodeFailover(t *testing.T) {
	clusterSize := 6
	podMembersToKill := []int{1, 5}
	autoFailoverWillOccur := false
	PersistentVolumeNodeFailoverGeneric(t, clusterSize, podMembersToKill, autoFailoverWillOccur)
}

// Create couchbase cluster with all nodes pointed to PVC
// Kill couchbase-server process on all nodes
// Operator should respawn all nodes with same name and reuse the PVC
func TestPersistentVolumeKillAllPods(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	platformTimingMultiplier := e2eutil.GetPlatformTimingMultiplier(f.PlatformType)

	clusterSize := 4
	podMembersToKill := []int{0, 1, 2, 3}
	autofailoverTimeout := 30
	bucketName := "PVBucket"
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverMaxCount"] = "3"
	clusterConfig["autoFailoverTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec, f.PlatformType)

	if _, err := e2eutil.CreateAdminConsoleClient(t, targetKube.APIHost(), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase); err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	expectedEvents := e2eutil.EventValidator{}
	expectedEvents.AddClusterEvent(testCouchbase, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

	// Calculate sleep time for action to be taken by operator
	timeToSleep := time.Duration(autofailoverTimeout)*time.Second + 30

	//For event validation
	memDownEventsValidator := e2eutil.EventValidator{}
	memRecoveredEventsValidator := e2eutil.EventValidator{}

	// For event tracking
	allMemberDownEvents := e2eutil.EventList{}
	allMemberRecoveredEvents := e2eutil.EventList{}

	// Kill couchbase server pods in cluster and test auto failover
	for _, podMemberId := range podMembersToKill {
		podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
		if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberName, &metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}

		// Saving both events in same validator, since it can occur in any order in real time
		memDownEventsValidator.AddClusterPodEvent(testCouchbase, "MemberDown", podMemberId)
		memRecoveredEventsValidator.AddClusterPodEvent(testCouchbase, "MemberRecovered", podMemberId)

		allMemberDownEvents = append(allMemberDownEvents, *e2eutil.NewMemberDownEvent(testCouchbase, podMemberId))
		allMemberRecoveredEvents = append(allMemberRecoveredEvents, *e2eutil.MemberRecoveredEvent(testCouchbase, podMemberId))
	}

	if _, err := e2eutil.WaitForListOfClusterEvents(targetKube.KubeClient, testCouchbase, allMemberDownEvents, clusterSize-1, 60); err != nil {
		t.Error(err)
	}

	clusterBalancedErr := make(chan error)
	go func() {
		// Wait for cluster balanced condition after recovering the cluster pods
		clusterBalancedErr <- e2eutil.WaitForClusterBalancedCondition(t, targetKube.CRClient, testCouchbase, 300*platformTimingMultiplier)
	}()

	time.Sleep(timeToSleep)

	if _, err := e2eutil.WaitForListOfClusterEvents(targetKube.KubeClient, testCouchbase, allMemberRecoveredEvents, clusterSize-1, 300*platformTimingMultiplier); err != nil {
		t.Error(err)
	}

	// First any one pod will be recovered, followed by member down & recovery events
	expectedEvents.AddAnyOfEvents(memRecoveredEventsValidator)
	for index := 1; index < clusterSize; index++ {
		expectedEvents.AddAnyOfEvents(memDownEventsValidator)
	}
	for index := 1; index < clusterSize; index++ {
		expectedEvents.AddAnyOfEvents(memRecoveredEventsValidator)
	}

	if err := <-clusterBalancedErr; err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	// Execute this test code only in case of kubernetes cluster,
	// since openshift container does not have permissions to execute in privileged mode
	if f.KubeType == "kubernetes" {
		// Kill couchbase server process in target pods
		for _, podMemberId := range podMembersToKill {
			memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
			if _, err := f.ExecShellInPod(f.TestClusters[0], memberName, "pkill beam.smp"); err != nil {
				t.Fatal(err)
			}
		}

		time.Sleep(timeToSleep)

		// Wait for cluster balanced condition after recovering the cluster pods
		if err := e2eutil.WaitForClusterBalancedCondition(t, targetKube.CRClient, testCouchbase, 300*platformTimingMultiplier); err != nil {
			t.Fatal(err)
		}

		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	}
	ValidateEvents(t, targetKube.KubeClient, f.Namespace, testCouchbase.Name, expectedEvents)
}

// Create cb cluster with PVC enabled
// Remove the persistent volume so the volume enters 'Terminating' condition
// Then remove the respective node
// New pod along with PVC will be created and rebalanced by the operator
func TestPersistentVolumeRemoveVolume(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 5
	podMemberToKill := 3
	bucketName := "PVBucket"
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(2, "test_config_1", []string{"data", "query", "index"})

	serviceConfig2 := e2eutil.GetServiceConfigMap(3, "test_config_2", []string{"data", "query", "index"})
	serviceConfig2["defaultVolMnt"] = pvcName
	serviceConfig2["dataVolMnt"] = pvcName
	serviceConfig2["indexVolMnt"] = pvcName
	serviceConfig2["analyticsVolMnt"] = pvcName + "," + pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)

	if err := e2eutil.RemovePersistentVolumesOfPod(targetKube.KubeClient, f.Namespace, testCouchbase.Name, podMemberToKill); err != nil {
		t.Fatal(err)
	}

	podMemberNameToKill := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberToKill)
	pvcList, err := targetKube.KubeClient.CoreV1().PersistentVolumeClaims(f.Namespace).List(metav1.ListOptions{LabelSelector: "couchbase_node=" + podMemberNameToKill})
	if err != nil {
		t.Fatalf("Unable to fetch persistent volume list for pod %s: %v", podMemberNameToKill, err)
	}

	for _, pvc := range pvcList.Items {
		t.Logf("Volume claim status of %s: %v", pvc.Name, pvc.Status)
	}

	if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberNameToKill, &metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Unable to kill pod member %d: %v", podMemberToKill, err)
	}
	expectedEvents.AddMemberDownEvent(testCouchbase, podMemberToKill)
	expectedEvents.AddMemberFailedOverEvent(testCouchbase, podMemberToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), 60)
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, clusterSize)
	if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberName, &metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to kill pod %s: %v", podMemberName, err)
	}
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, clusterSize)

	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 90)
	e2eutil.MustWaitForClusterEvent(t, targetKube.KubeClient, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 300)

	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, podMemberToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// To cross check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(testCouchbase.Name, 0): 0,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 1): 0,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 2): 5,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 3): 0,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 4): 5,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 5): 5,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 6): 0,
	}

	// To cross check number of persistent vol claims matches the defined spec
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap, f.PlatformType); err != nil {
		t.Error(err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create PVC enabled couchbase cluster
// Kill one of the nodes and also the operator pod
// Operator should restart and wait for failover to happen
// Rebalance the cluster by recovering the failedover node
func TestPersistentVolumeKillPodAndOperator(t *testing.T) {
	clusterSize := 4
	podMembersToKill := []int{1}
	PersistentVolumeKillNodesWithOperatorGeneric(t, clusterSize, podMembersToKill)
}

// Create PVC enabled couchbase cluster
// Kill all cb pod along with operator
// Operator should restart and wait for failover to happen
// Rebalance the cluster by recovering the failedover nodes
func TestPersistentVolumeKillAllPodsAndOperator(t *testing.T) {
	clusterSize := 3
	podMembersToKill := []int{0, 1, 2}
	PersistentVolumeKillNodesWithOperatorGeneric(t, clusterSize, podMembersToKill)
}

// Create couchbase cluster with Persistent volumes and server groups
// Kill one couchbase server pods from each server groups
// Operator should replace killed pods with new one with same name and reuse PVC
func TestPersistentVolumeRzaNodesKilled(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	platformTimingMultiplier := e2eutil.GetPlatformTimingMultiplier(f.PlatformType)

	clusterSize := 9
	pvcName := "couchbase"

	// Create cluster spec for RZA feature
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	// Deploy couchbase cluster with PVC
	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec, f.PlatformType)

	if _, err := e2eutil.CreateAdminConsoleClient(t, targetKube.APIHost(), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase); err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// Create a expected RZA results map for verification
	sort.Strings(availableServerGroupList)
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	memberIdsToKill := []int{1, 3, 8}
	receivedEvents := e2eutil.EventList{}
	eventChan := make(chan corev1.Event)
	errChan := make(chan error)
	for _, memberId := range memberIdsToKill {
		podNameToKill := couchbaseutil.CreateMemberName(testCouchbase.Name, memberId)
		event := e2eutil.NewMemberDownEvent(testCouchbase, memberId)
		go func(event corev1.Event) {
			err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, &event, 300)
			eventChan <- event
			errChan <- err
		}(*event)
		if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podNameToKill, &metav1.DeleteOptions{}); err != nil {
			t.Fatalf("Failed to kill pod %s: %v", podNameToKill, err)
		}
	}
	for _, _ = range memberIdsToKill {
		receivedEvents = append(receivedEvents, <-eventChan)
		if err = <-errChan; err != nil {
			t.Fatalf("failed to wait for event %v", err)
		}
	}

	for _, recEvent := range receivedEvents {
		expectedEvents = append(expectedEvents, recEvent)
	}

	eventsExpected := e2eutil.EventList{}
	for _, podMemberId := range memberIdsToKill {
		eventsExpected = append(eventsExpected, *e2eutil.MemberRecoveredEvent(testCouchbase, podMemberId))
	}
	eventsExpected = append(eventsExpected, *e2eutil.RebalanceStartedEvent(testCouchbase))
	eventsExpected = append(eventsExpected, *e2eutil.RebalanceCompletedEvent(testCouchbase))
	receivedEvents, err = e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, eventsExpected, 600*platformTimingMultiplier)

	for _, recEvent := range receivedEvents {
		expectedEvents = append(expectedEvents, recEvent)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30*platformTimingMultiplier)

	// Cross check rza deployment matches the expected values
	deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Logf("Failed to get deployed Rza map: %v", err)
	}
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create couchbase cluster with Persistent volumes and server groups
// Kill couchbase server pods on particular server groups
// Operator should replace killed pods with new one with same name and reuse PVC
func TestPersistentVolumeRzaFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	platformTimingMultiplier := e2eutil.GetPlatformTimingMultiplier(f.PlatformType)

	clusterSize := 9
	pvcName := "couchbase"

	// Create cluster spec for RZA feature
	availableServerGroupList := GetAvailabilityZones(t, targetKube)
	availableServerGroups := strings.Join(availableServerGroupList, ",")
	clusterConfig := e2eutil.GetClusterConfigMap(256, 256, 256, 256, 1024, 30, 2, true)
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName
	bucketConfig1 := e2eutil.BasicOneReplicaBucket
	serverGroups := map[string]string{"groupNames": availableServerGroups}
	configMap := map[string]map[string]string{
		"cluster":      clusterConfig,
		"service1":     serviceConfig1,
		"bucket1":      bucketConfig1,
		"serverGroups": serverGroups,
	}

	// Deploy couchbase cluster with PVC
	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec, f.PlatformType)

	if _, err := e2eutil.CreateAdminConsoleClient(t, targetKube.APIHost(), f.Namespace, f.PlatformType, targetKube.KubeClient, testCouchbase); err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// Create a expected RZA results map for verification
	sort.Strings(availableServerGroupList)
	expectedRzaResultMap := GetExpectedRzaResultMap(clusterSize, availableServerGroupList)

	// Create a map for server-groups based on deployed cb-server nodes
	deployedRzaGroupsMap, err := GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}
	// Cross check rza deployment matches the expected values
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	// Kill nodes in 3rd server groups
	memberIdsToKill := []int{2, 5, 8}
	receivedEvents := e2eutil.EventList{}
	eventChan := make(chan corev1.Event)
	errChan := make(chan error)
	for _, memberId := range memberIdsToKill {
		podNameToKill := couchbaseutil.CreateMemberName(testCouchbase.Name, memberId)
		event := e2eutil.NewMemberDownEvent(testCouchbase, memberId)
		go func(event corev1.Event) {
			err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, &event, 300)
			eventChan <- event
			errChan <- err
		}(*event)
		if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podNameToKill, &metav1.DeleteOptions{}); err != nil {
			t.Fatalf("Failed to kill pod %s: %v", podNameToKill, err)
		}
	}
	for _, _ = range memberIdsToKill {
		receivedEvents = append(receivedEvents, <-eventChan)
		if err = <-errChan; err != nil {
			t.Fatalf("failed to wait for event %v", err)
		}
	}

	for _, recEvent := range receivedEvents {
		expectedEvents = append(expectedEvents, recEvent)
	}

	eventsExpected := e2eutil.EventList{}
	for _, podMemberId := range memberIdsToKill {
		eventsExpected = append(eventsExpected, *e2eutil.MemberRecoveredEvent(testCouchbase, podMemberId))
	}
	eventsExpected = append(eventsExpected, *e2eutil.RebalanceStartedEvent(testCouchbase))
	eventsExpected = append(eventsExpected, *e2eutil.RebalanceCompletedEvent(testCouchbase))
	receivedEvents, err = e2eutil.WaitForClusterEventsInParallel(targetKube.KubeClient, testCouchbase, eventsExpected, 600*platformTimingMultiplier)

	for _, recEvent := range receivedEvents {
		expectedEvents = append(expectedEvents, recEvent)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries30*platformTimingMultiplier)

	// Cross check rza deployment matches the expected values
	deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create multiple Persistent volume claim definitions in spec
// Create couchbase cluster with one seperate service in isolated PVC
// Such that one group without PVC, 2nd using PVC spec1, 3rd with PVC spec2
// Kill single service node and test the behaviour
func TestPersistentVolumeWithSingleNodeService(t *testing.T) {
	availableServices := []string{"data", "query", "index", "analytics", "eventing"}
	volServiceMap := map[string]string{
		"data":      "dataVolMnt",
		"index":     "indexVolMnt",
		"analytics": "analyticsVolMnt",
	}

	clusterPodsWithoutPvc := 2
	clusterPodsWithPvc1 := 3
	clusterPodsWithPvc2 := 1

	pvc1Name := "couchbase-pvc1"
	pvc2Name := "couchbase-pvc2"

	for _, singleNodeService := range availableServices {
		if singleNodeService == "data" {
			continue
		}

		var otherServiceList []string
		var serviceConfig1, serviceConfig2, serviceConfig3 map[string]string
		for _, serviceName := range availableServices {
			if serviceName != singleNodeService {
				otherServiceList = append(otherServiceList, serviceName)
			}
		}

		// Create persistent volume less node spec
		serviceConfig1 = e2eutil.GetServiceConfigMap(clusterPodsWithoutPvc, "test_config_1", otherServiceList)

		// Create persistent volume definitions for other service config
		serviceConfig2 = e2eutil.GetServiceConfigMap(clusterPodsWithPvc1, "test_config_2", otherServiceList)

		// Create persistent volume definitions for single node service config
		serviceConfig3 = e2eutil.GetServiceConfigMap(clusterPodsWithPvc2, "test_config_3", []string{singleNodeService})

		for _, otherService := range otherServiceList {
			switch otherService {
			case "analytics", "data", "index":
				serviceConfig2[volServiceMap[otherService]] = pvc1Name
			}
		}

		switch singleNodeService {
		case "analytics", "data", "index":
			serviceConfig3[volServiceMap[singleNodeService]] = pvc2Name
		}

		// Add default volume for both PVC spec
		serviceConfig2["defaultVolMnt"] = pvc1Name
		serviceConfig3["defaultVolMnt"] = pvc2Name

		// Run actual test case
		t.Logf("Running single node service case for '%s'", singleNodeService)
		PersistentVolumeForSingleNodeServiceGeneric(t, serviceConfig1, serviceConfig2, serviceConfig3)
	}
}

// Create couchbase with large pvc template storage value request
func TestPersistentVolumeCreateWithHugeStorage(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	clusterSize := 5
	bucketName := "PVBucket"
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	clusterConfig["autoFailoverOnDiskIssues"] = "true"
	clusterConfig["autoFailoverOnDiskIssuesTimeout"] = "30"
	serviceConfig1 := e2eutil.GetServiceConfigMap(2, "test_config_1", []string{"data", "query", "index"})

	serviceConfig2 := e2eutil.GetServiceConfigMap(clusterSize-2, "test_config_2", []string{"data", "query", "index"})
	serviceConfig2["defaultVolMnt"] = pvcName
	serviceConfig2["dataVolMnt"] = pvcName
	serviceConfig2["indexVolMnt"] = pvcName
	serviceConfig2["analyticsVolMnt"] = pvcName + "," + pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"service2": serviceConfig2,
		"bucket1":  bucketConfig1,
	}

	// This will request storage claim of 2000Gi
	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2000)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)

	// To cross check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(testCouchbase.Name, 0): 0,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 1): 0,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 2): 5,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 3): 5,
		couchbaseutil.CreateMemberName(testCouchbase.Name, 4): 5,
	}

	// To cross check number of persistent vol claims matches the defined spec
	if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap, f.PlatformType); err != nil {
		t.Error(err)
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

// Create 3 node couchbase cluster initially
// Resize cluster to different size
// Check for PVC status and cluster health condition
func TestPersistentVolumeResizeCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)
	platformTimingMultiplier := e2eutil.GetPlatformTimingMultiplier(f.PlatformType)

	clusterSize := 3
	bucketName := "PVBucket"
	pvcName := "couchbase"
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data", "query", "index"})
	serviceConfig1["defaultVolMnt"] = pvcName
	serviceConfig1["dataVolMnt"] = pvcName
	serviceConfig1["indexVolMnt"] = pvcName

	bucketConfig1 := e2eutil.GetBucketConfigMap(bucketName, "couchbase", "high", 100, 2, true, false)
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"bucket1":  bucketConfig1,
	}

	pvcTemplate1 := createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec, f.PlatformType)

	expectedEvents := e2eutil.EventList{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, memberIndex)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	expectedEvents.AddBucketCreateEvent(testCouchbase, bucketName)

	expectedPvcMap := map[string]int{}
	resizeClusterSizes := []int{2, 5, 1, 3}
	for _, clusterSize = range resizeClusterSizes {
		service := 0

		testCouchbase = e2eutil.MustResizeClusterNoWait(t, service, clusterSize, targetKube.CRClient, testCouchbase)
		t.Logf("Waiting For Cluster Size To Be: %v...\n", strconv.Itoa(clusterSize))
		names, err := e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, clusterSize, 60*platformTimingMultiplier, testCouchbase)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Resize Success: %v...\n", names)

		e2eutil.MustWaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10)

		switch clusterSize {
		case 2:
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			expectedEvents.AddMemberRemoveEvent(testCouchbase, 2)

		case 5:
			for memberId := 3; memberId <= clusterSize; memberId++ {
				expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
			}
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)

		case 1:
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
			for _, memberId := range []int{1, 3, 4, 5} {
				expectedEvents.AddMemberRemoveEvent(testCouchbase, memberId)
			}

		case 3:
			expectedEvents.AddMemberAddEvent(testCouchbase, 6)
			expectedEvents.AddMemberAddEvent(testCouchbase, 7)
			expectedEvents.AddRebalanceStartedEvent(testCouchbase)
		}
		expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

		// Populate the expectedPvcMap for maximum available nodes everytime
		for memberId := 0; memberId < 9; memberId++ {
			memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, memberId)
			expectedPvcMap[memberName] = 0
		}
		podList, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + testCouchbase.Name})
		if err != nil {
			t.Fatalf("Failed to fetch pod list: %v", err)
		}
		for _, pod := range podList.Items {
			expectedPvcMap[pod.Name] = 3
		}

		// To cross check number of persistent vol claims matches the defined spec
		if err := VerifyPvcMappingForPods(t, targetKube.KubeClient, f.Namespace, expectedPvcMap, f.PlatformType); err != nil {
			t.Error(err)
		}
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}
