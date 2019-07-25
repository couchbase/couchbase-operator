package e2espec

import (
	"sort"
	"strconv"
	"strings"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// cluster settings
var (
	defaultClusterSettings = api.ClusterConfig{
		DataServiceMemQuota:                    e2e_constants.Mem256Mb,
		IndexServiceMemQuota:                   e2e_constants.Mem256Mb,
		SearchServiceMemQuota:                  e2e_constants.Mem256Mb,
		EventingServiceMemQuota:                e2e_constants.Mem256Mb,
		AnalyticsServiceMemQuota:               e2e_constants.Mem1Gb,
		IndexStorageSetting:                    constants.IndexStorageModeMemoryOptimized,
		AutoFailoverTimeout:                    30,
		AutoFailoverMaxCount:                   1,
		AutoFailoverOnDataDiskIssuesTimePeriod: 120,
	}
)

// bucket settings
var (
	DefaultBucketSettings = api.BucketConfig{
		BucketName:         "default",
		BucketType:         constants.BucketTypeCouchbase,
		BucketMemoryQuota:  e2e_constants.Mem256Mb,
		BucketReplicas:     constants.BucketReplicasOne,
		IoPriority:         constants.BucketIoPriorityHigh,
		EvictionPolicy:     constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: constants.BucketIndexReplicasDisabled,
	}
)

// server settings
var (
	defaultServerSettings = api.ServerConfig{
		Size: e2e_constants.Size1,
		Name: "test_config_1",
		Services: api.ServiceList{
			api.DataService,
			api.QueryService,
			api.IndexService,
		},
	}
)

func SetCbBaseImage(baseImageName string) {
	if baseImageName = strings.TrimSpace(baseImageName); baseImageName != "" {
		e2e_constants.CbServerBaseImage = baseImageName
	}
}

func SetCbImageVersion(cbImgVer string) {
	if cbImgVer = strings.TrimSpace(cbImgVer); cbImgVer != "" {
		e2e_constants.CbServerVersion = cbImgVer
	}
}

var storageClassName string

func SetStorageClassName(storageClassNameIn string) {
	if storageClassNameIn = strings.TrimSpace(storageClassNameIn); storageClassNameIn != "" {
		storageClassName = storageClassNameIn
	}
}

func GetCouchbaseDockerImgName() string {
	return e2e_constants.CbServerBaseImage + ":" + e2e_constants.CbServerVersion
}

func GenerateValidBucketSettings(bucketTypes []string) []api.BucketConfig {
	generatedSettings := []api.BucketConfig{}
	for _, bucketType := range bucketTypes {
		switch {
		case bucketType == constants.BucketTypeCouchbase:
			bucketMemoryQuotas := []int{e2e_constants.Mem256Mb}
			bucketReplicas := []int{1}
			ioPriorities := []string{constants.BucketIoPriorityHigh}
			evictionPolicies := []string{constants.BucketEvictionPolicyFullEviction}
			conflictResolutions := []string{constants.BucketConflictResolutionSeqno, constants.BucketConflictResolutionTimestamp}
			enableFlushes := []bool{constants.BucketFlushEnabled}
			enableIndexReplicas := []bool{constants.BucketIndexReplicasEnabled}
			for _, bucketMemoryQuota := range bucketMemoryQuotas {
				for _, bucketReplica := range bucketReplicas {
					for _, ioPriority := range ioPriorities {
						for _, evictionPolicy := range evictionPolicies {
							for _, conflictResolution := range conflictResolutions {
								for _, enableFlush := range enableFlushes {
									for _, enableIndexReplica := range enableIndexReplicas {
										bucketSetting := api.BucketConfig{
											BucketName:         "default",
											BucketType:         bucketType,
											BucketMemoryQuota:  bucketMemoryQuota,
											BucketReplicas:     bucketReplica,
											IoPriority:         ioPriority,
											EvictionPolicy:     evictionPolicy,
											ConflictResolution: conflictResolution,
											EnableFlush:        enableFlush,
											EnableIndexReplica: enableIndexReplica,
										}
										generatedSettings = append(generatedSettings, bucketSetting)
									}
								}
							}
						}
					}
				}
			}
		case bucketType == constants.BucketTypeMemcached:
			bucketMemoryQuotas := []int{e2e_constants.Mem256Mb}
			enableFlushes := []bool{true, false}
			for _, bucketMemoryQuota := range bucketMemoryQuotas {
				for _, enableFlush := range enableFlushes {
					bucketSetting := api.BucketConfig{
						BucketName:        "default",
						BucketType:        bucketType,
						BucketMemoryQuota: bucketMemoryQuota,
						EnableFlush:       enableFlush,
					}
					generatedSettings = append(generatedSettings, bucketSetting)
				}
			}
		case bucketType == constants.BucketTypeEphemeral:
			bucketMemoryQuotas := []int{e2e_constants.Mem256Mb}
			bucketReplicas := []int{1}
			ioPriorities := []string{constants.BucketIoPriorityHigh}
			evictionPolicies := []string{constants.BucketEvictionPolicyNoEviction, constants.BucketEvictionPolicyNRUEviction}
			conflictResolutions := []string{constants.BucketConflictResolutionSeqno, constants.BucketConflictResolutionTimestamp}
			enableFlushes := []bool{constants.BucketFlushEnabled, constants.BucketFlushDisabled}
			for _, bucketMemoryQuota := range bucketMemoryQuotas {
				for _, bucketReplica := range bucketReplicas {
					for _, ioPriority := range ioPriorities {
						for _, evictionPolicy := range evictionPolicies {
							for _, conflictResolution := range conflictResolutions {
								for _, enableFlush := range enableFlushes {
									bucketSetting := api.BucketConfig{
										BucketName:         "default",
										BucketType:         bucketType,
										BucketMemoryQuota:  bucketMemoryQuota,
										BucketReplicas:     bucketReplica,
										IoPriority:         ioPriority,
										EvictionPolicy:     evictionPolicy,
										ConflictResolution: conflictResolution,
										EnableFlush:        enableFlush,
									}
									generatedSettings = append(generatedSettings, bucketSetting)
								}
							}
						}
					}
				}
			}
		}
	}

	return generatedSettings
}

// imagePullSecret to use to apply to pods.  Ignored if empty.
var imagePullSecret string

// SetImagePullSecret sets the privaye image pull secret for this module.
// TODO: globals are banned!!
func SetImagePullSecret(s string) {
	imagePullSecret = s
}

// ApplyImagePullSecret adds an image pull secret to all the Couchbase server pods so that
// they can use private repositories.
func ApplyImagePullSecret(cluster *api.CouchbaseCluster) {
	if imagePullSecret != "" {
		for i := range cluster.Spec.ServerSettings {
			if cluster.Spec.ServerSettings[i].Pod == nil {
				cluster.Spec.ServerSettings[i].Pod = &api.PodPolicy{}
			}
			cluster.Spec.ServerSettings[i].Pod.ImagePullSecrets = []v1.LocalObjectReference{
				{
					Name: imagePullSecret,
				},
			}
		}
	}
}

// basic 3 node cluster
func NewBasicCluster(genName, secretName string, size int, withBucket bool, exposed bool) *api.CouchbaseCluster {
	bucketConfig := []api.BucketConfig{}
	if withBucket {
		bucketSettings := DefaultBucketSettings
		bucketSettings.BucketName = "default"
		bucketConfig = []api.BucketConfig{bucketSettings}
	}
	spec := api.ClusterSpec{
		BaseImage:       e2e_constants.CbServerBaseImage,
		Version:         e2e_constants.CbServerVersion,
		AuthSecret:      e2e_constants.KubeTestSecretName,
		ClusterSettings: defaultClusterSettings,
		BucketSettings:  bucketConfig,
		ServerSettings: []api.ServerConfig{api.ServerConfig{
			Size: size,
			Name: "test_config_1",
			Services: api.ServiceList{
				api.DataService,
				api.QueryService,
				api.IndexService,
			},
		}},
		ExposedFeatures: []string{},
	}
	crd := NewClusterCRD(genName, spec)
	if exposed {
		crd.Spec.ExposeAdminConsole = true
		crd.Spec.AdminConsoleServices = api.ServiceList{
			api.DataService,
		}
	}
	return crd
}

func NewBasicClusterSpec(size int, bucket, console bool) *api.CouchbaseCluster {
	bucketConfig := []api.BucketConfig{}
	if bucket {
		bucketSettings := DefaultBucketSettings
		bucketSettings.BucketName = "default"
		bucketConfig = []api.BucketConfig{bucketSettings}
	}
	spec := api.ClusterSpec{
		BaseImage:       e2e_constants.CbServerBaseImage,
		Version:         e2e_constants.CbServerVersion,
		AuthSecret:      e2e_constants.KubeTestSecretName,
		ClusterSettings: defaultClusterSettings,
		BucketSettings:  bucketConfig,
		ServerSettings: []api.ServerConfig{api.ServerConfig{
			Size: size,
			Name: "test_config_1",
			Services: api.ServiceList{
				api.DataService,
				api.QueryService,
				api.IndexService,
			},
		}},
		ExposedFeatures: []string{},
	}
	crd := NewClusterCRD(e2e_constants.ClusterNamePrefix, spec)
	if console {
		crd.Spec.ExposeAdminConsole = true
		crd.Spec.AdminConsoleServices = api.ServiceList{
			api.DataService,
		}
	}
	return crd
}

// NewSupportableClusterSpec returns a basic supportable cluster spec with a stateful and stateless
// MDS groups of the defined size.  They use default and logs volume mounts respectively.
func NewSupportableClusterSpec(size int) api.ClusterSpec {
	spec := api.ClusterSpec{
		BaseImage:       e2e_constants.CbServerBaseImage,
		Version:         e2e_constants.CbServerVersion,
		AuthSecret:      e2e_constants.KubeTestSecretName,
		ClusterSettings: defaultClusterSettings,
		BucketSettings: []api.BucketConfig{
			DefaultBucketSettings,
		},
		ServerSettings: []api.ServerConfig{
			{
				Name: "stateful",
				Size: size,
				Services: api.ServiceList{
					api.DataService,
					api.IndexService,
				},
				Pod: &api.PodPolicy{
					VolumeMounts: &api.VolumeMounts{
						DefaultClaim: "couchbase",
					},
				},
			},
			{
				Name: "stateless",
				Size: size,
				Services: api.ServiceList{
					api.QueryService,
				},
				Pod: &api.PodPolicy{
					VolumeMounts: &api.VolumeMounts{
						LogsClaim: "couchbase",
					},
				},
			},
		},
		VolumeClaimTemplates: []v1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "couchbase",
					Annotations: map[string]string{},
				},
				Spec: v1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClassName,
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceStorage: *resource.NewScaledQuantity(1, 30),
						},
					},
				},
			},
		},
	}

	// The defaults are too aggressive.  When killing a pod during a rebalance the operator
	// may hang for ~30 seconds due to network retries. During this period we may or may not
	// observe a failover leading to non-determinism.
	spec.ClusterSettings.AutoFailoverTimeout = 120

	return spec
}

