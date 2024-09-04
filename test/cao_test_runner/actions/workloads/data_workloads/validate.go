package dataworkloads

import (
	"errors"
	"fmt"
	"strconv"
)

const (
	defaultDocSize = int64(1024)
)

var (
	ErrFilterPodCount              = errors.New("filter pods count shall be 1")
	ErrBucketCountNotProvided      = errors.New("bucket count is not provided")
	ErrBucketNameEmpty             = errors.New("bucket name is empty")
	ErrScopeEmpty                  = errors.New("scope name is empty")
	ErrCollectionEmpty             = errors.New("collection name is empty")
	ErrOpsRateNotProvided          = errors.New("ops rate not provided")
	ErrCRUDValuesNotProvided       = errors.New("crud values not provided")
	ErrReadsDeletesMoreThanCreates = errors.New("reads or deletes are more than creates")
)

func ValidateDataWorkloadConfig(config *DataWorkloadConfig) error {
	err := validateBucketsConfig(config)
	if err != nil {
		return fmt.Errorf("validate data workload config: %w", err)
	}

	if config.CBPodFilter.Count > 1 {
		return fmt.Errorf("new data workload config: %w", ErrFilterPodCount)
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

// validateBucketsConfig validates the DataWorkloadConfig.Buckets (BucketConfig).
/*
 * If DataWorkloadConfig.BucketCount > 0 and DataWorkloadConfig.Buckets is nil then default BucketConfig is loaded.
 * If BucketConfig.Bucket != "" and its ScopeConfig and Collections slice is nil then default Scope and Collection is used.
 */
func validateBucketsConfig(config *DataWorkloadConfig) error {
	if config.BucketCount <= 0 {
		return fmt.Errorf("validate buckets config: %w", ErrBucketCountNotProvided)
	}

	if config.BucketCount > 0 && config.Buckets == nil {
		config.Buckets = populateBucketConfig(config.BucketCount)
		return nil
	}

	for _, bucketConfig := range config.Buckets {
		if bucketConfig.Bucket == "" {
			return fmt.Errorf("validate buckets config: %w", ErrBucketNameEmpty)
		}

		if bucketConfig.Scopes == nil {
			bucketConfig.Scopes = []ScopeConfig{{
				Scope:       "_default",
				Collections: []string{"_default"},
			}}
		}

		for _, scopeConfig := range bucketConfig.Scopes {
			if scopeConfig.Scope == "" {
				return fmt.Errorf("validate buckets config: %w", ErrScopeEmpty)
			}

			if scopeConfig.Collections == nil {
				scopeConfig.Collections = []string{"_default"}
			}

			for _, coll := range scopeConfig.Collections {
				if coll == "" {
					return fmt.Errorf("validate buckets config: %w", ErrCollectionEmpty)
				}
			}
		}
	}

	return nil
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

	return nil
}

// validateSiriusConfig validates the config for Sirius data workload.
func validateSiriusConfig(config *DataWorkloadConfig) error {
	// TODO
	panic("validateSiriusConfig to be implemented")
}

func populateBucketConfig(bucketCount int) []BucketConfig {
	var bucketConfigs []BucketConfig

	// Bucket naming goes bucket0, bucket1, ...
	bucketPrefix := "bucket"

	for i := 0; i < bucketCount; i++ {
		bucketConfigs = append(bucketConfigs, BucketConfig{
			Bucket: bucketPrefix + strconv.Itoa(i),
			Scopes: []ScopeConfig{{
				Scope:       "_default",
				Collections: []string{"_default"},
			}},
		})
	}

	return bucketConfigs
}
