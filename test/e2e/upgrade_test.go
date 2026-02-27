package e2e

import (
	"sort"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// This is an illegal target version.
	targetVersionIllegalUpgrade = "couchbase/server:enterprise-10.0.0"
	// This is an illegal target version.
	targetVersionIllegalDowngrade = "couchbase/server:enterprise-5.0.0"
)

// upgradeSequence is a common upgrade sequence of adding a node, balancing it
// in, ejecting another and the rebalance completing.
var upgradeSequence = eventschema.Sequence{
	Validators: []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete}}},
		},
		},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	},
}

// RollingUpgradeSequence is what to expect when a cluster is upgraded all at once.
func RollingUpgradeSequence(clusterSize, maxNumber int) eventschema.Validatable {
	schema := eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		},
	}

	for clusterSize > 0 {
		times := maxNumber
		if clusterSize < maxNumber {
			times = clusterSize
		}

		clusterSize -= times

		upgrade := eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Repeat{Times: times, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Repeat{Times: times, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}

		schema.Validators = append(schema.Validators, upgrade)
	}

	schema.Validators = append(schema.Validators, eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished})

	return schema
}

// upgradeFailedAddRecoverableSequence is a common sequence for generating events for a new
// member being added, the pod being killed before a rebalance can commence, and the
// recovery steps.  Due to a race condition the pod may actually go down rather than enter
// failed add.
func upgradeFailedAddRecoverableSequence(victimName string) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victimName},
			// This suffers from race conditions, so it's essentially random...
			// Either the pod is killed before the operator can detect it and
			// rebalance picks it up, or the operator will spot it's broken, abort
			// the loop and recover it straight away.
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
						eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
						eventschema.Optional{
							Validator: eventschema.Sequence{
								Validators: []eventschema.Validatable{
									eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
									eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
								},
							},
						},
					},
				},
			},
			eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
			eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			// once member is recovered the Operator will proceed with upgrade
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
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
			// This suffers from race conditions, so it's essentially random...
			eventschema.AnyOf{
				Validators: []eventschema.Validatable{
					// ... either the pod is added to the cluster and rebalance is started before
					// server complains ...
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
							eventschema.Optional{
								Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
							},
							eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
						},
					},
					// ... or the operator notices it's gone pop, aborts the topology reconcile
					// and next time around the pod is already in failed add. This is logged as
					// a reconcile failed event.
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
							eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
						},
					},
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
							eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
							eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
							eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
						},
					},
				},
			},
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						eventschema.Optional{
							Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
						},
						// I wonder why this is the only case where a member removed event doesn't happen?
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
					},
				},
			},
		},
	}
}

// MemberAddAndDownUnecoverableSequence is a common sequence for generating events for a new
// member being added, the pod being killed during a rebalance and the recovery steps.  Warning,
// the behaviour of this changes based on what kind of stateful service is enabled - eventing
// being the prime cause of inconsistency.
func upgradeDownUnrecoverableSequence(victimName string) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
			eventschema.Optional{
				Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			},
			eventschema.AnyOf{
				Validators: []eventschema.Validatable{
					// In the first incarnation, the candidate has already been
					// ejected, so is removed while the upgraded node is failed
					// and replaced.
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
							eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
							eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
							eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
							eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
						},
					},
					// In the second incarnation, the candidate is still active
					// so the failed node is ejected and the upgrade restarts.
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Optional{
								Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
							},
							eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
							eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: victimName},
							eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
							upgradeSequence,
						},
					},
				},
			},
		},
	}
}

func TestUpgrade(t *testing.T) {
	testUpgrade(t, false)
}

func TestUpgradePersistent(t *testing.T) {
	testUpgrade(t, true)
}

// testUpgrade upgrades a three node cluster.
func testUpgrade(t *testing.T, persistent bool) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	var cluster *couchbasev2.CouchbaseCluster
	if persistent {
		cluster = clusterOptionsUpgrade().WithPersistentTopology(clusterSize).MustCreate(t, kubernetes)
	} else {
		cluster = clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	}

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestUpgradeStabilizationPeriod(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	stabilizationPeriodS := int64(60)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	var cluster *couchbasev2.CouchbaseCluster
	cluster = clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		StabilizationPeriod: e2espec.NewDurationS(stabilizationPeriodS),
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Check that the cluster goes into the waiting state the right number of times
	for i := 0; i < clusterSize; i++ {
		e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)

		if i < clusterSize-1 {
			e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 10*time.Minute, couchbasev2.ClusterConditionWaitingBetweenUpgrades)
			e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionWaitingBetweenUpgrades, v1.ConditionTrue, cluster, 10*time.Minute)
			// Validate that it stays in the waiting state for the right amount of time (minus 10 seconds so not too flakey)
			e2eutil.AssertClusterConditionFor(t, kubernetes, couchbasev2.ClusterConditionWaitingBetweenUpgrades, v1.ConditionTrue, cluster, time.Duration(stabilizationPeriodS-10)*time.Second)
			e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 10*time.Minute, couchbasev2.ClusterConditionWaitingBetweenUpgrades)
		}
	}
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeRollback begins an upgrade then rolls it back to the previous version.
func TestUpgradeRollback(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// this will happen as the first upgrade begins, at which point revert.  The cluster will
	// healthy after rollback has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImageUpgrade), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

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
		eventschema.Repeat{
			Times:     2,
			Validator: upgradeSequence,
		},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeKillPodOnCreate begins an upgrade then kills a pod to be added to the
// cluster before a rebalance has occurred.
func TestUpgradeKillPodOnCreate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size3
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is created immediately
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeInvalidUpgrade ensures an upgrade cannot happen across major versions.
func TestUpgradeInvalidUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When the cluster is ready, start the upgrade.  Expect the update to be rejected.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", targetVersionIllegalUpgrade))
}

// TestUpgradeInvalidDowngrade ensures you cannot downgrade.
func TestUpgradeInvalidDowngrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When the cluster is ready, start the downgrade.  Expect the update to be rejected.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", targetVersionIllegalDowngrade))
}

// TestUpgradeInvalidRollback ensures you cannot rollback to a different version.
func TestUpgradeInvalidRollback(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// this will happen as the first upgrade begins, at which point try rollabck to an illegal version.
	// Expect the update to be rejected.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 2*time.Minute)
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", targetVersionIllegalDowngrade))
}

// TestUpgradeSupportable tests that upgrades work for a supportable cluster.
func TestUpgradeSupportable(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)

	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 40*time.Minute)

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

	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	if e2eutil.MustCheckIfUpgradeOverVersion(t, initialVersion, upgradeVersion, "8.0.0") {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonBucketEdited})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeSupportableKillStatefulPodOnCreate tests that upgrades work for a supportable cluster
// where a stateful pod is killed on creation.
func TestUpgradeSupportableKillStatefulPodOnCreate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is created immediately
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 10*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 40*time.Minute)

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
		// both the victim pod and upgraded pod occurred in same sequence.
		// therefore, these 2 do not need to be accounted for here
		eventschema.Repeat{Times: clusterSize - victimCycle - 1, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	if e2eutil.MustCheckIfUpgradeOverVersion(t, initialVersion, upgradeVersion, "8.0.0") {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonBucketEdited})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeSupportableKillStatefulPodOnRebalance tests that upgrades work for a supportable cluster
