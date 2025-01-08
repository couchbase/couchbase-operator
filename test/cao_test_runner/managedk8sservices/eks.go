package managedk8sservices

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

var (
	ErrASGNotFound              = errors.New("asg not found")
	ErrInvalidAmiType           = errors.New("invalid ami type")
	ErrSecurityGroupNotFound    = errors.New("security group not found")
	ErrTimeoutClusterDeletion   = errors.New("timeout reached while waiting for cluster to be deleted")
	ErrTimeoutNodeGroupDeletion = errors.New("timeout reached while waiting for node group to be deleted")
	ErrNoInternetGatewaysFound  = errors.New("no internet gateways found for vpc")
	ErrNoRouteTablesFound       = errors.New("no route tables found for subnet")
	ErrNoAssociationsFound      = errors.New("no associations between subnet and route table found")
	ErrInvalidOIDURL            = errors.New("invalid oid url")
	ErrInvalidPolicy            = errors.New("invalid policy")
	ErrTagKeyInvalid            = errors.New("invalid tag key")
	ErrTagValueInvalid          = errors.New("invalid tag value")
)

// EKSSessionStore implements ManagedService interface.
/*
 * EKSSessionStore stores multiple EKSSession for each different cluster.
 * To get EKSSession for an EKS cluster, use `eks-cluster-name`+ `:` + `region` as the key for EKSSessions map.
 * E.g. of key: eksClusterName:us-east-2.
 */
type EKSSessionStore struct {
	EKSSessions map[string]*EKSSession
	lock        sync.Mutex
}

// EKSSession holds all the required aws clients and sessions required to perform actions on an EKS Cluster.
/*
 * EKSSession holds the information for one EKS Cluster.
 */
type EKSSession struct {
	eksClient   *eks.Client
	asgClient   *autoscaling.Client
	ec2Client   *ec2.Client
	iamClient   *iam.Client
	cred        *ManagedServiceCredentials
	region      string
	clusterName string
}

// NewEKSSession initializes a new EKSSession with the provided AWS credentials and region.
func NewEKSSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) (*EKSSession, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(managedSvcCred.EKSCredentials.eksRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(managedSvcCred.EKSCredentials.eksAccessKey, managedSvcCred.EKSCredentials.eksSecretKey, "")))
	if err != nil {
		return nil, fmt.Errorf("create new EKSSession: %w", err)
	}

	return &EKSSession{
		eksClient:   eks.NewFromConfig(cfg),
		asgClient:   autoscaling.NewFromConfig(cfg),
		ec2Client:   ec2.NewFromConfig(cfg),
		iamClient:   iam.NewFromConfig(cfg),
		cred:        managedSvcCred,
		region:      managedSvcCred.EKSCredentials.eksRegion,
		clusterName: managedSvcCred.ClusterName,
	}, nil
}

// ConfigEKSSessionStore initialises EKSSessionStore.
func ConfigEKSSessionStore() ManagedService {
	return &EKSSessionStore{
		EKSSessions: make(map[string]*EKSSession),
		lock:        sync.Mutex{},
	}
}

func NewEKSSessionStore() *EKSSessionStore {
	return &EKSSessionStore{
		EKSSessions: make(map[string]*EKSSession),
		lock:        sync.Mutex{},
	}
}

// GetKey returns the key for the map EKSSessionStore.EKSSessions to retrieve the appropriate EKSSession for the cluster.
// Key is built using ClusterName+Region. E.g. eksClusterName:us-east-2.
func getEKSKey(managedSvcCred *ManagedServiceCredentials) string {
	key := managedSvcCred.ClusterName + ":" + managedSvcCred.EKSCredentials.eksRegion
	return key
}

func ValidateAMIType(ami ekstypes.AMITypes) (bool, error) {
	if slices.Contains(ami.Values(), ami) {
		return true, nil
	}
	return false, fmt.Errorf("invalid ami type %s: %w", ami, ErrInvalidAmiType)
}

// SetSession adds an EKS cluster to the EKSSessionStore.
// It creates a new EKSSession for the provided EKS cluster.
func (ess *EKSSessionStore) SetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error {
	defer ess.lock.Unlock()
	ess.lock.Lock()

	if _, ok := ess.EKSSessions[getEKSKey(managedSvcCred)]; !ok {
		eksSess, err := NewEKSSession(ctx, managedSvcCred)
		if err != nil {
			return fmt.Errorf("set eks session store: %w", err)
		}

		ess.EKSSessions[getEKSKey(managedSvcCred)] = eksSess
	}

	return nil
}

// GetSession returns the EKSSession for an EKS Cluster.
// If EKSSession for the cluster is not present in EKSSessionStore then it sets it.
func (ess *EKSSessionStore) GetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) (*EKSSession, error) {
	if _, ok := ess.EKSSessions[getEKSKey(managedSvcCred)]; !ok {
		err := ess.SetSession(ctx, managedSvcCred)
		if err != nil {
			return nil, fmt.Errorf("get eks session: %w", err)
		}
	}

	return ess.EKSSessions[getEKSKey(managedSvcCred)], nil
}

// Check checks if the k8s cluster in EKS is accessible or not.
func (ess *EKSSessionStore) Check(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error {
	eksSession, err := ess.GetSession(ctx, managedSvcCred)
	if err != nil {
		return fmt.Errorf("check eks cluster accessibility: %w", err)
	}

	_, err = eksSession.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(eksSession.clusterName),
	})
	if err != nil {
		return fmt.Errorf("check eks cluster accessibility: %w", err)
	}

	return nil
}

