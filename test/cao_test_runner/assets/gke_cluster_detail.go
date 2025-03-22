package assets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
)

var (
	ErrGKEClusterNameAlreadySet = errors.New("gke cluster name already set, cannot be changed")
	ErrGKEClusterNameNotSet     = errors.New("gke cluster name not set")
	ErrGKENodePoolNameNotSet    = errors.New("gke node pool name not set")
)

type GKEClusterDetail struct {
	gkeClusterName    string
	kubernetesVersion string
	releaseChannel    managedk8sservices.ReleaseChannel
	nodePoolDetails   map[string]*GKENodePoolDetail
	// TODO Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

type GKENodePoolDetail struct {
	nodePoolName    string
	k8sVersion      string
	machineType     string
	imageType       string
	diskType        string
	numNodesPerZone int32
	diskSize        int32
	// TODO Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewGKEClusterDetail(gkeClusterName string, kubernetesVersion string,
	releaseChannel managedk8sservices.ReleaseChannel, nodePoolDetails map[string]*GKENodePoolDetail) *GKEClusterDetail {
	return &GKEClusterDetail{
		gkeClusterName:    gkeClusterName,
		kubernetesVersion: kubernetesVersion,
		releaseChannel:    releaseChannel,
		nodePoolDetails:   nodePoolDetails,
	}
}

func NewGKENodePoolDetail(npName string, k8sVersion, machineType, imageType, diskType string,
	numNodesPerZone, diskSize int32) *GKENodePoolDetail {
	return &GKENodePoolDetail{
		nodePoolName:    npName,
		k8sVersion:      k8sVersion,
		machineType:     machineType,
		imageType:       imageType,
		diskType:        diskType,
		numNodesPerZone: numNodesPerZone,
		diskSize:        diskSize,
	}
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
--------Getter and GetterSetter Interface Definitions------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

type GKEClusterDetailGetter interface {
	GetGKEClusterName() string
	GetKubernetesVersion() string
	GetReleaseChannel() managedk8sservices.ReleaseChannel
	GetNodePoolDetailGetter(nodePoolName string) GKENodePoolDetailGetter
}

type GKEClusterDetailGetterSetter interface {
	// Getters
	GetGKEClusterName() string
	GetKubernetesVersion() string
	GetReleaseChannel() managedk8sservices.ReleaseChannel
	GetNodePoolDetailGetterSetter(nodePoolName string) GKENodePoolDetailGetterSetter

	// Setters
	SetGKEClusterName(gkeClusterName string) error
	SetKubernetesVersion(kubernetesVersion string) error
	SetReleaseChannel(releaseChannel managedk8sservices.ReleaseChannel) error
	SetNodePoolDetail(nodePoolDetail *GKENodePoolDetail) error
}

type GKENodePoolDetailGetter interface {
	GetNodePoolName() string
	GetKubernetesVersion() string
	GetMachineType() string
	GetImageType() string
	GetDiskType() string
	GetNumNodesPerZone() int32
	GetDiskSize() int32
}

type GKENodePoolDetailGetterSetter interface {
	// Getters
	GetNodePoolName() string
	GetKubernetesVersion() string
	GetMachineType() string
	GetImageType() string
	GetDiskType() string
	GetNumNodesPerZone() int32
	GetDiskSize() int32

	// Setters
	SetNodePoolName(npName string) error
	SetKubernetesVersion(kubernetesVersion string) error
	SetMachineType(machineType string) error
	SetImageType(imageType string) error
	SetNumNodesPerZone(count int32) error
	SetDiskSize(diskSize int32) error
	SetDiskType(diskType string) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------GKEClusterDetail Getters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *GKEClusterDetail) GetGKEClusterName() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.gkeClusterName
}

func (ac *GKEClusterDetail) GetKubernetesVersion() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.kubernetesVersion
}

func (ac *GKEClusterDetail) GetReleaseChannel() managedk8sservices.ReleaseChannel {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.releaseChannel
}

func (ac *GKEClusterDetail) GetNodePoolDetailGetter(nodePoolName string) GKENodePoolDetailGetter {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.nodePoolDetails[nodePoolName]
}

func (ac *GKEClusterDetail) GetNodePoolDetailGetterSetter(nodePoolName string) GKENodePoolDetailGetterSetter {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.nodePoolDetails[nodePoolName]
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------GKEClusterDetail Setters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *GKEClusterDetail) SetGKEClusterName(gkeClusterName string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.gkeClusterName != "" {
		return fmt.Errorf("set gke cluster name: %w", ErrGKEClusterNameAlreadySet)
	}

	ac.gkeClusterName = gkeClusterName

	return nil
}

func (ac *GKEClusterDetail) SetKubernetesVersion(kubernetesVersion string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.kubernetesVersion = kubernetesVersion

	return nil
}

func (ac *GKEClusterDetail) SetReleaseChannel(releaseChannel managedk8sservices.ReleaseChannel) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, err := managedk8sservices.ValidateReleaseChannel(releaseChannel); err != nil {
		return fmt.Errorf("set release channel: %w", err)
	}

	ac.releaseChannel = releaseChannel

	return nil
}

func (ac *GKEClusterDetail) SetNodePoolDetail(nodePoolDetail *GKENodePoolDetail) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.nodePoolDetails[nodePoolDetail.GetNodePoolName()] = nodePoolDetail

	return nil
}