// where a stateful pod is killed on rebalance.
func TestUpgradeSupportableKillStatefulPodOnRebalance(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Runtime configuration.

	// When the cluster is ready, start the upgrade.  When the victim pod is balancing in
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 10*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, kubernetes, cluster, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewUpgradeFinishedEvent(cluster), 30*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
}

// TestUpgradeSupportableKillExistingStatefulPodOnRebalance tests that upgrades work for a
// supportable cluster where a stateful pod is killed on rebalance.
func TestUpgradeSupportableKillExistingStatefulPodOnRebalance(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	mdsGroupSize := constants.Size2
	victimCycle := 1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimCycle)

	// When the cluster is ready, start the upgrade.  When the victim pod is balancing in
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForRebalanceEjectingNode(t, kubernetes, cluster, victimName, 10*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, kubernetes, cluster, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimCycle, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 40*time.Minute)
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.UpgradeFinishedEvent(cluster), 10*time.Minute)

	expectedVersion := couchbaseutil.GetVersionTag(f.CouchbaseServerImage)
	e2eutil.MustCheckPodsForVersion(t, kubernetes, cluster, f.CouchbaseServerImage, expectedVersion)
}

// TestUpgradeSupportableKillStatelessPodOnCreate tests that upgrades work for a supportable cluster
// where a stateless pod is killed on creation.
func TestUpgradeSupportableKillStatelessPodOnCreate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2          //4
	victimCycle := mdsGroupSize + 1          //3
	victimIndex := clusterSize + victimCycle // 7

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is created immediately
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 20*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 40*time.Minute)

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

	// If we're upgrading over 8.0.0, we need to add the bucket edited event where we disable the encryption defaults.
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	if e2eutil.MustCheckIfUpgradeOverVersion(t, initialVersion, upgradeVersion, "8.0.0") {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonBucketEdited})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeSupportableKillStatelessPodOnRebalance tests that upgrades work for a supportable cluster
// where a stateless pod is killed on rebalance.
func TestUpgradeSupportableKillStatelessPodOnRebalance(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	victimCycle := mdsGroupSize + 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is balancing in
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 20*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, kubernetes, cluster, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimIndex, false)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 40*time.Minute)

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
		eventschema.Repeat{Times: clusterSize - (victimCycle + 1), Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	if e2eutil.MustCheckIfUpgradeOverVersion(t, initialVersion, upgradeVersion, "8.0.0") {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonBucketEdited})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeEnv tests the upgrade mechanism being used to add environment variables
// to an existing cluster.
func TestUpgradeEnv(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster without TLS.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Once up and running modify the pod policy.
	env := []v1.EnvVar{
		{
			Name:  "bugs",
			Value: "bunny",
		},
	}
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/servers/0/env", env), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeToSupportable tests we can take an ephemeral cluster and make it supportable
// by dynamically adding persistent volumes.
func TestUpgradeToSupportable(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// PV configuration
	pvcName := e2eutil.GetPvcName(f.LocalPV)

	// Create the cluster without PVs.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Once up and running add in PV support.
	templates := []couchbasev2.PersistentVolumeClaimTemplate{createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, f.LocalPV, 2)}

	mounts := &couchbasev2.VolumeMounts{
		DefaultClaim: pvcName,
	}
	patchset := jsonpatch.NewPatchSet().
		Add("/spec/volumeClaimTemplates", templates).
		Add("/spec/servers/0/volumeMounts", mounts)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeToTLS tests that we can take an insecure cluster and make it secure.
func TestUpgradeToTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster without TLS.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When ready create the required TLS secrets and patch them into the running
	// cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{ClusterName: cluster.Name})

	tls := &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/tls", tls), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeToMandatoryMutualTLS tests enabling TLS and mTLS at the same time.
func TestUpgradeToMandatoryMutualTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	policy := couchbasev2.ClientCertificatePolicyMandatory
	clusterSize := 3

	// Create the cluster without TLS.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// When ready create the required TLS secrets and patch them into the running
	// cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{ClusterName: cluster.Name})

	tls := &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		ClientCertificatePolicy: &policy,
		ClientCertificatePaths: []couchbasev2.ClientCertificatePath{
			{
				Path: "subject.cn",
			},
		},
	}
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/tls", tls), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated, Message: string(k8sutil.ClientTLSUpdateReasonCreateClientAuth)},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradePVC tests that we can increase the storage capacity of PVCS.
func TestUpgradePVC(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2

	// Create the cluster.
	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Update the PVC template size from 1Gi to 2GI
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/resources/requests", v1.ResourceList{v1.ResourceStorage: *e2espec.NewResourceQuantityMi(2048)}), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node in the server group is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: mdsGroupSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradePVCStorageClass tests that we can change the storage class of PVCs.
func TestUpgradePVCStorageClass(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).DefaultAndExplicitStorageClass()

	// Static configuration.
	mdsGroupSize := 2
	clusterSize := mdsGroupSize * 2

	// Create the cluster.
	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).WithDefaultStorageClass().MustCreate(t, kubernetes)

	// Update the PVC storage class from none to the configured one.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/storageClassName", f.StorageClassName), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node in the server group is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: mdsGroupSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeImmediate tests an immediate upgrade.
