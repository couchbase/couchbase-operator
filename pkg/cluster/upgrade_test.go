/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"fmt"
	"slices"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// TestDetectZoneChange covers the swap-vs-in-place decision. Under an override every server class
// declares its zone via a required topology.kubernetes.io/zone pod-template node selector, so the
// requested pod always carries the zone and detectZoneChange reduces to a plain zone-key comparison.
//
// All cases fall out of the plain comparison:
//   - migrating in (no zone yet):  "" != "us-east-1a"  → swap
//   - same AZ, PG reshuffle:       "az-a" != "az-a"    → in-place
//   - cross-AZ (re-pinned class):  "az-b" != "az-a"    → swap
//   - no server groups (both ""):  "" != ""            → in-place
func TestDetectZoneChange(t *testing.T) {
	pvc := k8sutil.NewPersistentVolumeClaimState() // non-nil so the nil short-circuit doesn't fire
	override := "eks.amazonaws.com/nodegroup"
	zoneKey := constants.ServerGroupLabel

	sel := func(m map[string]string) *v1.PodSpec { return &v1.PodSpec{NodeSelector: m} }

	cases := []struct {
		name              string
		actual, requested *v1.PodSpec
		want              bool
	}{
		// Migrating in: the requested pod carries the (required) zone; an un-migrated pod has none →
		// "" != zone → swap. A real NullScheduler pod has a NIL NodeSelector (not an empty map), which
		// must still resolve to "" — this guards the nil-map short-circuit regression.
		{"migrating in (nil NodeSelector)", sel(nil), sel(map[string]string{override: "pg1", zoneKey: "az-a"}), true},
		{"migrating in (empty NodeSelector)", sel(map[string]string{}), sel(map[string]string{override: "pg1", zoneKey: "az-a"}), true},
		// Un-migrated pod already in the target zone → zones match → in-place.
		{"override, same AZ", sel(map[string]string{zoneKey: "az-a"}), sel(map[string]string{override: "pg1", zoneKey: "az-a"}), false},
		// Un-migrated pod in a different zone → zones differ → swap.
		{"override, cross AZ", sel(map[string]string{zoneKey: "az-b"}), sel(map[string]string{override: "pg1", zoneKey: "az-a"}), true},
		// Migrated pod (override label + zone): PG reshuffle within the same AZ → in-place.
		{"override, steady PG reshuffle", sel(map[string]string{override: "pg1", zoneKey: "az-a"}), sel(map[string]string{override: "pg2", zoneKey: "az-a"}), false},
		// Re-pin a class's zone → swap.
		{"override, AZ change", sel(map[string]string{override: "a1", zoneKey: "az-a"}), sel(map[string]string{override: "d1", zoneKey: "az-d"}), true},
		{"override, same AZ PG reshuffle", sel(map[string]string{override: "a1", zoneKey: "az-a"}), sel(map[string]string{override: "a2", zoneKey: "az-a"}), false},
		// No override — group value lives under the zone key.
		{"no override, zone change", sel(map[string]string{zoneKey: "az-a"}), sel(map[string]string{zoneKey: "az-b"}), true},
		{"no override, same zone", sel(map[string]string{zoneKey: "az-a"}), sel(map[string]string{zoneKey: "az-a"}), false},
		// No server groups at all — both zone selectors empty/nil → in-place.
		{"no server groups", sel(nil), sel(nil), false},
		// The zone was removed from the spec, so nothing asks the pod to move, meaning in place, which keeps the volume.
		{"zone selector removed (nil NodeSelector)", sel(map[string]string{zoneKey: "az-a"}), sel(nil), false},
		{"zone selector removed (empty NodeSelector)", sel(map[string]string{zoneKey: "az-a"}), sel(map[string]string{}), false},
		{"zone selector removed, override kept", sel(map[string]string{override: "a1", zoneKey: "az-a"}), sel(map[string]string{override: "a1"}), false},
	}

	for _, tc := range cases {
		if got := detectZoneChange(tc.actual, tc.requested, pvc); got != tc.want {
			t.Errorf("%s: detectZoneChange = %v, want %v", tc.name, got, tc.want)
		}
	}

	// No volume → never a zone-driven swap.
	if detectZoneChange(sel(map[string]string{zoneKey: "az-a"}), sel(map[string]string{zoneKey: "az-b"}), nil) {
		t.Errorf("nil pvcState: detectZoneChange = true, want false")
	}
}

