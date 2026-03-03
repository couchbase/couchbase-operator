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
	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// Reference contains data so other modules can extract data associated
// with discovered associated with resource instances.
type Reference interface {
	// Kind is the Kubernetes kind of a resource
	Kind() string
	// Name is the name of the resource
	Name() string
}

// Resource abstracts away the details of handling different Kubernetes
// resource types.  It should be used to collect a class of resources
// from a global or namespaced scope.  The scope may be further limited
// by context specific parameters.
type Resource interface {
	// Kind returns the Kubernetes kind of the resource
	Kind() string
	// Fetch requests a list of the required resource type from Kubernetes
	Fetch() error
	// Write writes the resources to the requested backend
	Write(backend.Backend) error
	// References returns a list of resources that were discovered by a Fetch
	References() []Reference
}

// Initializer is a function signature used to get resource handlers.
type Initializer func(*context.Context) Resource

// getResourceSelector returns a label selector which will scope the resources we
// can collect in the requested namespace based on configuration directives.
func getResourceSelector(c *config.Configuration, all bool) (labels.Selector, error) {
	// Collect everything we can
	if all {
		return labels.Everything(), nil
	}

	// Collect requirements for the label selector
	requirements := []labels.Requirement{}

	// By default we only collect items labeled as couchbase
	req, err := labels.NewRequirement(constants.LabelApp, selection.Equals, []string{constants.App})
	if err != nil {
		return nil, err
	}

	requirements = append(requirements, *req)

	// If we specify specific clusters add this requirement
	if len(c.Clusters) != 0 {
		req, err := labels.NewRequirement(constants.LabelCluster, selection.In, c.Clusters)
		if err != nil {
			return nil, err
		}

		requirements = append(requirements, *req)
	}

	// Create and return the selector
	selector := labels.NewSelector()

	return selector.Add(requirements...), nil
}

// GetResourceSelector returns a label selector which will scope the resources we
// can collect in the requested namespace based on configuration directives.
func GetResourceSelector(c *config.Configuration) (labels.Selector, error) {
	return getResourceSelector(c, c.All)
}

// GetResourceSelectorForCluster returns a label selector which will scope the resources we
// can collect in the requested namespace based on configuration directives.  Explicitly
// limits scope to the cluster and ignores the --all flag.
func GetResourceSelectorForCluster(c *config.Configuration) (labels.Selector, error) {
	return getResourceSelector(c, false)
}
