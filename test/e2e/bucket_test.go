package e2e

import (
	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/cluster"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/framework"
	"os"
	"testing"
)

func TestNegBucketAdd(t *testing.T) {

}

func TestBucketDelete(t *testing.T) {

}

func TestNegBucketDelete(t *testing.T) {

}

// edits memory quota of default bucket

func TestEditBucketMemoryQuota(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, secret, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, testCouchbase, secret)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	fullEvictionPolicy := "fullEviction"
	seqnoConflictResolution := "seqno"
	enabled := true
	disabled := false

	// change memory quota
	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings = []api.BucketConfig{
			api.BucketConfig{
				BucketName:         "default",
				BucketType:         "couchbase",
				BucketMemoryQuota:  128,
				BucketReplicas:     1,
				IoPriority:         "high",
				EvictionPolicy:     &fullEvictionPolicy,
				ConflictResolution: &seqnoConflictResolution,
				EnableFlush:        &enabled,
				EnableIndexReplica: &disabled,
			},
		}

	}
	if testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 10, updateFunc); err != nil {
		t.Fatalf("failed to change memory quota: %v", err)
	}

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
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}

}

func TestBucketConfig(t *testing.T) {

}

// attempt to change bucket type to ephemeral
func TestInvalidBucketSpecUpdate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, secret, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, testCouchbase, secret)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

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
	testCouchbase, secret, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, testCouchbase, secret)

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
