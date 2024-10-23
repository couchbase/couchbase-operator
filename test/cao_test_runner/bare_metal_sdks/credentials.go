package baremetalsdks

import (
	"os"
	"sync"
)

var (
	awsCloudCred *AWSCredentials
	awsOnce      sync.Once
)

// CloudProviderCredentials stores all the credentials required to connect to different Cloud Providers.
// All the Session structs (e.g. AWSSession) shall include this struct.
type CloudProviderCredentials struct {
	AWSCredentials *AWSCredentials
}

type AWSCredentials struct {
	awsRegion    string `env:"AWS_REGION"`
	awsAccessKey string `env:"AWS_ACCESS_KEY"`
	awsSecretKey string `env:"AWS_SECRET_KEY"`
}

// NewCloudProviderCredentials sets up the CloudProviderCredentials for all the CloudProvider(s).
// It gets the values from environment variables.
func NewCloudProviderCredentials(cloudProviders []CloudProvider) (*CloudProviderCredentials, error) {
	svc := &CloudProviderCredentials{
		// ClusterName:      clusterName,
		AWSCredentials: GetAWSCredentials("", "", ""),
	}

	if ok, err := ValidateCloudProviderCredentials(svc, cloudProviders); !ok || err != nil {
		return nil, err
	}

	return svc, nil
}

// ======================================================
// ========== Create CloudProviderCredentials ===========
// ======================================================

// GetAWSCredentials sets the CloudProviderCredentials with the provided credentials. If not provided then it gets
// AWS credentials from the env variables.
/*
 * Recommended way to set the credentials is by calling the function NewCloudProviderCredentials (uses env variables).
 * Use case of this function is if it is required to set CloudProviderCredentials by directly providing the credentials.
 */
func GetAWSCredentials(accessKey, secretKey, region string) *AWSCredentials {
	awsOnce.Do(func() {
		awsCloudCred = &AWSCredentials{
			awsRegion:    region,
			awsAccessKey: accessKey,
			awsSecretKey: secretKey,
		}

		if accessKey == "" {
			if envValue, ok := os.LookupEnv(awsAccessKeyEnv); ok {
				awsCloudCred.awsAccessKey = envValue
			}
		}

		if secretKey == "" {
			if envValue, ok := os.LookupEnv(awsSecretKeyEnv); ok {
				awsCloudCred.awsSecretKey = envValue
			}
		}

		if region == "" {
			if envValue, ok := os.LookupEnv(awsRegionEnv); ok {
				awsCloudCred.awsRegion = envValue
			}
		}
	})

	return awsCloudCred
}

// ======================================================
// ========== CloudProviderCredentials Methods ==========
// ======================================================

func (awsCred *AWSCredentials) SetRegion(region string) {
	awsCred.awsRegion = region
}
