// +build !redhat

package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

var (
	admissionImageDefault = "couchbase/admission-controller:" + version.VersionWithRevision()
	operatorImageDefault  = "couchbase/operator:" + version.VersionWithRevision()
)
