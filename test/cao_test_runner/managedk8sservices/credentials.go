package managedk8sservices

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

var (
	ErrClusterNameNotProvided          = errors.New("cluster name not provided")
	ErrEKSAccessKeyEmpty               = errors.New("eks access key empty")
	ErrEKSSecretKeyEmpty               = errors.New("eks secret key empty")
	ErrEKSRegionEmpty                  = errors.New("eks region empty")
	ErrAKSSubscriptionIDEmpty          = errors.New("aks subscription ID empty")
	ErrAKSTenantIDEmpty                = errors.New("aks tenant ID empty")
	ErrAKSServicePrincipalIDEmpty      = errors.New("aks service principal ID empty")
	ErrAKSSServicePrincipalSecretEmpty = errors.New("aks service principal secret empty")
	ErrAKSRegionEmpty                  = errors.New("aks region empty")
	ErrNotImplemented                  = errors.New("not implemnted error")
)

var (
	eksSvcCred *EKSCredentials
	aksSvcCred *AKSCredentials
	eksOnce    sync.Once
	aksOnce    sync.Once
)

// ManagedServiceCredentials stores all the credentials required to connect to different Managed K8S Services.
// All the Session structs (e.g. EKSSession) shall include this struct.
type ManagedServiceCredentials struct {
	ClusterName    string
	EKSCredentials *EKSCredentials
	AKSCredentials *AKSCredentials
}

type EKSCredentials struct {
	eksRegion    string `env:"EKS_REGION"`
	eksAccessKey string `env:"EKS_ACCESS_KEY"`
	eksSecretKey string `env:"EKS_SECRET_KEY"`
}

type AKSCredentials struct {
	aksRegion                 string `env:"AKS_REGION"`
	aksSubscriptionID         string `env:"AKS_SUBSCRIPTION_ID"`
	aksServicePrincipalID     string `env:"AKS_SERVICE_PRINCIPAL_ID"`
	aksServicePrincipalSecret string `env:"AKS_SERVICE_PRINCIPAL_SECRET_ID"`
	aksTenantID               string `env:"AKS_TENANT_ID"`
}

// GetManagedServiceCredentials sets up the ManagedServiceCredentials for all the ManagedServiceProviders.
// It gets the values from environment variables.
func NewManagedServiceCredentials(ms []ManagedServiceProvider, clusterName string) (*ManagedServiceCredentials, error) {
	svc := &ManagedServiceCredentials{
		ClusterName:    clusterName,
		EKSCredentials: GetEKSCredentials("", "", ""),
		AKSCredentials: GetAKSCredentials("", "", "", "", ""),
	}
	if ok, err := ValidateManagedServiceCredentials(svc, ms); !ok || err != nil {
		return nil, err
	}

	return svc, nil
}

// ======================================================
// =============== Validation of Credentials  ===========
// ======================================================

func ValidateManagedServiceCredentials(svcCred *ManagedServiceCredentials, providers []ManagedServiceProvider) (bool, error) {
	if svcCred.ClusterName == "" {
		return false, fmt.Errorf("cluster name not provided: %w", ErrClusterNameNotProvided)
	}

	for _, ms := range providers {
		switch ms {
		case EKSManagedService:
			if ok, err := ValidateEKSCredentials(svcCred.EKSCredentials); !ok || err != nil {
				return ok, err
			}
		case AKSManagedService:
			if ok, err := ValidateAKSCredentials(svcCred.AKSCredentials); !ok || err != nil {
				return ok, err
			}
		case GKEManagedService:
			return false, ErrNotImplemented
		case KindManagedService:
			return false, ErrNotImplemented
		}
	}

	return true, nil
}

func ValidateEKSCredentials(eksSvcCred *EKSCredentials) (bool, error) {
	if eksSvcCred.eksAccessKey == "" {
		return false, fmt.Errorf("eks access key is missing: %w", ErrEKSAccessKeyEmpty)
	}

	if eksSvcCred.eksRegion == "" {
		return false, fmt.Errorf("eks region key is missing: %w", ErrEKSRegionEmpty)
	}

	if eksSvcCred.eksSecretKey == "" {
		return false, fmt.Errorf("eks secret key is missing: %w", ErrEKSSecretKeyEmpty)
	}

	return true, nil
}

