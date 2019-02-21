package framework

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"

	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"github.com/sirupsen/logrus"
)

const (
	dockerPullSecretName = "test-docker-pull-secret"
)

// Variable to store the results globally
var Results = []TestResult{}

// analyzeResults accepts a list of test results and displays success rates
func AnalyzeResults(t *testing.T) {
	t.Logf("Suite Test Results: \n")

	failures := []string{}
	for i, result := range Results {
		if result.Result {
			t.Logf("%d: %s...PASS", i+1, result.Name)
		} else {
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
		err = fmt.Errorf("Unable to read cluster config file `%s`: %v", ymlFilePath, err)
		return
	}

	if err = yaml.Unmarshal(ymlFileContent, &runTimeConfig); err != nil {
		err = fmt.Errorf("Unable to decode test config: %v", err)
		return
	}
	for i, kubeConf := range runTimeConfig.KubeConfig {
		if strings.HasPrefix(kubeConf.ClusterConfig, "~/") {
			runTimeConfig.KubeConfig[i].ClusterConfig = strings.Replace(kubeConf.ClusterConfig, "~", os.Getenv("HOME"), 1)
		}
	}
	return
}

// Function to read cluster specific data
func GetClusterConfigFromYml(ymlFilePath, reqClusterType string, reqClusters []string) (clusters []ClusterInfo, err error) {
	var clusterConf ClusterConfig

	yamlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = fmt.Errorf("Unable to read cluster config file `%s`: %v", ymlFilePath, err)
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
	clusterRoleList, err := kubeClient.RbacV1().ClusterRoles().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, clusterRole := range clusterRoleList.Items {
		if clusterRole.GetName() == roleName {
			kubeClient.RbacV1().ClusterRoles().Delete(roleName, &metav1.DeleteOptions{})
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
	clusterRoleBindingList, err := kubeClient.RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, clusterRoleBinding := range clusterRoleBindingList.Items {
		if clusterRoleBinding.GetName() == clusterRoleBindingName {
			kubeClient.RbacV1().ClusterRoleBindings().Delete(clusterRoleBindingName, &metav1.DeleteOptions{})
			err = WaitForClusterRoleBindingDeleted(kubeClient, clusterRoleBindingName, 30)
			if err != nil {
				return err
			}
			break
		}
	}
	return nil
}

// RecreateDockerAuthSecret deletes existing secrets and creates a new one if specified.
// This secret, if defined, will be added to the operator and admission controllers in
// order to pull from a private repository.
func RecreateDockerAuthSecret(client kubernetes.Interface) error {
	// Clean up the old authentication secret if it exists
	if err := client.CoreV1().Secrets(runtimeParams.Namespace).Delete(dockerPullSecretName, nil); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// If specified create the authentication secret
	if runtimeParams.DockerServer != "" {
		// Check all the necessary bits are there
		if runtimeParams.DockerUsername == "" {
			return fmt.Errorf("docker username must be specified with docker server")
		}
		if runtimeParams.DockerPassword == "" {
			return fmt.Errorf("docker password must be specified with docker server")
		}

		// auth string is simply "username:password" base64 encoded
		auth := runtimeParams.DockerUsername + ":" + runtimeParams.DockerPassword
		auth = base64.StdEncoding.EncodeToString([]byte(auth))

		// authentication data is encoded as per "~/.docker/config.json", and created by "docker login"
		data := `{"auths":{"` + runtimeParams.DockerServer + `":{"auth":"` + auth + `"}}}`

		// create the new secret
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: dockerPullSecretName,
			},
			Type: v1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				".dockerconfigjson": []byte(data),
			},
		}
		if _, err := client.CoreV1().Secrets(runtimeParams.Namespace).Create(secret); err != nil {
			return err
		}
	}

	return nil
}

func RecreateClusterRoles(kubeClient kubernetes.Interface, roleName string) error {
	if err := RemoveClusterRole(kubeClient, roleName); err != nil {
		return nil
	}

	clusterRoleSpec := config.GetOperatorClusterRole()
	clusterRoleSpec.Name = roleName
	_, err := kubeClient.RbacV1().ClusterRoles().Create(clusterRoleSpec)
	return err
}

func RecreateServiceAccount(kubeClient kubernetes.Interface, namespace, serviceAccountName string) error {
	if err := RemoveServiceAccount(kubeClient, namespace, serviceAccountName); err != nil {
		return err
	}
	if serviceAccountName == "default" {
		return nil
	}
	// Create service account given by the name
	serviceAccountSpec := config.GetOperatorServiceAccount()
	serviceAccountSpec.Name = serviceAccountName
	_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(serviceAccountSpec)
	return err
}