// GetInstancesByK8sNodeName gets the ec2 instance ids for the provided kubernetes node names.
func (ess *EKSSessionStore) GetInstancesByK8sNodeName(ctx context.Context, managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error) {
	eksSession, err := ess.GetSession(ctx, managedSvcCred)
	if err != nil {
		return []string{}, fmt.Errorf("get ec2 instance by k8s node name: %w", err)
	}

	eksASG, err := eksSession.GetAutoscalingGroupsForCluster(ctx)
	if err != nil {
		return []string{}, fmt.Errorf("get ec2 instance by k8s node name: %w", err)
	}

	var instances []ec2types.Instance

	for _, asg := range eksASG {
		ins, err := eksSession.GetInstancesInAutoscalingGroup(ctx, *asg.AutoScalingGroupName)
		if err != nil {
			return []string{}, fmt.Errorf("get ec2 instance by k8s node name: %w", err)
		}

		instances = append(instances, ins...)
	}

	var instanceIDs []string

	// Getting the required instance IDs. In EKS the K8S node name is set as the ec2 instance Private DNS Name
	for _, instance := range instances {
		for _, nodeName := range nodeNames {
			if nodeName == *instance.PrivateDnsName {
				instanceIDs = append(instanceIDs, *instance.InstanceId)
			}
		}
	}

	return instanceIDs, nil
}

// ================================================
// ====== Methods implemented by EKSSession ======
// ================================================

// GetEKSCluster returns *eks.Cluster which can be used to retrieve information about a specific EKS cluster.
func (es *EKSSession) GetEKSCluster(ctx context.Context) (*ekstypes.Cluster, error) {
	input := &eks.DescribeClusterInput{
		Name: aws.String(es.clusterName),
	}

	result, err := es.eksClient.DescribeCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("get eks cluster: %w", err)
	}

	return result.Cluster, nil
}

// GetNodegroupsForCluster retrieves the nodegroups for an EKS cluster.
func (es *EKSSession) GetNodegroupsForCluster(ctx context.Context) ([]*ekstypes.Nodegroup, error) {
	input := &eks.ListNodegroupsInput{
		ClusterName: aws.String(es.clusterName),
	}

	result, err := es.eksClient.ListNodegroups(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("get nodegroups: %w", err)
	}

	nodegroups := make([]*ekstypes.Nodegroup, 0)

	for _, ngName := range result.Nodegroups {
		ng, err := es.eksClient.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
			ClusterName:   aws.String(es.clusterName),
			NodegroupName: &ngName,
		})
		if err != nil {
			return nil, fmt.Errorf("get nodegroup %s: %w", ngName, err)
		}

		nodegroups = append(nodegroups, ng.Nodegroup)
	}

	return nodegroups, nil
}

// ModifyNodegroup modifies a nodegroup of the EKS cluster.
func (es *EKSSession) ModifyNodegroup(ctx context.Context,
	nodegroupName string, minSize, maxSize, desiredSize int32) error {
	input := &eks.UpdateNodegroupConfigInput{
		ClusterName:   aws.String(es.clusterName),
		NodegroupName: aws.String(nodegroupName),
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			MinSize:     &minSize,
			MaxSize:     &maxSize,
			DesiredSize: &desiredSize,
		},
	}

	_, err := es.eksClient.UpdateNodegroupConfig(ctx, input)
	if err != nil {
		return fmt.Errorf("update nodegroup %s: %w", nodegroupName, err)
	}

	return nil
}

// GetAutoscalingGroupsForCluster retrieves all autoscaling groups associated with the EKS cluster.
func (es *EKSSession) GetAutoscalingGroupsForCluster(ctx context.Context) ([]autoscalingtypes.AutoScalingGroup, error) {
	// First, get all nodegroups in the cluster
	nodegroups, err := es.GetNodegroupsForCluster(ctx)
	if err != nil {
		return nil, fmt.Errorf("get asg for eks cluster: %w", err)
	}

	// Collect all ASG names from all nodegroups
	var asgNames []string

	for _, ng := range nodegroups {
		if ng != nil {
			for _, asg := range ng.Resources.AutoScalingGroups {
				asgNames = append(asgNames, *asg.Name)
			}
		}
	}

	// If no ASGs found, return empty slice
	if len(asgNames) == 0 {
		return []autoscalingtypes.AutoScalingGroup{}, nil
	}

	// Describe all collected ASGs
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: asgNames,
	}

	result, err := es.asgClient.DescribeAutoScalingGroups(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe autoscaling groups: %w", err)
	}

	return result.AutoScalingGroups, nil
}

// GetAutoscalingGroupsForNodegroup retrieves the autoscaling groups for a nodegroup.
func (es *EKSSession) GetAutoscalingGroupsForNodegroup(ctx context.Context, nodegroupName string) ([]autoscalingtypes.AutoScalingGroup, error) {
	// First, get the nodegroup details
	ngInput := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(es.clusterName),
		NodegroupName: aws.String(nodegroupName),
	}

	ngResult, err := es.eksClient.DescribeNodegroup(ctx, ngInput)
	if err != nil {
		return nil, fmt.Errorf("get asg for nodegroup: describe nodegroup %s: %w", nodegroupName, err)
	}

	// Get the ASG names from the nodegroup resources
	asgNames := make([]string, 0)
	for _, resource := range ngResult.Nodegroup.Resources.AutoScalingGroups {
		asgNames = append(asgNames, *resource.Name)
	}

	// Describe the ASGs
	asgInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: asgNames,
	}

	asgResult, err := es.asgClient.DescribeAutoScalingGroups(ctx, asgInput)
	if err != nil {
		return nil, fmt.Errorf("describe autoscaling groups: %w", err)
	}

	return asgResult.AutoScalingGroups, nil
}

