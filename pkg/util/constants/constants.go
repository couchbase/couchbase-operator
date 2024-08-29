package constants

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	EnvOperatorPodName             = "POD_NAME"
	EnvOperatorPodNamespace        = "MY_POD_NAMESPACE"
	EnvCouchbaseImageName          = "RELATED_IMAGE_COUCHBASE_SERVER"
	EnvBackupImageName             = "RELATED_IMAGE_COUCHBASE_BACKUP"
	EnvMetricsImageName            = "RELATED_IMAGE_COUCHBASE_METRICS"
	EnvCloudNativeGatewayImageName = "RELATED_IMAGE_COUCHBASE_CLOUD_NATIVE_GATEWAY"
	EnvDigestsConfigMap            = "IMAGE_DIGESTS_CONFIG_MAP"
	AuthSecretUsernameKey          = "username"
	AuthSecretPasswordKey          = "password"
)

// Cloud Native Gateway Constants.
const (
	CloudNativeGatewayOtlpFlag = "--otlp-endpoint"
)

const (
	IntMax = int(^uint(0) >> 1)
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
	PVCImageAnnotation            = "pvc.couchbase.com/image"
	SVCSpecAnnotation             = "svc.couchbase.com/spec"
	PodTLSAnnotation              = "pod.couchbase.com/tls"
	PodInitializedAnnotation      = "pod.couchbase.com/initialized"
	CouchbaseVersionAnnotationKey = "server.couchbase.com/version"
	ResourceVersionAnnotation     = "operator.couchbase.com/version"

	// Local storage annotation is used to identify a storage class
	// that does not offer dynamic provisioning.
	LocalStorageAnnotation = "storage.couchbase.com/local"

	// AddNodeInsecureAnnotation is an experimental and insecure annotation
	// that enforces the use of HTTP use when clustering Couchbase Nodes.
	// This will only work for Server versions 6.5 - 7.0 since 7.1 will
	// enforce the use https.
	AddNodeInsecureAnnotation = "server.couchbase.com/add-node-insecure"

	// ConfigurationVersionAnnotation is used to flag who created resources e.g.
	// us, and for what version.  This gives us the ability in the future to reason
	// about what needs doing to upgrade the operator, or can be used by support as a
	// sanity check.
	ConfigurationVersionAnnotation = "config.couchbase.com/version"

	CronjobSpecAnnotation = "cronjob.couchbase.com/spec"

	JobSpecAnnotation = "job.couchbase.com/spec"
	// PodInitializedAnnotationMinVersion is the version pod.couchbase.com/initialized
	// first appeared in, so we shouldn't do anything with resources created on earlier
	// versions.
	PodInitializedAnnotationMinVersion = "2.2.0"

	// DefaultScopeOrCollectionName is the hard coded default for scope and collection
	// backward compatibility.
	DefaultScopeOrCollectionName = "_default"

	// CertManagerCAKey is the CA certificate key in the server secret, if provided.
	CertManagerCAKey = "ca.crt"

	// OperatorSecret* are keys into the legacy Operator TLS secret.  These are deprecated
	// and need deleting as soon as possible.
	OperatorSecretClientCertKey = "couchbase-operator.crt"
	OperatorSecretPrivateKeyKey = "couchbase-operator.key"
	OperatorSecretCAKey         = "ca.crt"

	// ClientSecret keys representing TLS resources for use by clients/sdks/sidecars.
	ClientSecretRootCA     = "ca.crt"
	ClientSecretServerCert = "tls.crt"
	ClientSecretServerKey  = "tls.key"
	ClientSecretMutualCert = "mtls.crt"
	ClientSecretMutualKey  = "mtls.key"

	DefaultBucketStorageBackend = "couchstore"

	// PKCS#12 secret vars.
	PKCS12FileName = "couchbase-server.p12"
	TLSPassword    = "tls-password"
)

