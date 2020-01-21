package framework

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"

	"gopkg.in/yaml.v2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	dockerPullSecretName = "test-docker-pull-secret"
)

// Variable to store the results globally
var Results = []TestResult{}

// analyzeResults accepts a list of test results and displays success rates
func AnalyzeResults(t *testing.T) {
	t.Logf("Suite Test Results: \n")

	//structs for xml

	type TestCase struct {
		XMLName xml.Name `xml:"testcase"`
		Name    string   `xml:"name,attr"`
		Time    string   `xml:"time,attr"`
		Error   string   `xml:"error,omitempty"`
	}

	type TestSuite struct {
		XMLName   xml.Name   `xml:"testsuite"`
		Name      string     `xml:"name,attr"`
		Tests     string     `xml:"tests,attr"`
		Errors    string     `xml:"errors,attr"`
		Failures  string     `xml:"failures,attr"`
		Skip      string     `xml:"skip,attr"`
		Time      string     `xml:"time,attr"`
		Testcases []TestCase `xml:"testcase"`
	}

	failures := []string{}
	instabilities := []string{}
	testcases := []TestCase{}

	for i, result := range Results {

		if result.Result {
			t.Logf("%d: %s...PASS", i+1, result.Name)
			testcases = append(testcases, TestCase{Name: result.Name, Time: "0"})
		} else {
			t.Logf("%d: %s...FAIL", i+1, result.Name)
			testcases = append(testcases, TestCase{Name: result.Name, Time: "0", Error: "fail"})
			failures = append(failures, result.Name)
		}
		if result.Unstable {
			testcases = append(testcases, TestCase{Name: result.Name, Time: "0", Error: "unstable"})
			instabilities = append(instabilities, result.Name)
		}
	}

	testsuite := TestSuite{
		Name:      suiteData.SuiteName,
		Tests:     strconv.Itoa(len(Results)),
		Errors:    strconv.Itoa(len(instabilities)),
		Failures:  strconv.Itoa(len(failures)),
		Skip:      "0",
		Time:      "0",
		Testcases: testcases,
	}

	pass := float64(len(Results) - len(failures))
	fail := float64(len(failures))
	total := float64(len(Results))
	passRate := (pass / total) * 100.0

	if fail > 0 {
		t.Logf("Failures: ")
		for i, test := range failures {
			t.Logf("%d: %s", i+1, test)
		}
	}

	if len(instabilities) > 0 {
		t.Log("Unstable tests:")
		for i, test := range instabilities {
			t.Logf("%d: %s", i+1, test)
		}
	}

	t.Logf("\n Pass: %f \n Fail: %f \n Pass Rate: %f", pass, fail, passRate)

	if xmlstring, err := xml.MarshalIndent(testsuite, "", "    "); err == nil {
		xmlstring = []byte(xml.Header + string(xmlstring))
		err := ioutil.WriteFile("results.xml", xmlstring, 0644)
		if err != nil {
			t.Fatalf("Failed to write test XML: %v", err)
		}
	}

	if fail > 0 {
		t.Fatalf("suite contains failures")
	}
}

// Read Test run params from test_config yaml file
func readRuntimeConfig(ymlFilePath string) (runTimeConfig TestRunParam, err error) {
	ymlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = fmt.Errorf("unable to read cluster config file `%s`: %v", ymlFilePath, err)
		return
	}

	if err = yaml.Unmarshal(ymlFileContent, &runTimeConfig); err != nil {
		err = fmt.Errorf("unable to decode test config: %v", err)
		return
	}
	for i, kubeConf := range runTimeConfig.KubeConfig {
		if strings.HasPrefix(kubeConf.ClusterConfig, "~/") {
			runTimeConfig.KubeConfig[i].ClusterConfig = strings.Replace(kubeConf.ClusterConfig, "~", os.Getenv("HOME"), 1)
		}
	}
	return
}