// NewSupportableCluster returns a basic supportable cluster with a stateful and stateless
// MDS groups of the defined size.  They use default and logs volume mounts respectively.
func NewSupportableCluster(size int) *api.CouchbaseCluster {
	spec := NewSupportableClusterSpec(size)
	return NewClusterCRD(e2e_constants.ClusterNamePrefix, spec)
}

// basic 3 node cluster with Xdcr cluster
func NewBasicXdcrCluster(genName, secretName string, size int, withBucket, exposed bool) *api.CouchbaseCluster {
	bucketConfig := []api.BucketConfig{}
	if withBucket {
		bucketSettings := DefaultBucketSettings
		bucketSettings.BucketName = "default"
		bucketConfig = []api.BucketConfig{bucketSettings}
	}
	spec := api.ClusterSpec{
		BaseImage:       e2e_constants.CbServerBaseImage,
		Version:         e2e_constants.CbServerVersion,
		AuthSecret:      e2e_constants.KubeTestSecretName,
		ClusterSettings: defaultClusterSettings,
		BucketSettings:  bucketConfig,
		ServerSettings: []api.ServerConfig{api.ServerConfig{
			Size: size,
			Name: "test_config_1",
			Services: api.ServiceList{
				api.DataService,
				api.QueryService,
				api.IndexService,
			},
		}},
		ExposedFeatures: []string{"xdcr"},
	}
	spec.ClusterSettings.AutoFailoverTimeout = 30
	spec.ClusterSettings.AutoFailoverMaxCount = 3
	crd := NewClusterCRD(genName, spec)
	if exposed {
		crd.Spec.ExposeAdminConsole = true
		crd.Spec.AdminConsoleServices = api.ServiceList{
			api.DataService,
		}
	}
	return crd
}

