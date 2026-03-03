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

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
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
	cluster := e2espec.NewBasicCluster(clusterSize)
	cluster.Spec.HibernationStrategy = &hibernationStrategy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Hibernate the cluster, wait for the hibernating status then allow recovery.
	patchset := jsonpatch.NewPatchSet().Add("/Spec/Hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, time.Minute)

	patchset = jsonpatch.NewPatchSet().Remove("/Spec/Hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster recreated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
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
	cluster := e2espec.NewSupportableCluster(mdsGroupSize)
	cluster.Spec.HibernationStrategy = &hibernationStrategy
	cluster.Spec.RecoveryPolicy = &recoveryStrategy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Hibernate the cluster, wait for the hibernating status then allow recovery.
	patchset := jsonpatch.NewPatchSet().Add("/Spec/Hibernate", true)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, 3*time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionHibernating, v1.ConditionTrue, cluster, 3*time.Minute)

	patchset = jsonpatch.NewPatchSet().Remove("/Spec/Hibernate")
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patchset, time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Cluster recovered
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.PodDownWithPVCRecoverySequenceWithEphemeral(clusterSize, mdsGroupSize, mdsGroupSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
