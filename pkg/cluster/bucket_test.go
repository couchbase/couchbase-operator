package cluster

import (
	"reflect"
	"testing"
	"time"

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

	newBuckets := gatherCouchbaseBuckets(features, labels.Everything(), k8sBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)
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

	newBuckets := gatherCouchbaseBuckets(features, labels.Everything(), k8sBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)
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

	newBuckets := gatherCouchbaseBuckets(features, labels.Everything(), k8sBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)
	if newBuckets[0].MagmaSeqTreeDataBlockSize != nil && *(newBuckets[0].MagmaSeqTreeDataBlockSize) != 5555 {
		t.Fatalf("expected MagmaSeqTreeDataBlockSize=5555, found %d", *(newBuckets[0].MagmaSeqTreeDataBlockSize))
	}

	if newBuckets[0].MagmaKeyTreeDataBlockSize != nil && *(newBuckets[0].MagmaKeyTreeDataBlockSize) != 6666 {
		t.Fatalf("expected MagmaKeyTreeDataBlockSize=6666, found %d", *(newBuckets[0].MagmaKeyTreeDataBlockSize))
	}
}

func TestGatherBucketAutoCompactionSettings(t *testing.T) {
	type expected struct {
		settings      couchbaseutil.BucketAutoCompactionSettings
		purgeInterval *float64
	}

	testcases := []struct {
		name            string
		crdSettings     *couchbasev2.AutoCompactionSpecBucket
		storageBackend  couchbaseutil.CouchbaseStorageBackend
		clusterSettings *couchbasev2.AutoCompaction
		expected        expected
	}{
		{
			name:            "no crd settings",
			crdSettings:     nil,
			storageBackend:  couchbaseutil.CouchbaseStorageBackendCouchstore,
			clusterSettings: &couchbasev2.AutoCompaction{},
			expected:        expected{settings: couchbaseutil.BucketAutoCompactionSettings{}},
		},
		{
			name:           "no cluster settings",
			crdSettings:    &couchbasev2.AutoCompactionSpecBucket{},
			storageBackend: couchbaseutil.CouchbaseStorageBackendCouchstore,
			expected:       expected{settings: couchbaseutil.BucketAutoCompactionSettings{}},
		},
		{
			name: "couchstore with crd settings",
			crdSettings: &couchbasev2.AutoCompactionSpecBucket{
				ViewFragmentationThreshold:     crdViewThreshold(intPtr(10), intPtr(100)),
				DatabaseFragmentationThreshold: crdDatabaseThreshold(intPtr(10), intPtr(100)),
				TimeWindow:                     crdTimeWindow(true, "00:00", "23:59"),
				TombstonePurgeInterval:         &v1.Duration{Duration: 24 * time.Hour},
			},
			storageBackend: couchbaseutil.CouchbaseStorageBackendCouchstore,
			clusterSettings: &couchbasev2.AutoCompaction{
				ParallelCompaction: true,
			},
			expected: expected{
				settings: couchbaseutil.BucketAutoCompactionSettings{
					Enabled: true,
					Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
						ViewFragmentationThreshold:     cbViewThreshold(10, 100),
						DatabaseFragmentationThreshold: cbDatabaseThreshold(10, 100),
						AllowedTimePeriod:              cbTimeWindow(true, 0, 0, 23, 59),
						ParallelDBAndViewCompaction:    true,
					},
				},
				purgeInterval: floatPtr(1),
			},
		},
		{
			name: "couchstore with limited crd settings including magma using cluster purge interval",
			crdSettings: &couchbasev2.AutoCompactionSpecBucket{
				ViewFragmentationThreshold:     crdViewThreshold(intPtr(10), nil),
				DatabaseFragmentationThreshold: crdDatabaseThreshold(nil, intPtr(150)),
			},
			storageBackend: couchbaseutil.CouchbaseStorageBackendCouchstore,
			clusterSettings: &couchbasev2.AutoCompaction{
				ParallelCompaction:                    false,
				TombstonePurgeInterval:                &v1.Duration{Duration: 96 * time.Hour},
				MagmaFragmentationThresholdPercentage: intPtr(50),
			},
			expected: expected{
				settings: couchbaseutil.BucketAutoCompactionSettings{
					Enabled: true,
					Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
						ViewFragmentationThreshold:     cbViewThreshold(10, 0),
						DatabaseFragmentationThreshold: cbDatabaseThreshold(0, 150),
						ParallelDBAndViewCompaction:    false,
					},
				},
				purgeInterval: floatPtr(4),
			},
		},
		{
			name: "magma with crd settings",
			crdSettings: &couchbasev2.AutoCompactionSpecBucket{
				MagmaFragmentationThresholdPercentage: intPtr(50),
			},
			storageBackend: couchbaseutil.CouchbaseStorageBackendMagma,
			clusterSettings: &couchbasev2.AutoCompaction{
				ParallelCompaction: true,
			},
			expected: expected{settings: couchbaseutil.BucketAutoCompactionSettings{
				Enabled: true,
				Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
					ParallelDBAndViewCompaction:           true,
					MagmaFragmentationThresholdPercentage: 50,
				},
			}},
		},
		{
			name: "magma missing on bucket crd takes default from cluster settings",
			crdSettings: &couchbasev2.AutoCompactionSpecBucket{
				MagmaFragmentationThresholdPercentage: intPtr(85),
			},
			storageBackend: couchbaseutil.CouchbaseStorageBackendMagma,
			clusterSettings: &couchbasev2.AutoCompaction{
				ParallelCompaction: true,
			},
			expected: expected{settings: couchbaseutil.BucketAutoCompactionSettings{
				Enabled: true,
				Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
					ParallelDBAndViewCompaction:           true,
					MagmaFragmentationThresholdPercentage: 85,
				},
			}},
		},
		{
			name: "magma missing on bucket crd and cluster settings defaults to 50",
			crdSettings: &couchbasev2.AutoCompactionSpecBucket{
				TombstonePurgeInterval: &v1.Duration{Duration: 48 * time.Hour},
			},
			storageBackend: couchbaseutil.CouchbaseStorageBackendMagma,
			clusterSettings: &couchbasev2.AutoCompaction{
				ParallelCompaction: true,
			},
			expected: expected{
				settings: couchbaseutil.BucketAutoCompactionSettings{
					Enabled: true,
					Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
						ParallelDBAndViewCompaction:           true,
						MagmaFragmentationThresholdPercentage: 50,
					},
				},
				purgeInterval: floatPtr(2),
			},
		},
		{
			name: "magma with crd settings ignores couchstore settings",
			crdSettings: &couchbasev2.AutoCompactionSpecBucket{
				ViewFragmentationThreshold:            crdViewThreshold(intPtr(10), intPtr(100)),
				DatabaseFragmentationThreshold:        crdDatabaseThreshold(intPtr(10), intPtr(100)),
				TimeWindow:                            crdTimeWindow(true, "00:00", "23:59"),
				TombstonePurgeInterval:                &v1.Duration{Duration: 36 * time.Hour},
				MagmaFragmentationThresholdPercentage: intPtr(15),
			},
			storageBackend: couchbaseutil.CouchbaseStorageBackendMagma,
			clusterSettings: &couchbasev2.AutoCompaction{
				ParallelCompaction: true,
			},
			expected: expected{
				settings: couchbaseutil.BucketAutoCompactionSettings{
					Enabled: true,
					Settings: &couchbaseutil.AutoCompactionAutoCompactionSettings{
						ParallelDBAndViewCompaction:           true,
						MagmaFragmentationThresholdPercentage: 15,
					},
				},
				purgeInterval: floatPtr(1.5),
			},
		},
		{
			name: "magma with cluster settings but no bucket settings",
			crdSettings: &couchbasev2.AutoCompactionSpecBucket{
				ViewFragmentationThreshold: crdViewThreshold(intPtr(10), intPtr(100)),
			},
			storageBackend: couchbaseutil.CouchbaseStorageBackendMagma,
			clusterSettings: &couchbasev2.AutoCompaction{
				MagmaFragmentationThresholdPercentage: intPtr(75),
				TombstonePurgeInterval:                &v1.Duration{Duration: 120 * time.Hour},
			},
			expected: expected{
				settings: couchbaseutil.BucketAutoCompactionSettings{
					Enabled:  false,
					Settings: nil,
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			settings, purgeInterval := gatherBucketAutoCompactionSettings(testcase.crdSettings, testcase.storageBackend, testcase.clusterSettings)

			if !reflect.DeepEqual(settings, testcase.expected.settings) {
				t.Fatalf("test %s failed, expected settings %v, but got %v", testcase.name, testcase.expected.settings.Settings, settings.Settings)
			}

			if !reflect.DeepEqual(purgeInterval, testcase.expected.purgeInterval) {
				t.Fatalf("test %s failed, expected purgeInterval %v, but got %v", testcase.name, &testcase.expected.purgeInterval, &purgeInterval)
			}
		})
	}
}

func cbViewThreshold(percent, sizeMi int) couchbaseutil.AutoCompactionViewFragmentationThreshold {
	return couchbaseutil.AutoCompactionViewFragmentationThreshold{Percentage: percent, Size: int64(sizeMi * 1024 * 1024)}
}

func cbDatabaseThreshold(percent, sizeMi int) couchbaseutil.AutoCompactionDatabaseFragmentationThreshold {
	return couchbaseutil.AutoCompactionDatabaseFragmentationThreshold{Percentage: percent, Size: int64(sizeMi * 1024 * 1024)}
}

func cbTimeWindow(abortCompaction bool, fromHour, fromMin, toHour, toMin int) *couchbaseutil.AutoCompactionAllowedTimePeriod {
	return &couchbaseutil.AutoCompactionAllowedTimePeriod{
		AbortOutside: abortCompaction,
		FromMinute:   fromMin,
		FromHour:     fromHour,
		ToMinute:     toMin,
		ToHour:       toHour,
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
