package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	"github.com/couchbase/gocbmgr"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("cluster")

var (
	// Be very aggressive here.  If pods are deleted with volumes missing then
	// they stay Terminating forever.  This should emulate --grace-period=0 --force
	// See: https://github.com/kubernetes/kubernetes/issues/51835
	podTerminationGracePeriod = int64(0)
)

const (
	tlsOperatorSecretCACert string = "ca.crt"
	tlsOperatorSecretCert   string = "couchbase-operator.crt"
	tlsOperatorSecretKey    string = "couchbase-operator.key"
)

type Config struct {
	KubeCli          kubernetes.Interface
	CouchbaseCRCli   versioned.Interface
	PodCreateTimeout string
}

type ClusterStats struct {
	LastReconcileStartTime    time.Time `json:"last_reconcile_loop_start_time,omitempty"`
	LastReconcileEndTime      time.Time `json:"last_reconcile_loop_end_time,omitempty"`
	LastCompletedLoopDuration float64   `json:"last_completed_loop_duration_sec,omitempty"`
	ReconcileLoopStatus       string    `json:"reconcile_loop_status,omitempty"`
	ReconcileLoopSleepTime    int       `json:"reconcile_loop_sleep_time,omitempty"`
	ClusterPhase              string    `json:"clusterPhase"`
	ControlPaused             bool      `json:"controlPaused"`
}

type Cluster struct {
	config      Config
	cluster     *couchbasev1.CouchbaseCluster
	status      couchbasev1.ClusterStatus
	members     couchbaseutil.MemberSet
	eventsCli   corev1.EventInterface
	username    string
	password    string
	client      *couchbaseutil.CouchbaseClient // Client to communicate with the Couchbase admin port
	ctx         context.Context                // Context used to cancel long running operations
	cancel      context.CancelFunc             // Closure on the context to indicate cancellation
	lastEvent   time.Time                      // When we raised the last event (see raiseEvent)
	recorder    record.EventRecorder           // Buffers and aggegates events
	scheduler   scheduler.Scheduler            // Pod placement scheduler
	lastLoopSt  time.Time
	lastLoopEn  time.Time
	loopStatus  string
	forceUpdate bool // Members are cached so we can trust dead ephemeral pods are ours, this allows us to force a refresh
}

func New(config Config, cl *couchbasev1.CouchbaseCluster) (*Cluster, error) {
	c := &Cluster{
		config:  config,
		status:  *(cl.Status.DeepCopy()),
		cluster: cl,
	}

	// I have no idea what's going on here, defaults are set by the admission controller
	// but by the time that happens we are already processing the request in which the
	// name is blank???
	if c.cluster.Spec.ClusterSettings.ClusterName == "" {
		c.cluster.Spec.ClusterSettings.ClusterName = c.cluster.Name
	}

	kubeconfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	kubeclient, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	couchbaseclient, err := versioned.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	c.config.KubeCli = kubeclient
	c.config.CouchbaseCRCli = couchbaseclient
	c.eventsCli = c.config.KubeCli.CoreV1().Events(cl.Namespace)

	// Cancel is used to abort the go routine when the operator is deleted
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// Set up our event logger.  Note that this will cache and aggregate
	// events over a 10 minute window.
	if err := couchbasev1.AddToScheme(scheme.Scheme); err != nil {
		log.Error(err, "Error adding scheme to client-go for event broadcasting", "cluster", c.cluster.Name)
		return nil, err
	}
	broadcaster := record.NewBroadcaster()
	c.recorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: os.Getenv(constants.EnvOperatorPodName)})
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: c.config.KubeCli.CoreV1().Events(c.cluster.Namespace)})

	log.Info("Watching new cluster", "cluster", c.cluster.Name)

	// Initialize the scheduler for the initial pod
	if c.scheduler, err = scheduler.New(c.config.KubeCli, c.cluster); err != nil {
		log.Error(err, "Error initializing pod scheduler", "cluster", c.cluster.Name)
		return nil, err
	}

	// Spawn the janitor process which monitors persistent log volumes.
	go newJanitor(c).run()

	if err := c.setup(); err != nil {
		log.Error(err, "Cluster setup failed", "cluster", c.cluster.Name)
		if c.status.Phase != couchbasev1.ClusterPhaseFailed {
			c.status.SetReason(err.Error())
			c.status.SetPhase(couchbasev1.ClusterPhaseFailed)
			if err := c.updateCRStatus(); err != nil {
				log.Error(err, "Failed to update cluster phase", "cluster", c.cluster.Name, "phase", couchbasev1.ClusterPhaseFailed)
			}
		}
		return nil, err
	}

	c.status.SetPhase(couchbasev1.ClusterPhaseRunning)
	if err := c.updateCRStatus(); err != nil {
		log.Error(err, "Status update failed", "cluster", c.cluster.Name)
	}
	log.Info("Running", "cluster", c.cluster.Name)

	c.runReconcile()

	return c, nil
}

