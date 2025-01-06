package upgradekubernetes

import (
	"context"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/sirupsen/logrus"
)

type EKSNodeGroupUpgradeConfig struct {
	NodeGroupName           string `yaml:"nodeGroupName"`
	WaitForNodeGroupUpgrade bool   `yaml:"waitForNodeGroupUpgrade"`
}

type UpgradeEKSCluster struct {
	ClusterName            string
	Region                 string
	KubernetesVersion      string
	UpgradeClusterVersion  bool
	WaitForClusterUpgrade  bool
	UpgradeNodeGroups      bool
	NodeGroupsToUpgrade    []EKSNodeGroupUpgradeConfig
	ManagedServiceProvider *managedk8sservices.ManagedServiceProvider
}

func (uec *UpgradeEKSCluster) UpgradeCluster(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{uec.ManagedServiceProvider}, uec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(uec.ManagedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	if uec.UpgradeClusterVersion {
		if err := eksSession.UpdateClusterKubernetesVersion(ctx, &uec.KubernetesVersion, uec.WaitForClusterUpgrade); err != nil {
			return fmt.Errorf("unable to upgrade k8s version for cluster %s: %w", uec.ClusterName, err)
		}

		if uec.WaitForClusterUpgrade {
			logrus.Infof("Upgrade kubernetes version to %s for cluster %s successful",
				uec.KubernetesVersion, uec.ClusterName)
		} else {
			logrus.Infof("Upgrade request for kubernetes version to %s for cluster %s successful",
				uec.KubernetesVersion, uec.ClusterName)
		}

	}

	if !uec.UpgradeNodeGroups {
		return nil
	}

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch node groups for cluster %s: %w", uec.ClusterName, err)
	}

	// If no node groups passed, upgrade all, wait for upgrade
	if len(uec.NodeGroupsToUpgrade) == 0 {
		for _, nodeGroup := range nodeGroups {
			if err := eksSession.UpdateNodeGroupKubernetesVersion(ctx, nodeGroup.NodegroupName,
				&uec.KubernetesVersion, true); err != nil {
				return fmt.Errorf("unable to update node groups for cluster %s: %w", uec.ClusterName, err)
			}

			logrus.Infof("Upgrade kubernetes version to %s for node group %s of cluster %s successful",
				uec.KubernetesVersion, *nodeGroup.NodegroupName, uec.ClusterName)
		}
	}

	for _, nodeGroupToUpgrade := range uec.NodeGroupsToUpgrade {
		for _, nodeGroup := range nodeGroups {
			if nodeGroupToUpgrade.NodeGroupName == *nodeGroup.NodegroupName {
				if err := eksSession.UpdateNodeGroupKubernetesVersion(ctx, nodeGroup.NodegroupName,
					&uec.KubernetesVersion, nodeGroupToUpgrade.WaitForNodeGroupUpgrade); err != nil {
					return fmt.Errorf("unable to update node groups for cluster %s: %w", uec.ClusterName, err)
				}

				if nodeGroupToUpgrade.WaitForNodeGroupUpgrade {
					logrus.Infof("Upgrade kubernetes version to %s for node group %s of cluster %s successful",
						uec.KubernetesVersion, *nodeGroup.NodegroupName, uec.ClusterName)
				} else {
					logrus.Infof("Upgrade request for kubernetes version to %s for node group %s of cluster %s successful",
						uec.KubernetesVersion, *nodeGroup.NodegroupName, uec.ClusterName)
				}
			}
		}
	}

	return nil
}

func (uec *UpgradeEKSCluster) ValidateParams(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{uec.ManagedServiceProvider}, uec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(uec.ManagedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	if _, err := eksSession.GetEKSCluster(ctx); err != nil {
		return fmt.Errorf("unable to fetch cluster details %s: %w", uec.ClusterName, err)
	}

	return nil
}
