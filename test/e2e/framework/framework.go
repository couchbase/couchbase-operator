package framework

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/util/k8sutil/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
)

// Init performs one time only initialization of the framework.  Dynamic calls to these
// functions will result in race conditions and spurious failures.
func Init() error {
	// Register CouchbaseCluster and CustomResourceDefinition types with the main library.
	if err := v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := couchbasev2.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := apiextensionsv1beta1.SchemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := readYamlData(); err != nil {
		return err
	}

	// Start the timeout timer, after reading in suite configuration.
	startTimeoutTimer()

	return nil
}

func readYamlData() (err error) {
	testConfigFilePath := flag.String("testconfig", "resources/test_config.yaml", "test_config.yaml path. eg: $HOME/test_config.yaml")
	flag.Parse()

	logrus.Info("Using test_config file ", *testConfigFilePath)
	runtimeParams, err = readRuntimeConfig(*testConfigFilePath)
	if err != nil {
		return err
	}

	suiteFilePath := "./resources/suites/" + runtimeParams.SuiteToRun + ".yaml"

	logrus.Info("Using suite file ", suiteFilePath)
	suiteData, err = getSuiteDataFromYml(suiteFilePath)
	return err
}

// Returs time.Duration from given string
// Default return value: "2h0m0s"
func GetDuration(timeoutStr string) time.Duration {
	// Default timeout to 2 hours
	durationToReturn := (2 * time.Hour)

	pattern, _ := regexp.Compile("^([0-9]+)([mhd])$")

	// Calculates only if valid pattern exists
	if pattern.MatchString(timeoutStr) {
		match := pattern.FindStringSubmatch(timeoutStr)
		timeoutVal, err := strconv.Atoi(match[1])
		if err != nil {
			return durationToReturn
		}
		timeoutDuration := time.Duration(timeoutVal)
		switch match[2] {
		case "m":
			durationToReturn = timeoutDuration * time.Minute
		case "h":
			durationToReturn = timeoutDuration * time.Hour
		case "d":
			durationToReturn = timeoutDuration * (time.Hour * 24)
		}
	}
	return durationToReturn
}

// startTimeoutTimer starts timeout trigger based on given value in suiteData.Timeout
func startTimeoutTimer() {
	go func() {
		// In case of SystemTests, timeout will be set as part of test case
		if suiteData.SuiteName == "TestSystem" {
			logrus.Info("Skipping setting timer")
		}
		timeoutDuration := GetDuration(suiteData.Timeout)
		logrus.Infof("Setting timeout of %v from %v", timeoutDuration, time.Now())
		<-time.After(timeoutDuration)
		logrus.Infof("Timeout happened at %v", time.Now())

		// Dump out backtraces in case this is a deadlock situation.
		profile := pprof.Lookup("goroutine")
		if profile != nil {
			buffer := &bytes.Buffer{}
			_ = profile.WriteTo(buffer, 2)
			logrus.Info(buffer.String())
		}

		panic("Test timed out..")
	}()
}

func CreateDeploymentObject(operatorImage string, operatorPort int, podCreateTimeout fmt.Stringer) (deployment *appsv1.Deployment, err error) {
	deployment = config.GetOperatorDeployment(operatorImage, dockerPullSecretName, podCreateTimeout, "--zap-level", "debug")

	// Manually set the HTTP port.
	if operatorPort != 0 {
		listerAddrArg := "--listen-addr=0.0.0.0:" + strconv.Itoa(operatorPort)
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, listerAddrArg)
	}
	return
}

