/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package k8sutil

import (
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

func (l volumeMountList) getVolumeMount(t *testing.T, name volumeMountName) volumeMount {
	for _, mapping := range l {
		if mapping.name == name {
			return mapping
		}
	}

	t.Fatalf("missing %v claim", name)

	return volumeMount{}
}

// Test paths to persist method returns appropriate claims
// for known volume mounts.
func TestPathsToPersist(t *testing.T) {
	claimName := "couchbase"
	mounts := &couchbasev2.VolumeMounts{
		DefaultClaim: claimName,
		DataClaim:    claimName,
		IndexClaim:   claimName,
	}

	member := couchbaseutil.NewMember("namespace", "cluster", "name", "version", "class", false)

	paths, err := getPathsToPersist(member, mounts)
	if err != nil {
		t.Fatal(err)
	}

	defaultClaim := paths.getVolumeMount(t, defaultVolumeMount)
	if defaultClaim.persistentVolumeClaimTemplateName != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, defaultClaim, claimName)
	}

	dataClaim := paths.getVolumeMount(t, dataVolumeMount)
	if dataClaim.persistentVolumeClaimTemplateName != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, dataClaim, claimName)
	}

	indexClaim := paths.getVolumeMount(t, indexVolumeMount)
	if indexClaim.persistentVolumeClaimTemplateName != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, indexClaim, claimName)
	}
}

// Test that expected path is returned for the specified volume mount.
func TestPathsForVolumeMountName(t *testing.T) {
	path := pathForVolumeMountName(defaultVolumeMount)
	if path != couchbaseVolumeDefaultConfigDir {
		t.Fatalf(`invalid path for default volume: "%s", expected: "%s"`, path, couchbaseVolumeDefaultConfigDir)
	}

	path = pathForVolumeMountName(dataVolumeMount)
	if path != CouchbaseVolumeMountDataDir {
		t.Fatalf(`invalid path for data volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountDataDir)
	}

	path = pathForVolumeMountName(indexVolumeMount)
	if path != CouchbaseVolumeMountIndexDir {
		t.Fatalf(`invalid path for index volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountIndexDir)
	}
}
