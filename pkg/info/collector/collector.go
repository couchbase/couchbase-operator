package collector

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
)

// Collectors are similar to resources in that they abstract the collection
// and formatting of resources, however they target specific instances.  For
// example this is used to collect events associated with a specific cluster
// instance
type Collector interface {
	// Kind returns the Kubernetes kind of the resource
	Kind() string
	// Fetch collects resource instances for a specific involved object
	Fetch(resource.ResourceReference) error
	// Write writes collected resources to the specified backend
	Write(backend.Backend) error
}

type CollectorInitializer func(*context.Context) Collector
