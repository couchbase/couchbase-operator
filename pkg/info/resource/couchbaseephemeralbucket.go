package resource

import (
	ctx "context"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseEphemeralBucketResource represents a collection of couchbase clusters.
type couchbaseEphemeralBucketResource struct {
	context *context.Context
	// couchbaseEphemeralBuckets is the raw output from listing couchbaseEphemeralBuckets
	couchbaseEphemeralBuckets []couchbasev2.CouchbaseEphemeralBucket
}

// NewCouchbaseEphemeralBucketResource initializes a new pod resource.
func NewCouchbaseEphemeralBucketResource(context *context.Context) Resource {
	return &couchbaseEphemeralBucketResource{
		context: context,
	}
}

func (r *couchbaseEphemeralBucketResource) Kind() string {
	return "CouchbaseEphemeralBucket"
}

// Fetch collects all buckets as defined in the namespace.
func (r *couchbaseEphemeralBucketResource) Fetch() error {
	couchbaseEphemeralBuckets, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseEphemeralBuckets(r.context.Namespace()).List(ctx.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	r.couchbaseEphemeralBuckets = couchbaseEphemeralBuckets.Items

	return nil
}

func (r *couchbaseEphemeralBucketResource) Write(b backend.Backend) error {
	for _, couchbaseEphemeralBucket := range r.couchbaseEphemeralBuckets {
		data, err := yaml.Marshal(couchbaseEphemeralBucket)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseEphemeralBucket.Name, couchbaseEphemeralBucket.Name+".yaml"), string(data))
	}

	return nil
}

func (r *couchbaseEphemeralBucketResource) References() []Reference {
	return []Reference{}
}
