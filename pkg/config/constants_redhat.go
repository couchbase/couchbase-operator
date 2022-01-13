//go:build redhat

package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	imageRepo = "registry.connect.redhat.com"
)

var (
	admissionImageDefault = imageRepo + "/couchbase/admission-controller:" + version.WithRevision()
	operatorImageDefault  = imageRepo + "/couchbase/operator:" + version.WithRevision()
	caoBinaryName         = "cao"
)