// Label types added to pods.
const (
	App = "couchbase"

	LabelApp                = "app"
	LabelCluster            = "couchbase_cluster"
	LabelNode               = "couchbase_node"
	LabelNodeConf           = "couchbase_node_conf"
	LabelVolumeName         = "couchbase_volume"
	LabelServer             = "couchbase_server"
	LabelBackup             = "couchbase_backup"
	LabelBackupRestore      = "couchbase_restore"
	LabelServicePrefix      = "couchbase_service_"
	LabelCloudNativeGateway = "couchbase_cloud_native_gateway"

	AnnotationVolumeNodeConf             = "serverConfig" // TODO: perhaps change to LabelNodeConf for parity?
	AnnotationVolumeMountPath            = "path"
	AnnotationVolumeMountSubPaths        = "subpaths" // Additional paths associated with mount (internal use by only)
	AnnotationPrometheusScrape           = "prometheus.io/scrape"
	AnnotationPrometheusPath             = "prometheus.io/path"
	AnnotationPrometheusPort             = "prometheus.io/port"
	AnnotationPrometheusScheme           = "prometheus.io/scheme"
	AnnotationReschedule                 = "cao.couchbase.com/reschedule"
	AnnotationUnreconcilable             = "dac.couchbase.com/unreconcilable"
	AnnotationSkipDACValidation          = "dac.couchbase.com/skipvalidation"
	AnnotationDisableAdmissionController = "dac.couchbase.com/skipDAC"

	ServerGroupLabel    = corev1.LabelTopologyZone
	TopologyRegionLabel = corev1.LabelTopologyRegion

	EKSTopologyLabel   = "topology.ebs.csi.aws.com/zone"
	GKETopologyLabel   = "topology.gke.io/zone"
	AzureTopologyLabel = "topology.disk.csi.azure.com/zone"

	// Used to annotate services with names which will get syncronized to a cloud DNS provider.
	DNSAnnotation = "external-dns.alpha.kubernetes.io/hostname"

	CouchbaseContainerName   = "couchbase-server"
	CouchbaseTLSVolumeName   = "couchbase-server-tls"
	CouchbaseTLSCAVolumeName = "couchbase-server-tls-ca"

	// Name of private key mounted within server used to decrypt incoming passphrase.
	CouchbaseTLSPassphraseKey = "tls-passphrase-key"

	// Name of passphrase configmap script.
	CouchbaseTLSPassphraseScript = "tls-passphrase-script"

	// Name of the key containing the actual passphrase string within the Passphrase Secret.
	PassphraseSecretKey = "passphrase"

	EnabledValue = "enabled"
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
	// The DAC will not allow any version lower than this to run, it may be possible
	// but some features won't work most likely and lead to support requests.
	CouchbaseVersionMin = "6.5.0"
	// CommunityEditionImage and InvalidBaseImage must both be of versions equal to
	// or greater than CouchbaseVersionMin.

	// CommunityEditionImage is a version of CE that exists.  Sadly we have to
	// hard code this (not do a regex replace) as this only gets major releases,
	// mo minors or patches.
	CommunityEditionImage = "couchbase/server:community-6.6.0"
	// InvalidBaseImage is an invalid/nonexistant base image used for testing.
	InvalidBaseImage = "basecouch/123:enterprise-6.6.2"

	// MinimumCouchbaseVersionForCNG is the minimum CB version for CNG support.
	MinimumCouchbaseVersionForCNG = "7.2.2"

	// MinimumCouchbaseVersionNoCNGRestriction is the minimum cb version where we dont restrict
	// CNG version (due to the cbauth issue in versions before this).
	MinimumCouchbaseVersionNoCNGRestriction = "7.2.4"
	// MinimumCNGVersionWithCBAuthSupport is the first CNG version that has CBAuth support.
	MinimumCNGVersionWithCBAuthSupport = "0.2.0"
)

const (
	// VolumeDetachedAnnotation is attached to a PVC to give an indication of
	// when it was detected as not being attached to a pod.
	VolumeDetachedAnnotation = "pv.couchbase.com/detached"
)

const (
	// LDAPSecretCACert is the field within a k8s secret containing the cacert PEM.
	LDAPSecretCACert = "ca.crt"
	// LDAPSecretPassword is the field within a k8s secret containing the password PEM.
	LDAPSecretPassword = "password"
)

