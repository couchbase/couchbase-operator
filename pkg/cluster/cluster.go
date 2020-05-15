package cluster

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/diff"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"

	"github.com/golang/groupcache/lru"
	"github.com/prometheus/client_golang/prometheus"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var log = logf.Log.WithName("cluster")

var (
	// Be very aggressive here.  If pods are deleted with volumes missing then
	// they stay Terminating forever.  This should emulate --grace-period=0 --force
	// See: https://github.com/kubernetes/kubernetes/issues/51835
	podTerminationGracePeriod = int64(0)

	reconcileTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "couchbase_reconcile_total",
		Help: "Total reconcile operations performed on a specifc cluster",
	}, []string{"namespace", "name", "result"})

	reconcileFailureMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "couchbase_reconcile_failures",
		Help: "Total failed reconcile operations performed on a specifc cluster",
	}, []string{"namespace", "name"})

	reconcileDurationMetric = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "couchbase_reconcile_time_seconds",
		Help: "Length of time per reconcile for a specific cluster",
	}, []string{"namespace", "name"})
)

func init() {
	metrics.Registry.MustRegister(
		reconcileTotalMetric,
		reconcileFailureMetric,
		reconcileDurationMetric,
	)
}

const (
	tlsOperatorSecretCACert string = "ca.crt"
	tlsOperatorSecretCert   string = "couchbase-operator.crt"
	tlsOperatorSecretKey    string = "couchbase-operator.key"
)

type Config struct {
	PodCreateTimeout string
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
func New(config Config, cl *couchbasev2.CouchbaseCluster) (*Cluster, error) {
	c := &Cluster{
		config:          config,
		cluster:         cl,
		eventCache:      lru.New(1024),
		recoveryTime:    map[string]time.Time{},
		members:         couchbaseutil.MemberSet{},
		callableMembers: couchbaseutil.MemberSet{},
	}

	// Cancel is used to abort the go routine when the operator is deleted
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// Initialize Kubernetes clients and caches.
	var err error

	c.k8s, err = client.NewClient(c.ctx, c.cluster.Namespace, labels.SelectorFromSet(k8sutil.LabelsForCluster(c.cluster)))
	if err != nil {
		return nil, err
	}

	// Create a new persistence layer to store and retrieve state.  Add in
	// defaults if they don't exist.
	if c.state, err = persistence.New(c.k8s.KubeClient, cl); err != nil {
		return nil, err
	}

	log.Info("Watching new cluster", "cluster", c.namespacedName())

	// Initialize the scheduler for the initial pod
	if c.scheduler, err = scheduler.New(c.k8s, c.cluster); err != nil {
		log.Error(err, "Error initializing pod scheduler", "cluster", c.namespacedName())
		return nil, err
	}

	// Spawn the janitor process which monitors persistent log volumes.
	go newJanitor(c).run()

	if err := c.setupAuth(c.cluster.Spec.Security.AdminSecret); err != nil {
		return nil, err
	}

	if err := c.initCouchbaseClient(); err != nil {
		return nil, err
	}

	// Perform any necessary upgrades to the cluster and kubernetes resources.
	if err := c.operatorUpgrade(); err != nil {
		return nil, err
	}

	log.Info("Running", "cluster", c.namespacedName())

	c.runReconcile()

	return c, nil
}

func (c *Cluster) Delete() {
	// Notify client operations to stop what they are doing e.g. abort retry loops
	c.cancel()

	// Clean up caches.
	c.k8s.Shutdown()
}

func (c *Cluster) create() error {
	log.Info("Cluster does not exist so the operator is attempting to create it", "cluster", c.namespacedName())

	version, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return err
	}

	// Clear the persistent state for a new cluster, it may be doing DR and we need
	// to go off the spec, not what is in memory.
	if err := c.state.Clear(); err != nil {
		return err
	}

	if err := c.state.Insert(persistence.PodIndex, "0"); err != nil {
		return err
	}

	if err := c.state.Insert(persistence.Version, version); err != nil {
		return err
	}

	c.cluster.Status.CurrentVersion = version

	if len(c.cluster.Spec.Servers) == 0 {
		return fmt.Errorf("cluster create: no server specification defined")
	}

	idx := c.indexOfServerConfigWithService(couchbasev2.DataService)
	if idx == -1 {
		return fmt.Errorf("cluster create: at least one server specification must contain the `data` service")
	}

	c.members = couchbaseutil.NewMemberSet()

	m, err := c.createMember(c.cluster.Spec.Servers[idx])
	if err != nil {
		return err
	}

	log.Info("Operator added member", "cluster", c.namespacedName(), "name", m.Name)

	c.raiseEvent(k8sutil.MemberAddEvent(m.Name, c.cluster))

	if err := c.initMember(m, c.cluster.Spec.Servers[idx]); err != nil {
		return err
	}

	// This takes a while to get set, yawn...
	var uuid string

	callback := func() error {
		info := &couchbaseutil.PoolsInfo{}
		if err := couchbaseutil.GetPools(info).On(c.api, m); err != nil {
			return err
		}

		uuid = info.GetUUID()
		if uuid == "" {
			return fmt.Errorf("cluster UUID not set")
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(c.ctx, time.Minute)
	defer cancel()

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
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
		reconcileDurationMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name).Observe(reconcileTime.Seconds())
	}()

	// If the user has requested that we pause operations.
	if c.cluster.Spec.Paused {
		c.cluster.Status.PauseControl()
		log.Info("Operator paused, skipping", "cluster", c.namespacedName())
		reconcileTotalMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name, "paused").Inc()

		return
	}

	// Otherwise indicate that we are in control.
	c.cluster.Status.Control()

	running, pending := c.pollPods()
	if len(pending) > 0 {
		// Pod startup might take long, e.g. pulling image. It would
		// deterministically become running or succeeded/failed later.
		log.Info("Pods pending creation, skipping", "cluster", c.namespacedName(), "running", len(running), "pending", len(pending))
		reconcileTotalMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name, "pending").Inc()

		return
	}

	// Members are updated each iteration by performing a union of Kubernetes resources
	// we discover, and any hosts that Couchbase knows about, if we can actually talk
	// to it.  By performing no caching the behaviour of the systems is identical during
	// runtime and after a restart.
	if err := c.updateMembers(); err != nil {
		log.Error(err, "Failed to update members", "cluster", c.namespacedName())
		reconcileTotalMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name, "error").Inc()
		reconcileFailureMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name).Inc()

		return
	}

	// Finally reconcile state according to the specification.
	if err := c.reconcile(); err != nil {
		log.Error(err, "Reconciliation failed", "cluster", c.namespacedName())
		reconcileTotalMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name, "error").Inc()
		reconcileFailureMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name).Inc()

		return
	}

	reconcileTotalMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name, "success").Inc()
}

