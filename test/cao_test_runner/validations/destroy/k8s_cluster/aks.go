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
	aksPlatform    = assets.Kubernetes
	aksEnvironment = assets.Cloud
	aksProvider    = assets.Azure
)

var (
	ErrClusterAKSExists = errors.New("aks cluster exists")
)

type ValidateAKSCluster struct {
	ClusterName string
}

func (v *ValidateAKSCluster) ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != aksPlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != aksEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != aksProvider {
		return fmt.Errorf("validate cluster: %w", ErrServiceProviderMismatch)
	}

	_, err = testAssets.GetK8SClustersGetterSetter().GetAKSClusterDetailGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*assets.ManagedServiceProvider{managedServiceProvider}, v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	resourceGroup := v.ClusterName + "-rg"

	_, err = aksSession.GetCluster(ctx, resourceGroup)
	if err == nil {
		// Cluster does exist in AKS
		return fmt.Errorf("validate cluster: %w", ErrClusterAKSExists)
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

	if err := testAssets.GetK8SClustersGetterSetter().DeleteAKSClusterDetail(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if err := testAssets.GetK8SClustersGetterSetter().DeleteK8SCluster(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	return nil
}
