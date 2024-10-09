package assets

type TestAssets struct {
	resultsDirectory string
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
	return &ts.resultsDirectory
}

func (ts *TestAssets) SetResultsDirectory(resultsDirectory string) {
	ts.resultsDirectory = resultsDirectory
}
