// +build redhat

package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	imageRepo             = "registry.connect.redhat.com"
	admissionImageDefault = imageRepo + "/couchbase/admission-controller:" + version.Version + "-" + version.RedHatBuild
	operatorImageDefault  = imageRepo + "/couchbase/operator:" + version.Version + "-" + version.RedHatBuild
)
