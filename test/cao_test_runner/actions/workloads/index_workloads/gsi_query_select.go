package indexworkloads

import (
	"fmt"
	"math/rand"
	"time"
)

type IndexSelectionStrategy string

const (
	RandomIndexSelection    IndexSelectionStrategy = "random"
	RangeIndexSelection     IndexSelectionStrategy = "range"
	SelectiveIndexSelection IndexSelectionStrategy = "selective"
)

// SelectAndPopulateGSIQueries selects the indices based on the selection strategy and populates the queries.
/*
 * Gets the queries based on the template name.
 * Selects the indices based on the IndexSelectionStrategy.
 * Populates the queries with the index name, bucket name, scope name, collection name, index replicas and partitions.
 * Returns a map of bucket name to the index queries for the bucket. map[bucket-name][]{index queries...}.
 * Index Naming: idx_<bucket-name>_<idx-prefix>_<index-num>. E.g. idx_bucket1_12_25_150535_2.
 */
func SelectAndPopulateGSIQueries(idxWorkloadConfig *IndexWorkloadConfig) map[string][]string {
	queries := make(map[string][]string)

	// Getting the required queries based on the template name.
	templateQueries := getTemplateQueries(idxWorkloadConfig.IndexConfig.TemplateName)
	numIdxQueries := len(gideonIdxQueries)
	idxPrefix := time.Now().Format("01_02_150405")

	for _, bucketConfig := range idxWorkloadConfig.Buckets {
		idxNum := 0
		// indices is the indices of the templateIdxQueries e.g. gideonIdxQueries.
		indices := selectQueries(idxWorkloadConfig.IndexConfig.IndexSelectionStrategy, idxWorkloadConfig.IndexConfig.NumIndexes, numIdxQueries, idxWorkloadConfig.IndexConfig.Indices)

		for _, scopeConfig := range bucketConfig.Scopes {
			for _, collection := range scopeConfig.Collections {
				for _, i := range indices {
					idxName := fmt.Sprintf("idx_%s_%s_%d", bucketConfig.Bucket, idxPrefix, idxNum)
					idxNum++

					query := populateGSIQuery(templateQueries[i], idxName, bucketConfig.Bucket, scopeConfig.Scope, collection, idxWorkloadConfig.IndexConfig.NumReplicas, idxWorkloadConfig.IndexConfig.NumPartitions)
					queries[bucketConfig.Bucket] = append(queries[bucketConfig.Bucket], query)
				}
			}
		}
	}

	return queries
}

// selectQueries returns the indices to be selected based on the selection strategy.
func selectQueries(strategy IndexSelectionStrategy, reqQueries, totalQueries int, rangeOrSelective []int) []int {
	sliceToReturn := make([]int, 0)

	switch strategy {
	case RangeIndexSelection:
		{
			for i := rangeOrSelective[0]; i < rangeOrSelective[1]; i++ {
				sliceToReturn = append(sliceToReturn, i)
			}
		}
	case SelectiveIndexSelection:
		{
			sliceToReturn = rangeOrSelective
		}
	case RandomIndexSelection:
		{
			indices := make([]int, totalQueries)
			for i := range totalQueries {
				indices[i] = i
			}

			randGen := rand.New(rand.NewSource(time.Now().UnixNano()))

			// Fisher-Yates shuffle to randomize the indices
			for i := len(indices) - 1; i > 0; i-- {
				j := randGen.Intn(i + 1)
				indices[i], indices[j] = indices[j], indices[i]
			}

			sliceToReturn = indices[:reqQueries]
		}
	}

	return sliceToReturn
}