// GetInstancesInAutoscalingGroup retrieves details of instances in the specified autoscaling group.
func (es *EKSSession) GetInstancesInAutoscalingGroup(ctx context.Context, asgName string) ([]ec2types.Instance, error) {
	// Describe the autoscaling group to get instance IDs
	asgInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{asgName},
	}

	asgResult, err := es.asgClient.DescribeAutoScalingGroups(ctx, asgInput)
	if err != nil {
		return nil, fmt.Errorf("describe autoscaling group %s: %w", asgName, err)
	}

	if len(asgResult.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("autoscaling group %s: %w", asgName, ErrASGNotFound)
	}

	// Getting the first autoscaling group
	asg := asgResult.AutoScalingGroups[0]

	// Get the instance IDs
	var instanceIds []string
	for _, instance := range asg.Instances {
		instanceIds = append(instanceIds, *instance.InstanceId)
	}

	if len(instanceIds) == 0 {
		return []ec2types.Instance{}, nil // Return empty slice if no instances
	}

	// Describe EC2 instances to get more details
	ec2Input := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIds,
	}

	ec2Result, err := es.ec2Client.DescribeInstances(ctx, ec2Input)
	if err != nil {
		return nil, fmt.Errorf("describe EC2 instances: %w", err)
	}

	var resultInstances []ec2types.Instance

	for _, reservation := range ec2Result.Reservations {
		resultInstances = append(resultInstances, reservation.Instances...)
	}

	return resultInstances, nil
}

func (es *EKSSession) CreateVPC(ctx context.Context, cidrBlock, vpcName string) (*ec2types.Vpc, error) {
	result, err := es.ec2Client.CreateVpc(
		ctx,
		&ec2.CreateVpcInput{
			CidrBlock: aws.String(cidrBlock),
		})
	if err != nil {
		return nil, fmt.Errorf("create VPC: %w", err)
	}

	// Naming the VPC.
	err = es.addTagsToResource(ctx, *result.Vpc.VpcId, map[string]string{"Name": vpcName})
	if err != nil {
		return nil, fmt.Errorf("tag VPC: %w", err)
	}

	return result.Vpc, nil
}

func (es *EKSSession) CreateSubnets(ctx context.Context, vpcID string, azs []string, cidrs []string) ([]*ec2types.Subnet, error) {
	var subnets []*ec2types.Subnet

	for i, cidr := range cidrs {
		result, err := es.ec2Client.CreateSubnet(
			ctx,
			&ec2.CreateSubnetInput{
				CidrBlock:        aws.String(cidr),
				VpcId:            aws.String(vpcID),
				AvailabilityZone: aws.String(azs[i]),
			})
		if err != nil {
			return nil, fmt.Errorf("create subnets: %w", err)
		}

		subnets = append(subnets, result.Subnet)
	}

	return subnets, nil
}

func (es *EKSSession) CreateSecurityGroup(ctx context.Context, vpcID, groupName string) (*ec2types.SecurityGroup, error) {
	result, err := es.ec2Client.CreateSecurityGroup(
		ctx,
		&ec2.CreateSecurityGroupInput{
			GroupName:   aws.String(groupName),
			Description: aws.String("Security group for EKS cluster"),
			VpcId:       aws.String(vpcID),
		})
	if err != nil {
		return nil, fmt.Errorf("create security group %s: %w", groupName, err)
	}

	// TODO : Take the ip permissions as params
	_, err = es.ec2Client.AuthorizeSecurityGroupIngress(
		ctx,
		&ec2.AuthorizeSecurityGroupIngressInput{
			GroupId: aws.String(*result.GroupId),
			IpPermissions: []ec2types.IpPermission{
				{
					IpProtocol: aws.String("-1"),
					FromPort:   aws.Int32(0),
					ToPort:     aws.Int32(0),
					IpRanges: []ec2types.IpRange{
						{
							CidrIp: aws.String("0.0.0.0/0"),
						},
					},
				},
			},
		})
	if err != nil {
		return nil, fmt.Errorf("failed to authorize ingress: %w", err)
	}

	// TODO : Take the ip permissions as params
	// _, err = es.EC2Client.AuthorizeSecurityGroupEgress(&ec2.AuthorizeSecurityGroupEgressInput{
	// 	GroupId: aws.String(*result.GroupId),
	// 	IpPermissions: []*ec2.IpPermission{
	// 		{
	// 			IpProtocol: aws.String("-1"),
	// 			FromPort:   aws.Int64(0),
	// 			ToPort:     aws.Int64(0),
	// 			IpRanges: []*ec2.IpRange{
	// 				{
	// 					CidrIp: aws.String("0.0.0.0/0"),
	// 				},
	// 			},
	// 		},
	// 	},
	// })
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to authorize egress: %w", err)
	// }

	describeResult, err := es.ec2Client.DescribeSecurityGroups(
		ctx,
		&ec2.DescribeSecurityGroupsInput{
			GroupIds: []string{*result.GroupId},
		})
	if err != nil {
		return nil, fmt.Errorf("could not describe security group: %w", err)
	}

	if len(describeResult.SecurityGroups) > 0 {
		return &describeResult.SecurityGroups[0], nil
	}

	return nil, ErrSecurityGroupNotFound
}

func (es *EKSSession) GetSecurityGroupsByGroupID(ctx context.Context, groupID []string) ([]ec2types.SecurityGroup, error) {
	describeResult, err := es.ec2Client.DescribeSecurityGroups(
		ctx,
		&ec2.DescribeSecurityGroupsInput{
			GroupIds: groupID,
		})
	if err != nil {
		return nil, fmt.Errorf("could not describe security group: %w", err)
	}

	if len(describeResult.SecurityGroups) > 0 {
		return describeResult.SecurityGroups, nil
	}

	return nil, ErrSecurityGroupNotFound
}

