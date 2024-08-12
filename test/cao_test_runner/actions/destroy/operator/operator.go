package destoryoperator

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/shell"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
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

const (
	scopeArgs string = "--scope"
)

type OperatorConfig struct {
	CAOBinaryPath string           `yaml:"caoBinaryPath" caoCli:"required"`
	Scope         ScopeType        `yaml:"scope"`
	Validators    []map[string]any `yaml:"validators,omitempty"`
}

func NewDeleteOperatorConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*OperatorConfig)
		if !ok {
			return nil, ErrUnableToDecodeOperatorConfig
		}

		switch c.Scope {
		case BlankScope:
			c.Scope = DefaultScope
		case Namespace, Cluster:
			// No-op
		default:
			return nil, ErrIllegalScope
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

func (action *DeleteOperator) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrNoOperatorConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func generatOperatorArguments(c *OperatorConfig) []string {
	commandArgs := []string{"delete", "operator"}

	argMap := map[string]string{
		scopeArgs: string(c.Scope),
	}

	for argument, value := range argMap {
		commandArgs = append(commandArgs, argument, value)
	}

	return commandArgs
}

func (action *DeleteOperator) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrNoOperatorConfigFound
	}

	logrus.Infof("Operator pod deletion started")

	commandArgs := generatOperatorArguments(c)

	logrus.Info("cao delete operator at :", time.Now().Format(time.RFC3339))

	err := shell.RunWithoutOutputCapture(c.CAOBinaryPath, commandArgs...)
	if err != nil {
		return fmt.Errorf("failed to execute cao delete operator: %w", err)
	}

	return nil
}

func (action *DeleteOperator) Config() interface{} {
	return action.yamlConfig
}
