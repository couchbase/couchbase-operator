package framework

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"

	"github.com/sirupsen/logrus"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var Global *Framework

type Framework struct {
	opImage       string
	KubeClient    kubernetes.Interface
	CRClient      versioned.Interface
	Deployment    *v1beta1.Deployment
	Namespace     string
	DefaultSecret *v1.Secret
	config        *rest.Config
	LogDir        string
	//S3Cli      *s3.S3
	//S3Bucket   string
}

// Setup setups a test framework and points "Global" to it.
func Setup() error {
	kubeconfig := flag.String("kubeconfig", "", "kube config path, e.g. $HOME/.kube/config")
	opImage := flag.String("operator-image", "", "operator image, e.g. couchbase/couchbase-operator")
	deploymentSpec := flag.String("deployment-spec", "", "deployment spec, eg. $PWD/example/deployment.yaml")
	ns := flag.String("namespace", "default", "e2e test namespace")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return err
	}
	cli, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	err = v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		return err
	}

	err = api.AddToScheme(scheme.Scheme)
	if err != nil {
		return err
	}

	raw, err := ioutil.ReadFile(*deploymentSpec)
	if err != nil {
		return err
	}

	deserializer := scheme.Codecs.UniversalDeserializer()
	obj, _, err := deserializer.Decode([]byte(raw), nil, nil)
	if err != nil {
		return err
	}

	deployment, ok := obj.(*v1beta1.Deployment)
	if !ok {
		return fmt.Errorf("File %s does not define a deployment", *deploymentSpec)
	}

	logDir, err := makeLogDir()
	if err != nil {
		return err
	}

	Global = &Framework{
		KubeClient: cli,
		CRClient:   client.MustNew(config),
		Deployment: deployment,
		Namespace:  *ns,
		opImage:    *opImage,
		config:     config,
		//S3Bucket:   os.Getenv("TEST_S3_BUCKET"),
		LogDir: logDir,
	}
	return Global.setup()
}

func Teardown() error {
	if err := Global.DeleteCouchbaseOperatorCompletely(Global.Deployment.GetName()); err != nil {
		return err
	}

	err := e2eutil.DeleteSecret(Global.KubeClient, Global.Namespace, Global.DefaultSecret.Name, &metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("Unable to delete the default secret: %v", err)
	}

	clusters, err := Global.CRClient.CouchbaseV1beta1().CouchbaseClusters(Global.Namespace).List(metav1.ListOptions{})
	for _, cluster := range clusters.Items {
		Global.CRClient.CouchbaseV1beta1().CouchbaseClusters(Global.Namespace).Delete(cluster.Name, metav1.NewDeleteOptions(0))
	}

	pods, err := Global.KubeClient.CoreV1().Pods(Global.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	killPods := []string{}
	for _, pod := range pods.Items {
		killPods = append(killPods, pod.Name)
	}
	e2eutil.KillMembers(Global.KubeClient, Global.Namespace, killPods...)

	services, err := Global.KubeClient.CoreV1().Services(Global.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	for _, service := range services.Items {
		Global.KubeClient.CoreV1().Services(Global.Namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
	}

	// TODO: check all deleted and wait
	Global = nil
	logrus.Info("e2e teardown successfully")
	return nil
}

func (f *Framework) setup() error {
	logrus.Info("cleaning up namespace before deployment")
	deployments, err := f.KubeClient.ExtensionsV1beta1().Deployments(f.Namespace).List(metav1.ListOptions{})
	for _, deployment := range deployments.Items {
		Global.DeleteCouchbaseOperatorCompletely(deployment.GetName())
	}

	clusters, err := f.CRClient.CouchbaseV1beta1().CouchbaseClusters(f.Namespace).List(metav1.ListOptions{})
	for _, cluster := range clusters.Items {
		f.CRClient.CouchbaseV1beta1().CouchbaseClusters(f.Namespace).Delete(cluster.Name, metav1.NewDeleteOptions(0))
	}

	pods, err := f.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	killPods := []string{}
	for _, pod := range pods.Items {
		killPods = append(killPods, pod.Name)
	}
	e2eutil.KillMembers(f.KubeClient, f.Namespace, killPods...)

	services, err := f.KubeClient.CoreV1().Services(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
	for _, service := range services.Items {
		f.KubeClient.CoreV1().Services(f.Namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
	}

	e2eutil.DeleteSecret(f.KubeClient, f.Namespace, "basic-test-secret", &metav1.DeleteOptions{})

	logrus.Info("deploying operator")
	if err := f.SetupCouchbaseOperator(); err != nil {
		return fmt.Errorf("failed to setup couchbase operator: %v", err)
	}

	secret, err := e2eutil.CreateSecret(f.KubeClient, f.Namespace, e2espec.NewDefaultSecret(f.Namespace))
	if err != nil {
		return fmt.Errorf("failed to create default couchbase secret: %v", err)
	}
	f.DefaultSecret = secret

	logrus.Info("couchbase operator created successfully")
	if os.Getenv("AWS_TEST_ENABLED") == "true" {
		/*if err := f.setupAWS(); err != nil {
			return fmt.Errorf("fail to setup aws: %v", err)
		}*/
	}
	logrus.Info("e2e setup successfully")
	return nil
}

func (f *Framework) SetupCouchbaseOperator() error {
	_, err := f.KubeClient.ExtensionsV1beta1().Deployments(f.Namespace).Create(f.Deployment)
	if err != nil {
		return err
	}

	return e2eutil.WaitUntilOperatorReady(f.KubeClient, f.Namespace, "couchbase-operator")
}

func (f *Framework) DeleteCouchbaseOperatorCompletely(deploymentName string) error {
	err := f.deleteCouchbaseOperator(deploymentName)
	if err != nil {
		return err
	}
	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	err = retryutil.Retry(5*time.Second, 24, func() (bool, error) {
		_, err := f.KubeClient.ExtensionsV1beta1().Deployments(f.Namespace).Get("couchbase-operator", metav1.GetOptions{})
		if err == nil {
			return false, nil
		}
		if k8sutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		return fmt.Errorf("fail to wait couchbase operator pod gone from API: %v", err)
	}
	return nil
}

func (f *Framework) deleteCouchbaseOperator(deploymentName string) error {
	deletePropagation := metav1.DeletePropagationForeground
	deleteOpts := metav1.NewDeleteOptions(0)
	deleteOpts.PropagationPolicy = &deletePropagation
	return f.KubeClient.ExtensionsV1beta1().Deployments(f.Namespace).Delete(deploymentName, deleteOpts)
}

func (f *Framework) ApiServerHost() string {
	return f.config.Host
}

func (f *Framework) PodClient() typedv1.PodInterface {
	return f.KubeClient.CoreV1().Pods(f.Namespace)
}

func makeLogDir() (string, error) {
	dir, err := GenerateLogDir()
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, os.ModePerm)
}

func GenerateLogDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	t := time.Now()
	ts := t.Format(time.RFC3339)
	return filepath.Join(cwd, "logs", ts), nil
}

/*
func (f *Framework) setupAWS() error {
	if err := os.Setenv("AWS_SHARED_CREDENTIALS_FILE", os.Getenv("AWS_CREDENTIAL")); err != nil {
		return err
	}
	if err := os.Setenv("AWS_CONFIG_FILE", os.Getenv("AWS_CONFIG")); err != nil {
		return err
	}
	sess, err := session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return err
	}
	f.S3Cli = s3.New(sess)
	return nil
}*/
