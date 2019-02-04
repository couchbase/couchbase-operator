package e2eutil

import (
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateClusterWithCRD(t *testing.T, k8s *types.Cluster, crd *api.CouchbaseCluster, namespace, secretName string, size int, withBucket bool) (*api.CouchbaseCluster, error) {
	testCouchbase, err := CreateCluster(t, k8s.CRClient, namespace, crd)
	if err != nil {
		return nil, err
	}
	if err := WaitClusterStatusHealthy(t, k8s, crd, 5*time.Minute); err != nil {
		return nil, err
	}
	if withBucket == true {
		err = WaitUntilBucketsExists(t, k8s.CRClient, crd.Spec.BucketNames(), 18, testCouchbase)
		if err != nil {
			return nil, err
		}
	}
	return testCouchbase, nil
}

func CreateCluster(t *testing.T, crClient versioned.Interface, namespace string, cl *api.CouchbaseCluster) (*api.CouchbaseCluster, error) {
	cl.Namespace = namespace
	res, err := k8sutil.CreateCouchbaseCluster(crClient, cl)
	if err != nil {
		return res, err
	}
	t.Logf("creating couchbase cluster: %s", res.Name)

	return res, nil
}

func DeleteCluster(t *testing.T, crClient versioned.Interface, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster, retries int) error {
	t.Logf("deleting couchbase cluster: %v", cl.Name)
	err := k8sutil.DeleteCouchbaseCluster(crClient, cl)
	if err != nil {
		return err
	}
	return waitResourcesDeleted(t, kubeClient, cl, retries)
}

func getClusterCRD(crClient versioned.Interface, cl *api.CouchbaseCluster) (*api.CouchbaseCluster, error) {
	return crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
}

// NameLabelSelector returns a label selector of the form name=<name>
func NameLabelSelector(label, name string) map[string]string {
	return map[string]string{label: name}
}
