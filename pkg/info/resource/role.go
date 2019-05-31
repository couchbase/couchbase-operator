package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	"k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RoleResource represents a collection of Roles
type RoleResource struct {
	context *context.Context
	// Roles is the raw output from listing Roles
	Roles *v1.RoleList
}

// NewRoleResource initializes a new Role resource
func NewRoleResource(context *context.Context) Resource {
	return &RoleResource{
		context: context,
	}
}

func (r *RoleResource) Kind() string {
	return "Role"
}

// Fetch collects all Roles as defined by the configuration
func (r *RoleResource) Fetch() error {
	var err error
	r.Roles, err = r.context.KubeClient.RbacV1().Roles(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (r *RoleResource) Write(b backend.Backend) error {
	for _, Role := range r.Roles.Items {
		data, err := yaml.Marshal(Role)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), Role.Name, Role.Name+".yaml"), string(data))
	}
	return nil
}

func (r *RoleResource) References() []ResourceReference {
	return []ResourceReference{}
}
