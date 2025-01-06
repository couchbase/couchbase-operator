package managedk8sservices

import (
	"context"
	"errors"

	caoinstallutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils/cao_install_utils"
)

// ManagedServiceProvider defines the supported Managed K8s Services.
type EnvironmentType string
type ProviderType string

const (
	Kind  EnvironmentType = "kind"
	Cloud EnvironmentType = "cloud"
	AWS   ProviderType    = "aws"
	Azure ProviderType    = "azure"
	GCP   ProviderType    = "gcp"
)

type ManagedServiceProvider struct {
	Platform    caoinstallutils.PlatformType `yaml:"platform"`
	Environment EnvironmentType              `yaml:"environment"`
	Provider    ProviderType                 `yaml:"provider"`
}

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
	ErrIllegalPlatform        = errors.New("illegal platform")
	ErrIllegalEnvironment     = errors.New("illegal environment")
	ErrIllegalConfiguration   = errors.New("illegal configuration")
	ErrIllegalProvider        = errors.New("illegal provider")
)

type ManagedService interface {
	// SetSession adds an K8S cluster Session to the respective ManagedServiceProvider SessionStore.
	SetSession(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error

	// Check verifies if the k8s cluster in the ManagedServiceProvider is accessible or not.
	Check(ctx context.Context, managedSvcCred *ManagedServiceCredentials) error

	// GetInstancesByK8sNodeName gets the instance / VM ids for the provided k8s node names.
	GetInstancesByK8sNodeName(ctx context.Context, managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error)
}

func NewManagedService(ms *ManagedServiceProvider) ManagedService {
	switch ms.Platform {
	case caoinstallutils.Kubernetes:
		switch ms.Environment {
		case Kind:
			return nil
		case Cloud:
			switch ms.Provider {
			case AWS:
				eksSessStore := ConfigEKSSessionStore()
				return eksSessStore
			case Azure:
				aksSessStore := ConfigAKSSessionStore()
				return aksSessStore
			case GCP:
				gkeSessStore := ConfigGKESessionStore()
				return gkeSessStore
			default:
				return nil
			}
		default:
			return nil
		}
	case caoinstallutils.Openshift:
		return nil
	default:
		return nil
	}
}

func ValidateManagedServices(ms *ManagedServiceProvider) error {
	switch ms.Platform {
	case caoinstallutils.Kubernetes, caoinstallutils.Openshift:
		// No-op
	default:
		return ErrIllegalPlatform
	}

	switch ms.Environment {
	case Kind:
		// No-op
	case Cloud:
		switch ms.Provider {
		case AWS, Azure, GCP:
			// No-op
		default:
			return ErrIllegalProvider
		}
	default:
		return ErrIllegalEnvironment
	}

	if ms.Environment == Kind && ms.Platform == caoinstallutils.Openshift {
		return ErrIllegalConfiguration
	}

	return nil
}
