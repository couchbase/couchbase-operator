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
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
)

func TestCollectionHistoryAnnotation(t *testing.T) {
	collection := couchbasev2.CouchbaseCollection{}

	collection.Annotations = make(map[string]string)
	collection.Annotations["cao.couchbase.com/history"] = "true"

	err := annotations.Populate(&collection.Spec, collection.Annotations)
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}

	if !*(collection.Spec.History) {
		t.Fatal("history not set to true")
	}

	collection.Spec = couchbasev2.CouchbaseCollectionSpec{}
	collection.Annotations["cao.couchbase.com/history"] = "false"

	err = annotations.Populate(&collection.Spec, collection.Annotations)
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}

	if *(collection.Spec.History) {
		t.Fatal("history not set to false")
	}
}
