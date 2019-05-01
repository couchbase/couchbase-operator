package e2eutil

import (
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func CreateCluster(t *testing.T, crClient versioned.Interface, namespace string, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	cl.Namespace = namespace
	res, err := k8sutil.CreateCouchbaseCluster(crClient, cl)
	if err != nil {
		return res, err
	}
	t.Logf("creating couchbase cluster: %s", res.Name)

	return res, nil
}

func DeleteCluster(t *testing.T, crClient versioned.Interface, kubeClient kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, retries int) error {
	t.Logf("deleting couchbase cluster: %v", cl.Name)
	err := k8sutil.DeleteCouchbaseCluster(crClient, cl)
	if err != nil {
		return err
	}
	return waitResourcesDeleted(t, kubeClient, cl, retries)
}

func getClusterCRD(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Get(cl.Name, metav1.GetOptions{})
}

// NameLabelSelector returns a label selector of the form name=<name>
func NameLabelSelector(label, name string) map[string]string {
	return map[string]string{label: name}
}
