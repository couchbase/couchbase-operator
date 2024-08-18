package managedk8sservices

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
)

type AMIType string

const (
	AmazonLinux2x86       AMIType = "AL2_x86_64"
	AmazonLinux2x86GPU    AMIType = "AL2_x86_64_GPU"
	AmazonLinux2ARM       AMIType = "AL2_ARM_64"
	BottleRocketx86       AMIType = "BOTTLEROCKET_x86_64"
	BottleRocketARM       AMIType = "BOTTLEROCKET_ARM_64"
	BottleRocketNvidiax86 AMIType = "BOTTLEROCKET_x86_64_NVIDIA"
	BottleRocketNvidiaARM AMIType = "BOTTLEROCKET_ARM_64_NVIDIA"
	AmazonLinux2023x86    AMIType = "AL2023_x86_64_STANDARD"
	AmazonLinux2023ARM    AMIType = "AL2023_ARM_64_STANDARD"
	Windows2019Core       AMIType = "WINDOWS_CORE_2019_x86_64"
	Windows2022Core       AMIType = "WINDOWS_CORE_2022_x86_64"
	Windows2019Full       AMIType = "WINDOWS_FULL_2019_x86_64"
	Windows2022Full       AMIType = "WINDOWS_FULL_2022_x86_64"
)

var (
	ErrASGNotFound    = errors.New("asg not found")
	ErrInvalidAmiType = errors.New("invalid ami type")
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
	Session     *session.Session
	EKSClient   *eks.EKS
	ASGClient   *autoscaling.AutoScaling
	EC2Client   *ec2.EC2
	IAMClient   *iam.IAM
	Cred        *ManagedServiceCredentials
	Region      string
	ClusterName string
}

