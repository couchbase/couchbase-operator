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
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseUserResource represents a collection of couchbase clusters
type couchbaseUserResource struct {
	context *context.Context
	// couchbaseUsers is the raw output from listing couchbaseUsers
	couchbaseUsers []couchbasev2.CouchbaseUser
}

// NewCouchbaseUserResource initializes a new pod resource
func NewCouchbaseUserResource(context *context.Context) Resource {
	return &couchbaseUserResource{
		context: context,
	}
}

func (r *couchbaseUserResource) Kind() string {
	return "CouchbaseUser"
}

// Fetch collects all users as defined in the namespace
func (r *couchbaseUserResource) Fetch() error {
	couchbaseUsers, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseUsers(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	r.couchbaseUsers = couchbaseUsers.Items
	return nil
}

func (r *couchbaseUserResource) Write(b backend.Backend) error {
	for _, couchbaseUser := range r.couchbaseUsers {
		data, err := yaml.Marshal(couchbaseUser)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseUser.Name, couchbaseUser.Name+".yaml"), string(data))
	}
	return nil
}

func (r *couchbaseUserResource) References() []ResourceReference {
	return []ResourceReference{}
}
