package framework

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
)

// Init performs one time only initialization of the framework.  Dynamic calls to these
// functions will result in race conditions and spurious failures.
func Init() {
	// Register CouchbaseCluster and CustomResourceDefinition types with the main library.
	if err := v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := api.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := apiextensionsv1beta1.SchemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Start the timeout timer.
	startTimeoutTimer()
}

func ReadYamlData() (err error) {
	testConfigFilePath := flag.String("testconfig", "resources/test_config.yaml", "test_config.yaml path. eg: $HOME/test_config.yaml")
	flag.Parse()

	logrus.Info("Using test_config file ", *testConfigFilePath)
	runtimeParams, err = ReadRuntimeConfig(*testConfigFilePath)
	if err != nil {
		return err
	}

	suiteFilePath := "./resources/suites/" + runtimeParams.SuiteToRun + ".yaml"

	logrus.Info("Using suite file ", suiteFilePath)
	suiteData, err = GetSuiteDataFromYml(suiteFilePath)
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

func CreateDeploymentObject(operatorImage string, operatorPort int) (deployment *appsv1.Deployment, err error) {
	deployment = config.GetOperatorDeployment(operatorImage, dockerPullSecretName)

	// Manually set the HTTP port.
	if operatorPort != 0 {
		listerAddrArg := "--listen-addr=0.0.0.0:" + strconv.Itoa(operatorPort)
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, listerAddrArg)
		deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Port = intstr.FromInt(operatorPort)
	}
	return
}

// Setup setups a test framework and points "Global" to it.
func Setup(t *testing.T) (err error) {

	// Initialize Global from runtime info
	Global = &Framework{
		Namespace:                     runtimeParams.Namespace,
		KubeType:                      runtimeParams.KubeType,
		KubeVersion:                   runtimeParams.KubeVersion,
		OpImage:                       runtimeParams.OperatorImage,
		SkipTeardown:                  runtimeParams.SkipTearDown,
		CollectLogs:                   runtimeParams.CollectLogsOnFailure,
		SuiteYmlData:                  suiteData,
		ClusterConfFile:               runtimeParams.ClusterConfFile,
		PlatformType:                  runtimeParams.PlatformType,
		ClusterSpec:                   types.ClusterMap{},
		CouchbaseServerVersion:        runtimeParams.CbServerImgVer,
		CouchbaseServerUpgradeVersion: runtimeParams.CbServerImgVerUpgrade,
		StorageClassName:              runtimeParams.StorageClassName,
	}

	Global.Deployment, err = CreateDeploymentObject(runtimeParams.OperatorImage, 0)
	if err != nil {
		return err
	}

	Global.LogDir, err = makeLogDir()
	if err != nil {
		return err
	}

	for _, kubeConf := range runtimeParams.KubeConfig {
		clusterSpec, cerr := CreateKubeClusterObject(kubeConf.ClusterConfig, kubeConf.Context)
		if cerr != nil {
			return cerr
		}
		Global.ClusterSpec[kubeConf.ClusterName] = clusterSpec
	}

	// Setting required spec values from test_config yaml
	e2espec.SetStorageClassName(runtimeParams.StorageClassName)
	e2espec.SetCbBaseImage(runtimeParams.CbServerBaseImage)
	e2espec.SetCbImageVersion(runtimeParams.CbServerImgVer)

	logrus.Info("Docker Registry")
	logrus.Info(" →  server: " + runtimeParams.DockerServer)
	logrus.Info(" →  username: " + runtimeParams.DockerUsername)
	logrus.Info(" →  password: " + strings.Repeat("*", len(runtimeParams.DockerPassword)))
	logrus.Info("Container Images")
	logrus.Info(" →  couchbase operator: " + runtimeParams.OperatorImage)
	logrus.Info(" →  couchbase admission controller: " + runtimeParams.AdmissionControllerImage)
	logrus.Info(" →  couchbase server: " + e2espec.GetCouchbaseDockerImgName())
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

	for kubeName := range Global.ClusterSpec {
		if err = Global.SetupFramework(kubeName); err != nil {
			return err
		}
	}
	return nil
}

func DeleteAllJobs(kubeClient kubernetes.Interface) error {
	logrus.Info("Deleting jobs")
	jobs, err := kubeClient.BatchV1().Jobs(Global.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list jobs: " + err.Error())
	}
	for _, job := range jobs.Items {
		if err := kubeClient.BatchV1().Jobs(Global.Namespace).Delete(job.Name, metav1.NewDeleteOptions(0)); err != nil {
			return errors.New("Failed to delete job: " + err.Error())
		}
		logrus.Infof("Job deleted: %s", job.Name)
	}
	return nil
}

