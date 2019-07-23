package main

import (
	"fmt"
	"os"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/collector"
	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	"github.com/couchbase/couchbase-operator/pkg/info/logs"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	_ "k8s.io/client-go/plugin/pkg/client/auth/openstack"
)

var (
	// Define all of the resource types we can collect information for
	resourceInitializers = []resource.ResourceInitializer{
		resource.NewConfigMapResource,
		resource.NewCouchbaseClusterResource,
		resource.NewCouchbaseBucketResource,
		resource.NewCouchbaseEphemeralBucketResource,
		resource.NewCouchbaseMemcachedBucketResource,
		resource.NewCouchbaseReplicationResource,
		resource.NewDeploymentResource,
		resource.NewEndpointResource,
		resource.NewPersistentVolumeClaimResource,
		resource.NewPodDisruptionBudgetResource,
		resource.NewPodResource,
		resource.NewRoleResource,
		resource.NewRoleBindingResource,
		resource.NewSecretResource,
		resource.NewServiceResource,
	}

	// Define all implied sub-resources we can collect information for
	collectorInitializers = []collector.CollectorInitializer{
		collector.NewEventCollector,
		collector.NewLogCollector,
		collector.NewOperatorCollector,
	}

	// Define all cluster scoped resource types
	clusterResourceInitializers = []resource.ResourceInitializer{
		resource.NewClusterRoleResource,
		resource.NewClusterRoleBindingResource,
		resource.NewCustomResourceDefinitionResource,
		resource.NewNodeResource,
		resource.NewPersistentVolumeResource,
	}
)

// harvestSub collects resources implicitly associated with a resource type e.g. logs/events
func harvestSub(context *context.Context, backend backend.Backend, references []resource.ResourceReference) error {
	// For all sub resource types create a handler, fetch and write to the backend
	for _, initializer := range collectorInitializers {
		for _, ref := range references {
			collector := initializer(context)
			if err := collector.Fetch(ref); err != nil {
				fmt.Printf("unable to fetch %s for type %s name %s: %v\n", collector.Kind(), ref.Kind(), ref.Name(), err)
				continue
			}
			if err := collector.Write(backend); err != nil {
				fmt.Printf("unable to write %s for type %s name %s: %v\n", collector.Kind(), ref.Kind(), ref.Name(), err)
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
			fmt.Printf("unable to fetch resources for type %s: %v\n", resource.Kind(), err)
			continue
		}
		if err := resource.Write(backend); err != nil {
			fmt.Printf("unable to write resources for type %s: %v\n", resource.Kind(), err)
			continue
		}
		if err := harvestSub(context, backend, resource.References()); err != nil {
			continue
		}
	}

	return nil
}

// clusterExists is a helper to see if a named cluster exists in the supplied list
func clusterExists(clusters *couchbasev2.CouchbaseClusterList, name string) bool {
	for _, cluster := range clusters.Items {
		if cluster.Name == name {
			return true
		}
	}
	return false
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

	// Before we do anything further ensure we've been correctly configured.
	// Check basic connectivity via the discovery API.  This is plain text
	// TODO: Use the resource list to filter the resources we gather
	_, err := context.KubeClient.Discovery().ServerResources()
	if err != nil {
		fmt.Println("unable to discover cluster resources:", err)
		os.Exit(1)
	}

	// Check we have at least access to the couchbase clusters.  This will test
	// TLS and RBAC settings are correct
	clusters, err := k8s.GetCouchbaseClusters(context)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// This is set to true if there is anything worth collecting
	anythingToCollect := false

	// Check there is something to collect, warn if not but continue so we get
	// debug information about the operator itself
	if len(clusters.Items) == 0 {
		fmt.Println("no CouchbaseCluster resources discovered in name space", context.Namespace())
	} else {
		anythingToCollect = true
	}

	// Finally check to see if any requested CouchbaseClusters exist
	for _, name := range context.Config.Clusters {
		if !clusterExists(clusters, name) {
			fmt.Println("requested cluster", name, "not found in namespace", context.Namespace())
			os.Exit(1)
		}
	}

	// Check that there is an operator deployment running
	if deployment, err := k8s.GetOperatorDeployment(context); err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else if deployment == nil {
		fmt.Println("no Couchbase Operator Deployment resource discovered in name space (check the -operator-image flag is correctly set)", context.Namespace())
	} else {
		anythingToCollect = true
	}

	// Bomb out if there is nothing of interest to collect from
	if !anythingToCollect {
		fmt.Println("nothing to collect in name space", context.Namespace())
		os.Exit(1)
	}

	// Collect logs first, this supports reporting which logs are available to be collected
	// which can then be explicitly collected on the next run.
	if err := logs.Collect(context); err != nil {
		fmt.Println("log collection failed:", err)
	}

	// Initialize the backend file writer, defer closing so it will flush any
	// state in the event of a critical error. From here on any resources are
	// collected on a best-attempt basis e.g. irrespective of RBAC failures.
	backend, err := backend.New(&context.Config)
	if err != nil {
		fmt.Println("unable to initialize backend:", err)
		os.Exit(1)
	}
	defer backend.Close()

	// Store the arguments used to invoke the command
	if err := backend.WriteFile(util.ArchiveName()+"/cmdline", strings.Join(os.Args, " ")); err != nil {
		fmt.Println("failed to archive:", err)
	}

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
		context.NamespaceOverride = "kube-system"
		context.Config.All = true

		// Harvest and restore
		if err := harvest(context, backend, resourceInitializers); err != nil {
			fmt.Println(err)
		}
	}
}