// TestPvcImageToStamp checks we only add the image to a PVC that is missing it,
// and only when a running pod is there to read the real image from.
func TestPvcImageToStamp(t *testing.T) {
	couchbasePod := func(image string) *v1.Pod {
		return &v1.Pod{
			Spec: v1.PodSpec{Containers: []v1.Container{{Name: constants.CouchbaseContainerName, Image: image}}},
		}
	}

	cases := []struct {
		name string
		pvc  *v1.PersistentVolumeClaim
		pod  *v1.Pod
		want string
	}{
		{
			name: "missing annotation with running pod is stamped from the pod image",
			pvc:  &v1.PersistentVolumeClaim{},
			pod:  couchbasePod("couchbase/server:7.2.4"),
			want: "couchbase/server:7.2.4",
		},
		{
			name: "annotation already set is left alone",
			pvc: &v1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{constants.PVCImageAnnotation: "couchbase/server:7.2.4"}}},
			pod:  couchbasePod("couchbase/server:7.6.0"),
			want: "",
		},
		{
			name: "no running pod means nothing to stamp yet",
			pvc:  &v1.PersistentVolumeClaim{},
			pod:  nil,
			want: "",
		},
		{
			name: "pod being deleted is not used",
			pvc:  &v1.PersistentVolumeClaim{},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time}},
				Spec:       v1.PodSpec{Containers: []v1.Container{{Name: constants.CouchbaseContainerName, Image: "couchbase/server:7.2.4"}}},
			},
			want: "",
		},
		{
			name: "pod without a couchbase container yields no image",
			pvc:  &v1.PersistentVolumeClaim{},
			pod:  &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "sidecar", Image: "other:1.0"}}}},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pvcImageToStamp(tc.pvc, tc.pod); got != tc.want {
				t.Errorf("pvcImageToStamp = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMigrationCycleStrategy covers how we cycle a node to finish a bucket migration.
// Two things come out of it, whether we fail the node over in place instead of
// swap rebalancing it, and whether that node needs full recovery.
// Server only drops a node's override once it has rewritten that node's vBuckets,
// and delta recovery keeps the files that are already there. That is fine for an
// eviction policy change, which doesn't touch those files, but a storage backend
// change rewrites them, so delta recovery leaves the override in place and the
// migration silently never finishes.
func TestMigrationCycleStrategy(t *testing.T) {
	cases := []struct {
		name                       string
		upgradeProcess             couchbasev2.UpgradeProcess
		hasStorageBackendOverrides bool
		members                    int
		wantInPlace                bool
		wantFullRecovery           bool
	}{
		// Default. Nothing changes for users who haven't asked for inPlace upgrades.
		{"swap rebalance, eviction only", couchbasev2.SwapRebalance, false, 3, false, false},
		{"swap rebalance, backend", couchbasev2.SwapRebalance, true, 3, false, false},
		// Unset behaves as SwapRebalance, matching GetUpgradeProcess.
		{"unset", couchbasev2.UpgradeProcess(""), true, 3, false, false},

		// Eviction policy only, nothing on disk needs rewriting, so delta will do.
		{"inPlace, eviction only", couchbasev2.InPlaceUpgrade, false, 3, true, false},
		// Backend override present, on its own or alongside an eviction change, the
		// vBucket files have to be written out again, so we must not use delta.
		{"inPlace, backend", couchbasev2.InPlaceUpgrade, true, 3, true, true},
		{"deltaRecovery, eviction only", couchbasev2.DeltaRecovery, false, 3, true, false},
		{"deltaRecovery, backend", couchbasev2.DeltaRecovery, true, 3, true, true},

		// A single node has nowhere to move its data, so it takes the swap rebalance.
		{"inPlace, one member", couchbasev2.InPlaceUpgrade, false, 1, false, false},
		{"inPlace, one member, backend", couchbasev2.InPlaceUpgrade, true, 1, false, false},
		{"inPlace, two members", couchbasev2.InPlaceUpgrade, false, 2, true, false},
	}

	for _, tc := range cases {
		inPlace, fullRecovery := migrationCycleStrategy(tc.upgradeProcess, tc.hasStorageBackendOverrides, tc.members)
		if inPlace != tc.wantInPlace || fullRecovery != tc.wantFullRecovery {
			t.Errorf("%s: migrationCycleStrategy = (inPlace %v, fullRecovery %v), want (%v, %v)",
				tc.name, inPlace, fullRecovery, tc.wantInPlace, tc.wantFullRecovery)
		}
	}
}

// TestPendingPodsBlockingRebalance covers the check that gates both
// handleRebalance and handleEjectedMembers. A pending initialization pod
// blocks the rebalance so every joining pod lands in one rebalance, so an
// eject member's pod is not destroyed before the rebalance that ejects it
// has run. A pod waiting on external DNS is exempt (it may wait a long time
// and would block rebalances existing members need), unless it is a swap rebalance
// replacement, which must join before its old node is ejected.
func TestPendingPodsBlockingRebalance(t *testing.T) {
	pod := func(name string, pendingDNS, upgradeReplacement bool) *v1.Pod {
		p := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}

		if pendingDNS {
			p.Status.Conditions = []v1.PodCondition{{
				Type:   k8sutil.PodPendingExternalDNSCondition,
				Status: v1.ConditionTrue,
			}}
		}

		if upgradeReplacement {
			p.Annotations = map[string]string{constants.UpgradeTrackingAnnotation: "cb-example-0000"}
		}

		return p
	}

	pendingInit := func(names ...string) couchbaseutil.MemberSet {
		ms := couchbaseutil.MemberSet{}
		for _, name := range names {
			ms.Add(couchbaseutil.NewMember("default", "cb-example", name, "", "", false, ""))
		}

		return ms
	}

	testcases := []struct {
		name           string
		pendingInit    couchbaseutil.MemberSet
		pendingDNSPods []*v1.Pod
		expected       []string
	}{
		{
			name:        "no pods initializing, so the rebalance is free to run",
			pendingInit: pendingInit(),
			expected:    nil,
		},
		{
			name:        "a pending initialization pod blocks the rebalance",
			pendingInit: pendingInit("cb-example-0005"),
			expected:    []string{"cb-example-0005"},
		},
		{
			// The scale down regression, a scale up pod still initializing must keep
			// handleEjectedMembers from destroying the scale down pod early.
			name:        "only the pods still initializing block",
			pendingInit: pendingInit("cb-example-0005", "cb-example-0006"),
			expected:    []string{"cb-example-0005", "cb-example-0006"},
		},
		{
			name:           "a pod only waiting on external DNS does not block",
			pendingInit:    pendingInit("cb-example-0005"),
			pendingDNSPods: []*v1.Pod{pod("cb-example-0005", true, false)},
			expected:       nil,
		},
		{
			name:           "a swap rebalance replacement waiting on DNS still blocks",
			pendingInit:    pendingInit("cb-example-0005"),
			pendingDNSPods: []*v1.Pod{pod("cb-example-0005", true, true)},
			expected:       []string{"cb-example-0005"},
		},
		{
			name:           "DNS exemption applies per pod",
			pendingInit:    pendingInit("cb-example-0005", "cb-example-0006"),
			pendingDNSPods: []*v1.Pod{pod("cb-example-0005", true, false)},
			expected:       []string{"cb-example-0006"},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			blocking := pendingPodsBlockingRebalance(testcase.pendingInit, testcase.pendingDNSPods)

			if blocking.Size() != len(testcase.expected) {
				t.Fatalf("expected %d blocking pods %v, got %d %v",
					len(testcase.expected), testcase.expected, blocking.Size(), blocking.Names())
			}

			for _, name := range testcase.expected {
				if !blocking.Contains(name) {
					t.Errorf("expected %s to block the rebalance, got %v", name, blocking.Names())
				}
			}
		})
	}
}

