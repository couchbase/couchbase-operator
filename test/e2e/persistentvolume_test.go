/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This will create a Persistent volume claim data
// for adding into the cluster CRD.
func createPersistentVolumeClaimSpec(storageClass string, pvcName string, resourceQtyVal int64) couchbasev2.PersistentVolumeClaimTemplate {
	resourceQuantity := apiresource.NewQuantity(resourceQtyVal*1024*1024*1024, apiresource.BinarySI)

	pvc := couchbasev2.PersistentVolumeClaimTemplate{
		ObjectMeta: couchbasev2.NamedObjectMeta{
			Name: pvcName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": *resourceQuantity,
				},
			},
		},
	}

	if storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}

	return pvc
}

// Verifies actual PVC wrt to server pods matches the expected PVC mapping given by user.
func verifyPvcMappingForPods(t *testing.T, k8s *types.Cluster, expectedPvcMap map[string]int) (errToReturn error) {
	pvcMappingVerify := func() error {
		for memberName, pvcCount := range expectedPvcMap {
			pvcList, err := k8s.KubeClient.CoreV1().PersistentVolumeClaims(k8s.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "couchbase_node=" + memberName})
			if err != nil {
				return err
			} else if len(pvcList.Items) != pvcCount {
				t.Logf("Persistent volume claims not created as expected for %s. Has %d volume, expected %d", memberName, len(pvcList.Items), pvcCount)
				return fmt.Errorf("pvc mapping verification failed")
			}
		}

		return nil
	}

	return retryutil.RetryFor(5*time.Minute, pvcMappingVerify)
}

func mustVerifyPvcMappingForPods(t *testing.T, k8s *types.Cluster, expectedPvcMap map[string]int) {
	if err := verifyPvcMappingForPods(t, k8s, expectedPvcMap); err != nil {
		e2eutil.Die(t, err)
	}
}

// TestPersistentVolumeAutoFailover tests couchbase server can failover a node
// with PV backing and the operator can reconcile.
func TestPersistentVolumeAutoFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	victim := 1
	pvcName := "couchbase"

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(10)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, e2espec.DefaultBucketTwoReplicas(), time.Minute)
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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 6
	victim1 := 1
	victim2 := 5
	pvcName := "couchbase"

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// When ready terminate the victims, expect the Operator auto recover the nodes.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim1, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim2, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, victim1), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events are as expected:
	// * Cluster created
	// * Nodes go down, operator recovers in one of many different ways...
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.AnyOf{
			Validators: []eventschema.Validatable{
				e2eutil.PodDownWithPVCRecoverySequence(clusterSize, 2),
				eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Repeat{
							Times:     2,
							Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
						},
						eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
						eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
						eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
					},
				},
			},
		},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create multi-node couchbase cluster with volumeClaimTemplates.
// Create test bucket and verify.
func TestPersistentVolumeCreateCluster(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2

	// Create a basic supportable cluster with 2 stateful and 2 stateless nodes
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)
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
	mustVerifyPvcMappingForPods(t, kubernetes, expectedPvcMap)
}

// TestPersistentVolumeKillAllPods tests the operator can recover a cluster
// when all pods go down simultaneously.
func TestPersistentVolumeKillAllPods(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	pvcName := "couchbase"

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

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

// TestPersistentVolumeKillAllPodsTLS tests the operator can recover from a total failure
// with TLS enabled.
func TestPersistentVolumeKillAllPodsTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	pvcName := "couchbase"

	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, targetKube, &e2eutil.TLSOpts{})
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Name = ctx.ClusterName
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 4
	victim := 1
	pvcName := "couchbase"

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	time.Sleep(30 * time.Second) // Allow bucket to warm up before killing anything

	// When ready delete the victim node while the operator is down.  On restart
	// Server should failover the node and the operator recovers the cluster.
	e2eutil.MustDeleteCouchbaseOperator(t, targetKube)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	pvcName := "couchbase"

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(60)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// When ready kill all pods while the operator is down.  Upon restart expect
	// the operator to recover a single node, then manually recover the others.
	e2eutil.MustDeleteCouchbaseOperator(t, targetKube)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 0, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 1, false)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 2, false)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberDownEvent(testCouchbase, 1), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 10*time.Minute)

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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster().ServerGroups(2)

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)

	// Static configuration.
	clusterSize := len(availableServerGroups) * 2
	pvcName := "couchbase"

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(60)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	// Create a expected RZA results map for verification
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
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

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ServerGroups(2)

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)

	// Static configuration.
	clusterSize := len(availableServerGroups) + 1
	pvcName := "couchbase"

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Create a expected RZA results map for verification
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
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

