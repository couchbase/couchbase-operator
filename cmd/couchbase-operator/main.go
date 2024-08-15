package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/apis"
	"github.com/couchbase/couchbase-operator/pkg/chaos"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/controller"
	"github.com/couchbase/couchbase-operator/pkg/logging"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	listenAddr         string
	printVersion       bool
	podCreateTimeout   string
	podDeleteDelay     string
	podReadinessDelay  string
	podReadinessPeriod string
	chaosLevel         int
	concurrency        int

	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)

var log = logf.Log.WithName("main")

// create controller from initialised config.
func main() {
	logOptions := &logging.Options{}
	logOptions.AddFlagSet(flag.CommandLine)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.StringVar(&listenAddr, "listen-addr", "0.0.0.0:8080", "The address on which the HTTP server will listen to")
	pflag.IntVar(&chaosLevel, "chaos-level", -1, "DO NOT USE IN PRODUCTION - level of chaos injected into the couchbase clusters created by the operator.")
	pflag.BoolVar(&printVersion, "version", false, "Show version and quit")
	pflag.StringVar(&podCreateTimeout, "pod-create-timeout", "10m", "Sets the amount of time to wait for Pod creation to complete")
	pflag.StringVar(&podDeleteDelay, "pod-delete-delay", "0m", "Sets the amount of time to wait to allow a pod to recover before deleting it")
	pflag.StringVar(&podReadinessDelay, "pod-readiness-delay", "10s", "Sets the amount of time to wait after the pod has started before readiness probes are initiated")
	pflag.StringVar(&podReadinessPeriod, "pod-readiness-period", "20s", "Sets the period between readiness probes")
	pflag.IntVar(&concurrency, "concurrency", 4, "Number of concurrent reconciles to allow")
	pflag.Parse()

	// Route all library logging to the ZAP JSON logger.
	logger := logging.New(logOptions)
	logf.SetLogger(logger)
	klog.SetLogger(logger)

	// Log the version, branch and revision so we know
	// * Version feature set
	// * Whether this is an official or development branch
	// * The exact commit defects are raised against
	log.Info(version.Application, "version", version.WithBuildNumber(), "revision", revision.Revision())

	metrics.InitMetrics()

	if printVersion {
		os.Exit(0)
	}

	namespace, ok := os.LookupEnv("WATCH_NAMESPACE")
	if !ok {
		log.Error(fmt.Errorf("WATCH_NAMESPACE must be set"), "Failed to get watch namespace")
		os.Exit(1)
	}

	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "Error getting config")
		os.Exit(1)
	}

	log.V(1).Info("Initializing resource manager.")

	mgr, err := manager.New(cfg, manager.Options{
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				namespace: {LabelSelector: labels.Everything(), FieldSelector: fields.Everything()},
			},
		},
		Metrics: server.Options{
			BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		},
		LeaderElection:          true,
		LeaderElectionNamespace: namespace,
		LeaderElectionID:        "couchbase-operator",
	})
	if err != nil {
		log.Error(err, "Error initializing manager")
		os.Exit(1)
	}

	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "Error adding data types to scheme")
		os.Exit(1)
	}

	log.V(1).Info("Initializing controller.")

	clusterConfig := cluster.Config{
		PodCreateTimeout:   parseDuration(podCreateTimeout),
		PodDeleteDelay:     parseDuration(podDeleteDelay),
		PodReadinessDelay:  parseDuration(podReadinessDelay),
		PodReadinessPeriod: parseDuration(podReadinessPeriod),
	}

	if err := controller.AddToManager(mgr, concurrency, clusterConfig); err != nil {
		log.Error(err, "Error adding controller to manager")
		os.Exit(1)
	}

	// Report as ready when we have the lock and done initialization.
	http.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	go func() { _ = http.ListenAndServe(listenAddr, nil) }()

	chaos.Start(context.Background(), mgr, namespace, chaosLevel)

	log.V(1).Info("Starting resource manager.")

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Error starting resource manager")
		os.Exit(1)
	}
}

func parseDuration(durationString string) time.Duration {
	duration, err := time.ParseDuration(durationString)
	if err != nil {
		log.Error(err, "Error parsing cli options")
		os.Exit(1)
	}

	return duration
}
