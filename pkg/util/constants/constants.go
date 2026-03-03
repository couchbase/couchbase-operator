/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

	AutoFailoverTimeoutMin                                   uint64  = 5
	AutoFailoverTimeoutMax                                   uint64  = 3600
	AutoFailoverMaxCountMin                                  uint64  = 1
	AutoFailoverMaxCountMax                                  uint64  = 3
	AutoFailoverOnDataDiskIssuesTimePeriodMin                uint64  = 5
	AutoFailoverOnDataDiskIssuesTimePeriodMax                uint64  = 3600
	AutoFailoverOnDataDiskNonResponsivenessTimePeriodMin     float64 = 5
	AutoFailoverOnDataDiskNonResponsivenessTimePeriodDefault float64 = 120
	AutoFailoverOnDataDiskNonResponsivenessTimePeriodMax     float64 = 3600

	PodSpecAnnotation             = "pod.couchbase.com/spec"
	PVCSpecAnnotation             = "pvc.couchbase.com/spec"
	PVCImageAnnotation            = "pvc.couchbase.com/image"
	SVCSpecAnnotation             = "svc.couchbase.com/spec"
	PodTLSAnnotation              = "pod.couchbase.com/tls"
	PodInitializedAnnotation      = "pod.couchbase.com/initialized"
	CouchbaseVersionAnnotationKey = "server.couchbase.com/version"
	ResourceVersionAnnotation     = "operator.couchbase.com/version"
	CouchbaseHostnameAnnotation   = "server.couchbase.com/hostname"

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

	AnnotationVolumeNodeConf                  = "serverConfig" // TODO: perhaps change to LabelNodeConf for parity?
	AnnotationVolumeMountPath                 = "path"
	AnnotationVolumeMountSubPaths             = "subpaths" // Additional paths associated with mount (internal use by only)
	AnnotationPrometheusScrape                = "prometheus.io/scrape"
	AnnotationPrometheusPath                  = "prometheus.io/path"
	AnnotationPrometheusPort                  = "prometheus.io/port"
	AnnotationPrometheusScheme                = "prometheus.io/scheme"
	AnnotationReschedule                      = "cao.couchbase.com/reschedule"
	AnnotationUnreconcilable                  = "dac.couchbase.com/unreconcilable"
	AnnotationSkipDACValidation               = "dac.couchbase.com/skipvalidation"
	AnnotationDisableAdmissionController      = "dac.couchbase.com/skipDAC"
	AnnotationSkipClusterNameLengthValidation = "dac.couchbase.com/skipClusterNameLengthValidation"
	AnnotationForceDeleteLockfile             = "cao.couchbase.com/forceDeleteLockfile"

	AnnotationLastReconciledSpec = "operator.couchbase.com/lastReconciledSpec"

	ServerGroupLabel    = corev1.LabelTopologyZone
	TopologyRegionLabel = corev1.LabelTopologyRegion

	EKSTopologyLabel   = "topology.ebs.csi.aws.com/zone"
	GKETopologyLabel   = "topology.gke.io/zone"
	AzureTopologyLabel = "topology.disk.csi.azure.com/zone"

	// Used to annotate services with names which will get syncronized to a cloud DNS provider.
	DNSAnnotation = "external-dns.alpha.kubernetes.io/hostname"

	CouchbaseContainerName       = "couchbase-server"
	CouchbaseTLSVolumeName       = "couchbase-server-tls"
	CouchbaseTLSCAVolumeName     = "couchbase-server-tls-ca"
	CouchbaseKeyShadowVolumeName = "couchbase-server-key-shadow"

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
	CouchbaseVersionMin = "7.0.0"
	// CommunityEditionImage and InvalidBaseImage must both be of versions equal to
	// or greater than CouchbaseVersionMin.

	// CommunityEditionImage is a version of CE that exists.  Sadly we have to
	// hard code this (not do a regex replace) as this only gets major releases,
	// mo minors or patches.
	CommunityEditionImage = "couchbase/server:community-" + CouchbaseVersionMin
	// InvalidBaseImage is an invalid/nonexistant base image used for testing.
	InvalidBaseImage = "basecouch/123:enterprise-" + CouchbaseVersionMin

	// MinimumCouchbaseVersionForCNG is the minimum CB version for CNG support.
	MinimumCouchbaseVersionForCNG = "7.2.2"

	// MinimumCouchbaseVersionNoCNGRestriction is the minimum cb version where we dont restrict
	// CNG version (due to the cbauth issue in versions before this).
	MinimumCouchbaseVersionNoCNGRestriction = "7.2.4"
	// MinimumCNGVersionWithCBAuthSupport is the first CNG version that has CBAuth support.
	MinimumCNGVersionWithCBAuthSupport = "0.2.0"
	// MinimumVersionForMagmaDefaultBackend is the minimum version of Couchbase Server to set magma as the default bucket storage backend.
	MinimumVersionForMagmaDefaultBackend = "8.0.0"
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
	// cb server arm
	"7c4503efa96095ee55946932353382d0b24b0f7bdb80f4f5a995364007276c17": "couchbase-7.1.1-1",
	"9f9726b0ce3e23951a3c28882b1417f0afcbebd237428737f1ff4e953c0fd780": "couchbase-7.1.2-1",
	"4f1455906c40f6dfc1e003496714f38f8a82d208683f521fbd75543406c8d12a": "couchbase-7.1.3-1",
	"1f2995726138ace5ae6a090c211281b6218907a993383dd3ef670c99ec4ef6a3": "couchbase-7.1.4-1",
	"ed2a80bc5e0a99765daa3f2d2c610d342b8a8344b8669244c860b44b76483956": "couchbase-7.1.5-1",
	"5c172561bb64ff4767a11c880cfb9fb98b0f928033b42b3a894c2d9872d5fdad": "couchbase-7.1.6-1",
	"0917047f068ad49bca49925650d18c213ac7d9ef2088496bc88539565498cd13": "couchbase-7.2.0-1",
	"bc1b650aefbd2ff20a3335a189b141444da06deb050d2b19d77cdb02f9e7771e": "couchbase-7.2.2-1",
	"ab9eda172556dbc41c5c999e583fce5137235df79c424370fe18bf860ea84617": "couchbase-7.2.3-1",
	"0bcc269a5628ffdb6298548916d828b36e3d00fd0dd33aad5c16d22d53fe5307": "couchbase-7.2.4-1",
	"42d05c5374b7ddcbdb67a0dfec3e324a00c1fd7ffb1c07cda569caac5bd2523e": "couchbase-7.2.5-1",
	"a971f83bdce43e12843c997d66ec7cbb1f16f8119bdb91bd053c71dd101ee562": "couchbase-7.2.6-1",
	"e6485648247318a829a270c1ec5688097887458c555e5c2af339c21ea9f940ca": "couchbase-7.2.7-1",
	"f1ba04960f89918132a742b7acbb4e1505f271279795348c546d11b9ad89afd3": "couchbase-7.2.8-1",
	"7f01ca00a1b7bd929e0e3b0a3ac065af85a045d81c072f8faa8567bcbec5a702": "couchbase-7.6.0-1",
	"3fd3961a96a326f3d293b3c18f5579b16420ff8a30549889fc2c28e0133162bc": "couchbase-7.6.1-1",
	"966c7d62ef86b4e93cef20de27b12060ede48b8e482d2ecd7e35cb2da4ad729b": "couchbase-7.6.2-1",
	"e09538db5a7b9c32590df919b81649f4e38df9b019c2719dff4e54742191a4ff": "couchbase-7.6.3-1",
	"45f4771b3cf8bf8f4ac66d66fa77b0e8c19a269e9b24c980248e12b9d818e7f3": "couchbase-7.6.4-1",
	"2c0c0dbeb10b6dc78d9d84157e69a6956c9ac073d835d6a5c13d9302c139c28b": "couchbase-7.6.5-1",
	"b745ba4f0e174b23d1a28644badcd77d0b33139820b48809b090ab0e274328bb": "couchbase-7.6.6-1",
	"c854f41459b3df75db30749bc46013ba861424a72cbfda716f9a211e245b46ff": "couchbase-7.6.7-1",
	"0d6daae7fe12789cfe55318e65f5219d4422887f9860b9a0c881b8e73631ffd5": "couchbase-7.6.8-1",
	"2dcb640e49f776c0152e47da9f787d05d44d2010a7aab41767a0e1548a4b8ef3": "couchbase-8.0.0-1",

	// cb server amd
	"a7dd51bfab8526b130315d795f31ab3e74a742fee9db7633f331415f405b3869": "couchbase-7.1.1-1",
	"bab250ffefe7690bb566c5823f06b38729099acc78d8201f7cdc871791c8b198": "couchbase-7.1.2-1",
	"960efff5fd034bede20fedaed1de6061d99eb428a1edd2d36ccc6fda98e443e7": "couchbase-7.1.3-1",
	"5ca7721d1b56b5e4c60009b1a1c07281ab39f810017f507d31b3fda345a236a1": "couchbase-7.1.4-1",
	"d6a8cc987a6fa1800c3f2b5248d1b3727aae515dd9cef8df672b7a03b310bb12": "couchbase-7.1.5-1",
	"9d58509839d77776bc38396ae2933f82740a3768c6c3a5312df1d8690def7776": "couchbase-7.1.6-1",
	"f88667ccf53ffc24c706b437b74688974130cfdabdd369adc7358f3c14da4fc2": "couchbase-7.2.0-1",
	"0d2371be2890316ed7f6c0f687740018d40cf539d0d08ee25afb4d08624498c2": "couchbase-7.2.2-1",
	"5e79f4fe8da6a158311bcbff133c1ce1ed7e24f7f8bae749bc2196a534a5c055": "couchbase-7.2.3-1",
	"8c4dca17f19f0b545da5fa9d6f775bb3a1032efc546c5fc07466f44de342ad61": "couchbase-7.2.4-1",
	"cfb7f69021f193805a8e807a9002aeeaa43c6b9d873d85cae21e00aed2b79338": "couchbase-7.2.5-1",
	"03cc47e30b1b49304906a77216b99f40b09e0d2cff50f291dcdd57e94a695456": "couchbase-7.2.6-1",
	"46474fc0b75edcc491a1edc9c358fd08d3dac2d6634e010c4f5826d34e430fd3": "couchbase-7.2.7-1",
	"eedbee0789a3a39e336ba91c5c55d386e62337009f4060d67194ed4420506a87": "couchbase-7.2.8-1",
	"ffc3e41d03e6b862b090b90fc0e973f426d322443eba988ba603e9f352bc5a89": "couchbase-7.6.0-1",
	"6eef19c5bf2d0ba262554777770828702d728bb628bbb0057c8cb78d6bf74f73": "couchbase-7.6.1-1",
	"2848ed9f6be2cb981a35c434d949c31c000c88e51a43c5567149bf6de986aa7e": "couchbase-7.6.2-1",
	"0ef3e409859943a331715f267f9740c31250e0dfc6bd12b74cee160f549ffa27": "couchbase-7.6.3-1",
	"334ea378837e7bf55ea13f1bbc2764dabd54eac84a6b93baa34d59218bde48ea": "couchbase-7.6.4-1",
	"49fb48f93e3cd04efd4dea3bd154b0b5cdecf8f031830db67c73b494d1510202": "couchbase-7.6.5-1",
	"a1f47c462ae376d7ad7d1f23e5a20e2f63415692b86d1a2d0e4b298cb368e151": "couchbase-7.6.6-1",
	"27ad531a891a14cc887ad87f65856fd7e6ee81d24615d67180f097edfc86a9b7": "couchbase-7.6.7-1",
	"4d29ea18f9880ab6d7dc0a462eb514371601349853836e2692a89da8d0009351": "couchbase-7.6.8-1",
	"d4b4da9969ee5bc0b15ac9f0ebc1e46b8e5ba6ab94343225dbcb2ca776371516": "couchbase-8.0.0-1",

	// Legacy images
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