func (es *EKSSession) GetSecurityGroupsByGroupNames(ctx context.Context, groupNames []string) ([]ec2types.SecurityGroup, error) {
	describeResult, err := es.ec2Client.DescribeSecurityGroups(
		ctx,
		&ec2.DescribeSecurityGroupsInput{
			GroupNames: groupNames,
		})
	if err != nil {
		return nil, fmt.Errorf("could not describe security group: %w", err)
	}

	if len(describeResult.SecurityGroups) > 0 {
		return describeResult.SecurityGroups, nil
	}

	return nil, ErrSecurityGroupNotFound
}

func (es *EKSSession) CreateEKSCluster(ctx context.Context, version string, subnetIDs, securityGroupIDs []string,
	waitForClusterCreation bool) (*ekstypes.Cluster, error) {
	trustPolicy := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {
					"Service": "eks.amazonaws.com"
				},
				"Action": "sts:AssumeRole"
			}
		]
	}`

	policies := []string{
		"arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
		"arn:aws:iam::aws:policy/AmazonEKSVPCResourceController",
	}
	roleName := es.clusterName + "-eks-role"

	role, err := es.CreateIAMRole(ctx, roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	input := &eks.CreateClusterInput{
		Name:    aws.String(es.clusterName),
		RoleArn: aws.String(*role.Arn),
		ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
			SubnetIds:        subnetIDs,
			SecurityGroupIds: securityGroupIDs,
		},
		Version: aws.String(version),
	}

	result, err := es.eksClient.CreateCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create EKS cluster: %w", err)
	}

	if !waitForClusterCreation {
		return result.Cluster, nil
	}

	if err = es.waitForClusterActive(ctx); err != nil {
		return nil, fmt.Errorf("failed waiting for cluster to become ACTIVE: %w", err)
	}

	describeResult, err := es.eksClient.DescribeCluster(
		ctx,
		&eks.DescribeClusterInput{
			Name: aws.String(es.clusterName),
		})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster after creation: %w", err)
	}

	return describeResult.Cluster, nil
}

func (es *EKSSession) waitForClusterActive(ctx context.Context) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			output, err := es.eksClient.DescribeCluster(
				ctx,
				&eks.DescribeClusterInput{
					Name: aws.String(es.clusterName),
				})
			if err != nil {
				return fmt.Errorf("failed to describe cluster: %w", err)
			}

			if output.Cluster.Status == ekstypes.ClusterStatusActive {
				return nil
			}
		}
	}
}

func (es *EKSSession) waitForNodeGroupActive(ctx context.Context, nodeGroupName string) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			output, err := es.eksClient.DescribeNodegroup(
				ctx,
				&eks.DescribeNodegroupInput{
					ClusterName:   aws.String(es.clusterName),
					NodegroupName: aws.String(nodeGroupName),
				})
			if err != nil {
				return fmt.Errorf("failed to describe cluster: %w", err)
			}

			if output.Nodegroup.Status == ekstypes.NodegroupStatusActive {
				return nil
			}
		}
	}
}

func (es *EKSSession) CreateNodeGroup(ctx context.Context, instanceType, nodeGroupName, sshKey string, subnets, securityGroupIDs []string,
	minSize, desiredSize, maxSize, diskSize int32, amiType ekstypes.AMITypes, waitForNodeGroupCreation bool) (*ekstypes.Nodegroup, error) {
	if ok, err := ValidateAMIType(amiType); !ok || err != nil {
		return nil, err
	}

	trustPolicy := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Sid": "EKSNodeAssumeRole",
				"Effect": "Allow",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Action": "sts:AssumeRole"
			}
		]
	}`

	policies := []string{
		"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
		"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
		"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
	}
	roleName := es.clusterName + "-eks-ng-role" + time.Now().Format("20060102150405")

	role, err := es.CreateIAMRole(ctx, roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	input := &eks.CreateNodegroupInput{
		ClusterName:   aws.String(es.clusterName),
		NodegroupName: aws.String(nodeGroupName),
		Subnets:       subnets,
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			MinSize:     aws.Int32(minSize),
			DesiredSize: aws.Int32(desiredSize),
			MaxSize:     aws.Int32(maxSize),
		},
		InstanceTypes: []string{instanceType}, // TODO - Take the entire array as param
		NodeRole:      aws.String(*role.Arn),
		AmiType:       amiType,
		DiskSize:      aws.Int32(diskSize),
		RemoteAccess: &ekstypes.RemoteAccessConfig{
			Ec2SshKey: aws.String(sshKey),
		},
	}

	result, err := es.eksClient.CreateNodegroup(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create node group %s: %w", nodeGroupName, err)
	}

	if !waitForNodeGroupCreation {
		return result.Nodegroup, nil
	}

	if err = es.waitForNodeGroupActive(ctx, nodeGroupName); err != nil {
		return nil, fmt.Errorf("failed waiting for node group %s to become ACTIVE: %w", nodeGroupName, err)
	}

	describeResult, err := es.eksClient.DescribeNodegroup(
		ctx,
		&eks.DescribeNodegroupInput{
			ClusterName:   aws.String(es.clusterName),
			NodegroupName: aws.String(nodeGroupName),
		})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster node group %s after creation: %w", nodeGroupName, err)
	}

	return describeResult.Nodegroup, nil
}

func (es *EKSSession) GetRoleArn(ctx context.Context, roleName string) (string, error) {
	role, err := es.iamClient.GetRole(
		ctx,
		&iam.GetRoleInput{
			RoleName: aws.String(roleName),
		})
	if err != nil {
		return "", fmt.Errorf("failed to get IAM role: %w", err)
	}

	return *role.Role.Arn, nil
}

func (es *EKSSession) CreateIAMRole(ctx context.Context, roleName, trustPolicy string, policies []string) (*iamtypes.Role, error) {
	createRoleInput := &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(trustPolicy),
	}

	role, err := es.iamClient.CreateRole(ctx, createRoleInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create IAM role: %w", err)
	}

	for _, policyArn := range policies {
		_, err = es.iamClient.AttachRolePolicy(
			ctx,
			&iam.AttachRolePolicyInput{
				RoleName:  aws.String(roleName),
				PolicyArn: aws.String(policyArn),
			})
		if err != nil {
			return nil, fmt.Errorf("failed to attach policy %s: %w", policyArn, err)
		}
	}

	return role.Role, nil
}

