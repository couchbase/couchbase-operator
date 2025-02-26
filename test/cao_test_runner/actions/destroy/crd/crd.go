package destroycrd

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeCRDSetupConfig = errors.New("unable to decode CRDSetupConfig")
	ErrNoConfigFound        = errors.New("no config found for setting up crds")
	ErrNoAvailableContexts  = errors.New("no such available contexts")
	ErrCRDFileNoExist       = errors.New("crd file does not exist")
)

type CRDDestroyConfig struct {
	Description []string `yaml:"description"`
	ClusterName string   `yaml:"clusterName" caoCli:"required,context" env:"CLUSTER_NAME"`
}

type DestroyCRD struct {
	desc       string
	yamlConfig interface{}
}

func NewCRDDestroyConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*CRDDestroyConfig)
		if !ok {
			return nil, ErrDecodeCRDSetupConfig
		}

		return &DestroyCRD{
			desc:       "delete the crds",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoConfigFound
}

func (action *DestroyCRD) Describe() string {
	return action.desc
}

func (action *DestroyCRD) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	c, ok := action.yamlConfig.(*CRDDestroyConfig)
	if !ok {
		return ErrDecodeCRDSetupConfig
	}

	logrus.Infof("CRD delete action started")

	cluster, err := testAssets.GetK8SClustersGetter().GetK8SClusterGetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("crd destroy do: %w", err)
	}

	allCRDs := cluster.GetAllCouchbaseCRDsGetter()

	for _, assetCRD := range allCRDs {
		if err := kubectl.DeleteCRD(assetCRD.GetCRDName()).ExecWithoutOutputCapture(); err != nil {
			return fmt.Errorf("crd destroy do: %w", err)
		}
	}

	return nil
}

func (action *DestroyCRD) Config() interface{} {
	return action.yamlConfig
}

func (action *DestroyCRD) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoConfigFound
	}

	_, ok := action.yamlConfig.(*CRDDestroyConfig)
	if !ok {
		return ErrDecodeCRDSetupConfig
	}

	return nil
}