func (c *Cluster) Delete() {
	// Notify client operations to stop what they are doing e.g. abort retry loops
	c.cancel()
}

func (c *Cluster) setup() error {
	var shouldCreateCluster bool
	switch c.status.Phase {
	case couchbasev1.ClusterPhaseNone:
		shouldCreateCluster = true
	case couchbasev1.ClusterPhaseCreating:
		return cberrors.ErrClusterCreating
	case couchbasev1.ClusterPhaseRunning:
		shouldCreateCluster = false
	default:
		return fmt.Errorf("unexpected cluster phase: %s", c.status.Phase)
	}

	if err := c.setupAuth(c.cluster.Spec.AuthSecret); err != nil {
		return err
	}

	if err := c.initCouchbaseClient(); err != nil {
		return err
	}

	if shouldCreateCluster {
		if err := c.create(); err != nil {
			return err
		}
	} else {
		log.Info("Cluster already exists, the operator will now manage it", "cluster", c.cluster.Name)

		// TLS may have updated across the restart, so the client CA is valid
		// but the rest of the cluster is uncontactable.  For this we need a
		// set of members to iterate over, however the normal updateMembers()
		// tries to contact Couchbase server to verify membership, that will
		// fail as TLS invalid.  We need to just take what we are given before
		// making any calls to Couchbase server.
		running, _, err := c.pollPods()
		if err != nil {
			return err
		}
		c.members = podsToMemberSet(running, c.isSecureClient())
		if err := c.reconcileTLS(); err != nil {
			return err
		}
		c.members = nil

		// Perform any necessary upgrades to the cluster and kubernetes resources.
		if err := c.operatorUpgrade(); err != nil {
			return err
		}
	}

	// Once the cluster is guaranteed to exist, extract the UUID and inject
	// it into the client.  Connections from now on will be aborted if the
	// UUID reported on the persistent connection differs
	c.client.SetUUID(c.status.ClusterID)

	return nil
}