func DeleteCouchbaseServices(kubeClient kubernetes.Interface) error {
	logrus.Info("Deleting Couchbase services")
	services, err := kubeClient.CoreV1().Services(Global.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return errors.New("Failed to list services: " + err.Error())
	}
	for _, service := range services.Items {
		if err := kubeClient.CoreV1().Services(Global.Namespace).Delete(service.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
		logrus.Infof("Service deleted: %s", service.Name)
	}
	return nil
}

func DeleteCouchbaseClusters(kubeClient kubernetes.Interface, crClient versioned.Interface) error {
	logrus.Info("Deleting Couchbase clusters")
	clusters, err := crClient.CouchbaseV1().CouchbaseClusters(Global.Namespace).List(metav1.ListOptions{})
	if err != nil && !k8sutil.IsKubernetesResourceNotFoundError(err) {
		return err
	}
	for _, cluster := range clusters.Items {
		if err := crClient.CouchbaseV1().CouchbaseClusters(Global.Namespace).Delete(cluster.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
		logrus.Infof("Deleted Couchbase cluster: %s", cluster.Name)
		pods, err := kubeClient.CoreV1().Pods(Global.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + cluster.Name})
		if err != nil {
			return errors.New("Failed to list pods for cluster: " + err.Error())
		}
		killPods := []string{}
		for _, pod := range pods.Items {
			killPods = append(killPods, pod.Name)
		}
		if err := e2eutil.KillMembers(kubeClient, Global.Namespace, cluster.Name, killPods...); err != nil {
			return err
		}
	}
	return nil
}

func CreateAdmissionController(client kubernetes.Interface) error {
	logrus.Infof("Creating admission controller")
	if err := createAdmissionController(client); err != nil {
		return err
	}
	if err := waitAdmissionController(client); err != nil {
		return err
	}
	return nil
}

func DeleteAdmissionController(client kubernetes.Interface) error {
	logrus.Info("Deleting admission controller")
	return deleteAdmissionController(client)
}

func cleanUpNamespace() (err error) {
	logrus.Infof("Cleaning up namespace: %s", Global.Namespace)
	for _, targetKube := range Global.ClusterSpec {
		// Clean-up Jobs
		if err := DeleteAllJobs(targetKube.KubeClient); err != nil {
			return err
		}

		// Remove secrets
		if targetKube.DefaultSecret != nil {
			if err := e2eutil.DeleteSecret(targetKube.KubeClient, Global.Namespace, targetKube.DefaultSecret.Name, &metav1.DeleteOptions{}); err != nil {
				if !k8sutil.IsKubernetesResourceNotFoundError(err) {
					return errors.New("Unable to delete the default secret: " + err.Error())
				}
			}
		}
		if err := e2eutil.DeleteSecret(targetKube.KubeClient, Global.Namespace, "basic-test-secret", &metav1.DeleteOptions{}); err != nil {
			return err
		}

		// Clean-up Deployments and pods
		if err := DeleteOperatorCompletely(targetKube.KubeClient, Global.Deployment.Name, Global.Namespace); err != nil {
			return err
		}

		// Clear all couchbase clusters
		if err := DeleteCouchbaseClusters(targetKube.KubeClient, targetKube.CRClient); err != nil {
			return err
		}

		// Clean-up Couchbase services
		if err := DeleteCouchbaseServices(targetKube.KubeClient); err != nil {
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
		return errors.New("framework is uninitialized")
	}

	if Global.SkipTeardown {
		return nil
	}
	if err := cleanUpNamespace(); err != nil {
		return err
	}
	return nil
}

func CreateKubeClusterObject(kubeConfPath, context string) (*types.Cluster, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfPath},
		&clientcmd.ConfigOverrides{CurrentContext: context},
	).ClientConfig()
	if err != nil {
		return nil, err
	}

	return &types.Cluster{
		Config:       config,
		CRClient:     client.MustNew(config),
		KubeClient:   kubernetes.NewForConfigOrDie(config),
		KubeConfPath: kubeConfPath,
		Context:      context,
	}, nil
}

func (f *Framework) CreateSecretInKubeCluster(kubeName string) error {
	secret, err := e2eutil.CreateSecret(f.ClusterSpec[kubeName].KubeClient, f.Namespace, e2espec.NewDefaultSecret(f.Namespace))
	if err != nil {
		err = errors.New("failed to create default couchbase secret: " + err.Error())
		return err
	}
	f.ClusterSpec[kubeName].DefaultSecret = secret
	return err
}

