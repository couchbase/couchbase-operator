package k8sclustervalidator

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
)

type KubernetesClusterValidator struct {
	Name        string      `yaml:"name" caoCli:"required"`
	ClusterName string      `yaml:"clusterName" caoCli:"required,context"`
	State       string      `yaml:"state" caoCli:"required"`
	KindConfig  *KindConfig `yaml:"kindConfig"`
	EKSConfig   *EKSConfig  `yaml:"eksConfig"`
	AKSConfig   *AKSConfig  `yaml:"aksConfig"`
}

func (c *KubernetesClusterValidator) Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error {
	if err := c.ValidateCluster(testAssets); err != nil {
		return fmt.Errorf("validator run: %w", err)
	}

	v, err := NewValidateClusterUtil(c, testAssets)
	if err != nil {
		return fmt.Errorf("validator run: %w", err)
	}

	if err := v.ValidateCluster(ctx.Context(), testAssets); err != nil {
		return fmt.Errorf("validator run: %w", err)
	}

	return nil
}

func (c *KubernetesClusterValidator) ValidateCluster(testAssets assets.TestAssetGetterSetter) error {
	// Generic K8S Cluster Validations
	// TODO : Check if all nodes are healthy
	return nil
}

func (c *KubernetesClusterValidator) GetState() string {
	return c.State
}
