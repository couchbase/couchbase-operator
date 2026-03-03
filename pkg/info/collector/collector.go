/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package collector

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
)

// Collectors are similar to resources in that they abstract the collection
// and formatting of resources, however they target specific instances.  For
// example this is used to collect events associated with a specific cluster
// instance.
type Collector interface {
	// Kind returns the Kubernetes kind of the resource
	Kind() string
	// Fetch collects resource instances for a specific involved object
	Fetch(resource.Reference) error
	// Write writes collected resources to the specified backend
	Write(backend.Backend) error
}

type Initializer func(*context.Context) Collector
