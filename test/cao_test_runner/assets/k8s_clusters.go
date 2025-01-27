package assets

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/yaml"
	"github.com/sirupsen/logrus"
)

var (
	ErrServiceProviderAlreadySet = errors.New("service provider already set, cannot be changed")
	ErrPlatformAlreadySet        = errors.New("platform already set, cannot be changed")
	ErrClusterNotFound           = errors.New("cluster not found")
	ErrNotImplemented            = errors.New("not implemented")
)

type K8SClusters struct {
	k8sClusters  map[string]*K8SCluster
	kindClusters map[string]*KindClusterDetail
	eksClusters  map[string]*EKSClusterDetail
	aksClusters  map[string]*AKSClusterDetail
	gkeClusters  map[string]*GKEClusterDetail

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
	GetAllEKSClustersDetailsGetter() []EKSClusterDetailGetter
	GetEKSClusterDetailGetter(clusterName string) (EKSClusterDetailGetter, error)
	GetAllAKSClustersDetailsGetter() []AKSClusterDetailGetter
	GetAKSClusterDetailGetter(clusterName string) (AKSClusterDetailGetter, error)
	GetAllGKEClustersDetailsGetter() []GKEClusterDetailGetter
	GetGKEClusterDetailGetter(clusterName string) (GKEClusterDetailGetter, error)
}

type K8SClustersGetterSetter interface {
	// Getters
	GetAllK8SClustersGetterSetter() []K8SClusterGetterSetter
	GetK8SClusterGetterSetter(clusterName string) (K8SClusterGetterSetter, error)
	GetAllKindClustersDetailsGetterSetter() []KindClusterDetailGetterSetter
	GetKindClusterDetailGetterSetter(clusterName string) (KindClusterDetailGetterSetter, error)
	GetAllEKSClustersDetailsGetterSetter() []EKSClusterDetailGetterSetter
	GetEKSClusterDetailGetterSetter(clusterName string) (EKSClusterDetailGetterSetter, error)
	GetAllAKSClustersDetailsGetterSetter() []AKSClusterDetailGetterSetter
	GetAKSClusterDetailGetterSetter(clusterName string) (AKSClusterDetailGetterSetter, error)
	GetAllGKEClustersDetailsGetterSetter() []GKEClusterDetailGetterSetter
	GetGKEClusterDetailGetterSetter(clusterName string) (GKEClusterDetailGetterSetter, error)

	// Setters
	SetK8SCluster(k8sCluster *K8SCluster) error
	SetKindClusterDetail(kindClusterDetail *KindClusterDetail) error
	SetEKSClusterDetail(eksClusterDetail *EKSClusterDetail) error
	SetAKSClusterDetail(aksClusterDetail *AKSClusterDetail) error
	SetGKEClusterDetail(gkeClusterDetail *GKEClusterDetail) error

	// Deletes
	DeleteK8SCluster(clusterName string) error
	DeleteKindClusterDetail(clusterName string) error
	DeleteEKSClusterDetail(clusterName string) error
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

func (ks *K8SClusters) GetAllEKSClustersDetailsGetter() []EKSClusterDetailGetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []EKSClusterDetailGetter
	for _, cluster := range ks.eksClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetEKSClusterDetailGetter(clusterName string) (EKSClusterDetailGetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.eksClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get eks cluster detail getter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

func (ks *K8SClusters) GetAllEKSClustersDetailsGetterSetter() []EKSClusterDetailGetterSetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []EKSClusterDetailGetterSetter
	for _, cluster := range ks.eksClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetEKSClusterDetailGetterSetter(clusterName string) (EKSClusterDetailGetterSetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.eksClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get eks cluster detail getter setter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

func (ks *K8SClusters) GetAllAKSClustersDetailsGetter() []AKSClusterDetailGetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []AKSClusterDetailGetter
	for _, cluster := range ks.aksClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetAKSClusterDetailGetter(clusterName string) (AKSClusterDetailGetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.aksClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get aks cluster detail getter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

func (ks *K8SClusters) GetAllAKSClustersDetailsGetterSetter() []AKSClusterDetailGetterSetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []AKSClusterDetailGetterSetter
	for _, cluster := range ks.aksClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetAKSClusterDetailGetterSetter(clusterName string) (AKSClusterDetailGetterSetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.aksClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get aks cluster detail getter setter: %w", ErrClusterNotFound)
	}

	return cluster, nil
}

func (ks *K8SClusters) GetAllGKEClustersDetailsGetter() []GKEClusterDetailGetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []GKEClusterDetailGetter
	for _, cluster := range ks.gkeClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetGKEClusterDetailGetter(clusterName string) (GKEClusterDetailGetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.gkeClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get gke cluster detail getter: %w", ErrClusterNotFound)
	}

	return cluster, nil

}

func (ks *K8SClusters) GetAllGKEClustersDetailsGetterSetter() []GKEClusterDetailGetterSetter {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var clusters []GKEClusterDetailGetterSetter
	for _, cluster := range ks.gkeClusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ks *K8SClusters) GetGKEClusterDetailGetterSetter(clusterName string) (GKEClusterDetailGetterSetter, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	cluster, ok := ks.gkeClusters[clusterName]
	if !ok {
		return nil, fmt.Errorf("get gke cluster detail getter setter: %w", ErrClusterNotFound)
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

func (ks *K8SClusters) SetK8SCluster(k8sCluster *K8SCluster) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.k8sClusters[k8sCluster.clusterName] = k8sCluster

	return nil
}

func (ks *K8SClusters) SetKindClusterDetail(kindClusterDetail *KindClusterDetail) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.kindClusters[kindClusterDetail.kindClusterName] = kindClusterDetail

	return nil
}

func (ks *K8SClusters) SetEKSClusterDetail(eksClusterDetail *EKSClusterDetail) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.eksClusters[eksClusterDetail.eksClusterName] = eksClusterDetail

	return nil
}

func (ks *K8SClusters) SetAKSClusterDetail(aksClusterDetail *AKSClusterDetail) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.aksClusters[aksClusterDetail.aksClusterName] = aksClusterDetail

	return nil
}

