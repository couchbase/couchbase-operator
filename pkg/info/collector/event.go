package collector

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/ghodss/yaml"

	"k8s.io/api/core/v1"
)

// eventCollector represents a collection of events
type eventCollector struct {
	context *context.Context
	// events is the raw output from listing events
	events []v1.Event
	// resource keeps a record of what the logs are for
	resource resource.Reference
}

// NewEventCollector initializes a new event resource.
func NewEventCollector(context *context.Context) Collector {
	return &eventCollector{
		context: context,
	}
}

func (r *eventCollector) Kind() string {
	return "Event"
}

// Fetch collects all events as defined for the resource.
func (r *eventCollector) Fetch(resource resource.Reference) error {
	events, err := k8sutil.GetEventsForResource(r.context.KubeClient, r.context.Namespace(), resource.Kind(), resource.Name())
	if err != nil {
		return err
	}

	r.events = events
	r.resource = resource

	return nil
}

func (r *eventCollector) Write(b backend.Backend) error {
	if len(r.events) == 0 {
		return nil
	}

	data, err := yaml.Marshal(r.events)
	if err != nil {
		return err
	}

	_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), "events.yaml"), string(data))

	return nil
}
