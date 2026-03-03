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

// serviceResource represents a collection of services
type serviceResource struct {
	context *context.Context
	// services is the raw output from listing services
	services *v1.ServiceList
}

// NewServiceResource initializes a new service resource
func NewServiceResource(context *context.Context) Resource {
	return &serviceResource{
		context: context,
	}
}

func (r *serviceResource) Kind() string {
	return "Service"
}

// Fetch collects all services as defined by the configuration
func (r *serviceResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}
	r.services, err = r.context.KubeClient.CoreV1().Services(r.context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	return nil
}

func (r *serviceResource) Write(b backend.Backend) error {
	for _, service := range r.services.Items {
		data, err := yaml.Marshal(service)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), service.Name, service.Name+".yaml"), string(data))
	}
	return nil
}

func (r *serviceResource) References() []ResourceReference {
	references := []ResourceReference{}
	for _, service := range r.services.Items {
		references = append(references, newResourceReference(r.Kind(), service.Name))
	}
	return references
}