func RecreateCRDs(k8s *types.Cluster) error {
	clientSet, err := clientset.NewForConfig(k8s.Config)
	if err != nil {
		return errors.New("failed to create clientset object: " + err.Error())
	}
	if err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{}); err != nil {
		return errors.New("failed to delete CRDs: " + err.Error())
	}
	if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(k8sutil.GetCRD()); err != nil {
		return err
	}
	return nil
}

func (f *Framework) RemoveK8SNodeTaints(kubeClient kubernetes.Interface) error {
	logrus.Info("Marking all nodes as schedulable")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, e2eutil.IntMax, "", "", func() error {
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

func (f *Framework) SetupFramework(kubeName string) error {
	targetKube := f.ClusterSpec[kubeName]

	if err := f.RemoveK8SNodeTaints(targetKube.KubeClient); err != nil {
		return err
	}

	// Creating required namespaces and cluster roles before deploying the operator
	if err := CreateK8SNamespace(targetKube.KubeClient, f.Namespace); err != nil {
		return err
	}

	logrus.Infof("Cleaning up namespace %s before deployment in %s", f.Namespace, kubeName)
	if err := DeleteOperatorCompletely(targetKube.KubeClient, Global.Deployment.Name, f.Namespace); err == nil {
		logrus.Infof("Deployment deleted: %v", Global.Deployment.Name)
	}

	if err := DeleteAdmissionController(targetKube.KubeClient); err != nil {
		return err
	}

	if err := DeleteCouchbaseClusters(targetKube.KubeClient, targetKube.CRClient); err != nil {
		return err
	}

	if err := DeleteAllJobs(targetKube.KubeClient); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)

	if err := DeleteCouchbaseServices(targetKube.KubeClient); err != nil {
		return err
	}

	logrus.Info("Deleting orphaned pods")
	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		err = targetKube.KubeClient.CoreV1().Pods(f.Namespace).Delete(pod.Name, metav1.NewDeleteOptions(0))
		if err != nil {
			return err
		}
		logrus.Infof("Pod deleted: %v", pod.Name)
	}

	endpoints, err := targetKube.KubeClient.CoreV1().Endpoints(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints.Items {
		if err := targetKube.KubeClient.CoreV1().Endpoints(f.Namespace).Delete(endpoint.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
		logrus.Infof("Endpoint deleted: %v", endpoint.Name)
	}

	logrus.Info("Deleting secrets")
	if err := e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, "basic-test-secret", &metav1.DeleteOptions{}); err == nil {
		logrus.Infof("Secret deleted: %v", "basic-test-secret")
	}

	logrus.Info("Recreating CRD")
	if err := RecreateCRDs(targetKube); err != nil {
		return err
	}

	logrus.Info("Recreating docker auth secret")
	if err := RecreateDockerAuthSecret(targetKube.KubeClient); err != nil {
		return err
	}

	logrus.Info("Recreating role")
	if err := RecreateRoles(targetKube.KubeClient, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Recreating service account")
	if err := RecreateServiceAccount(targetKube.KubeClient, f.Namespace, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Recreating role binding")
	if err := RecreateRoleBindings(targetKube.KubeClient, f.Namespace, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Creating secret")
	if err = f.CreateSecretInKubeCluster(kubeName); err != nil {
		return err
	}

	if err := CreateAdmissionController(targetKube.KubeClient); err != nil {
		return err
	}

	logrus.Info("Setting up operator")
	if err := f.SetupCouchbaseOperator(f.ClusterSpec[kubeName]); err != nil {
		return errors.New("failed to setup couchbase operator: " + err.Error())
	}
	logrus.Info("Couchbase operator created successfully")
	logrus.Info("E2E setup successfully")
	return nil
}

func (f *Framework) SetupCouchbaseOperator(targetKube *types.Cluster) error {
	if _, err := targetKube.KubeClient.AppsV1().Deployments(f.Namespace).Create(f.Deployment); err != nil {
		return err
	}
	return e2eutil.WaitUntilOperatorReady(targetKube.KubeClient, f.Namespace, constants.CouchbaseOperatorLabel)
}

func (f *Framework) GetOperatorRestartCount(kubeClient kubernetes.Interface, namespace string) (int32, error) {
	operatorPodName, err := e2eutil.GetOperatorName(kubeClient, namespace)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var operatorPod *v1.Pod
	err = retryutil.Retry(ctx, 5*time.Second, e2eutil.IntMax, func() (bool, error) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	return retryutil.Retry(ctx, 5*time.Second, e2eutil.IntMax, func() (bool, error) {
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

// Execute shell command and returns the stderr and stdout buffers
func runExecCommand(command *exec.Cmd) error {
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		return errors.New("error during execution: " + err.Error() + "\n\n stdout: " + stdout.String() + "\n\n stderr: " + stderr.String())
	}
	return nil
}
