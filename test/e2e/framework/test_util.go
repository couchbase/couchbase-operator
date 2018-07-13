package framework

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"runtime/debug"
)

// TestFunc defines the test function type
type TestFunc func(*testing.T)

// DecoratorArgs will be used to pass arguments to decorators
type DecoratorArgs struct {
	KubeNames []string
}

// TestDecorator decorates a test function.  This is used to augment an
// existing test usually to perform setup and tear-down tasks e.g.
// initializing and deleting a cluster or applying TLS configuration
type TestDecorator func(TestFunc, DecoratorArgs) TestFunc

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

func RecreateClusterRoles(kubeClient kubernetes.Interface, roleName string) error {
	clusterRoleList, err := kubeClient.RbacV1beta1().ClusterRoles().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, clusterRole := range clusterRoleList.Items {
		if clusterRole.GetName() == roleName {
			kubeClient.RbacV1beta1().ClusterRoles().Delete(roleName, &metav1.DeleteOptions{})
			err = WaitForClusterRoleDeleted(kubeClient, roleName, 30)
			if err != nil {
				return err
			}
			break
		}
	}

	policyRule1 := rbacv1.PolicyRule{
		APIGroups: []string{"couchbase.database.couchbase.com"},
		Resources: []string{"couchbaseclusters"},
		Verbs:     []string{"*"},
	}

	policyRule2 := rbacv1.PolicyRule{
		APIGroups: []string{"storage.k8s.io"},
		Resources: []string{"storageclasses"},
		Verbs:     []string{"get"},
	}

	policyRule3 := rbacv1.PolicyRule{
		APIGroups: []string{"apiextensions.k8s.io"},
		Resources: []string{"customresourcedefinitions"},
		Verbs:     []string{"*"},
	}

	policyRule4 := rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods", "services", "endpoints", "persistentvolumeclaims", "persistentvolumes", "events", "secrets"},
		Verbs:     []string{"*"},
	}

	policyRule5 := rbacv1.PolicyRule{
		APIGroups: []string{"apps"},
		Resources: []string{"deployments"},
		Verbs:     []string{"*"},
	}

	clusterRoleSpec := &rbacv1.ClusterRole{
		TypeMeta:   metav1.TypeMeta{Kind: "ClusterRole", APIVersion: "rbac.authorization.k8s.io/v1beta1"},
		ObjectMeta: metav1.ObjectMeta{Name: roleName},
		Rules:      []rbacv1.PolicyRule{policyRule1, policyRule2, policyRule3, policyRule4, policyRule5},
	}
	_, err = kubeClient.RbacV1beta1().ClusterRoles().Create(clusterRoleSpec)
	return err
}

func RecreateServiceAccount(kubeClient kubernetes.Interface, namespace, serviceAccountName string) error {
	if serviceAccountName == "default" {
		return nil
	}

	// Delete service accounts apart from default one
	svcAccList, err := kubeClient.CoreV1().ServiceAccounts(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, svcAcc := range svcAccList.Items {
		if svcAcc.GetName() == "default" {
			continue
		}
		if svcAcc.GetName() == serviceAccountName {
			kubeClient.CoreV1().ServiceAccounts(namespace).Delete(svcAcc.GetName(), &metav1.DeleteOptions{})
			err = WaitForServiceAccountDeleted(kubeClient, serviceAccountName, namespace, 30)
			if err != nil {
				return err
			}
		}

	}

	// Create service account given by the name
	serviceAccountSpec := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName, Namespace: namespace},
	}
	_, err = kubeClient.CoreV1().ServiceAccounts(namespace).Create(serviceAccountSpec)
	return err
}

func RecreateClusterRoleBindings(kubeClient kubernetes.Interface, namespace, clusterRoleName string) error {
	clusterRoleBindingName := clusterRoleName + "-" + namespace
	clusterRoleBindingList, err := kubeClient.RbacV1beta1().ClusterRoleBindings().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, clusterRoleBinding := range clusterRoleBindingList.Items {
		if clusterRoleBinding.GetName() == clusterRoleBindingName {
			kubeClient.RbacV1beta1().ClusterRoleBindings().Delete(clusterRoleBindingName, &metav1.DeleteOptions{})
			err = WaitForClusterRoleBindingDeleted(kubeClient, clusterRoleBindingName, 30)
			if err != nil {
				return err
			}
			break
		}
	}
	clusterRoleBindingSubjects := []rbacv1.Subject{
		rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      clusterRoleName,
			Namespace: namespace,
		},
	}

	clusterRoleBindingSpec := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: clusterRoleName},
		Subjects:   clusterRoleBindingSubjects,
	}
	_, err = kubeClient.RbacV1beta1().ClusterRoleBindings().Create(clusterRoleBindingSpec)
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
	t.Log(stdout.String())
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
		"ansible_ssh_common_args": "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
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

func WaitForClusterRoleDeleted(kubeClient kubernetes.Interface, roleName string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out waiting for role to be delete: " + roleName)

		case <-tickChan:
			clusterRoleList, err := kubeClient.RbacV1beta1().ClusterRoles().List(metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, clusterRole := range clusterRoleList.Items {
				if clusterRole.GetName() == roleName {
					break
				}
			}
			return nil
		}
	}
}

func WaitForServiceAccountDeleted(kubeClient kubernetes.Interface, serviceAccountName string, namespace string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out waiting for service account to be delete: " + serviceAccountName)

		case <-tickChan:
			svcAccList, err := kubeClient.CoreV1().ServiceAccounts(namespace).List(metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, svcAcc := range svcAccList.Items {
				if svcAcc.GetName() == "default" {
					break
				}
				if svcAcc.GetName() == serviceAccountName {
					break
				}

			}
			return nil
		}
	}
}

func WaitForClusterRoleBindingDeleted(kubeClient kubernetes.Interface, clusterRoleBindingName string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out waiting for cluster role binding to be delete: " + clusterRoleBindingName)

		case <-tickChan:
			clusterRoleBindingList, err := kubeClient.RbacV1beta1().ClusterRoleBindings().List(metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, clusterRoleBinding := range clusterRoleBindingList.Items {
				if clusterRoleBinding.GetName() == clusterRoleBindingName {
					break
				}

			}
			return nil
		}
	}
}

func RecoverDecorator(test TestFunc, args DecoratorArgs) TestFunc {
	wrapperFunc := func(t *testing.T) {
		defer func(t *testing.T) {
			if r := recover(); r != nil {
				debug.PrintStack()
				t.Logf("Recovered: %v", r)
				t.Fatal("test failed due to panic")
			}
		}(t)
		test(t)
	}
	return wrapperFunc
}
