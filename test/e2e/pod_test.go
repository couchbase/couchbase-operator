/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPodResourcesBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(0.7*maxMem)) + "Mi"
	memLimit := strconv.Itoa(int(0.8*maxMem)) + "Mi"

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memLimit),
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestNegPodResourcesBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(0.8*maxMem)) + "Mi"
	memLimit := strconv.Itoa(int(0.7*maxMem)) + "Mi"

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memLimit),
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, testCouchbase)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, 0), 15*time.Minute)
}

// TestPodResourcesCannotBePlaced tests for additional pods failing creation due to
// resource starvation.
// 1. Get the minimum memory on any node, set pods to reserve this amount
// 2. Calculate the maximum number of pods which can be allocated on the k8s cluster
// 3. Create a Couchbase cluster with this number of pods, saturating memory
// 4. Try scaling up by one pod
// 5. Expect to see an event indicating that pod creation failed.
func TestPodResourcesCannotBePlaced(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	minMem := e2eutil.MustGetMinNodeMem(t, targetKube)
	memoryRequest := 0.9 * minMem
	memReq := strconv.Itoa(int(memoryRequest)) + "Mi"
	clusterSize := e2eutil.MustGetMaxScale(t, targetKube, memoryRequest)

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// When the cluster is ready, scale up, the node shouldn't be scheduled.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize), 2*f.PodCreateTimeout)

	// Check the events match what we expect:
	// * N-1 members added
	// * Final member fails to be created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestFirstNodePodResourcesCannotBePlaced(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(2*maxMem)) + "Mi"

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, testCouchbase)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, 0), 15*time.Minute)
}

func TestAntiAffinityOn(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube)

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.AntiAffinity = true
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestAntiAffinityOnCannotBePlaced(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodesAbsolute(t, targetKube) + 1

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.AntiAffinity = true
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, testCouchbase)

	// Wait for a healthy status.
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize-1), 2*f.PodCreateTimeout)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		eventschema.RepeatAtMost{Times: clusterSize - 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestAntiAffinityOnCannotBeScaled(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube)

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).Generate(targetKube)
	testCouchbase.Spec.AntiAffinity = true
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// When ready scale beyond the limits and wait for a failed creation.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize), 2*f.PodCreateTimeout)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestAntiAffinityOff(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, targetKube).StaticCluster()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube) + 1

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	// Wait for a healthy status.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestCustomAnnotationsAndLabelsStayAfterReconcile(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create the cluster.
	testCouchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, targetKube)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)

	// Add a custom annotation and label to the pod, wait for reconcile and then confirm the annotations are still present
	newAnnotations := make(map[string]string)
	newAnnotations["special.istio.annotation"] = "true"

	newLabels := make(map[string]string)
	newLabels["special.istio.label"] = "true"

	// Add custom annotations and labels
	e2eutil.MustAddCustomAnnotationAndLabels(t, targetKube, testCouchbase, newAnnotations, newLabels)
	// Wait for reconcile period to pass
	time.Sleep(time.Minute)
	// Ensure we still have them after waiting for a reconcile period to pass
	e2eutil.MustCheckCustomAnnotationAndLabels(t, targetKube, testCouchbase, newAnnotations, newLabels)
}
