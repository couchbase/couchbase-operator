package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	"k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// clusterRoleBindingResource represents a collection of clusterRoleBindings
type clusterRoleBindingResource struct {
	context *context.Context
	// clusterRoleBindings is the raw output from listing clusterRoleBindings
	clusterRoleBindings *v1.ClusterRoleBindingList
}

// NewClusterRoleBindingResource initializes a new clusterRoleBinding resource
func NewClusterRoleBindingResource(context *context.Context) Resource {
	return &clusterRoleBindingResource{
		context: context,
	}
}

func (r *clusterRoleBindingResource) Kind() string {
	return "ClusterRoleBinding"
}

// Fetch collects all clusterRoleBindings as defined by the configuration
func (r *clusterRoleBindingResource) Fetch() error {
	var err error
	r.clusterRoleBindings, err = r.context.KubeClient.RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (r *clusterRoleBindingResource) Write(b backend.Backend) error {
	for _, clusterRoleBinding := range r.clusterRoleBindings.Items {
		data, err := yaml.Marshal(clusterRoleBinding)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePathUnscoped(r.Kind(), clusterRoleBinding.Name, clusterRoleBinding.Name+".yaml"), string(data))
	}
	return nil
}

func (r *clusterRoleBindingResource) References() []ResourceReference {
	return []ResourceReference{}
}
