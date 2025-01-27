package managedk8sservices

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	container "cloud.google.com/go/container/apiv1"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	"github.com/sirupsen/logrus"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var (
	ErrOperationError = errors.New("operation error")
)

type GKESessionStore struct {
	GKESessions map[string]*GKESession
	lock        sync.Mutex
}

type GKESession struct {
	globalOperationsClient *compute.GlobalOperationsClient
	networksClient         *compute.NetworksClient
	firewallsClient        *compute.FirewallsClient
	subnetClient           *compute.SubnetworksClient
	clusterManagerClient   *container.ClusterManagerClient
	cred                   *ManagedServiceCredentials
	region                 string
	clusterName            string
	projectID              string
}

func NewGKESession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) (*GKESession, error) {
	credsFileOptions := option.WithCredentialsFile(managedSvcCred.GKECredentials.gkeCredentialsJSONPath)

	networksClient, err := compute.NewNetworksRESTClient(ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create networks client: %w", err)
	}

	globalOperationsClient, err := compute.NewGlobalOperationsRESTClient(ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create operations client: %w", err)
	}

	firewallClient, err := compute.NewFirewallsRESTClient(ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create firewall client: %w", err)
	}

	subnetClient, err := compute.NewSubnetworksRESTClient(ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet client: %w", err)
	}

	clusterManagerClient, err := container.NewClusterManagerRESTClient(ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create container client: %w", err)
	}

	return &GKESession{
		globalOperationsClient: globalOperationsClient,
		networksClient:         networksClient,
		firewallsClient:        firewallClient,
		subnetClient:           subnetClient,
		clusterManagerClient:   clusterManagerClient,
		cred:                   managedSvcCred,
		region:                 managedSvcCred.GKECredentials.gkeRegion,
		clusterName:            managedSvcCred.ClusterName,
		projectID:              managedSvcCred.GKECredentials.gkeProjectID,
	}, nil
}

func ConfigGKESessionStore() ManagedService {
	return &GKESessionStore{
		GKESessions: make(map[string]*GKESession),
		lock:        sync.Mutex{},
	}
}

func NewGKESessionStore() *GKESessionStore {
	return &GKESessionStore{
		GKESessions: make(map[string]*GKESession),
		lock:        sync.Mutex{},
	}
}

func getGKEKey(managedSvcCred *ManagedServiceCredentials) string {
	key := managedSvcCred.ClusterName + ":" + managedSvcCred.GKECredentials.gkeRegion
	return key
}

func (gss *GKESessionStore) SetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error {
	defer gss.lock.Unlock()
	gss.lock.Lock()

	if _, ok := gss.GKESessions[getGKEKey(managedSvcCred)]; !ok {
		gkeSess, err := NewGKESession(ctx, managedSvcCred)
		if err != nil {
			return fmt.Errorf("set gke session store: %w", err)
		}

		gss.GKESessions[getGKEKey(managedSvcCred)] = gkeSess
	}

	return nil
}

func (gss *GKESessionStore) GetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) (*GKESession, error) {
	if _, ok := gss.GKESessions[getGKEKey(managedSvcCred)]; !ok {
		err := gss.SetSession(ctx, managedSvcCred)
		if err != nil {
			return nil, fmt.Errorf("get gke session: %w", err)
		}
	}

	return gss.GKESessions[getGKEKey(managedSvcCred)], nil
}

