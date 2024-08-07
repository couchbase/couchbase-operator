package managedk8sservices

import (
	"errors"
	"fmt"
	"os"
)

var (
	ErrClusterNameNotProvided = errors.New("cluster name not provided")
	ErrEnvVariableMissing     = errors.New("env variable missing")
)

// ManagedServiceCredentials stores all the credentials required to connect to different Managed K8S Services.
// All the Session structs (e.g. EKSSession) shall include this struct.
type ManagedServiceCredentials struct {
	ClusterName string `env:"clusterName"`
	EKS         EKSCredentials
}

type EKSCredentials struct {
	EKSRegion    string `env:"EKS_REGION"`
	EKSAccessKey string `env:"EKS_ACCESS_KEY"`
	EKSSecretKey string `env:"EKS_SECRET_KEY"`
}

// NewManagedServiceCredentials sets up the ManagedServiceCredentials for the specified ManagedServiceProvider.
// It gets the value from environment variables.
func NewManagedServiceCredentials(ms ManagedServiceProvider, clusterName string) (*ManagedServiceCredentials, error) {
	switch ms {
	case EKSManagedService:
		return GetEKSCredentials(clusterName, "", "", "")
	default:
		return nil, fmt.Errorf("get new ManagedServiceCredentials %s: %w", ms, ErrManagedServiceNotFound)
	}
}

// GetEKSCredentials sets the ManagedServiceCredentials with the provided credentials. If not provided then it gets
// EKS credentials from the env variables.
/*
 * Recommended way to set the credentials is by calling the function NewManagedServiceCredentials (uses env variables).
 * Use case of this function is if it is required to set ManagedServiceCredentials by directly providing the credentials.
 */
func GetEKSCredentials(clusterName, accessKey, secretKey, region string) (*ManagedServiceCredentials, error) {
	eksSvcCred := ManagedServiceCredentials{
		ClusterName: clusterName,
		EKS: EKSCredentials{
			EKSRegion:    region,
			EKSAccessKey: accessKey,
			EKSSecretKey: secretKey,
		},
	}

	if clusterName == "" {
		return nil, fmt.Errorf("get eks credentials: %w", ErrClusterNameNotProvided)
	}

	if accessKey == "" {
		if envValue, ok := os.LookupEnv(eksAccessKeyEnv); ok {
			eksSvcCred.EKS.EKSAccessKey = envValue
		}

		return nil, fmt.Errorf("get eks credentials: lookup env variable %s: %w", eksAccessKeyEnv, ErrEnvVariableMissing)
	}

	if secretKey == "" {
		envValue, ok := os.LookupEnv(eksSecretKeyEnv)
		if ok {
			eksSvcCred.EKS.EKSSecretKey = envValue
		}

		return nil, fmt.Errorf("get eks credentials: lookup env variable %s: %w", eksSecretKeyEnv, ErrEnvVariableMissing)
	}

	if region == "" {
		if envValue, ok := os.LookupEnv(eksRegionEnv); ok {
			eksSvcCred.EKS.EKSRegion = envValue
		}

		return nil, fmt.Errorf("get eks credentials: lookup env variable %s: %w", eksRegionEnv, ErrEnvVariableMissing)
	}

	return &eksSvcCred, nil
}
