package e2eutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	operator_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/analyzer"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	other_jsonpatch "github.com/evanphx/json-patch"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

// randomSuffix generates a 5 character random suffix to be appended to
// k8s resources to avoid namespace collisions (especially events).
func RandomSuffix() string {
	return RandomString(5)
}

// RandomString generates an arbitrary length random string.  Not cryptographically
// secure, but who cares, this is a test suite :D  At present this uses the dictionary
// 0-9a-z as that is compatible with DNS names and Couchbase Server passwords.
func RandomString(length int) string {
	// Seed the PRNG so we get vagely random suffixes across runs
	ranGen := rand.New(rand.New(rand.NewSource(time.Now().UnixNano())))

	// Generate a random 5 character suffix for the cluster name
	suffix := ""

	for i := 0; i < length; i++ {
		// Our alphabet is 0-9 a-z, so 36 characters
		ordinal := ranGen.Intn(36)
		// Less than 10 places it in the 0-9 range, otherwise in
		// the a-z range
		if ordinal < 10 {
			ordinal += int('0')
		} else {
			ordinal += int('a') - 10
		}

		// Append to the name
		suffix += string(rune(ordinal))
	}

	return suffix
}

// CreateNewClusterFromSpec creates a cluster and waits for various ready conditions.
// Performs retries and garbage collection in the event of transient failure.
func CreateNewClusterFromSpec(t *testing.T, k8s *types.Cluster, clusterSpec *couchbasev2.CouchbaseCluster, timeout int) *couchbasev2.CouchbaseCluster {
	// Create the cluster.
	cluster, err := CreateCluster(k8s, clusterSpec)
	if err != nil {
		Die(t, err)
	}

	if timeout != -1 {
		MustWaitClusterStatusHealthy(t, k8s, cluster, time.Duration(timeout*int(time.Minute)))
	}

	// Update the cluster status, this is important for the test, especially if the cluster
	// name is auto-generated.
	updatedCluster, err := getClusterCRD(k8s.CRClient, cluster)
	if err != nil {
		Die(t, err)
	}

	return updatedCluster
}

// MustNewClusterFromSpec calls the CreateNewClusterFromSpec function with a default
// timeout of 15 minutes.
func MustNewClusterFromSpec(t *testing.T, k8s *types.Cluster, clusterSpec *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	return CreateNewClusterFromSpec(t, k8s, clusterSpec, 15)
}

func MustNewClusterFromSpecAsync(t *testing.T, k8s *types.Cluster, clusterSpec *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	cluster, err := CreateCluster(k8s, clusterSpec)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// applyTLS optionally layers on server side TLS support to a cluster.
func applyTLS(cluster *couchbasev2.CouchbaseCluster, tls, clientTLS *TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	if tls == nil {
		return
	}

	// Add the explicit name generated for the cluster, and encoded in the TLS
	// certificates.  Also clear out the generate name that will most likely
	// have been implicitly filled in.
	cluster.Name = tls.ClusterName
	cluster.GenerateName = ""

	// All TLS is handled at this level, purely as an artifact of x509.go living
	// in this model.  As such this will not be filled in by the underlying
	// generators.
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{}

	switch {
	case tls.LegacyTLS():
		// Legacy mode just associates the bespoke secrets with the API
		// fields, the operator secret contains the cluster CA.  There can
		// only be one PKI used with legacy, so ignore the client TLS
		// entirely.
		cluster.Spec.Networking.TLS.Static = &couchbasev2.StaticTLS{
			ServerSecret:   tls.ClusterSecretName,
			OperatorSecret: tls.OperatorSecretName,
		}
	case tls.Source == TLSSourceKubernetesSecret:
		// In Kubernetes mode, the operator and cluster secrets contain
		// only the cert/key pairs, the CA needs to be applied to the
		// root certificates field.
		cluster.Spec.Networking.TLS.SecretSource = &couchbasev2.TLSSecretSource{
			ServerSecretName: tls.ClusterSecretName,
		}

		cluster.Spec.Networking.TLS.RootCAs = []string{
			tls.CASecretName,
		}

		// When client certification is in play, if we are using a separate CA
		// then that needs adding to the list of root CAs.
		if policy != nil {
			if clientTLS != nil {
				cluster.Spec.Networking.TLS.SecretSource.ClientSecretName = clientTLS.OperatorSecretName
				cluster.Spec.Networking.TLS.RootCAs = append(cluster.Spec.Networking.TLS.RootCAs, clientTLS.CASecretName)
			} else {
				cluster.Spec.Networking.TLS.SecretSource.ClientSecretName = tls.OperatorSecretName
			}
		}
	case tls.Source == TLSSourceCertManagerSecret:
		// Cert-manager mode just associates the bespoke secrets with the API
		// fields, the cluster secret contains the cluster CA.
		cluster.Spec.Networking.TLS.SecretSource = &couchbasev2.TLSSecretSource{
			ServerSecretName: tls.ClusterSecretName,
		}

		// When client certification is in play, if we are using a separate CA
		// then that needs adding to the list of root CAs.
		if policy != nil {
			if clientTLS != nil {
				cluster.Spec.Networking.TLS.SecretSource.ClientSecretName = clientTLS.OperatorSecretName
				cluster.Spec.Networking.TLS.RootCAs = append(cluster.Spec.Networking.TLS.RootCAs, clientTLS.CASecretName)
			} else {
				cluster.Spec.Networking.TLS.SecretSource.ClientSecretName = tls.OperatorSecretName
			}
		}
	}
}

// applyMTLS optionally layers on client side TLS support to a cluster.
func applyMTLS(cluster *couchbasev2.CouchbaseCluster, policy *couchbasev2.ClientCertificatePolicy) {
	if policy == nil {
		return
	}

	cluster.Spec.Networking.TLS.ClientCertificatePolicy = policy
	cluster.Spec.Networking.TLS.ClientCertificatePaths = []couchbasev2.ClientCertificatePath{
		{
			Path: "subject.cn",
		},
	}
}

// applyDNS optionally applies a custom DNS server to cluster pods.
func applyDNS(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, service *v1.Service) {
	if service == nil {
		return
	}

	for index := range cluster.Spec.Servers {
		if cluster.Spec.Servers[index].Pod == nil {
			cluster.Spec.Servers[index].Pod = &couchbasev2.PodTemplate{}
		}

		cluster.Spec.Servers[index].Pod.Spec.DNSPolicy = v1.DNSNone
		cluster.Spec.Servers[index].Pod.Spec.DNSConfig = &v1.PodDNSConfig{
			Nameservers: []string{
				service.Spec.ClusterIP,
			},
			Searches: getSearchDomains(k8s),
		}
	}
}

// applyS3 optionally applies S3 credentials to the backup configuration.
func applyS3(cluster *couchbasev2.CouchbaseCluster, secret *v1.Secret) {
	if secret == nil {
		return
	}

	cluster.Spec.Backup.S3Secret = secret.Name //nolint:staticcheck
}

// applyObjEndpoint optionally applies a custom obj endpoint to the backup config.
func applyObjEndpoint(cluster *couchbasev2.CouchbaseCluster, objEndpoint string, secret *v1.Secret) {
	if objEndpoint == "" {
		return
	}

	cluster.Spec.Backup.ObjectEndpoint = &couchbasev2.ObjectEndpoint{ //nolint:staticcheck
		URL: objEndpoint,
	}

	if secret != nil {
		cluster.Spec.Backup.ObjectEndpoint.CertSecret = secret.Name //nolint:staticcheck
	}
}

// applyIAMRole optionally sets backup to use host EC2 IAM Role.
func applyS3IAMRole(cluster *couchbasev2.CouchbaseCluster, useIAMRole bool) {
	cluster.Spec.Backup.UseIAMRole = useIAMRole //nolint:staticcheck
}

// applyGenericNetworking optionally applies generic networking to the cluster, this
// exposes the admin console to provide a HTTP load-balanced endpoint, giving HA
// service discovery, albeit with unstable IP based addressing, and exposed features,
// in this case XDCR only because they are legacy tests and should be taken out back
// and shot.
func applyGenericNetworking(cluster *couchbasev2.CouchbaseCluster, genericNetworking bool) {
	if !genericNetworking {
		return
	}

	// Set this to zero for generic networking, as node ports are pretty much
	// instantaneous, and will cause test timeouts.
	cluster.Spec.Networking.WaitForAddressReachableDelay = &metav1.Duration{}
	cluster.Spec.Networking.ExposeAdminConsole = true
	cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureXDCR,
	}
}

