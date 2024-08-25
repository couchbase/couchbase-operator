package destroykubernetes

import (
	"errors"
	"fmt"

	installutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

type EnvironmentType string
type ProviderType string

const (
	Kind        EnvironmentType = "kind"
	Cloud       EnvironmentType = "cloud"
	AWS         ProviderType    = "aws"
	Azure       ProviderType    = "azure"
	GoogleCloud ProviderType    = "googleCloud"
)

var (
	ErrDecodeKubernetesConfig = errors.New("unable to decode KubernetesDestroyConfig")
	ErrNoConfigFound          = errors.New("no config found for deleting kubernetes cluster")
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalEnvironment     = errors.New("illegal environment")
	ErrIllegalConfiguration   = errors.New("illegal configuration")
)

type KubernetesDestroyConfig struct {
	Description []string                  `yaml:"description"`
	ClusterName string                    `yaml:"clusterName" caoCli:"required"`
	Platform    installutils.PlatformType `yaml:"platform" caoCli:"required"`
	Environment EnvironmentType           `yaml:"environment" caoCli:"required"`
	Provider    ProviderType              `yaml:"provider"`
	EKSRegion   string                    `yaml:"eksRegion"`
	Validators  []map[string]any          `yaml:"validators,omitempty"`
}

type DestroyKubernetes struct {
	desc       string
	yamlConfig interface{}
}

func NewKubernetesConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*KubernetesDestroyConfig)
		if !ok {
			return nil, ErrDecodeKubernetesConfig
		}

		switch c.Platform {
		case installutils.Kubernetes, installutils.Openshift:
			// No-op
		default:
			return nil, ErrIllegalPlatform
		}

		switch c.Environment {
		case Kind, Cloud:
			// No-op
		default:
			return nil, ErrIllegalEnvironment
		}

		if c.Environment == Kind && c.Platform == installutils.Openshift {
			return nil, ErrIllegalConfiguration
		}

		return &DestroyKubernetes{
			desc:       "deleting the kubernetes cluster and environment",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *DestroyKubernetes) Describe() string {
	return action.desc
}

func (action *DestroyKubernetes) Do(ctx *context.Context, config interface{}) error {
	c, ok := action.yamlConfig.(*KubernetesDestroyConfig)
	if !ok {
		return ErrNoConfigFound
	}

	logrus.Infof("Kubernetes Destroy started")

	deleteClusterUtil, err := NewDeleteClusterUtil(c)
	if err != nil {
		return err
	}

	ctxContext := ctx.Context()

	if err = deleteClusterUtil.ValidateParams(&ctxContext); err != nil {
		return err
	}

	if err = deleteClusterUtil.DeleteCluster(&ctxContext); err != nil {
		return err
	}

	return nil
}

func (action *DestroyKubernetes) Config() interface{} {
	return action.yamlConfig
}

func (action *DestroyKubernetes) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*KubernetesDestroyConfig)
	if !ok {
		return ErrNoConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
