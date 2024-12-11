package destoryoperator

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrUnableToDecodeOperatorConfig = errors.New("unable to decode OperatorConfig in Do()")
	ErrNoOperatorConfigFound        = errors.New("no config found for deleting operator pod")
	ErrIllegalScope                 = errors.New("illegal scope")
	ErrCAOBinaryPathInvalid         = errors.New("cao binary path does not exist")
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
	Description   []string         `yaml:"description"`
	CAOBinaryPath string           `yaml:"caoBinaryPath" caoCli:"required,context" env:"CAO_BINARY_PATH"`
	Scope         ScopeType        `yaml:"scope"`
	Validators    []map[string]any `yaml:"validators,omitempty"`
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

func (action *DeleteOperator) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrNoOperatorConfigFound
	}

	switch c.Scope {
	case BlankScope:
		c.Scope = DefaultScope
	case Namespace, Cluster:
		// No-op
	default:
		return ErrIllegalScope
	}

	if ok = fileutils.NewFile(c.CAOBinaryPath).IsFileExists(); !ok {
		return fmt.Errorf("cao binary path %s does not exist: %w", c.CAOBinaryPath, ErrCAOBinaryPathInvalid)
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *DeleteOperator) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrNoOperatorConfigFound
	}

	logrus.Infof("Operator pod deletion started")

	logrus.Info("cao delete operator at :", time.Now().Format(time.RFC3339))

	cao.WithBinaryPath(c.CAOBinaryPath)

	if err := cao.DeleteOperator(string(c.Scope)).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("failed to execute cao delete operator: %w", err)
	}

	return nil
}

func (action *DeleteOperator) Config() interface{} {
	return action.yamlConfig
}
