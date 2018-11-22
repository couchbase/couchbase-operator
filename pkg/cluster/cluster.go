package cluster

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	"github.com/couchbase/gocbmgr"
	"github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

var (
	reconcileInterval = 8 * time.Second
	// Be very aggressive here.  If pods are deleted with volumes missing then
	// they stay Terminating forever.  This should emulate --grace-period=0 --force
	// See: https://github.com/kubernetes/kubernetes/issues/51835
	podTerminationGracePeriod = int64(0)
)

type clusterEventType string

const (
	eventDeleteCluster      clusterEventType = "Delete"
	eventModifyCluster      clusterEventType = "Modify"
	tlsOperatorSecretCACert string           = "ca.crt"
	tlsOperatorSecretCert   string           = "couchbase-operator.crt"
	tlsOperatorSecretKey    string           = "couchbase-operator.key"
)

type clusterEvent struct {
	typ     clusterEventType
	cluster *api.CouchbaseCluster
}

type Config struct {
	ServiceAccount string
	KubeCli        kubernetes.Interface
	CouchbaseCRCli versioned.Interface
	LogLevel       logrus.Level
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
	logger     *logrus.Entry
	config     Config
	cluster    *api.CouchbaseCluster
	status     api.ClusterStatus
	eventCh    chan *clusterEvent
	stopCh     chan struct{}
	members    couchbaseutil.MemberSet
	tlsConfig  *tls.Config
	eventsCli  corev1.EventInterface
	username   string
	password   string
	client     *couchbaseutil.CouchbaseClient // Client to communicate with the Couchbase admin port
	ctx        context.Context                // Context used to cancel long running operations
	cancel     context.CancelFunc             // Closure on the context to indicate cancellation
	lastEvent  time.Time                      // When we raised the last event (see raiseEvent)
	recorder   record.EventRecorder           // Buffers and aggegates events
	scheduler  scheduler.Scheduler            // Pod placement scheduler
	lastLoopSt time.Time
	lastLoopEn time.Time
	loopStatus string
}

func New(config Config, cl *api.CouchbaseCluster) *Cluster {
	c := &Cluster{
		logger: logrus.New().WithFields(logrus.Fields{
			"module":       "cluster",
			"cluster-name": cl.Name,
		}),
		config:    config,
		status:    *(cl.Status.DeepCopy()),
		cluster:   cl,
		eventCh:   make(chan *clusterEvent, 100),
		stopCh:    make(chan struct{}),
		eventsCli: config.KubeCli.Core().Events(cl.Namespace),
	}

	c.logger.Logger.SetLevel(c.config.LogLevel)

	// Cancel is used to abort the go routine when the operator is deleted
	// The logger value is used by other clients who don't have access to the
	// per cluster log
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.ctx = context.WithValue(c.ctx, "logger", c.logger)

	// Set up our event logger.  Note that this will cache and aggregate
	// events over a 10 minute window.
	if err := api.AddToScheme(scheme.Scheme); err != nil {
		c.logger.Errorf("Error adding scheme to client-go for event broadcasting: %s", err.Error())
		return nil
	}
	broadcaster := record.NewBroadcaster()
	c.recorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: os.Getenv(constants.EnvOperatorPodName)})
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: config.KubeCli.Core().Events(c.cluster.Namespace)})

	c.logger.Info("Watching new cluster")

	// Initialize the scheduler for the initial pod
	var err error
	if c.scheduler, err = scheduler.New(c.config.KubeCli, c.cluster); err != nil {
		c.logger.Errorf("Error initializing pod scheduler: %s", err.Error())
		return nil
	}

	// Spawn the janitor process which monitors persistent log volumes.
	go newJanitor(c).run()

	go func() {
		if err := c.setup(); err != nil {
			c.logger.Errorf("Cluster setup failed: %v", err)
			if c.status.Phase != api.ClusterPhaseFailed {
				c.status.SetReason(err.Error())
				c.status.SetPhase(api.ClusterPhaseFailed)
				if err := c.updateCRStatus(); err != nil {
					c.logger.Errorf("Failed to update cluster phase (%v): %v",
						api.ClusterPhaseFailed, err)
				}
			}
			return
		}
		c.run()
	}()

	return c
}

