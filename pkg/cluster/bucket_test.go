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
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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

	newBuckets := gatherCouchbaseBuckets(features, labels.Everything(), k8sBucket, nil)
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
