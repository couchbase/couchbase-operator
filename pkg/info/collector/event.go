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
	ctx "context"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	log_meta "github.com/couchbase/couchbase-operator/pkg/info/meta"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// eventCollector represents a collection of events.
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

var eventsCache *v1.EventList

// Fetch collects all events as defined for the resource.
func (r *eventCollector) Fetch(resource resource.Reference) error {
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

	path := util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), "events.yaml")

	if r.resource.Kind() == "CouchbaseCluster" {
		log_meta.SetClusterEvents(r.resource.Name(), path)
	}

	_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), "events.yaml"), string(data))

	return nil
}