// Update is called periodically or on a CR change, print out any diffs in the spec
// then update the specification and unconditionally reconcile.
func (c *Cluster) Update(cluster *couchbasev2.CouchbaseCluster) {
	if !reflect.DeepEqual(cluster.Spec, c.cluster.Spec) {
		c.logUpdate(c.cluster.Spec, cluster.Spec)
	}

	c.cluster = cluster
	c.runReconcile()
}

func (c *Cluster) logUpdate(old, new interface{}) {
	d, err := diff.Diff(old, new)
	if err != nil {
		log.Error(err, "cluster", c.namespacedName())
		return
	}

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
	cluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Get(c.cluster.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Ignore the case where nothing needs to be updated
	if reflect.DeepEqual(c.cluster.Status, cluster.Status) {
		return nil
	}

	c.logUpdate(cluster.Status, c.cluster.Status)

	// Copy the updated status to our cluster object and try update it
	cluster.Status = c.cluster.Status

	if _, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Update(cluster); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) isSecureClient() bool {
	return c.cluster.Spec.Networking.TLS.IsSecureClient()
}

func (c *Cluster) createPod(ctx context.Context, m *couchbaseutil.Member, serverSpec couchbasev2.ServerConfig) error {
	log.Info("Creating pod", "cluster", c.namespacedName(), "name", m.Name, "image", c.cluster.Spec.CouchbaseImage())

	_, err := k8sutil.CreateCouchbasePod(ctx, c.k8s, c.scheduler, c.cluster, m, serverSpec)

	return err
}

// Remove Pod and any volumes associated with pod if requested
// ore volumes are associated with default claim.
func (c *Cluster) removePod(name string, removeVolumes bool) error {
	opts := metav1.NewDeleteOptions(podTerminationGracePeriod)

	err := k8sutil.DeleteCouchbasePod(c.k8s, c.cluster.Namespace, name, opts, removeVolumes)
	if err != nil {
		log.Error(err, "Pod deletion failed", "cluster", c.namespacedName())
		return err
	}

	log.Info("Pod deleted", "cluster", c.namespacedName(), "name", name)

	return nil
}

// Delete pod and create with same name.
// Persisted members will reuse volume mounts.
func (c *Cluster) recreatePod(m *couchbaseutil.Member) error {
	config := c.cluster.Spec.GetServerConfigByName(m.ServerConfig)
	if config == nil {
		return fmt.Errorf("config for pod does not exist: %s", m.ServerConfig)
	}

	opts := metav1.NewDeleteOptions(podTerminationGracePeriod)

	if err := k8sutil.DeletePod(c.k8s, c.cluster.Namespace, m.Name, opts); err != nil {
		return err
	}

	if err := c.waitForDeletePod(m.Name, 120); err != nil {
		return err
	}

	// The pod creation timeout is global across this operation e.g. PVCs, pods, the lot.
	podCreateTimeout, err := time.ParseDuration(c.config.PodCreateTimeout)
	if err != nil {
		return fmt.Errorf("PodCreateTimeout improperly formatted: %v", err)
	}

	ctx, cancel := context.WithTimeout(c.ctx, podCreateTimeout)
	defer cancel()

	err = c.createPod(ctx, m, *config)
	if err != nil {
		return err
	}

	return c.waitForCreatePod(ctx, m)
}

// wait with context.
func (c *Cluster) waitForCreatePod(ctx context.Context, member *couchbaseutil.Member) error {
	if err := k8sutil.WaitForPod(ctx, c.k8s.KubeClient, c.cluster.Namespace, member.Name, member.GetHostPort()); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) waitForDeletePod(podName string, timeout int64) error {
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	if err := k8sutil.WaitForDeletePod(ctx, c.k8s.KubeClient, c.cluster.Namespace, podName); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) isPodRecoverable(m *couchbaseutil.Member) bool {
	config := c.cluster.Spec.GetServerConfigByName(m.ServerConfig)
	if config == nil {
		return false
	}

	if err := k8sutil.IsPodRecoverable(c.k8s, *config, m.Name); err != nil {
		log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", m.Name, "reason", err)
		return false
	}

	return true
}

// Selects any member that can be recovered and attempts to restart it.
func (c *Cluster) recoverClusterDown() error {
	// Use Names() as that returns a deterministic/sorted list for testing.
	for _, name := range c.members.Names() {
		m := c.members[name]
		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				return fmt.Errorf("node %s could not be recovered: %s", m.Name, err.Error())
			}

			log.Info("Pod recovering", "cluster", c.namespacedName(), "name", m.Name)
			c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name, c.cluster))

			break
		}
	}

	return nil
}

