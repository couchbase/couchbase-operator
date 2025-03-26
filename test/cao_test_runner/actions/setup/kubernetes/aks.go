package setupkubernetes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type CreateAKSCluster struct {
	ClusterName            string
	Region                 string
	KubernetesVersion      string
	AKSSystemNodePools     []*AKSSystemNodePool
	AKSUserNodePools       []*AKSUserNodePool
	KubeConfigPath         *fileutils.File
	ManagedServiceProvider *managedk8sservices.ManagedServiceProvider
}

// AKSSystemNodePool represents the system node pool configuration for AKS.
// First system node pool will be used to create the cluster.
// https://learn.microsoft.com/en-us/azure/aks/use-system-pools.
type AKSSystemNodePool struct {
	Count    int32                     `yaml:"count"`
	VMSize   string                    `yaml:"vmSize"`
	DiskSize int32                     `yaml:"diskSize"`
	OSSKU    armcontainerservice.OSSKU `yaml:"osSKU"` // OSType is Linux
	// DedicatedSysNP determines to have a dedicated System Node Pool or not.
	// If true then CriticalAddonsOnly=true:NoSchedule taint will be applied to prevent application pods from being scheduled.
	// If false (default), then application pods can be scheduled on system node pool.
	DedicatedSysNP bool `yaml:"dedicatedSysNP"`
}
type AKSUserNodePool struct {
	Count    int32                      `yaml:"count"`
	VMSize   string                     `yaml:"vmSize"`
	DiskSize int32                      `yaml:"diskSize"`
	OSType   armcontainerservice.OSType `yaml:"osType"`
	OSSKU    armcontainerservice.OSSKU  `yaml:"osSKU"`
}

var (
	ErrCountInvalid                  = errors.New("for environment type 'cloud' and provider 'azure', Count must be greater than 0")
	ErrAKSDiskSizeInvalid            = errors.New("for environment type 'cloud' and provider 'azure', DiskSize must be greater than 0")
	ErrAKSDefaultNodePoolNotProvided = errors.New("default system node pool not provided")
	ErrAKSInvalidDedicatedSysNP      = errors.New("cannot have dedicated system node pool without user node pools")
	ErrAKSClusterAlreadyExists       = errors.New("aks cluster already exists")
	ErrAKSResourceGroupAlreadyExists = errors.New("aks resource group already exists")
	ErrCurrentFileFetchFail          = errors.New("current file fetch failed")
)

var (
	systemNodePoolOSType = armcontainerservice.OSTypeLinux         // System Node Pools only support OSType = Linux
	systemNodePoolMode   = armcontainerservice.AgentPoolModeSystem // System Node Pools have AgentPoolMode = System
	userNodePoolMode     = armcontainerservice.AgentPoolModeUser   // User Node Pools have AgentPoolMode = User
)

