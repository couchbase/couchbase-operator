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
	ErrInvalidGKEConfig              = errors.New("gke config is nil, testAssets does not hold cluster details. Validator does not know what to validate")
	ErrGKEK8SVersionMismatch         = errors.New("gke k8s version mismatch")
	ErrGKENodePoolsMismatch          = errors.New("gke node pools mismatch")
	ErrGKEMachineTypeMismatch        = errors.New("gke machine type mismatch")
	ErrGKEImageTypeMismatch          = errors.New("gke image type mismatch")
	ErrGKEDiskTypeMismatch           = errors.New("gke disk type mismatch")
	ErrGKEDiskSizeNodePoolMismatch   = errors.New("gke disk size node pool mismatch")
	ErrGKECountMismatch              = errors.New("gke count mismatch")
	ErrGKEReleaseChannelMismatch     = errors.New("gke release channel mismatch")
	ErrGKEK8SVersionNodePoolMismatch = errors.New("gke k8s version node pool mismatch")
	ErrGKENodesMismatch              = errors.New("gke cluster nodes mismatch")
)

type GKEConfig struct {
	KubernetesVersion *string                            `yaml:"kubernetesVersion"`
	MachineType       *string                            `yaml:"machineType"`
	ImageType         *string                            `yaml:"imageType"`
	DiskType          *string                            `yaml:"diskType"`
	Count             *int32                             `yaml:"count"`
	DiskSize          *int32                             `yaml:"diskSize"`
	NumNodePools      *int                               `yaml:"numNodePools"`
	ReleaseChannel    *managedk8sservices.ReleaseChannel `yaml:"releaseChannel"`
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
		logrus.Infof("Validating prev state")
		if err := c.ValidatePrevState(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When GKEConfig is not nil and testAssets does not hold gkeClusterDetail
	if errConfig != nil && k8sCluster == nil {
		logrus.Infof("Validating new cluster")
		if err := c.ValidateNewCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	// When GKEConfig is not nil and testAssets does hold gkeClusterDetail
	if errConfig != nil && k8sCluster != nil {
		logrus.Infof("Validating cluster post updates")
		if err := c.ValidateUpdateCluster(ctx, testAssets); err != nil {
			return fmt.Errorf("validate cluster: %w", err)
		}
	}

	logrus.Infof("Cluster %s successfully validated in EKS", c.ClusterName)

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

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if strings.Split(gkeCluster.CurrentMasterVersion, "-")[0] != cluster.GetKubernetesVersion() {
		return fmt.Errorf("validate prev state: %w", ErrGKEK8SVersionMismatch)
	}

	if int(gkeCluster.ReleaseChannel.Channel) != managedk8sservices.ReleaseChannelMap[cluster.GetReleaseChannel()] {
		return fmt.Errorf("validate prev state: %w", ErrGKEReleaseChannelMismatch)
	}

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("validate prev state: %w", err)
	}

	if len(nodePools.NodePools) != len(cluster.GetAllNodePools()) {
		return fmt.Errorf("validate prev state: %w", ErrEKSNodeGroupsMismatch)
	}

	for _, nodePool := range nodePools.NodePools {
		if nodePool.Config.MachineType != cluster.GetMachineType() {
			return fmt.Errorf("validate prev state: %w", ErrGKEMachineTypeMismatch)
		}

		if !strings.EqualFold(nodePool.Config.ImageType, cluster.GetImageType()) {
			return fmt.Errorf("validate prev state: %w", ErrGKEImageTypeMismatch)
		}

		if nodePool.Config.DiskType != cluster.GetDiskType() {
			return fmt.Errorf("validate prev state: %w", ErrGKEDiskTypeMismatch)
		}

		if nodePool.InitialNodeCount != cluster.GetCount() {
			return fmt.Errorf("validate prev state: %w", ErrGKECountMismatch)
		}

		if nodePool.Config.DiskSizeGb != cluster.GetDiskSize() {
			return fmt.Errorf("validate prev state: %w", ErrGKEDiskSizeNodePoolMismatch)
		}

		if strings.Split(nodePool.Version, "-")[0] != cluster.GetKubernetesVersion() {
			return fmt.Errorf("validate prev state: %w", ErrGKEK8SVersionNodePoolMismatch)
		}
	}

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

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if strings.Split(gkeCluster.CurrentMasterVersion, "-")[0] != *c.GKEConfig.KubernetesVersion {
		return fmt.Errorf("validate new cluster: %w", ErrGKEK8SVersionMismatch)
	}

	if int(gkeCluster.ReleaseChannel.Channel) != managedk8sservices.ReleaseChannelMap[*c.GKEConfig.ReleaseChannel] {
		return fmt.Errorf("validate new cluster: %w", ErrGKEReleaseChannelMismatch)
	}

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if len(nodePools.NodePools) != *c.GKEConfig.NumNodePools {
		return fmt.Errorf("validate new cluster: %w", ErrGKENodePoolsMismatch)
	}

	var nodePoolNames []*string

	for _, nodePool := range nodePools.NodePools {
		nodePoolNames = append(nodePoolNames, &nodePool.Name)

		if nodePool.Config.MachineType != *c.GKEConfig.MachineType {
			return fmt.Errorf("validate new cluster: %w", ErrGKEMachineTypeMismatch)
		}

		if !strings.EqualFold(nodePool.Config.ImageType, *c.GKEConfig.ImageType) {
			return fmt.Errorf("validate new cluster: %w", ErrGKEImageTypeMismatch)
		}

		if nodePool.Config.DiskType != *c.GKEConfig.DiskType {
			return fmt.Errorf("validate new cluster: %w", ErrGKEDiskTypeMismatch)
		}

		if nodePool.Config.DiskSizeGb != *c.GKEConfig.DiskSize {
			return fmt.Errorf("validate new cluster: %w", ErrGKEDiskSizeNodePoolMismatch)
		}

		if nodePool.InitialNodeCount != *c.GKEConfig.Count {
			return fmt.Errorf("validate new cluster: %w", ErrGKECountMismatch)
		}

		if strings.Split(nodePool.Version, "-")[0] != *c.GKEConfig.KubernetesVersion {
			return fmt.Errorf("validate new cluster: %w", ErrGKEK8SVersionNodePoolMismatch)
		}
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate new cluster: %w", err)
	}

	if len(allNodes) != *c.GKEConfig.NumNodePools*int(*c.GKEConfig.Count) {
		return fmt.Errorf("validate new cluster: %w", ErrGKENodesMismatch)
	}

	if err := testAssets.GetK8SClustersGetterSetter().SetGKEClusterDetail(
		assets.NewGKEClusterDetail(c.ClusterName, nodePoolNames, *c.GKEConfig.KubernetesVersion,
			*c.GKEConfig.MachineType, *c.GKEConfig.ImageType, *c.GKEConfig.DiskType,
			*c.GKEConfig.DiskSize, *c.GKEConfig.Count, *c.GKEConfig.ReleaseChannel)); err != nil {
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

	if err := checkIfClusterExistsInKubeconfig(c.ClusterName); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	// Check if the k8s version is correct
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

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if c.GKEConfig.NumNodePools != nil {
		if len(nodePools.NodePools) != *c.GKEConfig.NumNodePools {
			return fmt.Errorf("validate update cluster: %w", ErrGKENodePoolsMismatch)
		}
	} else {
		if len(nodePools.NodePools) != len(cluster.GetAllNodePools()) {
			return fmt.Errorf("validate update cluster: %w", ErrGKENodePoolsMismatch)
		}
	}

	var nodePoolNames []*string

	for _, nodePool := range nodePools.NodePools {
		nodePoolNames = append(nodePoolNames, &nodePool.Name)

		if c.GKEConfig.Count != nil {
			if nodePool.InitialNodeCount != *c.GKEConfig.Count {
				return fmt.Errorf("validate update cluster: %w", ErrGKECountMismatch)
			}

			if err := cluster.SetCount(*c.GKEConfig.Count); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}

		} else {
			if nodePool.InitialNodeCount != cluster.GetCount() {
				return fmt.Errorf("validate update cluster: %w", ErrGKECountMismatch)
			}
		}

		if c.GKEConfig.MachineType != nil {
			if nodePool.Config.MachineType != *c.GKEConfig.MachineType {
				return fmt.Errorf("validate update cluster: %w", ErrGKEMachineTypeMismatch)
			}

			if err := cluster.SetMachineType(*c.GKEConfig.MachineType); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if nodePool.Config.MachineType != cluster.GetMachineType() {
				return fmt.Errorf("validate update cluster: %w", ErrGKEMachineTypeMismatch)
			}
		}

		if c.GKEConfig.ImageType != nil {
			if !strings.EqualFold(nodePool.Config.ImageType, *c.GKEConfig.ImageType) {
				return fmt.Errorf("validate update cluster: %w", ErrGKEImageTypeMismatch)
			}

			if err := cluster.SetImageType(*c.GKEConfig.ImageType); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if !strings.EqualFold(nodePool.Config.ImageType, cluster.GetImageType()) {
				return fmt.Errorf("validate update cluster: %w", ErrGKEImageTypeMismatch)
			}
		}

		if c.GKEConfig.DiskType != nil {
			if nodePool.Config.DiskType != *c.GKEConfig.DiskType {
				return fmt.Errorf("validate update cluster: %w", ErrGKEDiskTypeMismatch)
			}

			if err := cluster.SetDiskType(*c.GKEConfig.DiskType); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if nodePool.Config.DiskType != cluster.GetDiskType() {
				return fmt.Errorf("validate update cluster: %w", ErrGKEDiskTypeMismatch)
			}
		}

		if c.GKEConfig.DiskSize != nil {
			if nodePool.Config.DiskSizeGb != *c.GKEConfig.DiskSize {
				return fmt.Errorf("validate update cluster: %w", ErrGKEDiskSizeNodePoolMismatch)
			}

			if err := cluster.SetDiskSize(*c.GKEConfig.DiskSize); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if nodePool.Config.DiskSizeGb != cluster.GetDiskSize() {
				return fmt.Errorf("validate update cluster: %w", ErrGKEDiskSizeNodePoolMismatch)
			}
		}

		if c.GKEConfig.KubernetesVersion != nil {
			if strings.Split(nodePool.Version, "-")[0] != *c.GKEConfig.KubernetesVersion {
				return fmt.Errorf("validate update cluster: %w", ErrGKEK8SVersionNodePoolMismatch)
			}

			if err := cluster.SetKubernetesVersion(*c.GKEConfig.KubernetesVersion); err != nil {
				return fmt.Errorf("validate update cluster: %w", err)
			}
		} else {
			if strings.Split(nodePool.Version, "-")[0] != cluster.GetKubernetesVersion() {
				return fmt.Errorf("validate update cluster: %w", ErrGKEK8SVersionNodePoolMismatch)
			}
		}
	}

	if err := cluster.SetNodePools(nodePoolNames); err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("validate update cluster: %w", err)
	}

	if c.GKEConfig.NumNodePools != nil || c.GKEConfig.Count != nil {
		if len(allNodes) != len(cluster.GetAllNodePools())*int(cluster.GetCount()) {
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
		if len(allNodes) != len(cluster.GetAllNodePools())*int(cluster.GetCount()) {
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
