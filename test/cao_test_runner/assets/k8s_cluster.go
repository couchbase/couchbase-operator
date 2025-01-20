package assets

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrClusterNameAlreadySet = errors.New("cluster name already set, cannot be changed")
)

type K8SCluster struct {
	clusterName     string
	serviceProvider *ManagedServiceProvider

	nodes []*string

	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewK8SCluster(clusterName string, serviceProvider *ManagedServiceProvider, nodes []*string) *K8SCluster {
	return &K8SCluster{
		clusterName:     clusterName,
		serviceProvider: serviceProvider,
		nodes:           nodes,
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

type K8SClusterGetter interface {
	GetClusterName() string
	GetServiceProvider() *ManagedServiceProvider
	GetNodes() []*string
}

type K8SClusterGetterSetter interface {
	// Getters
	GetClusterName() string
	GetServiceProvider() *ManagedServiceProvider
	GetNodes() []*string

	// Setters
	SetClusterName(clusterName string) error
	SetServiceProvider(ms *ManagedServiceProvider) error
	SetNodes(nodes []*string) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------K8SCluster Getters----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *K8SCluster) GetClusterName() string {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.clusterName
}

func (kc *K8SCluster) GetServiceProvider() *ManagedServiceProvider {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.serviceProvider
}

func (kc *K8SCluster) GetNodes() []*string {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.nodes
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------K8SCluster Setters----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *K8SCluster) SetClusterName(clusterName string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	if kc.clusterName != "" {
		return fmt.Errorf("set cluster name: %w", ErrClusterNameAlreadySet)
	}

	kc.clusterName = clusterName

	return nil
}

func (kc *K8SCluster) SetServiceProvider(ms *ManagedServiceProvider) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	if kc.serviceProvider != nil {
		return fmt.Errorf("set service provider: %w", ErrServiceProviderAlreadySet)
	}

	if err := ValidateManagedServices(ms); err != nil {
		return fmt.Errorf("set service provider: %w", err)
	}

	kc.serviceProvider = ms

	return nil
}

func (kc *K8SCluster) SetNodes(nodes []*string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	kc.nodes = nodes

	return nil
}
