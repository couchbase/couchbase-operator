// Utilities for parsing and accessing fields within version strings.

package couchbaseutil

import (
	"fmt"
	"strconv"
	"strings"
)

// A Couchbase version.  This type is immutable
type Version struct {
	// Full name e.g. "enterprise-5.0.1"
	version string
	// Prefix (release edition) e.g. "enterprise"
	prefix string
	// Semantic version e.g. [5, 0, 1]
	semver []int
}

// Initialise the version struct.  Prefix and semver are lazilly initialised
func NewVersion(version string) (*Version, error) {
	if version == "" {
		return nil, fmt.Errorf("null version")
	}

	// Try split on the last '-'.  If there is one, assume that everything
	// before is the prefix (edition) and everything after us the semver
	// otherwise we just assume the whole version string is the semver.
	// This handles both "5.0.1" and "enterprise-5.0.1"
	semverString := version
	prefix := ""
	index := strings.LastIndex(version, "-")
	if index != -1 {
		// "enterprise-" will cause array out of bounds
		if index+1 >= len(version) {
			return nil, fmt.Errorf("malformed version '%s'", version)
		}
		prefix = version[:index]
		semverString = version[index+1:]
	}

	// Split into individual fields, we expect this to be in the form:
	// major.minor.patch
	semverFields := strings.Split(semverString, ".")
	if len(semverFields) != 3 {
		return nil, fmt.Errorf("badly formed semantic version '%s'", semverString)
	}

	// Convert into the output slice
	semver := make([]int, len(semverFields))
	for index, valueRaw := range semverFields {
		value, err := strconv.Atoi(valueRaw)
		if err != nil {
			return nil, fmt.Errorf("unexpected field '%s' in semver '%s'", valueRaw, semverString)
		}
		semver[index] = value
	}

	return &Version{version, prefix, semver}, nil
}

// Return the full version string
func (v *Version) Version() string {
	return v.version
}

// Get the prefix (edition) string
func (v *Version) Prefix() string {
	return v.prefix
}

// Get the major revision
func (v *Version) Major() int {
	return v.semver[0]
}

// Get the minor revision
func (v *Version) Minor() int {
	return v.semver[1]
}

// Get the patch revision
func (v *Version) Patch() int {
	return v.semver[2]
}

// Compare semantic versions
// Returns -1 if a < b, 0 if a == b and 1 if a > b
func (a *Version) Compare(b *Version) int {
	for index := range a.semver {
		if a.semver[index] > b.semver[index] {
			return 1
		}
		if a.semver[index] < b.semver[index] {
			return -1
		}
	}
	return 0
}