// new custom cluster
func CreateClusterSpec(genName, secretName string, config map[string]map[string]string) api.ClusterSpec {
	keys := []string{}
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Spec object to return
	spec := api.ClusterSpec{
		BaseImage:            e2e_constants.CbServerBaseImage,
		Version:              e2e_constants.CbServerVersion,
		AuthSecret:           secretName,
		ClusterSettings:      defaultClusterSettings,
		BucketSettings:       []api.BucketConfig{},
		ServerSettings:       []api.ServerConfig{},
		AdminConsoleServices: api.ServiceList{},
		ExposedFeatures:      api.ExposedFeatureList{},
		ServerGroups:         []string{},
	}

	for _, key := range keys {
		switch {
		// Updates Cluster settings in ClusterSpec
		case strings.Contains(key, "cluster"):
			for setting := range config[key] {
				switch setting {
				case "dataServiceMemQuota":
					dataServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.DataServiceMemQuota = dataServiceMemQuota
				case "indexServiceMemQuota":
					indexServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.IndexServiceMemQuota = indexServiceMemQuota
				case "searchServiceMemQuota":
					searchServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.SearchServiceMemQuota = searchServiceMemQuota
				case "indexStorageSetting":
					spec.ClusterSettings.IndexStorageSetting = config[key][setting]
				case "eventingServiceMemQuota":
					eventingServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.EventingServiceMemQuota = eventingServiceMemQuota
				case "analyticsServiceMemQuota":
					analyticsServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.AnalyticsServiceMemQuota = analyticsServiceMemQuota
				case "autoFailoverTimeout":
					autoFailoverTimeout, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.AutoFailoverTimeout = autoFailoverTimeout
				case "autoFailoverMaxCount":
					autoFailoverMaxCount, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.AutoFailoverMaxCount = autoFailoverMaxCount
				case "autoFailoverServerGroup":
					autoFailoverServerGroup, _ := strconv.ParseBool(config[key][setting])
					spec.ClusterSettings.AutoFailoverServerGroup = autoFailoverServerGroup
				case "autoFailoverOnDiskIssues":
					autoFailoverOnDiskIssues, _ := strconv.ParseBool(config[key][setting])
					spec.ClusterSettings.AutoFailoverOnDataDiskIssues = autoFailoverOnDiskIssues
				case "autoFailoverOnDiskIssuesTimeout":
					autoFailoverOnDiskIssuesTimeout, _ := strconv.ParseUint(config[key][setting], 10, 64)
					spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod = autoFailoverOnDiskIssuesTimeout
				}
			}

		// Update Buckets configs in ClusterSpec
		case strings.Contains(key, "bucket"):
			bucketSettings := DefaultBucketSettings
			for setting := range config[key] {
				switch setting {
				case "bucketName":
					bucketSettings.BucketName = config[key][setting]
				case "bucketType":
					bucketSettings.BucketType = config[key][setting]
				case "bucketMemoryQuota":
					bucketMemoryQuota, _ := strconv.Atoi(config[key][setting])
					bucketSettings.BucketMemoryQuota = bucketMemoryQuota
				case "bucketReplicas":
					bucketReplicas, _ := strconv.Atoi(config[key][setting])
					bucketSettings.BucketReplicas = bucketReplicas
				case "ioPriority":
					ioPriority := config[key][setting]
					bucketSettings.IoPriority = ioPriority
				case "evictionPolicy":
					policy := config[key][setting]
					bucketSettings.EvictionPolicy = policy
				case "conflictResolution":
					confResoultion := config[key][setting]
					bucketSettings.ConflictResolution = confResoultion
				case "enableFlush":
					enableFlush, _ := strconv.ParseBool(config[key][setting])
					bucketSettings.EnableFlush = enableFlush
				case "enableIndexReplica":
					enableIndexReplica, _ := strconv.ParseBool(config[key][setting])
					bucketSettings.EnableIndexReplica = enableIndexReplica
				}
			}
			spec.BucketSettings = append(spec.BucketSettings, bucketSettings)

		// Modify Service in ClusterSpec
		case strings.Contains(key, "service"):
			serverSettings := defaultServerSettings
			volumeMnt := &api.VolumeMounts{}
			podPolicy := &api.PodPolicy{VolumeMounts: nil}
			podPolicy.Resources = v1.ResourceRequirements{
				Limits:   make(v1.ResourceList),
				Requests: make(v1.ResourceList),
			}
			serverSettings.Pod = podPolicy
			for setting := range config[key] {
				switch setting {
				case "name":
					serverSettings.Name = config[key][setting]
				case "size":
					size, _ := strconv.Atoi(config[key][setting])
					serverSettings.Size = size
				case "services":
					serverSettings.Services = api.NewServiceList(strings.Split(config[key][setting], ","))
				case "serverGroups":
					serverSettings.ServerGroups = strings.Split(config[key][setting], ",")
				case "resourceMemLimit":
					if _, err := strconv.Atoi(config[key][setting]); err == nil {
						serverSettings.Pod.Resources.Limits[v1.ResourceMemory] = resource.MustParse(config[key][setting] + "Mi")
					}
				case "resourceMemRequest":
					if _, err := strconv.Atoi(config[key][setting]); err == nil {
						serverSettings.Pod.Resources.Requests[v1.ResourceMemory] = resource.MustParse(config[key][setting] + "Mi")
					}
				case "defaultVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.DefaultClaim = config[key][setting]
				case "indexVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.IndexClaim = config[key][setting]
				case "dataVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.DataClaim = config[key][setting]
				case "analyticsVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.AnalyticsClaims = strings.Split(config[key][setting], ",")
				case "logVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.LogsClaim = config[key][setting]
				}
			}
			spec.ServerSettings = append(spec.ServerSettings, serverSettings)

		// Sets ExposedFeatures in ClusterSpec
		case key == "exposedFeatures":
			spec.ExposedFeatures = strings.Split(config[key]["featureNames"], ",")

		// Updates AdminConsoleServices in ClusterSpec
		case key == "adminConsoleServices":
			for _, serviceName := range strings.Split(config[key]["services"], ",") {
				spec.AdminConsoleServices = append(spec.AdminConsoleServices, api.Service(serviceName))
			}

		// Sets ServerGroups in ClusterSpec
		case key == "serverGroups":
			spec.ServerGroups = strings.Split(config[key]["groupNames"], ",")

		// Updates ClusterSpec settings like BaseImage, version, antiaffinity...
		case strings.Contains(key, "other"):
			for setting := range config[key] {
				switch setting {
				case "baseImageName":
					spec.BaseImage = config[key][setting]
				case "versionNum":
					spec.Version = config[key][setting]
				case "antiAffinity":
					if config[key][setting] == "on" {
						spec.AntiAffinity = true
					} else if config[key][setting] == "off" {
						spec.AntiAffinity = false
					}
				case "logRetentionTime":
					spec.LogRetentionTime = config[key][setting]
				case "logRetentionCount":
					if logRetentionCount, err := strconv.Atoi(config[key][setting]); err == nil {
						spec.LogRetentionCount = logRetentionCount
					}
				}
			}
		}
	}
	return spec
}

