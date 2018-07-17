package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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
	"github.com/couchbase/couchbase-operator/pkg/util/logutil"
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
	VerifyVersion  bool
}

func New(cfg Config) *Controller {
	controller := &Controller{
		Config:   cfg,
		logger:   logrus.WithField("module", "controller"),
		clusters: CreateManagedClusters(),
	}

	http.HandleFunc("/v1/settings/logging", controller.SettingsLoggingHandler)
	http.HandleFunc("/v1/stats/cluster", controller.StatsClusterHandler)
	return controller
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

		// We need to explicitly obtain the lock here and not call the Store() function
		// because we need to ensure that the cluster is created and inserted into the
		// cluster map before releasing the lock. This is because config updates to
		// clusters (like changing the log level) only apply to clusters in the map. By
		// obtaining the lock to create a cluster and insert it into the map we can
		// ensure that the cluster is in the map when the config updates are applied.
		c.clusters.Lock.Lock()
		if toAdd := cluster.New(c.makeClusterConfig(), clus); toAdd != nil {
			c.clusters.Values[clus.Name] = toAdd
		}
		c.clusters.Lock.Unlock()

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
		LogLevel:       logutil.LogLevel(),
		EnableUpgrades: c.Config.EnableUpgrades,
	}
}

func (c *Controller) SettingsLoggingHandler(w http.ResponseWriter, r *http.Request) {
	type LoggerSettings struct {
		LogLevel string `json:"logLevel"`
	}

	if r.Method == http.MethodGet {
		var response LoggerSettings
		response.LogLevel = logutil.LogLevel().String()

		data, err := json.Marshal(response)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}

		w.Header().Add("Content-Type", "application/json")
		w.Write(data)
	} else if r.Method == http.MethodPost {
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			w.Write([]byte("Content-Type must be application/json"))
			return
		}

		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		var request LoggerSettings
		err = json.Unmarshal(body, &request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		level, err := logrus.ParseLevel(request.LogLevel)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		logutil.SetLogLevel(level)
		c.clusters.Range(func(key string, value *cluster.Cluster) bool {
			value.SetLoggingLevel(level)
			return true
		})

		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (c *Controller) StatsClusterHandler(w http.ResponseWriter, r *http.Request) {

	type Stats struct {
		Clusters map[string]*cluster.ClusterStats `json:"clusters"`
	}

	if r.Method == http.MethodGet {
		response := &Stats{
			Clusters: make(map[string]*cluster.ClusterStats),
		}

		c.clusters.Range(func(key string, value *cluster.Cluster) bool {
			response.Clusters[key] = value.Stats()
			return true
		})

		data, err := json.Marshal(response)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.Write(data)
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
