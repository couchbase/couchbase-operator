package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseBucketResource represents a collection of couchbase clusters
type couchbaseBucketResource struct {
	context *context.Context
	// couchbaseBuckets is the raw output from listing couchbaseBuckets
	couchbaseBuckets []couchbasev2.CouchbaseBucket
}

// NewCouchbaseBucketResource initializes a new pod resource.
func NewCouchbaseBucketResource(context *context.Context) Resource {
	return &couchbaseBucketResource{
		context: context,
	}
}

func (r *couchbaseBucketResource) Kind() string {
	return "CouchbaseBucket"
}

// Fetch collects all buckets as defined in the namespace.
func (r *couchbaseBucketResource) Fetch() error {
	couchbaseBuckets, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseBuckets(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	r.couchbaseBuckets = couchbaseBuckets.Items
	return nil
}

func (r *couchbaseBucketResource) Write(b backend.Backend) error {
	for _, couchbaseBucket := range r.couchbaseBuckets {
		data, err := yaml.Marshal(couchbaseBucket)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseBucket.Name, couchbaseBucket.Name+".yaml"), string(data))
	}
	return nil
}

func (r *couchbaseBucketResource) References() []Reference {
	return []Reference{}
}