func ValidateAKSCredentials(aksSvcCred *AKSCredentials) (bool, error) {
	if aksSvcCred.aksRegion == "" {
		return false, fmt.Errorf("aks region key is missing: %w", ErrAKSRegionEmpty)
	}

	if aksSvcCred.aksServicePrincipalID == "" {
		return false, fmt.Errorf("aks service principal id key is missing: %w", ErrAKSServicePrincipalIDEmpty)
	}

	if aksSvcCred.aksServicePrincipalSecret == "" {
		return false, fmt.Errorf("aks service principal secret id key is missing: %w", ErrAKSSServicePrincipalSecretEmpty)
	}

	if aksSvcCred.aksTenantID == "" {
		return false, fmt.Errorf("aks tenant id key is missing: %w", ErrAKSTenantIDEmpty)
	}

	if aksSvcCred.aksSubscriptionID == "" {
		return false, fmt.Errorf("aks subscription id key is missing: %w", ErrAKSSubscriptionIDEmpty)
	}

	return true, nil
}

// ======================================================
// =============== PlatformCredential Creates ===========
// ======================================================

// GetEKSCredentials sets the ManagedServiceCredentials with the provided credentials. If not provided then it gets
// EKS credentials from the env variables.
/*
 * Recommended way to set the credentials is by calling the function NewManagedServiceCredentials (uses env variables).
 * Use case of this function is if it is required to set ManagedServiceCredentials by directly providing the credentials.
 */
func GetEKSCredentials(accessKey, secretKey, region string) *EKSCredentials {
	eksOnce.Do(func() {
		eksSvcCred = &EKSCredentials{
			eksRegion:    region,
			eksAccessKey: accessKey,
			eksSecretKey: secretKey,
		}

		if accessKey == "" {
			if envValue, ok := os.LookupEnv(eksAccessKeyEnv); ok {
				eksSvcCred.eksAccessKey = envValue
			}
		}

		if secretKey == "" {
			if envValue, ok := os.LookupEnv(eksSecretKeyEnv); ok {
				eksSvcCred.eksSecretKey = envValue
			}
		}

		if region == "" {
			if envValue, ok := os.LookupEnv(eksRegionEnv); ok {
				eksSvcCred.eksRegion = envValue
			}
		}
	})

	return eksSvcCred
}

// GetAKSCredentials sets the ManagedServiceCredentials with the provided credentials. If not provided then it gets
// AKS credentials from the env variables.
/*
 * Recommended way to set the credentials is by calling the function NewManagedServiceCredentials (uses env variables).
 * Use case of this function is if it is required to set ManagedServiceCredentials by directly providing the credentials.
 */
func GetAKSCredentials(region, subscriptionID, servicePrincipalID, servicePrincipaSecret, tenantID string) *AKSCredentials {
	aksOnce.Do(func() {
		aksSvcCred = &AKSCredentials{
			aksRegion:                 region,
			aksSubscriptionID:         subscriptionID,
			aksServicePrincipalID:     servicePrincipalID,
			aksServicePrincipalSecret: servicePrincipaSecret,
			aksTenantID:               tenantID,
		}

		if region == "" {
			if envValue, ok := os.LookupEnv(aksRegionEnv); ok {
				aksSvcCred.aksRegion = envValue
			}
		}

		if subscriptionID == "" {
			if envValue, ok := os.LookupEnv(aksSubscriptionIDEnv); ok {
				aksSvcCred.aksSubscriptionID = envValue
			}
		}

		if servicePrincipalID == "" {
			if envValue, ok := os.LookupEnv(aksServicePrincipalIDEnv); ok {
				aksSvcCred.aksServicePrincipalID = envValue
			}
		}

		if servicePrincipaSecret == "" {
			if envValue, ok := os.LookupEnv(aksServicePrincipalSecretEnv); ok {
				aksSvcCred.aksServicePrincipalSecret = envValue
			}
		}

		if tenantID == "" {
			if envValue, ok := os.LookupEnv(aksTenantIDEnv); ok {
				aksSvcCred.aksTenantID = envValue
			}
		}
	})

	return aksSvcCred
}
