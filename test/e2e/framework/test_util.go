package framework

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1beta1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"runtime/debug"
)

// Const Ansible setting string
var (
	ansibleLoginSectionData = map[string]string{
		"ansible_connection":      "ssh",
		"ansible_ssh_user":        "root",
		"ansible_ssh_pass":        "couchbase",
		"ansible_ssh_common_args": "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
	}
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
	PullDockerImages   bool           `yaml:"pullDockerImages"`
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

func RemoveClusterRole(kubeClient kubernetes.Interface, roleName string) error {
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
	return nil
}

func RemoveServiceAccount(kubeClient kubernetes.Interface, namespace, serviceAccountName string) error {
	svcAccList, err := kubeClient.CoreV1().ServiceAccounts(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, svcAcc := range svcAccList.Items {
		if svcAcc.GetName() == serviceAccountName {
			kubeClient.CoreV1().ServiceAccounts(namespace).Delete(svcAcc.GetName(), &metav1.DeleteOptions{})
			err = WaitForServiceAccountDeleted(kubeClient, serviceAccountName, namespace, 30)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func RemoveClusterRoleBinding(kubeClient kubernetes.Interface, namespace, clusterRoleBindingName string) error {
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
	return nil
}

func RecreateClusterRoles(kubeClient kubernetes.Interface, roleName string) error {
	if err := RemoveClusterRole(kubeClient, roleName); err != nil {
		return nil
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
	_, err := kubeClient.RbacV1beta1().ClusterRoles().Create(clusterRoleSpec)
	return err
}

func RecreateServiceAccount(kubeClient kubernetes.Interface, namespace, serviceAccountName string) error {
	if serviceAccountName == "default" {
		return nil
	}

	if err := RemoveServiceAccount(kubeClient, namespace, serviceAccountName); err != nil {
		return err
	}

	// Create service account given by the name
	serviceAccountSpec := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName, Namespace: namespace},
	}
	_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(serviceAccountSpec)
	return err
}

func RecreateClusterRoleBindings(kubeClient kubernetes.Interface, namespace, clusterRoleName string) error {
	clusterRoleBindingName := clusterRoleName + "-" + namespace
	if err := RemoveClusterRoleBinding(kubeClient, namespace, clusterRoleBindingName); err != nil {
		return err
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
	_, err := kubeClient.RbacV1beta1().ClusterRoleBindings().Create(clusterRoleBindingSpec)
	return err
}

// Create ansibleHost file for the list of host provided
func createAnsibleHostFileFromHosts(filePathToSave string, hostList []string) error {
	loadOptions := ini.LoadOptions{}
	loadOptions.Loose = true

	newClusterConfig, err := ini.LoadSources(loadOptions, "")
	if err != nil {
		return errors.New("Unable to initialize cluster file: " + err.Error())
	}

	loginSectionForCluster, err := newClusterConfig.NewSection("all:vars")
	if err != nil {
		return errors.New("Error while creating new section 'all'")
	}

	nodesSection, err := newClusterConfig.NewSection("nodes")
	if err != nil {
		return errors.New("Error while creating new section 'nodes'")
	}

	for key, value := range ansibleLoginSectionData {
		loginSectionForCluster.NewKey(key, value)
	}

	for _, host := range hostList {
		nodesSection.NewKey(host, "")
	}

	err = newClusterConfig.SaveTo(filePathToSave)
	if err != nil {
		return errors.New("Unable to save playbook file: " + err.Error())
	}
	return nil
}

// Create hosts file for each cluster to be used by Ansible script
func createAnsibleHostFiles(filePathToSave string, kubeClusterSpec ClusterInfo) error {
	loadOptions := ini.LoadOptions{}
	loadOptions.UnparseableSections = []string{"master_node", "worker_node"}
	loadOptions.Loose = true

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

	for key, value := range ansibleLoginSectionData {
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

func RecreateClusterRolesEtcd(kubeClient kubernetes.Interface) error {
	if err := RemoveClusterRole(kubeClient, "etcd-operator"); err != nil {
		return err
	}

	policyRule1 := rbacv1.PolicyRule{
		APIGroups: []string{"etcd.database.coreos.com"},
		Resources: []string{"etcdclusters", "etcdbackups", "etcdrestores"},
		Verbs:     []string{"*"},
	}

	policyRule2 := rbacv1.PolicyRule{
		APIGroups: []string{"apiextensions.k8s.io"},
		Resources: []string{"customresourcedefinitions"},
		Verbs:     []string{"*"},
	}

	policyRule3 := rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods", "services", "endpoints", "persistentvolumeclaims", "events"},
		Verbs:     []string{"*"},
	}

	policyRule4 := rbacv1.PolicyRule{
		APIGroups: []string{"apps"},
		Resources: []string{"deployments"},
		Verbs:     []string{"*"},
	}

	policyRule5 := rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"secrets"},
		Verbs:     []string{"get"},
	}

	clusterRoleSpec := &rbacv1.ClusterRole{
		TypeMeta:   metav1.TypeMeta{Kind: "ClusterRole", APIVersion: "rbac.authorization.k8s.io/v1beta1"},
		ObjectMeta: metav1.ObjectMeta{Name: "etcd-operator"},
		Rules:      []rbacv1.PolicyRule{policyRule1, policyRule2, policyRule3, policyRule4, policyRule5},
	}
	_, err := kubeClient.RbacV1beta1().ClusterRoles().Create(clusterRoleSpec)
	return err
}

func RecreateClusterRoleBindingsEtcd(kubeClient kubernetes.Interface) error {
	clusterRoleBindingName := "etcd-operator"
	clusterRoleName := "etcd-operator"
	if err := RemoveClusterRoleBinding(kubeClient, "default", clusterRoleBindingName); err != nil {
		return err
	}

	clusterRoleBindingSubjects := []rbacv1.Subject{
		rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      "default",
			Namespace: "default",
		},
	}

	clusterRoleBindingSpec := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName},
		Subjects: clusterRoleBindingSubjects,
	}
	_, err := kubeClient.RbacV1beta1().ClusterRoleBindings().Create(clusterRoleBindingSpec)
	return err
}

func RecreateClusterRolesPortworx(kubeClient kubernetes.Interface) error {
	if err := RemoveClusterRole(kubeClient, "node-get-put-list-role"); err != nil {
		return err
	}

	policyRule1 := rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"nodes"},
		Verbs:     []string{"watch", "get", "update", "list"},
	}

	policyRule2 := rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"delete", "get", "list"},
	}

	policyRule3 := rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"persistentvolumeclaims", "persistentvolumes"},
		Verbs:     []string{"get", "list"},
	}

	clusterRoleSpec := &rbacv1.ClusterRole{
		TypeMeta:   metav1.TypeMeta{Kind: "ClusterRole", APIVersion: "rbac.authorization.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "node-get-put-list-role"},
		Rules:      []rbacv1.PolicyRule{policyRule1, policyRule2, policyRule3},
	}
	_, err := kubeClient.RbacV1beta1().ClusterRoles().Create(clusterRoleSpec)
	return err
}

