package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/chaos"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/controller"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/logutil"
	"github.com/couchbase/couchbase-operator/pkg/util/probe"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
)

var (
	listenAddr    string
	name          string
	namespace     string
	logLevel      string
	createCrd     bool
	printVersion  bool
	verifyVersion bool

	chaosLevel int

	mainLogger *logrus.Entry
)

// parse command-line args and initialise config
func init() {
	flag.StringVar(&listenAddr, "listen-addr", "0.0.0.0:8080", "The address on which the HTTP server will listen to")
	flag.IntVar(&chaosLevel, "chaos-level", -1, "DO NOT USE IN PRODUCTION - level of chaos injected into the couchbase clusters created by the operator.")
	flag.BoolVar(&createCrd, "create-crd", false, "Create the crd if it does not exist")
	flag.BoolVar(&printVersion, "version", false, "Show version and quit")
	flag.BoolVar(&verifyVersion, "verify-version", true, "Skip verification of required couchbase min version")
	flag.StringVar(&logLevel, "log-level", "info", "Sets the logging level (panic, fatal, error, warn, info, debug)")
	flag.Parse()
	logrus.SetOutput(os.Stdout)
	mainLogger = logrus.WithFields(logrus.Fields{"module": "main"})
}

// create controller from initialised config
func main() {
	if level, err := logrus.ParseLevel(logLevel); err != nil {
		mainLogger.Fatalf("Invalid log level: %s", logLevel)
	} else {
		logutil.SetLogLevel(level)
	}

	namespace = os.Getenv("MY_POD_NAMESPACE")
	if len(namespace) == 0 {
		mainLogger.Fatalf("Must set env MY_POD_NAMESPACE")
	}
	name = os.Getenv("MY_POD_NAME")
	if len(name) == 0 {
		mainLogger.Fatalf("Must set env MY_POD_NAME")
	}

	if printVersion {
		fmt.Println(version.Application, version.Version)
		os.Exit(0)
	}

	// Log the version, branch and revision so we know
	// * Version feature set
	// * Whether this is an official or development branch
	// * The exact commit defects are raised against
	mainLogger.Infof("%s v%s (%s)", version.Application, version.Version, revision.Revision())

	id, err := os.Hostname()
	if err != nil {
		mainLogger.Fatalf("Failed to get container hostname: %v", err)
	}

	kubecli := k8sutil.MustNewKubeClient()

	http.HandleFunc(probe.HTTPReadyzEndpoint, probe.ReadyzHandler)
	go http.ListenAndServe(listenAddr, nil)

	mainLogger.Info("Obtaining resource lock")
	rl, err := resourcelock.New(resourcelock.EndpointsResourceLock,
		namespace,
		"couchbase-operator",
		kubecli.CoreV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: createRecorder(kubecli, name, namespace),
		})
	if err != nil {
		mainLogger.Fatalf("Error creating resource lock: %v", err)
	}

	mainLogger.Info("Attempting to be elected the couchbase-operator leader")
	leaderelection.RunOrDie(leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				mainLogger.Fatalf("Leader election lost")
			},
		},
	})
}

func run(stop <-chan struct{}) {
	mainLogger.Info("I'm the leader, attempt to start the operator")
	cfg := newControllerConfig()

	chaos.Start(context.Background(), cfg.KubeCli, cfg.Namespace, chaosLevel)

	c := controller.New(cfg)
	c.Start()
}

func newControllerConfig() controller.Config {
	mainLogger.Info("Creating the couchbase-operator controller")
	kubecli := k8sutil.MustNewKubeClient()

	serviceAccount, err := getMyPodServiceAccount(kubecli)
	if err != nil {
		mainLogger.Fatalf("Fail to get my pod's service account: %v", err)
	}

	cfg := controller.Config{
		Namespace:      namespace,
		ServiceAccount: serviceAccount,
		KubeCli:        kubecli,
		CouchbaseCRCli: client.MustNewInCluster(),
		CreateCrd:      createCrd,
		VerifyVersion:  verifyVersion,
	}

	return cfg
}

func getMyPodServiceAccount(kubecli kubernetes.Interface) (string, error) {
	var sa string
	err := retryutil.Retry(context.Background(), 5*time.Second, 100, func() (bool, error) {
		pod, err := kubecli.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			mainLogger.Errorf("Fail to get operator pod (%s): %v", name, err)
			return false, nil
		}
		sa = pod.Spec.ServiceAccountName
		return true, nil
	})

	if err != nil {
		mainLogger.Infof("Found pod service account: %s", sa)
	}
	return sa, err
}

func createRecorder(kubecli kubernetes.Interface, name, namespace string) record.EventRecorder {
	mainLogger.Info("Starting event recorder")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logrus.WithField("module", "event_recorder").Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: v1core.New(kubecli.Core().RESTClient()).Events(namespace)})
	return eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: name})
}
