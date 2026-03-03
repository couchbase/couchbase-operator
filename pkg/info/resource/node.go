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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nodeResource represents a collection of nodes
type nodeResource struct {
	context *context.Context
	// nodes is the raw output from listing nodes
	nodes *v1.NodeList
}

// NewNodeResource initializes a new node resource
func NewNodeResource(context *context.Context) Resource {
	return &nodeResource{
		context: context,
	}
}

func (r *nodeResource) Kind() string {
	return "Node"
}

// Fetch collects all nodes as defined by the configuration
func (r *nodeResource) Fetch() error {
	var err error
	r.nodes, err = r.context.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (r *nodeResource) Write(b backend.Backend) error {
	for _, node := range r.nodes.Items {
		data, err := yaml.Marshal(node)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePathUnscoped(r.Kind(), node.Name, node.Name+".yaml"), string(data))
	}
	return nil
}

func (r *nodeResource) References() []ResourceReference {
	references := []ResourceReference{}
	for _, node := range r.nodes.Items {
		references = append(references, newResourceReference(r.Kind(), node.Name))
	}
	return references
}
