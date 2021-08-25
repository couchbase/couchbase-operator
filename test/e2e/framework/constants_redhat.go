// +build redhat

package framework

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	imageRepo = "registry.connect.redhat.com"
)

var (
	// TODO: all the other images for test need a RHCC analogue.
	admissionImageDefault = imageRepo + "/couchbase/admission-controller:" + version.WithRevision()
	operatorImageDefault  = imageRepo + "/couchbase/operator:" + version.WithRevision()
)
