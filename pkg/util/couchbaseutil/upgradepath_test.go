/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import (
	"strings"
	"testing"
)

func TestValidUpgrades(t *testing.T) {
	type upgrade struct {
		old, new string
	}

	validUpgrades := []upgrade{
		{"6.6.5", "6.6.6"},
		{"6.6.6", "7.2.3"},
		{"6.6.0", "7.2.3"},
		{"6.6.0", "7.1.3"},
		{"6.6.0", "7.0.5"},
		{"7.2.0", "8.0.0"},
		{"7.2.0", "7.2.4"},
		{"7.2.0", "7.6.0"},
		{"7.2.3", "7.2.4"},
		{"7.2.3", "7.6.0"},
		{"7.2.3", "8.0.0"},
		{"7.6.0", "8.0.0"},
		{"8.0.0", "8.5.0"},
		// Documented first hop for pre-6.6 sources.
		{"5.5.0", "6.6.0"},
		{"6.0.5", "6.6.3"},
		// 7.1.x meets the 7.2.4+/7.6.x floor exactly (MB-59786).
		{"7.1.0", "7.6.0"},
		{"7.1.6", "7.2.8"},
	}

	for _, u := range validUpgrades {
		if err := CheckUpgradePath(u.old, u.new); err != nil {
			t.Errorf("expected upgrade %s -> %s to be valid: %v", u.old, u.new, err)
		}
	}
}

func TestInvalidUpgrades(t *testing.T) {
	type upgrade struct {
		old, new string
	}

	invalidUpgrades := []upgrade{
		{"5.1.2", "7.1.0"},
		{"5.1.2", "7.2.4"},
		{"6.6.5", "7.2.4"},
		{"6.6.5", "7.6.0"},
		{"5.1.2", "8.0.0"},
		{"6.6.5", "8.0.0"},
		{"7.1.6", "8.0.0"},
		{"7.6.0", "7.2.4"},
		// Source below the target's floor.
		{"6.0.5", "7.1.0"},
		{"6.5.2", "7.2.0"},
		{"8.5.0", "9.0.0"},
	}

	for _, u := range invalidUpgrades {
		if err := CheckUpgradePath(u.old, u.new); err == nil {
			t.Errorf("expected upgrade %s -> %s to be invalid", u.old, u.new)
		}
	}
}

func TestCheckUpgradePath(t *testing.T) {
	type upgrade struct {
		old, new, expectedMsg string
	}

	invalidUpgrades := []upgrade{
		{"7.6.0", "7.2.4", "Downgrades are not supported"},
		{"5.1.2", "8.0.0", "An online upgrade to 8.0.0 needs a source of 7.2.0 or later"},
		{"8.0.0", "10.0.0", "not a supported online-upgrade target"},
		{"6.5.2", "7.2.0", "An online upgrade to 7.2.0 needs a source of 6.6.0 or later"},
		{"6.0.5", "7.1.0", "An online upgrade to 7.1.0 needs a source of 6.6.0 or later"},
		{"6.6.5", "7.6.0", "needs a source of 7.1.0 or later"},
		{"6.6.5", "7.2.4", "needs a source of 7.1.0 or later"},
		{"7.1.6", "8.0.0", "needs a source of 7.2.0 or later"},
		{"8.5.0", "9.0.0", "not a supported online-upgrade target"},
	}

	for _, u := range invalidUpgrades {
		err := CheckUpgradePath(u.old, u.new)
		if err == nil {
			t.Errorf("expected upgrade %s -> %s to be invalid", u.old, u.new)
			continue
		}

		if !strings.Contains(err.Error(), u.expectedMsg) {
			t.Errorf("expected upgrade %s -> %s to be invalid with message containing '%s' but got '%s'", u.old, u.new, u.expectedMsg, err.Error())
		}
	}
}

