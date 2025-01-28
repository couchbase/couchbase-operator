package k8sclustervalidator

import (
	"context"
	"errors"
	"fmt"

	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/sirupsen/logrus"
)

const (
	eksPlatform    = managedk8sservices.Kubernetes
	eksEnvironment = managedk8sservices.Cloud
	eksProvider    = managedk8sservices.AWS
)

var (
	ErrInvalidEKSConfig                 = errors.New("eks config is nil, testAssets does not hold cluster details. Validator does not know what to validate")
	ErrEKSK8SVersionMismatch            = errors.New("eks k8s version mismatch")
	ErrEKSNodeGroupsMismatch            = errors.New("eks node groups mismatch")
	ErrEKSAMINodeGroupMismatch          = errors.New("eks ami node group mismatch")
	ErrEKSInstanceTypeNodeGroupMismatch = errors.New("eks instance type node group mismatch")
	ErrEKSDiskSizeNodeGroupMismatch     = errors.New("eks disk size node group mismatch")
	ErrEKSDesiredSizeNodeGroupMismatch  = errors.New("eks desired size node group mismatch")
	ErrEKSMaxSizeNodeGroupMismatch      = errors.New("eks max size node group mismatch")
	ErrEKSMinSizeNodeGroupMismatch      = errors.New("eks min size node group mismatch")
	ErrEKSK8SVersionNodeGroupMismatch   = errors.New("eks k8s version node group mismatch")
	ErrEKSNodesMismatch                 = errors.New("eks cluster nodes mismatch")
)

type EKSConfig struct {
	KubernetesVersion *string            `yaml:"kubernetesVersion"`
	InstanceType      *string            `yaml:"instanceType"`
	NumNodeGroups     *int               `yaml:"numNodeGroups"`
	MinSize           *int32             `yaml:"minSize"`
	MaxSize           *int32             `yaml:"maxSize"`
	DesiredSize       *int32             `yaml:"desiredSize"`
	DiskSize          *int32             `yaml:"diskSize"`
	AMI               *ekstypes.AMITypes `yaml:"ami"`
}

type ValidateEKSCluster struct {
	ClusterName string
	EKSConfig   *EKSConfig
}

/*
TODO : Also check if all the nodes are healthy and ready
TODO : When number of nodegroups are updated, make sure the original nodes are intact and same.
TODO : Check if OIDC provider exists and is correct
TODO : Validate all values of EKSConfig before validating with cluster. Reduces eks calls and makes sure the yaml is correct.
TODO : Take config of each node group as a param like in AKSValidator. This will allow for more granular control.
*/

