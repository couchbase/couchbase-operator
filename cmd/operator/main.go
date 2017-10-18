package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/couchbaselabs/couchbase-operator/pkg/client"
	"github.com/couchbaselabs/couchbase-operator/pkg/controller"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/probe"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"

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

const version = "0.8.0"

var (
	listenAddr   string
	name         string
	namespace    string
	printVersion bool
	mainLogger   *logrus.Entry
)

// parse command-line args and initialise config
func init() {
	flag.StringVar(&listenAddr, "listen-addr", "0.0.0.0:8080", "The address on which the HTTP server will listen to")
	flag.BoolVar(&printVersion, "version", false, "Show version and quit")
	flag.Parse()
	logrus.SetOutput(os.Stdout)
	mainLogger = logrus.WithFields(logrus.Fields{"module": "main"})
}

// create controller from initialised config
func main() {
	namespace = os.Getenv("MY_POD_NAMESPACE")
	if len(namespace) == 0 {
		mainLogger.Fatalf("Must set env MY_POD_NAMESPACE")
	}
	name = os.Getenv("MY_POD_NAME")
	if len(name) == 0 {
		mainLogger.Fatalf("Must set env MY_POD_NAME")
	}

	if printVersion {
		fmt.Println("couchbase-operator", version)
		os.Exit(0)
	}

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
	if err := cfg.Validate(); err != nil {
		mainLogger.Fatalf("Invalid operator config: %v", err)
	}

	//go periodicFullGC(cfg.KubeCli, cfg.Namespace, gcInterval)

	//startChaos(context.Background(), cfg.KubeCli, cfg.Namespace, chaosLevel)

	c := controller.New(cfg)
	err := c.Start()
	mainLogger.Fatalf("Controller Start() failed: %v", err)
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
		KubeExtCli:     k8sutil.MustNewKubeExtClient(),
		CouchbaseCRCli: client.MustNewInCluster(),
	}

	return cfg
}

func getMyPodServiceAccount(kubecli kubernetes.Interface) (string, error) {
	var sa string
	err := retryutil.Retry(5*time.Second, 100, func() (bool, error) {
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
