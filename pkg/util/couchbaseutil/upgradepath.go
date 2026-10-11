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
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

// Each upgrade kind has a floor table: a row covers targets from since until
// the next row, which accept sources >= minSource. Online, old and new nodes
// mix, so the floor is ns_server's min compat version; offline, nodes boot the
// target on old data, so it is the start of its ns_config upgrade chain.
type upgradeFloor struct {
	since     string
	minSource string
}

// unsupported closes a table: no floor is known for a major this operator has
// not been taught about.
const unsupported = ""

// onlineUpgradeFloors gives the documented upgrade paths:
//
//	5.x, 6.0, 6.5 -> 6.6 -> 7.2.3 -> 8.0
//	7.0           -> 7.2.3 -> 8.0
//	7.1           -> 7.2.4+ or 7.6 -> 8.0
//	7.2, 7.6      -> 8.0
var onlineUpgradeFloors = []upgradeFloor{
	{"5.0.0", "3.0.0"},
	{"5.5.0", "4.0.0"},
	{"6.5.0", "5.0.0"},
	// Pre-6.6 clusters must stop at 6.6 before entering 7.x (MB-51464).
	{"7.0.0", "6.6.0"},
	// 7.2.4 moved to OTP 25 (MB-59786, MB-58925).
	{"7.2.4", "7.1.0"},
	{"8.0.0", "7.2.0"},
	{"9.0.0", unsupported},
}

// offlineUpgradeFloors is looser than online below 7.2.4: with no old node left
// running, only the ns_config chain matters. Sub-7 targets are only reachable
// when admission validation is skipped.
var offlineUpgradeFloors = []upgradeFloor{
	{"5.1.0", "3.0.0"},
	{"6.0.0", "4.0.0"},
	{"6.5.0", "5.0.0"},
	{"7.0.0", "5.5.3"},
	{"7.1.0", "6.5.0"},
	// 7.2.4 pruned its chain to start at 7.1 (MB-59786).
	{"7.2.4", "7.1.0"},
	{"8.0.0", "7.2.0"},
	{"9.0.0", unsupported},
}

// CheckUpgradePath validates an online (rolling) upgrade.
func CheckUpgradePath(current, target string) error {
	return checkUpgradePath(onlineUpgradeFloors, "online", current, target)
}

// CheckOfflineUpgradePath validates an offline upgrade, where every node boots
// the target on its retained volume.
func CheckOfflineUpgradePath(current, target string) error {
	return checkUpgradePath(offlineUpgradeFloors, "offline", current, target)
}

// CheckHibernationUpgradePath validates an image change made while hibernated
// or by an Offline upgrade. Index nodes can't wake onto 7.6.0/7.6.1, as a wake
// can't swap around MB-62547. alsoOnline adds the online rules, for a mixed
// wake or a cluster not asleep yet.
func CheckHibernationUpgradePath(current, target string, hasIndexNodes, alsoOnline bool) error {
	if err := CheckOfflineUpgradePath(current, target); err != nil {
		return err
	}

	if hasIndexNodes {
		requiresSwap, err := IndexUpgradeRequiresSwap(current, target)
		if err != nil {
			return err
		}

		if requiresSwap {
			return fmt.Errorf("%w: cannot upgrade from %s to %s while hibernating. Index service nodes require swap rebalance for upgrades to 7.6.0 or 7.6.1, which cannot happen on a hibernation wake; upgrade to 7.6.2 or later instead", errors.ErrInvalidVersion, current, target)
		}
	}

	if alsoOnline {
		return CheckUpgradePath(current, target)
	}

	return nil
}

// CheckInPlaceUpgradePath validates recreating a node on its retained volume
// during an online upgrade. Only downgrades are refused: the path itself was
// validated at admission, and under skip-validation the user's choice stands.
func CheckInPlaceUpgradePath(current, target string) error {
	return checkNotDowngrade(current, target)
}

func checkUpgradePath(floors []upgradeFloor, process, current, target string) error {
	// Same version (e.g. a registry change) is not an upgrade.
	if same, err := VersionEqual(current, target); err != nil || same {
		return err
	}

	if err := checkNotDowngrade(current, target); err != nil {
		return err
	}

	floor, err := minUpgradeSource(floors, target)
	if err != nil {
		return err
	}

	if floor == unsupported {
		return fmt.Errorf("%w: cannot upgrade from %s to %s. Couchbase Server %s is not a supported %s-upgrade target for this operator version", errors.ErrInvalidVersion, current, target, target, process)
	}

	if below, err := VersionBefore(current, floor); err != nil {
		return err
	} else if below {
		return fmt.Errorf("%w: cannot upgrade from %s to %s. An %s upgrade to %s needs a source of %s or later; upgrade to an intermediate version first", errors.ErrInvalidVersion, current, target, process, target, floor)
	}

	return nil
}

func checkNotDowngrade(current, target string) error {
	if downgrade, err := VersionBefore(target, current); err != nil {
		return err
	} else if downgrade {
		return fmt.Errorf("%w: cannot upgrade from %s to %s. Downgrades are not supported", errors.ErrInvalidVersion, current, target)
	}

	return nil
}

// minUpgradeSource returns the floor of the last row at or below target.
func minUpgradeSource(floors []upgradeFloor, target string) (string, error) {
	for i := len(floors) - 1; i >= 0; i-- {
		covered, err := VersionAfter(target, floors[i].since)
		if err != nil {
			return "", err
		}

		if covered {
			return floors[i].minSource, nil
		}
	}

	return unsupported, nil
}

// UpgradeCrosses reports whether moving from one version to another crosses
// version: from is below it and to is at or above it.
func UpgradeCrosses(from, to, version string) (bool, error) {
	fromBefore, err := VersionBefore(from, version)
	if err != nil {
		return false, err
	}

	toAtLeast, err := VersionAfter(to, version)
	if err != nil {
		return false, err
	}

	return fromBefore && toAtLeast, nil
}

// IndexUpgradeRequiresSwap reports whether index nodes must swap rebalance:
// pre-7.6 to 7.6.0/7.6.1, whose plasma layout migration is broken (MB-62547).
func IndexUpgradeRequiresSwap(source, target string) (bool, error) {
	crosses76, err := UpgradeCrosses(source, target, "7.6.0")
	if err != nil {
		return false, err
	}

	targetBefore762, err := VersionBefore(target, "7.6.2")
	if err != nil {
		return false, err
	}

	return crosses76 && targetBefore762, nil
}
