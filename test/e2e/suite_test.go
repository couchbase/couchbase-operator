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
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"github.com/sirupsen/logrus"
)

func collectClusterLogs(t *testing.T, logDir string) {
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
	defer func() { _ = os.Chdir(pwd) }()

	// Collect logs from all clusters defined for this test.
	for index := range f.TestClusters {
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
		return runtime.NumGoroutine() <= expected, nil
	}

	if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
		fmt.Println("WARN: goroutine leak detected:", expected, "vs", runtime.NumGoroutine())

		trace := &bytes.Buffer{}
		profile := pprof.Lookup("goroutine")
		_ = profile.WriteTo(trace, 2)
		fmt.Println(trace.String())
	}
}

func runSuite(t *testing.T) {
	f := framework.Global

	// Over riding pullImage to true since if new cluster is created, images should be pulled
	logrus.Info("Starting suite ", f.SuiteYmlData.SuiteName)

	for _, testGroup := range f.SuiteYmlData.TestCaseGroup {
		// Add the cluster names to the global test clusters so the
		// individual tests can reference them.
		f.TestClusters = testGroup.ClusterName

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

			testFunc = framework.RecoverDecorator(testFunc, decoratorArgs)

			if testFunc == nil {
				continue
			}

			// Either the test or Couchbase Server may suffer from instability so
			// we allow a retry.  This means that we get a better idea of overall
			// pass rates without having to rerun the entire suite and collate the
			// results.
			//
			// Unstable tests will be listed in the suite output so pay attention as
			// these may be bugs that need to be raised or fixed. Secondly do not rely
			// on retries as it makes the tests take longer and costs us more money!
			unstable := false
			pass := false

			for attempt := 0; attempt < f.TestRetries; attempt++ {
				if pass = runTest(t, testName, testFunc); pass {
					if attempt != 0 {
						unstable = true
					}

					break
				}
			}

			// Give real time feedback ...
			if pass {
				fmt.Println("PASS")
			} else {
				fmt.Println("FAIL")
			}

			result := framework.TestResult{
				Name:     testName,
				Result:   pass,
				Unstable: unstable,
			}
			framework.Results = append(framework.Results, result)
		}
	}
}

// getOperatorRestartCounts returns the restart counts for the operator in each
// cluster.
func getOperatorRestartCounts() map[string]int {
	f := framework.Global

	result := map[string]int{}

	for _, cluster := range f.TestClusters {
		k8s := f.ClusterSpec[cluster]

		restarts, err := f.GetOperatorRestartCount(k8s)
		if err != nil {
			fmt.Println("WARN: unable to get restart counts on cluster", cluster, ":", err)
		}

		result[cluster] = int(restarts)
	}

	return result
}

// operatorRestarted returns whether the operator restarted/crashed during this
// test.
func operatorRestarted(before map[string]int) bool {
	after := getOperatorRestartCounts()

	restarted := false

	for cluster := range before {
		if before[cluster] != after[cluster] {
			fmt.Println("WARN: operator crash detected in cluster", cluster)

			restarted = true
		}
	}

	return restarted
}

// runTest runs a named test once, spotting bugs in the operator, the test itself and
// performing cleanup and logging duties.
func runTest(t *testing.T, name string, test func(*testing.T)) bool {
	f := framework.Global

	// Do any pre-test clean up actions
	for clusterName, cluster := range f.ClusterSpec {
		k8s := f.ClusterSpec[clusterName]
		_ = e2eutil.CleanLDAPResources(k8s)
		e2eutil.DeleteSyncGateway(cluster)
	}

	// Run the test, catch and report any goroutine leaks or operator crashes
	restartCounts := getOperatorRestartCounts()
	preGoroutines := runtime.NumGoroutine()
	pass := t.Run(name, test)

	goroutineLeakCheck(preGoroutines)

	if operatorRestarted(restartCounts) {
		pass = false
	}

	// Collect logs.
	if f.CollectLogs && !pass {
		collectClusterLogs(t, f.LogDir+"/"+name)
	}

	// Cleanup the namespace.
	if !f.SkipTeardown {
		for clusterName, cluster := range f.ClusterSpec {
			e2eutil.CleanUpCluster(t, cluster, f.LogDir, clusterName, name)
		}
	}

	return pass
}

func TestOperator(t *testing.T) {
	if err := framework.Setup(t); err != nil {
		t.Fatal("Failed to setup framework: " + err.Error())
	}

	runSuite(t)
	framework.AnalyzeResults(t)
}
