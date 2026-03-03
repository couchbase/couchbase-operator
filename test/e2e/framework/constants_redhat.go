/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
	mobileImageDefault              = imageRepo + "/couchbase/sync-gateway:2.8.2-1"
	serverImageDefault              = imageRepo + "/couchbase/server:7.0.2-1"
	serverImageUpgradeFromDefault   = imageRepo + "/couchbase/server:6.6.3-1"
	exporterImageDefault            = imageRepo + "/couchbase/exporter:1.0.5-1"
	exporterImageUpgradeFromDefault = imageRepo + "/couchbase/exporter:1.0.4-2"
	backupImageDefault              = imageRepo + "/couchbase/operator-backup:1.2.0-1"
	loggingImageDefault             = imageRepo + "/couchbase/fluent-bit:1.1.1-1"
	loggingImageUpgradeFromDefault  = imageRepo + "/couchbase/fluent-bit:1.0.4-1"
)
