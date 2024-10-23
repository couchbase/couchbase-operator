package baremetalsdks

import (
	"context"
	"errors"
)

// CloudProvider defines the supported cloud providers for bare metal nodes / instances.
type CloudProvider string

const (
	AWS   CloudProvider = "aws"
	Azure CloudProvider = "azure"
	GCP   CloudProvider = "gcp"
)

// Environment Variables for Credentials.
const (
	awsAccessKeyEnv                = "AWS_ACCESS_KEY"
	awsSecretKeyEnv                = "AWS_SECRET_KEY"
	awsRegionEnv                   = "AWS_REGION"
	azureRegionEnv                 = "AZURE_REGION"
	azureSubscriptionIDEnv         = "AZURE_SUBSCRIPTION_ID"
	azureServicePrincipalIDEnv     = "AZURE_SERVICE_PRINCIPAL_ID"
	azureServicePrincipalSecretEnv = "AZURE_SERVICE_PRINCIPAL_SECRET_ID"
	azureTenantIDEnv               = "AZURE_TENANT_ID"
	gcpRegionEnv                   = "GCP_REGION"
	gcpProjectIDEnv                = "GCP_PROJECT_ID"
	gcpAuthAccountEnv              = "GCP_AUTH_ACCOUNT"
	gcpCredentialsJSONPathEnv      = "GCP_CREDENTIALS_JSON_PATH"
)

var (
	ErrCloudProviderNotFound = errors.New("cloud provider not found")
)

type BareMetalInterface interface {
	// SetSession adds a Bare Metal Session to the respective CloudProvider SessionStore.
	SetSession(ctx context.Context, managedSvcCred *CloudProviderCredentials) error

	// Check verifies if the CloudProvider is accessible or not.
	Check(ctx context.Context, managedSvcCred *CloudProviderCredentials) error
}

func NewCloudProvider(cloudProvider CloudProvider) BareMetalInterface {
	switch cloudProvider {
	case AWS:
		awsSessStore := ConfigAWSSessionStore()
		return awsSessStore
	// case Azure:
	// 	azureSessStore := ConfigAzureSessionStore()
	// 	return azureSessStore
	// case GCP:
	// 	gcpSessStore := ConfigGCPSessionStore()
	// 	return gcpSessStore
	default:
		return nil
	}
}
