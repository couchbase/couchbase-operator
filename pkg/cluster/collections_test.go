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
