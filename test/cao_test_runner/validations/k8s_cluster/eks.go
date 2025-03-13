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
	ErrInvalidEKSConfig               = errors.New("EKSConfig is nil and testAssets does not hold eks cluster details, validator does not know what to validate")
	ErrInvalidEKSNodegroupConfig      = errors.New("EKSNodegroupConfig is nil and testAssets does not hold eks nodegroup details, validator does not know what to validate")
	ErrEKSNodegroupGSIsNil            = errors.New("EKSNodegroupDetailGetterSetter is nil")
	ErrEKSK8SVersionMismatch          = errors.New("eks cluster k8s version mismatch")
	ErrEKSNodesMismatch               = errors.New("eks cluster nodes mismatch")
	ErrEKSNodeGroupsMismatch          = errors.New("eks node group name mismatch")
	ErrEKSNodeGroupK8SVersionMismatch = errors.New("eks node group k8s version mismatch")
	ErrEKSInstanceTypeMismatch        = errors.New("eks node group instance type mismatch")
	ErrEKSDesiredSizeMismatch         = errors.New("eks node group desired size mismatch")
	ErrEKSMaxSizeMismatch             = errors.New("eks node group max size mismatch")
	ErrEKSMinSizeMismatch             = errors.New("eks node group min size mismatch")
	ErrEKSAMIMismatch                 = errors.New("eks node group ami mismatch")
	ErrEKSDiskSizeMismatch            = errors.New("eks node group disk size mismatch")
)

type EKSConfig struct {
	KubernetesVersion  *string                        `yaml:"kubernetesVersion"`
	EKSNodegroupConfig map[string]*EKSNodegroupConfig `yaml:"eksNodegroupConfig"`
}

