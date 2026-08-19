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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// overrideLabelKey is the custom node label used as the server-group key in these tests — a stand-in
// for eks.amazonaws.com/nodegroup. Nodes are labelled with it directly, so the tests need no real
// availability zones and run on a plain kind cluster.
const overrideLabelKey = "cao.couchbase.com/e2e-placement-group"

// Annotation serverGroupsLabelOverride.
const overrideAnnotation = "cao.couchbase.com/serverGroupsLabelOverride"

// overrideZone is the availability zone declared on every server class in single-AZ override tests. It is a
// bookkeeping value (not a node selector), so it needs no matching node label — kind-friendly.
const overrideZone = "e2e-zone-a"

// schedulableNodes returns nodes a Couchbase pod could be scheduled on (no control-plane role, no
// NoSchedule/NoExecute taint).
func schedulableNodes(t *testing.T, k8s *types.Cluster) []*corev1.Node {
	out := []*corev1.Node{}

	for _, node := range e2eutil.MustNodes(t, k8s) {
		if _, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]; isControlPlane {
			continue
		}

		blocked := false
		for _, taint := range node.Spec.Taints {
			if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
				blocked = true
				break
			}
		}

		if !blocked {
			out = append(out, node)
		}
	}

	return out
}

// labelNodes stamps key=<group> on each node, round-robin across groups, and returns a cleanup that
// restores the prior label state.
func labelNodes(t *testing.T, k8s *types.Cluster, nodes []*corev1.Node, key string, groups []string) func() {
	originals := map[string]string{} // node name → prior value ("" means the label was absent)

	patch := func(name, value string) {
		var body string
		if value == "" {
			body = fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, key)
		} else {
			body = fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, key, value)
		}

		if _, err := k8s.KubeClient.CoreV1().Nodes().Patch(context.Background(), name, k8stypes.MergePatchType, []byte(body), metav1.PatchOptions{}); err != nil {
			e2eutil.Die(t, err)
		}
	}

	for i, node := range nodes {
		originals[node.Name] = node.Labels[key]
		patch(node.Name, groups[i%len(groups)])
	}

	return func() {
		for name, value := range originals {
			patch(name, value)
		}
	}
}

// isEKS returns true if the cluster infrastructure matches AWS EKS. This is used in tests
// where the test behavior is different on EKS vs kind.
func isEKS(nodes []*corev1.Node) bool {
	for _, node := range nodes {
		// EKS nodes use the "aws://" provider scheme (e.g., aws:///us-east-1a/i-xxxxxx)
		if strings.HasPrefix(node.Spec.ProviderID, "aws://") {
			return true
		}
		// Alternatively, inspect well-known EKS node group labels
		for labelKey := range node.Labels {
			if strings.HasPrefix(labelKey, "eks.amazonaws.com") {
				return true
			}
		}
	}
	return false
}

// addDefaultPersistentVolume gives the first server class a default persistent volume claim, so PVCs
// exist. detectZoneChange short-circuits to in-place when there is no volume (pvcState == nil), so a
// swap-vs-in-place test MUST use persistent storage.
func addDefaultPersistentVolume(cluster *couchbasev2.CouchbaseCluster) {
	f := framework.Global
	pvcName := e2eutil.GetPvcName(f.LocalPV)
	cluster.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{DefaultClaim: pvcName}
	cluster.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		createPersistentVolumeClaimSpec(f.StorageClassName, pvcName, f.LocalPV, 1),
	}
}

// useInPlaceUpgrade opts the cluster into InPlaceUpgrade via spec.upgrade (the spec.upgradeProcess
// field is deprecated and overridden by the CRD default on spec.upgrade). Without it
// GetUpgradeProcess() defaults to SwapRebalance and every spec change swap-rebalances.
func useInPlaceUpgrade(cluster *couchbasev2.CouchbaseCluster) {
	cluster.Spec.Upgrade = &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.InPlaceUpgrade}
}

