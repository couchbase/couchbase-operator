/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

//go:build redhat

package certification

import (
	"github.com/couchbase/couchbase-operator/pkg/version"

	corev1 "k8s.io/api/core/v1"
)

const (
	imageRepo              = "registry.connect.redhat.com"
	useFSGroup             = false
	imagePullPolicyDefault = string(corev1.PullIfNotPresent)
)

var imageDefault = imageRepo + "/couchbase/operator-certification:" + version.WithRevision()
