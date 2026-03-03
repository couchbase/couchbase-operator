/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
