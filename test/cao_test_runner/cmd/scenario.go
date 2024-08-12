package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/task"
	"github.com/spf13/cobra"
)

var (
	ErrFound = errors.New("")
)

func newScenarioCmd() *cobra.Command {
	scenarioCmd := &cobra.Command{
		Use:   "scenario",
		Short: "Run one or more actions specified in a scenario file",
		Long:  "Given a filepath to a scenario json file, this command loads then executes the task tree defined",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunScenario()
		},
	}
	addStringFlag(scenarioCmd, scenarioKey, "f", "", "Scenario YAML file path")
	addStringFlag(scenarioCmd, outputPathKey, "o", ".", "CSV output path")
	addStringFlag(scenarioCmd, scenarioTags, "", "", "Run only scenario with tags")
	addBoolFlag(scenarioCmd, triggerLogCollectionKey, "l", false, "trigger log collection")

	return scenarioCmd
}

func RunScenario() error {
	rootCfg, err := buildConfigurator()
	if err != nil {
		return err
	}

	f := task.FilePath(rootCfg.Scenario)

	errs := task.RunScenario(f, task.FileRead)

	if len(errs) != 0 {
		var errorStr []string
		for _, err := range errs {
			errorStr = append(errorStr, err.Error())
		}

		return fmt.Errorf(" %w: %s", ErrFound, strings.Join(errorStr, " "))
	}

	return nil
}
