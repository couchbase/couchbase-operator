package k8sclustervalidator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/sirupsen/logrus"
)

const (
	gkePlatform    = managedk8sservices.Kubernetes
	gkeEnvironment = managedk8sservices.Cloud
	gkeProvider    = managedk8sservices.GCP
)

var (
	ErrInvalidGKEConfig              = errors.New("GKEConfig is nil and testAssets does not hold gke cluster details, validator does not know what to validate")
	ErrInvalidGKENodePoolConfig      = errors.New("GKENodePoolConfig is nil and testAssets does not hold gke node pool details, validator does not know what to validate")
	ErrGKENodePoolGSIsNil            = errors.New("GKENodePoolDetailGetterSetter is nil")
	ErrGKEK8SVersionMismatch         = errors.New("gke cluster k8s version mismatch")
	ErrGKEReleaseChannelMismatch     = errors.New("gke cluster release channel mismatch")
	ErrGKENodesMismatch              = errors.New("gke cluster nodes mismatch")
	ErrGKENodePoolsMismatch          = errors.New("gke node pool name mismatch")
	ErrGKENodePoolK8SVersionMismatch = errors.New("gke node pool k8s version mismatch")
	ErrGKEMachineTypeMismatch        = errors.New("gke node pool machine type mismatch")
	ErrGKEImageTypeMismatch          = errors.New("gke node pool image type mismatch")
	ErrGKEDiskTypeMismatch           = errors.New("gke node pool disk type mismatch")
	ErrGKEDiskSizeMismatch           = errors.New("gke node pool disk size mismatch")
	ErrGKENodeCountMismatch          = errors.New("gke node pool nodes count mismatch")
)

type GKEConfig struct {
	KubernetesVersion *string                            `yaml:"kubernetesVersion"`
	ReleaseChannel    *managedk8sservices.ReleaseChannel `yaml:"releaseChannel"`
	GKENodePoolConfig map[string]*GKENodePoolConfig      `yaml:"gkeNodePoolConfig"`
}

type GKENodePoolConfig struct {
	KubernetesVersion *string `yaml:"kubernetesVersion"`
	NumNodesPerZone   *int32  `yaml:"numNodesPerZone"`
	MachineType       *string `yaml:"machineType"`
	ImageType         *string `yaml:"imageType"`
	DiskType          *string `yaml:"diskType"`
	DiskSize          *int32  `yaml:"diskSize"`
}

type ValidateGKECluster struct {
	ClusterName string
	GKEConfig   *GKEConfig
}

/*
TODO : Also check if all the nodes are healthy and ready
TODO : When number of nodepools are updated, make sure the original nodes are intact and same.
TODO : Validate all values of GKEConfig before validating with cluster. Reduces gke calls and makes sure the yaml is correct.
TODO : Take config of each node pool as a param like in AKSValidator. This will allow for more granular control.
*/

