package e2espec

import (
	"strconv"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PersistenceType indicates the type of volume to attach to a server class.
type PersistenceType string

const (
	// PersistData means all data and logs are persisted (the pod can be
	// recovered).
	PersistData PersistenceType = "data"

	// PersistLogs means only logs are persisted, the pod needs to be recreated
	// (this was a huge and stupid mistake that needs to be removed).
	PersistLogs PersistenceType = "logs"
)

// ServerClass is an abstract representation of a server class.
type ServerClass struct {
	// Name is the name of the class and must be unique within the cluster.
	Name string

	// Size is the number of pods that this class should contain.
	Size int

	// Services is the list of Couchbase services that should be provisioned.
	Services []couchbasev2.Service

	// Persistence indicates whether we want persistent volumes.
	Persistence PersistenceType

	// VolumeSize defines the size of the volumes to create when using
	// persistence.
	VolumeSize string
}

// ClusterTopology defines what the cluster should look like.
type ClusterTopology []ServerClass

// DeepCopy clones each use of a topology to keep them immuatable.
func (t ClusterTopology) DeepCopy() ClusterTopology {
	o := make(ClusterTopology, len(t))

	for i, class := range t {
		services := make([]couchbasev2.Service, len(class.Services))

		for j, service := range class.Services {
			services[j] = service
		}

		o[i] = ServerClass{
			Name:        class.Name,
			Size:        class.Size,
			Services:    services,
			Persistence: class.Persistence,
			VolumeSize:  class.VolumeSize,
		}
	}

	return o
}

var (
	// EphemeralTopology is a simple ephemeral cluster, useful for testing
	// basic configuration settings.
	EphemeralTopology = ClusterTopology{
		{
			Name: "default",
			Services: []couchbasev2.Service{
				couchbasev2.DataService,
				couchbasev2.IndexService,
				couchbasev2.QueryService,
			},
		},
	}

	// MixedEphemeralTopology separates stateful and
	// stateless services into separate server groups.
	MixedEphemeralTopology = ClusterTopology{
		{
			Name: "default",
			Services: []couchbasev2.Service{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
		},
		{
			Name: "query",
			Services: []couchbasev2.Service{
				couchbasev2.QueryService,
			},
		},
	}

	// PersistentTopology is a persistent volume backed cluster, this is
	// what we expect customers to use.
	PersistentTopology = ClusterTopology{
		{
			Name: "default",
			Services: []couchbasev2.Service{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
			Persistence: PersistData,
			VolumeSize:  "1Gi",
		},
	}

	// MixedTopology is a more complex (aka we messed up) topology
	// that allows recoverable data nodes, but ephemeral query ones.
	MixedTopology = ClusterTopology{
		{
			Name: "stateful",
			Services: []couchbasev2.Service{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
			Persistence: PersistData,
			VolumeSize:  "1Gi",
		},
		{
			Name: "stateless",
			Services: []couchbasev2.Service{
				couchbasev2.QueryService,
				// Eventing is not technically necessary here, however some tests rely on
				// synchronization events before proceeding.  Eventing causes a rebalance
				// when the Couchbase server process goes down so we can tell when it's
				// back in the cluster.
				couchbasev2.EventingService,
			},
			Persistence: PersistLogs,
			VolumeSize:  "1Gi",
		},
	}
)

// ClusterOptions allows things about a cluster to be modified.
type ClusterOptions struct {
	// Couchbase Server container image to use.
	Image string

	// Cluster topology (shape and size).
	Topology ClusterTopology

	// Autofailver timeout.  Smaller means faster, but also means
	// more race conditions.
	AutoFailoverTimeout *metav1.Duration

	// Prometheus exporter container image to use.
	MonitoringImage string

	// Backup image name.
	BackupImage string

	// Log shipping image name
	LoggingImage string

	// Storage class to use.
	StorageClass string

	// Platform type.
	Platform couchbasev2.PlatformType

	// Istio support.
	Istio bool

	// Enbles maintenance mode of Autoscaler during rebalance and specifies
	// optional duration of time to wait after before re-enabling autoscaler
	AutoscaleStabilizationPeriod *metav1.Duration
}

// bucket settings.
var (
	defaultBucket = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.DefaultBucket,
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        NewResourceQuantityMi(256),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
		},
	}

	defaultBucketTwoReplicas = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.DefaultBucket,
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        NewResourceQuantityMi(256),
			Replicas:           2,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
		},
	}

	defaultBucketThreeReplicas = &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.DefaultBucket,
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        NewResourceQuantityMi(256),
			Replicas:           3,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
			EnableIndexReplica: false,
		},
	}

	defaultEphemeralBucket = &couchbasev2.CouchbaseEphemeralBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.DefaultEphemeralBucket,
		},
		Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
			MemoryQuota:        NewResourceQuantityMi(256),
			Replicas:           1,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
			EnableFlush:        true,
		},
	}

	defaultMemcachedBucket = &couchbasev2.CouchbaseMemcachedBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.DefaultBucket,
		},
		Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
			MemoryQuota: NewResourceQuantityMi(256),
		},
	}
)

