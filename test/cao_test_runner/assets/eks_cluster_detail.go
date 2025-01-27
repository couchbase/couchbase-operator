package assets

import (
	"errors"
	"fmt"
	"sync"

	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

var (
	ErrEKSClusterNameAlreadySet = errors.New("eks cluster name already set, cannot be changed")
)

type EKSClusterDetail struct {
	eksClusterName    string
	nodeGroups        []*string
	kubernetesVersion string
	desiredSize       int32
	minSize           int32
	maxSize           int32
	instanceType      string
	ami               ekstypes.AMITypes
	diskSize          int32
	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewEKSClusterDetail(eksClusterName string, nodeGroups []*string, kubernetesVersion, instanceType string,
	desiredSize, minSize, maxSize, diskSize int32, ami ekstypes.AMITypes) *EKSClusterDetail {
	return &EKSClusterDetail{
		eksClusterName:    eksClusterName,
		nodeGroups:        nodeGroups,
		kubernetesVersion: kubernetesVersion,
		desiredSize:       desiredSize,
		minSize:           minSize,
		maxSize:           maxSize,
		instanceType:      instanceType,
		ami:               ami,
		diskSize:          diskSize,
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

type EKSClusterDetailGetter interface {
	GetEKSClusterName() string
	GetAllNodeGroups() []*string
	GetKubernetesVersion() string
	GetDesiredSize() int32
	GetMinSize() int32
	GetMaxSize() int32
	GetInstanceType() string
	GetAMI() ekstypes.AMITypes
	GetDiskSize() int32
}

type EKSClusterDetailGetterSetter interface {
	// Getters
	GetEKSClusterName() string
	GetAllNodeGroups() []*string
	GetKubernetesVersion() string
	GetDesiredSize() int32
	GetMinSize() int32
	GetMaxSize() int32
	GetInstanceType() string
	GetAMI() ekstypes.AMITypes
	GetDiskSize() int32

	// Setters
	SetEKSClusterName(eksClusterName string) error
	SetNodeGroups(nodeGroups []*string) error
	SetKubernetesVersion(kubernetesVersion string) error
	SetDesiredSize(desiredSize int32) error
	SetMinSize(minSize int32) error
	SetMaxSize(maxSize int32) error
	SetInstanceType(instanceType string) error
	SetAMI(ami ekstypes.AMITypes) error
	SetDiskSize(diskSize int32) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------EKSClusterDetail Getters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ekscd *EKSClusterDetail) GetEKSClusterName() string {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.eksClusterName
}

func (ekscd *EKSClusterDetail) GetAllNodeGroups() []*string {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	return ekscd.nodeGroups
}

func (ekscd *EKSClusterDetail) GetKubernetesVersion() string {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.kubernetesVersion
}

func (ekscd *EKSClusterDetail) GetDesiredSize() int32 {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.desiredSize
}

func (ekscd *EKSClusterDetail) GetMinSize() int32 {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.minSize
}

func (ekscd *EKSClusterDetail) GetMaxSize() int32 {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.maxSize
}

func (ekscd *EKSClusterDetail) GetInstanceType() string {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.instanceType
}

func (ekscd *EKSClusterDetail) GetAMI() ekstypes.AMITypes {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.ami
}

func (ekscd *EKSClusterDetail) GetDiskSize() int32 {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.diskSize
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
------------------EKSClusterDetail Setters-----------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ekscd *EKSClusterDetail) SetEKSClusterName(eksClusterName string) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	if ekscd.eksClusterName != "" {
		return fmt.Errorf("set eks cluster name: %w", ErrEKSClusterNameAlreadySet)
	}

	ekscd.eksClusterName = eksClusterName
	return nil
}

func (ekscd *EKSClusterDetail) SetNodeGroups(nodeGroups []*string) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.nodeGroups = nodeGroups
	return nil
}

func (ekscd *EKSClusterDetail) SetKubernetesVersion(kubernetesVersion string) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.kubernetesVersion = kubernetesVersion
	return nil
}

func (ekscd *EKSClusterDetail) SetDesiredSize(desiredSize int32) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.desiredSize = desiredSize
	return nil
}

func (ekscd *EKSClusterDetail) SetMinSize(minSize int32) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.minSize = minSize
	return nil
}

func (ekscd *EKSClusterDetail) SetMaxSize(maxSize int32) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.maxSize = maxSize
	return nil
}

func (ekscd *EKSClusterDetail) SetInstanceType(instanceType string) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.instanceType = instanceType
	return nil
}

func (ekscd *EKSClusterDetail) SetAMI(ami ekstypes.AMITypes) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.ami = ami
	return nil
}

func (ekscd *EKSClusterDetail) SetDiskSize(diskSize int32) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.diskSize = diskSize
	return nil
}
