package workloads

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

var (
	ErrBucketNameEmpty       = errors.New("bucket name is empty")
	ErrInvalidBucketName     = errors.New("invalid bucket name")
	ErrScopeNameEmpty        = errors.New("scope name is empty")
	ErrInvalidScopeName      = errors.New("invalid scope name")
	ErrCollectionNameEmpty   = errors.New("collection name is empty")
	ErrInvalidCollectionName = errors.New("invalid collection name")
)

var (
	// https://docs.couchbase.com/server/current/manage/manage-buckets/create-bucket.html.
	validBucketNameRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_\-%.]{0,99}$`)
	// https://docs.couchbase.com/server/current/learn/data/scopes-and-collections.html#naming-for-scopes-and-collections.
	validScopeOrCollNameRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_\-%]{0,250}$`)
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
		if err := validateBucketName(bucketConfig.Bucket); err != nil {
			return fmt.Errorf("validate buckets config: %w", err)
		}

		if bucketConfig.Scopes == nil {
			bucketConfig.Scopes = []ScopeConfig{{
				Scope:       "_default",
				Collections: []string{"_default"},
			}}
		}

		for _, scopeConfig := range bucketConfig.Scopes {
			if err := validateScopeName(scopeConfig.Scope); err != nil {
				return fmt.Errorf("validate buckets config: %w", err)
			}

			if scopeConfig.Collections == nil {
				scopeConfig.Collections = []string{"_default"}
			}

			for _, coll := range scopeConfig.Collections {
				if err := validateCollectionName(coll); err != nil {
					return fmt.Errorf("validate buckets config: %w", err)
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

func validateBucketName(bucket string) error {
	if bucket == "" {
		return fmt.Errorf("validate bucket name: %w", ErrBucketNameEmpty)
	}

	if !validBucketNameRegex.MatchString(bucket) {
		return fmt.Errorf("validate bucket name: %w", ErrInvalidBucketName)
	}

	return nil
}

func validateScopeName(scope string) error {
	if scope == "" {
		return fmt.Errorf("validate scope name: %w", ErrScopeNameEmpty)
	}

	if scope == "_system" {
		return fmt.Errorf("validate scope name: %w", ErrInvalidScopeName)
	}

	if !validScopeOrCollNameRegex.MatchString(scope) {
		if scope != "_default" {
			return fmt.Errorf("validate scope name: %w", ErrInvalidScopeName)
		}
	}

	return nil
}

func validateCollectionName(collection string) error {
	if collection == "" {
		return fmt.Errorf("validate collection name: %w", ErrCollectionNameEmpty)
	}

	if !validScopeOrCollNameRegex.MatchString(collection) {
		if collection != "_default" {
			return fmt.Errorf("validate collection name: %w", ErrInvalidCollectionName)
		}
	}

	return nil
}