// Setup setups a test framework and points "Global" to it.
func Setup(t *testing.T) (err error) {

	// Always run the test at least once, unless overridden by the config.
	testRetries := 1
	if runtimeParams.TestRetries != nil {
		testRetries = *runtimeParams.TestRetries
	}

	// Initialize Global from runtime info
	Global = &Framework{
		KubeType:                    runtimeParams.KubeType,
		KubeVersion:                 runtimeParams.KubeVersion,
		OpImage:                     runtimeParams.OperatorImage,
		SkipTeardown:                runtimeParams.SkipTearDown,
		CollectLogs:                 runtimeParams.CollectLogsOnFailure,
		SuiteYmlData:                suiteData,
		ClusterConfFile:             runtimeParams.ClusterConfFile,
		ClusterSpec:                 types.ClusterMap{},
		CouchbaseServerImage:        runtimeParams.CouchbaseServerImage,
		CouchbaseServerImageUpgrade: runtimeParams.CouchbaseServerImageUpgrade,
		StorageClassName:            runtimeParams.StorageClassName,
		TestRetries:                 testRetries,
		PodCreateTimeout:            5 * time.Minute,
		SyncGatewayImage:            runtimeParams.SyncGatewayImage,
	}

	Global.Deployment, err = CreateDeploymentObject(runtimeParams.OperatorImage, 0, Global.PodCreateTimeout)
	if err != nil {
		return err
	}

	Global.LogDir, err = makeLogDir()
	if err != nil {
		return err
	}

	for _, kubeConf := range runtimeParams.KubeConfig {
		clusterSpec, cerr := createKubeClusterObject(kubeConf)
		if cerr != nil {
			return cerr
		}
		Global.ClusterSpec[kubeConf.ClusterName] = clusterSpec
	}

	// Set any defaults.
	if Global.SyncGatewayImage == "" {
		Global.SyncGatewayImage = "couchbase/sync-gateway:2.7.0-enterprise"
	}

	// Setting required spec values from test_config yaml
	e2espec.SetStorageClassName(runtimeParams.StorageClassName)
	e2espec.SetCouchbaseServerImage(runtimeParams.CouchbaseServerImage)
	e2espec.SetPlatform(runtimeParams.Platform)

	logrus.Info("Docker Registry")
	logrus.Info(" →  server: " + runtimeParams.DockerServer)
	logrus.Info(" →  username: " + runtimeParams.DockerUsername)
	logrus.Info(" →  password: " + strings.Repeat("*", len(runtimeParams.DockerPassword)))
	logrus.Info("Container Images")
	logrus.Info(" →  couchbase operator: " + runtimeParams.OperatorImage)
	logrus.Info(" →  couchbase admission controller: " + runtimeParams.AdmissionControllerImage)
	logrus.Info(" →  couchbase server: " + runtimeParams.CouchbaseServerImage)
	logrus.Info(" →  couchbase server upgrade: " + runtimeParams.CouchbaseServerImageUpgrade)
	logrus.Info(" →  couchbase sync gateway: " + Global.SyncGatewayImage)
	for _, config := range Global.ClusterSpec {
		logrus.Info("Cluster: ", config.Name)
		logrus.Info(" →  path: " + config.KubeConfPath)
		logrus.Info(" →  context: " + config.Context)
		logrus.Info(" →  namespace: " + config.Namespace)
	}
	logrus.Info("Kubernetes")
	logrus.Info(" →  storage class: " + runtimeParams.StorageClassName)
	logrus.Info("Logs")
	logrus.Info(" →  directory: " + Global.LogDir)

	// Setup the cbopinfo absolute path so it will not change if we move directories
	wd, oserr := os.Getwd()
	if oserr != nil {
		return oserr
	}
	Global.CbopinfoPath = wd + "/../../build/bin/cbopinfo"

	for _, k8s := range Global.ClusterSpec {
		if err = Global.SetupFramework(k8s); err != nil {
			return err
		}
	}
	return nil
}

func cleanUpNamespace() (err error) {
	for _, k8s := range Global.ClusterSpec {
		logrus.Infof("Cleaning up namespace %s in %s", k8s.Namespace, k8s.Name)
		// Remove secrets
		if k8s.DefaultSecret != nil {
			if err := e2eutil.DeleteSecret(k8s.KubeClient, k8s.Namespace, k8s.DefaultSecret.Name, &metav1.DeleteOptions{}); err != nil {
				if !k8sutil.IsKubernetesResourceNotFoundError(err) {
					return fmt.Errorf("unable to delete the default secret: %v", err)
				}
			}
		}

		// Clean-up Deployments and pods
		if err := DeleteOperatorCompletely(k8s.KubeClient, Global.Deployment.Name, k8s.Namespace); err != nil {
			return err
		}

		// Blow away any couchbase cluster resources (and friends)
		e2eutil.CleanK8Cluster(k8s, k8s.Namespace)

		// Clean up any LDAP Pods & Services
		if err := e2eutil.CleanLDAPResources(k8s.KubeClient, k8s.Namespace); err != nil {
			return err
		}
	}

	// TODO: check all deleted and wait
	Global = nil
	logrus.Info("Namespace cleaned-up successfully")
	return
}

