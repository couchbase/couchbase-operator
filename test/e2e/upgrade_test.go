package e2e

import (
	"testing"
	"time"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"k8s.io/api/core/v1"
)

const (
	// This is the required initial server version
	sourceVersion = "enterprise-5.5.0"
	// This is the required target server version
	targetVersion = "enterprise-5.5.2"
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

// clusterCreateSequence is a common function for generating cluster creation events.
func clusterCreateSequence(size int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     size,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// TestUpgrade upgrades a three node cluster.
func TestUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.ClusterSpec[f.TestClusters[0]]

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes.KubeClient, kubernetes.CRClient, f.Namespace, kubernetes.DefaultSecret.Name, clusterSize, constants.WithoutBucket, constants.AdminHidden)
	if cluster.Spec.Version != sourceVersion {
		t.Skip("Skipping test as version is not as expected")
	}

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes.CRClient, cluster.Name, f.Namespace, clusterSize, constants.Retries10)
	e2eutil.MustUpdateClusterSpec(t, "Version", targetVersion, kubernetes.CRClient, cluster, constants.Retries10)
	e2eutil.MustWaitForClusterCondition(t, kubernetes.CRClient, couchbasev1.ClusterConditionUpgrading, v1.ConditionTrue, cluster, time.Now(), 120)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes.CRClient, cluster.Name, f.Namespace, clusterSize, constants.Retries120)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		clusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes.KubeClient, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeRollback begins an upgrade then rolls it back to the previous version.
func TestUpgradeRollback(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.ClusterSpec[f.TestClusters[0]]

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes.KubeClient, kubernetes.CRClient, f.Namespace, kubernetes.DefaultSecret.Name, clusterSize, constants.WithoutBucket, constants.AdminHidden)
	if cluster.Spec.Version != sourceVersion {
		t.Skip("Skipping test as version is not as expected")
	}

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// this will happen as the first upgrade begins, at which point revert.  The cluster will
	// healthy after rollback has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes.CRClient, cluster.Name, f.Namespace, clusterSize, constants.Retries10)
	e2eutil.MustUpdateClusterSpec(t, "Version", targetVersion, kubernetes.CRClient, cluster, constants.Retries10)
	e2eutil.MustWaitForClusterCondition(t, kubernetes.CRClient, couchbasev1.ClusterConditionUpgrading, v1.ConditionTrue, cluster, time.Now(), 120)
	e2eutil.MustUpdateClusterSpec(t, "Version", sourceVersion, kubernetes.CRClient, cluster, constants.Retries10)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes.CRClient, cluster.Name, f.Namespace, clusterSize, constants.Retries120)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * One node upgrades
	// * Rollback starts
	// * One node is rolled back
	// * Rollback completes
	expectedEvents := []eventschema.Validatable{
		clusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonRollbackStarted},
		upgradeSequence,
		eventschema.Event{Reason: k8sutil.EventReasonRollbackFinished},
	}

	ValidateEvents(t, kubernetes.KubeClient, f.Namespace, cluster.Name, expectedEvents)
}

// TestUpgradeKillPodOnCreate begins an upgrade then kills a pod to be added to the
// cluster before a rebalance has occurred.
func TestUpgradeKillPodOnCreate(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.ClusterSpec[f.TestClusters[0]]

	// Static configuration.
	clusterSize := constants.Size3
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes.KubeClient, kubernetes.CRClient, f.Namespace, kubernetes.DefaultSecret.Name, clusterSize, constants.WithoutBucket, constants.AdminHidden)
	if cluster.Spec.Version != sourceVersion {
		t.Skip("Skipping test as version is not as expected")
	}

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is created immediately
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes.CRClient, cluster.Name, f.Namespace, clusterSize, constants.Retries10)
	e2eutil.MustUpdateClusterSpec(t, "Version", targetVersion, kubernetes.CRClient, cluster, constants.Retries10)
	e2eutil.MustWaitForClusterEvent(t, kubernetes.KubeClient, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 120)
	e2eutil.MustKillPodForMember(t, kubernetes.KubeClient, cluster, victimIndex)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes.CRClient, cluster.Name, f.Namespace, clusterSize, constants.Retries120)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * For iterations up to the victim cycle expect nodes upgrade
	// * Victim node failed to add and is balanced out
	// * For the remaining iterations upgrades nodes upgrade
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		clusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
		eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Repeat{Times: clusterSize - victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes.KubeClient, f.Namespace, cluster.Name, expectedEvents)
}
