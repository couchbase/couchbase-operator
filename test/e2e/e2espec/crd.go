package e2espec

import (
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	e2e_constants "github.com/couchbase/couchbase-operator/test/e2e/constants"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// bucket settings
var (
	DefaultBucket = &couchbasev2.CouchbaseBucket{
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
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	DefaultBucketTwoReplicas = &couchbasev2.CouchbaseBucket{
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
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	DefaultBucketThreeReplicas = &couchbasev2.CouchbaseBucket{
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
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionModePassive,
		},
	}

	DefaultEphemeralBucket = &couchbasev2.CouchbaseEphemeralBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: e2e_constants.DefaultBucket,
		},
		Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
			MemoryQuota: NewResourceQuantityMi(256),
		},
	}
)

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

func SetCouchbaseServerImage(imageName string) {
	if imageName = strings.TrimSpace(imageName); imageName != "" {
		e2e_constants.CouchbaseServerImage = imageName
	}
}

var storageClassName string

func SetStorageClassName(storageClassNameIn string) {
	if storageClassNameIn = strings.TrimSpace(storageClassNameIn); storageClassNameIn != "" {
		storageClassName = storageClassNameIn
	}
}

var platform couchbasev2.PlatformType

func SetPlatform(p couchbasev2.PlatformType) {
	platform = p
}

func GenerateValidBucketSettings(bucketTypes []string) []runtime.Object {
	buckets := []runtime.Object{}
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

// imagePullSecret to use to apply to pods.  Ignored if empty.
var imagePullSecret string

// SetImagePullSecret sets the privaye image pull secret for this module.
// TODO: globals are banned!!
func SetImagePullSecret(s string) {
	imagePullSecret = s
}

// ApplyImagePullSecret adds an image pull secret to all the Couchbase server pods so that
// they can use private repositories.
func ApplyImagePullSecret(cluster *couchbasev2.CouchbaseCluster) {
	if imagePullSecret != "" {
		for i := range cluster.Spec.Servers {
			if cluster.Spec.Servers[i].Pod == nil {
				cluster.Spec.Servers[i].Pod = &v1.PodTemplateSpec{}
			}
			cluster.Spec.Servers[i].Pod.Spec.ImagePullSecrets = []v1.LocalObjectReference{
				{
					Name: imagePullSecret,
				},
			}
		}
	}
}

// basic 3 node cluster
func NewBasicCluster(size int) *couchbasev2.CouchbaseCluster {
	spec := couchbasev2.ClusterSpec{
		Image: e2e_constants.CouchbaseServerImage,
		Security: couchbasev2.CouchbaseClusterSecuritySpec{
			AdminSecret: e2e_constants.KubeTestSecretName,
			RBAC: couchbasev2.RBAC{
				Managed: true,
			},
		},
		Buckets: couchbasev2.Buckets{
			Managed: true,
		},
		Servers: []couchbasev2.ServerConfig{couchbasev2.ServerConfig{
			Size: size,
			Name: "test_config_1",
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.QueryService,
				couchbasev2.IndexService,
			},
		}},
	}
	if platform != "" {
		spec.Platform = platform
	}
	return NewClusterCRD(e2e_constants.ClusterNamePrefix, spec)
}

func NewBasicClusterSpec(size int) *couchbasev2.CouchbaseCluster {
	spec := couchbasev2.ClusterSpec{
		Image: e2e_constants.CouchbaseServerImage,
		Security: couchbasev2.CouchbaseClusterSecuritySpec{
			AdminSecret: e2e_constants.KubeTestSecretName,
		},
		Buckets: couchbasev2.Buckets{
			Managed: true,
		},
		Servers: []couchbasev2.ServerConfig{couchbasev2.ServerConfig{
			Size: size,
			Name: "test_config_1",
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.QueryService,
				couchbasev2.IndexService,
			},
		}},
	}
	if platform != "" {
		spec.Platform = platform
	}
	return NewClusterCRD(e2e_constants.ClusterNamePrefix, spec)
}

