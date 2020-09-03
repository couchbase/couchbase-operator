package constants

const (
	EnvOperatorPodName      = "MY_POD_NAME"
	EnvOperatorPodNamespace = "MY_POD_NAMESPACE"
	EnvCouchbaseImageName   = "RELATED_IMAGE_COUCHBASE_SERVER"
	EnvBackupImageName      = "RELATED_IMAGE_COUCHBASE_BACKUP"
	EnvMetricsImageName     = "RELATED_IMAGE_COUCHBASE_METRICS"
	EnvDigestsConfigMap     = "IMAGE_DIGESTS_CONFIG_MAP"
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
	PVCSpecAnnotation             = "pvc.couchbase.com/spec"
	SVCSpecAnnotation             = "svc.couchbase.com/spec"
	PodTLSAnnotation              = "pod.couchbase.com/tls"
	CouchbaseVersionAnnotationKey = "server.couchbase.com/version"
	ResourceVersionAnnotation     = "operator.couchbase.com/version"

	CronjobSpecAnnotation = "cronjob.couchbase.com/spec"
)

// Label types added to pods.
const (
	App = "couchbase"

	LabelApp           = "app"
	LabelCluster       = "couchbase_cluster"
	LabelNode          = "couchbase_node"
	LabelNodeConf      = "couchbase_node_conf"
	LabelVolumeName    = "couchbase_volume"
	LabelBackup        = "couchbase_backup"
	LabelBackupRestore = "couchbase_restore"

	AnnotationVolumeNodeConf  = "serverConfig" // TODO: perhaps change to LabelNodeConf for parity?
	AnnotationVolumeMountPath = "path"

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
	CouchbaseVersionMin = "enterprise-6.0.4"
	CouchbaseVersion650 = "enterprise-6.5.0"
)

const (
	// VolumeDetachedAnnotation is attached to a PVC to give an indication of
	// when it was detected as not being attached to a pod.
	VolumeDetachedAnnotation = "pv.couchbase.com/detached"
)

const (
	// LDAPSecretCACert is the field within a k8s secret containing the cacert PEM
	LDAPSecretCACert = "ca.crt"
	// LDAPSecretPassword is the field within a k8s secret containing the password PEM
	LDAPSecretPassword = "password"
)

// ImageDigests is a sha256 to image conversion map
// ¯\_(ツ)_/¯
//
// TODO: Use downward api to VolumeMount annotations into Pod and do this lookup because
// annotations can only be accessed in volumes and not in environment variables."
// https://docs.openshift.com/container-platform/4.3/nodes/containers/nodes-containers-downward-api.html
var ImageDigests = map[string]string{
	"e83852666816dd0a0d86180a0867aa25458c21702f28e5903bb012e079ebe055": "couchbase-6.0.4-1",
	"b21765563ba510c0b1ca43bc9287567761d901b8d00fee704031e8f405bfa501": "couchbase-6.5.0-3",
	"fd6d9c0ef033009e76d60dc36f55ce7f3aaa942a7be9c2b66c335eabc8f5b11e": "couchbase-6.5.1-1",
	"218080954d616b78405a42e0df7561b7cd1bae09c4b2addc9cc56244d1eab0e0": "backup-6.5.0-3",
	"d499d8a7b1ee0682994a422ccaa5759dc9ba093ca48a60ea668a8149a747057d": "backup-6.5.1-1",
	"3d0a9de740110b924b1ad5e83bb1e36b308f1b53f9e76a50cffcbeda9d34ea78": "backup-6.5.1-104-1",
	"b5a052fab4c635ab2d880a6ac771c66f554b9a4b5b1b53a73ba8ef1b573be372": "metrics-1.0.0-2",
	"18015c72d17a33a21ea221d48fddf493848fc1ca5702007f289369c5815fb3df": "metrics-1.0.0-5",
}