func (c *Cluster) Update(cl *api.CouchbaseCluster) {
	c.send(&clusterEvent{
		typ:     eventModifyCluster,
		cluster: cl,
	})
}

func (c *Cluster) Delete() {
	// Inhibit logging as this will just be error messages
	c.logger.Logger.Out = ioutil.Discard
	// Notify client operations to stop what they are doing e.g. abort retry loops
	c.cancel()
	c.send(&clusterEvent{typ: eventDeleteCluster})
}

func (c *Cluster) send(ev *clusterEvent) {
	select {
	case c.eventCh <- ev:
		l, ecap := len(c.eventCh), cap(c.eventCh)
		if l > int(float64(ecap)*0.8) {
			c.logger.Warningf("eventCh buffer is almost full [%d/%d]", l, ecap)
		}
	case <-c.stopCh:
	}
}

func (c *Cluster) setup() error {
	var shouldCreateCluster bool
	switch c.status.Phase {
	case api.ClusterPhaseNone:
		shouldCreateCluster = true
	case api.ClusterPhaseCreating:
		return cberrors.ErrClusterCreating
	case api.ClusterPhaseRunning:
		shouldCreateCluster = false
	default:
		return fmt.Errorf("Unexpected cluster phase: %s", c.status.Phase)
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
		c.logger.Infof("Cluster already exists, the operator will now manage it")
	}

	// Once the cluster is guaranteed to exist, extract the UUID and inject
	// it into the client.  Connections from now on will be aborted if the
	// UUID reported on the persistent connection differs
	c.client.SetUUID(c.status.ClusterID)

	return nil
}

func (c *Cluster) create() error {
	c.logger.Infof("Cluster does not exist so the operator is attempting to create it")
	c.status.SetPhase(api.ClusterPhaseCreating)
	c.status.SetVersion(c.cluster.Spec.Version)

	if err := c.updateCRStatus(); err != nil {
		return fmt.Errorf("Cluster create: failed to update cluster phase (%v): %v",
			api.ClusterPhaseCreating, err)
	}

	if len(c.cluster.Spec.ServerSettings) == 0 {
		return fmt.Errorf("Cluster create: no server specification defined")
	}

	idx := c.indexOfServerConfigWithService(api.DataService)
	if idx == -1 {
		return fmt.Errorf("Cluster create: at least one server specification must contain the `data` service")
	}

	// Set up services e.g. DNS records before calling WaitForPod which will poll
	// for the admin port via a DNS lookup
	if err := c.setupServices(); err != nil {
		return fmt.Errorf("Cluster create: fail to create services: %v", err)
	}

	c.members = couchbaseutil.NewMemberSet()
	m, err := c.createMember(c.cluster.Spec.ServerSettings[idx])
	if err != nil {
		return err
	}

	c.logger.Infof("Operator added member (%s) to manage", m.Name)
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
	c.logger.Infof("Creating headless service for data nodes")
	err := k8sutil.CreatePeerService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, c.cluster.AsOwner())
	if err != nil {
		return err
	}
	if c.cluster.Spec.ExposeAdminConsole {
		c.logger.Infof("Creating NodePort UI service (%s) for %s nodes", k8sutil.AdminServiceName(c.cluster.Name),
			strings.Join(c.cluster.Spec.AdminConsoleServices.StringSlice(), ","))
		svc, err := c.createUIService(c.cluster.Spec.AdminConsoleServices)
		if err != nil {
			return err
		}
		c.raiseEvent(k8sutil.AdminConsoleSvcCreateEvent(svc.Name, c.cluster))
	}
	return nil
}

