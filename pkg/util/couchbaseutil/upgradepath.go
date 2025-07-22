package couchbaseutil

import (
	"fmt"
	"strconv"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

// SupportedUpgradePath represents the valid upgrade path.
// Each version is put into a group based on the versions in this array,
// Versions are only valid if they are within the same group or to the next group.
// E.g. Group 1 is version >= 5.0.0 AND < 6.6.0.
var SupportedUpgradePath = []string{"0.0.0", "5.0.0", "6.6.0", "7.1.0", "7.2.4"}

func ValidUpgrade(old, new string) (bool, error) {
	if isUpgrade, err := VersionAfter(new, old); err != nil {
		return false, err
	} else if !isUpgrade {
		return false, nil
	}

	oldGroup, err := getUpgradeGroup(old)
	if err != nil {
		return false, err
	}

	newGroup, err := getUpgradeGroup(new)
	if err != nil {
		return false, err
	}

	withinTwoMajorVersions, err := VersionsWithinTwoMajorVersions(old, new)
	if err != nil {
		return false, err
	}

	return withinTwoMajorVersions && (newGroup == oldGroup || oldGroup+1 == newGroup), nil
}

func CheckUpgradePath(currentVersion, newVersion string) error {
	if valid, err := ValidUpgrade(currentVersion, newVersion); err != nil {
		return err
	} else if !valid {
		if isDowngrade, err := VersionBefore(newVersion, currentVersion); err != nil {
			return err
		} else if isDowngrade {
			return fmt.Errorf("%w: cannot upgrade from %s to %s. Downgrades are not supported", errors.ErrInvalidVersion, currentVersion, newVersion)
		}
		upperboundVersion, err := GetUpgradeUpperbound(currentVersion)
		if err != nil {
			return err
		}

		return fmt.Errorf("%w: cannot upgrade from %s to %s. Version must be less than %s", errors.ErrInvalidVersion, currentVersion, newVersion, upperboundVersion)
	}

	return nil
}

func GetUpgradeUpperbound(version string) (string, error) {
	upgradeGroup, err := getUpgradeGroup(version)
	if err != nil {
		return "", err
	}

	upperBoundGroup := upgradeGroup + 2

	if upperBoundGroup >= len(SupportedUpgradePath) {
		v, err := NewVersion(version)
		if err != nil {
			return "", err
		}

		v.semver[0]++

		var upperVersion string

		for _, item := range v.semver {
			upperVersion += strconv.Itoa(item) + "."
		}

		return upperVersion, nil
	}

	return SupportedUpgradePath[upperBoundGroup], nil
}

func getUpgradeGroup(version string) (int, error) {
	for versionGroup := range SupportedUpgradePath {
		after, err := VersionAfter(version, SupportedUpgradePath[versionGroup])
		if err != nil {
			return 0, err
		}

		if !after {
			return versionGroup - 1, nil
		}
	}

	return len(SupportedUpgradePath) - 1, nil
}
