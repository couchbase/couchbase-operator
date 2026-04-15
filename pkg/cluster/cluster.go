/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/diff"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"

	"github.com/Masterminds/semver"
	"github.com/golang/groupcache/lru"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("cluster")

// Be very aggressive here.  If pods are deleted with volumes missing then
// they stay Terminating forever.  This should emulate --grace-period=0 --force.
// See: https://github.com/kubernetes/kubernetes/issues/51835.
// Per K8S-2836 we now allow a PodDeleteDelay to be passed into config, this defaults
// to 0 as well.
// var podTerminationGracePeriod = int64(0)

type Config struct {
	PodCreateTimeout      time.Duration
	PodDeleteDelay        time.Duration
	PodReadinessDelay     time.Duration
	PodReadinessPeriod    time.Duration
	PodRecoveryMaxRetries int
}

// Cluster is the core internal data type representing a Couchbase cluster.
type Cluster struct {
	// config is the configuration passed in from the command line.
	config Config

	// k8s is a Kubernetes interface layer.  All resource types managed by
	// this client use caching to improve performance and remove load from
	// the Kubernetes API servers.
	k8s *client.Client

	// cluster is the current couchbase cluster resource from Kubernetes.
	// This is ephemeral and is updated with each iteration of the runloop.
	cluster *couchbasev2.CouchbaseCluster

	// members is the set of all members we curretly recognize.
	members couchbaseutil.MemberSet

	// callableMembers is the subset of members that we can attempt to call
	// with the API. The CallAPI functions will iterate over this set in
	// an attempt to avoid transient errors.
	callableMembers couchbaseutil.MemberSet

	// username is the administrative username that the operator is using
	// to communicate with the cluster.  This should be persisted in the
	// cluster config map to enable rotation across restarts.
	username string

	// password is the administrative users's password the the operator
	// is using to communicate with the cluster. This should be persisted
	// in the cluster config map to enable rotation across restarts.
	password string

	// api is client used to communicate with Couchbase server.
	api *couchbaseutil.Client

	// ctx is a golang context used to cancel long running or iterative
	// operations.  It is closed when the cluster is deleted to clean up
	// go routines and avoid excessive error messages in the log stream.
	// As the runloop is synchronously driven now, this serves little
	// purpose, consider deleting it...
	ctx context.Context

	// cancel is the function that causes the ctx to close.
	cancel context.CancelFunc

	// lastEvent records when the last event was raised.  Although based
	// on time, which has sub-second accuracy, when marshalled into JSON
	// and sent to the API, times are reduced to a second granularity.
	// This means that events can alias, and that ordering--critical to
	// the test framework--becomes non-deterministic.  We track the last
	// event time and delay subsequent events until the next whole second.
	lastEvent time.Time

	// scheduler is the interface that control distribution of pods across
	// available nodes on the platform.
	scheduler scheduler.Scheduler

	// eventCache is responsible for caching certain events that repeat
	// often, and can be agreggated by incrementing their counts.  Unlike
	// the cache implemented by the Kubernetes client library, this one
	// does not rate-limit and discard events, which would cause non-
	// determinism in testing.
	eventCache *lru.Cache

	// state is the persistent storage associated with the cluster.  This
	// should be used judiciously, and where possible state observed from
	// either Kubernetes or Couchbase server.
	state persistence.PersistentStorage

	// recoveryTime is a threshold for automatic recovery of a pod backed
	// by a persistent volume.  When the current time passes this threshold
	// then we attempt manual recovery by recreating the pod.
	recoveryTime map[string]time.Time

	// lastRecoveryAttemptTime tracks the time of the last recovery attempt per pod.
	lastRecoveryAttemptTime map[string]time.Time

	// lastSuccessfulRecoveryTime tracks the time of the last successful recovery per pod.
	lastSuccessfulRecoveryTime map[string]time.Time

	// generation is the most recent resource generation we know about.  For
	// some reason a read after write can go back in time, I'm not certain it's
	// caching we are doing, but the API itself.
	generation int64

	// tlsCache allows us to load up and verify TLS data at the beginning of
	// a reconcile so it appears atomic throughout the process.
	tlsCache *tlsCache

	// mirWatchdog is the context for the MIR watchdog goroutine.
	// It is separate from the main cluster context to allow starting/stopping
	// the watchdog based on the spec.
	mirWatchdog *MirWatchdogContext

	// failedGroupsMu protects read-modify-write operations on the failed
	// scheduling server groups tracker to prevent lost updates when multiple
	// pod creation goroutines run concurrently.
	failedGroupsMu sync.RWMutex
}

// namespacedName returns a unique identifier for a cluster within Kubernetes.
// controller-runtime is actually just using the raw NamespacedName in its logs
// these days, maintaining the structured JSON, so perhaps we should do the same
// and adjust tooling to match.
func (c *Cluster) namespacedName() string {
	return c.cluster.NamespacedName()
}

// updateRecoveryElapsedMetrics updates the time-since recovery metrics with the
// current elapsed seconds since the last recovery attempt and last successful recovery.
func (c *Cluster) updateRecoveryElapsedMetrics() {
	for name, t := range c.lastRecoveryAttemptTime {
		metrics.PodTimeSinceLastRecoveryAttemptMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, name})...).Set(time.Since(t).Seconds())
	}

	for name, t := range c.lastSuccessfulRecoveryTime {
		metrics.PodTimeSinceLastSuccessfulRecoveryMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, name})...).Set(time.Since(t).Seconds())
	}
}

// persistRecoveryTimestamps writes the in-memory recovery timestamp maps to the
// persistent storage so they survive operator restarts.
func (c *Cluster) persistRecoveryTimestamps() {
	if len(c.lastRecoveryAttemptTime) > 0 {
		serialized := make(map[string]string, len(c.lastRecoveryAttemptTime))
		for name, t := range c.lastRecoveryAttemptTime {
			serialized[name] = t.Format(time.RFC3339)
		}

		b, err := json.Marshal(serialized)
		if err != nil {
			log.Error(err, "Failed to marshal lastRecoveryAttemptTime", "cluster", c.namespacedName())
		} else if err := c.state.Upsert(persistence.LastRecoveryAttemptTimes, string(b)); err != nil {
			log.Error(err, "Failed to persist lastRecoveryAttemptTime", "cluster", c.namespacedName())
		}
	}

	if len(c.lastSuccessfulRecoveryTime) > 0 {
		serialized := make(map[string]string, len(c.lastSuccessfulRecoveryTime))
		for name, t := range c.lastSuccessfulRecoveryTime {
			serialized[name] = t.Format(time.RFC3339)
		}

		b, err := json.Marshal(serialized)
		if err != nil {
			log.Error(err, "Failed to marshal lastSuccessfulRecoveryTime", "cluster", c.namespacedName())
		} else if err := c.state.Upsert(persistence.LastSuccessfulRecoveryTimes, string(b)); err != nil {
			log.Error(err, "Failed to persist lastSuccessfulRecoveryTime", "cluster", c.namespacedName())
		}
	}
}

// restoreRecoveryTimestamps reads persisted recovery timestamps from the
// persistent storage and repopulates the in-memory maps.
func (c *Cluster) restoreRecoveryTimestamps() {
	if raw, err := c.state.Get(persistence.LastRecoveryAttemptTimes); err == nil {
		var serialized map[string]string
		if err := json.Unmarshal([]byte(raw), &serialized); err != nil {
			log.Error(err, "Failed to unmarshal lastRecoveryAttemptTimes", "cluster", c.namespacedName())
		} else {
			for name, ts := range serialized {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					c.lastRecoveryAttemptTime[name] = t
				}
			}
		}
	}

	if raw, err := c.state.Get(persistence.LastSuccessfulRecoveryTimes); err == nil {
		var serialized map[string]string
		if err := json.Unmarshal([]byte(raw), &serialized); err != nil {
			log.Error(err, "Failed to unmarshal lastSuccessfulRecoveryTimes", "cluster", c.namespacedName())
		} else {
			for name, ts := range serialized {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					c.lastSuccessfulRecoveryTime[name] = t
				}
			}
		}
	}
}