// Create couchbase cluster with Persistent volumes and server groups.
// Kill couchbase server pods on a particular server group.
// Operator should replace killed pods with new one with same name and reuse PVC.
func TestPersistentVolumeRzaFailover(t *testing.T) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster().ServerGroups(2)

	pvcName := "couchbase"

	// Create cluster spec for RZA feature
	availableServerGroups := getAvailabilityZones(t, targetKube)
	clusterSize := len(availableServerGroups) * 2

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(60)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 2
	testCouchbase.Spec.ClusterSettings.AutoFailoverServerGroup = true
	testCouchbase.Spec.ServerGroups = availableServerGroups
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	// Create a expected RZA results map for verification
	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
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
		e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, podMemberToKill, false)
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

// Create 3 node couchbase cluster initially.
// Resize cluster to different size.
// Check for PVC status and cluster health condition.
func TestPersistentVolumeResizeCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	pvcName := "couchbase"

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(30)
	testCouchbase.Spec.ClusterSettings.AutoFailoverMaxCount = 3
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
		IndexClaim:   pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

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

		podList, err := targetKube.KubeClient.CoreV1().Pods(targetKube.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + testCouchbase.Name})
		if err != nil {
			e2eutil.Die(t, fmt.Errorf("Failed to fetch pod list: %w", err))
		}

		for _, pod := range podList.Items {
			expectedPvcMap[pod.Name] = 3
		}

		// To cross check number of persistent vol claims matches the defined spec
		mustVerifyPvcMappingForPods(t, targetKube, expectedPvcMap)
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

