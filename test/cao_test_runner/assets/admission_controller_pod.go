package assets

import (
	"fmt"
	"sync"
)

type AdmissionControllerPod struct {
	admissionControllerPodName string
	admissionControllerImage   string
	scope                      ScopeType
	namespace                  string
	replicas                   int

	mu sync.Mutex
}

func NewAdmissionControllerPod(admissionControllerPodName string) *AdmissionControllerPod {
	return &AdmissionControllerPod{
		admissionControllerPodName: admissionControllerPodName,
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

type AdmissionControllerPodGetter interface {
	GetAdmissionControllerPodName() string
	GetScope() ScopeType
	GetNamespace() string
	GetAdmissionControllerImage() string
	GetReplicas() int
}

type AdmissionControllerPodGetterSetter interface {
	// Getters
	GetAdmissionControllerPodName() string
	GetScope() ScopeType
	GetNamespace() string
	GetAdmissionControllerImage() string
	GetReplicas() int

	// Setters
	SetAdmissionControllerPodName(admissionControllerPodName string) error
	SetScope(scope ScopeType) error
	SetNamespace(namespace string) error
	SetAdmissionControllerImage(admissionControllerImage string) error
	SetReplicas(replicas int) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------AdmissionControllerPod Getters----------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *AdmissionControllerPod) GetAdmissionControllerPodName() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.admissionControllerPodName
}

func (ac *AdmissionControllerPod) GetScope() ScopeType {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.scope
}

func (ac *AdmissionControllerPod) GetNamespace() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.namespace
}

func (ac *AdmissionControllerPod) GetAdmissionControllerImage() string {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.admissionControllerImage
}

func (ac *AdmissionControllerPod) GetReplicas() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.replicas
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------AdmissionControllerPod Setters----------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ac *AdmissionControllerPod) SetAdmissionControllerPodName(admissionControllerPodName string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.admissionControllerPodName = admissionControllerPodName
	return nil
}

func (ac *AdmissionControllerPod) SetScope(scope ScopeType) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if err := ValidateScopeType(scope); err != nil {
		return fmt.Errorf("set scope: %w", err)
	}

	ac.scope = scope
	return nil
}

func (ac *AdmissionControllerPod) SetNamespace(namespace string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	// TODO : Make sure that the namespace set here is in K8SCluster.Namespaces as well
	ac.namespace = namespace

	return nil
}

func (ac *AdmissionControllerPod) SetAdmissionControllerImage(admissionControllerImage string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.admissionControllerImage = admissionControllerImage
	return nil
}

func (ac *AdmissionControllerPod) SetReplicas(replicas int) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.replicas = replicas
	return nil
}