func Teardown() error {
	if Global == nil {
		return fmt.Errorf("framework is uninitialized")
	}

	if Global.SkipTeardown {
		return nil
	}
	if err := cleanUpNamespace(); err != nil {
		return err
	}
	return nil
}

func createKubeClusterObject(c KubeConfData) (*types.Cluster, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: c.ClusterConfig},
		&clientcmd.ConfigOverrides{CurrentContext: c.Context},
	).ClientConfig()
	if err != nil {
		return nil, err
	}

	return &types.Cluster{
		Config:       config,
		CRClient:     client.MustNew(config),
		KubeClient:   kubernetes.NewForConfigOrDie(config),
		KubeConfPath: c.ClusterConfig,
		Context:      c.Context,
		Namespace:    c.Namespace,
		Name:         c.ClusterName,
	}, nil
}

func (f *Framework) CreateSecretInKubeCluster(k8s *types.Cluster) error {
	secret, err := e2eutil.CreateSecret(k8s.KubeClient, k8s.Namespace, e2espec.NewDefaultSecret(k8s.Namespace))
	if err != nil {
		err = fmt.Errorf("failed to create default couchbase secret: %v", err)
		return err
	}
	k8s.DefaultSecret = secret
	return err
}

func recreateCRDs(k8s *types.Cluster) error {
	clientSet, err := clientset.NewForConfig(k8s.Config)
	if err != nil {
		return fmt.Errorf("failed to create clientset object: %v", err)
	}

	crds, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list CRDs: %v", err)
	}
	for _, crd := range crds.Items {
		if crd.Spec.Group == "couchbase.com" {
			if err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crd.Name, metav1.NewDeleteOptions(0)); err != nil {
				return fmt.Errorf("failed to delete CRD: %v", err)
			}

			// wait for crd delete
			if err := e2eutil.WaitForCRDDeletion(clientSet, crd.Name, time.Minute); err != nil {
				return err
			}
		}
	}

	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetCouchbaseClusterCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetCouchbaseBucketCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetCouchbaseEphemeralBucketCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetCouchbaseMemcachedBucketCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetCouchbaseReplicationCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetUserCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetGroupCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetRoleBindingCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetCouchbaseBackupCRD()); err != nil {
		return err
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(validationv2.GetCouchbaseBackupRestoreCRD()); err != nil {
		return err
	}
	return nil
}

func (f *Framework) RemoveK8SNodeTaints(kubeClient kubernetes.Interface) error {
	logrus.Info("Marking all nodes as schedulable")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		nodeTaintList := []v1.Taint{}
		k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to get node list: %v", err)
		}
		for nodeIndex := range k8sNodeList.Items {
			if err := e2eutil.SetNodeTaintAndSchedulableProperty(kubeClient, false, nodeTaintList, nodeIndex); err != nil {
				return fmt.Errorf("failed to update node taint: %v", err)
			}
		}
		return nil
	})
}

