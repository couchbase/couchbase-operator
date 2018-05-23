package k8sutil

import (
	"testing"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
)

// Test paths to persist method returns appropriate claims
// for known volume mounts
func TestPathsToPersist(t *testing.T) {
	claimName := "couchbase"
	mounts := &cbapi.VolumeMounts{
		DefaultClaim: &claimName,
		DataClaim:    &claimName,
		IndexClaim:   &claimName,
	}

	paths := getPathsToPersist(mounts)
	defaultClaim, ok := paths[cbapi.DefaultVolumeMount]
	if !ok {
		t.Fatalf("missing default claim")
	}
	if defaultClaim != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, defaultClaim, claimName)
	}
	dataClaim, ok := paths[cbapi.DataVolumeMount]
	if !ok {
		t.Fatalf("missing data claim")
	}
	if dataClaim != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, dataClaim, claimName)
	}
	indexClaim, ok := paths[cbapi.IndexVolumeMount]
	if !ok {
		t.Fatalf("missing index claim")
	}
	if indexClaim != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, indexClaim, claimName)
	}
}

// Test that expected path is returned for the specified volume mount
func TestPathsForVolumeMountName(t *testing.T) {

	path := pathForVolumeMountName(cbapi.DefaultVolumeMount)
	if path != couchbaseVolumeDefaultConfigDir {
		t.Fatalf(`invalid path for default volume: "%s", expected: "%s"`, path, couchbaseVolumeDefaultConfigDir)
	}
	path = pathForVolumeMountName(cbapi.DataVolumeMount)
	if path != CouchbaseVolumeMountDataDir {
		t.Fatalf(`invalid path for data volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountDataDir)
	}
	path = pathForVolumeMountName(cbapi.IndexVolumeMount)
	if path != CouchbaseVolumeMountIndexDir {
		t.Fatalf(`invalid path for index volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountIndexDir)
	}
}