// Create cluster with online volume resize enabled.
// Resize persistent volumes.
// Check volumes resized without Pod upgrade.
func TestOnlinePersistentVolumeResize(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ExpandableStorage()

	// Static configuration.
	clusterSize := 1
	pvcName := "couchbase"

	// Create cluster with Online Resizing Enabled
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.EnableOnlineVolumeExpansion = true
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Resize up to 3Gi
	requestedQuantity := e2espec.NewResourceQuantityGi(3)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", requestedQuantity), time.Minute)

	// Verify resize state
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcName, requestedQuantity, 5*time.Minute)

	// Events indirectly verify that upgrade did not occur
	// since no scale up events should be present
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 2 * clusterSize, Validator: e2eutil.VolumeExpansionSuccessSequence()},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create MDS cluster with online volume resize enabled for single server group.
// Resize and verify volumes resized only for specified sever group.
func TestOnlinePersistentVolumeResizeMDS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ExpandableStorage()

	// Static configuration.
	groupSize := 1
	pvcDataName := "couchbase_data"
	pvcIndexName := "couchbase_index"
	secondConfigName := "test_config_2"

	// Define cluster with separate data service config
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithPersistentTopology(groupSize).Generate(targetKube)
	testCouchbase.Spec.EnableOnlineVolumeExpansion = true
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcDataName,
		DataClaim:    pvcDataName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcDataName, 2),
	}

	// Add standalone index service as separate config with separate claim templates
	newService := couchbasev2.ServerConfig{
		Size:     groupSize,
		Name:     secondConfigName,
		Services: couchbasev2.ServiceList{couchbasev2.IndexService},
	}
	testCouchbase.Spec.Servers = append(testCouchbase.Spec.Servers, newService)
	testCouchbase.Spec.Servers[1].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcIndexName,
		IndexClaim:   pvcIndexName,
	}
	pvcIndexClaimTemplate := createPersistentVolumeClaimSpec(f.StorageClassName, pvcIndexName, 2)
	testCouchbase.Spec.VolumeClaimTemplates = append(testCouchbase.Spec.VolumeClaimTemplates, pvcIndexClaimTemplate)

	// Create cluster
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Resize and data services from 2Gi to 3Gi
	requestedQuantity := e2espec.NewResourceQuantityGi(3)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", requestedQuantity), time.Minute)
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcDataName, requestedQuantity, 5*time.Minute)
	// Verify index service volumes are still 2Gi
	requestedQuantity = e2espec.NewResourceQuantityGi(2)
	memberName = couchbaseutil.CreateMemberName(testCouchbase.Name, 1)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcIndexName, requestedQuantity, 5*time.Minute)
	// Scale index service volumes to 4Gi
	requestedQuantity = e2espec.NewResourceQuantityGi(4)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/1/spec/resources/requests/storage", requestedQuantity), time.Minute)
	memberName = couchbaseutil.CreateMemberName(testCouchbase.Name, 1)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcIndexName, requestedQuantity, 6*time.Minute)

	// Verify events
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(groupSize * 2),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 4 * groupSize, Validator: e2eutil.VolumeExpansionSuccessSequence()},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create cluster with different claim templates for different services.
func TestOnlinePersistentVolumeResizeMixedClaims(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ExpandableStorage()

	// Static configuration.
	groupSize := 1
	pvcDataName := "couchbase_data"
	pvcIndexName := "couchbase_index"

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithPersistentTopology(groupSize).Generate(targetKube)
	testCouchbase.Spec.EnableOnlineVolumeExpansion = true
	// Define different claims for data and index service
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcDataName,
		DataClaim:    pvcDataName,
		IndexClaim:   pvcIndexName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcDataName, 2),
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcIndexName, 2),
	}

	// Create cluster
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Resize and data services from 2Gi to 3Gi
	requestedQuantity := e2espec.NewResourceQuantityGi(3)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", requestedQuantity), time.Minute)
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcDataName, requestedQuantity, 5*time.Minute)
	// Verify index service volumes are still 2Gi
	requestedQuantity = e2espec.NewResourceQuantityGi(2)
	memberName = couchbaseutil.CreateMemberName(testCouchbase.Name, 1)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcIndexName, requestedQuantity, 5*time.Minute)

	// Verify events
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(groupSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 2 * groupSize, Validator: e2eutil.VolumeExpansionSuccessSequence()},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Create cluster with volume expansion enabled and verify that duplicate size
// requests are treated as no-op and do not result in additional events.
func TestOnlinePersistentVolumeResizeNop(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ExpandableStorage()

	// Static configuration.
	groupSize := 1
	pvcDataName := "couchbase_data"
	pvcIndexName := "couchbase_index"

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithPersistentTopology(groupSize).Generate(targetKube)
	testCouchbase.Spec.EnableOnlineVolumeExpansion = true
	// Define different claims for data and index service
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcDataName,
		DataClaim:    pvcDataName,
		IndexClaim:   pvcIndexName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcDataName, 2),
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcIndexName, 2),
	}

	// Create cluster
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Resize and data services from 2Gi to 2Gi
	requestedQuantity := e2espec.NewResourceQuantityGi(2)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", requestedQuantity), time.Minute)
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcDataName, requestedQuantity, 5*time.Minute)

	// Verify events
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(groupSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestOnlinePersistentVolumeResizeWithDocs(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ExpandableStorage()

	// Static configuration.
	clusterSize := 1
	pvcName := "couchbase"
	numOfDocs := f.DocsCount

	// Create cluster with Online Resizing Enabled
	bucket := e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.EnableOnlineVolumeExpansion = true
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Resize up to 3Gi
	requestedQuantity := e2espec.NewResourceQuantityGi(3)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", requestedQuantity), time.Minute)

	// Start Adding Docs
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).MustCreate(t, targetKube, testCouchbase)

	// Verify resize state
	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcName, requestedQuantity, 5*time.Minute)

	// Check all docs were added during resize
	e2eutil.MustVerifyDocCountInBucket(t, targetKube, testCouchbase, bucket.GetName(), numOfDocs, time.Minute)

	// Events indirectly verify that upgrade did not occur
	// since no scale up events should be present
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 2 * clusterSize, Validator: e2eutil.VolumeExpansionSuccessSequence()},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestOnlinePersistentVolumeResizeWhenPodKilled brings down a pod during a PV resize, and checks that the resize
// is still correctly applied.
func TestOnlinePersistentVolumeResizeWhenPodKilled(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).ExpandableStorage()

	// Static configuration.
	clusterSize := 1
	pvcName := "couchbase"

	// Create cluster with Online Resizing Enabled
	bucket := e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas())
	testCouchbase := clusterOptions().WithPersistentTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.EnableOnlineVolumeExpansion = true
	testCouchbase.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
		DataClaim:    pvcName,
	}
	testCouchbase.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, 2),
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)
	e2eutil.MustWaitUntilBucketExists(t, targetKube, testCouchbase, bucket, time.Minute)

	time.Sleep(30 * time.Second) // Allow bucket to warm up before killing anything

	// Resize up to 3Gi
	requestedQuantity := e2espec.NewResourceQuantityGi(3)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", requestedQuantity), time.Minute)

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewVolumeExpandStartedEvent(k8sutil.NameForPersistentVolumeClaim(memberName, 1, "data"), "2Gi", "3Gi", testCouchbase), 2*time.Minute)

	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, 0, false)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Verify resize state
	e2eutil.MustWaitForPodVolumeSize(t, targetKube, memberName, pvcName, requestedQuantity, 10*time.Minute)

	// Verify pod recovery during expansion events
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonExpandVolumeStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
		eventschema.Event{Reason: k8sutil.EventReasonExpandVolumeSucceeded},
		eventschema.Event{Reason: k8sutil.EventReasonExpandVolumeStarted},
		eventschema.Event{Reason: k8sutil.EventReasonExpandVolumeSucceeded},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
