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
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	"github.com/couchbase/gocbmgr"

	"github.com/golang/groupcache/lru"
	"github.com/prometheus/client_golang/prometheus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
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

type Cluster struct {
	config       Config
	k8s          *client.Client
	cluster      *couchbasev2.CouchbaseCluster
	members      couchbaseutil.MemberSet
	username     string
	password     string
	client       *couchbaseutil.CouchbaseClient // Client to communicate with the Couchbase admin port
	ctx          context.Context                // Context used to cancel long running operations
	cancel       context.CancelFunc             // Closure on the context to indicate cancellation
	lastEvent    time.Time                      // When we raised the last event (see raiseEvent)
	scheduler    scheduler.Scheduler            // Pod placement scheduler
	forceUpdate  bool                           // Members are cached so we can trust dead ephemeral pods are ours, this allows us to force a refresh
	eventCache   *lru.Cache                     // Used to store events for aggregation
	state        persistence.PersistentStorage  // Used to store persistent cluster state
	recoveryTime map[string]time.Time           // Used to determine when to manually recover nodes
}

// namespacedName returns a unique identifier for a cluster within Kubernetes.
func (c *Cluster) namespacedName() string {
	return types.NamespacedName{Namespace: c.cluster.Namespace, Name: c.cluster.Name}.String()
}

