package framework

import (
	"bytes"
	"errors"
	"flag"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	pkg_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"

	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

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

// Starts timeout trigger based on given value in suiteData.Timeout
func StartTimeoutTimer() {
	go func() {
		// In case of SystemTests, timeout will be set as part of test case
		if suiteData.SuiteName == "TestSystem" {
			logrus.Info("Skipping setting timer")
		}
		timeoutDuration := GetDuration(suiteData.Timeout)
		logrus.Infof("Setting timeout of %v from %v", timeoutDuration, time.Now())
		<-time.After(timeoutDuration)
		logrus.Infof("Timeout happened at %v", time.Now())
		panic("Test timed out..")
	}()
}

// Setup setups a test framework and points "Global" to it.
func Setup(t *testing.T) error {
	clusterSpecMap := make(ClusterMap)

	err := v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		return err
	}

	err = api.AddToScheme(scheme.Scheme)
	if err != nil {
		return err
	}

	deploymentSpecContent, err := ioutil.ReadFile(runtimeParams.DeploymentSpec)
	if err != nil {
		return err
	}

	deserializer := scheme.Codecs.UniversalDeserializer()
	obj, _, err := deserializer.Decode([]byte(deploymentSpecContent), nil, nil)
	if err != nil {
		return err
	}

	deployment, ok := obj.(*v1beta1.Deployment)
	if !ok {
		errMsg := "File " + runtimeParams.DeploymentSpec + " does not define a deployment"
		return errors.New(errMsg)
	}

	// set operator image from env var
	oi := runtimeParams.OperatorImage
	if oi != "" {
		deployment.Spec.Template.Spec.Containers[0].Image = oi
	}

	// set ServiceAccountName if default is not being used for testing
	if runtimeParams.ServiceAccountName != "" {
		deployment.Spec.Template.Spec.ServiceAccountName = runtimeParams.ServiceAccountName
	}

	logDir, err := makeLogDir()
	if err != nil {
		return err
	}

	for _, kubeConf := range runtimeParams.KubeConfig {
		clusterSpec, err := CreateKubeClusterObject(kubeConf.ClusterConfig)
		if err != nil {
			return err
		}
		clusterSpecMap[kubeConf.ClusterName] = &clusterSpec
	}

	// Setting required spec values from test_config yaml
	e2espec.SetStorageClassName(runtimeParams.StorageClassName)
	e2espec.SetCbBaseImage(runtimeParams.CbServerBaseImage)
	e2espec.SetCbImageVersion(runtimeParams.CbServerImgVer)

	logrus.Info("Using couchbase-operator: " + deployment.Spec.Template.Spec.Containers[0].Image)
	logrus.Info("Using couchbase-server: " + e2espec.GetCouchbaseDockerImgName())
	logrus.Info("Using storage class: " + constants.StorageClassName)
	logrus.Info("Logs will be stored in: " + logDir)

	Global = &Framework{
		Deployment:      deployment,
		Namespace:       runtimeParams.Namespace,
		KubeType:        runtimeParams.KubeType,
		KubeVersion:     runtimeParams.KubeVersion,
		OpImage:         runtimeParams.OperatorImage,
		LogDir:          logDir,
		SkipTeardown:    runtimeParams.SkipTearDown,
		CollectLogs:     runtimeParams.CollectLogsOnFailure,
		ClusterSpec:     clusterSpecMap,
		SuiteYmlData:    suiteData,
		ClusterConfFile: runtimeParams.ClusterConfFile,
		PullDockerImage: runtimeParams.PullDockerImages,
	}
	for kubeName, _ := range Global.ClusterSpec {
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
		e2eutil.KillMembers(kubeClient, Global.Namespace, cluster.Name, killPods...)
	}
	return nil
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
				return errors.New("Unable to delete the default secret: " + err.Error())
			}
		}
		e2eutil.DeleteSecret(targetKube.KubeClient, Global.Namespace, "basic-test-secret", &metav1.DeleteOptions{})

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
	if Global.SkipTeardown {
		return nil
	}
	if err := cleanUpNamespace(); err != nil {
		return err
	}
	return nil
}