func TestUpgradeImmediate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{UpgradeStrategy: couchbasev2.ImmediateUpgrade}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		RollingUpgradeSequence(clusterSize, clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeConstrained tests a rolling upgrade, but limited to a
// certain percentage.
func TestUpgradeConstrained(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := 3
	upgradablePercent := "67%"
	upgradeChunkSize := 2
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeStrategy: couchbasev2.RollingUpgrade,
		RollingUpgrade: &couchbasev2.RollingUpgradeConstraints{
			MaxUpgradablePercent: upgradablePercent,
		},
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		RollingUpgradeSequence(clusterSize, upgradeChunkSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestUpgradeBucketDurability(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").Upgradable()

	// Static Config
	clusterSize := 3
	numOfDocs := f.DocsCount
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	bucket := e2eutil.GetBucket(f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Add("/spec/minimumDurability", couchbasev2.CouchbaseBucketMinimumDurabilityMajority), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/DurabilityMinLevel", couchbaseutil.DurabilityMajority), time.Minute)

	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 2*numOfDocs, time.Minute)

	// Upgrades from pre 8.0 to post 8.0 will add an additional event as the bucket encryption settings are updated
	// to the operator defaults.
	editCount := 1
	if e2eutil.MustCheckIfUpgradeOverVersion(t, initialVersion, upgradeVersion, "8.0.0") {
		editCount = 2
	}

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		eventschema.Repeat{Times: editCount, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeWithTLS tests that we can upgrade a cluster with TLS enabled.
func TestUpgradeWithTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion) // Static configuration.
	clusterSize := constants.Size3
	// Create the cluster.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).WithTLS(ctx).MustCreate(t, kubernetes)
	// When the cluster is healthy, check the TLS is correctly configured.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)

	// Patch spec to new image
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	// When the cluster is healthy, check the TLS is correctly configured
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestDeltaRecovery(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable()

	clusterSize := 3
	numOfDocs := f.DocsCount

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	cluster := clusterOptionsUpgrade().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.InPlaceUpgrade}

	// This config triggers unexpected counter errors during upgrade
	kubernetes.DisableResourceAllocation = true
	cluster.Spec.Servers[0].Services = []couchbasev2.Service{
		couchbasev2.DataService,
		couchbasev2.IndexService,
		couchbasev2.QueryService,
		couchbasev2.SearchService,
		couchbasev2.AnalyticsService,
		couchbasev2.EventingService,
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)

	e2eutil.MustWaitClusterStatusHealthyWithoutError(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.UpgradeFinishedEvent(cluster), 10*time.Minute)
	e2eutil.MustCheckPodsForVersion(t, kubernetes, cluster, f.CouchbaseServerImage, upgradeVersion)
}

func TestDeltaRecoveryWithoutDataService(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable()

	groupSize1 := 2
	groupSize2 := 1
	clusterSize := groupSize1 + groupSize2

	// Create the cluster with two server classes, and exposed features.
	cluster := clusterOptionsUpgrade().WithPersistentTopology(clusterSize).Generate(kubernetes)

	cluster.Spec.Servers = []couchbasev2.ServerConfig{
		{
			Name: "data",
			Size: groupSize1,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				DataClaim:    "default",
				IndexClaim:   "default",
			},
		},
		{
			Name: "query",
			Size: groupSize2,
			Services: couchbasev2.ServiceList{
				couchbasev2.IndexService,
				couchbasev2.QueryService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				IndexClaim:   "default",
			},
		},
	}

	runDeltaRecoveryTests(t, kubernetes, cluster, f, "data", 20*time.Minute)
}

func TestDeltaRecoveryWithVariousServices(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable()

	groupSize1 := 2
	groupSize2 := 1
	clusterSize := groupSize1

	// Create the cluster with various server classes, and exposed features.
	cluster := clusterOptionsUpgrade().WithPersistentTopology(clusterSize).Generate(kubernetes)

	cluster.Spec.Servers = []couchbasev2.ServerConfig{
		{
			Name: "data",
			Size: groupSize1,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				DataClaim:    "default",
			},
		},
		{
			Name: "kvIndexQuery",
			Size: groupSize1,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
				couchbasev2.QueryService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				IndexClaim:   "default",
			},
		},
		{
			Name: "indexQueryAnalytics",
			Size: groupSize2,
			Services: couchbasev2.ServiceList{
				couchbasev2.IndexService,
				couchbasev2.QueryService,
				couchbasev2.AnalyticsService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				IndexClaim:   "default",
			},
		},
		{
			Name: "queryAnalytics",
			Size: groupSize2,
			Services: couchbasev2.ServiceList{
				couchbasev2.QueryService,
				couchbasev2.AnalyticsService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
			},
		},
		{
			Name: "query",
			Size: groupSize2,
			Services: couchbasev2.ServiceList{
				couchbasev2.QueryService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
			},
		},
		{
			Name: "kvIndexQueryAnalytics",
			Size: groupSize1,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
				couchbasev2.QueryService,
				couchbasev2.AnalyticsService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				IndexClaim:   "default",
			},
		},
		{
			Name: "index",
			Size: groupSize2,
			Services: couchbasev2.ServiceList{
				couchbasev2.IndexService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				IndexClaim:   "default",
			},
		},
		{
			Name: "indexQuery",
			Size: groupSize2,
			Services: couchbasev2.ServiceList{
				couchbasev2.IndexService,
				couchbasev2.QueryService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				IndexClaim:   "default",
			},
		},
		{
			Name: "kvIndex",
			Size: groupSize1,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "default",
				IndexClaim:   "default",
			},
		},
	}

	runDeltaRecoveryTests(t, kubernetes, cluster, f, "data", 40*time.Minute)
}

func runDeltaRecoveryTests(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, f *framework.Framework, bucketName string, timeout time.Duration) {
	numOfDocs := f.DocsCount

	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.InPlaceUpgrade}

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	memoryQuota, err := resource.ParseQuantity("1024Mi")
	if err != nil {
		e2eutil.Die(t, err)
	}

	cluster.Spec.ClusterSettings.DataServiceMemQuota = &memoryQuota
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	bucket.SetName(bucketName)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, timeout)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.UpgradeFinishedEvent(cluster), 20*time.Minute)
	e2eutil.MustCheckPodsForVersion(t, kubernetes, cluster, f.CouchbaseServerImage, upgradeVersion)
}

func TestResilientDeltaRecovery(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable()

	clusterSize := 3
	upgradeProcess := couchbasev2.InPlaceUpgrade
	numOfDocs := f.DocsCount

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	cluster := clusterOptionsUpgrade().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.UpgradeProcess = &upgradeProcess

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)

	// Get Orchestrator Node
	orchestratorNode := e2eutil.MustGetOrchestratorNode(t, kubernetes, cluster)

	index, err := couchbaseutil.GetIndexFromMemberName(strings.Split(strings.Split(orchestratorNode, "@")[1], ".")[0])

	if err != nil {
		e2eutil.Die(t, err)
	}

	// Start the upgrade and wait for graceful failover to start
	cluster = e2eutil.MustPatchClusterAndWaitForGracefulFailoverToStart(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Kill the orchestrator node
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, index, false)

	// Wait for graceful failover to fall over
	e2eutil.MustWaitForClusterWithErrorMessage(t, kubernetes, "graceful failover failed:", cluster, 5*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Check the version and document counts are as we expect
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.UpgradeFinishedEvent(cluster), 10*time.Minute)
	e2eutil.MustCheckPodsForVersion(t, kubernetes, cluster, f.CouchbaseServerImage, upgradeVersion)
}

func TestInplaceUpgradeWithRollback(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.6.0").Upgradable()

	// Static configuration.
	clusterSize := constants.Size3
	upgradeProcess := couchbasev2.InPlaceUpgrade

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	cluster.Spec.UpgradeProcess = &upgradeProcess

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// this will happen as the first upgrade begins, at which point revert.  The cluster will
	// healthy after rollback has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImageUpgrade), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	startingVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	e2eutil.MustCheckPodsForVersion(t, kubernetes, cluster, f.CouchbaseServerImageUpgrade, startingVersion)

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
		eventschema.Repeat{
			Times:     2,
			Validator: upgradeSequence,
		},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestInPlaceUpgradeWithMultipleNodes(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable().ServerGroups(1)

	available := getAvailabilityZones(t, kubernetes)

	// Static configuration.
	clusterSize := 6
	maxUpgradeable := 2
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeStrategy:  couchbasev2.RollingUpgrade,
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServerGroups,
		RollingUpgrade: &couchbasev2.RollingUpgradeConstraints{
			MaxUpgradable: maxUpgradeable,
		},
		UpgradeProcess: couchbasev2.InPlaceUpgrade,
	}

	cluster.Spec.ServerGroups = []string{available[0]}
	// Ensure the single server config uses all groups
	cluster.Spec.Servers[0].ServerGroups = []string{available[0]}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.  We expect the upgrading condition to exist,
	// then the cluster to become healthy after upgrade has completed.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: 3,
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
					eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted}},
			},
		},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeStatefulPodDeletionDoesNotImplicitlyUpgrade tests that when a pod is deleted during
