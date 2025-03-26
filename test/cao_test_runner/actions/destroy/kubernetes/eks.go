package destroykubernetes

import (
	"fmt"
	"strings"

	"context"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/sirupsen/logrus"
)

type DeleteEKSCluster struct {
	ClusterName            string
	Region                 string
	ManagedServiceProvider *managedk8sservices.ManagedServiceProvider
}

func containsRoute(routeTables []ec2types.RouteTable, routeID string) bool {
	for _, routeTable := range routeTables {
		if *routeTable.RouteTableId == routeID {
			return true
		}
	}

	return false
}

func (dec *DeleteEKSCluster) DeleteCluster(ctx context.Context) error {
	if err := dec.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{dec.ManagedServiceProvider}, dec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(dec.ManagedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	cluster, err := eksSession.GetEKSCluster(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch cluster details %s: %w", dec.ClusterName, err)
	}

	oidcARN, err := eksSession.FetchOidcArnFromURL(ctx, cluster.Identity.Oidc.Issuer)
	if err != nil {
		return fmt.Errorf("unable to fetch oidc arn from url: %w", err)
	}

	if err := eksSession.DeleteOidcProvider(ctx, oidcARN); err != nil {
		return fmt.Errorf("unable to delete oidc provider: %w", err)
	}

	logrus.Infof("Deleted oidc provider %s from cluster %s", *cluster.Identity.Oidc.Issuer, dec.ClusterName)

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch node groups for cluster %s: %w", dec.ClusterName, err)
	}

	for _, nodeGroup := range nodeGroups {
		if err = eksSession.DeleteNodeGroup(ctx, *nodeGroup.NodegroupName, true); err != nil {
			return fmt.Errorf("unable to delete node group %s of cluster %s: %w", *nodeGroup.NodegroupName, dec.ClusterName, err)
		}

		logrus.Infof("Deleted node group %s from cluster %s", *nodeGroup.NodegroupName, dec.ClusterName)

		parts := strings.Split(*nodeGroup.NodeRole, "/")
		roleName := &parts[len(parts)-1]

		if err = eksSession.DeleteIAMRole(ctx, roleName); err != nil {
			return fmt.Errorf("unable to delete iam %s for node group %s of cluster %s: %w", *nodeGroup.NodeRole, *nodeGroup.NodegroupName, dec.ClusterName, err)
		}

		logrus.Infof("Deleted IAM role %s of node group %s from cluster %s", *nodeGroup.NodeRole, *nodeGroup.NodegroupName, dec.ClusterName)
	}

	if err = eksSession.DeleteCluster(ctx, true); err != nil {
		return fmt.Errorf("unable to delete cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Infof("Deleted cluster %s", dec.ClusterName)

	if err = eksSession.DeleteSecurityGroups(ctx, cluster.ResourcesVpcConfig.SecurityGroupIds); err != nil {
		return fmt.Errorf("unable to delete security group for cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Infof("Deleted security groups from cluster %s", dec.ClusterName)

	igw, err := eksSession.GetInternetGatewayForVPC(ctx, cluster.ResourcesVpcConfig.VpcId)
	if err != nil {
		return fmt.Errorf("unable to find internet gateway attached to vpc: %w", err)
	}

	if err := eksSession.DetachInternetGateway(ctx, cluster.ResourcesVpcConfig.VpcId, igw.InternetGatewayId); err != nil {
		return fmt.Errorf("unable to detach internet gateway: %w", err)
	}

	logrus.Info("Detached Internet Gateway from vpc")

	if err := eksSession.DeleteInternetGateway(ctx, igw.InternetGatewayId); err != nil {
		return fmt.Errorf("unable to delete internet gateway: %w", err)
	}

	logrus.Info("Deleted Internet Gateway")

	var routes []ec2types.RouteTable

	for _, subnetID := range cluster.ResourcesVpcConfig.SubnetIds {
		routeTables, err := eksSession.GetRouteTablesForSubnet(ctx, &subnetID)
		if err != nil {
			return fmt.Errorf("unable to find route tables for subnet: %w", err)
		}

		for _, routeTable := range routeTables {
			if !containsRoute(routes, *routeTable.RouteTableId) {
				routes = append(routes, routeTable)
			}

			association, err := eksSession.GetRouteTableAssociation(ctx, routeTable.RouteTableId, &subnetID)
			if err != nil {
				return fmt.Errorf("unable to find association with route table and subnet: %w", err)
			}

			if err := eksSession.DissociateRouteTable(ctx, association.RouteTableAssociationId); err != nil {
				return fmt.Errorf("unable to find association with route table and subnet: %w", err)
			}
		}
	}

	for _, routeTable := range routes {
		if err = eksSession.DeleteRoute(ctx, routeTable.RouteTableId); err != nil {
			return fmt.Errorf("unable to delete route table and subnet: %w", err)
		}

		if err := eksSession.DeleteRouteTable(ctx, routeTable.RouteTableId); err != nil {
			return fmt.Errorf("unable to delete route table: %w", err)
		}
	}

	logrus.Info("Dissociated route tables from subnets and deleted route tables")

	if err = eksSession.DeleteSubnets(ctx, cluster.ResourcesVpcConfig.SubnetIds); err != nil {
		return fmt.Errorf("unable to delete subnets for cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Infof("Deleted subnets from cluster %s", dec.ClusterName)

	if err = eksSession.DeleteVpc(ctx, cluster.ResourcesVpcConfig.VpcId); err != nil {
		return fmt.Errorf("unable to delete vpc for cluster %s: %w", dec.ClusterName, err)
	}

	logrus.Infof("Deleted vpc from cluster %s", dec.ClusterName)

	rolesToDelete := []string{dec.ClusterName + "-eks-role", dec.ClusterName + "-eks-ebscsidriver-role"}
	for _, roleName := range rolesToDelete {
		if err = eksSession.DeleteIAMRole(ctx, &roleName); err != nil {
			// The policy clustername-ebs-csidriver-role is only created sometimes
			// TODO : Investigate why and when this role is created and fix the code accordingly
			// return fmt.Errorf("unable to delete iam role %s: %w", roleName, err)
			logrus.Errorf("unable to delete iam role %s", roleName)
		}

		logrus.Infof("Deleted IAM role %s", roleName)
	}

	policiesToDelete := []string{dec.ClusterName + "-ebs-csi-driver-policy"}
	for _, policyName := range policiesToDelete {
		if err = eksSession.DeletePolicy(ctx, &policyName); err != nil {
			// The policy clustername-ebs-csi-driver-policy is only created sometimes
			// TODO : Investigate why and when this policy is created and fix the code accordingly
			// return fmt.Errorf("unable to delete iam policy %s: %w", policyName, err)
			logrus.Errorf("unable to delete iam policy %s", policyName)
		}

		logrus.Infof("Deleted IAM policy %s", policyName)
	}

	contextName := fmt.Sprintf("eks-%s@%s", dec.ClusterName, dec.Region)
	if err := kubectl.DeleteContext(contextName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("delete eks cluster: %w", err)
	}

	logrus.Infof("Deleted context %s from kubeconfig", contextName)

	if err := kubectl.DeleteCluster(dec.ClusterName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("delete eks cluster: %w", err)
	}

	logrus.Infof("Deleted cluster %s from kubeconfig", dec.ClusterName)

	if err := kubectl.DeleteUser(dec.ClusterName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("delete eks cluster: %w", err)
	}

	logrus.Infof("Deleted user %s from kubeconfig", dec.ClusterName)

	if _, _, err := kubectl.UnsetCurrentContext().Exec(false, false); err != nil {
		return fmt.Errorf("delete eks cluster: %w", err)
	}

	logrus.Info("Unset the current-context in kubeconfig")

	return nil
}

func (dec *DeleteEKSCluster) ValidateParams(ctx context.Context) error {
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{dec.ManagedServiceProvider}, dec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(dec.ManagedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	if _, err := eksSession.GetEKSCluster(ctx); err != nil {
		return fmt.Errorf("unable to fetch cluster details %s: %w", dec.ClusterName, err)
	}

	return nil
}
