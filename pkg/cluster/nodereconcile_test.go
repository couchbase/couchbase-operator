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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// fakeCouchbase acts as the server in the failover tests.
// Each read of the node state gives the next entry in the list.
// When the list runs out, it keeps giving the last entry.
type fakeCouchbase struct {
	hostname       string
	states         []fakeNodeState
	poolsCalls     int
	failoverStatus int
	failoverCalls  int // How many failover requests were sent.
}

// fakeNodeState is what the server reports for the node at one point in time.
type fakeNodeState struct {
	membership string // Either active or inactiveFailed.
	rebalance  string // Either running or none.
}

// Do answers the two calls the failover code makes, reading the node
// state and asking for a failover. Any other call gets not found.
func (f *fakeCouchbase) Do(req *http.Request) (*http.Response, error) {
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/pools/default":
		state := f.states[min(f.poolsCalls, len(f.states)-1)]
		f.poolsCalls++

		body := fmt.Sprintf(`{"rebalanceStatus":%q,"nodes":[{"hostname":%q,"clusterMembership":%q,"status":"healthy"}]}`,
			state.rebalance, f.hostname, state.membership)

		return fakeResponse(req, http.StatusOK, body), nil
	case req.Method == http.MethodPost && req.URL.Path == "/controller/failOver":
		f.failoverCalls++

		// Any status other than OK acts like the server is still
		// busy with an earlier failover.
		if f.failoverStatus != http.StatusOK {
			return fakeResponse(req, f.failoverStatus, "Cannot failover during rebalance"), nil
		}

		return fakeResponse(req, http.StatusOK, ""), nil
	}

	return fakeResponse(req, http.StatusNotFound, ""), nil
}

func (f *fakeCouchbase) CloseIdleConnections() {}

func fakeResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// TestFailoverNodeForInPlaceUpgrade checks the hard failover used by an
// in place upgrade for nodes without the data service.
func TestFailoverNodeForInPlaceUpgrade(t *testing.T) {
	// The REST client counts every request, so the metrics have to exist.
	metrics.InitMetrics()

	tests := []struct {
		name           string
		states         []fakeNodeState
		failoverStatus int
		failoverCalls  int
		expectErr      error
	}{
		{
			name:           "node already failed over skips the failover",
			states:         []fakeNodeState{{"inactiveFailed", "none"}},
			failoverStatus: http.StatusOK,
			failoverCalls:  0,
		},
		{
			name:           "failover request succeeds",
			states:         []fakeNodeState{{"active", "none"}},
			failoverStatus: http.StatusOK,
			failoverCalls:  1,
		},
		{
			name:           "failover request is rejected but the server finishes it",
			states:         []fakeNodeState{{"active", "running"}, {"inactiveFailed", "none"}},
			failoverStatus: http.StatusServiceUnavailable,
			failoverCalls:  1,
		},
		{
			name:           "failover request is rejected and nothing is running",
			states:         []fakeNodeState{{"active", "none"}},
			failoverStatus: http.StatusServiceUnavailable,
			failoverCalls:  1,
			expectErr:      ErrHardFailoverFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := couchbaseutil.NewMember("default", "cb", "cb-0000", "8.0.2", "analytics", false, "")

			fake := &fakeCouchbase{
				hostname:       string(candidate.GetHostName()),
				states:         test.states,
				failoverStatus: test.failoverStatus,
			}

			api := couchbaseutil.New(context.Background(), "default/cb", "Administrator", "password", logr.Discard())
			api.Client = fake

			c := &Cluster{
				cluster: &couchbasev2.CouchbaseCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "cb", Namespace: "default"},
					Spec: couchbasev2.ClusterSpec{
						Servers: []couchbasev2.ServerConfig{
							{Name: "analytics", Services: couchbasev2.ServiceList{couchbasev2.AnalyticsService}},
						},
					},
				},
				api:             api,
				log:             logr.Discard(),
				callableMembers: couchbaseutil.NewMemberSet(candidate),
			}

			r := &ReconcileMachine{}

			done, err := r.failoverNodeForInPlaceUpgrade(candidate, c)

			if test.expectErr != nil {
				if !errors.Is(err, test.expectErr) {
					t.Fatalf("expected error %v, got %v", test.expectErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if !done {
					t.Errorf("expected the node to be ready for recreate")
				}
			}

			// The request must only be sent once, a retry would hit the running failover.
			if fake.failoverCalls != test.failoverCalls {
				t.Errorf("expected %d failover requests, got %d", test.failoverCalls, fake.failoverCalls)
			}
		})
	}
}