// NewEKSSession initializes a new EKSSession with the provided AWS credentials and region.
func NewEKSSession(managedSvcCred *ManagedServiceCredentials) (*EKSSession, error) {
	awsSess, err := session.NewSession(&aws.Config{
		Region:      aws.String(managedSvcCred.EKS.EKSRegion),
		Credentials: credentials.NewStaticCredentials(managedSvcCred.EKS.EKSAccessKey, managedSvcCred.EKS.EKSSecretKey, ""),
	})
	if err != nil {
		return nil, fmt.Errorf("create new EKSSession: %w", err)
	}

	return &EKSSession{
		Session:     awsSess,
		EKSClient:   eks.New(awsSess),
		ASGClient:   autoscaling.New(awsSess),
		EC2Client:   ec2.New(awsSess),
		IAMClient:   iam.New(awsSess),
		Cred:        managedSvcCred,
		Region:      managedSvcCred.EKS.EKSRegion,
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
func GetKey(managedSvcCred *ManagedServiceCredentials) string {
	key := managedSvcCred.ClusterName + ":" + managedSvcCred.EKS.EKSRegion
	return key
}

func ValidateAMIType(ami AMIType) (bool, error) {
	switch ami {
	case AmazonLinux2x86, AmazonLinux2x86GPU, AmazonLinux2ARM, BottleRocketx86,
		BottleRocketARM, BottleRocketNvidiax86, BottleRocketNvidiaARM, AmazonLinux2023x86,
		AmazonLinux2023ARM, Windows2019Core, Windows2022Core, Windows2019Full, Windows2022Full:
		return true, nil
	default:
		return false, fmt.Errorf("invalid ami type %s: %w", ami, ErrInvalidAmiType)
	}
}

// SetSession adds an EKS cluster to the EKSSessionStore.
// It creates a new EKSSession for the provided EKS cluster.
func (ess *EKSSessionStore) SetSession(managedSvcCred *ManagedServiceCredentials) error {
	defer ess.lock.Unlock()
	ess.lock.Lock()

	if _, ok := ess.EKSSessions[GetKey(managedSvcCred)]; !ok {
		eksSess, err := NewEKSSession(managedSvcCred)
		if err != nil {
			return fmt.Errorf("set eks session store: %w", err)
		}

		ess.EKSSessions[GetKey(managedSvcCred)] = eksSess

		return nil
	}

	return nil
}

// GetSession returns the EKSSession for an EKS Cluster.
// If EKSSession for the cluster is not present in EKSSessionStore then it sets it.
func (ess *EKSSessionStore) GetSession(managedSvcCred *ManagedServiceCredentials) (*EKSSession, error) {
	if _, ok := ess.EKSSessions[GetKey(managedSvcCred)]; !ok {
		err := ess.SetSession(managedSvcCred)
		if err != nil {
			return nil, fmt.Errorf("get eks session: %w", err)
		}

		return ess.EKSSessions[GetKey(managedSvcCred)], nil
	}

	return ess.EKSSessions[GetKey(managedSvcCred)], nil
}

// Check checks if the k8s cluster in EKS is accessible or not.
func (ess *EKSSessionStore) Check(managedSvcCred *ManagedServiceCredentials) error {
	eksSession, err := ess.GetSession(managedSvcCred)
	if err != nil {
		return fmt.Errorf("check eks cluster accessibility: %w", err)
	}

	_, err = eksSession.EKSClient.DescribeCluster(&eks.DescribeClusterInput{
		Name: aws.String(eksSession.ClusterName),
	})
	if err != nil {
		return fmt.Errorf("check eks cluster accessibility: %w", err)
	}

	return nil
}

// GetInstancesByK8sNodeName gets the ec2 instance ids for the provided kubernetes node names.
func (ess *EKSSessionStore) GetInstancesByK8sNodeName(managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error) {
	eksSession, err := ess.GetSession(managedSvcCred)
	if err != nil {
		return []string{}, fmt.Errorf("get ec2 instance by k8s node name: %w", err)
	}

	eksASG, err := eksSession.GetAutoscalingGroupsForCluster()
	if err != nil {
		return []string{}, fmt.Errorf("get ec2 instance by k8s node name: %w", err)
	}

	var instances []*ec2.Instance

	for _, asg := range eksASG {
		ins, err := eksSession.GetInstancesInAutoscalingGroup(*asg.AutoScalingGroupName)
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

func (ess *EKSSessionStore) GetInstancesByStrategy(managedSvcCred *ManagedServiceCredentials, strategy K8sNodeFindStrategy) ([]string, error) {
	// TODO implement me
	panic("implement me")
}

// ================================================
// ====== Methods implemented by EKSSession ======
// ================================================

// GetEKSCluster returns *eks.Cluster which can be used to retrieve information about a specific EKS cluster.
func (es *EKSSession) GetEKSCluster() (*eks.Cluster, error) {
	input := &eks.DescribeClusterInput{
		Name: aws.String(es.ClusterName),
	}

	result, err := es.EKSClient.DescribeCluster(input)
	if err != nil {
		return nil, fmt.Errorf("get eks cluster: %w", err)
	}

	return result.Cluster, nil
}

// GetNodegroupsForCluster retrieves the nodegroups for an EKS cluster.
func (es *EKSSession) GetNodegroupsForCluster() ([]*eks.Nodegroup, error) {
	input := &eks.ListNodegroupsInput{
		ClusterName: aws.String(es.ClusterName),
	}

	result, err := es.EKSClient.ListNodegroups(input)
	if err != nil {
		return nil, fmt.Errorf("get nodegroups: %w", err)
	}

	nodegroups := make([]*eks.Nodegroup, 0)

	for _, ngName := range result.Nodegroups {
		ng, err := es.EKSClient.DescribeNodegroup(&eks.DescribeNodegroupInput{
			ClusterName:   aws.String(es.ClusterName),
			NodegroupName: ngName,
		})
		if err != nil {
			return nil, fmt.Errorf("get nodegroup %s: %w", *ngName, err)
		}

		nodegroups = append(nodegroups, ng.Nodegroup)
	}

	return nodegroups, nil
}

// ModifyNodegroup modifies a nodegroup of the EKS cluster.
func (es *EKSSession) ModifyNodegroup(nodegroupName string, minSize, maxSize, desiredSize int64) error {
	input := &eks.UpdateNodegroupConfigInput{
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodegroupName),
		ScalingConfig: &eks.NodegroupScalingConfig{
			MinSize:     aws.Int64(minSize),
			MaxSize:     aws.Int64(maxSize),
			DesiredSize: aws.Int64(desiredSize),
		},
	}

	_, err := es.EKSClient.UpdateNodegroupConfig(input)
	if err != nil {
		return fmt.Errorf("update nodegroup %s: %w", nodegroupName, err)
	}

	return nil
}

// GetAutoscalingGroupsForCluster retrieves all autoscaling groups associated with the EKS cluster.
func (es *EKSSession) GetAutoscalingGroupsForCluster() ([]*autoscaling.Group, error) {
	// First, get all nodegroups in the cluster
	nodegroups, err := es.GetNodegroupsForCluster()
	if err != nil {
		return nil, fmt.Errorf("get asg for eks cluster: %w", err)
	}

	// Collect all ASG names from all nodegroups
	var asgNames []*string

	for _, ng := range nodegroups {
		if ng != nil {
			for _, asg := range ng.Resources.AutoScalingGroups {
				asgNames = append(asgNames, asg.Name)
			}
		}
	}

	// If no ASGs found, return empty slice
	if len(asgNames) == 0 {
		return []*autoscaling.Group{}, nil
	}

	// Describe all collected ASGs
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: asgNames,
	}

	result, err := es.ASGClient.DescribeAutoScalingGroups(input)
	if err != nil {
		return nil, fmt.Errorf("describe autoscaling groups: %w", err)
	}

	return result.AutoScalingGroups, nil
}

// GetAutoscalingGroupsForNodegroup retrieves the autoscaling groups for a nodegroup.
func (es *EKSSession) GetAutoscalingGroupsForNodegroup(nodegroupName string) ([]*autoscaling.Group, error) {
	// First, get the nodegroup details
	ngInput := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodegroupName),
	}

	ngResult, err := es.EKSClient.DescribeNodegroup(ngInput)
	if err != nil {
		return nil, fmt.Errorf("get asg for nodegroup: describe nodegroup %s: %w", nodegroupName, err)
	}

	// Get the ASG names from the nodegroup resources
	asgNames := make([]*string, 0)
	for _, resource := range ngResult.Nodegroup.Resources.AutoScalingGroups {
		asgNames = append(asgNames, resource.Name)
	}

	// Describe the ASGs
	asgInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: asgNames,
	}

	asgResult, err := es.ASGClient.DescribeAutoScalingGroups(asgInput)
	if err != nil {
		return nil, fmt.Errorf("describe autoscaling groups: %w", err)
	}

	return asgResult.AutoScalingGroups, nil
}

