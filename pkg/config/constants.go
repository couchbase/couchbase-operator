//go:build !redhat
// +build !redhat

package config

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
	k8sversion "k8s.io/apimachinery/pkg/version"
)

var (
	admissionImageDefault = "couchbase/admission-controller:" + version.WithRevision()
	operatorImageDefault  = "couchbase/operator:" + version.WithRevision()
	caoBinaryName         = "cao"

	// Note: these should be updated every release.

	technicalLowerBound = &k8sversion.Info{Major: "1", Minor: "26", GitVersion: "v1.26.0"}
	supportedLowerBound = &k8sversion.Info{Major: "1", Minor: "26", GitVersion: "v1.26.0"}
	supportedUpperBound = &k8sversion.Info{Major: "1", Minor: "29", GitVersion: "v1.29.0"}
)
