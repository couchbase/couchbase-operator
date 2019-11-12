package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseRoleResource represents a collection of couchbase clusters
type couchbaseRoleResource struct {
	context *context.Context
	// couchbaseRoles is the raw output from listing couchbaseRoles
	couchbaseRoles []couchbasev2.CouchbaseRole
}

// NewCouchbaseRoleResource initializes a new pod resource
func NewCouchbaseRoleResource(context *context.Context) Resource {
	return &couchbaseRoleResource{
		context: context,
	}
}

func (r *couchbaseRoleResource) Kind() string {
	return "CouchbaseRole"
}

// Fetch collects all roles as defined in the namespace
func (r *couchbaseRoleResource) Fetch() error {
	couchbaseRoles, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseRoles(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	r.couchbaseRoles = couchbaseRoles.Items
	return nil
}

func (r *couchbaseRoleResource) Write(b backend.Backend) error {
	for _, couchbaseRole := range r.couchbaseRoles {
		data, err := yaml.Marshal(couchbaseRole)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseRole.Name, couchbaseRole.Name+".yaml"), string(data))
	}
	return nil
}

func (r *couchbaseRoleResource) References() []ResourceReference {
	return []ResourceReference{}
}
