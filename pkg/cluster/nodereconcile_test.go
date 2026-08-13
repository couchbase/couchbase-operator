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

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
)

// TestClampRollbackBatch pins which end of the bucket replica range each rollback method
// binds on: failover on the least replicated bucket, rebalance-out on the most.
func TestClampRollbackBatch(t *testing.T) {
	tests := []struct {
		name            string
		method          couchbasev2.RollbackMethod
		candidates      int
		minReplicas     int
		maxReplicas     int
		haveBuckets     bool
		dataNodes       int
		callableMembers int
		expected        int
	}{
		{
			// K8S-4884: replica-1 buckets must not authorise a batch that leaves the
			// replica-2 bucket unable to place a full chain. Five data nodes minus a
			// batch of two leaves three, exactly what replica 2 needs.
			name:            "rebalance out binds on the most replicated bucket",
			method:          couchbasev2.RollbackMethodConstrainedRebalanceOut,
			candidates:      3,
			minReplicas:     1,
			maxReplicas:     2,
			haveBuckets:     true,
			dataNodes:       5,
			callableMembers: 10,
			expected:        2,
		},
		{
			name:            "rebalance out uses the whole batch when replicas are uniform",
			method:          couchbasev2.RollbackMethodConstrainedRebalanceOut,
			candidates:      3,
			minReplicas:     1,
			maxReplicas:     1,
			haveBuckets:     true,
			dataNodes:       5,
			callableMembers: 10,
			expected:        3,
		},
		{
			name:            "rebalance out works with zero replicas",
			method:          couchbasev2.RollbackMethodConstrainedRebalanceOut,
			candidates:      3,
			minReplicas:     0,
			maxReplicas:     0,
			haveBuckets:     true,
			dataNodes:       5,
			callableMembers: 10,
			expected:        3,
		},
		{
			name:            "rebalance out keeps one data node when there are no buckets",
			method:          couchbasev2.RollbackMethodConstrainedRebalanceOut,
			candidates:      3,
			haveBuckets:     false,
			dataNodes:       3,
			callableMembers: 6,
			expected:        2,
		},
		{
			// No batch fits, so the caller reverts to swap rather than under-placing.
			// Capping the batch at a single node would not have saved this topology.
			name:            "rebalance out refuses when even one node breaks the chain",
			method:          couchbasev2.RollbackMethodConstrainedRebalanceOut,
			candidates:      1,
			minReplicas:     1,
			maxReplicas:     2,
			haveBuckets:     true,
			dataNodes:       3,
			callableMembers: 6,
			expected:        0,
		},
		{
			name:            "failover binds on the least replicated bucket",
			method:          couchbasev2.RollbackMethodConstrainedFailover,
			candidates:      3,
			minReplicas:     1,
			maxReplicas:     2,
			haveBuckets:     true,
			dataNodes:       5,
			callableMembers: 10,
			expected:        1,
		},
		{
			name:            "failover batches up to the replicas it can spare",
			method:          couchbasev2.RollbackMethodConstrainedFailover,
			candidates:      3,
			minReplicas:     3,
			maxReplicas:     3,
			haveBuckets:     true,
			dataNodes:       5,
			callableMembers: 10,
			expected:        2,
		},
		{
			name:            "failover refuses when any bucket has no replicas",
			method:          couchbasev2.RollbackMethodConstrainedFailover,
			candidates:      3,
			minReplicas:     0,
			maxReplicas:     2,
			haveBuckets:     true,
			dataNodes:       5,
			callableMembers: 10,
			expected:        0,
		},
		{
			name:            "swap rebalance is not handled here",
			method:          couchbasev2.RollbackMethodSwapRebalance,
			candidates:      3,
			minReplicas:     1,
			maxReplicas:     1,
			haveBuckets:     true,
			dataNodes:       5,
			callableMembers: 10,
			expected:        0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batch := clampRollbackBatch(test.method, test.candidates, test.minReplicas, test.maxReplicas,
				test.haveBuckets, test.dataNodes, test.callableMembers)
			if batch != test.expected {
				t.Errorf("expected batch %d, got %d", test.expected, batch)
			}
		})
	}
}
