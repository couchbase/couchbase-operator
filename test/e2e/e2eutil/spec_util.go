package e2eutil

import (
	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewCluster(genName, secretName string, size int) *api.CouchbaseCluster {
	return &api.CouchbaseCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       api.CRDResourceKind,
			APIVersion: api.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genName,
		},
		Spec: api.ClusterSpec{
			Size:       size,
			BaseImage:  "couchbase/server",
			Version:    "enterprise-5.0.0",
			AuthSecret: secretName,
			ClusterSettings: &api.ClusterConfig{
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

func NewSecret(namespace, name string, data map[string][]byte) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: data,
	}
}

func BasicSecret(namespace string) *v1.Secret {
	data := map[string][]byte{
		"username": []byte("Administrator"),
		"password": []byte("password"),
	}
	return NewSecret(namespace, "basic-test-secret", data)
}

// NameLabelSelector returns a label selector of the form name=<name>
func NameLabelSelector(name string) map[string]string {
	return map[string]string{"name": name}
}
