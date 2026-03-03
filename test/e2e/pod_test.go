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

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// This is broken because the memory allocation stuff cannot be trusted.
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, kubernetes)
	memReq := strconv.Itoa(int(0.7*maxMem)) + "Mi"
	memLimit := strconv.Itoa(int(0.8*maxMem)) + "Mi"

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memLimit),
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestNegPodResourcesBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// This is broken because the memory allocation stuff cannot be trusted.
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, kubernetes)
	memReq := strconv.Itoa(int(0.8*maxMem)) + "Mi"
	memLimit := strconv.Itoa(int(0.7*maxMem)) + "Mi"

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memLimit),
		},
	}
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, 0), 15*time.Minute)
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

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// Put this on a big cluster and it's going to literally take it over with
	// hundreds of pods.  Poor test, we should constrain it with label selectors
	// to limit the blast radius.  Also the memory calculations are just wrong.
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	minMem := e2eutil.MustGetMinNodeMem(t, kubernetes)
	memoryRequest := 0.9 * minMem
	memReq := strconv.Itoa(int(memoryRequest)) + "Mi"
	clusterSize := e2eutil.MustGetMaxScale(t, kubernetes, memoryRequest)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, scale up, the node shouldn't be scheduled.
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+1, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, clusterSize), 2*f.PodCreateTimeout)

	// Check the events match what we expect:
	// * N-1 members added
	// * Final member fails to be created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestFirstNodePodResourcesCannotBePlaced(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// This is broken because the memory allocation stuff cannot be trusted.
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, kubernetes)
	memReq := strconv.Itoa(int(2*maxMem)) + "Mi"

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(memReq),
		},
	}
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, 0), 15*time.Minute)
}

func TestAntiAffinityOn(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// This test is broken because we cannot guarantee the N node cluster will
	// fit when CPU and memory constraints are taken into account.
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, kubernetes)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.AntiAffinity = true
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestAntiAffinityOnCannotBePlaced(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// This is broken because it doesn't consider memory allocation.
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodesAbsolute(t, kubernetes) + 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.AntiAffinity = true
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// Wait for a healthy status.
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, clusterSize-1), 2*f.PodCreateTimeout)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		eventschema.RepeatAtMost{Times: clusterSize - 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestAntiAffinityOnCannotBeScaled(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// This is broken because it doesn't consider memory allocation.
	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, kubernetes)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.AntiAffinity = true
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When ready scale beyond the limits and wait for a failed creation.
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+1, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, clusterSize), 2*f.PodCreateTimeout)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestAntiAffinityOff(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster()

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, kubernetes) + 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for a healthy status.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCustomAnnotationsAndLabelsStayAfterReconcile(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)

	// Add a custom annotation and label to the pod, wait for reconcile and then confirm the annotations are still present
	newAnnotations := make(map[string]string)
	newAnnotations["special.istio.annotation"] = "true"

	newLabels := make(map[string]string)
	newLabels["special.istio.label"] = "true"

	// Add custom annotations and labels
	e2eutil.MustAddCustomAnnotationAndLabels(t, kubernetes, cluster, newAnnotations, newLabels)
	// Wait for reconcile period to pass
	time.Sleep(time.Minute)
	// Ensure we still have them after waiting for a reconcile period to pass
	e2eutil.MustCheckCustomAnnotationAndLabels(t, kubernetes, cluster, newAnnotations, newLabels)
}