func CreateKubeClusterObject(kubeConfPath string) (Cluster, error) {
	clusterSpec := Cluster{}
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfPath)
	if err != nil {
		return clusterSpec, err
	}
	cli, err := kubernetes.NewForConfig(config)
	if err != nil {
		return clusterSpec, err
	}

	clusterSpec.Config = config
	clusterSpec.CRClient = client.MustNew(config)
	clusterSpec.KubeClient = cli
	return clusterSpec, err
}

func (f *Framework) CreateSecretInKubeCluster(kubeName string) error {
	secret, err := e2eutil.CreateSecret(f.ClusterSpec[kubeName].KubeClient, f.Namespace, e2espec.NewDefaultSecret(f.Namespace))
	if err != nil {
		err = errors.New("Failed to create default couchbase secret: " + err.Error())
		return err
	}
	f.ClusterSpec[kubeName].DefaultSecret = secret
	return err
}

func (f *Framework) DeleteCRDs(config *rest.Config) error {
	logrus.Info("Removing CRDs")
	clientSet, err := clientset.NewForConfig(config)
	if err != nil {
		return errors.New("Failed to create clientset object: " + err.Error())
	}
	if err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{}); err != nil {
		return errors.New("Failed to delete CRDs: " + err.Error())
	}
	return nil
}

func (f *Framework) RemoveK8SNodeTaints(kubeClient kubernetes.Interface) error {
	logrus.Info("Marking all nodes as schedulable")
	nodeTaintList := []v1.Taint{}
	for i := 0; i < constants.Retries5; i++ {
		k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil && i == 2 {
			return errors.New("Failed to get node list: " + err.Error())
		}
		for nodeIndex, _ := range k8sNodeList.Items {
			err := e2eutil.SetNodeTaintAndSchedulableProperty(kubeClient, false, nodeTaintList, nodeIndex)
			if err != nil && i == 2 {
				return errors.New("Failed to update node taint: " + err.Error())
			}
		}
	}
	return nil
}