func (es *EKSSession) CreateAddon(ctx context.Context, addonName, addonVersion, roleArn string) (*ekstypes.Addon, error) {
	input := &eks.CreateAddonInput{
		ClusterName:           aws.String(es.clusterName),
		AddonName:             aws.String(addonName),
		AddonVersion:          aws.String(addonVersion),
		ServiceAccountRoleArn: aws.String(roleArn),
	}

	result, err := es.eksClient.CreateAddon(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create addon %s: %w", addonName, err)
	}

	return result.Addon, nil
}

func (es *EKSSession) CreatePolicy(ctx context.Context, policyName, policyDocument string) (*string, error) {
	result, err := es.iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     &policyName,
		PolicyDocument: &policyDocument,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create policy %s: %w", policyName, err)
	}

	return result.Policy.Arn, nil
}

func (es *EKSSession) AttachPolicyToRole(ctx context.Context, policyArn, roleName *string) error {
	if _, err := es.iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		PolicyArn: policyArn,
		RoleName:  roleName,
	}); err != nil {
		return fmt.Errorf("unable to attach policy %s to role %s: %w", *policyArn, *roleName, err)
	}

	return nil
}

func (es *EKSSession) EnableEBSCSIDriverAddon(ctx context.Context, addonVersion, oidURL string) (*ekstypes.Addon, error) {
	addonName := "aws-ebs-csi-driver"

	oidURLTokens := strings.Split(oidURL, "/")

	var oidCID string
	if len(oidURLTokens) > 0 {
		oidCID = oidURLTokens[len(oidURLTokens)-1]
	} else {
		return nil, fmt.Errorf("oid url is invalid %s: %w", oidURL, ErrInvalidOIDURL)
	}

	trustPolicy := fmt.Sprintf(`{
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Principal": {
                    "Federated": "arn:aws:iam::%s:oidc-provider/oidc.eks.%s.amazonaws.com/id/%s"
                },
                "Action": "sts:AssumeRoleWithWebIdentity",
                "Condition": {
                    "StringLike": {
                        "oidc.eks.%s.amazonaws.com/id/%s:aud": "sts.amazonaws.com",
                        "oidc.eks.%s.amazonaws.com/id/%s:sub": "system:serviceaccount:kube-system:ebs-csi-controller-sa"
                    }
                }
            }
        ]
    }`, es.cred.EKSCredentials.eksAccountID, es.region, oidCID, es.region, oidCID, es.region, oidCID)

	policyName := es.clusterName + "-ebs-csi-driver-policy"
	policyDocument := `{
        "Version": "2012-10-17",
        "Statement": [
            {
                "Action": [
                    "ec2:ModifyVolume",
                    "ec2:DetachVolume",
                    "ec2:DescribeVolumesModifications",
                    "ec2:DescribeVolumes",
                    "ec2:DescribeTags",
                    "ec2:DescribeSnapshots",
                    "ec2:DescribeInstances",
                    "ec2:DescribeAvailabilityZones",
                    "ec2:CreateSnapshot",
                    "ec2:AttachVolume"
                ],
                "Effect": "Allow",
                "Resource": "*"
            },
            {
                "Action": "ec2:CreateTags",
                "Condition": {
                    "StringEquals": {
                        "ec2:CreateAction": [
                            "CreateVolume",
                            "CreateSnapshot"
                        ]
                    }
                },
                "Effect": "Allow",
                "Resource": [
                    "arn:aws:ec2:*:*:volume/*",
                    "arn:aws:ec2:*:*:snapshot/*"
                ]
            },
            {
                "Action": "ec2:DeleteTags",
                "Effect": "Allow",
                "Resource": [
                    "arn:aws:ec2:*:*:volume/*",
                    "arn:aws:ec2:*:*:snapshot/*"
                ]
            },
            {
                "Action": "ec2:CreateVolume",
                "Condition": {
                    "StringLike": {
                        "aws:RequestTag/ebs.csi.aws.com/cluster": "true"
                    }
                },
                "Effect": "Allow",
                "Resource": "*"
            },
            {
                "Action": "ec2:DeleteVolume",
                "Condition": {
                    "StringLike": {
                        "ec2:ResourceTag/ebs.csi.aws.com/cluster": "true"
                    }
                },
                "Effect": "Allow",
                "Resource": "*"
            },
            {
                "Action": "ec2:DeleteSnapshot",
                "Condition": {
                    "StringLike": {
                        "ec2:ResourceTag/CSIVolumeSnapshotName": "*"
                    }
                },
                "Effect": "Allow",
                "Resource": "*"
            },
            {
                "Action": "ec2:DeleteSnapshot",
                "Condition": {
                    "StringLike": {
                        "ec2:ResourceTag/ebs.csi.aws.com/cluster": "true"
                    }
                },
                "Effect": "Allow",
                "Resource": "*"
            }
        ]
    }`

	policyArn, err := es.CreatePolicy(ctx, policyName, policyDocument)
	if err != nil {
		return nil, fmt.Errorf("cannot create managed policy %s: %w", policyName, err)
	}

	roleName := es.clusterName + "-eks-ebscsidriver-role"

	role, err := es.CreateIAMRole(ctx, roleName, trustPolicy, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	if err := es.AttachPolicyToRole(ctx, policyArn, &roleName); err != nil {
		return nil, fmt.Errorf("cannot attach policy %s to role %s: %w", policyName, roleName, err)
	}

	return es.CreateAddon(ctx, addonName, addonVersion, *role.Arn)
}

func (es *EKSSession) EnableAutoAssignPublicIP(ctx context.Context, subnetID string) error {
	if _, err := es.ec2Client.ModifySubnetAttribute(
		ctx,
		&ec2.ModifySubnetAttributeInput{
			SubnetId: aws.String(subnetID),
			MapPublicIpOnLaunch: &ec2types.AttributeBooleanValue{
				Value: aws.Bool(true),
			},
		}); err != nil {
		return fmt.Errorf("enable auto-assign public IP for subnet %s: %w", subnetID, err)
	}

	return nil
}

func (es *EKSSession) DeleteNodeGroup(ctx context.Context, nodeGroupName string, waitForDeletion bool) error {
	input := &eks.DeleteNodegroupInput{
		ClusterName:   aws.String(es.clusterName),
		NodegroupName: aws.String(nodeGroupName),
	}

	if _, err := es.eksClient.DeleteNodegroup(ctx, input); err != nil {
		return fmt.Errorf("failed to delete node group %s: %w", nodeGroupName, err)
	}

	if !waitForDeletion {
		return nil
	}

	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(30 * time.Second)

	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout reached while waiting for node group %s to be deleted: %w", nodeGroupName, ErrTimeoutNodeGroupDeletion)

		case <-ticker.C:
			_, err := es.eksClient.DescribeNodegroup(
				ctx,
				&eks.DescribeNodegroupInput{
					ClusterName:   aws.String(es.clusterName),
					NodegroupName: aws.String(nodeGroupName),
				})

			if err != nil {
				var resourceError *ekstypes.ResourceNotFoundException
				if errors.As(err, &resourceError) {
					return nil
				}

				return fmt.Errorf("failed to describe node group %s: %w", nodeGroupName, err)
			}
		}
	}
}

