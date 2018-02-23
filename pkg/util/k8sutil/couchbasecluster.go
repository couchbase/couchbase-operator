package k8sutil

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
)

func CreateCouchbaseCluster(crClient versioned.Interface, cl *api.CouchbaseCluster) (*api.CouchbaseCluster, error) {
	res, err := crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Create(cl)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func DeleteCouchbaseCluster(crClient versioned.Interface, cl *api.CouchbaseCluster) error {
	return crClient.CouchbaseV1beta1().CouchbaseClusters(cl.Namespace).Delete(cl.Name, nil)
}
