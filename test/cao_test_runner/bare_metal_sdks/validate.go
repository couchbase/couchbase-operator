package baremetalsdks

import (
	"errors"
	"fmt"
)

var (
	ErrAWSAccessKeyEmpty = errors.New("aws access key empty")
	ErrAWSSecretKeyEmpty = errors.New("aws secret key empty")
	ErrAWSRegionEmpty    = errors.New("aws region empty")
)

func ValidateCloudProviders(provider CloudProvider) bool {
	switch provider {
	case AWS, Azure, GCP:
		return true
	default:
		return false
	}
}

// ======================================================
// ========= Validate CloudProviderCredentials  =========
// ======================================================

func ValidateCloudProviderCredentials(svcCred *CloudProviderCredentials, providers []CloudProvider) (bool, error) {
	for _, ms := range providers {
		switch ms {
		case AWS:
			if ok, err := ValidateAWSCredentials(svcCred.AWSCredentials); !ok || err != nil {
				return ok, err
			}
		}
	}

	return true, nil
}

func ValidateAWSCredentials(awsCred *AWSCredentials) (bool, error) {
	if awsCred.awsAccessKey == "" {
		return false, fmt.Errorf("aws access key is missing: %w", ErrAWSAccessKeyEmpty)
	}

	if awsCred.awsRegion == "" {
		return false, fmt.Errorf("aws region key is missing: %w", ErrAWSRegionEmpty)
	}

	if awsCred.awsSecretKey == "" {
		return false, fmt.Errorf("aws secret key is missing: %w", ErrAWSSecretKeyEmpty)
	}

	return true, nil
}
