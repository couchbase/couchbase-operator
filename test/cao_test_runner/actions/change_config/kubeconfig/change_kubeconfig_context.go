package changekubeconfig

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeKubeConfigContextChangeConfig = errors.New("unable to decode KubeConfigContextChangeConfig")
	ErrNoConfigFound                       = errors.New("no config found for changing kubeconfig context")
	ErrNoAvailableContexts                 = errors.New("no such available contexts")
)

type KubeConfigContextChangeConfig struct {
	Description []string         `yaml:"description"`
	K8SContext  string           `yaml:"k8sContext" caoCli:"required,context"`
	Validators  []map[string]any `yaml:"validators,omitempty"`
}

type ChangeKubeConfigContext struct {
	desc       string
	yamlConfig interface{}
}

func NewKubeConfigContextChangeConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*KubeConfigContextChangeConfig)
		if !ok {
			return nil, ErrDecodeKubeConfigContextChangeConfig
		}

		return &ChangeKubeConfigContext{
			desc:       "change the kubeconfig context",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *ChangeKubeConfigContext) Describe() string {
	return action.desc
}

func (action *ChangeKubeConfigContext) Do(ctx *context.Context) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubeConfigContextChangeConfig)
	if !ok {
		return ErrNoConfigFound
	}

	logrus.Infof("Kubeconfig context change started")

	currK8sContext, err := kubectl.GetCurrentContext().Output()
	if err != nil {
		return fmt.Errorf("kubectl cannot fetch current context: %w", err)
	}

	if currK8sContext != c.K8SContext {
		output, err := kubectl.GetContexts().Output()
		if err != nil {
			return fmt.Errorf("kubectl cannot fetch all available contexts: %w", err)
		}

		availableContexts := strings.Split(output, "\n")

		if slices.Contains(availableContexts, c.K8SContext) {
			err = kubectl.UseContext(c.K8SContext).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("kubectl cannot fetch current context: %w", err)
			}
		} else {
			return fmt.Errorf("cannot change context to %s: %w", c.K8SContext, ErrNoAvailableContexts)
		}
	}

	ctx.WithID(context.K8sContextKey, c.K8SContext)

	return nil
}

func (action *ChangeKubeConfigContext) Config() interface{} {
	return action.yamlConfig
}

func (action *ChangeKubeConfigContext) RunValidators(ctx *context.Context, state string) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubeConfigContextChangeConfig)
	if !ok {
		return ErrNoConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *ChangeKubeConfigContext) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	_, ok := action.yamlConfig.(*KubeConfigContextChangeConfig)
	if !ok {
		return ErrNoConfigFound
	}

	return nil
}
