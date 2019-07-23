package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// couchbaseClusterResource represents a collection of couchbase clusters
type couchbaseClusterResource struct {
	context *context.Context
	// pods is the raw output from listing couchbaseClusters
	couchbaseClusters []couchbasev2.CouchbaseCluster
}

// NewCouchbaseClusterResource initializes a new pod resource
func NewCouchbaseClusterResource(context *context.Context) Resource {
	return &couchbaseClusterResource{
		context: context,
	}
}

func (r *couchbaseClusterResource) Kind() string {
	return "CouchbaseCluster"
}

// Fetch collects all pods as defined by the configuration
func (r *couchbaseClusterResource) Fetch() error {
	// We have to manually filter here as couchbaseCluster are not labelled and field
	// selectors aren't up to matching based on name (multiple couchbaseClusters that is)
	couchbaseClusters, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseClusters(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// We just want to auto detect couchbaseClusters so use the whole list
	if len(r.context.Config.Clusters) == 0 {
		r.couchbaseClusters = couchbaseClusters.Items
		return nil
	}

	// Create a set of requested couchbaseCluster names
	requested := map[string]interface{}{}
	for _, name := range r.context.Config.Clusters {
		requested[name] = nil
	}

	// Scan the list of couchbaseClusters and select only the requested ones
	r.couchbaseClusters = []couchbasev2.CouchbaseCluster{}
	for _, couchbaseCluster := range couchbaseClusters.Items {
		if _, ok := requested[couchbaseCluster.Name]; ok {
			r.couchbaseClusters = append(r.couchbaseClusters, couchbaseCluster)
		}
	}

	return nil
}

func (r *couchbaseClusterResource) Write(b backend.Backend) error {
	for _, couchbaseCluster := range r.couchbaseClusters {
		data, err := yaml.Marshal(couchbaseCluster)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseCluster.Name, couchbaseCluster.Name+".yaml"), string(data))
	}
	return nil
}

func (r *couchbaseClusterResource) References() []ResourceReference {
	references := []ResourceReference{}
	for _, couchbaseCluster := range r.couchbaseClusters {
		references = append(references, newResourceReference(r.Kind(), couchbaseCluster.Name))
	}
	return references
}
