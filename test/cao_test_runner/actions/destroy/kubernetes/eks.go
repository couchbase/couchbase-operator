package destroykubernetes

import (
	"fmt"
	"strings"

	"context"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

type DeleteEKSCluster struct {
	ClusterName string
	Region      string
}

func containsRoute(routeTables []*ec2.RouteTable, routeID string) bool {
	for _, routeTable := range routeTables {
		if *routeTable.RouteTableId == routeID {
			return true
		}
	}

	return false
}

func (dec *DeleteEKSCluster) DeleteCluster(ctx *context.Context) error {
	if err := dec.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.EKSManagedService}, dec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(managedk8sservices.EKSManagedService)
	if err = eksSessionStore.SetSession(svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	cluster, err := eksSession.GetEKSCluster()
	if err != nil {
		return fmt.Errorf("unable to fetch cluster details %s: %w", dec.ClusterName, err)
	}

	nodeGroups, err := eksSession.GetNodegroupsForCluster()
	if err != nil {
		return fmt.Errorf("unable to fetch node groups for cluster %s: %w", dec.ClusterName, err)
	}

	for _, nodeGroup := range nodeGroups {
		if err = eksSession.DeleteNodeGroup(*nodeGroup.NodegroupName, true); err != nil {
			return fmt.Errorf("unable to delete node group %s of cluster %s: %w", *nodeGroup.NodegroupName, dec.ClusterName, err)
		}

		logrus.Info(fmt.Sprintf("Deleted node group %s from cluster %s", *nodeGroup.NodegroupName, dec.ClusterName))

		parts := strings.Split(*nodeGroup.NodeRole, "/")
		roleName := &parts[len(parts)-1]

		if err = eksSession.DeleteIAMRole(roleName); err != nil {
			return fmt.Errorf("unable to delete iam %s for node group %s of cluster %s: %w", *nodeGroup.NodeRole, *nodeGroup.NodegroupName, dec.ClusterName, err)
		}

		logrus.Info(fmt.Sprintf("Deleted IAM role %s of node group %s from cluster %s", *nodeGroup.NodeRole, *nodeGroup.NodegroupName, dec.ClusterName))
	}

	if err = eksSession.DeleteCluster(true); err != nil {
		return fmt.Errorf("unable to delete cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted cluster %s", dec.ClusterName))

	if err = eksSession.DeleteSecurityGroups(cluster.ResourcesVpcConfig.SecurityGroupIds); err != nil {
		return fmt.Errorf("unable to delete security group for cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted security groups from cluster %s", dec.ClusterName))

	igw, err := eksSession.GetInternetGatewayForVPC(cluster.ResourcesVpcConfig.VpcId)
	if err != nil {
		return fmt.Errorf("unable to find internet gateway attached to vpc: %w", err)
	}

	if err := eksSession.DetachInternetGateway(cluster.ResourcesVpcConfig.VpcId, igw.InternetGatewayId); err != nil {
		return fmt.Errorf("unable to detach internet gateway: %w", err)
	}

	logrus.Info("Detached Internet Gateway from vpc")

	if err := eksSession.DeleteInternetGateway(igw.InternetGatewayId); err != nil {
		return fmt.Errorf("unable to delete internet gateway: %w", err)
	}

	logrus.Info("Deleted Internet Gateway")

	var routes []*ec2.RouteTable

	for _, subnetID := range cluster.ResourcesVpcConfig.SubnetIds {
		routeTables, err := eksSession.GetRouteTablesForSubnet(subnetID)
		if err != nil {
			return fmt.Errorf("unable to find route tables for subnet: %w", err)
		}

		for _, routeTable := range routeTables {
			if !containsRoute(routes, *routeTable.RouteTableId) {
				routes = append(routes, routeTable)
			}

			association, err := eksSession.GetRouteTableAssociation(routeTable.RouteTableId, subnetID)
			if err != nil {
				return fmt.Errorf("unable to find association with route table and subnet: %w", err)
			}

			if err := eksSession.DissociateRouteTable(association.RouteTableAssociationId); err != nil {
				return fmt.Errorf("unable to find association with route table and subnet: %w", err)
			}
		}
	}

	for _, routeTable := range routes {
		if err = eksSession.DeleteRoute(routeTable.RouteTableId); err != nil {
			return fmt.Errorf("unable to delete route table and subnet: %w", err)
		}

		if err := eksSession.DeleteRouteTable(routeTable.RouteTableId); err != nil {
			return fmt.Errorf("unable to delete route table: %w", err)
		}
	}

	logrus.Info("Dissociated route tables from subnets and deleted route tables")

	if err = eksSession.DeleteSubnets(cluster.ResourcesVpcConfig.SubnetIds); err != nil {
		return fmt.Errorf("unable to delete subnets for cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted subnets from cluster %s", dec.ClusterName))

	if err = eksSession.DeleteVpc(cluster.ResourcesVpcConfig.VpcId); err != nil {
		return fmt.Errorf("unable to delete vpc for cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("Deleted vpc from cluster %s", dec.ClusterName))

	rolesToDelete := []string{dec.ClusterName + "-eks-role", dec.ClusterName + "-eks-ebscsidriver-role"}
	for _, roleName := range rolesToDelete {
		if err = eksSession.DeleteIAMRole(&roleName); err != nil {
			return fmt.Errorf("unable to delete iam role %s: %w", roleName, err)
		}

		logrus.Info(fmt.Sprintf("Deleted IAM role %s", roleName))
	}

	contextName := fmt.Sprintf("eks-%s@%s", dec.ClusterName, dec.Region)
	if err := kubectl.DeleteContext(contextName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to delete context from kubectl %s: %w", contextName, err)
	}

	return nil
}

func (dec *DeleteEKSCluster) ValidateParams(_ *context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.EKSManagedService}, dec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(managedk8sservices.EKSManagedService)
	if err = eksSessionStore.SetSession(svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	if _, err := eksSession.GetEKSCluster(); err != nil {
		return fmt.Errorf("unable to fetch cluster details %s: %w", dec.ClusterName, err)
	}

	return nil
}
