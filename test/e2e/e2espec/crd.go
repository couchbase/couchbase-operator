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

// ClusterOptions allows things about a cluster to be modified.
type ClusterOptions struct {
	// Couchbase Server container image to use.
	Image string

	// Cluster size for single class clusters.
	Size int

	// Autofailver timeout.  Smaller means faster, but also means
	// more race conditions.
	AutoFailoverTimeout *metav1.Duration

	// Prometheus exporter container image to use.
	MonitoringImage string

	// Backup image name.
	BackupImage string

	// Storage class to use.
	StorageClass string

	// Platform type.
	Platform couchbasev2.PlatformType

	// Istio support.
	Istio bool
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

// Create a very basic cluster with rbac and buckets managed, a single server
// class with data/query/index enabled.
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
			Servers: []couchbasev2.ServerConfig{
				{
					Size: options.Size,
					Name: e2e_constants.CouchbaseServerConfig,
					Services: couchbasev2.ServiceList{
						couchbasev2.DataService,
						couchbasev2.QueryService,
						couchbasev2.IndexService,
					},
				},
			},
			Platform: options.Platform,
			Backup: couchbasev2.Backup{
				Managed:        true,
				Image:          options.BackupImage,
				ServiceAccount: config.BackupResourceName,
			},
		},
	}

	if options.Istio {
		platform := couchbasev2.NetworkPlatformIstio
		cluster.Spec.Networking.NetworkPlatform = &platform
	}

	return cluster
}

// NewSupportableCluster returns a basic supportable cluster with a stateful and stateless
// MDS groups of the defined size.  They use default and logs volume mounts respectively.
func NewSupportableCluster(options *ClusterOptions) *couchbasev2.CouchbaseCluster {
	cluster := NewBasicCluster(options)

	cluster.Spec.Servers = []couchbasev2.ServerConfig{
		{
			Name: "stateful",
			Size: options.Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.IndexService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				DefaultClaim: "couchbase",
			},
		},
		{
			Name: "stateless",
			Size: options.Size,
			Services: couchbasev2.ServiceList{
				couchbasev2.QueryService,
				// Eventing is not technically necessary here, however some tests rely on
				// synchronization events before proceeding.  Eventing causes a rebalance
				// when the Couchbase server process goes down so we can tell when it's
				// back in the cluster.
				couchbasev2.EventingService,
			},
			VolumeMounts: &couchbasev2.VolumeMounts{
				LogsClaim: "couchbase",
			},
		},
	}

	cluster.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{
		{
			ObjectMeta: couchbasev2.NamedObjectMeta{
				Name:        "couchbase",
				Annotations: map[string]string{},
			},
			Spec: v1.PersistentVolumeClaimSpec{
				StorageClassName: &options.StorageClass,
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: *NewResourceQuantityMi(1024),
					},
				},
			},
		},
	}

	return cluster
}

// Stateful 3 node cluster with a single volume.
// Spec will request 1Gb of storage (minikube default is 5gb).
func NewStatefulCluster(options *ClusterOptions) *couchbasev2.CouchbaseCluster {
	crd := NewBasicCluster(options)
	couchbase := "couchbase"

	crd.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: couchbase,
	}

	resources := CreateResources(v1.ResourceStorage, 1, 1, "Gi")
	claim := couchbasev2.PersistentVolumeClaimTemplate{
		ObjectMeta: couchbasev2.NamedObjectMeta{
			Name: "couchbase",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			StorageClassName: &options.StorageClass,
			Resources:        resources,
		},
	}

	crd.Spec.VolumeClaimTemplates = []couchbasev2.PersistentVolumeClaimTemplate{claim}

	return crd
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
