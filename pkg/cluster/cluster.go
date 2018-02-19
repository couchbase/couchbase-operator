package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbaselabs/gocbmgr"
	"github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	reconcileInterval         = 8 * time.Second
	podTerminationGracePeriod = int64(5)
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
}

type Cluster struct {
	logger        *logrus.Entry
	config        Config
	cluster       *api.CouchbaseCluster
	status        api.ClusterStatus
	memberCounter int
	eventCh       chan *clusterEvent
	stopCh        chan struct{}
	members       couchbaseutil.MemberSet
	tlsConfig     *tls.Config
	eventsCli     corev1.EventInterface
	username      string
	password      string
	client        *couchbaseutil.CouchbaseClient // Client to communicate with the Couchbase admin port
	ctx           context.Context                // Context used to cancel long running operations
	cancel        context.CancelFunc             // Closure on the context to indicate cancellation
}

func New(config Config, cl *api.CouchbaseCluster) *Cluster {
	c := &Cluster{
		logger: logrus.WithFields(logrus.Fields{
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
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.logger.Info("Watching new cluster")

	go func() {
		if err := c.setup(); err != nil {
			c.logger.Errorf("cluster failed to setup: %v", err)
			if c.status.Phase != api.ClusterPhaseFailed {
				c.status.SetReason(err.Error())
				c.status.SetPhase(api.ClusterPhaseFailed)
				if err := c.updateCRStatus(); err != nil {
					c.logger.Errorf("failed to update cluster phase (%v): %v",
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
		return cberrors.ErrCreatedCluster
	case api.ClusterPhaseRunning:
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
		return c.create()
	}

	return nil
}

func (c *Cluster) create() error {
	c.status.SetPhase(api.ClusterPhaseCreating)
	if err := c.updateCRStatus(); err != nil {
		return fmt.Errorf("cluster create: failed to update cluster phase (%v): %v",
			api.ClusterPhaseCreating, err)
	}

	if len(c.cluster.Spec.ServerSettings) == 0 {
		return fmt.Errorf("cluster create: no server specification defined")
	}

	idx := c.indexOfServerConfigWithService("data")
	if idx == -1 {
		return fmt.Errorf("cluster create: at least one server specification must contain the `data` service")
	}

	m := &couchbaseutil.Member{
		Name:         couchbaseutil.CreateMemberName(c.cluster.Name, c.memberCounter),
		Namespace:    c.cluster.Namespace,
		ServerConfig: c.cluster.Spec.ServerSettings[idx].Name,
		SecureClient: false,
	}
	ms := couchbaseutil.NewMemberSet(m)

	// Set up services e.g. DNS records before calling WaitForPod which will poll
	// for the admin port via a DNS lookup
	if err := c.setupServices(); err != nil {
		return fmt.Errorf("cluster create: fail to create services: %v", err)
	}

	if err := c.createPod(m, c.cluster.Spec.ServerSettings[idx]); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
	defer cancel()
	if err := k8sutil.WaitForPod(ctx, c.config.KubeCli, c.cluster.Namespace, m.Name, m.HostURL()); err != nil {
		return err
	}

	c.memberCounter++
	c.members = ms

	// Initialise TLS if requested, after this point the member will switch to
	// using encrpyted communication
	if err := c.initMemberTLS(m, c.cluster.Spec); err != nil {
		return err
	}

	if err := c.initMember(m, c.cluster.Spec.ServerSettings[idx]); err != nil {
		return err
	}

	uuid, err := c.client.ClusterUUID(m)
	if err != nil {
		return err
	}

	_, err = c.eventsCli.Create(k8sutil.MemberAddEvent(m.Name, c.cluster))
	if err != nil {
		c.logger.Errorf("failed to create new member add event: %v", err)
	}

	c.status.Size = c.members.Size()
	c.updateMemberStatus(c.members)
	c.status.SetVersion(c.cluster.Spec.Version)
	c.status.SetClusterID(uuid)
	return c.updateCRStatus()
}

func (c *Cluster) setupServices() error {
	err := k8sutil.CreatePeerService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, c.cluster.AsOwner())
	if err != nil {
		return err
	}
	if c.cluster.Spec.ExposeAdminConsole {

		svc, err := c.createUIService(c.cluster.Spec.AdminConsoleServices)
		if err != nil {
			return err
		}
		_, err = c.eventsCli.Create(k8sutil.AdminConsoleSvcCreateEvent(svc.Name, c.cluster))
		if err != nil {
			c.logger.Errorf("failed to create new service event: %v", err)
			return err
		}
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
			if c.cluster.Spec.Paused {
				c.status.PauseControl()
				c.logger.Infof("control is paused, skipping reconciliation")
				continue
			} else {
				c.status.Control()
			}

			running, pending, err := c.pollPods()
			if err != nil {
				c.logger.Errorf("fail to poll pods: %v", err)
				continue
			}

			if len(pending) > 0 {
				// Pod startup might take long, e.g. pulling image. It would
				// deterministically become running or succeeded/failed later.
				c.logger.Infof("skip reconciliation: running (%v), pending (%v)",
					k8sutil.GetPodNames(running), k8sutil.GetPodNames(pending))
				continue
			}

			// On controller restore, we could have "members == nil"
			if rerr != nil || c.members == nil {
				rerr = c.updateMembers(podsToMemberSet(running, c.isSecureClient()))
				if rerr != nil {
					c.logger.Errorf("failed to update members: %v", rerr)
					break
				}
			}

			rerr = c.reconcile(running)
			if rerr != nil {
				c.logger.Errorf("failed to reconcile: %v", rerr)
				break
			}

			if err := c.updateCRStatus(); err != nil {
				c.logger.Warningf("periodic update CR status failed: %v", err)
			}

		}

	}
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
	if reflect.DeepEqual(c.cluster.Status, c.status) {
		return nil
	}

	newCluster := c.cluster
	newCluster.Status = c.status
	newCluster, err := c.config.CouchbaseCRCli.CouchbaseV1beta1().CouchbaseClusters(c.cluster.Namespace).Update(c.cluster)

	if err != nil {
		return fmt.Errorf("failed to update CR status: %v", err)
	}

	c.cluster = newCluster

	return nil
}

func (c *Cluster) isSecureClient() bool {
	return c.cluster.Spec.TLS.IsSecureClient()
}

func (c *Cluster) createPod(m *couchbaseutil.Member, serverSpec api.ServerConfig) error {

	pod, err := k8sutil.CreateCouchbasePod(m, c.cluster.Name, c.cluster.Spec,
		serverSpec, c.cluster.AsOwner())
	if err != nil {
		return err
	}
	_, err = c.config.KubeCli.Core().Pods(c.cluster.Namespace).Create(pod)
	return err
}

func (c *Cluster) removePod(name string) error {
	ns := c.cluster.Namespace
	opts := metav1.NewDeleteOptions(podTerminationGracePeriod)
	err := c.config.KubeCli.Core().Pods(ns).Delete(name, opts)
	if err != nil {
		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
		c.logger.Warnf("pod (%s) not found while trying to delete it", name)
	}

	c.logger.Infof("deleted pod (%s)", name)
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

func (c *Cluster) updateMemberStatus(members couchbaseutil.MemberSet) {
	var ready, unready []string
	for _, m := range members {
		url := m.ClientURL()
		healthy, err := couchbaseutil.CheckHealth(url, c.tlsConfig)
		if err != nil {
			c.logger.Warningf("health check of couchbase member (%s) failed: %v", url, err)
		}
		if healthy {
			ready = append(ready, m.Name)
		} else {
			unready = append(unready, m.Name)
		}
	}
	c.status.Members.Ready = ready
	c.status.Members.Unready = unready
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
		return cberrors.ErrSecretMissingUsername{authSecret}
	}
	if password, ok := data[constants.AuthSecretPasswordKey]; ok {
		c.password = string(password[:])
	} else {
		return cberrors.ErrSecretMissingPassword{authSecret}
	}

	return nil
}

func (c *Cluster) initCouchbaseClient() error {
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
		if _, ok := secret.Data[tlsOperatorSecretCert]; !ok {
			return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretCert)
		}
		if _, ok := secret.Data[tlsOperatorSecretKey]; !ok {
			return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretKey)
		}

		// Add the TLS context
		tls := &cbmgr.TLSAuth{
			CACert: secret.Data[tlsOperatorSecretCACert],
			ClientAuth: &cbmgr.TLSClientAuth{
				Cert: secret.Data[tlsOperatorSecretCert],
				Key:  secret.Data[tlsOperatorSecretKey],
			},
		}
		c.client.SetTLS(tls)
	}

	return nil
}

func (c *Cluster) createUIService(services []string) (*v1.Service, error) {
	svc, err := k8sutil.CreateUIService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, services, c.cluster.AsOwner())
	if err == nil {
		c.status.AdminConsolePort, c.status.AdminConsolePortSSL = k8sutil.GetAdminConsolePorts(svc)
	}
	return svc, err
}

func (c *Cluster) deleteUIService(svcName string) error {
	err := k8sutil.DeleteService(c.config.KubeCli, svcName, c.cluster.Namespace, nil)
	if err == nil {
		c.status.AdminConsolePort, c.status.AdminConsolePortSSL = "", ""
	}
	return err
}

func (c *Cluster) indexOfServerConfigWithService(svc string) int {
	for idx, serverSpec := range c.cluster.Spec.ServerSettings {
		for _, service := range serverSpec.Services {
			if service == svc {
				return idx
			}
		}
	}

	return -1
}
