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
	}
	for _, testcase := range testcases {
		valid, err := VersionAfter(testcase.actualVersion, testcase.requiredVersion)
		if err != nil {
			t.Fatal(err)
		}

		if valid != testcase.valid {
			t.Errorf("unexpectedly failed version check: %s,%s - %s", testcase.actualVersion, testcase.requiredVersion, strconv.FormatBool(testcase.valid))
		}
	}
}
