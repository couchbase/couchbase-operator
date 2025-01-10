package cmd

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
)

const (
	scenarioKey             = "scenario"
	outputPathKey           = "output"
	triggerLogCollectionKey = "triggerLogCollection"
	scenarioTags            = "tags"
)

func buildConfigurator() (*RootConfig, error) {
	rootCfg, err := buildRootConfig()
	if err != nil {
		return nil, err
	}

	return rootCfg, err
}

func buildTestAssets(resultsDirectory *fileutils.Directory) (*assets.TestAssets, error) {
	testAssets := assets.NewTestAssets()

	testAssets.SetResultsDirectory(resultsDirectory)

	return testAssets, nil
}