func (c *Cluster) run() {
	c.status.SetPhase(api.ClusterPhaseRunning)
	if err := c.updateCRStatus(); err != nil {
		c.logger.Warningf("update initial CR status failed: %v", err)
	}
	c.logger.Infof("start running...")

	var rerr error
	for {
		select {
		case event := <-c.eventCh:
			switch event.typ {
			case eventModifyCluster:
				c.handleUpdateEvent(event)
			case eventDeleteCluster:
				c.logger.Infof("cluster is deleted by the user")
				return
			default:
				panic("unknown event type" + event.typ)
			}

		case <-time.After(reconcileInterval):
			st := time.Now()
			c.loopStatus = "running"
			if c.cluster.Spec.Paused {
				c.status.PauseControl()
				if err := c.updateCRStatus(); err != nil {
					c.logger.Warningf("periodic update CR status failed: %v", err)
				}
				c.logger.Infof("control is paused, skipping reconciliation")
				c.setLastLoopTimes(st)
				continue
			} else {
				c.status.Control()
			}

			running, pending, err := c.pollPods()
			if err != nil {
				c.logger.Errorf("fail to poll pods: %v", err)
				c.setLastLoopTimes(st)
				continue
			}

			if len(pending) > 0 {
				// Pod startup might take long, e.g. pulling image. It would
				// deterministically become running or succeeded/failed later.
				c.logger.Infof("skip reconciliation: running (%v), pending (%v)",
					k8sutil.GetPodNames(running), k8sutil.GetPodNames(pending))
				c.setLastLoopTimes(st)
				continue
			}

			// When cluster is being restored or reconcile error has occured then
			// then the memberset can be updated from the status of running pods.
			// Otherwise restore members from any config maps we can find
			if (rerr != nil || c.members == nil) && len(running) > 0 {
				rerr = c.updateMembers(podsToMemberSet(running, c.isSecureClient()))
				if rerr != nil {
					c.logger.Errorf("failed to update members: %v", rerr)
					break
				}
			} else if c.members == nil {
				c.members, _ = k8sutil.PVCToMemberset(c.config.KubeCli, c.cluster.Namespace, c.cluster.Name, c.isSecureClient())
			}

			if err := c.reconcile(running); err != nil {
				c.logger.Errorf("failed to reconcile: %v", err)
				// If a pod is killed during a rebalance say, we'd force a reload of
				// the members.  As server knows about the member, but it no longer
				// exists it will error continuously, whereas previously it would have
				// correctly reconciled.
				//rerr = err
			}

			if err := c.updateCRStatus(); err != nil {
				c.logger.Warningf("periodic update CR status failed: %v", err)
			}
			c.setLastLoopTimes(st)
		}
	}
}

func (c *Cluster) setLastLoopTimes(st time.Time) {
	c.lastLoopSt = st
	c.lastLoopEn = time.Now()
	c.loopStatus = "sleeping"
}

func (c *Cluster) handleUpdateEvent(event *clusterEvent) {
	oldSpec := c.cluster.Spec.DeepCopy()
	c.cluster = event.cluster

	if !reflect.DeepEqual(event.cluster.Spec, *oldSpec) {
		c.logSpecUpdate(*oldSpec, event.cluster.Spec)
	}
}

func (c *Cluster) logSpecUpdate(oldSpec, newSpec api.ClusterSpec) {
	oldSpecBytes, err := json.MarshalIndent(oldSpec, "", "    ")
	if err != nil {
		c.logger.Errorf("failed to marshal cluster spec: %v", err)
	}
	newSpecBytes, err := json.MarshalIndent(newSpec, "", "    ")
	if err != nil {
		c.logger.Errorf("failed to marshal cluster spec: %v", err)
	}

	c.logger.Infof("spec update: Old Spec:")
	for _, m := range strings.Split(string(oldSpecBytes), "\n") {
		c.logger.Info(m)
	}

	c.logger.Infof("New Spec:")
	for _, m := range strings.Split(string(newSpecBytes), "\n") {
		c.logger.Info(m)
	}
}

