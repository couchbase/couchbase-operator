/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
	SVCSpecAnnotation             = "svc.couchbase.com/spec"
	PodTLSAnnotation              = "pod.couchbase.com/tls"
	PodInitializedAnnotation      = "pod.couchbase.com/initialized"
	CouchbaseVersionAnnotationKey = "server.couchbase.com/version"
	ResourceVersionAnnotation     = "operator.couchbase.com/version"

	CronjobSpecAnnotation = "cronjob.couchbase.com/spec"

	// PodInitializedAnnotationMinVersion is the version pod.couchbase.com/initialized
	// first appeared in, so we shouldn't do anything with resources created on earlier
	// versions.
	PodInitializedAnnotationMinVersion = "2.2.0"
)

// Label types added to pods.
const (
	App = "couchbase"

	LabelApp           = "app"
	LabelCluster       = "couchbase_cluster"
	LabelNode          = "couchbase_node"
	LabelNodeConf      = "couchbase_node_conf"
	LabelVolumeName    = "couchbase_volume"
	LabelServer        = "couchbase_server"
	LabelBackup        = "couchbase_backup"
	LabelBackupRestore = "couchbase_restore"
	LabelServicePrefix = "couchbase_service_"

	AnnotationVolumeNodeConf   = "serverConfig" // TODO: perhaps change to LabelNodeConf for parity?
	AnnotationVolumeMountPath  = "path"
	AnnotationPrometheusScrape = "prometheus.io/scrape"
	AnnotationPrometheusPath   = "prometheus.io/path"
	AnnotationPrometheusPort   = "prometheus.io/port"

	ServerGroupLabel = "failure-domain.beta.kubernetes.io/zone"

	// Used to annotate services with names which will get syncronized to a cloud DNS provider.
	DNSAnnotation = "external-dns.alpha.kubernetes.io/hostname"

	CouchbaseContainerName = "couchbase-server"
	CouchbaseTLSVolumeName = "couchbase-server-tls"

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
	"187046a848f32233e7e92705c57fa864b1d373c2078a92b51c9706bec6e372e5": "couchbase-6.6.2-1",
	"cb8c5aba14feb955854a17c0923f79c8476872f86b3f52570d859b991d23231b": "couchbase-6.6.3-1",
	"43a5c1abd5f85ec09019b8794e082e28ae2562195046660d2ead7f059ba67f64": "couchbase-7.0.0-1",
	"336f88411f8889c5c22f50bd88f0bb2606393d87a5a302525057b3969db14b92": "couchbase-7.0.1-1",
	"3d0a9de740110b924b1ad5e83bb1e36b308f1b53f9e76a50cffcbeda9d34ea78": "backup-6.5.1-104-1",
	"c0ab51854294d117c4ecf867b541ed6dc67410294d72f560cc33b038d98e4b76": "backup-6.6.0-102-2",
	"b470d46135ed798d89b21457aef97f949b46411cd0b74cb8e1de00122829885e": "backup-1.1.0-1",
	"b5a052fab4c635ab2d880a6ac771c66f554b9a4b5b1b53a73ba8ef1b573be372": "metrics-1.0.0-2",
	"18015c72d17a33a21ea221d48fddf493848fc1ca5702007f289369c5815fb3df": "metrics-1.0.0-5",
	"0854d57c7249a940ab31b451b6d4053d79e85648452da86738315605c00aafcb": "metrics-1.0.4-2",
	"b9ff3aec88f42f8e6164d61a1c5f845b4c3dd3f606ac552170d5c61311ce5784": "metrics-1.0.5-1",
	"43ecf7c8efc841c169425ea14a8d4c69a788fe47fad4d159ed4c9d7bb83bbde7": "fluent-1.0.1-1",
	"e7d0e6cdc03f62de16c4e62d38f16f0f54714fea2339301a38885001c16f3d43": "fluent-1.0.3-1",
	"6eec92cceffeef11f37e2d3c82f3604f034f5f983bed2cce918a99883782e47a": "fluent-1.0.4-1",
	"87b292bef91e7a0297d0ca7578ad8d2bce2e62bb6d7d083b6e389a7605f488db": "fluent-1.1.0-1",
}
