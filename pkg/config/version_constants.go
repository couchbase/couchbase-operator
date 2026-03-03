/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package config

import "k8s.io/apimachinery/pkg/version"

var (
	// Note: these should be updated every release.
	technicalLowerBound = &version.Info{Major: "1", Minor: "23", GitVersion: "v1.23.0"}
	supportedLowerBound = &version.Info{Major: "1", Minor: "26", GitVersion: "v1.26.0"}
	supportedUpperBound = &version.Info{Major: "1", Minor: "34", GitVersion: "v1.34.0"}
)
