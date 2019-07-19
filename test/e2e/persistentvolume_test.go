package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
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

// Verifies actual PVC wrt to server pods matches the expected PVC mapping given by user
func verifyPvcMappingForPods(t *testing.T, k8s *types.Cluster, namespace string, expectedPvcMap map[string]int) (errToReturn error) {
	pvcMappingVerify := func() error {
		for memberName, pvcCount := range expectedPvcMap {
			pvcList, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: "couchbase_node=" + memberName})
			if err != nil {
				return err
			} else if len(pvcList.Items) != pvcCount {
				t.Logf("Persistent volume claims not created as expected for %s. Has %d volume, expected %d", memberName, len(pvcList.Items), pvcCount)
				return fmt.Errorf("pvc mapping verification failed")
			}
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, e2eutil.IntMax, "", "", pvcMappingVerify)
}

func mustVerifyPvcMappingForPods(t *testing.T, k8s *types.Cluster, namespace string, expectedPvcMap map[string]int) {
	if err := verifyPvcMappingForPods(t, k8s, namespace, expectedPvcMap); err != nil {
		e2eutil.Die(t, err)
	}
}

// Generic function to test the cb-server down and pod remove scenarios
func persistentVolumeNodeFailoverGeneric(t *testing.T, clusterSize int, podMembersToKill []int, autoFailoverWillOccur bool) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	pvcName := "couchbase"

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 10
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		VolumeMounts: &couchbasev2.VolumeMounts{
			DefaultClaim: pvcName,
			DataClaim:    pvcName,
		},
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// Kill couchbase server pods in cluster and test auto failover
	for _, podMemberId := range podMembersToKill {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberId, false)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

	// Execute this test code only in case of kubernetes cluster,
	// since openshift container does not have permissions to execute in privileged mode
	if f.KubeType == "kubernetes" {
		// Kill couchbase server process in target pods
		for _, podMemberId := range podMembersToKill {
			memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, podMemberId)
			e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "pkill beam.smp")
		}

		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 10*time.Minute)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	// K8S-537 - Will fail due to Pod recovery event not generated
	//ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Generic function to kill pods with operator
// Wait for recovery to happen on killed pods and reuse the volumes claims
func persistentVolumeKillNodesWithOperatorGeneric(t *testing.T, clusterSize int, podMembersToKill []int) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	pvcName := "couchbase"

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 30
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		VolumeMounts: &couchbasev2.VolumeMounts{
			DefaultClaim: pvcName,
			DataClaim:    pvcName,
			IndexClaim:   pvcName,
		},
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// Kill couchbase server pods in cluster and test auto failover
	e2eutil.MustDeleteCouchbaseOperator(t, targetKube, f.Namespace)
	for _, victim := range podMembersToKill {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, false)
	}
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceCompletedEvent(testCouchbase), 5*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, len(podMembersToKill)),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create multi-node couchbase cluster with volumeClaimTemplates
// Create test bucket and verify
func TestPersistentVolumeCreateCluster(t *testing.T) {
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2

	// Create a basic supportable cluster with 2 stateful and 2 stateless nodes
	e2eutil.MustNewBucket(t, kubernetes, f.Namespace, e2espec.DefaultBucket)
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
	mustVerifyPvcMappingForPods(t, kubernetes, f.Namespace, expectedPvcMap)
}

// Create PV enabled couchbase cluster
// First kill multiple cb-server process and then kill multiple cb pods
// Both the cases, failover will be triggered and recovery pod will be created
// replacing the old one with same pod name and reuses the persistent volume claims
func TestPersistentVolumeAutoFailover(t *testing.T) {
	clusterSize := 3
	podMembersToKill := []int{1}
	autoFailoverWillOccur := true
	persistentVolumeNodeFailoverGeneric(t, clusterSize, podMembersToKill, autoFailoverWillOccur)
}

// Create PV enabled couchbase cluster
// First kill multiple cb-server process and then kill multiple cb pods
// Both the cases failover will not be triggered, so new pod should spawed replacing the
// old ones using the same pod name and reuse the persistent volume claims
func TestPersistentVolumeNodeFailover(t *testing.T) {
	clusterSize := 6
	podMembersToKill := []int{1, 5}
	autoFailoverWillOccur := false
	persistentVolumeNodeFailoverGeneric(t, clusterSize, podMembersToKill, autoFailoverWillOccur)
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
	pvcName := "couchbase"

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 30
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		VolumeMounts: &couchbasev2.VolumeMounts{
			DefaultClaim: pvcName,
			DataClaim:    pvcName,
			IndexClaim:   pvcName,
		},
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

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
	pvcName := "couchbase"

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 30
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		VolumeMounts: &couchbasev2.VolumeMounts{
			DefaultClaim: pvcName,
			DataClaim:    pvcName,
			IndexClaim:   pvcName,
		},
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	for i := 0; i < clusterSize; i++ {
		memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, i)
		e2eutil.MustExecShellInPod(t, targetKube, f.Namespace, memberName, "pkill beam.smp")
	}
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Don't bother with events here, it's impossible to check.
}

