package e2e

import (
	"fmt"
	"os"
	"testing"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbaselabs/couchbase-operator/test/e2e/framework"
)

func TestResizeClusterUp(t *testing.T) {
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
		cl.Spec.Size = 5
	}
	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, 10, updateFunc); err != nil {
		t.Fatal(err)
	}
	fmt.Println("scaling up")

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 5, 6, testCouchbase); err != nil {
		t.Fatalf("failed to resize to 5 members couchbase cluster: %v", err)
	}
	fmt.Println("successfully scaled up from 3 to 5 members in the cluster")
}

func TestResizeClusterUpNegative(t *testing.T) {

}

func TestResizeClusterDown(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	secret, err := e2eutil.CreateSecret(t, f.KubeClient, f.Namespace, e2espec.NewBasicSecret(f.Namespace))
	if err != nil {
		t.Fatal(err)
	}
	testCouchbase, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2espec.NewBasicCluster("test-couchbase-", secret.Name, 5))
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

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 5, 18, testCouchbase); err != nil {
		t.Fatalf("failed to create 5 members couchbase cluster: %v", err)
	}

	fmt.Println("initialized 5 members in the cluster")

	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.Size = 3
	}
	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, 10, updateFunc); err != nil {
		t.Fatal(err)
	}
	fmt.Println("scaling down")

	if _, err := e2eutil.WaitUntilSizeReached(t, f.CRClient, 3, 18, testCouchbase); err != nil {
		t.Fatalf("failed to resize to 3 members couchbase cluster: %v", err)
	}
	fmt.Println("successfully scaled down from 5 to 3 members in the cluster")
}

func TestResizeClusterDownNegative(t *testing.T) {

}
