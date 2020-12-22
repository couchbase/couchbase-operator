package version

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/revision"
)

const (
	Application = "couchbase-operator"
)

var (
	Version     string
	Revision    string
	BuildNumber string
)

// This will generate things like 1.0.0 and 1.0.0-beta1 and should be used
// for things like docker images.
func WithRevision() string {
	v := Version

	if Revision != "" {
		v = v + "-" + Revision
	}

	return v
}

// This will generate things like "1.0.0 (build 123)" and should be used for
// binary version strings so we can tell exactly which build (and by extension
// commit) is being used.
func WithBuildNumber() string {
	return fmt.Sprintf("%s (build %s)", WithRevision(), BuildNumber)
}

// UserAgent is a valid user agent string as defined by the HTTP specification
// https://tools.ietf.org/html/rfc1945#section-10.15, this is used to identify
// what unique version of the Operator has been interacting with Couchbase
// server.
func UserAgent() string {
	return fmt.Sprintf("%s/%s (commit/%s; build/%s)", Application, Version, revision.Revision(), BuildNumber)
}
