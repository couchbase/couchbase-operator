package assets

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
)

var (
	ErrAKSClusterNameAlreadySet = errors.New("aks cluster name already set, cannot be changed")
	ErrAKSClusterNameNotSet     = errors.New("aks cluster name not set")
)

type AKSClusterDetail struct {
	aksClusterName    string
	nodePools         map[string]*AKSNodePool
	kubernetesVersion string
	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

type AKSNodePool struct {
	name     string
	osSKU    armcontainerservice.OSSKU
	osType   armcontainerservice.OSType
	count    int32
	vmSize   string
	diskSize int32
	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewAKSNodePool(name string, osSKU armcontainerservice.OSSKU, osType armcontainerservice.OSType, count int32, vmSize string, diskSize int32) *AKSNodePool {
	return &AKSNodePool{
		name:     name,
		osSKU:    osSKU,
		osType:   osType,
		count:    count,
		vmSize:   vmSize,
		diskSize: diskSize,
	}
}

func NewAKSClusterDetail(aksClusterName string, nodePools []*AKSNodePool, kubernetesVersion string) *AKSClusterDetail {
	var nodePoolsMap = make(map[string]*AKSNodePool)
	for _, nodePool := range nodePools {
		nodePoolsMap[nodePool.name] = nodePool
	}

	return &AKSClusterDetail{
		aksClusterName:    aksClusterName,
		nodePools:         nodePoolsMap,
		kubernetesVersion: kubernetesVersion,
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

type AKSNodePoolGetter interface {
	GetNodePoolName() string
	GetOsSKU() armcontainerservice.OSSKU
	GetOsType() armcontainerservice.OSType
	GetCount() int32
	GetVMSize() string
	GetDiskSize() int32
}

type AKSNodePoolGetterSetter interface {
	// Getters
	GetNodePoolName() string
	GetOsSKU() armcontainerservice.OSSKU
	GetOsType() armcontainerservice.OSType
	GetCount() int32
	GetVMSize() string
	GetDiskSize() int32

	// Setters
	SetNodePoolName(nodePoolName string) error
	SetOsSKU(osSKU armcontainerservice.OSSKU) error
	SetOsType(osType armcontainerservice.OSType) error
	SetCount(count int32) error
	SetVMSize(vmSize string) error
	SetDiskSize(diskSize int32) error
}

type AKSClusterDetailGetter interface {
	GetAKSClusterName() string
	GetAllNodePoolsGetter() []AKSNodePoolGetter
	GetNodePoolGetter(nodePoolName string) AKSNodePoolGetter
	GetKubernetesVersion() string
}

type AKSClusterDetailGetterSetter interface {
	// Getters
	GetAKSClusterName() string
	GetAllNodePoolsGetterSetter() []AKSNodePoolGetterSetter
	GetNodePoolGetterSetter(nodePoolName string) AKSNodePoolGetterSetter
	GetKubernetesVersion() string

	// Setters
	SetAKSClusterName(aksClusterName string) error
	SetNodePool(nodePool *AKSNodePool) error
	SetKubernetesVersion(kubernetesVersion string) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------AKSClusterDetail Getters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *AKSClusterDetail) GetAKSClusterName() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.aksClusterName
}

func (ac *AKSClusterDetail) GetAllNodePoolsGetter() []AKSNodePoolGetter {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	var nodePools []AKSNodePoolGetter
	for _, nodePool := range ac.nodePools {
		nodePools = append(nodePools, nodePool)
	}

	return nodePools
}

func (ac *AKSClusterDetail) GetNodePoolGetter(nodePoolName string) AKSNodePoolGetter {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.nodePools[nodePoolName]
}

func (ac *AKSClusterDetail) GetAllNodePoolsGetterSetter() []AKSNodePoolGetterSetter {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	var nodePools []AKSNodePoolGetterSetter
	for _, nodePool := range ac.nodePools {
		nodePools = append(nodePools, nodePool)
	}

	return nodePools
}

func (ac *AKSClusterDetail) GetNodePoolGetterSetter(nodePoolName string) AKSNodePoolGetterSetter {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	return ac.nodePools[nodePoolName]
}

func (ac *AKSClusterDetail) GetKubernetesVersion() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.kubernetesVersion
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------AKSClusterDetail Setters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *AKSClusterDetail) SetAKSClusterName(aksClusterName string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.aksClusterName != "" {
		return fmt.Errorf("set aks cluster name: %w", ErrAKSClusterNameAlreadySet)
	}

	ac.aksClusterName = aksClusterName

	return nil
}

func (ac *AKSClusterDetail) SetNodePool(nodePool *AKSNodePool) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.nodePools[nodePool.name] = nodePool

	return nil
}

func (ac *AKSClusterDetail) SetKubernetesVersion(kubernetesVersion string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.kubernetesVersion = kubernetesVersion

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------AKSNodePool Getters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *AKSNodePool) GetNodePoolName() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.name
}

