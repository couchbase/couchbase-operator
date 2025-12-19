package controller

import (
	"context"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/validationrunner"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

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

// CouchbaseEncryptionKeyReconciler reconciles CouchbaseEncryptionKey resources to remove
// per-cluster finalizers when their corresponding cluster no longer exists.
type CouchbaseEncryptionKeyReconciler struct {
	client client.Client
}

// Reconcile handles CouchbaseEncryptionKey deletion and cleans up cluster-scoped finalizers
// if the referenced CouchbaseCluster no longer exists.
func (r *CouchbaseEncryptionKeyReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log.V(2).Info("Received encryption key event", "key", req.NamespacedName)

	key := &couchbasev2.CouchbaseEncryptionKey{}
	if err := r.client.Get(ctx, req.NamespacedName, key); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		log.Error(err, "Failed to get CouchbaseEncryptionKey", "key", req.NamespacedName)

		return reconcile.Result{}, err
	}

	// Only act on deletion
	if key.DeletionTimestamp == nil {
		return reconcile.Result{}, nil
	}

	prefix := constants.EncryptionKeyFinalizerPrefix + "."
	finalizers := key.Finalizers
	clusterFinalizersRemain := false
	kept := make([]string, 0, len(finalizers))

	for _, f := range finalizers {
		// Only consider our cluster-scoped finalizers
		if !strings.HasPrefix(f, prefix) {
			kept = append(kept, f)
			continue
		}

		clusterName := strings.TrimPrefix(f, prefix)
		// Verify if the cluster still exists
		cluster := &couchbasev2.CouchbaseCluster{}
		if err := r.client.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: clusterName}, cluster); err != nil {
			if errors.IsNotFound(err) {
				// Cluster is gone; drop this finalizer
				continue
			} else {
				// On transient errors, log and keep the finalizer
				log.Error(err, "Failed to get CouchbaseCluster for finalizer check", "cluster", clusterName, "key", req.NamespacedName)
			}
		}

		// Cluster exists; keep the finalizer
		clusterFinalizersRemain = true

		kept = append(kept, f)
	}

	if len(kept) != len(finalizers) {
		key.Finalizers = kept
		if err := r.client.Update(ctx, key); err != nil {
			log.Error(err, "Failed to update CouchbaseEncryptionKey finalizers", "key", req.NamespacedName)
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, err
		}

		log.Info("Removed dangling encryption key finalizers for non-existent clusters", "key", req.NamespacedName)
	}

	if clusterFinalizersRemain {
		return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return reconcile.Result{}, nil
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

	warnings, err := validationrunner.CheckCouchbaseClusterResource(couchbase)
	if err != nil {
		return r.reconcileFailedValidationCluster(couchbase, err)
	}

	if warnings != nil {
		log.V(1).Info("Validation warnings.", "cluster", request.NamespacedName, "warnings", warnings)
	}

	// See if we know about the cluster already.
	c, existingCluster := r.clusters.Load(request.NamespacedName.String())

	// Check if this is actually the same cluster or if was delete and recreated
	if c != nil && c.GetCouchbaseCluster().UID != couchbase.UID {
		log.V(2).Info("Deleting recreated cluster", "cluster", request.NamespacedName)
		c.Delete()
		r.clusters.Delete(request.NamespacedName.String())

		existingCluster = false
	}

	if !existingCluster {
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

		// We should check change constraints if this is an existing cluster but the operator has restarted and therefore doesn't have an in-memory
		// representation of the last state of the cluster. We will only validate the spec here.
		if existingSpec := c.GetCouchbaseCluster().GetSpecFromAnnotation(); existingSpec != nil {
			existingCluster := c.GetCouchbaseCluster().DeepCopy()

			existingCluster.Spec = *existingSpec
			if err := validationrunner.CheckClusterChangeConstraints(existingCluster, c.GetCouchbaseCluster()); err != nil {
				log.Error(err, "Validation failed", "cluster", request.NamespacedName)
				c.UpdateOnFailedValidationOperatorRestart(err, existingCluster)
			}
		}

		c.RunReconcile(r.operatorStartTime)

		r.clusters.Store(request.NamespacedName.String(), c)

		return requeueResult, nil
	}

	c.ReconcileMirWatchdogContext()

	if err := validationrunner.CheckClusterChangeConstraints(c.GetCouchbaseCluster(), couchbase); err != nil {
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
		c.RunReconcile(r.operatorStartTime)
	} else {
		c.Update(couchbase, r.operatorStartTime)
	}

	return requeueResult, nil
}

func (r *CouchbaseClusterReconciler) reconcileFailedValidationCluster(couchbase *couchbasev2.CouchbaseCluster, validationErr error) (reconcile.Result, error) {
	c, existingCluster := r.clusters.Load(couchbase.NamespacedName())
	reconcileResult := reconcile.Result{
		RequeueAfter: 10 * time.Second,
	}

	if !existingCluster {
		// Cluster created or detected during a restart, start a new management routine.
		log.V(2).Info("Creating cluster", "cluster", couchbase.NamespacedName())

		var err error
		c, err = cluster.New(r.clusterConfig, couchbase)

		if err != nil {
			log.Error(err, "Failed to create Couchbase cluster", "cluster", couchbase.NamespacedName())
			return reconcileResult, err
		}

		r.clusters.Store(couchbase.NamespacedName(), c)
	}

	log.Error(validationErr, "Validation failed.", "cluster", couchbase.NamespacedName())

	if err := c.UpdateFailedValidation(validationErr); err != nil {
		log.Error(err, "Failed to update cluster status", "cluster", couchbase.NamespacedName())
	}

	// Do not return an error here. Controller runtime will ignore RequeueAfter and wait apply an exponential backoff.
	return reconcileResult, nil
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

	// Register Cluster controller to handle cluster reconciliation
	src := source.Kind(mgr.GetCache(), &couchbasev2.CouchbaseCluster{})
	if err := c.Watch(src, &handler.EnqueueRequestForObject{}); err != nil {
		return err
	}

	// Register Encryption Key controller to handle finalizer cleanup
	ekReconciler := &CouchbaseEncryptionKeyReconciler{client: mgr.GetClient()}
	ekOptions := controller.Options{
		Reconciler:              ekReconciler,
		MaxConcurrentReconciles: concurrency,
	}

	ekController, err := controller.New("couchbase-encryption-key-controller", mgr, ekOptions)
	if err != nil {
		return err
	}

	ekSrc := source.Kind(mgr.GetCache(), &couchbasev2.CouchbaseEncryptionKey{})

	return ekController.Watch(ekSrc, &handler.EnqueueRequestForObject{})
}
