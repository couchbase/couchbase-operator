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
	"google.golang.org/api/option"
)

var (
	ErrInvalidReleaseChannel = errors.New("invalid release channel")
	ErrOperationError        = errors.New("operation error")
)

type ReleaseChannel string

const (
	UnspecifiedReleaseChannel ReleaseChannel = "UNSPECIFIED"
	RapidReleaseChannel       ReleaseChannel = "RAPID"
	RegularReleaseChannel     ReleaseChannel = "REGULAR"
	StableReleaseChannel      ReleaseChannel = "STABLE"
)

var (
	releaseChannelMap = map[ReleaseChannel]int{
		UnspecifiedReleaseChannel: 0,
		RapidReleaseChannel:       1,
		RegularReleaseChannel:     2,
		StableReleaseChannel:      3,
	}
)

type GKESessionStore struct {
	GKESessions map[string]*GKESession
	lock        sync.Mutex
}

type GKESession struct {
	GlobalOperationsClient *compute.GlobalOperationsClient
	NetworksCLient         *compute.NetworksClient
	FirewallsClient        *compute.FirewallsClient
	SubnetClient           *compute.SubnetworksClient
	ClusterManagerClient   *container.ClusterManagerClient
	Cred                   *ManagedServiceCredentials
	Region                 string
	ClusterName            string
	ProjectID              string
}

func ValidateReleaseChannel(rc ReleaseChannel) (bool, error) {
	switch rc {
	case UnspecifiedReleaseChannel, RapidReleaseChannel, RegularReleaseChannel, StableReleaseChannel:
		return true, nil
	default:
		return false, ErrInvalidReleaseChannel
	}
}