func (ac *AKSNodePool) GetOsSKU() armcontainerservice.OSSKU {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.osSKU
}

func (ac *AKSNodePool) GetOsType() armcontainerservice.OSType {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.osType
}

func (ac *AKSNodePool) GetCount() int32 {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.count
}

func (ac *AKSNodePool) GetVMSize() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.vmSize
}

func (ac *AKSNodePool) GetDiskSize() int32 {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.diskSize
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------AKSNodePool Setters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *AKSNodePool) SetNodePoolName(nodePoolName string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.name = nodePoolName

	return nil
}

func (ac *AKSNodePool) SetOsSKU(osSKU armcontainerservice.OSSKU) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.osSKU = osSKU

	return nil
}

func (ac *AKSNodePool) SetOsType(osType armcontainerservice.OSType) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.osType = osType

	return nil
}

func (ac *AKSNodePool) SetCount(count int32) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.count = count

	return nil
}

func (ac *AKSNodePool) SetVMSize(vmSize string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.vmSize = vmSize

	return nil
}

func (ac *AKSNodePool) SetDiskSize(diskSize int32) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.diskSize = diskSize

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------Populate AKS Cluster Detail Functions---------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *AKSClusterDetail) PopulateAKSClusterDetail() error {
	if kc.aksClusterName == "" {
		return fmt.Errorf("populate aks cluster detail: %w", ErrAKSClusterNameNotSet)
	}

	managedServiceProvider := managedk8sservices.NewManagedServiceProvider(managedk8sservices.Kubernetes,
		managedk8sservices.Cloud, managedk8sservices.Azure)

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, kc.aksClusterName)
	if err != nil {
		return fmt.Errorf("populate aks cluster detail: %w", err)
	}

	ctx := context.Background()

	aksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = aksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("populate aks cluster detail: %w", err)
	}

	aksSession, err := aksSessionStore.(*managedk8sservices.AKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("populate aks cluster detail: %w", err)
	}

	resourceGroup := kc.aksClusterName + "-rg"

	aksCluster, err := aksSession.GetCluster(ctx, resourceGroup)
	if err != nil {
		// Cluster does not exist in AKS
		return fmt.Errorf("populate aks cluster detail: %w", err)
	}

	kc.nodePools = make(map[string]*AKSNodePool)

	nodePools, err := aksSession.ListNodePools(ctx, resourceGroup)
	if err != nil {
		return fmt.Errorf("populate aks cluster detail: %w", err)
	}

	for _, nodePool := range nodePools {
		assetNodePool := NewAKSNodePool(*nodePool.Name, *nodePool.Properties.OSSKU,
			*nodePool.Properties.OSType, *nodePool.Properties.Count, *nodePool.Properties.VMSize, *nodePool.Properties.OSDiskSizeGB)

		if err := kc.SetNodePool(assetNodePool); err != nil {
			return fmt.Errorf("populate aks cluster detail: %w", err)
		}
	}

	if err := kc.SetKubernetesVersion(*aksCluster.Properties.KubernetesVersion); err != nil {
		return fmt.Errorf("populate aks cluster detail: %w", err)
	}

	return nil
}
