package k8sclustervalidator

import (
	"context"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/sirupsen/logrus"
)

const (
	aksPlatform    = assets.Kubernetes
	aksEnvironment = assets.Cloud
	aksProvider    = assets.Azure
)

var (
	ErrInvalidAKSConfig              = errors.New("aks config is nil, testAssets does not hold cluster details. Validator does not know what to validate")
	ErrAKSK8SVersionMismatch         = errors.New("aks k8s version mismatch")
	ErrAKSNodePoolsMismatch          = errors.New("aks node pools mismatch")
	ErrAKSOSTypeMismatch             = errors.New("aks os type mismatch")
	ErrAKSOSSKUMismatch              = errors.New("aks os sku mismatch")
	ErrAKSDiskSizeNodePoolMismatch   = errors.New("aks disk size node pool mismatch")
	ErrAKSCountMismatch              = errors.New("aks count mismatch")
	ErrAKSVMSizeMismatch             = errors.New("aks vm size mismatch")
	ErrAKSK8SVersionNodePoolMismatch = errors.New("aks k8s version node pool mismatch")
	ErrAKSNodesMismatch              = errors.New("aks cluster nodes mismatch")
)

type NodePoolConfig struct {
	Name     *string                     `yaml:"nodePoolName"`
	OSSKU    *armcontainerservice.OSSKU  `yaml:"osSKU"`
	OSType   *armcontainerservice.OSType `yaml:"osType"`
	Count    *int32                      `yaml:"count"`
	VMSize   *string                     `yaml:"vmSize"`
	DiskSize *int32                      `yaml:"diskSize"`
}

type AKSConfig struct {
	KubernetesVersion *string           `yaml:"kubernetesVersion"`
	NodePoolConfig    []*NodePoolConfig `yaml:"nodePoolConfig"`
}

type ValidateAKSCluster struct {
	ClusterName string
	AKSConfig   *AKSConfig
}

