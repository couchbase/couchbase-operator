package indexworkloads

import "fmt"

func generateQueryForIdx(idxWorkloadConfig *IndexWorkloadConfig) map[string][]string {
	// TODO
	panic("generateQueryForIdx to be implemented")
}

// generateQueryForPrimaryIdx generates the queries for the primary indexes.
/*
 * Returns a map of bucket name to the list of queries for the primary indexes for that bucket.
 */
func generateQueryForPrimaryIdx(idxWorkloadConfig *IndexWorkloadConfig) map[string][]string {
	queries := make(map[string][]string)

	for _, bucketConfig := range idxWorkloadConfig.Buckets {
		idxNum := 0

		for _, scopeConfig := range bucketConfig.Scopes {
			for _, collection := range scopeConfig.Collections {
				for range idxWorkloadConfig.IndexConfig.NumIndexes {
					primaryIndexName := fmt.Sprintf("primary_idx_%d", idxNum)
					idxNum++
					query := fmt.Sprintf("\"CREATE PRIMARY INDEX %s ON %s.%s.%s WITH { \\\"num_replica\\\": %d };\"", primaryIndexName, bucketConfig.Bucket, scopeConfig.Scope, collection, idxWorkloadConfig.IndexConfig.IndexReplicas)
					queries[bucketConfig.Bucket] = append(queries[bucketConfig.Bucket], query)
				}
			}
		}
	}

	return queries
}
