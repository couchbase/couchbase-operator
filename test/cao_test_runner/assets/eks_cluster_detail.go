package assets

import (
	"context"
	"errors"
	"fmt"
	"sync"

	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
)

var (
	ErrEKSClusterNameAlreadySet = errors.New("eks cluster name already set, cannot be changed")
	ErrEKSClusterNameNotSet     = errors.New("eks cluster name not set")
	ErrEKSNodegroupNameNotFound = errors.New("eks nodegroup name not found")
)

type EKSClusterDetail struct {
	eksClusterName      string
	nodegroupNames      []*string
	kubernetesVersion   string
	eksNodegroupDetails map[string]*EKSNodegroupDetail
	// TODO Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

type EKSNodegroupDetail struct {
	ngName       *string
	ngK8SVersion string
	desiredSize  int32
	minSize      int32
	maxSize      int32
	instanceType string
	ami          ekstypes.AMITypes
	diskSize     int32
	// TODO Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewEKSClusterDetail(eksClusterName string, nodegroupNames []*string, kubernetesVersion string, nodegroupDetails map[string]*EKSNodegroupDetail) *EKSClusterDetail {
	return &EKSClusterDetail{
		eksClusterName:      eksClusterName,
		nodegroupNames:      nodegroupNames,
		kubernetesVersion:   kubernetesVersion,
		eksNodegroupDetails: nodegroupDetails,
	}
}

func NewEKSNodegroupDetail(nodegroupName *string, k8sVersion, instanceType string,
	desiredSize, minSize, maxSize, diskSize int32, ami ekstypes.AMITypes) *EKSNodegroupDetail {
	return &EKSNodegroupDetail{
		ngName:       nodegroupName,
		ngK8SVersion: k8sVersion,
		desiredSize:  desiredSize,
		minSize:      minSize,
		maxSize:      maxSize,
		instanceType: instanceType,
		ami:          ami,
		diskSize:     diskSize,
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
	GetAllNodegroupNames() []*string
	GetKubernetesVersion() string
	GetNodegroupDetailGetter(nodegroupName *string) EKSNodegroupDetailGetter
}

type EKSClusterDetailGetterSetter interface {
	// Getters
	GetEKSClusterName() string
	GetAllNodegroupNames() []*string
	GetKubernetesVersion() string
	GetNodegroupDetailGetterSetter(nodegroupName *string) EKSNodegroupDetailGetterSetter

	// Setters
	SetEKSClusterName(eksClusterName string) error
	SetNodegroupNames(nodeGroups []*string) error
	SetKubernetesVersion(kubernetesVersion string) error
	SetNodegroup(nodegroup *EKSNodegroupDetail) error
}

type EKSNodegroupDetailGetter interface {
	GetNodegroupName() *string
	GetK8SVersion() string
	GetDesiredSize() int32
	GetMinSize() int32
	GetMaxSize() int32
	GetInstanceType() string
	GetAMI() ekstypes.AMITypes
	GetDiskSize() int32
}

type EKSNodegroupDetailGetterSetter interface {
	// Getters
	GetNodegroupName() *string
	GetK8SVersion() string
	GetDesiredSize() int32
	GetMinSize() int32
	GetMaxSize() int32
	GetInstanceType() string
	GetAMI() ekstypes.AMITypes
	GetDiskSize() int32

	// Setters
	SetNodegroupName(nodegroupName *string) error
	SetK8SVersion(k8sVersion string) error
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

func (ekscd *EKSClusterDetail) GetAllNodegroupNames() []*string {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	return ekscd.nodegroupNames
}

func (ekscd *EKSClusterDetail) GetKubernetesVersion() string {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()

	return ekscd.kubernetesVersion
}

func (ekscd *EKSClusterDetail) GetNodegroupDetailGetter(nodegroupName *string) EKSNodegroupDetailGetter {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	return ekscd.eksNodegroupDetails[*nodegroupName]
}

func (ekscd *EKSClusterDetail) GetNodegroupDetailGetterSetter(nodegroupName *string) EKSNodegroupDetailGetterSetter {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	return ekscd.eksNodegroupDetails[*nodegroupName]
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

func (ekscd *EKSClusterDetail) SetNodegroupNames(nodeGroups []*string) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.nodegroupNames = nodeGroups
	return nil
}

func (ekscd *EKSClusterDetail) SetKubernetesVersion(kubernetesVersion string) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.kubernetesVersion = kubernetesVersion
	return nil
}

func (ekscd *EKSClusterDetail) SetNodegroup(nodegroup *EKSNodegroupDetail) error {
	ekscd.mu.Lock()
	defer ekscd.mu.Unlock()
	ekscd.eksNodegroupDetails[*nodegroup.GetNodegroupName()] = nodegroup
	return nil
}

// =================================================================================
// =================================================================================
// =================================================================================
// ========================= EKSNodegroupDetail Getters ============================
// =================================================================================
// =================================================================================
// =================================================================================

func (end *EKSNodegroupDetail) GetNodegroupName() *string {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.ngName
}

func (end *EKSNodegroupDetail) GetK8SVersion() string {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.ngK8SVersion
}

func (end *EKSNodegroupDetail) GetDesiredSize() int32 {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.desiredSize
}

func (end *EKSNodegroupDetail) GetMinSize() int32 {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.minSize
}

func (end *EKSNodegroupDetail) GetMaxSize() int32 {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.maxSize
}

func (end *EKSNodegroupDetail) GetInstanceType() string {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.instanceType
}

func (end *EKSNodegroupDetail) GetAMI() ekstypes.AMITypes {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.ami
}

func (end *EKSNodegroupDetail) GetDiskSize() int32 {
	end.mu.Lock()
	defer end.mu.Unlock()

	return end.diskSize
}

// =================================================================================
// =================================================================================
// =================================================================================
// ========================= EKSNodegroupDetail Setters ============================
// =================================================================================
// =================================================================================
// =================================================================================

func (end *EKSNodegroupDetail) SetNodegroupName(nodegroupName *string) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.ngName = nodegroupName
	return nil
}

func (end *EKSNodegroupDetail) SetK8SVersion(k8sVersion string) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.ngK8SVersion = k8sVersion
	return nil
}

