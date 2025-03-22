package setupkubernetes

import (
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeKubernetesConfig = errors.New("unable to decode KubernetesSetupConfig")
	ErrNoConfigFound          = errors.New("no config found for setting up kubernetes cluster")
)

type KubernetesSetupConfig struct {
	Description              []string                           `yaml:"description"`
	ClusterName              string                             `yaml:"clusterName" caoCli:"required,context" env:"CLUSTER_NAME"`
	Platform                 managedk8sservices.PlatformType    `yaml:"platform" caoCli:"required" env:"PLATFORM"`
	Environment              managedk8sservices.EnvironmentType `yaml:"environment" caoCli:"required" env:"ENVIRONMENT"`
	NumControlPlane          int                                `yaml:"numControlPlane"`
	NumWorkers               int                                `yaml:"numWorkers"`
	LoadDockerImageToKind    bool                               `yaml:"loadDockerImageToKind"`
	OperatorImage            string                             `yaml:"operatorImage" caoCli:"required" env:"OPERATOR_IMAGE"`
	AdmissionControllerImage string                             `yaml:"admissionControllerImage" caoCli:"required" env:"ADMISSION_CONTROLLER_IMAGE"`
	Provider                 managedk8sservices.ProviderType    `yaml:"provider" env:"PROVIDER"`
	EKSRegion                string                             `yaml:"eksRegion" caoCli:"context" env:"EKS_REGION"`
	AKSRegion                string                             `yaml:"aksRegion" caoCli:"context" env:"AKS_REGION"`
	GKERegion                string                             `yaml:"gkeRegion" caoCli:"context" env:"GKE_REGION"`
	KubernetesVersion        string                             `yaml:"kubernetesVersion"`
	EKSNodegroups            []*EKSNodegroup                    `yaml:"eksNodegroups"`
	GKENodePools             []*GKENodePool                     `yaml:"gkeNodePools"`
	DiskSize                 int                                `yaml:"diskSize"`       // AKS, GKE
	OSSKU                    armcontainerservice.OSSKU          `yaml:"osSKU"`          // AKS
	OSType                   armcontainerservice.OSType         `yaml:"osType"`         // AKS
	VMSize                   string                             `yaml:"vmSize"`         // AKS
	Count                    int                                `yaml:"count"`          // AKS, GKE
	NumNodePools             int                                `yaml:"numNodePools"`   // AKS, GKE
	MachineType              string                             `yaml:"machineType"`    // GKE
	ImageType                string                             `yaml:"imageType"`      // GKE
	DiskType                 string                             `yaml:"diskType"`       // GKE
	ReleaseChannel           managedk8sservices.ReleaseChannel  `yaml:"releaseChannel"` // GKE
	kubeconfigPath           *fileutils.File
	ms                       *managedk8sservices.ManagedServiceProvider
	resultsDirectory         *fileutils.Directory
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

func (action *SetupKubernetes) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubernetesSetupConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	logrus.Infof("Kubernetes Setup started")

	c.resultsDirectory = testAssets.GetResultsDirectory()
	c.kubeconfigPath = testAssets.GetKubeconfigPath()

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

	return nil
}

func (action *SetupKubernetes) Config() interface{} {
	return action.yamlConfig
}

func (action *SetupKubernetes) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubernetesSetupConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	c.ms = managedk8sservices.NewManagedServiceProvider(c.Platform, c.Environment, c.Provider)

	if err := managedk8sservices.ValidateManagedServices(c.ms); err != nil {
		return err
	}

	return nil
}
