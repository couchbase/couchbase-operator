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

	return retryutil.RetryOnErr(ctx, 5*time.Second, pvcMappingVerify)
}

func mustVerifyPvcMappingForPods(t *testing.T, k8s *types.Cluster, namespace string, expectedPvcMap map[string]int) {
	if err := verifyPvcMappingForPods(t, k8s, namespace, expectedPvcMap); err != nil {
		e2eutil.Die(t, err)
	}
}

// TestPersistentVolumeAutoFailover tests couchbase server can failover a node
// with PV backing and the operator can reconcile.
func TestPersistentVolumeAutoFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Static configuration.
	clusterSize := 3
	victim := 1

	// Create the cluster.
	pvcName := "couchbase"
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(10)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)
	time.Sleep(30 * time.Second) // Allow bucket to warm up before killing anything

	// When ready terminate a node, expect Server to auto failover and the operator
	// to replace the node.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, victim), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events are as expected:
	// * Cluster created
	// * Node goes down, fails over and is replaced
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownFailedWithPVCRecoverySequence(1),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestPersistentVolumeAutoFailover tests the operator can recover multiple
// nodes when Server cannot auto failover.
func TestPersistentVolumeAutoRecovery(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Static configuration.
	clusterSize := 6
	victim1 := 1
	victim2 := 5

	// Create the cluster.
	pvcName := "couchbase"
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// When ready terminate the victims, expect the Operator auto recover the nodes.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim1, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim2, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, victim1), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events are as expected:
	// * Cluster created
	// * Nodes goes down, operator recovers
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, 2),
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

// TestPersistentVolumeKillAllPods tests the operator can recover a cluster
// when all pods go down simultaneously.
func TestPersistentVolumeKillAllPods(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	pvcName := "couchbase"
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// When ready kill all pods while the operator is down.  Upon restart expect
	// the operator to recover a single node, then manually recover the others.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 0, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 1, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 2, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, 1), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the results are as expected:
	// * Cluster created
	// * Single pod is recovered
	// * Remaining pods are manually recovered after auto-failover timeout
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestPersistentVolumeKillPodAndOperator tests the operator is able to handle a single
// down node after a restart.
func TestPersistentVolumeKillPodAndOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Static configuration.
	clusterSize := 4
	victim := 1

	// Create the cluster.
	pvcName := "couchbase"
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)
	time.Sleep(30 * time.Second) // Allow bucket to warm up before killing anything

	// When ready delete the victim node while the operator is down.  On restart
	// Server should failover the node and the operator recovers the cluster.
	e2eutil.MustDeleteCouchbaseOperator(t, targetKube, f.Namespace)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, victim), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the results are as expected:
	// * Cluster created
	// * Victim goes down, fails over and is replaced.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownFailedWithPVCRecoverySequence(1),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestPersistentVolumeKillAllPodsAndOperator tests the operator can handle all nodes
// down after a restart.
func TestPersistentVolumeKillAllPodsAndOperator(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	pvcName := "couchbase"
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// When ready kill all pods while the operator is down.  Upon restart expect
	// the operator to recover a single node, then manually recover the others.
	e2eutil.MustDeleteCouchbaseOperator(t, targetKube, f.Namespace)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 0, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 1, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 2, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, 1), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the results are as expected:
	// * Cluster created
	// * Single pod is recovered
	// * Remaining pods are manually recovered after auto-failover timeout
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestPersistentVolumeRzaNodesKilled tests operator recovery of pods spanning
// multiple server groups.
func TestPersistentVolumeRzaNodesKilled(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube) / len(availableServerGroups) * len(availableServerGroups)

	// Create the cluster.
	pvcName := "couchbase"
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		createPersistentVolumeClaimSpec(t, targetKube, f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

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

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, victims[0]), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Cross check rza deployment matches the expected values
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	// Check the results are as expected:
	// * Cluster created
	// * Single pod is recovered
	// * Remaining pods are manually recovered after auto-failover timeout
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, len(victims)),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestPersistentVolumeRzaNodesKilledUnbalanced tests operator recovery of pods spanning
// multiple server groups. However with this test the groups are unbalanced.  So we have
// A:0,3 B:1 C:2, killing 0, 1, 2 leaves a pod alive in A.  The scheduler will want to
// schedule pod 0 into B to keep things balanced, however it needs to back into A as that
// is where the volumes are.
func TestPersistentVolumeRzaNodesKilledUnbalanced(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	if !supportsMultipleVolumeClaims(t, targetKube) {
		t.Skip("storage class unsupported")
	}

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)

	// Static configuration.
	clusterSize := len(availableServerGroups) + 1

	// Create the cluster.
	pvcName := "couchbase"
	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
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

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, victims[0]), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

	// Cross check rza deployment matches the expected values
	expected.mustValidateRzaMap(t, targetKube, testCouchbase)

	// Check the results are as expected:
	// * Cluster created
	// * Single pod is recovered
	// * Remaining pods are manually recovered after auto-failover timeout
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownWithPVCRecoverySequence(clusterSize, len(victims)),
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

	pvcName := "couchbase"

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)

	clusterSize := e2eutil.MustNumNodes(t, targetKube) / len(availableServerGroups) * len(availableServerGroups)

	e2eutil.MustNewBucket(t, targetKube, f.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	testCouchbase.Spec.ClusterSettings.AutoFailoverServerGroup = true
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
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
		e2eutil.PodDownFailedWithPVCRecoverySequence(len(victims)),
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
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
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
		for memberID := 0; memberID < 9; memberID++ {
			memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, memberID)
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
