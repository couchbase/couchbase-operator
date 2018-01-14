package e2e

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"testing"
	"time"
)

// TestPauseControl tests the user can pause the operator from controlling
// an couchbase cluster.
func TestPauseOperator(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 3, false, false)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents := e2eutil.EventList{}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, 3, 15, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create 3 members couchbase cluster: %v", err)
	}

	t.Logf("Pausing operator...")
	testCouchbase, err = e2eutil.UpdateClusterSpec("Paused", "true", f.CRClient, testCouchbase, 10)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// TODO: this is used to wait for the CR to be updated.
	// TODO: make this wait for reliable
	time.Sleep(5 * time.Second)

	t.Logf("Killing pod...")
	e2eutil.KillPods(t, f.KubeClient, testCouchbase, 1)
	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 2, 3, testCouchbase); err != nil {
		t.Fatalf("failed to wait for killed member to die: %v", err)
	}
	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 10, testCouchbase); err == nil {
		t.Fatalf("cluster should not be recovered: control is paused")
	}
	t.Logf("Resuming operator...")
	testCouchbase, err = e2eutil.UpdateClusterSpec("Paused", "false", f.CRClient, testCouchbase, 10)
	if err != nil {
		t.Fatalf("failed to resume control: %v", err)
	}
	t.Logf("Waiting for recovery...")
	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 3, 20, testCouchbase); err != nil {
		t.Fatalf("failed to resize to 3 members couchbase cluster: %v", err)
	}

	event = e2eutil.NewMemberAddEvent(testCouchbase, 3)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)

	//verify events
	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
	}
}

func TestKillOperator(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 3, false, false)
	defer e2eutil.DestroyCluster(t, f.KubeClient, f.CRClient, f.Namespace, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 1)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)

	event = e2eutil.NewMemberAddEvent(testCouchbase, 2)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)

	event = k8sutil.RebalanceEvent(testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddRebalanceEvent(testCouchbase)

	_, err = e2eutil.WaitUntilSizeReached(t, f.CRClient, 3, 15, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create 3 members couchbase cluster: %v", err)
	}
	t.Logf("Killing operator...")
	err = e2eutil.DeleteCouchbaseOperator(f.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("failed to kill couchbase operator: %v", err)
	}
	t.Logf("Operator killed...")
	t.Logf("Waiting for operator to recover...")
	err = e2eutil.WaitUntilOperatorReady(f.KubeClient, f.Namespace, "couchbase-operator")
	if err != nil {
		t.Fatalf("failed to recover couchbase operator: %v", err)
	}
	t.Logf("Operator recovered...")

	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
	}
}

func TestKillOperatorAndUpdateClusterConfig(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true, false)
	defer e2eutil.DestroyCluster(t, f.KubeClient, f.CRClient, f.Namespace, testCouchbase)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents := e2eutil.EventList{}

	event := e2eutil.NewMemberAddEvent(testCouchbase, 0)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	event = k8sutil.BucketCreateEvent("default", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	service, err := e2eutil.CreateService(t, f.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.DeleteService(t, f.KubeClient, f.Namespace, service.Name, nil)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, 5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// make a bucket spec with flush disabled
	t.Logf("externally changing bucket flush to: false")
	bucket, err := e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		disabled := false
		b.EnableFlush = &disabled
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			if bucket.EnableFlush != nil {
				t.Logf("enabled bucket flush: %t", *bucket.EnableFlush)
				return *bucket.EnableFlush
			}
		}
		return false
	}

	t.Logf("Killing operator and changing bucket flush from enabled to disabled...")
	err = e2eutil.DeleteCouchbaseOperator(f.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("failed to kill couchbase operator: %v", err)
	}

	err = e2eutil.EditBucketAndVerify(t, client, bucket, 5, e2eutil.FlushDisabledVerifier)
	if err != nil {
		t.Fatalf("error occurred editing cluster bucket %v", err)
	}
	if _, allowed := err.(errors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket flush: %v", err)
	}

	t.Logf("Waiting for operator to recover...")
	err = e2eutil.WaitUntilOperatorReady(f.KubeClient, f.Namespace, "couchbase-operator")
	if err != nil {
		t.Fatalf("failed to recover couchbase operator: %v", err)
	}
	t.Logf("Operator recovered...")
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to enable bucket flush %v", err)
	}
	t.Logf("Bucket settings reverted...")

	event = k8sutil.BucketEditEvent("default", testCouchbase)
	err = e2eutil.WaitForClusterEvent(f.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	err = e2eutil.WaitUntilEventsCompare(t, f.KubeClient, 6, testCouchbase, expectedEvents, f.Namespace)
	if err != nil {
		t.Fatalf("compare events failed: %v", err)
	}
}