// New is called when we first observe a CouchbaseCluster resource.  This may be due to
// creation or recovery after an Operator restart.
func New(config Config, cluster *couchbasev2.CouchbaseCluster) (*Cluster, error) {
	c := &Cluster{
		config:                     config,
		cluster:                    cluster,
		eventCache:                 lru.New(1024),
		recoveryTime:               map[string]time.Time{},
		lastRecoveryAttemptTime:    map[string]time.Time{},
		lastSuccessfulRecoveryTime: map[string]time.Time{},
		members:                    couchbaseutil.MemberSet{},
		callableMembers:            couchbaseutil.MemberSet{},
		generation:                 cluster.Generation,
	}

	log.Info("Watching new cluster", "cluster", c.namespacedName())

	// Cancel is used to abort the go routine when the operator is deleted
	c.ctx, c.cancel = context.WithCancel(context.Background())

	var err error

	// Initialize Kubernetes clients.
	c.k8s, err = client.NewClient(c.ctx, c.cluster.Namespace, labels.SelectorFromSet(k8sutil.LabelsForCluster(c.cluster)), nil)
	if err != nil {
		return nil, err
	}

	c.InitCounterMetrics()

	// Once the client is setup, everything goes though this creation function so
	// we can catch errors and set the cluster error condition.
	if err := c.newCluster(); err != nil {
		c.cluster.Status.SetErrorCondition(err.Error())

		if err := c.updateCRStatus(); err != nil {
			log.Info("unable to update status", "cluster", c.namespacedName(), "error", err)
		}

		return nil, err
	}

	log.Info("Running", "cluster", c.namespacedName())

	c.restoreRecoveryTimestamps()

	if err := annotations.Populate(&c.cluster.Spec, c.cluster.Annotations); err != nil {
		log.Error(err, "Failed to apply annotations to cluster spec", "cluster", c.namespacedName())
	}

	return c, nil
}