// ImageDigests is a sha256 to image conversion map
// The format is <image_type>-<semver>
// Only the semver matters. The image_type is just for readability
// to identify what kind of digest we are dealing with.
//
// When used in restricted mode all of these digests are pre-fetched.
// And since only digests an be used, if someone upgrades to another
// version we need to decode the from-to digest by looking at the semver.
// In the event that the digest does not exist in this map, there is an
// option to deploy a custom map via config map (EnvDigestsConfigMap),
// but if config map also isn't there then disaster will ensue.
var ImageDigests = map[string]string{
	"b21765563ba510c0b1ca43bc9287567761d901b8d00fee704031e8f405bfa501": "couchbase-6.5.0-3",
	"fd6d9c0ef033009e76d60dc36f55ce7f3aaa942a7be9c2b66c335eabc8f5b11e": "couchbase-6.5.1-1",
	"01343aa7f613173a990d57ddc8923af217f4dd4b83873f4d130a675d8e31d682": "couchbase-6.5.2-1",
	"6eb80268663ba53f2802a27128abb8399f79519c455bfa0f3810cc4dc029f193": "couchbase-6.6.1-4",
	"187046a848f32233e7e92705c57fa864b1d373c2078a92b51c9706bec6e372e5": "couchbase-6.6.2-1",
	"a1fa6e548734e2275b2f9748e264a829111c58b434540ca1fbf7101f3cbb0705": "couchbase-6.6.4-1",
	"76f227833f929115adbbbc28988f721b2af9aa825219ee9429482a2287a91603": "couchbase-6.6.5-1",
	"82b240169627d8d0fce211563be2a35ca85672770f6dfaf4160bcf2dfd89e835": "couchbase-7.0.0-4",
	"fa5d031059e005cd9d85983b1a120dab37fc60136cb699534b110f49d27388f7": "couchbase-7.0.1-2",
	"447c8450ee6007bf4d2973fd0d728b0c13ce5ddefffeeebc0d227b59e1e7227e": "couchbase-7.0.2-1",
	"bf35e17217e48540d7c58cba3724b7e57de6428afb71fcef0745e93983e5b03e": "couchbase-7.0.3-1",
	"05aad0f1d3a373b60dece893a9c185dcb0e0630aa6f0c0f310ad8767918fd2af": "couchbase-7.0.4-1",
	"3dfcb551ce71cf32cd52ae5b6adcbed736ee0375ce90a62dbdf3a2eb61d0e7b2": "couchbase-7.1.0-1",
	"c060eb5621e79fed7d945d87925cbe68dcc379e8c319df9a77119e0448ebce5f": "couchbase-7.1.2-1",
	"d0d1734a98fea7639793873d9a54c27d6be6e7838edad2a38e8d451d66be3497": "couchbase-7.1.3-1",
	"3d0a9de740110b924b1ad5e83bb1e36b308f1b53f9e76a50cffcbeda9d34ea78": "backup-6.5.1-104-1",
	"c0ab51854294d117c4ecf867b541ed6dc67410294d72f560cc33b038d98e4b76": "backup-6.6.0-102-2",
	"b470d46135ed798d89b21457aef97f949b46411cd0b74cb8e1de00122829885e": "backup-1.1.0-1",
	"2dbca31ce84ca3824c4a7321f9c3084fb154af7f2afdd9c7c9a512ece925dbc0": "backup-1.1.1-1",
	"21e3b4424e132b7d48814cc623dd6eaf755936edca3de7994911a8731a080d90": "backup-1.2.0-1",
	"e46c4e32e645df4b9a6a2cfda04fbf8ecb66da1fdf47d073de9ec49796bd3935": "backup-1.3.0-2",
	"7bcc296a4ce58bc0eccab5522712e97a92bc81f2b7e9b2fc7d1b7571a1a169cb": "backup-1.3.1-1",
	"b5a052fab4c635ab2d880a6ac771c66f554b9a4b5b1b53a73ba8ef1b573be372": "metrics-1.0.0-2",
	"18015c72d17a33a21ea221d48fddf493848fc1ca5702007f289369c5815fb3df": "metrics-1.0.0-5",
	"0854d57c7249a940ab31b451b6d4053d79e85648452da86738315605c00aafcb": "metrics-1.0.4-2",
	"b9ff3aec88f42f8e6164d61a1c5f845b4c3dd3f606ac552170d5c61311ce5784": "metrics-1.0.5-1",
	"fda5d8278acb72682ebc0151ab77f4c212ba3c7b850e3074b54408cd85cf0df6": "metrics-1.0.6-2",
	"d392e6c902f784abfc083c9bf5ce11895d0183347b6c21b259678fd85f312cd4": "metrics-1.0.7-1",
	"43ecf7c8efc841c169425ea14a8d4c69a788fe47fad4d159ed4c9d7bb83bbde7": "fluent-1.0.1-1",
	"e7d0e6cdc03f62de16c4e62d38f16f0f54714fea2339301a38885001c16f3d43": "fluent-1.0.3-1",
	"6eec92cceffeef11f37e2d3c82f3604f034f5f983bed2cce918a99883782e47a": "fluent-1.0.4-1",
	"87b292bef91e7a0297d0ca7578ad8d2bce2e62bb6d7d083b6e389a7605f488db": "fluent-1.1.0-1",
	"e158f1bc21fb5c758b4d7a787772ed081de511db5e0d3663eb692a53b94d47a6": "fluent-1.1.2-1",
	"95e0475087c863089374ec9a0e56000a146dd9005230c6920d857a3a0406ba37": "fluent-1.1.3-1",
	"8f2be774ca3f573b903ad4926cdf5e57769f2328d5a2e5a78c2af56f4c6cbbe3": "fluent-1.2.0-4",
	"b5475e68861b9b9b6595d907e6e0f8ad80b567adeeb1a915ef1578b6a6f7b6d6": "fluent-1.2.1-1",
}

const (
	// MagmaSeqTreeDataDefaultBlockSize default block size for Magma seqIndex.
	MagmaSeqTreeDataDefaultBlockSize = 4096
	// MagmaKeyTreeDataDefaultBlockSize default block size for Magma keyIndex.
	MagmaKeyTreeDataDefaultBlockSize = 4096
)
