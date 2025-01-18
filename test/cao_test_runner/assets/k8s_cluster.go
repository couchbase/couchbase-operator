package assets

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrServiceProviderAlreadySet = errors.New("service provider already set, cannot be changed")
	ErrPlatformAlreadySet        = errors.New("platform already set, cannot be changed")
)

type K8SCluster struct {
	serviceProvider *ManagedServiceProvider

	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

type K8SClusterGetter interface {
	GetServiceProvider() *ManagedServiceProvider
}

type K8SClusterGetterSetter interface {
	// Getters
	GetServiceProvider() *ManagedServiceProvider

	// Setters
	SetServiceProvider(ms *ManagedServiceProvider) error
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

func (kc *K8SCluster) GetServiceProvider() *ManagedServiceProvider {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.serviceProvider
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
