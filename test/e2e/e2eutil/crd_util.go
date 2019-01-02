package e2eutil

import (
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateClusterWithCRD(t *testing.T, crClient versioned.Interface, crd *api.CouchbaseCluster, namespace, secretName string, size int, withBucket bool) (*api.CouchbaseCluster, error) {
	testCouchbase, err := CreateCluster(t, crClient, namespace, crd)
	if err != nil {
		return nil, err
	}
	_, err = WaitUntilSizeReached(t, crClient, size, 18, testCouchbase)
	if err != nil {
		return nil, err
	}
	if withBucket == true {
		err = WaitUntilBucketsExists(t, crClient, crd.Spec.BucketNames(), 18, testCouchbase)
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

func UpdateCluster(crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int, updateFunc k8sutil.CouchbaseClusterCRUpdateFunc) (*api.CouchbaseCluster, error) {
	return AtomicUpdateClusterCR(crClient, cl.Name, cl.Namespace, maxRetries, updateFunc)
}

func MustUpdateCluster(t *testing.T, crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int, updateFunc k8sutil.CouchbaseClusterCRUpdateFunc) *api.CouchbaseCluster {
	cluster, err := UpdateCluster(crClient, cl, maxRetries, updateFunc)
	if err != nil {
		Die(t, err)
	}
	return cluster
}

func AtomicUpdateClusterCR(crClient versioned.Interface, name, namespace string, maxRetries int, updateFunc k8sutil.CouchbaseClusterCRUpdateFunc) (*api.CouchbaseCluster, error) {
	result := &api.CouchbaseCluster{}
	err := retryutil.Retry(Context, 1*time.Second, maxRetries, func() (done bool, err error) {
		couchbaseCluster, err := crClient.CouchbaseV1().CouchbaseClusters(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		updateFunc(couchbaseCluster)

		result, err = crClient.CouchbaseV1().CouchbaseClusters(namespace).Update(couchbaseCluster)
		if err != nil {
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return result, err
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
