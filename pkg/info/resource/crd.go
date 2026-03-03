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
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// crdResource represents a collection of crds.
type crdResource struct {
	context *context.Context
	// crds is the raw output from listing crds
	crds []v1beta1.CustomResourceDefinition
}

// NewCustomResourceDefinitionResource initializes a new crd resource.
func NewCustomResourceDefinitionResource(context *context.Context) Resource {
	return &crdResource{
		context: context,
	}
}

func (r *crdResource) Kind() string {
	return "CustomResourceDefinition"
}

// Fetch collects all crds as defined by the configuration.
func (r *crdResource) Fetch() error {
	// Fetch all CRDs in the system
	crds, err := r.context.KubeExtClient.ApiextensionsV1beta1().CustomResourceDefinitions().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// Filter out only ones defined by couchbase
	r.crds = []v1beta1.CustomResourceDefinition{}

	for _, crd := range crds.Items {
		if strings.HasSuffix(crd.Name, ".couchbase.com") {
			r.crds = append(r.crds, crd)
		}
	}

	return nil
}

func (r *crdResource) Write(b backend.Backend) error {
	for _, crd := range r.crds {
		data, err := yaml.Marshal(crd)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePathUnscoped(r.Kind(), crd.Name, crd.Name+".yaml"), string(data))
	}

	return nil
}

func (r *crdResource) References() []Reference {
	return []Reference{}
}