func (es *EKSSession) DeleteSecurityGroups(ctx context.Context, groupIDs []string) error {
	for _, groupID := range groupIDs {
		if _, err := es.ec2Client.DeleteSecurityGroup(
			ctx,
			&ec2.DeleteSecurityGroupInput{
				GroupId: &groupID,
			}); err != nil {
			return fmt.Errorf("unable to delete security group %s: %w", groupID, err)
		}
	}

	return nil
}

func (es *EKSSession) CreateInternetGateway(ctx context.Context, igwName string) (*ec2types.InternetGateway, error) {
	result, err := es.ec2Client.CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{})
	if err != nil {
		return nil, fmt.Errorf("create internet gateway: %w", err)
	}

	err = es.addTagsToResource(ctx, *result.InternetGateway.InternetGatewayId, map[string]string{"Name": igwName})
	if err != nil {
		return nil, fmt.Errorf("tag internet gateway: %w", err)
	}

	return result.InternetGateway, nil
}

func (es *EKSSession) AttachInternetGateway(ctx context.Context, vpcID, igwID string) error {
	if _, err := es.ec2Client.AttachInternetGateway(
		ctx,
		&ec2.AttachInternetGatewayInput{
			VpcId:             aws.String(vpcID),
			InternetGatewayId: aws.String(igwID),
		}); err != nil {
		return fmt.Errorf("attach internet gateway: %w", err)
	}

	return nil
}

func (es *EKSSession) DeleteSubnets(ctx context.Context, subnetIDs []string) error {
	for _, subnetID := range subnetIDs {
		if _, err := es.ec2Client.DeleteSubnet(
			ctx,
			&ec2.DeleteSubnetInput{
				SubnetId: &subnetID,
			}); err != nil {
			return fmt.Errorf("unable to delete subnet %s: %w", subnetID, err)
		}
	}

	return nil
}

func (es *EKSSession) CreateRouteTable(ctx context.Context, vpcID, igwID, routeTableName string) (*ec2types.RouteTable, error) {
	result, err := es.ec2Client.CreateRouteTable(
		ctx,
		&ec2.CreateRouteTableInput{
			VpcId: aws.String(vpcID),
		})
	if err != nil {
		return nil, fmt.Errorf("create route table: %w", err)
	}

	// TODO: Take the routes as params
	if _, err = es.ec2Client.CreateRoute(
		ctx,
		&ec2.CreateRouteInput{
			RouteTableId:         result.RouteTable.RouteTableId,
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			GatewayId:            aws.String(igwID),
		}); err != nil {
		return nil, fmt.Errorf("add route to internet gateway: %w", err)
	}

	err = es.addTagsToResource(ctx, *result.RouteTable.RouteTableId, map[string]string{"Name": routeTableName})
	if err != nil {
		return nil, fmt.Errorf("create route in Route Table: %w", err)
	}

	return result.RouteTable, nil
}

func (es *EKSSession) AssociateRouteTable(ctx context.Context, routeTableID string, subnetID string) error {
	if _, err := es.ec2Client.AssociateRouteTable(
		ctx,
		&ec2.AssociateRouteTableInput{
			RouteTableId: aws.String(routeTableID),
			SubnetId:     aws.String(subnetID),
		}); err != nil {
		return fmt.Errorf("associate route table %s with subnet %s: %w", routeTableID, subnetID, err)
	}

	return nil
}

func (es *EKSSession) DeleteVpc(ctx context.Context, vpcID *string) error {
	if _, err := es.ec2Client.DeleteVpc(
		ctx,
		&ec2.DeleteVpcInput{
			VpcId: vpcID,
		}); err != nil {
		return fmt.Errorf("unable to delete vpc %s: %w", *vpcID, err)
	}

	return nil
}

