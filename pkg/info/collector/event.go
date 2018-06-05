package collector

import (
	"sort"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// eventCollector represents a collection of events
type eventCollector struct {
	context *context.Context
	// events is the raw output from listing events
	events []v1.Event
	// resource keeps a record of what the logs are for
	resource resource.ResourceReference
}

// NewEventCollector initializes a new event resource
func NewEventCollector(context *context.Context) Collector {
	return &eventCollector{
		context: context,
	}
}

func (r *eventCollector) Kind() string {
	return "Event"
}

// Fetch collects all events as defined for the resource
func (r *eventCollector) Fetch(resource resource.ResourceReference) error {
	events, err := r.context.KubeClient.CoreV1().Events(r.context.Config.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// Also build up data structures necessary for sorting
	filteredEvents := []v1.Event{}
	for _, event := range events.Items {
		if event.InvolvedObject.Kind == resource.Kind() && event.InvolvedObject.Name == resource.Name() {
			filteredEvents = append(filteredEvents, event)
		}
	}

	// Sort the timestamps
	sorter := eventSorter{events: filteredEvents}
	sort.Sort(sorter)
	r.events = sorter.events
	r.resource = resource
	return nil
}

type eventSorter struct {
	events []v1.Event
}

func (s eventSorter) Len() int {
	return len(s.events)
}

func (s eventSorter) Swap(i, j int) {
	s.events[i], s.events[j] = s.events[j], s.events[i]
}

func (s eventSorter) Less(i, j int) bool {
	return s.events[i].LastTimestamp.String() < s.events[j].LastTimestamp.String()
}

func (r *eventCollector) Write(b backend.Backend) error {
	if len(r.events) == 0 {
		return nil
	}

	data, err := yaml.Marshal(r.events)
	if err != nil {
		return err
	}

	b.WriteFile(util.ArchivePath(r.context.Config.Namespace, r.resource.Kind(), r.resource.Name(), "events.yaml"), string(data))
	return nil
}
