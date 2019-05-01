package decoder

import (
	"encoding/json"
	"fmt"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
)

// Decodes a raw JSON string to a CouchbaseCluster object
//
// Before calling this function ensure that you have registered the
// CouchbaseCluster object as a Scheme with the client. This can be
// done with by calling AddToScheme(scheme.Scheme) for the CRD.
func DecodeCouchbaseCluster(raw []byte) (*couchbasev1.CouchbaseCluster, error) {
	// Before decoding, run this through CRD validation, the output is
	// far better than anything the decoder can provide.  This also has
	// the effect of looking more like the output if we were to fire the
	// CR directly at the API server.
	j, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(j, u); err != nil {
		return nil, err
	}
	if err := k8sutil.ValidateCRD(u); err != nil {
		return nil, err
	}

	decoder := scheme.Codecs.UniversalDeserializer()
	obj, kind, err := decoder.Decode(raw, nil, nil)
	if err != nil {
		return nil, err
	}

	if kind.Kind != "CouchbaseCluster" {
		return nil, fmt.Errorf("spec is not a CouchbaseCluster")
	}

	if rv, ok := obj.(*couchbasev1.CouchbaseCluster); ok {
		return rv, nil
	}

	return nil, fmt.Errorf("could not convert object to CouchbaseCluster")
}

func EncodeCouchbaseCluster(resource *couchbasev1.CouchbaseCluster) ([]byte, error) {
	yaml, err := yaml.Marshal(resource)
	if err != nil {
		return nil, err
	}
	return yaml, nil
}