// Defaults for bucket API.
const (
	// MagmaSeqTreeDataDefaultBlockSize default block size for Magma seqIndex.
	MagmaSeqTreeDataDefaultBlockSize = 4096
	// MagmaKeyTreeDataDefaultBlockSize default block size for Magma keyIndex.
	MagmaKeyTreeDataDefaultBlockSize = 4096

	// VersionPruningWindowHrsDefault default number of hours to retain version history for a bucket.
	VersionPruningWindowHrsDefault = 720

	// ExpiryPagerSleepTimeDefaultSeconds default number of seconds to sleep between expiry pager runs.
	ExpiryPagerSleepTimeDefaultSeconds = 600

	// BucketWarmupBehaviorDefault default warmup behavior for a bucket.
	BucketWarmupBehaviorDefault = "background"

	// MemoryLowWatermarkDefault default memory low watermark for a bucket.
	MemoryLowWatermarkDefault = 75

	// MemoryHighWatermarkDefault default memory high watermark for a bucket.
	MemoryHighWatermarkDefault = 85

	// DurabilityImpossibleFallbackDefault default durability impossible fallback for a bucket.
	DurabilityImpossibleFallbackDefault = "disabled"

	// DefaultEncryptionAtRestRotationInterval default rotation interval for a bucket.
	DefaultEncryptionAtRestRotationInterval = 0

	// DefaultEncryptionAtRestKeyLifetime default key lifetime for a bucket.
	DefaultEncryptionAtRestKeyLifetime = 0

	// DefaultEncryptionAtRestKeyID default key id for a bucket.
	DefaultEncryptionAtRestKeyID = -1

	// DefaultNumVBucketsMagma is the default number of vbuckets for a magma bucket.
	DefaultNumVBucketsMagma int = 128

	// DefaultNumVBucketsCouchstore is the default number of vbuckets for a couchstore bucket.
	DefaultNumVBucketsCouchstore int = 1024
)

