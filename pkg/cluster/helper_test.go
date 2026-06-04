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
	"fmt"
	"strconv"
	"testing"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
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
		// Use digest for 7.0.4
		{
			"05aad0f1d3a373b60dece893a9c185dcb0e0630aa6f0c0f310ad8767918fd2af",
			"7.1.0",
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

func TestGetLowestMemberVersion(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name            string
		memberVersions  []string
		expectedVersion string
	}{
		{
			name:            "empty",
			memberVersions:  []string{},
			expectedVersion: "",
		},
		{
			name:            "single",
			memberVersions:  []string{"7.0.0"},
			expectedVersion: "7.0.0",
		},
		{
			name:            "members",
			memberVersions:  []string{"7.0.0", "6.6.2", "7.1.0"},
			expectedVersion: "6.6.2",
		},
		{
			name:            "members with multiple figures",
			memberVersions:  []string{"7.0.0", "6.8.10", "6.8.7"},
			expectedVersion: "6.8.7",
		},
		{
			name:            "same",
			memberVersions:  []string{"6.8.10", "6.8.10"},
			expectedVersion: "6.8.10",
		},
		{
			name:            "multiple second digit",
			memberVersions:  []string{"7.0.0", "6.10.1", "6.8.10", "6.8.11"},
			expectedVersion: "6.8.10",
		},
		{
			name:            "with build numbers",
			memberVersions:  []string{"7.0.0-1000", "7.0.0-1001", "7.0.0-1002", "7.0.0-1003"},
			expectedVersion: "7.0.0-1000",
		},
		{
			name:            "with and without build numbers",
			memberVersions:  []string{"7.0.0-1000", "7.0.0", "7.0.0-1003"},
			expectedVersion: "7.0.0",
		},
	}

	for _, testcase := range testcases {
		c := Cluster{
			members: couchbaseutil.MemberSet{},
		}

		for _, version := range testcase.memberVersions {
			name := fmt.Sprintf("%s-%s", testcase.name, version)
			m := couchbaseutil.NewMember("", "", name, version, "", false, "")
			c.members.Add(m)
		}

		lowestVersion := c.GetLowestMemberVersion()
		if lowestVersion != testcase.expectedVersion {
			t.Errorf("unexpectedly got lowest version: %s expected %s", lowestVersion, testcase.expectedVersion)
		}
	}
}

func TestGetHighestMemberVersion(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name            string
		memberVersions  []string
		expectedVersion string
	}{
		{
			name:            "empty",
			memberVersions:  []string{},
			expectedVersion: "",
		},
		{
			name:            "single",
			memberVersions:  []string{"7.0.0"},
			expectedVersion: "7.0.0",
		},
		{
			name:            "members",
			memberVersions:  []string{"7.0.0", "6.6.2", "7.1.0"},
			expectedVersion: "7.1.0",
		},
		{
			name:            "members with multiple figures",
			memberVersions:  []string{"7.0.0", "6.8.10", "6.8.7"},
			expectedVersion: "7.0.0",
		},
		{
			name:            "same",
			memberVersions:  []string{"6.8.10", "6.8.10"},
			expectedVersion: "6.8.10",
		},
		{
			name:            "multiple second digit",
			memberVersions:  []string{"7.0.0", "6.10.1", "6.8.10", "7.1.11"},
			expectedVersion: "7.1.11",
		},
	}

	for _, testcase := range testcases {
		c := Cluster{
			members: couchbaseutil.MemberSet{},
		}

		for _, version := range testcase.memberVersions {
			name := fmt.Sprintf("%s-%s", testcase.name, version)
			m := couchbaseutil.NewMember("", "", name, version, "", false, "")
			c.members.Add(m)
		}

		highestVersion := c.GetHighestMemberVersion()
		if highestVersion != testcase.expectedVersion {
			t.Errorf("unexpectedly got highest version: %s expected %s", highestVersion, testcase.expectedVersion)
		}
	}
}
