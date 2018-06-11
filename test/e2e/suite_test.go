package e2e

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

func runSuite(t *testing.T) {
	f := framework.Global
	ymlFilePath := "./resources/ansible"
	t.Logf("Starting suite %s", f.SuiteYmlData.SuiteName)

	for _, testGroup := range f.SuiteYmlData.TestCaseGroup {
		requiredClusters := []string{}
		for _, currClusterName := range testGroup.ClusterName {
			if !framework.ElementExistsInArr(currClusterName, requiredClusters) {
				requiredClusters = append(requiredClusters, currClusterName)
			}
		}

		kubeClustersToSetup, err := framework.GetClusterConfigFromYml(f.KubeType, requiredClusters)
		if err != nil {
			t.Logf("Skipping %s.. %s", testGroup.GroupName, err)
			break
		}

		for _, kubeCluster := range kubeClustersToSetup {
			if _, ok := f.ClusterSpec[kubeCluster.ClusterName]; ok {
				continue
			}

			if err := framework.SetupK8SCluster(t, f.KubeType, f.KubeVersion, ymlFilePath, kubeCluster); err != nil {
				t.Fatal(err)
			}

			kubeConfigPath := ymlFilePath + "/" + f.KubeType + "/config_" + kubeCluster.ClusterName
			clusterSpec, err := framework.CreateKubeClusterObject(kubeConfigPath)
			if err != nil {
				t.Fatal(err)
			}

			f.ClusterSpec[kubeCluster.ClusterName] = &clusterSpec
			if err = f.CreateSecretInKubeCluster(kubeCluster.ClusterName); err != nil {
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
