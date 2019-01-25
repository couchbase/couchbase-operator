package e2e

import (
	"testing"
	"time"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"k8s.io/api/core/v1"
)

const (
	// This is an illegal target version
	targetVersionIllegalUpgrade = "enterprise-10.0.0"
	// This is an illegal target version
	targetVersionIllegalDowngrade = "enterprise-5.0.0"
)

var (
	// upgradeSequence is a common upgrade sequence of adding a node, balancing it
	// in, ejecting another and the rebalance completing.
	upgradeSequence = eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
)

// upgradeFailedAddRecoverableSequence is a common sequence for generating events for a new
// member being added, the pod being killed before a rebalance can commence, and the
// recovery steps.
func upgradeFailedAddRecoverableSequence(victimName string) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// upgradeFailedAddUnrecoverableSequence is a common sequence for generating events for a new
// member being added, the pod being killed before a rebalance can commence, and the
// recovery steps.
func upgradeFailedAddUnrecoverableSequence(victimName string) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
			eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			// I wonder why this is the only case where a member removed event doesn't happen?
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// upgradeDownRecoverableSequence is a common sequence for generating events for a new
// member being added, the pod being killed during a rebalance and the recovery steps.
func upgradeDownRecoverableSequence(victimName string) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
			eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// MemberAddAndDownUnecoverableSequence is a common sequence for generating events for a new
// member being added, the pod being killed during a rebalance and the recovery steps.
func upgradeDownUnrecoverableSequence(victimName string) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
			eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// skipUpgrade checks configuration and skips a test if illegal.
func skipUpgrade(t *testing.T) {
	f := framework.Global

	if f.CouchbaseServerUpgradeVersion == "" {
		t.Skip("Upgrade version not specified")
	}

	version, err := couchbaseutil.NewVersion(f.CouchbaseServerVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}
	upgrade, err := couchbaseutil.NewVersion(f.CouchbaseServerUpgradeVersion)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if version.GreaterEqual(upgrade) {
		t.Skip("Upgrade base version greater than or equal to upgrade version")
	}
}

// TestUpgrade upgrades a three node cluster.
func TestUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev1.ClusterConditionUpgrading, v1.ConditionTrue, cluster, time.Now(), 120)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries120)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeRollback begins an upgrade then rolls it back to the previous version.
func TestUpgradeRollback(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// this will happen as the first upgrade begins, at which point revert.  The cluster will
	// healthy after rollback has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev1.ClusterConditionUpgrading, v1.ConditionTrue, cluster, time.Now(), 120)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerVersion), constants.Retries10)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries120)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * One node upgrades
	// * Rollback starts
	// * One node is rolled back
	// * Rollback completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonRollbackStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonRollbackFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeKillPodOnCreate begins an upgrade then kills a pod to be added to the
// cluster before a rebalance has occurred.
func TestUpgradeKillPodOnCreate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	clusterSize := constants.Size3
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is created immediately
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 120)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries120)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * For iterations up to the victim cycle expect nodes upgrade
	// * Victim node failed to add and is balanced out
	// * For the remaining iterations upgrades nodes upgrade
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: victimCycle, Validator: upgradeSequence},
		upgradeFailedAddUnrecoverableSequence(victimName),
		eventschema.Repeat{Times: clusterSize - victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeInvalidUpgrade ensures an upgrade cannot happen across major versions.
func TestUpgradeInvalidUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// When the cluster is ready, start the upgrade.  Expect the update to be rejected.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", targetVersionIllegalUpgrade))
}

// TestUpgradeInvalidDowngrade ensures you cannot downgrade.
func TestUpgradeInvalidDowngrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// When the cluster is ready, start the downgrade.  Expect the update to be rejected.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", targetVersionIllegalDowngrade))
}

// TestUpgradeInvalidRollback ensures you cannot rollback to a different version.
func TestUpgradeInvalidRollback(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// this will happen as the first upgrade begins, at which point try rollabck to an illegal version.
	// Expect the update to be rejected.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev1.ClusterConditionUpgrading, v1.ConditionTrue, cluster, time.Now(), 120)
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", targetVersionIllegalDowngrade))
}

// TestUpgradeSupportable tests that upgrades work for a supportable cluster.
func TestUpgradeSupportable(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev1.ClusterConditionUpgrading, v1.ConditionTrue, cluster, time.Now(), 120)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries240)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeSupportableKillStatefulPodOnCreate tests that upgrades work for a supportable cluster
// where a stateful pod is killed on creation.
func TestUpgradeSupportableKillStatefulPodOnCreate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is created immediately
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 600)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries240)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * For iterations up to the victim cycle expect nodes upgrade
	// * Victim node failed to add and is balanced out
	// * For the remaining iterations upgrades nodes upgrade
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: victimCycle, Validator: upgradeSequence},
		upgradeFailedAddRecoverableSequence(victimName),
		eventschema.Repeat{Times: clusterSize - victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeSupportableKillStatefulPodOnRebalance tests that upgrades work for a supportable cluster
// where a stateful pod is killed on rebalance.
func TestUpgradeSupportableKillStatefulPodOnRebalance(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is balancing in
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 600)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 30)
	time.Sleep(5 * time.Second)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries240)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * For iterations up to the victim cycle expect nodes upgrade
	// * Victim node failed to balance in and is ejected to maintain scale
	// * For the remaining iterations upgrades nodes upgrade
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: victimCycle, Validator: upgradeSequence},
		upgradeDownRecoverableSequence(victimName),
		eventschema.Repeat{Times: clusterSize - victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeSupportableKillStatelessPodOnCreate tests that upgrades work for a supportable cluster
// where a stateless pod is killed on creation.
func TestUpgradeSupportableKillStatelessPodOnCreate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimCycle := mdsGroupSize + 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is created immediately
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 600)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries240)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * For iterations up to the victim cycle expect nodes upgrade
	// * Victim node failed to add and is balanced out
	// * For the remaining iterations upgrades nodes upgrade
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: victimCycle, Validator: upgradeSequence},
		upgradeFailedAddUnrecoverableSequence(victimName),
		eventschema.Repeat{Times: clusterSize - victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeSupportableKillStatelessPodOnRebalance tests that upgrades work for a supportable cluster
// where a stateless pod is killed on rebalance.
func TestUpgradeSupportableKillStatelessPodOnRebalance(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Skip if not correctly configured
	skipUpgrade(t)

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimCycle := mdsGroupSize + 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewSupportableCluster(t, kubernetes, f.Namespace, mdsGroupSize)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is balancing in
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/Version", f.CouchbaseServerUpgradeVersion), constants.Retries10)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 600)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 30)
	time.Sleep(5 * time.Second)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries240)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * For iterations up to the victim cycle expect nodes upgrade
	// * Victim node failed to balance in and is ejected to maintain scale
	// * For the remaining iterations upgrades nodes upgrade
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: victimCycle, Validator: upgradeSequence},
		upgradeDownUnrecoverableSequence(victimName),
		eventschema.Repeat{Times: clusterSize - victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}
