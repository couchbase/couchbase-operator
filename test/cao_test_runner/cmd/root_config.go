package cmd

import (
	"errors"

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
	cfg.OutputPath = viper.GetString(outputPathKey)

	return cfg, nil
}
