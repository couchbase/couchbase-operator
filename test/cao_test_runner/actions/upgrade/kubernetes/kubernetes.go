package upgradekubernetes

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	caoinstallutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils/cao_install_utils"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeKubernetesConfig = errors.New("unable to decode KubernetesUpgradeConfig")
	ErrNoConfigFound          = errors.New("no config found for upgrading kubernetes cluster")
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalEnvironment     = errors.New("illegal environment")
	ErrIllegalConfiguration   = errors.New("illegal configuration")
	ErrIllegalProvider        = errors.New("illegal provider")
)

type KubernetesUpgradeConfig struct {
	Description             []string                           `yaml:"description"`
	ClusterName             string                             `yaml:"clusterName" caoCli:"required,context" env:"CLUSTER_NAME"`
	Platform                caoinstallutils.PlatformType       `yaml:"platform" caoCli:"required,context" env:"PLATFORM"`
	Environment             managedk8sservices.EnvironmentType `yaml:"environment" caoCli:"required,context" env:"ENVIRONMENT"`
	Provider                managedk8sservices.ProviderType    `yaml:"provider" caoCli:"context" env:"PROVIDER"`
	EKSRegion               string                             `yaml:"eksRegion" caoCli:"context" env:"EKS_REGION"`
	AKSRegion               string                             `yaml:"aksRegion" caoCli:"context" env:"AKS_REGION"`
	GKERegion               string                             `yaml:"gkeRegion" caoCli:"context" env:"GKE_REGION"`
	KubernetesVersion       string                             `yaml:"kubernetesVersion"`
	UpgradeClusterVersion   bool                               `yaml:"upgradeClusterVersion"`
	WaitForClusterUpgrade   bool                               `yaml:"waitForClusterUpgrade"`
	UpgradeAKSNodePools     bool                               `yaml:"upgradeAKSNodePools"`
	AKSNodePoolsToUpgrade   []AKSNodePoolUpgradeConfig         `yaml:"aksNodePoolsToUpgrade"`
	UpgradeEKSNodeGroups    bool                               `yaml:"upgradeEKSNodeGroups"`
	EKSNodeGroupsToUpgrade  []EKSNodeGroupUpgradeConfig        `yaml:"eksNodeGroupsToUpgrade"`
	UpgradeGKEMaster        bool                               `yaml:"upgradeGKEMaster"`
	WaitForGKEMasterUpgrade bool                               `yaml:"waitForGKEMasterUpgrade"`
	UpgradeGKENodePool      bool                               `yaml:"upgradeGKENodePool"`
	GKENodePoolsToUpgrade   []GKENodePoolUpgradeConfig         `yaml:"gkeNodePoolsToUpgrade"`
	Validators              []map[string]any                   `yaml:"validators,omitempty"`
	ms                      *managedk8sservices.ManagedServiceProvider
}

type KubernetesUpgrade struct {
	desc       string
	yamlConfig interface{}
}

func NewKubernetesUpgradeConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*KubernetesUpgradeConfig)
		if !ok {
			return nil, ErrDecodeKubernetesConfig
		}

		return &KubernetesUpgrade{
			desc:       "upgrade kubernetes cluster",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *KubernetesUpgrade) Describe() string {
	return action.desc
}

func (action *KubernetesUpgrade) Do(ctx *context.Context) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubernetesUpgradeConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	logrus.Infof("Kubernetes Upgrade started")

	upgradeClusterUtil, err := NewUpgradeClusterUtil(c)
	if err != nil {
		return err
	}

	ctxContext := ctx.Context()

	if err = upgradeClusterUtil.ValidateParams(ctxContext); err != nil {
		return err
	}

	if err = upgradeClusterUtil.UpgradeCluster(ctxContext); err != nil {
		return err
	}

	return nil
}

func (action *KubernetesUpgrade) Config() interface{} {
	return action.yamlConfig
}

func (action *KubernetesUpgrade) RunValidators(ctx *context.Context, state string) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubernetesUpgradeConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *KubernetesUpgrade) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubernetesUpgradeConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	c.ms = &managedk8sservices.ManagedServiceProvider{
		Platform:    c.Platform,
		Environment: c.Environment,
		Provider:    c.Provider,
	}

	if err := managedk8sservices.ValidateManagedServices(c.ms); err != nil {
		return err
	}

	return nil
}