// addForegroundDeleteFinalizer adds a finalizer to the cluster that tells it
// to wait for all dependent resources to be deleted before deleting itself.
// Means that a quick delete/recreate doesn't run aground on resource conflicts.
// Does however mean things can get stuck more easily...
func (c *Cluster) addForegroundDeleteFinalizer() error {
	var hasForegroundDeleteFinalizer bool

	for _, finalizer := range c.cluster.Finalizers {
		if finalizer == metav1.FinalizerDeleteDependents {
			hasForegroundDeleteFinalizer = true
			break
		}
	}

	if !hasForegroundDeleteFinalizer {
		c.cluster.Finalizers = append(c.cluster.Finalizers, metav1.FinalizerDeleteDependents)

		newCluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Update(context.Background(), c.cluster, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		c.cluster = newCluster
	}

	return nil
}

// newCluster does the bulk of cluster initialization once the cluster object is initialized.
func (c *Cluster) newCluster() error {
	var err error

	if err := c.addForegroundDeleteFinalizer(); err != nil {
		return err
	}

	// Create a new persistence layer to store and retrieve state.  Add in
	// defaults if they don't exist.
	if c.state, err = persistence.New(c.k8s, c.cluster); err != nil {
		return err
	}

	// Spawn the janitor process which monitors persistent log volumes.
	go newJanitor(c).run()

	if err := annotations.Populate(&c.cluster.Spec, c.cluster.Annotations); err != nil {
		log.Error(err, "Failed to apply annotations to cluster spec", "cluster", c.namespacedName())
	}

	// Load the most recent username, password and TLS data from either
	// peristence, or the underlying secrets, and initialize a client for
	// connection to Couchbase server.
	if err := c.initClients(); err != nil {
		return err
	}

	// Perform any necessary upgrades to the cluster and kubernetes resources.
	err = c.operatorUpgrade()

	return err
}

func (c *Cluster) Delete() {
	// Notify client operations to stop what they are doing e.g. abort retry loops
	c.cancel()

	// Stop the MIR watchdog if it's running
	if c.mirWatchdog != nil {
		c.mirWatchdog.Stop()
	}

	// Remove finalizers on EncryptionKeys
	c.removeEncryptionKeyFinalizers()

	// Clean up caches.
	c.k8s.Shutdown()
}

func (c *Cluster) removeEncryptionKeyFinalizers() {
	encryptionKeys := c.k8s.CouchbaseEncryptionKeys.List()

	for _, key := range encryptionKeys {
		c.removeFinalizer(key)
	}
}

func (c *Cluster) initializeClusterState() error {
	lowestImage, err := c.cluster.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	version, err := k8sutil.CouchbaseVersion(lowestImage)
	if err != nil {
		return err
	}

	// Clear the persistent state for a new cluster, it may be doing DR and we need
	// to go off the spec, not what is in memory.

	// The only thing we want to keep are the failed serverGroups.
	failedGroupsTracker, err := c.state.Get(persistence.FailedSchedulingServerGroupsTracker)
	if err != nil && !goerrors.Is(err, persistence.ErrKeyError) {
		return err
	}

	if err := c.state.Clear(); err != nil {
		return err
	}

	if failedGroupsTracker != "" {
		if err := c.state.Upsert(persistence.FailedSchedulingServerGroupsTracker, failedGroupsTracker); err != nil {
			return err
		}
	}

	// Once cleared, initialize the clients, using the underlying secrets as the source
	// of truth (as opposed to the persistent state data).
	if err := c.initClients(); err != nil {
		return err
	}

	// Use Upsert instead of Insert here: after Clear() the persistence Secret is empty
	// in k8s, but the informer cache may still reflect the pre-Clear state. Insert() uses
	// a "key must not exist" guard that apply() silently swallows when the guard fires,
	// meaning the write never reaches k8s and the next Get() times out. Upsert() always
	// writes through regardless of cache state.
	if err := c.state.Upsert(persistence.PodIndex, "0"); err != nil {
		return err
	}

	if err := c.state.Upsert(persistence.Version, version); err != nil {
		return err
	}

	if err := c.state.Upsert(persistence.Password, c.password); err != nil {
		return err
	}

	if err := c.state.Upsert(persistence.Upgrading, string(persistence.UpgradeInactive)); err != nil {
		return err
	}

	tls := c.api.GetTLS()

	if tls != nil {
		if err := c.state.Upsert(persistence.CACertificate, string(tls.CACert)); err != nil {
			return err
		}

		if tls.ClientAuth != nil {
			if err := c.state.Upsert(persistence.ClientCertificate, string(tls.ClientAuth.Cert)); err != nil {
				return err
			}

			if err := c.state.Upsert(persistence.ClientKey, string(tls.ClientAuth.Key)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Cluster) isSGReschedulingEnabled() bool {
	return c.cluster.Spec.ServerGroupsEnabled() && c.cluster.Spec.RescheduleDifferentServerGroup
}

func (c *Cluster) addFailedSchedulingServerGroups(serverGroup string) error {
	if !c.isSGReschedulingEnabled() {
		return nil
	}

	// Protect the read-modify-write operation to prevent lost updates
	// when multiple pod creation goroutines run concurrently.
	c.failedGroupsMu.Lock()
	defer c.failedGroupsMu.Unlock()

	failedGroupsTracker, err := c.getFailedServerGroupsTracker()
	if err != nil {
		return err
	}

	// Increment the count for the server group
	if v, ok := failedGroupsTracker[serverGroup]; ok {
		failedGroupsTracker[serverGroup] = v + 1
	} else {
		failedGroupsTracker[serverGroup] = 1
	}

	b, err := json.Marshal(failedGroupsTracker)
	if err != nil {
		return err
	}

	return c.state.Upsert(persistence.FailedSchedulingServerGroupsTracker, string(b))
}

var readyConditions = []couchbasev2.ClusterConditionType{
	couchbasev2.ClusterConditionBalanced,
	couchbasev2.ClusterConditionAvailable,
}

var notReadyConditions = []couchbasev2.ClusterConditionType{
	couchbasev2.ClusterConditionScaling,
	couchbasev2.ClusterConditionScalingDown,
	couchbasev2.ClusterConditionScalingUp,
	couchbasev2.ClusterConditionUpgrading,
}

func (c *Cluster) clearFailedSchedulingServerGroupsIfReady() {
	if !c.isSGReschedulingEnabled() {
		return
	}

	for _, condition := range readyConditions {
		if !c.cluster.HasCondition(condition) {
			return
		}
	}

	for _, condition := range notReadyConditions {
		if c.cluster.HasCondition(condition) {
			return
		}
	}

	desiredSize := 0

	for _, server := range c.cluster.Spec.Servers {
		desiredSize += server.Size
	}

	if c.cluster.Status.Size != desiredSize {
		return
	}

	// Protect the delete operation to ensure consistency with concurrent
	// reads and writes from pod creation goroutines.
	c.failedGroupsMu.Lock()
	defer c.failedGroupsMu.Unlock()

	if err := c.state.Delete(persistence.FailedSchedulingServerGroupsTracker); err != nil && !goerrors.Is(err, persistence.ErrKeyError) {
		log.Error(err, "Failed to clear failed scheduling server groups", "cluster", c.namespacedName())
	}
}

// create is the main cluster creation routine.  It is called on initial cluster creation
// and any time it is recreated (e.g. all ephemeral pods have been killed).
func (c *Cluster) create() error {
	log.Info("Cluster does not exist so the operator is attempting to create it", "cluster", c.namespacedName())

	if err := c.initializeClusterState(); err != nil {
		return err
	}

	lowestImage, err := c.cluster.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	version, err := k8sutil.CouchbaseVersion(lowestImage)
	if err != nil {
		return err
	}

	c.cluster.Status.SetCreatingCondition()
	c.cluster.Status.CurrentVersion = version

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	// createInitialMember creates the pod async and flags it with PendingInitializationCondition.
	// handleReadyPendingPod() will call configureInitialMember + fetchAndPersistClusterUUID
	// once the pod is Running.
	return c.createInitialMember()
}

// initalizeClusterKubernetesResources stores the cluster UUID and syncs member
// state after the initial cluster target is contactable. Used by migration clusters.
func (c *Cluster) initalizeClusterKubernetesResources(target any) error {
	uuid, err := c.fetchAndPersistClusterUUID(target)
	if err != nil {
		return err
	}

	if err := c.updateMembers(); err != nil {
		return err
	}

	c.cluster.Status.SetClusterID(uuid)
	c.cluster.Status.SetBalancedCondition()

	return c.updateCRStatus()
}

// podInitialized tells us whether Couchbase server has been fully
// initialized and the API will work as expected.  This came in with
// 2.2.
func (c *Cluster) podInitialized(pod *v1.Pod) bool {
	// The initialized annotation came to be in 2.2...
	versionAnnotation, ok := pod.Annotations[constants.ResourceVersionAnnotation]
	if !ok {
		return true
	}

	version, err := semver.NewVersion(versionAnnotation)
	if err != nil {
		log.Error(err, "Failed to parse pod version", "cluster", c.namespacedName(), "pod", pod.Name, "version", versionAnnotation)
		return true
	}

	threshold := semver.MustParse(constants.PodInitializedAnnotationMinVersion)
	if version.LessThan(threshold) {
		return true
	}

	// Pod is initialized, let the normal reconcile process occur.
	if _, ok := pod.Annotations[constants.PodInitializedAnnotation]; ok {
		return true
	}

	return false
}

// RunReconcile gathers a list of pods in cluster from Kubernetes, optionally
// initializes our internal member list if we need to e.g. we have been restarted and
// have lost state or a previous error may have resulted in inconsistent state, then
// compares reality with the specification and makes the former match it.
//
// It accepts a flag forcing it update internal state from Kubernetes, and returns a
// similar flag to indicate we require a forced update with the next invocation.
// nolint:gocognit
func (c *Cluster) RunReconcile(operatorStartTime time.Time) {
	// Always update the cluster status and reconcile loop time.
	start := time.Now()

	c.updateRecoveryElapsedMetrics()

	defer func() {
		if err := c.updateCRStatus(); err != nil {
			log.Error(err, "Status update failed", "cluster", c.namespacedName())
		}

		reconcileTime := time.Since(start)

		metrics.ReconcileDurationMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Observe(reconcileTime.Seconds())
	}()

	if _, err := c.state.Get(persistence.RebalanceRetries); err != nil {
		// If the key doesn't exist, create it.
		// We don't want to do this every time we reconcile.
		if goerrors.Is(err, persistence.ErrKeyError) {
			if err := c.state.Insert(persistence.RebalanceRetries, "1"); err != nil {
				return
			}
		}
	}

	// If the user has requested that we pause operations.
	if c.cluster.Spec.Paused {
		c.cluster.Status.PauseControl()
		log.Info("Operator paused, skipping", "cluster", c.namespacedName())

		metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "paused"})...).Inc()

		return
	}

	// If manual intervention is required, we will skip reconciliation if the SkipReconciliation flag is set.
	if condition := c.cluster.Status.GetCondition(couchbasev2.ClusterConditionManualInterventionRequired); condition != nil && condition.Status == v1.ConditionTrue {
		running := c.isMirWatchdogRunning()
		enabled := c.isMirWatchdogEnabled()
		if running && enabled && c.cluster.Spec.MirWatchdog != nil && c.cluster.Spec.MirWatchdog.SkipReconciliation != nil && *c.cluster.Spec.MirWatchdog.SkipReconciliation {
			log.Info("Manual intervention required, skipping reconciliation", "cluster", c.namespacedName(), "reason", condition.Message)
			metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "mir"})...).Inc()

			return
		}

		switch {
		case !running:
			// If we have the condition but it's not running, we should have already cleared it. This is just a safety check.
			c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionManualInterventionRequired)
			metrics.ManualInterventionRequiredMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Set(0)
		case !enabled:
			// If the MirWatchdog is running but not enabled, it's possible that the spec has been updated to disable it before the mirWatchdogContext is reconciled.
			// We can handle stopping it here if this occurs.
			c.StopMirWatchdog()
		default:
			// We have the condition and the MirWatchdog is running and enabled, but we don't want to skip reconciliation so we should log the reason and continue.
			log.Info("Manual intervention required", "cluster", c.namespacedName(), "reason", condition.Message)
		}
	}

	// Otherwise indicate that we are in control.
	c.cluster.Status.Control()

	// Process pods with PendingInitializationCondition (async pod creation).
	// For new clusters (no ready members), skip updateMembers and just handle pending pods.
	// For existing clusters, process pending pods but continue with normal reconciliation.
	pendingInitPods := c.getPendingInitPods()
	if len(pendingInitPods) > 0 && c.readyMembers().Empty() {
		log.V(1).Info("Pods pending initialization during cluster creation, skipping updateMembers",
			"cluster", c.namespacedName(), "count", len(pendingInitPods))
		c.updatePendingInitializationConditions()
		// Return early - next cycle will either complete initialization or retry
		return
	}

	if len(pendingInitPods) > 0 {
		log.V(1).Info("Pods pending initialization, processing before reconciliation",
			"cluster", c.namespacedName(), "count", len(pendingInitPods))
		c.updatePendingInitializationConditions()
		// Continue with normal reconciliation
	}

	// Members are updated each iteration by performing a union of Kubernetes resources
	// we discover, and any hosts that Couchbase knows about, if we can actually talk
	// to it.  By performing no caching the behaviour of the systems is identical during
	// runtime and after a restart.
	if err := c.updateMembers(); err != nil {
		log.Error(err, "Failed to update members", "cluster", c.namespacedName())
		c.raiseEvent(k8sutil.ReconcileFailedEvent(c.cluster, err))

		metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "error"})...).Inc()
		metrics.ReconcileFailureMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Inc()

		// When we call updateMembers, it's going to look at all running pods can try
		// to dial Couchbase and get health status.  It's entirely possible that the
		// operator got killed or rescheduled before pods were correctly initialized,
		// and thus will not respond to our pleas for help.  Execute any uninitialized
		// nodes, so that we may recreate the cluster next time around.
		running, _ := c.getClusterPodsByPhase()
		for _, pod := range running {
			if c.podInitialized(pod) {
				continue
			}

			log.Info("Killing uninitialized pod", "cluster", c.namespacedName(), "pod", pod.Name)

			if err := k8sutil.DeletePod(c.k8s, c.cluster.Namespace, pod.Name, c.config.GetDeleteOptions()); err != nil {
				log.Error(err, "Failed to delete uninitialized pod", "cluster", c.namespacedName(), "pod", pod.Name)
				continue
			}
		}

		// Now is the time to check if any of the active client/server certs have expired
		// otherwise we're not going to be able to update members or do anything.
		if err := c.rotateExpiredCertificates(); err != nil {
			log.Error(err, "Failed to rotate expired certificates", "cluster", c.namespacedName())
		}

		return
	}

	if err := c.checkUpdateTime(operatorStartTime); err != nil {
		log.Error(err, "Error when checking time of last update", "cluster", c.namespacedName())
	}

	var err error
	// If we are in migration mode handle that differently.
	if c.cluster.IsMigrationCluster() {
		log.Info("Cluster is in migration mode", "cluster", c.namespacedName())
		err = c.reconcileMigrationCluster()
	} else {
		err = c.reconcile()
	}

	// Finally reconcile state according to the specification.
	// Every reconcile should either set or clear the error condition.
	// This lets us spot very easily any persistent error conditions from
	// external tooling (rather than them going into a log).  For example,
	// if I upgrade the operator, does it break?  If I start in a broken
	// state and take some action, does it fix itself.  The other added
	// bonus is it will show up on dashboards like a christmas tree.
	if err != nil {
		var stackTracedError *errors.StackTracedError

		if goerrors.As(err, &stackTracedError) {
			log.Info("Reconciliation failed", "cluster", c.namespacedName(), "error", err.Error(), "stack", stackTracedError.GetStack())
		} else {
			log.Error(err, "Reconciliation failed", "cluster", c.namespacedName())
		}

		c.cluster.Status.SetErrorCondition(err.Error())

		if err := c.updateCRStatus(); err != nil {
			log.Info("unable to update status", "cluster", c.namespacedName(), "error", err)
		}

		c.raiseEvent(k8sutil.ReconcileFailedEvent(c.cluster, err))

		metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "error"})...).Inc()
		metrics.ReconcileFailureMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Inc()

		return
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionError)
	if err := retryutil.RetryFor(4*time.Second, c.updateCRStatus); err != nil {
		log.Info("unable to update status", "cluster", c.namespacedName(), "error", err)
	}

	metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "success"})...).Inc()
}

