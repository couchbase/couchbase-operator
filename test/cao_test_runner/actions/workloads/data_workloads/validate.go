package dataworkloads

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
)

const (
	defaultDocSize = int64(1024)
)

var (
	ErrBucketCountNotProvided         = errors.New("bucket count is not provided")
	ErrOpsRateNotProvided             = errors.New("ops rate not provided")
	ErrCRUDValuesNotProvided          = errors.New("crud values not provided")
	ErrReadsDeletesMoreThanCreates    = errors.New("reads or deletes are more than creates")
	ErrCheckJobCompletionNotSupported = errors.New("check job completion is not supported")
)

// ValidateDataWorkloadName validates the DataWorkloadName for the supported Data Workloads.
func ValidateDataWorkloadName(name DataWorkloadName) bool {
	switch name {
	case GideonDataWorkload, SiriusDataWorkload, PillowFightDataWorkload:
		return true
	default:
		return false
	}
}

func ValidateDataWorkloadConfig(config *DataWorkloadConfig) error {
	if config.BucketCount <= 0 {
		return fmt.Errorf("validate data workload config: %w", ErrBucketCountNotProvided)
	}

	// If DataWorkloadConfig.Buckets is not provided then we populate it with default values: bucket0._default._default
	if config.BucketCount > 0 && config.Buckets == nil {
		config.Buckets = workloads.PopulateBucketConfig(config.BucketCount)
	}

	err := workloads.ValidateBucketsConfig(config.Buckets)
	if err != nil {
		return fmt.Errorf("validate data workload config: %w", err)
	}

	switch config.DataWorkloadName {
	case GideonDataWorkload:
		{
			return validateGideonConfig(config)
		}
	case SiriusDataWorkload:
		{
			return validateSiriusConfig(config)
		}
	default:
		return fmt.Errorf("validate data workload config: %w", ErrInvalidDataWorkloadName)
	}
}

// validateGideonConfig validates the config for Gideon data workload.
func validateGideonConfig(config *DataWorkloadConfig) error {
	if config.OpsRate <= 0 {
		return fmt.Errorf("validate gideon config: %w", ErrOpsRateNotProvided)
	}

	if config.DocSize <= 0 {
		config.DocSize = defaultDocSize
	}

	if config.Creates == 0 && config.Reads == 0 && config.Updates == 0 && config.Deletes == 0 {
		return fmt.Errorf("validate gideon config: %w", ErrCRUDValuesNotProvided)
	}

	if config.Deletes > config.Creates || config.Reads > config.Creates {
		return fmt.Errorf("validate gideon config: %w", ErrReadsDeletesMoreThanCreates)
	}

	if config.CheckJobCompletion {
		return fmt.Errorf("validate gideon config: %w", ErrCheckJobCompletionNotSupported)
	}

	return nil
}

// validateSiriusConfig validates the config for Sirius data workload.
func validateSiriusConfig(config *DataWorkloadConfig) error {
	// TODO
	panic("validateSiriusConfig to be implemented")
}
