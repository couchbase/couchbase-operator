package e2eutil

import (
	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewCluster(genName string, size int) *api.CouchbaseCluster {
	return &api.CouchbaseCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       api.CRDResourceKind,
			APIVersion: api.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genName,
		},
		Spec: api.ClusterSpec{
			Size:      size,
			BaseImage: "couchbase/server",
			Version:   "enterprise-4.5.1",
			ClusterSettings: &api.ClusterConfig{
				ClusterAuth:           api.ClusterAuth{"Administrator", "password"},
				Services:              "kv,n1ql,index",
				DataServiceMemQuota:   256,
				IndexServiceMemQuota:  256,
				SearchServiceMemQuota: 256,
				IndexStorageSetting:   "memory_optimized",
				DataPath:              "/opt/couchbase/var/lib/couchbase/data",
				IndexPath:             "/opt/couchbase/var/lib/couchbase/data",
				AutoFailoverTimeout:   30,
			},
			BucketSettings: []api.BucketConfig{},
		},
	}
}

// NameLabelSelector returns a label selector of the form name=<name>
func NameLabelSelector(name string) map[string]string {
	return map[string]string{"name": name}
}
