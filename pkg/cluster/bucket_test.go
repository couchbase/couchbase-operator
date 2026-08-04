/*
Copyright 2023-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"math"
	"reflect"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	newBuckets := gatherCouchbaseBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, k8sBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)
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

	newBuckets := gatherCouchbaseBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, k8sBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)
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

	newBuckets := gatherCouchbaseBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, k8sBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)
	if newBuckets[0].MagmaSeqTreeDataBlockSize != nil && *(newBuckets[0].MagmaSeqTreeDataBlockSize) != 5555 {
		t.Fatalf("expected MagmaSeqTreeDataBlockSize=5555, found %d", *(newBuckets[0].MagmaSeqTreeDataBlockSize))
	}

	if newBuckets[0].MagmaKeyTreeDataBlockSize != nil && *(newBuckets[0].MagmaKeyTreeDataBlockSize) != 6666 {
		t.Fatalf("expected MagmaKeyTreeDataBlockSize=6666, found %d", *(newBuckets[0].MagmaKeyTreeDataBlockSize))
	}
}

// TestGatherCouchbaseBucketsKVThrottle checks how a bucket's throttle values are gathered
// values the user set are copied through unchanged, values the user left out fall back to the
// server's defaults (0 reserved, max uint64 hard limit), which is what lets the reconciler
// settle instead of updating every loop, nothing is set at all on server versions that don't
// support the feature.
func TestGatherCouchbaseBucketsKVThrottle(t *testing.T) {
	reserved := int64(3000)
	hardLimit := int64(6000)

	testcases := []struct {
		name            string
		supported       bool
		specReserved    *int64
		specHardLimit   *int64
		expectReserved  *uint64
		expectHardLimit *uint64
	}{
		{
			name:            "values set are passed through",
			supported:       true,
			specReserved:    &reserved,
			specHardLimit:   &hardLimit,
			expectReserved:  uint64Ptr(3000),
			expectHardLimit: uint64Ptr(6000),
		},
		{
			name:            "omitted values default to server defaults so the reconciler settles",
			supported:       true,
			specReserved:    nil,
			specHardLimit:   nil,
			expectReserved:  uint64Ptr(0),
			expectHardLimit: uint64Ptr(math.MaxUint64),
		},
		{
			name:            "not set when feature unsupported",
			supported:       false,
			specReserved:    &reserved,
			specHardLimit:   &hardLimit,
			expectReserved:  nil,
			expectHardLimit: nil,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			k8sBucket := []*couchbasev2.CouchbaseBucket{{
				Spec: couchbasev2.CouchbaseBucketSpec{
					Name:              "test",
					MemoryQuota:       resource.NewQuantity(100, resource.BinarySI),
					ThrottleReserved:  tc.specReserved,
					ThrottleHardLimit: tc.specHardLimit,
				},
			}}

			features := SupportedFeatureMap{SupportedKVThrottle: tc.supported}

			newBuckets := gatherCouchbaseBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, k8sBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)

			if !reflect.DeepEqual(newBuckets[0].ThrottleReserved, tc.expectReserved) {
				t.Errorf("ThrottleReserved: expected %v, got %v", derefUint64(tc.expectReserved), derefUint64(newBuckets[0].ThrottleReserved))
			}

			if !reflect.DeepEqual(newBuckets[0].ThrottleHardLimit, tc.expectHardLimit) {
				t.Errorf("ThrottleHardLimit: expected %v, got %v", derefUint64(tc.expectHardLimit), derefUint64(newBuckets[0].ThrottleHardLimit))
			}
		})
	}
}

func uint64Ptr(v uint64) *uint64 { return &v }

func derefUint64(v *uint64) interface{} {
	if v == nil {
		return "nil"
	}

	return *v
}

// TestGatherEphemeralBucketsKVThrottle is the ephemeral bucket version of
// TestGatherCouchbaseBucketsKVThrottle, values the user set are kept, omitted values fall back to
// the server defaults (0 reserved, max uint64 hard limit), and nothing is set below 8.1.
func TestGatherEphemeralBucketsKVThrottle(t *testing.T) {
	reserved := int64(3000)
	hardLimit := int64(6000)

	testcases := []struct {
		name            string
		supported       bool
		specReserved    *int64
		specHardLimit   *int64
		expectReserved  *uint64
		expectHardLimit *uint64
	}{
		{
			name:            "values set are passed through",
			supported:       true,
			specReserved:    &reserved,
			specHardLimit:   &hardLimit,
			expectReserved:  uint64Ptr(3000),
			expectHardLimit: uint64Ptr(6000),
		},
		{
			name:            "omitted values default to server defaults so the reconciler settles",
			supported:       true,
			specReserved:    nil,
			specHardLimit:   nil,
			expectReserved:  uint64Ptr(0),
			expectHardLimit: uint64Ptr(math.MaxUint64),
		},
		{
			name:            "not set when feature unsupported",
			supported:       false,
			specReserved:    &reserved,
			specHardLimit:   &hardLimit,
			expectReserved:  nil,
			expectHardLimit: nil,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			k8sBucket := []*couchbasev2.CouchbaseEphemeralBucket{{
				Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
					Name:              "test",
					MemoryQuota:       resource.NewQuantity(100, resource.BinarySI),
					ThrottleReserved:  tc.specReserved,
					ThrottleHardLimit: tc.specHardLimit,
				},
			}}

			features := SupportedFeatureMap{SupportedKVThrottle: tc.supported}

			newBuckets := gatherEphemeralBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, k8sBucket, nil, nil, &couchbasev2.CouchbaseCluster{})

			if !reflect.DeepEqual(newBuckets[0].ThrottleReserved, tc.expectReserved) {
				t.Errorf("ThrottleReserved: expected %v, got %v", derefUint64(tc.expectReserved), derefUint64(newBuckets[0].ThrottleReserved))
			}

			if !reflect.DeepEqual(newBuckets[0].ThrottleHardLimit, tc.expectHardLimit) {
				t.Errorf("ThrottleHardLimit: expected %v, got %v", derefUint64(tc.expectHardLimit), derefUint64(newBuckets[0].ThrottleHardLimit))
			}
		})
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

// TestGatherBucketsDataServiceRebalanceType checks the per bucket Data Service rebalance type for
// both couchbase and ephemeral buckets, which support the setting identically: values the user set
// are passed through, an omitted value falls back to the server's own default of "auto", and nothing
// is set below 8.1.0 where the field does not exist.
//
// The defaulting is the important part. See TestGatherBucketsRebalanceTypeIsIdempotent for why.
func TestGatherBucketsDataServiceRebalanceType(t *testing.T) {
	// gatherCouchbaseBuckets and gatherEphemeralBuckets take different bucket types and argument
	// lists, so each kind supplies a closure that gathers a single bucket carrying the given spec
	// value and returns the rebalance type the operator would send to the server. Every case below
	// then runs against both kinds, which is what keeps the two paths from drifting apart.
	bucketKinds := []struct {
		name   string
		gather func(SupportedFeatureMap, couchbasev2.DataServiceRebalanceType) string
	}{
		{
			name: "couchbase",
			gather: func(features SupportedFeatureMap, specValue couchbasev2.DataServiceRebalanceType) string {
				k8sBuckets := []*couchbasev2.CouchbaseBucket{{
					Spec: couchbasev2.CouchbaseBucketSpec{
						Name:                     "test",
						MemoryQuota:              resource.NewQuantity(100, resource.BinarySI),
						DataServiceRebalanceType: specValue,
					},
				}}

				gathered := gatherCouchbaseBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, k8sBuckets, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)

				return gathered[0].DataServiceRebalanceType
			},
		},
		{
			name: "ephemeral",
			gather: func(features SupportedFeatureMap, specValue couchbasev2.DataServiceRebalanceType) string {
				k8sBuckets := []*couchbasev2.CouchbaseEphemeralBucket{{
					Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
						Name:                     "test",
						MemoryQuota:              resource.NewQuantity(100, resource.BinarySI),
						DataServiceRebalanceType: specValue,
					},
				}}

				gathered := gatherEphemeralBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, k8sBuckets, nil, nil, &couchbasev2.CouchbaseCluster{})

				return gathered[0].DataServiceRebalanceType
			},
		},
	}

	testcases := []struct {
		name          string
		supported     bool
		specValue     couchbasev2.DataServiceRebalanceType
		expectedValue string
	}{
		{
			name:          "auto is passed through",
			supported:     true,
			specValue:     couchbasev2.DataServiceRebalanceTypeAuto,
			expectedValue: "auto",
		},
		{
			name:          "preferFileBased is passed through",
			supported:     true,
			specValue:     couchbasev2.DataServiceRebalanceTypePreferFileBased,
			expectedValue: "preferFileBased",
		},
		{
			name:          "preferDcp is passed through",
			supported:     true,
			specValue:     couchbasev2.DataServiceRebalanceTypePreferDcp,
			expectedValue: "preferDcp",
		},
		{
			name:          "an omitted value defaults to auto so the reconciler settles",
			supported:     true,
			specValue:     "",
			expectedValue: "auto",
		},
		{
			// Below 8.1.0 the field must stay empty so that FormEncode omits it entirely, rather
			// than sending a key the server does not understand.
			name:          "not set when the feature is unsupported",
			supported:     false,
			specValue:     couchbasev2.DataServiceRebalanceTypePreferDcp,
			expectedValue: "",
		},
	}

	for _, kind := range bucketKinds {
		t.Run(kind.name, func(t *testing.T) {
			for _, tc := range testcases {
				t.Run(tc.name, func(t *testing.T) {
					features := SupportedFeatureMap{SupportedFileBasedRebalance: tc.supported}

					if gathered := kind.gather(features, tc.specValue); gathered != tc.expectedValue {
						t.Errorf("DataServiceRebalanceType: expected %q, got %q", tc.expectedValue, gathered)
					}
				})
			}
		})
	}
}

// TestGatherBucketsRebalanceTypeIsIdempotent guards against a reconcile hot loop.
//
// inspectBuckets compares the whole requested bucket against the bucket the server reports with
// reflect.DeepEqual, and updates it on any difference. The server always reports
// dataServiceRebalanceType, defaulting it to "auto". If the operator left the field empty when the
// user omits it, requested ("") would never equal actual ("auto"), so every reconcile would POST
// every bucket forever. This asserts the gathered value matches what a server would report for a
// bucket the user has not configured.
func TestGatherBucketsRebalanceTypeIsIdempotent(t *testing.T) {
	// What the server reports back for a bucket where the user never set the field.
	const serverReportedDefault = "auto"

	features := SupportedFeatureMap{SupportedFileBasedRebalance: true}

	couchbaseBucket := []*couchbasev2.CouchbaseBucket{{
		Spec: couchbasev2.CouchbaseBucketSpec{
			Name:        "test",
			MemoryQuota: resource.NewQuantity(100, resource.BinarySI),
		},
	}}

	ephemeralBucket := []*couchbasev2.CouchbaseEphemeralBucket{{
		Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
			Name:        "test",
			MemoryQuota: resource.NewQuantity(100, resource.BinarySI),
		},
	}}

	gathered := map[string]string{
		"couchbase": gatherCouchbaseBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, couchbaseBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)[0].DataServiceRebalanceType,
		"ephemeral": gatherEphemeralBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, ephemeralBucket, nil, nil, &couchbasev2.CouchbaseCluster{})[0].DataServiceRebalanceType,
	}

	for bucketType, value := range gathered {
		if value != serverReportedDefault {
			t.Errorf("%s bucket: gathered %q but the server reports %q, so the reconciler would update the bucket on every loop",
				bucketType, value, serverReportedDefault)
		}
	}

	// Gathering twice must produce the same value, a second pass must not drift.
	second := gatherCouchbaseBuckets(features, &couchbasev2.ObjectSelectorAsSelector{}, couchbaseBucket, nil, &couchbasev2.CouchbaseCluster{}, nil, nil)
	if second[0].DataServiceRebalanceType != gathered["couchbase"] {
		t.Errorf("gathering twice was not stable: first %q, second %q", gathered["couchbase"], second[0].DataServiceRebalanceType)
	}
}
