package managedk8sservices

import (
	"errors"
)

type ManagedServiceProvider string
type K8sNodeFindStrategy string

const (
	// Supported Managed K8s Services.
	KindManagedService ManagedServiceProvider = "kind"
	EKSManagedService  ManagedServiceProvider = "eks"
	AKSManagedService  ManagedServiceProvider = "aks"
	GKEManagedService  ManagedServiceProvider = "gke"

	// Environment Variables for Credentials.
	eksAccessKeyEnv = "EKS_ACCESS_KEY"
	eksSecretKeyEnv = "EKS_SECRET_KEY"
	eksRegionEnv    = "EKS_REGION"

	// All the K8sNodeFindStrategy are listed here. Used to find the K8S node based on various different selection strategy.
	FindStrategyRandom K8sNodeFindStrategy = "random"
	FindStrategyOldest K8sNodeFindStrategy = "oldest"
	FindStrategyNewest K8sNodeFindStrategy = "newest"
	FindStrategyAll    K8sNodeFindStrategy = "all"
)

var (
	ErrManagedServiceNotFound = errors.New("managed service not found")
)

type ManagedService interface {
	SetSession(managedSvcCred *ManagedServiceCredentials) error
	Check(managedSvcCred *ManagedServiceCredentials) error
	GetInstancesByK8sNodeName(managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error)
	GetInstancesByStrategy(managedSvcCred *ManagedServiceCredentials, strategy K8sNodeFindStrategy) ([]string, error)
}

func NewManagedService(ms ManagedServiceProvider) ManagedService {
	switch ms {
	case EKSManagedService:
		eksSessStore := ConfigEKSSessionStore()
		return eksSessStore
	default:
		return nil
	}
}