func (gss *GKESessionStore) Check(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error {
	// TODO implement me
	panic("implement me")
}

func (gss *GKESessionStore) GetInstancesByK8sNodeName(ctx context.Context, managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error) {
	// TODO implement me
	panic("implement me")
}

// ================================================
// ====== Methods implemented by GKESession ======
// ================================================

func (gks *GKESession) WaitForOperation(ctx context.Context, operationName string) error {
	for {
		op, err := gks.globalOperationsClient.Get(ctx, &computepb.GetGlobalOperationRequest{
			Project:   gks.projectID,
			Operation: operationName,
		})
		if err != nil {
			return fmt.Errorf("failed to get operation: %w", err)
		}

		if op.Status.String() == "DONE" {
			if op.GetError() != nil {
				return fmt.Errorf("operation error %v : %w", op.GetError(), ErrOperationError)
			}

			break
		}

		time.Sleep(5 * time.Second)
	}

	return nil
}

func (gks *GKESession) WaitForClusterOperation(ctx context.Context, operationName string) error {
	for {
		opStatus, err := gks.clusterManagerClient.GetOperation(ctx, &containerpb.GetOperationRequest{
			Name: fmt.Sprintf("projects/%s/locations/%s/operations/%s", gks.projectID, gks.region, operationName),
		})
		if err != nil {
			return fmt.Errorf("failed to get operation status: %w", err)
		}

		if opStatus.Status == containerpb.Operation_DONE {
			if opStatus.Error != nil {
				return fmt.Errorf("operation failed %v: %w", opStatus.Error, ErrOperationError)
			}

			return nil
		}

		time.Sleep(30 * time.Second)
	}
}

func (gks *GKESession) CreateVirtualNetwork(ctx context.Context, networkName string, autoCreateSubnet bool) error {
	network := &computepb.Network{
		Name:                  &networkName,
		AutoCreateSubnetworks: &autoCreateSubnet,
	}

	req := &computepb.InsertNetworkRequest{
		Project:         gks.projectID,
		NetworkResource: network,
	}

	op, err := gks.networksClient.Insert(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	err = gks.WaitForOperation(ctx, op.Name())
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}

	return nil
}

func (gks *GKESession) CreateFirewallRule(ctx context.Context, firewallRuleName, networkName string) error {
	network := fmt.Sprintf("projects/%s/global/networks/%s", gks.projectID, networkName)
	ipProtocol := "all"
	direction := "INGRESS"

	// TODO : Take source ranges, direction, ip protocols as params
	firewallRule := &computepb.Firewall{
		Name:    &firewallRuleName,
		Network: &network,
		Allowed: []*computepb.Allowed{
			{
				IPProtocol: &ipProtocol,
			},
		},
		SourceRanges: []string{"10.0.0.0/8"},
		Direction:    &direction,
	}

	firewallReq := &computepb.InsertFirewallRequest{
		Project:          gks.projectID,
		FirewallResource: firewallRule,
	}

	firewallOp, err := gks.firewallsClient.Insert(ctx, firewallReq)
	if err != nil {
		return fmt.Errorf("failed to create firewall rule: %w", err)
	}

	if err = gks.WaitForOperation(ctx, firewallOp.Name()); err != nil {
		return fmt.Errorf("firewall rule creation operation failed: %w", err)
	}

	return nil
}

func (gks *GKESession) CreateSubnet(ctx context.Context, subnetName, networkName, ipCidrRange string,
	waitForSubnetCreation bool) error {
	network := fmt.Sprintf("projects/%s/global/networks/%s", gks.projectID, networkName)
	region := fmt.Sprintf("projects/%s/regions/%s", gks.projectID, gks.region)

	subnetwork := &computepb.Subnetwork{
		Name:        &subnetName,
		Network:     &network,
		IpCidrRange: &ipCidrRange,
		Region:      &region,
	}

	subnetReq := &computepb.InsertSubnetworkRequest{
		Project:            gks.projectID,
		Region:             gks.region,
		SubnetworkResource: subnetwork,
	}

	_, err := gks.subnetClient.Insert(ctx, subnetReq)
	if err != nil {
		return fmt.Errorf("failed to create subnet %s: %w", subnetName, err)
	}

	// if err = gks.WaitForOperation(ctx, subnetOp.Name()); err != nil {
	// 	return fmt.Errorf("subnet %s creation operation failed: %w", subnetName, err)
	// }

	if !waitForSubnetCreation {
		return nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(60)*time.Minute)
	defer cancel()

	interval := 60

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout reached while waiting for subnet %s to be deleted", subnetName)
		default:
		}

		exists, _ := gks.IsSubnetExists(timeoutCtx, subnetName)
		if exists {
			logrus.Infof("Subnet %s has been created.\n", subnetName)
			return nil
		} else {
			logrus.Infof("Subnet %s does not exist. Attempting to check again in %v...\n", subnetName, interval)
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}

}