// Update is called periodically or on a CR change, print out any diffs in the spec
// then update the specification and unconditionally reconcile.
func (c *Cluster) Update(cluster *couchbasev2.CouchbaseCluster, operatorStartTime time.Time) {
	if cluster.Generation < c.generation {
		log.Info("API returned old version, skipping reconcile", "cluster", c.namespacedName())
		return
	}

	if err := annotations.Populate(&cluster.Spec, cluster.Annotations); err != nil {
		log.Error(err, "Failed to apply annotations to cluster spec", "cluster", c.namespacedName())
	}

	if !reflect.DeepEqual(cluster.Spec, c.cluster.Spec) {
		c.logUpdate(c.cluster.Spec, cluster.Spec)
		c.cluster.Status.LastUpdateTime = time.Now().Format(time.RFC3339)
	}

	c.cluster = cluster
	c.RunReconcile(operatorStartTime)
}

func (c *Cluster) logUpdate(old, new interface{}) {
	d := diff.PrettyDiff(old, new)

	// reflect.DeepEqual doesn't make the difference between a nil map and an
	// empty one, so this could be legitimately triggered even if there is no
	// difference.
	if d != "" {
		log.Info("Resource updated", "cluster", c.namespacedName(), "diff", d)
	}
}

func (c *Cluster) updateCRStatus() error {
	// The cluster object can be updated asynchronously e.g. via a spec update,
	// hence what's in etcd need not reflect what's locally cached and the k8s
	// server will reject any updates that fail the CAS test.  We only pick up
	// these updates between reconcile executions (see handleUpdateEvent).
	cluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Get(context.Background(), c.cluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	// Ignore the case where nothing needs to be updated
	if reflect.DeepEqual(c.cluster.Status, cluster.Status) {
		return nil
	}

	d, err := diff.Diff(c.cluster.Status, cluster.Status)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	if d == "" {
		return nil
	}

	c.logUpdate(cluster.Status, c.cluster.Status)

	// Copy the updated status to our cluster object and try update it
	cluster.Status = c.cluster.Status

	newCluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Update(context.Background(), cluster, metav1.UpdateOptions{})
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	c.generation = newCluster.Generation

	return nil
}

func (c *Cluster) UpdateOnFailedValidationOperatorRestart(validationErr error, existingCluster *couchbasev2.CouchbaseCluster) {
	c.cluster.Status.SetErrorCondition(validationErr.Error())
	c.cluster = existingCluster
}

// updateCRSpecAnnotation updates lastReconciledSpec annotation with the JSON representation of the current spec.
// The annotation will only be updated if the spec has changed.
func (c *Cluster) UpdateCRSpecAnnotation() error {
	cluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Get(context.Background(), c.cluster.Name, metav1.GetOptions{})

	if err != nil {
		return errors.NewStackTracedError(err)
	}

	lastReconciledSpec := cluster.GetSpecFromAnnotation()
	if reflect.DeepEqual(c.cluster.Spec, lastReconciledSpec) {
		return nil
	}

	d, err := diff.Diff(c.cluster.Spec, lastReconciledSpec)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	if d == "" {
		return nil
	}

	specJSON, err := json.Marshal(c.cluster.Spec)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	couchbaseutil.AddAnnotation(&cluster.ObjectMeta, constants.AnnotationLastReconciledSpec, string(specJSON))

	_, err = c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Update(context.Background(), cluster, metav1.UpdateOptions{})

	return err
}

// Selects any member that can be recovered and attempts to restart it.
func (c *Cluster) recoverClusterDown() (bool, error) {
	// Use Names() as that returns a deterministic/sorted list for testing.
	for _, name := range c.members.Names() {
		m := c.members[name]
		if c.isPodRecoverable(m) {
			pod, podExists := c.k8s.Pods.Get(name)
			if podExists && pod.DeletionTimestamp == nil && k8sutil.HasPendingInitializationCondition(pod) {
				log.V(1).Info("Down pod already exists, skipping recreation", "cluster", c.namespacedName(), "name", name)
				continue
			}

			if err := c.recreatePod(m, false); err != nil {
				return false, fmt.Errorf("node %s could not be recovered: %w", m.Name(), err)
			}

			log.Info("Pod recovering", "cluster", c.namespacedName(), "name", m.Name())
			c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name(), c.cluster))

			return true, nil
		}
	}

	return false, nil
}