func DefaultBucket() *couchbasev2.CouchbaseBucket {
	return defaultBucket.DeepCopy()
}

func DefaultBucketTwoReplicas() *couchbasev2.CouchbaseBucket {
	return defaultBucketTwoReplicas.DeepCopy()
}

func DefaultBucketThreeReplicas() *couchbasev2.CouchbaseBucket {
	return defaultBucketThreeReplicas.DeepCopy()
}

func DefaultEphemeralBucket() *couchbasev2.CouchbaseEphemeralBucket {
	return defaultEphemeralBucket.DeepCopy()
}

func DefaultMemcachedBucket() *couchbasev2.CouchbaseMemcachedBucket {
	return defaultMemcachedBucket.DeepCopy()
}

func GetReplication(srcBucket, dstBucket string) *couchbasev2.CouchbaseReplication {
	replication := &couchbasev2.CouchbaseReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.DefaultReplication,
		},
		Spec: couchbasev2.CouchbaseReplicationSpec{
			Bucket:       srcBucket,
			RemoteBucket: dstBucket,
		},
	}

	return replication
}

func NewResourceQuantityMi(value int64) *resource.Quantity {
	return resource.NewQuantity(value<<20, resource.BinarySI)
}

func NewResourceQuantityGi(value int64) *resource.Quantity {
	return NewResourceQuantityMi(value * 1024)
}

func NewDurationS(value uint64) *metav1.Duration {
	return &metav1.Duration{Duration: time.Duration(value) * time.Second}
}