func applyLogStreaming(cluster *couchbasev2.CouchbaseCluster, config *couchbasev2.CouchbaseClusterLoggingConfigurationSpec) {
	cluster.Spec.Logging.Server = config
}

func applyAuditing(cluster *couchbasev2.CouchbaseCluster, config *couchbasev2.CouchbaseClusterAuditLoggingSpec) {
	cluster.Spec.Logging.Audit = config
}

func applyMonitoring(cluster *couchbasev2.CouchbaseCluster, config *couchbasev2.CouchbaseClusterMonitoringSpec) {
	cluster.Spec.Monitoring = config
}

// applyAnnotations optionally applies custom annotations to cluster pods.
func applyAnnotations(cluster *couchbasev2.CouchbaseCluster, annotations map[string]string) {
	if annotations == nil {
		return
	}

	for index := range cluster.Spec.Servers {
		if cluster.Spec.Servers[index].Pod == nil {
			cluster.Spec.Servers[index].Pod = &couchbasev2.PodTemplate{}
		}

		pod := cluster.Spec.Servers[index].Pod.DeepCopy()

		if pod.ObjectMeta.Annotations == nil {
			pod.ObjectMeta.Annotations = make(map[string]string)
		}

		for k, v := range annotations {
			pod.ObjectMeta.Annotations[k] = v
		}

		cluster.Spec.Servers[index].Pod = pod
	}
}

func applyCloudNativeGateway(cluster *couchbasev2.CouchbaseCluster, config *couchbasev2.CloudNativeGateway) {
	if config == nil {
		return
	}

	cluster.Spec.Networking.CloudNativeGateway = config
}

// ClusterOptions is used to generate or create all Couchbase clusters by the framework.
// The key observation is all clusters are ostensibly the same, with features layered on
// top.  We use the builder pattern to declare those features, so they are only defined
// in a single place.  You can still generate a cluster (as opposed to create one) in
// order to add more esoteric configuration that isn't quite generic enough to warrant
// a build step.
type ClusterOptions struct {
	Options *e2espec.ClusterOptions

	// TLS is a full PKI for server and client certificates...
	TLS *TLSContext

	// ClientTLS, if set, allows the client certificate configuration to be
	// overridden and taken from a different PKI than the server certificates.
	ClientTLS *TLSContext

	TLSPolicy *couchbasev2.ClientCertificatePolicy

	DNS *v1.Service

	S3Credentials *v1.Secret

	ObjEndpoint string

	ObjEndpointCertSecret *v1.Secret

	GenericNetworking bool

	LogStreaming *couchbasev2.CouchbaseClusterLoggingConfigurationSpec

	AuditConfiguration *couchbasev2.CouchbaseClusterAuditLoggingSpec

	MonitoringConfiguration *couchbasev2.CouchbaseClusterMonitoringSpec

	CloudNativeGateway *couchbasev2.CloudNativeGateway

	S3UseIAM bool

	Annotations map[string]string
}

// WithPodAnnotations defines a cluster with annotations set on all pods.
func (o *ClusterOptions) WithPodAnnotations(annotations map[string]string) *ClusterOptions {
	o.Annotations = annotations

	return o
}

func (o *ClusterOptions) WithCloudNativeGateway(image string) *ClusterOptions {
	o.CloudNativeGateway = &couchbasev2.CloudNativeGateway{
		Image: image,
		TLS:   nil,
	}

	return o
}

// WithEphemeralTopology defines a cluster as being ephemeral (no volumes).
// It has one server class with data/index/query enabled and of the specified
// size.
func (o *ClusterOptions) WithEphemeralTopology(size int) *ClusterOptions {
	topology := e2espec.EphemeralTopology.DeepCopy()
	topology[0].Size = size

	o.Options.Topology = topology

	return o
}

// WithMixedEphemeralTopology defines a cluster as having
// data/index in one server class and query in the other.
func (o *ClusterOptions) WithMixedEphemeralTopology(size int) *ClusterOptions {
	topology := e2espec.MixedEphemeralTopology.DeepCopy()
	topology[0].Size = size
	topology[1].Size = size

	o.Options.Topology = topology

	return o
}

// WithPersistentTopology defines a cluster as having persistent volumes.
// Is has one server class with data/index enabled and of the specified size.
func (o *ClusterOptions) WithPersistentTopology(size int) *ClusterOptions {
	topology := e2espec.PersistentTopology.DeepCopy()
	topology[0].Size = size

	o.Options.Topology = topology

	return o
}

// WithMixedTopology defines a cluster as having persistent volumes for
// data, where pertinent, and logs elsewhere.  It has two server classes --
// stateful and stateless -- with data/index and query/eventing enabled
// respectively.  Each server class is of the specified size.
func (o *ClusterOptions) WithMixedTopology(size int) *ClusterOptions {
	topology := e2espec.MixedTopology.DeepCopy()
	topology[0].Size = size
	topology[1].Size = size

	o.Options.Topology = topology

	return o
}

// WithSplitEphemeralTopology is intended to split the data, index and query
// services across separate server classes. Primarily to test data in isolation
// from index which none of the other topologies do.
func (o *ClusterOptions) WithSplitEphemeralTopology(size int) *ClusterOptions {
	topology := e2espec.SplitEphemeralTopology.DeepCopy()
	topology[0].Size = size
	topology[1].Size = size
	topology[2].Size = size

	o.Options.Topology = topology

	return o
}