func (c *ValidateEKSCluster) ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		If EKSConfig is nil and testAssets does not hold eksClusterDetail for the cluster
			The validator does not know what to validate and will error out

		If EKSConfig is nil and testAssets does hold eksClusterDetail for the cluster
			The validator checks and validates if the k8s environment is as per state held in testAssets and eksClusterDetail

		If EKSConfig is not nil and testAssets does not hold eksClusterDetail for the cluster
			The eks cluster is a new cluster and did not exist previously. Validator should validate the new cluster according to
			the config

		If EKSConfig is not nil and testAssets does hold eksClusterDetail for the cluster
			There are changes to the cluster. Validator should validate the new cluster according to the config.
	*/

	/*
		Validator validates the following
		 - If the cluster exists
		 - If the k8s version is correct
		 - If there are right number of node groups, with correct ami, size, etc.
		 - If the eks environment is as per the testAssets state
	*/

	_, errConfig := checkConfigIsNil(c.EKSConfig)

	// When EKSConfig is nil and testAssets does not hold eksClusterDetail
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		if errConfig == nil {
			return fmt.Errorf("validate cluster: %w", ErrInvalidEKSConfig)
		}
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetEKSClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		if errConfig == nil {
			return fmt.Errorf("validate cluster: %w", ErrInvalidEKSConfig)
		}
	}

	if (cluster == nil && k8sCluster != nil) || (cluster != nil && k8sCluster == nil) {
		return fmt.Errorf("validate cluster: %w", ErrTestAssetsNotPopulated)
	}

	// When EKSConfig is nil and testAssets does hold eksClusterDetail
	if errConfig == nil && k8sCluster != nil {
		logrus.Infof("Validating prev state")
		if err := c.ValidatePrevState(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When EKSConfig is not nil and testAssets does not hold eksClusterDetail
	if errConfig != nil && k8sCluster == nil {
		logrus.Infof("Validating new cluster")
		if err := c.ValidateNewCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When EKSConfig is not nil and testAssets does hold eksClusterDetail
	if errConfig != nil && k8sCluster != nil {
		logrus.Infof("Validating cluster post updates")
		if err := c.ValidateUpdateCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	logrus.Infof("Cluster %s successfully validated in EKS", c.ClusterName)

	return nil
}

func (c *ValidateEKSCluster) ValidatePrevState(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetEKSClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != eksPlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != eksEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != eksProvider {
		return fmt.Errorf("validate prev state: %w", ErrServiceProviderMismatch)
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	eksCluster, err := eksSession.GetEKSCluster(ctx)
	if err != nil {
		// Cluster does not exist in EKS
		return fmt.Errorf("validate prev state: %w", err)
	}

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if *eksCluster.Version != cluster.GetKubernetesVersion() {
		return fmt.Errorf("validate prev state: %w", ErrEKSK8SVersionMismatch)
	}

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if len(nodeGroups) != len(cluster.GetAllNodeGroups()) {
		return fmt.Errorf("validate prev state: %w", ErrEKSNodeGroupsMismatch)
	}

	for _, nodeGroup := range nodeGroups {
		if *nodeGroup.ScalingConfig.DesiredSize != cluster.GetDesiredSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSDesiredSizeNodeGroupMismatch)
		}

		if *nodeGroup.ScalingConfig.MinSize != cluster.GetMinSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSMinSizeNodeGroupMismatch)
		}

		if *nodeGroup.ScalingConfig.MaxSize != cluster.GetMaxSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSMaxSizeNodeGroupMismatch)
		}

		if nodeGroup.AmiType != cluster.GetAMI() {
			return fmt.Errorf("validate prev state: %w", ErrEKSAMINodeGroupMismatch)
		}

		if nodeGroup.InstanceTypes[0] != cluster.GetInstanceType() {
			return fmt.Errorf("validate prev state: %w", ErrEKSInstanceTypeNodeGroupMismatch)
		}

		if *nodeGroup.DiskSize != cluster.GetDiskSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSDiskSizeNodeGroupMismatch)
		}

		if *nodeGroup.Version != cluster.GetKubernetesVersion() {
			return fmt.Errorf("validate prev state: %w", ErrEKSK8SVersionNodeGroupMismatch)
		}
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if len(allNodes) != len(k8sCluster.GetNodes()) {
		return fmt.Errorf("validate prev state: %w", ErrEKSNodesMismatch)
	}

	for _, nodeName := range allNodes {
		if !containsStrPtr(k8sCluster.GetNodes(), &nodeName) {
			return fmt.Errorf("validate prev state: %w", ErrEKSNodesMismatch)
		}
	}

	return nil
}

