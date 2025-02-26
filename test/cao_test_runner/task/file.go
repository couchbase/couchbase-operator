package task

import (
	"context"
	"os"
	"strings"
)

const (
	defaultTimeoutMins = 10080 // 7 Days = 7 * 24 * 60
)

type FilePath string

type FileReaderFunc func(p FilePath) ([]byte, error)

func FileRead(p FilePath) ([]byte, error) {
	b, err := os.ReadFile(string(p))
	if err != nil {
		return nil, err
	}

	return b, nil
}

type ScenarioRunner struct {
	Tree *Tree
	Ctx  context.Context
}

type taskIn struct {
	Trees         []taskIn               `yaml:"trees,omitempty"`
	Action        string                 `yaml:"action,omitempty"`
	Iterations    int                    `yaml:"iterations,omitempty"`
	MaxConcurrent int                    `yaml:"maxConcurrent,omitempty"`
	TimeoutInMins float64                `yaml:"timeoutInMins,omitempty"`
	Blocking      bool                   `yaml:"blocking,omitempty"`
	Config        map[string]interface{} `yaml:"config,omitempty"`
	ScenarioName  string                 `yaml:"-"`
}

type multiScenarioIn struct {
	TimeoutInMins float64 `yaml:"timeoutInMins,omitempty"`
	// Scenarios holds the sets of scenarios that are to be executed
	Scenarios [][]FilePath `yaml:"scenarios"`
}

func scenarioNameFromPath(path FilePath) string {
	// ./scenarios/upgrade/delta_recovery/delta_recovery.yaml becomes scenarios/upgrade/delta_recovery/delta_recovery
	return strings.TrimSuffix(strings.TrimPrefix(string(path), "./"), ".yaml")
}