func TestValidOfflineUpgrades(t *testing.T) {
	type upgrade struct {
		old, new string
	}

	valid := []upgrade{
		// Same as online, but reached in a single offline boot.
		{"7.2.0", "8.0.0"},
		{"7.2.0", "8.5.0"},
		{"7.2.4", "8.5.0"},
		{"7.6.0", "8.0.0"},
		{"8.0.0", "8.5.0"},
		{"7.1.0", "7.6.0"},
		// Equal version is a no-op (e.g. a same-version image/registry change).
		{"8.0.0", "8.0.0"},
		// Patch hops within a line share one config_version — no-op boots.
		{"7.0.0", "7.0.1"},
		{"7.0.1", "7.0.5"},
		{"8.0.0", "8.0.3"},
		// 5.x/6.x targets, only reachable under skip-validation.
		{"3.0.0", "5.1.1"}, // exactly at {5,1}'s floor
		{"4.0.0", "6.0.5"}, // exactly at {6,0}'s floor
		{"5.0.0", "6.5.2"}, // exactly at {6,5}'s floor
		{"5.0.0", "6.6.5"}, // exactly at {6,6}'s floor
		{"5.5.0", "6.6.6"},
		{"6.0.0", "6.6.6"},
		{"6.6.0", "7.0.5"},
		{"7.1.0", "7.2.4"},
		// At or above the target's chain floor.
		{"6.5.0", "7.1.2"},
		{"6.6.0", "7.1.2"},
		{"7.0.0", "7.2.0"},
		{"6.5.0", "7.2.0"},
	}

	for _, u := range valid {
		if err := CheckOfflineUpgradePath(u.old, u.new); err != nil {
			t.Errorf("expected offline upgrade %s -> %s to be valid: %v", u.old, u.new, err)
		}
	}
}

func TestInvalidOfflineUpgrades(t *testing.T) {
	type upgrade struct {
		old, new, expectedMsg string
	}

	invalid := []upgrade{
		{"8.0.0", "7.6.0", "Downgrades are not supported"},
		{"8.5.0", "8.0.0", "Downgrades are not supported"},
		// Below the target's chain floor: ns_server would halt(1) on boot.
		{"7.1.0", "8.0.0", "needs a source of 7.2.0 or later"},
		// Unmapped future major is a hard stop.
		{"8.5.0", "9.0.0", "not a supported offline-upgrade target"},
		{"2.0.0", "5.1.1", "needs a source of 3.0.0 or later"},
		{"3.0.0", "6.0.5", "needs a source of 4.0.0 or later"},
		{"4.6.5", "6.5.2", "needs a source of 5.0.0 or later"},
		{"4.0.0", "6.6.5", "needs a source of 5.0.0 or later"},
		// Targets below 5.x are unsupported.
		{"4.0.0", "4.6.5", "not a supported offline-upgrade target"},
		// Below the target's chain floor.
		{"6.6.0", "8.5.0", "needs a source of 7.2.0 or later"},
		{"7.0.0", "7.6.0", "needs a source of 7.1.0 or later"},
		{"6.6.0", "7.6.0", "needs a source of 7.1.0 or later"},
		{"7.0.0", "7.2.4", "needs a source of 7.1.0 or later"},
	}

	for _, u := range invalid {
		err := CheckOfflineUpgradePath(u.old, u.new)
		if err == nil {
			t.Errorf("expected offline upgrade %s -> %s to be invalid", u.old, u.new)
			continue
		}

		if !strings.Contains(err.Error(), u.expectedMsg) {
			t.Errorf("expected offline upgrade %s -> %s to fail with '%s' but got '%s'", u.old, u.new, u.expectedMsg, err.Error())
		}
	}
}

func TestCheckInPlaceUpgradePath(t *testing.T) {
	type upgrade struct {
		old, new string
		valid    bool
	}

	cases := []upgrade{
		// Rollback: routed to swap by the no-downgrade rule.
		{"7.2.5", "7.2.0", false},
		// Digest sentinel against a real target reads as a downgrade.
		{"9.9.9", "8.0.0", false},
		// In-place only refuses downgrades; floors are checked at admission.
		{"7.0.5", "7.2.0", true},
		{"7.1.3", "8.0.0", true},
		{"7.1.3", "7.6.2", true},
		{"8.0.0", "8.0.3", true},
		{"8.0.0", "8.0.0", true},
	}

	for _, u := range cases {
		if err := CheckInPlaceUpgradePath(u.old, u.new); (err == nil) != u.valid {
			t.Errorf("expected CheckInPlaceUpgradePath(%s, %s) valid = %v, got %v", u.old, u.new, u.valid, err)
		}
	}
}

func TestUpgradeCrosses(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"7.6.4", "8.0.0", true},
		{"7.6.4", "8.5.0", true},
		{"8.0.0", "8.5.0", false}, // already past it
		{"7.2.0", "7.6.4", false}, // not reached
		{"8.0.0", "7.6.4", false}, // downgrade
		{"7.6.4", "7.6.4", false},
	}

	for _, c := range cases {
		if got, err := UpgradeCrosses(c.from, c.to, "8.0.0"); err != nil || got != c.want {
			t.Errorf("UpgradeCrosses(%s, %s, 8.0.0) = %v, %v; want %v", c.from, c.to, got, err, c.want)
		}
	}

	if _, err := UpgradeCrosses("bad", "8.0.0", "8.0.0"); err == nil {
		t.Error("expected an error for an unparsable version")
	}
}

