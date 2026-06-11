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
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	v1 "k8s.io/api/core/v1"
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