// an upgrade, the replacement pod is not implicitly upgraded.
func TestUpgradeStatefulPodDeletionDoesNotImplicitlyUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size4
	victimIndex := 3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	// Create the cluster with persistent storage
	clusterOptions := clusterOptionsUpgrade().WithPersistentTopology(clusterSize)
	clusterOptions.Options.AutoFailoverTimeout = e2espec.NewDurationS(120)
	cluster := clusterOptions.MustCreate(t, kubernetes)

	// Start the upgrade
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for upgrade to start
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	// e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceStartedEvent(cluster), 5*time.Minute)

	// Delete victim pod before it gets upgraded
	e2eutil.MustKillMemberWithDefaultGracePeriod(t, kubernetes, cluster, victimIndex)

	// Wait for pod to be recreated and verify it's still running the initial version
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.MemberRecoveredEvent(cluster, victimIndex), 5*time.Minute)
	e2eutil.MustCheckPodForVersion(t, kubernetes, victimName, f.CouchbaseServerImageUpgrade, initialVersion)

	// Let upgrade complete
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Verify all pods are now on the upgraded version
	e2eutil.MustCheckPodsForVersion(t, kubernetes, cluster, f.CouchbaseServerImage, upgradeVersion)
}

func TestPartialUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	classSize := constants.Size1
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	numOldPods := 1

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithMixedEphemeralTopology(classSize).Generate(kubernetes)

	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		PreviousVersionPodCount: numOldPods,
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	clusterSize := cluster.Spec.TotalSize()

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionMixedMode, v1.ConditionTrue, cluster, 1*time.Minute)

	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, initialVersion, time.Minute)

	expectedImageCountMap := map[string]int{
		f.CouchbaseServerImageUpgrade: numOldPods,
		f.CouchbaseServerImage:        clusterSize - numOldPods,
	}

	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMap)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 1*time.Minute, couchbasev2.ClusterConditionMixedMode)

	expectedImageCountMap = map[string]int{
		f.CouchbaseServerImage: clusterSize,
	}

	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMap)
}

func TestServerClassesUpgradeOrder(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	numOldPods := 2
	// Static configuration.
	classSize := constants.Size2

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithMixedTopology(classSize).Generate(kubernetes)

	// Set the upgrade order to upgrade the second server class first and keep two pods on the initial version
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServerClasses,
		UpgradeOrder: []string{
			cluster.Spec.Servers[1].Name,
			cluster.Spec.Servers[0].Name,
		},
		PreviousVersionPodCount: numOldPods,
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade of the first server class to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is still the initial version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)

	// Verify the first server class is on the initial version and the second server class is on the upgrade version
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImageUpgrade, initialVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImage, upgradeVersion)

	// Set the previous version pod count to 0 to start the upgrade of the second server class
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	// Wait for the upgrade of the second server class to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImage, upgradeVersion)
}

func TestServicesUpgradeOrder(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	classSize := constants.Size1
	clusterSize := classSize * 3

	// Static configuration.
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithSplitEphemeralTopology(classSize).Generate(kubernetes)
	// servers[0] -> data
	// servers[1] -> index
	// servers[2] -> query

	// Set the upgrade order to upgrade the second server class first and keep two pods on the initial version
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServices,
		UpgradeOrder: []string{
			string(couchbasev2.QueryService),
			string(couchbasev2.DataService),
			string(couchbasev2.IndexService),
		},
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[2].Name, f.CouchbaseServerImage, upgradeVersion)

	expectedIndexUpgradeOrder := []int{2, 0, 1}

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded in the expected order
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
	}

	for _, podIndex := range expectedIndexUpgradeOrder {
		upgradeSequence := eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: couchbaseutil.CreateMemberName(cluster.Name, podIndex)},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}
		expectedEvents = append(expectedEvents, upgradeSequence)
	}

	expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished})

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestServicesUpgradeOrderWithArbiterNodes(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	classSize := constants.Size2

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	cluster := clusterOptionsUpgrade().WithEphemeralAndArbiterTopology(classSize).Generate(kubernetes)
	// servers [0] -> Data + Index + Query
	// servers [1] -> Arbiter

	// Set the upgrade order to upgrade the arbiter first, followed by anything else.
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServices,
		UpgradeOrder: []string{
			"arbiter",
		},
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Start the upgrade
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImage, upgradeVersion)

	expectedIndexUpgradeOrder := []int{2, 3, 0, 1}

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(classSize * 2),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
	}

	for _, podIndex := range expectedIndexUpgradeOrder {
		upgradeSequence := eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: couchbaseutil.CreateMemberName(cluster.Name, podIndex)},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}
		expectedEvents = append(expectedEvents, upgradeSequence)
	}

	expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished})

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestServicesUpgradeOrderSharedServices(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	classSize := constants.Size1
	numOldPods := classSize * 1
	// Static configuration.

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithSplitEphemeralTopology(classSize).Generate(kubernetes)

	// Modify the index server class to have data and index
	cluster.Spec.Servers[1].Services = []couchbasev2.Service{couchbasev2.DataService, couchbasev2.IndexService}
	cluster.Spec.Servers[1].Name = "data_index"

	// Set the upgrade order to upgrade the second server class first and keep two pods on the initial version
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServices,
		UpgradeOrder: []string{
			string(couchbasev2.DataService),
			string(couchbasev2.QueryService),
		},
		PreviousVersionPodCount: numOldPods,
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade of the first service to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is still the initial version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)

	// Verify the first and second server class is on the initial upgrade version, as they both have the data service and
	// the third server class is on the initial version as it has the index service
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[2].Name, f.CouchbaseServerImageUpgrade, initialVersion)

	// Set the previous version pod count to 0 to start the upgrade of the final server class
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	// Wait for the upgrade of the final server class to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[2].Name, f.CouchbaseServerImage, upgradeVersion)
}

func TestServerGroupUpgradeOrder(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	availableServerGroups := getAvailabilityZones(t, kubernetes)

	classSize := 2
	numOldPods := 2

	// Create a cluster with server groups enabled
	cluster := clusterOptionsUpgrade().WithMixedTopology(classSize).Generate(kubernetes)

	// Enable server groups
	cluster.Spec.ServerGroups = availableServerGroups[:2]

	// Set the upgrade order to upgrade by server groups
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServerGroups,
		UpgradeOrder: []string{
			availableServerGroups[1],
			availableServerGroups[0],
		},
		PreviousVersionPodCount: numOldPods,
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade of the first server group to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is still the initial version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)

	// Verify the first server group is on the initial version and the second server group is upgraded
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[0], f.CouchbaseServerImageUpgrade, initialVersion)
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[1], f.CouchbaseServerImage, upgradeVersion)

	// Set the previous version pod count to 0 to start the upgrade of the remaining server groups
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	// Wait for the upgrade of the remaining server groups to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Verify the all server groups are upgraded
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[0], f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[1], f.CouchbaseServerImage, upgradeVersion)
}