func TestIndexUpgradeRequiresSwap(t *testing.T) {
	type upgrade struct {
		source, target string
	}

	required := []upgrade{
		{"7.1.0", "7.6.0"},
		{"7.2.4", "7.6.1"},
		{"7.1.6", "7.6.1"},
		// Any pre-7.6 source.
		{"7.0.5", "7.6.0"},
		{"6.6.0", "7.6.1"},
	}

	for _, u := range required {
		if ok, err := IndexUpgradeRequiresSwap(u.source, u.target); err != nil {
			t.Error(err)
		} else if !ok {
			t.Errorf("expected upgrade %s -> %s to require swap rebalance for index nodes", u.source, u.target)
		}
	}

	notRequired := []upgrade{
		// Target already fixed.
		{"7.2.4", "7.6.2"},
		// Source not in the 7.1.x/7.2.x window.
		{"7.6.0", "7.6.1"},
		// Target above the window.
		{"7.2.0", "8.0.0"},
		// Target below the window.
		{"7.2.0", "7.2.4"},
	}

	for _, u := range notRequired {
		if ok, err := IndexUpgradeRequiresSwap(u.source, u.target); err != nil {
			t.Error(err)
		} else if ok {
			t.Errorf("expected upgrade %s -> %s to not require swap rebalance for index nodes", u.source, u.target)
		}
	}
}

func TestCheckHibernationUpgradePath(t *testing.T) {
	type upgrade struct {
		old, new      string
		hasIndexNodes bool
		wakesMixed    bool
		expectedMsg   string // empty means the upgrade must be valid
	}

	cases := []upgrade{
		// Index nodes targeting the defective plasma-migration window.
		{"7.2.0", "7.6.0", true, false, "upgrade to 7.6.2 or later instead"},
		{"7.2.4", "7.6.1", true, false, "upgrade to 7.6.2 or later instead"},
		{"7.1.2", "7.6.0", true, false, "upgrade to 7.6.2 or later instead"},
		// Fixed target, index nodes fine.
		{"7.2.0", "7.6.2", true, false, ""},
		// Window target but no index nodes — nothing to migrate.
		{"7.2.0", "7.6.0", false, false, ""},
		// Below the target's chain floor (7.1): the offline floor check fires
		// before the index rule.
		{"7.0.5", "7.6.0", true, false, "needs a source of 7.1.0 or later"},
		// Source not in the window (already on 7.6.x).
		{"7.6.0", "7.6.1", true, false, ""},
		// PVPC > 0 wakes a mixed cluster, so the staging stop applies.
		{"6.5.0", "7.0.5", false, true, "An online upgrade to 7.0.5 needs a source of 6.6.0 or later"},
		{"7.1.2", "7.6.2", false, true, ""},
		{"7.2.0", "8.0.0", false, true, ""},
	}

	for _, u := range cases {
		err := CheckHibernationUpgradePath(u.old, u.new, u.hasIndexNodes, u.wakesMixed)

		if u.expectedMsg == "" {
			if err != nil {
				t.Errorf("expected hibernation upgrade %s -> %s (index nodes: %v) to be valid but got '%s'", u.old, u.new, u.hasIndexNodes, err.Error())
			}
			continue
		}

		if err == nil {
			t.Errorf("expected hibernation upgrade %s -> %s (index nodes: %v) to be invalid", u.old, u.new, u.hasIndexNodes)
			continue
		}

		if !strings.Contains(err.Error(), u.expectedMsg) {
			t.Errorf("expected hibernation upgrade %s -> %s to fail with '%s' but got '%s'", u.old, u.new, u.expectedMsg, err.Error())
		}
	}
}

func TestMinOfflineUpgradeSource724Split(t *testing.T) {
	cases := []struct {
		target string
		floor  string
	}{
		// 7.2.4 moved to OTP 25 and pruned its chain to {7,1} (MB-59786) —
		// the one known intra-line floor change.
		{"7.2.0", "6.5.0"},
		{"7.2.3", "6.5.0"},
		{"7.2.4", "7.1.0"},
		{"7.2.8", "7.1.0"},
		{"7.6.0", "7.1.0"},
		{"8.0.0", "7.2.0"},
	}

	for _, c := range cases {
		floor, err := minUpgradeSource(offlineUpgradeFloors, c.target)
		if err != nil {
			t.Fatal(err)
		} else if floor != c.floor {
			t.Errorf("expected floor %s for target %s but got %s", c.floor, c.target, floor)
		}
	}
}
