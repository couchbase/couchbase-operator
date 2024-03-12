package tlsutil

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

type Version struct {
	semver [2]int
}

func NewVersion(v string) (*Version, error) {
	if !re.MatchString(v) {
		return nil, fmt.Errorf("%w: malformed tls string '%s'", errors.NewStackTracedError(errors.ErrInvalidVersion), v)
	}

	stripped := v[3:]

	parts := strings.Split(stripped, ".")

	maj, _ := strconv.Atoi(parts[0])
	min, _ := strconv.Atoi(parts[1])

	return &Version{
		semver: [2]int{maj, min},
	}, nil
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

func (v *Version) Less(o *Version) bool {
	return v.Compare(o) < 0
}

// GreaterEqual returns true if v >= o.
func (v *Version) GreaterEqual(o *Version) bool {
	return !v.Less(o)
}

func StringGreaterEqual(v, r string) (bool, error) {
	version, err := NewVersion(v)
	if err != nil {
		return false, err
	}

	requirement, err := NewVersion(r)
	if err != nil {
		return false, err
	}

	return version.GreaterEqual(requirement), nil
}

// At the bottom of the file because my VSCode messes up syntax highlighting for all code after it.
var re = regexp.MustCompile(`^(?i)(TLS)[0-9]+\.[0-9]+$`)