func (c *Cluster) pollPods() (running, pending []*v1.Pod) {
	pods := c.k8s.Pods.List()

	for _, pod := range pods {
		// Avoid polling deleted pods
		if pod.DeletionTimestamp != nil {
			continue
		}

		if len(pod.OwnerReferences) < 1 {
			log.Info("Pod ignored, no owner", "cluster", c.namespacedName(), "name", pod.Name)
			continue
		}

		if pod.OwnerReferences[0].UID != c.cluster.UID {
			log.Info("Pod ignored, invalid owner", "cluster", c.namespacedName(), "name", pod.Name, "cluster_uid", c.cluster.UID, "pod_uid", pod.OwnerReferences[0].UID)
			continue
		}

		switch pod.Status.Phase {
		case v1.PodRunning:
			running = append(running, pod)
		case v1.PodPending:
			pending = append(pending, pod)
		}
	}

	return running, pending
}

func (c *Cluster) updateMemberStatus(firstMember bool) error {
	// Hack, NS server doesn't start to work properly until the full initialization
	// sequence is performed, so a call to /pools/default will not work :sadpanda:
	// We need to avoid this code path in order to avoid this hack.
	if firstMember {
		c.updateMemberStatusWithClusterInfo(c.members, nil)
		return nil
	}

	status, err := c.GetStatus()
	if err != nil {
		return err
	}

	ready := couchbaseutil.MemberSet{}

	for name, member := range c.members {
		state, ok := status.NodeStates[name]
		if !ok {
			continue
		}

		if state == NodeStateActive {
			ready.Add(member)
		}
	}

	unready := c.members.Diff(ready)

	c.updateMemberStatusWithClusterInfo(ready, unready)

	return nil
}

