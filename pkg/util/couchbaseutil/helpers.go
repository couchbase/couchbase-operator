package couchbaseutil

import (
	"fmt"
	"time"
)

// VersionAfter determines whether the configured version is greater than
// or equal to the required version, useful for enabling features at runtime.
func VersionAfter(version, required string) (bool, error) {
	v1, err := NewVersion(version)
	if err != nil {
		return false, err
	}

	v2, err := NewVersion(required)
	if err != nil {
		return false, err
	}

	return v1.GreaterEqual(v2), nil
}

func VersionsWithinTwoMajorVersions(oldVersion string, newVersion string) (bool, error) {
	old, err := NewVersion(oldVersion)
	if err != nil {
		return false, err
	}

	newV, err := NewVersion(newVersion)
	if err != nil {
		return false, err
	}

	gap := newV.semver[0] - old.semver[0]

	if gap >= 2 {
		return false, nil
	}

	return true, nil
}

// VersionBefore determines whether the configured version is less than
// the required version.
func VersionBefore(version, required string) (bool, error) {
	v1, err := NewVersion(version)
	if err != nil {
		return false, err
	}

	v2, err := NewVersion(required)
	if err != nil {
		return false, err
	}

	return v1.Less(v2), nil
}

func (b *Bucket) CanBeMigrated(backend CouchbaseStorageBackend) (bool, string) {
	if backend == CouchbaseStorageBackendMagma {
		if b.BucketMemoryQuota < 1024 {
			return false, fmt.Sprintf("memory quota (%v) below minimum %v", b.BucketMemoryQuota, 1024)
		}
	}

	return true, ""
}

// The Couchbase Query Duration string requires a number and a unit so we just convert to nanoseconds
// to not lose any resolution.
func CouchbaseQueryDurationString(t time.Duration) string {
	return fmt.Sprintf("%vns", t.Nanoseconds())
}
