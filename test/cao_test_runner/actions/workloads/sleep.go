package workloads

import (
	"errors"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
)

var (
	ErrConfigSleep   = errors.New("sleep config is nil")
	ErrSleepDecode   = errors.New("unable to decode sleepConfig")
	DefaultSleepTime = 30 * time.Second
)

type SleepActionConfig struct {
	Duration time.Duration `yaml:"duration" `
}

func NewSleepActionConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		var c *SleepActionConfig
		c, ok := config.(*SleepActionConfig)

		if !ok {
			return nil, ErrSleepDecode
		}

		return &SleepAction{
			desc:       "sleep execution to delay the child actions",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrConfigSleep
}

type SleepAction struct {
	desc       string
	yamlConfig interface{}
}

func (s *SleepAction) Describe() string {
	return s.desc
}

func (s *SleepAction) Do(_ *context.Context, testAssets assets.TestAssetGetter) error {
	if s.yamlConfig == nil {
		return ErrConfigSleep
	}

	c, ok := s.yamlConfig.(*SleepActionConfig)
	if !ok {
		return ErrSleepDecode
	}

	logrus.Infof("Sleeping for %s", c.Duration.String())

	time.Sleep(c.Duration)

	logrus.Infof("Sleep complete.")

	return nil
}

func (s *SleepAction) Config() interface{} {
	return s.yamlConfig
}

func (s *SleepAction) CheckConfig() error {
	if s.yamlConfig == nil {
		return ErrConfigSleep
	}

	c, ok := s.yamlConfig.(*SleepActionConfig)
	if !ok {
		return ErrSleepDecode
	}

	if c.Duration == 0 {
		c.Duration = DefaultSleepTime
	}

	return nil
}
