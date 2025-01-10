package assets

import "sync"

type TestAssets struct {
	resultsDirectory string
	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewTestAssets() *TestAssets {
	return &TestAssets{}
}

type TestAssetGetter interface {
	GetResultsDirectory() *string
}

type TestAssetGetterSetter interface {
	GetResultsDirectory() *string
	SetResultsDirectory(resultsDirectory string)
}

func (ts *TestAssets) GetResultsDirectory() *string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return &ts.resultsDirectory
}

func (ts *TestAssets) SetResultsDirectory(resultsDirectory string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.resultsDirectory = resultsDirectory
}
