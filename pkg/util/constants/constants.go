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
