package controller

import (
	"context"
	"fmt"
	"time"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/probe"

	"github.com/sirupsen/logrus"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/fields"
	kwatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

var (
	initRetryWaitTime = 30 * time.Second
)

// TODO: get rid of this once we use workqueue
var pt *panicTimer

func init() {
	pt = newPanicTimer(time.Minute, "unexpected long blocking (> 1 Minute) when handling cluster event")
}

type Event struct {
	Type   kwatch.EventType
	Object *cbapi.CouchbaseCluster
}

type Controller struct {
	Config
	logger   *logrus.Entry
	clusters map[string]*cluster.Cluster
}

type Config struct {
	MasterHost     string
	Namespace      string
	ServiceAccount string
	KubeCli        kubernetes.Interface
	KubeExtCli     apiextensionsclient.Interface
	CouchbaseCRCli versioned.Interface
}

func (c *Config) Validate() error {
	return nil
}

func New(cfg Config) *Controller {
	return &Controller{
		Config:   cfg,
		logger:   logrus.WithField("module", "controller"),
		clusters: make(map[string]*cluster.Cluster),
	}
}

func (c *Controller) Start() error {

	for {
		err := c.initResource()
		if err == nil {
			break
		}
		c.logger.Errorf("Initialization failed: %v", err)
		c.logger.Infof("Retry initialization in %v...", initRetryWaitTime)
		time.Sleep(initRetryWaitTime)
	}

	c.logger.Infof("CRD initialized, listening for events...")
	probe.SetReady()
	c.run()
	panic("unreachable")
}

func (c *Controller) run() {
	source := cache.NewListWatchFromClient(
		c.Config.CouchbaseCRCli.CouchbaseV1beta1().RESTClient(),
		cbapi.CRDResourcePlural,
		c.Config.Namespace,
		fields.Everything())

	_, informer := cache.NewIndexerInformer(source, &cbapi.CouchbaseCluster{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAddCouchbaseCluster,
		UpdateFunc: c.onUpdateCouchbaseCluster,
		DeleteFunc: c.onDeleteCouchbaseCluster,
	}, cache.Indexers{})

	ctx := context.TODO()
	// TODO: use workqueue to avoid blocking
	informer.Run(ctx.Done())
}

func (c *Controller) initResource() error {
	version, err := k8sutil.GetKubernetesVersion(c.Config.KubeCli)
	if err != nil {
		c.logger.Logger.Warn("Unable to get server version due to %v, skipping validation registration", err)
	}

	err = k8sutil.CreateCRD(c.KubeExtCli, version)
	if err != nil && !k8sutil.IsKubernetesResourceAlreadyExistError(err) {
		return fmt.Errorf("fail to create CRD: %v", err)
	}

	return k8sutil.WaitCRDReady(c.KubeExtCli)
}

func (c *Controller) onAddCouchbaseCluster(obj interface{}) {
	c.syncCouchbaseClus(obj.(*cbapi.CouchbaseCluster))
}

func (c *Controller) onUpdateCouchbaseCluster(oldObj, newObj interface{}) {
	c.syncCouchbaseClus(newObj.(*cbapi.CouchbaseCluster))
}

func (c *Controller) onDeleteCouchbaseCluster(obj interface{}) {
	clus, ok := obj.(*cbapi.CouchbaseCluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			panic(fmt.Sprintf("unknown object from CouchbaseCluster delete event: %#v", obj))
		}
		clus, ok = tombstone.Obj.(*cbapi.CouchbaseCluster)
		if !ok {
			panic(fmt.Sprintf("Tombstone contained object that is not an CouchbaseCluster: %#v", obj))
		}
	}
	ev := &Event{
		Type:   kwatch.Deleted,
		Object: clus,
	}

	pt.start()
	err := c.handleClusterEvent(ev)
	if err != nil {
		logrus.Warningf("Fail to handle delete event: %v", err)
	}
	pt.stop()
}

func (c *Controller) syncCouchbaseClus(clus *cbapi.CouchbaseCluster) {
	ev := &Event{
		Type:   kwatch.Added,
		Object: clus,
	}
	if _, ok := c.clusters[clus.Name]; ok { // re-watch or restart could give ADD event
		ev.Type = kwatch.Modified
	}

	pt.start()
	err := c.handleClusterEvent(ev)
	if err != nil {
		logrus.Warningf("Fail to handle add/update event: %v", err)
	}
	pt.stop()
}

func (c *Controller) handleClusterEvent(event *Event) error {
	clus := event.Object

	if clus.Status.IsFailed() {
		if event.Type == kwatch.Deleted {
			delete(c.clusters, clus.Name)
			return nil
		}
		return fmt.Errorf("ignore failed cluster (%s). Please delete its CR", clus.Name)
	}

	// TODO: add validation to spec update.
	clus.Spec.Cleanup()

	switch event.Type {
	case kwatch.Added:
		if _, ok := c.clusters[clus.Name]; ok {
			return fmt.Errorf("unsafe state. cluster (%s) was created before but we received event (%s)", clus.Name, event.Type)
		}

		nc := cluster.New(c.makeClusterConfig(), clus)

		c.clusters[clus.Name] = nc
	case kwatch.Modified:
		if _, ok := c.clusters[clus.Name]; !ok {
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		c.clusters[clus.Name].Update(clus)

	case kwatch.Deleted:
		if _, ok := c.clusters[clus.Name]; !ok {
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		c.clusters[clus.Name].Delete()
		delete(c.clusters, clus.Name)
	}
	return nil
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		ServiceAccount: c.Config.ServiceAccount,
		KubeCli:        c.Config.KubeCli,
		CouchbaseCRCli: c.Config.CouchbaseCRCli,
	}
}
