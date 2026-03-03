/*
Copyright 2025-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	opconst "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/clustercapabilities"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestServerGroupAddRedistributesPods verifies that when a server group is
// re-added to the cluster spec in STABLE mode the operator will redistribute
// existing pods into the newly added group (A* reschedule -> swap rebalance).
func TestServerGroupAddRedistributesPods(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()
	framework.Requires(t, kubernetes).StaticCluster()
	caps := clustercapabilities.MustNewCapabilities(t, kubernetes.KubeClient)

	available := caps.AvailabilityZones
	if len(available) < 3 {
		t.Skip("need at least 3 availability zones to run this test")
	}

	// Create cluster with specified number of pods for redistribution testing
	// Use a single server class (ephemeral) so A* is exercised per-class
	const podCount = 3
	cluster := clusterOptions().WithEphemeralTopology(podCount).Generate(kubernetes)
	cluster.Spec.ServerGroups = []string{available[0], available[1], available[2]}
	// Ensure the single server config uses all groups
	cluster.Spec.Servers[0].ServerGroups = []string{available[0], available[1], available[2]}
	cluster.ObjectMeta.Annotations = map[string]string{"cao.couchbase.com/rescheduleDifferentServerGroup": "false"}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Remove one server group (simulate previously removed)
	removed := available[2]
	newGroups := []string{available[0], available[1]}
	patch := jsonpatch.NewPatchSet().
		Replace("/spec/serverGroups", newGroups).
		Replace("/spec/servers/0/serverGroups", newGroups)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patch, time.Minute)

	// Wait for the operator to handle the removed server group
	// This may happen via SwapRebalance (pod recreation) if pods are unrecoverable
	// In stable mode, we expect a full rebalance (RebalanceCompleted event indicates the operation finished)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Replace serverGroups to include the removed group again
	finalGroups := []string{available[0], available[1], available[2]}
	patch3 := jsonpatch.NewPatchSet().
		Replace("/spec/serverGroups", finalGroups).
		Replace("/spec/servers/0/serverGroups", finalGroups)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patch3, time.Minute)

	// In stable mode, we expect A* to proactively redistribute pods into the re-added group
	// Wait for the RebalanceCompleted event which indicates the redistribution has finished
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Verify at least one pod was redistributed into the re-added server group
	pods, err := kubernetes.KubeClient.CoreV1().Pods(cluster.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", opconst.LabelCluster, cluster.Name)})
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}

	found := false

	for _, p := range pods.Items {
		if p.Spec.NodeSelector != nil {
			if sg, ok := p.Spec.NodeSelector[opconst.ServerGroupLabel]; ok && sg == removed {
				found = true
				break
			}
		}
	}

	if !found {
		t.Fatalf("Expected at least one pod to be redistributed into re-added server group %s in stable mode", removed)
	}

	t.Logf("Good: A* proactive rebalancing occurred in stable mode - pod redistributed into re-added server group %s", removed)
}

// TestServerGroupAddNoRedistributeUnstable verifies that when a server group is
// re-added to the cluster spec in UNSTABLE mode (rescheduleDifferentServerGroup=true)
// the operator will NOT proactively redistribute existing pods into the newly
// added group (no A*), i.e. existing pods remain where they are.
func TestServerGroupAddNoRedistributeUnstable(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTestExclusive(t)
	defer cleanup()
	framework.Requires(t, kubernetes).StaticCluster()
	caps := clustercapabilities.MustNewCapabilities(t, kubernetes.KubeClient)

	available := caps.AvailabilityZones
	if len(available) < 3 {
		t.Skip("need at least 3 availability zones to run this test")
	}

	// Create cluster with specified number of pods
	const podCount = 3
	cluster := clusterOptions().WithEphemeralTopology(podCount).Generate(kubernetes)
	cluster.Spec.ServerGroups = []string{available[0], available[1], available[2]}
	cluster.Spec.Servers[0].ServerGroups = []string{available[0], available[1], available[2]}
	// Set unstable mode at creation time
	cluster.ObjectMeta.Annotations = map[string]string{"cao.couchbase.com/rescheduleDifferentServerGroup": "true"}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Remove one server group (simulate previously removed)
	removed := available[2]
	newGroups := []string{available[0], available[1]}
	patch := jsonpatch.NewPatchSet().
		Replace("/spec/serverGroups", newGroups).
		Replace("/spec/servers/0/serverGroups", newGroups)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patch, time.Minute)

	// Wait for the operator to reschedule the pod from the removed zone
	// This may happen via SwapRebalance (pod recreation) if pods are unrecoverable
	// Wait for the SwapRebalance to complete (RebalanceCompleted event indicates the operation finished)
	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Verify the pod was actually moved out of the removed zone
	pods := e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, podCount, 2*time.Minute)
	podsInRemovedZone := 0

	for _, p := range pods {
		if p.Spec.NodeSelector != nil {
			if sg, ok := p.Spec.NodeSelector[opconst.ServerGroupLabel]; ok && sg == removed {
				podsInRemovedZone++
			}
		}
	}

	if podsInRemovedZone > 0 {
		t.Fatalf("Expected pod to be moved out of removed zone %s, but %d pods still there", removed, podsInRemovedZone)
	}

	t.Logf("Good: Pod was successfully moved out of removed zone %s", removed)

	// Replace serverGroups to include the removed group again
	finalGroups := []string{available[0], available[1], available[2]}
	patch3 := jsonpatch.NewPatchSet().Replace("/spec/serverGroups", finalGroups)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, patch3, time.Minute)

	// In unstable mode we expect NO proactive redistribution into the re-added group
	// Wait for the cluster to settle, but we should NOT see a new rebalance event
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)

	// Give the operator a chance to potentially start a rebalance (it shouldn't in unstable mode)
	time.Sleep(30 * time.Second)

	// Verify the re-added server group remains empty (no proactive redistribution)
	pods = e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, podCount, 1*time.Minute)
	for _, p := range pods {
		if p.Spec.NodeSelector != nil {
			if sg, ok := p.Spec.NodeSelector[opconst.ServerGroupLabel]; ok && sg == removed {
				t.Fatalf("unexpected pod moved into re-added server group %s in unstable mode", removed)
			}
		}
	}

	t.Logf("Good: No proactive redistribution occurred in unstable mode - re-added server group %s remains empty", removed)
}
