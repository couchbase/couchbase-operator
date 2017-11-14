package e2e

import (
	"os"
	"testing"
	"time"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
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
