// +build !redhat

package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	admissionImageDefault = "couchbase/admission-controller:" + version.Version + version.DockerBuild
	operatorImageDefault  = "couchbase/operator:" + version.Version + version.DockerBuild
)
