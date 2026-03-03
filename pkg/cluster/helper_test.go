/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"strconv"
	"testing"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
)

type versionTest struct {
	actualVersion   string
	requiredVersion string
	valid           bool
}

func TestClusterIsAtLeastVersion(t *testing.T) {
	t.Parallel()

	testcases := []versionTest{
		{
			"1.0.0",
			"0.9.0",
			true,
		},
		{
			"1.0.0",
			"1.0.1",
			false,
		},
		{
			"6.6.2",
			"7.0.0",
			false,
		},
		{
			"7.0.0",
			"7.0.0",
			true,
		},
		// Use digest for 6.6.3
		{
			"cb8c5aba14feb955854a17c0923f79c8476872f86b3f52570d859b991d23231b",
			"7.0.0",
			false,
		},
	}
	for _, testcase := range testcases {
		c := Cluster{
			cluster: &v2.CouchbaseCluster{
				Spec: v2.ClusterSpec{
					Image: "couchbase:" + testcase.actualVersion,
				},
			},
		}
		valid, err := c.IsAtLeastVersion(testcase.requiredVersion)

		if err != nil {
			t.Fatal(err)
		}

		if valid != testcase.valid {
			t.Errorf("unexpectedly failed version check: %s,%s - %s", testcase.actualVersion, testcase.requiredVersion, strconv.FormatBool(testcase.valid))
		}
	}
}
