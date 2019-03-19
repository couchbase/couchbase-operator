package e2e

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This will create a Persistent volume claim data
// for adding into the cluster CRD
func createPersistentVolumeClaimSpec(t *testing.T, k8s *types.Cluster, storageClass, pvcName string, resourceQtyVal int64) corev1.PersistentVolumeClaim {
	sc, err := k8s.KubeClient.StorageV1().StorageClasses().Get(storageClass, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	annotations := map[string]string{}

	if sc.VolumeBindingMode != nil && *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer {
		annotations[operator_constants.AnnotationVolumeBindingMode] = string(storagev1.VolumeBindingWaitForFirstConsumer)
	}

	resourceQuantity := apiresource.NewQuantity(resourceQtyVal*1024*1024*1024, apiresource.BinarySI)
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pvcName,
			Annotations: annotations,
		},
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
func VerifyPvcMappingForPods(t *testing.T, k8s *types.Cluster, namespace string, expectedPvcMap map[string]int) (errToReturn error) {
	pvcMappingVerify := func() error {
		for memberName, pvcCount := range expectedPvcMap {
			pvcList, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: "couchbase_node=" + memberName})
			if err != nil {
				return err
			} else if len(pvcList.Items) != pvcCount {
				t.Logf("Persistent volume claims not created as expected for %s. Has %d volume, expected %d", memberName, len(pvcList.Items), pvcCount)
				return errors.New("PVC mapping verification failed")
			}
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, e2eutil.IntMax, "", "", func() error {
		// Sleep before next poll
		return pvcMappingVerify()
	})
}

func MustVerifyPvcMappingForPods(t *testing.T, k8s *types.Cluster, namespace string, expectedPvcMap map[string]int) {
	if err := VerifyPvcMappingForPods(t, k8s, namespace, expectedPvcMap); err != nil {
		e2eutil.Die(t, err)
	}
}

// Generic function to test the cb-server down and pod remove scenarios
func PersistentVolumeNodeFailoverGeneric(t *testing.T, clusterSize int, podMembersToKill []int, autoFailoverWillOccur bool) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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

	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec)

	expectedEvents := e2eutil.EventValidator{}
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", memberIndex)
	}
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	expectedEvents.AddClusterBucketEvent(testCouchbase, "Create", bucketName)

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
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

	// For event schema validation
	expectedEvents.AddParallelEvents(memberDownEvents)
	expectedEvents.AddParallelEvents(memberRecoveredEvents)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")

	// Execute this test code only in case of kubernetes cluster,
	// since openshift container does not have permissions to execute in privileged mode
	if f.KubeType == "kubernetes" {
		// Kill couchbase server process in target pods
		for _, podMemberId := range podMembersToKill {
			memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
			e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "pkill beam.smp")
		}
		expectedEvents.AddParallelEvents(memberDownEvents)
		expectedEvents.AddParallelEvents(memberRecoveredEvents)

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	// K8S-537 - Will fail due to Pod recovery event not generated
	//ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Generic function to kill pods with operator
