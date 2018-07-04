package e2espec

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// other settings
var (
	baseImage = "couchbase/server"
	version   = "5.5.0-beta"
)

// cluster settings
var (
	defaultClusterSettings = api.ClusterConfig{
		DataServiceMemQuota:                    256,
		IndexServiceMemQuota:                   256,
		SearchServiceMemQuota:                  256,
		EventingServiceMemQuota:                256,
		AnalyticsServiceMemQuota:               1024,
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
		BucketMemoryQuota:  256,
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
		Size:     1,
		Name:     "test_config_1",
		Services: []string{"data", "query", "index"},
	}
)

func GenerateValidBucketSettings(bucketTypes []string) []api.BucketConfig {
	generatedSettings := []api.BucketConfig{}
	for _, bucketType := range bucketTypes {
		switch {
		case bucketType == constants.BucketTypeCouchbase:
			bucketMemoryQuotas := []int{256}
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
			bucketMemoryQuotas := []int{256}
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
			bucketMemoryQuotas := []int{256}
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

// basic 3 node cluster
func NewBasicCluster(genName, secretName string, size int, withBucket bool, exposed bool) *api.CouchbaseCluster {
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
			Size:     size,
			Name:     "test_config_1",
			Services: []string{"data", "query", "index"},
		}},
		ExposedFeatures: []string{},
	}
	crd := NewClusterCRD(genName, spec)
	if exposed {
		crd.Spec.ExposeAdminConsole = true
	}
	fmt.Printf("crd: %+v\n", crd)
	return crd
}

// basic 3 node cluster with Xdcr cluster
func NewBasicXdcrCluster(genName, secretName string, size int, withBucket, exposed bool) *api.CouchbaseCluster {
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
			Size:     size,
			Name:     "test_config_1",
			Services: []string{"data", "query", "index"},
		}},
		ExposedFeatures: []string{"xdcr"},
	}
	crd := NewClusterCRD(genName, spec)
	if exposed {
		crd.Spec.ExposeAdminConsole = true
	}
	return crd
}

