package validator

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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

	if cluster.Spec.ClusterSettings.Indexer != nil && cluster.Spec.ClusterSettings.Indexer.StorageMode != couchbasev2.CouchbaseClusterIndexStorageSettingStandard {
		warnings = append(warnings, "CouchbaseCluster spec.cluster.indexer.storageMode is not set to plasma. This is recommended for production clusters.")
	}

	if cluster.Spec.Buckets.Synchronize {
		warnings = append(warnings, "CouchbaseCluster spec.buckets.synchronize is enabled. This is intended for development and should not be used for production clusters.")
	}

	if cluster.Spec.ClusterSettings.AutoCompaction == nil {
		warnings = append(warnings, "CouchbaseCluster spec.cluster.autoCompaction settings have not been configured. It is recommended these are used for production clusters.")
	}

	if !checkLogVolumeMountConfigured(cluster.Spec.Servers) {
		warnings = append(warnings, "CouchbaseCluster spec.servers.volumeMounts.default or spec.servers.volumeMounts.logs is not configured for at least one server resource. To ensure logs are persisted, it is recommended one of these is configured for production clusters.")
	}

	return warnings
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

	if bucket.Spec.StorageBackend != couchbasev2.CouchbaseStorageBackendMagma {
		warnings = append(warnings, "CouchbaseBucket spec.storageBackend is not set to magma, which is the storage mechanism recommended for production clusters.")
	}

	if bucket.Spec.SampleBucket {
		warnings = append(warnings, "CouchbaseBucket cao.couchbase.com/sampleBucket annotation has been enabled. This is intended for development and should not be used for production clusters. While enabled, the bucket will not be updated to match the CRD specification.")
	}

	return warnings
}
