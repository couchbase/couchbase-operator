package assets

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrNamespaceNameAlreadySet = errors.New("namespace name already set, cannot be changed")
)

type Namespace struct {
	namespaceName string
	pods          []*string
	services      []*string

	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewNamespace(namespaceName string, pods []*string, services []*string) *Namespace {
	return &Namespace{
		namespaceName: namespaceName,
		pods:          pods,
		services:      services,
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

type NamespaceGetter interface {
	GetNamespaceName() string
	GetAllPods() []*string
	GetAllServices() []*string
}

type NamespaceGetterSetter interface {
	// Getters
	GetNamespaceName() string
	GetAllPods() []*string
	GetAllServices() []*string

	// Setters
	SetNamespaceName(namespaceName string) error
	SetPods(pods []*string) error
	SetServices(services []*string) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------Namespace Getters-----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ns *Namespace) GetNamespaceName() string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return ns.namespaceName
}

func (ns *Namespace) GetAllPods() []*string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return ns.pods
}

func (ns *Namespace) GetAllServices() []*string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return ns.services
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------Namespace Setters-----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ns *Namespace) SetNamespaceName(namespaceName string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if ns.namespaceName != "" {
		return fmt.Errorf("set namespace name: %w", ErrNamespaceNameAlreadySet)
	}

	ns.namespaceName = namespaceName

	return nil
}

func (ns *Namespace) SetPods(pods []*string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.pods = pods
	return nil
}

func (ns *Namespace) SetServices(services []*string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.services = services
	return nil
}