func (o *ClusterOptions) WithDefaultLogStreaming() *ClusterOptions {
	o.LogStreaming = &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
		Enabled:           true,
		ConfigurationName: "fluent-bit-config",
		Sidecar:           &couchbasev2.LogShipperSidecarSpec{},
	}

	if imageName := strings.TrimSpace(o.Options.LoggingImage); imageName != "" {
		o.LogStreaming.Sidecar.Image = imageName
	}

	return o
}

func (o *ClusterOptions) WithCustomLogStreaming() *ClusterOptions {
	o.LogStreaming = &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
		Enabled:             true,
		ManageConfiguration: &[]bool{false}[0],
		ConfigurationName:   "custom-fluent-bit-config",
		Sidecar: &couchbasev2.LogShipperSidecarSpec{
			ConfigurationMountPath: "/test/custom",
		},
	}

	if imageName := strings.TrimSpace(o.Options.LoggingImage); imageName != "" {
		o.LogStreaming.Sidecar.Image = imageName
	}

	return o
}

// Create auditing configuration with short rotation size and cleanup interval for testing.
func (o *ClusterOptions) WithAuditing(enableCleanup bool) *ClusterOptions {
	o.AuditConfiguration = &couchbasev2.CouchbaseClusterAuditLoggingSpec{
		Enabled:        true,
		DisabledEvents: []int{},
		DisabledUsers:  []couchbasev2.AuditDisabledUser{},
		Rotation: &couchbasev2.CouchbaseClusterLogRotationSpec{
			Size: k8sutil.NewResourceQuantityMi(1),
		},
	}

	if enableCleanup {
		o.AuditConfiguration.GarbageCollection = &couchbasev2.CouchbaseClusterAuditGarbageCollectionSpec{
			Sidecar: &couchbasev2.CouchbaseClusterAuditCleanupSidecarSpec{
				Enabled:  true,
				Interval: k8sutil.NewDurationS(30),
			},
		}
	}

	return o
}

func (o *ClusterOptions) WithMonitoring() *ClusterOptions {
	o.MonitoringConfiguration = &couchbasev2.CouchbaseClusterMonitoringSpec{
		Prometheus: &couchbasev2.CouchbaseClusterMonitoringPrometheusSpec{
			Enabled: true,
		},
	}

	if imageName := strings.TrimSpace(o.Options.MonitoringImage); imageName != "" {
		o.MonitoringConfiguration.Prometheus.Image = imageName
	}

	return o
}

// WithTLS sets the cluster as having TLS configured.
func (o *ClusterOptions) WithTLS(tls *TLSContext) *ClusterOptions {
	o.TLS = tls

	return o
}

// WithMutualTLS sets the cluster as having mTLS configured.
func (o *ClusterOptions) WithMutualTLS(tls *TLSContext, policy *couchbasev2.ClientCertificatePolicy) *ClusterOptions {
	o.TLS = tls
	o.TLSPolicy = policy

	return o
}

// WithClientTLS overrides the client TLS configuration so it comes from a different
// PKI to that of the server.
func (o *ClusterOptions) WithClientTLS(clientTLS *TLSContext) *ClusterOptions {
	o.ClientTLS = clientTLS

	return o
}

// WithDNS sets the cluster as having a custom DNS server.
func (o *ClusterOptions) WithDNS(dns *v1.Service) *ClusterOptions {
	o.DNS = dns

	return o
}

// WithS3 set the cluster as using S3 for backups.
func (o *ClusterOptions) WithS3(s3 *v1.Secret) *ClusterOptions {
	o.S3Credentials = s3

	return o
}

// WithObjEndpoint set the cluster to use a custom obj endpoint for backups.
func (o *ClusterOptions) WithObjEndpoint(objEndpoint string) *ClusterOptions {
	o.ObjEndpoint = objEndpoint

	return o
}

func (o *ClusterOptions) WithObjEndpointCert(certSecret *v1.Secret) *ClusterOptions {
	o.ObjEndpointCertSecret = certSecret

	return o
}

// WithGenericNetworking enables Satan's insecure, unstable, shit show of a network mode.
func (o *ClusterOptions) WithGenericNetworking() *ClusterOptions {
	o.GenericNetworking = true

	return o
}

// WithDefaultStorageClass overrides the explicit storage class and uses the default.
func (o *ClusterOptions) WithDefaultStorageClass() *ClusterOptions {
	o.Options.StorageClass = ""

	return o
}

// WithAutoscaleStabilizationPeriod insulates cluster from scale requests while scaling.
func (o *ClusterOptions) WithAutoscaleStabilizationPeriod(seconds int) *ClusterOptions {
	o.Options.AutoscaleStabilizationPeriod = &metav1.Duration{Duration: time.Duration(seconds) * time.Second}

	return o
}

// Generate generates the basic cluster based on options and applies
// and features.
func (o *ClusterOptions) Generate(k8s *types.Cluster) *couchbasev2.CouchbaseCluster {
	cluster := e2espec.NewBasicCluster(o.Options)

	applyTLS(cluster, o.TLS, o.ClientTLS, o.TLSPolicy)
	applyMTLS(cluster, o.TLSPolicy)
	applyDNS(k8s, cluster, o.DNS)
	applyS3(cluster, o.S3Credentials)
	applyObjEndpoint(cluster, o.ObjEndpoint, o.ObjEndpointCertSecret)
	applyS3IAMRole(cluster, o.S3UseIAM)
	applyGenericNetworking(cluster, o.GenericNetworking)
	applyLogStreaming(cluster, o.LogStreaming)
	applyAuditing(cluster, o.AuditConfiguration)
	applyMonitoring(cluster, o.MonitoringConfiguration)
	applyAnnotations(cluster, o.Annotations)
	applyCloudNativeGateway(cluster, o.CloudNativeGateway)

	return cluster
}

// MustCreate calls Generate then creates the cluster in Kubernetes, dying
// on error.
func (o *ClusterOptions) MustCreate(t *testing.T, k8s *types.Cluster) *couchbasev2.CouchbaseCluster {
	return MustNewClusterFromSpec(t, k8s, o.Generate(k8s))
}

// MustNotCreate calls Generate then creates the cluster in Kubernetes, dying
// on success.
func (o *ClusterOptions) MustNotCreate(t *testing.T, k8s *types.Cluster) {
	if _, err := CreateCluster(k8s, o.Generate(k8s)); err == nil {
		Die(t, fmt.Errorf("cluster created unexpectedly"))
	}
}

