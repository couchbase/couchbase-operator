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
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
)

// TestPodReadiness creates a cluster and watches the first pod that is created
// during the process.  We expect its 'Ready' condition to be 'False' and the reason
// to be 'ReadinessGatesNotReady' until the cluster is fully balanced, at which
// point, now data is distributed, and we can tolerate a deletion, it turns to 'True'.
func TestPodReadiness(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2espec.NewBasicCluster(clusterSize)
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// Wait for the other members to come up, expecting the pod to stay unready
	// until after rebalance.
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, 1), 5*time.Minute)
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 0, v1.ConditionFalse, time.Minute)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.NewMemberAddEvent(cluster, 2), 5*time.Minute)
	e2eutil.MustValidatePodReadiness(t, kubernetes, cluster, 0, v1.ConditionFalse, time.Minute)
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

	// Static configuration.
	clusterSize := 3

	// Dynamic configuration.  We need at least the cluster size of nodes, plus one
	// to allocate the new pod into.
	if e2eutil.MustNumNodes(t, kubernetes) < (clusterSize + 1) {
		t.Skip("insufficient kubernetes nodes")
	}

	// Create the cluster.
	cluster := e2espec.NewBasicCluster(clusterSize)
	cluster.Spec.AntiAffinity = true
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Ensure the nodes are cleaned up afterwards whatever happens.
	cleanupTaints := func() {
		_ = f.RemoveK8SNodeTaints(kubernetes.KubeClient)
	}
	defer cleanupTaints()

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
