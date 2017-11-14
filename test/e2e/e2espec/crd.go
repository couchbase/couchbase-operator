package e2espec

import (
	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	baseImage = "couchbase/server"
	version   = "enterprise-5.0.0"
)

// cluster settings
var (
	basicClusterSettings = &api.ClusterConfig{
		Services:              "kv,n1ql,index",
		DataServiceMemQuota:   256,
		IndexServiceMemQuota:  256,
		SearchServiceMemQuota: 256,
		IndexStorageSetting:   "memory_optimized",
		DataPath:              "/opt/couchbase/var/lib/couchbase/data",
		IndexPath:             "/opt/couchbase/var/lib/couchbase/data",
		AutoFailoverTimeout:   30,
	}
)

func NewBasicCluster(genName, secretName string, size int) *api.CouchbaseCluster {
	spec := api.ClusterSpec{
		Size:            size,
		BaseImage:       baseImage,
		Version:         version,
		AuthSecret:      secretName,
		ClusterSettings: basicClusterSettings,
		BucketSettings:  []api.BucketConfig{},
	}
	return NewClusterCRD(genName, spec)
}

func NewClusterCRD(genName string, spec api.ClusterSpec) *api.CouchbaseCluster {
	return &api.CouchbaseCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       api.CRDResourceKind,
			APIVersion: api.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genName,
		},
		Spec: spec,
	}
}
