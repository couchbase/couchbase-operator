package assets

import (
	"sync"

	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
)

type TestAssets struct {
	resultsDirectory *fileutils.Directory
	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewTestAssets() *TestAssets {
	return &TestAssets{}
}

type TestAssetGetter interface {
	GetResultsDirectory() *fileutils.Directory
}

type TestAssetGetterSetter interface {
	GetResultsDirectory() *fileutils.Directory
	SetResultsDirectory(resultsDirectory *fileutils.Directory)
}

func (ts *TestAssets) GetResultsDirectory() *fileutils.Directory {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.resultsDirectory
}

func (ts *TestAssets) SetResultsDirectory(resultsDirectory *fileutils.Directory) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.resultsDirectory = resultsDirectory
}

// This function is used to populate test assets in the beginning with existing cluster details.
func (ts *TestAssets) PopulateTestAssets() {

}
