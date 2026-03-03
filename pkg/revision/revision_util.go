/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