func (c *Cluster) create() error {
	log.Info("Cluster does not exist so the operator is attempting to create it", "cluster", c.cluster.Name)
	c.status.SetPhase(couchbasev1.ClusterPhaseCreating)
	c.status.SetVersion(c.cluster.Spec.Version)

	if err := c.updateCRStatus(); err != nil {
		return fmt.Errorf("cluster create: failed to update cluster phase (%v): %v",
			couchbasev1.ClusterPhaseCreating, err)
	}

	if len(c.cluster.Spec.ServerSettings) == 0 {
		return fmt.Errorf("cluster create: no server specification defined")
	}

	idx := c.indexOfServerConfigWithService(couchbasev1.DataService)
	if idx == -1 {
		return fmt.Errorf("cluster create: at least one server specification must contain the `data` service")
	}

	// Set up services e.g. DNS records before calling WaitForPod which will poll
	// for the admin port via a DNS lookup
	if err := c.setupServices(); err != nil {
		return fmt.Errorf("cluster create: fail to create services: %v", err)
	}

	c.members = couchbaseutil.NewMemberSet()
	m, err := c.createMember(c.cluster.Spec.ServerSettings[idx])
	if err != nil {
		return err
	}

	log.Info("Operator added member", "cluster", c.cluster.Name, "name", m.Name)
	c.raiseEvent(k8sutil.MemberAddEvent(m.Name, c.cluster))

	if err := c.initMember(m, c.cluster.Spec.ServerSettings[idx]); err != nil {
		return err
	}

	uuid, err := c.client.ClusterUUID(m)
	if err != nil {
		return err
	}

	c.status.SetClusterID(uuid)
	c.status.SetBalancedCondition()
	return c.updateCRStatus()
}

func (c *Cluster) setupServices() error {
	log.Info("Creating headless service for DNS", "cluster", c.cluster.Name)
	err := k8sutil.CreatePeerService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, c.cluster.AsOwner())
	if err != nil {
		return err
	}
	if c.cluster.Spec.ExposeAdminConsole {
		c.reconcileAdminService()
	}
	return nil
}

// runReconcile gathers a list of pods in cluster from Kubernetes, optionally
// initializes our internal member list if we need to e.g. we have been restarted and
// have lost state or a previous error may have resulted in inconsistent state, then
// compares reality with the specification and makes the former match it.
//
// It accepts a flag forcing it update internal state from Kubernetes, and returns a
// similar flag to indicate we require a forced update with the next invocation.
func (c *Cluster) runReconcile() {
	// Always gather runloop statistics.
	defer c.setLastLoopTimes(time.Now())

	// Always update the cluster status.
	defer func() {
		if err := c.updateCRStatus(); err != nil {
			log.Error(err, "Status update failed", "cluster", c.cluster.Name)
		}
	}()

	// Indicate that the reconcile function is getting run.
	c.loopStatus = "running"

	// If the user has requested that we pause operations.
	if c.cluster.Spec.Paused {
		c.status.PauseControl()
		log.Info("Operator paused, skipping", "cluster", c.cluster.Name)
		return
	}

	// Otherwise indicate that we are in control.
	c.status.Control()

	running, pending, err := c.pollPods()
	if err != nil {
		log.Error(err, "Failed to list pods", "cluster", c.cluster.Name)
		return
	}

	if len(pending) > 0 {
		// Pod startup might take long, e.g. pulling image. It would
		// deterministically become running or succeeded/failed later.
		log.Info("Pods pending creation, skipping", "cluster", c.cluster.Name, "running", len(running), "pending", len(pending))
		return
	}

	// When cluster is being restored or reconcile error has occurred then
	// then the memberset can be updated from the status of running pods and
	// persistent volumes.
	if c.forceUpdate || c.members.Empty() {
		c.forceUpdate = false
		if err := c.updateMembers(podsToMemberSet(running, c.isSecureClient())); err != nil {
			log.Error(err, "Failed to update members", "cluster", c.cluster.Name)
			c.forceUpdate = true
			return
		}
	}

	// Finally reconcile state according to the specification.
	if err := c.reconcile(running); err != nil {
		log.Error(err, "Reconciliation failed", "cluster", c.cluster.Name)
		return
	}
}

// Update is called periodically or on a CR change, print out any diffs in the spec
// then update the specification and unconditionally reconcile.
func (c *Cluster) Update(cluster *couchbasev1.CouchbaseCluster) {
	if !reflect.DeepEqual(cluster.Spec, c.cluster.Spec) {
		c.logSpecUpdate(c.cluster.Spec, cluster.Spec)
		c.cluster = cluster
	}

	c.runReconcile()
}

