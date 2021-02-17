package main

import (
	ctx "context"
	"fmt"
	"os"

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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	_ "k8s.io/client-go/plugin/pkg/client/auth/openstack"
)

var (
	// Add a resource type here for it to be collected.
	resources = []resource.Collector{
		{Resource: &couchbasev2.CouchbaseCluster{}, Scope: resource.ScopeClusterName},
		{Resource: &couchbasev2.CouchbaseBucket{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseEphemeralBucket{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseMemcachedBucket{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseReplication{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseUser{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseGroup{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseRoleBinding{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseBackup{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseBackupRestore{}, Scope: resource.ScopeAll},
		{Resource: &couchbasev2.CouchbaseAutoscaler{}, Scope: resource.ScopeAll},
		{Resource: &corev1.ConfigMap{}, Scope: resource.ScopeCluster},
		{Resource: &corev1.Endpoints{}, Scope: resource.ScopeCluster},
		{Resource: &corev1.Namespace{}, Scope: resource.ScopeNamespace},
		{Resource: &corev1.Node{}, Scope: resource.ScopeAll},
		{Resource: &corev1.PersistentVolume{}, Scope: resource.ScopeAll},
		{Resource: &corev1.PersistentVolumeClaim{}, Scope: resource.ScopeCluster},
		{Resource: &corev1.Pod{}, Scope: resource.ScopeCluster},
		{Resource: &corev1.Secret{}, Scope: resource.ScopeAll},
		{Resource: &corev1.Service{}, Scope: resource.ScopeCluster},
		{Resource: &corev1.ServiceAccount{}, Scope: resource.ScopeAll},
		{Resource: &appsv1.Deployment{}, Scope: resource.ScopeOperatorDeployment},
		{Resource: &rbacv1.ClusterRole{}, Scope: resource.ScopeAll},
		{Resource: &rbacv1.ClusterRoleBinding{}, Scope: resource.ScopeAll},
		{Resource: &rbacv1.Role{}, Scope: resource.ScopeAll},
		{Resource: &rbacv1.RoleBinding{}, Scope: resource.ScopeAll},
		{Resource: &batchv1.Job{}, Scope: resource.ScopeAll},
		{Resource: &batchv1beta1.CronJob{}, Scope: resource.ScopeAll},
		{Resource: &apiextensionsv1.CustomResourceDefinition{}, Scope: resource.ScopeCouchbaseGroup},
		{Resource: &policyv1beta1.PodDisruptionBudget{}, Scope: resource.ScopeCluster},
	}

	// Define all implied sub-resources we can collect information for.
	collectorInitializers = []collector.Initializer{
		collector.NewEventCollector,
		collector.NewLogCollector,
		collector.NewOperatorCollector,
	}
)

// harvestSub collects resources implicitly associated with a resource type e.g. logs/events.
func harvestSub(context *context.Context, backend backend.Backend, references []resource.Reference) {
	// For all sub resource types create a handler, fetch and write to the backend.
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
func main() {
	// Parse our configuration.
	context := &context.Context{Config: config.Parse()}

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
	for _, name := range context.Config.Clusters {
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

	defer backend.Close()

	references := resource.Collect(context, backend, resources)
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

		references := resource.Collect(context, backend, resources)
		harvestSub(context, backend, references)
	}
}
