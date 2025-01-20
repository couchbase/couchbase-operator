package assets

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

var (
	ErrKindClusterNameAlreadySet = errors.New("kind cluster name already set, cannot be changed")
)

type KindClusterDetail struct {
	kindClusterName   string
	controlPlaneNodes []*string
	workerNodes       []*string

	// Assess the necessity of a lock over ReadWrites. Can be replaced by RWMutex then.
	mu sync.Mutex
}

func NewKindClusterDetail(kindClusterName string, controlPlaneNodes, workerNodes []*string) *KindClusterDetail {
	return &KindClusterDetail{
		kindClusterName:   kindClusterName,
		controlPlaneNodes: controlPlaneNodes,
		workerNodes:       workerNodes,
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

type KindClusterDetailGetter interface {
	GetKindClusterName() string
	GetAllControlPlaneNodes() []*string
	GetAllWorkerNodes() []*string
	GetAllNodes() []*string
}

type KindClusterDetailGetterSetter interface {
	// Getters
	GetKindClusterName() string
	GetAllControlPlaneNodes() []*string
	GetAllWorkerNodes() []*string
	GetAllNodes() []*string

	// Setters
	SetKindClusterName(kindClusterName string) error
	SetControlPlaneNodes(controlPlaneNodes []*string) error
	SetWorkerNodes(workerNodes []*string) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------KindClusterDetail Getters---------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *KindClusterDetail) GetKindClusterName() string {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.kindClusterName
}

func (kc *KindClusterDetail) GetAllControlPlaneNodes() []*string {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.controlPlaneNodes
}

func (kc *KindClusterDetail) GetAllWorkerNodes() []*string {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return kc.workerNodes
}

func (kc *KindClusterDetail) GetAllNodes() []*string {
	kc.mu.Lock()
	defer kc.mu.Unlock()
	return append(kc.controlPlaneNodes, kc.workerNodes...)
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------KindClusterDetail Setters---------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *KindClusterDetail) SetKindClusterName(kindClusterName string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	if kc.kindClusterName != "" {
		return fmt.Errorf("set kind cluster name: %w", ErrKindClusterNameAlreadySet)
	}

	kc.kindClusterName = kindClusterName

	return nil
}

func (kc *KindClusterDetail) SetControlPlaneNodes(controlPlaneNodes []*string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	kc.controlPlaneNodes = controlPlaneNodes

	return nil
}

func (kc *KindClusterDetail) SetWorkerNodes(workerNodes []*string) error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	kc.workerNodes = workerNodes

	return nil
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------Populate Kind Cluster Detail Functions--------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (kc *KindClusterDetail) PopulateKindClusterDetail() error {
	allNodes, err := nodes.GetNodeNames()
	if err != nil {
		return fmt.Errorf("populate kind cluster detail: %w", err)
	}

	var controlPlaneNodes []*string
	var workerNodes []*string

	for _, node := range allNodes {
		if strings.Contains(node, "control-plane") {
			controlPlaneNodes = append(controlPlaneNodes, &node)
		} else {
			workerNodes = append(workerNodes, &node)
		}
	}

	if err := kc.SetControlPlaneNodes(controlPlaneNodes); err != nil {
		return fmt.Errorf("populate kind cluster detail: %w", err)
	}

	if err := kc.SetWorkerNodes(workerNodes); err != nil {
		return fmt.Errorf("populate kind cluster detail: %w", err)
	}

	return nil
}
