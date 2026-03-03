/*
Copyright 2025-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package scheduler_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
)

// Helper function to create a simplified pod object for testing.
func makePod(name, class, group string) *v1.Pod {
	p := &v1.Pod{}
	p.Name = name
	p.Labels = map[string]string{constants.LabelNodeConf: class}

	if group != "" {
		p.Spec.NodeSelector = map[string]string{constants.ServerGroupLabel: group}
	}

	p.Status.Phase = v1.PodRunning

	return p
}

func TestRescheduleUnschedulableOnly(t *testing.T) {
	// Scenario: A server config class "sc1" theoretically used groups [a,b,c],
	// but the cluster definition (and thus the scheduler's active groups) only has [a,b].
	// Pod p1 is currently in group "c" (which is now unschedulable).
	// Pods p2 and p3 are in valid groups "a" and "b".
	pods := []*v1.Pod{
		makePod("p1", "sc1", "c"),
		makePod("p2", "sc1", "a"),
		makePod("p3", "sc1", "b"),
	}

	cluster := &couchbasev2.CouchbaseCluster{}
	// Define server config for class "sc1" with only "a" and "b" as valid server groups.
	cluster.Spec = couchbasev2.ClusterSpec{
		Servers: []couchbasev2.ServerConfig{
			{Name: "sc1", ServerGroups: []string{"a", "b"}},
		},
		ServerGroups: []string{"a", "b"}, // Global server groups also exclude "c"
	}

	sched, err := scheduler.NewStripeScheduler(pods, cluster)
	require.NoError(t, err)

	moves, err := sched.RescheduleUnschedulableOnly()
	require.NoError(t, err)

	// We expect one move: p1 out of "c" to either "a" or "b".
	require.Len(t, moves, 1, "expected exactly one move for unschedulable pod")

	// Verify scheduler's internal state reflects the move conceptually
	// (though the actual state object for A* is immutable, here we're testing the output)
	move := moves[0]
	require.Equal(t, "p1", move.Name, "expected p1 to be moved")
	require.Equal(t, "c", move.From, "expected p1 to move from group c")
	require.Contains(t, []string{"a", "b"}, move.To, "expected p1 to move to a valid group (a or b)")
}

func TestCreateRespectsAvoidGroups(t *testing.T) {
	// Scenario: Scheduler is initialized with groups "a" and "b".
	// Group "a" is marked as to be avoided.
	// When a new pod is created, it should be placed in "b".
	// Initialize with some pods to populate the scheduler's understanding of groups
	pods := []*v1.Pod{
		makePod("existing-a", "sc1", "a"),
		makePod("existing-b", "sc1", "b"),
	}

	cluster := &couchbasev2.CouchbaseCluster{}
	cluster.Spec = couchbasev2.ClusterSpec{
		Servers: []couchbasev2.ServerConfig{
			{Name: "sc1", ServerGroups: []string{"a", "b"}},
		},
		ServerGroups: []string{"a", "b"},
	}

	sched, err := scheduler.NewStripeScheduler(pods, cluster)
	require.NoError(t, err)

	// Mark group "a" to be avoided
	sched.AvoidGroups("a")

	// Attempt to create a new pod
	newPodName := "new-pod-to-create"
	assignedGroup, err := sched.Create("sc1", newPodName, "")
	require.NoError(t, err)

	// Expect the new pod to be assigned to group "b" (since "a" is avoided)
	require.Equal(t, "b", assignedGroup, "expected new pod to be assigned to group b")

	// Verify that if "b" was also avoided, it picks a random one if no non-avoided groups are left,
	// or in a more balanced scenario, still avoids it.
	sched.AvoidGroups("a", "b")
	assignedGroup2, err2 := sched.Create("sc1", "another-new-pod", "")
	require.NoError(t, err2)
	// In this specific simplified test, since all defined groups are avoided, it will pick randomly.
	// The exact group isn't deterministic here, but it shouldn't error.
	require.Contains(t, []string{"a", "b"}, assignedGroup2, "expected new pod to be assigned to one of the groups, even if avoided as a fallback")
}