func (c *Cluster) setLastLoopTimes(st time.Time) {
	c.lastLoopSt = st
	c.lastLoopEn = time.Now()
	c.loopStatus = "sleeping"
}

func (c *Cluster) logSpecUpdate(oldSpec, newSpec couchbasev1.ClusterSpec) {
	oldSpecBytes, err := yaml.Marshal(oldSpec)
	if err != nil {
		log.Error(err, "YAML marshal failed", "cluster", c.cluster.Name)
	}
	newSpecBytes, err := yaml.Marshal(newSpec)
	if err != nil {
		log.Error(err, "YAML marshal failed", "cluster", c.cluster.Name)
	}

	log.Info("Specification updated", "cluster", c.cluster.Name)
	diff := cmp.Diff(string(oldSpecBytes), string(newSpecBytes))
	for _, m := range strings.Split(diff, "\n") {
		log.Info(m, "cluster", c.cluster.Name)
	}
}

func (c *Cluster) updateCRStatus() error {
	// The cluster object can be updated asynchronously e.g. via a spec update,
	// hence what's in etcd need not reflect what's locally cached and the k8s
	// server will reject any updates that fail the CAS test.  We only pick up
	// these updates between reconcile executions (see handleUpdateEvent).
	cluster, err := c.config.CouchbaseCRCli.CouchbaseV1().CouchbaseClusters(c.cluster.Namespace).Get(c.cluster.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Ignore the case where nothing needs to be updated
	if reflect.DeepEqual(c.cluster.Status, c.status) {
		return nil
	}

	// Copy the updated status to our cluster object and try update it
	cluster.Status = c.status
	if _, err := c.config.CouchbaseCRCli.CouchbaseV1().CouchbaseClusters(c.cluster.Namespace).Update(cluster); err != nil {
		return err
	}
	return nil
}

func (c *Cluster) isSecureClient() bool {
	return c.cluster.Spec.TLS.IsSecureClient()
}

func (c *Cluster) createPod(ctx context.Context, m *couchbaseutil.Member, serverSpec couchbasev1.ServerConfig) error {
	version := c.cluster.Spec.Version
	log.Info("Creating pod", "cluster", c.cluster.Name, "name", m.Name, "version", version)
	_, err := k8sutil.CreateCouchbasePod(c.config.KubeCli, c.scheduler, c.cluster, m, version, serverSpec, ctx)
	return err
}

// Remove Pod and any volumes associated with pod if requested
// ore volumes are associated with default claim
func (c *Cluster) removePod(name string, removeVolumes bool) error {

	opts := metav1.NewDeleteOptions(podTerminationGracePeriod)
	err := k8sutil.DeleteCouchbasePod(c.config.KubeCli, c.cluster.Namespace, c.cluster.Name, name, opts, removeVolumes)
	if err != nil {
		log.Error(err, "Pod deletion failed", "cluster", c.cluster.Name)
		return err
	}

	log.Info("Pod deleted", "cluster", c.cluster.Name, "name", name)
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
	err := k8sutil.DeletePod(c.config.KubeCli, c.cluster.Namespace, m.Name, opts)
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
	if err := k8sutil.WaitForPod(ctx, c.config.KubeCli, c.cluster.Namespace, member.Name, member.HostURL()); err != nil {
		return err
	}
	return nil
}

func (c *Cluster) waitForDeletePod(podName string, timeout int64) error {
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	if err := k8sutil.WaitForDeletePod(ctx, c.config.KubeCli, c.cluster.Namespace, podName); err != nil {
		return err
	}
	return nil
}

func (c *Cluster) isPodRecoverable(m *couchbaseutil.Member) bool {
	recoverable := false
	if config := c.cluster.Spec.GetServerConfigByName(m.ServerConfig); config != nil {
		err := k8sutil.IsPodRecoverable(c.config.KubeCli, *config, m.Name, c.cluster.Name, c.cluster.Namespace)
		if err != nil {
			log.Error(err, "Pod unrecoverable", "cluster", c.cluster.Name, "name", m.Name)
		} else {
			recoverable = true
		}
	}
	return recoverable
}

// Checks if a timestamp has elapsed recommended duration for cluster recovery
// along with time remaining until reaching the required duration
func (c *Cluster) elapsedRecoveryDuration(ts time.Time) (bool, time.Duration) {

	// get duration since timestamp
	elapsedDuration := time.Since(ts)

	// require a duration of 30s after autofailover timeout
	requiredDuration := time.Duration(c.cluster.Spec.ClusterSettings.AutoFailoverTimeout+30) * time.Second
	return elapsedDuration > requiredDuration, (requiredDuration - elapsedDuration).Truncate(time.Second)
}

// Selects any member that can be recovered and attempts to restart it
func (c *Cluster) recoverClusterDown() error {
	for _, m := range c.members {
		if c.isPodRecoverable(m) {
			if err := c.recreatePod(m); err != nil {
				return fmt.Errorf("node %s could not be recovered: %s", m.ClientURL(), err.Error())
			} else {
				log.Info("Pod recovering", "cluster", c.cluster.Name, "name", m.Name)
				c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name, c.cluster))
				break
			}
		}
	}
	return nil
}