// Defaults for AutoFailover API.
const (
	DefaultAllowFailoverEphemeralNoReplicas = false
)

// Encryption at rest usage types.
const (
	// EncryptionAtRestUsageConfiguration is the usage type for configuration encryption.
	EncryptionAtRestUsageConfiguration = "configuration"

	// EncryptionAtRestUsageAudit is the usage type for audit encryption.
	EncryptionAtRestUsageAudit = "audit"

	// EncryptionAtRestUsageLog is the usage type for log encryption.
	EncryptionAtRestUsageLog = "log"
)

const (
	// EncryptionKeyUsageBucketEncryptionPrefix is the prefix for bucket encryption usage
	// a specific bucket. For a bucket names default the usage will be "bucket-encryption-default".
	EncryptionKeyUsageBucketEncryptionPrefix = "bucket-encryption"

	// EncryptionKeyFinalizerPrefix is the finalizer for encryption keys.
	EncryptionKeyFinalizerPrefix = "encryptionkey.couchbase.com/finalizer"

	// AWSCredentialsSecretKey is the key in the AWS credentials secret that contains the credentials file.
	AWSCredentialsSecretKey = "credentials"

	// KMIPClientSecretKey is the key in the KMIP client secret that contains the client private key passphrase.
	KMIPClientSecretPassphraseKey = "passphrase"

	// KMIPClientSecretCertKey is the key in the KMIP client secret that contains the client private key certificate.
	KMIPClientSecretCertKey = "tls.crt"

	// KMIPClientSecretKeyKey is the key in the KMIP client secret that contains the client private key.
	KMIPClientSecretKeyKey = "tls.key"
)