func (end *EKSNodegroupDetail) SetDesiredSize(desiredSize int32) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.desiredSize = desiredSize
	return nil
}

func (end *EKSNodegroupDetail) SetMinSize(minSize int32) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.minSize = minSize
	return nil
}

func (end *EKSNodegroupDetail) SetMaxSize(maxSize int32) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.maxSize = maxSize
	return nil
}

func (end *EKSNodegroupDetail) SetInstanceType(instanceType string) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.instanceType = instanceType
	return nil
}

func (end *EKSNodegroupDetail) SetAMI(ami ekstypes.AMITypes) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.ami = ami
	return nil
}

func (end *EKSNodegroupDetail) SetDiskSize(diskSize int32) error {
	end.mu.Lock()
	defer end.mu.Unlock()
	end.diskSize = diskSize
	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------Populate EKS Cluster Detail Functions---------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *EKSClusterDetail) PopulateEKSClusterDetail() error {
	if kc.GetEKSClusterName() == "" {
		return fmt.Errorf("populate eks cluster: %w", ErrEKSClusterNameNotSet)
	}

	managedServiceProvider := managedk8sservices.NewManagedServiceProvider(managedk8sservices.Kubernetes,
		managedk8sservices.Cloud, managedk8sservices.AWS)

	svc, err := managedk8sservices.NewManagedServiceCredentials(
		[]*managedk8sservices.ManagedServiceProvider{managedServiceProvider}, kc.GetEKSClusterName())
	if err != nil {
		return fmt.Errorf("populate eks cluster: %w", err)
	}

	ctx := context.Background()

	eksSessionStore := managedk8sservices.NewManagedService(managedServiceProvider)
	if err = eksSessionStore.SetSession(ctx, svc); err != nil {
		return fmt.Errorf("populate eks cluster: %w", err)
	}

	eksSession, err := eksSessionStore.(*managedk8sservices.EKSSessionStore).GetSession(ctx, svc)
	if err != nil {
		return fmt.Errorf("populate eks cluster: %w", err)
	}

	eksCluster, err := eksSession.GetEKSCluster(ctx)
	if err != nil {
		// Cluster does not exist in EKS
		return fmt.Errorf("populate eks cluster: %w", err)
	}

	nodeGroups, err := eksSession.GetNodegroupsForCluster(ctx)
	if err != nil {
		return fmt.Errorf("populate eks cluster: %w", err)
	}

	if err := kc.SetKubernetesVersion(*eksCluster.Version); err != nil {
		return fmt.Errorf("populate eks cluster: %w", err)
	}

	kc.mu.Lock()

	kc.eksNodegroupDetails = make(map[string]*EKSNodegroupDetail)

	kc.mu.Unlock()

	var nodeGroupNames []*string

	// Populate the nodegroup details
	for _, ng := range nodeGroups {
		nodeGroupNames = append(nodeGroupNames, ng.NodegroupName)

		eksNgDetail := &EKSNodegroupDetail{
			ngName: ng.NodegroupName,
		}

		err := eksNgDetail.populateEKSNodegroupDetail(ng)
		if err != nil {
			return fmt.Errorf("populate eks cluster: %w", err)
		}

		err = kc.SetNodegroup(eksNgDetail)
		if err != nil {
			return fmt.Errorf("populate eks cluster: %w", err)
		}
	}

	if err := kc.SetNodegroupNames(nodeGroupNames); err != nil {
		return fmt.Errorf("populate eks cluster: %w", err)
	}

	return nil
}

func (kcd *EKSNodegroupDetail) populateEKSNodegroupDetail(ng *ekstypes.Nodegroup) error {
	if kcd.GetNodegroupName() == nil || *kcd.GetNodegroupName() == "" {
		return fmt.Errorf("populate eks nodegroup: %w", ErrEKSNodegroupNameNotFound)
	}

	if err := kcd.SetK8SVersion(*ng.Version); err != nil {
		return fmt.Errorf("populate eks nodegroup %s: %w", *ng.NodegroupName, err)
	}

	if err := kcd.SetDesiredSize(*ng.ScalingConfig.DesiredSize); err != nil {
		return fmt.Errorf("populate eks nodegroup %s: %w", *ng.NodegroupName, err)
	}

	if err := kcd.SetMinSize(*ng.ScalingConfig.MinSize); err != nil {
		return fmt.Errorf("populate eks nodegroup %s: %w", *ng.NodegroupName, err)
	}

	if err := kcd.SetMaxSize(*ng.ScalingConfig.MaxSize); err != nil {
		return fmt.Errorf("populate eks nodegroup %s: %w", *ng.NodegroupName, err)
	}

	if err := kcd.SetInstanceType(ng.InstanceTypes[0]); err != nil {
		return fmt.Errorf("populate eks nodegroup %s: %w", *ng.NodegroupName, err)
	}

	if err := kcd.SetAMI(ng.AmiType); err != nil {
		return fmt.Errorf("populate eks nodegroup %s: %w", *ng.NodegroupName, err)
	}

	if err := kcd.SetDiskSize(*ng.DiskSize); err != nil {
		return fmt.Errorf("populate eks nodegroup %s: %w", *ng.NodegroupName, err)
	}

	return nil
}
