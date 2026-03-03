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

// couchbaseMemcachedBucketResource represents a collection of couchbase clusters.
type couchbaseMemcachedBucketResource struct {
	context *context.Context
	// couchbaseMemcachedBuckets is the raw output from listing couchbaseMemcachedBuckets
	couchbaseMemcachedBuckets []couchbasev2.CouchbaseMemcachedBucket
}

// NewCouchbaseMemcachedBucketResource initializes a new pod resource.
func NewCouchbaseMemcachedBucketResource(context *context.Context) Resource {
	return &couchbaseMemcachedBucketResource{
		context: context,
	}
}

func (r *couchbaseMemcachedBucketResource) Kind() string {
	return "CouchbaseMemcachedBucket"
}

// Fetch collects all buckets as defined in the namespace.
func (r *couchbaseMemcachedBucketResource) Fetch() error {
	couchbaseMemcachedBuckets, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseMemcachedBuckets(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	r.couchbaseMemcachedBuckets = couchbaseMemcachedBuckets.Items

	return nil
}

func (r *couchbaseMemcachedBucketResource) Write(b backend.Backend) error {
	for _, couchbaseMemcachedBucket := range r.couchbaseMemcachedBuckets {
		data, err := yaml.Marshal(couchbaseMemcachedBucket)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseMemcachedBucket.Name, couchbaseMemcachedBucket.Name+".yaml"), string(data))
	}

	return nil
}

func (r *couchbaseMemcachedBucketResource) References() []Reference {
	return []Reference{}
}
