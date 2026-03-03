/*
Copyright 2019-Present Couchbase, Inc.

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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceAccountResource represents a collection of ServiceAccounts.
type ServiceAccountResource struct {
	context *context.Context
	// ServiceAccounts is the raw output from listing ServiceAccounts
	ServiceAccounts *v1.ServiceAccountList
}

// NewServiceAccountResource initializes a new ServiceAccount resource.
func NewServiceAccountResource(context *context.Context) Resource {
	return &ServiceAccountResource{
		context: context,
	}
}

func (r *ServiceAccountResource) Kind() string {
	return "ServiceAccount"
}

// Fetch collects all ServiceAccounts as defined by the configuration.
func (r *ServiceAccountResource) Fetch() error {
	var err error

	r.ServiceAccounts, err = r.context.KubeClient.CoreV1().ServiceAccounts(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (r *ServiceAccountResource) Write(b backend.Backend) error {
	for _, ServiceAccount := range r.ServiceAccounts.Items {
		data, err := yaml.Marshal(ServiceAccount)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), ServiceAccount.Name, ServiceAccount.Name+".yaml"), string(data))
	}

	return nil
}

func (r *ServiceAccountResource) References() []Reference {
	return []Reference{}
}
