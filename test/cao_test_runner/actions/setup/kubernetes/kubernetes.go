package setupkubernetes

import (
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
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
	ErrDecodeKubernetesConfig = errors.New("unable to decode KubernetesSetupConfig")
	ErrNoConfigFound          = errors.New("no config found for setting up kubernetes cluster")
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalEnvironment     = errors.New("illegal environment")
	ErrIllegalConfiguration   = errors.New("illegal configuration")
	ErrIllegalProvider        = errors.New("illegal provider")
)

type KubernetesSetupConfig struct {
	Description              []string                          `yaml:"description"`
	ClusterName              string                            `yaml:"clusterName" caoCli:"required"`
	Platform                 installutils.PlatformType         `yaml:"platform" caoCli:"required"`
	Environment              EnvironmentType                   `yaml:"environment" caoCli:"required"`
	NumControlPlane          int                               `yaml:"numControlPlane"`
	NumWorkers               int                               `yaml:"numWorkers"`
	OperatorImage            string                            `yaml:"operatorImage" caoCli:"required"`
	AdmissionControllerImage string                            `yaml:"admissionControllerImage" caoCli:"required"`
	Provider                 ProviderType                      `yaml:"provider"`
	EKSRegion                string                            `yaml:"eksRegion" env:"EKS_REGION"`
	AKSRegion                string                            `yaml:"aksRegion" env:"AKS_REGION"`
	GKERegion                string                            `yaml:"gkeRegion" env:"GKE_REGION"`
	KubernetesVersion        string                            `yaml:"kubernetesVersion"`
	InstanceType             string                            `yaml:"instanceType"`
	NumNodeGroups            int                               `yaml:"numNodeGroups"`
	MinSize                  int                               `yaml:"minSize"`
	MaxSize                  int                               `yaml:"maxSize"`
	DesiredSize              int                               `yaml:"desiredSize"`
	DiskSize                 int                               `yaml:"diskSize"`
	AMI                      managedk8sservices.AMIType        `yaml:"ami"`
	KubeConfigPath           string                            `yaml:"kubeconfigPath" env:"KUBECONFIG"`
	OSSKU                    armcontainerservice.OSSKU         `yaml:"osSKU"`
	OSType                   armcontainerservice.OSType        `yaml:"osType"`
	VMSize                   string                            `yaml:"vmSize"`
	Count                    int                               `yaml:"count"`
	NumNodePools             int                               `yaml:"numNodePools"`
	MachineType              string                            `yaml:"machineType"`
	ImageType                string                            `yaml:"imageType"`
	DiskType                 string                            `yaml:"diskType"`
	ReleaseChannel           managedk8sservices.ReleaseChannel `yaml:"releaseChannel"`
	Validators               []map[string]any                  `yaml:"validators,omitempty"`
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
		case Kind:
			// No-op
		case Cloud:
			switch c.Provider {
			case AWS, Azure, GoogleCloud:
				// No-op
			default:
				return nil, ErrIllegalProvider
			}
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

	ctxContext := ctx.Context()

	if err = createClusterUtil.ValidateParams(&ctxContext); err != nil {
		return err
	}

	if err = createClusterUtil.CreateCluster(&ctxContext); err != nil {
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
