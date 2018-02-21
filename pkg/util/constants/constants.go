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
)

type KubernetesVersion string

const (
	KubernetesVersionUnknown KubernetesVersion = "0000"
	KubernetesVersion1_7     KubernetesVersion = "0107"
	KubernetesVersion1_8     KubernetesVersion = "0108"
	KubernetesVersion1_9     KubernetesVersion = "0109"
	KubernetesVersion1_10    KubernetesVersion = "0110"
)
