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
	r.RoleBindings, err = r.context.KubeClient.RbacV1().RoleBindings(r.context.Namespace()).List(metav1.ListOptions{})
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

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), RoleBinding.Name, RoleBinding.Name+".yaml"), string(data))
	}
	return nil
}

func (r *RoleBindingResource) References() []ResourceReference {
	return []ResourceReference{}
}
