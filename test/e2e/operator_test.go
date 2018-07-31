package e2e

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"testing"
)

// TestPauseControl tests the user can pause the operator from controlling
// an couchbase cluster.
func TestPauseOperator(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]
	memberIdToKill := 0

	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	_, err = e2eutil.WaitUntilSizeReached(t, targetKube.CRClient, e2eutil.Size3, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create 3 members couchbase cluster: %v", err)
	}

	t.Logf("Pausing operator...")
	testCouchbase, err = e2eutil.UpdateClusterSpec("Paused", "true", targetKube.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	if err := e2eutil.WaitForClusterStatus(t, targetKube.CRClient, "ControlPaused", "true", testCouchbase, 300); err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	t.Logf("Killing pod...")
	e2eutil.KillPods(t, targetKube.KubeClient, testCouchbase, 1)
	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, e2eutil.Size2, e2eutil.Retries10, testCouchbase); err != nil {
		t.Fatalf("failed to wait for killed member to die: %v", err)
	}

	if _, err := e2eutil.WaitUntilPodSizeReached(t, targetKube.KubeClient, e2eutil.Size3, e2eutil.Retries10, testCouchbase); err == nil {
		t.Fatalf("cluster should not be recovered: control is paused")
	}

	t.Logf("Resuming operator...")
	testCouchbase, err = e2eutil.UpdateClusterSpec("Paused", "false", targetKube.CRClient, testCouchbase, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("failed to resume control: %v", err)
	}

	if err := e2eutil.WaitForClusterStatus(t, targetKube.CRClient, "ControlPaused", "false", testCouchbase, 300); err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	expectedEvents.AddMemberFailedOverEvent(testCouchbase, memberIdToKill)

	event := e2eutil.NewMemberAddEvent(testCouchbase, 3)
	if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 120); err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddMemberRemoveEvent(testCouchbase, memberIdToKill)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}
	ValidateClusterEvents(t, targetKube.KubeClient, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestKillOperator(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
	if err != nil {
		t.Fatalf("failed to create 3 members couchbase cluster: %v", err)
	}
	t.Logf("Killing operator...")
	err = e2eutil.DeleteCouchbaseOperator(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("failed to kill couchbase operator: %v", err)
	}
	t.Logf("Operator killed...")
	t.Logf("Waiting for operator to recover...")
	err = e2eutil.WaitUntilOperatorReady(targetKube.KubeClient, f.Namespace, "couchbase-operator")
	if err != nil {
		t.Fatalf("failed to recover couchbase operator: %v", err)
	}
	t.Logf("Operator recovered...")

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestKillOperatorAndUpdateClusterConfig(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	kubeName := "BasicCluster"
	targetKube := f.ClusterSpec[kubeName]

	testCouchbase, err := e2eutil.NewClusterBasic(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, targetKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), targetKube.KubeClient, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// make a bucket spec with flush disabled
	t.Logf("externally changing bucket flush to: false")
	bucket, err := e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		b.EnableFlush = constants.BucketFlushDisabled
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			t.Logf("enabled bucket flush: %t", bucket.EnableFlush)
			return bucket.EnableFlush == constants.BucketFlushEnabled
		}
		return false
	}

	t.Logf("Killing operator and changing bucket flush from enabled to disabled...")
	err = e2eutil.DeleteCouchbaseOperator(targetKube.KubeClient, f.Namespace)
	if err != nil {
		t.Fatalf("failed to kill couchbase operator: %v", err)
	}

	err = e2eutil.EditBucketAndVerify(t, client, bucket, e2eutil.Retries5, e2eutil.FlushDisabledVerifier)
	if err != nil {
		t.Fatalf("error occurred editing cluster bucket %v", err)
	}
	if _, allowed := err.(errors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket flush: %v", err)
	}

	t.Logf("Waiting for operator to recover...")
	err = e2eutil.WaitUntilOperatorReady(targetKube.KubeClient, f.Namespace, "couchbase-operator")
	if err != nil {
		t.Fatalf("failed to recover couchbase operator: %v", err)
	}
	t.Logf("Operator recovered...")
	if err := e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to enable bucket flush %v", err)
	}
	t.Logf("Bucket settings reverted...")

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	event := k8sutil.BucketEditEvent("default", testCouchbase)
	err = e2eutil.WaitForClusterEvent(targetKube.KubeClient, testCouchbase, event, 300)
	if err != nil {
		t.Fatal(err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(targetKube.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}
