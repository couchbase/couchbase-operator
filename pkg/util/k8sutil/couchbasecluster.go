package k8sutil

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetCouchbaseCluster(crClient versioned.Interface, namespace, name string) (*api.CouchbaseCluster, error) {
	return crClient.CouchbaseV1().CouchbaseClusters(namespace).Get(name, metav1.GetOptions{})
}

func CreateCouchbaseCluster(crClient versioned.Interface, cl *api.CouchbaseCluster) (*api.CouchbaseCluster, error) {
	return crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Create(cl)
}

func DeleteCouchbaseCluster(crClient versioned.Interface, cl *api.CouchbaseCluster) error {
	return crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Delete(cl.Name, nil)
}

func UpdateCouchbaseCluster(crClient versioned.Interface, cl *api.CouchbaseCluster) (*api.CouchbaseCluster, error) {
	return crClient.CouchbaseV1().CouchbaseClusters(cl.Namespace).Update(cl)
}
