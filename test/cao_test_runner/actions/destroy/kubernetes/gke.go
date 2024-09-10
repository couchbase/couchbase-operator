package destroykubernetes

import (
	"context"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

type DeleteGKECluster struct {
	ClusterName string
	Region      string
}

func (dgc *DeleteGKECluster) DeleteCluster(ctx *context.Context) error {
	if err := dgc.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.GKEManagedService}, dgc.ClusterName)
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

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list node pools of cluster %s: %w", dgc.ClusterName, err)
	}

	for _, nodePool := range nodePools.NodePools {
		if err := gkeSession.DeleteNodePool(ctx, nodePool.Name); err != nil {
			return fmt.Errorf("failed to delete node pool %s of cluster %s: %w", nodePool.Name, dgc.ClusterName, err)
		}

		logrus.Info(fmt.Sprintf("Node pool %s of cluster %s deleted", nodePool.Name, dgc.ClusterName))
	}

	if err := gkeSession.DeleteCluster(ctx); err != nil {
		return fmt.Errorf("failed to delete cluster %s: %w", dgc.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted GKE Cluster %s", dgc.ClusterName))

	subnetName := dgc.ClusterName + "-subnet"
	networkName := dgc.ClusterName + "-network"
	firewallRuleName := dgc.ClusterName + "-firewall"
	contextName := fmt.Sprintf("gke_%s_%s", dgc.Region, dgc.ClusterName)

	if err := gkeSession.DeleteSubnet(ctx, subnetName); err != nil {
		return fmt.Errorf("failed to delete subnet %s of virtual network %s: %w", subnetName, networkName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted subnet %s of virtual network %s", subnetName, networkName))

	if err := gkeSession.DeleteFirewallRule(ctx, firewallRuleName); err != nil {
		return fmt.Errorf("failed to delete firewall rule %s of virtual network %s: %w", firewallRuleName, networkName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted firewall rule %s of virtual network %s", firewallRuleName, networkName))

	if err := gkeSession.DeleteVirtualNetwork(ctx, networkName); err != nil {
		return fmt.Errorf("failed to delete virtual network %s: %w", networkName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted virtual network %s", networkName))

	if err := kubectl.DeleteContext(contextName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("failed to delete context %s from kube config: %w", contextName, err)
	}

	return nil
}

func (dgc *DeleteGKECluster) ValidateParams(ctx *context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.GKEManagedService}, dgc.ClusterName)
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
		return fmt.Errorf("unable to fetch cluster details %s: %w", dgc.ClusterName, err)
	}

	return nil
}