func NewGKESession(ctx *context.Context, managedSvcCred *ManagedServiceCredentials) (*GKESession, error) {
	credsFileOptions := option.WithCredentialsFile(managedSvcCred.GKECredentials.gkeCredentialsJSONPath)

	networksClient, err := compute.NewNetworksRESTClient(*ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create networks client: %w", err)
	}

	globalOperationsClient, err := compute.NewGlobalOperationsRESTClient(*ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create operations client: %w", err)
	}

	firewallClient, err := compute.NewFirewallsRESTClient(*ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create firewall client: %w", err)
	}

	subnetClient, err := compute.NewSubnetworksRESTClient(*ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet client: %w", err)
	}

	clusterManagerClient, err := container.NewClusterManagerRESTClient(*ctx, credsFileOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create container client: %w", err)
	}

	return &GKESession{
		GlobalOperationsClient: globalOperationsClient,
		NetworksCLient:         networksClient,
		FirewallsClient:        firewallClient,
		SubnetClient:           subnetClient,
		ClusterManagerClient:   clusterManagerClient,
		Cred:                   managedSvcCred,
		Region:                 managedSvcCred.GKECredentials.gkeRegion,
		ClusterName:            managedSvcCred.ClusterName,
		ProjectID:              managedSvcCred.GKECredentials.gkeProjectID,
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

func (gss *GKESessionStore) SetSession(ctx *context.Context, managedSvcCred *ManagedServiceCredentials) error {
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

func (gss *GKESessionStore) GetSession(ctx *context.Context, managedSvcCred *ManagedServiceCredentials) (*GKESession, error) {
	if _, ok := gss.GKESessions[getGKEKey(managedSvcCred)]; !ok {
		err := gss.SetSession(ctx, managedSvcCred)
		if err != nil {
			return nil, fmt.Errorf("get gke session: %w", err)
		}
	}

	return gss.GKESessions[getGKEKey(managedSvcCred)], nil
}

func (gss *GKESessionStore) Check(ctx *context.Context, managedSvcCred *ManagedServiceCredentials) error {
	// TODO implement me
	panic("implement me")
}

func (gss *GKESessionStore) GetInstancesByK8sNodeName(ctx *context.Context, managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error) {
	// TODO implement me
	panic("implement me")
}

// ================================================
// ====== Methods implemented by GKESession ======
// ================================================

func (gks *GKESession) WaitForOperation(ctx *context.Context, operationName string) error {
	for {
		op, err := gks.GlobalOperationsClient.Get(*ctx, &computepb.GetGlobalOperationRequest{
			Project:   gks.ProjectID,
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

func (gks *GKESession) CreateVirtualNetwork(ctx *context.Context, networkName string, autoCreateSubnet bool) error {
	network := &computepb.Network{
		Name:                  &networkName,
		AutoCreateSubnetworks: &autoCreateSubnet,
	}

	req := &computepb.InsertNetworkRequest{
		Project:         gks.ProjectID,
		NetworkResource: network,
	}

	op, err := gks.NetworksCLient.Insert(*ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	err = gks.WaitForOperation(ctx, op.Name())
	if err != nil {
		return fmt.Errorf("operation failed: %w", err)
	}

	return nil
}

func (gks *GKESession) CreateFirewallRule(ctx *context.Context, firewallRuleName, networkName string) error {
	network := fmt.Sprintf("projects/%s/global/networks/%s", gks.ProjectID, networkName)
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
		Project:          gks.ProjectID,
		FirewallResource: firewallRule,
	}

	firewallOp, err := gks.FirewallsClient.Insert(*ctx, firewallReq)
	if err != nil {
		return fmt.Errorf("failed to create firewall rule: %w", err)
	}

	if err = gks.WaitForOperation(ctx, firewallOp.Name()); err != nil {
		return fmt.Errorf("firewall rule creation operation failed: %w", err)
	}

	return nil
}

func (gks *GKESession) CreateSubnet(ctx *context.Context, subnetName, networkName, ipCidrRange string) error {
	network := fmt.Sprintf("projects/%s/global/networks/%s", gks.ProjectID, networkName)
	region := fmt.Sprintf("projects/%s/regions/%s", gks.ProjectID, gks.Region)

	subnetwork := &computepb.Subnetwork{
		Name:        &subnetName,
		Network:     &network,
		IpCidrRange: &ipCidrRange,
		Region:      &region,
	}

	subnetReq := &computepb.InsertSubnetworkRequest{
		Project:            gks.ProjectID,
		Region:             gks.Region,
		SubnetworkResource: subnetwork,
	}

	_, err := gks.SubnetClient.Insert(*ctx, subnetReq)
	if err != nil {
		return fmt.Errorf("failed to create subnet %s: %w", subnetName, err)
	}

	// if err = gks.WaitForOperation(ctx, subnetOp.Name()); err != nil {
	// 	return fmt.Errorf("subnet %s creation operation failed: %w", subnetName, err)
	// }

	return nil
}

func (gks *GKESession) CreateCluster(ctx *context.Context, networkName, subnetworkName, machineType, imageType, diskType, initialClusterVersion, nodePoolName string,
	diskSize, numNodes int, releaseChannel ReleaseChannel) error {
	if ok, err := ValidateReleaseChannel(releaseChannel); !ok || err != nil {
		return fmt.Errorf("invalid release channel: %w", err)
	}

	cluster := &containerpb.Cluster{
		Name:     gks.ClusterName,
		Location: gks.Region,
		ReleaseChannel: &containerpb.ReleaseChannel{
			Channel: containerpb.ReleaseChannel_Channel(releaseChannelMap[releaseChannel]),
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
	}

	req := &containerpb.CreateClusterRequest{
		Parent:  fmt.Sprintf("projects/%s/locations/%s", gks.ProjectID, gks.Region),
		Cluster: cluster,
	}

	operation, err := gks.ClusterManagerClient.CreateCluster(*ctx, req, nil)
	if err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	if err = gks.WaitForOperation(ctx, operation.Name); err != nil {
		return fmt.Errorf("cluster %s creation operation failed: %w", gks.ClusterName, err)
	}

	return nil
}

func (gks *GKESession) CreateNodePool(ctx *context.Context, nodePoolName, machineType, diskType, imageType string, diskSize, numNodes int) error {
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
		Parent:   fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.ProjectID, gks.Region, gks.ClusterName),
		NodePool: nodePool,
	}

	op, err := gks.ClusterManagerClient.CreateNodePool(*ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create node pool: %v", err)
	}

	if err := gks.WaitForOperation(ctx, op.Name); err != nil {
		return fmt.Errorf("node pool %s creation operation failed: %w", nodePoolName, err)
	}

	return nil
}

func (gks *GKESession) GetCluster(ctx *context.Context) (*containerpb.Cluster, error) {
	req := &containerpb.GetClusterRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gks.ProjectID, gks.Region, gks.ClusterName),
	}

	cluster, err := gks.ClusterManagerClient.GetCluster(*ctx, req, nil)
	if err != nil {
		return nil, fmt.Errorf("error fetching cluster details: %w", err)
	}

	return cluster, nil
}