// getClusterPods returns all pods related to the cluster, excluding any anciliary
// ones such as backup and restore.
func (c *Cluster) getClusterPods() []*v1.Pod {
	return c.discardUntrustedPods(c.k8s.Pods.List(constants.LabelServer))
}

// discardUntrustedPods removes anything we don't "trust" e.g. looks a bit off like
// a user has tried to inject something into the cluster outside of our control.
func (c *Cluster) discardUntrustedPods(pods []*v1.Pod) (filtered []*v1.Pod) {
	for _, pod := range pods {
		if len(pod.OwnerReferences) < 1 {
			log.Info("Pod ignored, no owner", "cluster", c.namespacedName(), "name", pod.Name)
			continue
		}

		if pod.OwnerReferences[0].UID != c.cluster.UID {
			log.Info("Pod ignored, invalid owner", "cluster", c.namespacedName(), "name", pod.Name, "cluster_uid", c.cluster.UID, "pod_uid", pod.OwnerReferences[0].UID)
			continue
		}

		filtered = append(filtered, pod)
	}

	return
}

func (c *Cluster) getClusterPodsByPhase() (running, pending []*v1.Pod) {
	pods := c.getClusterPods()

	for _, pod := range pods {
		switch pod.Status.Phase {
		case v1.PodRunning:
			running = append(running, pod)
		case v1.PodPending:
			pending = append(pending, pod)
		}
	}

	return
}

// getPendingDNSPods returns all cluster pods that have the PodPendingExternalDNSCondition set,
// i.e. pods that have been CBS-added but are waiting for external DNS propagation.
func (c *Cluster) getPendingDNSPods() []*v1.Pod {
	clusterPods := c.getClusterPods()

	var pendingPods []*v1.Pod

	for _, pod := range clusterPods {
		if k8sutil.IsPendingDNSMember(pod) {
			pendingPods = append(pendingPods, pod)
		}
	}

	return pendingPods
}

// initClients sets up communication with the Couchbase cluster.
// This needs to be done on start up for existing clusters (loading the
// most recent good credentials from the persistent secret), and every
// time we attempt to recreate the cluster, as the password is cached
// it needs to be refereshed incase it is updated.
func (c *Cluster) initClients() error {
	if err := c.setupAuth(); err != nil {
		return err
	}

	return c.initCouchbaseClient()
}

