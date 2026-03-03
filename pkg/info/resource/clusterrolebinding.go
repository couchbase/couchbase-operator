/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// clusterRoleBindingResource represents a collection of clusterRoleBindings.
type clusterRoleBindingResource struct {
	context *context.Context
	// clusterRoleBindings is the raw output from listing clusterRoleBindings
	clusterRoleBindings *v1.ClusterRoleBindingList
}

// NewClusterRoleBindingResource initializes a new clusterRoleBinding resource.
func NewClusterRoleBindingResource(context *context.Context) Resource {
	return &clusterRoleBindingResource{
		context: context,
	}
}

func (r *clusterRoleBindingResource) Kind() string {
	return "ClusterRoleBinding"
}

// Fetch collects all clusterRoleBindings as defined by the configuration.
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

func (r *clusterRoleBindingResource) References() []Reference {
	return []Reference{}
}
