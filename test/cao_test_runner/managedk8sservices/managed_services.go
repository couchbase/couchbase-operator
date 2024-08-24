package managedk8sservices

import (
	"errors"
)

// ManagedServiceProvider defines the supported Managed K8s Services.
type ManagedServiceProvider string

const (
	KindManagedService ManagedServiceProvider = "kind"
	EKSManagedService  ManagedServiceProvider = "eks"
	AKSManagedService  ManagedServiceProvider = "aks"
	GKEManagedService  ManagedServiceProvider = "gke"
)

// Environment Variables for Credentials.
const (
	eksAccessKeyEnv              = "EKS_ACCESS_KEY"
	eksSecretKeyEnv              = "EKS_SECRET_KEY"
	eksRegionEnv                 = "EKS_REGION"
	aksRegionEnv                 = "AKS_REGION"
	aksSubscriptionIDEnv         = "AKS_SUBSCRIPTION_ID"
	aksServicePrincipalIDEnv     = "AKS_SERVICE_PRINCIPAL_ID"
	aksServicePrincipalSecretEnv = "AKS_SERVICE_PRINCIPAL_SECRET_ID"
	aksTenantIDEnv               = "AKS_TENANT_ID"
)

var (
	ErrManagedServiceNotFound = errors.New("managed service not found")
)

type ManagedService interface {
	// SetSession adds an K8S cluster Session to the respective ManagedServiceProvider SessionStore.
	SetSession(managedSvcCred *ManagedServiceCredentials) error

	// Check verifies if the k8s cluster in the ManagedServiceProvider is accessible or not.
	Check(managedSvcCred *ManagedServiceCredentials) error

	// GetInstancesByK8sNodeName gets the instance / VM ids for the provided k8s node names.
	GetInstancesByK8sNodeName(managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error)
}

func NewManagedService(ms ManagedServiceProvider) ManagedService {
	switch ms {
	case EKSManagedService:
		eksSessStore := ConfigEKSSessionStore()
		return eksSessStore
	case AKSManagedService:
		aksSessStore := ConfigAKSSessionStore()
		return aksSessStore
	default:
		return nil
	}
}

func ValidateManagedServices(ms ManagedServiceProvider) bool {
	switch ms {
	case EKSManagedService, AKSManagedService, GKEManagedService, KindManagedService:
		return true
	default:
		return false
	}
}