func (c *ValidateGKECluster) ValidateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	/*
		If GKEConfig is nil and testAssets does not hold gkeClusterDetail for the cluster
			The validator does not know what to validate and will error out

		If GKEConfig is nil and testAssets does hold gkeClusterDetail for the cluster
			The validator checks and validates if the k8s environment is as per state held in testAssets and gkeClusterDetail

		If GKEConfig is not nil and testAssets does not hold gkeClusterDetail for the cluster
			The gke cluster is a new cluster and did not exist previously. Validator should validate the new cluster according to
			the config

		If GKEConfig is not nil and testAssets does hold gkeClusterDetail for the cluster
			There are changes to the cluster. Validator should validate the new cluster according to the config.
	*/

	/*
		Validator validates the following
		 - If the cluster exists
		 - If the k8s version is correct
		 - If there are right number of node pools, with correct machineType, diskType, etc.
		 - If the gke environment is as per the testAssets state
	*/

	logrus.Infof("Starting validation of GKE cluster `%s`", c.ClusterName)

	_, errConfig := checkConfigIsNil(c.GKEConfig)

	// When GKEConfig is nil and testAssets does not hold gkeClusterDetail
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		if errConfig == nil {
			return fmt.Errorf("validate cluster: %w", ErrInvalidGKEConfig)
		}
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetGKEClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		if errConfig == nil {
			return fmt.Errorf("validate cluster: %w", ErrInvalidGKEConfig)
		}
	}

	if (cluster == nil && k8sCluster != nil) || (cluster != nil && k8sCluster == nil) {
		return fmt.Errorf("validate cluster: %w", ErrTestAssetsNotPopulated)
	}

	// When GKEConfig is nil and testAssets does hold gkeClusterDetail
	if errConfig == nil && k8sCluster != nil {
		logrus.Infof("Validating the previous state of GKE cluster `%s`", c.ClusterName)
		if err := c.ValidatePrevState(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When GKEConfig is not nil and testAssets does not hold gkeClusterDetail
	if errConfig != nil && k8sCluster == nil {
		logrus.Infof("Validating the new GKE cluster `%s`", c.ClusterName)
		if err := c.ValidateNewCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When GKEConfig is not nil and testAssets does hold gkeClusterDetail
	if errConfig != nil && k8sCluster != nil {
		logrus.Infof("Validating the updates to GKE cluster `%s`", c.ClusterName)
		if err := c.ValidateUpdateCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	logrus.Infof("Validated GKE cluster %s successfully", c.ClusterName)

	return nil
}

func (c *ValidateGKECluster) ValidatePrevState(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetGKEClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != gkePlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != gkeEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != gkeProvider {
		return fmt.Errorf("validate prev state: %w", ErrServiceProviderMismatch)
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	gkeCluster, err := gkeSession.GetCluster(ctx)
	if err != nil {
		// Cluster does not exist in GKE
		return fmt.Errorf("validate prev state: %w", err)
	}

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	// Validating the GKE Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if strings.Split(gkeCluster.CurrentMasterVersion, "-")[0] != cluster.GetKubernetesVersion() {
		return fmt.Errorf("validate prev state: %w", ErrGKEK8SVersionMismatch)
	}

	if int(gkeCluster.ReleaseChannel.Channel) != managedk8sservices.ReleaseChannelMap[cluster.GetReleaseChannel()] {
		return fmt.Errorf("validate prev state: %w", ErrGKEReleaseChannelMismatch)
	}

	// Validating the GKE Node Pools Details
	for _, nodePool := range nodePools.NodePools {
		npGS := cluster.GetNodePoolDetailGetterSetter(nodePool.Name)
		if npGS == nil {
			return fmt.Errorf("validate prev state: %w", ErrGKENodePoolGSIsNil)
		}

		if nodePool.Name != npGS.GetNodePoolName() {
			return fmt.Errorf("validate prev state: %w", ErrGKENodePoolsMismatch)
		}

		if strings.Split(nodePool.Version, "-")[0] != npGS.GetKubernetesVersion() {
			return fmt.Errorf("validate prev state: %w", ErrGKENodePoolK8SVersionMismatch)
		}

		if nodePool.InitialNodeCount != npGS.GetNumNodesPerZone() {
			return fmt.Errorf("validate prev state: %w", ErrGKENodeCountMismatch)
		}

		if nodePool.Config.MachineType != npGS.GetMachineType() {
			return fmt.Errorf("validate prev state: %w", ErrGKEMachineTypeMismatch)
		}

		if !strings.EqualFold(nodePool.Config.ImageType, npGS.GetImageType()) {
			return fmt.Errorf("validate prev state: %w", ErrGKEImageTypeMismatch)
		}

		if nodePool.Config.DiskType != npGS.GetDiskType() {
			return fmt.Errorf("validate prev state: %w", ErrGKEDiskTypeMismatch)
		}

		if nodePool.Config.DiskSizeGb != npGS.GetDiskSize() {
			return fmt.Errorf("validate prev state: %w", ErrGKEDiskSizeMismatch)
		}
	}

	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if len(allNodes) != len(k8sCluster.GetNodes()) {
		return fmt.Errorf("validate prev state: %w", ErrGKENodesMismatch)
	}

	for _, nodeName := range allNodes {
		if !containsStrPtr(k8sCluster.GetNodes(), &nodeName) {
			return fmt.Errorf("validate prev state: %w", ErrGKENodesMismatch)
		}
	}

	return nil
}

func (c *ValidateGKECluster) ValidateNewCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	managedServiceProvider := managedk8sservices.NewManagedServiceProvider(gkePlatform, gkeEnvironment, gkeProvider)

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	gkeCluster, err := gkeSession.GetCluster(ctx)
	if err != nil {
		// Cluster does not exist in GKE
		return fmt.Errorf("validate new cluster: %w", err)
	}

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	// Validating GKE Cluster Details
	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if c.GKEConfig.KubernetesVersion == nil || strings.Split(gkeCluster.CurrentMasterVersion, "-")[0] != *c.GKEConfig.KubernetesVersion {
		return fmt.Errorf("validate new cluster: %w", ErrGKEK8SVersionMismatch)
	}

	if c.GKEConfig.ReleaseChannel == nil || int(gkeCluster.ReleaseChannel.Channel) != managedk8sservices.ReleaseChannelMap[*c.GKEConfig.ReleaseChannel] {
		return fmt.Errorf("validate new cluster: %w", ErrGKEReleaseChannelMismatch)
	}

	if len(nodePools.NodePools) != len(c.GKEConfig.GKENodePoolConfig) {
		return fmt.Errorf("validate new cluster: %w", ErrGKENodePoolsMismatch)
	}

	totalGKENodes := 0

	// Validating GKE Node Pool Details
	for _, nodePool := range nodePools.NodePools {
		totalGKENodes += int(nodePool.InitialNodeCount) * len(nodePool.Locations) // Node count is per zone

		npConfig := c.GKEConfig.GKENodePoolConfig[nodePool.Name]
		if npConfig == nil {
			return fmt.Errorf("validate new cluster: %w", ErrInvalidGKENodePoolConfig)
		}

		if npConfig.KubernetesVersion == nil || strings.Split(nodePool.Version, "-")[0] != *npConfig.KubernetesVersion {
			return fmt.Errorf("validate new cluster: %w", ErrGKENodePoolK8SVersionMismatch)
		}

		if npConfig.NumNodesPerZone == nil || nodePool.InitialNodeCount != *npConfig.NumNodesPerZone {
			return fmt.Errorf("validate new cluster: %w", ErrGKENodeCountMismatch)
		}

		if npConfig.MachineType == nil || nodePool.Config.MachineType != *npConfig.MachineType {
			return fmt.Errorf("validate new cluster: %w", ErrGKEMachineTypeMismatch)
		}

		if npConfig.ImageType == nil || !strings.EqualFold(nodePool.Config.ImageType, *npConfig.ImageType) {
			return fmt.Errorf("validate new cluster: %w", ErrGKEImageTypeMismatch)
		}

		if npConfig.DiskType == nil || nodePool.Config.DiskType != *npConfig.DiskType {
			return fmt.Errorf("validate new cluster: %w", ErrGKEDiskTypeMismatch)
		}

		if npConfig.DiskSize == nil || nodePool.Config.DiskSizeGb != *npConfig.DiskSize {
			return fmt.Errorf("validate new cluster: %w", ErrGKEDiskSizeMismatch)
		}
	}

	// TODO FIX if multiple contexts are present, then set the context correctly before fetching the node names
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if len(allNodes) != totalGKENodes {
		return fmt.Errorf("validate new cluster: %w", ErrGKENodesMismatch)
	}

	// Saving the GKE Cluster (with Node Pools) Details to TestAssets
	nodePoolDetailsMap := make(map[string]*assets.GKENodePoolDetail)

	for _, nodePool := range nodePools.NodePools {
		nodePoolDetailsMap[nodePool.Name] = assets.NewGKENodePoolDetail(nodePool.Name, *c.GKEConfig.KubernetesVersion, nodePool.Config.MachineType,
			nodePool.Config.ImageType, nodePool.Config.DiskType, nodePool.InitialNodeCount, nodePool.Config.DiskSizeGb)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetGKEClusterDetail(
		assets.NewGKEClusterDetail(c.ClusterName, *c.GKEConfig.KubernetesVersion, *c.GKEConfig.ReleaseChannel, nodePoolDetailsMap)); err != nil {
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

func (c *ValidateGKECluster) ValidateUpdateCluster(ctx context.Context, testAssets assets.TestAssetGetterSetter) error {
	k8sCluster, err := testAssets.GetK8SClustersGetterSetter().GetK8SClusterGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	cluster, err := testAssets.GetK8SClustersGetterSetter().GetGKEClusterDetailGetterSetter(c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if k8sCluster.GetServiceProvider().GetPlatform() != gkePlatform ||
		k8sCluster.GetServiceProvider().GetEnvironment() != gkeEnvironment ||
		k8sCluster.GetServiceProvider().GetProvider() != gkeProvider {
		return fmt.Errorf("validate update cluster: %w", ErrServiceProviderMismatch)
	}

	managedServiceProvider := k8sCluster.GetServiceProvider()

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, c.ClusterName)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	gkeSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	gkeCluster, err := gkeSession.GetCluster(ctx)
	if err != nil {
		// Cluster does not exist in GKE
		return fmt.Errorf("validate update cluster: %w", err)
	}

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	// Validating GKE Cluster Details
	if c.GKEConfig.KubernetesVersion != nil {
		if strings.Split(gkeCluster.CurrentMasterVersion, "-")[0] != *c.GKEConfig.KubernetesVersion {
			return fmt.Errorf("validate update cluster: %w", ErrGKEK8SVersionMismatch)
		}

		if err := cluster.SetKubernetesVersion(*c.GKEConfig.KubernetesVersion); err != nil {
			return fmt.Errorf("validate update cluster: %w", err)
		}
	} else {
		if strings.Split(gkeCluster.CurrentMasterVersion, "-")[0] != cluster.GetKubernetesVersion() {
			return fmt.Errorf("validate update cluster: %w", ErrGKEK8SVersionMismatch)
		}
	}

	if c.GKEConfig.ReleaseChannel != nil {
		if int(gkeCluster.ReleaseChannel.Channel) != managedk8sservices.ReleaseChannelMap[*c.GKEConfig.ReleaseChannel] {
			return fmt.Errorf("validate update cluster: %w", ErrGKEReleaseChannelMismatch)
		}

		if err := cluster.SetReleaseChannel(*c.GKEConfig.ReleaseChannel); err != nil {
			return fmt.Errorf("validate update cluster: %w", err)
		}
	} else {
		if int(gkeCluster.ReleaseChannel.Channel) != managedk8sservices.ReleaseChannelMap[cluster.GetReleaseChannel()] {
			return fmt.Errorf("validate update cluster: %w", ErrGKEReleaseChannelMismatch)
		}
	}

	// Validating GKE Node Pool Details
	totalGKENodes := 0

	for _, nodePool := range nodePools.NodePools {
		npValidatorConfig := c.GKEConfig.GKENodePoolConfig[nodePool.Name]
		npGetterSetter := cluster.GetNodePoolDetailGetterSetter(nodePool.Name)

		totalGKENodes += int(nodePool.InitialNodeCount) * len(nodePool.Locations) // Node count is per zone

		if npValidatorConfig == nil && npGetterSetter == nil {
			// Node pool does not exist in config and asset
			return fmt.Errorf("validate update cluster: %w", ErrInvalidGKENodePoolConfig)
		}

		if npValidatorConfig != nil && npGetterSetter != nil {
			// Node pool exists in both config and asset.
			// Possibly the node pool config has been updated.
			// Validator checks if the node pool is same as validator config.
			if npValidatorConfig.KubernetesVersion != nil {
				if strings.Split(nodePool.Version, "-")[0] != *npValidatorConfig.KubernetesVersion {
					return fmt.Errorf("validate update cluster: %w", ErrGKENodePoolK8SVersionMismatch)
				}

				if err := npGetterSetter.SetKubernetesVersion(*npValidatorConfig.KubernetesVersion); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if strings.Split(nodePool.Version, "-")[0] != npGetterSetter.GetKubernetesVersion() {
					return fmt.Errorf("validate update cluster: %w", ErrGKENodePoolK8SVersionMismatch)
				}
			}

			if npValidatorConfig.NumNodesPerZone != nil {
				if nodePool.InitialNodeCount != *npValidatorConfig.NumNodesPerZone {
					return fmt.Errorf("validate update cluster: %w", ErrGKENodeCountMismatch)
				}

				if err := npGetterSetter.SetNumNodesPerZone(*npValidatorConfig.NumNodesPerZone); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if nodePool.InitialNodeCount != npGetterSetter.GetNumNodesPerZone() {
					return fmt.Errorf("validate update cluster: %w", ErrGKENodeCountMismatch)
				}
			}

			if npValidatorConfig.MachineType != nil {
				if nodePool.Config.MachineType != *npValidatorConfig.MachineType {
					return fmt.Errorf("validate update cluster: %w", ErrGKEMachineTypeMismatch)
				}

				if err := npGetterSetter.SetMachineType(*npValidatorConfig.MachineType); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if nodePool.Config.MachineType != npGetterSetter.GetMachineType() {
					return fmt.Errorf("validate update cluster: %w", ErrGKEMachineTypeMismatch)
				}
			}

			if npValidatorConfig.ImageType != nil {
				if !strings.EqualFold(nodePool.Config.ImageType, *npValidatorConfig.ImageType) {
					return fmt.Errorf("validate update cluster: %w", ErrGKEImageTypeMismatch)
				}

				if err := npGetterSetter.SetImageType(*npValidatorConfig.ImageType); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if !strings.EqualFold(nodePool.Config.ImageType, npGetterSetter.GetImageType()) {
					return fmt.Errorf("validate update cluster: %w", ErrGKEImageTypeMismatch)
				}
			}

			if npValidatorConfig.DiskType != nil {
				if nodePool.Config.DiskType != *npValidatorConfig.DiskType {
					return fmt.Errorf("validate update cluster: %w", ErrGKEDiskTypeMismatch)
				}

				if err := npGetterSetter.SetDiskType(*npValidatorConfig.DiskType); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if nodePool.Config.DiskType != npGetterSetter.GetDiskType() {
					return fmt.Errorf("validate update cluster: %w", ErrGKEDiskTypeMismatch)
				}
			}

			if npValidatorConfig.DiskSize != nil {
				if nodePool.Config.DiskSizeGb != *npValidatorConfig.DiskSize {
					return fmt.Errorf("validate update cluster: %w", ErrGKEDiskSizeMismatch)
				}

				if err := npGetterSetter.SetDiskSize(*npValidatorConfig.DiskSize); err != nil {
					return fmt.Errorf("validate update cluster: %w", err)
				}
			} else {
				if nodePool.Config.DiskSizeGb != npGetterSetter.GetDiskSize() {
					return fmt.Errorf("validate update cluster: %w", ErrGKEDiskSizeMismatch)
				}
			}
		} else if npValidatorConfig == nil {
			// Node pool exists in asset but not in validator config.
			// Validator verifies the previous state.

			if nodePool.Name != npGetterSetter.GetNodePoolName() {
				return fmt.Errorf("validate update state: %w", ErrGKENodePoolsMismatch)
			}

			if strings.Split(nodePool.Version, "-")[0] != npGetterSetter.GetKubernetesVersion() {
				return fmt.Errorf("validate update state: %w", ErrGKENodePoolK8SVersionMismatch)
			}

			if nodePool.InitialNodeCount != npGetterSetter.GetNumNodesPerZone() {
				return fmt.Errorf("validate update state: %w", ErrGKENodeCountMismatch)
			}

			if nodePool.Config.MachineType != npGetterSetter.GetMachineType() {
				return fmt.Errorf("validate update state: %w", ErrGKEMachineTypeMismatch)
			}

			if !strings.EqualFold(nodePool.Config.ImageType, npGetterSetter.GetImageType()) {
				return fmt.Errorf("validate update state: %w", ErrGKEImageTypeMismatch)
			}

			if nodePool.Config.DiskType != npGetterSetter.GetDiskType() {
				return fmt.Errorf("validate update state: %w", ErrGKEDiskTypeMismatch)
			}

			if nodePool.Config.DiskSizeGb != npGetterSetter.GetDiskSize() {
				return fmt.Errorf("validate update state: %w", ErrGKEDiskSizeMismatch)
			}
		} else {
			// Node pool exists in config but not in asset.
			// This is potentially a new node pool.
			// Validator checks if the node pool params are same as in validator config.

			if strings.Split(nodePool.Version, "-")[0] != *npValidatorConfig.KubernetesVersion {
				return fmt.Errorf("validate update cluster: %w", ErrGKENodePoolK8SVersionMismatch)
			}

			if nodePool.InitialNodeCount != *npValidatorConfig.NumNodesPerZone {
				return fmt.Errorf("validate update cluster: %w", ErrGKENodeCountMismatch)
			}

			if nodePool.Config.MachineType != *npValidatorConfig.MachineType {
				return fmt.Errorf("validate update cluster: %w", ErrGKEMachineTypeMismatch)
			}

			if !strings.EqualFold(nodePool.Config.ImageType, *npValidatorConfig.ImageType) {
				return fmt.Errorf("validate update cluster: %w", ErrGKEImageTypeMismatch)
			}

			if nodePool.Config.DiskType != *npValidatorConfig.DiskType {
				return fmt.Errorf("validate update cluster: %w", ErrGKEDiskTypeMismatch)
			}

			if nodePool.Config.DiskSizeGb != *npValidatorConfig.DiskSize {
				return fmt.Errorf("validate update cluster: %w", ErrGKEDiskSizeMismatch)
			}

			if err := cluster.SetNodePoolDetail(assets.NewGKENodePoolDetail(nodePool.Name, *npValidatorConfig.KubernetesVersion,
				*npValidatorConfig.MachineType, *npValidatorConfig.ImageType, *npValidatorConfig.DiskType,
				*npValidatorConfig.NumNodesPerZone, *npValidatorConfig.DiskSize)); err != nil {
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

	if c.GKEConfig.GKENodePoolConfig != nil {
		if len(allNodes) != totalGKENodes {
			return fmt.Errorf("validate update cluster: %w", ErrGKENodesMismatch)
		}

		var nodeNames []*string

		for _, nodeName := range allNodes {
			nodeNames = append(nodeNames, &nodeName)
		}

		if err := k8sCluster.SetNodes(nodeNames); err != nil {
			return fmt.Errorf("validate update cluster: %w", err)
		}
	} else {
		if len(allNodes) != totalGKENodes {
			return fmt.Errorf("validate update cluster: %w", ErrGKENodesMismatch)
		}

		for _, nodeName := range allNodes {
			if !containsStrPtr(k8sCluster.GetNodes(), &nodeName) {
				return fmt.Errorf("validate update cluster: %w", ErrGKENodesMismatch)
			}
		}
	}

	return nil
}
