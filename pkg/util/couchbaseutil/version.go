/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// Utilities for parsing and accessing fields within version strings.

package couchbaseutil

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

// A Couchbase version.  This type is immutable.
type Version struct {
	// Full name e.g. "enterprise-5.5.0"
	version string
	// Prefix (release edition) e.g. "enterprise"
	prefix string
	// Semantic version e.g. [5, 0, 1]
	semver []int
}

// Initialise the version struct.  Prefix and semver are lazilly initialised.
func NewVersion(version string) (*Version, error) {
	if version == "" {
		return nil, fmt.Errorf("%w: null version", errors.NewStackTracedError(errors.ErrInvalidVersion))
	}

	// lookup version associated with sha256 digest
	if IsSHA256Version(version) {
		version = GetSHA256Version(version)
	}

	// Gather semver and optional edition with expected format
	// "<edition>-<semver>" or "<semver>-<edition>", ie:
	//			"5.5.0" and "enterprise-5.5.0" and "5.5.0-beta"
	re := regexp.MustCompile(`^(?:[0-9]+.*:)?(?:(\w+)-)?(\d+)\.(\d+)\.(\d+)(?:-(\w+))?`)

	matches := re.FindStringSubmatch(version)
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: malformed version '%s'", errors.NewStackTracedError(errors.ErrInvalidVersion), version)
	}

	prefix := matches[1]
	if prefix == "" {
		prefix = matches[5]
	}

	semver := make([]int, 3)

	for i := 0; i < 3; i++ {
		val, _ := strconv.Atoi(matches[i+2])
		semver[i] = val
	}

	return &Version{version, prefix, semver}, nil
}

// Checks if version is sha256.
func IsSHA256Version(version string) bool {
	re := regexp.MustCompile(`^[A-Fa-f0-9]{64}$`)
	matches := re.FindStringSubmatch(version)

	return len(matches) != 0
}

// Get readable version from sha256.
func GetSHA256Version(version string) string {
	if v, ok := constants.ImageDigests[version]; ok {
		return v
	}

	// attempt additional lookup on associated config map
	if digests, ok := os.LookupEnv(constants.EnvDigestsConfigMap); ok {
		// match against digests presented as list of equalities
		re := regexp.MustCompile(version + `=(.*)\s?\n`)

		parts := re.FindStringSubmatch(digests)
		if len(parts) == 2 {
			return parts[1]
		}
	}

	// Trusting user provided a valid version since we don't
	// know what version the digest maps to
	return "9.9.9"
}

// Return the full version string.
func (v *Version) Version() string {
	return v.version
}

// Get the prefix (edition) string.
func (v *Version) Prefix() string {
	return v.prefix
}

// Get the major revision.
func (v *Version) Major() int {
	return v.semver[0]
}

// Get the minor revision.
func (v *Version) Minor() int {
	return v.semver[1]
}

// Get the patch revision.
func (v *Version) Patch() int {
	return v.semver[2]
}

// Semver gets the semver string.
func (v *Version) Semver() string {
	return fmt.Sprintf("%d.%d.%d", v.Major(), v.Minor(), v.Patch())
}

// String representation of version.
func (v *Version) String() string {
	return v.Semver()
}

// Compare semantic versions.
// Returns -1 if v < o, 0 if v == o and 1 if v > o.
func (v *Version) Compare(o *Version) int {
	for index := range v.semver {
		if v.semver[index] > o.semver[index] {
			return 1
		}

		if v.semver[index] < o.semver[index] {
			return -1
		}
	}

	return 0
}

// Equal returns true if v == o.
func (v *Version) Equal(o *Version) bool {
	return v.Compare(o) == 0
}

// Less returns true if v < o.
func (v *Version) Less(o *Version) bool {
	return v.Compare(o) < 0
}

// GreaterEqual returns true if v >= o.
func (v *Version) GreaterEqual(o *Version) bool {
	return !v.Less(o)
}

func (v *Version) GreaterEqualString(o string) bool {
	ov, _ := NewVersion(o)
	return v.GreaterEqual(ov)
}

func VerifyVersion(version string) error {
	v, err := NewVersion(version)
	if err != nil {
		return err
	}

	minVersion, _ := NewVersion(constants.CouchbaseVersionMin)
	if v.Compare(minVersion) == -1 {
		return fmt.Errorf("%w: version %s is unsupported, requires at least %s", errors.NewStackTracedError(errors.ErrInvalidVersion), version, minVersion)
	}

	return nil
}
