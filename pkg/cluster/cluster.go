package cluster

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/garbagecollection"
	"github.com/couchbaselabs/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/constants"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"
	"github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

var (
	reconcileInterval         = 8 * time.Second
	podTerminationGracePeriod = int64(5)
)

type clusterEventType string

const (
	eventDeleteCluster clusterEventType = "Delete"
	eventModifyCluster clusterEventType = "Modify"
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
	gc            *garbagecollection.GC
	username      string
	password      string
}

func New(config Config, cl *api.CouchbaseCluster) *Cluster {
	c := &Cluster{
		logger: logrus.WithFields(logrus.Fields{
			"module":       "cluster",
			"cluster-name": cl.Name,
		}),
		config:  config,
		status:  *(cl.Status.DeepCopy()),
		cluster: cl,
		eventCh: make(chan *clusterEvent, 100),
		stopCh:  make(chan struct{}),
		gc:      garbagecollection.New(config.KubeCli, cl.Namespace),
	}
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
		return errCreatedCluster
	case api.ClusterPhaseRunning:
		shouldCreateCluster = false
	default:
		return fmt.Errorf("unexpected cluster phase: %s", c.status.Phase)
	}

	if err := c.setupAuth(c.cluster.Spec.AuthSecret); err != nil {
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

	if err := c.createPod(ms, m, c.cluster.Spec.ServerSettings[idx]); err != nil {
		return err
	}

	if err := c.waitForPod(m.Name); err != nil {
		return err
	}

	c.memberCounter++
	c.members = ms
	if err := c.setupServices(); err != nil {
		return fmt.Errorf("cluster create: fail to create services: %v", err)
	}

	if err := c.initMember(m, c.cluster.Spec.ServerSettings[idx]); err != nil {
		return err
	}

	uuid, err := couchbaseutil.ClusterUUID(m, c.username, c.password, c.cluster.Name)
	if err != nil {
		return err
	}

	c.status.SetClusterID(uuid)
	return c.updateCRStatus()
}

func (c *Cluster) setupServices() error {
	return k8sutil.CreatePeerService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, c.cluster.AsOwner())
}

func (c *Cluster) run() {
	defer func() {
		c.logger.Infof("deleting the failed cluster")
		c.reportFailedStatus()
		c.delete()
	}()

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

func (c *Cluster) delete() {
	c.gc.CollectCluster(c.cluster.Name, garbagecollection.NullUID)
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

func (c *Cluster) createPod(members couchbaseutil.MemberSet, m *couchbaseutil.Member,
	serverSpec api.ServerConfig) error {

	pod := k8sutil.CreateCouchbasePod(m, c.cluster.Name, c.cluster.Spec,
		serverSpec, c.cluster.AsOwner())
	_, err := c.config.KubeCli.Core().Pods(c.cluster.Namespace).Create(pod)
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

func (c *Cluster) waitForPod(podName string) error {

	opts := metav1.ListOptions{
		LabelSelector: "couchbase_node=" + podName,
	}

	watcher, err := c.config.KubeCli.Core().Pods(c.cluster.Namespace).Watch(opts)
	if err != nil {
		return err
	}
	events := watcher.ResultChan()
	for ev := range events {
		obj := ev.Object.(*v1.Pod)
		status := obj.Status

		switch ev.Type {

		// check if any error occurred creating pod
		case watch.Error:
			return ErrCreatingPod(status.Reason)
		case watch.Deleted:
			return ErrCreatingPod(status.Reason)
		case watch.Added, watch.Modified:

			// make sure created pod is now running
			switch status.Phase {
			case v1.PodRunning:
				return nil
			case v1.PodPending:
			default:
				return ErrRunningPod(status.Reason)
			}
		}
	}

	return errUnkownCreatePod
}

func (c *Cluster) pollPods() (running, pending []*v1.Pod, err error) {
	podList, err := c.config.KubeCli.Core().Pods(c.cluster.Namespace).List(k8sutil.ClusterListOpt(c.cluster.Name))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list running pods: %v", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
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

func (c *Cluster) reportFailedStatus() {
	retryInterval := 5 * time.Second

	f := func() (bool, error) {
		c.status.SetPhase(api.ClusterPhaseFailed)
		err := c.updateCRStatus()
		if err == nil || k8sutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}

		if !apierrors.IsConflict(err) {
			c.logger.Warningf("retry report status in %v: fail to update: %v", retryInterval, err)
			return false, nil
		}

		cl, err := c.config.CouchbaseCRCli.CouchbaseV1beta1().CouchbaseClusters(c.cluster.Namespace).
			Get(c.cluster.Name, metav1.GetOptions{})
		if err != nil {
			// Update (PUT) will return conflict even if object is deleted since we have UID set in object.
			// Because it will check UID first and return something like:
			// "Precondition failed: UID in precondition: 0xc42712c0f0, UID in object meta: ".
			if k8sutil.IsKubernetesResourceNotFoundError(err) {
				return true, nil
			}
			c.logger.Warningf("retry report status in %v: fail to get latest version: %v", retryInterval, err)
			return false, nil
		}
		c.cluster = cl
		return false, nil

	}
	retryutil.Retry(retryInterval, math.MaxInt64, f)
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
		return ErrSecretMissingUsername(authSecret)
	}
	if password, ok := data[constants.AuthSecretPasswordKey]; ok {
		c.password = string(password[:])
	} else {
		return ErrSecretMissingPassword(authSecret)
	}

	return nil
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
