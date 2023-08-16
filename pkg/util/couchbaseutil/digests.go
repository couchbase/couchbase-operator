package couchbaseutil

import (
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

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
func UpdateImageDigestMap(image string, poolsVersion string) (string, bool) {
	version := GetVersionTag(image)

	if !IsSHA256Version(version) { // only SHA256 is eligible.
		return "", false
	}

	if foundVer, found := constants.ImageDigests[version]; found {
		return foundVer, false
	}

	// apiVersion, err := NewVersion(poolsVersion)
	// if err != nil {
	// 	return "", false
	// }

	newImage := fmt.Sprintf("couchbase-%s", poolsVersion)
	constants.ImageDigests[version] = newImage

	return newImage, true
}
