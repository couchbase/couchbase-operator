package framework

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/couchbaselabs/couchbase-operator/pkg/client"
	"github.com/couchbaselabs/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/constants"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/probe"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2eutil"

	"github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var Global *Framework

type Framework struct {
	opImage    string
	KubeClient kubernetes.Interface
	CRClient   versioned.Interface
	Namespace  string
	//S3Cli      *s3.S3
	//S3Bucket   string
}

// Setup setups a test framework and points "Global" to it.
func Setup() error {
	kubeconfig := flag.String("kubeconfig", "", "kube config path, e.g. $HOME/.kube/config")
	opImage := flag.String("operator-image", "", "operator image, e.g. couchbase/couchbase-operator")
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

	Global = &Framework{
		KubeClient: cli,
		CRClient:   client.MustNew(config),
		Namespace:  *ns,
		opImage:    *opImage,
		//S3Bucket:   os.Getenv("TEST_S3_BUCKET"),
	}
	return Global.setup()
}

func Teardown() error {
	if err := Global.deleteCouchbaseOperator(); err != nil {
		return err
	}
	// TODO: check all deleted and wait
	Global = nil
	logrus.Info("e2e teardown successfully")
	return nil
}

func (f *Framework) setup() error {
	if err := f.SetupCouchbaseOperator(); err != nil {
		return fmt.Errorf("failed to setup couchbase operator: %v", err)
	}
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
	// TODO: unify this and the yaml file in example/
	cmd := []string{"/usr/local/bin/couchbase-operator"}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "couchbase-operator",
			Labels: map[string]string{"name": "couchbase-operator"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            "couchbase-operator",
					Image:           f.opImage,
					ImagePullPolicy: v1.PullIfNotPresent,
					Command:         cmd,
					Env: []v1.EnvVar{
						{
							Name:      constants.EnvOperatorPodNamespace,
							ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
						},
						{
							Name:      constants.EnvOperatorPodName,
							ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.name"}},
						},
					},
					ReadinessProbe: &v1.Probe{
						Handler: v1.Handler{
							HTTPGet: &v1.HTTPGetAction{
								Path: probe.HTTPReadyzEndpoint,
								Port: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
							},
						},
						InitialDelaySeconds: 3,
						PeriodSeconds:       3,
						FailureThreshold:    3,
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	p, err := e2eutil.CreateAndWaitPod(f.KubeClient, f.Namespace, pod, 60*time.Second)
	if err != nil {
		// assuming `kubectl` installed on $PATH
		cmd := exec.Command("kubectl", "-n", f.Namespace, "describe", "pod", "couchbase-operator")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Run() // Just ignore the error...
		logrus.Info("describing couchbase-operator pod:", out.String())
		return err
	}
	logrus.Infof("couchbase operator pod is running on node (%s)", p.Spec.NodeName)

	return e2eutil.WaitUntilOperatorReady(f.KubeClient, f.Namespace, "couchbase-operator")
}

func (f *Framework) DeleteCouchbaseOperatorCompletely() error {
	err := f.deleteCouchbaseOperator()
	if err != nil {
		return err
	}
	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	err = retryutil.Retry(5*time.Second, 6, func() (bool, error) {
		_, err := f.KubeClient.CoreV1().Pods(f.Namespace).Get("couchbase-operator", metav1.GetOptions{})
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

func (f *Framework) deleteCouchbaseOperator() error {
	return f.KubeClient.CoreV1().Pods(f.Namespace).Delete("couchbase-operator", metav1.NewDeleteOptions(1))
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
