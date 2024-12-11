package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	pkgconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestDenyCommunityEdition tries installing with a CE version of Couchbase server
// and expects it not to work.
func TestDenyCommunityEdition(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Don't run this on Openshift etc. as there is no community edition.
	skipEnterpriseOnlyPlatform(t)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Image = pkgconstants.CommunityEditionImage
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberCreationFailedEvent(cluster, 0), 15*time.Minute)
}

// Tests editing service spec
// 1. Create 1 node cluster with single service spec
// 2. Update service spec size from 1 to 2 (verify via rest call to cluster).
func TestEditServiceConfig(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When ready update the server class size and wait for the cluster to be scaled.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", clusterSize+1), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster is scaled up.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleUpSequence(constants.Size1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests manual failover and operator recovery of cluster
// 1. Create 2 node cluster
// 2. Manually failover 1 member
// 3. Wait for operator to add back failed node.
func TestNodeManualFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size2

	// create 2 node cluster with admin console
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// When ready failover a node, expect it to be added back in.
	e2eutil.MustFailoverNode(t, kubernetes, cluster, 0, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member balanced back in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestNodeRecoveryAfterMemberAdd tests killing a node during a scale up.
func TestNodeRecoveryAfterMemberAdd(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	scaleSize := constants.Size5
	triggerIndex := 3
	victimIndex := 1

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready begin scaling up.  When the third new member is added
	// kill the victim node.  Expect the cluster to become healthy again.
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, scaleSize, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, triggerIndex), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * New nodes added
	// * Rebalance starts and fails
	// * Victim failed add
	// * New node added and rebalanced in
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: scaleSize - clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests scenario where the node being added to is killed before it can be
// rebalanced in.
//
// Expects: autofailover of down node occurs and a replacement node is added
// in order to reach desired cluster size.
func TestNodeRecoveryKilledNewMember(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1
	scaleSize := 3
	victimIndex := 2

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When ready scale the cluster and kill the victim as it is added.  Expect
	// the operator to replace it and balance it in.
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, scaleSize, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, kubernetes, cluster, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster scaled up
	// * Node goes down during a rebalance
	// * Node is failedover and replaced
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: scaleSize - clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestKillNodesAfterRebalanceAndFailover tests repeated pod termination at
// different points in a scale up operation.
func TestKillNodesAfterRebalanceAndFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	scaledClusterSize := constants.Size3
	victim1Index := scaledClusterSize - 1
	victim2Index := scaledClusterSize

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Runtime configuration.
	victim1Name := couchbaseutil.CreateMemberName(cluster.Name, victim1Index)
	victim2Name := couchbaseutil.CreateMemberName(cluster.Name, victim2Index)

	// When the cluster is healthy, resize to the target size.  When the first victim starts balancing in
	// kill it.  When the second victim is created to replace the dead node, kill it too.  Cluster should
	// end up healthy.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	cluster = e2eutil.MustResizeClusterNoWait(t, 0, scaledClusterSize, kubernetes, cluster)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, kubernetes, cluster, 25.0, 2*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim1Index, true)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victim2Index), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victim2Index, true)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Scale up begins
	// * Rebalance fails
	// * First victim node goes down and fails over
	// * New node is created to replace the first victim
	// * Rebalance fails
	// * Second victim fails to add
	// * Another new node is created and balanced in and the first victim is removed.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: scaledClusterSize - clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		// The operator may miss seeing this due to network timeouts
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victim1Name}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victim1Name},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victim2Name},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted}},
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete}},
		eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victim2Name},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victim1Name},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Test that a foreign node is removed from cluster
