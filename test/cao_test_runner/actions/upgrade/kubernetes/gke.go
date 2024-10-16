package upgradekubernetes

import (
	"context"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/sirupsen/logrus"
)

type GKENodePoolUpgradeConfig struct {
	NodePoolName           string `yaml:"nodePoolName"`
	WaitForNodePoolUpgrade bool   `yaml:"waitForNodePoolUpgrade"`
}

type UpgradeGKECluster struct {
	ClusterName           string
	Region                string
	KubernetesVersion     string
	UpgradeClusterVersion bool
	WaitForClusterUpgrade bool
	UpgradeMaster         bool
	WaitForMasterUpgrade  bool
	UpgradeNodePool       bool
	NodePoolsToUpgrade    []GKENodePoolUpgradeConfig
}

var (
	ErrInvalidKubernetesVersion = errors.New("for environment type 'cloud' and provider 'gcp', kubernetes version is invalid")
)

func (ugc *UpgradeGKECluster) UpgradeCluster(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials([]managedk8sservices.ManagedServiceProvider{managedk8sservices.GKEManagedService}, ugc.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedk8sservices.GKEManagedService)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set gke session: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get gke session: %w", err)
	}

	if ugc.UpgradeClusterVersion {
		if err := gkeSession.UpdateClusterKubernetesVersion(ctx, ugc.KubernetesVersion, ugc.WaitForClusterUpgrade); err != nil {
			return fmt.Errorf("unable to upgrade k8s version for cluster %s: %w", ugc.ClusterName, err)
		}

		if ugc.WaitForClusterUpgrade {
			logrus.Infof("Upgrade kubernetes version to %s for cluster %s successful",
				ugc.KubernetesVersion, ugc.ClusterName)
		} else {
			logrus.Infof("Upgrade request for kubernetes version to %s for cluster %s successful",
				ugc.KubernetesVersion, ugc.ClusterName)
		}
	}

	if ugc.UpgradeMaster {
		if err := gkeSession.UpdateMasterKubernetesVersion(ctx, ugc.KubernetesVersion, ugc.WaitForMasterUpgrade); err != nil {
			return fmt.Errorf("unable to upgrade k8s version for cluster %s: %w", ugc.ClusterName, err)
		}

		if ugc.WaitForMasterUpgrade {
			logrus.Infof("Upgrade kubernetes version to %s for master of cluster %s successful",
				ugc.KubernetesVersion, ugc.ClusterName)
		} else {
			logrus.Infof("Upgrade request for kubernetes version to %s for master of cluster %s successful",
				ugc.KubernetesVersion, ugc.ClusterName)
		}
	}

	if !ugc.UpgradeNodePool {
		return nil
	}

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list node pools of cluster %s: %w", ugc.ClusterName, err)
	}

	// If no node pools passed, upgrade all, wait for upgrade
	if len(ugc.NodePoolsToUpgrade) == 0 {
		for _, nodePool := range nodePools.NodePools {
			if err := gkeSession.UpdateNodePoolKubernetesVersion(ctx, nodePool.Name, ugc.KubernetesVersion, true); err != nil {
				return fmt.Errorf("failed to delete node pool %s of cluster %s: %w", nodePool.Name, ugc.ClusterName, err)
			}

			logrus.Infof("Upgrade kubernetes version to %s for node pool %s of cluster %s successful",
				ugc.KubernetesVersion, nodePool.Name, ugc.ClusterName)
		}
	}

	for _, nodePoolToUpgrade := range ugc.NodePoolsToUpgrade {
		for _, nodePool := range nodePools.NodePools {
			if nodePoolToUpgrade.NodePoolName == nodePool.Name {
				if err := gkeSession.UpdateNodePoolKubernetesVersion(ctx, nodePool.Name,
					ugc.KubernetesVersion, nodePoolToUpgrade.WaitForNodePoolUpgrade); err != nil {
					return fmt.Errorf("failed to delete node pool %s of cluster %s: %w", nodePool.Name, ugc.ClusterName, err)
				}

				if nodePoolToUpgrade.WaitForNodePoolUpgrade {
					logrus.Infof("Upgrade kubernetes version to %s for node pool %s of cluster %s successful",
						ugc.KubernetesVersion, nodePool.Name, ugc.ClusterName)
				} else {
					logrus.Infof("Upgrade request for kubernetes version to %s for node pool %s of cluster %s successful",
						ugc.KubernetesVersion, nodePool.Name, ugc.ClusterName)
				}
			}
		}
	}

	return nil
}

func (ugc *UpgradeGKECluster) ValidateParams(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials([]managedk8sservices.ManagedServiceProvider{managedk8sservices.GKEManagedService}, ugc.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedk8sservices.GKEManagedService)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set gke session: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get gke session: %w", err)
	}

	if _, err := gkeSession.GetCluster(ctx); err != nil {
		return fmt.Errorf("unable to fetch cluster details: %w", err)
	}

	validKubernetesVersions, err := gkeSession.ListAvailableKubernetesVersions(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch valid kubernetes versions: %w", err)
	}

	var versionAvailable bool

	for _, version := range validKubernetesVersions {
		if version == ugc.KubernetesVersion {
			versionAvailable = true
		}
	}

	if !versionAvailable {
		return fmt.Errorf("invalid kubernetes version, not available in GKE: %w", ErrInvalidKubernetesVersion)
	}

	return nil
}
