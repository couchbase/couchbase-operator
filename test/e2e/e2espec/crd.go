package e2espec

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
	s "strings"
)

// other settings
var (
	baseImage               = "couchbase/server"
	version                 = "enterprise-5.0.0"
	fullEvictionPolicy      = "fullEviction"
	seqnoConflictResolution = "seqno"
	enabled                 = true
	disabled                = false
)

// cluster settings
var (
	defaultClusterSettings = api.ClusterConfig{
		DataServiceMemQuota:   256,
		IndexServiceMemQuota:  256,
		SearchServiceMemQuota: 256,
		IndexStorageSetting:   "memory_optimized",
		AutoFailoverTimeout:   30,
	}
)

// bucket settings
var (
	DefaultBucketSettings = api.BucketConfig{
		BucketName:         "default",
		BucketType:         "couchbase",
		BucketMemoryQuota:  256,
		BucketReplicas:     1,
		IoPriority:         "high",
		EvictionPolicy:     &fullEvictionPolicy,
		ConflictResolution: &seqnoConflictResolution,
		EnableFlush:        &enabled,
		EnableIndexReplica: &disabled,
	}
)

// server settings
var (
	defaultServerSettings = api.ServerConfig{
		Size:      1,
		Name:      "test_config_1",
		Services:  []string{"data", "n1ql", "index"},
		DataPath:  "/opt/couchbase/var/lib/couchbase/data",
		IndexPath: "/opt/couchbase/var/lib/couchbase/data",
	}
)

// basic 3 node cluster
func NewBasicCluster(genName, secretName string, size int, withBucket bool) *api.CouchbaseCluster {

	bucketConfig := []api.BucketConfig{}
	if withBucket == true {
		bucketSettings := api.BucketConfig(DefaultBucketSettings)
		bucketSettings.BucketName = "default"
		bucketConfig = []api.BucketConfig{bucketSettings}
	}
	spec := api.ClusterSpec{
		BaseImage:       baseImage,
		Version:         version,
		AuthSecret:      secretName,
		ClusterSettings: defaultClusterSettings,
		BucketSettings:  bucketConfig,
		ServerSettings: []api.ServerConfig{api.ServerConfig{
			Size:      size,
			Name:      "test_config_1",
			Services:  []string{"data", "n1ql", "index"},
			DataPath:  "/opt/couchbase/var/lib/couchbase/data",
			IndexPath: "/opt/couchbase/var/lib/couchbase/data",
		}},
	}
	return NewClusterCRD(genName, spec)
}

// new custom cluster
func NewMultiCluster(genName, secretName string, config map[string]map[string]string) *api.CouchbaseCluster {

	clusterSettings := api.ClusterConfig(defaultClusterSettings)
	bucketConfig := []api.BucketConfig{}
	serverConfig := []api.ServerConfig{}
	for key := range config {
		switch {
		case s.Contains(key, "cluster"):
			for setting := range config[key] {
				switch {
				case setting == "dataServiceMemQuota":
					dataServiceMemQuota, _ := strconv.Atoi(config[key][setting])
					clusterSettings.DataServiceMemQuota = dataServiceMemQuota
				case setting == "indexServiceMemQuota":
					indexServiceMemQuota, _ := strconv.Atoi(config[key][setting])
					clusterSettings.IndexServiceMemQuota = indexServiceMemQuota
				case setting == "searchServiceMemQuota":
					searchServiceMemQuota, _ := strconv.Atoi(config[key][setting])
					clusterSettings.SearchServiceMemQuota = searchServiceMemQuota
				case setting == "indexStorageSetting":
					clusterSettings.IndexStorageSetting = config[key][setting]
				case setting == "autoFailoverTimeout":
					autoFailoverTimeout, _ := strconv.ParseUint(config[key][setting], 0, 64)
					clusterSettings.AutoFailoverTimeout = autoFailoverTimeout
				}
			}
		case s.Contains(key, "bucket"):
			bucketSettings := api.BucketConfig(DefaultBucketSettings)
			for setting := range config[key] {
				switch {
				case setting == "bucketName":
					bucketSettings.BucketName = config[key][setting]
				case setting == "bucketType":
					bucketSettings.BucketType = config[key][setting]
				case setting == "bucketMemoryQuota":
					bucketMemoryQuota, _ := strconv.Atoi(config[key][setting])
					bucketSettings.BucketMemoryQuota = bucketMemoryQuota
				case setting == "bucketReplicas":
					bucketReplicas, _ := strconv.Atoi(config[key][setting])
					bucketSettings.BucketReplicas = bucketReplicas
				case setting == "ioPriority":
					bucketSettings.IoPriority = config[key][setting]
				case setting == "evictionPolicy":
					policy := config[key][setting]
					bucketSettings.EvictionPolicy = &policy
				case setting == "conflictResolution":
					confResoultion := config[key][setting]
					bucketSettings.ConflictResolution = &confResoultion
				case setting == "enableFlush":
					enableFlush, _ := strconv.ParseBool(config[key][setting])
					bucketSettings.EnableFlush = &(enableFlush)
				case setting == "enableIndexReplica":
					enableIndexReplica, _ := strconv.ParseBool(config[key][setting])
					bucketSettings.EnableIndexReplica = &(enableIndexReplica)
				}
			}
			bucketConfig = append(bucketConfig, bucketSettings)
		case s.Contains(key, "service"):
			serverSettings := api.ServerConfig(defaultServerSettings)
			for setting := range config[key] {
				switch {
				case setting == "name":
					serverSettings.Name = config[key][setting]
				case setting == "size":
					size, _ := strconv.Atoi(config[key][setting])
					serverSettings.Size = size
				case setting == "services":
					services := []string{}
					parsedServices := s.Split(config[key][setting], ",")
					for _, service := range parsedServices {
						services = append(services, service)
					}
					serverSettings.Services = services
				case setting == "dataPath":
					serverSettings.DataPath = config[key][setting]
				case setting == "indexPath":
					serverSettings.IndexPath = config[key][setting]
				}
			}
			serverConfig = append(serverConfig, serverSettings)
		}
	}
	spec := api.ClusterSpec{
		BaseImage:       baseImage,
		Version:         version,
		AuthSecret:      secretName,
		ClusterSettings: clusterSettings,
		BucketSettings:  bucketConfig,
		ServerSettings:  serverConfig,
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