func (cac *CreateAKSCluster) CreateCluster(ctx context.Context) error {
	if err := cac.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{cac.ManagedServiceProvider}, cac.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(cac.ManagedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set aks session: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get aks session: %w", err)
	}

	resourceGroupName := cac.ClusterName + "-rg"
	if err := aksSession.CreateResourceGroup(ctx, resourceGroupName); err != nil {
		return fmt.Errorf("unable to create resource group %s: %w", resourceGroupName, err)
	}

	logrus.Infof("Resource group %s created", resourceGroupName)

	virtualNetworkName := cac.ClusterName + "-vnet"
	if err := aksSession.CreateVirtualNetwork(ctx, resourceGroupName, virtualNetworkName); err != nil {
		return fmt.Errorf("unable to create virtual network %s: %w", virtualNetworkName, err)
	}

	logrus.Infof("Virtual Network %s created", virtualNetworkName)

	subnetName := cac.ClusterName + "-subnet"
	if err := aksSession.CreateSubnet(ctx, resourceGroupName, virtualNetworkName, subnetName); err != nil {
		return fmt.Errorf("unable to create subnet %s: %w", subnetName, err)
	}

	logrus.Infof("Subnet %s created", subnetName)

	subnet, err := aksSession.GetSubnet(ctx, resourceGroupName, virtualNetworkName, subnetName)
	if err != nil {
		return fmt.Errorf("unable to fetch subnet %s: %w", subnetName, err)
	}

	if err := aksSession.CreateCluster(ctx, cac.KubernetesVersion, resourceGroupName, *subnet.ID, cac.AKSSystemNodePools[0].VMSize,
		cac.AKSSystemNodePools[0].Count, cac.AKSSystemNodePools[0].DiskSize, &cac.AKSSystemNodePools[0].OSSKU, true); err != nil {
		return fmt.Errorf("create aks cluster %s: %w", cac.ClusterName, err)
	}

	logrus.Infof("AKS Cluster %s created", cac.ClusterName)

	// Creating the remaining System Node Pools
	for i := 1; i < len(cac.AKSSystemNodePools); i++ {
		nodePoolName := fmt.Sprintf("systempool%d", i)

		if err := aksSession.CreateNodePool(ctx, resourceGroupName, nodePoolName, *subnet.ID,
			cac.AKSSystemNodePools[i].VMSize, cac.AKSSystemNodePools[i].Count, cac.AKSSystemNodePools[i].DiskSize,
			&systemNodePoolOSType, &cac.AKSSystemNodePools[i].OSSKU, &systemNodePoolMode, true); err != nil {
			return fmt.Errorf("create aks cluster %s: %w", cac.ClusterName, err)
		}

		logrus.Infof("Created system node pool %s", nodePoolName)
	}

	// Creating the User Node Pools
	for i := 0; i < len(cac.AKSUserNodePools); i++ {
		nodePoolName := fmt.Sprintf("nodepool%d", i) // Linux Node Pools have a max length of 12

		if cac.AKSUserNodePools[i].OSType == armcontainerservice.OSTypeWindows {
			nodePoolName = fmt.Sprintf("np%d", i) // Windows Node Pools have a max length of 6
		}

		if err := aksSession.CreateNodePool(ctx, resourceGroupName, nodePoolName, *subnet.ID,
			cac.AKSUserNodePools[i].VMSize, cac.AKSUserNodePools[i].Count, cac.AKSUserNodePools[i].DiskSize,
			&cac.AKSUserNodePools[i].OSType, &cac.AKSUserNodePools[i].OSSKU, &userNodePoolMode, true); err != nil {
			return fmt.Errorf("unable to create node pool %s: %w", nodePoolName, err)
		}

		logrus.Infof("Created user node pool %s", nodePoolName)
	}

	logrus.Infof("All node pools for cluster %s created", cac.ClusterName)

	creds, err := aksSession.ListClusterUserCredentials(ctx, resourceGroupName)
	if err != nil {
		return fmt.Errorf("unable to fetch cluster user credentials for cluster %s: %w", cac.ClusterName, err)
	}

	if err := cac.updateKubeconfig(creds); err != nil {
		return fmt.Errorf("failed to update kubeconfig: %w", err)
	}

	logrus.Info("Updated kubeconfig with the cluster details")

	_, filePath, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("unable to fetch current file path: %w", ErrCurrentFileFetchFail)
	}

	currentFile, _ := filepath.Abs(filePath)

	azureStorageClassPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "test_data", "cloud_storage_class", "cao-azurefile.yaml")

	if _, _, err := kubectl.ApplyFiles(azureStorageClassPath).Exec(false, false); err != nil {
		return fmt.Errorf("unable to apply azure storage class path %s: %w", azureStorageClassPath, err)
	}

	logrus.Info("Azure storage class yaml applied")

	// Applying taints for dedicated system node pool
	var nodeList *nodes.NodeList
	var getNodesOnce sync.Once
	var getNodesErr error

	for i, sysNodePool := range cac.AKSSystemNodePools {
		sysNodePoolName := fmt.Sprintf("systempool%d", i)

		if sysNodePool.DedicatedSysNP {
			getNodesOnce.Do(func() {
				nodeList, getNodesErr = nodes.GetNodes(nil)
			})
			if getNodesErr != nil {
				return fmt.Errorf("create aks cluster: %w", err)
			}

			for _, nodeInfo := range nodeList.Nodes {
				if _, ok := nodeInfo.Metadata.Labels["agentpool"]; ok && nodeInfo.Metadata.Labels["agentpool"] == sysNodePoolName {
					if _, _, err := kubectl.Taint([]string{nodeInfo.Metadata.Name}, "CriticalAddonsOnly", "true", "NoSchedule").Exec(false, true); err != nil {
						return fmt.Errorf("create aks cluster: %w", err)
					}
				}
			}

			logrus.Infof("Node Pool %s is now a Dedicated System Node Pool", sysNodePoolName)
		}
	}

	return nil
}

