package namespacesetup

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrUnableToDecodeSetupNamespaceConfig = errors.New("unable to decode SetupNamespaceConfig in Do()")
	ErrNoSetupNamespaceConfigFound        = errors.New("no config found for creating namespace")
	ErrIllegalImagePullPolicy             = errors.New("illegal image pull policy")
	ErrIllegalScope                       = errors.New("illegal scope")
	ErrCAOBinaryPathInvalid               = errors.New("cao binary path does not exist")
)

type SetupNamespaceConfig struct {
	Description []string         `yaml:"description"`
	Namespace   string           `yaml:"namespace" caoCli:"context" env:"NAMESPACE"`
	Validators  []map[string]any `yaml:"validators,omitempty"`
}

func NewSetupNamespaceConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*SetupNamespaceConfig)
		if !ok {
			return nil, ErrUnableToDecodeSetupNamespaceConfig
		}

		return &SetupNamespace{
			desc:       "Setup and create namespace",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoSetupNamespaceConfigFound
}

type SetupNamespace struct {
	desc       string
	yamlConfig interface{}
}

func (action *SetupNamespace) Describe() string {
	return action.desc
}

func (action *SetupNamespace) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*SetupNamespaceConfig)
	if !ok {
		return ErrNoSetupNamespaceConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *SetupNamespace) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*SetupNamespaceConfig)
	if !ok {
		return ErrNoSetupNamespaceConfigFound
	}

	logrus.Infof("Namespace creation started")

	if err := kubectl.CreateNamespace(c.Namespace).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to create namespace %s: %w", c.Namespace, err)
	}

	ctx.WithID(context.NamespaceIDKey, c.Namespace)

	return nil
}

func (action *SetupNamespace) Config() interface{} {
	return action.yamlConfig
}