// Use username and password from secret store.
func (c *Cluster) setupAuth() error {
	secret, found := c.k8s.Secrets.Get(c.cluster.Spec.Security.AdminSecret)
	if !found {
		return fmt.Errorf("%w: unable to get admin secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Security.AdminSecret)
	}

	username, ok := secret.Data[constants.AuthSecretUsernameKey]
	if !ok {
		return fmt.Errorf("%w: admin secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.AuthSecretUsernameKey)
	}

	// The stored password trumps everything, because it's not infeasible
	// the the user will try to rotate it with the operator down.
	password, err := c.state.Get(persistence.Password)
	if err != nil {
		// Doesn't exist yet, assume the cluster is just starting up
		// so set it.
		if !goerrors.Is(err, persistence.ErrKeyError) {
			return err
		}

		passwordRaw, ok := secret.Data[constants.AuthSecretPasswordKey]
		if !ok {
			return fmt.Errorf("%w: admin secret missing %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.AuthSecretPasswordKey)
		}

		password = string(passwordRaw)
	}

	c.username = string(username)
	c.password = password

	return nil
}

func (c *Cluster) initCouchbaseClient() error {
	log.Info("Couchbase client starting", "cluster", c.namespacedName())

	c.api = couchbaseutil.New(c.ctx, c.namespacedName(), c.username, c.password)

	// Our source of truth is always the persistent cache.  If the user has rotated
	// TLS while the operator is down then we have only the new certificates, while
	// server is using the the old configuration.  Likewise the user may have removed
	// the TLS configuration entirely while down, but pods are flagged as TLS enabled
	// so we need to honour that.
	var ca []byte

	var clientCert []byte

	var clientKey []byte

	log.V(2).Info("Looking for registry key", "key", persistence.CACertificate)

	if caString, err := c.state.Get(persistence.CACertificate); err != nil {
		if !goerrors.Is(err, persistence.ErrKeyError) {
			return err
		}
	} else {
		ca = []byte(caString)

		log.V(2).Info("Found key", "value", caString)
	}

	log.V(2).Info("Looking for registry key", "key", persistence.ClientCertificate)

	if clientCertString, err := c.state.Get(persistence.ClientCertificate); err != nil {
		if !goerrors.Is(err, persistence.ErrKeyError) {
			return err
		}
	} else {
		clientCert = []byte(clientCertString)

		log.V(2).Info("Found key", "value", clientCertString)
	}

	log.V(2).Info("Looking for registry key", "key", persistence.ClientKey)

	if clientKeyString, err := c.state.Get(persistence.ClientKey); err != nil {
		if !goerrors.Is(err, persistence.ErrKeyError) {
			return err
		}
	} else {
		clientKey = []byte(clientKeyString)

		log.V(2).Info("Found key", "value", clientKeyString)
	}

	// If the persistent cache is not populated, but TLS is enabled, then there
	// are two assumptions; this is either a new cluster, or it's an existing one
	// being upgraded to this version.
	if ca == nil && c.cluster.IsTLSEnabled() {
		log.V(1).Info("No TLS configuration cached", "cluster", c.namespacedName())

		rootCAs, err := c.getCAs()
		if err != nil {
			return err
		}

		serverCA, _, _, _, err := c.getVerifiedServerTLSData(rootCAs)
		if err != nil {
			return err
		}

		ca = serverCA

		// Optionally enable client authentication
		if c.cluster.Spec.Networking.TLS.ClientCertificatePolicy != nil {
			cert, key, err := c.getTLSClientData()
			if err != nil {
				return err
			}

			clientCert = cert
			clientKey = key
		}
	}

	// Finally if there is any TLS configuration available at all, then use it
	// to populate the client.
	if ca != nil {
		// Add the TLS context
		tls := &couchbaseutil.TLSAuth{
			CACert: ca,
		}

		if clientCert != nil {
			tls.ClientAuth = &couchbaseutil.TLSClientAuth{
				Cert: clientCert,
				Key:  clientKey,
			}
		}

		c.api.SetTLS(tls)
	}

	return nil
}

func (c *Cluster) indexOfServerConfigWithService(svc couchbasev2.Service) int {
	for idx, serverSpec := range c.cluster.Spec.Servers {
		for _, service := range serverSpec.Services {
			if service == svc {
				return idx
			}
		}
	}

	return -1
}

// clusterCreateMember create a new member and adds it to our member list.
func (c *Cluster) clusterCreateMember(member couchbaseutil.Member) error {
	firstMember := c.members.Empty()

	c.members.Add(member)

	c.cluster.Status.Size = c.members.Size()

	return c.updateMemberStatus(firstMember)
}

// clusterAddMember notifies that a new member has been added to the cluster
// and can be called.
func (c *Cluster) clusterAddMember(member couchbaseutil.Member) {
	c.callableMembers.Add(member)
}

// Removes a member from our cluster object and updates the cluster status.
func (c *Cluster) clusterRemoveMember(name string) error {
	c.members.Remove(name)
	c.callableMembers.Remove(name)

	c.cluster.Status.Size = c.members.Size()

	// If there are no members left, we don't need to update their status
	if c.members.Empty() {
		return nil
	}

	return c.updateMemberStatus(false)
}

// Raises an event.  While time.Time has nanosecond accuracy, this is lost when
// marshalled into JSON on the wire, so events within the same second look like
// they happened at exactly the same time, and thus ordering is arbitrary.  We
// rate limit new events so they always appear to occur at a visibly different
// time.
func (c *Cluster) raiseEvent(event *v1.Event) *v1.Event {
	// Work out how long since we last raised an event
	duration := event.FirstTimestamp.Time.Sub(c.lastEvent)

	if duration < time.Second {
		// Sleep until the next whole second
		timestamp := event.FirstTimestamp.Time.Add(time.Second).Truncate(time.Second)
		time.Sleep(time.Until(timestamp))

		// Update the event metadata so the events don't time travel!
		event.FirstTimestamp.Time = timestamp
		event.LastTimestamp.Time = timestamp
	}

	// Post the event to kubernetes
	event, err := c.k8s.KubeClient.CoreV1().Events(c.cluster.Namespace).Create(context.Background(), event, metav1.CreateOptions{})
	if err != nil {
		log.Error(err, "Event creation failed", "cluster", c.namespacedName(), "event", event.Reason)
		return nil
	}

	// Update the last event timestamp
	c.lastEvent = event.FirstTimestamp.Time

	return event
}

// raiseEventCached raises an event but first checks an LRU cache and optionally
// aggregates events together.
func (c *Cluster) raiseEventCached(event *v1.Event) {
	key := strings.Join([]string{event.Type, event.Reason, event.Message}, "")

	entry, ok := c.eventCache.Get(key)
	if ok {
		e := entry.(*v1.Event)
		if time.Since(e.LastTimestamp.Time) < 10*time.Minute {
			e.Count++
			e.LastTimestamp = metav1.Now()

			e, err := c.k8s.KubeClient.CoreV1().Events(c.cluster.Namespace).Update(context.Background(), e, metav1.UpdateOptions{})
			if err != nil {
				log.Error(err, "Event update failed", "cluster", c.namespacedName(), "event", event.Reason)
			}

			c.eventCache.Add(key, e)

			return
		}
	}

	if event = c.raiseEvent(event); event != nil {
		c.eventCache.Add(key, event)
	}
}

// getAvailableIndexs will return an array of indexs available to be used for
// pod names.
func (c *Cluster) getAvailableIndexes(num int) ([]int, error) {
	start, err := c.getPodIndex()
	if err != nil {
		return nil, err
	}

	indexes := make([]int, 0, num)
	for i := 0; i < num; i++ {
		indexes = append(indexes, start+i)
	}

	err = c.setPodIndex(indexes[len(indexes)-1] + 1)
	if err != nil {
		return nil, err
	}

	return indexes, nil
}

// getPodIndex returns the current pod naming index.
func (c *Cluster) getPodIndex() (int, error) {
	podIndexStr, err := c.state.Get(persistence.PodIndex)
	if err != nil {
		return -1, err
	}

	podIndex, err := strconv.Atoi(podIndexStr)
	if err != nil {
		return -1, errors.NewStackTracedError(err)
	}

	return podIndex, nil
}

// setPodIndex updates the current pod naming index and commits to etcd.
func (c *Cluster) setPodIndex(index int) error {
	return c.state.Update(persistence.PodIndex, strconv.Itoa(index))
}

func (c *Cluster) logStatus(status *MemberState) {
	status.LogStatus(c.namespacedName())
	c.scheduler.LogStatus(c.namespacedName())
}

// hibernate checks if the cluster can enter hibernation and if so, hibernates it.
func (c *Cluster) hibernate() (bool, error) {
	// If the cluster isn't already hibernating, we should check that we can enter hibernation and warn if it's not possible.
	if !c.cluster.HasCondition(couchbasev2.ClusterConditionHibernating) {
		canHibernate, reason := c.cluster.CanHibernate()

		if !canHibernate {
			log.Info("[WARN] Hibernation requested. Cluster will enter hibernation once it is stable", "reason", reason)
			return false, nil
		} else {
			log.Info("Cluster hibernation requested", "cluster", c.namespacedName())
			c.raiseEvent(k8sutil.HibernationStartedEvent(c.cluster))
		}
	}

	members := podsToMemberSet(c.getClusterPods())

	for _, member := range members {
		log.Info("Hibernating pod", "cluster", c.namespacedName(), "name", member.Name())

		if err := c.removePod(member.Name(), false); err != nil {
			return true, err
		}

		if err := c.clusterRemoveMember(member.Name()); err != nil {
			return true, err
		}
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionAvailable)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionBalanced)
	c.cluster.Status.SetHibernatingCondition("Cluster hibernating")

	if err := c.updateCRStatus(); err != nil {
		return true, err
	}

	log.Info("Cluster is hibernating", "cluster", c.namespacedName())

	return true, nil
}

func (c *Cluster) checkUpdateTime(operatorStartTime time.Time) error {
	// If there's no value, it must be a brand new cluster so ignore this step for now.
	if c.cluster.Status.LastUpdateTime == "" {
		return nil
	}

	timeOfChange, parseErr := time.Parse(time.RFC3339, c.cluster.Status.LastUpdateTime)
	if parseErr != nil {
		return parseErr
	}

	if !timeOfChange.IsZero() {
		if timeOfChange.Before(operatorStartTime) {
			log.Info("Operator started after changes made. Revert your changes to avoid errors.")
			c.cluster.Status.SetErrorCondition("Operator started after changes made. Revert your changes to avoid errors.")

			if err := c.state.Upsert(persistence.ChangesMadeBeforeOperatorStart, "true"); err != nil {
				return err
			}

			return nil
		}

		if _, err := c.state.Get(persistence.ChangesMadeBeforeOperatorStart); err == nil {
			c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionError)

			if err := c.state.Update(persistence.ChangesMadeBeforeOperatorStart, "false"); err != nil {
				return err
			}
		}
	}

	return nil
}

// ReconcileMirWatchdogContext manages the MIR watchdog lifecycle based on the cluster spec.
func (c *Cluster) ReconcileMirWatchdogContext() {
	mw := c.cluster.Spec.MirWatchdog

	enabled := mw != nil && mw.Enabled != nil && *mw.Enabled
	running := c.isMirWatchdogRunning()

	// Stop the watchdog if it's disabled and running.
	if !enabled && running {
		c.StopMirWatchdog()
		return
	}

	// Start the watchdog if it's enabled but not running.
	if enabled && !running {
		// Determine the desired interval
		desiredInterval := 20 * time.Second
		if mw != nil && mw.Interval != nil {
			desiredInterval = mw.Interval.Duration
		}

		log.Info("Starting Manual Intervention Required watchdog", "cluster", c.namespacedName(), "interval", desiredInterval)
		c.mirWatchdog = StartMirWatchdog(c, desiredInterval)
		return
	}
}

func (c *Cluster) StopMirWatchdog() {
	log.Info("Stopping Manual Intervention Required watchdog", "cluster", c.namespacedName())
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionManualInterventionRequired)
	metrics.ManualInterventionRequiredMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Set(0)
	c.mirWatchdog.Stop()
}