// NewBucketOld creates a bucket.
func NewBucketOld(k8s *types.Cluster, bucket metav1.Object) (metav1.Object, error) {
	switch t := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseBuckets(k8s.Namespace).Create(context.Background(), t, metav1.CreateOptions{})
	case *couchbasev2.CouchbaseEphemeralBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(k8s.Namespace).Create(context.Background(), t, metav1.CreateOptions{})
	case *couchbasev2.CouchbaseMemcachedBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(k8s.Namespace).Create(context.Background(), t, metav1.CreateOptions{})
	default:
		return nil, fmt.Errorf("unsupported bucket type")
	}
}

func MustNewBucket(t *testing.T, k8s *types.Cluster, bucket metav1.Object) metav1.Object {
	object, err := NewBucketOld(k8s, bucket)
	if err != nil {
		Die(t, err)
	}

	return object
}

func generateBucket(bucketType BucketType, compressionMode couchbasev2.CouchbaseBucketCompressionMode, durability couchbasev2.CouchbaseBucketMinimumDurability) metav1.Object {
	switch bucketType {
	case "couchbase":
		bucket := e2espec.DefaultBucket()
		bucket.Spec.CompressionMode = compressionMode

		if durability != "" {
			bucket.Spec.MinimumDurability = durability
		}

		return bucket
	case "ephemeral":
		bucket := e2espec.DefaultEphemeralBucket()
		bucket.Spec.CompressionMode = compressionMode

		if durability != "" {
			bucket.Spec.MinimumDurability = couchbasev2.CouchbaseEphemeralBucketMinimumDurability(durability)
		}

		return bucket
	case "memcached":
		return e2espec.DefaultMemcachedBucket()
	default:
		return e2espec.DefaultBucket()
	}
}

func GetBucket(bucketType BucketType, compressionMode couchbasev2.CouchbaseBucketCompressionMode) metav1.Object {
	return generateBucket(bucketType, compressionMode, "")
}

func GetDurableBucket(bucketType BucketType, compressionMode couchbasev2.CouchbaseBucketCompressionMode, durability couchbasev2.CouchbaseBucketMinimumDurability) metav1.Object {
	return generateBucket(bucketType, compressionMode, durability)
}

func MustGetBucket(bucketType BucketType, compressionMode couchbasev2.CouchbaseBucketCompressionMode) metav1.Object {
	bucket := GetBucket(bucketType, compressionMode)

	return bucket
}

func DeleteBackup(k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseBackups(backup.Namespace).Delete(context.Background(), backup.Name, *metav1.NewDeleteOptions(0))
}

func MustDeleteBackup(t *testing.T, k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup) {
	if err := DeleteBackup(k8s, backup); err != nil {
		Die(t, err)
	}
}

func DeleteBackupRestore(k8s *types.Cluster, restore *couchbasev2.CouchbaseBackupRestore) error {
	return k8s.CRClient.CouchbaseV2().CouchbaseBackupRestores(restore.Namespace).Delete(context.Background(), restore.Name, *metav1.NewDeleteOptions(0))
}

func MustDeleteBackupRestore(t *testing.T, k8s *types.Cluster, restore *couchbasev2.CouchbaseBackupRestore) {
	if err := DeleteBackupRestore(k8s, restore); err != nil {
		Die(t, err)
	}
}

func DeleteBucket(k8s *types.Cluster, bucket metav1.Object) error {
	switch t := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseBuckets(k8s.Namespace).Delete(context.Background(), t.Name, *metav1.NewDeleteOptions(0))
	case *couchbasev2.CouchbaseEphemeralBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(k8s.Namespace).Delete(context.Background(), t.Name, *metav1.NewDeleteOptions(0))
	case *couchbasev2.CouchbaseMemcachedBucket:
		return k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(k8s.Namespace).Delete(context.Background(), t.Name, *metav1.NewDeleteOptions(0))
	default:
		return fmt.Errorf("unsupported bucket type")
	}
}

func MustDeleteBucket(t *testing.T, k8s *types.Cluster, bucket metav1.Object) {
	if err := DeleteBucket(k8s, bucket); err != nil {
		Die(t, err)
	}
}

func AddServices(k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, newService couchbasev2.ServerConfig, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	settings := append(cl.Spec.Servers, newService)
	return patchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace("/spec/servers", settings), timeout)
}

func MustAddServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, newService couchbasev2.ServerConfig, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	couchbase, err := AddServices(k8s, cl, newService, timeout)
	if err != nil {
		Die(t, err)
	}

	return couchbase
}

func RemoveServices(k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, removeServiceName string, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	newServiceConfig := []couchbasev2.ServerConfig{}

	for _, service := range cl.Spec.Servers {
		if service.Name != removeServiceName {
			newServiceConfig = append(newServiceConfig, service)
		}
	}

	return patchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace("/spec/servers", newServiceConfig), timeout)
}

func MustRemoveServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, removeServiceName string, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	couchbase, err := RemoveServices(k8s, cl, removeServiceName, timeout)
	if err != nil {
		Die(t, err)
	}

	return couchbase
}

func ScaleServices(k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, servicesMap map[string]int, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	newServiceConfig := []couchbasev2.ServerConfig{}

	for _, service := range cl.Spec.Servers {
		for serviceName, size := range servicesMap {
			if serviceName == service.Name {
				service.Size = size
			}
		}

		newServiceConfig = append(newServiceConfig, service)
	}

	return patchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace("/spec/servers", newServiceConfig), timeout)
}

func MustScaleServices(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, servicesMap map[string]int, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	couchbase, err := ScaleServices(k8s, cl, servicesMap, timeout)
	if err != nil {
		Die(t, err)
	}

	return couchbase
}

// patchResource applies a JSON patch to any resource type.
func patchResource(k8s *types.Cluster, resource runtime.Object, patches jsonpatch.PatchSet, timeout time.Duration) (runtime.Object, error) {
	// Map from object to dynamic API mapping... "e.g. couchbase.com/v2/couchbaseclusters"
	kinds, _, err := scheme.Scheme.ObjectKinds(resource)
	if err != nil {
		return nil, err
	}

	gvk := kinds[0]

	mapping, err := k8s.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}

	// Up cast the resource into a meta object so we can interrogate name and namespace.
	metaResource, ok := resource.(metav1.Object)
	if !ok {
		return nil, fmt.Errorf("unable to convert from runtime to meta resource")
	}

	// Convert our JSON patch into a generic JSON one.
	patchSet, err := json.Marshal(patches.Patches())
	if err != nil {
		return nil, err
	}

	patch, err := other_jsonpatch.DecodePatch(patchSet)

	if err != nil {
		return nil, err
	}

	callback := func() error {
		// Load up the most recent revision of the requested resource so we don't get
		// CAS errors from etcd.
		current, err := k8s.DynamicClient.Resource(mapping.Resource).Namespace(metaResource.GetNamespace()).Get(context.Background(), metaResource.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Convert to JSON and apply the patch.
		document, err := json.Marshal(current)
		if err != nil {
			return err
		}

		patchedDocument, err := patch.Apply(document)
		if err != nil {
			return err
		}

		// If everything applied correctly, and anything changed, then update the
		// resource.
		if bytes.Equal(document, patchedDocument) {
			return nil
		}

		updated := &unstructured.Unstructured{}
		if err := json.Unmarshal(patchedDocument, updated); err != nil {
			return err
		}

		if updated, err = k8s.DynamicClient.Resource(mapping.Resource).Namespace(metaResource.GetNamespace()).Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
			return err
		}

		// All good, update the return type
		typedUpdated, err := scheme.Scheme.New(gvk)
		if err != nil {
			return err
		}

		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(updated.Object, typedUpdated); err != nil {
			return err
		}

		resource = typedUpdated

		return nil
	}

	// Retry the patching to handle CAS errors, transient errors and waiting for
	// resource values to change asynchronously.
	if err := retryutil.RetryFor(timeout, callback); err != nil {
		return nil, err
	}

	return resource, nil
}

