package setupkubernetes

import (
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	installutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

type EnvironmentType string
type ProviderType string

const (
	Kind  EnvironmentType = "kind"
	Cloud EnvironmentType = "cloud"
	AWS   ProviderType    = "aws"
	Azure ProviderType    = "azure"
	GCP   ProviderType    = "gcp"
)

var (
	ErrDecodeKubernetesConfig = errors.New("unable to decode KubernetesSetupConfig")
	ErrNoConfigFound          = errors.New("no config found for setting up kubernetes cluster")
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalEnvironment     = errors.New("illegal environment")
	ErrIllegalConfiguration   = errors.New("illegal configuration")
	ErrIllegalProvider        = errors.New("illegal provider")
	ErrIllegalKubectlPath     = errors.New("illegal kubectl path")
)

type KubernetesSetupConfig struct {
	Description              []string                          `yaml:"description"`
	KubectlPath              string                            `yaml:"kubectlPath" env:"KUBECTL_PATH"`
	ClusterName              string                            `yaml:"clusterName" caoCli:"required,context" env:"CLUSTER_NAME"`
	Platform                 installutils.PlatformType         `yaml:"platform" caoCli:"required,context" env:"PLATFORM"`
	Environment              EnvironmentType                   `yaml:"environment" caoCli:"required,context" env:"ENVIRONMENT"`
	NumControlPlane          int                               `yaml:"numControlPlane"`
	NumWorkers               int                               `yaml:"numWorkers"`
	OperatorImage            string                            `yaml:"operatorImage" caoCli:"required,context" env:"OPERATOR_IMAGE"`
	AdmissionControllerImage string                            `yaml:"admissionControllerImage" caoCli:"required,context" env:"ADMISSION_CONTROLLER_IMAGE"`
	Provider                 ProviderType                      `yaml:"provider" caoCli:"context" env:"PROVIDER"`
	EKSRegion                string                            `yaml:"eksRegion" caoCli:"context" env:"EKS_REGION"`
	AKSRegion                string                            `yaml:"aksRegion" caoCli:"context" env:"AKS_REGION"`
	GKERegion                string                            `yaml:"gkeRegion" caoCli:"context" env:"GKE_REGION"`
	KubernetesVersion        string                            `yaml:"kubernetesVersion"`
	InstanceType             string                            `yaml:"instanceType"`
	NumNodeGroups            int                               `yaml:"numNodeGroups"`
	MinSize                  int                               `yaml:"minSize"`
	MaxSize                  int                               `yaml:"maxSize"`
	DesiredSize              int                               `yaml:"desiredSize"`
	DiskSize                 int                               `yaml:"diskSize"`
	AMI                      ekstypes.AMITypes                 `yaml:"ami"`
	KubeConfigPath           string                            `yaml:"kubeconfigPath" caoCli:"context" env:"KUBECONFIG"`
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

	if c.KubectlPath != "" {
		kubectl.WithBinaryPath(c.KubectlPath)
	}

	createClusterUtil, err := NewCreateClusterUtil(c)
	if err != nil {
		return err
	}

	ctxContext := ctx.Context()

	if err = createClusterUtil.ValidateParams(ctxContext); err != nil {
		return err
	}

	if err = createClusterUtil.CreateCluster(ctxContext); err != nil {
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

	switch c.Platform {
	case installutils.Kubernetes, installutils.Openshift:
		// No-op
	default:
		return ErrIllegalPlatform
	}

	switch c.Environment {
	case Kind:
		// No-op
	case Cloud:
		switch c.Provider {
		case AWS, Azure, GCP:
			// No-op
		default:
			return ErrIllegalProvider
		}
	default:
		return ErrIllegalEnvironment
	}

	if c.Environment == Kind && c.Platform == installutils.Openshift {
		return ErrIllegalConfiguration
	}
	if c.KubectlPath != "" {
		if !fileutils.NewFile(c.KubectlPath).IsFileExists() {
			return ErrIllegalKubectlPath
		}
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