// New is called when we first observe a CouchbaseCluster resource.  This may be due to
// creation or recovery after an Operator restart.
func New(config Config, cl *couchbasev2.CouchbaseCluster) (*Cluster, error) {
	c := &Cluster{
		config:       config,
		cluster:      cl,
		eventCache:   lru.New(1024),
		recoveryTime: map[string]time.Time{},
	}

	// Cancel is used to abort the go routine when the operator is deleted
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// Initialize Kubernetes clients and caches.
	var err error
	c.k8s, err = client.NewClient(c.ctx, c.cluster.Namespace, labels.SelectorFromSet(k8sutil.LabelsForCluster(c.cluster.Name)))
	if err != nil {
		return nil, err
	}

	// Create a new persistence layer to store and retrieve state.  Add in
	// defaults if they don't exist.
	if c.state, err = persistence.New(c.k8s.KubeClient, cl); err != nil {
		return nil, err
	}
	if err := c.state.Insert(persistence.Phase, string(couchbasev2.ClusterPhaseNone)); err != nil && !persistence.IsKeyError(err) {
		return nil, err
	}
	if err := c.state.Insert(persistence.PodIndex, "0"); err != nil && !persistence.IsKeyError(err) {
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

	if err := c.setup(); err != nil {
		log.Error(err, "Cluster setup failed", "cluster", c.namespacedName())
		c.cluster.Status.SetReason(err.Error())
		if err := c.updatePhase(couchbasev2.ClusterPhaseFailed); err != nil {
			return nil, err
		}
		return nil, err
	}

	if err := c.updatePhase(couchbasev2.ClusterPhaseRunning); err != nil {
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

func (c *Cluster) setup() error {
	phase, err := c.state.Get(persistence.Phase)
	if err != nil {
		return err
	}
	c.cluster.Status.SetPhase(couchbasev2.ClusterPhase(phase))
	if err := c.updateCRStatus(); err != nil {
		log.Info("warning failed to update cluster status", "cluster", c.namespacedName())
	}

	var shouldCreateCluster bool
	switch couchbasev2.ClusterPhase(phase) {
	case couchbasev2.ClusterPhaseNone:
		shouldCreateCluster = true
	case couchbasev2.ClusterPhaseCreating:
		return cberrors.ErrClusterCreating
	case couchbasev2.ClusterPhaseRunning:
		shouldCreateCluster = false
	default:
		return fmt.Errorf("unexpected cluster phase: %s", phase)
	}

	if err := c.setupAuth(c.cluster.Spec.Security.AdminSecret); err != nil {
		return err
	}

	if err := c.initCouchbaseClient(); err != nil {
		return err
	}

	// Establish DNS for cluster communication.
	if err := k8sutil.ReconcilePeerServices(c.k8s, c.cluster.Namespace, c.cluster.Name, c.cluster.AsOwner()); err != nil {
		return err
	}

	// Setup the UI so we can monitor cluster creation
	if err := c.reconcileAdminService(); err != nil {
		return err
	}

	if shouldCreateCluster {
		if err := c.create(); err != nil {
			return err
		}
	} else {
		log.Info("Cluster already exists, the operator will now manage it", "cluster", c.namespacedName())

		// Reload dynamic persistent storage.
		if err := c.reconcilePersistentStatus(); err != nil {
			return err
		}

		// TLS may have updated across the restart, so the client CA is valid
		// but the rest of the cluster is uncontactable.  For this we need a
		// set of members to iterate over, however the normal updateMembers()
		// tries to contact Couchbase server to verify membership, that will
		// fail as TLS invalid.  We need to just take what we are given before
		// making any calls to Couchbase server.
		running, _ := c.pollPods()
		c.members = podsToMemberSet(running)
		if err := c.reconcileTLS(); err != nil {
			return err
		}

		// Let the normal security machanisms kick in.
		c.members = nil

		// Perform any necessary upgrades to the cluster and kubernetes resources.
		if err := c.operatorUpgrade(); err != nil {
			return err
		}
	}

	// Once the cluster is guaranteed to exist, extract the UUID and inject
	// it into the client.  Connections from now on will be aborted if the
	// UUID reported on the persistent connection differs
	c.client.SetUUID(c.cluster.Status.ClusterID)

	return nil
}

// updatePhase updates the cluster phase in both persistent state and the status.
func (c *Cluster) updatePhase(phase couchbasev2.ClusterPhase) error {
	if err := c.state.Update(persistence.Phase, string(phase)); err != nil {
		return err
	}
	c.cluster.Status.SetPhase(phase)
	if err := c.updateCRStatus(); err != nil {
		log.Info("warning failed to update cluster status", "cluster", c.namespacedName())
	}
	return nil
}

func (c *Cluster) create() error {
	log.Info("Cluster does not exist so the operator is attempting to create it", "cluster", c.namespacedName())

	version, err := k8sutil.CouchbaseVersion(c.cluster.Spec.Image)
	if err != nil {
		return err
	}

	if err := c.state.Insert(persistence.Version, version); err != nil {
		return err
	}
	c.cluster.Status.CurrentVersion = version

	if err := c.updatePhase(couchbasev2.ClusterPhaseCreating); err != nil {
		return err
	}

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

	uuid, err := c.client.ClusterUUID(m)
	if err != nil {
		return err
	}

	if err := c.state.Insert(persistence.UUID, uuid); err != nil {
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

	// When cluster is being restored or reconcile error has occurred then
	// then the memberset can be updated from the status of running pods and
	// persistent volumes.
	if c.forceUpdate || c.members.Empty() {
		c.forceUpdate = false
		if err := c.updateMembers(podsToMemberSet(running)); err != nil {
			log.Error(err, "Failed to update members", "cluster", c.namespacedName())
			reconcileTotalMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name, "error").Inc()
			reconcileFailureMetric.WithLabelValues(c.cluster.Namespace, c.cluster.Name).Inc()
			c.forceUpdate = true
			return
		}
	}

	// Finally reconcile state according to the specification.
	if err := c.reconcile(running); err != nil {
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
	log.Info("Resource updated", "cluster", c.namespacedName(), "diff", d)
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
	log.Info("Creating pod", "cluster", c.namespacedName(), "name", m.Name, "image", c.cluster.Spec.Image)
	_, err := k8sutil.CreateCouchbasePod(c.k8s, c.scheduler, c.cluster, m, serverSpec, ctx)
	return err
}

// Remove Pod and any volumes associated with pod if requested
// ore volumes are associated with default claim
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
// Persisted members will reuse volume mounts
func (c *Cluster) recreatePod(m *couchbaseutil.Member) error {
	config := c.cluster.Spec.GetServerConfigByName(m.ServerConfig)
	if config == nil {
		return fmt.Errorf("config for pod does not exist: %s", m.ServerConfig)
	}
	opts := metav1.NewDeleteOptions(podTerminationGracePeriod)
	err := k8sutil.DeletePod(c.k8s, c.cluster.Namespace, m.Name, opts)
	if err != nil {
		return err
	}
	err = c.waitForDeletePod(m.Name, 120)
	if err != nil {
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

// wait with context
func (c *Cluster) waitForCreatePod(ctx context.Context, member *couchbaseutil.Member) error {
	if err := k8sutil.WaitForPod(ctx, c.k8s.KubeClient, c.cluster.Namespace, member.Name, member.HostURL()); err != nil {
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
	recoverable := false
	if config := c.cluster.Spec.GetServerConfigByName(m.ServerConfig); config != nil {
		err := k8sutil.IsPodRecoverable(c.k8s, *config, m.Name)
		if err != nil {
			log.Info("Pod unrecoverable", "cluster", c.namespacedName(), "name", m.Name, "reason", err)
		} else {
			recoverable = true
		}
	}
	return recoverable
}

// Selects any member that can be recovered and attempts to restart it
func (c *Cluster) recoverClusterDown() error {
	// Use Names() as that returns a deterministic/sorted list for testing.
	for _, name := range c.members.Names() {
		m := c.members[name]
		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				return fmt.Errorf("node %s could not be recovered: %s", m.ClientURL(), err.Error())
			} else {
				log.Info("Pod recovering", "cluster", c.namespacedName(), "name", m.Name)
				c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name, c.cluster))
				break
			}
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

func (c *Cluster) updateMemberStatus() error {
	// The first member will get here when the pod is brought up, however calls
	// to get the cluster status will fail as it hasn't been initialized yet, so
	// instead just return an empty status
	var info *couchbaseutil.ClusterStatus
	if c.cluster.Status.ClusterID == "" {
		info = couchbaseutil.NewClusterStatus()
	} else {
		var err error
		if info, err = c.client.GetClusterStatus(c.members); err != nil {
			return err
		}
	}
	return c.updateMemberStatusWithClusterInfo(info)
}

// use cluster info to set ready members from active nodes
// and all remaining nodes as unready
func (c *Cluster) updateMemberStatusWithClusterInfo(cs *couchbaseutil.ClusterStatus) error {
	c.cluster.Status.Members.SetReady(cs.ActiveNodes.Names())
	c.cluster.Status.Members.SetUnready(c.members.Diff(cs.ActiveNodes).Names())
	return c.updateCRStatus()
}

// Use username and password from secret store
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

	c.client = couchbaseutil.NewCouchbaseClient(c.ctx, c.cluster.Name, c.username, c.password)

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
		tls := &cbmgr.TLSAuth{
			CACert: secret.Data[tlsOperatorSecretCACert],
		}

		// Optionally enable client authentication
		if c.cluster.Spec.Networking.TLS.ClientCertificatePolicy != nil {
			_, clientCert, clientKey, err := c.getTLSClientData()
			if err != nil {
				return err
			}
			tls.ClientAuth = &cbmgr.TLSClientAuth{
				Cert: clientCert,
				Key:  clientKey,
			}
		}

		c.client.SetTLS(tls)
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

// Adds a new member to our cluster object and updates the cluster status
func (c *Cluster) clusterAddMember(member *couchbaseutil.Member) error {
	c.members.Add(member)
	c.cluster.Status.Size = c.members.Size()
	return c.updateMemberStatus()
}

// Removes a member from our cluster object and updates the cluster status
func (c *Cluster) clusterRemoveMember(name string) error {
	c.members.Remove(name)
	c.cluster.Status.Size = c.members.Size()
	return c.updateMemberStatus()
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
// aggregates events together
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
// according to status readiness.  Otherwise, fallback to cluster members
func (c *Cluster) readyMembers() couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}
	readyNodes := c.cluster.Status.Members.Ready

	// Get running pods to ensure ready nodes exists
	running, _ := c.pollPods()
	podMembers := podsToMemberSet(running)
	for _, node := range readyNodes {
		if m, ok := c.members[node]; ok {
			if _, ok := podMembers[node]; ok {
				members.Add(m)
			}
		}
	}
	if members.Empty() && c.members != nil {
		err := c.updateMembers(podMembers)
		if err != nil {
			log.Error(err, "Member update failed", "cluster", c.namespacedName())
		}
		members = c.members
	}
	return members
}

// Check if volume only has log volumes mounted
func (c *Cluster) memberHasLogVolumes(name string) bool {
	if m, ok := c.members[name]; ok {
		config := c.cluster.Spec.GetServerConfigByName(m.ServerConfig)
		if config != nil {
			if mounts := config.GetVolumeMounts(); mounts != nil {
				return mounts.LogsOnly()
			}
		}
	}
	return false
}

// getPodIndex returns the current pod naming index
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

// setPodIndex updates the current pod naming index and commits to etcd
func (c *Cluster) setPodIndex(index int) error {
	return c.state.Update(persistence.PodIndex, strconv.Itoa(index))
}

// incPodIndex gets the current pod naming index and increments it
func (c *Cluster) incPodIndex() error {
	podIndex, err := c.getPodIndex()
	if err != nil {
		return err
	}
	return c.setPodIndex(podIndex + 1)
}

// decPodIndex gets the current pod naming index and decrements it
func (c *Cluster) decPodIndex() error {
	podIndex, err := c.getPodIndex()
	if err != nil {
		return err
	}
	return c.setPodIndex(podIndex - 1)
}

func (c *Cluster) logStatus(status *couchbaseutil.ClusterStatus) {
	status.LogStatus(c.namespacedName())
	c.scheduler.LogStatus(c.namespacedName())
}
