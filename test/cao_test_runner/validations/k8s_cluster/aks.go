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
	aksPlatform    = managedk8sservices.Kubernetes
	aksEnvironment = managedk8sservices.Cloud
	aksProvider    = managedk8sservices.Azure
)

var (
	ErrInvalidAKSConfig              = errors.New("AKSConfig is nil and testAssets does not hold aks cluster details, validator does not know what to validate")
	ErrInvalidAKSNodePoolConfig      = errors.New("AKSNodePoolConfig is nil and testAssets does not hold aks node pool details, validator does not know what to validate")
	ErrAKSNodePoolGSIsNil            = errors.New("AKSNodePoolDetailGetterSetter is nil")
	ErrAKSK8SVersionMismatch         = errors.New("aks cluster k8s version mismatch")
	ErrAKSNodesMismatch              = errors.New("aks cluster nodes mismatch")
	ErrAKSNodePoolsMismatch          = errors.New("aks node pool name mismatch")
	ErrAKSNodePoolK8SVersionMismatch = errors.New("aks node pool k8s version mismatch")
	ErrAKSVMSizeMismatch             = errors.New("aks node pool vm size mismatch")
	ErrAKSCountMismatch              = errors.New("aks node pool count mismatch")
	ErrAKSNodePoolModeMismatch       = errors.New("aks node pool mode mismatch")
	ErrAKSOSTypeMismatch             = errors.New("aks node pool os type mismatch")
	ErrAKSOSSKUMismatch              = errors.New("aks node pool os sku mismatch")
	ErrAKSDiskSizeMismatch           = errors.New("aks node pool disk size mismatch")
)

type AKSNodePoolConfig struct {
	Name       *string                            `yaml:"nodePoolName"`
	K8sVersion *string                            `yaml:"kubernetesVersion"`
	VMSize     *string                            `yaml:"vmSize"`
	Count      *int32                             `yaml:"count"`
	PoolMode   *armcontainerservice.AgentPoolMode `yaml:"poolMode"`
	OSSKU      *armcontainerservice.OSSKU         `yaml:"osSKU"`
	OSType     *armcontainerservice.OSType        `yaml:"osType"`
	DiskSize   *int32                             `yaml:"diskSize"`
}

