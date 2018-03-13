package decoder

import (
	"fmt"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"

	"k8s.io/client-go/kubernetes/scheme"
)

// Decodes a raw JSON string to a CouchbaseCluster object
//
// Before calling this function ensure that you have registered the
// CouchbaseCluster object as a Scheme with the client. This can be
// done with by calling AddToScheme(scheme.Scheme) for the CRD.
func DecodeCouchbaseCluster(raw []byte) (*api.CouchbaseCluster, error) {
	decoder := scheme.Codecs.UniversalDeserializer()
	obj, kind, err := decoder.Decode(raw, nil, nil)
	if err != nil {
		return nil, err
	}

	if kind.Kind != "CouchbaseCluster" {
		return nil, fmt.Errorf("Spec is not a CouchbaseCluster")
	}

	if rv, ok := obj.(*api.CouchbaseCluster); ok {
		return rv, nil
	}

	return nil, fmt.Errorf("Could not convert object to CouchbaseCluster")
}
