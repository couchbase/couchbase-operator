package managedk8sservices

import (
	"context"
	"errors"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
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

func NewManagedService(ms *assets.ManagedServiceProvider) ManagedService {
	switch ms.GetPlatform() {
	case assets.Kubernetes:
		switch ms.GetEnvironment() {
		case assets.Kind:
			return nil
		case assets.Cloud:
			switch ms.GetProvider() {
			case assets.AWS:
				eksSessStore := ConfigEKSSessionStore()
				return eksSessStore
			case assets.Azure:
				aksSessStore := ConfigAKSSessionStore()
				return aksSessStore
			case assets.GCP:
				gkeSessStore := ConfigGKESessionStore()
				return gkeSessStore
			default:
				return nil
			}
		default:
			return nil
		}
	case assets.Openshift:
		return nil
	default:
		return nil
	}
}
