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
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
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

type Cluster struct {
	KubeClient    kubernetes.Interface
	CRClient      versioned.Interface
	DefaultSecret *v1.Secret
	Config        *rest.Config
}

type ClusterMap map[string]*Cluster

type Framework struct {
	opImage         string
	Deployment      *v1beta1.Deployment
	Namespace       string
	KubeType        string
	KubeVersion     string
	ClusterSpec     ClusterMap
	LogDir          string
	SkipTeardown    bool
	Duration        int
	SuiteYmlData    SuiteData
	ClusterConfFile string
	PullDockerImage bool
	//S3Cli         *s3.S3
	//S3Bucket      string
}

var Global *Framework
var runtimeParams TestRunParam
var suiteData SuiteData

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

	duration, err := strconv.Atoi(runtimeParams.TestDuration)
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
	logrus.Info("Using storage class: " + e2espec.StorageClassName)
	logrus.Info("Logs will be stored in: " + logDir)

	Global = &Framework{
		Deployment:      deployment,
		Namespace:       runtimeParams.Namespace,
		KubeType:        runtimeParams.KubeType,
		KubeVersion:     runtimeParams.KubeVersion,
		opImage:         runtimeParams.OperatorImage,
		LogDir:          logDir,
		SkipTeardown:    runtimeParams.SkipTearDown,
		Duration:        duration,
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

func cleanUpNamespace() (err error) {
	logrus.Info("Cleaning up namespace")
	for _, targetKube := range Global.ClusterSpec {
		// Clean-up Jobs and Secrets
		jobs, err := targetKube.KubeClient.BatchV1().Jobs(Global.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return errors.New("Failed to list jobs: " + err.Error())
		}
		for _, job := range jobs.Items {
			err = targetKube.KubeClient.BatchV1().Jobs(Global.Namespace).Delete(job.Name, metav1.NewDeleteOptions(0))
			if err != nil {
				return errors.New("Failed to delete job: " + err.Error())
			}
		}
		if targetKube.DefaultSecret != nil {
			err = e2eutil.DeleteSecret(targetKube.KubeClient, Global.Namespace, targetKube.DefaultSecret.Name, &metav1.DeleteOptions{})
			if err != nil {
				return errors.New("Unable to delete the default secret: " + err.Error())
			}
		}
		e2eutil.DeleteSecret(targetKube.KubeClient, Global.Namespace, "basic-test-secret", &metav1.DeleteOptions{})

		// Clean-up Deployments and pods

		deployments, err := targetKube.KubeClient.ExtensionsV1beta1().Deployments(Global.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return errors.New("Failed to list deployments: " + err.Error())
		}
		for _, deployment := range deployments.Items {
			DeleteOperatorCompletely(targetKube.KubeClient, deployment.GetName(), Global.Namespace)
		}

		// Clear couchbase pods
		clusters, err := targetKube.CRClient.CouchbaseV1().CouchbaseClusters(Global.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return errors.New("Failed to list clusters: " + err.Error())
		}
		for _, cluster := range clusters.Items {
			targetKube.CRClient.CouchbaseV1().CouchbaseClusters(Global.Namespace).Delete(cluster.Name, metav1.NewDeleteOptions(0))
			pods, err := targetKube.KubeClient.CoreV1().Pods(Global.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase,couchbase_cluster=" + cluster.Name})
			if err != nil {
				return errors.New("Failed to list pods for cluster: " + err.Error())
			}
			killPods := []string{}
			for _, pod := range pods.Items {
				killPods = append(killPods, pod.Name)
			}
			e2eutil.KillMembers(targetKube.KubeClient, Global.Namespace, cluster.Name, killPods...)
		}

		// Clean-up Couchbase services
		services, err := targetKube.KubeClient.CoreV1().Services(Global.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
		if err != nil {
			return errors.New("Failed to list services: " + err.Error())
		}
		for _, service := range services.Items {
			targetKube.KubeClient.CoreV1().Services(Global.Namespace).Delete(service.Name, metav1.NewDeleteOptions(0))
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
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to get node list: " + err.Error())
	}
	for nodeIndex, _ := range k8sNodeList.Items {
		if err := e2eutil.SetNodeTaintAndSchedulableProperty(kubeClient, false, nodeTaintList, nodeIndex); err != nil {
			return errors.New("Failed to update node taint: " + err.Error())
		}
	}
	return nil
}

func (f *Framework) SetupFramework(kubeName string) error {
	targetKube := f.ClusterSpec[kubeName]
	kubeConfigPath := GetKubeConfigToUse(kubeName)

	if err := f.RemoveK8SNodeTaints(targetKube.KubeClient); err != nil {
		return nil
	}

	if err := f.DeleteCRDs(targetKube.Config); err != nil {
		return err
	}

	logrus.Info("Creating Portworx service")
	if err := RecreateServicePortworx(targetKube.KubeClient); err != nil {
		return err
	}

	deletePortworxCmd := exec.Command("bash", "./resources/thirdparty/portworx/delete-portworx-automation.sh", kubeConfigPath)
	runExecCmd(deletePortworxCmd)

	logrus.Info("deleting etcd")
	deleteEtcdCmd := exec.Command("bash", "./resources/thirdparty/etcd/delete-etcd-automation.sh", kubeConfigPath)
	runExecCmd(deleteEtcdCmd)

	logrus.Info("Cleaning up namespace before deployment for " + kubeName)

	logrus.Info("Deleting deployments")
	deployments, err := targetKube.KubeClient.ExtensionsV1beta1().Deployments(f.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to list deployments: " + err.Error())
	}
	for _, deployment := range deployments.Items {
		err = DeleteOperatorCompletely(targetKube.KubeClient, deployment.GetName(), f.Namespace)
		if err != nil {
			return err
		}
		logrus.Infof("deployment deleted: %v", deployment.GetName())
	}

	logrus.Info("Deleting clusters")
	clusters, _ := targetKube.CRClient.CouchbaseV1().CouchbaseClusters(f.Namespace).List(metav1.ListOptions{})
	for _, cluster := range clusters.Items {
		err = targetKube.CRClient.CouchbaseV1().CouchbaseClusters(f.Namespace).Delete(cluster.Name, metav1.NewDeleteOptions(0))
		if err != nil {
			return err
		}
		logrus.Infof("cluster deleted: %v", cluster.Name)
		pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase,couchbase_cluster=" + cluster.Name})
		if err != nil {
			return errors.New("failed to list pods for cluster: " + err.Error())
		}
		killPods := []string{}
		for _, pod := range pods.Items {
			killPods = append(killPods, pod.Name)
		}
		e2eutil.KillMembers(targetKube.KubeClient, Global.Namespace, cluster.Name, killPods...)
	}

	logrus.Info("Deleting jobs")
	jobs, err := targetKube.KubeClient.BatchV1().Jobs(f.Namespace).List(metav1.ListOptions{})
	for _, job := range jobs.Items {
		err = targetKube.KubeClient.BatchV1().Jobs(f.Namespace).Delete(job.Name, metav1.NewDeleteOptions(0))
		if err != nil {
			return err
		}
		logrus.Infof("jobs deleted: %v", job.Name)
	}
	time.Sleep(2 * time.Second)

	logrus.Info("Deleting services")
	services, err := targetKube.KubeClient.CoreV1().Services(f.Namespace).List(metav1.ListOptions{LabelSelector: e2eutil.CouchbaseLabel})
	for _, service := range services.Items {
		if err := targetKube.KubeClient.CoreV1().Services(f.Namespace).Delete(service.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
		logrus.Infof("service deleted: %v", service.Name)
	}

	logrus.Info("Deleting orphaned pods")
	pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: e2eutil.CouchbaseLabel})
	for _, pod := range pods.Items {
		err = targetKube.KubeClient.CoreV1().Pods(f.Namespace).Delete(pod.Name, metav1.NewDeleteOptions(0))
		if err != nil {
			return err
		}
		logrus.Infof("pod deleted: %v", pod.Name)
	}

	endpoints, err := targetKube.KubeClient.CoreV1().Endpoints(f.Namespace).List(metav1.ListOptions{LabelSelector: e2eutil.CouchbaseLabel})
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints.Items {
		err = targetKube.KubeClient.CoreV1().Endpoints(f.Namespace).Delete(endpoint.Name, metav1.NewDeleteOptions(0))
		if err != nil {
			return err
		}
		logrus.Infof("endpoint deleted: %v", endpoint.Name)
	}

	logrus.Info("Deleting secrets")
	if err := e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, "basic-test-secret", &metav1.DeleteOptions{}); err == nil {
		logrus.Infof("secret deleted: %v", "basic-test-secret")
	}

	// Creating required namespaces and cluster roles before deploying the operator
	logrus.Info("Creating namespace")
	if err := CreateK8SNamespace(targetKube.KubeClient, f.Namespace); err != nil {
		return err
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
		dockerImgList := []string{f.opImage, e2espec.GetCouchbaseDockerImgName()}
		if err = f.PullDockerImages(targetKube.KubeClient, kubeName, dockerImgList); err != nil {
			return err
		}
	}

	logrus.Info("Setting up operator..")
	if err := f.SetupCouchbaseOperator(f.ClusterSpec[kubeName]); err != nil {
		return errors.New("Failed to setup couchbase operator: " + err.Error())
	}
	logrus.Info("couchbase operator created successfully")
	logrus.Info("e2e setup successfully")
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
	ansibleCmd := exec.Command("ansible-playbook", "-i", inventoryFile, pullDockerImageFile, "--extra-vars", ansibleExtraVarParam)
	var stdout bytes.Buffer
	ansibleCmd.Stdout = &stdout
	if err := ansibleCmd.Run(); err != nil {
		logrus.Info(stdout.String())
		return errors.New("Error during ansible execution: " + err.Error())
	}
	return nil
}

func (f *Framework) SetupCouchbaseOperator(targetKube *Cluster) error {
	_, err := targetKube.KubeClient.ExtensionsV1beta1().Deployments(f.Namespace).Create(f.Deployment)
	if err != nil {
		return err
	}

	return e2eutil.WaitUntilOperatorReady(targetKube.KubeClient, f.Namespace, e2eutil.CouchbaseOperatorLabel)
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

func DeleteOperatorCompletely(kubeClient kubernetes.Interface, deploymentName string, namespace string) error {
	err := deleteOperator(kubeClient, deploymentName, namespace)
	if err != nil {
		return err
	}
	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	err = retryutil.Retry(e2eutil.Context, 5*time.Second, 24, func() (bool, error) {
		_, err = kubeClient.ExtensionsV1beta1().Deployments(namespace).Get(deploymentName, metav1.GetOptions{})
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

func deleteOperator(kubeClient kubernetes.Interface, deploymentName string, namespace string) error {
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
