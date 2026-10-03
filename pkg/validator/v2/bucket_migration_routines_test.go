/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package v2

import (
	"strings"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	couchbasefake "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned/fake"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

const routinesDisabledWarning = "because spec.buckets.enableBucketMigrationRoutines is not true"

func TestCheckConstraintUnmanagedBucketMigrationRoutines(t *testing.T) {
	magma := couchbasev2.CouchbaseStorageBackendMagma

	testcases := []struct {
		name        string
		buckets     couchbasev2.Buckets
		expectWarns bool
	}{
		{
			name:        "AnnotationWithoutRoutines",
			buckets:     couchbasev2.Buckets{TargetUnmanagedBucketStorageBackend: &magma},
			expectWarns: true,
		},
		{
			name:    "AnnotationWithRoutines",
			buckets: couchbasev2.Buckets{TargetUnmanagedBucketStorageBackend: &magma, EnableBucketMigrationRoutines: true},
		},
		{
			name:    "AnnotationOnManagedBuckets",
			buckets: couchbasev2.Buckets{Managed: true, TargetUnmanagedBucketStorageBackend: &magma},
		},
		{
			name:    "NoAnnotation",
			buckets: couchbasev2.Buckets{},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			cluster := &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{Buckets: testcase.buckets}}

			warnings, err := checkConstraintUnmanagedBucketMigrationRoutines(nil, cluster)
			if err != nil {
				t.Fatalf("unexpected error: %s", err.Error())
			}

			if (len(warnings) != 0) != testcase.expectWarns {
				t.Errorf("expected warnings %v but got %v", testcase.expectWarns, warnings)
			}
		})
	}
}

func TestCheckChangeConstraintsBucketMigrationRoutines(t *testing.T) {
	for _, routines := range []bool{false, true} {
		cluster := &couchbasev2.CouchbaseCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default"},
			Spec: couchbasev2.ClusterSpec{
				Image:   "couchbase/server:7.6.2",
				Buckets: couchbasev2.Buckets{Managed: true, EnableBucketMigrationRoutines: routines},
			},
		}

		prev := &couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{Name: "bucket", Namespace: "default"},
			Spec:       couchbasev2.CouchbaseBucketSpec{StorageBackend: couchbasev2.CouchbaseStorageBackendCouchstore},
		}

		curr := prev.DeepCopy()
		curr.Spec.StorageBackend = couchbasev2.CouchbaseStorageBackendMagma

		v := types.New(k8sfake.NewSimpleClientset(), couchbasefake.NewSimpleClientset(cluster, prev), nil)

		warnings, err := CheckChangeConstraintsBucket(v, prev, curr, cluster)
		if err != nil {
			t.Fatalf("routines=%v: a backend change should not be rejected, got: %s", routines, err.Error())
		}

		if warned := strings.Contains(strings.Join(warnings, "; "), routinesDisabledWarning); warned == routines {
			t.Errorf("routines=%v: expected warning %v but got %v", routines, !routines, warnings)
		}
	}
}
