package upgradekubernetes

import (
	"context"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/sirupsen/logrus"
)

type AKSNodePoolUpgradeConfig struct {
	NodePoolName           string `yaml:"nodePoolName"`
	WaitForNodePoolUpgrade bool   `yaml:"waitForNodePoolUpgrade"`
}

type UpgradeAKSCluster struct {
	ClusterName            string
	Region                 string
	KubernetesVersion      string
	UpgradeClusterVersion  bool
	WaitForClusterUpgrade  bool
	UpgradeNodePools       bool
	NodePoolsToUpgrade     []AKSNodePoolUpgradeConfig
	ManagedServiceProvider *managedk8sservices.ManagedServiceProvider
}

func (uac *UpgradeAKSCluster) UpgradeCluster(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{uac.ManagedServiceProvider}, uac.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(uac.ManagedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set aks session: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get aks session: %w", err)
	}

	resourceGroupName := uac.ClusterName + "-rg"

	if uac.UpgradeClusterVersion {
		if err := aksSession.UpdateClusterKubernetesVersion(ctx, resourceGroupName,
			uac.KubernetesVersion, uac.WaitForClusterUpgrade); err != nil {
			return fmt.Errorf("unable to upgrade k8s version for cluster %s: %w", uac.ClusterName, err)
		}

		if uac.WaitForClusterUpgrade {
			logrus.Infof("Upgrade kubernetes version to %s for cluster %s successful",
				uac.KubernetesVersion, uac.ClusterName)
		} else {
			logrus.Infof("Upgrade request for kubernetes version to %s for cluster %s successful",
				uac.KubernetesVersion, uac.ClusterName)
		}
	}

	if !uac.UpgradeNodePools {
		return nil
	}

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroupName)
	if err != nil {
		return fmt.Errorf("unable to list node pools for cluster %s: %w", uac.ClusterName, err)
	}

	// If no node pools passed, upgrade all, wait for upgrade
	if len(uac.NodePoolsToUpgrade) == 0 {
		for _, nodePool := range nodePools {
			if err := aksSession.UpdateNodePoolKubernetesVersion(ctx, resourceGroupName,
				*nodePool.Name, uac.KubernetesVersion, true); err != nil {
				return fmt.Errorf("unable to upgrade k8s version for cluster %s: %w", uac.ClusterName, err)
			}

			logrus.Infof("Upgrade kubernetes version to %s for node pool %s of cluster %s successful",
				uac.KubernetesVersion, *nodePool.Name, uac.ClusterName)
		}
	}

	for _, nodePoolConfig := range uac.NodePoolsToUpgrade {
		for _, nodePool := range nodePools {
			if nodePoolConfig.NodePoolName == *nodePool.Name {
				if err := aksSession.UpdateNodePoolKubernetesVersion(ctx, resourceGroupName,
					*nodePool.Name, uac.KubernetesVersion, nodePoolConfig.WaitForNodePoolUpgrade); err != nil {
					return fmt.Errorf("unable to upgrade k8s version for cluster %s: %w", uac.ClusterName, err)
				}

				if nodePoolConfig.WaitForNodePoolUpgrade {
					logrus.Infof("Upgrade kubernetes version to %s for node pool %s of cluster %s successful",
						uac.KubernetesVersion, *nodePool.Name, uac.ClusterName)
				} else {
					logrus.Infof("Upgrade request for kubernetes version to %s for node pool %s of cluster %s successful",
						uac.KubernetesVersion, *nodePool.Name, uac.ClusterName)
				}
			}
		}
	}

	return nil
}

func (uac *UpgradeAKSCluster) ValidateParams(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{uac.ManagedServiceProvider}, uac.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(uac.ManagedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set aks session: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get aks session: %w", err)
	}

	resourceGroupName := uac.ClusterName + "-rg"
	if _, err := aksSession.GetResourceGroup(ctx, resourceGroupName); err != nil {
		return fmt.Errorf("unable to fetch resource group %s: %w", resourceGroupName, err)
	}

	if _, err := aksSession.GetCluster(ctx, resourceGroupName); err != nil {
		return fmt.Errorf("unable to fetch cluster %s: %w", uac.ClusterName, err)
	}

	return nil
}
