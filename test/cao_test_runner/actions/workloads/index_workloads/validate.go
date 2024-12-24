package indexworkloads

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/workloads"
)

var (
	ErrBucketCountNotProvided               = errors.New("bucket count is not provided")
	ErrNumIndexesNotProvided                = errors.New("number of indexes is not provided")
	ErrNumReplicasGreaterThanNumQueryPods   = errors.New("number of replicas is greater than the number of query pods")
	ErrNumPartitionsGreaterThanNumQueryPods = errors.New("number of partitions is greater than the number of query pods")
	ErrInvalidIndexSelectionStrategy        = errors.New("invalid index selection strategy")
	ErrNumIndexesExceedsTemplateQueries     = errors.New("number of indexes exceeds the number of template queries")
	ErrNumIndicesNotEqualToNumIndexes       = errors.New("number of indices not equal to the NumIndexes provided")
	ErrNumIndicesZero                       = errors.New("number of indices is zero")
	ErrInvalidIndices                       = errors.New("invalid indices slice provided")
	ErrMalformedIndicesRange                = errors.New("malformed indices range provided")
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

	err = validateIndexConfig(config)
	if err != nil {
		return fmt.Errorf("validate index workload config: %w", err)
	}

	return nil
}

func validateIndexConfig(config *IndexWorkloadConfig) error {
	idxConfig := &config.IndexConfig

	if idxConfig.NumIndexes <= 0 {
		return fmt.Errorf("validate index config: %w", ErrNumIndexesNotProvided)
	}

	if idxConfig.NumReplicas >= config.NumIndexPods {
		return fmt.Errorf("validate index config: %w", ErrNumReplicasGreaterThanNumQueryPods)
	}

	if idxConfig.NumPartitions >= config.NumIndexPods {
		return fmt.Errorf("validate index config: %w", ErrNumPartitionsGreaterThanNumQueryPods)
	}

	// Validations for GSI
	if !idxConfig.PrimaryIndex {
		err := validateTemplateName(idxConfig.TemplateName)
		if err != nil {
			return fmt.Errorf("validate index config: %w", err)
		}

		err = validateIndexSelectionStrategyAndIndices(config)
		if err != nil {
			return fmt.Errorf("validate index config: %w", err)
		}
	}

	return nil
}

// validateIndexSelectionStrategyAndIndices
/*
 * Validates the IndexSelectionStrategy.
 * Validates the Indices slice wrt the IndexSelectionStrategy and IndexConfig.NumIndexes.
 */
func validateIndexSelectionStrategyAndIndices(config *IndexWorkloadConfig) error {
	templateQueries := getTemplateQueries(config.IndexConfig.TemplateName)

	if (config.IndexConfig.Indices == nil || len(config.IndexConfig.Indices) == 0) && config.IndexConfig.IndexSelectionStrategy != RandomIndexSelection {
		return fmt.Errorf("validate index selection strategy %s: %w", config.IndexConfig.IndexSelectionStrategy, ErrNumIndicesZero)
	}

	if config.IndexConfig.NumIndexes > len(templateQueries) {
		return fmt.Errorf("validate index selection strategy %s: %w", config.IndexConfig.IndexSelectionStrategy, ErrNumIndexesExceedsTemplateQueries)
	}

	switch config.IndexConfig.IndexSelectionStrategy {
	case RandomIndexSelection:
		// No validation required
		return nil
	case SelectiveIndexSelection:
		{
			if len(config.IndexConfig.Indices) != config.IndexConfig.NumIndexes {
				return fmt.Errorf("validate index selection strategy %s: %w", SelectiveIndexSelection, ErrNumIndicesNotEqualToNumIndexes)
			}

			if len(config.IndexConfig.Indices) > len(templateQueries) {
				return fmt.Errorf("validate index selection strategy %s: %w", SelectiveIndexSelection, ErrNumIndexesExceedsTemplateQueries)
			}

			// Validating the indices
			for _, i := range config.IndexConfig.Indices {
				if i < 0 || i > len(templateQueries) {
					return fmt.Errorf("validate index selection strategy %s: %w", config.IndexConfig.IndexSelectionStrategy, ErrInvalidIndices)
				}
			}
		}
	case RangeIndexSelection:
		{
			if len(config.IndexConfig.Indices) != 2 {
				return fmt.Errorf("validate index selection strategy %s: %w", RangeIndexSelection, ErrInvalidIndices)
			}

			// Validating the indices. The elements of indices should be in the range [0, len(templateQueries)]
			for _, i := range config.IndexConfig.Indices {
				if i < 0 || i > len(templateQueries) {
					return fmt.Errorf("validate index selection strategy %s: %w", config.IndexConfig.IndexSelectionStrategy, ErrInvalidIndices)
				}
			}

			if config.IndexConfig.Indices[0] >= config.IndexConfig.Indices[1] {
				return fmt.Errorf("validate index selection strategy %s: %w", config.IndexConfig.IndexSelectionStrategy, ErrMalformedIndicesRange)
			}
		}
	default:
		return fmt.Errorf("validate index selection strategy %s: %w", config.IndexConfig.IndexSelectionStrategy, ErrInvalidIndexSelectionStrategy)
	}

	return nil
}
