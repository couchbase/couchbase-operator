package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestDenyCommunityEdition tries installing with a CE version of Couchbase server
// and expects it not to work.
func TestDenyCommunityEdition(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Don't run this on Openshift etc. as there is no community edition.
	skipEnterpriseOnlyPlatform(t)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.Image = constants.CommunityEditionImage
	testCouchbase = e2eutil.MustNewClusterFromSpecAsync(t, targetKube, testCouchbase)

	// Expect the cluster to enter a failed state
	e2eutil.MustWaitClusterPhaseFailed(t, targetKube, testCouchbase, 15*time.Minute)
}

// Tests editing service spec
// 1. Create 1 node cluster with single service spec
// 2. Update service spec size from 1 to 2 (verify via rest call to cluster).
func TestEditServiceConfig(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// When ready update the server class size and wait for the cluster to be scaled.
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Size", clusterSize+1), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 2*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster is scaled up.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.ClusterScaleUpSequence(constants.Size1),
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests manual failover and operator recovery of cluster
// 1. Create 2 node cluster
// 2. Manually failover 1 member
// 3. Wait for operator to add back failed node.
func TestNodeManualFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size2

	// create 2 node cluster with admin console
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// When ready failover a node, expect it to be added back in.
	e2eutil.MustFailoverNode(t, targetKube, testCouchbase, 0, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Member balanced back in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestNodeRecoveryAfterMemberAdd tests killing a node during a scale up.
func TestNodeRecoveryAfterMemberAdd(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1
	scaleSize := constants.Size5
	triggerIndex := 3
	victimIndex := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When the cluster is ready begin scaling up.  When the third new member is added
	// kill the victim node.  Expect the cluster to become healthy again.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, scaleSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, triggerIndex), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests scenario where the node being added to is killed before it can be
// rebalanced in.
//
// Expects: autofailover of down node occurs and a replacement node is added
// in order to reach desired cluster size.
func TestNodeRecoveryKilledNewMember(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 1
	scaleSize := 3
	victimIndex := 2

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When ready scale the cluster and kill the victim as it is added.  Expect
	// the operator to replace it and balance it in.
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, scaleSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, targetKube, testCouchbase, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestKillNodesAfterRebalanceAndFailover tests repeated pod termination at
// different points in a scale up operation.
func TestKillNodesAfterRebalanceAndFailover(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1
	scaledClusterSize := constants.Size3
	victim1Index := scaledClusterSize - 1
	victim2Index := scaledClusterSize

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Runtime configuration.
	victim1Name := couchbaseutil.CreateMemberName(testCouchbase.Name, victim1Index)
	victim2Name := couchbaseutil.CreateMemberName(testCouchbase.Name, victim2Index)

	// When the cluster is healthy, resize to the target size.  When the first victim starts balancing in
	// kill it.  When the second victim is created to replace the dead node, kill it too.  Cluster should
	// end up healthy.
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, scaledClusterSize, targetKube, testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, targetKube, testCouchbase, 25.0, 2*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim1Index, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, victim2Index), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victim2Index, true)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victim2Name},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victim1Name},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Runtime configuration.
	foreignNodeName := testCouchbase.Name + "-hrisovalantis" // (this is Greek ;p)
	member := &couchbaseutil.Member{
		Name:         foreignNodeName,
		Namespace:    targetKube.Namespace,
		ClusterName:  testCouchbase.Name,
		ServerConfig: testCouchbase.Spec.Servers[0].Name,
	}

	// When ready create and add a new node, expect the operator to remove it
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", true), time.Minute)
	e2eutil.MustAddNode(t, targetKube, testCouchbase, testCouchbase.Spec.Servers[0].Services, member)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Paused", false), time.Minute)
	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, clusterSize, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Foreign node is ejected.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: foreignNodeName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests one node failing in a cluster with no buckets
// 1. Create a 5 node cluster with no buckets
// 2. Kill a single node
// 3. Wait for autofailover, rebalance, and healthy.
func TestRecoveryAfterOnePodFailureNoBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5
	victimIndex := 1

	// Create the cluster.
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Kill a single pod and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pod goes down and fails over
	// * Replacement is balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
		eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests two nodes failing in a cluster with no buckets
// 1. Create 5 node cluster with no buckets
// 2. Kill two nodes
// 3. Manually failover the killed nodes
// 4. Wait for rebalance and healthy cluster.
func TestRecoveryAfterTwoPodFailureNoBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5
	victimIndex1 := 0
	victimIndex2 := 1

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)

	// Kill a two pods and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex1, true)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex2, true)
	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, 2, time.Minute)
	e2eutil.MustFailoverNodes(t, targetKube, testCouchbase, []int{victimIndex1, victimIndex2}, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pods go down and fail over
	// * Replacements are balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests one nodes failing in a cluster with one bucket with one replica
// 1. Create 5 node cluster with one bucket with 1 replica
// 2. Kill one node
// 3. Wait for rebalance and healthy cluster.
func TestRecoveryAfterOnePodFailureBucketOneReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5
	victimIndex := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Kill a single pod and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests two nodes failing in a cluster with one bucket with one replica
// 1. Create 5 node cluster with one bucket with 1 replica
// 2. Kill two nodes
// 3. Manually failover the two killed nodes
// 4. Wait for rebalance and healthy cluster.
func TestRecoveryAfterTwoPodFailureBucketOneReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5
	victimIndex1 := 0
	victimIndex2 := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Kill a two pods and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex1, true)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex2, true)
	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, 2, time.Minute)
	e2eutil.MustFailoverNodes(t, targetKube, testCouchbase, []int{victimIndex1, victimIndex2}, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pods go down and fail over
	// * Replacements are balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests one node failing in a cluster with one bucket with two replicas
// 1. Create 5 node cluster with one bucket with two replicas
// 2. Kill one node
// 3. Wait for rebalance and healthy cluster.
func TestRecoveryAfterOnePodFailureBucketTwoReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5
	victimIndex := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucketTwoReplicas.Name}, time.Minute)

	// Kill a single pod and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex, true)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests two nodes failing in a cluster with one bucket with two replicas
