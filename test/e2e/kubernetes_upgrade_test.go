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

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
)

// TestPodReadiness creates a cluster and watches the first pod that is created
// during the process.  With K8S-4601 (CNG readiness integration), the readiness gate
// is set to True immediately after a pod is CBS-initialized (not deferred until
// rebalance completes), because CNG needs the pod in EndpointSlices as soon as it
// can serve traffic.  We verify readiness stays True through the scaling process.
func TestPodReadiness(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// After K8S-4601, pod[0]'s readiness gate is set True during initialization,
	// before other members are added or rebalance runs.
	// With async pod creation, member init order is non-deterministic (0002 may
	// init before 0001), so use MustObserveClusterEvent for both to avoid missing
	// an already-emitted event.
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, 1), 5*time.Minute)
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 0, v1.ConditionTrue, time.Minute)
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, 2), 5*time.Minute)
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 0, v1.ConditionTrue, time.Minute)
}

// TestKubernetesRollingUpgrade simulates a Kubernetes rolling upgrade.  We taint
// each node in turn with a NoExecute and zero grace period.  We wait a small amount
// of time (say 30 seconds) to let deployments do their thing, respecting any pod
// disruption budgets before continuing on to the next node.  Expect each Couchbase pod
// to be evicted and recovered in no particular order.  The operator will be evicted
// at some random point, and we expect this to be transparent.
func TestKubernetesRollingUpgrade(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	framework.Requires(t, kubernetes).StaticCluster().Rethink()

	// Static configuration.
	clusterSize := 3

	// Dynamic configuration.  We need at least the cluster size of nodes, plus one
	// to allocate the new pod into.
	if e2eutil.MustNumNodes(t, kubernetes) < (clusterSize + 1) {
		t.Skip("insufficient kubernetes nodes")
	}

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.AntiAffinity = true
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Ensure the nodes are cleaned up afterwards whatever happens.
	defer e2eutil.MustUntaintAll(t, kubernetes)

	// Perform the upgrade.
	// If you are lucky eveictions happen clusterSize times, if not then the size of the
	// Kubernetes cluster size, so scale the timeout accordingly.
	e2eutil.MustRollingUpgrade(t, kubernetes, 5*time.Duration(e2eutil.MustNumNodes(t, kubernetes))*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * At least clusterSize evictions happened, causing a down, fail and recovery.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		e2eutil.KubernetesUpgradeSequenceEphemeral(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
