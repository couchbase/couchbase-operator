package e2e

import (
	"fmt"
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

		kubeConfPath := e2eutil.GetKubeConfigToUse(kubeCluster.ClusterName)
		cmdArgs := []string{"-operator-image", framework.Global.OpImage, "-kubeconfig", kubeConfPath, "-namespace", namespace, "-collectinfo"}
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

	// Over riding pullImage to true since if new cluster is created, images should be pulled
	f.PullDockerImage = true
	var operatorRestartCount int32 = 0
	logrus.Info("Starting suite ", f.SuiteYmlData.SuiteName)

	for _, testGroup := range f.SuiteYmlData.TestCaseGroup {
		skipCurrTestGroup := false
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
				skipCurrTestGroup = true
				t.Error(err)
				break
			}

			kubeConfigPath := os.Getenv("HOME") + "/.kube/config_" + kubeCluster.ClusterName
			clusterSpec, err := framework.CreateKubeClusterObject(kubeConfigPath)
			if err != nil {
				skipCurrTestGroup = true
				t.Error(err)
				break
			}

			f.ClusterSpec[kubeName] = &clusterSpec

			totalNodes := len(kubeCluster.MasterNodeList) + len(kubeCluster.WorkerNodeList)
			logrus.Infof("Waiting for '%d' nodes to become available", totalNodes)
			if err := e2eutil.WaitForKubeNodesToBeReady(f.ClusterSpec[kubeName].KubeClient, totalNodes, 600); err != nil {
				skipCurrTestGroup = true
				t.Error(err)
				// Remove the map entry and break the loop
				delete(f.ClusterSpec, kubeName)
				break
			}

			if err := f.SetupFramework(kubeName); err != nil {
				skipCurrTestGroup = true
				t.Errorf("Failed to setup framework: %v", err)
				// Remove the map entry and break the loop
				delete(f.ClusterSpec, kubeName)
				break
			}

			if err := framework.SetupPersistentVolume(t, f.ClusterSpec[kubeName].KubeClient, f.Namespace, kubeCluster.ClusterName, kubeCluster.StorageClassType); err != nil {
				skipCurrTestGroup = true
				t.Error(err)
				// Remove the map entry and break the loop
				delete(f.ClusterSpec, kubeName)
				break
			}
		}

		// Avoid executing particular test group in case of setup failure
		if skipCurrTestGroup {
			continue
		}

		// Do setup procedure for this group
		for _, setupFunc := range testGroup.GroupSetup {
			if _, ok := TestGroupSetupFuncMap[setupFunc]; !ok {
				logrus.Errorf("Function %s not found", setupFunc)
				continue
			}
			logrus.Infof("Running setup %s for %s", setupFunc, testGroup.GroupName)
			//TestGroupSetupFuncMap[setupFunc](t, kubeClustersToSetup)
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
				fmt.Printf("  TestPassed: %v\n", testPassed)

				// Detect couchbase-operator crash / restart event
				for _, targetKube := range f.ClusterSpec {
					if currRestartCount := f.GetOperatorRestartCount(targetKube.KubeClient, f.Namespace); currRestartCount != operatorRestartCount {
						testPassed = false
						operatorRestartCount = currRestartCount
						t.Logf("Operator pod restart count is %d", operatorRestartCount)
					}
				}

				// Collect logs if test fails
				if !testPassed && f.CollectLogs {
					logDir := f.LogDir + "/" + testName
					collectClusterLogs(t, kubeClustersToSetup, f.Namespace, testName, logDir)
				}

				// Clean up all known clusters if SkipTeardown is disabled
				if !f.SkipTeardown {
					for _, targetKube := range f.ClusterSpec {
						e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)
					}
				}
				framework.Results = append(framework.Results, framework.TestResult{Name: testName, Result: testPassed})
			}
		}

		// Do clean procedure for this group
		for _, teardownFunc := range testGroup.GroupTeardown {
			if _, ok := TestGroupSetupFuncMap[teardownFunc]; !ok {
				logrus.Errorf("Function %s not found", teardownFunc)
				continue
			}
			logrus.Infof("Running teardown %s for %s", teardownFunc, testGroup.GroupName)
			//TestGroupSetupFuncMap[teardownFunc](t, kubeClustersToSetup)
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