func (es *EKSSession) DeletePolicy(ctx context.Context, policyName *string) error {
	result, err := es.iamClient.ListPolicies(ctx, &iam.ListPoliciesInput{})
	if err != nil {
		return fmt.Errorf("unable to list policies: %w", err)
	}

	var policyArn *string
	for _, policy := range result.Policies {
		if *policy.PolicyName == *policyName {
			policyArn = policy.Arn
		}
	}

	if policyArn == nil {
		return fmt.Errorf("unable to find policy %s: %w", *policyName, ErrInvalidPolicy)
	}

	if _, err := es.iamClient.DeletePolicy(
		ctx,
		&iam.DeletePolicyInput{
			PolicyArn: policyArn,
		},
	); err != nil {
		return fmt.Errorf("unable to delete policy %s: %w", *policyName, err)
	}

	return nil
}

func (es *EKSSession) DeleteIAMRole(ctx context.Context, nodeRole *string) error {
	policiesOutput, err := es.iamClient.ListAttachedRolePolicies(
		ctx,
		&iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(*nodeRole),
		})
	if err != nil {
		return fmt.Errorf("unable to list attached policies for role %s: %w", *nodeRole, err)
	}

	for _, policy := range policiesOutput.AttachedPolicies {
		detachPolicyInput := &iam.DetachRolePolicyInput{
			RoleName:  aws.String(*nodeRole),
			PolicyArn: policy.PolicyArn,
		}

		if _, err := es.iamClient.DetachRolePolicy(ctx, detachPolicyInput); err != nil {
			return fmt.Errorf("unable to detach policy %s from role %s: %w", *policy.PolicyArn, *nodeRole, err)
		}
	}

	if _, err := es.iamClient.DeleteRole(
		ctx,
		&iam.DeleteRoleInput{
			RoleName: nodeRole,
		}); err != nil {
		return fmt.Errorf("unable to delete role %s: %w", *nodeRole, err)
	}

	return nil
}

func (es *EKSSession) DeleteCluster(ctx context.Context, waitForDeletion bool) error {
	if _, err := es.eksClient.DeleteCluster(
		ctx,
		&eks.DeleteClusterInput{
			Name: &es.clusterName,
		}); err != nil {
		return fmt.Errorf("unable to delete cluster %s: %w", es.clusterName, err)
	}

	if !waitForDeletion {
		return nil
	}

	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(30 * time.Second)

	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout reached while waiting for cluster %s to be deleted: %w", es.clusterName, ErrTimeoutClusterDeletion)

		case <-ticker.C:
			_, err := es.eksClient.DescribeCluster(
				ctx,
				&eks.DescribeClusterInput{
					Name: aws.String(es.clusterName),
				})

			if err != nil {
				var resourceError *ekstypes.ResourceNotFoundException
				if errors.As(err, &resourceError) {
					return nil
				}

				return fmt.Errorf("failed to describe cluster %s: %w", es.clusterName, err)
			}
		}
	}
}

func (es *EKSSession) DissociateRouteTable(ctx context.Context, associationID *string) error {
	if _, err := es.ec2Client.DisassociateRouteTable(
		ctx,
		&ec2.DisassociateRouteTableInput{
			AssociationId: associationID,
		}); err != nil {
		return fmt.Errorf("failed to dissociate Route Table with Subnet: %w", err)
	}

	return nil
}

func (es *EKSSession) DeleteRouteTable(ctx context.Context, routeTableID *string) error {
	if _, err := es.ec2Client.DeleteRouteTable(
		ctx,
		&ec2.DeleteRouteTableInput{
			RouteTableId: routeTableID,
		}); err != nil {
		return fmt.Errorf("unable to delete route table: %w", err)
	}

	return nil
}

func (es *EKSSession) DetachInternetGateway(ctx context.Context, vpcID, igwID *string) error {
	if _, err := es.ec2Client.DetachInternetGateway(
		ctx,
		&ec2.DetachInternetGatewayInput{
			InternetGatewayId: igwID,
			VpcId:             vpcID,
		}); err != nil {
		return fmt.Errorf("unable to detach internet gateway: %w", err)
	}

	return nil
}

func (es *EKSSession) DeleteInternetGateway(ctx context.Context, igwID *string) error {
	if _, err := es.ec2Client.DeleteInternetGateway(
		ctx,
		&ec2.DeleteInternetGatewayInput{
			InternetGatewayId: igwID,
		}); err != nil {
		return fmt.Errorf("unable to delete internet gateway: %w", err)
	}

	return nil
}

func (es *EKSSession) GetInternetGatewayForVPC(ctx context.Context, vpcID *string) (*ec2types.InternetGateway, error) {
	filter := ec2types.Filter{
		Name:   aws.String("attachment.vpc-id"),
		Values: []string{*vpcID},
	}

	resp, err := es.ec2Client.DescribeInternetGateways(
		ctx,
		&ec2.DescribeInternetGatewaysInput{
			Filters: []ec2types.Filter{
				filter,
			},
		})
	if err != nil {
		return nil, fmt.Errorf("unable to fetch internet gateways for vpc: %w", err)
	}

	if len(resp.InternetGateways) > 0 {
		return &resp.InternetGateways[0], nil
	}

	return nil, fmt.Errorf("unable to find any internet gateways for vpc: %w", ErrNoInternetGatewaysFound)
}

func (es *EKSSession) GetRouteTablesForSubnet(ctx context.Context, subnetID *string) ([]ec2types.RouteTable, error) {
	filter := ec2types.Filter{
		Name:   aws.String("association.subnet-id"),
		Values: []string{*subnetID},
	}

	resp, err := es.ec2Client.DescribeRouteTables(
		ctx,
		&ec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				filter,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch route table for the subnet: %w", err)
	}

	if len(resp.RouteTables) > 0 {
		return resp.RouteTables, nil
	}

	return nil, fmt.Errorf("no route tables found for the subnet: %w", ErrNoRouteTablesFound)
}

