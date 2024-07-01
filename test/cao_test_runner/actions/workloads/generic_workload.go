package workloads

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/kubectl"
	"github.com/sirupsen/logrus"
)

var (
	ErrConfigWorkload = errors.New("genericWorkloadConfig is nil")
	ErrWorkloadDecode = errors.New("unable to decode genericWorkloadConfig")
)

type GenericWorkloadConfig struct {
	Name               string `yaml:"name" caoCli:"required"`
	SpecPath           string `yaml:"specPath" caoCli:"required"`
	PreRunWait         int    `yaml:"preRunWait"`
	PostRunWait        int    `yaml:"postRunWait"`
	RunDuration        int    `yaml:"runDuration" caoCli:"required"`
	CheckJobCompletion bool   `yaml:"checkJobCompletion"`
}

func NewGenericWorkloadConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		var c *GenericWorkloadConfig
		c, ok := config.(*GenericWorkloadConfig)

		if !ok {
			return nil, ErrWorkloadDecode
		}

		return &GenericWorkload{
			desc:       "run generic workload to CB in K8S",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrConfigWorkload
}

type GenericWorkload struct {
	desc       string
	yamlConfig interface{}
}

func (g *GenericWorkload) Describe() string {
	return g.desc
}

func (g *GenericWorkload) Do(_ *context.Context, _ interface{}) error {
	workloadConfig, _ := g.yamlConfig.(*GenericWorkloadConfig)

	// Introduce a wait before starting to run the workload.
	if workloadConfig.PreRunWait != 0 {
		logrus.Infof("Waiting for %d minute(s) before starting workload: %s", workloadConfig.PreRunWait, workloadConfig.Name)
		time.Sleep(time.Duration(workloadConfig.PreRunWait) * time.Minute)
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Apply the YAML of the workload
	err = kubectl.ApplyFiles(path.Join(dir, workloadConfig.SpecPath)).InNamespace("default").ExecWithoutOutputCapture()
	if err != nil {
		logrus.Error("kubectl apply workload yaml:", err)
		return fmt.Errorf("kubectl apply workload yaml: %w", err)
	}

	logrus.Infof("Started workload: %s", workloadConfig.Name)

	// Execute the workload for duration = workloadConfig.RunDuration.
	if workloadConfig.RunDuration != 0 {
		logrus.Infof("Running `%s` for %d minute(s)", workloadConfig.Name, workloadConfig.RunDuration)

		// If workloadConfig.CheckJobCompletion is true, then the workload (job) is removed as soon as it is completed.
		// Else, the workload (job) will run for workloadConfig.RunDuration duration even if it has not been completed.
		if workloadConfig.CheckJobCompletion {
			// TODO: get the name of the job and then check if it has been completed or not.
			// checkJobCompletion()
			// will have to implement something like context with deadline. Since this function shall not be blocking
			logrus.Error("workloadConfig.CheckJobCompletion to be implemented")
		} else {
			time.Sleep(time.Duration(workloadConfig.RunDuration) * time.Minute)
		}
	} else {
		logrus.Infof("Running `%s`", workloadConfig.Name)
		return nil
	}

	// After the workload duration is over, we delete the workload
	err = kubectl.DeleteFromFiles(path.Join(dir, workloadConfig.SpecPath)).InNamespace("default").ExecWithoutOutputCapture()
	if err != nil {
		logrus.Error("kubectl delete: ", err)
		return fmt.Errorf("kubectl delete: %w", err)
	}

	logrus.Infof("Deleted workload: %s", workloadConfig.Name)

	// Introduce a wait after the workload gets completed.
	if workloadConfig.PostRunWait != 0 {
		logrus.Infof("Waiting for %d minute(s) after completing workload: %s", workloadConfig.PostRunWait, workloadConfig.Name)
		time.Sleep(time.Duration(workloadConfig.PostRunWait) * time.Minute)
	}

	return nil
}

func (g *GenericWorkload) Config() interface{} {
	return g.yamlConfig
}

func (g *GenericWorkload) Checks(_ *context.Context, _ interface{}, _ string) error {
	return nil
}
