package assets

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/crds"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

var (
	ErrClusterNameAlreadySet = errors.New("cluster name already set, cannot be changed")
	ErrClusterNameNotSet     = errors.New("cluster name not set")
	ErrServiceProviderNotSet = errors.New("service provider not set")
	ErrIllegalCAOPath        = errors.New("illegal cao path")
	ErrNamespaceNotFound     = errors.New("namespace not found")
	ErrCouchbaseCRDNotFound  = errors.New("couchbase crd not found")
	ErrOperatorPodNotFound   = errors.New("operator pod not found")
)

type K8SCluster struct {
	clusterName             string
	serviceProvider         *managedk8sservices.ManagedServiceProvider
	nodes                   []*string
	namespaces              map[string]*Namespace
	crds                    map[string]*CouchbaseCRD
	caoPath                 *fileutils.File
	operatorPods            map[string]*OperatorPod
	admissionControllerPods map[string]*AdmissionControllerPod

	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewK8SCluster(clusterName string, serviceProvider *managedk8sservices.ManagedServiceProvider) (*K8SCluster, error) {
	k8sCluster := &K8SCluster{
		clusterName:     clusterName,
		serviceProvider: serviceProvider,
	}

	if err := k8sCluster.PopulateK8SCluster(); err != nil {
		return nil, fmt.Errorf("new k8s cluster: %w", err)
	}

	return k8sCluster, nil
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
	GetAllCouchbaseCRDsGetter() []CouchbaseCRDGetter
	GetCouchbaseCRDGetter(crdName string) (CouchbaseCRDGetter, error)
	GetCAOPath() *fileutils.File
	GetAllOperatorPodsGetter() []OperatorPodGetter
	GetOperatorPodGetter(operatorPodName string) (OperatorPodGetter, error)
	GetAllAdmissionControllerPodsGetter() []AdmissionControllerPodGetter
	GetAdmissionControllerPodGetter(admissionControllerPodName string) (AdmissionControllerPodGetter, error)
}

type K8SClusterGetterSetter interface {
	// Getters
	GetClusterName() string
	GetServiceProvider() *managedk8sservices.ManagedServiceProvider
	GetNodes() []*string
	GetAllNamespacesGetterSetters() []NamespaceGetterSetter
	GetNamespaceGetterSetter(namespace string) (NamespaceGetterSetter, error)
	GetAllCouchbaseCRDsGetterSetter() []CouchbaseCRDGetterSetter
	GetCouchbaseCRDGetterSetter(crdName string) (CouchbaseCRDGetterSetter, error)
	GetCAOPath() *fileutils.File
	GetAllOperatorPodsGetterSetter() []OperatorPodGetterSetter
	GetOperatorPodGetterSetter(operatorPodName string) (OperatorPodGetterSetter, error)
	GetAllAdmissionControllerPodsGetterSetter() []AdmissionControllerPodGetterSetter
	GetAdmissionControllerPodGetterSetter(admissionControllerPodName string) (AdmissionControllerPodGetterSetter, error)

	// Setters
	SetClusterName(clusterName string) error
	SetServiceProvider(ms *managedk8sservices.ManagedServiceProvider) error
	SetNodes(nodes []*string) error
	SetNamespaces(namespace *Namespace) error
	SetCouchbaseCRD(crd *CouchbaseCRD) error
	SetCAOPath(caoPath *fileutils.File) error
	SetOperatorPod(operatorPod *OperatorPod) error
	SetAdmissionControllerPod(admissionControllerPod *AdmissionControllerPod) error

	// Deletes
	DeleteOperatorPod(operatorPodName string) error
	DeleteAdmissionControllerPod(admissionControllerPodName string) error
	DeleteCRD(crdName string) error
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

func (kc *K8SCluster) GetAllCouchbaseCRDsGetter() []CouchbaseCRDGetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var crds []CouchbaseCRDGetter
	for _, crd := range kc.crds {
		crds = append(crds, crd)
	}

	return crds
}

func (kc *K8SCluster) GetCouchbaseCRDGetter(crdName string) (CouchbaseCRDGetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	crd, ok := kc.crds[crdName]
	if !ok {
		return nil, fmt.Errorf("get crd getter: %w", ErrCouchbaseCRDNotFound)
	}

	return crd, nil
}

func (kc *K8SCluster) GetAllCouchbaseCRDsGetterSetter() []CouchbaseCRDGetterSetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var crds []CouchbaseCRDGetterSetter
	for _, crd := range kc.crds {
		crds = append(crds, crd)
	}

	return crds
}

