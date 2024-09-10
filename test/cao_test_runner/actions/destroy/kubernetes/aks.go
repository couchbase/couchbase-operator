package destroykubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

type DeleteAKSCluster struct {
	ClusterName string
	Region      string
}

func (dac *DeleteAKSCluster) DeleteCluster(ctx *context.Context) error {
	if err := dac.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.AKSManagedService}, dac.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedk8sservices.AKSManagedService)

	if err = aksSessionStore.SetSession(svc); err != nil {
		return fmt.Errorf("unable to set aks session: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(svc)
	if err != nil {
		return fmt.Errorf("unable to get aks session: %w", err)
	}

	resourceGroupName := dac.ClusterName + "-rg"

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroupName)
	if err != nil {
		return fmt.Errorf("unable to list node pools for cluster %s: %w", dac.ClusterName, err)
	}

	for _, nodePool := range nodePools {
		if strings.Contains(*nodePool.Name, "system") {
			continue
		}

		if err := aksSession.DeleteNodePool(ctx, resourceGroupName, *nodePool.Name, true); err != nil {
			return fmt.Errorf("error deleting node pool %s: %w", *nodePool.Name, err)
		}

		logrus.Info(fmt.Sprintf("Deleted node pool %s from cluster %s", *nodePool.Name, dac.ClusterName))
	}

	if err := aksSession.DeleteCluster(ctx, resourceGroupName, true); err != nil {
		return fmt.Errorf("error deleting cluster %s: %w", dac.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted AKS cluster %s", dac.ClusterName))

	virtualNetworkName := dac.ClusterName + "-vnet"
	subnetName := dac.ClusterName + "-subnet"

	if err := aksSession.DeleteSubnet(ctx, resourceGroupName, virtualNetworkName, subnetName, true); err != nil {
		return fmt.Errorf("error deleting subnet %s: %w", subnetName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted subnet %s from virtual network %s", subnetName, virtualNetworkName))

	if err := aksSession.DeleteVirtualNetwork(ctx, resourceGroupName, virtualNetworkName, true); err != nil {
		return fmt.Errorf("error deleting virtual network %s: %w", virtualNetworkName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted virtual network %s from resource group %s", virtualNetworkName, resourceGroupName))

	if err := aksSession.DeleteResourceGroup(ctx, resourceGroupName, true); err != nil {
		return fmt.Errorf("error deleting resource group %s: %w", resourceGroupName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted resource group %s", resourceGroupName))

	contextName := dac.ClusterName

	if err := kubectl.DeleteContext(contextName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("error deleting context %s from kubectl: %w", contextName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted kubectl context %s", contextName))

	return nil
}
func (dac *DeleteAKSCluster) ValidateParams(ctx *context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.AKSManagedService}, dac.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedk8sservices.AKSManagedService)

	if err = aksSessionStore.SetSession(svc); err != nil {
		return fmt.Errorf("unable to set aks session: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(svc)
	if err != nil {
		return fmt.Errorf("unable to get aks session: %w", err)
	}

	resourceGroupName := dac.ClusterName + "-rg"

	if _, err := aksSession.GetResourceGroup(ctx, resourceGroupName); err != nil {
		return fmt.Errorf("unable to fetch resource group details %s: %w", resourceGroupName, err)
	}

	if _, err := aksSession.GetCluster(ctx, resourceGroupName); err != nil {
		return fmt.Errorf("unable to fetch cluster details %s: %w", dac.ClusterName, err)
	}

	return nil
}
