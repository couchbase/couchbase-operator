package managedk8sservices

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	EKSClient   *eks.Client
	ASGClient   *autoscaling.Client
	EC2Client   *ec2.Client
	IAMClient   *iam.Client
	Cred        *ManagedServiceCredentials
	Region      string
	ClusterName string
}

// NewEKSSession initializes a new EKSSession with the provided AWS credentials and region.
func NewEKSSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) (*EKSSession, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(managedSvcCred.EKSCredentials.eksRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(managedSvcCred.EKSCredentials.eksAccessKey, managedSvcCred.EKSCredentials.eksSecretKey, "")))
	if err != nil {
		return nil, fmt.Errorf("create new EKSSession: %w", err)
	}

	return &EKSSession{
		EKSClient:   eks.NewFromConfig(cfg),
		ASGClient:   autoscaling.NewFromConfig(cfg),
		EC2Client:   ec2.NewFromConfig(cfg),
		IAMClient:   iam.NewFromConfig(cfg),
		Cred:        managedSvcCred,
		Region:      managedSvcCred.EKSCredentials.eksRegion,
		ClusterName: managedSvcCred.ClusterName,
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

	_, err = eksSession.EKSClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(eksSession.ClusterName),
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
		Name: aws.String(es.ClusterName),
	}

	result, err := es.EKSClient.DescribeCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("get eks cluster: %w", err)
	}

	return result.Cluster, nil
}

// GetNodegroupsForCluster retrieves the nodegroups for an EKS cluster.
func (es *EKSSession) GetNodegroupsForCluster(ctx context.Context) ([]*ekstypes.Nodegroup, error) {
	input := &eks.ListNodegroupsInput{
		ClusterName: aws.String(es.ClusterName),
	}

	result, err := es.EKSClient.ListNodegroups(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("get nodegroups: %w", err)
	}

	nodegroups := make([]*ekstypes.Nodegroup, 0)

	for _, ngName := range result.Nodegroups {
		ng, err := es.EKSClient.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
			ClusterName:   aws.String(es.ClusterName),
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
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodegroupName),
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			MinSize:     &minSize,
			MaxSize:     &maxSize,
			DesiredSize: &desiredSize,
		},
	}

	_, err := es.EKSClient.UpdateNodegroupConfig(ctx, input)
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

	result, err := es.ASGClient.DescribeAutoScalingGroups(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe autoscaling groups: %w", err)
	}

	return result.AutoScalingGroups, nil
}

// GetAutoscalingGroupsForNodegroup retrieves the autoscaling groups for a nodegroup.
func (es *EKSSession) GetAutoscalingGroupsForNodegroup(ctx context.Context, nodegroupName string) ([]autoscalingtypes.AutoScalingGroup, error) {
	// First, get the nodegroup details
	ngInput := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodegroupName),
	}

	ngResult, err := es.EKSClient.DescribeNodegroup(ctx, ngInput)
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

	asgResult, err := es.ASGClient.DescribeAutoScalingGroups(ctx, asgInput)
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

	asgResult, err := es.ASGClient.DescribeAutoScalingGroups(ctx, asgInput)
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

	ec2Result, err := es.EC2Client.DescribeInstances(ctx, ec2Input)
	if err != nil {
		return nil, fmt.Errorf("describe EC2 instances: %w", err)
	}

	var resultInstances []ec2types.Instance

	for _, reservation := range ec2Result.Reservations {
		resultInstances = append(resultInstances, reservation.Instances...)
	}

	return resultInstances, nil
}

