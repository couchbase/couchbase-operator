/*
Copyright 2020-Present Couchbase, Inc.

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
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
)

// TestHibernateEphemeralImmediate tests the somewhat pointless killing
// of an ephemeral cluster and the restore.
func TestHibernateEphemeralImmediate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	hibernationStrategy := couchbasev2.ImmediateHibernation

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.HibernationStrategy = &hibernationStrategy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Hibernate the cluster, wait for the hibernating status then allow recovery.
	patchset := jsonpatch.NewPatchSet().Add("/spec/hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, time.Minute)

	patchset = jsonpatch.NewPatchSet().Remove("/spec/hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster entered hibernation
	// * Cluster left hibernation
	// * Cluster recreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonHibernationStarted},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationEnded},
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestHibernateSupportableImmediate tests that a supportable cluster
// can be hibernated and brought back.
func TestHibernateSupportableImmediate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	mdsGroupSize := constants.Size2
	clusterSize := mdsGroupSize * 2
	hibernationStrategy := couchbasev2.ImmediateHibernation
	recoveryStrategy := couchbasev2.PrioritizeUptime

	// Create the cluster.
	cluster := clusterOptions().WithMixedTopology(mdsGroupSize).Generate(kubernetes)
	cluster.Spec.HibernationStrategy = &hibernationStrategy
	cluster.Spec.RecoveryPolicy = &recoveryStrategy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Hibernate the cluster, wait for the hibernating status then allow recovery.
	patchset := jsonpatch.NewPatchSet().Add("/spec/hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, 3*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 3*time.Minute)

	patchset = jsonpatch.NewPatchSet().Remove("/spec/hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster entered hibernation
	// * Cluster left hibernation
	// * Cluster recovered
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonHibernationStarted},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationEnded},
		e2eutil.PodDownWithPVCRecoverySequenceWithEphemeral(t, clusterSize, mdsGroupSize, mdsGroupSize, f.CouchbaseServerImage),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestHibernateOccursAfterUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	// Static configuration.
	clusterSize := 3
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	// Create the cluster.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Upgrade the cluster to the new version.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// Wait for the cluster to start upgrading then enable hibernation.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)

	patchset := jsonpatch.NewPatchSet().Add("/spec/hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)

	// Expect the cluster to enter hibernation at some point.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 10*time.Minute)

	// Disable hibernation and expect the cluster to recover back to the upgraded version.
	patchset = jsonpatch.NewPatchSet().Remove("/spec/hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Validate the events match what we expect:
	// * Cluster created
	// * Cluster upgraded
	// * Cluster entered hibernation
	// * Cluster left hibernation
	// * Cluster recreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		RollingUpgradeSequence(clusterSize, 1),
		eventschema.Event{Reason: k8sutil.EventReasonHibernationStarted},
		eventschema.Event{Reason: k8sutil.EventReasonHibernationEnded},
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