type AKSConfig struct {
	KubernetesVersion *string                       `yaml:"kubernetesVersion"`
	AKSNodePoolConfig map[string]*AKSNodePoolConfig `yaml:"aksNodePoolConfig"`
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

	logrus.Infof("Starting validation of AKS cluster `%s`", c.ClusterName)

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
		logrus.Infof("Validating the previous state of AKS cluster `%s`", c.ClusterName)
		if err := c.ValidatePrevState(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When AKSConfig is not nil and testAssets does not hold aksClusterDetail
	if errConfig != nil && k8sCluster == nil {
		logrus.Infof("Validating the new AKS cluster `%s`", c.ClusterName)
		if err := c.ValidateNewCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When AKSConfig is not nil and testAssets does hold aksClusterDetail
	if errConfig != nil && k8sCluster != nil {
		logrus.Infof("Validating the updates to AKS cluster `%s`", c.ClusterName)
		if err := c.ValidateUpdateCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	logrus.Infof("Validated AKS cluster %s successfully", c.ClusterName)

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
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
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

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroupName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	// Validating the AKS Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if *aksCluster.Properties.KubernetesVersion != cluster.GetKubernetesVersion() {
		return fmt.Errorf("validate prev state: %w", ErrAKSK8SVersionMismatch)
	}

	allNodePoolsAsset := cluster.GetAllNodePoolsGetterSetter()

	if len(nodePools) != len(allNodePoolsAsset) {
		return fmt.Errorf("validate prev state: %w", ErrAKSNodePoolsMismatch)
	}

	// Validating the AKS Node Pools Details
	for _, nodePool := range nodePools {
		npGS := cluster.GetNodePoolGetterSetter(*nodePool.Name)
		if npGS == nil {
			return fmt.Errorf("validate prev state: %w", ErrAKSNodePoolGSIsNil)
		}

		if *nodePool.Name != npGS.GetNodePoolName() {
			return fmt.Errorf("validate prev state: %w", ErrAKSNodePoolsMismatch)
		}

		if *nodePool.Properties.OrchestratorVersion != npGS.GetKubernetesVersion() {
			return fmt.Errorf("validate prev state: %w", ErrAKSNodePoolK8SVersionMismatch)
		}

		if *nodePool.Properties.VMSize != npGS.GetVMSize() {
			return fmt.Errorf("validate prev state: %w", ErrAKSVMSizeMismatch)
		}

		if *nodePool.Properties.Count != npGS.GetCount() {
			return fmt.Errorf("validate prev state: %w", ErrAKSCountMismatch)
		}

		if *nodePool.Properties.Mode != npGS.GetNodePoolMode() {
			return fmt.Errorf("validate prev state: %w", ErrAKSNodePoolModeMismatch)
		}

		if *nodePool.Properties.OSType != npGS.GetOsType() {
			return fmt.Errorf("validate prev state: %w", ErrAKSOSTypeMismatch)
		}

		if *nodePool.Properties.OSSKU != npGS.GetOsSKU() {
			return fmt.Errorf("validate prev state: %w", ErrAKSOSSKUMismatch)
		}

		if *nodePool.Properties.OSDiskSizeGB != npGS.GetDiskSize() {
			return fmt.Errorf("validate prev state: %w", ErrAKSDiskSizeMismatch)
		}
	}

	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if len(allNodes) != len(k8sCluster.GetNodes()) {
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
	managedServiceProvider := managedk8sservices.NewManagedServiceProvider(aksPlatform, aksEnvironment, aksProvider)

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
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

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroup)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	// Validating AKS Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if *aksCluster.Properties.KubernetesVersion != *c.AKSConfig.KubernetesVersion {
		return fmt.Errorf("validate new cluster: %w", ErrAKSK8SVersionMismatch)
	}

	if len(nodePools) != len(c.AKSConfig.AKSNodePoolConfig) {
		return fmt.Errorf("validate new cluster: %w", ErrAKSNodePoolsMismatch)
	}

	nodePoolDetailsMap := make(map[string]*assets.AKSNodePool)
	totalAKSNodes := 0

	// Validating AKS Node Pool Details
	for _, nodePool := range nodePools {
		totalAKSNodes += int(*nodePool.Properties.Count)

		npConfig := c.AKSConfig.AKSNodePoolConfig[*nodePool.Name]
		if npConfig == nil {
			return fmt.Errorf("validate new cluster: %w", ErrInvalidAKSNodePoolConfig)
		}

		if *nodePool.Properties.OrchestratorVersion != *npConfig.K8sVersion {
			return fmt.Errorf("validate new cluster: %w", ErrAKSNodePoolK8SVersionMismatch)
		}

		if *nodePool.Properties.VMSize != *npConfig.VMSize {
			return fmt.Errorf("validate new cluster: %w", ErrAKSVMSizeMismatch)
		}

		if *nodePool.Properties.Count != *npConfig.Count {
			return fmt.Errorf("validate new cluster: %w", ErrAKSCountMismatch)
		}

		if *nodePool.Properties.Mode != *npConfig.PoolMode {
			return fmt.Errorf("validate new cluster: %w", ErrAKSNodePoolModeMismatch)
		}

		if *nodePool.Properties.OSType != *npConfig.OSType {
			return fmt.Errorf("validate new cluster: %w", ErrAKSOSTypeMismatch)
		}

		if *nodePool.Properties.OSSKU != *npConfig.OSSKU {
			return fmt.Errorf("validate new cluster: %w", ErrAKSOSSKUMismatch)
		}

		if *nodePool.Properties.OSDiskSizeGB != *npConfig.DiskSize {
			return fmt.Errorf("validate new cluster: %w", ErrAKSDiskSizeMismatch)
		}

		nodePoolDetailsMap[*nodePool.Name] = assets.NewAKSNodePool(*nodePool.Name, *npConfig.K8sVersion, *npConfig.Count,
			*npConfig.PoolMode, *npConfig.OSSKU, *npConfig.OSType, *npConfig.VMSize, *npConfig.DiskSize)
	}

	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if len(allNodes) != (totalAKSNodes) {
		return fmt.Errorf("validate new cluster: %w", ErrAKSNodesMismatch)
	}

	// Saving the AKS Cluster (with Node Pools) Details to TestAssets
	if err := testAssets.GetK8SClustersGetterSetter().SetAKSClusterDetail(
		assets.NewAKSClusterDetail(c.ClusterName, *c.AKSConfig.KubernetesVersion, nodePoolDetailsMap)); err != nil {
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
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
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

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroup)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	// Validating AKS Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

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

	// Validating AKS Node Pool Details
	totalAKSNodes := 0

	for _, nodePool := range nodePools {
		totalAKSNodes += int(*nodePool.Properties.Count)

		configNodePool := c.AKSConfig.AKSNodePoolConfig[*nodePool.Name]
		assetNodePool := cluster.GetNodePoolGetterSetter(*nodePool.Name)

		if configNodePool == nil && assetNodePool == nil {
			// Node pool does not exist in config and asset
			// Validator throws an error
			return fmt.Errorf("validate update cluster: %w", ErrInvalidAKSNodePoolConfig)
		} else if configNodePool != nil && assetNodePool != nil {
			// Node Pool exists in both config and asset - This could mean update of nodepool
			// Validator checks if the node pool is same as config
			if c.AKSConfig.KubernetesVersion != nil {
				if *nodePool.Properties.OrchestratorVersion != *configNodePool.K8sVersion {
					return fmt.Errorf("validate update cluster: %w", ErrAKSNodePoolK8SVersionMismatch)
				}

				if err := cluster.SetKubernetesVersion(*configNodePool.K8sVersion); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.OrchestratorVersion != assetNodePool.GetKubernetesVersion() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSNodePoolK8SVersionMismatch)
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

			if configNodePool.PoolMode != nil {
				if *nodePool.Properties.Mode != *configNodePool.PoolMode {
					return fmt.Errorf("validate update cluster: %w", ErrAKSNodePoolModeMismatch)
				}

				if err := assetNodePool.SetNodePoolMode(*configNodePool.PoolMode); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.Mode != assetNodePool.GetNodePoolMode() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSNodePoolModeMismatch)
				}
			}

			if configNodePool.OSType != nil {
				if *nodePool.Properties.OSType != *configNodePool.OSType {
					return fmt.Errorf("validate update cluster: %w", ErrAKSOSTypeMismatch)
				}

				if err := assetNodePool.SetOsType(*configNodePool.OSType); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.OSType != assetNodePool.GetOsType() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSOSTypeMismatch)
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

			if configNodePool.DiskSize != nil {
				if *nodePool.Properties.OSDiskSizeGB != *configNodePool.DiskSize {
					return fmt.Errorf("validate update cluster: %w", ErrAKSDiskSizeMismatch)
				}

				if err := assetNodePool.SetDiskSize(*configNodePool.DiskSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if *nodePool.Properties.OSDiskSizeGB != assetNodePool.GetDiskSize() {
					return fmt.Errorf("validate update cluster: %w", ErrAKSDiskSizeMismatch)
				}
			}
		} else if configNodePool == nil {
			// Node Pool exists in asset but not in config
			// Validator checks if the node pool is same as asset (previous state check)

			if *nodePool.Properties.OrchestratorVersion != assetNodePool.GetKubernetesVersion() {
				return fmt.Errorf("validate update state: %w", ErrAKSNodePoolK8SVersionMismatch)
			}

			if *nodePool.Properties.VMSize != assetNodePool.GetVMSize() {
				return fmt.Errorf("validate update state: %w", ErrAKSVMSizeMismatch)
			}

			if *nodePool.Properties.Count != assetNodePool.GetCount() {
				return fmt.Errorf("validate update state: %w", ErrAKSCountMismatch)
			}

			if *nodePool.Properties.Mode != assetNodePool.GetNodePoolMode() {
				return fmt.Errorf("validate update state: %w", ErrAKSNodePoolModeMismatch)
			}

			if *nodePool.Properties.OSType != assetNodePool.GetOsType() {
				return fmt.Errorf("validate update state: %w", ErrAKSOSTypeMismatch)
			}

			if *nodePool.Properties.OSSKU != assetNodePool.GetOsSKU() {
				return fmt.Errorf("validate update state: %w", ErrAKSOSSKUMismatch)
			}

			if *nodePool.Properties.OSDiskSizeGB != assetNodePool.GetDiskSize() {
				return fmt.Errorf("validate update state: %w", ErrAKSDiskSizeMismatch)
			}
		} else {
			// Node Pool exists in config but not in asset - This could be a new node pool
			// Validator checks if the node pool is same as config (new cluster check)

			if *nodePool.Properties.OrchestratorVersion != *configNodePool.K8sVersion {
				return fmt.Errorf("validate update cluster: %w", ErrAKSNodePoolK8SVersionMismatch)
			}

			if *nodePool.Properties.VMSize != *configNodePool.VMSize {
				return fmt.Errorf("validate update cluster: %w", ErrAKSVMSizeMismatch)
			}

			if *nodePool.Properties.Count != *configNodePool.Count {
				return fmt.Errorf("validate update cluster: %w", ErrAKSCountMismatch)
			}

			if *nodePool.Properties.Mode != *configNodePool.PoolMode {
				return fmt.Errorf("validate update cluster: %w", ErrAKSNodePoolModeMismatch)
			}

			if *nodePool.Properties.OSType != *configNodePool.OSType {
				return fmt.Errorf("validate update cluster: %w", ErrAKSOSTypeMismatch)
			}

			if *nodePool.Properties.OSSKU != *configNodePool.OSSKU {
				return fmt.Errorf("validate update cluster: %w", ErrAKSOSSKUMismatch)
			}

			if *nodePool.Properties.OSDiskSizeGB != *configNodePool.DiskSize {
				return fmt.Errorf("validate update cluster: %w", ErrAKSDiskSizeMismatch)
			}

			if err := cluster.SetNodePool(assets.NewAKSNodePool(*configNodePool.Name, *configNodePool.K8sVersion, *configNodePool.Count,
				*configNodePool.PoolMode, *configNodePool.OSSKU, *configNodePool.OSType, *configNodePool.VMSize, *configNodePool.DiskSize)); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		}
	}

	// Verifying the number of nodes across all the node pools.
	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if len(c.AKSConfig.AKSNodePoolConfig) != 0 {
		if len(allNodes) != totalAKSNodes {
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
		if len(allNodes) != totalAKSNodes {
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
