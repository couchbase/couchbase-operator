package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// clusterRoleResource represents a collection of clusterRoles.
type clusterRoleResource struct {
	context *context.Context
	// clusterRoles is the raw output from listing clusterRoles
	clusterRoles *v1.ClusterRoleList
}

// NewClusterRoleResource initializes a new clusterRole resource.
func NewClusterRoleResource(context *context.Context) Resource {
	return &clusterRoleResource{
		context: context,
	}
}

func (r *clusterRoleResource) Kind() string {
	return "ClusterRole"
}

// Fetch collects all clusterRoles as defined by the configuration.
func (r *clusterRoleResource) Fetch() error {
	var err error

	r.clusterRoles, err = r.context.KubeClient.RbacV1().ClusterRoles().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (r *clusterRoleResource) Write(b backend.Backend) error {
	for _, clusterRole := range r.clusterRoles.Items {
		data, err := yaml.Marshal(clusterRole)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePathUnscoped(r.Kind(), clusterRole.Name, clusterRole.Name+".yaml"), string(data))
	}

	return nil
}

func (r *clusterRoleResource) References() []Reference {
	return []Reference{}
}
