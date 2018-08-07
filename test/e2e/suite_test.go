package e2e

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"github.com/sirupsen/logrus"
)

func collectClusterLogs(t *testing.T, kubeClustersToSetup []framework.ClusterInfo, namespace, testName, logDir string) {
	for _, kubeCluster := range kubeClustersToSetup {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			t.Errorf("Failed to create dir %s: %v", logDir, err)
			continue
		}

		kubeConfPath := framework.GetKubeConfigToUse(kubeCluster.ClusterName)
		cmdArgs := []string{"-kubeconfig", kubeConfPath, "-namespace", namespace, "-collectinfo"}
		execOut, err := runCbopinfoCmd(cmdArgs)
		execOutStr := strings.TrimSpace(string(execOut))
		if err != nil {
			t.Logf("cbopinfo returned: %s", execOutStr)
			t.Errorf("cbopinfo command failed: %v", err)
		}

		logFileName := getLogFileNameFromExecOutput(execOutStr)
		if err := os.Rename(logFileName, logDir+"/"+logFileName); err != nil {
			t.Errorf("Failed to move log file: %v", err)
		}

		collectInfoOutputPattern := regexp.MustCompile("(kubectl cp [-+:/a-zA-Z0-9 ]+\\.zip \\.)")
		cbCollectCmdList := collectInfoOutputPattern.FindAllString(execOutStr, -1)
		for _, cbCollectCmd := range cbCollectCmdList {
			cmdArgs := strings.Split(cbCollectCmd, " ")
			cmdArgs = append(cmdArgs, "--kubeconfig")
			cmdArgs = append(cmdArgs, kubeConfPath)
			cmdArgs = append(cmdArgs, "--namespace")
			cmdArgs = append(cmdArgs, namespace)
			cmdOutput, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput()
			cmdOutputStr := strings.TrimSpace(string(cmdOutput))
			if err != nil {
				t.Logf("kubectl returned: %s", cmdOutputStr)
				t.Errorf("Failed to fetch couchbase log: %v", err)
			}
			cbFileNamePath := strings.Split(cmdArgs[len(cmdArgs)-6], "/")
			cbFileName := cbFileNamePath[len(cbFileNamePath)-1]
			if err := os.Rename(cbFileName, logDir+"/"+cbFileName); err != nil {
				t.Errorf("Failed to move cb log file: %v", err)
			}
		}
	}
}

func runSuite(t *testing.T) {
	f := framework.Global
	reqOpImage := f.Deployment.Spec.Template.Spec.Containers[0].Image
	ymlFilePath := "./resources/ansible"
	var operatorRestartCount int32 = 0
	logrus.Info("Starting suite ", f.SuiteYmlData.SuiteName)

	for _, testGroup := range f.SuiteYmlData.TestCaseGroup {
		requiredClusters := []string{}
		for _, currClusterName := range testGroup.ClusterName {
			if !framework.ElementExistsInArr(currClusterName, requiredClusters) {
				requiredClusters = append(requiredClusters, currClusterName)
			}
		}

		kubeClustersToSetup, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, requiredClusters)
		if err != nil {
			logrus.Info("Skipping ", testGroup.GroupName, ".. ", err.Error())
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

			kubeConfigPath := os.Getenv("HOME") + "/.kube/config_" + kubeCluster.ClusterName
			clusterSpec, err := framework.CreateKubeClusterObject(kubeConfigPath)
			if err != nil {
				t.Fatal(err)
			}

			f.ClusterSpec[kubeName] = &clusterSpec

			logrus.Info("Waiting for Kube nodes to become available")
			if err = e2eutil.WaitForKubeNodesToBeReady(t, f.ClusterSpec[kubeName].KubeClient, 600); err != nil {
				t.Fatal(err)
			}

			if err := f.SetupFramework(kubeName); err != nil {
				t.Fatalf("Failed to setup framework: %v", err)
			}
		}

		for _, currTestCase := range testGroup.TestCase {
			testName := currTestCase.TcName

			if _, ok := TestFuncMap[testName]; !ok {
				t.Logf("Skipping %s.. Undefined test", testName)
				continue
			}
			testFunc := TestFuncMap[testName]
			decoratorArgs := framework.DecoratorArgs{
				KubeNames: testGroup.ClusterName,
			}
			for _, funcName := range currTestCase.Decorators {
				if _, ok := DecoratorFuncMap[funcName]; !ok {
					t.Logf("Skipping %s.. Undefined decorator %s", testName, funcName)
					testFunc = nil
					break
				}

				testFunc = DecoratorFuncMap[funcName](testFunc, decoratorArgs)
			}

			testFunc = DecoratorFuncMap["recoverDecorator"](testFunc, decoratorArgs)

			if testFunc != nil {
				testPassed := t.Run(testName, testFunc)

				// Detect couchbase-operator crash / restart event
				for _, kubeCluster := range kubeClustersToSetup {
					targetKube := f.ClusterSpec[kubeCluster.ClusterName]
					if currRestartCount := f.GetOperatorRestartCount(targetKube.KubeClient, f.Namespace); currRestartCount != operatorRestartCount {
						testPassed = false
						operatorRestartCount = currRestartCount
						t.Logf("Operator pod restart count is %d", operatorRestartCount)
					}
				}

				// Collect logs if test fails
				if !testPassed {
					logDir := f.LogDir + "/" + testName
					collectClusterLogs(t, kubeClustersToSetup, f.Namespace, testName, logDir)
				}

				// Clean up all known clusters if SkipTeardown is disabled
				if !f.SkipTeardown {
					for _, kubeCluster := range kubeClustersToSetup {
						targetKube := f.ClusterSpec[kubeCluster.ClusterName]
						e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					}
				}
				framework.Results = append(framework.Results, framework.TestResult{Name: testName, Result: testPassed})
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