func (es *EKSSession) GetRouteTableAssociation(ctx context.Context, routeTableID, subnetID *string) (*ec2types.RouteTableAssociation, error) {
	resp, err := es.ec2Client.DescribeRouteTables(
		ctx,
		&ec2.DescribeRouteTablesInput{
			RouteTableIds: []string{*routeTableID},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch route table info: %w", err)
	}

	for _, rt := range resp.RouteTables {
		for _, assoc := range rt.Associations {
			if *assoc.SubnetId == *subnetID {
				return &assoc, nil
			}
		}
	}

	return nil, fmt.Errorf("no associations found between subnet and routeTable: %w", ErrNoAssociationsFound)
}

func (es *EKSSession) DeleteRoute(ctx context.Context, routeTableID *string) error {
	// TODO : Take destination cidr block as param
	if _, err := es.ec2Client.DeleteRoute(
		ctx,
		&ec2.DeleteRouteInput{
			RouteTableId:         routeTableID,
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
		}); err != nil {
		return fmt.Errorf("unable to detach route: %w", err)
	}

	return nil
}

func (es *EKSSession) TerminateInstances(ctx context.Context, instanceIds []string) error {
	if _, err := es.ec2Client.TerminateInstances(
		ctx,
		&ec2.TerminateInstancesInput{
			InstanceIds: instanceIds,
		}); err != nil {
		return fmt.Errorf("terminate ec2 instance: %w", err)
	}

	return nil
}

func (es *EKSSession) RebootInstances(ctx context.Context, instanceIds []string) error {
	if _, err := es.ec2Client.RebootInstances(
		ctx,
		&ec2.RebootInstancesInput{
			InstanceIds: instanceIds,
		}); err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	return nil
}

func (es *EKSSession) CreateOidcProvider(ctx context.Context, url *string, clientIDList []string, oidcName string) error {
	_, err := es.iamClient.CreateOpenIDConnectProvider(ctx, &iam.CreateOpenIDConnectProviderInput{
		Url:          url,
		ClientIDList: clientIDList,
		Tags: []iamtypes.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(oidcName),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create oidc provider %s: %w", *url, err)
	}

	return nil
}

func (es *EKSSession) DeleteOidcProvider(ctx context.Context, openIDConnectProviderArn *string) error {
	if _, err := es.iamClient.DeleteOpenIDConnectProvider(
		ctx,
		&iam.DeleteOpenIDConnectProviderInput{
			OpenIDConnectProviderArn: openIDConnectProviderArn,
		},
	); err != nil {
		return fmt.Errorf("unable to delete oidc provider %s: %w", *openIDConnectProviderArn, err)
	}

	return nil
}

func (es *EKSSession) ListOidcProviders(ctx context.Context) ([]iamtypes.OpenIDConnectProviderListEntry, error) {
	result, err := es.iamClient.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return nil, fmt.Errorf("unable to list open id connect providers: %w", err)
	}

	return result.OpenIDConnectProviderList, nil
}

func (es *EKSSession) FetchOidcArnFromURL(ctx context.Context, oidcURL *string) (*string, error) {
	oidcARNs, err := es.ListOidcProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to list open id connect providers: %w", err)
	}

	oidcURLToken := strings.Split(*oidcURL, "https://")[1]

	for _, oidcARN := range oidcARNs {
		if strings.Contains(*oidcARN.Arn, oidcURLToken) {
			return oidcARN.Arn, nil
		}
	}

	return nil, fmt.Errorf("no OIDC provider found for url %s: %w", *oidcURL, ErrInvalidOIDURL)
}

func (es *EKSSession) UpdateClusterKubernetesVersion(ctx context.Context, k8sVersion *string,
	waitForClusterUpdate bool) error {
	if _, err := es.eksClient.UpdateClusterVersion(ctx, &eks.UpdateClusterVersionInput{
		Name:    &es.clusterName,
		Version: k8sVersion,
	}); err != nil {
		return fmt.Errorf("unable to update k8s cluster version: %w", err)
	}

	if waitForClusterUpdate {
		if err := es.waitForClusterActive(ctx); err != nil {
			return fmt.Errorf("failed waiting for cluster to become ACTIVE: %w", err)
		}
	}

	return nil
}

func (es *EKSSession) UpdateNodeGroupKubernetesVersion(ctx context.Context, nodeGroupName, k8sVersion *string,
	waitForNodeGroupUpdate bool) error {
	if _, err := es.eksClient.UpdateNodegroupVersion(ctx, &eks.UpdateNodegroupVersionInput{
		ClusterName:   &es.clusterName,
		NodegroupName: nodeGroupName,
		Version:       k8sVersion,
	}); err != nil {
		return fmt.Errorf("unable to update k8s node group version: %w", err)
	}

	if waitForNodeGroupUpdate {
		if err := es.waitForNodeGroupActive(ctx, *nodeGroupName); err != nil {
			return fmt.Errorf("failed waiting for node group to become ACTIVE: %w", err)
		}
	}

	return nil
}

// addTagsToResource adds tags to a given AWS resource. Add the Tag: Key and Value to tags.
func (es *EKSSession) addTagsToResource(ctx context.Context, resourceID string, tags map[string]string) error {
	var ec2Tags []ec2types.Tag

	for key, value := range tags {
		if len(key) > 128 || len(key) == 0 {
			return fmt.Errorf("add tag `%s:%s` to resource: %w", key, value, ErrTagKeyInvalid)
		}

		if len(value) > 256 {
			return fmt.Errorf("add tag `%s:%s` to resource: %w", key, value, ErrTagValueInvalid)
		}

		ec2Tags = append(ec2Tags, ec2types.Tag{
			Key:   aws.String(key),
			Value: aws.String(value),
		})
	}

	_, err := es.ec2Client.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{resourceID},
		Tags:      ec2Tags,
	})
	if err != nil {
		return fmt.Errorf("add tags to resource %s: %w", resourceID, err)
	}

	return nil
}
