/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/unreconcilable"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGatherBuildsBucketRefIndexWithSpecName covers the awkward case where
// spec.name differs from metadata.name. The bucket then has two identities, and
// the side index built during gather is the only thing that can tie the
// Couchbase-side name back to the CR carrying the condition.
func TestGatherBuildsBucketRefIndexWithSpecName(t *testing.T) {
	const (
		crName     = "my-bucket-cr"
		bucketName = "my.bucket%name"
	)

	buckets := []*couchbasev2.CouchbaseBucket{{
		ObjectMeta: metav1.ObjectMeta{Name: crName, Namespace: fakeClusterNamespace},
		Spec: couchbasev2.CouchbaseBucketSpec{
			// Perfectly legal as a Couchbase bucket name, quite illegal as a
			// metadata.name. That is the whole reason spec.name exists, and
			// the whole reason the two key spaces drift apart.
			Name:        bucketName,
			MemoryQuota: resource.NewQuantity(100, resource.BinarySI),
		},
	}}

	tracker := unreconcilable.New(fakeClusterName)

	gathered := gatherCouchbaseBuckets(SupportedFeatureMap{}, &couchbasev2.ObjectSelectorAsSelector{},
		buckets, nil, &couchbasev2.CouchbaseCluster{}, nil, nil, tracker)

	if len(gathered) != 1 {
		t.Fatalf("expected 1 gathered bucket, got %d", len(gathered))
	}

	if gathered[0].BucketName != bucketName {
		t.Fatalf("gathered bucket name = %q, want spec.name %q", gathered[0].BucketName, bucketName)
	}

	ref, found := tracker.BucketRef(bucketName)
	if !found {
		t.Fatal("gather did not index the Couchbase bucket name against its CR")
	}

	want := unreconcilable.Ref{Kind: couchbasev2.BucketCRDResourceKind, Name: crName}
	if ref != want {
		t.Errorf("bucket ref = %v, want %v", ref, want)
	}

	// And here is the payoff. A validator holding nothing but the Couchbase name
	// still marks the CR, and the bucket goes quiet in both key spaces.
	tracker.MarkBucket(bucketName, unreconcilable.ReasonImmutableFieldChanged, "conflictResolution is immutable")

	if !tracker.IsSkipped(unreconcilable.KindBucketName, bucketName) {
		t.Error("bucket not skipped under its Couchbase name")
	}

	if !tracker.IsSkipped(couchbasev2.BucketCRDResourceKind, crName) {
		t.Error("bucket not skipped under its CR name")
	}
}

// TestGatherIndexesEphemeralBuckets checks the ephemeral gather function
// indexes its kind, so a marked ephemeral bucket has a resource to report
// against.
func TestGatherIndexesEphemeralBuckets(t *testing.T) {
	tracker := unreconcilable.New(fakeClusterName)

	ephemeral := []*couchbasev2.CouchbaseEphemeralBucket{{
		ObjectMeta: metav1.ObjectMeta{Name: "eph-cr", Namespace: fakeClusterNamespace},
		Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
			Name:        "eph.bucket",
			MemoryQuota: resource.NewQuantity(100, resource.BinarySI),
		},
	}}

	gatherEphemeralBuckets(SupportedFeatureMap{}, &couchbasev2.ObjectSelectorAsSelector{},
		ephemeral, nil, nil, &couchbasev2.CouchbaseCluster{}, tracker)

	ref, found := tracker.BucketRef("eph.bucket")
	if !found {
		t.Fatal("ephemeral bucket was not indexed")
	}

	want := unreconcilable.Ref{Kind: couchbasev2.EphemeralBucketCRDResourceKind, Name: "eph-cr"}
	if ref != want {
		t.Errorf("ephemeral ref = %v, want %v", ref, want)
	}
}

// TestUnreconcilableTrackerIsPerCluster checks the accessor hands back the
// cluster's own tracker, scoped to its own name. Conditions are written per
// cluster, so two clusters that both select one bucket must not trample each
// other's verdict.
func TestUnreconcilableTrackerIsPerCluster(t *testing.T) {
	c := &Cluster{unreconcilable: unreconcilable.New(fakeClusterName)}

	if got := c.Unreconcilable().ClusterName(); got != fakeClusterName {
		t.Errorf("tracker cluster name = %q, want %q", got, fakeClusterName)
	}

	other := &Cluster{unreconcilable: unreconcilable.New("other-cluster")}

	ref := unreconcilable.Ref{Kind: couchbasev2.BucketCRDResourceKind, Name: "shared"}

	c.Unreconcilable().Mark(ref, unreconcilable.ReasonValidationFailed, "bad")

	if !c.Unreconcilable().IsSkipped(ref.Kind, ref.Name) {
		t.Error("marking one cluster's tracker did not take effect on it")
	}

	if other.Unreconcilable().IsSkipped(ref.Kind, ref.Name) {
		t.Error("marking one cluster's tracker leaked into another cluster's")
	}
}
