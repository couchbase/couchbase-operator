/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package version

import (
	"fmt"
)

const (
	Application = "couchbase-operator"
)

var (
	Version        string
	Revision       string
	RevisionRedHat string
	BuildNumber    string
)

// This will generate things like 1.0.0 and 1.0.0-beta1 and should be used
// for things like docker images.
func VersionWithRevision() string {
	v := Version
	if Revision != "" {
		v = v + "-" + Revision
	}
	return v
}

// This will do as above, but also adds in the Red Hat container tag revision
// e.g. 1.0.0-beta1-1.
func VersionWithRevisionRedHat() string {
	return VersionWithRevision() + "-" + RevisionRedHat
}

// This will generate things like "1.0.0 (build 123)" and should be used for
// binary version strings so we can tell exactly which build (and by extension
// commit) is being used.
func VersionWithBuildNumber() string {
	return fmt.Sprintf("%s (build %s)", VersionWithRevision(), BuildNumber)
}