func GenerateValidBucketSettings(bucketTypes []string) []metav1.Object {
	buckets := []metav1.Object{}

	for _, bucketType := range bucketTypes {
		switch {
		case bucketType == constants.BucketTypeCouchbase:
			bucketMemoryQuotas := []*resource.Quantity{
				NewResourceQuantityMi(256),
			}
			bucketReplicas := []int{1}
			ioPriorities := []couchbasev2.CouchbaseBucketIOPriority{
				couchbasev2.CouchbaseBucketIOPriorityHigh,
			}
			evictionPolicies := []couchbasev2.CouchbaseBucketEvictionPolicy{
				couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			}
			conflictResolutions := []couchbasev2.CouchbaseBucketConflictResolution{
				couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
				couchbasev2.CouchbaseBucketConflictResolutionTimestamp,
			}
			enableFlushes := []bool{
				true,
			}
			enableIndexReplicas := []bool{
				true,
			}

			for _, bucketMemoryQuota := range bucketMemoryQuotas {
				for _, bucketReplica := range bucketReplicas {
					for _, ioPriority := range ioPriorities {
						for _, evictionPolicy := range evictionPolicies {
							for _, conflictResolution := range conflictResolutions {
								for _, enableFlush := range enableFlushes {
									for _, enableIndexReplica := range enableIndexReplicas {
										buckets = append(buckets, &couchbasev2.CouchbaseBucket{
											ObjectMeta: metav1.ObjectMeta{
												Name: e2e_constants.DefaultBucket,
											},
											Spec: couchbasev2.CouchbaseBucketSpec{
												MemoryQuota:        bucketMemoryQuota,
												Replicas:           bucketReplica,
												IoPriority:         ioPriority,
												EvictionPolicy:     evictionPolicy,
												ConflictResolution: conflictResolution,
												EnableFlush:        enableFlush,
												EnableIndexReplica: enableIndexReplica,
												CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
											},
										})
									}
								}
							}
						}
					}
				}
			}
		case bucketType == constants.BucketTypeMemcached:
			bucketMemoryQuotas := []*resource.Quantity{
				NewResourceQuantityMi(256),
			}
			enableFlushes := []bool{
				true,
				false,
			}

			for _, bucketMemoryQuota := range bucketMemoryQuotas {
				for _, enableFlush := range enableFlushes {
					buckets = append(buckets, &couchbasev2.CouchbaseMemcachedBucket{
						ObjectMeta: metav1.ObjectMeta{
							Name: e2e_constants.DefaultBucket,
						},
						Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
							MemoryQuota: bucketMemoryQuota,
							EnableFlush: enableFlush,
						},
					})
				}
			}
		case bucketType == constants.BucketTypeEphemeral:
			bucketMemoryQuotas := []*resource.Quantity{
				NewResourceQuantityMi(256),
			}
			bucketReplicas := []int{1}
			ioPriorities := []couchbasev2.CouchbaseBucketIOPriority{
				couchbasev2.CouchbaseBucketIOPriorityHigh,
			}
			evictionPolicies := []couchbasev2.CouchbaseEphemeralBucketEvictionPolicy{
				couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNoEviction,
				couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction,
			}
			conflictResolutions := []couchbasev2.CouchbaseBucketConflictResolution{
				couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
				couchbasev2.CouchbaseBucketConflictResolutionTimestamp,
			}
			enableFlushes := []bool{
				true,
				false,
			}

			for _, bucketMemoryQuota := range bucketMemoryQuotas {
				for _, bucketReplica := range bucketReplicas {
					for _, ioPriority := range ioPriorities {
						for _, evictionPolicy := range evictionPolicies {
							for _, conflictResolution := range conflictResolutions {
								for _, enableFlush := range enableFlushes {
									buckets = append(buckets, &couchbasev2.CouchbaseEphemeralBucket{
										ObjectMeta: metav1.ObjectMeta{
											Name: e2e_constants.DefaultBucket,
										},
										Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
											MemoryQuota:        bucketMemoryQuota,
											Replicas:           bucketReplica,
											IoPriority:         ioPriority,
											EvictionPolicy:     evictionPolicy,
											ConflictResolution: conflictResolution,
											EnableFlush:        enableFlush,
											CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
										},
									})
								}
							}
						}
					}
				}
			}
		}
	}

	return buckets
}

// ApplyImagePullSecret adds image pull secrets to all the Couchbase server pods so that
// they can use private repositories.
func ApplyImagePullSecret(cluster *couchbasev2.CouchbaseCluster, imagePullSecrets []string) {
	if imagePullSecrets == nil {
		return
	}

	references := make([]v1.LocalObjectReference, len(imagePullSecrets))

	for i, secret := range imagePullSecrets {
		references[i].Name = secret
	}

	for i := range cluster.Spec.Servers {
		if cluster.Spec.Servers[i].Pod == nil {
			cluster.Spec.Servers[i].Pod = &couchbasev2.PodTemplate{}
		}

		cluster.Spec.Servers[i].Pod.Spec.ImagePullSecrets = references
	}

	cluster.Spec.Backup.ImagePullSecrets = references
}

