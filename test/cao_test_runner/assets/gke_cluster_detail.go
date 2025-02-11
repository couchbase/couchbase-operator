package assets

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
)

var (
	ErrGKEClusterNameAlreadySet = errors.New("gke cluster name already set, cannot be changed")
	ErrGKEClusterNameNotSet     = errors.New("gke cluster name not set")
)

type GKEClusterDetail struct {
	gkeClusterName    string
	nodePools         []*string
	kubernetesVersion string
	machineType       string
	imageType         string
	diskType          string
	count             int32
	diskSize          int32
	releaseChannel    managedk8sservices.ReleaseChannel
	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewGKEClusterDetail(gkeClusterName string, nodePools []*string, kubernetesVersion, machineType, imageType, diskType string,
	diskSize, count int32, releaseChannel managedk8sservices.ReleaseChannel) *GKEClusterDetail {
	return &GKEClusterDetail{
		gkeClusterName:    gkeClusterName,
		nodePools:         nodePools,
		kubernetesVersion: kubernetesVersion,
		machineType:       machineType,
		imageType:         imageType,
		diskType:          diskType,
		count:             count,
		diskSize:          diskSize,
		releaseChannel:    releaseChannel,
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
	GetAllNodePools() []*string
	GetKubernetesVersion() string
	GetMachineType() string
	GetImageType() string
	GetDiskType() string
	GetCount() int32
	GetDiskSize() int32
	GetReleaseChannel() managedk8sservices.ReleaseChannel
}

type GKEClusterDetailGetterSetter interface {
	// Getters
	GetGKEClusterName() string
	GetAllNodePools() []*string
	GetKubernetesVersion() string
	GetMachineType() string
	GetImageType() string
	GetDiskType() string
	GetCount() int32
	GetDiskSize() int32
	GetReleaseChannel() managedk8sservices.ReleaseChannel

	// Setters
	SetGKEClusterName(gkeClusterName string) error
	SetNodePools(nodePools []*string) error
	SetKubernetesVersion(kubernetesVersion string) error
	SetMachineType(machineType string) error
	SetImageType(imageType string) error
	SetCount(count int32) error
	SetDiskSize(diskSize int32) error
	SetDiskType(diskType string) error
	SetReleaseChannel(releaseChannel managedk8sservices.ReleaseChannel) error
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

func (ac *GKEClusterDetail) GetAllNodePools() []*string {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.nodePools
}

func (ac *GKEClusterDetail) GetKubernetesVersion() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.kubernetesVersion
}

func (ac *GKEClusterDetail) GetMachineType() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.machineType
}

func (ac *GKEClusterDetail) GetImageType() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.imageType
}

func (ac *GKEClusterDetail) GetDiskType() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.diskType
}

func (ac *GKEClusterDetail) GetCount() int32 {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.count
}

func (ac *GKEClusterDetail) GetDiskSize() int32 {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.diskSize
}

func (ac *GKEClusterDetail) GetReleaseChannel() managedk8sservices.ReleaseChannel {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.releaseChannel
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

func (ac *GKEClusterDetail) SetNodePools(nodePools []*string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.nodePools = nodePools

	return nil
}

func (ac *GKEClusterDetail) SetKubernetesVersion(kubernetesVersion string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.kubernetesVersion = kubernetesVersion

	return nil
}

func (ac *GKEClusterDetail) SetMachineType(machineType string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.machineType = machineType

	return nil
}

func (ac *GKEClusterDetail) SetImageType(imageType string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.imageType = imageType

	return nil
}

func (ac *GKEClusterDetail) SetDiskType(diskType string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.diskType = diskType

	return nil
}

func (ac *GKEClusterDetail) SetCount(count int32) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.count = count

	return nil
}

func (ac *GKEClusterDetail) SetDiskSize(diskSize int32) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.diskSize = diskSize

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

	var nodePoolNames []*string

	nodePools, err := gkeSession.ListNodePools(ctx)
	if err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	for _, nodePool := range nodePools.NodePools {
		nodePoolNames = append(nodePoolNames, &nodePool.Name)
	}

	if err := kc.SetNodePools(nodePoolNames); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetKubernetesVersion(gkeCluster.CurrentMasterVersion); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetMachineType(nodePools.NodePools[0].Config.MachineType); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetImageType(nodePools.NodePools[0].Config.ImageType); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetDiskType(nodePools.NodePools[0].Config.DiskType); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetCount(nodePools.NodePools[0].InitialNodeCount); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetDiskSize(nodePools.NodePools[0].Config.DiskSizeGb); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	if err := kc.SetReleaseChannel(managedk8sservices.ReverseReleaseChannelMap[int(gkeCluster.ReleaseChannel.Channel)]); err != nil {
		return fmt.Errorf("populate gke cluster detail: %w", err)
	}

	return nil
}