func (c *Cluster) updateCRStatus() error {
	kind := c.cluster.Kind
	apiVersion := c.cluster.APIVersion

	// The cluster object can be updated asynchronously e.g. via a spec update,
	// hence what's in etcd need not reflect what's locally cached and the k8s
	// server will reject any updates that fail the CAS test.  We only pick up
	// these updates between reconcile executions (see handleUpdateEvent).
	cluster, err := c.config.CouchbaseCRCli.CouchbaseV1().CouchbaseClusters(c.cluster.Namespace).Get(c.cluster.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if c.cluster.ResourceVersion != cluster.ResourceVersion {
		c.logger.Infof("Resource version conflict, updating %s to %s", c.cluster.ResourceVersion, cluster.ResourceVersion)
		c.cluster = cluster
	}

	// Ignore the case where nothing needs to be updated
	if reflect.DeepEqual(c.cluster.Status, c.status) {
		return nil
	}

	// Copy the updated status to our cluster object and try update it
	c.cluster.Status = c.status
	newCluster, err := c.config.CouchbaseCRCli.CouchbaseV1().CouchbaseClusters(c.cluster.Namespace).Update(c.cluster)
	if err != nil {
		return err
	}

	// Cache the new cluster object (with its new revision ID)
	//
	// Note: TypeMeta isn't populated after the Update() so manually restore.
	//       May be related to https://github.com/kubernetes/apiextensions-apiserver/issues/29 for tracking
	//       This must be populated for cached events to work properly as there is a bug in the fallback
	//       code which parses the object link to extract the same information.
	c.cluster = newCluster
	c.cluster.Kind = kind
	c.cluster.APIVersion = apiVersion
	return nil
}

func (c *Cluster) isSecureClient() bool {
	return c.cluster.Spec.TLS.IsSecureClient()
}

func (c *Cluster) createPod(m *couchbaseutil.Member, serverSpec api.ServerConfig) error {
	version := c.cluster.Spec.Version
	c.logger.Infof("Creating a pod (%s) running Couchbase %s", m.Name, version)
	_, err := k8sutil.CreateCouchbasePod(c.config.KubeCli, c.scheduler, c.cluster, m, version, serverSpec, c.ctx)
	return err
}

// Remove Pod and any volumes associated with pod if requested
// ore volumes are associated with default claim
func (c *Cluster) removePod(name string, removeVolumes bool) error {

	opts := metav1.NewDeleteOptions(podTerminationGracePeriod)
	err := k8sutil.DeleteCouchbasePod(c.config.KubeCli, c.cluster.Namespace, c.cluster.Name, name, opts, removeVolumes)
	if err != nil {
		c.logger.Errorf("error occurred during pod deletion (%s)", err)
		return err
	}

	c.logger.Infof("deleted pod (%s)", name)
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
	err = c.createPod(m, *config)
	if err != nil {
		return err
	}
	return c.waitForCreatePod(m, 120)
}

// wait with context
func (c *Cluster) waitForCreatePod(member *couchbaseutil.Member, timeout int64) error {
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(timeout)*time.Second)
	defer cancel()
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
			c.logger.Warningf("%s is unrecoverable: %v", m.Name, err)
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
	elapsedDuration := time.Now().Sub(ts)

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
				c.logger.Infof("recovering node %s", m.ClientURL())
				c.raiseEventCached(k8sutil.MemberRecoveredEvent(m.Name, c.cluster))
				break
			}
		}
	}
	return nil
}