func patchCluster(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	resource, err := patchResource(k8s, cluster, patches, timeout)
	if err != nil {
		return nil, err
	}

	return resource.(*couchbasev2.CouchbaseCluster), nil
}

func MustPatchCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	cluster, err := patchCluster(k8s, cluster, patches, timeout)

	if err != nil {
		Die(t, err)
	}

	return cluster
}

// MustNotPatchCluster patches the cluster with a list of JSON patch objects, dying if the test succeeded.
func MustNotPatchCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, patches jsonpatch.PatchSet) {
	if _, err := patchCluster(k8s, cluster, patches, 30*time.Second); err == nil {
		Die(t, fmt.Errorf("cluster patch applied unexpectedly"))
	}
}

func MustPatchBackup(t *testing.T, k8s *types.Cluster, backup *couchbasev2.CouchbaseBackup, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseBackup {
	resource, err := patchResource(k8s, backup, patches, timeout)
	if err != nil {
		Die(t, err)
	}

	return resource.(*couchbasev2.CouchbaseBackup)
}

// MustPatchBucket patches the bucket with a list of JSON patch objects, dying if the patch fails.
func MustPatchBucket(t *testing.T, k8s *types.Cluster, bucket metav1.Object, patches jsonpatch.PatchSet, timeout time.Duration) metav1.Object {
	resource, err := patchResource(k8s, bucket.(runtime.Object), patches, timeout)
	if err != nil {
		Die(t, err)
	}

	return resource.(metav1.Object)
}

// MustNotPatchBucket patches the bucket with a list of JSON patch objects, dying if the patch succeeds.
func MustNotPatchBucket(t *testing.T, k8s *types.Cluster, bucket metav1.Object, patches jsonpatch.PatchSet) {
	if _, err := patchResource(k8s, bucket.(runtime.Object), patches, 30*time.Second); err == nil {
		Die(t, fmt.Errorf("bucket patch applied unexpectedly"))
	}
}

func MustPatchReplication(t *testing.T, k8s *types.Cluster, replication *couchbasev2.CouchbaseReplication, patches jsonpatch.PatchSet, timeout time.Duration) *couchbasev2.CouchbaseReplication {
	resource, err := patchResource(k8s, replication, patches, timeout)
	if err != nil {
		Die(t, err)
	}

	return resource.(*couchbasev2.CouchbaseReplication)
}

func KillMembers(kubecli kubernetes.Interface, cluster *couchbasev2.CouchbaseCluster, names ...string) error {
	for _, name := range names {
		if err := KillMember(kubecli, cluster, name, true); err != nil {
			return err
		}
	}

	return nil
}

// Kill member deletes Pod and optionally checks for any associated Volume to delete.
func KillMember(kubecli kubernetes.Interface, cluster *couchbasev2.CouchbaseCluster, name string, removeVolumes bool) error {
	if err := kubecli.CoreV1().Pods(cluster.Namespace).Delete(context.Background(), name, *metav1.NewDeleteOptions(0)); err != nil {
		return err
	}

	if removeVolumes {
		if err := kubecli.CoreV1().PersistentVolumeClaims(cluster.Namespace).DeleteCollection(context.Background(), *metav1.NewDeleteOptions(0), NodeListOpt(cluster, name)); err != nil {
			return err
		}
	}

	return nil
}

func WriteLogs(k8s *types.Cluster, logDir string) error {
	if err := os.MkdirAll(logDir, os.ModePerm); err != nil {
		return err
	}

	options := metav1.ListOptions{LabelSelector: constants.CouchbaseOperatorLabel}

	pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), options)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			logOptions := &v1.PodLogOptions{
				Container: container.Name,
			}
			req := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).GetLogs(pod.Name, logOptions)

			data, err := req.DoRaw(context.Background())
			if err != nil {
				return err
			}

			logFile := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", container.Name, pod.Name))

			if err := os.WriteFile(logFile, data, 0o644); err != nil {
				return err
			}
		}
	}

	return nil
}

func ResizeClusterNoWait(service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster) (*couchbasev2.CouchbaseCluster, error) {
	return patchCluster(k8s, cl, jsonpatch.NewPatchSet().Replace(fmt.Sprintf("/spec/servers/%d/size", service), clusterSize), 30*time.Second)
}

func MustResizeClusterNoWait(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster) *couchbasev2.CouchbaseCluster {
	cluster, err := ResizeClusterNoWait(service, clusterSize, k8s, cl)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

// ResizeCluster resizes the MDS service to the desired size and waits until the cluster is
// healthy.
func ResizeCluster(service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) (*couchbasev2.CouchbaseCluster, error) {
	cluster, err := ResizeClusterNoWait(service, clusterSize, k8s, cl)
	if err != nil {
		return cl, err
	}

	if err := WaitClusterStatusHealthy(k8s, cluster, timeout); err != nil {
		return cluster, err
	}

	return cluster, nil
}

func MustResizeCluster(t *testing.T, service int, clusterSize int, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, timeout time.Duration) *couchbasev2.CouchbaseCluster {
	cluster, err := ResizeCluster(service, clusterSize, k8s, cl, timeout)
	if err != nil {
		Die(t, err)
	}

	return cluster
}

func KillPods(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, numToKill int) {
	pods, err := k8s.KubeClient.CoreV1().Pods(cl.Namespace).List(context.Background(), ClusterListOpt(cl))
	if err != nil {
		Die(t, err)
	}

	items := len(pods.Items)
	if numToKill > items {
		Die(t, fmt.Errorf("trying to kill %d pods, but only %d exist", numToKill, items))
	}

	killPods := []*v1.Pod{}
	killPodNames := []string{}

	for i := 0; i < numToKill; i++ {
		killPods = append(killPods, &pods.Items[i])
		killPodNames = append(killPodNames, pods.Items[i].Name)
	}

	if err := KillMembers(k8s.KubeClient, cl, killPodNames...); err != nil {
		Die(t, err)
	}

	for _, pod := range killPods {
		if err := retryutil.RetryFor(time.Minute, ResourceDeleted(k8s, pod)); err != nil {
			Die(t, err)
		}
	}
}

func KillPodForMember(kubeCli kubernetes.Interface, cl *couchbasev2.CouchbaseCluster, memberID int) error {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return KillMember(kubeCli, cl, name, true)
}

func MustKillPodForMember(t *testing.T, k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, memberID int, removeVolumes bool) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	if err := KillMember(k8s.KubeClient, cl, name, removeVolumes); err != nil {
		Die(t, err)
	}
}

