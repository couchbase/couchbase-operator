package cmd

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
)

const (
	scenarioKey             = "scenario"
	kubectlPathKey          = "kubectlPath"
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

func buildTestAssets(resultsDirectory *fileutils.Directory, kubectlPath *fileutils.File) (*assets.TestAssets, error) {
	testAssets := assets.NewTestAssets()

	if err := testAssets.SetResultsDirectory(resultsDirectory); err != nil {
		return nil, fmt.Errorf("build test assets: %w", err)
	}

	if err := testAssets.SetKubectlPath(kubectlPath); err != nil {
		return nil, fmt.Errorf("build test assets: %w", err)
	}

	// Populate test assets with existing data (current state of clusters)
	if err := testAssets.PopulateTestAssets(); err != nil {
		return nil, fmt.Errorf("build test assets: %w", err)
	}

	return testAssets, nil
}
