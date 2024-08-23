package cluster

import (
	"context"
	goerrors "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
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
	PodCreateTimeout   time.Duration
	PodDeleteDelay     time.Duration
	PodReadinessDelay  time.Duration
	PodReadinessPeriod time.Duration
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

	// generation is the most recent resource generation we know about.  For
	// some reason a read after write can go back in time, I'm not certain it's
	// caching we are doing, but the API itself.
	generation int64

	// tlsCache allows us to load up and verify TLS data at the beginning of
	// a reconcile so it appears atomic throughout the process.
	tlsCache *tlsCache
}

// namespacedName returns a unique identifier for a cluster within Kubernetes.
// controller-runtime is actually just using the raw NamespacedName in its logs
// these days, maintaining the structured JSON, so perhaps we should do the same
// and adjust tooling to match.
func (c *Cluster) namespacedName() string {
	return c.cluster.NamespacedName()
}

// New is called when we first observe a CouchbaseCluster resource.  This may be due to
// creation or recovery after an Operator restart.
func New(config Config, cluster *couchbasev2.CouchbaseCluster) (*Cluster, error) {
	c := &Cluster{
		config:          config,
		cluster:         cluster,
		eventCache:      lru.New(1024),
		recoveryTime:    map[string]time.Time{},
		members:         couchbaseutil.MemberSet{},
		callableMembers: couchbaseutil.MemberSet{},
		generation:      cluster.Generation,
	}

	log.Info("Watching new cluster", "cluster", c.namespacedName())

	// Cancel is used to abort the go routine when the operator is deleted
	c.ctx, c.cancel = context.WithCancel(context.Background())

	var err error

	// Initialize Kubernetes clients.
	c.k8s, err = client.NewClient(c.ctx, c.cluster.Namespace, labels.SelectorFromSet(k8sutil.LabelsForCluster(c.cluster)))
	if err != nil {
		return nil, err
	}

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

	if err := annotations.Populate(&c.cluster.Spec, c.cluster.Annotations); err != nil {
		log.Error(err, "Failed to apply annotations to cluster spec", "cluster", c.namespacedName())
	}

	// No reason other than it's marginally quicker than waiting for the next run!
	c.runReconcile()

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

	// Clean up caches.
	c.k8s.Shutdown()
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
	failedGroups, err := c.state.Get(persistence.FailedSchedulingServerGroups)
	if err != nil && !goerrors.Is(err, persistence.ErrKeyError) {
		return err
	}

	if err := c.state.Clear(); err != nil {
		return err
	}

	if failedGroups != "" {
		if err := c.state.Insert(persistence.FailedSchedulingServerGroups, failedGroups); err != nil {
			return err
		}
	}

	// Once cleared, initialize the clients, using the underlying secrets as the source
	// of truth (as opposed to the persistent state data).
	if err := c.initClients(); err != nil {
		return err
	}

	if err := c.state.Insert(persistence.PodIndex, "0"); err != nil {
		return err
	}

	if err := c.state.Insert(persistence.Version, version); err != nil {
		return err
	}

	if err := c.state.Insert(persistence.Password, c.password); err != nil {
		return err
	}

	if err := c.state.Insert(persistence.Upgrading, string(persistence.UpgradeInactive)); err != nil {
		return err
	}

	tls := c.api.GetTLS()

	if tls != nil {
		if err := c.state.Insert(persistence.CACertificate, string(tls.CACert)); err != nil {
			return err
		}

		if tls.ClientAuth != nil {
			if err := c.state.Insert(persistence.ClientCertificate, string(tls.ClientAuth.Cert)); err != nil {
				return err
			}

			if err := c.state.Insert(persistence.ClientKey, string(tls.ClientAuth.Key)); err != nil {
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

	// Add the server groups that failed to schedule to the state.
	failedGroups := []string{}

	if failedGroupsString, err := c.state.Get(persistence.FailedSchedulingServerGroups); err != nil && !goerrors.Is(err, persistence.ErrKeyError) {
		return err
	} else if err == nil {
		failedGroups = strings.Split(failedGroupsString, ServerGroupAvoidDelimiter)
	}

	failedGroups = append(failedGroups, serverGroup)

	return c.state.Upsert(persistence.FailedSchedulingServerGroups, strings.Join(failedGroups, ServerGroupAvoidDelimiter))
}

func (c *Cluster) clearFailedSchedulingServerGroups() {
	if !c.isSGReschedulingEnabled() {
		return
	}

	if err := c.state.Delete(persistence.FailedSchedulingServerGroups); err != nil && !goerrors.Is(err, persistence.ErrKeyError) {
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

	start := time.Now()

	member, class, err := c.createInitialMember()
	if err != nil {
		return err
	}

	if err := c.configureInitialMember(member, class); err != nil {
		return err
	}

	log.Info("Operator added member", "cluster", c.namespacedName(), "name", member.Name())

	c.raiseEvent(k8sutil.MemberAddEvent(member.Name(), c.cluster))

	metrics.PodReadinessDurationMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name, class.Name})...).Observe(float64(time.Since(start)))

	// This takes a while to get set, yawn...
	var uuid string

	callback := func() error {
		info := &couchbaseutil.PoolsInfo{}
		if err := couchbaseutil.GetPools(info).On(c.api, member); err != nil {
			return err
		}

		uuid = info.GetUUID()
		if uuid == "" {
			return fmt.Errorf("cluster UUID not set: %w", errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		return nil
	}

	if err := retryutil.RetryFor(time.Minute, callback); err != nil {
		return err
	}

	if err := c.state.Insert(persistence.UUID, uuid); err != nil {
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

// runReconcile gathers a list of pods in cluster from Kubernetes, optionally
// initializes our internal member list if we need to e.g. we have been restarted and
// have lost state or a previous error may have resulted in inconsistent state, then
// compares reality with the specification and makes the former match it.
//
// It accepts a flag forcing it update internal state from Kubernetes, and returns a
// similar flag to indicate we require a forced update with the next invocation.
func (c *Cluster) runReconcile() {
	// Always update the cluster status and reconcile loop time.
	start := time.Now()

	defer func() {
		if err := c.updateCRStatus(); err != nil {
			log.Error(err, "Status update failed", "cluster", c.namespacedName())
		}

		reconcileTime := time.Since(start)

		metrics.ReconcileDurationMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Observe(reconcileTime.Seconds())
	}()

	// If the user has requested that we pause operations.
	if c.cluster.Spec.Paused {
		c.cluster.Status.PauseControl()
		log.Info("Operator paused, skipping", "cluster", c.namespacedName())

		metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "paused"})...).Inc()

		return
	}

	// Otherwise indicate that we are in control.
	c.cluster.Status.Control()

	running, pending := c.getClusterPodsByPhase()
	if len(pending) > 0 {
		// Pod startup might take long, e.g. pulling image. It would
		// deterministically become running or succeeded/failed later.
		log.Info("Pods pending creation, skipping", "cluster", c.namespacedName(), "running", len(running), "pending", len(pending))

		metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "paused"})...).Inc()

		return
	}

	// Members are updated each iteration by performing a union of Kubernetes resources
	// we discover, and any hosts that Couchbase knows about, if we can actually talk
	// to it.  By performing no caching the behaviour of the systems is identical during
	// runtime and after a restart.
	if err := c.updateMembers(); err != nil {
		log.Error(err, "Failed to update members", "cluster", c.namespacedName())

		metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "error"})...).Inc()
		metrics.ReconcileFailureMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Inc()

		// When we call updateMembers, it's going to look at all running pods can try
		// to dial Couchbase and get health status.  It's entirely possible that the
		// operator got killed or rescheduled before pods were correctly initialized,
		// and thus will not respond to our pleas for help.  Execute any uninitialized
		// nodes, so that we may recreate the cluster next time around.
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

	// Finally reconcile state according to the specification.
	// Every reconcile should either set or clear the error condition.
	// This lets us spot very easily any persistent error conditions from
	// external tooling (rather than them going into a log).  For example,
	// if I upgrade the operator, does it break?  If I start in a broken
	// state and take some action, does it fix itself.  The other added
	// bonus is it will show up on dashboards like a christmas tree.
	if err := c.reconcile(); err != nil {
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

		metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "error"})...).Inc()
		metrics.ReconcileFailureMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name})...).Inc()

		return
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionError)

	if err := c.updateCRStatus(); err != nil {
		log.Info("unable to update status", "cluster", c.namespacedName(), "error", err)
	}

	metrics.ReconcileTotalMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Namespace, c.cluster.Name, "paused"})...).Inc()
}

