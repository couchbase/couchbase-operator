/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package controller

import (
	"context"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"

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
	client           client.Client
	clusters         *ManagedClusters
	podCreateTimeout string
	podDeleteDelay   string
}

// Reconcile is triggered when an event occurs on a watched resource.
// TODO: The reconcile.Result allows events to be requeued which *may*
// allow us to do away with the separate go routine sitting in a loop,
// or it may just fill up with events due to status updates...
func (r *CouchbaseClusterReconciler) Reconcile(_ context.Context, request reconcile.Request) (reconcile.Result, error) {
	log.V(2).Info("Received couchbase cluster event", "cluster", request.NamespacedName)

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

	// See if we know about the cluster already.
	c, ok := r.clusters.Load(request.NamespacedName.String())
	if !ok {
		// Cluster created or detected during a restart, start a new management routine.
		log.V(2).Info("Creating cluster", "cluster", request.NamespacedName)

		c, err := cluster.New(cluster.Config{PodCreateTimeout: r.podCreateTimeout, PodDeleteDelay: r.podDeleteDelay}, couchbase)
		if err != nil {
			log.Error(err, "Failed to create Couchbase cluster", "cluster", request.NamespacedName)
			return reconcile.Result{}, err
		}

		r.clusters.Store(request.NamespacedName.String(), c)

		return requeueResult, nil
	}

	// Existing cluster updated
	// TODO: We should just reload the cluster and aggregate other resources in
	// the cluster controller and check for updates there.
	log.V(2).Info("Updating cluster", "cluster", request.NamespacedName)

	c.Update(couchbase)

	return requeueResult, nil
}

// AddToManager registers a controller reconciler with the manager and triggers updates
// when CouchbaseCluster objects are modified.
func AddToManager(mgr manager.Manager, createTimeout string, concurrency int, deleteDelay string) error {
	r := &CouchbaseClusterReconciler{
		client:           mgr.GetClient(),
		clusters:         CreateManagedClusters(),
		podCreateTimeout: createTimeout,
		podDeleteDelay:   deleteDelay,
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
