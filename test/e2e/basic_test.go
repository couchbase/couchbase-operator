package e2e

import (
	"os"
	"testing"
	"time"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/cluster"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/framework"
)

func TestCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	secret, err := e2eutil.CreateSecret(t, f.KubeClient, f.Namespace, e2espec.NewBasicSecret(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2espec.NewBasicCluster("test-couchbase-", secret.Name, 3))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testCouchbase); err != nil {
			t.Fatal(err)
		}
		if err := e2eutil.DeleteSecret(t, f.KubeClient, f.Namespace, secret.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 3, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create 3 members couchbase cluster: %v", err)
	}
}

// TestPauseControl tests the user can pause the operator from controlling
// an couchbase cluster.
func TestPauseControl(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	secret, err := e2eutil.CreateSecret(t, f.KubeClient, f.Namespace, e2espec.NewBasicSecret(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2espec.NewBasicCluster("test-couchbase-", secret.Name, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testCouchbase); err != nil {
			t.Fatal(err)
		}
		if err := e2eutil.DeleteSecret(t, f.KubeClient, f.Namespace, secret.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	names, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 3, 15, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create 3 members couchbase cluster: %v", err)
	}

	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = true
	}
	if testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 10, updateFunc); err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// TODO: this is used to wait for the CR to be updated.
	// TODO: make this wait for reliable
	time.Sleep(5 * time.Second)

	if err := e2eutil.KillMembers(f.KubeClient, f.Namespace, names[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 2, 3, testCouchbase); err != nil {
		t.Fatalf("failed to wait for killed member to die: %v", err)
	}
	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 3, testCouchbase); err == nil {
		t.Fatalf("cluster should not be recovered: control is paused")
	}

	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.Paused = false
	}
	if testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 10, updateFunc); err != nil {
		t.Fatalf("failed to resume control: %v", err)
	}

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 3, 15, testCouchbase); err != nil {
		t.Fatalf("failed to resize to 3 members couchbase cluster: %v", err)
	}
}

// creates cluster with single bucket
func TestCreateBucketCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	secret, err := e2eutil.CreateSecret(t, f.KubeClient, f.Namespace, e2espec.NewBasicSecret(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2espec.NewSingleBucketCluster("test-couchbase-", secret.Name, "default", 1))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testCouchbase); err != nil {
			t.Fatal(err)
		}
		if err := e2eutil.DeleteSecret(t, f.KubeClient, f.Namespace, secret.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 1, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create to 1 member couchbase cluster: %v", err)
	}

	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create to default bucket %v", err)
	}

}

// edits memory quota of default bucket
func TestEditBucketMemoryQuota(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	secret, err := e2eutil.CreateSecret(t, f.KubeClient, f.Namespace, e2espec.NewBasicSecret(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2espec.NewSingleBucketCluster("test-couchbase-", secret.Name, "default", 1))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testCouchbase); err != nil {
			t.Fatal(err)
		}
		if err := e2eutil.DeleteSecret(t, f.KubeClient, f.Namespace, secret.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 1, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create to 1 member couchbase cluster: %v", err)
	}

	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create to default bucket %v", err)
	}

	// change memory quota
	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketMemoryQuota = 128
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
}

// attempt to change bucket type to ephemeral
func TestInvalidBucketSpecUpdate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	secret, err := e2eutil.CreateSecret(t, f.KubeClient, f.Namespace, e2espec.NewBasicSecret(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2espec.NewSingleBucketCluster("test-couchbase-", secret.Name, "default", 1))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testCouchbase); err != nil {
			t.Fatal(err)
		}
		if err := e2eutil.DeleteSecret(t, f.KubeClient, f.Namespace, secret.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 1, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create to 1 member couchbase cluster: %v", err)
	}

	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create to default bucket %v", err)
	}

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
}

// ensure updates to buckets made externally to cluster are reverted
// when values do not match with spec
func TestRevertExternalBucketUpdates(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	secret, err := e2eutil.CreateSecret(t, f.KubeClient, f.Namespace, e2espec.NewBasicSecret(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}

	testCouchbase, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2espec.NewSingleBucketCluster("test-couchbase-", secret.Name, "default", 1))
	if err != nil {
		t.Fatal(err)
	}

	service, err := e2eutil.CreateService(t, f.KubeClient, f.Namespace, e2espec.NewNodePortService(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testCouchbase); err != nil {
			t.Fatal(err)
		}
		if err := e2eutil.DeleteSecret(t, f.KubeClient, f.Namespace, secret.Name, nil); err != nil {
			t.Fatal(err)
		}
		if err := e2eutil.DeleteService(t, f.KubeClient, f.Namespace, service.Name, nil); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 1, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create to 1 member couchbase cluster: %v", err)
	}

	// bucket should exist with flush enabled
	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			t.Logf("enabled bucket flush: %t", bucket.EnableFlush)
			return bucket.EnableFlush
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
		b.EnableFlush = false
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
}
