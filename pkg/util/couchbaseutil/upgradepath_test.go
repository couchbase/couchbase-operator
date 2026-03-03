/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import "testing"

func TestGetUpgradePath(t *testing.T) {
	type versionGroup struct {
		version string
		group   int
	}

	expectedGroups := []versionGroup{
		{"2.1.2", 0},
		{"5.1.2", 1},
		{"6.0.5", 1},
		{"6.6.0", 2},
		{"6.6.5", 2},
		{"7.0.4", 2},
		{"7.1.6", 3},
		{"7.2.3", 3},
		{"7.2.4", 4},
		{"7.6.0", 4},
		{"7.8.0", 4},
	}

	for _, expectedGroup := range expectedGroups {
		g, err := getUpgradeGroup(expectedGroup.version)
		if err != nil {
			t.Error(err)
		} else if g != expectedGroup.group {
			t.Errorf("expected version %s to have group %v but got %v", expectedGroup.version, expectedGroup.group, g)
		}
	}
}

func TestValidUpgrades(t *testing.T) {
	type upgrade struct {
		old, new string
	}

	validUpgrades := []upgrade{
		{"6.6.5", "6.6.6"},
		{"6.6.6", "7.2.3"},
		{"6.6.0", "7.2.3"},
		{"6.6.0", "7.1.3"},
		{"7.2.3", "7.2.4"},
		{"7.2.3", "7.6.0"}}

	for _, u := range validUpgrades {
		if valid, err := ValidUpgrade(u.old, u.new); err != nil {
			t.Error(err)
		} else if !valid {
			t.Errorf("expected upgrade %s -> %s to be valid", u.old, u.new)
		}
	}
}

func TestInvalidUpgrades(t *testing.T) {
	type upgrade struct {
		old, new string
	}

	invalidUpgrades := []upgrade{
		{"6.6.5", "7.2.4"},
		{"6.6.5", "7.6.0"},
		{"5.1.2", "7.1.0"},
		{"5.1.2", "7.2.4"},
		{"7.6.0", "7.2.4"},
	}

	for _, u := range invalidUpgrades {
		if valid, err := ValidUpgrade(u.old, u.new); err != nil {
			t.Error(err)
		} else if valid {
			t.Errorf("expected upgrade %s -> %s to be invalid", u.old, u.new)
		}
	}
}
