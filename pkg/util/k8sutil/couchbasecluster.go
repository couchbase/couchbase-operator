package k8sutil

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetCouchbaseCluster(crClient versioned.Interface, namespace, name string) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(namespace).Get(name, metav1.GetOptions{})
}

func CreateCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Create(cl)
}

func DeleteCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) error {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Delete(cl.Name, nil)
}

func UpdateCouchbaseCluster(crClient versioned.Interface, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return crClient.CouchbaseV2().CouchbaseClusters(cl.Namespace).Update(cl)
}