func (c *Cluster) InitCounterMetrics() {
	metrics.BackupJobsCreatedTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Add(0)
	metrics.InPlaceUpgradeFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.InPlaceUpgradeTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.PodReplacementsMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.PodReplacementsFailedMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.ReconcileFailureMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Add(0)
	metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "paused"})...).Add(0)
	metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "success"})...).Add(0)
	metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "error"})...).Add(0)
	metrics.SwapRebalanceFailuresMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.SwapRebalancesTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.RebalanceAttemptsTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.RebalanceAttemptFailuresTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.RebalancesTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.RebalancesFailedTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Add(0)
	metrics.RebalanceTimeSecondsMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Set(0)
	metrics.ManualInterventionRequiredMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Set(0)
}

func (c *Cluster) isMirWatchdogEnabled() bool {
	return c.cluster.Spec.MirWatchdog != nil && c.cluster.Spec.MirWatchdog.Enabled != nil && *c.cluster.Spec.MirWatchdog.Enabled
}

func (c *Cluster) isMirWatchdogRunning() bool {
	return c.mirWatchdog != nil && c.mirWatchdog.isRunning()
}

// memberFromPod constructs a Member from a pod's labels and annotations.
// This is used for pods that are not yet in c.members (e.g., during async initialization).
func (c *Cluster) memberFromPod(pod *v1.Pod) couchbaseutil.Member {
	labels := pod.GetLabels()
	configName := labels[constants.LabelNodeConf]
	version := pod.Annotations[constants.CouchbaseVersionAnnotationKey]
	_, secure := pod.Annotations[constants.PodTLSAnnotation]
	hostname := pod.Annotations[constants.CouchbaseHostnameAnnotation]
	image := extractCouchbaseImage(pod)

	member := couchbaseutil.NewMember(pod.Namespace, c.cluster.Name, pod.Name, version, configName, secure, image)
	if hostname != "" && hostname != member.GetDNSName() {
		member = couchbaseutil.NewExtConnectedMember(pod.Namespace, c.cluster.Name, pod.Name, version, configName, secure, hostname, image)
	}

	return member
}

// isPodActuallyReady checks if a pod is running and the main container is ready.
func (c *Cluster) isPodActuallyReady(pod *v1.Pod) bool {
	// Pod must be in running phase
	if pod.Status.Phase != v1.PodRunning {
		return false
	}

	return k8sutil.IsPodMainContainerReady(pod)
}