func (kc *K8SCluster) GetCouchbaseCRDGetterSetter(crdName string) (CouchbaseCRDGetterSetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	crd, ok := kc.crds[crdName]
	if !ok {
		return nil, fmt.Errorf("get crd getter setter: %w", ErrCouchbaseCRDNotFound)
	}

	return crd, nil
}

func (ts *K8SCluster) GetCAOPath() *fileutils.File {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.caoPath
}

func (kc *K8SCluster) GetAllOperatorPodsGetter() []OperatorPodGetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var operatorPods []OperatorPodGetter
	for _, operatorPod := range kc.operatorPods {
		operatorPods = append(operatorPods, operatorPod)
	}

	return operatorPods
}

func (kc *K8SCluster) GetOperatorPodGetter(operatorPodName string) (OperatorPodGetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	operatorPod, ok := kc.operatorPods[operatorPodName]
	if !ok {
		return nil, fmt.Errorf("get operator pod getter: %w", ErrOperatorPodNotFound)
	}

	return operatorPod, nil
}

func (kc *K8SCluster) GetAllOperatorPodsGetterSetter() []OperatorPodGetterSetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var operatorPods []OperatorPodGetterSetter
	for _, operatorPod := range kc.operatorPods {
		operatorPods = append(operatorPods, operatorPod)
	}

	return operatorPods
}

func (kc *K8SCluster) GetOperatorPodGetterSetter(operatorPodName string) (OperatorPodGetterSetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	operatorPod, ok := kc.operatorPods[operatorPodName]
	if !ok {
		return nil, fmt.Errorf("get operator pod getter setter: %w", ErrOperatorPodNotFound)
	}

	return operatorPod, nil
}

func (kc *K8SCluster) GetAllAdmissionControllerPodsGetter() []AdmissionControllerPodGetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var admissionControllerPods []AdmissionControllerPodGetter
	for _, admissionControllerPod := range kc.admissionControllerPods {
		admissionControllerPods = append(admissionControllerPods, admissionControllerPod)
	}

	return admissionControllerPods
}

func (kc *K8SCluster) GetAdmissionControllerPodGetter(admissionControllerPodName string) (AdmissionControllerPodGetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	admissionControllerPod, ok := kc.admissionControllerPods[admissionControllerPodName]
	if !ok {
		return nil, fmt.Errorf("get admission controller pod getter: %w", ErrOperatorPodNotFound)
	}

	return admissionControllerPod, nil
}

func (kc *K8SCluster) GetAllAdmissionControllerPodsGetterSetter() []AdmissionControllerPodGetterSetter {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	var admissionControllerPods []AdmissionControllerPodGetterSetter
	for _, admissionControllerPod := range kc.admissionControllerPods {
		admissionControllerPods = append(admissionControllerPods, admissionControllerPod)
	}

	return admissionControllerPods
}

func (kc *K8SCluster) GetAdmissionControllerPodGetterSetter(admissionControllerPodName string) (AdmissionControllerPodGetterSetter, error) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	admissionControllerPod, ok := kc.admissionControllerPods[admissionControllerPodName]
	if !ok {
		return nil, fmt.Errorf("get admission controller pod getter setter: %w", ErrOperatorPodNotFound)
	}

	return admissionControllerPod, nil
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

func (kc *K8SCluster) SetCouchbaseCRD(crd *CouchbaseCRD) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	kc.crds[crd.crdName] = crd

	return nil
}

func (ts *K8SCluster) SetCAOPath(caoPath *fileutils.File) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !caoPath.IsFileExists() {
		if _, err := exec.LookPath(caoPath.FilePath); err != nil {
			return ErrIllegalCAOPath
		}
	}

	cao.WithBinaryPath(caoPath.FilePath)
	ts.caoPath = caoPath
	return nil
}

func (kc *K8SCluster) SetOperatorPod(operatorPod *OperatorPod) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	kc.operatorPods[operatorPod.operatorPodName] = operatorPod

	return nil
}

