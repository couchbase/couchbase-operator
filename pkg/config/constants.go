//go:build !redhat
// +build !redhat

package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

var (
	admissionImageDefault = "couchbase/admission-controller:" + version.WithRevision()
	operatorImageDefault  = "couchbase/operator:" + version.WithRevision()
)
