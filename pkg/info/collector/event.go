package collector

import (
	ctx "context"
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	log_meta "github.com/couchbase/couchbase-operator/pkg/info/meta"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"github.com/couchbase/couchbase-operator/pkg/util/eventcollectorutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// eventCollector represents a collection of events.
type eventCollector struct {
	context *context.Context
	// events is the raw output from listing events
	events []v1.Event

	// eventBuffers contains the buffer dumps from the event collector
	eventBuffers map[string][]byte

	// resource keeps a record of what the logs are for
	resource resource.Reference
}

// NewEventCollector initializes a new event resource.
func NewEventCollector(context *context.Context) Collector {
	return &eventCollector{
		context:      context,
		eventBuffers: map[string][]byte{},
	}
}

func (r *eventCollector) Kind() string {
	return "Event"
}

var eventsCache *v1.EventList

// Fetch collects all events as defined for the resource.
func (r *eventCollector) Fetch(resource resource.Reference) error {
	r.resource = resource

	if resource.IsEventCollector() {
		return r.fetchEventsFromEventCollector()
	}

	if eventsCache == nil {
		var err error

		if eventsCache, err = r.context.KubeClient.CoreV1().Events("").List(ctx.Background(), metav1.ListOptions{}); err != nil {
			return err
		}
	}

	events := []v1.Event{}

	for _, event := range eventsCache.Items {
		if event.InvolvedObject.Kind == resource.Kind() && event.InvolvedObject.Namespace == r.context.Namespace() && event.InvolvedObject.Name == resource.Name() {
			events = append(events, event)
		}
	}

	r.events = events

	return nil
}

func (r *eventCollector) fetchEventsFromEventCollector() error {
	pod, err := k8s.GetPod(r.context, r.resource.Kind(), r.resource.Name())
	if err != nil {
		return err
	}

	return r.collectBuffers(pod)
}

func (r *eventCollector) collectBuffers(pod *v1.Pod) error {
	port, err := netutil.GetFreePort()
	if err != nil {
		return fmt.Errorf("unable to allocate port: %w", err)
	}

	pf := portforward.PortForwarder{
		Config:    r.context.KubeConfig,
		Client:    r.context.KubeClient,
		Namespace: r.context.Namespace(),
		Pod:       pod.Name,
		Port:      port + ":" + r.context.Config.EventCollectorPort,
	}

	if err := pf.ForwardPorts(); err != nil {
		return fmt.Errorf("unable to forward port %s for pod %s", r.context.Config.OperatorRestPort, pod.Name)
	}

	defer pf.Close()

	// The port-forwarder is a bit chatty on error, but we can shut it up
	restorer := portforward.Silent()
	defer restorer()

	client := eventcollectorutil.NewLocalClient(port)

	buff, err := client.GetBuffer()
	if err == nil {
		r.eventBuffers["buffer"] = buff
	}

	dumps, err := client.GetDumps()
	if err == nil {
		for _, d := range dumps {
			if d.Status != "Complete" {
				continue
			}

			dump, err := client.GetDump(d)

			if err == nil {
				r.eventBuffers[d.Name] = dump
			}
		}
	}

	return nil
}

func (r *eventCollector) Write(b backend.Backend) error {
	if len(r.eventBuffers) != 0 {
		return r.writeEventBuffers(b)
	}

	if len(r.events) == 0 {
		return nil
	}

	data, err := yaml.Marshal(r.events)
	if err != nil {
		return err
	}

	path := util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), "events.yaml")

	if r.resource.Kind() == "CouchbaseCluster" {
		log_meta.SetClusterEvents(r.resource.Name(), path)
	}

	_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), "events.yaml"), string(data))

	return nil
}

func (r *eventCollector) writeEventBuffers(b backend.Backend) error {
	for name, data := range r.eventBuffers {
		if len(data) > 0 {
			return b.WriteFile(util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), name+".json"), string(data))
		}
	}

	return nil
}
