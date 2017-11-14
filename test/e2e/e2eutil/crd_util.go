package e2eutil

import (
	"testing"
	"time"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

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
	err := retryutil.Retry(1*time.Second, maxRetries, func() (done bool, err error) {
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

func DeleteCluster(t *testing.T, crClient versioned.Interface, kubeClient kubernetes.Interface, cl *api.CouchbaseCluster) error {
	t.Logf("deleting couchbase cluster: %v", cl.Name)
	err := crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Delete(cl.Name, nil)
	if err != nil {
		return err
	}
	return waitResourcesDeleted(t, kubeClient, cl)
}

// NameLabelSelector returns a label selector of the form name=<name>
func NameLabelSelector(name string) map[string]string {
	return map[string]string{"name": name}
}