func CreateMemberPod(k8s *types.Cluster, cl *couchbasev2.CouchbaseCluster, m couchbaseutil.Member) (*v1.Pod, error) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: m.Name(),
			// At a minimum, any naughty user needs to have the following labels
			// to establish DNS and allow node-to-node connectivity.
			Labels: k8sutil.SelectorForClusterResource(cl),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				k8sutil.CouchbaseContainer(cl.Spec.Image),
			},
			Hostname:  m.Name(),
			Subdomain: cl.Name,
		},
	}

	for _, config := range cl.Spec.Servers {
		if config.Name == m.Config() {
			p, err := k8s.KubeClient.CoreV1().Pods(cl.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			if err != nil {
				return nil, err
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			err = k8sutil.WaitForPod(ctx, k8s.KubeClient, cl.Namespace, pod.Name, "")
			if err != nil {
				return nil, err
			}

			return p, nil
		}
	}

	return nil, NewErrServerConfigNotFound(m.Config())
}

func deleteCouchbaseOperator(k8s *types.Cluster) error {
	name, err := GetOperatorName(k8s)
	if err != nil {
		return err
	}

	return k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Delete(context.Background(), name, *metav1.NewDeleteOptions(0))
}

func MustDeleteCouchbaseOperator(t *testing.T, k8s *types.Cluster) {
	if err := deleteCouchbaseOperator(k8s); err != nil {
		Die(t, err)
	}
}

func KillOperatorAndWaitForRecovery(k8s *types.Cluster) error {
	if err := deleteCouchbaseOperator(k8s); err != nil {
		return fmt.Errorf("failed to kill couchbase operator: %w", err)
	}

	if err := WaitUntilOperatorReady(k8s, 5*time.Minute); err != nil {
		return fmt.Errorf("failed to recover couchbase operator: %w", err)
	}

	return nil
}

func MustKillOperatorAndWaitForRecovery(t *testing.T, k8s *types.Cluster) {
	if err := KillOperatorAndWaitForRecovery(k8s); err != nil {
		Die(t, err)
	}
}

// MustDeleteOperatorDeployment shuts down the operator and waits for it to be garbage collected
// once all the dependant pods are cleaned up.  This allows us to explicitly make alterations
// while the operator is not running and see what happens on a restart without introducing race
// conditions.
func MustDeleteOperatorDeployment(t *testing.T, k8s *types.Cluster, timeout time.Duration) {
	if err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Delete(context.Background(), k8s.OperatorDeployment.Name, *metav1.NewDeleteOptions(0)); err != nil {
		Die(t, err)
	}

	callback := func() error {
		_, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Get(context.Background(), k8s.OperatorDeployment.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}

			return err
		}

		return fmt.Errorf("deployment still exists")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}

	// Be very sure the operator is dead
	selector, err := metav1.LabelSelectorAsSelector(k8s.OperatorDeployment.Spec.Selector)
	if err != nil {
		Die(t, err)
	}

	callback = func() error {
		pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return err
		}

		if len(pods.Items) > 0 {
			return fmt.Errorf("pods still exist")
		}

		return nil
	}

	if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}
}

// MustCreateOperatorDeployment is the partner of MustDeleteOperatorDeployment which is used to
// restart the operator synchronously, potentially after modifying resources.
func MustCreateOperatorDeployment(t *testing.T, k8s *types.Cluster) {
	if _, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(context.Background(), k8s.OperatorDeployment, metav1.CreateOptions{}); err != nil {
		Die(t, err)
	}
}

func MustCreateOperatorDeploymentWithEnvVars(t *testing.T, k8s *types.Cluster, envVars []v1.EnvVar) {
	operatorContainer := &k8s.OperatorDeployment.Spec.Template.Spec.Containers[0]
	operatorContainer.Env = append(operatorContainer.Env, envVars...)

	if _, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(context.Background(), k8s.OperatorDeployment, metav1.CreateOptions{}); err != nil {
		Die(t, err)
	}
}

func GetOperatorName(k8s *types.Cluster) (string, error) {
	var pods *v1.PodList

	selector := labels.SelectorFromSet(labels.Set(NameLabelSelector("app", "couchbase-operator")))

	outerErr := retryutil.RetryFor(time.Minute, func() error {
		var err error

		pods, err = k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return err
		}

		return nil
	})
	if outerErr != nil {
		return "couchbase-operator", outerErr
	}

	operatorPods := []string{}

	for _, pod := range pods.Items {
		operatorPods = append(operatorPods, pod.Name)
	}

	if len(operatorPods) == 0 {
		return "", fmt.Errorf("no pods available")
	}

	if len(operatorPods) > 1 {
		return "couchbase-operator", fmt.Errorf("too many couchbase operators")
	}

	return operatorPods[0], nil
}

// getSchedulableNodes returns a list of all nodes that can be scheduled onto.
func getSchedulableNodes(k8s *types.Cluster) ([]*v1.Node, error) {
	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := []*v1.Node{}

	for index := range nodes.Items {
		schedulable := true

		for _, taint := range nodes.Items[index].Spec.Taints {
			if taint.Effect == v1.TaintEffectNoSchedule {
				schedulable = false
				break
			}
		}

		if schedulable {
			result = append(result, &nodes.Items[index])
		}
	}

	return result, nil
}

// MustNumNodesAbsolute returns the number of nodes in the cluster
// irrespective of scheduling constraints.
func MustNumNodesAbsolute(t *testing.T, k8s *types.Cluster) int {
	nodes, err := k8s.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	return len(nodes.Items)
}

// MustNodes returns the nodes in the cluster.
func MustNodes(t *testing.T, k8s *types.Cluster) []*v1.Node {
	nodes, err := getSchedulableNodes(k8s)
	if err != nil {
		Die(t, err)
	}

	return nodes
}

// MustNumNodes returns the number of nodes in the cluster.
func MustNumNodes(t *testing.T, k8s *types.Cluster) int {
	nodes, err := getSchedulableNodes(k8s)
	if err != nil {
		Die(t, err)
	}

	return len(nodes)
}

// memoryRequirementToFloat takes a memory requirement, scales it into MiB and
// then casts to a floating point.
func memoryRequirementToFloat(quantity resource.Quantity) float64 {
	return float64(quantity.Value() >> 20)
}

