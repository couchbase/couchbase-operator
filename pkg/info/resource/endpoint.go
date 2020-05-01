package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// endpointResource represents a collection of endpoints
type endpointResource struct {
	context *context.Context
	// endpoints is the raw output from listing endpoints
	endpoints *v1.EndpointsList
}

// NewEndpointResource initializes a new endpoint resource
func NewEndpointResource(context *context.Context) Resource {
	return &endpointResource{
		context: context,
	}
}

func (r *endpointResource) Kind() string {
	return "Endpoints"
}

// Fetch collects all endpoints as defined by the configuration
func (r *endpointResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}
	r.endpoints, err = r.context.KubeClient.CoreV1().Endpoints(r.context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	return nil
}

func (r *endpointResource) Write(b backend.Backend) error {
	for _, endpoint := range r.endpoints.Items {
		data, err := yaml.Marshal(endpoint)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), endpoint.Name, endpoint.Name+".yaml"), string(data))
	}
	return nil
}

func (r *endpointResource) References() []Reference {
	return []Reference{}
}