func CreateClusterCRD(genName string, adminConsoleExposed bool, spec api.ClusterSpec) *api.CouchbaseCluster {
	crd := NewClusterCRD(genName, spec)
	if adminConsoleExposed {
		crd.Spec.ExposeAdminConsole = true
		crd.Spec.AdminConsoleServices = api.ServiceList{
			api.DataService,
		}
	}
	return crd
}

func NewMultiCluster(genName, secretName string, config map[string]map[string]string, exposed bool) *api.CouchbaseCluster {
	spec := CreateClusterSpec(genName, secretName, config)
	return CreateClusterCRD(genName, exposed, spec)
}

// Stateful 3 node cluster with a single volume.
// Spec will request 1Gb of storage (minikube default is 5gb).
func NewStatefulCluster(genName, secretName string, size int, withBucket bool, exposed bool) *api.CouchbaseCluster {

	crd := NewBasicCluster(genName, secretName, size, withBucket, exposed)
	couchbase := "couchbase"
	crd.Spec.ServerSettings[0].Pod = &api.PodPolicy{
		VolumeMounts: &api.VolumeMounts{DefaultClaim: couchbase},
	}

	storagePolicy := CreatePodPolicy(v1.ResourceStorage, 1, 1, "Gi")
	claim := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "couchbase",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			StorageClassName: &storageClassName,
			Resources:        storagePolicy.Resources,
		},
	}
	crd.Spec.VolumeClaimTemplates = []v1.PersistentVolumeClaim{claim}
	return crd
}

// NewClusterCRD creates a new Couchbase cluster CRD and associates the specified
// specification with it.  The cluster name may be dynamically generated by the
// K8S manager or explicitly defined where we need to know it ahead of time e.g.
// TLS.  TLS policy is also applied based on global settings
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

// Create Pod Policy with memory limit and requests in MB
func CreateMemoryPodPolicy(request, limit int) *api.PodPolicy {
	return CreatePodPolicy(v1.ResourceMemory, request, limit, "Mi")
}

// Create limit and request pod policy according to scale... ie 'Mi, Gi' where applicable
func CreatePodPolicy(resourceName v1.ResourceName, request, limit int, scale string) *api.PodPolicy {
	podPolicy := &api.PodPolicy{}
	podPolicy.Resources = v1.ResourceRequirements{
		Limits:   make(v1.ResourceList),
		Requests: make(v1.ResourceList),
	}
	podPolicy.Resources.Limits[resourceName] = resource.MustParse(strconv.Itoa(limit) + scale)
	podPolicy.Resources.Requests[resourceName] = resource.MustParse(strconv.Itoa(request) + scale)
	return podPolicy
}