// TestSortCandidatesByUpgradeOrder checks every candidate comes back in upgrade
// order, so previousVersionPodCount holds back the last ones, while the rolling
// filter still only takes the first group. A pod whose class is gone from the
// spec fits no group and goes last.
func TestSortCandidatesByUpgradeOrder(t *testing.T) {
	member := func(name, class string) couchbaseutil.Member {
		return couchbaseutil.NewMember("default", "cb", name, "7.6.11", class, false, "couchbase/server:7.6.11")
	}

	candidates := couchbaseutil.NewMemberSet(
		member("cb-0000", "data-only"), member("cb-0001", "data-only"),
		member("cb-0002", "idx-query"), member("cb-0003", "idx-query"),
		member("cb-0004", "rem-services"), member("cb-0005", "rem-services"),
		member("cb-0006", "svc-less"), member("cb-0007", "gone"),
	)

	servers := []couchbasev2.ServerConfig{
		{Name: "data-only", Services: []couchbasev2.Service{couchbasev2.DataService}},
		{Name: "idx-query", Services: []couchbasev2.Service{couchbasev2.IndexService, couchbasev2.QueryService}},
		{Name: "rem-services", Services: []couchbasev2.Service{couchbasev2.SearchService}},
		{Name: "svc-less"},
	}

	names := func(list couchbaseutil.MemberList) []string {
		out := []string{}
		for _, m := range list {
			out = append(out, m.Name())
		}

		return out
	}

	cases := []struct {
		orderType couchbasev2.UpgradeOrderType
		order     []string
	}{
		{couchbasev2.UpgradeOrderTypeServerClasses, []string{"idx-query", "data-only"}},
		// svc-less counts as an arbiter, which the default service order puts last.
		{couchbasev2.UpgradeOrderTypeServices, []string{"index"}},
	}

	for _, tc := range cases {
		c := &Cluster{cluster: &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{
			Servers: servers,
			Upgrade: &couchbasev2.UpgradeSpec{UpgradeOrderType: tc.orderType, UpgradeOrder: tc.order},
		}}}

		want := []string{"cb-0002", "cb-0003", "cb-0000", "cb-0001", "cb-0004", "cb-0005", "cb-0006", "cb-0007"}
		if got := names(c.sortCandidatesByUpgradeOrder(candidates)); !slices.Equal(got, want) {
			t.Errorf("%s sort: got %v, want %v", tc.orderType, got, want)
		}

		if got := names(c.filterCandidatesByUpgradeOrder(candidates)); !slices.Equal(got, want[:2]) {
			t.Errorf("%s filter: got %v, want %v", tc.orderType, got, want[:2])
		}
	}
}

