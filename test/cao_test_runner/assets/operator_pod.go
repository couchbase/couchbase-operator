package assets

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrInvalidScopeType = errors.New("invalid scope type")
)

type ImagePullPolicyType string
type ScopeType string

const (
	NamespaceScope ScopeType = "namespace"
	ClusterScope   ScopeType = "cluster"
)

type OperatorPod struct {
	operatorPodName string
	operatorImage   string
	scope           ScopeType
	namespace       string

	mu sync.Mutex
}

func NewOperatorPod(operatorPodName, operatorImage, namespace string, scope ScopeType) (*OperatorPod, error) {
	operatorPod := &OperatorPod{
		operatorPodName: operatorPodName,
	}

	if err := operatorPod.SetOperatorImage(operatorImage); err != nil {
		return nil, fmt.Errorf("new operator pod: %w", err)
	}

	if err := operatorPod.SetNamespace(namespace); err != nil {
		return nil, fmt.Errorf("new operator pod: %w", err)
	}

	if err := operatorPod.SetScope(scope); err != nil {
		return nil, fmt.Errorf("new operator pod: %w", err)
	}

	return operatorPod, nil
}

func ValidateScopeType(scope ScopeType) error {
	switch scope {
	case NamespaceScope, ClusterScope:
		return nil
	default:
		return ErrInvalidScopeType
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

type OperatorPodGetter interface {
	GetOperatorPodName() string
	GetScope() ScopeType
	GetNamespace() string
	GetOperatorImage() string
}

type OperatorPodGetterSetter interface {
	// Getters
	GetOperatorPodName() string
	GetScope() ScopeType
	GetNamespace() string
	GetOperatorImage() string

	// Setters
	SetOperatorPodName(operatorPodName string) error
	SetScope(scope ScopeType) error
	SetNamespace(namespace string) error
	SetOperatorImage(operatorImage string) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------OperatorPod Getters---------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (op *OperatorPod) GetOperatorPodName() string {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.operatorPodName
}

func (op *OperatorPod) GetScope() ScopeType {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.scope
}

func (op *OperatorPod) GetNamespace() string {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.namespace
}

func (op *OperatorPod) GetOperatorImage() string {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.operatorImage
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------OperatorPod Setters---------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (op *OperatorPod) SetOperatorPodName(operatorPodName string) error {
	op.mu.Lock()
	defer op.mu.Unlock()
	op.operatorPodName = operatorPodName
	return nil
}

func (op *OperatorPod) SetScope(scope ScopeType) error {
	op.mu.Lock()
	defer op.mu.Unlock()

	if err := ValidateScopeType(scope); err != nil {
		return fmt.Errorf("set scope: %w", err)
	}

	op.scope = scope
	return nil
}

func (op *OperatorPod) SetNamespace(namespace string) error {
	op.mu.Lock()
	defer op.mu.Unlock()

	// TODO : Make sure that the namespace set here is in K8SCluster.Namespaces as well

	op.namespace = namespace
	return nil
}

func (op *OperatorPod) SetOperatorImage(operatorImage string) error {
	op.mu.Lock()
	defer op.mu.Unlock()
	op.operatorImage = operatorImage
	return nil
}
