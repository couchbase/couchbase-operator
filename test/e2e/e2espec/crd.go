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
	basicClusterSettings = api.ClusterConfig{
		DataServiceMemQuota:   256,
		IndexServiceMemQuota:  256,
		SearchServiceMemQuota: 256,
		IndexStorageSetting:   "memory_optimized",
		AutoFailoverTimeout:   30,
	}
)

// bucket settings
var (
	defaultBucketSettings = api.BucketConfig{
		BucketName:         "default",
		BucketType:         "couchbase",
		BucketMemoryQuota:  256,
		BucketReplicas:     1,
		IoPriority:         "high",
		EvictionPolicy:     "fullEviction",
		ConflictResolution: "seqno",
		EnableFlush:        true,
		EnableIndexReplica: false,
	}
)

func defaultServerSettings(size int) []api.ServerConfig {
	return []api.ServerConfig{
		api.ServerConfig{
			Size:      size,
			Name:      "test_config_1",
			Services:  []string{"data", "n1ql", "index"},
			DataPath:  "/opt/couchbase/var/lib/couchbase/data",
			IndexPath: "/opt/couchbase/var/lib/couchbase/data",
		},
	}
}

func NewBasicCluster(genName, secretName string, size int) *api.CouchbaseCluster {
	spec := api.ClusterSpec{
		BaseImage:       baseImage,
		Version:         version,
		AuthSecret:      secretName,
		ClusterSettings: basicClusterSettings,
		BucketSettings:  []api.BucketConfig{},
		ServerSettings:  defaultServerSettings(size),
	}
	return NewClusterCRD(genName, spec)
}

func NewSingleBucketCluster(genName, secretName, bucketName string, size int) *api.CouchbaseCluster {
	bucketSettings := api.BucketConfig(defaultBucketSettings)
	bucketSettings.BucketName = bucketName

	spec := api.ClusterSpec{
		BaseImage:       baseImage,
		Version:         version,
		AuthSecret:      secretName,
		ClusterSettings: basicClusterSettings,
		BucketSettings:  []api.BucketConfig{bucketSettings},
		ServerSettings:  defaultServerSettings(size),
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
