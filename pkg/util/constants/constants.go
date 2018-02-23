package constants

const (
	EnvOperatorPodName      = "MY_POD_NAME"
	EnvOperatorPodNamespace = "MY_POD_NAMESPACE"
	AuthSecretUsernameKey   = "username"
	AuthSecretPasswordKey   = "password"
)

var (
	BucketTypeCouchbase = "couchbase"
	BucketTypeEphemeral = "ephemeral"
	BucketTypeMembase   = "membase"
	BucketTypeMemcached = "memcached"

	BucketReplicasZero  = 0
	BucketReplicasOne   = 1
	BucketReplicasTwo   = 2
	BucketReplicasThree = 3

	BucketIoPriorityHigh = "high"
	BucketIoPriorityLow  = "low"

	BucketEvictionPolicyValueOnly    = "valueOnly"
	BucketEvictionPolicyFullEviction = "fullEviction"
	BucketEvictionPolicyNRUEviction  = "nruEviction"
	BucketEvictionPolicyNoEviction   = "noEviction"

	BucketConflictResolutionSeqno     = "seqno"
	BucketConflictResolutionTimestamp = "lww"

	BucketFlushEnabled  = true
	BucketFlushDisabled = false

	BucketIndexReplicasEnabled  = true
	BucketIndexReplicasDisabled = false

	IndexStorageModeMemoryOptimized = "memory_optimized"
	IndexStorageModePlasma          = "plasma"

	DefaultDataPath  = "/opt/couchbase/var/lib/couchbase"
	DefaultIndexPath = "/opt/couchbase/var/lib/couchbase"
)

// Represents the Kubernetes version by its major and minor parts. The first
// two digits represent the major version and the last two digits represent the
// minor version. We do not track the maintainence version.
type KubernetesVersion string

const (
	KubernetesVersionUnknown KubernetesVersion = "0000"
	KubernetesVersion1_7     KubernetesVersion = "0107"
	KubernetesVersion1_8     KubernetesVersion = "0108"
	KubernetesVersion1_9     KubernetesVersion = "0109"
	KubernetesVersion1_10    KubernetesVersion = "0110"
	KubernetesVersionMax     KubernetesVersion = "9999"
)
