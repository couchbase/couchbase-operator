//go:build !redhat
// +build !redhat

package framework

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

var (
	admissionImageDefault           = "couchbase/admission-controller:" + version.WithRevision()
	operatorImageDefault            = "couchbase/operator:" + version.WithRevision()
	mobileImageDefault              = "couchbase/sync-gateway:2.8.2-enterprise"
	serverImageDefault              = "couchbase/server:7.0.2"
	serverImageUpgradeFromDefault   = "couchbase/server:6.6.3"
	exporterImageDefault            = "couchbase/exporter:1.0.5"
	exporterImageUpgradeFromDefault = "couchbase/exporter:1.0.4"
	backupImageDefault              = "couchbase/operator-backup:1.2.0"
	loggingImageDefault             = "couchbase/fluent-bit:1.1.1"
	loggingImageUpgradeFromDefault  = "couchbase/fluent-bit:1.0.4"
)
