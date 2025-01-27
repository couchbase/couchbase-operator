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
	gkePlatform    = managedk8sservices.Kubernetes
	gkeEnvironment = managedk8sservices.Cloud
	gkeProvider    = managedk8sservices.GCP
)

var (
	ErrClusterGKEExists = errors.New("gke cluster exists")
)

type ValidateGKECluster struct {
	ClusterName string
}

func (v *ValidateGKECluster) ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {

	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != gkePlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != gkeEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != gkeProvider {
		return fmt.Errorf("validate cluster: %w", ErrServiceProviderMismatch)
	}

	_, err = testAssets.GetK8SClustersGetterSetter().GetGKEClusterDetailGetterSetter(v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, v.ClusterName)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	_, err = gkeSession.GetCluster(ctx)
	if err == nil {
		// Cluster does exist in GKE
		return fmt.Errorf("validate cluster: %w", ErrClusterGKEExists)
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

	if err := testAssets.GetK8SClustersGetterSetter().DeleteGKEClusterDetail(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	if err := testAssets.GetK8SClustersGetterSetter().DeleteK8SCluster(v.ClusterName); err != nil {
		return fmt.Errorf("validate cluster: %w", err)
	}

	return nil
}