// 1. Create 5 node cluster with one bucket with two replicas
// 2. Kill two nodes
// 3. Manually failover the two killed nodes
// 4. Wait for rebalance and healthy cluster.
func TestRecoveryAfterTwoPodFailureBucketTwoReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5
	victimIndex1 := 0
	victimIndex2 := 1

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucketTwoReplicas)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucketTwoReplicas.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Kill a two pods and wait for the cluster to recover.
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex1, true)
	e2eutil.MustKillPodForMember(t, targetKube, testCouchbase, victimIndex2, true)
	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, 2, time.Minute)
	e2eutil.MustFailoverNodes(t, targetKube, testCouchbase, []int{victimIndex1, victimIndex2}, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster is created
	// * Pods go down and fail over
	// * Replacements are balanced in
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestRecoveryAfterOneNsServerFailureBucketOneReplica(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size5
	victimIndex := 0

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	// Runtime configuration
	victimName := couchbaseutil.CreateMemberName(testCouchbase.Name, victimIndex)

	// When ready kill the Couchbase Server process and await recovery
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)
	e2eutil.MustKillCouchbaseService(t, targetKube, victimName, f.KubeType)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

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

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestRecoveryAfterOneNodeUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT; iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT")

	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 4, 5*time.Minute)
	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 5, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Pod goes down
	// * New member balanced in to replace it
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestRecoveryNodeTmpUnreachableBucketOneReplica(t *testing.T) {
	t.Skip("test not fully implemented...")

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 5
	autofailoverTimeout := 30 * time.Second

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.ClusterSettings.AutoFailoverTimeout = &metav1.Duration{Duration: autofailoverTimeout}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Generate workload during the operation.
	defer e2eutil.MustGenerateWorkload(t, targetKube, testCouchbase, f.CouchbaseServerImage, e2espec.DefaultBucket.Name)()

	memberName := couchbaseutil.CreateMemberName(testCouchbase.Name, 0)

	//block all incoming and outgoing traffic expect ssh on port 22
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -A INPUT -p tcp -s 0/0 -d $(/bin/hostname -i) --sport 513:65535 --dport 22 -m state --state NEW,ESTABLISHED -j ACCEPT")
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -A OUTPUT -p tcp -s $(/bin/hostname -i) -d 0/0 --sport 22 --dport 513:65535 -m state --state ESTABLISHED -j ACCEPT")
	time.Sleep(autofailoverTimeout / 2)
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -F")
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -X")
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -P INPUT DROP")
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -P OUTPUT DROP")
	e2eutil.MustExecShellInPod(t, targetKube, memberName, "iptables -P FORWARD DROP")
	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 4, 5*time.Minute)
	e2eutil.MustWaitUntilPodSizeReached(t, targetKube, testCouchbase, 5, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Pod goes down
	// * Pod recovers and the operator rebalances
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestTaintK8SNodeAndRemoveTaint(t *testing.T) {
	t.Skip("this hasn't ever worked - select a bloody node with a pod running on")

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Deploy couchbase cluster
	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, clusterSize)
	e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{e2espec.DefaultBucket.Name}, time.Minute)

	// Set taint properties
	podTaint := []v1.Taint{
		{
			Key:    "noExecKey",
			Value:  "noExecVal",
			Effect: "NoExecute",
		},
	}

	nodeIndex := 2

	if err := e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, true, podTaint, nodeIndex); err != nil {
		e2eutil.Die(t, fmt.Errorf("Failed to set node taint and schedulable property: %v", err))
	}

	defer func() {
		_ = e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex)
	}()

	e2eutil.MustWaitForUnhealthyNodes(t, targetKube, testCouchbase, 1, time.Minute)

	if err := e2eutil.SetNodeTaintAndSchedulableProperty(targetKube.KubeClient, false, []v1.Taint{}, nodeIndex); err != nil {
		e2eutil.Die(t, fmt.Errorf("Failed to unset node taint and schedulable property: %v", err))
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.RebalanceStartedEvent(testCouchbase), 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Pod goes down
	// * Pod ejected and the operator recovers
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		e2eutil.PodDownFailoverRecoverySequence(),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
