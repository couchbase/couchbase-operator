package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseGroupResource represents a collection of couchbase clusters
type couchbaseGroupResource struct {
	context *context.Context
	// couchbaseGroups is the raw output from listing couchbaseGroups
	couchbaseGroups []couchbasev2.CouchbaseGroup
}

// NewCouchbaseGroupResource initializes a new pod resource
func NewCouchbaseGroupResource(context *context.Context) Resource {
	return &couchbaseGroupResource{
		context: context,
	}
}

func (r *couchbaseGroupResource) Kind() string {
	return "CouchbaseGroup"
}

// Fetch collects all users as defined in the namespace
func (r *couchbaseGroupResource) Fetch() error {
	couchbaseGroups, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseGroups(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	r.couchbaseGroups = couchbaseGroups.Items
	return nil
}

func (r *couchbaseGroupResource) Write(b backend.Backend) error {
	for _, couchbaseGroup := range r.couchbaseGroups {
		data, err := yaml.Marshal(couchbaseGroup)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseGroup.Name, couchbaseGroup.Name+".yaml"), string(data))
	}
	return nil
}

func (r *couchbaseGroupResource) References() []ResourceReference {
	return []ResourceReference{}
}