func (cac *CreateAKSCluster) ValidateParams(ctx context.Context) error {
	// Validating the CreateAKSCluster params
	if len(cac.AKSSystemNodePools) == 0 {
		return fmt.Errorf("validate create aks params: %w", ErrAKSDefaultNodePoolNotProvided)
	}

	if len(cac.AKSUserNodePools) == 0 && cac.AKSSystemNodePools[0].DedicatedSysNP {
		return fmt.Errorf("validate create aks params: %w", ErrAKSInvalidDedicatedSysNP)
	}

	// Validating the System Node Pools
	for _, nodePool := range cac.AKSSystemNodePools {
		if nodePool.Count <= 0 {
			return fmt.Errorf("validate create aks params: %w", ErrCountInvalid)
		}

		if nodePool.DiskSize <= 0 {
			return fmt.Errorf("validate create aks params: %w", ErrAKSDiskSizeInvalid)
		}

		if err := managedk8sservices.ValidateOSSKUType(&nodePool.OSSKU); err != nil {
			return fmt.Errorf("validate create aks params: %w", err)
		}
	}

	// Validating the User Node Pools
	for _, nodePool := range cac.AKSUserNodePools {
		if nodePool.Count <= 0 {
			return fmt.Errorf("validate create aks params: %w", ErrCountInvalid)
		}

		if nodePool.DiskSize <= 0 {
			return fmt.Errorf("validate create aks params: %w", ErrAKSDiskSizeInvalid)
		}

		if err := managedk8sservices.ValidateOSType(&nodePool.OSType); err != nil {
			return fmt.Errorf("validate create aks params: %w", err)
		}

		if nodePool.OSType == armcontainerservice.OSTypeWindows && nodePool.OSSKU != "" {
			return fmt.Errorf("invalid os sku type for os type windows: %w", managedk8sservices.ErrInvalidOSSKUType)
		}

		if err := managedk8sservices.ValidateOSSKUType(&nodePool.OSSKU); nodePool.OSType != armcontainerservice.OSTypeWindows && err != nil {
			return fmt.Errorf("validate create aks params: %w", err)
		}
	}

	// Validating the Azure resources
	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{cac.ManagedServiceProvider}, cac.ClusterName)
	if err != nil {
		return fmt.Errorf("validate create aks params: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(cac.ManagedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("validate create aks params: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("validate create aks params: %w", err)
	}

	resourceGroupName := cac.ClusterName + "-rg"
	if _, err := aksSession.GetResourceGroup(ctx, resourceGroupName); err == nil {
		return fmt.Errorf("validate create aks params: resource group %s: %w", resourceGroupName, ErrAKSResourceGroupAlreadyExists)
	}

	if _, err := aksSession.GetCluster(ctx, resourceGroupName); err == nil {
		return fmt.Errorf("validate create aks params: cluster %s: %w", cac.ClusterName, ErrAKSClusterAlreadyExists)
	}

	if !cac.KubeConfigPath.IsFileExists() {
		return fmt.Errorf("validate create aks params: kube config path %s : %w", cac.KubeConfigPath.FilePath, ErrKubeConfigFileInvalid)
	}

	return nil
}

func (cac *CreateAKSCluster) updateKubeconfig(creds *armcontainerservice.ManagedClustersClientListClusterUserCredentialsResponse) error {
	kubeconfig := creds.Kubeconfigs[0].Value

	var decodedKubeconfig []byte

	kubeconfigStr := string(kubeconfig)
	if _, err := base64.StdEncoding.DecodeString(kubeconfigStr); err == nil {
		decodedKubeconfig, err = base64.StdEncoding.DecodeString(kubeconfigStr)
		if err != nil {
			return fmt.Errorf("failed to decode kubeconfig: %w", err)
		}
	} else {
		decodedKubeconfig = kubeconfig
	}

	defer cac.KubeConfigPath.CloseFile()

	existingKubeconfig, err := cac.KubeConfigPath.ReadFile()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load existing kubeconfig: %w", err)
	}

	var existingConfig map[string]interface{}

	var newConfig map[string]interface{}

	if len(existingKubeconfig) > 0 {
		if err := yaml.Unmarshal(existingKubeconfig, &existingConfig); err != nil {
			return fmt.Errorf("failed to unmarshal existing kubeconfig: %w", err)
		}
	}

	if err := yaml.Unmarshal(decodedKubeconfig, &newConfig); err != nil {
		return fmt.Errorf("failed to unmarshal new kubeconfig: %w", err)
	}

	if existingConfig["clusters"] == nil {
		existingConfig["clusters"] = newConfig["clusters"]
	} else {
		existingConfig["clusters"] = append(existingConfig["clusters"].([]interface{}), newConfig["clusters"].([]interface{})...)
	}

	if existingConfig["users"] == nil {
		existingConfig["users"] = newConfig["users"]
	} else {
		existingConfig["users"] = append(existingConfig["users"].([]interface{}), newConfig["users"].([]interface{})...)
	}

	if existingConfig["contexts"] == nil {
		existingConfig["contexts"] = newConfig["contexts"]
	} else {
		existingConfig["contexts"] = append(existingConfig["contexts"].([]interface{}), newConfig["contexts"].([]interface{})...)
	}

	existingConfig["current-context"] = newConfig["current-context"]

	updatedKubeconfig, err := yaml.Marshal(existingConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal updated kubeconfig: %w", err)
	}

	if err := cac.KubeConfigPath.WriteFile(updatedKubeconfig, 0600); err != nil {
		return fmt.Errorf("failed to write updated kubeconfig file: %w", err)
	}

	if _, _, err := kubectl.UseContext(cac.ClusterName).Exec(false, false); err != nil {
		return fmt.Errorf("failed to set kubectl context: %w", err)
	}

	return nil
}