// use cluster info to set ready members from active nodes
// and all remaining nodes as unready.
func (c *Cluster) updateMemberStatusWithClusterInfo(ready, unready couchbaseutil.MemberSet) {
	if c.cluster.Status.Members == nil {
		c.cluster.Status.Members = &couchbasev2.MembersStatus{}
	}

	c.cluster.Status.Members.SetReady(ready.Names())
	c.cluster.Status.Members.SetUnready(unready.Names())

	if err := c.updateCRStatus(); err != nil {
		log.Info("unable to update status", "cluster", c.namespacedName(), "error", err)
	}
}

// Use username and password from secret store.
func (c *Cluster) setupAuth(authSecret string) error {
	secret, found := c.k8s.Secrets.Get(authSecret)
	if !found {
		return fmt.Errorf("unable to get admin secret %s", authSecret)
	}

	data := secret.Data
	if username, ok := data[constants.AuthSecretUsernameKey]; ok {
		c.username = string(username)
	} else {
		return cberrors.ErrSecretMissingUsername{Reason: authSecret}
	}

	if password, ok := data[constants.AuthSecretPasswordKey]; ok {
		c.password = string(password)
	} else {
		return cberrors.ErrSecretMissingPassword{Reason: authSecret}
	}

	return nil
}

func (c *Cluster) initCouchbaseClient() error {
	log.Info("Couchbase client starting", "cluster", c.namespacedName())

	c.api = couchbaseutil.New(c.ctx, c.namespacedName(), c.username, c.password)

	if c.isSecureClient() {
		// Grab the operator secret
		secretName := c.cluster.Spec.Networking.TLS.Static.OperatorSecret

		secret, found := c.k8s.Secrets.Get(secretName)
		if !found {
			return fmt.Errorf("unable to get operator secret %s", secretName)
		}

		// Extract the data
		if _, ok := secret.Data[tlsOperatorSecretCACert]; !ok {
			return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretCACert)
		}

		// Add the TLS context
		tls := &couchbaseutil.TLSAuth{
			CACert: secret.Data[tlsOperatorSecretCACert],
		}

		// Optionally enable client authentication
		if c.cluster.Spec.Networking.TLS.ClientCertificatePolicy != nil {
			clientCert, clientKey, err := c.getTLSClientData()
			if err != nil {
				return err
			}

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

// Adds a new member to our cluster object and updates the cluster status.
func (c *Cluster) clusterAddMember(member *couchbaseutil.Member) error {
	firstMember := c.members.Empty()

	c.members.Add(member)
	c.callableMembers.Add(member)

	c.cluster.Status.Size = c.members.Size()

	return c.updateMemberStatus(firstMember)
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
	event, err := c.k8s.KubeClient.CoreV1().Events(c.cluster.Namespace).Create(event)
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

			e, err := c.k8s.KubeClient.CoreV1().Events(c.cluster.Namespace).Update(e)
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

// clients should use ready members that are available to service requests
// according to status readiness.  Otherwise, fallback to cluster members.
func (c *Cluster) readyMembers() couchbaseutil.MemberSet {
	// This used to cross reference with pod liveness.  Why?
	// Performance improvment?  UX?
	return c.callableMembers
}

// Check if volume only has log volumes mounted.
func (c *Cluster) memberHasLogVolumes(name string) bool {
	return k8sutil.MemberHasLogVolumes(c.k8s, name)
}

// getPodIndex returns the current pod naming index.
func (c *Cluster) getPodIndex() (int, error) {
	podIndexStr, err := c.state.Get(persistence.PodIndex)
	if err != nil {
		return -1, err
	}

	podIndex, err := strconv.Atoi(podIndexStr)
	if err != nil {
		return -1, err
	}

	return podIndex, nil
}

// setPodIndex updates the current pod naming index and commits to etcd.
func (c *Cluster) setPodIndex(index int) error {
	return c.state.Update(persistence.PodIndex, strconv.Itoa(index))
}

// incPodIndex gets the current pod naming index and increments it.
func (c *Cluster) incPodIndex() error {
	podIndex, err := c.getPodIndex()
	if err != nil {
		return err
	}

	return c.setPodIndex(podIndex + 1)
}

// decPodIndex gets the current pod naming index and decrements it.
func (c *Cluster) decPodIndex() error {
	podIndex, err := c.getPodIndex()
	if err != nil {
		return err
	}

	return c.setPodIndex(podIndex - 1)
}

func (c *Cluster) logStatus(status *MemberState) {
	status.LogStatus(c.namespacedName())
	c.scheduler.LogStatus(c.namespacedName())
}
