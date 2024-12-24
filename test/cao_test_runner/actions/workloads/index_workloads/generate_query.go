package indexworkloads

import (
	"fmt"
	"time"
)

func generateQueryForGSI(idxWorkloadConfig *IndexWorkloadConfig) map[string][]string {
	return SelectAndPopulateGSIQueries(idxWorkloadConfig)
}

// generateQueryForPrimaryIdx generates the queries for the primary indexes.
/*
 * Returns a map of bucket name to the list of queries for the primary indexes for that bucket.
 * Primary Index naming: primary_<idx-prefix>_<index-num>. E.g. primary_12_25_150535_2.
 */
func generateQueryForPrimaryIdx(idxWorkloadConfig *IndexWorkloadConfig) map[string][]string {
	queries := make(map[string][]string)
	idxPrefix := time.Now().Format("01_02_150405")

	for _, bucketConfig := range idxWorkloadConfig.Buckets {
		idxNum := 0

		for _, scopeConfig := range bucketConfig.Scopes {
			for _, collection := range scopeConfig.Collections {
				for range idxWorkloadConfig.IndexConfig.NumIndexes {
					primaryIndexName := fmt.Sprintf("primary_%s_%d", idxPrefix, idxNum)
					idxNum++
					query := fmt.Sprintf("\"CREATE PRIMARY INDEX %s ON %s.%s.%s WITH { \\\"num_replica\\\": %d };\"", primaryIndexName, bucketConfig.Bucket, scopeConfig.Scope, collection, idxWorkloadConfig.IndexConfig.NumReplicas)
					queries[bucketConfig.Bucket] = append(queries[bucketConfig.Bucket], query)
				}
			}
		}
	}

	return queries
}
