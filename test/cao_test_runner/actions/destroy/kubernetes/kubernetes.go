package destroykubernetes

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeKubernetesConfig = errors.New("unable to decode KubernetesDestroyConfig")
	ErrNoConfigFound          = errors.New("no config found for deleting kubernetes cluster")
)

type KubernetesDestroyConfig struct {
	Description []string         `yaml:"description"`
	ClusterName string           `yaml:"clusterName" caoCli:"required,context" env:"CLUSTER_NAME"`
	EKSRegion   string           `yaml:"eksRegion" caoCli:"context" env:"EKS_REGION"`
	AKSRegion   string           `yaml:"aksRegion" caoCli:"context" env:"AKS_REGION"`
	GKERegion   string           `yaml:"gkeRegion" caoCli:"context" env:"GKE_REGION"`
	Validators  []map[string]any `yaml:"validators,omitempty"`
	ms          *assets.ManagedServiceProvider
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

func (action *DestroyKubernetes) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubernetesDestroyConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	cluster, err := testAssets.GetK8SClustersGetter().GetK8SClusterGetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("get k8s cluster: %w", err)
	}

	c.ms = cluster.GetServiceProvider()

	logrus.Infof("Kubernetes Destroy started")

	deleteClusterUtil, err := NewDeleteClusterUtil(c)
	if err != nil {
		return err
	}

	ctxContext := ctx.Context()

	if err = deleteClusterUtil.ValidateParams(ctxContext); err != nil {
		return err
	}

	if err = deleteClusterUtil.DeleteCluster(ctxContext); err != nil {
		return err
	}

	return nil
}

func (action *DestroyKubernetes) Config() interface{} {
	return action.yamlConfig
}

func (action *DestroyKubernetes) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	_, ok := action.yamlConfig.(*KubernetesDestroyConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	return nil
}

func (action *DestroyKubernetes) RunValidators(ctx *context.Context,
	state string, testAssets assets.TestAssetGetterSetter) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*KubernetesDestroyConfig)
	if !ok {
		return ErrDecodeKubernetesConfig
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state, testAssets); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}
