package workloads

import (
	"errors"
	"fmt"
	"strconv"
)

var (
	ErrBucketNameEmpty = errors.New("bucket name is empty")
	ErrScopeEmpty      = errors.New("scope name is empty")
	ErrCollectionEmpty = errors.New("collection name is empty")
)

type BucketConfig struct {
	Bucket string        `yaml:"bucket" caoCli:"required"`
	Scopes []ScopeConfig `yaml:"scopes"`
}

type ScopeConfig struct {
	Scope       string   `yaml:"scope" caoCli:"required"`
	Collections []string `yaml:"collections"`
}

// ValidateBucketsConfig validates the IndexWorkloadConfig.Buckets (BucketConfig).
/*
 * If IndexWorkloadConfig.BucketCount > 0 and IndexWorkloadConfig.Buckets is nil then default BucketConfig is loaded.
 * If BucketConfig.Bucket != "" and its ScopeConfig and Collections slice is nil then default Scope and Collection is used.
 */
func ValidateBucketsConfig(bucketsConfig []*BucketConfig) error {
	for _, bucketConfig := range bucketsConfig {
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

func PopulateBucketConfig(bucketCount int) []*BucketConfig {
	// var bucketConfigs []BucketConfig
	bucketConfigs := make([]*BucketConfig, 0)

	// Bucket naming goes bucket0, bucket1, ...
	bucketPrefix := "bucket"
	
	for i := range bucketCount {
		bucketConfigs = append(bucketConfigs, &BucketConfig{
			Bucket: bucketPrefix + strconv.Itoa(i),
			Scopes: []ScopeConfig{{
				Scope:       "_default",
				Collections: []string{"_default"},
			}},
		})
	}

	return bucketConfigs
}