// TestServerGroupUpgradeOrderWithArbiterNodes tests that arbiter nodes are upgraded last when upgrading by server groups
// and maxUpgradable is less than the size of the server group.
func TestServerGroupUpgradeOrderWithArbiterNodes(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	availableServerGroups := getAvailabilityZones(t, kubernetes)
	classSize := 2

	// Create a cluster with server groups enabled
	cluster := clusterOptionsUpgrade().WithMixedTopology(classSize).Generate(kubernetes)

	// Add an arbiter serverclass to the cluster at the start of the cluster servers list.
	cluster.Spec.Servers = append([]couchbasev2.ServerConfig{{
		Name:         "no_services",
		Size:         classSize,
		Services:     []couchbasev2.Service{},
		VolumeMounts: cluster.Spec.Servers[0].VolumeMounts.DeepCopy(),
	}}, cluster.Spec.Servers...)

	// Enable server groups
	cluster.Spec.ServerGroups = availableServerGroups[:2]

	// Set the upgrade order to upgrade by server groups
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServerGroups,
		UpgradeOrder: []string{
			availableServerGroups[1],
			availableServerGroups[0],
		},
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade of the first server group to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, k8sutil.UpgradeFinishedEvent(cluster), 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded in the expected order (with arbiter nodes upgraded last for each server group)
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(classSize * 3),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
	}

	// The arbiter nodes should have index 1 (serverGroup[0]) and 2 (serverGroup[1]). Index 0 is the initial node which cannot be an arbiter node.
	// We expect arbiter nodes to be upgraded last for each server group.
	normalUpgradeSequence := eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}

	arbiterUpgradeSequence := func(i int) eventschema.Sequence {
		return eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: couchbaseutil.CreateMemberName(cluster.Name, i)},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}
	}

	expectedEvents = append(expectedEvents, eventschema.Repeat{Times: 2, Validator: normalUpgradeSequence})
	expectedEvents = append(expectedEvents, arbiterUpgradeSequence(2))
	expectedEvents = append(expectedEvents, eventschema.Repeat{Times: 2, Validator: normalUpgradeSequence})
	expectedEvents = append(expectedEvents, arbiterUpgradeSequence(1))
	expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished})

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestNodeUpgradeOrder(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	classSize := 2
	clusterSize := classSize * 2

	// Create a cluster with server groups enabled
	cluster := clusterOptionsUpgrade().WithMixedTopology(classSize).Generate(kubernetes)

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// We have to wait until the cluster is created to set the upgrade order because the cluster has a generated name
	upgradeOrderIndexes := []int{3, 1, 2, 0}
	upgradeOrder := make([]string, len(upgradeOrderIndexes))

	for i, index := range upgradeOrderIndexes {
		upgradeOrder[i] = couchbaseutil.CreateMemberName(cluster.Name, index)
	}

	// Set the upgrade order to upgrade by server groups
	upgradeSpec := &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeNodes,
		UpgradeOrder:     upgradeOrder,
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage).Replace("/spec/upgrade", upgradeSpec), time.Minute)

	// Wait for the upgrade to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded in the expected order
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
	}

	for _, podIndex := range upgradeOrderIndexes {
		upgradeSequence := eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: couchbaseutil.CreateMemberName(cluster.Name, podIndex)},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}
		expectedEvents = append(expectedEvents, upgradeSequence)
	}

	expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished})

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestNodeUpgradeDefaultOrder(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	classSize := 2
	clusterSize := classSize * 2

	// Create a cluster with server groups enabled
	cluster := clusterOptionsUpgrade().WithMixedTopology(classSize).Generate(kubernetes)

	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeNodes,
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Before upgrading the cluster, we need to fetch the orchestrator index. If the orchestrator is at index 0,
	// we need to amend the expected upgrade order to initially skip this node. Once one node has been upgraded,
	// this should then become a new orchestrator and we can continue with the expected upgrade order.
	oNode := e2eutil.MustGetOrchestratorNode(t, kubernetes, cluster)
	oIndex, err := couchbaseutil.GetIndexFromMemberName(strings.Split(strings.Split(oNode, "@")[1], ".")[0])
	if err != nil {
		e2eutil.Die(t, err)
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade starts
	// * Each node is upgraded in the expected order
	// * Upgrade completes
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
	}

	expectedUpgradeOrderIndexes := []int{0, 1, 2, 3}

	if oIndex == 0 {
		expectedUpgradeOrderIndexes = []int{1, 0, 2, 3}
	}

	for _, podIndex := range expectedUpgradeOrderIndexes {
		upgradeSequence := eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: couchbaseutil.CreateMemberName(cluster.Name, podIndex)},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}
		expectedEvents = append(expectedEvents, upgradeSequence)
	}

	expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished})

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestServerGroupDefaultOrderUpgrade(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2)

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	availableServerGroups := getAvailabilityZones(t, kubernetes)

	classSize := 2
	numOldPods := 2

	// Create a cluster with server groups enabled
	cluster := clusterOptionsUpgrade().WithMixedTopology(classSize).Generate(kubernetes)

	serverGroups := availableServerGroups[:2]

	sort.Strings(serverGroups)

	// Enable server groups
	cluster.Spec.ServerGroups = serverGroups

	// Set the upgrade order to upgrade by server groups
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType:        couchbasev2.UpgradeOrderTypeServerGroups,
		PreviousVersionPodCount: numOldPods,
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade of the first server group to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is still the initial version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)

	// Verify the first server group is upgraded and the second server group is on the initial version
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[0], f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[1], f.CouchbaseServerImageUpgrade, initialVersion)

	// Set the previous version pod count to 0 to start the upgrade of the remaining server groups
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	// Wait for the upgrade of the remaining server groups to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Verify the all server groups are upgraded
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[0], f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerGroupPodsForVersion(t, kubernetes, cluster, availableServerGroups[1], f.CouchbaseServerImage, upgradeVersion)
}

func TestServerClassesDefaultOrderUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	numOldPods := 2
	// Static configuration.
	classSize := constants.Size2

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithMixedTopology(classSize).Generate(kubernetes)

	// Set the upgrade order to upgrade the second server class first and keep two pods on the initial version
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeOrderType:        couchbasev2.UpgradeOrderTypeServerClasses,
		PreviousVersionPodCount: numOldPods,
	}

	// Create the cluster
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the upgrade of the first server class to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is still the initial version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)

	// Verify the first server class is on the upgrade version and the second server class is on the initial version
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImageUpgrade, initialVersion)

	// Set the previous version pod count to 0 to start the upgrade of the second server class
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	// Wait for the upgrade of the second server class to finish
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the cluster version is the upgrade version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[0].Name, f.CouchbaseServerImage, upgradeVersion)
	e2eutil.MustCheckServerClassPodsForVersion(t, kubernetes, cluster, cluster.Spec.Servers[1].Name, f.CouchbaseServerImage, upgradeVersion)
}