type EKSNodegroupConfig struct {
	K8SVersion   *string            `yaml:"kubernetesVersion"`
	InstanceType *string            `yaml:"instanceType"`
	DesiredSize  *int32             `yaml:"desiredSize"`
	MinSize      *int32             `yaml:"minSize"`
	MaxSize      *int32             `yaml:"maxSize"`
	AMI          *ekstypes.AMITypes `yaml:"ami"`
	DiskSize     *int32             `yaml:"diskSize"`
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

	logrus.Infof("Starting validation of EKS cluster `%s`", c.ClusterName)

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
		logrus.Infof("Validating the previous state of EKS cluster `%s`", c.ClusterName)
		if err := c.ValidatePrevState(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When EKSConfig is not nil and testAssets does not hold eksClusterDetail
	if errConfig != nil && k8sCluster == nil {
		logrus.Infof("Validating the new EKS cluster `%s`", c.ClusterName)
		if err := c.ValidateNewCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When EKSConfig is not nil and testAssets does hold eksClusterDetail
	if errConfig != nil && k8sCluster != nil {
		logrus.Infof("Validating the updates to EKS cluster `%s`", c.ClusterName)
		if err := c.ValidateUpdateCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	logrus.Infof("Validated EKS cluster %s successfully", c.ClusterName)

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

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	// Validating the EKS Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if *eksCluster.Version != cluster.GetKubernetesVersion() {
		return fmt.Errorf("validate prev state: %w", ErrEKSK8SVersionMismatch)
	}

	// Validating the EKS Nodegroup Details
	for _, nodeGroup := range nodeGroups {
		ngGS := cluster.GetNodegroupDetailGetterSetter(nodeGroup.NodegroupName)
		if ngGS == nil {
			return fmt.Errorf("validate prev state: %w", ErrEKSNodegroupGSIsNil)
		}

		if *nodeGroup.NodegroupName != *ngGS.GetNodegroupName() {
			return fmt.Errorf("validate prev state: %w", ErrEKSNodeGroupsMismatch)
		}

		if *nodeGroup.Version != ngGS.GetK8SVersion() {
			return fmt.Errorf("validate prev state: %w", ErrEKSNodeGroupK8SVersionMismatch)
		}

		if nodeGroup.InstanceTypes[0] != ngGS.GetInstanceType() {
			return fmt.Errorf("validate prev state: %w", ErrEKSInstanceTypeMismatch)
		}

		if *nodeGroup.ScalingConfig.DesiredSize != ngGS.GetDesiredSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSDesiredSizeMismatch)
		}

		if *nodeGroup.ScalingConfig.MinSize != ngGS.GetMinSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSMinSizeMismatch)
		}

		if *nodeGroup.ScalingConfig.MaxSize != ngGS.GetMaxSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSMaxSizeMismatch)
		}

		if nodeGroup.AmiType != ngGS.GetAMI() {
			return fmt.Errorf("validate prev state: %w", ErrEKSAMIMismatch)
		}

		if *nodeGroup.DiskSize != ngGS.GetDiskSize() {
			return fmt.Errorf("validate prev state: %w", ErrEKSDiskSizeMismatch)
		}
	}

	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
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

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	// Validating EKS Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate new state: %w", err)
	}

	if c.EKSConfig.KubernetesVersion == nil || *eksCluster.Version != *c.EKSConfig.KubernetesVersion {
		return fmt.Errorf("validate new cluster: %w", ErrEKSK8SVersionMismatch)
	}

	if len(nodeGroups) != len(c.EKSConfig.EKSNodegroupConfig) {
		return fmt.Errorf("validate new cluster: %w", ErrEKSNodeGroupsMismatch)
	}

	// Validating EKS Nodegroup Details
	totalEKSNodes := 0

	for _, nodeGroup := range nodeGroups {
		totalEKSNodes += int(*nodeGroup.ScalingConfig.DesiredSize)

		ngConfig := c.EKSConfig.EKSNodegroupConfig[*nodeGroup.NodegroupName]
		if ngConfig == nil {
			return fmt.Errorf("validate new cluster: %w", ErrInvalidEKSNodegroupConfig)
		}

		if ngConfig.K8SVersion == nil || *nodeGroup.Version != *ngConfig.K8SVersion {
			return fmt.Errorf("validate new cluster: %w", ErrEKSNodeGroupK8SVersionMismatch)
		}

		if ngConfig.InstanceType == nil || nodeGroup.InstanceTypes[0] != *ngConfig.InstanceType {
			return fmt.Errorf("validate new cluster: %w", ErrEKSInstanceTypeMismatch)
		}

		if ngConfig.DesiredSize == nil || *nodeGroup.ScalingConfig.DesiredSize != *ngConfig.DesiredSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSDesiredSizeMismatch)
		}

		if ngConfig.MinSize == nil || *nodeGroup.ScalingConfig.MinSize != *ngConfig.MinSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSMinSizeMismatch)
		}

		if ngConfig.MaxSize == nil || *nodeGroup.ScalingConfig.MaxSize != *ngConfig.MaxSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSMaxSizeMismatch)
		}

		if ngConfig.AMI == nil || nodeGroup.AmiType != *ngConfig.AMI {
			return fmt.Errorf("validate new cluster: %w", ErrEKSAMIMismatch)
		}

		if ngConfig.DiskSize == nil || *nodeGroup.DiskSize != *ngConfig.DiskSize {
			return fmt.Errorf("validate new cluster: %w", ErrEKSDiskSizeMismatch)
		}
	}

	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if len(allNodes) != totalEKSNodes {
		return fmt.Errorf("validate new cluster: %w", ErrEKSNodesMismatch)
	}

	// Saving the EKS Cluster (and Nodegroups) Details to TestAssets
	eksNodegroupDetailsMap := make(map[string]*assets.EKSNodegroupDetail)

	for _, nodeGroup := range nodeGroups {
		eksNodegroupDetailsMap[*nodeGroup.NodegroupName] = assets.NewEKSNodegroupDetail(nodeGroup.NodegroupName,
			*nodeGroup.Version, nodeGroup.InstanceTypes[0], *nodeGroup.ScalingConfig.DesiredSize,
			*nodeGroup.ScalingConfig.MinSize, *nodeGroup.ScalingConfig.MaxSize, *nodeGroup.DiskSize, nodeGroup.AmiType)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetEKSClusterDetail(
		assets.NewEKSClusterDetail(c.ClusterName, *c.EKSConfig.KubernetesVersion, eksNodegroupDetailsMap)); err != nil {
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

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	// Validating EKS Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

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

	// Validating EKS Nodegroup Details
	totalEKSNodes := 0

	for _, nodeGroup := range nodeGroups {
		totalEKSNodes += int(*nodeGroup.ScalingConfig.DesiredSize)

		ngConfig := c.EKSConfig.EKSNodegroupConfig[*nodeGroup.NodegroupName]
		ngGetterSetter := cluster.GetNodegroupDetailGetterSetter(nodeGroup.NodegroupName)

		if ngConfig == nil && ngGetterSetter == nil {
			// Nodegroup does not exist in config and asset
			return fmt.Errorf("validate update cluster: %w", ErrInvalidEKSNodegroupConfig)
		}

		if ngConfig != nil && ngGetterSetter != nil {
			// Nodegroup exists in both config and asset.
			// Possibly the nodegroup config has been updated.
			// Validator checks if the nodegroup is same as validator config.

			if c.EKSConfig.KubernetesVersion != nil {
				if *nodeGroup.Version != *c.EKSConfig.KubernetesVersion {
					return fmt.Errorf("validate update cluster: %w", ErrEKSNodeGroupK8SVersionMismatch)
				}

				if err := cluster.SetKubernetesVersion(*c.EKSConfig.KubernetesVersion); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodeGroup.Version != cluster.GetKubernetesVersion() {
					return fmt.Errorf("validate update cluster: %w", ErrEKSNodeGroupK8SVersionMismatch)
				}
			}

			if ngConfig.InstanceType != nil {
				if nodeGroup.InstanceTypes[0] != *ngConfig.InstanceType {
					return fmt.Errorf("validate update cluster: %w", ErrEKSInstanceTypeMismatch)
				}

				if err := ngGetterSetter.SetInstanceType(*ngConfig.InstanceType); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if nodeGroup.InstanceTypes[0] != ngGetterSetter.GetInstanceType() {
					return fmt.Errorf("validate update cluster: %w", ErrEKSInstanceTypeMismatch)
				}
			}

			if ngConfig.DesiredSize != nil {
				if *nodeGroup.ScalingConfig.DesiredSize != *ngConfig.DesiredSize {
					return fmt.Errorf("validate update cluster: %w", ErrEKSDesiredSizeMismatch)
				}

				if err := ngGetterSetter.SetDesiredSize(*ngConfig.DesiredSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodeGroup.ScalingConfig.DesiredSize != ngGetterSetter.GetDesiredSize() {
					return fmt.Errorf("validate update cluster: %w", ErrEKSDesiredSizeMismatch)
				}
			}

			if ngConfig.MinSize != nil {
				if *nodeGroup.ScalingConfig.MinSize != *ngConfig.MinSize {
					return fmt.Errorf("validate update cluster: %w", ErrEKSMinSizeMismatch)
				}

				if err := ngGetterSetter.SetMinSize(*ngConfig.MinSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodeGroup.ScalingConfig.MinSize != ngGetterSetter.GetMinSize() {
					return fmt.Errorf("validate update cluster: %w", ErrEKSMinSizeMismatch)
				}
			}

			if ngConfig.MaxSize != nil {
				if *nodeGroup.ScalingConfig.MaxSize != *ngConfig.MaxSize {
					return fmt.Errorf("validate update cluster: %w", ErrEKSMaxSizeMismatch)
				}

				if err := ngGetterSetter.SetMaxSize(*ngConfig.MaxSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodeGroup.ScalingConfig.MaxSize != ngGetterSetter.GetMaxSize() {
					return fmt.Errorf("validate update cluster: %w", ErrEKSMaxSizeMismatch)
				}
			}

			if ngConfig.AMI != nil {
				if nodeGroup.AmiType != *ngConfig.AMI {
					return fmt.Errorf("validate update cluster: %w", ErrEKSAMIMismatch)
				}

				if err := ngGetterSetter.SetAMI(*ngConfig.AMI); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if nodeGroup.AmiType != ngGetterSetter.GetAMI() {
					return fmt.Errorf("validate update cluster: %w", ErrEKSAMIMismatch)
				}
			}

			if ngConfig.DiskSize != nil {
				if *nodeGroup.DiskSize != *ngConfig.DiskSize {
					return fmt.Errorf("validate update cluster: %w", ErrEKSDiskSizeMismatch)
				}

				if err := ngGetterSetter.SetDiskSize(*ngConfig.DiskSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodeGroup.DiskSize != ngGetterSetter.GetDiskSize() {
					return fmt.Errorf("validate update cluster: %w", ErrEKSDiskSizeMismatch)
				}
			}
		} else if ngConfig == nil {
			// Nodegroup exists in asset but not in validator config.
			// Validator verifies the previous state.

			if *nodeGroup.NodegroupName != *ngGetterSetter.GetNodegroupName() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSNodeGroupsMismatch)
			}

			if *nodeGroup.Version != ngGetterSetter.GetK8SVersion() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSNodeGroupK8SVersionMismatch)
			}

			if nodeGroup.InstanceTypes[0] != ngGetterSetter.GetInstanceType() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSInstanceTypeMismatch)
			}

			if *nodeGroup.ScalingConfig.DesiredSize != ngGetterSetter.GetDesiredSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDesiredSizeMismatch)
			}

			if *nodeGroup.ScalingConfig.MinSize != ngGetterSetter.GetMinSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMinSizeMismatch)
			}

			if *nodeGroup.ScalingConfig.MaxSize != ngGetterSetter.GetMaxSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMaxSizeMismatch)
			}

			if nodeGroup.AmiType != ngGetterSetter.GetAMI() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSAMIMismatch)
			}

			if *nodeGroup.DiskSize != ngGetterSetter.GetDiskSize() {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDiskSizeMismatch)
			}
		} else {
			// Nodegroup exists in config but not in asset.
			// This is potentially a new nodegroup.
			// Validator checks if the nodegroup params are same as in validator config.
			if *nodeGroup.Version != *ngConfig.K8SVersion {
				return fmt.Errorf("validate update cluster: %w", ErrEKSNodeGroupK8SVersionMismatch)
			}

			if nodeGroup.InstanceTypes[0] != *ngConfig.InstanceType {
				return fmt.Errorf("validate update cluster: %w", ErrEKSInstanceTypeMismatch)
			}

			if *nodeGroup.ScalingConfig.DesiredSize != *ngConfig.DesiredSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDesiredSizeMismatch)
			}

			if *nodeGroup.ScalingConfig.MinSize != *ngConfig.MinSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMinSizeMismatch)
			}

			if *nodeGroup.ScalingConfig.MaxSize != *ngConfig.MaxSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSMaxSizeMismatch)
			}

			if nodeGroup.AmiType != *ngConfig.AMI {
				return fmt.Errorf("validate update cluster: %w", ErrEKSAMIMismatch)
			}

			if *nodeGroup.DiskSize != *ngConfig.DiskSize {
				return fmt.Errorf("validate update cluster: %w", ErrEKSDiskSizeMismatch)
			}

			if err := cluster.SetNodegroup(assets.NewEKSNodegroupDetail(nodeGroup.NodegroupName, *ngConfig.K8SVersion,
				*ngConfig.InstanceType, *ngConfig.DesiredSize, *ngConfig.MinSize, *ngConfig.MaxSize, *ngConfig.DiskSize,
				*ngConfig.AMI)); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		}
	}

	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if c.EKSConfig.EKSNodegroupConfig != nil {
		if len(allNodes) != totalEKSNodes {
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
		if len(allNodes) != totalEKSNodes {
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