func (kc *K8SCluster) SetAdmissionControllerPod(admissionControllerPod *AdmissionControllerPod) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	kc.admissionControllerPods[admissionControllerPod.admissionControllerPodName] = admissionControllerPod

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------K8SCluster Deletes----------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *K8SCluster) DeleteOperatorPod(operatorPodName string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	delete(kc.operatorPods, operatorPodName)

	return nil
}

func (kc *K8SCluster) DeleteAdmissionControllerPod(admissionControllerPodName string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	delete(kc.admissionControllerPods, admissionControllerPodName)

	return nil
}

func (kc *K8SCluster) DeleteCRD(crdName string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	delete(kc.crds, crdName)

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
	if kc.GetServiceProvider() == nil {
		return fmt.Errorf("populate k8s cluster: %w", ErrServiceProviderNotSet)
	}

	if kc.GetClusterName() == "" {
		return fmt.Errorf("populate k8s cluster: %w", ErrClusterNameNotSet)
	}

	kc.mu.Lock()

	kc.namespaces = make(map[string]*Namespace)
	kc.crds = make(map[string]*CouchbaseCRD)
	kc.operatorPods = make(map[string]*OperatorPod)
	kc.admissionControllerPods = make(map[string]*AdmissionControllerPod)

	kc.mu.Unlock()

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

	out, _, err := kubectl.GetNamespaces().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("populate k8s cluster: %w", err)
	}

	allNamespaces := strings.Split(out, "\n")
	// Remove the last empty string
	allNamespaces = allNamespaces[:len(allNamespaces)-1]

	for _, namespace := range allNamespaces {
		namespace = strings.TrimPrefix(namespace, "namespace/")

		ns := &Namespace{namespaceName: namespace}

		if err := kc.SetNamespaces(ns); err != nil {
			return fmt.Errorf("populate k8s cluster: %w", err)
		}

		if err := ns.PopulateNamespace(); err != nil {
			return fmt.Errorf("populate k8s cluster: %w", err)
		}
	}

	crdsMap, err := crds.GetCRDsMap(nil)
	if err != nil && !errors.Is(err, crds.ErrNoCRDsInCluster) {
		return fmt.Errorf("populate k8s cluster: %w", err)
	}

	for crdName, crd := range crdsMap {
		// Filtering out the couchbase CRDs
		if crd.Metadata.Annotations["config.couchbase.com/version"] != nil {
			couchbaseCRD := &CouchbaseCRD{crdName: crdName, version: crd.Metadata.Annotations["config.couchbase.com/version"].(string)}
			if err := kc.SetCouchbaseCRD(couchbaseCRD); err != nil {
				return fmt.Errorf("populate k8s cluster: %w", err)
			}
		}
	}

	for _, ns := range kc.GetAllNamespacesGetters() {
		if len(ns.GetAllPods()) == 0 {
			continue
		}

		operatorPod, err := caopods.GetOperatorPod(ns.GetNamespaceName())
		if err != nil && !errors.Is(err, caopods.ErrOperatorPodDoesntExist) {
			return fmt.Errorf("populate k8s cluster: %w", err)
		}

		admissionPods, err := caopods.GetAdmissionPods(ns.GetNamespaceName())
		if err != nil && !errors.Is(err, caopods.ErrAdmissionPodDoesntExist) {
			return fmt.Errorf("populate k8s cluster: %w", err)
		}

		if operatorPod != nil {
			operator := &OperatorPod{
				operatorPodName: operatorPod.Metadata.Name,
				namespace:       ns.GetNamespaceName(),
				operatorImage:   operatorPod.Spec.Containers[0].Image,
				scope:           NamespaceScope,
			}

			if err := kc.SetOperatorPod(operator); err != nil {
				return fmt.Errorf("populate k8s cluster: %w", err)
			}
		}

		for _, admissionPod := range admissionPods {
			admission := AdmissionControllerPod{
				admissionControllerPodName: admissionPod.Metadata.Name,
				namespace:                  ns.GetNamespaceName(),
				admissionControllerImage:   admissionPod.Spec.Containers[0].Image,
				scope:                      ClusterScope,
				replicas:                   len(admissionPods),
			}

			if err := kc.SetAdmissionControllerPod(&admission); err != nil {
				return fmt.Errorf("populate k8s cluster: %w", err)
			}
		}
	}

	return nil
}