// TestUpgradePrevent3Versions tests that the operator prevents a cluster from having 3 different versions
// by blocking image changes when the cluster is in mixed mode. It also tests rollback during mixed mode.
func TestUpgradePrevent3Versions(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size5
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)
	initialVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImageUpgrade, f.CouchbaseServerImageUpgradeVersion)

	// Create the cluster at initial version (CouchbaseServerImageUpgrade)
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	// Set previousVersionPodCount to keep 3 pods at old version during upgrade
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		PreviousVersionPodCount: 3,
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Step 1: Partial upgrade to new version with previousVersionPodCount=3
	// This should result in 2 pods at new version, 3 at old version
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify we're in mixed mode
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionMixedMode, v1.ConditionTrue, cluster, 1*time.Minute)

	// Verify we have 2 versions: 3 at old (initialVersion), 2 at new (upgradeVersion)
	expectedImageCountMapMixed := map[string]int{
		f.CouchbaseServerImageUpgrade: 3, // Old version
		f.CouchbaseServerImage:        2, // New version
	}
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMapMixed)

	// Step 2: Attempt to change to a third version while in mixed mode
	// This should be REJECTED by the DAC
	thirdVersionImage := "couchbase/server:enterprise-8.1.0" // Hypothetical third version

	// Attempt to patch to third version - this should fail validation
	// MustNotPatchCluster will die if the patch succeeds, which it shouldn't
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", thirdVersionImage))

	// Verify we're still in mixed mode with only 2 versions
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionMixedMode, v1.ConditionTrue, cluster, 1*time.Minute)
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMapMixed)

	// Step 3: Test rollback during mixed mode
	// Rollback to original version - this SHOULD be allowed because it's rolling back to status.CurrentVersion
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImageUpgrade), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify rollback complete - all pods back at initial version
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, initialVersion, time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 1*time.Minute, couchbasev2.ClusterConditionMixedMode)

	expectedImageCountMapRollback := map[string]int{
		f.CouchbaseServerImageUpgrade: clusterSize, // All back to old version
	}
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMapRollback)

	// Step 4: Upgrade again with previousVersionPodCount=3
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify we're back in mixed mode
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionMixedMode, v1.ConditionTrue, cluster, 1*time.Minute)
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMapMixed)

	// Step 5: Complete the upgrade by setting previousVersionPodCount to 0
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify upgrade is complete and mixed mode is cleared
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 1*time.Minute, couchbasev2.ClusterConditionMixedMode)

	// Verify all pods are at the new version
	expectedImageCountMapFinal := map[string]int{
		f.CouchbaseServerImage: clusterSize, // All at new version
	}
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMapFinal)
}

// TestPreviousVersionPodCountScaleUp tests that during a mixed-mode upgrade with previousVersionPodCount,
// scaling up creates new pods with the old version image when required to maintain the previousVersionPodCount.
// This validates the fix for K8S-4522.
func TestPreviousVersionPodCountScaleUp(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := 3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster at initial version (CouchbaseServerImageUpgrade)
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	// Set previousVersionPodCount to keep 1 pod at old version during upgrade
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		PreviousVersionPodCount: 1,
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Step 1: Partial upgrade with previousVersionPodCount=1
	// This should result in 2 pods at new version, 1 at old version
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify we're in mixed mode
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionMixedMode, v1.ConditionTrue, cluster, 1*time.Minute)

	// Verify we have 2 versions: 1 at old, 2 at new
	expectedImageCountMap := map[string]int{
		f.CouchbaseServerImageUpgrade: 1, // Old version
		f.CouchbaseServerImage:        2, // New version
	}
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMap)

	// Step 2a: Verify DAC rejects increasing previousVersionPodCount without scaling
	// This should be rejected by DAC since there's no scaling
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 2))

	// Step 2b: Verify DAC rejects increasing previousVersionPodCount more than new nodes being added
	// Try to add 1 node but increase previousVersionPodCount by 2 - should fail
	e2eutil.MustNotPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().
		Replace("/spec/servers/0/size", 4).
		Replace("/spec/upgrade/previousVersionPodCount", 3))

	// Step 2c: Scale up from 3 to 5 pods with previousVersionPodCount=2
	// This validates that new pods are created with the old version when required
	// Adding 2 new nodes and increasing previousVersionPodCount by 1 (from 1 to 2) is valid
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().
		Replace("/spec/upgrade/previousVersionPodCount", 2).
		Replace("/spec/servers/0/size", 5),
		time.Minute)

	// Wait for cluster to scale up and reach healthy state
	// No need to wait for Upgrading condition as this is a scale operation, not a new upgrade phase
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify we have 2 versions: 2 at old, 3 at new
	expectedImageCountMapScaled := map[string]int{
		f.CouchbaseServerImageUpgrade: 2, // Old version
		f.CouchbaseServerImage:        3, // New version
	}
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMapScaled)

	// Step 3: Complete upgrade by setting previousVersionPodCount to 0
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/upgrade/previousVersionPodCount", 0), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify upgrade is complete and mixed mode is cleared
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 1*time.Minute, couchbasev2.ClusterConditionMixedMode)

	// Verify all pods are at the new version
	expectedImageCountMapFinal := map[string]int{
		f.CouchbaseServerImage: 5, // All at new version
	}
	e2eutil.MustCheckPodImageCountMap(t, kubernetes, cluster, expectedImageCountMapFinal)
}

