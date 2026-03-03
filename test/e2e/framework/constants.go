//go:build !redhat
// +build !redhat

/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