func (c *ValidateEKSCluster) ValidateNewCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	managedServiceProvider := managedk8sservices.NewManagedServiceProvider(eksPlatform, eksEnvironment, eksProvider)

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	eksCluster, err := eksSession.GetEKSCluster(ctx)
	if err != nil {
		// Cluster does not exist in EKS
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate new state: %w", err)
	}

	if *eksCluster.Version != *c.EKSConfig.KubernetesVersion {
		return fmt.Errorf("validate new cluster: %w", ErrEKSK8SVersionMismatch)
	}

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if len(nodeGroups) != *c.EKSConfig.NumNodeGroups {
		return fmt.Errorf("validate new cluster: %w", ErrEKSNodeGroupsMismatch)
	}

	var nodeGroupNames []*string

	for _, nodeGroup := range nodeGroups {
		nodeGroupNames = append(nodeGroupNames, nodeGroup.NodegroupName)

		if *nodeGroup.ScalingConfig.DesiredSize != *c.EKSConfig.DesiredSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSDesiredSizeNodeGroupMismatch)
		}

		if *nodeGroup.ScalingConfig.MinSize != *c.EKSConfig.MinSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSMinSizeNodeGroupMismatch)
		}

		if *nodeGroup.ScalingConfig.MaxSize != *c.EKSConfig.MaxSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSMaxSizeNodeGroupMismatch)
		}

		if nodeGroup.AmiType != *c.EKSConfig.AMI {
			return fmt.Errorf("validate new cluster: %w", ErrEKSAMINodeGroupMismatch)
		}

		if nodeGroup.InstanceTypes[0] != *c.EKSConfig.InstanceType {
			return fmt.Errorf("validate new cluster: %w", ErrEKSInstanceTypeNodeGroupMismatch)
		}

		if *nodeGroup.DiskSize != *c.EKSConfig.DiskSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSDiskSizeNodeGroupMismatch)
		}

		if *nodeGroup.Version != *c.EKSConfig.KubernetesVersion {
			return fmt.Errorf("validate new cluster: %w", ErrEKSK8SVersionNodeGroupMismatch)
		}
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if len(allNodes) != *c.EKSConfig.NumNodeGroups*int(*c.EKSConfig.DesiredSize) {
		return fmt.Errorf("validate prev state: %w", ErrEKSDesiredSizeNodeGroupMismatch)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetEKSClusterDetail(
		assets.NewEKSClusterDetail(c.ClusterName, nodeGroupNames, *c.EKSConfig.KubernetesVersion,
			*c.EKSConfig.InstanceType, *c.EKSConfig.DesiredSize, *c.EKSConfig.MinSize,
			*c.EKSConfig.MaxSize, *c.EKSConfig.DiskSize, *c.EKSConfig.AMI)); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	k8sCluster, err := assets.NewK8SCluster(c.ClusterName, managedServiceProvider)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetK8SCluster(k8sCluster); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	return nil
}

