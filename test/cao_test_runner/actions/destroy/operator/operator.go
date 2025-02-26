package destoryoperator

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	"github.com/sirupsen/logrus"
)

var (
	ErrUnableToDecodeOperatorConfig = errors.New("unable to decode OperatorConfig in Do()")
	ErrNoOperatorConfigFound        = errors.New("no config found for deleting operator pod")
	ErrIllegalScope                 = errors.New("illegal scope")
)

type ScopeType string

const (
	Namespace  ScopeType = "namespace"
	Cluster    ScopeType = "cluster"
	BlankScope ScopeType = ""
)

const (
	DefaultScope ScopeType = Namespace
)

type OperatorConfig struct {
	Description []string  `yaml:"description"`
	Scope       ScopeType `yaml:"scope"`
}

func NewDeleteOperatorConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*OperatorConfig)
		if !ok {
			return nil, ErrUnableToDecodeOperatorConfig
		}

		return &DeleteOperator{
			desc:       "Delete operator pod",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoOperatorConfigFound
}

type DeleteOperator struct {
	desc       string
	yamlConfig interface{}
}

func (action *DeleteOperator) Describe() string {
	return action.desc
}

func (action *DeleteOperator) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoOperatorConfigFound
	}

	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrUnableToDecodeOperatorConfig
	}

	switch c.Scope {
	case BlankScope:
		c.Scope = DefaultScope
	case Namespace, Cluster:
		// No-op
	default:
		return ErrIllegalScope
	}

	return nil
}

func (action *DeleteOperator) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if action.yamlConfig == nil {
		return ErrNoOperatorConfigFound
	}

	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrUnableToDecodeOperatorConfig
	}

	logrus.Infof("Operator pod deletion started")

	logrus.Info("cao delete operator at :", time.Now().Format(time.RFC3339))

	if err := cao.DeleteOperator(string(c.Scope)).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("failed to execute cao delete operator: %w", err)
	}

	return nil
}

func (action *DeleteOperator) Config() interface{} {
	return action.yamlConfig
}
