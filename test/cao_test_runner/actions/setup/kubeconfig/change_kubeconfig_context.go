package kubeconfigsetup

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeKubeConfigContextSetupConfig = errors.New("unable to decode KubeConfigContextSetupConfig")
	ErrNoConfigFound                      = errors.New("no config found for changing kubeconfig context")
	ErrNoAvailableContexts                = errors.New("no such available contexts")
)

type KubeConfigContextSetupConfig struct {
	Description []string         `yaml:"description"`
	K8sContext  string           `yaml:"k8sContext" caoCli:"required"`
	Validators  []map[string]any `yaml:"validators,omitempty"`
}

type ChangeKubeConfigContext struct {
	desc       string
	yamlConfig interface{}
}

func contains(array []string, str string) bool {
	for _, item := range array {
		if item == str {
			return true
		}
	}

	return false
}

func NewKubernetesSetupConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*KubeConfigContextSetupConfig)
		if !ok {
			return nil, ErrDecodeKubeConfigContextSetupConfig
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

func (action *ChangeKubeConfigContext) Do(ctx *context.Context, config interface{}) error {
	c, ok := action.yamlConfig.(*KubeConfigContextSetupConfig)
	if !ok {
		return ErrNoConfigFound
	}

	logrus.Infof("Kubeconfig context change started")

	currK8sContext, err := kubectl.GetCurrentContext().Output()
	if err != nil {
		return fmt.Errorf("kubectl cannot fetch current context: %w", err)
	}

	if currK8sContext != c.K8sContext {
		output, err := kubectl.GetContexts().Output()
		if err != nil {
			return fmt.Errorf("kubectl cannot fetch all available contexts: %w", err)
		}

		availableContexts := strings.Split(output, "\n")

		if contains(availableContexts, c.K8sContext) {
			err = kubectl.UseContext(c.K8sContext).ExecWithoutOutputCapture()
			if err != nil {
				return fmt.Errorf("kubectl cannot fetch current context: %w", err)
			}
		} else {
			return fmt.Errorf("cannot change context to %s: %w", c.K8sContext, ErrNoAvailableContexts)
		}
	}

	ctx.WithID(context.K8sContextKey, c.K8sContext)

	return nil
}

func (action *ChangeKubeConfigContext) Config() interface{} {
	return action.yamlConfig
}

func (action *ChangeKubeConfigContext) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*KubeConfigContextSetupConfig)
	if !ok {
		return ErrNoConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
