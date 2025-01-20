package assets

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrServiceProviderAlreadySet = errors.New("service provider already set, cannot be changed")
	ErrPlatformAlreadySet        = errors.New("platform already set, cannot be changed")
	ErrClusterNotFound           = errors.New("cluster not found")
)

type K8SClusters struct {
	k8sClusters  map[string]*K8SCluster
	kindClusters map[string]*KindClusterDetail

	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
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

type K8SClustersGetter interface {
	GetAllClustersGetter() []K8SClusterGetter
	GetK8SClusterGetter(clusterName string) (K8SClusterGetter, error)
	GetAllKindClustersDetailsGetter() []KindClusterDetailGetter
	GetKindClusterDetailGetter(clusterName string) (KindClusterDetailGetter, error)
}

type K8SClustersGetterSetter interface {
	// Getters
	GetAllK8SClustersGetterSetter() []K8SClusterGetterSetter
	GetK8SClusterGetterSetter(clusterName string) (K8SClusterGetterSetter, error)
	GetAllKindClustersDetailsGetterSetter() []KindClusterDetailGetterSetter
	GetKindClusterDetailGetterSetter(clusterName string) (KindClusterDetailGetterSetter, error)

	// Setters
	SetK8SCluster(clusterName string, k8sCluster *K8SCluster) error
	SetKindClusterDetail(clusterName string, kindClusterDetail *KindClusterDetail) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------K8SClusters Getters----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ks *K8SClusters) GetAllClustersGetter() []K8SClusterGetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []K8SClusterGetter
	for _, cluster := range ks.k8sClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetK8SClusterGetter(clusterName string) (K8SClusterGetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.k8sClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get k8s cluster getter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

func (ks *K8SClusters) GetAllKindClustersDetailsGetter() []KindClusterDetailGetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []KindClusterDetailGetter
	for _, cluster := range ks.kindClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetKindClusterDetailGetter(clusterName string) (KindClusterDetailGetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.kindClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get kind cluster detail getter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

func (ks *K8SClusters) GetAllK8SClustersGetterSetter() []K8SClusterGetterSetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []K8SClusterGetterSetter
	for _, cluster := range ks.k8sClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetK8SClusterGetterSetter(clusterName string) (K8SClusterGetterSetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.k8sClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get k8s cluster getter setter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

func (ks *K8SClusters) GetAllKindClustersDetailsGetterSetter() []KindClusterDetailGetterSetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []KindClusterDetailGetterSetter
	for _, cluster := range ks.kindClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetKindClusterDetailGetterSetter(clusterName string) (KindClusterDetailGetterSetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.kindClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get kind cluster detail getter setter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------K8SClusters Setters----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ks *K8SClusters) SetK8SCluster(clusterName string, k8sCluster *K8SCluster) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.k8sClusters[clusterName] = k8sCluster

	return nil
}

func (ks *K8SClusters) SetKindClusterDetail(clusterName string, kindClusterDetail *KindClusterDetail) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.kindClusters[clusterName] = kindClusterDetail

	return nil
}