func (c *Cluster) pollPods() (running, pending []*v1.Pod, err error) {
	podList, err := c.config.KubeCli.CoreV1().Pods(c.cluster.Namespace).List(k8sutil.ClusterListOpt(c.cluster.Name))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list running pods: %v", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		// Avoid polling deleted pods
		if pod.DeletionTimestamp != nil {
			continue
		}
		if len(pod.OwnerReferences) < 1 {
			log.Info("Pod ignored, no owner", "cluster", c.cluster.Name, "name", pod.Name)
			continue
		}
		if pod.OwnerReferences[0].UID != c.cluster.UID {
			log.Info("Pod ignored, invalid owner", "cluster", c.cluster.Name, "name", pod.Name, "cluster_uid", c.cluster.UID, "pod_uid", pod.OwnerReferences[0].UID)
			continue
		}
		switch pod.Status.Phase {
		case v1.PodRunning:
			running = append(running, pod)
		case v1.PodPending:
			pending = append(pending, pod)
		}
	}

	return running, pending, nil
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
	c.status.Members.SetReady(cs.ActiveNodes.Names())
	c.status.Members.SetUnready(c.members.Diff(cs.ActiveNodes).Names())
	return c.updateCRStatus()
}

// Use username and password from secret store
func (c *Cluster) setupAuth(authSecret string) error {
	opts := metav1.GetOptions{}
	secret, err := c.config.KubeCli.CoreV1().Secrets(c.cluster.Namespace).Get(authSecret, opts)
	if err != nil {
		return err
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
	log.Info("Couchbase client starting", "cluster", c.cluster.Name)

	c.client = couchbaseutil.NewCouchbaseClient(c.ctx, c.cluster.Name, c.username, c.password)

	if c.isSecureClient() {
		// Grab the operator secret
		secretName := c.cluster.Spec.TLS.Static.OperatorSecret
		secret, err := k8sutil.GetSecret(c.config.KubeCli, secretName, c.cluster.Namespace, nil)
		if err != nil {
			return err
		}

		// Extract the data
		if _, ok := secret.Data[tlsOperatorSecretCACert]; !ok {
			return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretCACert)
		}

		// Optionally enable client authentication
		var clientAuth *cbmgr.TLSClientAuth
		if _, ok := secret.Data[tlsOperatorSecretCert]; ok {
			if _, ok := secret.Data[tlsOperatorSecretKey]; !ok {
				return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretKey)
			}
			clientAuth = &cbmgr.TLSClientAuth{
				Cert: secret.Data[tlsOperatorSecretCert],
				Key:  secret.Data[tlsOperatorSecretKey],
			}
		}

		// Add the TLS context
		tls := &cbmgr.TLSAuth{
			CACert:     secret.Data[tlsOperatorSecretCACert],
			ClientAuth: clientAuth,
		}
		c.client.SetTLS(tls)
	}

	return nil
}

