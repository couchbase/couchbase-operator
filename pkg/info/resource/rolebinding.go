package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	"k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RoleBindingResource represents a collection of RoleBindings
type RoleBindingResource struct {
	context *context.Context
	// RoleBindings is the raw output from listing RoleBindings
	RoleBindings *v1.RoleBindingList
}

// NewRoleBindingResource initializes a new RoleBinding resource
func NewRoleBindingResource(context *context.Context) Resource {
	return &RoleBindingResource{
		context: context,
	}
}

func (r *RoleBindingResource) Kind() string {
	return "RoleBinding"
}

// Fetch collects all RoleBindings as defined by the configuration
func (r *RoleBindingResource) Fetch() error {
	var err error
	r.RoleBindings, err = r.context.KubeClient.RbacV1().RoleBindings(r.context.Config.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (r *RoleBindingResource) Write(b backend.Backend) error {
	for _, RoleBinding := range r.RoleBindings.Items {
		data, err := yaml.Marshal(RoleBinding)
		if err != nil {
			return err
		}

		b.WriteFile(util.ArchivePath(r.context.Config.Namespace, r.Kind(), RoleBinding.Name, RoleBinding.Name+".yaml"), string(data))
	}
	return nil
}

func (r *RoleBindingResource) References() []ResourceReference {
	return []ResourceReference{}
}
