package destroyadmissioncontroller

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
	ErrUnableToDecodeAdmissionConfig = errors.New("unable to decode AdmissionControllerConfig in Do()")
	ErrNoAdmissionConfigFound        = errors.New("no config found for deleting admission pod")
	ErrIllegalScope                  = errors.New("illegal scope")
)

type ScopeType string

const (
	Namespace  ScopeType = "namespace"
	Cluster    ScopeType = "cluster"
	BlankScope ScopeType = ""
)

const (
	DefaultScope ScopeType = Cluster
)

const (
	scopeArgs string = "--scope"
)

type AdmissionControllerConfig struct {
	CAOBinaryPath string           `yaml:"caoBinaryPath" caoCli:"required"`
	Scope         ScopeType        `yaml:"scope"`
	Validators    []map[string]any `yaml:"validators,omitempty"`
}

func NewDestroyAdmissionControllerConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*AdmissionControllerConfig)
		if !ok {
			return nil, ErrUnableToDecodeAdmissionConfig
		}

		switch c.Scope {
		case BlankScope:
			c.Scope = DefaultScope
		case Namespace, Cluster:
			// No-op
		default:
			return nil, ErrIllegalScope
		}

		return &DeleteAdmissionController{
			desc:       "Delete admission controller pod",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoAdmissionConfigFound
}

type DeleteAdmissionController struct {
	desc       string
	yamlConfig interface{}
}

func (action *DeleteAdmissionController) Describe() string {
	return action.desc
}

func (action *DeleteAdmissionController) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrNoAdmissionConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func generateAdmissionArguments(c *AdmissionControllerConfig) []string {
	commandArgs := []string{"delete", "admission"}

	argMap := map[string]string{
		scopeArgs: string(c.Scope),
	}

	for argument, value := range argMap {
		commandArgs = append(commandArgs, argument, value)
	}

	return commandArgs
}

func (action *DeleteAdmissionController) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrNoAdmissionConfigFound
	}

	logrus.Infof("Admission Controller pod deletion started")

	commandArgs := generateAdmissionArguments(c)

	logrus.Info("cao delete admission at :", time.Now().Format(time.RFC3339))

	err := shell.RunWithoutOutputCapture(c.CAOBinaryPath, commandArgs...)
	if err != nil {
		return fmt.Errorf("failed to execute cao delete admission: %w", err)
	}

	return nil
}

func (action *DeleteAdmissionController) Config() interface{} {
	return action.yamlConfig
}
