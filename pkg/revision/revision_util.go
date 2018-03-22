package revision

import (
	"fmt"
)

// Revision returns the SCM revision information for development builds to
// pinpoint the exact source code tree a defect was raised against.  If the
// information is not available we default to an official release.
func Revision() string {
	rev := "release"
	if gitBranch != "" && gitRevision != "" {
		rev = fmt.Sprintf("%s %s", gitBranch, gitRevision)
	}
	return rev
}
