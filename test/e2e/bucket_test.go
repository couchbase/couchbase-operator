package e2e

import (
	"os"
	"testing"
	"fmt"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/framework"
)

func TestBucketAdd(t *testing.T) {
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

	fmt.Println("initialized 3 members in the cluster")

	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings = []api.BucketConfig{
			api.BucketConfig{
				BucketName: "default",
				BucketType: "couchbase",
				BucketMemoryQuota: 128,
				BucketReplicas: 1,
				IoPriority: "high",
				EvictionPolicy: "fullEviction",
				ConflictResolution: "sequence",
				EnableFlush: true,
				EnableIndexReplica: false,
			},
		}
	}

	if testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, 10, updateFunc); err != nil {
		t.Fatalf("failed to update: %v", err)
	}

	fmt.Println("adding bucket")
}

func TestBucketAddNegative(t *testing.T) {

}

func TestBucketDelete(t *testing.T) {

}

func TestBucketDeleteNegative(t *testing.T) {

}

func TestBucketConfig(t *testing.T) {

}

func TestBucketConfigNegative(t *testing.T) {

}

