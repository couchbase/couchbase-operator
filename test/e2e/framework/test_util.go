package framework

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os/exec"
	"strconv"
	"testing"

	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestFunc defines the test function type
type TestFunc func(*testing.T)

// TestDecorator decorates a test function.  This is used to augment an
// existing test usually to perform setup and tear-down tasks e.g.
// initializing and deleting a cluster or applying TLS configuration
type TestDecorator func(TestFunc) TestFunc

// TestSuite defines a suite of tests
type TestSuite map[string]TestFunc
type TestSuiteDecorator map[string]TestDecorator

// Map to store Testcase name to their respective Function objects
type FuncMap map[string]func(*testing.T)
type DecoratorMap map[string]TestDecorator

// TestResult simply maps a test name to a pass/fail flag
type TestResult struct {
	Name   string
	Result bool
}

// Variable to store the results globally
var Results = []TestResult{}

// Runtime configuration
type KubeConfData struct {
	ClusterName   string `yaml:"name"`
	ClusterConfig string `yaml:"config"`
}

type TestRunParam struct {
	Namespace          string         `yaml:"namespace"`
	OperatorImage      string         `yaml:"operator-image"`
	SuiteToRun         string         `yaml:"suite"`
	DeploymentSpec     string         `yaml:"deployment-spec"`
	KubeType           string         `yaml:"kube-type"`
	KubeVersion        string         `yaml:"kube-version"`
	ServiceAccountName string         `yaml:"serviceAccountName"`
	KubeConfig         []KubeConfData `yaml:"kube-config"`
	TestDuration       string         `yaml:"duration"`
	SkipTearDown       bool           `yaml:"skip-tear-down"`
	ClusterConfFile    string         `yaml:"cluster-config"`
}

// To decode cluster yaml file
type ClusterInfo struct {
	ClusterName    string `yaml:"name"`
	MasterNodeList []struct {
		Ip        string `yaml:"ip"`
		NodeLabel string `yaml:"label"`
	} `yaml:"master"`
	WorkerNodeList []struct {
		Ip        string `yaml:"ip"`
		NodeLabel string `yaml:"label"`
	} `yaml:"worker"`
}

type ClusterConfig struct {
	ClusterInfo []struct {
		Type        string        `yaml:"type"`
		ClusterList []ClusterInfo `yaml:"clusters"`
	} `yaml:"types"`
}

// To decode test-suite yaml file
type SuiteData struct {
	SuiteName     string `yaml:"suite"`
	TestCaseGroup []struct {
		GroupName   string   `yaml:"name"`
		ClusterName []string `yaml:"clusters"`
		TestCase    []struct {
			TcName     string   `yaml:"name"`
			Decorators []string `yaml:"decorators"`
		} `yaml:"testcases"`
	} `yaml:"tcGroups"`
}

// analyzeResults accepts a list of test results and displays success rates
func AnalyzeResults(t *testing.T) {
	t.Logf("Suite Test Results: \n")

	failures := []string{}
	for i, result := range Results {
		if result.Result {
			t.Logf("%d: %s...PASS", i+1, result.Name)
		}
		if !result.Result {
			t.Logf("%d: %s...FAIL", i+1, result.Name)
			failures = append(failures, result.Name)

		}
	}

	pass := float64(len(Results) - len(failures))
	fail := float64(len(failures))
	total := float64(len(Results))
	passRate := float64((pass / total) * 100.0)

	if fail > 0 {
		t.Logf("Failures: ")
		for i, test := range failures {
			t.Logf("%d: %s", i+1, test)
		}
	}

	t.Logf("\n Pass: %f \n Fail: %f \n Pass Rate: %f", pass, fail, passRate)
	if fail > 0 {
		t.Fatalf("suite contains failures")
	}
}

func ElementExistsInArr(itemToSearch string, itemList []string) bool {
	for _, item := range itemList {
		if item == itemToSearch {
			return true
		}
	}
	return false
}

// Read Test run params from test_config yaml file
func ReadRuntimeConfig(ymlFilePath string) (runTimeConfig TestRunParam, err error) {
	ymlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = errors.New("Unable to read test config file:" + err.Error())
		return
	}

	err = yaml.Unmarshal(ymlFileContent, &runTimeConfig)
	if err != nil {
		err = errors.New("Unable to decode test config:" + err.Error())
		return
	}
	return
}

