package e2e

import (
	"strconv"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPodResourcesBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(0.7*maxMem)) + "Mi"
	memLimit := strconv.Itoa(int(0.8*maxMem)) + "Mi"

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memReq),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memLimit),
			},
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(0.8*maxMem)) + "Mi"
	memLimit := strconv.Itoa(int(0.7*maxMem)) + "Mi"

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memReq),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memLimit),
			},
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, f.Namespace, testCouchbase)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, 15*time.Minute)
}

// TestPodResourcesCannotBePlaced tests for additional pods failing creation due to
// resource starvation.
// 1. Get the minimum memory on any node, set pods to reserve this amount
// 2. Calculate the maximum number of pods which can be allocated on the k8s cluster
// 3. Create a Couchbase cluster with this number of pods, saturating memory
// 4. Try scaling up by one pod
// 5. Expect to see an event indicating that pod creation failed
func TestPodResourcesCannotBePlaced(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	minMem := e2eutil.MustGetMinNodeMem(t, targetKube)
	memoryRequest := 0.9 * minMem
	memReq := strconv.Itoa(int(memoryRequest)) + "Mi"
	clusterSize := e2eutil.MustGetMaxScale(t, targetKube, memoryRequest)

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memReq),
			},
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// When the cluster is ready, scale up, the node shouldn't be scheduled.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize), time.Minute)

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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 1
	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(2*maxMem)) + "Mi"

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.Servers[0].Pod = &couchbasev2.PodPolicy{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memReq),
			},
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, f.Namespace, testCouchbase)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, 15*time.Minute)
}

func TestAntiAffinityOn(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube)

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.AntiAffinity = true
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube) + 1

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.AntiAffinity = true
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, f.Namespace, testCouchbase)

	// Wait for a healthy status.
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize-1), 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		eventschema.Repeat{Times: clusterSize - 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberCreationFailed},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestAntiAffinityOnCannotBeScaled(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube)

	// Create the cluster.
	testCouchbase := e2espec.NewBasicClusterSpec(clusterSize)
	testCouchbase.Spec.AntiAffinity = true
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, f.Namespace, testCouchbase)

	// When ready scale beyond the limits and wait for a failed creation.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, clusterSize+1, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, clusterSize), 5*time.Minute)

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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := e2eutil.MustNumNodes(t, targetKube) + 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize)

	// Wait for a healthy status.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
