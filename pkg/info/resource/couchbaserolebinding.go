package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseRoleBindingResource represents a collection of couchbase clusters
type couchbaseRoleBindingResource struct {
	context *context.Context
	// couchbaseRoleBindings is the raw output from listing couchbaseRoleBindings
	couchbaseRoleBindings []couchbasev2.CouchbaseRoleBinding
}

// NewCouchbaseRoleBindingResource initializes a new pod resource.
func NewCouchbaseRoleBindingResource(context *context.Context) Resource {
	return &couchbaseRoleBindingResource{
		context: context,
	}
}

func (r *couchbaseRoleBindingResource) Kind() string {
	return "CouchbaseRoleBinding"
}

// Fetch collects all rolebindings as defined in the namespace.
func (r *couchbaseRoleBindingResource) Fetch() error {
	couchbaseRoleBindings, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseRoleBindings(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	r.couchbaseRoleBindings = couchbaseRoleBindings.Items
	return nil
}

func (r *couchbaseRoleBindingResource) Write(b backend.Backend) error {
	for _, couchbaseRoleBinding := range r.couchbaseRoleBindings {
		data, err := yaml.Marshal(couchbaseRoleBinding)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseRoleBinding.Name, couchbaseRoleBinding.Name+".yaml"), string(data))
	}
	return nil
}

func (r *couchbaseRoleBindingResource) References() []Reference {
	return []Reference{}
}
