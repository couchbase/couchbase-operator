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

	DefaultDataPath  = "/opt/couchbase/var/lib/couchbase/data"
	DefaultIndexPath = "/opt/couchbase/var/lib/couchbase/data"

	ServiceIndex  = "index"
	ServiceData   = "data"
	ServiceQuery  = "query"
	ServiceSearch = "search"

	AutoFailoverTimeoutMin                    uint64 = 5
	AutoFailoverTimeoutMax                    uint64 = 3600
	AutoFailoverMaxCountMin                   uint64 = 1
	AutoFailoverMaxCountMax                   uint64 = 3
	AutoFailoverOnDataDiskIssuesTimePeriodMin uint64 = 5
	AutoFailoverOnDataDiskIssuesTimePeriodMax uint64 = 3600
)

// Label types added to pods
const (
	LabelCluster  = "couchbase_cluster"
	LabelNodeConf = "couchbase_node_conf"

	ServerGroupLabel = "server-group.couchbase.com/zone"
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
	KubernetesVersion1_11    KubernetesVersion = "0111"
	KubernetesVersionMax     KubernetesVersion = "9999"
)

func (v KubernetesVersion) String() string {
	if v == KubernetesVersionMax || v == KubernetesVersionUnknown {
		return "unknown"
	}

	vstr := string(v)
	major := string(vstr[0:2])
	if v[0] == '0' {
		major = string(vstr[1])
	}

	minor := vstr[2:4]
	if v[2] == '0' {
		minor = string(vstr[3])
	}

	return major + "." + minor
}

const (
	CouchbaseVersionMin = "enterprise-5.5.0"
)
