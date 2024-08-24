package setupkubernetes

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

var (
	ErrEKSClusterAlreadyExists       = errors.New("eks cluster already exists")
	ErrEKSSecurityGroupAlreadyExists = errors.New("eks security group already exists")
	ErrInvalidAmiType                = errors.New("invalid ami type")
	ErrNumNodeGroupsInvalid          = errors.New("for environment type 'cloud' and provider 'aws', NumNodeGroups must be greater than 0")
	ErrDesiredSizeInvalid            = errors.New("for environment type 'cloud' and provider 'aws', DesiredSize must be greater than 0")
	ErrMinSizeInvalid                = errors.New("for environment type 'cloud' and provider 'aws', MinSize must be greater than 0")
	ErrMaxSizeInvalid                = errors.New("for environment type 'cloud' and provider 'aws', MaxSize must be greater than 0")
	ErrDiskSizeInvalid               = errors.New("for environment type 'cloud' and provider 'aws', DiskSize must be greater than 0")
)

const (
	vpcCIDR             = "10.0.0.0/16"
	subnetCIDR1         = "10.0.1.0/24"
	subnetCIDR2         = "10.0.2.0/24"
	subnetCIDR3         = "10.0.3.0/24"
	sshKey              = "qe-cao-testrunner"
	ebscsidriverVersion = "v1.17.0-eksbuild.1"
)

type CreateEKSCluster struct {
	ClusterName       string
	Region            string
	KubernetesVersion string
	InstanceType      string
	NumNodeGroups     int
	MinSize           int
	MaxSize           int
	DesiredSize       int
	DiskSize          int
	AMI               managedk8sservices.AMIType
	KubeConfigPath    string
}

func (cec *CreateEKSCluster) CreateCluster() error {
	if err := cec.ValidateParams(); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.EKSManagedService}, cec.ClusterName)
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

	vpc, err := eksSession.CreateVPC(vpcCIDR)
	if err != nil {
		return fmt.Errorf("error creating vpc: %w", err)
	}

	logrus.Info("Created VPC for the cluster")

	azs := []string{cec.Region + "a", cec.Region + "b", cec.Region + "c"}
	cidrs := []string{subnetCIDR1, subnetCIDR2, subnetCIDR3}

	subnets, err := eksSession.CreateSubnets(*vpc.VpcId, azs, cidrs)
	if err != nil {
		return fmt.Errorf("error creating subnets in the vpc: %w", err)
	}

	logrus.Info("Created subnets for the cluster")

	igw, err := eksSession.CreateInternetGateway()
	if err != nil {
		return fmt.Errorf("error trying to create internet gateway: %w", err)
	}

	logrus.Info("Created Internet Gateway for the VPC")

	if err := eksSession.AttachInternetGateway(*vpc.VpcId, *igw.InternetGatewayId); err != nil {
		return fmt.Errorf("error trying to attach internet gateway to vpc: %w", err)
	}

	logrus.Info("Attached Internet gateway to VPC")

	rt, err := eksSession.CreateRouteTable(*vpc.VpcId, *igw.InternetGatewayId)
	if err != nil {
		return fmt.Errorf("error trying to create route table with route to internet gateway: %w", err)
	}

	logrus.Info("Created route table for the subnet")

	subnetIds := make([]string, len(subnets))
	for i, subnet := range subnets {
		subnetIds[i] = *subnet.SubnetId
		if err := eksSession.EnableAutoAssignPublicIP(subnetIds[i]); err != nil {
			return fmt.Errorf("error enabling auto assign public ip: %w", err)
		}

		if err := eksSession.AssociateRouteTable(*rt.RouteTableId, subnetIds[i]); err != nil {
			return fmt.Errorf("error trying to associate route table to subnet: %w", err)
		}
	}

	logrus.Info("Attached route table to the subnet and allowed auto assign public ip to subnets")

	securityGroupName := cec.ClusterName + "-security-group"

	securityGroup, err := eksSession.CreateSecurityGroup(*vpc.VpcId, securityGroupName)
	if err != nil {
		return fmt.Errorf("error creating security group %s: %w", securityGroupName, err)
	}

	logrus.Info("Created Security groups for the cluster")

	cluster, err := eksSession.CreateEKSCluster(cec.KubernetesVersion, subnetIds,
		[]string{*securityGroup.GroupId}, true)
	if err != nil {
		return fmt.Errorf("unable to create eks cluster %s: %w", cec.ClusterName, err)
	}

	logrus.Info("Created EKS Cluster")

	for i := 0; i < cec.NumNodeGroups; i++ {
		nodeGroupName := fmt.Sprintf("%s-node-group-%d", cec.ClusterName, i)
		if _, err = eksSession.CreateNodeGroup(cec.InstanceType, nodeGroupName,
			sshKey, subnetIds, []string{*securityGroup.GroupId}, int64(cec.MinSize), int64(cec.DesiredSize),
			int64(cec.MaxSize), int64(cec.DiskSize), cec.AMI, true); err != nil {
			return fmt.Errorf("unable to create node group %s: %w", nodeGroupName, err)
		}
	}

	logrus.Info("Created Node groups for the cluster")

	if _, err = eksSession.EnableEBSCSIDriverAddon(ebscsidriverVersion); err != nil {
		return fmt.Errorf("unable to create ebs csi drive add on for eks cluster %s: %w",
			cec.ClusterName, err)
	}

	logrus.Info("Enabled EBSCSI Driver for the cluster")

	if err = cec.updateKubeconfig(cluster, cec.Region); err != nil {
		return err
	}

	logrus.Info("Updated kubeconfig with the cluster details")

	return nil
}

