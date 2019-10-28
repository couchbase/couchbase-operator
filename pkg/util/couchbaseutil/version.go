// Utilities for parsing and accessing fields within version strings.

package couchbaseutil

import (
	"fmt"
	"regexp"
	"strconv"

	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

// A Couchbase version.  This type is immutable
type Version struct {
	// Full name e.g. "enterprise-5.5.0"
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

	// Gather semver and optional edition with expected format
	// "<edition>-<semver>" or "<semver>-<edition>", ie:
	//			"5.5.0" and "enterprise-5.5.0" and "5.5.0-beta"
	re := regexp.MustCompile(`^(?:(\w+)-)?(\d+)\.(\d+)\.(\d+)(?:-(\w+))?`)
	matches := re.FindStringSubmatch(version)
	if len(matches) == 0 {
		return nil, fmt.Errorf("malformed version '%s'", version)
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

// Semver gets the semver string
func (v *Version) Semver() string {
	return fmt.Sprintf("%d.%d.%d", v.Major(), v.Minor(), v.Patch())
}

// Compare semantic versions
// Returns -1 if v < o, 0 if v == o and 1 if v > o
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

// Less returns true if v < o
func (v *Version) Less(o *Version) bool {
	return v.Compare(o) < 0
}

// GreaterEqual returns true if v >= o
func (v *Version) GreaterEqual(o *Version) bool {
	return !v.Less(o)
}

func VerifyVersion(version string) error {
	v, err := NewVersion(version)
	if err != nil {
		return err
	} else {
		minVersion, _ := NewVersion(constants.CouchbaseVersionMin)
		if v.Compare(minVersion) == -1 {
			return cberrors.ErrUnsupportedVersion{Version: version}
		}
	}
	return nil
}
