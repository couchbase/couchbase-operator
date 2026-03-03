/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package validator

import (
	"fmt"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// checkFieldsCouchbaseCluster will return a slice of strings, each representing a warning that should not block
// validation but should be communicated back to the user, such as non-optimal settings for production clusters.
func checkFieldsCouchbaseCluster(cluster couchbasev2.CouchbaseCluster) []string {
	warnings := make([]string, 0)

	if checkAutoFailoverDefaults(cluster.Spec.ClusterSettings) {
		warnings = append(warnings, "CouchbaseCluster spec.cluster.autoFailover settings have been left as their defaults. It is recommended these are tuned for production clusters.")
	}

	if !cluster.Spec.AntiAffinity {
		warnings = append(warnings, "CouchbaseCluster spec.antiAffinity is disabled. It is recommended this is enabled for production clusters.")
	}

	if cluster.Spec.ClusterSettings.Indexer == nil || cluster.Spec.ClusterSettings.Indexer.StorageMode == couchbasev2.CouchbaseClusterIndexStorageSettingMemoryOptimized {
		warnings = append(warnings, "CouchbaseCluster spec.cluster.indexer.storageMode is not set to plasma. This is recommended for production clusters.")
	}

	if cluster.Spec.Buckets.Synchronize {
		warnings = append(warnings, "CouchbaseCluster spec.buckets.synchronize is enabled. This is intended for development and should not be used for production clusters.")
	}

	if checkAutoCompactionDefaults(cluster.Spec.ClusterSettings.AutoCompaction) {
		warnings = append(warnings, "CouchbaseCluster spec.cluster.autoCompaction settings have been left as their defaults. It is recommended these are tuned for production clusters.")
	}

	if !checkLogVolumeMountConfigured(cluster.Spec.Servers) {
		warnings = append(warnings, "CouchbaseCluster spec.servers.volumeMounts.default or spec.servers.volumeMounts.logs is not configured for at least one server resource. To ensure logs are persisted, it is recommended one of these is configured for production clusters.")
	}

	// If hibernate is enabled and the cluster is not already hibernating, we should warn if we will not enter hibernation immediately.
	if cluster.Spec.Hibernate && !cluster.HasCondition(couchbasev2.ClusterConditionHibernating) {
		if willHibernate, reason := cluster.CanHibernate(); !willHibernate {
			warnings = append(warnings, fmt.Sprintf("CouchbaseCluster spec.hibernate is enabled, but the cluster cannot enter hibernation: %s. Hibernation will occur once the cluster is stable.", reason))
		}
	}

	return warnings
}

func checkAutoCompactionDefaults(autoCompaction *couchbasev2.AutoCompaction) bool {
	return autoCompaction != nil && *autoCompaction.DatabaseFragmentationThreshold.Percent == 30 && *autoCompaction.ViewFragmentationThreshold.Percent == 30 && autoCompaction.DatabaseFragmentationThreshold.Size == nil && autoCompaction.ViewFragmentationThreshold.Size == nil && *autoCompaction.TombstonePurgeInterval == metav1.Duration{Duration: 72 * time.Hour}
}

func checkAutoFailoverDefaults(clusterSettings couchbasev2.ClusterConfig) bool {
	return clusterSettings.AutoFailoverTimeout != nil && clusterSettings.AutoFailoverTimeout.Seconds() == 120 &&
		clusterSettings.AutoFailoverMaxCount == 1 && !clusterSettings.AutoFailoverOnDataDiskIssues && clusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod != nil && clusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod.Seconds() == 120
}

func checkLogVolumeMountConfigured(servers []couchbasev2.ServerConfig) bool {
	for _, server := range servers {
		volumeMounts := server.GetVolumeMounts()
		if volumeMounts == nil || (!volumeMounts.HasDefaultMount() && !volumeMounts.LogsOnly()) {
			return false
		}
	}

	return true
}

// checkFieldsCouchbaseBucket will return a slice of strings, each representing a warning that should not block
// validation but should be communicated back to the user, such as non-optimal settings for production clusters.
func checkFieldsCouchbaseBucket(bucket couchbasev2.CouchbaseBucket) []string {
	warnings := make([]string, 0)

	if bucket.Spec.SampleBucket {
		warnings = append(warnings, "CouchbaseBucket cao.couchbase.com/sampleBucket annotation has been enabled. This is intended for development and should not be used for production clusters. While enabled, the bucket will not be updated to match the CRD specification.")
	}

	return warnings
}