// new custom cluster
func CreateClusterSpec(genName, secretName string, config map[string]map[string]string) api.ClusterSpec {
	clusterSettings := api.ClusterConfig(defaultClusterSettings)
	bucketConfig := []api.BucketConfig{}
	serverConfig := []api.ServerConfig{}
	exposedFeatures := api.ExposedFeatureList{}
	serverGroups := []string{}
	baseImageName := baseImage
	versionNum := version
	antiAffinity := ""
	keys := []string{}
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch {
		case strings.Contains(key, "cluster"):
			for setting := range config[key] {
				switch {
				case setting == "dataServiceMemQuota":
					dataServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.DataServiceMemQuota = dataServiceMemQuota
				case setting == "indexServiceMemQuota":
					indexServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.IndexServiceMemQuota = indexServiceMemQuota
				case setting == "searchServiceMemQuota":
					searchServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.SearchServiceMemQuota = searchServiceMemQuota
				case setting == "indexStorageSetting":
					clusterSettings.IndexStorageSetting = config[key][setting]
				case setting == "eventingServiceMemQuota":
					eventingServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.EventingServiceMemQuota = eventingServiceMemQuota
				case setting == "analyticsServiceMemQuota":
					analyticsServiceMemQuota, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.AnalyticsServiceMemQuota = analyticsServiceMemQuota
				case setting == "autoFailoverTimeout":
					autoFailoverTimeout, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.AutoFailoverTimeout = autoFailoverTimeout
				case setting == "autoFailoverMaxCount":
					autoFailoverMaxCount, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.AutoFailoverMaxCount = autoFailoverMaxCount
				case setting == "autoFailoverServerGroup":
					autoFailoverServerGroup, _ := strconv.ParseBool(config[key][setting])
					clusterSettings.AutoFailoverServerGroup = autoFailoverServerGroup
				case setting == "autoFailoverOnDiskIssues":
					autoFailoverOnDiskIssues, _ := strconv.ParseBool(config[key][setting])
					clusterSettings.AutoFailoverOnDataDiskIssues = autoFailoverOnDiskIssues
				case setting == "autoFailoverOnDiskIssuesTimeout":
					autoFailoverOnDiskIssuesTimeout, _ := strconv.ParseUint(config[key][setting], 10, 64)
					clusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod = autoFailoverOnDiskIssuesTimeout
				}
			}
		case strings.Contains(key, "bucket"):
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
					ioPriority := config[key][setting]
					bucketSettings.IoPriority = ioPriority
				case setting == "evictionPolicy":
					policy := config[key][setting]
					bucketSettings.EvictionPolicy = policy
				case setting == "conflictResolution":
					confResoultion := config[key][setting]
					bucketSettings.ConflictResolution = confResoultion
				case setting == "enableFlush":
					enableFlush, _ := strconv.ParseBool(config[key][setting])
					bucketSettings.EnableFlush = enableFlush
				case setting == "enableIndexReplica":
					enableIndexReplica, _ := strconv.ParseBool(config[key][setting])
					bucketSettings.EnableIndexReplica = enableIndexReplica
				}
			}
			bucketConfig = append(bucketConfig, bucketSettings)
		case strings.Contains(key, "service"):
			serverSettings := api.ServerConfig(defaultServerSettings)
			volumeMnt := &api.VolumeMounts{}
			podPolicy := &api.PodPolicy{VolumeMounts: nil}
			podPolicy.Resources = v1.ResourceRequirements{
				Limits:   make(v1.ResourceList),
				Requests: make(v1.ResourceList),
			}
			serverSettings.Pod = podPolicy
			for setting := range config[key] {
				switch {
				case setting == "name":
					serverSettings.Name = config[key][setting]
				case setting == "size":
					size, _ := strconv.Atoi(config[key][setting])
					serverSettings.Size = size
				case setting == "services":
					services := []string{}
					parsedServices := strings.Split(config[key][setting], ",")
					for _, service := range parsedServices {
						services = append(services, service)
					}
					serverSettings.Services = services
				case setting == "serverGroups":
					serverGroups := []string{}
					parsedGroups := strings.Split(config[key][setting], ",")
					for _, group := range parsedGroups {
						serverGroups = append(serverGroups, group)
					}
					serverSettings.ServerGroups = serverGroups
				case setting == "resourceMemLimit":
					limit, _ := strconv.Atoi(config[key][setting])
					serverSettings.Pod.Resources.Limits[v1.ResourceMemory] = resource.MustParse(fmt.Sprintf("%d%s", limit, "Mi"))
				case setting == "resourceMemRequest":
					request, _ := strconv.Atoi(config[key][setting])
					serverSettings.Pod.Resources.Requests[v1.ResourceMemory] = resource.MustParse(fmt.Sprintf("%d%s", request, "Mi"))
				case setting == "defaultVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.DefaultClaim = config[key][setting]
				case setting == "indexVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.IndexClaim = config[key][setting]
				case setting == "dataVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.DataClaim = config[key][setting]
				case setting == "analyticsVolMnt":
					if serverSettings.Pod.VolumeMounts == nil {
						serverSettings.Pod.VolumeMounts = volumeMnt
					}
					serverSettings.Pod.VolumeMounts.AnalyticsClaims = strings.Split(config[key][setting], ",")
				}
			}
			serverConfig = append(serverConfig, serverSettings)
		case key == "exposedFeatures":
			exposedFeatures = strings.Split(config[key]["featureNames"], ",")
		case key == "serverGroups":
			serverGroups = strings.Split(config[key]["groupNames"], ",")
		case strings.Contains(key, "other"):
			for setting := range config[key] {
				switch {
				case setting == "antiAffinity":
					antiAffinity = config[key][setting]
				case setting == "baseImageName":
					baseImageName = config[key][setting]
				case setting == "versionNum":
					versionNum = config[key][setting]
				}
			}
		}
	}

	spec := api.ClusterSpec{
		BaseImage:       baseImageName,
		Version:         versionNum,
		AuthSecret:      secretName,
		ClusterSettings: clusterSettings,
		BucketSettings:  bucketConfig,
		ServerSettings:  serverConfig,
		ExposedFeatures: exposedFeatures,
		ServerGroups:    serverGroups,
	}

	switch {
	case antiAffinity == "on":
		spec.AntiAffinity = true

	case antiAffinity == "off":
		spec.AntiAffinity = false
	}
	return spec
}

