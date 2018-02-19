package e2eutil

import (
	"context"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
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
	res, err := crClient.CouchbaseV1beta1().CouchbaseClusters(namespace).Create(cl)
	if err != nil {
		return nil, err
	}
	t.Logf("creating couchbase cluster: %s", res.Name)

	return res, nil
}

func UpdateCluster(crClient versioned.Interface, cl *api.CouchbaseCluster, maxRetries int, updateFunc k8sutil.CouchbaseClusterCRUpdateFunc) (*api.CouchbaseCluster, error) {
	return AtomicUpdateClusterCR(crClient, cl.Name, cl.Namespace, maxRetries, updateFunc)
}

func AtomicUpdateClusterCR(crClient versioned.Interface, name, namespace string, maxRetries int, updateFunc k8sutil.CouchbaseClusterCRUpdateFunc) (*api.CouchbaseCluster, error) {
	result := &api.CouchbaseCluster{}
	err := retryutil.Retry(context.Background(), 1*time.Second, maxRetries, func() (done bool, err error) {
		couchbaseCluster, err := crClient.CouchbaseV1beta1().CouchbaseClusters(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		updateFunc(couchbaseCluster)

		result, err = crClient.CouchbaseV1beta1().CouchbaseClusters(namespace).Update(couchbaseCluster)
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
	err := crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Delete(cl.Name, nil)
	if err != nil {
		return err
	}
	return waitResourcesDeleted(t, kubeClient, cl, retries)
}

func GetClusterCRD(crClient versioned.Interface, cl *api.CouchbaseCluster) (*api.CouchbaseCluster, error) {
	return crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
}

// NameLabelSelector returns a label selector of the form name=<name>
func NameLabelSelector(name string) map[string]string {
	return map[string]string{"name": name}
}