func (c *Cluster) pollPods() (running, pending []*v1.Pod, err error) {
	podList, err := c.config.KubeCli.Core().Pods(c.cluster.Namespace).List(k8sutil.ClusterListOpt(c.cluster.Name))
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
			c.logger.Warningf("pollPods: ignore pod %v: no owner", pod.Name)
			continue
		}
		if pod.OwnerReferences[0].UID != c.cluster.UID {
			c.logger.Warningf("pollPods: ignore pod %v: owner (%v) is not %v",
				pod.Name, pod.OwnerReferences[0].UID, c.cluster.UID)
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

func (c *Cluster) updateMemberStatus() {
	// The first member will get here when the pod is brought up, however calls
	// to get the cluster status will fail as it hasn't been initialized yet, so
	// instead just return an empty status
	var info *couchbaseutil.ClusterStatus
	if c.cluster.Status.ClusterID == "" {
		info = couchbaseutil.NewClusterStatus()
	} else {
		var err error
		if info, err = c.client.GetClusterStatus(c.members); err != nil {
			c.logger.Warningf("update member status failed failed: %v", err)
			return
		}
	}
	c.updateMemberStatusWithClusterInfo(info)
}

// use cluster info to set ready members from active nodes
// and all remaining nodes as unready
func (c *Cluster) updateMemberStatusWithClusterInfo(cs *couchbaseutil.ClusterStatus) {
	c.status.Members.SetReady(cs.ActiveNodes.Names())
	c.status.Members.SetUnready(c.members.Diff(cs.ActiveNodes).Names())
	c.updateCRStatus()
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
		c.username = string(username[:])
	} else {
		return cberrors.ErrSecretMissingUsername{Reason: authSecret}
	}
	if password, ok := data[constants.AuthSecretPasswordKey]; ok {
		c.password = string(password[:])
	} else {
		return cberrors.ErrSecretMissingPassword{Reason: authSecret}
	}

	return nil
}

func (c *Cluster) initCouchbaseClient() error {
	secureStr := ""
	if c.isSecureClient() {
		secureStr = "secure "
	}
	c.logger.Infof("Setting up %sclient for operator communication with the cluster", secureStr)

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

func (c *Cluster) createUIService(services api.ServiceList) (*v1.Service, error) {
	svc, err := k8sutil.CreateUIService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, services, c.cluster.AsOwner())
	if err == nil {
		c.status.AdminConsolePort, c.status.AdminConsolePortSSL = k8sutil.GetAdminConsolePorts(svc)
	}
	return svc, err
}

func (c *Cluster) deleteUIService(svcName string) error {
	err := k8sutil.DeleteService(c.config.KubeCli, c.cluster.Namespace, svcName, nil)
	if err == nil {
		c.status.AdminConsolePort, c.status.AdminConsolePortSSL = "", ""
	}
	return err
}

func (c *Cluster) indexOfServerConfigWithService(svc api.Service) int {
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
func (c *Cluster) clusterAddMember(member *couchbaseutil.Member) {
	c.members.Add(member)
	c.status.Size = c.members.Size()
	c.updateMemberStatus()
}

// Removes a member from our cluster object and updates the cluster status
func (c *Cluster) clusterRemoveMember(name string) {
	c.members.Remove(name)
	c.status.Size = c.members.Size()
	c.updateMemberStatus()
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
		c.logger.Errorf("failed to create new %s event: %v", event.Reason, err)
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
		c.logger.Errorf("fail to poll pods: %v", err)
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
			c.logger.Errorf("failed to update members: %v", err)
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
		c.logger.Warnf("failed to update custom resource pod index: %v", err)
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

func (c *Cluster) SetLoggingLevel(level logrus.Level) {
	c.logger.Logger.SetLevel(level)
}

func (c *Cluster) Stats() *ClusterStats {
	return &ClusterStats{
		LastReconcileStartTime:    c.lastLoopSt,
		LastReconcileEndTime:      c.lastLoopEn,
		LastCompletedLoopDuration: c.lastLoopEn.Sub(c.lastLoopSt).Seconds(),
		ReconcileLoopStatus:       c.loopStatus,
		ReconcileLoopSleepTime:    int(reconcileInterval.Seconds()),
		ControlPaused:             c.status.ControlPaused,
		ClusterPhase:              string(c.status.Phase),
	}
}

func (c *Cluster) logStatus(status *couchbaseutil.ClusterStatus) {
	// We are performing an action log the cluster status
	b := &bytes.Buffer{}
	if err := status.LogStatus(b); err != nil {
		c.logger.Warnf("failed to log cluster status: %v", err)
	}
	if err := c.scheduler.LogStatus(b); err != nil {
		c.logger.Warnf("failed to log scheduler status: %v", err)
	}
	for _, line := range strings.Split(b.String(), "\n") {
		c.logger.Info(line)
	}
}