func (f *Framework) SetupFramework(kubeName string) error {
	targetKube := f.ClusterSpec[kubeName]

	if err := f.RemoveK8SNodeTaints(targetKube.KubeClient); err != nil {
		return err
	}

	if err := f.DeleteCRDs(targetKube.Config); err != nil {
		return err
	}

	if err := RemoveLabels(pkg_constants.ServerGroupLabel, targetKube.KubeClient); err != nil {
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

	logrus.Info("Recreating cluster role")
	if err := RecreateClusterRoles(targetKube.KubeClient, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Recreating service account")
	if err := RecreateServiceAccount(targetKube.KubeClient, f.Namespace, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Recreating cluster role binding")
	if err := RecreateClusterRoleBindings(targetKube.KubeClient, f.Namespace, f.Deployment.Spec.Template.Spec.ServiceAccountName); err != nil {
		return err
	}
	logrus.Info("Creating secret")
	if err = f.CreateSecretInKubeCluster(kubeName); err != nil {
		return err
	}

	if Global.PullDockerImage {
		dockerImgList := []string{f.OpImage, e2espec.GetCouchbaseDockerImgName()}
		if err = f.PullDockerImages(targetKube.KubeClient, kubeName, dockerImgList); err != nil {
			return err
		}
	}

	logrus.Info("Setting up operator")
	if err := f.SetupCouchbaseOperator(f.ClusterSpec[kubeName]); err != nil {
		return errors.New("Failed to setup couchbase operator: " + err.Error())
	}
	logrus.Info("Couchbase operator created successfully")
	logrus.Info("E2E setup successfully")
	return nil
}

func (f *Framework) PullDockerImages(kubeClient kubernetes.Interface, kubeName string, dockerImages []string) error {
	yamlFilePath := "./resources/ansible"
	pullDockerImageFile := yamlFilePath + "/generic/pullDockerImage.yaml"
	inventoryFile := yamlFilePath + "DockerHosts"

	logrus.Infof("Pulling docker images for %s", kubeName)
	k8sNodes, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list K8S nodes for cluster " + kubeName + ": " + err.Error())
	}
	ansibleIpList := []string{}
	for _, node := range k8sNodes.Items {
		ansibleIpList = append(ansibleIpList, node.Status.Addresses[0].Address)
	}

	if err := createAnsibleHostFileFromHosts(inventoryFile, ansibleIpList); err != nil {
		return err
	}

	ansibleExtraVarParam := "dockerImgName="
	for _, imgName := range dockerImages {
		ansibleExtraVarParam += imgName + ","
	}
	ansibleExtraVarParam = strings.TrimRight(ansibleExtraVarParam, ",")
	ansibleCmd := exec.Command("ansible-playbook", "-i", inventoryFile, pullDockerImageFile, "-c", "paramiko", "--extra-vars", ansibleExtraVarParam)
	var stdout bytes.Buffer
	ansibleCmd.Stdout = &stdout
	if err := ansibleCmd.Run(); err != nil {
		logrus.Info(stdout.String())
		return errors.New("Error during ansible execution: " + err.Error())
	}
	return nil
}

func (f *Framework) SetupCouchbaseOperator(targetKube *Cluster) error {
	if _, err := targetKube.KubeClient.ExtensionsV1beta1().Deployments(f.Namespace).Create(f.Deployment); err != nil {
		return err
	}
	return e2eutil.WaitUntilOperatorReady(targetKube.KubeClient, f.Namespace, constants.CouchbaseOperatorLabel)
}

func (f *Framework) GetOperatorRestartCount(kubeClient kubernetes.Interface, namespace string) int32 {
	operatorPodName, err := e2eutil.GetOperatorName(kubeClient, namespace)
	if err != nil {
		return -1
	}
	operatorPod, err := kubeClient.CoreV1().Pods(namespace).Get(operatorPodName, metav1.GetOptions{})
	if err != nil {
		return -1
	}
	return operatorPod.Status.ContainerStatuses[0].RestartCount
}

func DeleteOperatorCompletely(kubeClient kubernetes.Interface, deploymentName, namespace string) error {
	if err := deleteOperator(kubeClient, deploymentName, namespace); err != nil {
		return err
	}
	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	err := retryutil.Retry(e2eutil.Context, 5*time.Second, 24, func() (bool, error) {
		_, err := kubeClient.ExtensionsV1beta1().Deployments(namespace).Get(deploymentName, metav1.GetOptions{})
		if err == nil {
			return false, err
		}
		if k8sutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		return errors.New("fail to wait couchbase operator pod gone from API: " + err.Error())
	}
	return nil
}

func deleteOperator(kubeClient kubernetes.Interface, deploymentName, namespace string) error {
	deletePropagation := metav1.DeletePropagationForeground
	deleteOpts := metav1.NewDeleteOptions(0)
	deleteOpts.PropagationPolicy = &deletePropagation
	return kubeClient.ExtensionsV1beta1().Deployments(namespace).Delete(deploymentName, deleteOpts)
}

func (f *Framework) ApiServerHost(kubeName string) string {
	return f.ClusterSpec[kubeName].Config.Host
}

func (f *Framework) GetHostNameFromUrl(hostUrl string) (string, error) {
	u, err := url.Parse(hostUrl)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}

func (f *Framework) GetKubeHostname(kubeName string) (string, error) {
	targetHost := f.ClusterSpec[kubeName].Config.Host
	return f.GetHostNameFromUrl(targetHost)
}

func (f *Framework) PodClient(kubeName string) typedv1.PodInterface {
	return f.ClusterSpec[kubeName].KubeClient.CoreV1().Pods(f.Namespace)
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
func runExecCommand(t *testing.T, command *exec.Cmd) error {
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		return errors.New("Error during execution: " + err.Error() + "\n\n stdout: " + stdout.String() + "\n\n stderr: " + stderr.String())
	}
	return nil
}

func runExecCmd(command *exec.Cmd) error {
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		return errors.New("Error during execution: " + err.Error() + "\n\n stdout: " + stdout.String() + "\n\n stderr: " + stderr.String())
	}
	return nil
}
