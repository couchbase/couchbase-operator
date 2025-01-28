package assets

import (
	"errors"
	"fmt"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

var (
	ErrClusterNameAlreadySet = errors.New("cluster name already set, cannot be changed")
)

var (
	ErrNamespaceNotFound = errors.New("namespace not found")
)

type K8SCluster struct {
	clusterName     string
	serviceProvider *managedk8sservices.ManagedServiceProvider

	nodes []*string

	namespaces map[string]*Namespace

	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewK8SCluster(clusterName string, serviceProvider *managedk8sservices.ManagedServiceProvider, nodes []*string) *K8SCluster {
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
	GetServiceProvider() *managedk8sservices.ManagedServiceProvider
	GetNodes() []*string
	GetAllNamespacesGetters() []NamespaceGetter
	GetNamespaceGetter(namespace string) (NamespaceGetter, error)
}

type K8SClusterGetterSetter interface {
	// Getters
	GetClusterName() string
	GetServiceProvider() *managedk8sservices.ManagedServiceProvider
	GetNodes() []*string
	GetAllNamespacesGetterSetters() []NamespaceGetterSetter
	GetNamespaceGetterSetter(namespace string) (NamespaceGetterSetter, error)

	// Setters
	SetClusterName(clusterName string) error
	SetServiceProvider(ms *managedk8sservices.ManagedServiceProvider) error
	SetNodes(nodes []*string) error
	SetNamespaces(namespace *Namespace) error
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

func (kc *K8SCluster) GetServiceProvider() *managedk8sservices.ManagedServiceProvider {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.serviceProvider
}

func (kc *K8SCluster) GetNodes() []*string {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.nodes
}

func (kc *K8SCluster) GetAllNamespacesGetters() []NamespaceGetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var namespaces []NamespaceGetter
	for _, ns := range kc.namespaces {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

func (kc *K8SCluster) GetNamespaceGetter(namespace string) (NamespaceGetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	ns, ok := kc.namespaces[namespace]
	if !ok {
		return nil, fmt.Errorf("get namespace getter: %w", ErrNamespaceNotFound)
	}

	return ns, nil
}

func (kc *K8SCluster) GetAllNamespacesGetterSetters() []NamespaceGetterSetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var namespaces []NamespaceGetterSetter
	for _, ns := range kc.namespaces {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

func (kc *K8SCluster) GetNamespaceGetterSetter(namespace string) (NamespaceGetterSetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	ns, ok := kc.namespaces[namespace]
	if !ok {
		return nil, fmt.Errorf("get namespace getter setter: %w", ErrNamespaceNotFound)
	}

	return ns, nil
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

func (kc *K8SCluster) SetServiceProvider(ms *managedk8sservices.ManagedServiceProvider) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	if kc.serviceProvider != nil {
		return fmt.Errorf("set service provider: %w", ErrServiceProviderAlreadySet)
	}

	if err := managedk8sservices.ValidateManagedServices(ms); err != nil {
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

func (kc *K8SCluster) SetNamespaces(namespace *Namespace) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	kc.namespaces[namespace.namespaceName] = namespace

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------Populate K8S Cluster Functions------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *K8SCluster) PopulateK8SCluster() error {
	nodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("populate k8s cluster: %w", err)
	}

	var allNodes []*string
	for _, node := range nodes {
		allNodes = append(allNodes, &node)
	}

	if err := kc.SetNodes(allNodes); err != nil {
		return fmt.Errorf("populate k8s cluster: %w", err)
	}

	return nil
}
