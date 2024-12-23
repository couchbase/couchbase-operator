package indexworkloads

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
)

var (
	ErrBucketCountNotProvided = errors.New("bucket count is not provided")
	ErrNumIndexesNotProvided  = errors.New("number of indexes is not provided")
)

// ValidateIndexWorkloadName validates the IndexWorkloadName for the supported Index Workloads.
func ValidateIndexWorkloadName(name IndexWorkloadName) bool {
	switch name {
	case CBQIndexWorkload:
		return true
	case RestAPIIndexWorkload, QueryAppIndexWorkload:
		// TODO to be implemented
		return false
	default:
		return false
	}
}

func ValidateIndexWorkloadConfig(config *IndexWorkloadConfig) error {
	if config.BucketCount <= 0 {
		return fmt.Errorf("validate index workload config: %w", ErrBucketCountNotProvided)
	}

	if config.Buckets == nil {
		config.Buckets = workloads.PopulateBucketConfig(config.BucketCount)
	}

	err := workloads.ValidateBucketsConfig(config.Buckets)
	if err != nil {
		return fmt.Errorf("validate index workload config: %w", err)
	}

	switch config.IndexWorkloadName {
	case CBQIndexWorkload:
		{
			return validateCBQConfig(config)
		}
	default:
		return fmt.Errorf("validate index workload config: %w", ErrInvalidIndexWorkloadName)
	}
}

// validateCBQConfig validates the config for CBQ index workload.
func validateCBQConfig(config *IndexWorkloadConfig) error {
	if config.IndexConfig.NumIndexes <= 0 {
		return fmt.Errorf("validate cbq config: %w", ErrNumIndexesNotProvided)
	}

	return nil
}
