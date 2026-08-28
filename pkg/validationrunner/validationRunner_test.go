/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package validationrunner

import (
	"slices"
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/cluster"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"
)

// allocation is a terser spelling of cluster.BucketMemoryAllocation for the table
// below. A bucket with allocated == 0 has not been created yet.
func allocation(name string, requested, allocated int64) cluster.BucketMemoryAllocation {
	return cluster.BucketMemoryAllocation{
		BucketName:  name,
		RequestedMB: requested,
		AllocatedMB: allocated,
		Exists:      allocated > 0,
	}
}

// heldNames pulls the bucket names out of a verdict so the tests can compare
// against something readable.
func heldNames(held []validationv2.UnreconcilableBucket) []string {
	names := make([]string, 0, len(held))

	for _, bucket := range held {
		names = append(names, bucket.BucketName)
	}

	return names
}

func TestBucketsOverMemoryQuota(t *testing.T) {
	testcases := []struct {
		name        string
		quotaMB     int64
		allocations []cluster.BucketMemoryAllocation
		held        []string
	}{
		{
			name:        "everything fits, nothing is held",
			quotaMB:     1024,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 512, 0), allocation("b", 512, 0)},
		},
		{
			name:        "the bucket that does not fit is held, the one that does is not",
			quotaMB:     1024,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 600, 0), allocation("b", 600, 0)},
			held:        []string{"b"},
		},
		{
			name:        "holds are handed out in name order, whatever order they arrive in",
			quotaMB:     1024,
			allocations: []cluster.BucketMemoryAllocation{allocation("z", 600, 0), allocation("a", 600, 0)},
			held:        []string{"z"},
		},
		{
			name:        "buckets already on the cluster are never held",
			quotaMB:     1024,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 900, 900), allocation("b", 900, 900)},
		},
		{
			name:        "a resize that does not fit is held while the live allocation stays",
			quotaMB:     1024,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 900, 500), allocation("b", 400, 400)},
			held:        []string{"a"},
		},
		{
			name:        "a resize that fits in the headroom goes through",
			quotaMB:     1024,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 600, 500), allocation("b", 400, 400)},
		},
		{
			name:        "shrinking is always allowed, even from over quota",
			quotaMB:     512,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 200, 900)},
		},
		{
			name:        "with the quota already oversubscribed, no increase gets through",
			quotaMB:     512,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 900, 900), allocation("b", 100, 0)},
			held:        []string{"b"},
		},
		{
			name:        "a single bucket larger than the whole quota is held",
			quotaMB:     512,
			allocations: []cluster.BucketMemoryAllocation{allocation("a", 1024, 0)},
			held:        []string{"a"},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			got := heldNames(bucketsOverMemoryQuota(testcase.allocations, testcase.quotaMB))

			want := testcase.held
			if want == nil {
				want = []string{}
			}

			if !slices.Equal(got, want) {
				t.Errorf("held = %v, want %v", got, want)
			}
		})
	}
}
