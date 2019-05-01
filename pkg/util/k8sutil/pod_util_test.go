package k8sutil

import (
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
)

// Test paths to persist method returns appropriate claims
// for known volume mounts
func TestPathsToPersist(t *testing.T) {
	claimName := "couchbase"
	mounts := &couchbasev2.VolumeMounts{
		DefaultClaim: claimName,
		DataClaim:    claimName,
		IndexClaim:   claimName,
	}

	paths, err := getPathsToPersist(mounts)
	if err != nil {
		t.Fatal(err)
	}

	defaultClaim, ok := paths[couchbasev2.DefaultVolumeMount]
	if !ok {
		t.Fatalf("missing default claim")
	}
	if defaultClaim != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, defaultClaim, claimName)
	}
	dataClaim, ok := paths[couchbasev2.DataVolumeMount]
	if !ok {
		t.Fatalf("missing data claim")
	}
	if dataClaim != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, dataClaim, claimName)
	}
	indexClaim, ok := paths[couchbasev2.IndexVolumeMount]
	if !ok {
		t.Fatalf("missing index claim")
	}
	if indexClaim != claimName {
		t.Fatalf(`expected claim name "%s", got "%s"`, indexClaim, claimName)
	}
}

// Test that expected path is returned for the specified volume mount
func TestPathsForVolumeMountName(t *testing.T) {

	path := pathForVolumeMountName(couchbasev2.DefaultVolumeMount)
	if path != couchbaseVolumeDefaultConfigDir {
		t.Fatalf(`invalid path for default volume: "%s", expected: "%s"`, path, couchbaseVolumeDefaultConfigDir)
	}
	path = pathForVolumeMountName(couchbasev2.DataVolumeMount)
	if path != CouchbaseVolumeMountDataDir {
		t.Fatalf(`invalid path for data volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountDataDir)
	}
	path = pathForVolumeMountName(couchbasev2.IndexVolumeMount)
	if path != CouchbaseVolumeMountIndexDir {
		t.Fatalf(`invalid path for index volume: "%s", expected: "%s"`, path, CouchbaseVolumeMountIndexDir)
	}
}