// couchbasePods lists the cluster's server pods.
func couchbasePods(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) []corev1.Pod {
	pods, err := k8s.KubeClient.CoreV1().Pods(cluster.Namespace).List(context.Background(),
		metav1.ListOptions{LabelSelector: constants.CouchbaseServerClusterKey + "=" + cluster.Name})
	if err != nil {
		e2eutil.Die(t, err)
	}

	return pods.Items
}

// validateOverrideScheduling asserts every server pod is scheduled by the override key (not the
// default zone key), into one of the declared groups, and that every group is used.
func validateOverrideScheduling(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, groups []string) {
	allowed := map[string]bool{}
	for _, g := range groups {
		allowed[g] = true
	}

	seen := map[string]int{}
	for _, pod := range couchbasePods(t, k8s, cluster) {
		group := pod.Spec.NodeSelector[overrideLabelKey]
		switch {
		case group == "":
			e2eutil.Die(t, fmt.Errorf("pod %s has no %q node selector — override not applied", pod.Name, overrideLabelKey))
		case !allowed[group]:
			e2eutil.Die(t, fmt.Errorf("pod %s scheduled into group %q not in %v", pod.Name, group, groups))
		}

		seen[group]++
	}

	for _, g := range groups {
		if seen[g] == 0 {
			e2eutil.Die(t, fmt.Errorf("group %q got no pods; distribution %v", g, seen))
		}
	}
}

// mustAllPodsInZone asserts every server pod is pinned to the given zone.
func mustAllPodsInZone(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, zone string) {
	for _, pod := range couchbasePods(t, k8s, cluster) {
		if got := pod.Spec.NodeSelector[constants.FailureDomainZoneLabel]; got != zone {
			e2eutil.Die(t, fmt.Errorf("pod %s in zone %q, want %q", pod.Name, got, zone))
		}
	}
}

// memberNames returns the set of server pod names (member identities).
func memberNames(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster) map[string]bool {
	names := map[string]bool{}
	for _, pod := range couchbasePods(t, k8s, cluster) {
		names[pod.Name] = true
	}

	return names
}

// setupSingleAZTopology prepares the environment for single-AZ tests, returning filtered nodes,
// the active zone string, and a cleanup function to restore labels.
func setupSingleAZTopology(t *testing.T, k8s *types.Cluster, nodes []*corev1.Node) ([]*corev1.Node, string, func()) {
	var zone string
	var cleanups []func()

	if isEKS(nodes) {
		if len(nodes) > 0 {
			zone = nodes[0].Labels[constants.FailureDomainZoneLabel]
		}
		var zoneNodes []*corev1.Node
		for _, n := range nodes {
			if n.Labels[constants.FailureDomainZoneLabel] == zone {
				zoneNodes = append(zoneNodes, n)
			}
		}
		nodes = zoneNodes
	} else {
		zone = overrideZone
		cleanups = append(cleanups, labelNodes(t, k8s, nodes, constants.FailureDomainZoneLabel, []string{zone}))
	}

	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	return nodes, zone, cleanup
}

