//go:build !redhat
// +build !redhat

package framework

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

var (
	admissionImageDefault           = "couchbase/admission-controller:" + version.WithRevision()
	operatorImageDefault            = "couchbase/operator:" + version.WithRevision()
	mobileImageDefault              = "couchbase/sync-gateway:3.0.3-enterprise"
	serverImageDefault              = "couchbase/server:7.0.3"
	serverImageUpgradeFromDefault   = "couchbase/server:6.6.4"
	exporterImageDefault            = "couchbase/exporter:1.0.6"
	exporterImageUpgradeFromDefault = "couchbase/exporter:1.0.5"
	backupImageDefault              = "couchbase/operator-backup:1.3.0"
	loggingImageDefault             = "couchbase/fluent-bit:1.2.0"
	loggingImageUpgradeFromDefault  = "couchbase/fluent-bit:1.1.1"
	cloudNativeGatewayImageDefault  = "ghcr.io/cb-vanilla/cloud-native-gateway:0.1.0-132"
)