// Update is called periodically or on a CR change, print out any diffs in the spec
// then update the specification and unconditionally reconcile.
func (c *Cluster) Update(cluster *couchbasev2.CouchbaseCluster) {
	if cluster.Generation < c.generation {
		log.Info("API returned old version, skipping reconcile", "cluster", c.namespacedName())
		return
	}

	if err := annotations.Populate(&cluster.Spec, cluster.Annotations); err != nil {
		log.Error(err, "Failed to apply annotations to cluster spec", "cluster", c.namespacedName())
	}

	if !reflect.DeepEqual(cluster.Spec, c.cluster.Spec) {
		c.logUpdate(c.cluster.Spec, cluster.Spec)
	}

	c.cluster = cluster
	c.runReconcile()
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

// Selects any member that can be recovered and attempts to restart it.
func (c *Cluster) recoverClusterDown() (bool, error) {
	// Use Names() as that returns a deterministic/sorted list for testing.
	for _, name := range c.members.Names() {
		m := c.members[name]
		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
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
		log.V(1).Info("No TLS configuration cached")

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

// setVolumeExpansionState sets volume expansion state
// if enable flag is 'true' otherwise state is deleted.
func (c *Cluster) setVolumeExpansionState(enable bool) (err error) {
	if enable {
		err = c.state.Insert(persistence.VolumeExpansion, "true")
	} else {
		err = c.state.Delete(persistence.VolumeExpansion)
	}

	return err
}

// checkVolumeExpansionState simplifies inquiries as to
// whether or not the volume expansion state is set.
func (c *Cluster) checkVolumeExpansionState() bool {
	if _, err := c.state.Get(persistence.VolumeExpansion); err != nil {
		// not returning actual error, but instead false implies the bit is missing
		return false
	}

	return true
}

func (c *Cluster) logStatus(status *MemberState) {
	status.LogStatus(c.namespacedName())
	c.scheduler.LogStatus(c.namespacedName())
}

// hibernate puts the cluster to sleep, zzzz.
func (c *Cluster) hibernate() error {
	// Don't hibernate if the cluster is rebalancing otherwise things go bad
	if isRebalancing, err := c.isClusterRebalancing(); err != nil {
		return err
	} else if isRebalancing {
		log.Info("[WARN] The cluster is currently rebalancing, waiting for rebalance to complete before hibernating cluster")
		return nil
	}

	for _, pod := range c.getClusterPods() {
		log.Info("Hibernating pod", "cluster", c.namespacedName(), "name", pod.Name)

		if err := c.k8s.KubeClient.CoreV1().Pods(c.cluster.Namespace).Delete(context.Background(), pod.Name, *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionAvailable)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionBalanced)
	c.cluster.Status.SetHibernatingCondition("Cluster hibernating")

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	log.Info("Cluster is hibernating", "cluster", c.namespacedName())

	return nil
}

func (c *Cluster) isClusterRebalancing() (bool, error) {
	rebalanceProgress := couchbaseutil.RebalanceProgress{}

	if err := couchbaseutil.GetRebalanceProgress(&rebalanceProgress).On(c.api, c.readyMembers()); err != nil {
		return true, err
	}

	switch rebalanceProgress.Status {
	case couchbaseutil.RebalanceStatusNone, couchbaseutil.RebalanceStatusNotRunning:
		return false, nil
	}

	return true, nil
}