// setupMultiAZTopology prepares the cluster for multi-AZ swap testing, validating zone counts
// and returning target zones alongside a labeling cleanup function.
func setupMultiAZTopology(t *testing.T, k8s *types.Cluster, nodes []*corev1.Node) (string, string, func()) {
	var zoneA, zoneB string
	var cleanups []func()

	if isEKS(nodes) {
		zoneMap := map[string]bool{}
		for _, n := range nodes {
			if z := n.Labels[constants.FailureDomainZoneLabel]; z != "" {
				zoneMap[z] = true
			}
		}
		var actualZones []string
		for z := range zoneMap {
			actualZones = append(actualZones, z)
		}

		if len(actualZones) < 2 {
			t.Skip("Multi-AZ swap test requires an EKS cluster provisioned across at least 2 Availability Zones")
		}
		zoneA, zoneB = actualZones[0], actualZones[1]

		var nodesA, nodesB []*corev1.Node
		for _, n := range nodes {
			switch n.Labels[constants.FailureDomainZoneLabel] {
			case zoneA:
				nodesA = append(nodesA, n)
			case zoneB:
				nodesB = append(nodesB, n)
			}
		}

		if len(nodesA) == 0 || len(nodesB) == 0 {
			t.Skip("Need at least one node in each AWS AZ to perform multi-AZ swap testing")
		}

		cleanups = append(cleanups, labelNodes(t, k8s, nodesA, overrideLabelKey, []string{"pg-a"}))
		cleanups = append(cleanups, labelNodes(t, k8s, nodesB, overrideLabelKey, []string{"pg-b"}))
	} else {
		if len(nodes) < 2 {
			t.Skipf("need >=2 schedulable nodes (one per simulated zone), have %d", len(nodes))
		}
		zoneA, zoneB = "za", "zb"
		cleanups = append(cleanups, labelNodes(t, k8s, nodes, constants.FailureDomainZoneLabel, []string{zoneA, zoneB}))
		cleanups = append(cleanups, labelNodes(t, k8s, nodes, overrideLabelKey, []string{"pg-a", "pg-b"}))
	}

	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	return zoneA, zoneB, cleanup
}

// TestServerGroupOverrideSchedulesByCustomLabel verifies that, with serverGroupsLabelOverride set,
// the operator stripes pods across the override label (placement groups) instead of the default
// topology.kubernetes.io/zone label (single AZ — every class declares the same zone).
func TestServerGroupOverrideSchedulesByCustomLabel(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	nodes := schedulableNodes(t, kubernetes)
	nodes, zone, topoCleanup := setupSingleAZTopology(t, kubernetes, nodes)
	defer topoCleanup()

	if len(nodes) < 2 {
		t.Skipf("need >=2 nodes in zone %s to stripe across placement groups, have %d", zone, len(nodes))
	}

	groupCount := 3
	if len(nodes) < groupCount {
		groupCount = len(nodes)
	}

	groups := make([]string, groupCount)
	for i := range groups {
		groups[i] = fmt.Sprintf("pg-%d", i)
	}

	defer labelNodes(t, kubernetes, nodes, overrideLabelKey, groups)()

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(groupCount).Generate(kubernetes)
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	cluster.Annotations[overrideAnnotation] = overrideLabelKey
	cluster.Spec.ServerGroups = groups
	// The override requires every server class to declare its zone in the pod template.
	cluster.Spec.Servers[0].Pod = &couchbasev2.PodTemplate{Spec: corev1.PodSpec{
		NodeSelector: map[string]string{constants.FailureDomainZoneLabel: zone},
	}}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	validateOverrideScheduling(t, kubernetes, cluster, groups)
}