func (gks *GKESession) CreateCluster(ctx context.Context, networkName, subnetworkName, machineType, imageType, diskType, initialClusterVersion, nodePoolName string,
	diskSize, numNodes int, releaseChannel ReleaseChannel) error {
	if ok, err := ValidateReleaseChannel(releaseChannel); !ok || err != nil {
		return fmt.Errorf("invalid release channel: %w", err)
	}

	cluster := &containerpb.Cluster{
		Name:     gks.clusterName,
		Location: gks.region,
		ReleaseChannel: &containerpb.ReleaseChannel{
			Channel: containerpb.ReleaseChannel_Channel(ReleaseChannelMap[releaseChannel]),
		},
		InitialClusterVersion: initialClusterVersion,
		NodePools: []*containerpb.NodePool{
			{
				Name:             nodePoolName,
				InitialNodeCount: int32(numNodes),
				Config: &containerpb.NodeConfig{
					MachineType: machineType,
					DiskSizeGb:  int32(diskSize),
					DiskType:    diskType,
					ImageType:   imageType,
				},
				Autoscaling: &containerpb.NodePoolAutoscaling{
					Enabled: true,
				},
			},
		},
		Network:    networkName,
		Subnetwork: subnetworkName,
		IpAllocationPolicy: &containerpb.IPAllocationPolicy{
			UseIpAliases: false,
		},
		LegacyAbac: &containerpb.LegacyAbac{
			Enabled: false,
		},
		EnableKubernetesAlpha: false,
		LoggingService:        "none",
		MonitoringService:     "none",
		MasterAuth: &containerpb.MasterAuth{
			ClientCertificateConfig: &containerpb.ClientCertificateConfig{
				IssueClientCertificate: true,
			},
		},
	}

	req := &containerpb.CreateClusterRequest{
		Parent:  fmt.Sprintf("projects/%s/locations/%s", gks.projectID, gks.region),
		Cluster: cluster,
	}

	operation, err := gks.clusterManagerClient.CreateCluster(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	if err = gks.WaitForClusterOperation(ctx, operation.Name); err != nil {
		return fmt.Errorf("cluster %s creation operation failed: %w", gks.clusterName, err)
	}

	return nil
}

func (gks *GKESession) CreateNodePool(ctx context.Context, nodePoolName, machineType, diskType, imageType string, diskSize, numNodes int) error {
	nodePool := &containerpb.NodePool{
		Name:             nodePoolName,
		InitialNodeCount: int32(numNodes),
		Config: &containerpb.NodeConfig{
			MachineType: machineType,
			DiskSizeGb:  int32(diskSize),
			DiskType:    diskType,
			ImageType:   imageType,
		},
		Autoscaling: &containerpb.NodePoolAutoscaling{
			Enabled: true,
		},
	}

	req := &containerpb.CreateNodePoolRequest{
		Parent:   fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.projectID, gks.region, gks.clusterName),
		NodePool: nodePool,
	}

	op, err := gks.clusterManagerClient.CreateNodePool(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create node pool: %w", err)
	}

	if err := gks.WaitForOperation(ctx, op.Name); err != nil {
		return fmt.Errorf("node pool %s creation operation failed: %w", nodePoolName, err)
	}

	return nil
}

func (gks *GKESession) GetCluster(ctx context.Context) (*containerpb.Cluster, error) {
	req := &containerpb.GetClusterRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.projectID, gks.region, gks.clusterName),
	}

	cluster, err := gks.clusterManagerClient.GetCluster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("error fetching cluster details: %w", err)
	}

	return cluster, nil
}

func (gks *GKESession) ListNodePools(ctx context.Context) (*containerpb.ListNodePoolsResponse, error) {
	req := &containerpb.ListNodePoolsRequest{
		Parent: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.projectID, gks.region, gks.clusterName),
	}

	resp, err := gks.clusterManagerClient.ListNodePools(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("error listing node pools: %w", err)
	}

	return resp, err
}

func (gks *GKESession) DeleteNodePool(ctx context.Context, nodePoolName string) error {
	req := &containerpb.DeleteNodePoolRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s/nodePools/%s", gks.projectID, gks.region, gks.clusterName, nodePoolName),
	}

	operation, err := gks.clusterManagerClient.DeleteNodePool(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete node pool: %w", err)
	}

	if err = gks.WaitForClusterOperation(ctx, operation.Name); err != nil {
		return fmt.Errorf("node pool %s deletion operation failed: %w", nodePoolName, err)
	}

	return nil
}

func (gks *GKESession) DeleteCluster(ctx context.Context) error {
	req := &containerpb.DeleteClusterRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.projectID, gks.region, gks.clusterName),
	}

	operation, err := gks.clusterManagerClient.DeleteCluster(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete cluster: %w", err)
	}

	if err := gks.WaitForClusterOperation(ctx, operation.Name); err != nil {
		return fmt.Errorf("cluster %s deletion operation failed: %w", gks.clusterName, err)
	}

	return nil
}

func (gks *GKESession) IsSubnetExists(ctx context.Context, subnetName string) (bool, error) {
	req := &computepb.GetSubnetworkRequest{
		Project:    gks.projectID,
		Region:     gks.region,
		Subnetwork: subnetName,
	}

	_, err := gks.subnetClient.Get(ctx, req)
	if err != nil {
		// TODO : parse and check if the error is specifically that the subnet is not found
		return false, fmt.Errorf("failed to check subnet existence: %w", err)
	}

	return true, nil
}

