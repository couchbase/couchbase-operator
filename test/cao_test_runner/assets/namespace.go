package assets

import (
	"errors"
	"fmt"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/services"
)

var (
	ErrNamespaceNameAlreadySet = errors.New("namespace name already set, cannot be changed")
	ErrNamespaceNameNotSet     = errors.New("namespace name not set")
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

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------Populate Namespace-----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ns *Namespace) PopulateNamespace() error {
	if ns.namespaceName == "" {
		return fmt.Errorf("populate namespace: %w", ErrNamespaceNameNotSet)
	}

	allPods, err := pods.GetPodNames(ns.namespaceName)
	if err != nil && !errors.Is(err, pods.ErrNoPodsInNamespace) {
		return fmt.Errorf("populate namespace: %w", err)
	}

	allServices, err := services.GetServiceNames(ns.namespaceName)
	if err != nil && !errors.Is(err, services.ErrNoServicesInNamespace) {
		return fmt.Errorf("populate namespace: %w", err)
	}

	var pods []*string
	for _, pod := range allPods {
		pods = append(pods, &pod)
	}

	var services []*string
	for _, service := range allServices {
		services = append(services, &service)
	}

	if err := ns.SetPods(pods); err != nil {
		return fmt.Errorf("populate namespace: %w", err)
	}

	if err := ns.SetServices(services); err != nil {
		return fmt.Errorf("populate namespace: %w", err)
	}

	return nil
}
