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

	v1 "k8s.io/api/core/v1"
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
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	},
}

// rollingUpgradeSequence is what to expect when a cluster is upgraded all at once.
func rollingUpgradeSequence(clusterSize, maxNumber int) eventschema.Validatable {
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
			eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
			// once member is recovered the Operator will proceed with upgrade
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			// followed by removal of node that was upgraded pre & post failure
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
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
							eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
						},
					},
					// ... or the operator notices it's gone pop, aborts the topology reconcile
					// and next time around the pod is already in failed add.
					eventschema.Sequence{
						Validators: []eventschema.Validatable{
							eventschema.Event{Reason: k8sutil.EventReasonFailedAddNode, FuzzyMessage: victimName},
						},
					},
				},
			},
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						// I wonder why this is the only case where a member removed event doesn't happen?
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
					},
				},
			},
		},
	}
}

// upgradeDownRecoverableSequence is a common sequence for generating events for a new
// member being added, the pod being killed during a rebalance and the recovery steps.
func upgradeDownRecoverableSequence(victimName string) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
			eventschema.Event{Reason: k8sutil.EventReasonMemberDown, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver, FuzzyMessage: victimName},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered, FuzzyMessage: victimName},
			// Server sometimes gets a bit stuck...
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
						eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
					},
				},
			},
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
					},
				},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
					},
				},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
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

// TestUpgrade upgrades a three node cluster.
func TestUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := constants.Size3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage)

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)

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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimIndex)

	// When the cluster is ready, start the upgrade.  When the victim pod is balancing in
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 10*time.Minute)
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
		upgradeDownRecoverableSequence(victimName),
		eventschema.Repeat{Times: clusterSize - victimCycle, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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
	clusterSize := mdsGroupSize * 2
	victimCycle := 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptionsUpgrade().WithMixedTopology(mdsGroupSize).MustCreate(t, kubernetes)

	// Runtime configuration.
	victimName := couchbaseutil.CreateMemberName(cluster.Name, victimCycle)

	// When the cluster is ready, start the upgrade.  When the victim pod is balancing in
	// kill it.  The cluster should reach a healthy upgraded condition.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, victimIndex), 10*time.Minute)
	e2eutil.MustWaitForRebalanceProgress(t, kubernetes, cluster, 25.0, 5*time.Minute)
	e2eutil.MustKillPodForMember(t, kubernetes, cluster, victimCycle, false)
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
		upgradeDownRecoverableSequence(victimName),
		eventschema.Repeat{Times: clusterSize - victimCycle - 1, Validator: upgradeSequence},
		eventschema.Optional{Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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
	clusterSize := mdsGroupSize * 2
	victimCycle := mdsGroupSize + 1
	victimIndex := clusterSize + victimCycle

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	// Update the PVC storage class from none to the configure one.
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
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage)
	upgradeStrategy := couchbasev2.ImmediateUpgrade

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.UpgradeStrategy = &upgradeStrategy
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
		rollingUpgradeSequence(clusterSize, clusterSize),
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
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage)
	upgradeStrategy := couchbasev2.RollingUpgrade

	// Create the cluster, checking the version is as we expect, we need an upgrade path.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.UpgradeStrategy = &upgradeStrategy
	cluster.Spec.RollingUpgrade = &couchbasev2.RollingUpgradeConstraints{
		MaxUpgradablePercent: upgradablePercent,
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
		rollingUpgradeSequence(clusterSize, upgradeChunkSize),
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
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage)

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

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestUpgradeWithTLS tests that we can upgrade a cluster with TLS enabled.
func TestUpgradeWithTLS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage) // Static configuration.
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