// NewSupportableClusterSpec returns a basic supportable cluster spec with a stateful and stateless
// MDS groups of the defined size.  They use default and logs volume mounts respectively.
func NewSupportableClusterSpec(size int) couchbasev2.ClusterSpec {
	spec := couchbasev2.ClusterSpec{
		Image: e2e_constants.CouchbaseServerImage,
		Security: couchbasev2.CouchbaseClusterSecuritySpec{
			AdminSecret: e2e_constants.KubeTestSecretName,
		},
		Buckets: couchbasev2.Buckets{
			Managed: true,
		},
		Servers: []couchbasev2.ServerConfig{
			{
				Name: "stateful",
				Size: size,
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
				Size: size,
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
							v1.ResourceStorage: *NewResourceQuantityMi(1024),
						},
					},
				},
			},
		},
	}

	// The defaults are too aggressive.  When killing a pod during a rebalance the operator
	// may hang for ~30 seconds due to network retries. During this period we may or may not
	// observe a failover leading to non-determinism.
	spec.ClusterSettings.AutoFailoverTimeout = NewDurationS(120)

	if platform != "" {
		spec.Platform = platform
	}

	return spec
}

func NewBackupCluster(size int, imageName string) *couchbasev2.CouchbaseCluster {
	spec := NewBasicClusterSpec(size)
	spec.Spec.Backup.Managed = true
	spec.Spec.Backup.ServiceAccount = config.BackupResourceName
	if imageName = strings.TrimSpace(imageName); imageName != "" {
		spec.Spec.Backup.Image = imageName
	}
	return spec
}

// NewSupportableCluster returns a basic supportable cluster with a stateful and stateless
// MDS groups of the defined size.  They use default and logs volume mounts respectively.
func NewSupportableCluster(size int) *couchbasev2.CouchbaseCluster {
	spec := NewSupportableClusterSpec(size)
	return NewClusterCRD(e2e_constants.ClusterNamePrefix, spec)
}

// basic 3 node cluster with Xdcr cluster
// TODO: This doesn't appear to do anything special any more.
func NewBasicXdcrCluster(size int) *couchbasev2.CouchbaseCluster {
	spec := couchbasev2.ClusterSpec{
		Image: e2e_constants.CouchbaseServerImage,
		Security: couchbasev2.CouchbaseClusterSecuritySpec{
			AdminSecret: e2e_constants.KubeTestSecretName,
		},
		Buckets: couchbasev2.Buckets{
			Managed: true,
		},
		Servers: []couchbasev2.ServerConfig{couchbasev2.ServerConfig{
			Size: size,
			Name: "test_config_1",
			Services: couchbasev2.ServiceList{
				couchbasev2.DataService,
				couchbasev2.QueryService,
				couchbasev2.IndexService,
			},
		}},
	}
	spec.ClusterSettings.AutoFailoverTimeout = NewDurationS(30)
	spec.ClusterSettings.AutoFailoverMaxCount = 3
	if platform != "" {
		spec.Platform = platform
	}
	return NewClusterCRD(e2e_constants.ClusterNamePrefix, spec)
}

func CreateClusterCRD(genName string, spec couchbasev2.ClusterSpec) *couchbasev2.CouchbaseCluster {
	return NewClusterCRD(genName, spec)
}

// Stateful 3 node cluster with a single volume.
// Spec will request 1Gb of storage (minikube default is 5gb).
func NewStatefulCluster(size int) *couchbasev2.CouchbaseCluster {

	crd := NewBasicCluster(size)
	couchbase := "couchbase"
	crd.Spec.Servers[0].VolumeMounts = &couchbasev2.VolumeMounts{
		DefaultClaim: couchbase,
	}

	resources := CreateResources(v1.ResourceStorage, 1, 1, "Gi")
	claim := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "couchbase",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			StorageClassName: &storageClassName,
			Resources:        resources,
		},
	}
	crd.Spec.VolumeClaimTemplates = []v1.PersistentVolumeClaim{claim}
	return crd
}

// NewClusterCRD creates a new Couchbase cluster CRD and associates the specified
// specification with it.  The cluster name may be dynamically generated by the
// K8S manager or explicitly defined where we need to know it ahead of time e.g.
// TLS.  TLS policy is also applied based on global settings
func NewClusterCRD(genName string, spec couchbasev2.ClusterSpec) *couchbasev2.CouchbaseCluster {
	return &couchbasev2.CouchbaseCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       couchbasev2.ClusterCRDResourceKind,
			APIVersion: couchbasev2.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genName,
		},
		Spec: spec,
	}
}

// Create Pod Policy with memory limit and requests in MB
func CreateMemoryResources(request, limit int) v1.ResourceRequirements {
	return CreateResources(v1.ResourceMemory, request, limit, "Mi")
}

// Create limit and request pod policy according to scale... ie 'Mi, Gi' where applicable
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