//
// Expects: only nodes added by operator to be in cluster
// 1. Create 1 node cluster
// 2. Manually add 1 external member to cluster
// 3. Verify that external member was removed.
func TestRemoveForeignNode(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Runtime configuration.
	foreignNodeName := cluster.Name + "-hrisovalantis" // (this is Greek ;p)
	member := couchbaseutil.NewMember(kubernetes.Namespace, cluster.Name, foreignNodeName, "", cluster.Spec.Servers[0].Name, false)

	// When ready create and add a new node, expect the operator to remove it
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", true), time.Minute)
	e2eutil.MustAddNode(t, kubernetes, cluster, cluster.Spec.Servers[0].Services, member)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/paused", false), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Foreign node is ejected.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests one node failing in a cluster with no buckets
// 1. Create a 5 node cluster with no buckets
// 2. Kill a single node
// 3. Wait for autofailover, rebalance, and healthy.
func TestRecoveryAfterOnePodFailureNoBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 5
	victimIndex := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Kill a single pod and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pod goes down and fails over
	// * Replacement is balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests two nodes failing in a cluster with no buckets
// 1. Create 5 node cluster with no buckets
// 2. Kill two nodes
// 3. Manually failover the killed nodes
// 4. Wait for rebalance and healthy cluster.
func TestRecoveryAfterTwoPodFailureNoBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 5
	victimIndex1 := 0
	victimIndex2 := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Kill a two pods and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex1, true)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex2, true)
	e2eutil.MustWaitForUnhealthyNodes(t, kubernetes, cluster, 2, time.Minute)
	e2eutil.MustFailoverNodes(t, kubernetes, cluster, []int{victimIndex1, victimIndex2}, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pods go down and fail over
	// * Replacements are balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests one nodes failing in a cluster with one bucket with one replica
// 1. Create 5 node cluster with one bucket with 1 replica
// 2. Kill one node
// 3. Wait for rebalance and healthy cluster.
func TestRecoveryAfterOnePodFailureBucketOneReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 5
	victimIndex := 1

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Kill a single pod and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pod goes down and fails over
	// * Replacement is balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests two nodes failing in a cluster with one bucket with one replica
// 1. Create 5 node cluster with one bucket with 1 replica
// 2. Kill two nodes
// 3. Manually failover the two killed nodes
// 4. Wait for rebalance and healthy cluster.
func TestRecoveryAfterTwoPodFailureBucketOneReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 5
	victimIndex1 := 0
	victimIndex2 := 1

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(120)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Kill a two pods and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex1, true)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex2, true)
	e2eutil.MustWaitForUnhealthyNodes(t, kubernetes, cluster, 2, 3*time.Minute)
	e2eutil.MustFailoverNodes(t, kubernetes, cluster, []int{victimIndex1, victimIndex2}, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pods go down and fail over
	// * Replacements are balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{
			Validator: eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests one node failing in a cluster with one bucket with two replicas
// 1. Create 5 node cluster with one bucket with two replicas
// 2. Kill one node
// 3. Wait for rebalance and healthy cluster.
func TestRecoveryAfterOnePodFailureBucketTwoReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 5
	victimIndex := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, kubernetes, e2espec.DefaultBucketTwoReplicas())
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.AutoFailoverTimeout = e2espec.NewDurationS(120)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, e2espec.DefaultBucketTwoReplicas(), time.Minute)

	// Kill a single pod and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pod goes down and fails over
	// * Replacement is balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// Tests two nodes failing in a cluster with one bucket with two replicas
// 1. Create 5 node cluster with one bucket with two replicas
// 2. Kill two nodes
// 3. Manually failover the two killed nodes
// 4. Wait for rebalance and healthy cluster.
func TestRecoveryAfterTwoPodFailureBucketTwoReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 5
	victimIndex1 := 0
	victimIndex2 := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, kubernetes, e2espec.DefaultBucketTwoReplicas())
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, e2espec.DefaultBucketTwoReplicas(), time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, e2espec.DefaultBucket().Name)()

	// Kill a two pods and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex1, true)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex2, true)
	e2eutil.MustWaitForUnhealthyNodes(t, kubernetes, cluster, 2, time.Minute)
	e2eutil.MustFailoverNodes(t, kubernetes, cluster, []int{victimIndex1, victimIndex2}, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pods go down and fail over
	// * Replacements are balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Optional{
			Validator: eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestRecoveryAfterOneNsServerFailureBucketOneReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size5
	victimIndex := 0

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Runtime configuration
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When ready kill the Couchbase Server process and await recovery
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustKillCouchbaseService(t, kubernetes, victimName, f.KubeType)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member goes down and fails over
	// * New member balanced in to replace the failed one
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestAutoRecoveryEphemeralWithNoAutofailover tests that a single replica with
// two nodes down (data loss), ignores server when we tell it to and just nukes the
// pods in preference of having something working and not broken.
func TestAutoRecoveryEphemeralWithNoAutofailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 5
	victimIndex1 := 0
	victimIndex2 := 1
	recoveryPolicy := couchbasev2.PrioritizeUptime

	// Single replica bucket.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.RecoveryPolicy = &recoveryPolicy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, bucket.GetName())()

	// Kill a two pods and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex1, true)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex2, true)
	e2eutil.MustWaitForUnhealthyNodes(t, kubernetes, cluster, 2, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
}

func TestGracefulShutdown(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0")

	clusterSize := constants.Size1

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	victimID := 0
	victimeName := couchbaseutil.CreateMemberName(cluster.Name, victimID)

	output := &e2eutil.ExecOutput{}

	asyncOp := e2eutil.MustStartExecCommandOnPod(t, kubernetes, victimeName, "tail -f /opt/couchbase/var/lib/couchbase/logs/memcached.log.000000.txt", output, 5*time.Minute)
	defer asyncOp.Cancel()

	// Give the exec some time to start
	time.Sleep(15 * time.Second)

	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimID, false)

	if err := asyncOp.WaitForCompletion(); err != nil && !strings.Contains(err.Error(), "terminated with exit code 137") {
		e2eutil.Die(t, err)
	}

	if !strings.Contains(output.Stdout, "Gracefully shutting down") {
		fmt.Println("stderr:")
		fmt.Print(output.Stderr)
		fmt.Println("stdout:")
		fmt.Print(output.Stdout)
		e2eutil.Die(t, fmt.Errorf("stdout does not contain 'Gracefully shutting down'"))
	}
}