// Function to read Suite and required cluster info from suite.yaml file
func getSuiteDataFromYml(ymlFilePath string) (suiteData SuiteData, err error) {
	yamlFileContent, err := ioutil.ReadFile(ymlFilePath)
	if err != nil {
		err = fmt.Errorf("unable to read suite config file: %v", err)
		return
	}

	err = yaml.Unmarshal(yamlFileContent, &suiteData)
	if err != nil {
		err = fmt.Errorf("unable to decode suite config: %v", err)
		return
	}
	return
}

func createK8SNamespace(kubeClient kubernetes.Interface, namespaceName string) error {
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

func removeRole(kubeClient kubernetes.Interface, roleName string) error {
	if err := kubeClient.RbacV1().Roles(Global.Namespace).Delete(roleName, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return err
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
			if err := kubeClient.CoreV1().ServiceAccounts(namespace).Delete(svcAcc.GetName(), &metav1.DeleteOptions{}); err != nil {
				return err
			}
			if err := waitForServiceAccountDeleted(kubeClient, serviceAccountName, namespace, 30); err != nil {
				return err
			}
		}

	}
	return nil
}

// RecreateDockerAuthSecret deletes existing secrets and creates a new one if specified.
// This secret, if defined, will be added to the operator and admission controllers in
// order to pull from a private repository.
func recreateDockerAuthSecret(client kubernetes.Interface) error {
	// Clean up the old authentication secret if it exists
	if err := client.CoreV1().Secrets(runtimeParams.Namespace).Delete(dockerPullSecretName, nil); err != nil && !errors.IsNotFound(err) {
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

		// Register with the cluster creation module that we have a pull secret.
		e2espec.SetImagePullSecret(dockerPullSecretName)
	}

	return nil
}

func recreateRoles(kubeClient kubernetes.Interface, roleName string) error {
	if err := removeRole(kubeClient, config.OperatorResourceName); err != nil {
		return nil
	}
	if err := removeRole(kubeClient, config.BackupResourceName); err != nil {
		return nil
	}

	if err := CreateBackupRole(kubeClient, Global.Namespace); err != nil {
		return err
	}
	roleSpec := config.GetOperatorRole()
	roleSpec.Name = roleName
	_, err := kubeClient.RbacV1().Roles(Global.Namespace).Create(roleSpec)
	return err
}

func RecreateServiceAccount(kubeClient kubernetes.Interface, namespace, serviceAccountName string) error {
	if err := RemoveServiceAccount(kubeClient, namespace, serviceAccountName); err != nil {
		return err
	}
	if serviceAccountName == "default" {
		return nil
	}
	if err := RemoveServiceAccount(kubeClient, namespace, config.BackupResourceName); err != nil {
		return err
	}

	if err := CreateBackupServiceAccount(kubeClient, namespace); err != nil {
		return err
	}
	// Create service account given by the name
	serviceAccount := config.GetOperatorServiceAccount()
	serviceAccount.Name = serviceAccountName
	_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(serviceAccount)
	return err
}

func recreateRoleBindings(kubeClient kubernetes.Interface, namespace, clusterRoleName string) error {
	if err := removeRoleBinding(kubeClient, namespace, config.OperatorResourceName); err != nil {
		return err
	}
	if err := removeRoleBinding(kubeClient, namespace, config.BackupResourceName); err != nil {
		return err
	}

	if err := CreateBackupRoleBinding(kubeClient, namespace); err != nil {
		return err
	}
	clusterRoleBindingSpec := config.GetOperatorRoleBinding(Global.Namespace)
	_, err := kubeClient.RbacV1().RoleBindings(Global.Namespace).Create(clusterRoleBindingSpec)
	return err
}

func removeRoleBinding(kubeClient kubernetes.Interface, namespace, roleBindingName string) error {
	if err := kubeClient.RbacV1().RoleBindings(namespace).Delete(roleBindingName, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func waitForServiceAccountDeleted(kubeClient kubernetes.Interface, serviceAccountName string, namespace string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C
	for {
		select {
		case <-timeOutChan:
			return fmt.Errorf("timed out waiting for service account %s to be deleted", serviceAccountName)

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
