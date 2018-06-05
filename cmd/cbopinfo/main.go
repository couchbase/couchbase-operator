package main

import (
	"fmt"
	"os"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/collector"
	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
)

var (
	// Define all of the resource types we can collect information for
	resourceInitializers = []resource.ResourceInitializer{
		resource.NewClusterRoleResource,
		resource.NewClusterRoleBindingResource,
		resource.NewCouchbaseClusterResource,
		resource.NewCustomResourceDefinitionResource,
		resource.NewDeploymentResource,
		resource.NewEndpointResource,
		resource.NewPersistentVolumeClaimResource,
		resource.NewPodResource,
		resource.NewRoleResource,
		resource.NewRoleBindingResource,
		resource.NewSecretResource,
		resource.NewServiceResource,
	}

	// Define all implied sub-resources we can collect information for
	collectorInitializers = []collector.CollectorInitializer{
		collector.NewCouchbaseLogCollector,
		collector.NewEventCollector,
		collector.NewLogCollector,
	}

	// Define all cluster scoped resource types
	clusterResourceInitializers = []resource.ResourceInitializer{
		resource.NewNodeResource,
	}
)

// harvestSub collects resources implicitly associated with a resource type e.g. logs/events
func harvestSub(context *context.Context, backend backend.Backend, references []resource.ResourceReference) error {
	// For all sub resource types create a handler, fetch and write to the backend
	for _, initializer := range collectorInitializers {
		for _, ref := range references {
			collector := initializer(context)
			if err := collector.Fetch(ref); err != nil {
				fmt.Printf("unable to fetch %s for type %s name %s: %v", collector.Kind(), ref.Kind(), ref.Name(), err)
				continue
			}
			if err := collector.Write(backend); err != nil {
				fmt.Printf("unable to write %s for type %s name %s: %v", collector.Kind(), ref.Kind(), ref.Name(), err)
				continue
			}
		}
	}

	return nil
}

// harvest collects all resources the context allows and writes to the backend
func harvest(context *context.Context, backend backend.Backend, initializers []resource.ResourceInitializer) error {
	// Main loop, create the resource handler, fetch and write to the backend
	for _, initializer := range initializers {
		resource := initializer(context)
		if err := resource.Fetch(); err != nil {
			fmt.Printf("unable to fetch resources for type %s: %v", resource.Kind(), err)
			continue
		}
		if err := resource.Write(backend); err != nil {
			fmt.Printf("unable to write resources for type %s: %v", resource.Kind(), err)
			continue
		}
		if err := harvestSub(context, backend, resource.References()); err != nil {
			continue
		}
	}

	return nil
}

// main is the entry point of this application
func main() {
	// Parse our configuration
	context := &context.Context{Config: config.Parse()}

	// Allocate and initialize all Kubernetes specific context
	if err := k8s.InitContext(context); err != nil {
		fmt.Println("unable to initialize context:", err)
		os.Exit(1)
	}

	// Initialize the backend file writer, defer closing so it will flush any
	// state in the event of a critical error
	backend, err := backend.New(&context.Config)
	if err != nil {
		fmt.Println("unable to initialize backend:", err)
		os.Exit(1)
	}
	defer backend.Close()

	// Harvest the content defined by the user context
	if err := harvest(context, backend, resourceInitializers); err != nil {
		fmt.Println(err)
	}

	// Harvest unscoped cluster content
	if err := harvest(context, backend, clusterResourceInitializers); err != nil {
		fmt.Println(err)
	}

	// If system collections are allowed harvest from explicitly from that namespace
	if context.Config.System {
		// Switch to collecting everything in the system namespace
		context := context.Copy()
		context.Config.Namespace = "kube-system"
		context.Config.All = true

		// Harvest and restore
		if err := harvest(context, backend, resourceInitializers); err != nil {
			fmt.Println(err)
		}
	}
}
