package controller

import (
	"context"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/validationrunner"

	"k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// log is the logger for this module.
var log = logf.Log.WithName("controller")

// CouchbaseClusterReconciler is a reconciler object that implements to Reconciler interface
// as defined by the controller-runtime.
type CouchbaseClusterReconciler struct {
	client            client.Client
	clusters          *ManagedClusters
	clusterConfig     cluster.Config
	operatorStartTime time.Time
}

// Reconcile is triggered when an event occurs on a watched resource.
// TODO: The reconcile.Result allows events to be requeued which *may*
// allow us to do away with the separate go routine sitting in a loop,
// or it may just fill up with events due to status updates...
//
//nolint:gocognit
func (r *CouchbaseClusterReconciler) Reconcile(_ context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.V(2).Info("Received couchbase cluster event", "cluster", request.NamespacedName)

	validationFailed := false

	var validationErrors []string

	// By using the requeue mechanism we can get a periodic and synchronous reconcile.
	requeueResult := reconcile.Result{
		RequeueAfter: 10 * time.Second,
	}

	// Check the status of the cluster object.
	couchbase := &couchbasev2.CouchbaseCluster{}
	if err := r.client.Get(context.Background(), request.NamespacedName, couchbase); err != nil {
		if errors.IsNotFound(err) {
			// Cluster deleted
			cluster, ok := r.clusters.Load(request.NamespacedName.String())
			if !ok {
				log.V(2).Info("Cluster deleted but unknown", "cluster", request.NamespacedName)
				return reconcile.Result{}, nil
			}

			log.V(2).Info("Deleting cluster", "cluster", request.NamespacedName)

			cluster.Delete()
			r.clusters.Delete(request.NamespacedName.String())

			return reconcile.Result{}, nil
		}

		log.Error(err, "Failed to get CouchbaseCluster", "cluster", request.NamespacedName)

		return reconcile.Result{}, err
	}

	results, err := validationrunner.CheckCouchbaseClusterResource(couchbase)
	if err != nil {
		return reconcile.Result{}, err
	}

	if results != nil {
		log.V(1).Info("Validation warnings.", "warnings", results)
	}

	// See if we know about the cluster already.
	c, ok := r.clusters.Load(request.NamespacedName.String())
	if !ok {
		// Cluster created or detected during a restart, start a new management routine.
		log.V(2).Info("Creating cluster", "cluster", request.NamespacedName)

		c, err := cluster.New(r.clusterConfig, couchbase)
		if err != nil {
			log.Error(err, "Failed to create Couchbase cluster", "cluster", request.NamespacedName)
			return reconcile.Result{}, err
		}

		errs := validationrunner.CheckConstraints(c)

		for _, err := range errs {
			log.Error(err, "Validation failed", "cluster", request.NamespacedName)
			validationErrors = append(validationErrors, err.Error())
		}

		c.RunReconcile(r.operatorStartTime)

		r.clusters.Store(request.NamespacedName.String(), c)

		return requeueResult, nil
	}

	if err := validationrunner.CheckCouchbaseClusterResourceImmutableFields(couchbase, c.GetCouchbaseCluster()); err != nil {
		log.Error(err, "Validation failed.", "cluster", c.GetCouchbaseCluster().NamespacedName())

		validationErrors = append(validationErrors, err.Error())
		validationFailed = true
	}

	if err := validationrunner.CheckCouchbaseClusterResourceUpdate(couchbase, c.GetCouchbaseCluster()); err != nil {
		log.Error(err, "Validation failed.", "cluster", c.GetCouchbaseCluster().NamespacedName())

		validationErrors = append(validationErrors, err.Error())
		validationFailed = true
	}

	changeErrs := validationrunner.CheckChangeConstraints(c)

	for _, err := range changeErrs {
		log.Error(err, "Validation failed.", "cluster", c.GetCouchbaseCluster().NamespacedName())

		validationErrors = append(validationErrors, err.Error())
	}

	immutableErrs := validationrunner.ValidateImmutableFields(c)

	for _, err := range immutableErrs {
		log.Error(err, "Validation failed.", "cluster", c.GetCouchbaseCluster().NamespacedName())

		validationErrors = append(validationErrors, err.Error())
	}

	// Existing cluster updated
	// TODO: We should just reload the cluster and aggregate other resources in
	// the cluster controller and check for updates there.
	log.V(2).Info("Updating cluster", "cluster", request.NamespacedName)

	if len(validationErrors) != 0 {
		c.GetCouchbaseCluster().Status.SetErrorCondition(strings.Join(validationErrors, "; "))
	} else {
		c.GetCouchbaseCluster().Status.ClearCondition(couchbasev2.ClusterConditionError)
	}

	if validationFailed {
		c.Update(c.GetCouchbaseCluster(), r.operatorStartTime)
	} else {
		c.Update(couchbase, r.operatorStartTime)
	}

	return requeueResult, nil
}

// AddToManager registers a controller reconciler with the manager and triggers updates
// when CouchbaseCluster objects are modified.
func AddToManager(mgr manager.Manager, concurrency int, clusterConfig cluster.Config) error {
	r := &CouchbaseClusterReconciler{
		client:            mgr.GetClient(),
		clusters:          CreateManagedClusters(),
		clusterConfig:     clusterConfig,
		operatorStartTime: time.Now(),
	}

	options := controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: concurrency,
	}

	c, err := controller.New("couchbase-controller", mgr, options)
	if err != nil {
		return err
	}

	src := source.Kind(mgr.GetCache(), &couchbasev2.CouchbaseCluster{})

	return c.Watch(src, &handler.EnqueueRequestForObject{})
}
