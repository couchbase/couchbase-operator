package destroyk8sclustervalidator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
)

const (
	eksPlatform    = managedk8sservices.Kubernetes
	eksEnvironment = managedk8sservices.Cloud
	eksProvider    = managedk8sservices.AWS
)

var (
	ErrClusterEKSExists = errors.New("eks cluster exists")
)

type ValidateEKSCluster struct {
	ClusterName string
}

func (v *ValidateEKSCluster) ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != eksPlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != eksEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != eksProvider {
		return fmt.Errorf("validate cluster: %w", ErrServiceProviderMismatch)
	}

	_, err = testAssets.GetK8SClustersGetterSetter().GetEKSClusterDetailGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	_, err = eksSession.GetEKSCluster(ctx)
	if err == nil {
		// Cluster does exist in EKS
		return fmt.Errorf("validate cluster: %w", ErrClusterEKSExists)
	}

	out, _, err := kubectl.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	// Remove the first and last element from the slice
	allClusters = allClusters[1 : len(allClusters)-1]

	if contains(allClusters, v.ClusterName) {
		return fmt.Errorf("validate cluster: %w", ErrClusterKubeconfigExists)
	}

	if err := testAssets.GetK8SClustersGetterSetter().DeleteEKSClusterDetail(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if err := testAssets.GetK8SClustersGetterSetter().DeleteK8SCluster(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	return nil
}