func (f *Framework) SetupFramework(k8s *types.Cluster) error {
	if err := f.RemoveK8SNodeTaints(k8s.KubeClient); err != nil {
		return err
	}

	// Creating required namespaces and cluster roles before deploying the operator
	if err := createK8SNamespace(k8s.KubeClient, k8s.Namespace); err != nil {
		return err
	}

	logrus.Infof("Cleaning up namespace %s before deployment in %s", k8s.Namespace, k8s.Name)
	if err := DeleteOperatorCompletely(k8s.KubeClient, Global.Deployment.Name, k8s.Namespace); err != nil {
		return fmt.Errorf("failed to delete operator: %v", err)
	}

	logrus.Infof("Deleting admission controller")
	if err := deleteAdmissionController(k8s.KubeClient); err != nil {
		return err
	}

	e2eutil.CleanK8Cluster(k8s, k8s.Namespace)

	logrus.Info("Deleting orphaned pods")
	pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		if err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Delete(pod.Name, metav1.NewDeleteOptions(0)); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		logrus.Infof("Pod deleted: %v", pod.Name)
	}

	endpoints, err := k8s.KubeClient.CoreV1().Endpoints(k8s.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints.Items {
		if err := k8s.KubeClient.CoreV1().Endpoints(k8s.Namespace).Delete(endpoint.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
		logrus.Infof("Endpoint deleted: %v", endpoint.Name)
	}

	logrus.Info("Deleting secrets")
	if err := e2eutil.DeleteSecret(k8s.KubeClient, k8s.Namespace, "basic-test-secret", &metav1.DeleteOptions{}); err == nil {
		logrus.Infof("Secret deleted: %v", "basic-test-secret")
	}

	logrus.Info("Recreating CRD")
	if err := recreateCRDs(k8s); err != nil {
		return err
	}

	logrus.Info("Recreating docker auth secret")
	if err := recreateDockerAuthSecret(k8s); err != nil {
		return err
	}

	logrus.Info("Recreating role")
	if err := recreateRoles(k8s, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Recreating service account")
	if err := RecreateServiceAccount(k8s, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Recreating role binding")
	if err := recreateRoleBindings(k8s, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Creating secret")
	if err = f.CreateSecretInKubeCluster(k8s); err != nil {
		return err
	}

	logrus.Infof("Creating admission controller")
	if err := createAdmissionController(k8s.KubeClient); err != nil {
		return err
	}

	logrus.Info("Setting up operator")
	if err := f.SetupCouchbaseOperator(k8s); err != nil {
		return fmt.Errorf("failed to setup couchbase operator: %v", err)
	}
	logrus.Info("Couchbase operator created successfully")
	logrus.Info("E2E setup successfully")
	return nil
}

func (f *Framework) SetupCouchbaseOperator(k8s *types.Cluster) error {
	if _, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(f.Deployment); err != nil {
		return err
	}
	return e2eutil.WaitUntilOperatorReady(k8s.KubeClient, k8s.Namespace, constants.CouchbaseOperatorLabel)
}

func (f *Framework) GetOperatorRestartCount(kubeClient kubernetes.Interface, namespace string) (int32, error) {
	operatorPodName, err := e2eutil.GetOperatorName(kubeClient, namespace)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var operatorPod *v1.Pod
	err = retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
		operatorPod, err = kubeClient.CoreV1().Pods(namespace).Get(operatorPodName, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		return true, nil
	})
	if err != nil {
		return 0, err
	}
	return operatorPod.Status.ContainerStatuses[0].RestartCount, nil
}

func DeleteOperatorCompletely(kubeClient kubernetes.Interface, deploymentName, namespace string) error {
	if err := deleteOperator(kubeClient, deploymentName, namespace); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	return retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
		_, err := kubeClient.AppsV1().Deployments(namespace).Get(deploymentName, metav1.GetOptions{})
		if err == nil {
			return false, err
		}
		if k8sutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}
		return false, err
	})
}

func deleteOperator(kubeClient kubernetes.Interface, deploymentName, namespace string) error {
	deletePropagation := metav1.DeletePropagationForeground
	deleteOpts := metav1.NewDeleteOptions(0)
	deleteOpts.PropagationPolicy = &deletePropagation
	if err := kubeClient.AppsV1().Deployments(namespace).Delete(deploymentName, deleteOpts); err != nil {
		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

func (f *Framework) GetCluster(index int) *types.Cluster {
	return f.ClusterSpec[f.TestClusters[index]]
}

func makeLogDir() (string, error) {
	dir, err := generateLogDir()
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, os.ModePerm)
}

func generateLogDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	t := time.Now()
	ts := t.Format(time.RFC3339)
	return filepath.Join(cwd, "logs", ts), nil
}