// TestInPlaceUpgradeRecoverabilityOnServerGroupChanges tests that the operator will revert to SwapRebalance if a pod is not recoverable
// due to a change in the cluster.spec.serverGroups list as PVC's cannot be recovered if the pod is going to be moved to a different node.
func TestInPlaceUpgradeGlobalServerGroupChangesWithPV(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2).InplaceUpgradeable()

	clusterSize := constants.Size4

	serverGroups := getAvailabilityZones(t, kubernetes)

	// Create the cluster with 2 server groups and InPlaceUpgrade enabled
	cluster := clusterOptions().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = serverGroups[:2]
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeProcess: couchbasev2.InPlaceUpgrade,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check that the cluster is correctly scheduled across the two available server groups
	expectedRzaMap := getExpectedRzaResultMap(clusterSize, serverGroups[:2])
	expectedRzaMap.mustValidateRzaMap(t, kubernetes, cluster)

	time.Sleep(10 * time.Second) // Wait for a reconcile loop to happen

	// Remove the second zone from the cluster.spec.serverGroups list
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/serverGroups", serverGroups[:1]), time.Minute)

	// Wait for the RZA map to reflect the new server group configuration
	expectedRzaMap = getExpectedRzaResultMap(clusterSize, serverGroups[:1])
	MustWaitForRzaMap(t, kubernetes, cluster, expectedRzaMap, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the expected events
	// * Cluster created
	// * Upgrade started
	// * SwapRebalance sequence (not InPlaceUpgrade like the spec says)
	// * - This should happen for each of the pods in the now removed server group
	// * Upgrade finished
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize / 2, Validator: e2eutil.SwapRebalanceSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestInPlaceUpgradeClassServerGroupChangesWithPV tests that the operator will revert to SwapRebalance if a pod is not recoverable
// due to a change in the cluster.spec.servers[i].serverGroups list as PVC's cannot be recovered if the pod is going to be moved to a different node.
func TestInPlaceUpgradeClassServerGroupChangesWithPV(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2).InplaceUpgradeable()

	clusterSize := constants.Size4

	serverGroups := getAvailabilityZones(t, kubernetes)

	// Create the cluster with 2 server groups and InPlaceUpgrade enabled
	cluster := clusterOptions().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].ServerGroups = serverGroups[:2]
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeProcess: couchbasev2.InPlaceUpgrade,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check that the cluster is correctly scheduled across the two available server groups
	expectedRzaMap := getExpectedRzaResultMap(clusterSize, serverGroups[:2])
	expectedRzaMap.mustValidateRzaMap(t, kubernetes, cluster)

	// Remove the second zone from the cluster.spec.serverGroups list
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/servers/0/serverGroups", serverGroups[:1]), time.Minute)

	time.Sleep(10 * time.Second) // Wait for a reconcile loop to happen

	// Wait for the RZA map to reflect the new server group configuration
	expectedRzaMap = getExpectedRzaResultMap(clusterSize, serverGroups[:1])
	MustWaitForRzaMap(t, kubernetes, cluster, expectedRzaMap, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the expected events
	// * Cluster created
	// * Upgrade started
	// * SwapRebalance sequence (not InPlaceUpgrade like the spec says)
	// * - This should happen for each of the pods in the now removed server group
	// * Upgrade finished
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize / 2, Validator: e2eutil.SwapRebalanceSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestInPlaceUpgradeNodeSelectorZoneChangesWithPV tests that the operator will revert to SwapRebalance if a pod is not recoverable
// due to a change in the cluster.spec.servers[i].pod.spec.nodeSelector as PVC's cannot be recovered if the pod is going to be moved to a different node.
func TestInPlaceUpgradeNodeSelectorZoneChangesWithPV(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2).InplaceUpgradeable()

	clusterSize := constants.Size4

	serverGroups := getAvailabilityZones(t, kubernetes)

	// Create the cluster with one server group in the nodeselector
	cluster := clusterOptions().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Pod = &couchbasev2.PodTemplate{
		Spec: v1.PodSpec{
			NodeSelector: map[string]string{
				constants.FailureDomainZoneLabel: serverGroups[0],
			},
		},
	}
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeProcess: couchbasev2.InPlaceUpgrade,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check that the cluster is correctly scheduled across the only available server group
	expectedRzaMap := getExpectedRzaResultMap(clusterSize, serverGroups[:1])
	expectedRzaMap.mustValidateRzaMap(t, kubernetes, cluster)

	// Replace the node selector zone with the second zone in the list
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/servers/0/pod/spec/nodeSelector", map[string]string{
		constants.FailureDomainZoneLabel: serverGroups[1],
	}), time.Minute)

	time.Sleep(10 * time.Second) // Wait for a reconcile loop to happen

	// Wait for the RZA map to reflect the new server group configuration
	expectedRzaMap = getExpectedRzaResultMap(clusterSize, serverGroups[1:2])
	MustWaitForRzaMap(t, kubernetes, cluster, expectedRzaMap, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the expected events
	// * Cluster created
	// * Upgrade started
	// * SwapRebalance sequence (not InPlaceUpgrade like the spec says)
	// * - This should happen for all pods given we've moved from serverGroups[0] to serverGroups[1]
	// * Upgrade finished
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: e2eutil.SwapRebalanceSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestInPlaceUpgradeNodeSelectorZoneRemovedWithPV tests the Operator will upgrade pods in place if the explicit node selector is removed
// but PVC's already exist for the pod, meaning the pods can be recreated in place as the existing zone's are still valid.
func TestInPlaceUpgradeNodeSelectorZoneRemovedWithPV(t *testing.T) {
	f := framework.Global

	// TODO Check that no pod changes happen, given that the "old" server groups are still valid

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2).InplaceUpgradeable()

	clusterSize := constants.Size4

	serverGroups := getAvailabilityZones(t, kubernetes)

	// Create the cluster with one server group in the nodeselector
	cluster := clusterOptions().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Pod = &couchbasev2.PodTemplate{
		Spec: v1.PodSpec{
			NodeSelector: map[string]string{
				constants.FailureDomainZoneLabel: serverGroups[0],
			},
		},
	}
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeProcess: couchbasev2.InPlaceUpgrade,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check that the cluster is correctly scheduled across the only available server group
	expectedRzaMap := getExpectedRzaResultMap(clusterSize, serverGroups[:1])
	expectedRzaMap.mustValidateRzaMap(t, kubernetes, cluster)

	// Remove the zone node selector from the server class
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/servers/0/pod"), time.Minute)

	time.Sleep(10 * time.Second) // Wait for a reconcile loop to happen

	// There should not be any changes in the RZA map as pods should be re-created in place as the existing zone's are still valid
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewUpgradeFinishedEvent(cluster), 20*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// We can't check for the rza map as the pods will not have zone selectors as none exist in the spec,
	// but the events should show InPlaceUpgrade happening for each pod instead of SwapRebalance,
	// meaning they must have been re-created in place to allow for the PVC's to be recovered.

	// Verify the expected events
	// * Cluster created
	// * Upgrade started
	// * Given the pod spec has changed (removed explicit nodeselector), every node should be upgraded via InPlaceUpgrade
	// - This does not consist of a new member being added but an existing member recreated and rebalanced into the cluster
	// * Upgrade finished
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: eventschema.Sequence{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		}}},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestChangeClusterSettingsDuringUpgradeStabilizationPeriod(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	clusterSize := constants.Size2
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	stabilizationPeriodS := int64(60)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	var cluster *couchbasev2.CouchbaseCluster
	cluster = clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		StabilizationPeriod: e2espec.NewDurationS(stabilizationPeriodS),
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// When the cluster is ready, start the upgrade.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Patch the cluster while it's waiting for the stabilization period to end.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionWaitingBetweenUpgrades, v1.ConditionTrue, cluster, 10*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", e2espec.NewDurationS(33)), time.Minute)
	e2eutil.MustPatchAutoFailoverInfo(t, kubernetes, cluster, jsonpatch.NewPatchSet().Test("/Timeout", int64(33)), time.Minute)

	// Wait for the upgrade to finish and check the versions are correct.
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewUpgradeFinishedEvent(cluster), 20*time.Minute)
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Upgrade started
	// * SwapRebalance sequence
	// * Cluster settings edited
	// * SwapRebalance sequence
	// * Upgrade finished
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: 1, Validator: e2eutil.SwapRebalanceSequence},
		eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		eventschema.Repeat{Times: clusterSize - 1, Validator: e2eutil.SwapRebalanceSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestInPlaceUpgradeServerGroupZoneAddedWithPV tests that the operator properly handles
// server group zone addition during InPlaceUpgrade with PVCs by using SwapRebalance
// instead of going into an infinite loop.
func TestInPlaceUpgradeServerGroupZoneAddedWithPV(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).ServerGroups(2).InplaceUpgradeable()

	clusterSize := constants.Size2

	serverGroups := getAvailabilityZones(t, kubernetes)

	// Create the cluster with 1 server group initially, PVCs, and InPlaceUpgrade enabled
	cluster := clusterOptions().WithPersistentTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = serverGroups[:1]
	cluster.ObjectMeta.Annotations = map[string]string{"cao.couchbase.com/rescheduleDifferentServerGroup": "false"}
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeProcess: couchbasev2.InPlaceUpgrade,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check that the cluster is correctly scheduled across the single server group
	expectedRzaMap := getExpectedRzaResultMap(clusterSize, serverGroups[:1])
	expectedRzaMap.mustValidateRzaMap(t, kubernetes, cluster)

	// Add a second zone (server group becomes available)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/serverGroups", serverGroups[:2]), time.Minute)

	// Wait for the RZA map to reflect the new server group configuration
	expectedRzaMap = getExpectedRzaResultMap(clusterSize, serverGroups[:2])
	MustWaitForRzaMap(t, kubernetes, cluster, expectedRzaMap, 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
}

// TestInPlaceUpgradeIndexNodesWithSwapRebalanceOverride tests that index nodes are upgraded with SwapRebalance when the override is set,
// even if InPlaceUpgrade is specified as the upgrade process. It also checks that index nodes that persist on pods that are also running the data
// service will continue to be upgraded via InPlaceUpgrade.
func TestInPlaceUpgradeIndexNodesWithSwapRebalanceOverride(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable()

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	serverClassSize := constants.Size2

	// Initialise the cluster. This should have 3 service classes: data + index, query and index only,
	// and the index swap rebalance override enabled.
	// The names of the services are in order: default (idx + data), index-only, data-only
	cluster := clusterOptionsUpgrade().WithPersistentTopology(serverClassSize).Generate(kubernetes)

	idxOnly := cluster.Spec.Servers[0].DeepCopy()
	idxOnly.Name = "index-only"
	idxOnly.Services = idxOnly.Services[1:] // Only index service
	cluster.Spec.Servers = append(cluster.Spec.Servers, *idxOnly)
	dataOnly := cluster.Spec.Servers[0].DeepCopy()
	dataOnly.Name = "data-only"
	dataOnly.Services = dataOnly.Services[:1] // Only data service
	cluster.Spec.Servers = append(cluster.Spec.Servers, *dataOnly)

	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeProcess:   couchbasev2.InPlaceUpgrade,
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeServerClasses,
		UpgradeOrder:     []string{"default", "index-only", "data-only"},
	}

	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}
	cluster.Annotations["cao.couchbase.com/upgrade.swapRebalanceIndexServiceUpgrades"] = "true"

	// Initialise the cluster, start the upgrade and wait for it to finish.
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the upgrade completed successfully.
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect. The specified upgrade order means we should see
	// 2 InPlaceUpgrades for the default (idx + data) service, then 2 SwapRebalances for the index-only service,
	// then 2 InPlaceUpgrades for the data only service.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(len(cluster.Spec.Servers) * serverClassSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: serverClassSize, Validator: eventschema.Sequence{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		}}}, // default (data + index)
		eventschema.Repeat{Times: serverClassSize, Validator: e2eutil.SwapRebalanceSequence}, // index-only service
		eventschema.Repeat{Times: serverClassSize, Validator: eventschema.Sequence{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		}}}, // data-only
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestInPlaceUpgradeIndexNodesWithSwapRebalanceOverrideAnyCandidate tests that all upgrade candidates in a reconciliation loop are upgraded with SwapRebalance
// when the override is set if any of those candidates contain the index service, even if InPlaceUpgrade is specified as the upgrade process.
// It also checks that index nodes that persist on pods that are also running the data service will continue to be upgraded via InPlaceUpgrade.
func TestInPlaceUpgradeIndexNodesWithSwapRebalanceOverrideAnyCanidate(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable()

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	serverClassSize := constants.Size2

	// Initialise the cluster. This should have 3 service classes: data + index, query and index only,
	// and the index swap rebalance override enabled.
	// The names of the services are in order: default (idx + data), index-only, data-only
	cluster := clusterOptionsUpgrade().WithPersistentTopology(serverClassSize).Generate(kubernetes)

	idxOnly := cluster.Spec.Servers[0].DeepCopy()
	idxOnly.Name = "index-only"
	idxOnly.Services = idxOnly.Services[1:] // Only index service
	cluster.Spec.Servers = append(cluster.Spec.Servers, *idxOnly)
	dataOnly := cluster.Spec.Servers[0].DeepCopy()
	dataOnly.Name = "data-only"
	dataOnly.Services = dataOnly.Services[:1] // Only data service
	cluster.Spec.Servers = append(cluster.Spec.Servers, *dataOnly)

	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{
		UpgradeProcess:   couchbasev2.InPlaceUpgrade,
		UpgradeOrderType: couchbasev2.UpgradeOrderTypeNodes,
		RollingUpgrade: &couchbasev2.RollingUpgradeConstraints{
			MaxUpgradable: 2,
		},
	}

	if cluster.Annotations == nil {
		cluster.Annotations = make(map[string]string)
	}

	cluster.Annotations["cao.couchbase.com/upgrade.swapRebalanceIndexServiceUpgrades"] = "true"

	// Initialise the cluster, start the upgrade and wait for it to finish.
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Nodes will be created in order of the server classes. Therefore:
	// 0000 and 0001 = default (idx + data)
	// 0002 and 0003 = index-only
	// 0004 and 0005 = data-only
	// Given maxUpgradable is 2, we want to configure the upgrade order
	// so two candidate groups will contain one node that has the index service only,
	// and therefore will trigger both members in that group to be upgraded via swap rebalance.
	// The final 2 candidates should be upgraded via InPlaceUpgrade.
	upgradeOrderIndexes := []int{3, 0, 2, 4, 1, 5}
	upgradeOrder := make([]string, len(upgradeOrderIndexes))
	for i, index := range upgradeOrderIndexes {
		upgradeOrder[i] = couchbaseutil.CreateMemberName(cluster.Name, index)
	}

	cluster.Spec.Upgrade.UpgradeOrder = upgradeOrder

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/upgrade/upgradeOrder", upgradeOrder), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	// Verify the upgrade completed successfully.
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect:
	// - Cluster Created
	// - Upgrade Started. MaxUpgradable set to 2, so we should see 3 upgrade sequences in total
	// - Candidates 3 (index-only) and 0 (default) = swap rebalance due to index-only presence
	// - Candidates 2 (index-only) and 4 (data-only) = swap rebalance due to index-only presence
	// - Candidates 1 and 5 = InPlaceUpgrade as no index-only candidates. These cannot be
	// upgraded together as the upgrade order is not by server group, so we expect two separate sequences here, one for each candidate.
	// - Upgrade Finished
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(len(cluster.Spec.Servers) * serverClassSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: 2, Validator: e2eutil.MultiNodeSwapRebalanceSequence(2)},
		eventschema.Repeat{Times: 2, Validator: eventschema.Sequence{Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		}}},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