/*
TODO : Also check if all the nodes are healthy and ready
TODO : When number of nodepools are updated, make sure the original nodes are intact and same.
TODO : Validate all values of AKSConfig before validating with cluster. Reduces aks calls and makes sure the yaml is correct.
*/
func (c *ValidateAKSCluster) ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		If AKSConfig is nil and testAssets does not hold aksClusterDetail for the cluster
			The validator does not know what to validate and will error out

		If AKSConfig is nil and testAssets does hold aksClusterDetail for the cluster
			The validator checks and validates if the k8s environment is as per state held in testAssets and aksClusterDetail

		If AKSConfig is not nil and testAssets does not hold aksClusterDetail for the cluster
			The aks cluster is a new cluster and did not exist previously. Validator should validate the new cluster according to
			the config

		If AKSConfig is not nil and testAssets does hold aksClusterDetail for the cluster
			There are changes to the cluster. Validator should validate the new cluster according to the config.
	*/

	/*
		Validator validates the following
		 - If the cluster exists
		 - If the k8s version is correct
		 - If there are right number of node pools, with correct osSku, osType, etc.
		 - If the aks environment is as per the testAssets state
	*/

	_, errConfig := checkConfigIsNil(c.AKSConfig)

	// When AKSConfig is nil and testAssets does not hold aksClusterDetail
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		if errConfig == nil {
			return fmt.Errorf("validate cluster: %w", ErrInvalidAKSConfig)
		}
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetAKSClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		if errConfig == nil {
			return fmt.Errorf("validate cluster: %w", ErrInvalidAKSConfig)
		}
	}

	if (cluster == nil && k8sCluster != nil) || (cluster != nil && k8sCluster == nil) {
		return fmt.Errorf("validate cluster: %w", ErrTestAssetsNotPopulated)
	}

	// When AKSConfig is nil and testAssets does hold aksClusterDetail
	if errConfig == nil && k8sCluster != nil {
		logrus.Infof("Validating prev state")
		if err := c.ValidatePrevState(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When AKSConfig is not nil and testAssets does not hold aksClusterDetail
	if errConfig != nil && k8sCluster == nil {
		logrus.Infof("Validating new cluster")
		if err := c.ValidateNewCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When AKSConfig is not nil and testAssets does hold aksClusterDetail
	if errConfig != nil && k8sCluster != nil {
		logrus.Infof("Validating cluster post updates")
		if err := c.ValidateUpdateCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	logrus.Infof("Cluster %s successfully validated in AKS", c.ClusterName)

	return nil
}

func (c *ValidateAKSCluster) ValidatePrevState(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetAKSClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != aksPlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != aksEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != aksProvider {
		return fmt.Errorf("validate prev state: %w", ErrServiceProviderMismatch)
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*assets.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	resourceGroupName := c.ClusterName + "-rg"

	aksCluster, err := aksSession.GetCluster(ctx, resourceGroupName)
	if err != nil {
		// Cluster does not exist in AKS
		return fmt.Errorf("validate prev state: %w", err)
	}

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if *aksCluster.Properties.KubernetesVersion != cluster.GetKubernetesVersion() {
		return fmt.Errorf("validate prev state: %w", ErrAKSK8SVersionMismatch)
	}

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroupName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	allNodePoolsAsset := cluster.GetAllNodePoolsGetterSetter()

	if len(nodePools) != len(allNodePoolsAsset) {
		return fmt.Errorf("validate prev state: %w", ErrAKSNodePoolsMismatch)
	}

	for _, nodePool := range nodePools {
		var assetNodePool assets.AKSNodePoolGetterSetter
		assetNodePool = nil

		for _, nodePoolAsset := range allNodePoolsAsset {
			if *nodePool.Name == nodePoolAsset.GetNodePoolName() {
				assetNodePool = nodePoolAsset
				break
			}
		}

		if assetNodePool == nil {
			return fmt.Errorf("validate prev state: %w", ErrAKSNodePoolsMismatch)
		}

		if *nodePool.Properties.Count != assetNodePool.GetCount() {
			return fmt.Errorf("validate prev state: %w", ErrAKSCountMismatch)
		}

		if *nodePool.Properties.OSSKU != assetNodePool.GetOsSKU() {
			return fmt.Errorf("validate prev state: %w", ErrAKSOSSKUMismatch)
		}

		if *nodePool.Properties.OSType != assetNodePool.GetOsType() {
			return fmt.Errorf("validate prev state: %w", ErrAKSOSTypeMismatch)
		}

		if *nodePool.Properties.VMSize != assetNodePool.GetVMSize() {
			return fmt.Errorf("validate prev state: %w", ErrAKSVMSizeMismatch)
		}

		if *nodePool.Properties.OSDiskSizeGB != assetNodePool.GetDiskSize() {
			return fmt.Errorf("validate prev state: %w", ErrAKSDiskSizeNodePoolMismatch)
		}

		if *nodePool.Properties.OrchestratorVersion != cluster.GetKubernetesVersion() {
			return fmt.Errorf("validate prev state: %w", ErrAKSK8SVersionNodePoolMismatch)
		}
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	totalNodesCount := 0
	for _, nodePool := range allNodePoolsAsset {
		totalNodesCount += int(nodePool.GetCount())
	}

	if len(allNodes) != len(k8sCluster.GetNodes()) {
		return fmt.Errorf("validate prev state: %w", ErrAKSNodesMismatch)
	}

	if len(allNodes) != totalNodesCount {
		return fmt.Errorf("validate prev state: %w", ErrAKSNodesMismatch)
	}

	for _, nodeName := range allNodes {
		if !containsStrPtr(k8sCluster.GetNodes(), &nodeName) {
			return fmt.Errorf("validate prev state: %w", ErrAKSNodesMismatch)
		}
	}

	return nil
}

func (c *ValidateAKSCluster) ValidateNewCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	managedServiceProvider := assets.NewManagedServiceProvider(aksPlatform, aksEnvironment, aksProvider)

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*assets.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	resourceGroup := c.ClusterName + "-rg"

	aksCluster, err := aksSession.GetCluster(ctx, resourceGroup)
	if err != nil {
		// Cluster does not exist in AKS
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if *aksCluster.Properties.KubernetesVersion != *c.AKSConfig.KubernetesVersion {
		return fmt.Errorf("validate new cluster: %w", ErrAKSK8SVersionMismatch)
	}

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroup)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if len(nodePools) != len(c.AKSConfig.NodePoolConfig) {
		return fmt.Errorf("validate new cluster: %w", ErrAKSNodePoolsMismatch)
	}

	var assetNodePools []*assets.AKSNodePool

	for _, nodePool := range nodePools {
		var configNodePool *NodePoolConfig
		configNodePool = nil

		for _, nodePoolConfig := range c.AKSConfig.NodePoolConfig {
			if *nodePool.Name == *nodePoolConfig.Name {
				configNodePool = nodePoolConfig
				break
			}
		}

		if configNodePool == nil {
			return fmt.Errorf("validate new cluster: %w", ErrAKSNodePoolsMismatch)
		}

		if *nodePool.Properties.Count != *configNodePool.Count {
			return fmt.Errorf("validate new cluster: %w", ErrAKSCountMismatch)
		}

		if *nodePool.Properties.OSSKU != *configNodePool.OSSKU {
			return fmt.Errorf("validate new cluster: %w", ErrAKSOSSKUMismatch)
		}

		if *nodePool.Properties.OSType != *configNodePool.OSType {
			return fmt.Errorf("validate new cluster: %w", ErrAKSOSTypeMismatch)
		}

		if *nodePool.Properties.VMSize != *configNodePool.VMSize {
			return fmt.Errorf("validate new cluster: %w", ErrAKSVMSizeMismatch)
		}

		if *nodePool.Properties.OSDiskSizeGB != *configNodePool.DiskSize {
			return fmt.Errorf("validate new cluster: %w", ErrAKSDiskSizeNodePoolMismatch)
		}

		if *nodePool.Properties.OrchestratorVersion != *c.AKSConfig.KubernetesVersion {
			return fmt.Errorf("validate new cluster: %w", ErrAKSK8SVersionNodePoolMismatch)
		}

		assetNodePools = append(assetNodePools, assets.NewAKSNodePool(*nodePool.Name, *configNodePool.OSSKU,
			*configNodePool.OSType, *configNodePool.Count, *configNodePool.VMSize, *configNodePool.DiskSize))
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	totalNodesCount := 0
	for _, nodePool := range assetNodePools {
		totalNodesCount += int(nodePool.GetCount())
	}

	if len(allNodes) != (totalNodesCount) {
		return fmt.Errorf("validate new cluster: %w", ErrAKSCountMismatch)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetAKSClusterDetail(
		assets.NewAKSClusterDetail(c.ClusterName, assetNodePools, *c.AKSConfig.KubernetesVersion)); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	var nodes []*string
	for _, nodeName := range allNodes {
		nodes = append(nodes, &nodeName)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetK8SCluster(
		assets.NewK8SCluster(c.ClusterName, managedServiceProvider, nodes)); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	return nil
}

func (c *ValidateAKSCluster) ValidateUpdateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetAKSClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != aksPlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != aksEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != aksProvider {
		return fmt.Errorf("validate update cluster: %w", ErrServiceProviderMismatch)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*assets.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	resourceGroup := c.ClusterName + "-rg"

	aksCluster, err := aksSession.GetCluster(ctx, resourceGroup)
	if err != nil {
		// Cluster does not exist in AKS
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	// Check if the k8s version is correct
	if c.AKSConfig.KubernetesVersion != nil {
		if *aksCluster.Properties.KubernetesVersion != *c.AKSConfig.KubernetesVersion {
			return fmt.Errorf("validate update cluster: %w", ErrAKSK8SVersionMismatch)
		}

		if err := cluster.SetKubernetesVersion(*aksCluster.Properties.KubernetesVersion); err != nil {
			return fmt.Errorf("validate update cluster: %w", err)
		}
	} else {
		if *aksCluster.Properties.KubernetesVersion != cluster.GetKubernetesVersion() {
			return fmt.Errorf("validate update cluster: %w", ErrAKSK8SVersionMismatch)
		}
	}

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroup)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	for _, nodePool := range nodePools {
		var configNodePool *NodePoolConfig
		var assetNodePool assets.AKSNodePoolGetterSetter

		configNodePool = nil
		assetNodePool = nil

		for _, nodePoolConfig := range c.AKSConfig.NodePoolConfig {
			if *nodePool.Name == *nodePoolConfig.Name {
				configNodePool = nodePoolConfig
				break
			}
		}

		for _, nodePoolAsset := range cluster.GetAllNodePoolsGetterSetter() {
			if *nodePool.Name == nodePoolAsset.GetNodePoolName() {
				assetNodePool = nodePoolAsset
				break
			}
		}

		if configNodePool == nil && assetNodePool == nil {
			// Node pool does not exist in config and asset
			// Validator throws an error
			return fmt.Errorf("validate update cluster: %w", ErrAKSNodePoolsMismatch)
		} else if configNodePool != nil && assetNodePool != nil {
			// Node Pool exists in both config and asset - This could mean update of nodepool
			// Validator checks if the node pool is same as config

			if configNodePool.Count != nil {
				if *nodePool.Properties.Count != *configNodePool.Count {
					return fmt.Errorf("validate update cluster: %w", ErrAKSCountMismatch)
				}

				if err := assetNodePool.SetCount(*configNodePool.Count); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.Count != assetNodePool.GetCount() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSCountMismatch)
				}
			}

			if configNodePool.OSSKU != nil {
				if *nodePool.Properties.OSSKU != *configNodePool.OSSKU {
					return fmt.Errorf("validate update cluster: %w", ErrAKSOSSKUMismatch)
				}

				if err := assetNodePool.SetOsSKU(*configNodePool.OSSKU); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.OSSKU != assetNodePool.GetOsSKU() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSOSSKUMismatch)
				}
			}

			if configNodePool.VMSize != nil {
				if *nodePool.Properties.VMSize != *configNodePool.VMSize {
					return fmt.Errorf("validate update cluster: %w", ErrAKSVMSizeMismatch)
				}

				if err := assetNodePool.SetVMSize(*configNodePool.VMSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.VMSize != assetNodePool.GetVMSize() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSVMSizeMismatch)
				}
			}

			if configNodePool.DiskSize != nil {
				if *nodePool.Properties.OSDiskSizeGB != *configNodePool.DiskSize {
					return fmt.Errorf("validate update cluster: %w", ErrAKSDiskSizeNodePoolMismatch)
				}

				if err := assetNodePool.SetDiskSize(*configNodePool.DiskSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.OSDiskSizeGB != assetNodePool.GetDiskSize() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSDiskSizeNodePoolMismatch)
				}
			}

			if c.AKSConfig.KubernetesVersion != nil {
				if *nodePool.Properties.OrchestratorVersion != *c.AKSConfig.KubernetesVersion {
					return fmt.Errorf("validate update cluster: %w", ErrAKSK8SVersionNodePoolMismatch)
				}

				if err := cluster.SetKubernetesVersion(*c.AKSConfig.KubernetesVersion); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)

				}
			} else {
				if *nodePool.Properties.OrchestratorVersion != cluster.GetKubernetesVersion() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSK8SVersionNodePoolMismatch)
				}
			}

		} else if configNodePool == nil {
			// Node Pool exists in asset but not in config
			// Validator checks if the node pool is same as asset (previous state check)

			if *nodePool.Properties.Count != assetNodePool.GetCount() {
				return fmt.Errorf("validate prev state: %w", ErrAKSCountMismatch)
			}

			if *nodePool.Properties.OSSKU != assetNodePool.GetOsSKU() {
				return fmt.Errorf("validate prev state: %w", ErrAKSOSSKUMismatch)
			}

			if *nodePool.Properties.OSType != assetNodePool.GetOsType() {
				return fmt.Errorf("validate prev state: %w", ErrAKSOSTypeMismatch)
			}

			if *nodePool.Properties.VMSize != assetNodePool.GetVMSize() {
				return fmt.Errorf("validate prev state: %w", ErrAKSVMSizeMismatch)
			}

			if *nodePool.Properties.OSDiskSizeGB != assetNodePool.GetDiskSize() {
				return fmt.Errorf("validate prev state: %w", ErrAKSDiskSizeNodePoolMismatch)
			}

			if *nodePool.Properties.OrchestratorVersion != cluster.GetKubernetesVersion() {
				return fmt.Errorf("validate prev state: %w", ErrAKSK8SVersionNodePoolMismatch)
			}
		} else {
			// Node Pool exists in config but not in asset - This could be a new node pool
			// Validator checks if the node pool is same as config (new cluster check)

			if configNodePool == nil {
				return fmt.Errorf("validate new cluster: %w", ErrAKSNodePoolsMismatch)
			}

			if *nodePool.Properties.Count != *configNodePool.Count {
				return fmt.Errorf("validate new cluster: %w", ErrAKSCountMismatch)
			}

			if *nodePool.Properties.OSSKU != *configNodePool.OSSKU {
				return fmt.Errorf("validate new cluster: %w", ErrAKSOSSKUMismatch)
			}

			if *nodePool.Properties.OSType != *configNodePool.OSType {
				return fmt.Errorf("validate new cluster: %w", ErrAKSOSTypeMismatch)
			}

			if *nodePool.Properties.VMSize != *configNodePool.VMSize {
				return fmt.Errorf("validate new cluster: %w", ErrAKSVMSizeMismatch)
			}

			if *nodePool.Properties.OSDiskSizeGB != *configNodePool.DiskSize {
				return fmt.Errorf("validate new cluster: %w", ErrAKSDiskSizeNodePoolMismatch)
			}

			if *nodePool.Properties.OrchestratorVersion != *c.AKSConfig.KubernetesVersion {
				return fmt.Errorf("validate new cluster: %w", ErrAKSK8SVersionNodePoolMismatch)
			}

			if err := cluster.SetNodePool(assets.NewAKSNodePool(*configNodePool.Name, *configNodePool.OSSKU,
				*configNodePool.OSType, *configNodePool.Count, *configNodePool.VMSize, *configNodePool.DiskSize)); err != nil {
				return fmt.Errorf("validate new cluster: %w", err)
			}
		}
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	totalNodesCount := 0
	for _, nodePool := range cluster.GetAllNodePoolsGetterSetter() {
		totalNodesCount += int(nodePool.GetCount())
	}

	if len(c.AKSConfig.NodePoolConfig) != 0 {

		if len(allNodes) != totalNodesCount {
			return fmt.Errorf("validate update cluster: %w", ErrAKSNodesMismatch)
		}

		var nodeNames []*string

		for _, nodeName := range allNodes {
			nodeNames = append(nodeNames, &nodeName)
		}

		if err := k8sCluster.SetNodes(nodeNames); err != nil {
			return fmt.Errorf("validate update cluster: %w", err)
		}
	} else {
		if len(allNodes) != totalNodesCount {
			return fmt.Errorf("validate update cluster: %w", ErrAKSNodesMismatch)
		}

		for _, nodeName := range allNodes {
			if !containsStrPtr(k8sCluster.GetNodes(), &nodeName) {
				return fmt.Errorf("validate update cluster: %w", ErrAKSNodesMismatch)
			}
		}
	}

	return nil
}
