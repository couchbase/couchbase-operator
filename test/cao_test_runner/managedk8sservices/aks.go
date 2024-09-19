package managedk8sservices

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

var (
	ErrInvalidOSSKUType = errors.New("invalid os sku type")
	ErrInvalidOSType    = errors.New("invalid os type")
)

type AKSSessionStore struct {
	AKSSessions map[string]*AKSSession
	lock        sync.Mutex
}

type AKSSession struct {
	ResourceGroupClient   *armresources.ResourceGroupsClient
	VirtualNetworksClient *armnetwork.VirtualNetworksClient
	SubnetsClient         *armnetwork.SubnetsClient
	AKSClient             *armcontainerservice.ManagedClustersClient
	NodePoolClient        *armcontainerservice.AgentPoolsClient
	VMClient              *armcompute.VirtualMachinesClient
	Cred                  *ManagedServiceCredentials
	Region                string
	ClusterName           string
}

func NewAKSSession(managedSvcCred *ManagedServiceCredentials) (*AKSSession, error) {
	cred, err := azidentity.NewClientSecretCredential(managedSvcCred.AKSCredentials.aksTenantID,
		managedSvcCred.AKSCredentials.aksServicePrincipalID, managedSvcCred.AKSCredentials.aksServicePrincipalSecret, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create credentials: %w", err)
	}

	resourceGroupClient, err := armresources.NewResourceGroupsClient(managedSvcCred.AKSCredentials.aksSubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource group client: %w", err)
	}

	vnetClient, err := armnetwork.NewVirtualNetworksClient(managedSvcCred.AKSCredentials.aksSubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create virtual network client: %w", err)
	}

	subnetClient, err := armnetwork.NewSubnetsClient(managedSvcCred.AKSCredentials.aksSubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet client: %w", err)
	}

	aksClient, err := armcontainerservice.NewManagedClustersClient(managedSvcCred.AKSCredentials.aksSubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create AKS client: %w", err)
	}

	nodePoolClient, err := armcontainerservice.NewAgentPoolsClient(managedSvcCred.AKSCredentials.aksSubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create node pool client: %w", err)
	}

	vmClient, err := armcompute.NewVirtualMachinesClient(managedSvcCred.AKSCredentials.aksSubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("new aks session: get vm client: %w", err)
	}

	return &AKSSession{
		ResourceGroupClient:   resourceGroupClient,
		VirtualNetworksClient: vnetClient,
		SubnetsClient:         subnetClient,
		AKSClient:             aksClient,
		NodePoolClient:        nodePoolClient,
		VMClient:              vmClient,
		Cred:                  managedSvcCred,
		Region:                managedSvcCred.AKSCredentials.aksRegion,
		ClusterName:           managedSvcCred.ClusterName,
	}, nil
}

func ConfigAKSSessionStore() ManagedService {
	return &AKSSessionStore{
		AKSSessions: make(map[string]*AKSSession),
		lock:        sync.Mutex{},
	}
}

func NewAKSSessionStore() *AKSSessionStore {
	return &AKSSessionStore{
		AKSSessions: make(map[string]*AKSSession),
		lock:        sync.Mutex{},
	}
}

func ValidateOSSKUType(osSKU *armcontainerservice.OSSKU) error {
	switch *osSKU {
	case armcontainerservice.OSSKUCBLMariner, armcontainerservice.OSSKUUbuntu, "AzureLinux":
		return nil
	default:
		return fmt.Errorf("invalid os sku type: %w", ErrInvalidOSSKUType)
	}
}

func ValidateOSType(osType *armcontainerservice.OSType) error {
	switch *osType {
	case armcontainerservice.OSTypeLinux, armcontainerservice.OSTypeWindows:
		return nil
	default:
		return fmt.Errorf("invalid os type: %w", ErrInvalidOSType)
	}
}

func getAKSKey(managedSvcCred *ManagedServiceCredentials) string {
	key := managedSvcCred.ClusterName + ":" + managedSvcCred.EKSCredentials.eksRegion
	return key
}

func (ass *AKSSessionStore) SetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error {
	defer ass.lock.Unlock()
	ass.lock.Lock()

	if _, ok := ass.AKSSessions[getAKSKey(managedSvcCred)]; !ok {
		aksSess, err := NewAKSSession(managedSvcCred)
		if err != nil {
			return fmt.Errorf("set eks session store: %w", err)
		}

		ass.AKSSessions[getAKSKey(managedSvcCred)] = aksSess
	}

	return nil
}

func (ass *AKSSessionStore) GetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) (*AKSSession, error) {
	if _, ok := ass.AKSSessions[getAKSKey(managedSvcCred)]; !ok {
		err := ass.SetSession(ctx, managedSvcCred)
		if err != nil {
			return nil, fmt.Errorf("get aks session: %w", err)
		}
	}

	return ass.AKSSessions[getAKSKey(managedSvcCred)], nil
}