// getNodeAllocatableMemory creates a map from node name to available memory in MiB.
// This uses the per node allocatable total and deducts any pod limits or requests
// to determine what is left.
func getNodeAllocatableMemory(t *testing.T, k8s *types.Cluster) map[string]float64 {
	nodes, err := getSchedulableNodes(k8s)
	if err != nil {
		Die(t, err)
	}

	pods, err := k8s.KubeClient.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		Die(t, err)
	}

	result := map[string]float64{}

	for _, node := range nodes {
		// Begin with the allocatable amount of memory on the node.  This is fixed and
		// doesn't take in to account the running pods.
		allocatable := memoryRequirementToFloat(node.Status.Allocatable[v1.ResourceMemory])
		result[node.Name] = allocatable
	}

	// Next deduct any requests.
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if quantity, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
				result[pod.Spec.NodeName] -= memoryRequirementToFloat(quantity)
				continue
			}
		}
	}

	return result
}

// MustGetMinNodeMem returns the smallest amount of allocatable memory available on any node.
func MustGetMinNodeMem(t *testing.T, k8s *types.Cluster) float64 {
	allocatable := getNodeAllocatableMemory(t, k8s)

	result := math.Inf(+1)

	for _, value := range allocatable {
		result = math.Min(result, value)
	}

	if result == math.Inf(+1) {
		Die(t, fmt.Errorf("no minimum found"))
	}

	return result
}

// MustGetMaxNodeMem returns the largest amount of allocatable memory available on any node.
func MustGetMaxNodeMem(t *testing.T, k8s *types.Cluster) float64 {
	allocatable := getNodeAllocatableMemory(t, k8s)

	result := 0.0

	for _, value := range allocatable {
		result = math.Max(result, value)
	}

	if result == 0.0 {
		Die(t, fmt.Errorf("no maximum found"))
	}

	return result
}

// MustGetMaxScale accepts a memory figure and returns the number of pods that can be deployed
// across the cluster with that sized memory requirement.
func MustGetMaxScale(t *testing.T, k8s *types.Cluster, memory float64) int {
	allocatable := getNodeAllocatableMemory(t, k8s)

	result := 0

	for _, value := range allocatable {
		result += int(math.Floor(value / memory))
	}

	return result
}

func TLSCheckForCluster(k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, tls *TLSContext, timeout time.Duration) error {
	pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: constants.CouchbaseServerClusterKey + "=" + tls.ClusterName})
	if err != nil {
		return fmt.Errorf("unable to get couchbase pods: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// TLS handshake with pods
	for i := range pods.Items {
		pod := pods.Items[i]

		callback := func() error {
			if err := tlsCheckForPod(k8s, cluster, pod.GetName(), tls); err != nil {
				return fmt.Errorf("TLS verification failed: %w", err)
			}

			return nil
		}

		if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
			return err
		}
	}

	return nil
}

func MustCheckClusterTLS(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, ctx *TLSContext, timeout time.Duration) {
	if err := TLSCheckForCluster(k8s, cluster, ctx, timeout); err != nil {
		Die(t, err)
	}
}

func deletePod(k8s *types.Cluster, podName string) error {
	err := retryutil.RetryFor(time.Minute, func() error {
		return k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Delete(context.Background(), podName, *metav1.NewDeleteOptions(0))
	})

	return err
}

func Die(t *testing.T, err error) {
	stackTrace := string(debug.Stack())

	analyzer.RecordFailureMessage(t, err.Error(), stackTrace)

	t.Log(err)
	t.Log(stackTrace)
	t.FailNow()
}

// MustKillCouchbaseService kills the couchbase service depending on the platform type
// TODO: Find a generic way of doing this on OpenShift.
func MustKillCouchbaseService(t *testing.T, k8s *types.Cluster, member, kubernetesType string) {
	if kubernetesType == "kubernetes" {
		MustExecShellInPod(t, k8s, member, "rm -rf /opt/couchbase/*;killall5 -9")
		return
	}

	if err := deletePod(k8s, member); err != nil {
		Die(t, err)
	}
}

// MustDeletePodServices deletes all services in the cluster namespace that
// belong to individual pods.
func MustDeletePodServices(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	selector := constants.CouchbaseServerPodLabelStr + couchbase.Name

	services, err := k8s.KubeClient.CoreV1().Services(couchbase.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		Die(t, err)
	}

	for _, service := range services.Items {
		if _, ok := service.Spec.Selector[operator_constants.LabelNode]; ok {
			if err := DeleteService(k8s, service.Name, metav1.NewDeleteOptions(0)); err != nil {
				Die(t, err)
			}
		}
	}
}

// GenerateWorkload creates workload on a cluster with the cbc-pillowfight utility.  It
// inserts JSON documents continuously until deleted by the returned cleanup function.
func GenerateWorkload(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, image, bucket string) (func(), error) {
	podName := couchbase.Name + "-workloadgen"
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "couchbase-server",
					Image: image,
					Command: []string{
						"/opt/couchbase/bin/cbc-pillowfight",
					},
					Args: []string{
						"-U",
						fmt.Sprintf("couchbase://%s-srv.%s.svc/%s", couchbase.Name, couchbase.Namespace, bucket),
						"-u",
						string(k8s.DefaultSecret.Data["username"]),
						"-P",
						string(k8s.DefaultSecret.Data["password"]),
						// No pre-population, dive straight in.
						"-n",
						// JSON documents.
						"-J",
						// A small batch size does rudimentary rate limiting as minikube is likely
						// to get pwned.
						"-B", "1",
						// A large (ish) number of items so we generate lots of writes and flushes,
						// even a little compaction, but not too much or rebalances will take forever.
						"-I", "100000",
						// Run continuously.
						"-c", "-1",
					},
				},
			},
		},
	}

	if _, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		return nil, err
	}

	cleanup := func() {
		_ = k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).Delete(context.Background(), podName, *metav1.NewDeleteOptions(0))
	}

	return cleanup, nil
}

func MustGenerateWorkload(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, image, bucket string) func() {
	cleanup, err := GenerateWorkload(k8s, couchbase, image, bucket)
	if err != nil {
		Die(t, err)
	}

	return cleanup
}

