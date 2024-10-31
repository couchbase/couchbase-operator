package cluster

import (
	"reflect"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestHistoryRetention(t *testing.T) {
	k8sBucket := make([]*couchbasev2.CouchbaseBucket, 0)
	k8sBucket = append(k8sBucket, &couchbasev2.CouchbaseBucket{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{
				"cao.couchbase.com/historyRetention.seconds":                  "100",
				"cao.couchbase.com/historyRetention.bytes":                    "50",
				"cao.couchbase.com/historyRetention.collectionHistoryDefault": "true",
			},
		},

		Spec: couchbasev2.CouchbaseBucketSpec{
			Name:           "test",
			MemoryQuota:    resource.NewQuantity(100, resource.BinarySI),
			StorageBackend: "magma",
		},
	})

	features := SupportedFeatureMap{
		SupportedBackendCouchstore: true,
		SupportedBackendMagma:      true,
		SupportedDurability:        true,
		SupportedHistoryRetention:  true,
	}

	newBuckets := gatherCouchbaseBuckets(features, labels.Everything(), k8sBucket, nil, nil, nil)
	if newBuckets[0].HistoryRetentionBytes != 50 {
		t.Fatalf("expected HistoryRetentionBytes=50, found %d", newBuckets[0].HistoryRetentionBytes)
	}

	if newBuckets[0].HistoryRetentionSeconds != 100 {
		t.Fatalf("expected HistoryRetentionSeconds=100, found %d", newBuckets[0].HistoryRetentionSeconds)
	}

	if !*(newBuckets[0].HistoryRetentionCollectionDefault) {
		t.Fatalf("expected HistoryRetentionCollectionDefault=true, found %t", *(newBuckets[0].HistoryRetentionCollectionDefault))
	}
}

func TestMagmaNoDataBlockSizeSettingsViaAnnotations(t *testing.T) {
	k8sBucket := make([]*couchbasev2.CouchbaseBucket, 0)
	k8sBucket = append(k8sBucket, &couchbasev2.CouchbaseBucket{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{},
		},

		Spec: couchbasev2.CouchbaseBucketSpec{
			Name:           "test",
			MemoryQuota:    resource.NewQuantity(100, resource.BinarySI),
			StorageBackend: "magma",
		},
	})

	features := SupportedFeatureMap{
		SupportedBackendMagma: true,
	}

	newBuckets := gatherCouchbaseBuckets(features, labels.Everything(), k8sBucket, nil, nil, nil)
	if newBuckets[0].MagmaSeqTreeDataBlockSize != nil && *(newBuckets[0].MagmaSeqTreeDataBlockSize) != 4096 {
		t.Fatalf("expected MagmaSeqTreeDataBlockSize=4096, found %d", *(newBuckets[0].MagmaSeqTreeDataBlockSize))
	}

	if newBuckets[0].MagmaKeyTreeDataBlockSize != nil && *(newBuckets[0].MagmaKeyTreeDataBlockSize) != 4096 {
		t.Fatalf("expected MagmaKeyTreeDataBlockSize=4096, found %d", *(newBuckets[0].MagmaKeyTreeDataBlockSize))
	}
}

func TestMagmaDataBlockSizeSettingsViaAnnotations(t *testing.T) {
	k8sBucket := make([]*couchbasev2.CouchbaseBucket, 0)
	k8sBucket = append(k8sBucket, &couchbasev2.CouchbaseBucket{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{
				"cao.couchbase.com/magmaSeqTreeDataBlockSize": "5555",
				"cao.couchbase.com/magmaKeyTreeDataBlockSize": "6666",
			},
		},

		Spec: couchbasev2.CouchbaseBucketSpec{
			Name:           "test",
			MemoryQuota:    resource.NewQuantity(100, resource.BinarySI),
			StorageBackend: "magma",
		},
	})

	features := SupportedFeatureMap{
		SupportedBackendMagma: true,
	}

	newBuckets := gatherCouchbaseBuckets(features, labels.Everything(), k8sBucket, nil, nil, nil)
	if newBuckets[0].MagmaSeqTreeDataBlockSize != nil && *(newBuckets[0].MagmaSeqTreeDataBlockSize) != 5555 {
		t.Fatalf("expected MagmaSeqTreeDataBlockSize=5555, found %d", *(newBuckets[0].MagmaSeqTreeDataBlockSize))
	}

	if newBuckets[0].MagmaKeyTreeDataBlockSize != nil && *(newBuckets[0].MagmaKeyTreeDataBlockSize) != 6666 {
		t.Fatalf("expected MagmaKeyTreeDataBlockSize=6666, found %d", *(newBuckets[0].MagmaKeyTreeDataBlockSize))
	}
}

