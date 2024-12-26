package queryworkloads

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
)

var (
	ErrBucketCountNotProvided = errors.New("bucket count is not provided")
	ErrInvalidNumQueries      = errors.New("invalid number of queries provided")
	ErrInvalidQueryDuration   = errors.New("invalid query duration provided")
	ErrInvalidScanConsistency = errors.New("invalid scan consistency provided")
)

var (
	ValidScanConsistency = map[string]struct{}{
		"NOT_BOUNDED":  {},
		"REQUEST_PLUS": {},
	}
)

// ValidateQueryWorkloadName validates the QueryWorkloadName for the supported Query Workloads.
func ValidateQueryWorkloadName(name QueryWorkloadName) bool {
	switch name {
	case QueryAppWorkload:
		return true
	default:
		return false
	}
}

func ValidateQueryWorkloadConfig(config *QueryWorkloadConfig) error {
	if config.BucketCount <= 0 {
		return fmt.Errorf("validate query workload config: %w", ErrBucketCountNotProvided)
	}

	if config.Buckets == nil {
		config.Buckets = workloads.PopulateBucketConfig(config.BucketCount)
	}

	err := workloads.ValidateBucketsConfig(config.Buckets)
	if err != nil {
		return fmt.Errorf("validate query workload config: %w", err)
	}

	err = validateQueryConfig(config)
	if err != nil {
		return fmt.Errorf("validate query workload config: %w", err)
	}

	return nil
}

func validateQueryConfig(config *QueryWorkloadConfig) error {
	switch config.QueryWorkloadName {
	case QueryAppWorkload:
		return validateForQueryApp(config)
	default:
		return fmt.Errorf("validate query config: %w", ErrInvalidQueryWorkloadName)
	}
}

func validateForQueryApp(config *QueryWorkloadConfig) error {
	queryConfig := &config.QueryConfig

	if queryConfig.NumQueries <= 0 {
		return fmt.Errorf("validate for query app: %w", ErrInvalidNumQueries)
	}

	if queryConfig.QueryDuration < 0 {
		return fmt.Errorf("validate for query app: %w", ErrInvalidQueryDuration)
	}

	queryConfig.ScanConsistency = strings.ToUpper(queryConfig.ScanConsistency)
	if _, ok := ValidScanConsistency[queryConfig.ScanConsistency]; !ok {
		return fmt.Errorf("validate for query app: %w", ErrInvalidScanConsistency)
	}

	return nil
}