// TestBuildClassOrder checks the class order used to pick which scaled-up pods stay on the old version.
func TestBuildClassOrder(t *testing.T) {
	servers := []couchbasev2.ServerConfig{
		{Name: "data-only", Services: []couchbasev2.Service{couchbasev2.DataService}},
		{Name: "idx-query", Services: []couchbasev2.Service{couchbasev2.IndexService, couchbasev2.QueryService}},
		{Name: "rem-services", Services: []couchbasev2.Service{couchbasev2.SearchService}},
		{Name: "svc-less"},
	}

	cases := []struct {
		orderType couchbasev2.UpgradeOrderType
		order     []string
		want      []string
	}{
		{couchbasev2.UpgradeOrderTypeServerClasses, []string{"idx-query", "data-only"}, []string{"idx-query", "data-only", "rem-services", "svc-less"}},
		// A class without services counts as an arbiter, which the default order puts last.
		{couchbasev2.UpgradeOrderTypeServices, []string{"index"}, []string{"idx-query", "data-only", "rem-services", "svc-less"}},
	}

	for _, tc := range cases {
		c := &Cluster{cluster: &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{
			Servers: servers,
			Upgrade: &couchbasev2.UpgradeSpec{UpgradeOrderType: tc.orderType, UpgradeOrder: tc.order},
		}}}

		if got := c.buildClassOrder(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.orderType, got, tc.want)
		}
	}
}

// upgradeTestMembers makes a member for each pod name, all in the given server class.
func upgradeTestMembers(config string, names ...string) couchbaseutil.MemberSet {
	ms := couchbaseutil.MemberSet{}
	for _, name := range names {
		ms.Add(couchbaseutil.NewMember("default", "cb-example", name, "", config, false, ""))
	}

	return ms
}

// memberNames returns the pod names in the list, keeping their order.
func memberNames(ml couchbaseutil.MemberList) []string {
	names := []string{}
	for _, m := range ml {
		names = append(names, m.Name())
	}

	return names
}

