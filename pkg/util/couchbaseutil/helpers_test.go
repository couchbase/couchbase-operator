/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestDoStringSlicesContainEqualValues(t *testing.T) {
	t.Parallel()

	type stringSlicesContainEqualValuesTest struct {
		sliceA   []string
		sliceB   []string
		expected bool
	}

	testcases := []stringSlicesContainEqualValuesTest{
		{
			[]string{"one", "two", "three"},
			[]string{"one", "two", "three"},
			true,
		},
		{
			[]string{"one", "two", "three"},
			[]string{"one", "two", "four"},
			false,
		},
		{
			[]string{"one", "two", "three"},
			[]string{"one", "two"},
			false,
		},
		{
			[]string{"one", "two", "three"},
			[]string{"one", "two", "three", "four"},
			false,
		},
		{
			[]string{"one", "two", "two"},
			[]string{"one", "two", "two"},
			true,
		},
		{
			[]string{},
			[]string{},
			true,
		},
		{
			[]string{"one"},
			[]string{},
			false,
		},
	}

	for _, testcase := range testcases {
		result := DoStringSlicesContainEqualValues(strings.Join(testcase.sliceA, ","), strings.Join(testcase.sliceB, ","), ",")
		if result != testcase.expected {
			t.Errorf("expected DoStringSlicesContainEqualValues to return %v, but got %v", testcase.expected, result)
		}
	}
}

func TestAddAnnotation(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name                string
		k                   string
		v                   string
		existingAnnotations map[string]string
		expectedAnnotations map[string]string
	}{
		{
			name:                "no existing annotations",
			k:                   "some-key",
			v:                   "some-value",
			existingAnnotations: nil,
			expectedAnnotations: map[string]string{
				"some-key": "some-value",
			},
		},
		{
			name: "existing annotations",
			k:    "some-key",
			v:    "some-value",
			existingAnnotations: map[string]string{
				"existing-key-1": "existing-value",
				"existing-key-2": "existing-value",
			},
			expectedAnnotations: map[string]string{
				"existing-key-1": "existing-value",
				"existing-key-2": "existing-value",
				"some-key":       "some-value",
			},
		},
		{
			name: "existing annotation with same key",
			k:    "some-key",
			v:    "some-value",
			existingAnnotations: map[string]string{
				"some-key": "existing-value",
			},
			expectedAnnotations: map[string]string{
				"some-key": "some-value",
			},
		},
	}

	for _, testcase := range testcases {
		meta := &v1.ObjectMeta{}

		meta.SetAnnotations(testcase.existingAnnotations)

		AddAnnotation(meta, testcase.k, testcase.v)

		if eq := reflect.DeepEqual(meta.Annotations, testcase.expectedAnnotations); !eq {
			t.Errorf("test %v expected annotations to be %v, but got %v", testcase.name, testcase.expectedAnnotations, meta.Annotations)
		}
	}
}
