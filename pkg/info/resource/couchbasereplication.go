package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseReplicationResource represents a collection of couchbase clusters
type couchbaseReplicationResource struct {
	context *context.Context
	// couchbaseReplications is the raw output from listing couchbaseReplications
	couchbaseReplications []couchbasev2.CouchbaseReplication
}

// NewCouchbaseReplicationResource initializes a new pod resource.
func NewCouchbaseReplicationResource(context *context.Context) Resource {
	return &couchbaseReplicationResource{
		context: context,
	}
}

func (r *couchbaseReplicationResource) Kind() string {
	return "CouchbaseReplication"
}

// Fetch collects all replications as defined in the namespace.
func (r *couchbaseReplicationResource) Fetch() error {
	couchbaseReplications, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseReplications(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	r.couchbaseReplications = couchbaseReplications.Items
	return nil
}

func (r *couchbaseReplicationResource) Write(b backend.Backend) error {
	for _, couchbaseReplication := range r.couchbaseReplications {
		data, err := yaml.Marshal(couchbaseReplication)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseReplication.Name, couchbaseReplication.Name+".yaml"), string(data))
	}
	return nil
}

func (r *couchbaseReplicationResource) References() []Reference {
	return []Reference{}
}
