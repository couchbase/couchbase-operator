package validator

import (
	"strings"
	"testing"
	"time"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCheckFieldsCouchbaseCluster(t *testing.T) {
	testcases := []struct {
		name             string
		clusterSpec      v2.ClusterSpec
		expectedWarnings []string
	}{
		{
			name: "no warnings expected",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					AutoFailoverMaxCount: 2,
					AutoCompaction:       &v2.AutoCompaction{},
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
				AntiAffinity: true,
			},
			expectedWarnings: []string{},
		},
		{
			name: "spec.cluster.autoFailover settings left as defaults",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					AutoFailoverTimeout:                    &metav1.Duration{Duration: 120 * time.Second},
					AutoFailoverMaxCount:                   1,
					AutoFailoverOnDataDiskIssues:           false,
					AutoFailoverOnDataDiskIssuesTimePeriod: &metav1.Duration{Duration: 120 * time.Second},
					AutoCompaction:                         &v2.AutoCompaction{},
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
				AntiAffinity: true,
			},
			expectedWarnings: []string{"spec.cluster.autoFailover"},
		},
		{
			name: "spec.antiAffinity is disabled",
			clusterSpec: v2.ClusterSpec{
				AntiAffinity: false,
				ClusterSettings: v2.ClusterConfig{
					AutoFailoverMaxCount: 2,
					AutoCompaction:       &v2.AutoCompaction{},
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
			},
			expectedWarnings: []string{"spec.antiAffinity"},
		},
		{
			name: "spec.cluster.indexer.storageMode is not plasma",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingMemoryOptimized,
					},
					AutoFailoverMaxCount: 2,
					AutoCompaction:       &v2.AutoCompaction{},
				},
				AntiAffinity: true,
			},
			expectedWarnings: []string{"spec.cluster.indexer.storageMode"},
		},
		{
			name: "spec.cluster.indexer.storageMode is not plasma by default",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					AutoCompaction: &v2.AutoCompaction{},
					Indexer:        nil,
				},
				AntiAffinity: true,
			},
			expectedWarnings: []string{"spec.cluster.indexer.storageMode"},
		},
		{
			name: "spec.buckets.synchronize is enabled",
			clusterSpec: v2.ClusterSpec{
				Buckets: v2.Buckets{
					Synchronize: true,
				},
				ClusterSettings: v2.ClusterConfig{
					AutoCompaction: &v2.AutoCompaction{},
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
				AntiAffinity: true,
			},
			expectedWarnings: []string{"spec.buckets.synchronize"},
		},
		{
			name: "spec.cluster.autoCompaction is nil",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					AutoCompaction: nil,
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
				AntiAffinity: true,
			},
			expectedWarnings: []string{"spec.cluster.autoCompaction"},
		},
		{
			name: "spec.cluster.autoCompaction is nil and spec.antiAffinity is disabled",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					AutoCompaction:       nil,
					AutoFailoverMaxCount: 2,
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
				AntiAffinity: false,
			},
			expectedWarnings: []string{"spec.cluster.autoCompaction", "spec.antiAffinity"},
		},
		{
			name: "spec.servers.volumeMounts logs are not set",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					AutoCompaction: &v2.AutoCompaction{},
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
				AntiAffinity: true,
				Servers: []v2.ServerConfig{
					{
						VolumeMounts: &v2.VolumeMounts{
							LogsClaim:    "",
							DefaultClaim: "",
						},
					},
				},
			},
			expectedWarnings: []string{"spec.servers.volumeMounts.default or spec.servers.volumeMounts.logs"},
		},
		{
			name: "spec.servers.volumeMounts log claim is set",
			clusterSpec: v2.ClusterSpec{
				ClusterSettings: v2.ClusterConfig{
					AutoCompaction: &v2.AutoCompaction{},
					Indexer: &v2.CouchbaseClusterIndexerSettings{
						StorageMode: v2.CouchbaseClusterIndexStorageSettingStandard,
					},
				},
				AntiAffinity: true,
				Servers: []v2.ServerConfig{
					{
						VolumeMounts: &v2.VolumeMounts{
							LogsClaim:    "some_log_claim",
							DefaultClaim: "",
						},
					},
				},
			},
			expectedWarnings: []string{},
		},
	}

	for _, testcase := range testcases {
		cluster := v2.CouchbaseCluster{Spec: testcase.clusterSpec}
		result := checkFieldsCouchbaseCluster(cluster)

		if len(result) != len(testcase.expectedWarnings) {
			t.Errorf("%v failed. Expected there to be %v warnings on the cluster, but there were %v", testcase.name, len(testcase.expectedWarnings), result)
		}

		if !checkIfWarningPresent(testcase.expectedWarnings, result) {
			t.Errorf("%v failed. At least one of the expected warning substrings %v was missing from the list of results", testcase.name, testcase.expectedWarnings)
		}
	}
}

func TestCheckFieldsCouchbaseBuckets(t *testing.T) {
	testcases := []struct {
		name             string
		bucketSpec       v2.CouchbaseBucketSpec
		expectedWarnings []string
	}{
		{
			name: "no warning expected",
			bucketSpec: v2.CouchbaseBucketSpec{
				StorageBackend: v2.CouchbaseStorageBackendMagma,
			},
			expectedWarnings: []string{},
		},
		{
			name:             "spec.storageBackend not set to magma",
			bucketSpec:       v2.CouchbaseBucketSpec{StorageBackend: v2.CouchbaseStorageBackendCouchstore},
			expectedWarnings: []string{"spec.storageBackend"},
		},
		{
			name: "spec.sampleBucket is set to true ",
			bucketSpec: v2.CouchbaseBucketSpec{
				SampleBucket:   true,
				StorageBackend: v2.CouchbaseStorageBackendMagma,
			},
			expectedWarnings: []string{"CouchbaseBucket cao.couchbase.com/sampleBucket annotation has been enabled"},
		},
	}

	for _, testcase := range testcases {
		bucket := v2.CouchbaseBucket{Spec: testcase.bucketSpec}
		result := checkFieldsCouchbaseBucket(bucket)

		if len(result) != len(testcase.expectedWarnings) {
			t.Errorf("%v failed. Expected there to be %v warnings on the bucket, but there were %v", testcase.name, len(testcase.expectedWarnings), len(result))
		}

		if !checkIfWarningPresent(testcase.expectedWarnings, result) {
			t.Errorf("%v failed. At least one of the expected warning substrings %v was missing from the list of results", testcase.name, testcase.expectedWarnings)
		}
	}
}

func checkIfWarningPresent(expectedWarningSubstrings, actualWarnings []string) bool {
	for _, expectedWarningSubstr := range expectedWarningSubstrings {
		found := false

		for _, actualWarning := range actualWarnings {
			if strings.Contains(actualWarning, expectedWarningSubstr) {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}
