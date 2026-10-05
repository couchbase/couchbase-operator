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

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

func TestApplyRebalanceError(t *testing.T) {
	failed := couchbaseutil.Task{
		Type:         couchbaseutil.TaskTypeRebalance,
		Status:       "notRunning",
		ErrorMessage: "Rebalance failed. See logs for detailed reason. You can try again.",
	}

	cases := []struct {
		name      string
		tasks     couchbaseutil.TaskList
		balancing bool
		want      bool
	}{
		{"no rebalance task", couchbaseutil.TaskList{}, false, true},
		{"last rebalance succeeded", couchbaseutil.TaskList{{Type: couchbaseutil.TaskTypeRebalance, Status: "notRunning"}}, false, true},
		{"last rebalance failed", couchbaseutil.TaskList{failed}, false, false},
		{"rebalance running", couchbaseutil.TaskList{failed}, true, true},
	}

	for _, tc := range cases {
		status := &Status{Balanced: true, Balancing: tc.balancing}

		applyRebalanceError(status, &tc.tasks)

		if status.Balanced != tc.want {
			t.Errorf("%s: balanced = %v, want %v", tc.name, status.Balanced, tc.want)
		}
	}
}
