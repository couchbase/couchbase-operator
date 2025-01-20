package assets

import (
	"sync"
)

type KindClusterDetail struct {
	controlPlaneNodes []*string
	workerNodes       []*string

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

type KindClusterDetailGetter interface {
	GetAllControlPlaneNodes() []*string
	GetAllWorkerNodes() []*string
	GetAllNodes() []*string
}

type KindClusterDetailGetterSetter interface {
	// Getters
	GetAllControlPlaneNodes() []*string
	GetAllWorkerNodes() []*string
	GetAllNodes() []*string

	// Setters
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