// MustPopulateWithDataSize fills Couchbase with an approximate number of docs that
// fulfill the requested size.  I cannot for the life of me fathom how this pillowfight
// junk is meant to work, and cannot seem to make it generate what I want!  But it's
// a good enough approximation.
func MustPopulateWithDataSize(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, bucket, image string, size int, timeout time.Duration) {
	documentSize := 1 << 20 // 1 megabyte
	documents := size / documentSize

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "populator",
			Namespace: k8s.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyNever,
					Containers: []v1.Container{
						{
							Name:  "pillowfight",
							Image: image,
							Command: []string{
								"/opt/couchbase/bin/cbc-pillowfight",
							},
							Args: []string{
								"-U",
								fmt.Sprintf("couchbase://%s/%s", couchbase.Name, bucket),
								"-u",
								string(k8s.DefaultSecret.Data["username"]),
								"-P",
								string(k8s.DefaultSecret.Data["password"]),
								"--sequential",
								"-r",
								"100",
								"-c",
								"1",
								"-B",
								strconv.Itoa(documents),
								"-m",
								strconv.Itoa(documentSize),
								"-M",
								strconv.Itoa(documentSize),
								"-J",
								"-n",
							},
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if _, err := k8s.KubeClient.BatchV1().Jobs(k8s.Namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		Die(t, err)
	}

	if err := retryutil.Retry(ctx, time.Second, ResourceConstraints(k8s, job, resourceExists, resourceConditionExists("Complete", "True"), jobSucceeded(1))); err != nil {
		Die(t, err)
	}
}

// GetUUID returns the UUID of the cluster.
func GetUUID(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) (string, error) {
	uuid := ""

	callback := func() error {
		c, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(couchbase.Namespace).Get(context.Background(), couchbase.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if c.Status.ClusterID == "" {
			return fmt.Errorf("cluster ID is not set")
		}

		uuid = c.Status.ClusterID

		return nil
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		return "", err
	}

	return uuid, nil
}

func MustGetUUID(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, timeout time.Duration) string {
	uuid, err := GetUUID(k8s, couchbase, timeout)
	if err != nil {
		Die(t, err)
	}

	return uuid
}

// MustTerminateAllPods kills pods by causing the root process to shutdown.  This results in
// the same state as if cluster was powered off and back on again.
func MustTerminateAllPods(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster) {
	selector := labels.SelectorFromSet(labels.Set(k8sutil.LabelsForCluster(cluster)))

	pods, err := kubernetes.KubeClient.CoreV1().Pods(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		Die(t, err)
	}

	for _, pod := range pods.Items {
		// Pew pew pew!
		// "If runsvdir receives a TERM signal, it exits with 0 immediately."
		if _, _, err := ExecShellInPod(kubernetes, pod.Name, "kill -TERM 1"); err != nil {
			t.Logf("command may have failed, but that may be because the pod died: %v", err)
		}
	}
}

// ArgumentList represents parameters to cbopinfo.  They are modelled as a
// map to support keys and values (an empty value is ignored) and to allow
// simple overriding (uniqueness).
type ArgumentList map[string][]string

// Slice returns the flattened ArgumentList with empty values removed.
func (a ArgumentList) Slice() []string {
	args := []string{}

	for k, v := range a {
		for _, value := range v {
			args = append(args, k)

			if value != "" {
				args = append(args, value)
			}
		}
	}

	return args
}

// Add adds a new key and value to the argument list.
func (a ArgumentList) Add(k, v string) {
	a[k] = append(a[k], v)
}

// AddClusterDefaults adds in configuration specific default arguments that must
// be used for a successful run.
func (a ArgumentList) AddClusterDefaults(k8s *types.Cluster) {
	// Hmmm... good point, I wonder if cli tools support in-cluster, let's find out!
	if k8s.KubeConfPath != "" {
		a.Add("--kubeconfig", k8s.KubeConfPath)
	}

	a.Add("--namespace", k8s.Namespace)

	if k8s.Context != "" {
		a.Add("--context", k8s.Context)
	}
}

// AddEnvironmentDefaults adds in configuration specific default arguments for deployments
// that should be used for a successful run.
func (a ArgumentList) AddEnvironmentDefaults(operatorImage string) {
	a.Add("--operator-image", operatorImage)
}

// Clone duplicates an argument list.
func (a ArgumentList) Clone() ArgumentList {
	n := ArgumentList{}

	for k, v := range a {
		n[k] = make([]string, len(v))
		copy(n[k], v)
	}

	return n
}

// Generic function to run cbopinfo command.
func Cbopinfo(path string, cmdArgs []string) ([]byte, error) {
	args := []string{"collect-logs"}
	args = append(args, cmdArgs...)

	return exec.Command(path, args...).CombinedOutput()
}

func CollectLogs(t *testing.T, cluster *types.Cluster, logDir string, cbopinfoPath, operatorImage string, collectServerLogs bool, logLevel int) {
	// Create and move to the log directory.
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Logf("Failed to create dir %s: %v", logDir, err)
		return
	}

	// Collect logs from all known resources (e.g. sgw and other external services
	// will get collected).  Don't collect server logs by default, this takes an
	// abosulte eternity.
	args := ArgumentList{}
	args.AddClusterDefaults(cluster)
	args.AddEnvironmentDefaults(operatorImage)
	args.Add("--all", "")
	args.Add("--directory", logDir)
	args.Add("--log-level", strconv.Itoa(logLevel))

	if collectServerLogs {
		args.Add("--collectinfo", "")
		args.Add("--collectinfo-collect", "all")
	}

	execOut, err := Cbopinfo(cbopinfoPath, args.Slice())
	execOutStr := strings.TrimSpace(string(execOut))

	if err != nil {
		t.Logf("cbopinfo returned: %s", execOutStr)
		t.Logf("cbopinfo command failed: %v", err)
	}
}

func MustSetBucketTTL(t *testing.T, bucket metav1.Object, duration time.Duration) {
	d := metav1.Duration{
		Duration: duration,
	}

	switch b := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		b.Spec.MaxTTL = &d
	case *couchbasev2.CouchbaseEphemeralBucket:
		b.Spec.MaxTTL = &d
	default:
		Die(t, fmt.Errorf("bucket of incorrect type"))
	}
}

func MustRetrieveCouchbaseBucketByLabel(t *testing.T, kubernetes *types.Cluster, labelSelector string) *couchbasev2.CouchbaseBucket {
	buckets, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBuckets(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		Die(t, fmt.Errorf("cannot get bucket from kubernetes"))
	}

	if len(buckets.Items) != 1 {
		Die(t, fmt.Errorf("unexpected number of buckets returned"))
	}

	return &buckets.Items[0]
}

func MustRetrieveEphemeralBucketByLabel(t *testing.T, kubernetes *types.Cluster, labelSelector string) *couchbasev2.CouchbaseEphemeralBucket {
	buckets, err := kubernetes.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		Die(t, fmt.Errorf("cannot get bucket from kubernetes"))
	}

	if len(buckets.Items) != 1 {
		Die(t, fmt.Errorf("unexpected number of buckets returned"))
	}

	return &buckets.Items[0]
}

func MustRetrieveMemcachedBucketByLabel(t *testing.T, kubernetes *types.Cluster, labelSelector string) *couchbasev2.CouchbaseMemcachedBucket {
	buckets, err := kubernetes.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(kubernetes.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		Die(t, fmt.Errorf("cannot get bucket from kubernetes"))
	}

	if len(buckets.Items) != 1 {
		Die(t, fmt.Errorf("unexpected number of buckets returned"))
	}

	return &buckets.Items[0]
}

func GetPvcName(lpv bool) string {
	if lpv {
		return "local"
	}

	return "couchbase"
}
