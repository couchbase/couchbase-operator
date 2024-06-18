package workloads

import (
	"errors"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
)

var (
	ErrConfigSleep   = errors.New("sleep config is nil")
	ErrSleepDecode   = errors.New("unable to decode sleepConfig")
	DefaultSleepTime = 5
)

type SleepActionConfig struct {
	Time int `yaml:"time" `
}

func NewSleepActionConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		var c *SleepActionConfig
		c, ok := config.(*SleepActionConfig)

		if !ok {
			return nil, ErrSleepDecode
		}

		if c.Time == 0 {
			c.Time = DefaultSleepTime
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

func (s *SleepAction) Do(_ *context.Context, _ interface{}) error {
	c, _ := s.yamlConfig.(*SleepActionConfig)
	logrus.Infof("Sleeping for %d minutes", c.Time)
	time.Sleep(time.Duration(c.Time) * time.Minute)

	return nil
}

func (s *SleepAction) Config() interface{} {
	return s.yamlConfig
}

func (s *SleepAction) Checks(_ *context.Context, _ interface{}, _ string) error {
	return nil
}
