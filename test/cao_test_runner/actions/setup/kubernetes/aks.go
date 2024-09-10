package setupkubernetes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type CreateAKSCluster struct {
	ClusterName       string
	Region            string
	KubernetesVersion string
	VMSize            string
	Count             int32
	DiskSize          int32
	OSType            armcontainerservice.OSType
	OSSKU             armcontainerservice.OSSKU
	NumNodePools      int
	KubeConfigPath    string
}

var (
	ErrCountInvalid                  = errors.New("for environment type 'cloud' and provider 'azure', Count must be greater than 0")
	ErrAKSDiskSizeInvalid            = errors.New("for environment type 'cloud' and provider 'azure', DiskSize must be greater than 0")
	ErrNumNodePoolsInvalid           = errors.New("for environment type 'cloud' and provider 'azure', NumNodePools must be greater than 0")
	ErrAKSClusterAlreadyExists       = errors.New("aks cluster already exists")
	ErrAKSResourceGroupAlreadyExists = errors.New("aks resource group already exists")
	ErrCurrentFileFetchFail          = errors.New("current file fetch failed")
)

func (cac *CreateAKSCluster) CreateCluster(ctx *context.Context) error {
	if err := cac.ValidateParams(ctx); err != nil {
		return err
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.AKSManagedService}, cac.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedk8sservices.AKSManagedService)
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

	logrus.Info(fmt.Sprintf("Resource group %s created", resourceGroupName))

	virtualNetworkName := cac.ClusterName + "-vnet"
	if err := aksSession.CreateVirtualNetwork(ctx, resourceGroupName, virtualNetworkName); err != nil {
		return fmt.Errorf("unable to create virtual network %s: %w", virtualNetworkName, err)
	}

	logrus.Info(fmt.Sprintf("Virtual Network %s created", virtualNetworkName))

	subnetName := cac.ClusterName + "-subnet"
	if err := aksSession.CreateSubnet(ctx, resourceGroupName, virtualNetworkName, subnetName); err != nil {
		return fmt.Errorf("unable to create subnet %s: %w", subnetName, err)
	}

	logrus.Info(fmt.Sprintf("Subnet %s created", subnetName))

	subnet, err := aksSession.GetSubnet(ctx, resourceGroupName, virtualNetworkName, subnetName)
	if err != nil {
		return fmt.Errorf("unable to fetch subnet %s: %w", subnetName, err)
	}

	if err := aksSession.CreateCluster(ctx, cac.KubernetesVersion, resourceGroupName, *subnet.ID,
		cac.VMSize, &cac.OSType, &cac.OSSKU, true); err != nil {
		return fmt.Errorf("unable to create k8s cluster %s: %w", cac.ClusterName, err)
	}

	logrus.Info(fmt.Sprintf("AKS Cluster %s created", cac.ClusterName))

	// One node group is already created during cluster creation
	// Create remaining node groups here
	for i := 0; i < cac.NumNodePools; i++ {
		nodePoolName := fmt.Sprintf("nodepool%d", i)

		if err := aksSession.CreateNodePool(ctx, resourceGroupName, nodePoolName, *subnet.ID,
			cac.VMSize, cac.Count, cac.DiskSize, &cac.OSType, &cac.OSSKU, true); err != nil {
			return fmt.Errorf("unable to create node pool %s: %w", nodePoolName, err)
		}
	}

	logrus.Info(fmt.Sprintf("All node pools for cluster %s created", cac.ClusterName))

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

	azureStorageClassPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "test_data", "azure_storage_class", "cao-azurefile.yaml")

	if err := kubectl.ApplyFiles(azureStorageClassPath).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to apply azure storage class path %s: %w", azureStorageClassPath, err)
	}

	logrus.Info("Azure storage class yaml applied")

	return nil
}

func (cac *CreateAKSCluster) ValidateParams(ctx *context.Context) error {
	if cac.Count <= 0 {
		return ErrCountInvalid
	}

	if cac.DiskSize <= 0 {
		return ErrAKSDiskSizeInvalid
	}

	if cac.NumNodePools <= 0 {
		return ErrNumNodePoolsInvalid
	}

	if err := managedk8sservices.ValidateOSType(&cac.OSType); err != nil {
		return fmt.Errorf("invalid os type: %w", err)
	}

	if cac.OSType == armcontainerservice.OSTypeWindows && cac.OSSKU != "" {
		return fmt.Errorf("invalid os sku type for os type windows: %w", managedk8sservices.ErrInvalidOSSKUType)
	}

	if err := managedk8sservices.ValidateOSSKUType(&cac.OSSKU); cac.OSType != armcontainerservice.OSTypeWindows && err != nil {
		return fmt.Errorf("invalid os sku type: %w", err)
	}

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]managedk8sservices.ManagedServiceProvider{managedk8sservices.AKSManagedService}, cac.ClusterName)
	if err != nil {
		return fmt.Errorf("unable to create service credentials: %w", err)
	}

	aksSessionStore := managedk8sservices.NewManagedService(managedk8sservices.AKSManagedService)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("unable to set aks session: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("unable to get aks session: %w", err)
	}

	resourceGroupName := cac.ClusterName + "-rg"
	if _, err := aksSession.GetResourceGroup(ctx, resourceGroupName); err == nil {
		return fmt.Errorf("resource group %s already exists: %w", cac.ClusterName, ErrAKSResourceGroupAlreadyExists)
	}

	if _, err := aksSession.GetCluster(ctx, resourceGroupName); err == nil {
		return fmt.Errorf("cluster %s already exists: %w", cac.ClusterName, ErrAKSClusterAlreadyExists)
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

	kubeConfigFile := fileutils.NewFile(cac.KubeConfigPath)

	defer kubeConfigFile.CloseFile()

	existingKubeconfig, err := kubeConfigFile.ReadFile()
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

	if err := kubeConfigFile.WriteFile(updatedKubeconfig, 0600); err != nil {
		return fmt.Errorf("failed to write updated kubeconfig file: %w", err)
	}

	if err := kubectl.UseContext(cac.ClusterName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("failed to set kubectl context: %w", err)
	}

	return nil
}
