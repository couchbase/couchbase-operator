package cmd

import (
	"errors"
	"fmt"

	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/spf13/viper"
)

type RootConfig struct {
	Scenario             string
	ScenarioTags         []string
	TriggerLogCollection bool
	OutputPath           string
}

var (
	ErrEmptyScenario = errors.New("empty scenario")
)

func buildRootConfig() (*RootConfig, error) {
	cfg := &RootConfig{}
	cfg.ScenarioTags = viper.GetStringSlice(scenarioTags)

	if len(cfg.ScenarioTags) == 0 {
		cfg.ScenarioTags = append(cfg.ScenarioTags, "ALL")
	}

	cfg.Scenario = viper.GetString(scenarioKey)

	if cfg.Scenario == "" {
		return nil, ErrEmptyScenario
	}

	cfg.TriggerLogCollection = viper.GetBool(triggerLogCollectionKey)

	var err error
	cfg.OutputPath, err = makeOutputPath()
	if err != nil {
		return nil, fmt.Errorf("build root config: %w", err)
	}

	return cfg, nil
}

func makeOutputPath() (string, error) {
	outputPath := viper.GetString(outputPathKey)

	dir := fileutils.NewDirectory(outputPath, 0777)
	if !dir.IsDirectoryExists() {
		if err := dir.CreateDirectory(); err != nil {
			return "", fmt.Errorf("make output path: %w", err)
		}
	}

	return outputPath, nil
}