func (gks *GKESession) DeleteSubnet(ctx context.Context, subnetName string, waitForSubnetDeletion bool) error {
	req := &computepb.DeleteSubnetworkRequest{
		Project:    gks.projectID,
		Region:     gks.region,
		Subnetwork: subnetName,
	}

	_, err := gks.subnetClient.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete subnet: %w", err)
	}

	// if err := gks.WaitForOperation(ctx, operation.Name()); err != nil {
	// 	return fmt.Errorf("subnet %s deletion operation failed: %w", subnetName, err)
	// }

	if !waitForSubnetDeletion {
		return nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(60)*time.Minute)
	defer cancel()

	interval := 60

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout reached while waiting for subnet %s to be deleted", subnetName)
		default:
		}

		exists, _ := gks.IsSubnetExists(timeoutCtx, subnetName)
		if !exists {
			logrus.Infof("Subnet %s has been deleted.\n", subnetName)
			return nil
		} else {
			logrus.Infof("Subnet %s still exists. Retrying in %v...\n", subnetName, interval)
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}

}

func (gks *GKESession) DeleteFirewallRule(ctx context.Context, firewallRuleName string) error {
	req := &computepb.DeleteFirewallRequest{
		Firewall: firewallRuleName,
		Project:  gks.projectID,
	}

	operation, err := gks.firewallsClient.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete firewall rule: %w", err)
	}

	if err := gks.WaitForOperation(ctx, operation.Name()); err != nil {
		return fmt.Errorf("firewall rule %s deletion operation failed: %w", firewallRuleName, err)
	}

	return nil
}

func (gks *GKESession) DeleteVirtualNetwork(ctx context.Context, virtualNetworkName string) error {
	req := &computepb.DeleteNetworkRequest{
		Network: virtualNetworkName,
		Project: gks.projectID,
	}

	operation, err := gks.networksClient.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete virtual network: %w", err)
	}

	if err := gks.WaitForOperation(ctx, operation.Name()); err != nil {
		return fmt.Errorf("virtual network %s deletion operation failed: %w", virtualNetworkName, err)
	}

	return nil
}

func (gks *GKESession) ListAvailableKubernetesVersions(ctx context.Context) ([]string, error) {
	req := &containerpb.GetServerConfigRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s", gks.projectID, gks.region),
	}

	resp, err := gks.clusterManagerClient.GetServerConfig(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get server configuration: %w", err)
	}

	return resp.ValidMasterVersions, nil
}

func (gks *GKESession) UpdateClusterKubernetesVersion(ctx context.Context, k8sVersion string,
	waitForUpdate bool) error {
	operation, err := gks.clusterManagerClient.UpdateCluster(ctx, &containerpb.UpdateClusterRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.projectID, gks.region, gks.clusterName),
		Update: &containerpb.ClusterUpdate{
			DesiredMasterVersion: k8sVersion,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update cluster version: %w", err)
	}

	if waitForUpdate {
		if err := gks.WaitForClusterOperation(ctx, operation.Name); err != nil {
			return fmt.Errorf("cluster %s kubernetes version upgrade operation failed: %w", gks.clusterName, err)
		}
	}

	return nil
}

func (gks *GKESession) UpdateMasterKubernetesVersion(ctx context.Context, k8sVersion string, waitForUpdate bool) error {
	operation, err := gks.clusterManagerClient.UpdateMaster(ctx, &containerpb.UpdateMasterRequest{
		Name:          fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.projectID, gks.region, gks.clusterName),
		MasterVersion: k8sVersion,
	})
	if err != nil {
		return fmt.Errorf("failed to master kubernetes version: %w", err)
	}

	if waitForUpdate {
		if err := gks.WaitForClusterOperation(ctx, operation.Name); err != nil {
			return fmt.Errorf("cluster master %s kubernetes version upgrade operation failed: %w", gks.clusterName, err)
		}
	}

	return nil
}

func (gks *GKESession) UpdateNodePoolKubernetesVersion(ctx context.Context, nodePoolName, k8sVersion string,
	waitForUpdate bool) error {
	operation, err := gks.clusterManagerClient.UpdateNodePool(ctx, &containerpb.UpdateNodePoolRequest{
		Name:        fmt.Sprintf("projects/%s/locations/%s/clusters/%s/nodePools/%s", gks.projectID, gks.region, gks.clusterName, nodePoolName),
		NodeVersion: k8sVersion,
	})
	if err != nil {
		return fmt.Errorf("failed to update node pool %s version: %w", nodePoolName, err)
	}

	if waitForUpdate {
		if err := gks.WaitForClusterOperation(ctx, operation.Name); err != nil {
			return fmt.Errorf("node pool %s kubernetes version upgrade operation failed: %w", nodePoolName, err)
		}
	}

	return nil
}

func (gks *GKESession) ListSubnets(ctx context.Context) ([]*computepb.Subnetwork, error) {
	var subnets []*computepb.Subnetwork

	it := gks.subnetClient.List(ctx, &computepb.ListSubnetworksRequest{
		Project: gks.projectID,
		Region:  gks.region,
	})

	// Iterate through the subnets and collect them
	for {
		subnet, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list subnets: %w", err)
		}
		subnets = append(subnets, subnet)
	}

	return subnets, nil
}
