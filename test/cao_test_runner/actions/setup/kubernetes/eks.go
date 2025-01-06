package setupkubernetes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
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
	ErrKubeconfigFileInvalid         = errors.New("kubeconfig file does not exist")
)

const (
	vpcCIDR             = "10.0.0.0/16"
	subnetCIDR1         = "10.0.1.0/24"
	subnetCIDR2         = "10.0.2.0/24"
	subnetCIDR3         = "10.0.3.0/24"
	sshKey              = "qe-cao-testrunner"
	ebscsidriverVersion = "v1.35.0-eksbuild.1"
)

type CreateEKSCluster struct {
	ClusterName            string
	Region                 string
	KubernetesVersion      string
	InstanceType           string
	NumNodeGroups          int
	MinSize                int
	MaxSize                int
	DesiredSize            int
	DiskSize               int
	AMI                    ekstypes.AMITypes
	KubeConfigPath         string
	ManagedServiceProvider *managedk8sservices.ManagedServiceProvider
}

func (cec *CreateEKSCluster) CreateCluster(ctx context.Context) error {
	if err := cec.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{cec.ManagedServiceProvider}, cec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(cec.ManagedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	vpc, err := eksSession.CreateVPC(ctx, vpcCIDR)
	if err != nil {
		return fmt.Errorf("error creating vpc: %w", err)
	}

	logrus.Info("Created VPC for the cluster")

	azs := []string{cec.Region + "a", cec.Region + "b", cec.Region + "c"}
	cidrs := []string{subnetCIDR1, subnetCIDR2, subnetCIDR3}

	subnets, err := eksSession.CreateSubnets(ctx, *vpc.VpcId, azs, cidrs)
	if err != nil {
		return fmt.Errorf("error creating subnets in the vpc: %w", err)
	}

	logrus.Info("Created subnets for the cluster")

	igw, err := eksSession.CreateInternetGateway(ctx)
	if err != nil {
		return fmt.Errorf("error trying to create internet gateway: %w", err)
	}

	logrus.Info("Created Internet Gateway for the VPC")

	if err := eksSession.AttachInternetGateway(ctx, *vpc.VpcId, *igw.InternetGatewayId); err != nil {
		return fmt.Errorf("error trying to attach internet gateway to vpc: %w", err)
	}

	logrus.Info("Attached Internet gateway to VPC")

	rt, err := eksSession.CreateRouteTable(ctx, *vpc.VpcId, *igw.InternetGatewayId)
	if err != nil {
		return fmt.Errorf("error trying to create route table with route to internet gateway: %w", err)
	}

	logrus.Info("Created route table for the subnet")

	subnetIds := make([]string, len(subnets))
	for i, subnet := range subnets {
		subnetIds[i] = *subnet.SubnetId
		if err := eksSession.EnableAutoAssignPublicIP(ctx, subnetIds[i]); err != nil {
			return fmt.Errorf("error enabling auto assign public ip: %w", err)
		}

		if err := eksSession.AssociateRouteTable(ctx, *rt.RouteTableId, subnetIds[i]); err != nil {
			return fmt.Errorf("error trying to associate route table to subnet: %w", err)
		}
	}

	logrus.Info("Attached route table to the subnet and allowed auto assign public ip to subnets")

	securityGroupName := cec.ClusterName + "-security-group"

	securityGroup, err := eksSession.CreateSecurityGroup(ctx, *vpc.VpcId, securityGroupName)
	if err != nil {
		return fmt.Errorf("error creating security group %s: %w", securityGroupName, err)
	}

	logrus.Info("Created Security groups for the cluster")

	cluster, err := eksSession.CreateEKSCluster(ctx, cec.KubernetesVersion, subnetIds,
		[]string{*securityGroup.GroupId}, true)
	if err != nil {
		return fmt.Errorf("unable to create eks cluster %s: %w", cec.ClusterName, err)
	}

	logrus.Info("Created EKS Cluster")

	for i := 0; i < cec.NumNodeGroups; i++ {
		nodeGroupName := fmt.Sprintf("%s-node-group-%d", cec.ClusterName, i)
		if _, err = eksSession.CreateNodeGroup(ctx, cec.InstanceType, nodeGroupName,
			sshKey, subnetIds, []string{*securityGroup.GroupId}, int32(cec.MinSize), int32(cec.DesiredSize),
			int32(cec.MaxSize), int32(cec.DiskSize), cec.AMI, true); err != nil {
			return fmt.Errorf("unable to create node group %s: %w", nodeGroupName, err)
		}
	}

	logrus.Info("Created Node groups for the cluster")

	if err := eksSession.CreateOidcProvider(ctx, cluster.Identity.Oidc.Issuer, []string{"sts.amazonaws.com"}); err != nil {
		return fmt.Errorf("unable to create oidc provider: %w", err)
	}

	logrus.Info("Created OIDC Provider for the cluster")

	if _, err = eksSession.EnableEBSCSIDriverAddon(ctx, ebscsidriverVersion, *cluster.Identity.Oidc.Issuer); err != nil {
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

func (cec *CreateEKSCluster) updateKubeconfig(cluster *ekstypes.Cluster, region string) error {
	/*
			 requirement for this to work

			 For mac
			 brew install aws-iam-authenticator

			 For linux
			 curl -o aws-iam-authenticator https://amazon-eks.s3.us-west-2.amazonaws.com/1.21.9/2022-02-01/bin/linux/amd64/aws-iam-authenticator
			 chmod +x ./aws-iam-authenticator
		     sudo mv ./aws-iam-authenticator /usr/local/bin/
	*/
	if !fileutils.NewFile(cec.KubeConfigPath).IsFileExists() {
		return fmt.Errorf("kubeconfig path %s does not exist: %w", cec.KubeConfigPath, ErrKubeconfigFileInvalid)
	}

	kubeconfig, err := clientcmd.LoadFromFile(cec.KubeConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load existing kubeconfig file: %w", err)
	}

	caData, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
	if err != nil {
		return fmt.Errorf("failed to decode certificate authority data: %w", err)
	}

	clusterConfig := &api.Cluster{
		Server:                   *cluster.Endpoint,
		CertificateAuthorityData: caData,
	}

	contextName := fmt.Sprintf("eks-%s@%s", *cluster.Name, region)
	contextConfig := &api.Context{
		Cluster:  *cluster.Name,
		AuthInfo: *cluster.Name,
	}

	userConfig := &api.AuthInfo{
		Exec: &api.ExecConfig{
			APIVersion: "client.authentication.k8s.io/v1beta1",
			Command:    "aws-iam-authenticator",
			Args:       []string{"token", "-i", *cluster.Name},
		},
	}

	if kubeconfig.Clusters == nil {
		kubeconfig.Clusters = make(map[string]*api.Cluster)
	}

	if kubeconfig.Contexts == nil {
		kubeconfig.Contexts = make(map[string]*api.Context)
	}

	if kubeconfig.AuthInfos == nil {
		kubeconfig.AuthInfos = make(map[string]*api.AuthInfo)
	}

	kubeconfig.Clusters[*cluster.Name] = clusterConfig
	kubeconfig.Contexts[contextName] = contextConfig
	kubeconfig.AuthInfos[*cluster.Name] = userConfig
	kubeconfig.CurrentContext = contextName

	if err = clientcmd.WriteToFile(*kubeconfig, cec.KubeConfigPath); err != nil {
		return fmt.Errorf("failed to write kubeconfig file: %w", err)
	}

	return nil
}

func (cec *CreateEKSCluster) ValidateParams(ctx context.Context) error {
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
		[]*managedk8sservices.ManagedServiceProvider{cec.ManagedServiceProvider}, cec.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(cec.ManagedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set eks session: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get eks session: %w", err)
	}

	securityGroupName := cec.ClusterName + "-security-group"
	if _, err := eksSession.GetSecurityGroupsByGroupNames(ctx, []string{securityGroupName}); err == nil {
		return fmt.Errorf("security group %s already exists: %w", securityGroupName, ErrEKSSecurityGroupAlreadyExists)
	}

	if _, err := eksSession.GetEKSCluster(ctx); err == nil {
		return fmt.Errorf("eks cluster %s already exists: %w", cec.ClusterName, ErrEKSClusterAlreadyExists)
	}

	if !fileutils.NewFile(cec.KubeConfigPath).IsFileExists() {
		return fmt.Errorf("kubeconfig path %s does not exist: %w", cec.KubeConfigPath, ErrKubeconfigFileInvalid)
	}

	return nil
}
