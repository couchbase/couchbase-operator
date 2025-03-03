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
	serverImageDefault              = "couchbase/server:7.6.4"
	serverImageUpgradeFromDefault   = "couchbase/server:7.2.4"
	exporterImageDefault            = "couchbase/exporter:1.0.10"
	exporterImageUpgradeFromDefault = "couchbase/exporter:1.0.9"
	backupImageDefault              = "couchbase/operator-backup:1.4.1"
	loggingImageDefault             = "couchbase/fluent-bit:1.2.9"
	loggingImageUpgradeFromDefault  = "couchbase/fluent-bit:1.2.0"
	cloudNativeGatewayImageDefault  = "ghcr.io/cb-vanilla/cloud-native-gateway:1.1.0-124"
)
