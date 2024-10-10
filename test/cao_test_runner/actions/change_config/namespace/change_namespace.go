package changenamespace

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
	ErrDecodeNamespaceChangeConfig = errors.New("unable to decode NamespaceChangeConfig")
	ErrNoConfigFound               = errors.New("no config found for changing namespace")
	ErrNoAvailableNamespace        = errors.New("no such available namespaces")
)

type NamespaceChangeConfig struct {
	Description []string         `yaml:"description"`
	Namespace   string           `yaml:"namespace" caoCli:"required,context"`
	Validators  []map[string]any `yaml:"validators,omitempty"`
}

type ChangeNamespace struct {
	desc       string
	yamlConfig interface{}
}

func NewNamespaceChangeConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*NamespaceChangeConfig)
		if !ok {
			return nil, ErrDecodeNamespaceChangeConfig
		}

		return &ChangeNamespace{
			desc:       "change the current namespace",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *ChangeNamespace) Describe() string {
	return action.desc
}

func (action *ChangeNamespace) Do(ctx *context.Context, config interface{}) error {
	c, ok := action.yamlConfig.(*NamespaceChangeConfig)
	if !ok {
		return ErrNoConfigFound
	}

	logrus.Infof("Namespace change started")

	output, err := kubectl.GetNamespaces().Output()
	if err != nil {
		return fmt.Errorf("kubectl cannot fetch all namespaces: %w", err)
	}

	namespaces := strings.Split(output, "\n")

	if slices.Contains(namespaces, "namespace/"+c.Namespace) {
		ctx.WithID(context.NamespaceIDKey, c.Namespace)
	} else {
		return fmt.Errorf("kubectl cannot find namespace %s: %w", c.Namespace, ErrNoAvailableNamespace)
	}

	return nil
}

func (action *ChangeNamespace) Config() interface{} {
	return action.yamlConfig
}

func (action *ChangeNamespace) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*NamespaceChangeConfig)
	if !ok {
		return ErrNoConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