// TestServerGroupOverrideUpgradeIsInPlace verifies the in-place benefit: a version upgrade of
// a single-AZ placement-group cluster is applied IN PLACE — pods stay on their nodes (in their PGs)
// and are recreated, NOT swap-rebalanced. detectZoneChange must not treat the placement group as an
// availability zone and force a cross-AZ swap. (A deliberate serverGroups change DOES swap — the pod
// moves to a different node and the PVC can't follow — see TestInPlaceUpgradeClassServerGroupChangesWithPV.)
// Asserts the member-name set is unchanged; a swap would add a new member and eject an old one.
func TestServerGroupOverrideUpgradeIsInPlace(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).InplaceUpgradeable()

	nodes := schedulableNodes(t, kubernetes)
	nodes, zone, topoCleanup := setupSingleAZTopology(t, kubernetes, nodes)
	defer topoCleanup()

	// Lower the baseline constraint to require at least 2 nodes for custom label striping
	if len(nodes) < 2 {
		t.Skipf("need >=2 nodes in zone %s to stripe across placement groups, have %d", zone, len(nodes))
	}

	groupCount := 3
	if len(nodes) < groupCount {
		groupCount = len(nodes)
	}

	groups := make([]string, groupCount)
	for i := range groups {
		groups[i] = fmt.Sprintf("pg-%d", i)
	}
	defer labelNodes(t, kubernetes, nodes, overrideLabelKey, groups)()

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create at the upgrade-from image: override + per-class zone + InPlaceUpgrade + persistent volumes.
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(3).Generate(kubernetes)
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	cluster.Annotations[overrideAnnotation] = overrideLabelKey
	cluster.Spec.ServerGroups = groups
	// The override requires every server class to declare its zone in the pod template.
	cluster.Spec.Servers[0].Pod = &couchbasev2.PodTemplate{Spec: corev1.PodSpec{
		NodeSelector: map[string]string{constants.FailureDomainZoneLabel: zone},
	}}
	addDefaultPersistentVolume(cluster)
	useInPlaceUpgrade(cluster)

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	validateOverrideScheduling(t, kubernetes, cluster, groups)

	before := memberNames(t, kubernetes, cluster)

	// Upgrade the image. The pods don't change PG or node, so detectZoneChange stays false and the
	// pods upgrade in place rather than swap-rebalancing.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster,
		jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	validateOverrideScheduling(t, kubernetes, cluster, groups)

	after := memberNames(t, kubernetes, cluster)
	if len(before) != len(after) {
		e2eutil.Die(t, fmt.Errorf("member count changed (swap-rebalance?): before=%v after=%v", before, after))
	}
	for name := range before {
		if !after[name] {
			e2eutil.Die(t, fmt.Errorf("member %s was replaced (swap-rebalance, not in-place?): before=%v after=%v", name, before, after))
		}
	}
}

// TestServerGroupOverrideClassZoneChangeSwaps verifies the multi-AZ rule: changing a server
// class's declared availability zone forces a swap-rebalance (the volume can't follow). A
// placement-group change within the same AZ would be in-place — same code path as the same-AZ test
// above. Two zones are simulated via node labels. Persistent volumes.
func TestServerGroupOverrideClassZoneChangeSwaps(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	nodes := schedulableNodes(t, kubernetes)

	// Dynamic topology extraction/setup (multi-AZ)
	zoneA, zoneB, topoCleanup := setupMultiAZTopology(t, kubernetes, nodes)
	defer topoCleanup()

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(1).Generate(kubernetes)
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	cluster.Annotations[overrideAnnotation] = overrideLabelKey
	cluster.Spec.ServerGroups = nil
	cluster.Spec.Servers[0].ServerGroups = []string{"pg-a"}
	cluster.Spec.Servers[0].Pod = &couchbasev2.PodTemplate{}

	// Assigns the dynamic zone identifier discovered for zone A
	cluster.Spec.Servers[0].Pod.Spec.NodeSelector = map[string]string{constants.FailureDomainZoneLabel: zoneA}
	addDefaultPersistentVolume(cluster)
	useInPlaceUpgrade(cluster)

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	validateOverrideScheduling(t, kubernetes, cluster, []string{"pg-a"})
	mustAllPodsInZone(t, kubernetes, cluster, zoneA)

	before := memberNames(t, kubernetes, cluster)

	// Re-pin the class to zoneB / pg-b. The zoneA volume can't follow → must swap-rebalance.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().
		Replace("/spec/servers/0/pod/spec/nodeSelector", map[string]string{constants.FailureDomainZoneLabel: zoneB}).
		Replace("/spec/servers/0/serverGroups", []string{"pg-b"}), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, corev1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)

	validateOverrideScheduling(t, kubernetes, cluster, []string{"pg-b"})
	mustAllPodsInZone(t, kubernetes, cluster, zoneB)

	after := memberNames(t, kubernetes, cluster)
	for name := range after {
		if before[name] {
			e2eutil.Die(t, fmt.Errorf("member %s survived a cross-AZ change (expected swap-rebalance): before=%v after=%v", name, before, after))
		}
	}
}
