package managedk8sservices

import (
	"context"
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
	eksAccountID                 = "EKS_ACCOUNT_ID"
	aksRegionEnv                 = "AKS_REGION"
	aksSubscriptionIDEnv         = "AKS_SUBSCRIPTION_ID"
	aksServicePrincipalIDEnv     = "AKS_SERVICE_PRINCIPAL_ID"
	aksServicePrincipalSecretEnv = "AKS_SERVICE_PRINCIPAL_SECRET_ID"
	aksTenantIDEnv               = "AKS_TENANT_ID"
	gkeRegionEnv                 = "GKE_REGION"
	gkeProjectIDEnv              = "GKE_PROJECT_ID"
	gkeAuthAccountEnv            = "GKE_AUTH_ACCOUNT"
	gkeCredentialsJSONPathEnv    = "GKE_CREDENTIALS_JSON_PATH"
)

var (
	ErrManagedServiceNotFound = errors.New("managed service not found")
)

type ManagedService interface {
	// SetSession adds an K8S cluster Session to the respective ManagedServiceProvider SessionStore.
	SetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error

	// Check verifies if the k8s cluster in the ManagedServiceProvider is accessible or not.
	Check(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error

	// GetInstancesByK8sNodeName gets the instance / VM ids for the provided k8s node names.
	GetInstancesByK8sNodeName(ctx context.Context, managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error)
}

func NewManagedService(ms ManagedServiceProvider) ManagedService {
	switch ms {
	case EKSManagedService:
		eksSessStore := ConfigEKSSessionStore()
		return eksSessStore
	case AKSManagedService:
		aksSessStore := ConfigAKSSessionStore()
		return aksSessStore
	case GKEManagedService:
		gkeSessStore := ConfigGKESessionStore()
		return gkeSessStore
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
