package destroycrdvalidator

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/crds"
)

var (
	ErrCRDsExists                  = errors.New("crds still exist")
	ErrCRDsDoesntExistInTestAssets = errors.New("crds does not exist in test assets")
)

type DestroyCRDValidator struct {
	Name        string `yaml:"name" caoCli:"required"`
	ClusterName string `yaml:"clusterName" caoCli:"required,context"`
	State       string `yaml:"state" caoCli:"required"`
}

func (c *DestroyCRDValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator run: %w", err)
	}

	allAssetCRDs := k8sCluster.GetAllCouchbaseCRDsGetterSetter()

	if len(allAssetCRDs) == 0 {
		// CRDs does not exist in testAssets
		return fmt.Errorf("validator run: %w", ErrCRDsDoesntExistInTestAssets)
	}

	allCRDs, err := crds.GetCRDNames()
	if err != nil && !errors.Is(err, crds.ErrNoCRDsInCluster) {
		return fmt.Errorf("validator run: %w", err)
	}

	if len(allCRDs) != 0 {
		// CRDs still exists
		return fmt.Errorf("validator run: %w", ErrCRDsExists)
	}

	for _, assetCRD := range allAssetCRDs {
		if err := k8sCluster.DeleteCRD(assetCRD.GetCRDName()); err != nil {
			return fmt.Errorf("validator run: %w", err)
		}
	}

	return nil
}

func (c *DestroyCRDValidator) GetState() string {
	return c.State
}
