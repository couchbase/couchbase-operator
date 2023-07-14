package command

import (
	ctx "context"
	"fmt"
	"os"
	"regexp"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/collector"
	"github.com/couchbase/couchbase-operator/pkg/info/config"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	"github.com/couchbase/couchbase-operator/pkg/info/logs"
	log_meta "github.com/couchbase/couchbase-operator/pkg/info/meta"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// harvestSub collects resources implicitly associated with a resource type e.g. logs/events.
func harvestSub(context *context.Context, backend backend.Backend, references []resource.Reference) {
	// For all sub resource types create a handler, fetch and write to the backend.
	for _, initializer := range collector.Initializers {
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
}

// clusterExists is a helper to see if a named cluster exists in the supplied list.
func clusterExists(clusters *couchbasev2.CouchbaseClusterList, name string) bool {
	for _, cluster := range clusters.Items {
		if cluster.Name == name {
			return true
		}
	}

	return false
}

// main is the entry point of this application.
func collect(c config.Configuration) {
	// Parse our configuration.
	context := &context.Context{Config: c}

	// Allocate and initialize all Kubernetes specific context.
	if err := k8s.InitContext(context); err != nil {
		fmt.Println("unable to initialize context:", err)
		os.Exit(1)
	}

	// Before we do anything further ensure we've been correctly configured.
	// Check basic connectivity via the discovery API.  This is plain text.
	// Use the simplest possible call here, the discovery API has a tendency
	// to crap out if some aggregated controller is not working.
	if _, err := context.KubeClient.Discovery().ServerVersion(); err != nil {
		fmt.Println("unable to connect to kubernetes cluster:", err)
		os.Exit(1)
	}

	// Check the namespace exists.
	if _, err := context.KubeClient.CoreV1().Namespaces().Get(ctx.Background(), context.Namespace(), metav1.GetOptions{}); err != nil {
		if errors.IsNotFound(err) {
			fmt.Println("namespace", context.Namespace(), "does not exist")
			os.Exit(1)
		}

		fmt.Println(err)
		os.Exit(1)
	}

	// Check we have at least access to the couchbase clusters.  This will test
	// TLS and RBAC settings are correct.
	clusters, err := k8s.GetCouchbaseClusters(context)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// This is set to true if there is anything worth collecting.
	anythingToCollect := false

	// Check there is something to collect, warn if not but continue so we get
	// debug information about the operator itself.
	if len(clusters.Items) == 0 {
		fmt.Println("no CouchbaseCluster resources discovered in name space", context.Namespace())
	} else {
		anythingToCollect = true
	}

	// Finally check to see if any requested CouchbaseClusters exist.
	for _, name := range context.Config.Clusters.Values {
		if !clusterExists(clusters, name) {
			fmt.Println("requested cluster", name, "not found in namespace", context.Namespace())
			os.Exit(1)
		}
	}

	// Check that there is an operator deployment running.
	hasOperator, err := k8s.HasOperatorDeployment(context)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if !hasOperator {
		fmt.Println("no Couchbase Operator Deployment resource discovered in name space (check the -operator-image flag is correctly set)", context.Namespace())
	} else {
		anythingToCollect = true
	}

	// Bomb out if there is nothing of interest to collect from.
	if !anythingToCollect {
		fmt.Println("nothing to collect in name space", context.Namespace())
		os.Exit(1)
	}

	if err := log_meta.Init(context, os.Args); err != nil {
		fmt.Println("unable to initialize logs metadata", err)
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

	defer func() {
		backend.Close()
		// If log upload is specified, attempt to upload the collected logs
		if context.Config.Upload {
			if len(context.Config.Customer) > 50 {
				fmt.Println("Customer name invalid")
				os.Exit(1)
			}

			matched, err := regexp.Match("[A-Za-z0-9_.-]", []byte(context.Config.Customer))
			if err != nil {
				fmt.Println("Error parsing customer name: ", err)
				os.Exit(1)
			}

			if !matched {
				fmt.Print("Customer name invalid.")
				os.Exit(1)
			}

			if len(context.Config.Ticket) > 7 {
				fmt.Println("Ticket number invalid")
				os.Exit(1)
			}

			matched, err = regexp.Match("^[0-9]*$", []byte(context.Config.Ticket))

			if err != nil {
				fmt.Println("Error parsing ticket number ", err)
				os.Exit(1)
			}

			if !matched {
				fmt.Print("Ticket number invalid")
				os.Exit(1)
			}

			address := context.Config.UploadHost + "/" + context.Config.Customer + "/" + context.Config.Ticket + "/"
			proxy := context.Config.UploadProxy
			payload := util.ArchiveName() + ".tar.gz"
			upload(address, payload, proxy)
		}
	}()

	references := resource.Collect(context, backend, collector.Resources)
	harvestSub(context, backend, references)

	// Store the logs metadata
	metadata, err := log_meta.ToJSON()
	if err != nil {
		fmt.Println("warning: unable to add logs metadata:", err)
	} else if err := backend.WriteFile(util.ArchiveName()+"/metadata.json", metadata); err != nil {
		fmt.Println("failed to archive:", err)
	}

	// If system collections are allowed harvest from explicitly from that namespace.
	if context.Config.System {
		// Switch to collecting everything in the system namespace.
		context := context.Copy()
		context.NamespaceOverride = "kube-system"
		context.Config.All = true

		references := resource.Collect(context, backend, collector.Resources)
		harvestSub(context, backend, references)
	}
}
