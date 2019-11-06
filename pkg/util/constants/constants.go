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
	BucketTypeMemcached = "memcached"

	DefaultDataPath  = "/opt/couchbase/var/lib/couchbase/data"
	DefaultIndexPath = "/opt/couchbase/var/lib/couchbase/data"

	AutoFailoverTimeoutMin                    uint64 = 5
	AutoFailoverTimeoutMax                    uint64 = 3600
	AutoFailoverMaxCountMin                   uint64 = 1
	AutoFailoverMaxCountMax                   uint64 = 3
	AutoFailoverOnDataDiskIssuesTimePeriodMin uint64 = 5
	AutoFailoverOnDataDiskIssuesTimePeriodMax uint64 = 3600

	PodSpecAnnotation             = "pod.couchbase.com/spec"
	PodTLSAnnotation              = "pod.couchbase.com/tls"
	CouchbaseVersionAnnotationKey = "server.couchbase.com/version"
	ResourceVersionAnnotation     = "operator.couchbase.com/version"
)

// Label types added to pods
const (
	App = "couchbase"

	LabelApp        = "app"
	LabelCluster    = "couchbase_cluster"
	LabelNode       = "couchbase_node"
	LabelNodeConf   = "couchbase_node_conf"
	LabelVolumeName = "couchbase_volume"

	AnnotationVolumeNodeConf    = "serverConfig" // TODO: perhaps change to LabelNodeConf for parity?
	AnnotationVolumeMountPath   = "path"
	AnnotationVolumeBindingMode = "storageclass.couchbase.com/binding-mode"

	ServerGroupLabel = "failure-domain.beta.kubernetes.io/zone"

	// Used to annotate services with names which will get syncronized to a cloud DNS provider.
	DNSAnnotation = "external-dns.alpha.kubernetes.io/hostname"

	CouchbaseContainerName = "couchbase-server"
	CouchbaseTLSVolumeName = "couchbase-server-tls"
)

// Represents the Kubernetes version by its major and minor parts. The first
// two digits represent the major version and the last two digits represent the
// minor version. We do not track the maintenance version.
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
	major := vstr[0:2]
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

const (
	// VolumeDetachedAnnotation is attached to a PVC to give an indication of
	// when it was detected as not being attached to a pod.
	VolumeDetachedAnnotation = "pv.couchbase.com/detached"
)

// Cluster and Bucket RBAC Roles (as of 6.0)
var ClusterRoles = []string{"admin", "cluster_admin", "security_admin", "ro_admin", "replication_admin", "query_external_access", "query_system_catalog", "analytics_reader"}
var BucketRoles = []string{"bucket_admin", "views_admin", "fts_admin", "data_reader", "data_writer", "data_dcp_reader", "data_backup", "data_monitoring", "replication_target", "analytics_manager", "views_reader", "fts_searcher", "query_select", "query_update", "query_insert", "query_delete", "query_manage_index"}
