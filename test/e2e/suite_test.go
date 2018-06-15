package e2e

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"github.com/sirupsen/logrus"
)

func runSuite(t *testing.T) {
	f := framework.Global
	reqOpImage := f.Deployment.Spec.Template.Spec.Containers[0].Image
	ymlFilePath := "./resources/ansible"
	t.Logf("Starting suite %s", f.SuiteYmlData.SuiteName)

	for _, testGroup := range f.SuiteYmlData.TestCaseGroup {
		requiredClusters := []string{}
		for _, currClusterName := range testGroup.ClusterName {
			if !framework.ElementExistsInArr(currClusterName, requiredClusters) {
				requiredClusters = append(requiredClusters, currClusterName)
			}
		}

		kubeClustersToSetup, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, requiredClusters)
		if err != nil {
			t.Logf("Skipping %s.. %s", testGroup.GroupName, err)
			break
		}

		for _, kubeCluster := range kubeClustersToSetup {
			if _, ok := f.ClusterSpec[kubeCluster.ClusterName]; ok {
				continue
			}
			kubeName := kubeCluster.ClusterName
			logrus.Info("Creating K8S cluster " + kubeName + " for " + testGroup.GroupName)
			if err := framework.SetupK8SCluster(t, f.Namespace, f.KubeType, f.KubeVersion, ymlFilePath, reqOpImage, kubeCluster); err != nil {
				t.Fatal(err)
			}

			kubeConfigPath := ymlFilePath + "/" + f.KubeType + "/config_" + kubeCluster.ClusterName
			clusterSpec, err := framework.CreateKubeClusterObject(kubeConfigPath)
			if err != nil {
				t.Fatal(err)
			}

			f.ClusterSpec[kubeName] = &clusterSpec

			logrus.Info("Waiting for Kube nodes to become available")
			if err = e2eutil.WaitForKubeNodesToBeReady(t, f.ClusterSpec[kubeName].KubeClient, 300); err != nil {
				t.Fatal(err)
			}

			if err := f.SetupCouchbaseOperator(f.ClusterSpec[kubeName]); err != nil {
				t.Fatalf("Failed to setup couchbase operator: %v", err)
			}
			if err = f.CreateSecretInKubeCluster(kubeName); err != nil {
				t.Fatal(err)
			}
		}

		for _, currTestCase := range testGroup.TestCase {
			testName := currTestCase.TcName

			if _, ok := TestFuncMap[testName]; !ok {
				t.Logf("Skipping %s.. Undefined test", testName)
				continue
			}
			testFunc := TestFuncMap[testName]

			for _, funcName := range currTestCase.Decorators {
				if _, ok := DecoratorFuncMap[funcName]; !ok {
					t.Logf("Skipping %s.. Undefined decorator %s", testName, funcName)
					testFunc = nil
					break
				}
				testFunc = DecoratorFuncMap[funcName](testFunc)
			}
			if testFunc != nil {
				testResult := t.Run(testName, testFunc)
				framework.Results = append(framework.Results, framework.TestResult{Name: testName, Result: testResult})
			}
		}
	}
}

func TestOperator(t *testing.T) {
	if err := framework.Setup(t); err != nil {
		t.Fatal("Failed to setup framework: " + err.Error())
	}

	runSuite(t)
	framework.AnalyzeResults(t)
}