func (ass *AKSSessionStore) Check(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error {
	// TODO implement me
	panic("implement me")
}

func (ass *AKSSessionStore) GetInstancesByK8sNodeName(ctx context.Context, managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error) {
	// TODO implement me
	panic("implement me")
}

// ================================================
// ====== Methods implemented by AKSSession ======
// ================================================

func (aksSession *AKSSession) ListClusterUserCredentials(ctx context.Context, resourceGroupName string) (*armcontainerservice.ManagedClustersClientListClusterUserCredentialsResponse, error) {
	resp, err := aksSession.AKSClient.ListClusterUserCredentials(ctx, resourceGroupName, aksSession.ClusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster credentials: %w", err)
	}

	return &resp, nil
}

func (aksSession *AKSSession) GetResourceGroup(ctx context.Context, resourceGroupName string) (*armresources.ResourceGroupsClientGetResponse, error) {
	resp, err := aksSession.ResourceGroupClient.Get(ctx, resourceGroupName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource group: %w", err)
	}

	return &resp, nil
}

func (aksSession *AKSSession) GetCluster(ctx context.Context, resourceGroupName string) (*armcontainerservice.ManagedClustersClientGetResponse, error) {
	cluster, err := aksSession.AKSClient.Get(ctx, resourceGroupName, aksSession.ClusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes cluster: %w", err)
	}

	return &cluster, nil
}

func (aksSession *AKSSession) GetSubnet(ctx context.Context, resourceGroupName, virtualNetworkName, subnetName string) (*armnetwork.SubnetsClientGetResponse, error) {
	subnet, err := aksSession.SubnetsClient.Get(ctx, resourceGroupName, virtualNetworkName, subnetName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet: %w", err)
	}

	return &subnet, nil
}

func (aksSession *AKSSession) CreateResourceGroup(ctx context.Context, resourceGroupName string) error {
	parameters := armresources.ResourceGroup{
		Location: &aksSession.Region,
	}

	if _, err := aksSession.ResourceGroupClient.CreateOrUpdate(ctx, resourceGroupName, parameters, nil); err != nil {
		return fmt.Errorf("failed to create resource group: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) CreateVirtualNetwork(ctx context.Context, resourceGroupName, virtualNetworkName string) error {
	// TODO : Take address prefixes as parameter
	vnetParams := armnetwork.VirtualNetwork{
		Location: &aksSession.Region,
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: []*string{
					to.Ptr("10.0.0.0/12"),
				},
			},
		},
	}

	if _, err := aksSession.VirtualNetworksClient.BeginCreateOrUpdate(ctx, resourceGroupName, virtualNetworkName, vnetParams, nil); err != nil {
		return fmt.Errorf("failed to create virtual network: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) CreateSubnet(ctx context.Context, resourceGroupName, virtualNetworkName, subnetName string) error {
	// TODO : Take address prefix as parameter
	subnetParams := armnetwork.Subnet{
		Properties: &armnetwork.SubnetPropertiesFormat{
			AddressPrefix: to.Ptr("10.8.0.0/16"),
		},
	}

	if _, err := aksSession.SubnetsClient.BeginCreateOrUpdate(ctx, resourceGroupName, virtualNetworkName, subnetName, subnetParams, nil); err != nil {
		return fmt.Errorf("failed to create subnet: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) CreateCluster(ctx context.Context, kubernetesVersion, resourceGroupName, subnetID, vmSize string,
	osType *armcontainerservice.OSType, osSKU *armcontainerservice.OSSKU, waitForClusterCreation bool) error {
	// TODO : Take all IPs as params
	clusterParams := armcontainerservice.ManagedCluster{
		Location: &aksSession.Region,
		Properties: &armcontainerservice.ManagedClusterProperties{
			DNSPrefix:         &aksSession.ClusterName,
			KubernetesVersion: &kubernetesVersion,
			NetworkProfile: &armcontainerservice.NetworkProfile{
				NetworkPlugin:    to.Ptr(armcontainerservice.NetworkPluginAzure),
				DNSServiceIP:     to.Ptr("10.0.0.10"),
				DockerBridgeCidr: to.Ptr("172.17.0.1/16"),
				ServiceCidr:      to.Ptr("10.0.0.0/16"),
			},
			AddonProfiles: map[string]*armcontainerservice.ManagedClusterAddonProfile{
				"httpApplicationRouting": {
					Enabled: to.Ptr(true),
				},
			},
			AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{
				{
					Name:         to.Ptr("systempool1"),
					Count:        to.Ptr(int32(5)),
					VMSize:       &vmSize,
					OSDiskSizeGB: to.Ptr(int32(100)),
					OSSKU:        osSKU,
					Mode:         to.Ptr(armcontainerservice.AgentPoolModeSystem),
					OSType:       osType,
					Type:         to.Ptr(armcontainerservice.AgentPoolTypeVirtualMachineScaleSets),
					AvailabilityZones: []*string{
						to.Ptr("1"),
						to.Ptr("2"),
						to.Ptr("3"),
					},
					VnetSubnetID: &subnetID,
				},
			},
			ServicePrincipalProfile: &armcontainerservice.ManagedClusterServicePrincipalProfile{
				ClientID: &aksSession.Cred.AKSCredentials.aksServicePrincipalID,
				Secret:   &aksSession.Cred.AKSCredentials.aksServicePrincipalSecret,
			},
		},
	}

	poller, err := aksSession.AKSClient.BeginCreateOrUpdate(ctx, resourceGroupName, aksSession.ClusterName, clusterParams, nil)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes cluster: %w", err)
	}

	if !waitForClusterCreation {
		return nil
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("failed to wait for Kubernetes cluster creation to complete: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) CreateNodePool(ctx context.Context, resourceGroupName, nodePoolName, subnetID, vmSize string,
	count, diskSize int32, osType *armcontainerservice.OSType, osSKU *armcontainerservice.OSSKU, waitForNodeGroupCreation bool) error {
	if err := ValidateOSType(osType); err != nil {
		return fmt.Errorf("invalid os type: %w", err)
	}

	if *osType == armcontainerservice.OSTypeWindows && *osSKU != "" {
		return fmt.Errorf("invalid os sku type for os type windows: %w", ErrInvalidOSSKUType)
	}

	if err := ValidateOSSKUType(osSKU); *osType != armcontainerservice.OSTypeWindows && err != nil {
		return fmt.Errorf("invalid os sku type: %w", err)
	}

	nodePoolParams := armcontainerservice.AgentPool{
		Properties: &armcontainerservice.ManagedClusterAgentPoolProfileProperties{
			Count:        &count,
			VMSize:       &vmSize,
			OSDiskSizeGB: &diskSize,
			OSSKU:        osSKU,
			OSType:       osType,
			Mode:         to.Ptr(armcontainerservice.AgentPoolModeUser),
			Type:         to.Ptr(armcontainerservice.AgentPoolTypeVirtualMachineScaleSets),
			AvailabilityZones: []*string{
				to.Ptr("1"),
				to.Ptr("2"),
				to.Ptr("3"),
			},
			VnetSubnetID: &subnetID,
		},
	}

	poller, err := aksSession.NodePoolClient.BeginCreateOrUpdate(ctx, resourceGroupName, aksSession.ClusterName, nodePoolName, nodePoolParams, nil)
	if err != nil {
		return fmt.Errorf("failed to create node pool: %w", err)
	}

	if !waitForNodeGroupCreation {
		return nil
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("failed to wait for Kubernetes node pool creation to complete: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) DeleteNodePool(ctx context.Context, resourceGroupName, nodePoolName string, waitForNodePoolDeletion bool) error {
	poller, err := aksSession.NodePoolClient.BeginDelete(ctx, resourceGroupName, aksSession.ClusterName, nodePoolName, nil)
	if err != nil {
		return fmt.Errorf("error deleting node pool %s: %w", nodePoolName, err)
	}

	if !waitForNodePoolDeletion {
		return nil
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("failed to wait for kubernetes node pool deletion to complete: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) DeleteCluster(ctx context.Context, resourceGroupName string, waitForClusterDeletion bool) error {
	poller, err := aksSession.AKSClient.BeginDelete(ctx, resourceGroupName, aksSession.ClusterName, nil)
	if err != nil {
		return fmt.Errorf("error deleting cluster %s: %w", aksSession.ClusterName, err)
	}

	if !waitForClusterDeletion {
		return nil
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("failed to wait for aks cluster deletion to complete: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) ListNodePools(ctx context.Context, resourceGroupName string) ([]*armcontainerservice.AgentPool, error) {
	nodePoolsPager := aksSession.NodePoolClient.NewListPager(resourceGroupName, aksSession.ClusterName, nil)

	var nodePools []*armcontainerservice.AgentPool

	for nodePoolsPager.More() {
		resp, err := nodePoolsPager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to list node pools: %w", err)
		}

		nodePools = append(nodePools, resp.Value...)
	}

	return nodePools, nil
}

func (aksSession *AKSSession) DeleteSubnet(ctx context.Context, resourceGroupName, virtualNetworkName, subnetName string, waitForDeletion bool) error {
	poller, err := aksSession.SubnetsClient.BeginDelete(ctx, resourceGroupName, virtualNetworkName, subnetName, nil)
	if err != nil {
		return fmt.Errorf("error deleting subnet: %w", err)
	}

	if !waitForDeletion {
		return nil
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("failed to wait for subnet deletion to complete: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) DeleteVirtualNetwork(ctx context.Context, resourceGroupName, virtualNetworkName string, waitForDeletion bool) error {
	poller, err := aksSession.VirtualNetworksClient.BeginDelete(ctx, resourceGroupName, virtualNetworkName, nil)
	if err != nil {
		return fmt.Errorf("error deleting virtual network: %w", err)
	}

	if !waitForDeletion {
		return nil
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("failed to wait for virtual network deletion to complete: %w", err)
	}

	return nil
}

func (aksSession *AKSSession) DeleteResourceGroup(ctx context.Context, resourceGroupName string, waitForDeletion bool) error {
	poller, err := aksSession.ResourceGroupClient.BeginDelete(ctx, resourceGroupName, nil)
	if err != nil {
		return fmt.Errorf("error deleting resource group: %w", err)
	}

	if !waitForDeletion {
		return nil
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("failed to wait for resource group deletion to complete: %w", err)
	}

	return nil
}
