// +build !redhat

package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	admissionImageDefault = "couchbase/admission-controller:" + version.Version
	operatorImageDefault  = "couchbase/operator:" + version.Version
)
