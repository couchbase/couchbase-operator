package couchbaseutil

import (
	"strconv"
	"testing"
)

type versionTest struct {
	actualVersion   string
	requiredVersion string
	valid           bool
}

func TestCouchbaseVersionAfter(t *testing.T) {
	t.Parallel()

	testcases := []versionTest{
		{
			"hellome-1.0.0",
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
		{
			"community-1.0.8",
			"1.0.6",
			true,
		},
		{
			"7.0.3",
			"7.0.0",
			true,
		},
	}
	for _, testcase := range testcases {
		valid, err := VersionAfter(testcase.actualVersion, testcase.requiredVersion)
		if err != nil {
			t.Fatal(err)
		}

		if valid != testcase.valid {
			t.Errorf("unexpectedly failed version check: version: %s,required: %s - %s", testcase.actualVersion, testcase.requiredVersion, strconv.FormatBool(testcase.valid))
		}
	}
}

func TestGetIndexFromMemberName(t *testing.T) {
	name := "test-couchbase-xkjg4-9999"

	index, err := GetIndexFromMemberName(name)
	if err != nil {
		t.Fatal(err)
	}

	if index != 9999 {
		t.Fatal("expected index 0 found", index)
	}
}

func TestMemberOnVersion(t *testing.T) {
	t.Parallel()

	type memberOnVersionTest struct {
		targetMemberName string
		targetVersion    string
		expectedResult   bool
	}

	testcases := []memberOnVersionTest{
		{
			"ns_1@cb-example-0000.cb-example.default.svc",
			"1.0.0",
			false,
		},
		{
			"ns_1@cb-example-0000.cb-example.default.svc",
			"1.5.0",
			true,
		},
		{
			"non-existent-member-name",
			"2.0.0",
			false,
		},
	}

	clusterMembers := NewMemberSet(
		&memberImpl{
			name:    "cb-example-0000",
			version: "1.5.0"},
		&memberImpl{
			name:    "cb-example-0001",
			version: "2.0.0"},
		&memberImpl{
			name:    "cb-example-0002",
			version: "2.5.0"})

	for _, testcase := range testcases {
		result := MemberOnVersion(clusterMembers, testcase.targetMemberName, testcase.targetVersion)
		if result != testcase.expectedResult {
			t.Errorf("expected member version check to return %v, got %v", testcase.expectedResult, result)
		}
	}
}
