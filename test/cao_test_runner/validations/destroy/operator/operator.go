package destroyoperatorvalidator

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
)

var (
	ErrOperatorPodExists      = errors.New("operator pod exists")
	ErrOperatorPodNotInAssets = errors.New("operator pod not in assets")
)

type DestroyOperatorValidator struct {
	Name        string `yaml:"name" caoCli:"required"`
	Namespace   string `yaml:"namespace" caoCli:"required,context"`
	ClusterName string `yaml:"clusterName" caoCli:"required,context"`
	State       string `yaml:"state" caoCli:"required"`
}

func (c *DestroyOperatorValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		// Cluster does not exist
		return fmt.Errorf("validator operator previous state: %w", err)
	}

	operatorPods := k8sCluster.GetAllOperatorPodsGetterSetter()

	var assetOperatorPod assets.OperatorPodGetterSetter

	for _, pod := range operatorPods {
		if pod.GetNamespace() == c.Namespace {
			assetOperatorPod = pod
		}
	}

	if assetOperatorPod == nil {
		// Operator pod does not exist in testAssets
		return fmt.Errorf("validator operator previous state: %w", ErrOperatorPodNotInAssets)
	}

	for retry := 1; retry <= 3; retry += 1 {
		_, err := caopods.GetOperatorPod(c.Namespace)
		if err != nil && errors.Is(err, caopods.ErrOperatorPodDoesntExist) {
			break
		} else if err != nil {
			return fmt.Errorf("validator operator previous state: %w", err)
		} else if retry == 3 {
			return fmt.Errorf("validator operator previous state: %w", ErrOperatorPodExists)
		} else {
			time.Sleep(5 * time.Second)
		}
	}

	if err := k8sCluster.DeleteOperatorPod(assetOperatorPod.GetOperatorPodName()); err != nil {
		return fmt.Errorf("validator operator previous state: %w", err)
	}

	return nil
}

func (c *DestroyOperatorValidator) GetState() string {
	return c.State
}