// Create a basic clusters with rbac and buckets managed.  Server classes are
// dynamically generated.
func NewBasicCluster(options *ClusterOptions) *couchbasev2.CouchbaseCluster {
	cluster := &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: e2e_constants.ClusterNamePrefix,
		},
		Spec: couchbasev2.ClusterSpec{
			Image: options.Image,
			Security: couchbasev2.CouchbaseClusterSecuritySpec{
				AdminSecret: e2e_constants.KubeTestSecretName,
				RBAC: couchbasev2.RBAC{
					Managed: true,
				},
			},
			ClusterSettings: couchbasev2.ClusterConfig{
				AutoFailoverTimeout: options.AutoFailoverTimeout,
			},
			Buckets: couchbasev2.Buckets{
				Managed: true,
			},
			Servers:              []couchbasev2.ServerConfig{},
			VolumeClaimTemplates: []couchbasev2.PersistentVolumeClaimTemplate{},
			Backup: couchbasev2.Backup{
				Managed:        true,
				Image:          options.BackupImage,
				ServiceAccount: config.BackupResourceName,
			},
			AutoscaleStabilizationPeriod: options.AutoscaleStabilizationPeriod,
		},
	}

	if options.Platform != "gke-autopilot" {
		cluster.Spec.Platform = options.Platform
	}

	for _, class := range options.Topology {
		config := couchbasev2.ServerConfig{
			Name:     class.Name,
			Size:     class.Size,
			Services: class.Services,
		}

		if class.Persistence != "" {
			pvc := couchbasev2.PersistentVolumeClaimTemplate{
				ObjectMeta: couchbasev2.NamedObjectMeta{
					Name: class.Name,
				},
				Spec: v1.PersistentVolumeClaimSpec{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"storage": resource.MustParse(class.VolumeSize),
						},
					},
				},
			}

			switch class.Persistence {
			case PersistData:
				config.VolumeMounts = &couchbasev2.VolumeMounts{
					DefaultClaim: class.Name,
				}
			case PersistLogs:
				config.VolumeMounts = &couchbasev2.VolumeMounts{
					LogsClaim: class.Name,
				}
			}

			if options.StorageClass != "" {
				pvc.Spec.StorageClassName = &options.StorageClass
			}

			cluster.Spec.VolumeClaimTemplates = append(cluster.Spec.VolumeClaimTemplates, pvc)
		}

		cluster.Spec.Servers = append(cluster.Spec.Servers, config)
	}

	if options.Istio {
		platform := couchbasev2.NetworkPlatformIstio
		cluster.Spec.Networking.NetworkPlatform = &platform
	}

	return cluster
}

// Create Pod Policy with memory limit and requests in MB.
func CreateMemoryResources(request, limit int) v1.ResourceRequirements {
	return CreateResources(v1.ResourceMemory, request, limit, "Mi")
}

// Create limit and request pod policy according to scale... ie 'Mi, Gi' where applicable.
func CreateResources(resourceName v1.ResourceName, request, limit int, scale string) v1.ResourceRequirements {
	return v1.ResourceRequirements{
		Limits: v1.ResourceList{
			resourceName: resource.MustParse(strconv.Itoa(limit) + scale),
		},
		Requests: v1.ResourceList{
			resourceName: resource.MustParse(strconv.Itoa(request) + scale),
		},
	}
}

// ApplySecurityContext adds security context to all the Couchbase server pods so that
// they can run as Nonroot containers.
func ApplySecurityContext(cluster *couchbasev2.CouchbaseCluster, platformType string) {
	nonRoot := true

	cluster.Spec.SecurityContext = &v1.PodSecurityContext{
		RunAsNonRoot: &nonRoot,
	}

	if platformType != "openshift" {
		user := int64(1000)
		cluster.Spec.SecurityContext.RunAsUser = &user
	}
}
