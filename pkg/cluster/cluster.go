package cluster

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/couchbaselabs/couchbase-operator/pkg/client"
	"github.com/couchbaselabs/couchbase-operator/pkg/garbagecollection"
	"github.com/couchbaselabs/couchbase-operator/pkg/spec"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes"
)

var (
	reconcileInterval = 8 * time.Second
)

type clusterEventType string

const (
	eventDeleteCluster clusterEventType = "Delete"
	eventModifyCluster clusterEventType = "Modify"
)

type clusterEvent struct {
	typ     clusterEventType
	cluster *spec.CouchbaseCluster
}

type Config struct {
	ServiceAccount string
	KubeCli        kubernetes.Interface
	CouchbaseCRCli client.CouchbaseClusterCR
}

type Cluster struct {
	logger        *logrus.Entry
	config        Config
	cluster       *spec.CouchbaseCluster
	status        spec.ClusterStatus
	memberCounter int
	eventCh       chan *clusterEvent
	stopCh        chan struct{}
	gc            *garbagecollection.GC
}

func New(config Config, cl *spec.CouchbaseCluster) *Cluster {
	c := &Cluster{
		logger: logrus.WithFields(logrus.Fields{
			"module":       "cluster",
			"cluster-name": cl.Name,
		}),
		config:  config,
		status:  cl.Status.Copy(),
		cluster: cl,
		eventCh: make(chan *clusterEvent, 100),
		stopCh:  make(chan struct{}),
		gc:      garbagecollection.New(config.KubeCli, cl.Namespace),
	}
	c.logger.Info("Watching new cluster")

	go func() {
		if err := c.setup(); err != nil {
			c.logger.Errorf("cluster failed to setup: %v", err)
			if c.status.Phase != spec.ClusterPhaseFailed {
				c.status.SetReason(err.Error())
				c.status.SetPhase(spec.ClusterPhaseFailed)
				if err := c.updateCRStatus(); err != nil {
					c.logger.Errorf("failed to update cluster phase (%v): %v",
						spec.ClusterPhaseFailed, err)
				}
			}
			return
		}
		c.run()
	}()

	return c
}

func (c *Cluster) Update(cl *spec.CouchbaseCluster) {
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
	case spec.ClusterPhaseNone:
		shouldCreateCluster = true
	case spec.ClusterPhaseCreating:
		return errCreatedCluster
	case spec.ClusterPhaseRunning:
		shouldCreateCluster = false
	}

	if shouldCreateCluster {
		return c.create()
	}

	return nil
}

func (c *Cluster) create() error {
	c.status.SetPhase(spec.ClusterPhaseCreating)
	if err := c.updateCRStatus(); err != nil {
		return fmt.Errorf("cluster create: failed to update cluster phase (%v): %v",
			spec.ClusterPhaseCreating, err)
	}

	m := &couchbaseutil.Member{
		Name:      couchbaseutil.CreateMemberName(c.cluster.Name, c.memberCounter),
		Namespace: c.cluster.Namespace,
	}
	ms := couchbaseutil.NewMemberSet(m)
	if err := c.createPod(ms, m); err != nil {
		return err
	}

	return k8sutil.CreateCouchbaseService(c.config.KubeCli, c.cluster.Name,
		c.cluster.Namespace, c.cluster.AsOwner())
}

func (c *Cluster) run() {
	defer func() {
		c.logger.Infof("deleting the failed cluster")
		//c.reportFailedStatus()
		c.delete()
	}()

	c.status.SetPhase(spec.ClusterPhaseRunning)
	if err := c.updateCRStatus(); err != nil {
		c.logger.Warningf("update initial CR status failed: %v", err)
	}
	c.logger.Infof("start running...")

	//var rerr error
	for {
		select {
		case event := <-c.eventCh:
			switch event.typ {
			case eventModifyCluster:
				/*if isSpecEqual(event.cluster.Spec, c.cluster.Spec) {
					break
				}
				// TODO: we can't handle another upgrade while an upgrade is in progress
				c.logSpecUpdate(event.cluster.Spec)

				*/
				c.cluster = event.cluster
			case eventDeleteCluster:
				c.logger.Infof("cluster is deleted by the user")
				return
			default:
				panic("unknown event type" + event.typ)
			}

		case <-time.After(reconcileInterval):
			//start := time.Now()

			if c.cluster.Spec.Paused {
				c.status.PauseControl()
				c.logger.Infof("control is paused, skipping reconciliation")
				continue
			} else {
				c.status.Control()
			}
			/*
				running, pending, err := c.pollPods()
				if err != nil {
					c.logger.Errorf("fail to poll pods: %v", err)
					reconcileFailed.WithLabelValues("failed to poll pods").Inc()
					continue
				}

				if len(pending) > 0 {
					// Pod startup might take long, e.g. pulling image. It would deterministically become running or succeeded/failed later.
					c.logger.Infof("skip reconciliation: running (%v), pending (%v)", k8sutil.GetPodNames(running), k8sutil.GetPodNames(pending))
					reconcileFailed.WithLabelValues("not all pods are running").Inc()
					continue
				}
				if len(running) == 0 {
					c.logger.Warningf("all etcd pods are dead. Trying to recover from a previous backup")
					rerr = c.disasterRecovery(nil)
					if rerr != nil {
						c.logger.Errorf("fail to do disaster recovery: %v", rerr)
					}
					// On normal recovery case, we need backoff. On error case, this could be either backoff or leading to cluster delete.
					break
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

				if err := c.updateLocalBackupStatus(); err != nil {
					c.logger.Warningf("failed to update local backup service status: %v", err)
				}
				c.updateMemberStatus(c.members)
				if err := c.updateCRStatus(); err != nil {
					c.logger.Warningf("periodic update CR status failed: %v", err)
				}

				reconcileHistogram.WithLabelValues(c.name()).Observe(time.Since(start).Seconds())*/
		}
		/*
			if rerr != nil {
				reconcileFailed.WithLabelValues(rerr.Error()).Inc()
			}*/

		/*		if isFatalError(rerr) {
				c.status.SetReason(rerr.Error())
				c.logger.Errorf("cluster failed: %v", rerr)
				return
			}*/
	}
}

func (c *Cluster) delete() {
	c.gc.CollectCluster(c.cluster.Name, garbagecollection.NullUID)
}

func (c *Cluster) updateCRStatus() error {
	if reflect.DeepEqual(c.cluster.Status, c.status) {
		return nil
	}

	newCluster := c.cluster
	newCluster.Status = c.status
	newCluster, err := c.config.CouchbaseCRCli.Update(context.TODO(), c.cluster)
	if err != nil {
		return fmt.Errorf("failed to update CR status: %v", err)
	}

	c.cluster = newCluster

	return nil
}

func (c *Cluster) createPod(members couchbaseutil.MemberSet, m *couchbaseutil.Member) error {

	pod := k8sutil.CreateCouchbasePod(m, c.cluster.Name, c.cluster.Spec, c.cluster.AsOwner())
	_, err := c.config.KubeCli.Core().Pods(c.cluster.Namespace).Create(pod)
	return err
}
