package destroyk8sclustervalidator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kind"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

const (
	kindPlatform    = managedk8sservices.Kubernetes
	kindEnvironment = managedk8sservices.Kind
	kindProvider    = ""
)

var (
	ErrClusterKindExists       = errors.New("kind cluster exists")
	ErrClusterKubeconfigExists = errors.New("cluster exists in kubeconfig")
	ErrServiceProviderMismatch = errors.New("service provider mismatch")
)

type ValidateKindCluster struct {
	ClusterName string
}

func (v *ValidateKindCluster) ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	out, _, err := kind.GetClusters().Exec(true, false)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	allKindClusters := strings.Split(out, "\n")

	// Remove the last element from the slice
	allKindClusters = allKindClusters[:len(allKindClusters)-1]

	if contains(allKindClusters, v.ClusterName) {
		return fmt.Errorf("validate cluster: %w", ErrClusterKindExists)
	}

	out, _, err = kubectl.GetClusters().Exec(true, false)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	// Remove the first and last element from the slice
	allClusters = allClusters[1 : len(allClusters)-1]

	if contains(allClusters, v.ClusterName) {
		return fmt.Errorf("validate cluster: %w", ErrClusterKubeconfigExists)
	}

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	_, err = testAssets.GetK8SClustersGetterSetter().GetKindClusterDetailGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != kindPlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != kindEnvironment {
		return fmt.Errorf("validate cluster: %w", ErrServiceProviderMismatch)
	}

	if err := testAssets.GetK8SClustersGetterSetter().DeleteKindClusterDetail(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if err := testAssets.GetK8SClustersGetterSetter().DeleteK8SCluster(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	return nil
}