// GetInstancesInAutoscalingGroup retrieves details of instances in the specified autoscaling group.
func (es *EKSSession) GetInstancesInAutoscalingGroup(asgName string) ([]*ec2.Instance, error) {
	// Describe the autoscaling group to get instance IDs
	asgInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	}

	asgResult, err := es.ASGClient.DescribeAutoScalingGroups(asgInput)
	if err != nil {
		return nil, fmt.Errorf("describe autoscaling group %s: %w", asgName, err)
	}

	if len(asgResult.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("autoscaling group %s: %w", asgName, ErrASGNotFound)
	}

	// Getting the first autoscaling group
	asg := asgResult.AutoScalingGroups[0]

	// Get the instance IDs
	var instanceIds []*string
	for _, instance := range asg.Instances {
		instanceIds = append(instanceIds, instance.InstanceId)
	}

	if len(instanceIds) == 0 {
		return []*ec2.Instance{}, nil // Return empty slice if no instances
	}

	// Describe EC2 instances to get more details
	ec2Input := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIds,
	}

	ec2Result, err := es.EC2Client.DescribeInstances(ec2Input)
	if err != nil {
		return nil, fmt.Errorf("describe EC2 instances: %w", err)
	}

	var resultInstances []*ec2.Instance

	for _, reservation := range ec2Result.Reservations {
		for _, instance := range reservation.Instances {
			resultInstances = append(resultInstances, instance)
		}
	}

	return resultInstances, nil
}

