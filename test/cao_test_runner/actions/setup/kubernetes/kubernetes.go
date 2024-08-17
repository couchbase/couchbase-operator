package setupkubernetes

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

const (
	Kind  EnvironmentType = "kind"
	Cloud EnvironmentType = "cloud"
)

var (
	ErrDecodeKubernetesConfig = errors.New("unable to decode KubernetesSetupConfig")
	ErrNoConfigFound          = errors.New("no config found for setting up kubernetes cluster")
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalEnvironment     = errors.New("illegal environment")
	ErrIllegalConfiguration   = errors.New("illegal configuration")
)

type KubernetesSetupConfig struct {
	Description              []string                  `yaml:"description"`
	ClusterName              string                    `yaml:"clusterName" caoCli:"required"`
	Platform                 installutils.PlatformType `yaml:"platform" caoCli:"required"`
	Environment              EnvironmentType           `yaml:"environment" caoCli:"required"`
	NumControlPlane          int                       `yaml:"numControlPlane"`
	NumWorkers               int                       `yaml:"numWorkers"`
	OperatorImage            string                    `yaml:"operatorImage" caoCli:"required"`
	AdmissionControllerImage string                    `yaml:"admissionControllerImage" caoCli:"required"`
	Validators               []map[string]any          `yaml:"validators,omitempty"`
}

type SetupKubernetes struct {
	desc       string
	yamlConfig interface{}
}

func NewKubernetesConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*KubernetesSetupConfig)
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

		return &SetupKubernetes{
			desc:       "setup the kubernetes cluster and environment",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *SetupKubernetes) Describe() string {
	return action.desc
}

func (action *SetupKubernetes) Do(ctx *context.Context, config interface{}) error {
	c, ok := action.yamlConfig.(*KubernetesSetupConfig)
	if !ok {
		return ErrNoConfigFound
	}

	logrus.Infof("Kubernetes Setup started")

	createClusterUtil, err := NewCreateClusterUtil(c)
	if err != nil {
		return err
	}

	if err = createClusterUtil.ValidateParams(); err != nil {
		return err
	}

	if err = createClusterUtil.CreateCluster(); err != nil {
		return err
	}

	ctx.WithID(context.PlatformKey, string(c.Platform))
	ctx.WithID(context.EnvironmentKey, string(c.Environment))
	ctx.WithID(context.AdmissionIDKey, c.AdmissionControllerImage)
	ctx.WithID(context.OperatorIDKey, c.OperatorImage)

	return nil
}

func (action *SetupKubernetes) Config() interface{} {
	return action.yamlConfig
}

func (action *SetupKubernetes) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*KubernetesSetupConfig)
	if !ok {
		return ErrNoConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
