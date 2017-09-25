package cluster

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/couchbaselabs/couchbase-operator/pkg/client"
	"github.com/couchbaselabs/couchbase-operator/pkg/garbagecollection"
	"github.com/couchbaselabs/couchbase-operator/pkg/job"
	"github.com/couchbaselabs/couchbase-operator/pkg/spec"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
)

var (
	reconcileInterval = 8 * time.Second
)

type clusterEventType string

const (
	eventCreateCluster clusterEventType = "Create"
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
	jobSched      *job.Scheduler
}

func New(config Config, cl *spec.CouchbaseCluster) *Cluster {

	c := &Cluster{
		logger: logrus.WithFields(logrus.Fields{
			"module":       "cluster",
			"cluster-name": cl.Name,
		}),
		config:        config,
		status:        cl.Status.Copy(),
		memberCounter: 0,
		cluster:       cl,
		eventCh:       make(chan *clusterEvent, 100),
		stopCh:        make(chan struct{}),
		gc:            garbagecollection.New(config.KubeCli, cl.Namespace),
		jobSched:      job.NewScheduler(config.KubeCli, cl.Namespace),
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

	// start job watcher
	go func() {
		c.watchScedulerStatusUpdates()
	}()

	// send notification that cluster is created so that it can be initialized
	c.send(&clusterEvent{
		typ:     eventCreateCluster,
		cluster: cl,
	})
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

	for {
		select {
		case event := <-c.eventCh:
			switch event.typ {
			case eventCreateCluster:

				pod, err := c.getMemberPod(c.memberCounter - 1)
				if err != nil {
					c.logger.Errorf("Unable to get Pod for member: %d", c.memberCounter)
				}

				// get jobs required to enforce desired state
				jobsToSchedule := c.jobsForDesiredState(pod, event.cluster)

				// dispatch via scheduler
				err = c.jobSched.Dispatch(jobsToSchedule)
				if err != nil {
					c.logger.Errorf("Error occurred dispatching jobs: %v", err)
				}

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

			if c.cluster.Spec.Paused {
				c.status.PauseControl()
				c.logger.Infof("control is paused, skipping reconciliation")
				continue
			} else {
				c.status.Control()

				// DEBUGGING - periodically prints cluster status
				// data, err := json.Marshal(c.cluster)
				// if err == nil {
				// 	c.logger.Info(string(data))
				// }
			}

		}

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
	c.memberCounter += 1
	return err
}

// Get Pod representing the member counter
func (c *Cluster) getMemberPod(mc int) (*v1.Pod, error) {
	podName := couchbaseutil.CreateMemberName(c.cluster.Name, mc)
	pod, err := c.config.KubeCli.CoreV1().Pods(c.cluster.Namespace).
		Get(podName, metav1.GetOptions{})
	return pod, err
}

// make sure the pod has an IP.  If no IP is found then
// watcher will track updates until IP is present
// TODO: timeout/retry-count
func (c *Cluster) getPodIP(pod *v1.Pod) (string, error) {
	var err error
	var podIP string

	selector := "couchbase_node=" + pod.Name
	c.logger.Infof("Selector: %s", selector)
	watcher, err := c.config.
		KubeCli.
		Core().
		Pods(c.cluster.Namespace).
		Watch(metav1.ListOptions{LabelSelector: selector})

	if err != nil {
		c.logger.Errorf("Unable to watch Pod %s, %v", pod.Name, err)
	} else {
		for event := range watcher.ResultChan() {
			evPod := event.Object.(*v1.Pod)
			podIP = evPod.Status.PodIP
			c.logger.Infof("Pod-%s: %v", podIP, evPod)
			if podIP != "" {
				break
			}
		}
	}

	return podIP, err
}

// Creates jobs required to bring current spec
// in sync with desired state
func (c *Cluster) jobsForDesiredState(pod *v1.Pod, couchbaseCluster *spec.CouchbaseCluster) []job.JobRunner {
	jobs := []job.JobRunner{}
	clusterSpec := couchbaseCluster.Spec
	clusterStatus := c.status

	podIP, err := c.getPodIP(pod)
	if err != nil {
		return jobs
	}

	switch {
	case clusterSpec.Size > clusterStatus.Size:
		// Is a new node to the cluster which needs to be initialized
		jobs = append(jobs, job.NewNodeInitJob(podIP, c.cluster))
		fallthrough
	case clusterSpec.Size == (clusterStatus.Size + 1):
		// this is last node to be added to cluster. Run cluster-init
		jobs = append(jobs, job.NewClusterInitJob(podIP, c.cluster))
	}

	return jobs
}

// Watch for status update originating from job scheduler.
// Depending on the type of status being received,
// the spec can be updated.
//
// For instance, status that node-init has completed implies
// a node was initizlied and added to cluster
func (c *Cluster) watchScedulerStatusUpdates() {
	for {
		status := <-c.jobSched.StatusCh
		if status.Phase == job.Completed {
			if status.Type == job.NodeInit {
				c.status.Size = 1
			}
		}

		// update cr status
		c.updateCRStatus()
	}
}
