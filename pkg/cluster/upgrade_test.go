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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