func (cec *CreateEKSCluster) updateKubeconfig(cluster *eks.Cluster, region string) error {
	/*
			 requirement for this to work

			 For mac
			 brew install aws-iam-authenticator

			 For linux
			 curl -o aws-iam-authenticator https://amazon-eks.s3.us-west-2.amazonaws.com/1.21.9/2022-02-01/bin/linux/amd64/aws-iam-authenticator
			 chmod +x ./aws-iam-authenticator
		     sudo mv ./aws-iam-authenticator /usr/local/bin/
	*/
	caData, err := base64.StdEncoding.DecodeString(aws.StringValue(cluster.CertificateAuthority.Data))
	if err != nil {
		return fmt.Errorf("failed to decode certificate authority data: %w", err)
	}

	clusterName := aws.StringValue(cluster.Name)
	clusterConfig := &api.Cluster{
		Server:                   aws.StringValue(cluster.Endpoint),
		CertificateAuthorityData: caData,
	}

	contextName := fmt.Sprintf("eks-%s@%s", clusterName, region)
	contextConfig := &api.Context{
		Cluster:  clusterName,
		AuthInfo: clusterName,
	}

	userConfig := &api.AuthInfo{
		Exec: &api.ExecConfig{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Command:    "aws-iam-authenticator",
			Args:       []string{"token", "-i", clusterName},
		},
	}

	config := api.NewConfig()
	config.Clusters[clusterName] = clusterConfig
	config.Contexts[contextName] = contextConfig
	config.AuthInfos[clusterName] = userConfig
	config.CurrentContext = contextName

	if err = clientcmd.WriteToFile(*config, cec.KubeConfigPath); err != nil {
		return fmt.Errorf("failed to write kubeconfig file: %w", err)
	}

	return nil
}

func (cec *CreateEKSCluster) ValidateParams() error {
	if cec.NumNodeGroups <= 0 {
		return ErrNumNodeGroupsInvalid
	}

	if cec.DesiredSize <= 0 {
		return ErrDesiredSizeInvalid
	}

	if cec.MinSize <= 0 {
		return ErrMinSizeInvalid
	}

	if cec.MaxSize <= 0 {
		return ErrMaxSizeInvalid
	}

	if cec.DiskSize <= 0 {
		return ErrDiskSizeInvalid
	}

	if ok, err := managedk8sservices.ValidateAMIType(cec.AMI); !ok || err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.EKSManagedService}, cec.ClusterName)
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

	securityGroupName := cec.ClusterName + "-security-group"
	if _, err := eksSession.GetSecurityGroupsByGroupNames([]*string{&securityGroupName}); err == nil {
		return fmt.Errorf("security group %s already exists: %w", securityGroupName, ErrEKSSecurityGroupAlreadyExists)
	}

	if _, err := eksSession.GetEKSCluster(); err == nil {
		return fmt.Errorf("eks cluster %s already exists: %w", cec.ClusterName, ErrEKSClusterAlreadyExists)
	}

	return nil
}
