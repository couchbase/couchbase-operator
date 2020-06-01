package framework

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	dockerPullSecretName = "test-docker-pull-secret"
)

// Results is a global result store.
var Results = []TestResult{}

// analyzeResults accepts a list of test results and displays success rates.
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

// Read Test run params from test_config yaml file.
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

	return
}

// Function to read Suite and required cluster info from suite.yaml file.
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

func createK8SNamespace(k8s *types.Cluster) error {
	namespaceList, err := k8s.KubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// Return if namespace already exists
	for _, temNs := range namespaceList.Items {
		if temNs.GetName() == k8s.Namespace {
			return nil
		}
	}

	nsLabel := map[string]string{
		"name": k8s.Namespace,
	}

	nsSpec := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: k8s.Namespace, Labels: nsLabel}}

	_, err = k8s.KubeClient.CoreV1().Namespaces().Create(nsSpec)

	return err
}

func removeRole(k8s *types.Cluster, roleName string) error {
	if err := k8s.KubeClient.RbacV1().Roles(k8s.Namespace).Delete(roleName, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func RemoveServiceAccount(k8s *types.Cluster, serviceAccountName string) error {
	svcAccList, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, svcAcc := range svcAccList.Items {
		if svcAcc.GetName() == serviceAccountName {
			if err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).Delete(svcAcc.GetName(), &metav1.DeleteOptions{}); err != nil {
				return err
			}

			if err := waitForServiceAccountDeleted(k8s, serviceAccountName, 30); err != nil {
				return err
			}
		}
	}

	return nil
}

// RecreateDockerAuthSecret deletes existing secrets and creates a new one if specified.
// This secret, if defined, will be added to the operator and admission controllers in
// order to pull from a private repository.
func recreateDockerAuthSecret(kubeClient kubernetes.Interface, namespace string) error {
	// Clean up the old authentication secret if it exists
	if err := kubeClient.CoreV1().Secrets(namespace).Delete(dockerPullSecretName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	// If specified create the authentication secret
	if runtimeParams.DockerServer == "" || runtimeParams.DockerUsername == "" || runtimeParams.DockerPassword == "" {
		return nil
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
	if _, err := kubeClient.CoreV1().Secrets(namespace).Create(secret); err != nil {
		return err
	}

	// Register with the cluster creation module that we have a pull secret.
	e2espec.SetImagePullSecret(dockerPullSecretName)

	return nil
}

func recreateRoles(k8s *types.Cluster, roleName string) error {
	if err := removeRole(k8s, config.OperatorResourceName); err != nil {
		return nil
	}

	if err := removeRole(k8s, config.BackupResourceName); err != nil {
		return nil
	}

	if err := CreateBackupRole(k8s); err != nil {
		return err
	}

	roleSpec := config.GetOperatorRole()
	roleSpec.Name = roleName

	_, err := k8s.KubeClient.RbacV1().Roles(k8s.Namespace).Create(roleSpec)

	return err
}

func RecreateServiceAccount(k8s *types.Cluster, serviceAccountName string) error {
	if err := RemoveServiceAccount(k8s, serviceAccountName); err != nil {
		return err
	}

	if serviceAccountName == "default" {
		return nil
	}

	if err := RemoveServiceAccount(k8s, config.BackupResourceName); err != nil {
		return err
	}

	if err := CreateBackupServiceAccount(k8s); err != nil {
		return err
	}

	// Create service account given by the name
	serviceAccount := config.GetOperatorServiceAccount()
	serviceAccount.Name = serviceAccountName

	_, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).Create(serviceAccount)

	return err
}

func recreateRoleBindings(k8s *types.Cluster) error {
	if err := removeRoleBinding(k8s, config.OperatorResourceName); err != nil {
		return err
	}

	if err := removeRoleBinding(k8s, config.BackupResourceName); err != nil {
		return err
	}

	if err := CreateBackupRoleBinding(k8s); err != nil {
		return err
	}

	clusterRoleBindingSpec := config.GetOperatorRoleBinding(k8s.Namespace)

	_, err := k8s.KubeClient.RbacV1().RoleBindings(k8s.Namespace).Create(clusterRoleBindingSpec)

	return err
}

func removeRoleBinding(k8s *types.Cluster, roleBindingName string) error {
	if err := k8s.KubeClient.RbacV1().RoleBindings(k8s.Namespace).Delete(roleBindingName, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func waitForServiceAccountDeleted(k8s *types.Cluster, serviceAccountName string, waitTimeInSec int) error {
	timeOutChan := time.NewTimer(time.Duration(waitTimeInSec) * time.Second).C
	tickChan := time.NewTicker(time.Second * time.Duration(1)).C

	for {
		select {
		case <-timeOutChan:
			return fmt.Errorf("timed out waiting for service account %s to be deleted", serviceAccountName)

		case <-tickChan:
			svcAccList, err := k8s.KubeClient.CoreV1().ServiceAccounts(k8s.Namespace).List(metav1.ListOptions{})
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