func TestEqualizeBucketAutoCompactionSettings(t *testing.T) {
	testcases := []struct {
		name        string
		requested   couchbaseutil.Bucket
		actual      couchbaseutil.Bucket
		shouldEqual bool
	}{
		{
			name:        "Auto-compaction disabled",
			requested:   couchbaseutil.Bucket{},
			actual:      couchbaseutil.Bucket{},
			shouldEqual: true,
		},
		{
			name: "Purge interval not set on requested",
			requested: couchbaseutil.Bucket{
				AutoCompactionSettings: couchbaseutil.BucketAutoCompactionSettings{Enabled: true},
				PurgeInterval:          nil,
			},
			actual: couchbaseutil.Bucket{
				AutoCompactionSettings: couchbaseutil.BucketAutoCompactionSettings{Enabled: true},
				PurgeInterval:          floatPtr(2.5),
			},
			shouldEqual: true,
		},
		{
			name: "Threshold percentages not set on requested bucket",
			requested: couchbaseutil.Bucket{
				AutoCompactionSettings: cbEnabledAutoCompactionSettings(cbViewThreshold(0, 256), cbDatabaseThreshold(0, 256)),
				PurgeInterval:          floatPtr(1.5),
			},
			actual: couchbaseutil.Bucket{
				AutoCompactionSettings: cbEnabledAutoCompactionSettings(cbViewThreshold(10, 256), cbDatabaseThreshold(10, 256)),
				PurgeInterval:          floatPtr(1.5),
			},
			shouldEqual: true,
		},
		{
			name: "Threshold percentages changed on requested bucket",
			requested: couchbaseutil.Bucket{
				AutoCompactionSettings: cbEnabledAutoCompactionSettings(cbViewThreshold(10, 256), cbDatabaseThreshold(60, 256)),
				PurgeInterval:          floatPtr(1.5),
			},
			actual: couchbaseutil.Bucket{
				AutoCompactionSettings: cbEnabledAutoCompactionSettings(cbViewThreshold(15, 256), cbDatabaseThreshold(30, 256)),
				PurgeInterval:          floatPtr(1.5),
			},
			shouldEqual: false,
		},
		{
			name: "Threshold size changed on requested bucket",
			requested: couchbaseutil.Bucket{
				AutoCompactionSettings: cbEnabledAutoCompactionSettings(cbViewThreshold(30, 512), cbDatabaseThreshold(30, 256)),
				PurgeInterval:          floatPtr(1.5),
			},
			actual: couchbaseutil.Bucket{
				AutoCompactionSettings: cbEnabledAutoCompactionSettings(cbViewThreshold(30, 256), cbDatabaseThreshold(30, 256)),
				PurgeInterval:          floatPtr(1.5),
			},
			shouldEqual: false,
		},
	}

	for _, testcase := range testcases {
		equalizeBucketAutoCompactionSettings(&testcase.requested, &testcase.actual)

		result := reflect.DeepEqual(testcase.requested, testcase.actual)
		if result != testcase.shouldEqual {
			t.Errorf("test %v failed. Expected DeepEqual to be %v but got %v", testcase.name, testcase.shouldEqual, result)
		}
	}
}

