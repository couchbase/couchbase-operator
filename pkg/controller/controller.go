package controller

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/coreos/operator-sdk/pkg/sdk"
	"github.com/coreos/operator-sdk/pkg/sdk/handler"
	"github.com/coreos/operator-sdk/pkg/sdk/types"

	cbapi "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/probe"

	"github.com/sirupsen/logrus"

	kwatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
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
	clusters *ManagedClusters
}

type Config struct {
	MasterHost     string
	Namespace      string
	ServiceAccount string
	CreateCrd      bool
	EnableUpgrades bool
	KubeCli        kubernetes.Interface
	CouchbaseCRCli versioned.Interface
	LogLevel       logrus.Level
	VerifyVersion  bool
}

func New(cfg Config) *Controller {
	return &Controller{
		Config:   cfg,
		logger:   logrus.WithField("module", "controller"),
		clusters: CreateManagedClusters(),
	}
}

func (c *Controller) Start() {

	if c.Config.CreateCrd {
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
	}
	probe.SetReady()

	sdk.Initialize()
	sdk.Watch(cbapi.SchemeGroupVersion.String(), cbapi.CRDResourceKind, c.Config.Namespace, 0)
	sdk.Handle((handler.Handler)(c))
	sdk.Run(context.TODO())

	panic("unreachable")
}

func (c *Controller) Handle(ctx types.Context, event types.Event) error {
	if cluster, ok := event.Object.(*cbapi.CouchbaseCluster); ok {
		cluster.Initialize()
		ev := &Event{
			Type:   kwatch.Added,
			Object: cluster,
		}

		if event.Deleted {
			ev.Type = kwatch.Deleted
		} else {
			if _, ok := c.clusters.Load(ev.Object.Name); ok { // re-watch or restart could give ADD event
				ev.Type = kwatch.Modified
			}
		}

		pt.start()
		err := c.handleClusterEvent(ev)
		if err != nil {
			logrus.Warningf("Fail to handle event: %v", err)
		}
		pt.stop()
	}

	return nil
}

func (c *Controller) initResource() error {
	version, err := k8sutil.GetKubernetesVersion(c.Config.KubeCli)
	if err != nil {
		c.logger.Logger.Warn("Unable to get server version due to %v, skipping validation registration", err)
	}

	if version < constants.KubernetesVersion1_8 {
		return fmt.Errorf("kubernetes version %s is too old, %s is minimum supported version ",
			version, constants.KubernetesVersion1_8)
	}

	kubeExtClient := k8sutil.MustNewKubeExtClient()
	err = k8sutil.CreateCRD(kubeExtClient, version)
	if err != nil && !k8sutil.IsKubernetesResourceAlreadyExistError(err) {
		return fmt.Errorf("fail to create CRD: %v", err)
	}

	return k8sutil.WaitCRDReady(kubeExtClient)
}

func (c *Controller) handleClusterEvent(event *Event) error {
	clus := event.Object

	if clus.Status.IsFailed() {
		if event.Type == kwatch.Deleted {
			c.clusters.Delete(clus.Name)
			return nil
		}
		return fmt.Errorf("ignore failed cluster (%s). Please delete its CR", clus.Name)
	}

	switch event.Type {
	case kwatch.Added:
		if _, ok := c.clusters.Load(clus.Name); ok {
			return fmt.Errorf("unsafe state. cluster (%s) was created before but we received event (%s)", clus.Name, event.Type)
		}

		if c.VerifyVersion {
			err := couchbaseutil.VerifyVersion(clus.Spec.Version)
			if err != nil {
				return fmt.Errorf("Cluster create failed, Please delete its CR: %s, %v", clus.Name, err)
			}
		}
		c.clusters.Store(clus.Name, cluster.New(c.makeClusterConfig(), clus))

	case kwatch.Modified:
		clusterContext, ok := c.clusters.Load(clus.Name)
		if !ok {
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		clusterContext.Update(clus)

	case kwatch.Deleted:
		clusterContext, ok := c.clusters.Load(clus.Name)
		if !ok {
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		clusterContext.Delete()
		c.clusters.Delete(clus.Name)
	}
	return nil
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		ServiceAccount: c.Config.ServiceAccount,
		KubeCli:        c.Config.KubeCli,
		CouchbaseCRCli: c.Config.CouchbaseCRCli,
		LogLevel:       c.Config.LogLevel,
		EnableUpgrades: c.Config.EnableUpgrades,
	}
}