func (es *EKSSession) CreateVPC(ctx context.Context, cidrBlock string) (*ec2types.Vpc, error) {
	result, err := es.EC2Client.CreateVpc(
		ctx,
		&ec2.CreateVpcInput{
			CidrBlock: aws.String(cidrBlock),
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create VPC: %w", err)
	}

	return result.Vpc, nil
}

func (es *EKSSession) CreateSubnets(ctx context.Context, vpcID string, azs []string, cidrs []string) ([]*ec2types.Subnet, error) {
	var subnets []*ec2types.Subnet

	for i, cidr := range cidrs {
		result, err := es.EC2Client.CreateSubnet(
			ctx,
			&ec2.CreateSubnetInput{
				CidrBlock:        aws.String(cidr),
				VpcId:            aws.String(vpcID),
				AvailabilityZone: aws.String(azs[i]),
			})
		if err != nil {
			return nil, fmt.Errorf("failed to create subnet: %w", err)
		}

		subnets = append(subnets, result.Subnet)
	}

	return subnets, nil
}

func (es *EKSSession) CreateSecurityGroup(ctx context.Context, vpcID, groupName string) (*ec2types.SecurityGroup, error) {
	result, err := es.EC2Client.CreateSecurityGroup(
		ctx,
		&ec2.CreateSecurityGroupInput{
			GroupName:   aws.String(groupName),
			Description: aws.String("Security group for EKS cluster"),
			VpcId:       aws.String(vpcID),
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create security group: %w", err)
	}

	// TODO : Take the ip permissions as params
	_, err = es.EC2Client.AuthorizeSecurityGroupIngress(
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

	describeResult, err := es.EC2Client.DescribeSecurityGroups(
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
	describeResult, err := es.EC2Client.DescribeSecurityGroups(
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
	describeResult, err := es.EC2Client.DescribeSecurityGroups(
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
	roleName := es.ClusterName + "-eks-role"

	role, err := es.CreateIAMRole(ctx, roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	input := &eks.CreateClusterInput{
		Name:    aws.String(es.ClusterName),
		RoleArn: aws.String(*role.Arn),
		ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
			SubnetIds:        subnetIDs,
			SecurityGroupIds: securityGroupIDs,
		},
		Version: aws.String(version),
	}

	result, err := es.EKSClient.CreateCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create EKS cluster: %w", err)
	}

	if !waitForClusterCreation {
		return result.Cluster, nil
	}

	if err = es.waitForClusterActive(ctx); err != nil {
		return nil, fmt.Errorf("failed waiting for cluster to become ACTIVE: %w", err)
	}

	describeResult, err := es.EKSClient.DescribeCluster(
		ctx,
		&eks.DescribeClusterInput{
			Name: aws.String(es.ClusterName),
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
			output, err := es.EKSClient.DescribeCluster(
				ctx,
				&eks.DescribeClusterInput{
					Name: aws.String(es.ClusterName),
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
			output, err := es.EKSClient.DescribeNodegroup(
				ctx,
				&eks.DescribeNodegroupInput{
					ClusterName:   aws.String(es.ClusterName),
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
	roleName := es.ClusterName + "-eks-node-group-role" + time.Now().Format("20060102150405.00000000000006")

	role, err := es.CreateIAMRole(ctx, roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	input := &eks.CreateNodegroupInput{
		ClusterName:   aws.String(es.ClusterName),
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

	result, err := es.EKSClient.CreateNodegroup(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create node group: %w", err)
	}

	if !waitForNodeGroupCreation {
		return result.Nodegroup, nil
	}

	if err = es.waitForNodeGroupActive(ctx, nodeGroupName); err != nil {
		return nil, fmt.Errorf("failed waiting for node group to become ACTIVE: %w", err)
	}

	describeResult, err := es.EKSClient.DescribeNodegroup(
		ctx,
		&eks.DescribeNodegroupInput{
			ClusterName:   aws.String(es.ClusterName),
			NodegroupName: aws.String(nodeGroupName),
		})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster node group after creation: %w", err)
	}

	return describeResult.Nodegroup, nil
}

func (es *EKSSession) GetRoleArn(ctx context.Context, roleName string) (string, error) {
	role, err := es.IAMClient.GetRole(
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

	role, err := es.IAMClient.CreateRole(ctx, createRoleInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create IAM role: %w", err)
	}

	for _, policyArn := range policies {
		_, err = es.IAMClient.AttachRolePolicy(
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
		ClusterName:           aws.String(es.ClusterName),
		AddonName:             aws.String(addonName),
		AddonVersion:          aws.String(addonVersion),
		ServiceAccountRoleArn: aws.String(roleArn),
	}

	result, err := es.EKSClient.CreateAddon(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create addon %s: %w", addonName, err)
	}

	return result.Addon, nil
}

func (es *EKSSession) EnableEBSCSIDriverAddon(ctx context.Context, addonVersion string) (*ekstypes.Addon, error) {
	addonName := "aws-ebs-csi-driver"
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
		"arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy",
	}
	roleName := es.ClusterName + "-eks-ebscsidriver-role"

	role, err := es.CreateIAMRole(ctx, roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	return es.CreateAddon(ctx, addonName, addonVersion, *role.Arn)
}

func (es *EKSSession) EnableAutoAssignPublicIP(ctx context.Context, subnetID string) error {
	if _, err := es.EC2Client.ModifySubnetAttribute(
		ctx,
		&ec2.ModifySubnetAttributeInput{
			SubnetId: aws.String(subnetID),
			MapPublicIpOnLaunch: &ec2types.AttributeBooleanValue{
				Value: aws.Bool(true),
			},
		}); err != nil {
		return fmt.Errorf("failed to enable auto-assign public IP for subnet %s: %w", subnetID, err)
	}

	return nil
}

func (es *EKSSession) DeleteNodeGroup(ctx context.Context, nodeGroupName string, waitForDeletion bool) error {
	input := &eks.DeleteNodegroupInput{
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodeGroupName),
	}

	if _, err := es.EKSClient.DeleteNodegroup(ctx, input); err != nil {
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
			_, err := es.EKSClient.DescribeNodegroup(
				ctx,
				&eks.DescribeNodegroupInput{
					ClusterName:   aws.String(es.ClusterName),
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
		if _, err := es.EC2Client.DeleteSecurityGroup(
			ctx,
			&ec2.DeleteSecurityGroupInput{
				GroupId: &groupID,
			}); err != nil {
			return fmt.Errorf("unable to delete security group %s: %w", groupID, err)
		}
	}

	return nil
}

func (es *EKSSession) CreateInternetGateway(ctx context.Context) (*ec2types.InternetGateway, error) {
	result, err := es.EC2Client.CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Internet Gateway: %w", err)
	}

	return result.InternetGateway, nil
}

func (es *EKSSession) AttachInternetGateway(ctx context.Context, vpcID, igwID string) error {
	if _, err := es.EC2Client.AttachInternetGateway(
		ctx,
		&ec2.AttachInternetGatewayInput{
			VpcId:             aws.String(vpcID),
			InternetGatewayId: aws.String(igwID),
		}); err != nil {
		return fmt.Errorf("failed to attach Internet Gateway: %w", err)
	}

	return nil
}

func (es *EKSSession) DeleteSubnets(ctx context.Context, subnetIDs []string) error {
	for _, subnetID := range subnetIDs {
		if _, err := es.EC2Client.DeleteSubnet(
			ctx,
			&ec2.DeleteSubnetInput{
				SubnetId: &subnetID,
			}); err != nil {
			return fmt.Errorf("unable to delete subnet %s: %w", subnetID, err)
		}
	}

	return nil
}

func (es *EKSSession) CreateRouteTable(ctx context.Context, vpcID, igwID string) (*ec2types.RouteTable, error) {
	result, err := es.EC2Client.CreateRouteTable(
		ctx,
		&ec2.CreateRouteTableInput{
			VpcId: aws.String(vpcID),
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create Route Table: %w", err)
	}

	// TODO: Take the routes as params
	if _, err = es.EC2Client.CreateRoute(
		ctx,
		&ec2.CreateRouteInput{
			RouteTableId:         result.RouteTable.RouteTableId,
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			GatewayId:            aws.String(igwID),
		}); err != nil {
		return nil, fmt.Errorf("failed to create route in Route Table: %w", err)
	}

	return result.RouteTable, nil
}

func (es *EKSSession) AssociateRouteTable(ctx context.Context, routeTableID string, subnetID string) error {
	if _, err := es.EC2Client.AssociateRouteTable(
		ctx,
		&ec2.AssociateRouteTableInput{
			RouteTableId: aws.String(routeTableID),
			SubnetId:     aws.String(subnetID),
		}); err != nil {
		return fmt.Errorf("failed to associate Route Table with Subnet: %w", err)
	}

	return nil
}

func (es *EKSSession) DeleteVpc(ctx context.Context, vpcID *string) error {
	if _, err := es.EC2Client.DeleteVpc(
		ctx,
		&ec2.DeleteVpcInput{
			VpcId: vpcID,
		}); err != nil {
		return fmt.Errorf("unable to delete vpc %s: %w", *vpcID, err)
	}

	return nil
}

func (es *EKSSession) DeleteIAMRole(ctx context.Context, nodeRole *string) error {
	policiesOutput, err := es.IAMClient.ListAttachedRolePolicies(
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

		if _, err := es.IAMClient.DetachRolePolicy(ctx, detachPolicyInput); err != nil {
			return fmt.Errorf("unable to detach policy %s from role %s: %w", *policy.PolicyArn, *nodeRole, err)
		}
	}

	if _, err := es.IAMClient.DeleteRole(
		ctx,
		&iam.DeleteRoleInput{
			RoleName: nodeRole,
		}); err != nil {
		return fmt.Errorf("unable to delete role %s: %w", *nodeRole, err)
	}

	return nil
}

func (es *EKSSession) DeleteCluster(ctx context.Context, waitForDeletion bool) error {
	if _, err := es.EKSClient.DeleteCluster(
		ctx,
		&eks.DeleteClusterInput{
			Name: &es.ClusterName,
		}); err != nil {
		return fmt.Errorf("unable to delete cluster %s: %w", es.ClusterName, err)
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
			return fmt.Errorf("timeout reached while waiting for cluster %s to be deleted: %w", es.ClusterName, ErrTimeoutClusterDeletion)

		case <-ticker.C:
			_, err := es.EKSClient.DescribeCluster(
				ctx,
				&eks.DescribeClusterInput{
					Name: aws.String(es.ClusterName),
				})

			if err != nil {
				var resourceError *ekstypes.ResourceNotFoundException
				if errors.As(err, &resourceError) {
					return nil
				}

				return fmt.Errorf("failed to describe cluster %s: %w", es.ClusterName, err)
			}
		}
	}
}

func (es *EKSSession) DissociateRouteTable(ctx context.Context, associationID *string) error {
	if _, err := es.EC2Client.DisassociateRouteTable(
		ctx,
		&ec2.DisassociateRouteTableInput{
			AssociationId: associationID,
		}); err != nil {
		return fmt.Errorf("failed to dissociate Route Table with Subnet: %w", err)
	}

	return nil
}

func (es *EKSSession) DeleteRouteTable(ctx context.Context, routeTableID *string) error {
	if _, err := es.EC2Client.DeleteRouteTable(
		ctx,
		&ec2.DeleteRouteTableInput{
			RouteTableId: routeTableID,
		}); err != nil {
		return fmt.Errorf("unable to delete route table: %w", err)
	}

	return nil
}

func (es *EKSSession) DetachInternetGateway(ctx context.Context, vpcID, igwID *string) error {
	if _, err := es.EC2Client.DetachInternetGateway(
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
	if _, err := es.EC2Client.DeleteInternetGateway(
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

	resp, err := es.EC2Client.DescribeInternetGateways(
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

	resp, err := es.EC2Client.DescribeRouteTables(
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
	resp, err := es.EC2Client.DescribeRouteTables(
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
	if _, err := es.EC2Client.DeleteRoute(
		ctx,
		&ec2.DeleteRouteInput{
			RouteTableId:         routeTableID,
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
		}); err != nil {
		return fmt.Errorf("unable to detach route: %w", err)
	}

	return nil
}