func (ks *K8SClusters) SetGKEClusterDetail(gkeClusterDetail *GKEClusterDetail) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.gkeClusters[gkeClusterDetail.gkeClusterName] = gkeClusterDetail

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------K8SClusters Deletes----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ks *K8SClusters) DeleteK8SCluster(clusterName string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if _, exists := ks.k8sClusters[clusterName]; exists {
		delete(ks.k8sClusters, clusterName)
	} else {
		return fmt.Errorf("delete k8s cluster: %w", ErrClusterNotFound)
	}

	return nil
}

func (ks *K8SClusters) DeleteKindClusterDetail(clusterName string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if _, exists := ks.kindClusters[clusterName]; exists {
		delete(ks.kindClusters, clusterName)
	} else {
		return fmt.Errorf("delete kind cluster detail: %w", ErrClusterNotFound)
	}

	return nil
}

func (ks *K8SClusters) DeleteEKSClusterDetail(clusterName string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if _, exists := ks.eksClusters[clusterName]; exists {
		delete(ks.eksClusters, clusterName)
	} else {
		return fmt.Errorf("delete eks cluster detail: %w", ErrClusterNotFound)
	}

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------Populate K8S Clusters Functions---------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ks *K8SClusters) PopulateK8SClusters(kubeconfigPath *fileutils.File) error {
	ks.k8sClusters = make(map[string]*K8SCluster)
	ks.kindClusters = make(map[string]*KindClusterDetail)
	ks.eksClusters = make(map[string]*EKSClusterDetail)
	ks.aksClusters = make(map[string]*AKSClusterDetail)
	ks.gkeClusters = make(map[string]*GKEClusterDetail)

	if err := ks.PopulateAllClusters(kubeconfigPath); err != nil {
		return fmt.Errorf("populate k8s clusters: %w", err)
	}

	return nil
}