func CreateClusterCRD(genName string, adminConsoleExposed bool, spec api.ClusterSpec) *api.CouchbaseCluster {
	crd := NewClusterCRD(genName, spec)
	if adminConsoleExposed {
		crd.Spec.ExposeAdminConsole = true
	}
	return crd
}

func NewMultiCluster(genName, secretName string, config map[string]map[string]string, exposed bool) *api.CouchbaseCluster {
	spec := CreateClusterSpec(genName, secretName, config)
	return CreateClusterCRD(genName, exposed, spec)
}

// Stateful 3 node cluster with a single volume.
// Spec will request 1Gb of storage (minikube default is 5gb).
// Also uses 'standard' storage class which is default in kubernetes
func NewStatefulCluster(genName, secretName string, size int, withBucket bool, exposed bool) *api.CouchbaseCluster {

	crd := NewBasicCluster(genName, secretName, size, withBucket, exposed)
	couchbase := "couchbase"
	crd.Spec.ServerSettings[0].Pod = &api.PodPolicy{
		VolumeMounts: &api.VolumeMounts{DefaultClaim: couchbase},
	}

	standardStorageClass := "standard"
	storagePolicy := CreatePodPolicy(v1.ResourceStorage, 1, 1, "Gi")
	claim := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "couchbase",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			StorageClassName: &standardStorageClass,
			Resources:        storagePolicy.Resources,
		},
	}
	crd.Spec.VolumeClaimTemplates = []v1.PersistentVolumeClaim{claim}
	return crd
}

// Explicitly generated cluster name.  Used by TLS tests where we need to know
// the cluster DNS names before creating the cluster to generate certificates.
// This is non-reentrant so do not parallelize tests when in use.
var clusterName = []string{}

// Generates a random (mostly unique!) name for a new cluster
func SetClusterName(name string) {
	clusterName = append(clusterName, name)
}

// Resets the explicit cluster name
func ResetClusterName() {
	clusterName = []string{}
}

// tlsPolicy points to a TLS configuration.  If not nil it is attached to the
// next CRD to be created
var tlsPolicy = []*api.TLSPolicy{}

// SetTLS sets the TLS configuration for the next CRD
func SetTLS(tls *api.TLSPolicy) {
	tlsPolicy = append(tlsPolicy, tls)
}

// ResetTLS resets the TLS configuration so it is not applied to the next CRD
func ResetTLS() {
	tlsPolicy = []*api.TLSPolicy{}
}

// NewClusterCRD creates a new Couchbase cluster CRD and associates the specified
// specification with it.  The cluster name may be dynamically generated by the
// K8S manager or explicitly defined where we need to know it ahead of time e.g.
// TLS.  TLS policy is also applied based on global settings
func NewClusterCRD(genName string, spec api.ClusterSpec) *api.CouchbaseCluster {
	cluster := &api.CouchbaseCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       api.CRDResourceKind,
			APIVersion: api.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genName,
		},
		Spec: spec,
	}
	if len(clusterName) == 0 {
		cluster.ObjectMeta.GenerateName = genName
	} else {
		cluster.ObjectMeta.Name = clusterName[0]
		clusterName = clusterName[1:]
	}
	if len(tlsPolicy) == 0 {
		cluster.Spec.TLS = nil
	} else {
		cluster.Spec.TLS = tlsPolicy[0]
		tlsPolicy = tlsPolicy[1:]
	}
	return cluster
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
	resourceValue := fmt.Sprintf("%d%s", limit, scale)
	podPolicy.Resources.Limits[resourceName] = resource.MustParse(resourceValue)
	resourceValue = fmt.Sprintf("%d%s", request, scale)
	podPolicy.Resources.Requests[resourceName] = resource.MustParse(resourceValue)
	return podPolicy
}
