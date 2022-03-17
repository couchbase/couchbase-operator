//go:build redhat

package framework

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	imageRepo = "registry.connect.redhat.com"
)

var (
	admissionImageDefault           = imageRepo + "/couchbase/admission-controller:" + version.WithRevision()
	operatorImageDefault            = imageRepo + "/couchbase/operator:" + version.WithRevision()
	mobileImageDefault              = imageRepo + "/couchbase/sync-gateway:2.8.3-1"
	serverImageDefault              = imageRepo + "/couchbase/server:7.0.3-1"
	serverImageUpgradeFromDefault   = imageRepo + "/couchbase/server:6.6.4-1"
	exporterImageDefault            = imageRepo + "/couchbase/exporter:1.0.5-1"
	exporterImageUpgradeFromDefault = imageRepo + "/couchbase/exporter:1.0.4-2"
	backupImageDefault              = imageRepo + "/couchbase/operator-backup:1.3.0-1"
	loggingImageDefault             = imageRepo + "/couchbase/fluent-bit:1.2.0-1"
	loggingImageUpgradeFromDefault  = imageRepo + "/couchbase/fluent-bit:1.1.1-1"
)