// Function to read cluster specific data
func GetClusterConfigFromYml(ymlFilePath, reqClusterType string, reqClusters []string) (clusters []ClusterInfo, err error) {
	var clusterConf ClusterConfig

	yamlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = errors.New("Unable to read cluster config file: " + err.Error())
		return
	}

	err = yaml.Unmarshal(yamlFileContent, &clusterConf)
	if err != nil {
		err = errors.New("Unable to decode cluster config: " + err.Error())
		return
	}

	for _, currClusterType := range clusterConf.ClusterInfo {
		if currClusterType.Type == reqClusterType {
			for _, currCluster := range currClusterType.ClusterList {
				if ElementExistsInArr(currCluster.ClusterName, reqClusters) {
					clusters = append(clusters, currCluster)
				}
			}
		}
	}
	if len(reqClusters) != len(clusters) {
		err = errors.New("Unable to get cluster config for all required clusters")
	}
	return
}

func GetKubeClusterForKubeName(targetKubeName string) (kubeClusterData ClusterInfo, err error) {
	kubeClustersToSetup, err := GetClusterConfigFromYml(Global.ClusterConfFile, Global.KubeType, []string{targetKubeName})
	if err != nil {
		return
	}
	for _, kubeCluster := range kubeClustersToSetup {
		if kubeCluster.ClusterName == targetKubeName {
			kubeClusterData = kubeCluster
			return
		}
	}
	return
}

// Function to read Suite and required cluster info from suite.yaml file
func GetSuiteDataFromYml(ymlFilePath string) (suiteData SuiteData, err error) {
	yamlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = errors.New("Unable to read suite config file: " + err.Error())
		return
	}

	err = yaml.Unmarshal(yamlFileContent, &suiteData)
	if err != nil {
		err = errors.New("Unable to decode suite config: " + err.Error())
		return
	}
	return
}

func CreateK8SNamespace(kubeClient kubernetes.Interface, namespaceName string) error {
	namespaceList, err := kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// Return if namespace already exists
	for _, temNs := range namespaceList.Items {
		if temNs.GetName() == namespaceName {
			return nil
		}
	}

	nsLabel := map[string]string{
		"name": namespaceName,
	}
	nsSpec := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName, Labels: nsLabel}}
	_, err = kubeClient.CoreV1().Namespaces().Create(nsSpec)
	return err
}

// Execute shell command and returns the stderr and stdout buffers
func runExecCommand(t *testing.T, command *exec.Cmd) error {
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		t.Log(stdout.String())
		return errors.New("Error during ansible execution: " + stderr.String() + "\n" + err.Error())
	}
	return nil
}

// Create hosts file for each cluster to be used by Ansible script
func createAnsibleHostFiles(filePathToSave string, kubeClusterSpec ClusterInfo) error {
	loadOptions := ini.LoadOptions{}
	loadOptions.UnparseableSections = []string{"master_node", "worker_node"}
	loadOptions.Loose = true

	loginSectionData := map[string]string{
		"ansible_connection":      "ssh",
		"ansible_ssh_user":        "root",
		"ansible_ssh_pass":        "couchbase",
		"ansible_ssh_common_args": "-o StrictHostKeyChecking=no",
	}
	masterSectionData := ""
	workerSectionData := ""

	newClusterConfig, err := ini.LoadSources(loadOptions, "")
	if err != nil {
		return errors.New("Unable to initialize cluster file: " + err.Error())
	}

	loginSectionForCluster, err := newClusterConfig.NewSection("all:vars")
	if err != nil {
		return errors.New("Error while creating new section 'all'")
	}

	for key, value := range loginSectionData {
		loginSectionForCluster.NewKey(key, value)
	}

	for index, nodeData := range kubeClusterSpec.MasterNodeList {
		hostnameStr := "hostname=k8s-" + kubeClusterSpec.ClusterName + "-master" + strconv.Itoa(index+1)
		masterSectionData += nodeData.Ip + " " + hostnameStr + "\n"
	}
	for index, nodeData := range kubeClusterSpec.WorkerNodeList {
		hostnameStr := "hostname=k8s-" + kubeClusterSpec.ClusterName + "-worker" + strconv.Itoa(index+1)
		workerSectionData += nodeData.Ip + " " + hostnameStr + "\n"
	}

	_, err = newClusterConfig.NewRawSection("master_node", masterSectionData)
	if err != nil {
		return errors.New("Unable to create master_node section: " + err.Error())
	}
	_, err = newClusterConfig.NewRawSection("worker_node", workerSectionData)
	if err != nil {
		return errors.New("Unable to create worker_node section: " + err.Error())
	}

	err = newClusterConfig.SaveTo(filePathToSave)
	if err != nil {
		return errors.New("Unable to save cluster file: " + err.Error())
	}
	return nil
}