func (c *Cluster) indexOfServerConfigWithService(svc couchbasev1.Service) int {
	for idx, serverSpec := range c.cluster.Spec.ServerSettings {
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
	c.status.Size = c.members.Size()
	return c.updateMemberStatus()
}

// Removes a member from our cluster object and updates the cluster status
func (c *Cluster) clusterRemoveMember(name string) error {
	c.members.Remove(name)
	c.status.Size = c.members.Size()
	return c.updateMemberStatus()
}

// Raises an event.  Unfortunately the event stream only appears to have a
// 1s granularity, so insert a delay if necessary
func (c *Cluster) raiseEvent(event *v1.Event) {
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
	if _, err := c.eventsCli.Create(event); err != nil {
		log.Error(err, "Event creation failed", "cluster", c.cluster.Name, "event", event.Reason)
	}

	// Update the last event timestamp
	c.lastEvent = event.FirstTimestamp.Time
}

// raiseEventCached raises an event but first checks an LRU cache and optionally
// aggregates events together
func (c *Cluster) raiseEventCached(event *v1.Event) {
	c.recorder.Event(c.cluster, event.Type, event.Reason, event.Message)
}

// clients should use ready members that are available to service requests
// according to status readiness.  Otherwise, fallback to cluster members
func (c *Cluster) readyMembers() couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}
	readyNodes := c.status.Members.Ready.Names()

	// Get running pods to ensure ready nodes exists
	running, _, err := c.pollPods()
	if err != nil {
		log.Error(err, "Pod discovery failed", "cluster", c.cluster.Name)
		return c.members
	}
	podMembers := podsToMemberSet(running, c.isSecureClient())
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
			log.Error(err, "Member update failed", "cluster", c.cluster.Name)
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
func (c *Cluster) getPodIndex() int {
	return c.status.Members.Index
}

// setPodIndex updates the current pod naming index and commits to etcd
func (c *Cluster) setPodIndex(index int) {
	c.status.Members.Index = index
	if err := c.updateCRStatus(); err != nil {
		log.Error(err, "Pod index update failed", "cluster", c.cluster.Name)
	}
}

// incPodIndex gets the current pod naming index and increments it
func (c *Cluster) incPodIndex() {
	index := c.status.Members.Index
	c.setPodIndex(index + 1)
}

// decPodIndex gets the current pod naming index and decrements it
func (c *Cluster) decPodIndex() {
	index := c.status.Members.Index
	c.setPodIndex(index - 1)
}

// TODO: Prometheus please
func (c *Cluster) Stats() *ClusterStats {
	return &ClusterStats{
		LastReconcileStartTime:    c.lastLoopSt,
		LastReconcileEndTime:      c.lastLoopEn,
		LastCompletedLoopDuration: c.lastLoopEn.Sub(c.lastLoopSt).Seconds(),
		ReconcileLoopStatus:       c.loopStatus,
		ReconcileLoopSleepTime:    10,
		ControlPaused:             c.status.ControlPaused,
		ClusterPhase:              string(c.status.Phase),
	}
}

func (c *Cluster) logStatus(status *couchbaseutil.ClusterStatus) {
	// We are performing an action log the cluster status
	b := &bytes.Buffer{}
	if err := status.LogStatus(b); err != nil {
		log.Error(err, "Cluster status dump failed", "cluster", c.cluster.Name)
	}
	if err := c.scheduler.LogStatus(b); err != nil {
		log.Error(err, "Scheduler status dump failed", "cluster", c.cluster.Name)
	}
	for _, line := range strings.Split(b.String(), "\n") {
		log.Info(line, "cluster", c.cluster.Name)
	}
}