// TestOrderCandidatesByName checks the name order. Pods the user listed go
// first, then the rest by name, and the orchestrator goes last.
func TestOrderCandidatesByName(t *testing.T) {
	cases := []struct {
		name         string
		nodeOrder    []string
		orchestrator string
		want         []string
	}{
		{"sorted by name", nil, "", []string{"cb-example-0000", "cb-example-0001", "cb-example-0002"}},
		{"orchestrator goes last", nil, "ns_1@cb-example-0000.cb-example.default.svc", []string{"cb-example-0001", "cb-example-0002", "cb-example-0000"}},
		{"listed pods go first", []string{"cb-example-0002"}, "", []string{"cb-example-0002", "cb-example-0000", "cb-example-0001"}},
		// The list may hold names that are not pods, such as server group names.
		{"names that are not pods are ignored", []string{"az-a", "az-b"}, "", []string{"cb-example-0000", "cb-example-0001", "cb-example-0002"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidates := upgradeTestMembers("data", "cb-example-0002", "cb-example-0000", "cb-example-0001")

			got := memberNames(orderCandidatesByName(candidates, tc.nodeOrder, tc.orchestrator))
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGroupCandidatesByServerGroups checks the ServerGroups upgrade order when some
// pods were created before server groups were turned on, so they have no zone.
// Those pods must be upgraded first, in name order, instead of being skipped.
func TestGroupCandidatesByServerGroups(t *testing.T) {
	noNodeSelector := fmt.Errorf("%w: pod has no node selector", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	notFound := fmt.Errorf("%w: pod not found", errors.NewStackTracedError(errors.ErrResourceRequired))

	type lookup struct {
		group string
		err   error
	}

	cases := []struct {
		name string
		pods map[string]lookup
		want [][]string
	}{
		{
			name: "no pod has a node selector, so all go in name order",
			pods: map[string]lookup{
				"cb-example-0002": {err: noNodeSelector},
				"cb-example-0000": {err: noNodeSelector},
				"cb-example-0001": {err: noNodeSelector},
			},
			want: [][]string{{"cb-example-0000", "cb-example-0001", "cb-example-0002"}},
		},
		{
			name: "pods with no zone go before pods with a zone",
			pods: map[string]lookup{
				"cb-example-0000": {group: "az-a"},
				"cb-example-0001": {err: noNodeSelector},
				"cb-example-0002": {group: ""}, // node selector, but no zone key
			},
			want: [][]string{{"cb-example-0001", "cb-example-0002"}, {"cb-example-0000"}},
		},
		{
			name: "every pod has a zone, so pods go by server group",
			pods: map[string]lookup{
				"cb-example-0000": {group: "az-b"},
				"cb-example-0001": {group: "az-a"},
				"cb-example-0002": {group: "az-b"},
			},
			want: [][]string{{"cb-example-0001"}, {"cb-example-0000", "cb-example-0002"}},
		},
		{
			name: "a pod that can't be found is left for the final group",
			pods: map[string]lookup{
				"cb-example-0000": {err: notFound},
				"cb-example-0001": {group: "az-a"},
			},
			want: [][]string{{"cb-example-0001"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Cluster{
				cluster: &couchbasev2.CouchbaseCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "cb-example", Namespace: "default"},
					Spec: couchbasev2.ClusterSpec{
						ServerGroups: []string{"az-a", "az-b"},
						Servers: []couchbasev2.ServerConfig{
							{Name: "data", Services: []couchbasev2.Service{couchbasev2.DataService}},
						},
						Upgrade: &couchbasev2.UpgradeSpec{UpgradeOrderType: couchbasev2.UpgradeOrderTypeServerGroups},
					},
				},
				log: logf.Log.WithName("test"),
			}

			candidates := couchbaseutil.MemberSet{}
			for name := range tc.pods {
				candidates.Merge(upgradeTestMembers("data", name))
			}

			groups := c.groupCandidatesByServerGroups(candidates, func(name string) (string, error) {
				return tc.pods[name].group, tc.pods[name].err
			})

			got := [][]string{}
			for _, group := range groups {
				got = append(got, memberNames(group))
			}

			if !slices.EqualFunc(got, tc.want, slices.Equal[[]string]) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