func RecreateClusterRoleBindings(kubeClient kubernetes.Interface, namespace, clusterRoleName string) error {
	if err := RemoveClusterRoleBinding(kubeClient, namespace, clusterRoleName); err != nil {
		return err
	}

	clusterRoleBindingSpec := config.GetOperatorClusterRoleBinding(Global.Namespace)
	clusterRoleBindingSpec.Name = clusterRoleName
	_, err := kubeClient.RbacV1().ClusterRoleBindings().Create(clusterRoleBindingSpec)
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

	for key, value := range constants.AnsibleLoginSectionData {
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

	for key, value := range constants.AnsibleLoginSectionData {
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

func SetupK8SCluster(t *testing.T, namespace, kubeType, kubeVersion, ymlFilePath, reqOpImage string, kubeClusterSpec ClusterInfo) error {
	clusterHostFile := ymlFilePath + "/" + kubeClusterSpec.ClusterName
	clusterInitFile := ymlFilePath + "/" + kubeType + "/initialize.yaml"
	clusterSetupFile := ymlFilePath + "/" + kubeType + "/setupCluster.yaml"
	clusterConfFile := "config_" + kubeType + "_" + kubeClusterSpec.ClusterName

	if err := createAnsibleHostFiles(clusterHostFile, kubeClusterSpec); err != nil {
		return err
	}

	switch kubeType {
	case "kubernetes":
		logrus.Infof("Running ansible script for %s", kubeClusterSpec.ClusterName)

		ansibleExtraVarParam := "kubeVersion=" + kubeVersion
		ansibleCmd := exec.Command("ansible-playbook", "-i", clusterHostFile, clusterInitFile, "-c", "paramiko", "--extra-vars", ansibleExtraVarParam)
		if err := runExecCommand(t, ansibleCmd); err != nil {
			return err
		}
		kubeVersionParsed := strings.Split(kubeVersion, "-")
		kubeVersion = kubeVersionParsed[0]
		ansibleExtraVarParam = "kubeConfPathToSave=" + clusterConfFile + " kubeVersion=" + kubeVersion
		ansibleCmd = exec.Command("ansible-playbook", "-i", clusterHostFile, clusterSetupFile, "-c", "paramiko", "--extra-vars", ansibleExtraVarParam)
		if err := runExecCommand(t, ansibleCmd); err != nil {
			return err
		}

		logrus.Infof("Cluster %s created successfully", kubeClusterSpec.ClusterName)
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
			clusterRoleList, err := kubeClient.RbacV1().ClusterRoles().List(metav1.ListOptions{})
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
			clusterRoleBindingList, err := kubeClient.RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})
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
	_, err := kubeClient.RbacV1().ClusterRoles().Create(clusterRoleSpec)
	return err
}

func RecreateClusterRoleBindingsEtcd(kubeClient kubernetes.Interface, namespace string) error {
	clusterRoleBindingName := "etcd-operator"
	clusterRoleName := "etcd-operator"
	if err := RemoveClusterRoleBinding(kubeClient, namespace, clusterRoleBindingName); err != nil {
		return err
	}

	clusterRoleBindingSubjects := []rbacv1.Subject{
		rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      namespace,
			Namespace: namespace,
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
	_, err := kubeClient.RbacV1().ClusterRoleBindings().Create(clusterRoleBindingSpec)
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
	_, err := kubeClient.RbacV1().ClusterRoles().Create(clusterRoleSpec)
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
	_, err := kubeClient.RbacV1().ClusterRoleBindings().Create(clusterRoleBindingSpec)
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

func ServiceExists(kubeClient kubernetes.Interface, namespace, serviceName string) (bool, error) {
	svcList, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, svc := range svcList.Items {
		if svc.GetName() == serviceName {
			return true, nil
		}
	}
	return false, nil
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
	f := Global
	if err := RemoveStorageClass(kubeClient, f.StorageClassName); err != nil {
		return err
	}
	parameters := make(map[string]string)
	parameters["repl"] = "1"
	parameters["snap_interval"] = "70"
	parameters["io_priority"] = "high"

	storageClassSpec := &storagev1.StorageClass{
		TypeMeta:    metav1.TypeMeta{Kind: "StorageClass", APIVersion: "storage.k8s.io/v1beta1"},
		ObjectMeta:  metav1.ObjectMeta{Name: f.StorageClassName},
		Provisioner: "kubernetes.io/portworx-volume",
		Parameters:  parameters,
	}
	_, err := kubeClient.StorageV1().StorageClasses().Create(storageClassSpec)
	return err
}

func DeleteEtcd(t *testing.T, kubeClient kubernetes.Interface, namespace, kubeConfigPath string) error {
	logrus.Info("Running delete-etcd-automation.sh")
	wipePortworxCmd := exec.Command("bash", "./resources/thirdparty/etcd/delete-etcd-automation.sh", "--namespace", namespace, "--kubeConfig", kubeConfigPath)
	if err := runExecCommand(t, wipePortworxCmd); err != nil {
		logrus.Infof("Error deleteing etcd operator crd: %v", err)
	}

	logrus.Info("Deleting etcd-operator pods")
	if err := e2eutil.DeletePodsWithLabel(t, kubeClient, "name=etcd-operator", namespace); err != nil {
		return err
	}

	// delete etcd services
	logrus.Info("Deleting etcd services")
	services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: "app=etcd"})
	if err != nil {
		return err
	}
	for _, service := range services.Items {
		logrus.Infof("Deleting etcd service: %s", service.Name)
		if err := kubeClient.CoreV1().Services(namespace).Delete(service.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}

	// delete etcd endpoints
	logrus.Info("Deleting etcd endpoints")
	endpoints, err := kubeClient.CoreV1().Endpoints(namespace).List(metav1.ListOptions{LabelSelector: "app=etcd"})
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints.Items {
		logrus.Infof("deleting etcd endpoints: %s\n", endpoint.Name)
		if err := kubeClient.CoreV1().Endpoints(namespace).Delete(endpoint.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}
	if err := kubeClient.CoreV1().Endpoints(namespace).Delete("etcd-operator", metav1.NewDeleteOptions(0)); err != nil {
		logrus.Info("etcd-operator endpoint already deleted")
	}

	// delete etcd pods
	logrus.Info("Deleting etcd pods")
	if err := e2eutil.DeletePodsWithLabel(t, kubeClient, "app=etcd", namespace); err != nil {
		return err
	}

	logrus.Info("Deleting etcd deployment")
	DeleteOperatorCompletely(kubeClient, "etcd-operator", namespace)
	return nil
}

func CreateEtcdCluster(t *testing.T, kubeClient kubernetes.Interface, namespace, kubeConfigPath string) error {
	logrus.Info("Deploying etcd")
	createEtcCLusterdCmd := exec.Command("bash", "./resources/thirdparty/etcd/deploy-etcd-automation.sh", "--namespace", namespace, "--kubeConfig", kubeConfigPath)
	if err := runExecCommand(t, createEtcCLusterdCmd); err != nil {
		return errors.New("error creating etcd cluster: " + err.Error())
	}
	if err := e2eutil.WaitForPodsReadyWithLabel(t, kubeClient, 60, "app=etcd", namespace); err != nil {
		return err
	}
	logrus.Info("etcd deployed")
	return nil
}

func GetEtcdServiceEndpoint(kubeClient kubernetes.Interface, namespace string) (string, error) {
	services, err := kubeClient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: "app=etcd"})
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

func CreateEtcd(t *testing.T, kubeClient kubernetes.Interface, namespace, kubeConfigPath string) error {
	logrus.Info("Creating etcd cluster role")
	if err := RecreateClusterRolesEtcd(kubeClient); err != nil {
		return err
	}
	logrus.Info("Creating etcd cluster role binding")
	if err := RecreateClusterRoleBindingsEtcd(kubeClient, namespace); err != nil {
		return err
	}

	logrus.Info("Creating etcd cluster")
	if err := CreateEtcdCluster(t, kubeClient, namespace, kubeConfigPath); err != nil {
		return err
	}
	return nil
}

func CreatePortworx(t *testing.T, kubeClient kubernetes.Interface, namespace, kubeConfigPath string) error {
	logrus.Info("Creating portworx service account")
	if err := RecreateServiceAccountPortworx(kubeClient); err != nil {
		return err
	}

	logrus.Info("Creating portworx cluster role")
	if err := RecreateClusterRolesPortworx(kubeClient); err != nil {
		return err
	}

	logrus.Info("Creating portworx cluster role binding")
	if err := RecreateClusterRoleBindingsPortworx(kubeClient); err != nil {
		return err
	}

	logrus.Info("Creating portworx service")
	if err := RecreateServicePortworx(kubeClient); err != nil {
		return err
	}

	logrus.Info("Creating portworx storage class")
	if err := RecreateStorageClassPortworx(kubeClient); err != nil {
		return err
	}

	logrus.Info("Grabbing etcd endpoint ip")
	etcdEndpointIP, err := GetEtcdServiceEndpoint(kubeClient, namespace)
	if err != nil {
		return err
	}

	if err := e2eutil.AddLabelToNodes(t, kubeClient, "px/enabled", "false"); err != nil {
		return err
	}

	logrus.Info("Deploying portworx service")
	portworxClusterName := "test-portworx-" + e2eutil.RandomSuffix()
	deployPortworxCmd := exec.Command("bash", "./resources/thirdparty/portworx/deploy-portworx-automation.sh", etcdEndpointIP, portworxClusterName, kubeConfigPath)
	if err := runExecCommand(t, deployPortworxCmd); err != nil {
		return errors.New("error running submit-portworx-automation.sh: " + err.Error())
	}

	logrus.Info("Scaling up portworx pods")
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
				}
				continue
			}
			node := k8sNodeList.Items[i]
			if err := e2eutil.AddLabelToNode(t, kubeClient, node, "px/enabled", "true"); err != nil {
				if retryCount == 2 {
					return err
				}
				continue
			}
			break
		}
		time.Sleep(5 * time.Second)
		if err := e2eutil.WaitForPodsReadyWithLabel(t, kubeClient, 300, "name=portworx", "kube-system"); err != nil {
			e2eutil.AddLabelToNodes(t, kubeClient, "px/enabled", "true")
			time.Sleep(120 * time.Second)
			return err
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}

func DeletePortworx(t *testing.T, kubeClient kubernetes.Interface, kubeConfigPath string) error {
	logrus.Info("Creating portworx service")
	if err := RecreateServicePortworx(kubeClient); err != nil {
		return err
	}

	logrus.Info("Running delete-portworx-automation.sh")
	deletePortworxCmd := exec.Command("bash", "./resources/thirdparty/portworx/delete-portworx-automation.sh", kubeConfigPath)
	if err := runExecCommand(t, deletePortworxCmd); err != nil {
		return errors.New("error running delete-portworx-automation.sh: " + err.Error())
	}

	logrus.Info("Deleting portworx-service")
	if err := RemoveService(kubeClient, "kube-system", "portworx-service"); err != nil {
		return err
	}

	logrus.Info("Deleting portworx daemonset")
	if err := e2eutil.DeleteDaemonSetsWithLabel(t, kubeClient, "name=portworx", "kube-system"); err != nil {
		return err
	}

	logrus.Info("Deleting portworx pods")
	if err := e2eutil.DeletePodsWithLabel(t, kubeClient, "name=portworx", "kube-system"); err != nil {
		return err
	}

	logrus.Info("Deleting talisman job")
	jobs, err := kubeClient.BatchV1().Jobs("kube-system").List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, job := range jobs.Items {
		kubeClient.BatchV1().Jobs("kube-system").Delete(job.Name, metav1.NewDeleteOptions(0))
	}

	logrus.Info("Deleting talisman pods")
	if err := e2eutil.DeletePodsWithLabel(t, kubeClient, "name=talisman", "kube-system"); err != nil {
		return err
	}
	return nil
}

func SetupPersistentVolume(t *testing.T, kubeClient kubernetes.Interface, namespace, kubeName, storageClassType string) error {
	kubeConfigPath := e2eutil.GetKubeConfigToUse(Global.KubeType, kubeName)
	switch storageClassType {
	case "portworx":
		if err := DeletePortworx(t, kubeClient, kubeConfigPath); err != nil {
			t.Logf("non fatal error deleting portworx: %+v", err)
		}

		if err := DeleteEtcd(t, kubeClient, namespace, kubeConfigPath); err != nil {
			t.Fatal(err)
		}

		logrus.Info("Creating etcd cluster")
		maxRetries := constants.Retries5
		for retryCount := 0; retryCount < maxRetries; retryCount++ {
			if err := CreateEtcd(t, kubeClient, namespace, kubeConfigPath); err != nil {
				logrus.Infof("Error creating etcd: %v", err)
				DeleteEtcd(t, kubeClient, namespace, kubeConfigPath)
				if retryCount == maxRetries-1 {
					return err
				}
				time.Sleep(time.Second * 3)
				continue
			}
			break
		}

		logrus.Info("Creating Portworx cluster")
		for retryCount := 0; retryCount < maxRetries; retryCount++ {
			if err := CreatePortworx(t, kubeClient, namespace, kubeConfigPath); err != nil {
				logrus.Infof("Error creating portworx cluster: %v", err)
				DeletePortworx(t, kubeClient, kubeConfigPath)
				if retryCount == maxRetries-1 {
					return err
				}
				time.Sleep(time.Second * 3)
				continue
			}
			break
		}
	default:
		logrus.Infof("Storage type '%s' creation not supported", storageClassType)
	}
	return nil
}

// Remove specified label from all k8s nodes identified by kubeName
func RemoveLabels(nodeLabelName string, kubeClient kubernetes.Interface) error {
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to get k8s nodes " + err.Error())
	}
	for _, k8sNode := range k8sNodeList.Items {
		nodeLabels := k8sNode.GetLabels()
		delete(nodeLabels, nodeLabelName)
		k8sNode.SetLabels(nodeLabels)
		if _, err = kubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
			return errors.New("Failed to delete label for node " + k8sNode.Name + ": " + err.Error())
		}
	}
	return nil
}