// Create PVC enabled couchbase cluster
// Kill one of the nodes and also the operator pod
// Operator should restart and wait for failover to happen
// Rebalance the cluster by recovering the failedover node
func TestPersistentVolumeKillPodAndOperator(t *testing.T) {
	clusterSize := 4
	podMembersToKill := []int{1}
	persistentVolumeKillNodesWithOperatorGeneric(t, clusterSize, podMembersToKill)
}

// Create PVC enabled couchbase cluster
// Kill all cb pod along with operator
// Operator should restart and wait for failover to happen
// Rebalance the cluster by recovering the failedover nodes
func TestPersistentVolumeKillAllPodsAndOperator(t *testing.T) {
	clusterSize := 3
	podMembersToKill := []int{0, 1, 2}
	persistentVolumeKillNodesWithOperatorGeneric(t, clusterSize, podMembersToKill)
}

// Create couchbase cluster with Persistent volumes and server groups
// Kill one couchbase server pod from each server group
// Operator should replace killed pods with new one with same name and reuse PVC
func TestPersistentVolumeRzaNodesKilled(t *testing.T) {
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	clusterSize := e2eutil.MustNumNodes(t, targetKube)
	pvcName := "couchbase"

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 30
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		VolumeMounts: &couchbasev2.VolumeMounts{
			DefaultClaim: pvcName,
			DataClaim:    pvcName,
			IndexClaim:   pvcName,
		},
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// Create a expected RZA results map for verification
	expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	// kill the first N pods where N is the no of server groups
	victims := []int{}
	for i := 0; i < len(availableServerGroups); i++ {
		victims = append(victims, i)
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, false)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Cross check rza deployment matches the expected values
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	expectedEvents := []eventschema.Validatable{
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
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	clusterSize := e2eutil.MustNumNodes(t, targetKube)
	pvcName := "couchbase"

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 30
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	testCouchbase.Spec.ClusterSettings.AutoFailoverServerGroup = true
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		VolumeMounts: &couchbasev2.VolumeMounts{
			DefaultClaim: pvcName,
			DataClaim:    pvcName,
			IndexClaim:   pvcName,
		},
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Create a expected RZA results map for verification
	expected := mustGetExpectedRzaResultMap(t, targetKube, clusterSize)
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	// Kill nodes in 1st server group
	victimGroup := 0
	victims := []int{}
	for i := 0; i < clusterSize; i++ {
		if i%len(availableServerGroups) == victimGroup {
			victims = append(victims, i)
		}
	}

	// Loop to kill the nodes
	for _, podMemberToKill := range victims {
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, true)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Cross check rza deployment matches the expected values
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	expectedEvents := []eventschema.Validatable{
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

// Create 3 node couchbase cluster initially
// Resize cluster to different size
// Check for PVC status and cluster health condition
func TestPersistentVolumeResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Static configuration.
	clusterSize := 3

	pvcName := "couchbase"

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = 30
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		VolumeMounts: &couchbasev2.VolumeMounts{
			DefaultClaim: pvcName,
			DataClaim:    pvcName,
			IndexClaim:   pvcName,
		},
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	expectedPvcMap := map[string]int{}
	resizeClusterSizes := []int{2, 5, 1, 3}
	for _, clusterSize = range resizeClusterSizes {
		service := 0

		testCouchbase = e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, testCouchbase, 10*time.Minute)

		// Populate the expectedPvcMap for maximum available nodes everytime
		for memberId := 0; memberId < 9; memberId++ {
			memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, memberId)
			expectedPvcMap[memberName] = 0
		}
		podList, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + testCouchbase.Name})
		if err != nil {
			e2eutil.Die(t, fmt.Errorf("Failed to fetch pod list: %v", err))
		}
		for _, pod := range podList.Items {
			expectedPvcMap[pod.Name] = 3
		}

		// To cross check number of persistent vol claims matches the defined spec
		mustVerifyPvcMappingForPods(t, targetKube, f.Namespace, expectedPvcMap)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.ClusterScaleDownSequence(1),
		e2eutil.ClusterScaleUpSequence(3),
		e2eutil.ClusterScaleDownSequence(4),
		e2eutil.ClusterScaleUpSequence(2),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