func DetectServiceProvider(kubeconfigPath *fileutils.File, clusterName string) (*ManagedServiceProvider, error) {
	kubeconfig, err := yaml.UnmarshalYAMLFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("detect service provider: %w", err)
	}

	clustersInterface, exists := kubeconfig["clusters"]
	if !exists {
		return nil, fmt.Errorf("detect service provider: %w", ErrClusterNotFound)
	}

	clusters, _ := clustersInterface.([]interface{})

	for _, clusterInterface := range clusters {
		clusterMap, _ := clusterInterface.(map[string]interface{})
		name, _ := clusterMap["name"].(string)

		if name != clusterName {
			continue
		}

		clusterDetails, _ := clusterMap["cluster"].(map[string]interface{})

		serverURL, _ := clusterDetails["server"].(string)

		if strings.Contains(serverURL, "eks.amazonaws.com") {
			return NewManagedServiceProvider(Kubernetes, Cloud, AWS), nil
		} else if strings.Contains(serverURL, "azmk8s.io") {
			return NewManagedServiceProvider(Kubernetes, Cloud, Azure), nil
		} else if strings.Contains(serverURL, "googleapis.com") || strings.Contains(serverURL, "googleusercontent.com") {
			return NewManagedServiceProvider(Kubernetes, Cloud, GCP), nil
		} else if strings.Contains(serverURL, "127.0.0.1") || strings.Contains(serverURL, "localhost") {
			return NewManagedServiceProvider(Kubernetes, Kind, ""), nil
		}

	}

	return nil, fmt.Errorf("detect service provider: %w", ErrClusterNotFound)
}

func (ks *K8SClusters) PopulateAllClusters(kubeconfigPath *fileutils.File) error {
	out, _, err := kubectl.GetClusters().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("populate all clusters: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	// First field is "NAME" and removing last ""
	allClusters = allClusters[1 : len(allClusters)-1]

	for _, cluster := range allClusters {
		serviceProvider, err := DetectServiceProvider(kubeconfigPath, cluster)
		if err != nil {
			// The cluster cannot be properly detected. Won't be populated to TestAssets
			// TODO : Populate whatever can be found from such clusters as well
			logrus.Errorf("Service Provider of cluster %s cannot be detected. Not populated in TestAssets", cluster)
		} else {
			switch serviceProvider.GetPlatform() {
			case Kubernetes:
				switch serviceProvider.GetEnvironment() {
				case Kind:
					if err := ks.PopulateKindCluster(cluster); err != nil {
						return fmt.Errorf("populate all clusters: %w", err)
					}
				case Cloud:
					switch serviceProvider.GetProvider() {
					case AWS:
						return ErrNotImplemented
					case Azure:
						return ErrNotImplemented
					case GCP:
						return ErrNotImplemented
					default:
						return ErrNotImplemented
					}
				default:
					return ErrNotImplemented
				}
			case Openshift:
				return ErrNotImplemented
			default:
				return ErrNotImplemented
			}
		}
	}

	return nil
}

func (ks *K8SClusters) PopulateKindCluster(clusterName string) error {
	if strings.Contains(clusterName, "kind-") {
		clusterName = strings.Split(clusterName, "kind-")[1]
	}

	k8sCluster := &K8SCluster{
		clusterName:     clusterName,
		serviceProvider: NewManagedServiceProvider(Kubernetes, Kind, ""),
	}

	kindClusterDetail := &KindClusterDetail{
		kindClusterName: clusterName,
	}

	ks.k8sClusters[clusterName] = k8sCluster

	ks.kindClusters[clusterName] = kindClusterDetail

	if err := k8sCluster.PopulateK8SCluster(); err != nil {
		return fmt.Errorf("populate kind cluster: %w", err)
	}

	if err := kindClusterDetail.PopulateKindClusterDetail(); err != nil {
		return fmt.Errorf("populate kind cluster: %w", err)
	}

	return nil
}
