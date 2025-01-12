package cmd

import (
	"errors"
	"fmt"
	"time"

	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/spf13/viper"
)

type RootConfig struct {
	Scenario             string
	ScenarioTags         []string
	TriggerLogCollection bool
	OutputPath           *fileutils.Directory
	KubectlPath          *fileutils.File
	KubeconfigPath       *fileutils.File
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

	cfg.KubectlPath = fileutils.NewFile(viper.GetString(kubectlPathKey))

	cfg.KubeconfigPath = fileutils.NewFile(viper.GetString(kubeconfigPathKey))

	return cfg, nil
}

func makeOutputPath() (*fileutils.Directory, error) {
	outputPath := viper.GetString(outputPathKey)

	if outputPath == "." {
		resultsDir := fileutils.NewDirectory("./results", 0777)

		if !resultsDir.IsDirectoryExists() {
			if err := resultsDir.CreateDirectory(); err != nil {
				return nil, fmt.Errorf("make output path: %w", err)
			}
		}

		outputPath = fmt.Sprintf("./results/results-%s", time.Now().Format("2006-01-02-15-04-05"))
	}

	dir := fileutils.NewDirectory(outputPath, 0777)
	if !dir.IsDirectoryExists() {
		if err := dir.CreateDirectory(); err != nil {
			return nil, fmt.Errorf("make output path: %w", err)
		}
	}

	return dir, nil
}