func RecreateClusterRoleBindingsPortworx(kubeClient kubernetes.Interface) error {
	clusterRoleBindingName := "node-role-binding"
	clusterRoleName := "node-get-put-list-role"
	if err := RemoveClusterRoleBinding(kubeClient, "kube-system", clusterRoleBindingName); err != nil {
		return err
	}

	clusterRoleBindingSubjects := []rbacv1.Subject{
		rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      "px-account",
			Namespace: "kube-system",
		},
	}

	clusterRoleBindingSpec := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRoleBindingName},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName},
		Subjects: clusterRoleBindingSubjects,
	}
	_, err := kubeClient.RbacV1beta1().ClusterRoleBindings().Create(clusterRoleBindingSpec)
	return err
}

func RecreateServiceAccountPortworx(kubeClient kubernetes.Interface) error {
	serviceAccountName := "px-account"
	namespace := "kube-system"

	if err := RemoveServiceAccount(kubeClient, namespace, serviceAccountName); err != nil {
		return err
	}

	// Create service account given by the name
	serviceAccountSpec := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName, Namespace: namespace},
	}
	_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(serviceAccountSpec)
	return err
}

func WaitForServiceDeleted(kubeClient kubernetes.Interface, serviceName string, namespace string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out waiting for service account to be delete: " + serviceName)

		case <-tickChan:
			svcList, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, svc := range svcList.Items {
				if svc.GetName() == "default" {
					break
				}
				if svc.GetName() == serviceName {
					break
				}

			}
			return nil
		}
	}
}

func RemoveService(kubeClient kubernetes.Interface, namespace, serviceName string) error {
	svcList, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, svc := range svcList.Items {
		if svc.GetName() == serviceName {
			kubeClient.CoreV1().Services(namespace).Delete(svc.GetName(), &metav1.DeleteOptions{})
			err = WaitForServiceDeleted(kubeClient, serviceName, namespace, 30)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func RecreateServicePortworx(kubeClient kubernetes.Interface) error {
	serviceName := "portworx-service"
	namespace := "kube-system"

	if err := RemoveService(kubeClient, namespace, serviceName); err != nil {
		return err
	}
	labels := make(map[string]string)
	labels["name"] = "portworx"
	ports := []v1.ServicePort{{
		Name:       "px-api",
		Port:       9001,
		TargetPort: intstr.FromInt(9001),
		Protocol:   v1.ProtocolTCP,
	}}

	serviceSpec := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Ports:    ports,
			Selector: labels,
		},
	}
	_, err := kubeClient.CoreV1().Services(namespace).Create(serviceSpec)
	return err
}

func WaitForStorageClassDeleted(kubeClient kubernetes.Interface, storageClassName string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return errors.New("Timed out waiting for storage class to be delete: " + storageClassName)

		case <-tickChan:
			scList, err := kubeClient.StorageV1().StorageClasses().List(metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, sc := range scList.Items {
				if sc.GetName() == storageClassName {
					break
				}
			}
			return nil
		}
	}
}

