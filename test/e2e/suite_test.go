package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"github.com/sirupsen/logrus"
)

func collectClusterLogs(t *testing.T, testName, logDir string) {
	f := framework.Global

	// Create and move to the log directory.
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Errorf("Failed to create dir %s: %v", logDir, err)
		return
	}
	pwd, err := os.Getwd()
	if err != nil {
		t.Errorf("Failed to get pwd: %v", err)
		return
	}
	if err := os.Chdir(logDir); err != nil {
		t.Errorf("Failed to change directory: %v", err)
	}

	// Move back to where we were regardless of outcome.
	defer os.Chdir(pwd)

	// Collect logs from all clusters defined for this test.
	for index, _ := range f.TestClusters {
		cluster := f.GetCluster(index)

		args := argumentList{}
		args.addClusterDefaults(cluster)
		args.addEnvironmentDefaults()
		args.add("--collectinfo", "")
		args.add("--collectinfo-collect", "all")
		args.add("--system", "")

		execOut, err := runCbopinfoCmd(args.slice())
		execOutStr := strings.TrimSpace(string(execOut))
		if err != nil {
			t.Logf("cbopinfo returned: %s", execOutStr)
			t.Errorf("cbopinfo command failed: %v", err)
		}
	}
}

// goroutineLeakCheck compares the number of goroutines with what we started a test
// with.  If they don't match then raise a warning and print all routines so we can
// ensure they are correctly killed off.  If this triggers, your test is broken, fix
// it!
func goroutineLeakCheck(expected int) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	callback := func() (bool, error) {
		return runtime.NumGoroutine() == expected, nil
	}

	if err := retryutil.Retry(ctx, 5*time.Second, e2eutil.IntMax, callback); err != nil {
		fmt.Println("WARN: goroutine leak detected:", expected, "vs", runtime.NumGoroutine())
		trace := &bytes.Buffer{}
		profile := pprof.Lookup("goroutine")
		profile.WriteTo(trace, 2)
		fmt.Println(string(trace.Bytes()))
	}
}

func runSuite(t *testing.T) {
	f := framework.Global
	reqOpImage := f.Deployment.Spec.Template.Spec.Containers[0].Image
	ymlFilePath := "./resources/ansible"

	// Over riding pullImage to true since if new cluster is created, images should be pulled
	operatorRestartCount := map[string]int32{}
	logrus.Info("Starting suite ", f.SuiteYmlData.SuiteName)

	for _, testGroup := range f.SuiteYmlData.TestCaseGroup {
		// Add the cluster names to the global test clusters so the
		// individual tests can reference them.
		f.TestClusters = testGroup.ClusterName

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

			kubeConfigPath := e2eutil.GetKubeConfigToUse(f.KubeType, kubeCluster.ClusterName)
			clusterSpec, err := framework.CreateKubeClusterObject(kubeConfigPath, "")
			if err != nil {
				skipCurrTestGroup = true
				t.Error(err)
				break
			}

			f.ClusterSpec[kubeName] = clusterSpec

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

			// Override default storage class behaviour with special clusters.
			f.ClusterSpec[kubeName].SupportsMultipleVolumeClaims = kubeCluster.SupportsMultipleVolumeClaims

			if err := framework.SetupPersistentVolume(t, f.ClusterSpec[kubeName].KubeClient, f.Namespace, kubeCluster.ClusterName, kubeCluster.StorageClassType); err != nil {
				skipCurrTestGroup = true
				t.Error(err)
				// Remove the map entry and break the loop
				delete(f.ClusterSpec, kubeName)
				break
			}

			// Add required K8S node labels
			for retryCount := 0; retryCount < constants.Retries5; retryCount++ {
				t.Logf("Retry node label update: %d", retryCount)
				// Label K8S nodes based on the labels present in the cluster conf yaml file
				if err := K8SNodesAddLabel(constants.FailureDomainZoneLabel, clusterSpec.KubeClient, kubeCluster); err == nil {
					break
				} else if retryCount == 2 {
					skipCurrTestGroup = true
					t.Errorf("Failed to label the nodes: %v", err)
					// Remove the map entry and break the loop
					delete(f.ClusterSpec, kubeName)
					break
				}
			}

			if skipCurrTestGroup {
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
				preGoroutines := runtime.NumGoroutine()
				testPassed := t.Run(testName, testFunc)

				// Detect couchbase-operator crash / restart event
				for _, cluster := range f.TestClusters {
					currRestartCount, err := f.GetOperatorRestartCount(f.ClusterSpec[cluster].KubeClient, f.Namespace)
					if err != nil {
						t.Log(err)
					}
					if currRestartCount != operatorRestartCount[cluster] {
						testPassed = false
						t.Logf("Cluster %v restart count was %d and now is %d", cluster, operatorRestartCount[cluster], currRestartCount)
						operatorRestartCount[cluster] = currRestartCount
					}
				}

				// Give real time feedback ...
				if testPassed {
					fmt.Println("PASS")
				} else {
					fmt.Println("FAIL")
				}

				goroutineLeakCheck(preGoroutines)

				// Collect logs if test fails
				if !testPassed && f.CollectLogs {
					logDir := f.LogDir + "/" + testName
					collectClusterLogs(t, testName, logDir)
				}

				// Clean up all known clusters if SkipTeardown is disabled
				if !f.SkipTeardown {
					for kubeName, targetKube := range f.ClusterSpec {
						e2eutil.CleanUpCluster(t, targetKube, f.Namespace, f.LogDir, kubeName, testName)
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