func TestGatherBucketAutoCompactionSettings(t *testing.T) {
	cluster := couchbasev2.CouchbaseCluster{
		Spec: couchbasev2.ClusterSpec{
			ClusterSettings: couchbasev2.ClusterConfig{
				AutoCompaction: &couchbasev2.AutoCompaction{},
			},
		},
	}

	testcases := []struct {
		name                      string
		clusterParallelCompaction bool
		bucketCRDSpec             couchbasev2.AutoCompactionSpecBucket
		expected                  couchbaseutil.AutoCompactionAutoCompactionSettings
	}{
		{
			name:                      "Parallel compaction only",
			clusterParallelCompaction: false,
			bucketCRDSpec:             couchbasev2.AutoCompactionSpecBucket{},
			expected: couchbaseutil.AutoCompactionAutoCompactionSettings{
				ParallelDBAndViewCompaction: false,
			},
		},
		{
			name:                      "View threshold in CRD only",
			clusterParallelCompaction: false,
			bucketCRDSpec: couchbasev2.AutoCompactionSpecBucket{
				ViewFragmentationThreshold: crdViewThreshold(intPtr(10), intPtr(256)),
			},
			expected: couchbaseutil.AutoCompactionAutoCompactionSettings{
				ParallelDBAndViewCompaction: false,
				ViewFragmentationThreshold:  cbViewThreshold(10, 256),
			},
		},
		{
			name:                      "Database threshold in CRD only",
			clusterParallelCompaction: true,
			bucketCRDSpec: couchbasev2.AutoCompactionSpecBucket{
				DatabaseFragmentationThreshold: crdDatabaseThreshold(intPtr(25), intPtr(512)),
			},
			expected: couchbaseutil.AutoCompactionAutoCompactionSettings{
				ParallelDBAndViewCompaction:    true,
				DatabaseFragmentationThreshold: cbDatabaseThreshold(25, 512),
			},
		},
		{
			name:                      "Time window in CRD only",
			clusterParallelCompaction: true,
			bucketCRDSpec: couchbasev2.AutoCompactionSpecBucket{
				TimeWindow: crdTimeWindow(true, "10:15", "12:30"),
			},
			expected: couchbaseutil.AutoCompactionAutoCompactionSettings{
				ParallelDBAndViewCompaction: true,
				AllowedTimePeriod:           cbTimeWindow(true, 15, 10, 30, 12),
			},
		},
		{
			name:                      "Multiple settings in CRD",
			clusterParallelCompaction: true,
			bucketCRDSpec: couchbasev2.AutoCompactionSpecBucket{
				DatabaseFragmentationThreshold: crdDatabaseThreshold(intPtr(16), intPtr(300)),
				ViewFragmentationThreshold:     crdViewThreshold(nil, intPtr(128)),
				TimeWindow:                     crdTimeWindow(false, "12:30", "17:45"),
			},
			expected: couchbaseutil.AutoCompactionAutoCompactionSettings{
				ParallelDBAndViewCompaction:    true,
				DatabaseFragmentationThreshold: cbDatabaseThreshold(16, 300),
				ViewFragmentationThreshold:     cbViewThreshold(0, 128),
				AllowedTimePeriod:              cbTimeWindow(false, 30, 12, 45, 17),
			},
		},
	}

	for _, testcase := range testcases {
		cluster.Spec.ClusterSettings.AutoCompaction.ParallelCompaction = testcase.clusterParallelCompaction

		result := gatherBucketAutoCompactionSettings(&testcase.bucketCRDSpec, &cluster)
		if !reflect.DeepEqual(testcase.expected, *result) {
			t.Errorf("test %v failed. Unable to correctly set bucket auto-compaction settings, expected %v but got %v", testcase.name, testcase.expected, result)
		}
	}
}

func cbViewThreshold(percent, sizeMi int) couchbaseutil.AutoCompactionViewFragmentationThreshold {
	return couchbaseutil.AutoCompactionViewFragmentationThreshold{Percentage: percent, Size: int64(sizeMi * 1024 * 1024)}
}

func cbDatabaseThreshold(percent, sizeMi int) couchbaseutil.AutoCompactionDatabaseFragmentationThreshold {
	return couchbaseutil.AutoCompactionDatabaseFragmentationThreshold{Percentage: percent, Size: int64(sizeMi * 1024 * 1024)}
}

func cbTimeWindow(abortCompaction bool, fromMin, fromHour, toMin, toHour int) *couchbaseutil.AutoCompactionAllowedTimePeriod {
	return &couchbaseutil.AutoCompactionAllowedTimePeriod{
		AbortOutside: abortCompaction,
		FromMinute:   fromMin,
		FromHour:     fromHour,
		ToMinute:     toMin,
		ToHour:       toHour,
	}
}

func cbEnabledAutoCompactionSettings(viewThreshold couchbaseutil.AutoCompactionViewFragmentationThreshold, databaseThreshold couchbaseutil.AutoCompactionDatabaseFragmentationThreshold) couchbaseutil.BucketAutoCompactionSettings {
	return couchbaseutil.BucketAutoCompactionSettings{
		Enabled: true,
		Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
			ViewFragmentationThreshold:     viewThreshold,
			DatabaseFragmentationThreshold: databaseThreshold,
			AllowedTimePeriod:              nil,
		},
	}
}

func crdViewThreshold(percent, sizeMi *int) *couchbasev2.ViewFragmentationThresholdBucket {
	var size *resource.Quantity
	if sizeMi != nil {
		size = k8sutil.NewResourceQuantityMi(int64(*sizeMi))
	}

	return &couchbasev2.ViewFragmentationThresholdBucket{Percent: percent, Size: size}
}

func crdDatabaseThreshold(percent, sizeMi *int) *couchbasev2.DatabaseFragmentationThresholdBucket {
	var size *resource.Quantity
	if sizeMi != nil {
		size = k8sutil.NewResourceQuantityMi(int64(*sizeMi))
	}

	return &couchbasev2.DatabaseFragmentationThresholdBucket{Percent: percent, Size: size}
}

func crdTimeWindow(abortCompaction bool, start, end string) *couchbasev2.TimeWindow {
	return &couchbasev2.TimeWindow{
		AbortCompactionOutsideWindow: abortCompaction,
		Start:                        &start,
		End:                          &end,
	}
}

func floatPtr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}