func RemoveStorageClass(kubeClient kubernetes.Interface, storageClassName string) error {
	scList, err := kubeClient.StorageV1().StorageClasses().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, sc := range scList.Items {
		if sc.GetName() == storageClassName {
			kubeClient.StorageV1().StorageClasses().Delete(sc.GetName(), &metav1.DeleteOptions{})
			err = WaitForStorageClassDeleted(kubeClient, storageClassName, 30)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func RecreateStorageClassPortworx(kubeClient kubernetes.Interface) error {
	storageClassName := "standard"

	if err := RemoveStorageClass(kubeClient, storageClassName); err != nil {
		return err
	}
	parameters := make(map[string]string)
	parameters["repl"] = "1"
	parameters["snap_interval"] = "70"
	parameters["io_priority"] = "high"

	storageClassSpec := &storagev1.StorageClass{
		TypeMeta:    metav1.TypeMeta{Kind: "StorageClass", APIVersion: "storage.k8s.io/v1beta1"},
		ObjectMeta:  metav1.ObjectMeta{Name: storageClassName},
		Provisioner: "kubernetes.io/portworx-volume",
		Parameters:  parameters,
	}
	_, err := kubeClient.StorageV1().StorageClasses().Create(storageClassSpec)
	return err
}

func DeleteEtcd(t *testing.T, kubeClient kubernetes.Interface, kubeName string) error {
	t.Logf("deleting etcd deployment")
	DeleteOperatorCompletely(kubeClient, "etcd-operator", "default")

	t.Log("deleting ectd-operator pods")
	err := e2eutil.DeletePodsWithLabel(t, kubeClient, "name=etcd-operator", "default")
	if err != nil {
		return err
	}

	// delete etcd services
	t.Logf("deleting etcd services")
	services, err := kubeClient.CoreV1().Services("default").List(metav1.ListOptions{LabelSelector: "app=etcd"})
	if err != nil {
		return err
	}
	for _, service := range services.Items {
		t.Logf("deleting etcd service: %+v\n", service.Name)
		err = kubeClient.CoreV1().Services("default").Delete(service.Name, metav1.NewDeleteOptions(0))
		if err != nil {
			return err
		}
	}

	// delete etcd endpoints
	t.Logf("deleting etcd endpoints")
	endpoints, err := kubeClient.CoreV1().Endpoints("default").List(metav1.ListOptions{LabelSelector: "app=etcd"})
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints.Items {
		t.Logf("deleting etcd endpoints: %+v\n", endpoint.Name)
		err = kubeClient.CoreV1().Endpoints("default").Delete(endpoint.Name, metav1.NewDeleteOptions(0))
		if err != nil {
			return err
		}
	}
	err = kubeClient.CoreV1().Endpoints("default").Delete("etcd-operator", metav1.NewDeleteOptions(0))
	if err != nil {
		t.Logf("etcd-operator endpoint already deleted")
	}

	// delete etcd pods
	t.Log("deleting etcd pods")
	err = e2eutil.DeletePodsWithLabel(t, kubeClient, "app=etcd", "default")
	if err != nil {
		return err
	}

	t.Log("running delete-etcd-automation.sh")
	kubeConfigPath := os.Getenv("HOME") + "/.kube/config_" + kubeName
	wipePortworxCmd := exec.Command("bash", "./resources/thirdparty/etcd/delete-etcd-automation.sh", kubeConfigPath)
	if err := runExecCommand(t, wipePortworxCmd); err != nil {
		t.Logf("error deleteing etcd operator crd: " + err.Error())
	}

	return nil
}

func CreateEtcdCluster(t *testing.T, kubeName string) error {
	t.Logf("deploying etcd")
	kubeConfigPath := os.Getenv("HOME") + "/.kube/config_" + kubeName
	createEtcCLusterdCmd := exec.Command("bash", "./resources/thirdparty/etcd/deploy-etcd-automation.sh", kubeConfigPath)
	if err := runExecCommand(t, createEtcCLusterdCmd); err != nil {
		return errors.New("error creating etcd cluster: " + err.Error())
	}
	t.Logf("etcd deployed")
	return nil
}

func GetEtcdServiceEndpoint(kubeClient kubernetes.Interface) (string, error) {
	services, err := kubeClient.CoreV1().Services("default").List(metav1.ListOptions{LabelSelector: "app=etcd"})
	if err != nil {
		return "", err
	}
	for _, service := range services.Items {
		if service.Spec.ClusterIP != "" && service.Spec.ClusterIP != "None" {
			return service.Spec.ClusterIP, nil
		}
	}
	return "", errors.New("etcd client service not found")
}

func CreateEtcd(t *testing.T, kubeClient kubernetes.Interface, kubeName string) error {
	t.Log("creating etcd cluster role")
	err := RecreateClusterRolesEtcd(kubeClient)
	if err != nil {
		return err
	}
	t.Log("creating etcd cluster role binding")
	err = RecreateClusterRoleBindingsEtcd(kubeClient)
	if err != nil {
		return err
	}

	t.Log("creating etcd cluster")
	err = CreateEtcdCluster(t, kubeName)
	if err != nil {
		return err
	}

	return nil
}

func CreatePortworx(t *testing.T, kubeClient kubernetes.Interface, kubeName string) error {
	t.Log("creating portworx service account")
	err := RecreateServiceAccountPortworx(kubeClient)
	if err != nil {
		return err
	}

	t.Log("creating portworx cluster role")
	err = RecreateClusterRolesPortworx(kubeClient)
	if err != nil {
		return err
	}

	t.Log("creating portworx cluster role binding")
	err = RecreateClusterRoleBindingsPortworx(kubeClient)
	if err != nil {
		return err
	}

	t.Log("creating portworx service")
	err = RecreateServicePortworx(kubeClient)
	if err != nil {
		return err
	}

	t.Log("creating portworx storage class")
	err = RecreateStorageClassPortworx(kubeClient)
	if err != nil {
		return err
	}

	t.Log("grabbing etcd endpoint ip")
	etcdEndpointIP, err := GetEtcdServiceEndpoint(kubeClient)
	if err != nil {
		return err
	}

	err = e2eutil.AddLabelToNodes(t, kubeClient, "px/enabled", "false")
	if err != nil {
		return err
	}

	t.Log("deploying portworx service")
	kubeConfigPath := os.Getenv("HOME") + "/.kube/config_" + kubeName
	deployPortworxCmd := exec.Command("bash", "./resources/thirdparty/portworx/deploy-portworx-automation.sh", etcdEndpointIP, "portworx-test", kubeConfigPath)
	if err := runExecCommand(t, deployPortworxCmd); err != nil {
		return errors.New("error running submit-portworx-automation.sh: " + err.Error())
	}

	t.Log("scaling up portworx pods")
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	numNodes := len(k8sNodeList.Items)
	for i := 0; i < numNodes; i++ {
		//remove label from node i
		for retryCount := 0; retryCount < 3; retryCount++ {
			time.Sleep(5 * time.Second)
			k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
			if err != nil {
				if retryCount == 2 {
					return err
				} else {
					continue
				}
			}
			node := k8sNodeList.Items[i]
			err = e2eutil.AddLabelToNode(t, kubeClient, node, "px/enabled", "true")
			if err != nil {
				if retryCount == 2 {
					return err
				} else {
					continue
				}
			}
			break
		}
		time.Sleep(5 * time.Second)
		err = e2eutil.WaitForPodsReadyWithLabel(t, kubeClient, 300, "name=portworx", "kube-system")
		if err != nil {
			return err
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}

func DeletePortworx(t *testing.T, kubeClient kubernetes.Interface, kubeName string) error {
	t.Log("deleting portworx daemonset")
	err := e2eutil.DeleteDaemonSetsWithLabel(t, kubeClient, "name=portworx", "kube-system")
	if err != nil {
		return err
	}

	t.Log("deleting portworx pods")
	err = e2eutil.DeletePodsWithLabel(t, kubeClient, "name=portworx", "kube-system")
	if err != nil {
		return err
	}

	t.Log("deleting talisman job")
	jobs, err := kubeClient.BatchV1().Jobs("kube-system").List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, job := range jobs.Items {
		kubeClient.BatchV1().Jobs("kube-system").Delete(job.Name, metav1.NewDeleteOptions(0))
	}

	t.Log("deleting talisman pods")
	err = e2eutil.DeletePodsWithLabel(t, kubeClient, "name=talisman", "kube-system")
	if err != nil {
		return err
	}

	t.Log("running delete-portworx-automation.sh")
	err = RecreateServicePortworx(kubeClient)
	if err != nil {
		return err
	}
	kubeConfigPath := os.Getenv("HOME") + "/.kube/config_" + kubeName
	deletePortworxCmd := exec.Command("bash", "./resources/thirdparty/portworx/delete-portworx-automation.sh", kubeConfigPath)
	if err := runExecCommand(t, deletePortworxCmd); err != nil {
		return errors.New("error running delete-portworx-automation.sh: " + err.Error())
	}
	return nil
}
