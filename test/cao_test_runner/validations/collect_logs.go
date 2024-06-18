package validations

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
)

type CollectLogs struct {
	State        string `yaml:"state"`
	CBServerLogs bool   `yaml:"CBServerLogs" caoCli:"required"`
	OperatorLogs bool   `yaml:"operatorLogs" caoCli:"required"`
}

func (c *CollectLogs) Run(_ *context.Context) error {
	// TODO : implement mechanism to collect CB and Operator logs
	return nil
}

func (c *CollectLogs) GetState() string {
	return c.State
}
