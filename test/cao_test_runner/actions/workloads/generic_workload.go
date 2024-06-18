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
	Name      string `yaml:"name" caoCli:"required"`
	SpecPath  string `yaml:"specPath" caoCli:"required"`
	PreSleep  int    `yaml:"preSleep"`
	PostSleep int    `yaml:"postSleep"`
}

func NewGenericWorkloadConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		var c *GenericWorkloadConfig
		c, ok := config.(*GenericWorkloadConfig)

		if !ok {
			return nil, ErrWorkloadDecode
		}

		return &GenericWorkload{
			desc:       "generic workload to CB in K8S",
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
	c, _ := g.yamlConfig.(*GenericWorkloadConfig)

	if c.PreSleep != 0 {
		logrus.Infof("Started Pre Sleep before %s for %d minute", c.Name, c.PreSleep)
		time.Sleep(time.Duration(c.PreSleep) * time.Minute)
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	err = kubectl.ApplyFiles(path.Join(dir, c.SpecPath)).InNamespace("default").ExecWithoutOutputCapture()
	if err != nil {
		logrus.Error("kubectl apply:", err)
		return fmt.Errorf("kubectl apply: %w", err)
	}

	logrus.Infof("Started  %s", c.Name)

	if c.PostSleep != 0 {
		logrus.Infof("Started Post Sleep after %s for %d minute", c.Name, c.PostSleep)
		time.Sleep(time.Duration(c.PostSleep) * time.Minute)
	}

	return nil
}

func (g *GenericWorkload) Config() interface{} {
	return g.yamlConfig
}

func (g *GenericWorkload) Checks(_ *context.Context, _ interface{}, _ string) error {
	return nil
}