// Wait for recovery to happen on killed pods and reuse the volumes claims
func PersistentVolumeKillNodesWithOperatorGeneric(t *testing.T, clusterSize int, podMembersToKill []int) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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

	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec)

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
		// event will not have the respective event, but recovered event will be registered
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

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), time.Duration(totalTimeToRecover+60)*time.Second)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Generic proc to create cluster given by server configs passed
// Kill the pods and remove the PVC and verify for pod recovery
func PersistentVolumeForSingleNodeServiceGeneric(t *testing.T, serviceConfig1, serviceConfig2, serviceConfig3 map[string]string) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

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
	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvc1Name, 2)
	pvcTemplate2 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvc2Name, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1, pvcTemplate2}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec)

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

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.MemberRecoveredEvent(testCouchbase, podMemberIdToKill), 5*time.Minute)
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRecovered", podMemberIdToKill)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// To cross check number of persistent vol claims matches the defined spec
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap)

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

	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, constants.Size1, time.Minute)

	// Manual failover to recover the pod
	e2eutil.MustFailoverNode(t, targetKube, testCouchbase, podMemberIdToKill, time.Minute)
	expectedEvents.AddClusterPodEvent(testCouchbase, "FailedOver", podMemberIdToKill)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), 3*time.Minute)
	expectedEvents.AddClusterPodEvent(testCouchbase, "AddNewMember", clusterSize)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberRemoveEvent(testCouchbase, podMemberIdToKill), 5*time.Minute)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)

	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceStarted")
	expectedEvents.AddClusterPodEvent(testCouchbase, "MemberRemoved", podMemberIdToKill)
	expectedEvents.AddClusterEvent(testCouchbase, "RebalanceCompleted")
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)

	// Check number of persistent vol claims matches the defined spec
	expectedPvcMap := map[string]int{
		couchbaseutil.CreateMemberName(cluster.Name, 0): 1,
		couchbaseutil.CreateMemberName(cluster.Name, 1): 1,
		couchbaseutil.CreateMemberName(cluster.Name, 2): 1,
		couchbaseutil.CreateMemberName(cluster.Name, 3): 1,
	}

	// To cross check number of persistent vol claims matches the defined spec
	MustVerifyPvcMappingForPods(t, kubernetes, f.Namespace, expectedPvcMap)
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
func TestPersistentVolumeKillAllPodsDeletePod(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	clusterSize := 4
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

	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	for i := 0; i < clusterSize; i++ {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, i, false)
	}
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
		eventschema.Repeat{Times: clusterSize - 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: clusterSize - 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestPersistentVolumeKillAllPodsKillService(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	clusterSize := 4
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

	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	for i := 0; i < clusterSize; i++ {
		memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, i)
		e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "pkill beam.smp")
	}
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Don't bother with events here, it's impossible to check.
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

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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

	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec)

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

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, clusterSize), time.Minute)
	expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)

	podMemberName := couchbaseutil.CreateMemberName(testCouchbase.Name, clusterSize)
	if err := k8sutil.DeletePod(targetKube.KubeClient, f.Namespace, podMemberName, &metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to kill pod %s: %v", podMemberName, err)
	}
	expectedEvents.AddRebalanceIncompleteEvent(testCouchbase)
	expectedEvents.AddFailedAddNodeEvent(testCouchbase, clusterSize)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 2*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

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
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
// Kill one couchbase server pod from each server group
// Operator should replace killed pods with new one with same name and reuse PVC
func TestPersistentVolumeRzaNodesKilled(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	clusterSize := e2eutil.MustNumNodes(t, targetKube)
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
	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec)

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

	// kill the first N pods where N is the no of server groups
	victims := []int{}
	for i := 0; i < len(availableServerGroupList); i++ {
		victims = append(victims, i)
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, false)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Cross check rza deployment matches the expected values
	deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Logf("Failed to get deployed Rza map: %v", err)
	}
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create couchbase cluster with Persistent volumes and server groups
// Kill couchbase server pods on a particular server group
// Operator should replace killed pods with new one with same name and reuse PVC
func TestPersistentVolumeRzaFailover(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	clusterSize := e2eutil.MustNumNodes(t, targetKube)
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
	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminExposed, clusterSpec)

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

	// Kill nodes in 1st server group
	victimGroup := 0
	victims := []int{}
	for i := 0; i < clusterSize; i++ {
		if i%len(availableServerGroupList) == victimGroup {
			victims = append(victims, i)
		}
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, true)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Cross check rza deployment matches the expected values
	deployedRzaGroupsMap, err = GetDeployedRzaMap(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("Failed to get deployed Rza map: %v", err)
	}
	if reflect.DeepEqual(expectedRzaResultMap, deployedRzaGroupsMap) == false {
		t.Fatalf("RZA deployment failed to deploy as expected.\n Expected: %v\n Deployed: %v", expectedRzaResultMap, deployedRzaGroupsMap)
	}

	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: len(victims), Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create multiple Persistent volume claim definitions in spec
// Create couchbase cluster with one separate service in isolated PVC
// Such that one group without PVC, 2nd using PVC spec1, 3rd with PVC spec2
// Kill single service node and test the behaviour
func TestPersistentVolumeWithSingleNodeService(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	t.Skip("this doesn't work")

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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
	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2000)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec)

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
	MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

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

	pvcTemplate1 := createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2)
	clusterSpec := e2eutil.CreateClusterSpec(targetKube.DefaultSecret.Name, configMap)
	clusterSpec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{pvcTemplate1}
	createPodSecurityContext(1000, &clusterSpec)

	testCouchbase := e2eutil.MustCreateClusterFromSpec(t, targetKube, f.Namespace, constants.AdminHidden, clusterSpec)

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

		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, 5*time.Minute)

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
		MustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}
