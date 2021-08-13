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
		valid, err := VersionAfter(testcase.actualVersion, testcase.requiredVersion)
		if err != nil {
			t.Fatal(err)
		}

		if valid != testcase.valid {
			t.Errorf("unexpectedly failed version check: %s,%s - %s", testcase.actualVersion, testcase.requiredVersion, strconv.FormatBool(testcase.valid))
		}
	}
}
