/*
Copyright 2023-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import (
	"fmt"
	"strings"
	"sync"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/go-logr/logr"
)

// learnedDigests holds the digests learned from running servers, digest to version.
// Concurrent reconciles write it, so it is kept apart from constants.ImageDigests,
// which is compiled in and only ever read.
var learnedDigests sync.Map

// lookupDigest returns the version for a digest, compiled in or learned since start.
func lookupDigest(digest string) (string, bool) {
	if v, ok := constants.ImageDigests[digest]; ok {
		return v, true
	}

	if v, ok := learnedDigests.Load(digest); ok {
		return v.(string), true
	}

	return "", false
}

// Extracts the version part from an image tag.
func GetVersionTag(image string) string {
	parts := strings.Split(image, ":")

	lenParts := len(parts)
	if lenParts < 2 {
		return ""
	}

	return parts[lenParts-1]
}

// updates the internal map of image digests
// with the appropriate version for the given image.
// returns the version for SHA256 images, and a bool if it was updated.
func UpdateImageDigestMap(image string, poolsVersion string, log logr.Logger) (string, bool) {
	version := GetVersionTag(image)

	if !IsSHA256Version(version) { // only SHA256 is eligible.
		return "", false
	}

	if foundVer, found := lookupDigest(version); found {
		return foundVer, false
	}

	// apiVersion, err := NewVersion(poolsVersion)
	// if err != nil {
	// 	return "", false
	// }

	// If we don't have a version from the pools, try to find it in the env config map, otherwise trust the user provided version but don't add it to the digest map.
	if poolsVersion == "" {
		poolsVersion = GetVersionFromEnvConfigMap(version)
		if !VersionKnown(poolsVersion) {
			log.Info("Unable to find version for image", "image", image)
			return poolsVersion, false
		}
	}

	if !strings.HasPrefix(poolsVersion, "couchbase-") {
		poolsVersion = fmt.Sprintf("couchbase-%s", poolsVersion)
	}

	// Another reconcile may have learned it first; only the first store reports updated.
	if foundVer, loaded := learnedDigests.LoadOrStore(version, poolsVersion); loaded {
		return foundVer.(string), false
	}

	return poolsVersion, true
}
