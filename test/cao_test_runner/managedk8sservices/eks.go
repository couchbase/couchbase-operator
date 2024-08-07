package managedk8sservices

import (
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
)

var (
	ErrASGNotFound = errors.New("asg not found")
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