func (es *EKSSession) CreateVPC(cidrBlock string) (*ec2.Vpc, error) {
	result, err := es.EC2Client.CreateVpc(&ec2.CreateVpcInput{
		CidrBlock: aws.String(cidrBlock),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create VPC: %w", err)
	}

	return result.Vpc, nil
}

func (es *EKSSession) CreateSubnets(vpcID string, azs []string, cidrs []string) ([]*ec2.Subnet, error) {
	var subnets []*ec2.Subnet

	for i, cidr := range cidrs {
		result, err := es.EC2Client.CreateSubnet(&ec2.CreateSubnetInput{
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

func (es *EKSSession) CreateSecurityGroup(vpcID, groupName string) (*ec2.SecurityGroup, error) {
	result, err := es.EC2Client.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(groupName),
		Description: aws.String("Security group for EKS cluster"),
		VpcId:       aws.String(vpcID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create security group: %w", err)
	}

	// TODO : Take the ip permissions as params
	_, err = es.EC2Client.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: aws.String(*result.GroupId),
		IpPermissions: []*ec2.IpPermission{
			{
				IpProtocol: aws.String("-1"),
				FromPort:   aws.Int64(0),
				ToPort:     aws.Int64(0),
				IpRanges: []*ec2.IpRange{
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

	describeResult, err := es.EC2Client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{result.GroupId},
	})
	if err != nil {
		return nil, fmt.Errorf("could not describe security group: %w", err)
	}

	if len(describeResult.SecurityGroups) > 0 {
		return describeResult.SecurityGroups[0], nil
	}

	return nil, fmt.Errorf("security group not found")
}

func (es *EKSSession) GetSecurityGroupsByGroupID(groupID []*string) ([]*ec2.SecurityGroup, error) {
	describeResult, err := es.EC2Client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: groupID,
	})
	if err != nil {
		return nil, fmt.Errorf("could not describe security group: %w", err)
	}

	if len(describeResult.SecurityGroups) > 0 {
		return describeResult.SecurityGroups, nil
	}

	return nil, fmt.Errorf("security group not found")
}

func (es *EKSSession) GetSecurityGroupsByGroupNames(groupNames []*string) ([]*ec2.SecurityGroup, error) {
	describeResult, err := es.EC2Client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupNames: groupNames,
	})
	if err != nil {
		return nil, fmt.Errorf("could not describe security group: %w", err)
	}

	if len(describeResult.SecurityGroups) > 0 {
		return describeResult.SecurityGroups, nil
	}

	return nil, fmt.Errorf("security group not found")
}

func (es *EKSSession) CreateEKSCluster(version string, subnetIDs, securityGroupIDs []string,
	waitForClusterCreation bool) (*eks.Cluster, error) {
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

	role, err := es.CreateIAMRole(roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	input := &eks.CreateClusterInput{
		Name:    aws.String(es.ClusterName),
		RoleArn: aws.String(*role.Arn),
		ResourcesVpcConfig: &eks.VpcConfigRequest{
			SubnetIds:        aws.StringSlice(subnetIDs),
			SecurityGroupIds: aws.StringSlice(securityGroupIDs),
		},
		Version: aws.String(version),
	}

	result, err := es.EKSClient.CreateCluster(input)
	if err != nil {
		return nil, fmt.Errorf("failed to create EKS cluster: %w", err)
	}

	if !waitForClusterCreation {
		return result.Cluster, nil
	}

	if err = es.EKSClient.WaitUntilClusterActive(&eks.DescribeClusterInput{
		Name: aws.String(es.ClusterName),
	}); err != nil {
		return nil, fmt.Errorf("failed waiting for cluster to become ACTIVE: %w", err)
	}

	describeResult, err := es.EKSClient.DescribeCluster(&eks.DescribeClusterInput{
		Name: aws.String(es.ClusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster after creation: %w", err)
	}

	return describeResult.Cluster, nil
}

func (es *EKSSession) CreateNodeGroup(instanceType, nodeGroupName, sshKey string, subnets, securityGroupIDs []string,
	minSize, desiredSize, maxSize, diskSize int64, amiType AMIType, waitForNodeGroupCreation bool) (*eks.Nodegroup, error) {
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

	role, err := es.CreateIAMRole(roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	input := &eks.CreateNodegroupInput{
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodeGroupName),
		Subnets:       aws.StringSlice(subnets),
		ScalingConfig: &eks.NodegroupScalingConfig{
			MinSize:     aws.Int64(minSize),
			DesiredSize: aws.Int64(desiredSize),
			MaxSize:     aws.Int64(maxSize),
		},
		InstanceTypes: aws.StringSlice([]string{instanceType}), // TODO - Take the entire array as param
		NodeRole:      aws.String(*role.Arn),
		AmiType:       aws.String(string(amiType)),
		DiskSize:      aws.Int64(diskSize),
		RemoteAccess: &eks.RemoteAccessConfig{
			Ec2SshKey: aws.String(sshKey),
		},
	}

	result, err := es.EKSClient.CreateNodegroup(input)
	if err != nil {
		return nil, fmt.Errorf("failed to create node group: %w", err)
	}

	if !waitForNodeGroupCreation {
		return result.Nodegroup, nil
	}

	if err = es.EKSClient.WaitUntilNodegroupActive(&eks.DescribeNodegroupInput{
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodeGroupName),
	}); err != nil {
		return nil, fmt.Errorf("failed waiting for node group to become ACTIVE: %w", err)
	}

	describeResult, err := es.EKSClient.DescribeNodegroup(&eks.DescribeNodegroupInput{
		ClusterName:   aws.String(es.ClusterName),
		NodegroupName: aws.String(nodeGroupName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster node group after creation: %w", err)
	}

	return describeResult.Nodegroup, nil
}

func (es *EKSSession) GetRoleArn(roleName string) (string, error) {
	role, err := es.IAMClient.GetRole(&iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get IAM role: %w", err)
	}

	return *role.Role.Arn, nil
}

func (es *EKSSession) CreateIAMRole(roleName, trustPolicy string, policies []string) (*iam.Role, error) {
	createRoleInput := &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(trustPolicy),
	}

	role, err := es.IAMClient.CreateRole(createRoleInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create IAM role: %w", err)
	}

	for _, policyArn := range policies {
		_, err = es.IAMClient.AttachRolePolicy(&iam.AttachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(policyArn),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to attach policy %s: %w", policyArn, err)
		}
	}

	return role.Role, nil
}

func (es *EKSSession) CreateAddon(addonName, addonVersion, roleArn string) (*eks.Addon, error) {
	input := &eks.CreateAddonInput{
		ClusterName:           aws.String(es.ClusterName),
		AddonName:             aws.String(addonName),
		AddonVersion:          aws.String(addonVersion),
		ServiceAccountRoleArn: aws.String(roleArn),
	}

	result, err := es.EKSClient.CreateAddon(input)
	if err != nil {
		return nil, fmt.Errorf("failed to create addon %s: %w", addonName, err)
	}

	return result.Addon, nil
}

func (es *EKSSession) EnableEBSCSIDriverAddon(addonVersion string) (*eks.Addon, error) {
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

	role, err := es.CreateIAMRole(roleName, trustPolicy, policies)
	if err != nil {
		return nil, fmt.Errorf("cannot create role %s: %w", roleName, err)
	}

	return es.CreateAddon(addonName, addonVersion, *role.Arn)
}
