package e2e

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"testing"
)

func TestBucketAddRemove(t *testing.T) {
	t.Skip("This test is still being worked on")

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	bucketTypes := []string{"couchbase", "memcached", "ephemeral"} // couchbase, memcached, ephemeral
	bucketSettingsList := e2espec.GenerateValidBucketSettings(bucketTypes)
	for _, bucketSetting := range bucketSettingsList {
		expectedEvents := e2eutil.EventList{}
		t.Logf("Creating New Couchbase Cluster...\n")
		testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 3, false)
		if err != nil {
			t.Fatal(err)
		}
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

		expectedEvents.AddMemberAddEvent(testCouchbase, 0)
		expectedEvents.AddMemberAddEvent(testCouchbase, 1)
		expectedEvents.AddMemberAddEvent(testCouchbase, 2)
		expectedEvents.AddRebalanceEvent(testCouchbase)

		newConfig := []api.BucketConfig{bucketSetting}

		// add bucket
		t.Logf("Desired Bucket Properties: %v\n", newConfig)
		updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = newConfig }
		t.Logf("Adding Bucket To Cluster \n")
		testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 20, updateFunc)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("Waiting For Bucket To Be Created \n")
		err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{bucketSetting.BucketName}, 30, testCouchbase)
		if err != nil {
			t.Fatalf("failed to create bucket %v", err)
		}
		expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

		// delete bucket
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = []api.BucketConfig{} }
		t.Logf("Removing Bucket From Cluster \n")
		testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 10, updateFunc)
		if err != nil {
			t.Fatal(err)
		}
		err = e2eutil.WaitUntilBucketsNotExists(t, f.CRClient, []string{bucketSetting.BucketName}, 18, testCouchbase)
		if err != nil {
			t.Fatalf("failed to delete bucket %v", err)
		}

		expectedEvents.AddBucketDeleteEvent(testCouchbase, "default")

		events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
		if err != nil {
			t.Fatalf("failed to get coucbase cluster events: %v", err)
		}
		if !expectedEvents.Compare(events) {
			t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
		}

		// delete cluster
		t.Logf("Deleting Cluster \n")
		e2eutil.DestroyCluster(t, f.KubeClient, f.CRClient, f.Namespace, testCouchbase)
	}

}

func TestNegBucketAdd(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	fullEvictionPolicy      := "fullEviction"
	seqnoConflictResolution := "seqno"
	enabled                 := true
	disabled                := false
	bucketSettings := api.BucketConfig{
		BucketName:         "default",
		BucketType:         "ephemeral",
		BucketMemoryQuota:  256,
		BucketReplicas:     1,
		IoPriority:         "high",
		EvictionPolicy:     &fullEvictionPolicy,
		ConflictResolution: &seqnoConflictResolution,
		EnableFlush:        &enabled,
		EnableIndexReplica: &disabled,
	}
	bucketConfig := []api.BucketConfig{bucketSettings}

	// add bucket
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig)
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 20, updateFunc)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Waiting For Bucket To Be Created \n")
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{bucketSettings.BucketName}, 30, testCouchbase)
	if err == nil {
		t.Fatalf("failed to NOT create bucket %v", err)
	}
}

// edit bucket memory quota from 256 to 128
// revert change
// edit bucket replica count from 1 to 2
// revert change
// edit bucket flush policy from true to false
// revert change
func TestEditBucket(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}

	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// change memory quota
	_, err = e2eutil.UpdateBucketSpec("default", "BucketMemoryQuota", "128", f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketMemoryQuota == 128
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket ram quota %v", err)
	}

	// change memory quota back
	_, err = e2eutil.UpdateBucketSpec("default", "BucketMemoryQuota", "256", f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketMemoryQuota == 256
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket ram quota back %v", err)
	}

	// change replica count
	_, err = e2eutil.UpdateBucketSpec("default", "BucketReplicas", "2", f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketReplicas == 2
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket replica count %v", err)
	}

	// change replica count back
	_, err = e2eutil.UpdateBucketSpec("default", "BucketReplicas", "1", f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketReplicas == 1
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket replcia count back %v", err)
	}

	// change eviction policy
	_, err = e2eutil.UpdateBucketSpec("default", "EnableFlush", "false", f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	disabled := false
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.EnableFlush == &disabled
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket flush policy %v", err)
	}

	// change eviction policy back
	_, err = e2eutil.UpdateBucketSpec("default", "EnableFlush", "true", f.CRClient, testCouchbase, 5)
	if err != nil {
		t.Fatal(err)
	}
	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	enabled := true
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.EnableFlush == &enabled
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket flush policy back %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// attempt to change bucket type to ephemeral
func TestNegBucketEdit(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// edit bucket type
	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketType = "ephemeral"
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, 3, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	// verify type did not change
	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketType == "ephemeral"
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 3, testCouchbase, acceptsBucketFunc)
	if _, allowed := err.(cluster.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	// edit memory quota
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketMemoryQuota = 9999
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, 3, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	// verify type did not change
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketMemoryQuota == 256
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 3, testCouchbase, acceptsBucketFunc)
	if _, allowed := err.(cluster.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	// edit conflict resolution
	timestamp := "timestamp"
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].ConflictResolution = &timestamp
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, 3, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	// verify type did not change
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.ConflictResolution == &timestamp
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 3, testCouchbase, acceptsBucketFunc)
	if _, allowed := err.(cluster.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}


	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

// ensure updates to buckets made externally to cluster are reverted
// when values do not match with spec
func TestRevertExternalBucketUpdates(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.CRClient, f.Namespace, f.DefaultSecret.Name, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	service, err := e2eutil.CreateService(t, f.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteService(t, f.KubeClient, f.Namespace, service.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	// bucket should exist with flush enabled
	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			if bucket.EnableFlush != nil {
				t.Logf("enabled bucket flush: %t", *bucket.EnableFlush)
				return *bucket.EnableFlush
			}
		}
		return false
	}

	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to create default bucket with flush enabled %v", err)
	}

	// create connection to couchbase nodes
	serviceUrl, err := e2eutil.NodePortServiceClient(f.ApiServerHost(), service)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{serviceUrl})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	// make a bucket spec with flush disabled
	bucket, err := e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		disabled := false
		b.EnableFlush = &disabled
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	// edit bucket and verify change is reflected in cluster.
	err = e2eutil.EditBucketAndVerify(t, client, bucket, 5, e2eutil.FlushDisabledVerifier)

	if err != nil {
		t.Fatalf("error occurred editing cluster bucket %v", err)
	}

	if _, allowed := err.(cluster.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	// verify that the operator has reverted the changed
	// and re-enabled bucket flush
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to enable bucket flush %v", err)
	}
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

func TestBucketConfigNegative(t *testing.T) {

}