func (c *ValidateEKSCluster) ValidateUpdateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetEKSClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != eksPlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != eksEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != eksProvider {
		return fmt.Errorf("validate update cluster: %w", ErrServiceProviderMismatch)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	eksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	eksCluster, err := eksSession.GetEKSCluster(ctx)
	if err != nil {
		// Cluster does not exist in EKS
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	// Check if the k8s version is correct
	if c.EKSConfig.KubernetesVersion != nil {
		if *eksCluster.Version != *c.EKSConfig.KubernetesVersion {
			return fmt.Errorf("validate update cluster: %w", ErrEKSK8SVersionMismatch)
		}

		if err := cluster.SetKubernetesVersion(*eksCluster.Version); err != nil {
			return fmt.Errorf("validate update cluster: %w", err)
		}
	} else {
		if *eksCluster.Version != cluster.GetKubernetesVersion() {
			return fmt.Errorf("validate update cluster: %w", ErrEKSK8SVersionMismatch)
		}
	}

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if c.EKSConfig.NumNodeGroups != nil {
		if len(nodeGroups) != *c.EKSConfig.NumNodeGroups {
			return fmt.Errorf("validate update cluster: %w", ErrEKSNodeGroupsMismatch)
		}
	} else {
		if len(nodeGroups) != len(cluster.GetAllNodeGroups()) {
			return fmt.Errorf("validate update cluster: %w", ErrEKSNodeGroupsMismatch)
		}
	}

	var nodeGroupNames []*string

	for _, nodeGroup := range nodeGroups {
		nodeGroupNames = append(nodeGroupNames, nodeGroup.NodegroupName)

		if c.EKSConfig.DesiredSize != nil {
			if *nodeGroup.ScalingConfig.DesiredSize != *c.EKSConfig.DesiredSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDesiredSizeNodeGroupMismatch)
			}

			if err := cluster.SetDesiredSize(*c.EKSConfig.DesiredSize); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}

		} else {
			if *nodeGroup.ScalingConfig.DesiredSize != cluster.GetDesiredSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDesiredSizeNodeGroupMismatch)
			}
		}

		if c.EKSConfig.MinSize != nil {
			if *nodeGroup.ScalingConfig.MinSize != *c.EKSConfig.MinSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMinSizeNodeGroupMismatch)
			}

			if err := cluster.SetMinSize(*c.EKSConfig.MinSize); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if *nodeGroup.ScalingConfig.MinSize != cluster.GetMinSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMinSizeNodeGroupMismatch)
			}
		}

		if c.EKSConfig.MaxSize != nil {
			if *nodeGroup.ScalingConfig.MaxSize != *c.EKSConfig.MaxSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMaxSizeNodeGroupMismatch)
			}

			if err := cluster.SetMaxSize(*c.EKSConfig.MaxSize); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if *nodeGroup.ScalingConfig.MaxSize != cluster.GetMaxSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMaxSizeNodeGroupMismatch)
			}
		}

		if c.EKSConfig.AMI != nil {
			if nodeGroup.AmiType != *c.EKSConfig.AMI {
				return fmt.Errorf("validate update cluster: %w", ErrEKSAMINodeGroupMismatch)
			}

			if err := cluster.SetAMI(*c.EKSConfig.AMI); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if nodeGroup.AmiType != cluster.GetAMI() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSAMINodeGroupMismatch)
			}
		}

		if c.EKSConfig.InstanceType != nil {
			if nodeGroup.InstanceTypes[0] != *c.EKSConfig.InstanceType {
				return fmt.Errorf("validate update cluster: %w", ErrEKSInstanceTypeNodeGroupMismatch)
			}

			if err := cluster.SetInstanceType(*c.EKSConfig.InstanceType); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if nodeGroup.InstanceTypes[0] != cluster.GetInstanceType() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSInstanceTypeNodeGroupMismatch)
			}
		}

		if c.EKSConfig.DiskSize != nil {
			if *nodeGroup.DiskSize != *c.EKSConfig.DiskSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDiskSizeNodeGroupMismatch)
			}

			if err := cluster.SetDiskSize(*c.EKSConfig.DiskSize); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if *nodeGroup.DiskSize != cluster.GetDiskSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDiskSizeNodeGroupMismatch)
			}
		}

		if c.EKSConfig.KubernetesVersion != nil {
			if *nodeGroup.Version != *c.EKSConfig.KubernetesVersion {
				return fmt.Errorf("validate update cluster: %w", ErrEKSK8SVersionNodeGroupMismatch)
			}

			if err := cluster.SetKubernetesVersion(*c.EKSConfig.KubernetesVersion); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)

			}
		} else {
			if *nodeGroup.Version != cluster.GetKubernetesVersion() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSK8SVersionNodeGroupMismatch)
			}
		}
	}

	if err := cluster.SetNodeGroups(nodeGroupNames); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if c.EKSConfig.NumNodeGroups != nil || c.EKSConfig.DesiredSize != nil {
		if len(allNodes) != len(cluster.GetAllNodeGroups())*int(cluster.GetDesiredSize()) {
			return fmt.Errorf("validate update cluster: %w", ErrEKSNodesMismatch)
		}

		var nodeNames []*string

		for _, nodeName := range allNodes {
			nodeNames = append(nodeNames, &nodeName)
		}

		if err := k8sCluster.SetNodes(nodeNames); err != nil {
			return fmt.Errorf("validate update cluster: %w", err)
		}
	} else {
		if len(allNodes) != len(cluster.GetAllNodeGroups())*int(cluster.GetDesiredSize()) {
			return fmt.Errorf("validate update cluster: %w", ErrEKSNodesMismatch)
		}

		for _, nodeName := range allNodes {
			if !containsStrPtr(k8sCluster.GetNodes(), &nodeName) {
				return fmt.Errorf("validate update cluster: %w", ErrEKSNodesMismatch)
			}
		}
	}

	return nil
}