// =================================================================================
// =================================================================================
// =================================================================================
// ========================== GKENodePoolDetail Getters ============================
// =================================================================================
// =================================================================================
// =================================================================================

func (gnpd *GKENodePoolDetail) GetNodePoolName() string {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	return gnpd.nodePoolName
}

func (gnpd *GKENodePoolDetail) GetKubernetesVersion() string {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	return gnpd.k8sVersion
}

func (gnpd *GKENodePoolDetail) GetMachineType() string {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	return gnpd.machineType
}

func (gnpd *GKENodePoolDetail) GetImageType() string {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	return gnpd.imageType
}

func (gnpd *GKENodePoolDetail) GetDiskType() string {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	return gnpd.diskType
}

func (gnpd *GKENodePoolDetail) GetNumNodesPerZone() int32 {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	return gnpd.numNodesPerZone
}

func (gnpd *GKENodePoolDetail) GetDiskSize() int32 {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	return gnpd.diskSize
}

// =================================================================================
// =================================================================================
// =================================================================================
// ========================== GKENodePoolDetail Setters ============================
// =================================================================================
// =================================================================================
// =================================================================================

func (gnpd *GKENodePoolDetail) SetNodePoolName(npName string) error {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	gnpd.nodePoolName = npName

	return nil
}

func (gnpd *GKENodePoolDetail) SetKubernetesVersion(kubernetesVersion string) error {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	gnpd.k8sVersion = kubernetesVersion

	return nil
}

func (gnpd *GKENodePoolDetail) SetMachineType(machineType string) error {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	gnpd.machineType = machineType

	return nil
}

func (gnpd *GKENodePoolDetail) SetImageType(imageType string) error {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	gnpd.imageType = imageType

	return nil
}

func (gnpd *GKENodePoolDetail) SetDiskType(diskType string) error {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	gnpd.diskType = diskType

	return nil
}

func (gnpd *GKENodePoolDetail) SetNumNodesPerZone(count int32) error {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	gnpd.numNodesPerZone = count

	return nil
}

func (gnpd *GKENodePoolDetail) SetDiskSize(diskSize int32) error {
	gnpd.mu.Lock()
	defer gnpd.mu.Unlock()

	gnpd.diskSize = diskSize

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------Populate GKE Cluster Detail Functions---------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *GKEClusterDetail) PopulateGKEClusterDetail() error {
	if kc.GetGKEClusterName() == "" {
		return fmt.Errorf("populate gke cluster detail: %w", ErrGKEClusterNameNotSet)
	}

	managedServiceProvider := managedk8sservices.NewManagedServiceProvider(managedk8sservices.Kubernetes,
		managedk8sservices.Cloud, managedk8sservices.GCP)

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, kc.GetGKEClusterName())
	if err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	ctx := context.Background()

	gkeSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = gkeSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	gkeSession, err := gkeSessionStore.(*managedk8sservices.GKESessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	gkeCluster, err := gkeSession.GetCluster(ctx)
	if err != nil {
		// Cluster does not exist in GKE
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	// Populate the GKE Cluster Details
	if err := kc.SetKubernetesVersion(gkeCluster.CurrentMasterVersion); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetReleaseChannel(managedk8sservices.ReverseReleaseChannelMap[int(gkeCluster.ReleaseChannel.Channel)]); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	kc.mu.Lock()

	kc.nodePoolDetails = make(map[string]*GKENodePoolDetail)

	kc.mu.Unlock()

	// Populate the GKE Node Pool Details
	for _, nodePool := range nodePools.NodePools {
		gkeNodePoolDetail := &GKENodePoolDetail{
			nodePoolName: nodePool.Name,
		}

		err := gkeNodePoolDetail.populateGKENodePoolDetail(nodePool)
		if err != nil {
			return fmt.Errorf("populate gke cluster detail: %w", err)
		}

		err = kc.SetNodePoolDetail(gkeNodePoolDetail)
		if err != nil {
			return fmt.Errorf("populate gke cluster detail: %w", err)
		}
	}

	return nil
}

func (pgnpd *GKENodePoolDetail) populateGKENodePoolDetail(nodePool *containerpb.NodePool) error {
	if pgnpd.GetNodePoolName() == "" {
		return fmt.Errorf("populate gke nodepool detail: %w", ErrGKENodePoolNameNotSet)
	}

	if err := pgnpd.SetKubernetesVersion(strings.Split(nodePool.Version, "-")[0]); err != nil {
		return fmt.Errorf("populate gke nodepool detail: %w", err)
	}

	if err := pgnpd.SetMachineType(nodePool.Config.MachineType); err != nil {
		return fmt.Errorf("populate gke nodepool detail: %w", err)
	}

	if err := pgnpd.SetImageType(nodePool.Config.ImageType); err != nil {
		return fmt.Errorf("populate gke nodepool detail: %w", err)
	}

	if err := pgnpd.SetDiskType(nodePool.Config.DiskType); err != nil {
		return fmt.Errorf("populate gke nodepool detail: %w", err)
	}

	if err := pgnpd.SetNumNodesPerZone(nodePool.InitialNodeCount); err != nil {
		return fmt.Errorf("populate gke nodepool detail: %w", err)
	}

	if err := pgnpd.SetDiskSize(nodePool.Config.DiskSizeGb); err != nil {
		return fmt.Errorf("populate gke nodepool detail: %w", err)
	}

	return nil
}