// handleReadyPendingPod routes a Running pod with PendingInitializationCondition to
// the correct initialization path:
//  1. Already-initialized annotation present AND ClusterID set → condition clear missed; retry clear.
//  2. Existing cluster (callableMembers non-empty AND ClusterID set) → initMember + CBS-add.
//  3. PVC recovery (ClusterID set, pod recoverable) → set initialized + clear condition.
//  4. Initial cluster creation (fallthrough) → initMember + configureInitialMember + UUID fetch.
func (c *Cluster) handleReadyPendingPod(pod *v1.Pod) {
	if pod.DeletionTimestamp != nil {
		log.V(1).Info("Pod has deletion timestamp, skipping pending init",
			"cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	log.V(1).Info("Pod is ready, attempting to initialize", "cluster", c.namespacedName(), "pod", pod.Name)

	member := c.memberFromPod(pod)
	config := c.cluster.Spec.GetServerConfigByName(member.Config())
	if config == nil {
		log.Error(errors.NewStackTracedError(errors.ErrInternalError), "Cannot initialize pod: config not found",
			"cluster", c.namespacedName(), "pod", pod.Name, "config", member.Config())
		return
	}

	// Early-exit: if the pod already has the initialized annotation it was CBS-added in a
	// previous cycle but ClearPodPendingInitialization failed. Retry the clear and return —
	// no need to re-run initMember or AddNode.
	// Also requires ClusterID: if configureInitialMember succeeded (setting annotation)
	// but fetchAndPersistClusterUUID failed (ClusterID=""), path 4 must retry the UUID fetch.
	if _, ok := pod.Annotations[constants.PodInitializedAnnotation]; ok && c.cluster.Status.ClusterID != "" {
		if err := k8sutil.ClearPodPendingInitialization(c.k8s, pod); err != nil {
			log.Error(err, "Failed to clear stale pending initialization condition",
				"cluster", c.namespacedName(), "pod", pod.Name)
		}
		return
	}

	if !c.callableMembers.Empty() && c.cluster.Status.ClusterID != "" {
		// Pod already callable in CBS — skip initMember/addNode (which would
		// fail on a configured node) and just mark initialized.
		if _, alreadyCallable := c.callableMembers[member.Name()]; alreadyCallable {
			c.handlePendingPodAlreadyInitialized(pod, member)
			return
		}

		c.handlePendingPodForExistingCluster(pod, member, config)
		return
	}

	if c.cluster.Status.ClusterID != "" && c.isPodRecoverable(member) {
		c.handlePendingPodAlreadyInitialized(pod, member)
		return
	}

	c.handlePendingPodForNewCluster(pod, member, config)
}

// handlePendingPodForExistingCluster initializes a pod being added to an existing cluster:
// CBS hostname/TLS/storage init, then addNode. On success: set initialized + clear condition.
func (c *Cluster) handlePendingPodForExistingCluster(pod *v1.Pod, member couchbaseutil.Member, config *couchbasev2.ServerConfig) {
	if err := c.initMember(c.ctx, member, *config, false); err != nil {
		log.Error(err, "Pod initialization failed, will retry next cycle",
			"cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	url := member.GetDNSName()
	if _, ok := c.cluster.Annotations[constants.AddNodeInsecureAnnotation]; ok {
		url = member.GetHostURLPlaintext()
	}

	services, err := couchbaseutil.ServiceListFromStringArray(
		couchbasev2.ServiceList(config.Services).StringSlice())
	if err != nil {
		log.Error(err, "Failed to build services list for pod",
			"cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	if err := c.AddNodeWithPodReadyCheck(member, url, services, c.readyMembers(), asyncAddNodeRetryPeriod); err != nil {
		log.Error(err, "Failed to add pod to cluster, will retry next cycle",
			"cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	ctx, cancelWait := context.WithTimeout(c.ctx, time.Minute)
	defer cancelWait()
	if err := c.waitForPodAdded(ctx, member); err != nil {
		log.Error(err, "Node did not reach inactiveAdded state after addNode, will retry next cycle",
			"cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	log.Info("Operator added member", "cluster", c.namespacedName(), "name", member.Name())
	c.raiseEvent(k8sutil.MemberAddEvent(member.Name(), c.cluster))

	// Fire upgrade metrics at the correct semantic point: after CBS acknowledged the node.
	// The durable UpgradeTrackingAnnotation survives reconcile cycles and restarts.
	// It is NOT cleared here — cleared by handleUpgradeNode after stabilization.
	switch pod.Annotations[constants.UpgradeTrackingAnnotation] {
	case string(couchbasev2.InPlaceUpgrade):
		metrics.InPlaceUpgradeTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		metrics.PodReplacementsMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
	case string(couchbasev2.SwapRebalance):
		metrics.SwapRebalancesTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
		metrics.PodReplacementsMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Inc()
	}

	if condition := k8sutil.GetPodCondition(pod, k8sutil.PodPendingInitializationCondition); condition != nil {
		metrics.PodReadinessDurationMetric.WithLabelValues(
			c.addOptionalLabelValues([]string{c.cluster.Name, config.Name})...,
		).Observe(float64(time.Since(condition.LastTransitionTime.Time)))
	}

	if err := k8sutil.SetPodInitialized(c.k8s, member.Name()); err != nil {
		log.Error(err, "Failed to set pod initialized",
			"cluster", c.namespacedName(), "pod", pod.Name)
		// Do NOT fall through to ClearPodPendingInitialization — without the initialized
		// annotation, kill-uninitialized-pods would delete a live CBS-active node next cycle.
		return
	}

	if err := k8sutil.ClearPodPendingInitialization(c.k8s, pod); err != nil {
		log.Error(err, "Failed to clear pending initialization condition")
	}
}

// handlePendingPodAlreadyInitialized handles a pod that is already known to CBS
// (either recovering with its PVC or after a previous CBS-add where SetPodInitialized
// failed). No CBS init or addNode is needed — just mark initialized and clear PendingInit.
func (c *Cluster) handlePendingPodAlreadyInitialized(pod *v1.Pod, member couchbaseutil.Member) {
	log.V(1).Info("Pod already initialized in CBS, marking initialized",
		"cluster", c.namespacedName(), "pod", pod.Name)
	// SetPodInitialized BEFORE ClearPodPendingInitialization: if Set succeeds but Clear
	// fails, the early-exit in handleReadyPendingPod retries the clear on the next cycle.
	if err := k8sutil.SetPodInitialized(c.k8s, member.Name()); err != nil {
		log.Error(err, "Failed to set pod initialized", "cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	if err := k8sutil.ClearPodPendingInitialization(c.k8s, pod); err != nil {
		log.Error(err, "Failed to clear pending initialization condition")
	}
}

// handlePendingPodForNewCluster initializes the very first member of a new cluster:
// CBS init, cluster-level configuration (passwords, RAM quotas), then UUID fetch.
func (c *Cluster) handlePendingPodForNewCluster(pod *v1.Pod, member couchbaseutil.Member, config *couchbasev2.ServerConfig) {
	log.V(1).Info("Initializing initial cluster member", "cluster", c.namespacedName(), "pod", pod.Name)

	if err := c.initMember(c.ctx, member, *config, false); err != nil {
		log.Error(err, "Failed to initialize ready pod", "cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	c.clusterAddMember(member)

	// If the pod is already initialized (annotation set from a previous cycle where
	// configureInitialMember succeeded but fetchAndPersistClusterUUID failed), skip
	// reconfiguration and go straight to the UUID fetch — idempotent via Upsert.
	if _, ok := pod.Annotations[constants.PodInitializedAnnotation]; !ok {
		if err := c.configureInitialMember(member, config); err != nil {
			log.Error(err, "Failed to configure initial member")
			c.callableMembers = couchbaseutil.MemberSet{}
			return
		}
	}

	uuid, err := c.fetchAndPersistClusterUUID(member)
	if err != nil {
		log.Error(err, "Failed to fetch cluster UUID")
		c.callableMembers = couchbaseutil.MemberSet{}
		return
	}

	c.cluster.Status.SetClusterID(uuid)
	c.cluster.Status.SetBalancedCondition()

	log.Info("Operator added member", "cluster", c.namespacedName(), "name", member.Name())
	c.raiseEvent(k8sutil.MemberAddEvent(member.Name(), c.cluster))

	if condition := k8sutil.GetPodCondition(pod, k8sutil.PodPendingInitializationCondition); condition != nil {
		metrics.PodReadinessDurationMetric.WithLabelValues(
			c.addOptionalLabelValues([]string{c.cluster.Name, config.Name})...,
		).Observe(float64(time.Since(condition.LastTransitionTime.Time)))
	}

	if err := k8sutil.ClearPodPendingInitialization(c.k8s, pod); err != nil {
		log.Error(err, "Failed to clear pending initialization condition")
	}

	log.V(1).Info("Pod initialization complete", "cluster", c.namespacedName(), "pod", pod.Name)
}

// handleTimedOutPendingPod cleans up a pod that has exceeded its initialization timeout.
func (c *Cluster) handleTimedOutPendingPod(pod *v1.Pod) {
	log.Info("Pod initialization timed out, removing", "cluster", c.namespacedName(), "pod", pod.Name)

	if pod.Status.Phase != v1.PodRunning {
		if serverGroup, ok := pod.Spec.NodeSelector[constants.ServerGroupLabel]; ok && serverGroup != "" {
			if err := c.addFailedSchedulingServerGroups(serverGroup); err != nil {
				log.Error(err, "Failed to record server group scheduling failure",
					"cluster", c.namespacedName(), "pod", pod.Name, "serverGroup", serverGroup)
			}
		}
	}

	// Remove the pod first. If this fails, the condition remains set so the next
	// cycle retries the removal. Clearing the condition before removal would
	// expose the pod to handleUnclusteredNodes which would attempt to CBS-add it.
	if err := c.removePod(pod.Name, true); err != nil {
		log.Error(err, "Failed to remove timed-out pending pod", "cluster", c.namespacedName(), "pod", pod.Name)
		return
	}

	// Condition cleared only after successful removal.
	if err := k8sutil.ClearPodPendingInitialization(c.k8s, pod); err != nil {
		log.Error(err, "Failed to clear pending initialization condition", "pod", pod.Name)
	}

	c.raiseEventCached(k8sutil.MemberCreationFailedEvent(pod.Name, c.cluster))
}

// updatePendingInitializationConditions processes pods with the PendingInitialization condition.
// Ready pods are initialized; timed-out pods are cleaned up. Errors are logged by the
// individual handlers and do not abort processing of other pods.
func (c *Cluster) updatePendingInitializationConditions() {
	pendingPods := c.getPendingInitPods()
	if len(pendingPods) == 0 {
		return
	}

	for _, pod := range pendingPods {
		condition := k8sutil.GetPodCondition(pod, k8sutil.PodPendingInitializationCondition)
		if condition == nil {
			continue
		}

		// Ready pods should be CBS-added regardless of how long they took.
		if c.isPodActuallyReady(pod) {
			c.handleReadyPendingPod(pod)
			continue
		}

		podAge := time.Since(condition.LastTransitionTime.Time)
		if podAge > c.config.PodCreateTimeout {
			c.handleTimedOutPendingPod(pod)
			continue
		}

		log.V(1).Info("Pod pending initialization, not yet ready",
			"cluster", c.namespacedName(), "pod", pod.Name, "age", podAge.Round(time.Second))
	}
}

// getPendingInitPods returns all cluster pods that have the PendingInitializationCondition.
func (c *Cluster) getPendingInitPods() []*v1.Pod {
	clusterPods := c.getClusterPods()

	var pendingPods []*v1.Pod
	for _, pod := range clusterPods {
		if k8sutil.HasPendingInitializationCondition(pod) {
			pendingPods = append(pendingPods, pod)
		}
	}

	return pendingPods
}

// fetchAndPersistClusterUUID retries until it fetches a non-empty cluster UUID from the
// Couchbase pools API, then stores it in persistence.
func (c *Cluster) fetchAndPersistClusterUUID(target any) (string, error) {
	var uuid string

	callback := func() error {
		info := &couchbaseutil.PoolsInfo{}
		if err := couchbaseutil.GetPools(info).On(c.api, target); err != nil {
			return err
		}

		uuid = info.GetUUID()
		if uuid == "" {
			return fmt.Errorf("cluster UUID not set: %w", errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		return nil
	}

	if err := retryutil.RetryWithBackoff(time.Second, time.Minute, callback); err != nil {
		return "", err
	}

	// Upsert and not Insert incase the cluster UUID was already set by a previous pod that initialized and fetched the UUID but failed to clear its pending initialization condition / the operator restarted.
	if err := c.state.Upsert(persistence.UUID, uuid); err != nil {
		return "", err
	}

	return uuid, nil
}
