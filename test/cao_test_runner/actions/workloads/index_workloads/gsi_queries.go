package indexworkloads

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidTemplateName = errors.New("invalid template name")
)

// populateGSIQuery adds the required parameters to the Query.
func populateGSIQuery(idxQuery, idxName, bucket, scope, collection string, numReplicas, numPartitions int) string {
	// e.g.: CREATE INDEX %s ON %s.%s.%s(type, state) PARTITION BY HASH((META().id)) WITH { "num_replica": %d, "num_partition": %d };
	// Variables: Index Name, Bucket.Scope.Collection, Index Replicas, NumPartitions
	// Index Replica is always there, Partition may or may not be present.
	hasPartition, hasReplica := false, false

	if strings.Contains(idxQuery, "PARTITION BY") {
		hasPartition = true
	}

	if strings.Contains(idxQuery, "num_replica") {
		hasReplica = true
	}

	switch {
	case hasPartition && hasReplica:
		return fmt.Sprintf(idxQuery, idxName, bucket, scope, collection, numReplicas, numPartitions)
	case !hasPartition && hasReplica:
		return fmt.Sprintf(idxQuery, idxName, bucket, scope, collection, numReplicas)
	case hasPartition && !hasReplica:
		return fmt.Sprintf(idxQuery, idxName, bucket, scope, collection, numPartitions)
	default:
		return fmt.Sprintf(idxQuery, idxName, bucket, scope, collection)
	}
}

func getTemplateQueries(template string) []string {
	switch template {
	case "gideon":
		return gideonIdxQueries
	default:
		return nil
	}
}

func validateTemplateName(template string) error {
	switch template {
	case "gideon":
		return nil
	default:
		return fmt.Errorf("validate template %s: %w", template, ErrInvalidTemplateName)
	}
}

// ==========================================================================================
// ==================================== GSI QUERIES =========================================
// ==========================================================================================

var gideonIdxQueries = []string{
	"\"CREATE INDEX %s ON %s.%s.%s(type, state, rating, ops_sec) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(activity ASC INCLUDE MISSING, sum, failCount, build_id) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(activity, build_id, failCount, totalCount, ARRAY_COUNT(activity) ASC) WITH { \\\"num_replica\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(DISTINCT city, build_id, failCount, totalCount) WITH { \\\"num_replica\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(profile.name, profile.likes, profile.online, profile.friends, ARRAY_COUNT(profile.friends), ARRAY_COUNT(activity) DESC) WITH { \\\"num_replica\\\": %d };\"",

	"\"CREATE INDEX %s ON %s.%s.%s(build_id DESC INCLUDE MISSING, claim, result, priority) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(DISTINCT failCount INCLUDE MISSING, totalCount, duration, result, ARRAY_COUNT(profile.friends) ASC) WITH { \\\"num_replica\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(ops_sec, rating, profile.name, state, ARRAY_COUNT(profile.friends), ARRAY_COUNT(activity) DESC) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(component, os, build ASC, description) WITH { \\\"num_replica\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(profile.name, profile.status, profile.friends, ARRAY_COUNT(profile.friends) ASC, ARRAY_COUNT(activity) DESC) WITH { \\\"num_replica\\\": %d };\"",

	"\"CREATE INDEX %s ON %s.%s.%s(priority ASC INCLUDE MISSING, rating, state, result, ARRAY_COUNT(profile.friends) DESC, ARRAY_COUNT(activity) ASC) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(DISTINCT sum, url, name, component) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(failCount, DISTINCT totalCount, duration, description) WITH { \\\"num_replica\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(priority ASC INCLUDE MISSING, activity, DISTINCT build_id, rating) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(type DESC INCLUDE MISSING, name, url, component) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",

	"\"CREATE INDEX %s ON %s.%s.%s(description, name, component, failCount) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(ops_sec, activity, profile.name ASC, build_id, ARRAY_COUNT(profile.friends), ARRAY_COUNT(activity) ASC) WITH { \\\"num_replica\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(build_id DESC INCLUDE MISSING, ops_sec, result, priority, ARRAY_COUNT(profile.friends), ARRAY_COUNT(activity) DESC) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(rating ASC INCLUDE MISSING, DISTINCT padding, state, profile.friends) WITH { \\\"num_replica\\\": %d };\"",
	"\"CREATE INDEX %s ON %s.%s.%s(ops_sec, rating, DISTINCT description, url ASC) PARTITION BY HASH((META().id)) WITH { \\\"num_replica\\\": %d, \\\"num_partition\\\": %d };\"",
}