func createNamespaceFile(namespace string) error {
	nsJsonStr := "{\"kind\": \"Namespace\",\"apiVersion\": \"v1\",\"metadata\": { \"name\": \"" + namespace + "\", \"labels\": { \"name\": \"" + namespace + "\" }}}"
	fileByteData := []byte(nsJsonStr)
	return ioutil.WriteFile("/tmp/namespace.json", fileByteData, 0644)
}

func SetupK8SCluster(t *testing.T, namespace, kubeType, kubeVersion, ymlFilePath, reqOpImage string, kubeClusterSpec ClusterInfo) error {
	clusterHostFile := ymlFilePath + "/" + kubeClusterSpec.ClusterName
	clusterInitFile := ymlFilePath + "/" + kubeType + "/initialize.yaml"
	clusterSetupFile := ymlFilePath + "/" + kubeType + "/setupCluster.yaml"
	clusterNamespaceFile := ymlFilePath + "/generic/createNamespace.yaml"
	clusterRoleSetupFile := ymlFilePath + "/generic/createRoles.yaml"
	pullDockerImageFile := ymlFilePath + "/generic/pullDockerImage.yaml"

	err := createAnsibleHostFiles(clusterHostFile, kubeClusterSpec)
	if err != nil {
		return err
	}

	switch kubeType {
	case "kubernetes":
		t.Logf("Running ansible script for %s", kubeClusterSpec.ClusterName)

		ansibleExtraVarParam := "kubeVersion=" + kubeVersion
		ansibleCmd := exec.Command("ansible-playbook", "-i", clusterHostFile, clusterInitFile, "--extra-vars", ansibleExtraVarParam)
		if err := runExecCommand(t, ansibleCmd); err != nil {
			return err
		}

		ansibleExtraVarParam = "kubeConfPathToSave=config_" + kubeClusterSpec.ClusterName
		ansibleCmd = exec.Command("ansible-playbook", "-i", clusterHostFile, clusterSetupFile, "--extra-vars", ansibleExtraVarParam)
		if err := runExecCommand(t, ansibleCmd); err != nil {
			return err
		}

		if namespace != "default" {
			t.Logf("Creating namespace %s", namespace)
			if err = createNamespaceFile(namespace); err != nil {
				return errors.New("Failed to create namespace file: " + err.Error())
			}
			ansibleCmd = exec.Command("ansible-playbook", "-i", clusterHostFile, clusterNamespaceFile)
			if err := runExecCommand(t, ansibleCmd); err != nil {
				return err
			}
		}

		t.Logf("Creating role bindings for namespace %s", namespace)
		ansibleExtraVarParam = "namespace=" + namespace
		ansibleCmd = exec.Command("ansible-playbook", "-i", clusterHostFile, clusterRoleSetupFile, "--extra-vars", ansibleExtraVarParam)
		if err := runExecCommand(t, ansibleCmd); err != nil {
			return err
		}

		ansibleExtraVarParam = "dockerImgName=" + reqOpImage
		ansibleCmd = exec.Command("ansible-playbook", "-i", clusterHostFile, pullDockerImageFile, "--extra-vars", ansibleExtraVarParam)
		if err := runExecCommand(t, ansibleCmd); err != nil {
			return err
		}

		t.Logf("Cluster %s created successfully", kubeClusterSpec.ClusterName)
	default:
		return errors.New("Unsupported kube-type in test_config")
	}
	return nil
}
