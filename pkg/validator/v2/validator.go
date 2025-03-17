package v2

import (
	"crypto/x509"
	goerrors "errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	cbcluster "github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/tlsutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"
	"github.com/go-openapi/errors"
	"github.com/robfig/cron/v3"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	bucketTTLMax       = (1 << 31) - 1 // Puny 32 bit signed integers
	pruneAgeMaxSeconds = 35791394
)

// CheckConstraints does domain specific validation for a Couchbase cluster.
// NOTE: philosophically, we shouldn't need this many, and my gut feeling is that
// through clever API design we can cull some of them.  Obviously this is a
// CRDv3 thing.
func CheckConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if checkAnnotationSkipValidation(cluster.Annotations) {
		return nil, nil
	}

	checks := []func(*types.Validator, *couchbasev2.CouchbaseCluster) error{
		checkConstraintClusterName,
		checkConstraintServerImagesSet,
		checkConstraintDataServiceMemoryQuota,
		checkConstraintDataServiceMemcachedThreadCounts,
		checkConstraintIndexServiceMemoryQuota,
		checkConstraintSearchServiceMemoryQuota,
		checkConstraintEventingServiceMemoryQuota,
		checkConstraintAnalyticsServiceMemoryQuota,
		checkConstraintPerServiceClassPDB,
		checkConstraintQueryTemporarySpace,
		checkConstraintAutoFailoverTimeout,
		checkConstraintAutoFailoverMaxCount,
		checkConstraintAutoFailoverOnDataDiskIssuesTimePeriod,
		checkConstraintIndexerMemorySnapshotInterval,
		checkConstraintIndexerStableSnapshotInterval,
		checkConstraintAdminSecret,
		checkConstraintPrometheusAuthorizationSecret,
		checkConstraintLoggingPermissible,
		checkConstraintAuditLoggingPermissible,
		checkConstraintXDCRRemoteAuthentication,
		checkConstraintXDCRReplicationBuckets,
		checkConstraintXDCRReplicationScopesAndCollectionsSupported,
		checkConstraintXDCRReplicationRules,
		checkConstraintServerClassContainsDataService,
		checkoutConstraintNoServicelessClassBelow76,
		checkConstraintMoreThanTwoDataNodesMultiNodeClusterInPlaceUpgrade,
		checkConstraintClusterSupportable,
		checkConstraintServiceEnabledForVolumeMount,
		checkConstraintDefaultAndLogVolumesMututallyExclusive,
		checkConstraintServiceMountsUsedWithDefaultMount,
		checkConstraintTemplateExistsForMount,
		checkConstraintVolumeTemplateNameUnique,
		checkConstraintVolumeTemplateSize,
		checkConstraintVolumeTemplateStorageClass,
		checkConstraintServerMinimumVersion,
		checkConstraintTLSMinimumVersion,
		checkConstraintTLS,
		checkConstraintCloudNativeGatewayProvisioning,
		checkConstraintCloudNativeGatewayTLS,
		checkConstraintXDCRConnectionTLS,
		checkConstraintPublicNetworking,
		checkConstraintBucketNames,
		checkConstraintBucketSynchronization,
		checkConstraintMemoryAllocations,
		checkConstraintMTLSPaths,
		checkConstraintAutoCompactionCluster,
		checkConstraintLDAPAuthentication,
		checkConstraintLDAPConnectionTLS,
		checkConstraintLDAPAuthorization,
		checkConstraintAutoscalingStabilizationPeriod,
		checkConstraintBackupObjectEndpointSecret,
		checkClusterConstraintMagmaStorageBackend,
		checkConstraintK8sSecurityContext,
		checkConstraintMutuallyExclusiveUpgradeFields,
		checkConstraintBucketsAnnotations,
		checkMigrationConstraints,
		checkAdminServiceConstraints,
		checkClusterRBACConstraints,
	}

	warningChecks := []func(*types.Validator, *couchbasev2.CouchbaseCluster) ([]string, error){
		checkConstraintTwoDataNodesForDeltaRecovery,
		checkConstraintDeltaRecoveryDeprecated,
		checkConstraintServicelessOverAdminService,
	}

	var errs []error

	var warnings []string

	// Check that any CAO annotations are valid
	ws, err := annotations.PopulateWithWarnings(&cluster.Spec, cluster.Annotations)
	warnings = append(warnings, ws...)

	if err != nil {
		errs = append(errs, err)
	}

	for _, check := range checks {
		if err := check(v, cluster); err != nil {
			var composite *errors.CompositeError

			if ok := goerrors.As(err, &composite); ok {
				errs = append(errs, composite.Errors...)
				continue
			}

			errs = append(errs, err)
		}
	}

	for _, check := range warningChecks {
		w, err := check(v, cluster)
		if err != nil {
			var composite *errors.CompositeError

			if ok := goerrors.As(err, &composite); ok {
				errs = append(errs, composite.Errors...)
				continue
			}

			errs = append(errs, err)
		}

		if len(w) > 0 {
			warnings = append(warnings, w...)
		}
	}

	if errs != nil {
		return warnings, errors.CompositeValidationError(errs...)
	}

	return warnings, nil
}

func checkConstraintClusterName(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if len(cluster.Name) > 58 {
		return fmt.Errorf("cluster name must be less than 58 characters")
	}

	return nil
}

func checkConstraintPerServiceClassPDB(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.PerServiceClassPDB {
		return nil
	}

	var totalMinAvailable float32

	var totalIndexPodsMinAvailable int

	var totalKVPodsMinAvailable int

	if cluster.Spec.TotalSize() < 2*len(cluster.Spec.Servers)+1 {
		return fmt.Errorf("not enough nodes compared to serverclasses to enable per-service pdbs")
	}

	for _, serverClass := range cluster.Spec.Servers {
		if serverClass.Size < 2 {
			return fmt.Errorf("server class %s does not contain enough nodes to enable per-service pdbs", serverClass.Name)
		}

		totalMinAvailable += float32(serverClass.Size) - 1

		for _, service := range serverClass.Services {
			if service == couchbasev2.IndexService {
				totalIndexPodsMinAvailable += serverClass.Size - 1
			}

			if service == couchbasev2.DataService {
				totalKVPodsMinAvailable += serverClass.Size - 1
			}
		}
	}

	if cluster.Spec.ClusterSettings.Indexer != nil {
		if totalIndexPodsMinAvailable < cluster.Spec.ClusterSettings.Indexer.NumberOfReplica {
			return fmt.Errorf("cofiguration violates maximum index disruption of %d", cluster.Spec.ClusterSettings.Indexer.NumberOfReplica)
		}
	}

	if cluster.Spec.ClusterSettings.Data != nil {
		if totalKVPodsMinAvailable < cluster.Spec.ClusterSettings.Data.MinReplicasCount {
			return fmt.Errorf("cofiguration violates maximum data disruption of %d", cluster.Spec.ClusterSettings.Data.MinReplicasCount)
		}
	}

	if totalMinAvailable < float32(cluster.Spec.TotalSize())/2 {
		return fmt.Errorf("pod disruption budgets cannot disrupt more than 50 percent of the total server size")
	}

	return nil
}

func checkConstraintMutuallyExclusiveUpgradeFields(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.UpgradeProcess != nil && cluster.Spec.UpgradeStrategy != nil {
		if (*cluster.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade || *cluster.Spec.UpgradeProcess == couchbasev2.DeltaRecovery) && *cluster.Spec.UpgradeStrategy == couchbasev2.ImmediateUpgrade {
			return fmt.Errorf("cannot set spec.upgradeStrategy to ImmediateUpgrade when spec.UpgradeProcess is set to DeltaRecovery")
		}
	}

	return nil
}

var validStorageBackends = map[couchbasev2.CouchbaseStorageBackend]bool{
	couchbasev2.CouchbaseStorageBackendMagma:      true,
	couchbasev2.CouchbaseStorageBackendCouchstore: true,
}

func checkValidStorageBackend(backend, annotation string) error {
	if _, ok := validStorageBackends[couchbasev2.CouchbaseStorageBackend(backend)]; !ok {
		return fmt.Errorf("%s: (%s) annotation must be a valid storage backend: %v", annotation, backend, reflect.ValueOf(validStorageBackends).MapKeys())
	}

	return nil
}

func checkConstraintBucketsAnnotations(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	targetUnmanagedBucketStorageBackendValidation := func(backend, annotation string) error {
		tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
		if err != nil {
			return err
		}

		if after76, err := couchbaseutil.VersionAfter(tag, "7.6.0"); !after76 && err == nil {
			return fmt.Errorf("%s annotation can only be used with server version 7.6.0 or larger", annotation)
		}

		return checkValidStorageBackend(backend, annotation)
	}

	checkStringToBoolBucketAnnotation := func(val, _ string) error {
		return checkStringToBool(val)
	}

	checkStringToUintBucketAnnotation := func(backend, _ string) error {
		return checkStringToUint(backend)
	}

	magmaStorageBackendSupported, err := cluster.IsAtLeastVersion("7.1.0")
	if err != nil {
		return err
	}

	checkDefaultStorageBackend := func(backend, annotation string) error {
		if err := checkValidStorageBackend(backend, annotation); err != nil {
			return err
		}

		if !magmaStorageBackendSupported {
			if couchbasev2.CouchbaseStorageBackend(backend) == couchbasev2.CouchbaseStorageBackendMagma {
				return fmt.Errorf("magma storage backend requires Couchbase Server version 7.1.0 or later")
			}
		}

		return checkValidStorageBackend(backend, annotation)
	}

	bucketAnnotations := map[string]func(string, string) error{
		"cao.couchbase.com/buckets.defaultStorageBackend":               checkDefaultStorageBackend,
		"cao.couchbase.com/buckets.targetUnmanagedBucketStorageBackend": targetUnmanagedBucketStorageBackendValidation,
		"cao.couchbase.com/buckets.enableBucketMigrationRoutines":       checkStringToBoolBucketAnnotation,
		"cao.couchbase.com/buckets.maxConcurrentPodSwaps":               checkStringToUintBucketAnnotation,
	}

	for k, v := range cluster.Annotations {
		if testFunc, ok := bucketAnnotations[k]; ok {
			if err := testFunc(v, k); err != nil {
				errs = append(errs, fmt.Errorf("annotation '%s' failed to parse with error %w", k, err))
				continue
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkMigrationConstraints(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Migration == nil {
		return nil
	}

	if cluster.Spec.Hibernate {
		return fmt.Errorf("spec.hibernate cannot be enabled when spec.migration is configured")
	}

	if cluster.Spec.Migration.NumUnmanagedNodes >= cluster.Spec.TotalSize() {
		return fmt.Errorf("spec.migration.numUnmanagedNodes must be less than the total size of the cluster")
	}

	if cluster.Spec.Migration.MigrationOrderOverride == nil {
		return nil
	}

	if len(cluster.Spec.Migration.MigrationOrderOverride.ServerClassOrder) != 0 {
		validServerConfigs := []string{}

		for _, server := range cluster.Spec.Servers {
			validServerConfigs = append(validServerConfigs, server.Name)
		}

		for _, sc := range cluster.Spec.Migration.MigrationOrderOverride.ServerClassOrder {
			if !slices.Contains(validServerConfigs, sc) {
				return fmt.Errorf("spec.migration.migrationOrderOverride.serverClassOrder contains an invalid server class: %s", sc)
			}
		}
	}

	return nil
}

func checkAdminServiceConstraints(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	for i, config := range cluster.Spec.Servers {
		tag, err := k8sutil.CouchbaseVersion(cluster.Spec.ServerClassCouchbaseImage(&config))
		if err != nil {
			return err
		}

		serviceList := couchbasev2.ServiceList(config.Services)
		if serviceList.Contains(couchbasev2.AdminService) {
			if after76, err := couchbaseutil.VersionAfter(tag, "7.6.0"); !after76 && err == nil {
				return fmt.Errorf("couchbase server version must be greater than 7.6.0 to enable the admin service")
			}

			if len(serviceList) > 1 {
				return fmt.Errorf("spec.servers[%d].services cannot contain the admin service and other services", i)
			}
		}
	}

	return nil
}

// checkConstraintDataServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintDataServiceMemoryQuota(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.DataServiceMemQuota == nil {
		return errors.Required("spec.cluster.dataServiceMemoryQuota", "body", nil)
	}

	if cluster.Spec.ClusterSettings.DataServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintDataServiceMemcachedThreadCounts checks the reader/writer thread count for specific server version.
func checkConstraintDataServiceMemcachedThreadCounts(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	if after71, err := couchbaseutil.VersionAfter(tag, "7.1.0"); !after71 && err == nil {
		serverVerErrStr := "for couchbase server version 7.1.0 or greater"

		if cluster.Spec.ClusterSettings.Data == nil {
			return nil
		}

		if threads := cluster.Spec.ClusterSettings.Data.ReaderThreads; threads != nil && *threads < 4 {
			errs = append(errs, fmt.Errorf("spec.clusterSettings.data.readerThreads %d must be greater than or equal to 4 %s", *threads, serverVerErrStr))
		}

		if threads := cluster.Spec.ClusterSettings.Data.WriterThreads; threads != nil && *threads < 4 {
			errs = append(errs, fmt.Errorf("spec.clusterSettings.data.writerThreads %d must be greater than or equal to 4 %s", threads, serverVerErrStr))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintIndexServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintIndexServiceMemoryQuota(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.IndexServiceMemQuota == nil {
		return errors.Required("spec.cluster.indexServiceMemoryQuota", "body", nil)
	}

	if cluster.Spec.ClusterSettings.IndexServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.indexServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintSearchServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintSearchServiceMemoryQuota(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.SearchServiceMemQuota == nil {
		return errors.Required("spec.cluster.searchServiceMemoryQuota", "body", nil)
	}

	if cluster.Spec.ClusterSettings.SearchServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.searchServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintEventingServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintEventingServiceMemoryQuota(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.EventingServiceMemQuota == nil {
		return errors.Required("spec.cluster.eventingServiceMemoryQuota", "body", nil)
	}

	if cluster.Spec.ClusterSettings.EventingServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.eventingServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintAnalyticsServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintAnalyticsServiceMemoryQuota(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota == nil {
		return errors.Required("spec.cluster.anayticsServiceMemoryQuota", "body", nil)
	}

	if cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
		return fmt.Errorf("spec.cluster.analyticsServiceMemoryQuota in body should be greater than or equal to 1Gi")
	}

	return nil
}

// checkConstraintQueryTemporarySpace checks the query temporary space is higher than the lower limit.
func checkConstraintQueryTemporarySpace(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.ClusterSettings.Query == nil {
		return nil
	}

	if cluster.Spec.ClusterSettings.Query.TemporarySpace.Cmp(*k8sutil.NewResourceQuantityMi(0)) <= 0 {
		return fmt.Errorf("spec.cluster.query.temporarySpace in body should be greater than 0Mi")
	}

	return nil
}

// checkConstraintAutoFailoverTimeout checks the autofailover timeout is within range.
func checkConstraintAutoFailoverTimeout(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.AutoFailoverTimeout == nil {
		return errors.Required("spec.cluster.autoFailoverTimeout", "body", nil)
	}

	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	minSeconds := 5.0
	if after76, err := couchbaseutil.VersionAfter(tag, "7.6.0"); after76 && err == nil {
		minSeconds = 1.0
	}

	if cluster.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() < minSeconds {
		return fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be greater than or equal to the min autoFailover timeout")
	}

	if cluster.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() > 3600.0 {
		return fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be less than or equal to 1h")
	}

	return nil
}

// checkConstraintAutoFailoverTimeout checks the autofailover timeout is within range.
func checkConstraintAutoFailoverMaxCount(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	if after71, err := couchbaseutil.VersionAfter(tag, "7.1.0"); !after71 && err == nil {
		if cluster.Spec.ClusterSettings.AutoFailoverMaxCount > 3 {
			return fmt.Errorf("spec.cluster.autoFailoverMaxCount should be less than or equal to 3")
		}
	}

	return nil
}

// checkConstraintAutoFailoverOnDataDiskIssuesTimePeriod checks the auto failover timeout is within range.
func checkConstraintAutoFailoverOnDataDiskIssuesTimePeriod(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod == nil {
		return errors.Required("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod", "body", nil)
	}

	if cluster.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod.Seconds() < 5.0 {
		return fmt.Errorf("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be greater than or equal to 5s")
	}

	if cluster.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod.Seconds() > 3600.0 {
		return fmt.Errorf("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be less than or equal to 1h")
	}

	return nil
}

// checkConstraintIndexerMemorySnapshotInterval checks the indexer snapshot interval is within range.
func checkConstraintIndexerMemorySnapshotInterval(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.ClusterSettings.Indexer == nil {
		return nil
	}

	if int(cluster.Spec.ClusterSettings.Indexer.MemorySnapshotInterval.Milliseconds()) < 1 {
		return fmt.Errorf("spec.cluster.indexer.memorySnapshotInterval in body must be greater than or equal to 1ms")
	}

	return nil
}

// checkConstraintIndexerStableSnapshotInterval checks the indexer snapshot interval is within range.
func checkConstraintIndexerStableSnapshotInterval(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.ClusterSettings.Indexer == nil {
		return nil
	}

	if int(cluster.Spec.ClusterSettings.Indexer.StableSnapshotInterval.Milliseconds()) < 1 {
		return fmt.Errorf("spec.cluster.indexer.stableSnapshotInterval in body must be greater than or equal to 1ms")
	}

	return nil
}

// checkConstraintAdminSecret checks that the admin secret exists.  If it does then it checks
// that the "username" and "password" keys are present.  If the "password" key is present
// it also checks that the password is of the correct length and using the correct dictionary.
func checkConstraintAdminSecret(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !v.Options.ValidateSecrets {
		return nil
	}

	secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, cluster.Spec.Security.AdminSecret)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("secret %s referenced by spec.security.adminSecret must exist", cluster.Spec.Security.AdminSecret)
	}

	var errs []error

	if _, ok := secret.Data["username"]; !ok {
		errs = append(errs, fmt.Errorf("spec.security.adminSecret must contain \"username\" key"))
	}

	if value, ok := secret.Data["password"]; !ok {
		errs = append(errs, fmt.Errorf("spec.security.adminSecret must contain \"password\" key"))
	} else {
		if len(value) < 6 {
			errs = append(errs, errors.TooShort("password", "spec.security.adminSecret", 6, nil))
		}
		if strings.ContainsAny(string(value), `()<>,;:\"/[]?={}`) {
			errs = append(errs, fmt.Errorf(`password in spec.security.adminSecret must not contain any of the following characters ()<>,;:\"/[]?={}`))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintPrometheusAuthorizationSecret checks that if enabled, the Prometheus token
// based authentication secret exists.  If it does then check that it includes a "token" key.
func checkConstraintPrometheusAuthorizationSecret(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !v.Options.ValidateSecrets {
		return nil
	}

	if cluster.Spec.Monitoring == nil || cluster.Spec.Monitoring.Prometheus == nil {
		return nil
	}

	if cluster.Spec.Monitoring.Prometheus.AuthorizationSecret == nil {
		return nil
	}

	secretName := *cluster.Spec.Monitoring.Prometheus.AuthorizationSecret

	secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("secret %s referenced by spec.monitoring.prometheus.authorizationSecret must exist", secretName)
	}

	if _, ok := secret.Data["token"]; !ok {
		return fmt.Errorf("monitoring authorization secret %s must contain key 'token'", secretName)
	}

	return nil
}

// checkConstraintLoggingPermissible checks persistent volumes are being used.
func checkConstraintLoggingPermissible(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsServerLoggingEnabled() {
		return nil
	}

	if !cluster.IsSupportable() {
		return fmt.Errorf("server logging requires 'spec.servers.volumeMounts' to be configured")
	}

	return nil
}

// checkConstraintAuditLoggingPermissible checks that when using the audit log
// garbage collector, shared volumes are enabled and audit logging is also enabled.
func checkConstraintAuditLoggingPermissible(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsAuditGarbageCollectionSidecarEnabled() && !cluster.IsNativeAuditCleanupEnabled() {
		return nil
	}

	if cluster.IsAuditGarbageCollectionSidecarEnabled() && cluster.IsNativeAuditCleanupEnabled() {
		return fmt.Errorf("'spec.logging.audit.garbageCollection.sidecar' and 'spec.logging.audit.rotation.pruneAge' are mutually exclusive")
	}

	if cluster.IsNativeAuditCleanupEnabled() {
		tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
		if err != nil {
			return err
		}

		if nativeCleanupSupported, err := couchbaseutil.VersionAfter(tag, "7.2.4"); err != nil {
			return err
		} else if !nativeCleanupSupported {
			return fmt.Errorf("'spec.logging.audit.rotation.pruneAge' is only supported for server version 7.2.4+")
		}

		if int(cluster.Spec.Logging.Audit.Rotation.PruneAge.Duration.Seconds()) > pruneAgeMaxSeconds {
			return fmt.Errorf("'spec.logging.audit.rotation.pruneAge' has a maximum of 35791394 seconds")
		}
	}

	if !cluster.IsSupportable() {
		return fmt.Errorf("server audit logging requires 'spec.servers.volumeMounts' to be configured")
	}

	if !cluster.IsAuditLoggingEnabled() {
		return fmt.Errorf("server audit logging requires 'spec.logging.audit.garbageCollection.sidecar.enabled' or 'spec.logging.audit.rotation.pruneAge' to be set")
	}

	return nil
}

func checkConstraintXDCRRemoteAuthentication(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.XDCR.Managed {
		return nil
	}

	var errs []error

	for i, remoteCluster := range cluster.Spec.XDCR.RemoteClusters {
		if v.Options.ValidateSecrets && remoteCluster.AuthenticationSecret != nil {
			secretName := *remoteCluster.AuthenticationSecret

			_, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
			if err != nil {
				errs = append(errs, err)
			}

			if !found {
				errs = append(errs, fmt.Errorf("secret %s referenced by spec.xdcr.remoteClusters[%d].authenticationSecret must exist", secretName, i))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func areKeyspacesValid(allowRule couchbasev2.CouchbaseAllowReplicationMapping) error {
	// Scopes must be set
	if allowRule.SourceKeyspace.Scope == "" {
		return fmt.Errorf("invalid source scope")
	}

	if allowRule.TargetKeyspace.Scope == "" {
		return fmt.Errorf("invalid target scope")
	}

	// Collections must be set on both or neither
	if allowRule.SourceKeyspace.Collection != "" && allowRule.TargetKeyspace.Collection == "" {
		return fmt.Errorf("source collection with no target collection")
	}

	if allowRule.SourceKeyspace.Collection == "" && allowRule.TargetKeyspace.Collection != "" {
		return fmt.Errorf("target collection with no source collection")
	}

	return nil
}

func checkConstraintXDCRReplicationMappings(m couchbasev2.CouchbaseReplication) error {
	// We build up a set of errors overall or nothing
	var errs []error

	// All the keyspaces we're handling
	sourceKeyspacesHandled := make(map[string]bool)
	targetKeyspacesHandled := make(map[string]bool)

	for index, allowRule := range m.ExplicitMapping.AllowRules {
		// Helpers to handle our custom string type and shrink code.
		sourceKeyspace := fmt.Sprintf("%s.%s", allowRule.SourceKeyspace.Scope, allowRule.SourceKeyspace.Collection)
		targetKeyspace := fmt.Sprintf("%s.%s", allowRule.TargetKeyspace.Scope, allowRule.TargetKeyspace.Collection)

		// Check that source and target keyspaces are the same size, i.e. either both contain collections or scopes only or neither.
		if err := areKeyspacesValid(allowRule); err != nil {
			errs = append(errs, fmt.Errorf("explicitMapping.allowRules[%d].sourceKeyspace for %s invalid as source and target keyspaces must both target a scope or a collection: %s => %s (%w)", index, m.Name, sourceKeyspace, targetKeyspace, err))
			continue
		}

		// If we have a collection-level rule then we cannot have a scope-level rule unless it is the inverse: deny and allow.
		if allowRule.SourceKeyspace.Collection != "" {
			scopeOnlyKeyspace := fmt.Sprintf("%s.", allowRule.SourceKeyspace.Scope)
			if _, exists := sourceKeyspacesHandled[scopeOnlyKeyspace]; exists {
				errs = append(errs, fmt.Errorf("explicitMapping.allowRules[%d].sourceKeyspace for %s invalid as less specific rule already exists for source at scope level: %s", index, m.Name, sourceKeyspace))
				continue
			}
		}

		// Make sure no rules exist using the same source or target multiple times.
		if _, exists := sourceKeyspacesHandled[sourceKeyspace]; exists {
			errs = append(errs, fmt.Errorf("explicitMapping.allowRules[%d].sourceKeyspace for %s invalid as rule already exists for source: %s", index, m.Name, sourceKeyspace))
			continue
		}

		if _, exists := targetKeyspacesHandled[targetKeyspace]; exists {
			errs = append(errs, fmt.Errorf("explicitMapping.allowRules[%d].targetKeyspace for %s invalid as rule already exists for target: %s", index, m.Name, targetKeyspace))
			continue
		}

		sourceKeyspacesHandled[sourceKeyspace] = true
		targetKeyspacesHandled[targetKeyspace] = true
	}

	// Keep track of any specific to deny rules
	denyKeyspacesHandled := make(map[string]bool)

	for index, denyRule := range m.ExplicitMapping.DenyRules {
		// Check that we have at least a scope
		if denyRule.SourceKeyspace.Scope == "" {
			errs = append(errs, fmt.Errorf("explicitMapping.denyRules[%d].sourceKeyspace for %s invalid as source no scope defined", index, m.Name))
			continue
		}

		sourceKeyspace := fmt.Sprintf("%s.%s", denyRule.SourceKeyspace.Scope, denyRule.SourceKeyspace.Collection)

		// If we have a collection-level rule then we cannot have a scope-level rule unless it is the inverse: deny and allow.
		if denyRule.SourceKeyspace.Collection != "" {
			scopeOnlyKeyspace := fmt.Sprintf("%s.", denyRule.SourceKeyspace.Scope)
			// Check that it is not in the list of one for deny only, fine to be in the total list
			if _, exists := denyKeyspacesHandled[scopeOnlyKeyspace]; exists {
				errs = append(errs, fmt.Errorf("explicitMapping.denyRules[%d].sourceKeyspace for %s invalid as less specific rule already exists for source at scope level: %s", index, m.Name, sourceKeyspace))
				continue
			}
		}

		// Check for duplicate source keyspace rules in allow and deny
		if _, exists := sourceKeyspacesHandled[sourceKeyspace]; exists {
			errs = append(errs, fmt.Errorf("explicitMapping.denyRules[%d].sourceKeyspace for %s invalid as rule already exists for source: %s", index, m.Name, sourceKeyspace))
			continue
		}

		sourceKeyspacesHandled[sourceKeyspace] = true
		denyKeyspacesHandled[sourceKeyspace] = true
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkConstraintXDCRMigrationMappings(m couchbasev2.CouchbaseMigrationReplication) error {
	// We build up a set of errors overall or nothing
	var errs []error

	for index, migrationMapping := range m.MigrationMapping.Mappings {
		source := migrationMapping.Filter

		// Check that we only have one source using the default collection.
		if source == "_default._default" && len(m.MigrationMapping.Mappings) > 1 {
			errs = append(errs, fmt.Errorf("migrationMapping.mappings[%d].filter for %s invalid as multiple uses of the default collection as a source", index, m.Name))
		}

		// We must always have a target collection specified
		if migrationMapping.TargetKeyspace.Collection == "" {
			errs = append(errs, fmt.Errorf("migrationMapping.mappings[%d].filter for %s invalid as no target collection specified", index, m.Name))
		}
	}

	// No short cut errors here to build up full list
	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func isVersion7OrAbove(cluster *couchbasev2.CouchbaseCluster) (bool, error) {
	// Minimum supported version for scopes and collections is 7
	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return false, err
	}

	return couchbaseutil.VersionAfter(tag, "7.0.0")
}

func createReplicationHash(remoteClusterName string, spec couchbasev2.CouchbaseReplicationSpec) string {
	return fmt.Sprintf("%s/%s/%s", remoteClusterName, spec.Bucket, spec.RemoteBucket)
}

func checkConstraintXDCRReplicationRules(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.XDCR.Managed {
		return nil
	}

	var errs []error

	// We cannot have the same buckets involved in both replication and migration rules in the same remote cluster
	// or in fact no duplicates in general. We do this by hashing the remote name with the source and destination buckets to search for.
	currentRules := make(map[string]bool)

	for _, remoteCluster := range cluster.Spec.XDCR.RemoteClusters {
		replications, err := v.Abstraction.GetCouchbaseReplications(cluster.Namespace, remoteCluster.Replications.Selector)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, replication := range replications.Items {
			hash := createReplicationHash(remoteCluster.Name, replication.Spec)
			if _, exists := currentRules[hash]; exists {
				errs = append(errs, fmt.Errorf("duplicate rule (%s) for XDCR replication in couchbasereplications.couchbase.com/%s", hash, replication.Name))
				continue
			}

			currentRules[hash] = true

			err := checkConstraintXDCRReplicationMappings(replication)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid rule for XDCR replication in couchbasereplications.couchbase.com/%s: %w", replication.Name, err))
			}
		}

		migrations, err := v.Abstraction.GetCouchbaseMigrationReplications(cluster.Namespace, remoteCluster.Replications.Selector)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, migration := range migrations.Items {
			hash := createReplicationHash(remoteCluster.Name, migration.Spec)
			if _, exists := currentRules[hash]; exists {
				errs = append(errs, fmt.Errorf("duplicate rule (%s) for XDCR migration in couchbasereplications.couchbase.com/%s", hash, migration.Name))
				continue
			}

			currentRules[hash] = true

			err := checkConstraintXDCRMigrationMappings(migration)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid rule for XDCR migration in couchbasemigrations.couchbase.com/%s: %w", migration.Name, err))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkConstraintXDCRReplicationScopesAndCollectionsSupported(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.XDCR.Managed {
		return nil
	}

	supportedXDCRScopesAndCollections, err := isVersion7OrAbove(cluster)
	if err != nil {
		return fmt.Errorf("unable to check server version %s for scopes and collections support: %w", cluster.Spec.Image, err)
	}

	var errs []error

	if !supportedXDCRScopesAndCollections {
		for _, remoteCluster := range cluster.Spec.XDCR.RemoteClusters {
			replications, err := v.Abstraction.GetCouchbaseReplications(cluster.Namespace, remoteCluster.Replications.Selector)
			if err != nil {
				errs = append(errs, err)
			} else {
				for _, replication := range replications.Items {
					// Check we have no replication rules for scopes and collections
					if len(replication.ExplicitMapping.AllowRules) > 0 || len(replication.ExplicitMapping.DenyRules) > 0 {
						errs = append(errs, fmt.Errorf("invalid server version (%s) to support XDCR replication of scopes and collections in couchbasereplications.couchbase.com/%s", cluster.Spec.Image, replication.Name))
					}
				}
			}

			migrations, err := v.Abstraction.GetCouchbaseMigrationReplications(cluster.Namespace, remoteCluster.Replications.Selector)
			if err != nil {
				errs = append(errs, err)
			} else if len(migrations.Items) > 0 {
				errs = append(errs, fmt.Errorf("invalid server version (%s) to support XDCR migration of scopes and collections in couchbasemigrations.couchbase.com", cluster.Spec.Image))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkConstraintXDCRReplicationBuckets(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.XDCR.Managed {
		return nil
	}

	var errs []error

	for _, remoteCluster := range cluster.Spec.XDCR.RemoteClusters {
		replications, err := v.Abstraction.GetCouchbaseReplications(cluster.Namespace, remoteCluster.Replications.Selector)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, replication := range replications.Items {
			if err := validateBucketExists(v, cluster, string(replication.Spec.Bucket)); err != nil {
				errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasereplications.couchbase.com/%s must exist: %w", replication.Spec.Bucket, replication.Name, err))
			}
		}

		migrations, err := v.Abstraction.GetCouchbaseMigrationReplications(cluster.Namespace, remoteCluster.Replications.Selector)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, migration := range migrations.Items {
			if err := validateBucketExists(v, cluster, string(migration.Spec.Bucket)); err != nil {
				errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasemigrationreplications.couchbase.com/%s must exist: %w", migration.Spec.Bucket, migration.Name, err))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintServerClassContainsDataService checks are least one class has the data service enabled.
func checkConstraintServerClassContainsDataService(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	for _, config := range cluster.Spec.Servers {
		if couchbasev2.ServiceList(config.Services).Contains(couchbasev2.DataService) {
			return nil
		}
	}

	return errors.Required("at least one \"data\" service", "spec.servers.services", nil)
}

func checkoutConstraintNoServicelessClassBelow76(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	clusterVersionImage, err := cluster.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	version, err := k8sutil.CouchbaseVersion(clusterVersionImage)
	if err != nil {
		return err
	}

	if after76, err := couchbaseutil.VersionAfter(version, "7.6.0"); err != nil {
		return err
	} else if after76 {
		return nil
	}

	for i, config := range cluster.Spec.Servers {
		if len(config.Services) == 0 {
			return fmt.Errorf("spec.servers[%d].services requires atleast one service", i)
		}
	}

	return nil
}

func checkConstraintDeltaRecoveryDeprecated(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if cluster.Spec.UpgradeProcess != nil && *cluster.Spec.UpgradeProcess == couchbasev2.DeltaRecovery {
		return []string{"DeltaRecovery is deprecated, please use InPlaceUpgrade instead"}, nil
	}

	return nil, nil
}

func checkConstraintMoreThanTwoDataNodesMultiNodeClusterInPlaceUpgrade(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.UpgradeProcess == nil {
		return nil
	}

	if (*cluster.Spec.UpgradeProcess == couchbasev2.DeltaRecovery || *cluster.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade) && (cluster.GetNumberOfDataServiceNodes() < 2 && len(cluster.Spec.Servers) > 1) {
		return fmt.Errorf("cannot enable InPlaceUpgrade with one data service node in a multi-node cluster")
	}

	return nil
}

// checkConstraintTwoDataNodesForDeltaRecovery checks there are at least two data nodes when trying to use InPlaceUpgrade.
func checkConstraintTwoDataNodesForDeltaRecovery(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if cluster.Spec.UpgradeProcess != nil {
		if cluster.GetNumberOfDataServiceNodes() < 2 && (*cluster.Spec.UpgradeProcess == couchbasev2.DeltaRecovery || *cluster.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade) {
			return []string{"It is not possible to perform an online In-place Upgrade for a single-node cluster, the cluster will be offline while being upgraded. Please use the Swap Rebalance method to keep the cluster online."}, nil
		}
	}

	return nil, nil
}

// checkConstraintClusterSupportable checks that if you have one supportable class, they all are.
func checkConstraintClusterSupportable(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.AnySupportable() && !cluster.IsSupportable() {
		return fmt.Errorf("all server classes must have volumes defined, all classes with stateful services must have the 'default' mount defined")
	}

	return nil
}

// checkConstraintServiceEnabledForVolumeMount checks that volume mounts are only used with the correct services.
func checkConstraintServiceEnabledForVolumeMount(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for index, config := range cluster.Spec.Servers {
		if !config.VolumeMounts.HasVolumeMounts() {
			continue
		}

		services := couchbasev2.ServiceList(config.Services)

		if config.VolumeMounts.DataClaim != "" && !services.Contains(couchbasev2.DataService) {
			errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.data requires the data service to be enabled", index))
		}

		if config.VolumeMounts.IndexClaim != "" && !services.ContainsAny(couchbasev2.IndexService, couchbasev2.SearchService) {
			errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.index requires the index or search service to be enabled", index))
		}

		if config.VolumeMounts.AnalyticsClaims != nil && !services.Contains(couchbasev2.AnalyticsService) {
			errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.analytics requires the analytics service to be enabled", index))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintDefaultAndLogVolumesMututallyExclusive checks either logs or default mounts
// are specified, but never both at the same time.
func checkConstraintDefaultAndLogVolumesMututallyExclusive(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for index, config := range cluster.Spec.Servers {
		if !config.VolumeMounts.HasVolumeMounts() {
			continue
		}

		if config.VolumeMounts.LogsClaim != "" && config.VolumeMounts.DefaultClaim != "" {
			errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.servers[%d].volumeMounts", index), "", "default"))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintServiceMountsUsedWithDefaultMount checks that service specific mounts are
// only used when the default mount is i.e. persisting data and not /etc just doesn't work!
func checkConstraintServiceMountsUsedWithDefaultMount(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for index, config := range cluster.Spec.Servers {
		if !config.VolumeMounts.HasVolumeMounts() {
			continue
		}

		if config.VolumeMounts.HasSubMounts() && !config.VolumeMounts.HasDefaultMount() {
			errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].volumeMounts", index), nil))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintTemplateExistsForMount checks that volume mounts are only specified when the
// corresponding service is enabled.
func checkConstraintTemplateExistsForMount(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	templateNamesEnum := []interface{}{}

	for _, template := range cluster.Spec.VolumeClaimTemplates {
		templateNamesEnum = append(templateNamesEnum, template.ObjectMeta.Name)
	}

	for index, config := range cluster.Spec.Servers {
		if !config.VolumeMounts.HasVolumeMounts() {
			continue
		}

		if config.VolumeMounts.DefaultClaim != "" && cluster.Spec.GetVolumeClaimTemplate(config.VolumeMounts.DefaultClaim) == nil {
			errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].default", index), "", config.VolumeMounts.DefaultClaim, templateNamesEnum))
		}

		if config.VolumeMounts.DataClaim != "" && cluster.Spec.GetVolumeClaimTemplate(config.VolumeMounts.DataClaim) == nil {
			errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].data", index), "", config.VolumeMounts.DataClaim, templateNamesEnum))
		}

		if config.VolumeMounts.IndexClaim != "" && cluster.Spec.GetVolumeClaimTemplate(config.VolumeMounts.IndexClaim) == nil {
			errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].index", index), "", config.VolumeMounts.IndexClaim, templateNamesEnum))
		}

		for analyticsIndex, claim := range config.VolumeMounts.AnalyticsClaims {
			if cluster.Spec.GetVolumeClaimTemplate(claim) == nil {
				errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].analytics[%d]", index, analyticsIndex), "", claim, templateNamesEnum))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintVolumeTemplateNameUnique checks that volume claim templates have unique
// names and therefore selection is unambiguous.
func checkConstraintVolumeTemplateNameUnique(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	names := map[string]interface{}{}

	for _, template := range cluster.Spec.VolumeClaimTemplates {
		if _, ok := names[template.ObjectMeta.Name]; ok {
			return errors.DuplicateItems("spec.volumeClaimTemplates.metadata.name", "body")
		}

		names[template.ObjectMeta.Name] = nil
	}

	return nil
}

// checkConstraintVolumeTemplateSize checks that volume claim templates have a resource
// request (aka size) specified, and it's greater than zero.
func checkConstraintVolumeTemplateSize(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for _, template := range cluster.Spec.VolumeClaimTemplates {
		if template.Spec.Resources.Requests == nil {
			errs = append(errs, errors.Required(string(v1.ResourceStorage), "spec.volumeClaimTemplates.resources.requests", nil))
			continue
		}

		value, ok := template.Spec.Resources.Requests[v1.ResourceStorage]
		if !ok {
			errs = append(errs, errors.Required(string(v1.ResourceStorage), "spec.volumeClaimTemplates.resources.requests", nil))
			continue
		}

		if value.Sign() <= 0 {
			errs = append(errs, fmt.Errorf("spec.volumeClaimTemplates.resources.requests in body should be greater than 0"))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintVolumeTemplateStorageClass checks that volume claim templates reference
// a storage class that actually exists.  When using volume expansion it also checks that
// said feature is enabled for that storage class.
func checkConstraintVolumeTemplateStorageClass(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !v.Options.ValidateStorageClasses {
		return nil
	}

	var errs []error

	if (cluster.Spec.VolumeClaimTemplates == nil || len(cluster.Spec.VolumeClaimTemplates) == 0) && cluster.Spec.EnableOnlineVolumeExpansion {
		errs = append(errs, fmt.Errorf("spec.cluster.enableOnlineVolumeExpansion cannot be enabled since no volume claim templates have been definied"))
	}

	// If volumeClaimTemplates have been set, we should check a default exists on the cluster in case the storageClassName is omitted from any templates.
	var hasDefaultStorageClass bool

	if len(cluster.Spec.VolumeClaimTemplates) > 0 {
		scList, err := v.Abstraction.GetStorageClasses()

		if err != nil {
			errs = append(errs, err)
		}

		hasDefaultStorageClass = util.CheckDefaultStorageClassExists(scList)
	}

	for i, template := range cluster.Spec.VolumeClaimTemplates {
		if template.Spec.StorageClassName == nil {
			if !hasDefaultStorageClass {
				errs = append(errs, fmt.Errorf("spec.volumeClaimTemplates[%d].spec.storageClassName has not been configured and no default exists in the cluster", i))
			}

			continue
		}

		storageClassName := *template.Spec.StorageClassName

		storageClass, found, err := v.Abstraction.GetStorageClass(storageClassName)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if !found {
			errs = append(errs, fmt.Errorf("storage class %s must exist", storageClassName))
			continue
		}

		if !cluster.Spec.EnableOnlineVolumeExpansion {
			continue
		}

		if storageClass.AllowVolumeExpansion == nil || !*storageClass.AllowVolumeExpansion {
			errs = append(errs, fmt.Errorf("spec.cluster.enableOnlineVolumeExpansion cannot be enabled since storage class %q does not specify `allowVolumeExpansion=true`", storageClassName))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintServerMinimumVersion checks that the Couchbase version is supported.
func checkConstraintServerMinimumVersion(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// version check
	currentVersionString, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return fmt.Errorf("unsupported Couchbase version: %w", err)
	}

	currentVersion, err := couchbaseutil.NewVersion(currentVersionString)
	if err != nil {
		return fmt.Errorf("unsupported Couchbase version: %w", err)
	}

	// current version must be equal or greater than min version
	minVersion, _ := couchbaseutil.NewVersion(constants.CouchbaseVersionMin)
	if currentVersion.Less(minVersion) {
		return fmt.Errorf("unsupported Couchbase version: %s, minimum version required: %s", currentVersion, constants.CouchbaseVersionMin)
	}

	return nil
}

func checkConstraintTLSMinimumVersion(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsTLSEnabled() {
		return nil
	}

	currentVersionString, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)

	if err != nil {
		return fmt.Errorf("unsupported Couchbase version: %w", err)
	}

	if after71, err := couchbaseutil.VersionAfter(currentVersionString, "7.1.0"); err != nil {
		return err
	} else if !after71 {
		if invalidVersion, err := tlsutil.StringGreaterEqual(string(cluster.Spec.Networking.TLS.TLSMinimumVersion), "TLS1.3"); err != nil {
			return err
		} else if invalidVersion {
			return fmt.Errorf("tls1.3 is only supported for Couchbase 7.1.0+")
		}
	}

	if after76, err := couchbaseutil.VersionAfter(currentVersionString, "7.6.0"); err != nil {
		return err
	} else if after76 {
		if validVersion, err := tlsutil.StringGreaterEqual(string(cluster.Spec.Networking.TLS.TLSMinimumVersion), "TLS1.2"); err != nil {
			return err
		} else if !validVersion {
			return fmt.Errorf("tls1.0 and tls1.1 are not supported for Couchbase 7.6.0+")
		}
	}

	return nil
}

func checkConstraintServerImagesSet(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	versionSet := map[string]bool{}

	versionSet[cluster.Spec.Image] = true

	for _, serverConf := range cluster.Spec.Servers {
		if serverConf.Image != "" {
			versionSet[serverConf.Image] = true
		}
	}

	clusterImageVersion, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	for image := range versionSet {
		if classImageVersion, err := k8sutil.CouchbaseVersion(image); err != nil {
			return err
		} else if classImageVersion != clusterImageVersion && classImageVersion != cluster.Status.CurrentVersion {
			return fmt.Errorf("server class image version (%s) must match cluster image version (%s) or current version (%s)", classImageVersion, clusterImageVersion, cluster.Status.CurrentVersion)
		}
	}

	if len(versionSet) > 2 {
		versions := []string{}
		for version := range versionSet {
			versions = append(versions, version)
		}

		return fmt.Errorf("a maximum of two couchbase server images can be used in a single cluster, %v images are in use: %v", len(versionSet), versions)
	}

	if err := checkComatibleImages(cluster); err != nil {
		return err
	}

	if err := checkClusterImageHighest(cluster); err != nil {
		return err
	}

	return nil
}

func checkComatibleImages(cluster *couchbasev2.CouchbaseCluster) error {
	lowImage, err := cluster.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	highImage, err := cluster.Spec.HighestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	lowVersion, err := k8sutil.CouchbaseVersion(lowImage)
	if err != nil {
		return err
	}

	highVersion, err := couchbaseutil.CouchbaseImageVersion(highImage)
	if err != nil {
		return err
	}

	return couchbaseutil.CheckUpgradePath(lowVersion, highVersion)
}

func checkClusterImageHighest(cluster *couchbasev2.CouchbaseCluster) error {
	clusterImageVersion, err := couchbaseutil.NewVersionFromImage(cluster.Spec.Image)
	if err != nil {
		return err
	}

	for _, sc := range cluster.Spec.Servers {
		if sc.Image == "" || sc.Image == cluster.Spec.Image {
			continue
		}

		if classImageVersion, err := couchbaseutil.NewVersionFromImage(sc.Image); err != nil {
			return err
		} else if clusterImageVersion.Less(classImageVersion) {
			return fmt.Errorf("server class image version (%s) cannot be higher than cluster image version (%s)", classImageVersion, clusterImageVersion)
		}
	}

	return nil
}

// checkConstraintTLS checks that the X.509v3 SANs are correctly configured for use with
// Couchbase on Kubernetes, that the referenced secret exists and is correctly formatted.
func checkConstraintTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsTLSEnabled() {
		return nil
	}

	subjectAltNames := util_x509.MandatorySANs(cluster.Name, cluster.Namespace)

	if cluster.Spec.Networking.DNS != nil {
		subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", cluster.Spec.Networking.DNS.Domain))
	}

	errs := validateTLS(v, cluster, subjectAltNames)

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintCloudNativeGatewayTLS checks the validity of the Cloud Native Gateway TLS configs passed.
func checkConstraintCloudNativeGatewayTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Networking.CloudNativeGateway == nil {
		return nil
	}

	if cluster.Spec.Networking.CloudNativeGateway.TLS == nil {
		return nil
	}

	return validateCloudNativeGatewayServerTLS(v, cluster)
}

// checkConstraintCloudNativeGatewayProvisioning validates whether Cloud Native Gateway could be provisioned.
func checkConstraintCloudNativeGatewayProvisioning(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Networking.CloudNativeGateway == nil {
		return nil
	}

	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	if minSrvVerForCNG, err := couchbaseutil.VersionAfter(tag, constants.MinimumCouchbaseVersionForCNG); !minSrvVerForCNG && err == nil {
		return fmt.Errorf("cb server version must be %s or later for cloud native gateway support", constants.MinimumCouchbaseVersionForCNG)
	}

	if cngVerUnrestricted, err := couchbaseutil.VersionAfter(tag, constants.MinimumCouchbaseVersionNoCNGRestriction); !cngVerUnrestricted && err == nil {
		cngVer, err := k8sutil.CouchbaseVersion(cluster.Spec.CloudNativeGatewayImage())

		if err != nil {
			return err
		}

		if unallowedCNGVer, err := couchbaseutil.VersionAfter(cngVer, constants.MinimumCNGVersionWithCBAuthSupport); unallowedCNGVer && err == nil {
			return fmt.Errorf(
				"cb server version must be %s or later to support cloud native gateway version %s or later",
				constants.MinimumCouchbaseVersionNoCNGRestriction,
				constants.MinimumCNGVersionWithCBAuthSupport)
		}
	}

	if err != nil {
		return err
	}

	return nil
}

// checkConstraintXDCRConnectionTLS checks that the referenced XDCR connection secrets
// exist and are correctly formatted.
func checkConstraintXDCRConnectionTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	errs := validateTLSXDCR(v, cluster)

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintPublicNetworking checks that when the intention (this is implicit, thus
// probably bad) is to use public networking, then both TLS and DNS are configured.
func checkConstraintPublicNetworking(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.IsExposedFeatureServiceTypePublic() && !cluster.Spec.IsAdminConsoleServiceTypePublic() {
		return nil
	}

	var errs []error

	if cluster.Spec.Networking.TLS == nil {
		errs = append(errs, errors.Required("spec.networking.tls", "body", nil))
	}

	if cluster.Spec.Networking.DNS == nil {
		errs = append(errs, errors.Required("spec.networking.dns", "body", nil))
	}

	for _, memberConfig := range cluster.Spec.Servers {
		if memberConfig.Pod == nil {
			continue
		}

		if cluster.Spec.Networking.DNS != nil && memberConfig.Pod.Spec.HostNetwork {
			errs = append(errs, fmt.Errorf("cannot enable external DNS and hostnetworking"))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintBucketNames checks that all buckets referenced by this cluster have
// unique names.
func checkConstraintBucketNames(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	return validateBucketNameConstraints(v, cluster, nil)
}

// checkConstraintBucketSynchronization checks that when synchronization is enabled,
// that a label selector has been provided by the user.
func checkConstraintBucketSynchronization(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Buckets.Managed {
		return nil
	}

	if !cluster.Spec.Buckets.Synchronize {
		return nil
	}

	if cluster.Spec.Buckets.Selector != nil {
		return nil
	}

	return errors.Required("spec.buckets.selector", "body", nil)
}

// checkConstraintMemoryAllocations checks that all buckets referenced by this cluster
// have total memory requirements less than or equal to the data service memory quota.
func checkConstraintMemoryAllocations(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	return validateMemoryConstraints(v, cluster, nil)
}

// checkConstraintMTLSPaths checks that when mTLS is enabled, then paths are specified
// in order to extract user identinty from the X.509 client certificate.
func checkConstraintMTLSPaths(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsMutualTLSEnabled() {
		return nil
	}

	if len(cluster.Spec.Networking.TLS.ClientCertificatePaths) == 0 {
		return errors.TooFewItems("spec.networking.tls.clientCertificatePaths", "", 1, nil)
	}

	return nil
}

func checkConstraintAutoCompactionCluster(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	err := checkConstraintAutoCompactionTimeWindow(cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow)
	if err != nil {
		return err
	}

	return checkConstraintAutoCompactionPurgeInterval(cluster.Spec.ClusterSettings.AutoCompaction.TombstonePurgeInterval)
}

func checkConstraintAutoCompactionBucket(autoCompaction *couchbasev2.AutoCompactionSpecBucket) error {
	if autoCompaction == nil {
		return nil
	}

	if autoCompaction.TimeWindow != nil {
		err := checkConstraintAutoCompactionTimeWindow(*autoCompaction.TimeWindow)
		if err != nil {
			return err
		}
	}

	if autoCompaction.DatabaseFragmentationThreshold != nil && autoCompaction.DatabaseFragmentationThreshold.Size != nil {
		if autoCompaction.DatabaseFragmentationThreshold.Size.Cmp(*k8sutil.NewResourceQuantityMi(0)) <= 0 {
			return fmt.Errorf("autoCompaction.databaseFragmentationThreshold.size should be greater than 0Mi")
		}
	}

	if autoCompaction.ViewFragmentationThreshold != nil && autoCompaction.ViewFragmentationThreshold.Size != nil {
		if autoCompaction.ViewFragmentationThreshold.Size.Cmp(*k8sutil.NewResourceQuantityMi(0)) <= 0 {
			return fmt.Errorf("autoCompaction.viewFragmentationThreshold.size should be greater than 0Mi")
		}
	}

	return checkConstraintAutoCompactionPurgeInterval(autoCompaction.TombstonePurgeInterval)
}

// checkConstraintAutoCompactionTimeWindow checks that the auto-compaction time window is valid with
// start time being before end time.
func checkConstraintAutoCompactionTimeWindow(timeWindow couchbasev2.TimeWindow) error {
	if timeWindow.Start != nil {
		// both start and end are required for valid time window
		if timeWindow.End == nil {
			return errors.Required("autoCompaction.timeWindow.end", "body", nil)
		}

		// cluster will not start if start and end times are the same
		if *timeWindow.Start == *timeWindow.End {
			return fmt.Errorf("autoCompaction.timeWindow.start cannot be the same as autoCompaction.timeWindow.end")
		}
	} else if timeWindow.End != nil {
		// cannot have end without start
		return errors.Required("autoCompaction.timeWindow.start", "body", nil)
	}

	return nil
}

// checkConstraintAutoCompactionTombstonePurgeInterval checks that the tombstone purge
// interval is in range.
func checkConstraintAutoCompactionPurgeInterval(purgeInterval *metav1.Duration) error {
	if purgeInterval != nil {
		purgeIntervalHours := purgeInterval.Duration.Hours()
		if purgeIntervalHours < 1.0 {
			return fmt.Errorf("autoCompaction.tombstonePurgeInterval in body should be greater than or equal to 1h")
		}

		if purgeIntervalHours > 60.0*24.0 {
			return fmt.Errorf("autoCompaction.tombstonePurgeInterval in body should be less than or equal to 60d")
		}
	}

	return nil
}

// checkConstraintLDAPAuthentication checks that when enabled, either a template or
// query are provided for LDAP authentication.
func checkConstraintLDAPAuthentication(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	ldap := cluster.Spec.Security.LDAP

	if ldap == nil {
		return nil
	}

	if !ldap.AuthenticationEnabled {
		return nil
	}

	if ldap.UserDNMapping.Template == "" && ldap.UserDNMapping.Query == "" {
		return errors.Required("spec.security.ldap.userDNMapping", "body", nil)
	}

	if ldap.UserDNMapping.Template != "" && ldap.UserDNMapping.Query != "" {
		return fmt.Errorf("ldap.userDNMapping must contain either query or template")
	}

	return nil
}

func isTLSSecretRequired(cluster *couchbasev2.CouchbaseCluster) (bool, error) {
	requestedVersionString, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return false, fmt.Errorf("unsupported Couchbase version: %w", err)
	}

	requestedVersion, err := couchbaseutil.NewVersion(requestedVersionString)
	if err != nil {
		return false, fmt.Errorf("unsupported Couchbase version: %w", err)
	}

	minimumVersionNoSecret, _ := couchbaseutil.NewVersion("7.1.0")

	return requestedVersion.Less(minimumVersionNoSecret) ||
		(requestedVersion.GreaterEqual(minimumVersionNoSecret) &&
			(cluster.Spec.Networking.TLS != nil && len(cluster.Spec.Networking.TLS.RootCAs) == 0)), nil
}

// checkConstraintLDAPConnectionTLS checks that when enabled, the encryption type and
// TLS secrets exists and is correctly formatted for LDAPS.
func checkConstraintLDAPConnectionTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	ldap := cluster.Spec.Security.LDAP

	if ldap == nil {
		return nil
	}

	if !ldap.EnableCertValidation {
		return nil
	}

	if ldap.Encryption == couchbasev2.LDAPEncryptionNone {
		return fmt.Errorf("encryption must be one of %s | %s, when serverCertValidation is enabled", couchbasev2.LDAPEncryptionTLS, couchbasev2.LDAPEncryptionStartTLS)
	}

	tlsSecretName := cluster.Spec.Security.LDAP.TLSSecret

	secretRequired, err := isTLSSecretRequired(cluster)

	if err != nil {
		return err
	}

	if tlsSecretName == "" && secretRequired {
		return errors.Required("spec.security.ldap.tlsSecret", "body", nil)
	}

	if v.Options.ValidateSecrets {
		tlsSecret, found, err := v.Abstraction.GetSecret(cluster.Namespace, tlsSecretName)
		if err != nil {
			return err
		}

		if !found {
			return fmt.Errorf("secret %s referenced by security.ldap.tlsSecret must exist", tlsSecretName)
		}

		if _, ok := tlsSecret.Data["ca.crt"]; !ok {
			return fmt.Errorf("ldap tls secret %s must contain key 'ca.crt'", tlsSecretName)
		}
	}

	return nil
}

// checkConstraintLDAPAuthorization checks that when enabled, a query is provided
// for LDAP authorization.
func checkConstraintLDAPAuthorization(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	ldap := cluster.Spec.Security.LDAP

	if ldap == nil {
		return nil
	}

	// require groups query when group auth enabled
	if !ldap.AuthorizationEnabled {
		return nil
	}

	if ldap.GroupsQuery == "" {
		return errors.Required("security.ldap.groupsQuery", "body", nil)
	}

	return nil
}

// checkConstraintAutoscalingStabilizationPeriod checks the autoscaling stablization period is greater than the lower limit.
func checkConstraintAutoscalingStabilizationPeriod(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	stabilizationPeriod := cluster.Spec.AutoscaleStabilizationPeriod

	if stabilizationPeriod == nil {
		return nil
	}

	if stabilizationPeriod.Duration < 0 {
		return fmt.Errorf("spec.autoscaleStabilizationPeriod must be greater than or equal to 0s")
	}

	return nil
}

func checkStringToUint(val string) error {
	_, err := strconv.ParseUint(val, 10, 64)
	return err
}

func checkStringToBool(val string) error {
	_, err := strconv.ParseBool(val)
	return err
}

func checkHistoryRetentionBytesAnnotation(val string) error {
	bytes, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return err
	}

	return checkHistoryRetentionBytes(bytes)
}

func checkHistoryRetentionBytes(bytes uint64) error {
	minExpected := uint64(2 * 1024 * 1024 * 1024)
	if bytes > 0 && bytes < minExpected {
		return fmt.Errorf("historyRetention.bytes value %d is less than minimum working value of %d", bytes, minExpected)
	}

	return nil
}

func checkBucketHistoryRetentionSettings(bucket *couchbasev2.CouchbaseBucket) error {
	if bucket.Spec.HistoryRetentionSettings != nil {
		return checkHistoryRetentionBytes(bucket.Spec.HistoryRetentionSettings.Bytes)
	}

	return nil
}

func checkMagmaDataBlockSize(val string) error {
	bytes, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return err
	}

	minExpected := uint64(4 * 1024)
	maxExpected := uint64(128 * 1024)

	if bytes < minExpected || bytes > maxExpected {
		return fmt.Errorf("data block size %d must be in the size range of %d to %d", bytes, minExpected, maxExpected)
	}

	return nil
}

func checkBucketAnnotations(bucket *couchbasev2.CouchbaseBucket) []error {
	cdcAnnotations := map[string]func(string) error{
		"cao.couchbase.com/historyRetention.seconds":                  checkStringToUint,
		"cao.couchbase.com/historyRetention.bytes":                    checkHistoryRetentionBytesAnnotation,
		"cao.couchbase.com/historyRetention.collectionHistoryDefault": checkStringToBool,
		"cao.couchbase.com/magmaSeqTreeDataBlockSize":                 checkMagmaDataBlockSize,
		"cao.couchbase.com/magmaKeyTreeDataBlockSize":                 checkMagmaDataBlockSize,
	}

	var errs []error

	for k, v := range bucket.Annotations {
		if testFunc, ok := cdcAnnotations[k]; ok {
			if bucket.Spec.StorageBackend != couchbasev2.CouchbaseStorageBackendMagma && len(bucket.Spec.StorageBackend) != 0 {
				errs = append(errs, fmt.Errorf("annotation '%s' can only be used with magma storage backend", k))
				continue
			}

			if err := testFunc(v); err != nil {
				errs = append(errs, fmt.Errorf("annotation '%s' failed to parse with error %w", k, err))
				continue
			}
		}
	}

	return errs
}

func CheckConstraintsBucket(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	err := annotations.Populate(&bucket.Spec, bucket.Annotations)

	if err != nil {
		return err
	}

	if checkAnnotationSkipValidation(bucket.Annotations) {
		return nil
	}

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if bucket.Spec.StorageBackend != "" && bucket.Spec.MemoryQuota == nil {
		errs = append(errs, fmt.Errorf("spec.memoryQuota (nil) in body should be present and greater than or equal to 1024Mi for spec.storageBackend: magma"))
	}

	if bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackendMagma && bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
		errs = append(errs, fmt.Errorf("spec.memoryQuota (%v) in body should be greater than or equal to 1024Mi for spec.storageBackend: magma", bucket.Spec.MemoryQuota))
	}

	if bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackendMagma && bucket.Spec.EvictionPolicy != couchbasev2.CouchbaseBucketEvictionPolicyFullEviction {
		errs = append(errs, fmt.Errorf("spec.evictionPolicy (%v) must be fullEviction for magma storage backend", bucket.Spec.EvictionPolicy))
	}

	if bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackendMagma {
		if err := checkBucketClustersMagmaStorageBackend(v, bucket); err != nil {
			errs = append(errs, err)
		}

		if bucket.Spec.EnableIndexReplica {
			errs = append(errs, fmt.Errorf("cannot set spec.enableIndexReplica to true for magma buckets"))
		}
	}

	if bucket.Spec.MaxTTL != nil {
		if err := checkMaxTTL("spec.maxTTL", bucket.Spec.MaxTTL, false); err != nil {
			errs = append(errs, err)
		}
	}

	if err := validateBucketNameConstraints(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if err := validateMemoryConstraints(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if err := checkBucketScopesUnique(v, bucket.Namespace, couchbasev2.BucketCRDResourceKind, bucket.Name, bucket.Spec.Scopes); err != nil {
		errs = append(errs, err)
	}

	if err := checkBucketReplicasCount(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if err := checkBucketHistoryRetentionSettings(bucket); err != nil {
		errs = append(errs, err)
	}

	if err := checkConstraintAutoCompactionBucket(bucket.Spec.AutoCompaction); err != nil {
		errs = append(errs, err)
	}

	errs = append(errs, checkBucketAnnotations(bucket)...)

	if bucket.Spec.SampleBucket {
		if err := checkSampleBucketFieldPresets(bucket.Spec.ConflictResolution, bucket.Spec.EnableIndexReplica); err != nil {
			errs = append(errs, err)
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkBucketReplicasCount(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	var clusters = new(couchbasev2.CouchbaseClusterList)

	if cluster != nil {
		clusters.Items = []couchbasev2.CouchbaseCluster{*cluster}
	} else {
		allClusters, err := v.Abstraction.GetCouchbaseClusters(bucket.Namespace)
		if err != nil {
			return err
		}

		clusters = allClusters
	}

	for _, cluster := range clusters.Items {
		if cluster.Spec.ClusterSettings.Data == nil || cluster.Spec.ClusterSettings.Data.MinReplicasCount <= bucket.Spec.Replicas {
			continue
		}

		clusterBucketSelector, err := metav1.LabelSelectorAsSelector(cluster.Spec.Buckets.Selector)
		if err != nil {
			return err
		}

		if cluster.Spec.Buckets.Selector == nil || clusterBucketSelector.Matches(labels.Set(bucket.Labels)) {
			return fmt.Errorf("spec.replicas (%v) should be atleast %v (by %s, spec.cluster.data.minReplicasCount)",
				bucket.Spec.Replicas, cluster.Spec.ClusterSettings.Data.MinReplicasCount, cluster.Name)
		}
	}

	return nil
}

func CheckConstraintsEphemeralBucket(v *types.Validator, bucket *couchbasev2.CouchbaseEphemeralBucket, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	err := annotations.Populate(&bucket.Spec, bucket.Annotations)

	if err != nil {
		return err
	}

	if checkAnnotationSkipValidation(bucket.Annotations) {
		return nil
	}

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if bucket.Spec.MaxTTL != nil {
		if err := checkMaxTTL("spec.maxTTL", bucket.Spec.MaxTTL, false); err != nil {
			errs = append(errs, err)
		}
	}

	if err := validateBucketNameConstraints(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if err := validateMemoryConstraints(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if err := checkBucketScopesUnique(v, bucket.Namespace, couchbasev2.EphemeralBucketCRDResourceKind, bucket.Name, bucket.Spec.Scopes); err != nil {
		errs = append(errs, err)
	}

	if bucket.Spec.SampleBucket {
		if err := checkSampleBucketFieldPresets(bucket.Spec.ConflictResolution, false); err != nil {
			errs = append(errs, err)
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsMemcachedBucket(v *types.Validator, bucket *couchbasev2.CouchbaseMemcachedBucket, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	if checkAnnotationSkipValidation(bucket.Annotations) {
		return nil
	}

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if err := validateBucketNameConstraints(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if err := validateMemoryConstraints(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsReplication(_ *types.Validator, _ *couchbasev2.CouchbaseReplication) error {
	return nil
}

func CheckConstraintsCouchbaseUser(v *types.Validator, user *couchbasev2.CouchbaseUser) error {
	var errs []error

	if checkAnnotationSkipValidation(user.Annotations) {
		return nil
	}

	// only 'local' and 'ldap' auth domains accepted
	domain := user.Spec.AuthDomain
	switch domain {
	case couchbasev2.InternalAuthDomain:
		// password is required for internal auth domain
		authSecretName := user.Spec.AuthSecret
		if authSecretName == "" {
			emsg := fmt.Sprintf("spec.authSecret for `%s` domain", domain)
			errs = append(errs, errors.Required(emsg, user.Name, nil))
		} else if v.Options.ValidateSecrets {
			// Check the ldap auth secret exists and has the correct keys
			authSecret, found, err := v.Abstraction.GetSecret(user.Namespace, authSecretName)
			if err != nil {
				errs = append(errs, err)
			} else if !found {
				errs = append(errs, fmt.Errorf("secret %s referenced by user.spec.authSecret for `%s` must exist", authSecretName, user.Name))
			} else if _, ok := authSecret.Data["password"]; !ok {
				errs = append(errs, fmt.Errorf("ldap auth secret %s must contain password", authSecretName))
			}
		}
	case couchbasev2.LDAPAuthDomain:
		// authSecret not accepted for LDAP user
		if authSecretName := user.Spec.AuthSecret; authSecretName != "" {
			errs = append(errs, fmt.Errorf("secret %s not allowed for LDAP user `%s`", authSecretName, user.Name))
		}
	default:
		return fmt.Errorf("unknown auth domain: %s", user.Spec.AuthDomain)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// commonArrayPrefixMatchString takes two arrays, it picks the longest common length
// e.g. the shortest array length, and returns true if the prefixes match.
func commonArrayPrefixMatchString(a, b []string) bool {
	min := len(a)
	if len(b) < min {
		min = len(b)
	}

	for i := 0; i < min; i++ {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// checkBucketScopeOrCollectionNamesWithDefaultsOverlap checks each path in the given array for overlap e.g.
// "bucket" overlaps with "bucket.scope".  Algorithmically, we only care about paths whose lengths differ,
// and they have a maximal common prefix e.g. if one path is 10 elements, and another is 20, then 10 is the
// maximal common prefix length.  It turns out that CRD validation ensures each path is unique, so if
// a seen and the current path have the same length, they cannot be the same and alias.
func checkBucketScopeOrCollectionNamesWithDefaultsOverlap(paths []couchbasev2.BucketScopeOrCollectionNameWithDefaults) error {
	r := regexp.MustCompile(`(?:[^\\.]|\\.)+`)

	// Record all the keys we've seen so far, comparing each new key to this list.
	seen := [][]string{}

	for _, path := range paths {
		// Parse the raw path string into an array of parts.
		// The CRD schema validation ensures this is between 1-3 elements long.
		matches := r.FindAllStringSubmatch(string(path), -1)
		if len(matches) == 0 {
			return fmt.Errorf("unable to parse `%v`", path)
		}

		parts := make([]string, len(matches))

		for i := 0; i < len(matches); i++ {
			parts[i] = matches[i][0]
		}

		for _, s := range seen {
			// CRD schema validation ensures that no two paths are the same.
			if len(s) == len(parts) {
				continue
			}

			// Different length, and with a common prefix, this is an alias.
			if commonArrayPrefixMatchString(s, parts) {
				return fmt.Errorf("%s aliases with %s", strings.Join(s, "."), path)
			}
		}

		seen = append(seen, parts)
	}

	return nil
}

// checkBucketScopeOrCollectionNamesWithDefaultSameScope checks that two fully qualified data
// sources have the same level e.g. bucket -> bucket, scope -> scope, while bucket -> collection
// is illegal.
func checkBucketScopeOrCollectionNamesWithDefaultSameScope(a, b couchbasev2.BucketScopeOrCollectionNameWithDefaults) error {
	r := regexp.MustCompile(`(?:[^\\.]|\\.)+`)

	matches1 := r.FindAllStringSubmatch(string(a), -1)
	if len(matches1) == 0 {
		return fmt.Errorf("unable to parse `%v`", a)
	}

	matches2 := r.FindAllStringSubmatch(string(b), -1)
	if len(matches2) == 0 {
		return fmt.Errorf("unable to parse `%v`", b)
	}

	if len(matches1) != len(matches2) {
		return fmt.Errorf("%v is not in the same scope as %v", a, b)
	}

	return nil
}

func CheckConstraintsBackup(v *types.Validator, backup *couchbasev2.CouchbaseBackup) error {
	var errs []error

	if checkAnnotationSkipValidation(backup.Annotations) {
		return nil
	}

	if err := validateBackupCronSchedules(backup); err != nil {
		errs = err
	}

	if err := checkConstraintBackupObjStore(v, backup); err != nil {
		errs = append(errs, err)
	}

	if !backup.HasCloudStore() && backup.Spec.EphemeralVolume {
		errs = append(errs, fmt.Errorf("spec.ephemeralVolume is only useable with spec.objectStore.uri or spec.s3Bucket"))
	}

	if backup.Spec.Size.Value() <= 0 {
		errs = append(errs, fmt.Errorf("spec.size %d must be greater than 0", backup.Spec.Size.Value()))
	}

	if backup.Spec.Data != nil {
		if len(backup.Spec.Data.Include) > 0 && len(backup.Spec.Data.Exclude) > 0 {
			errs = append(errs, fmt.Errorf("spec.data.include and spec.data.exclude are mututally exclusive"))
		}

		if len(backup.Spec.Data.Include) > 0 {
			if err := checkBucketScopeOrCollectionNamesWithDefaultsOverlap(backup.Spec.Data.Include); err != nil {
				errs = append(errs, fmt.Errorf("spec.data.include invalid: %w", err))
			}
		}

		if len(backup.Spec.Data.Exclude) > 0 {
			if err := checkBucketScopeOrCollectionNamesWithDefaultsOverlap(backup.Spec.Data.Exclude); err != nil {
				errs = append(errs, fmt.Errorf("spec.data.exclude invalid: %w", err))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checks that if the secret for the object store exists,
// it contains the correct fields for access.
func checkConstraintStoreSecret(v *types.Validator, store *couchbasev2.ObjectStoreSpec, namespace string) error {
	if !v.Options.ValidateSecrets {
		return nil
	}

	// only validate the secret if it exists
	if len(store.Secret) == 0 {
		return nil
	}

	secretName := store.Secret

	secret, found, err := v.Abstraction.GetSecret(namespace, secretName)
	if err != nil {
		return err
	}

	if !found {
		// this should be already checked by the calling function to provide better context for the error.
		return fmt.Errorf("secret %s referenced by objectStore.secret must exist", secretName)
	}

	if store.UseIAM != nil && *store.UseIAM {
		if strings.HasPrefix(string(store.URI), "s3://") {
			if _, ok := secret.Data[cbcluster.StoreSecretRegion]; !ok {
				return fmt.Errorf("object store secret %s must contain key '%s' when using IAM", secretName, cbcluster.StoreSecretRegion)
			}
		}

		return nil
	}

	// these always have to exist unless using IAM.
	if _, ok := secret.Data[cbcluster.StoreSecretAccessID]; !ok {
		return fmt.Errorf("object store secret %s must contain key '%s'", secretName, cbcluster.StoreSecretAccessID)
	}

	if _, ok := secret.Data[cbcluster.StoreSecretAccessKey]; !ok {
		return fmt.Errorf("object store secret %s must contain key '%s'", secretName, cbcluster.StoreSecretAccessKey)
	}

	storeURI := string(store.URI)

	switch {
	case len(storeURI) == 0:
		// no object store no validation
		return nil
	case strings.HasPrefix(storeURI, "gs://"):
		// should contain refresh-token
		if _, ok := secret.Data[cbcluster.StoreSecretRefreshToken]; !ok {
			return fmt.Errorf("object store secret %s must contain key '%s' for Google Cloud buckets", secretName, cbcluster.StoreSecretRefreshToken)
		}
	case strings.HasPrefix(storeURI, "s3://"):
		// should contain region
		if _, ok := secret.Data[cbcluster.StoreSecretRegion]; !ok {
			return fmt.Errorf("object store secret %s must contain key '%s' for AWS S3 buckets", secretName, cbcluster.StoreSecretRegion)
		}
	case strings.HasPrefix(storeURI, "az://"):
		// doesn't need to contain any extra fields.
		return nil
	default:
		// we shouldn't reach this because of crd validation.
		return fmt.Errorf("secret %s is incompatible with unsupported object store %s", secretName, storeURI)
	}

	return nil
}

func checkConstraintBackupObjStore(v *types.Validator, backup *couchbasev2.CouchbaseBackup) error {
	if !v.Options.ValidateSecrets {
		return nil
	}

	if backup.Spec.ObjectStore == nil {
		return nil
	}

	if err := checkConstraintStoreSecret(v, backup.Spec.ObjectStore, backup.Namespace); err != nil {
		return fmt.Errorf("failure validating spec.objectStore %w", err)
	}

	if backup.Spec.ObjectStore.Endpoint != nil {
		secretName := backup.Spec.ObjectStore.Endpoint.CertSecret
		if len(secretName) == 0 {
			return nil
		}

		secret, found, err := v.Abstraction.GetSecret(backup.Namespace, secretName)

		if err != nil {
			return err
		}

		if !found {
			return fmt.Errorf("secret %s referenced by spec.objectStore.endpoint.secret. must exist", secretName)
		}

		if _, ok := secret.Data["tls.crt"]; !ok {
			return fmt.Errorf("custom object endpoint CA secret %s must contain key 'tls.crt'", secretName)
		}
	}

	return nil
}

func checkConstraintBackupRestoreObjStoreSecret(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	if !v.Options.ValidateSecrets {
		return nil
	}

	if restore.Spec.ObjectStore == nil {
		return nil
	}

	// only validate the secret if it exists
	if len(restore.Spec.ObjectStore.Secret) == 0 {
		return nil
	}

	if err := checkConstraintStoreSecret(v, restore.Spec.ObjectStore, restore.Namespace); err != nil {
		return fmt.Errorf("failure validating spec.objectStore %w", err)
	}

	if restore.Spec.ObjectStore.Endpoint != nil {
		secretName := restore.Spec.ObjectStore.Endpoint.CertSecret
		if len(secretName) == 0 {
			return nil
		}

		secret, found, err := v.Abstraction.GetSecret(restore.Namespace, secretName)

		if err != nil {
			return err
		}

		if !found {
			return fmt.Errorf("secret %s referenced by spec.objectStore.endpoint.secret. must exist", secretName)
		}

		if _, ok := secret.Data["tls.crt"]; !ok {
			return fmt.Errorf("custom object endpoint CA secret %s must contain key 'tls.crt'", secretName)
		}
	}

	return nil
}

func CheckConstraintsBackupRestore(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	if checkAnnotationSkipValidation(restore.Annotations) {
		return nil
	}

	checks := []func(*types.Validator, *couchbasev2.CouchbaseBackupRestore) error{
		checkContraintRestoreStart,
		checkContraintRestoreEnd,
		checkContraintRestoreRange,
		checkContraintRestoreData,
		checkConstraintBackupRestoreObjStoreSecret,
	}

	var errs []error

	for _, check := range checks {
		if err := check(v, restore); err != nil {
			var composite *errors.CompositeError

			if ok := goerrors.As(err, &composite); ok {
				errs = append(errs, composite.Errors...)
				continue
			}

			errs = append(errs, err)
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkConstraintBackupObjectEndpointSecret(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !v.Options.ValidateSecrets {
		return nil
	}

	endpoint := cluster.GetBackupStoreEndpoint()
	if endpoint == nil {
		return nil
	}

	if len(endpoint.CertSecret) == 0 {
		return nil
	}

	secretName := cluster.GetBackupStoreEndpoint().CertSecret

	secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("secret %s referenced by spec.backup.objectEndpoint.secret must exist", secretName)
	}

	if _, ok := secret.Data["tls.crt"]; !ok {
		return fmt.Errorf("custom object endpoint secret %s must contain key 'tls.crt'", secretName)
	}

	return nil
}

func checkContraintRestoreStart(_ *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	start := restore.Spec.Start

	if start == nil {
		return nil
	}

	if start.Str != nil && start.Int != nil {
		return fmt.Errorf("specify just one value, either Str or Int")
	}

	return nil
}

func checkContraintRestoreEnd(_ *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	end := restore.Spec.End

	if end == nil {
		return nil
	}

	// both str and int are specified
	if end.Str != nil && end.Int != nil {
		return fmt.Errorf("specify just one value, either Str or Int")
	}

	return nil
}

func checkContraintRestoreRange(_ *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	start := restore.Spec.Start
	end := restore.Spec.End

	if start == nil || end == nil {
		return nil
	}

	// start and end are using integer arguments
	if start.Int != nil && end.Int != nil {
		if *start.Int > *end.Int {
			return fmt.Errorf("start integer cannot be larger than end integer")
		}
	}

	return nil
}

func checkContraintRestoreData(_ *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	var errs []error

	if restore.Spec.Data == nil {
		return nil
	}

	if len(restore.Spec.Data.Include) > 0 && len(restore.Spec.Data.Exclude) > 0 {
		errs = append(errs, fmt.Errorf("spec.data.include and spec.data.exclude are mututally exclusive"))
	}

	if len(restore.Spec.Data.Include) > 0 {
		if err := checkBucketScopeOrCollectionNamesWithDefaultsOverlap(restore.Spec.Data.Include); err != nil {
			errs = append(errs, fmt.Errorf("spec.data.include invalid: %w", err))
		}
	}

	if len(restore.Spec.Data.Exclude) > 0 {
		if err := checkBucketScopeOrCollectionNamesWithDefaultsOverlap(restore.Spec.Data.Exclude); err != nil {
			errs = append(errs, fmt.Errorf("spec.data.exclude invalid: %w", err))
		}
	}

	var mappingSources []couchbasev2.BucketScopeOrCollectionNameWithDefaults

	for _, mapping := range restore.Spec.Data.Map {
		if err := checkBucketScopeOrCollectionNamesWithDefaultSameScope(mapping.Source, mapping.Target); err != nil {
			errs = append(errs, fmt.Errorf("spec.data.map invalid: %w", err))
		}

		mappingSources = append(mappingSources, mapping.Source)
	}

	if err := checkBucketScopeOrCollectionNamesWithDefaultsOverlap(mappingSources); err != nil {
		errs = append(errs, fmt.Errorf("spec.data.map invalid: %w", err))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsCouchbaseGroup(v *types.Validator, group *couchbasev2.CouchbaseGroup) error {
	var errs []error

	if checkAnnotationSkipValidation(group.Annotations) {
		return nil
	}

	for index, role := range group.Spec.Roles {
		// role itself must be valid
		isCluterRole := couchbasev2.IsClusterRole(role.Name)
		// Bucket cannot be used with cluster role
		if role.Bucket != "" && isCluterRole {
			errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.roles[%d].bucket for cluster role", index), "", string(role.Name)))
		}
	}

	if err := checkCouchbaseGroupRBACConstraints(v, group); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// getRootCAs returns the CAs.  When no CAs are returned e.g. nil, and no error, it's assumed
// that this is a race condition to do with resource creation order.
func getRootCAs(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([][]byte, error) {
	var rootCAs [][]byte

	// When in shadowed mode, the root CA may be specified by the the user or cert-manager.
	// When not, then the CA is required.
	if cluster.IsTLSShadowed() {
		secretName := cluster.Spec.Networking.TLS.SecretSource.ServerSecretName

		secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
		if err != nil {
			return nil, err
		}

		if !found {
			return nil, fmt.Errorf("secret %s referenced by spec.networking.tls.secretSource.serverSecretName must exist", secretName)
		}

		if ca, ok := secret.Data[constants.CertManagerCAKey]; ok {
			rootCAs = append(rootCAs, ca)
		}
	} else {
		secretName := cluster.Spec.Networking.TLS.Static.OperatorSecret

		secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
		if err != nil {
			return nil, err
		}

		if !found {
			return nil, fmt.Errorf("secret %s referenced by spec.networking.tls.static.operatorSecret must exist", secretName)
		}

		ca, ok := secret.Data[constants.OperatorSecretCAKey]
		if !ok {
			return nil, fmt.Errorf("tls secret %s must contain %s", secretName, constants.OperatorSecretCAKey)
		}

		rootCAs = append(rootCAs, ca)
	}

	// When using certificate pools, then the CA can come from any of these sources.
	for _, secretName := range cluster.Spec.Networking.TLS.RootCAs {
		secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
		if err != nil {
			return nil, err
		}

		if !found {
			return nil, fmt.Errorf("secret %s referenced by spec.networking.tls.rootCAs must exist", secretName)
		}

		ca, ok := secret.Data[v1.TLSCertKey]
		if !ok {
			return nil, fmt.Errorf("tls secret %s must contain %s", secretName, v1.TLSCertKey)
		}

		rootCAs = append(rootCAs, ca)
	}

	return rootCAs, nil
}

// getServerTLS returns the server key and chain, keystore, whether there was a soft error, or whether there was a hard error.
func getServerTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]byte, []byte, []byte, bool, string, error) {
	var secretPath string

	var secretName string

	var keyKey string

	var chainKey string

	if cluster.IsTLSShadowed() {
		secretPath = "spec.networking.tls.secretSource.serverSecretName"
		secretName = cluster.Spec.Networking.TLS.SecretSource.ServerSecretName
		keyKey = "tls.key"
		chainKey = "tls.crt"
	} else {
		secretPath = "spec.networking.tls.static.serverSecret"
		secretName = cluster.Spec.Networking.TLS.Static.ServerSecret
		keyKey = "pkey.key"
		chainKey = "chain.pem"
	}

	secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return nil, nil, nil, false, "", err
	}

	if !found {
		return nil, nil, nil, false, "", fmt.Errorf("secret %s referenced by %s must exist", secretName, secretPath)
	}

	keystore, passphrase, pkcs12Present, err := util_x509.ExtractPKCS12Info(secret)
	if err != nil {
		return nil, nil, nil, false, "", err
	}

	if pkcs12Present {
		return nil, nil, keystore, true, passphrase, nil
	}

	key, ok := secret.Data[keyKey]
	if !ok {
		return nil, nil, nil, false, "", fmt.Errorf("tls secret %s must contain %s", secretName, keyKey)
	}

	chain, ok := secret.Data[chainKey]
	if !ok {
		return nil, nil, nil, false, "", fmt.Errorf("tls secret %s must contain %s", secretName, chainKey)
	}

	return key, chain, nil, true, "", nil
}

// getClientTLS returns the client key and chain, keystore, whether there was a soft error, or whether there was a hard error.
func getClientTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]byte, []byte, bool, error) {
	var secretPath string

	var secretName string

	var keyKey string

	var chainKey string

	if cluster.IsTLSShadowed() {
		secretPath = "spec.networking.tls.secretSource.clientSecretName"
		secretName = cluster.Spec.Networking.TLS.SecretSource.ClientSecretName
		keyKey = "tls.key"
		chainKey = "tls.crt"
	} else {
		secretPath = "spec.networking.tls.static.operatorSecret"
		secretName = cluster.Spec.Networking.TLS.Static.OperatorSecret
		keyKey = "couchbase-operator.key"
		chainKey = "couchbase-operator.crt"
	}

	secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return nil, nil, false, err
	}

	if !found {
		return nil, nil, false, fmt.Errorf("secret %s referenced by %s must exist", secretName, secretPath)
	}

	key, ok := secret.Data[keyKey]
	if !ok {
		return nil, nil, false, fmt.Errorf("tls secret %s must contain %s", secretName, keyKey)
	}

	chain, ok := secret.Data[chainKey]
	if !ok {
		return nil, nil, false, fmt.Errorf("tls secret %s must contain %s", secretName, chainKey)
	}

	return key, chain, true, nil
}

// validateTLS checks TLS configuration exists and is valid
// * correct secrets exist
// * correct keys exist in the secrets
// * cerificate chain validates with the CA
// * certificates are
//   - in date
//   - have the correct attributes
//
// * leaf certificate has the correct SANs.
func validateTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, subjectAltNames []string) []error {
	if !cluster.IsTLSEnabled() || !v.Options.ValidateSecrets {
		return nil
	}

	// Get the CA for verification.
	rootCAs, err := getRootCAs(v, cluster)
	if err != nil {
		return []error{err}
	}

	// Nothing found, assume this is a recource creation race condition e.g. eventual consistency.
	if rootCAs == nil {
		return nil
	}

	// Check server certificates.
	key, chain, keystore, ok, passphrase, err := getServerTLS(v, cluster)
	if err != nil {
		return []error{err}
	}

	if !ok {
		return nil
	}

	if keystore != nil {
		var decodeErr error
		chain, key, _, decodeErr = util_x509.DecodePKCS12file(keystore, passphrase, cluster)

		if decodeErr != nil {
			return []error{decodeErr}
		}
	}

	if _, err := util_x509.Verify(rootCAs, chain, key, x509.ExtKeyUsageServerAuth, subjectAltNames, !cluster.IsTLSShadowed(), !cluster.Spec.Networking.ImprovedHostNetwork); err != nil {
		return []error{err}
	}

	// Check the client certificates.
	if !cluster.IsMutualTLSEnabled() {
		return nil
	}

	key, chain, ok, err = getClientTLS(v, cluster)
	if err != nil {
		return []error{err}
	}

	if !ok {
		return nil
	}

	if _, err := util_x509.Verify(rootCAs, chain, key, x509.ExtKeyUsageClientAuth, nil, false, !cluster.Spec.Networking.ImprovedHostNetwork); err != nil {
		return []error{err}
	}

	return nil
}

// validateTLSXDCR checks that TLS configuration for a remote cluster is valid.
// * if set the secret must exist
// * if set the secret must contain a CA.
func validateTLSXDCR(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) (errs []error) {
	if !v.Options.ValidateSecrets {
		return nil
	}

	for _, remoteCluster := range cluster.Spec.XDCR.RemoteClusters {
		if remoteCluster.TLS == nil {
			continue
		}

		if remoteCluster.TLS.Secret != nil {
			secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, *remoteCluster.TLS.Secret)
			if err != nil {
				errs = append(errs, err)

				continue
			}

			if !found {
				errs = append(errs, fmt.Errorf("xdcr tls secret %s for remote cluster %s must exist", *remoteCluster.TLS.Secret, remoteCluster.Name))
				continue
			}

			if _, ok := secret.Data[couchbasev2.RemoteClusterTLSCA]; !ok {
				errs = append(errs, fmt.Errorf("xdcr tls secret %s for remote cluster %s must contain key 'ca'", *remoteCluster.TLS.Secret, remoteCluster.Name))
				continue
			}
		}
	}

	return
}

// getClusterBuckets returns all abstract buckets for a cluster as per its scoping rules.
func getClusterBuckets(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]couchbasev2.AbstractBucket, error) {
	// Collect all the buckets referenced by the cluster using the same
	// scoping rules as defined for the cluster.
	buckets := []couchbasev2.AbstractBucket{}

	couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, err
	}

	for i := range couchbaseBuckets.Items {
		err = annotations.Populate(&couchbaseBuckets.Items[i].Spec, couchbaseBuckets.Items[i].Annotations)
		if err != nil {
			return nil, err
		}

		buckets = append(buckets, &couchbaseBuckets.Items[i])
	}

	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, err
	}

	for i := range ephemeralBuckets.Items {
		err = annotations.Populate(&ephemeralBuckets.Items[i].Spec, ephemeralBuckets.Items[i].Annotations)
		if err != nil {
			return nil, err
		}

		buckets = append(buckets, &ephemeralBuckets.Items[i])
	}

	memcachedBuckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, err
	}

	for i := range memcachedBuckets.Items {
		err = annotations.Populate(&memcachedBuckets.Items[i].Spec, memcachedBuckets.Items[i].Annotations)
		if err != nil {
			return nil, err
		}

		buckets = append(buckets, &memcachedBuckets.Items[i])
	}

	return buckets, nil
}

// validateBucketNameConstraints takes a cluster and finds all buckets, or
// a bucket and finds all clusters referencing it, checking that bucket names
// are not reused.
//
//nolint:gocognit
func validateBucketNameConstraints(v *types.Validator, object runtime.Object, cluster *couchbasev2.CouchbaseCluster) error {
	// Gather the clusters affected by this change (either adding a cluster or
	// a bucket -- bucket names are immutable).
	clusters := []*couchbasev2.CouchbaseCluster{}

	var abstractBucket couchbasev2.AbstractBucket

	switch t := object.(type) {
	case *couchbasev2.CouchbaseBucket, *couchbasev2.CouchbaseEphemeralBucket, *couchbasev2.CouchbaseMemcachedBucket:
		bucket, ok := object.(metav1.Object)
		if !ok {
			return fmt.Errorf("failed to type assert bucket to meta object")
		}

		abstractBucket, ok = object.(couchbasev2.AbstractBucket)
		if !ok {
			return fmt.Errorf("failed to type assert bucket to abstract bucket")
		}

		if len(bucket.GetName()) > 100 {
			return fmt.Errorf("bucket name %s exceeds the maximum length of 100 characters", bucket.GetName())
		}

		var namespacedClusters = new(couchbasev2.CouchbaseClusterList)

		if cluster != nil {
			namespacedClusters.Items = []couchbasev2.CouchbaseCluster{*cluster}
		} else {
			allClusters, err := v.Abstraction.GetCouchbaseClusters(bucket.GetNamespace())
			if err != nil {
				return err
			}

			namespacedClusters = allClusters
		}

		for i, cluster := range namespacedClusters.Items {
			clusterBucketSelector, err := metav1.LabelSelectorAsSelector(cluster.Spec.Buckets.Selector)
			if err != nil {
				return err
			}

			if clusterBucketSelector.Matches(labels.Set(bucket.GetLabels())) {
				clusters = append(clusters, &namespacedClusters.Items[i])
			}
		}
	case *couchbasev2.CouchbaseCluster:
		clusters = append(clusters, t)
	default:
		return fmt.Errorf("validate bucket names: unsupported type")
	}

	// Check each cluster affected by this operation...
	for _, cluster := range clusters {
		// Buckets aren't managed, you are on your own!
		if !cluster.Spec.Buckets.Managed {
			continue
		}

		// Collect all the buckets referenced by the cluster using the same
		// scoping rules as defined for the cluster.
		buckets, err := getClusterBuckets(v, cluster)
		if err != nil {
			return err
		}

		// Include the bucket in the name duplicate check if it's a new bucket
		if abstractBucket != nil {
			bucketFound := false

			for _, b := range buckets {
				if b.GetCouchbaseName() == abstractBucket.GetCouchbaseName() && b.GetType() == abstractBucket.GetType() {
					bucketFound = true
					break
				}
			}

			if !bucketFound {
				buckets = append(buckets, abstractBucket)
			}
		}

		// Gather the names in an associative array (a set essentially) and look for duplicates.
		names := map[string]interface{}{}

		for _, bucket := range buckets {
			name := bucket.GetCouchbaseName()

			if _, ok := names[name]; ok {
				return fmt.Errorf("bucket name %s defined multiple times for cluster %s", name, cluster.Name)
			}

			names[name] = nil
		}
	}

	return nil
}

// validateMemoryConstraints works in two different ways:
// * If a cluster is specified we are creating or updating cluster. Look up all buckets selected by it
// and ensure the total memory requirements do not surpass the data service memory quota.
// * If a bucket is specified then a bucket is being created or updated.  Look up all clusters that
// may select the bucket and ensure the total memory requirements do not surpass the data service memory
// quota for each cluster.
func validateMemoryConstraints(v *types.Validator, object runtime.Object, cluster *couchbasev2.CouchbaseCluster) error {
	var namespace string

	var bucket couchbasev2.AbstractBucket

	switch t := object.(type) {
	case *couchbasev2.CouchbaseCluster:
		return validateClusterMemoryConstraints(v, t)
	case *couchbasev2.CouchbaseBucket:
		namespace = t.Namespace
		bucket = t
	case *couchbasev2.CouchbaseEphemeralBucket:
		namespace = t.Namespace
		bucket = t
	case *couchbasev2.CouchbaseMemcachedBucket:
		namespace = t.Namespace
		bucket = t
	default:
		return fmt.Errorf("validate memory constraints: unsupported type")
	}

	var clusters = new(couchbasev2.CouchbaseClusterList)

	if cluster != nil {
		clusters.Items = []couchbasev2.CouchbaseCluster{*cluster}
	} else {
		allClusters, err := v.Abstraction.GetCouchbaseClusters(namespace)
		if err != nil {
			return err
		}

		clusters = allClusters
	}

	for i := range clusters.Items {
		cluster := clusters.Items[i]

		if err := validateClusterMemoryConstraints(v, &cluster, bucket); err != nil {
			return err
		}
	}

	return nil
}

// validateClusterMemoryConstraints given a cluster loads all buckets associated with it and
// validates that the allocated memory does not exceed the memory allocated for the
// data service. If validating against new/edited buckets, the memory quota from these buckets will replace any
// quotas from existing buckets with the same name.
func validateClusterMemoryConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, newBuckets ...couchbasev2.AbstractBucket) error {
	if !cluster.Spec.Buckets.Managed {
		return nil
	}

	// Collect all the buckets referenced by the cluster using the same
	// scoping rules as defined for the cluster.
	buckets, err := getClusterBuckets(v, cluster)
	if err != nil {
		return err
	}

	// Accumulate the per-node bucket memory quota, and reject it if greater than
	// that defined for the data service.
	allocated := resource.NewQuantity(0, resource.BinarySI)

	buckets = util.MergeAbstractBucketLists(newBuckets, buckets)

	sb := false

	for _, bucket := range buckets {
		// If the bucket is a sample bucket, we want to validate with a pre-determined resource quantity of 200Mi
		if bucket.IsSampleBucket() {
			allocated.Add(*k8sutil.NewResourceQuantityMi(int64(200)))

			sb = true
		} else {
			allocated.Add(*bucket.GetMemoryQuota())
		}
	}

	if cluster.Spec.ClusterSettings.DataServiceMemQuota != nil {
		if allocated.Cmp(*cluster.Spec.ClusterSettings.DataServiceMemQuota) > 0 {
			errMsg := fmt.Sprintf("bucket memory allocation (%v) exceeds data service quota (%v) on cluster %s", allocated, cluster.Spec.ClusterSettings.DataServiceMemQuota, cluster.Name)

			if sb {
				errMsg = fmt.Sprintf("%s, sample buckets have a memory quota of 200Mi", errMsg)
			}

			return fmt.Errorf(errMsg)
		}
	}

	return nil
}

// validateBucketExists ensures the specified Couchbase bucket exists.
func validateBucketExists(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, name string) error {
	buckets, err := v.Abstraction.GetBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	for _, bucket := range buckets {
		if bucket.GetCouchbaseName() != name {
			continue
		}

		if bucket.GetType() == couchbasev2.BucketTypeMemcached {
			return fmt.Errorf("memcached bucket %s cannot be replicated", name)
		}

		return nil
	}

	return fmt.Errorf("bucket %s not found", name)
}

// validateBackupCronSchedules ensures that the correct cronjob schedules are valid for the desired backup strategy.
func validateBackupCronSchedules(backup *couchbasev2.CouchbaseBackup) []error {
	var errs []error

	switch backup.Spec.Strategy {
	case couchbasev2.FullIncremental:
		if err := validateCronJobString(backup.Spec.Incremental, "spec.incremental"); err != nil {
			errs = append(errs, err)
		}

		if err := validateCronJobString(backup.Spec.Full, "spec.full"); err != nil {
			errs = append(errs, err)
		}
	case couchbasev2.FullOnly:
		if err := validateCronJobString(backup.Spec.Full, "spec.full"); err != nil {
			errs = append(errs, err)
		}
	case couchbasev2.ImmediateFull:
	case couchbasev2.ImmediateIncremental:
	default:
		errs = append(errs, fmt.Errorf("spec.strategy %s not valid, must be one of %s | %s",
			backup.Spec.Strategy, couchbasev2.FullIncremental, couchbasev2.FullOnly))
	}

	return errs
}

func validateCronJobString(schedule *couchbasev2.CouchbaseBackupSchedule, name string) error {
	if schedule == nil || len(schedule.Schedule) == 0 {
		return fmt.Errorf("%s.schedule : cronjob schedule %s cannot be empty", name, name)
	}

	p := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := p.Parse(schedule.Schedule); err != nil {
		return fmt.Errorf("%s.schedule : %w", name, err)
	}

	return nil
}

// checkMaxTTL is a generic check for document TTL, shared across buckets and collections. Setting allowNegOverride to true will allow "-1s" to be the given duration.
func checkMaxTTL(path string, value *metav1.Duration, allowNegOverride bool) error {
	if value == nil {
		return nil
	}

	timeout := int(value.Duration.Seconds())

	minValue := 0
	if allowNegOverride {
		minValue = -1
	}

	if timeout < minValue {
		return errors.ExceedsMinimumInt(path, "body", int64(minValue), false, nil)
	}

	if timeout > bucketTTLMax {
		return errors.ExceedsMaximumInt(path, "body", bucketTTLMax, false, nil)
	}

	return nil
}

func CheckConstraintsCollection(v *types.Validator, collection *couchbasev2.CouchbaseCollection) error {
	var errs []error

	if checkAnnotationSkipValidation(collection.Annotations) {
		return nil
	}

	if collection.Spec.MaxTTL != nil {
		if err := checkMaxTTL("spec.maxTTL", collection.Spec.MaxTTL, true); err != nil {
			errs = append(errs, err)
		}
	}

	if err := checkAllScopeCollectionsUnique(v, collection.Namespace); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsCollectionGroup(v *types.Validator, collectionGroup *couchbasev2.CouchbaseCollectionGroup) error {
	var errs []error

	if checkAnnotationSkipValidation(collectionGroup.Annotations) {
		return nil
	}

	if collectionGroup.Spec.MaxTTL != nil {
		if err := checkMaxTTL("spec.maxTTL", collectionGroup.Spec.MaxTTL, true); err != nil {
			errs = append(errs, err)
		}
	}

	if err := checkAllScopeCollectionsUnique(v, collectionGroup.Namespace); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsScope(v *types.Validator, scope *couchbasev2.CouchbaseScope) error {
	var errs []error

	if checkAnnotationSkipValidation(scope.Annotations) {
		return nil
	}

	if err := checkScopeCollectionsUnique(v, scope.Namespace, couchbasev2.ScopeCRDResourceKind, scope.Name, scope.Spec.Collections); err != nil {
		errs = append(errs, err)
	}

	if err := checkAllBucketScopesUnique(v, scope.Namespace); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsScopeGroup(v *types.Validator, scopeGroup *couchbasev2.CouchbaseScopeGroup) error {
	var errs []error

	if checkAnnotationSkipValidation(scopeGroup.Annotations) {
		return nil
	}

	if err := checkScopeCollectionsUnique(v, scopeGroup.Namespace, couchbasev2.ScopeGroupCRDResourceKind, scopeGroup.Name, scopeGroup.Spec.Collections); err != nil {
		errs = append(errs, err)
	}

	if err := checkAllBucketScopesUnique(v, scopeGroup.Namespace); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkAllScopeCollectionsUnique is a somewhat heavyweight beast.  When we create a collection
// we check every scope in the namespace in case a scope references the collection, before validating
// each of those scopes individually.
func checkAllScopeCollectionsUnique(v *types.Validator, namespace string) error {
	scopes, err := v.Abstraction.GetCouchbaseScopes(namespace, nil)
	if err != nil {
		return err
	}

	for _, scope := range scopes.Items {
		if err := checkScopeCollectionsUnique(v, namespace, couchbasev2.ScopeCRDResourceKind, scope.Name, scope.Spec.Collections); err != nil {
			return err
		}
	}

	scopeGroups, err := v.Abstraction.GetCouchbaseScopeGroups(namespace, nil)
	if err != nil {
		return err
	}

	for _, scopeGroup := range scopeGroups.Items {
		if err := checkScopeCollectionsUnique(v, namespace, couchbasev2.ScopeGroupCRDResourceKind, scopeGroup.Name, scopeGroup.Spec.Collections); err != nil {
			return err
		}
	}

	return nil
}

// nameFirstDefinition provides tracking of what resource first defines a name in the event
// of a collision.
type nameFirstDefinition struct {
	// kind is the resource type.
	kind string

	// name is the resource name.
	name string
}

// nameFirstDefinitionMap provides tracking of what resource first defines a name in the event
// of a collision.  The map key is the couchbase name.
type nameFirstDefinitionMap map[string]nameFirstDefinition

// checkScopeCollectionsUnique accepts a collection selector from either a scope or scope group
// and runs through every referenced collection and checks for duplicate names.
func checkScopeCollectionsUnique(v *types.Validator, namespace, kind, resourceName string, selector *couchbasev2.CollectionSelector) error {
	if selector == nil {
		return nil
	}

	names := nameFirstDefinitionMap{}

	if err := checkScopeCollectionsUniqueExplicit(v, namespace, kind, resourceName, selector, names); err != nil {
		return err
	}

	return checkScopeCollectionsUniqueImplicit(v, namespace, kind, resourceName, selector, names)
}

// checkScopeCollectionsUniqueExplicit checks collections included in a scope by reference have
// unique names.
func checkScopeCollectionsUniqueExplicit(v *types.Validator, namespace, kind, resourceName string, selector *couchbasev2.CollectionSelector, names nameFirstDefinitionMap) error {
	for _, resource := range selector.Resources {
		switch resource.Kind {
		case couchbasev2.CollectionCRDResourceKind:
			collection, found, err := v.Abstraction.GetCouchbaseCollection(namespace, resource.StrName())
			if err != nil {
				return err
			}

			if !found {
				break
			}

			if first, ok := names[collection.CouchbaseName()]; ok {
				return fmt.Errorf("couchbase collection name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", collection.CouchbaseName(), kind, resourceName, couchbasev2.CollectionCRDResourceKind, collection.Name, first.kind, first.name)
			}

			names[collection.CouchbaseName()] = nameFirstDefinition{
				kind: couchbasev2.CollectionCRDResourceKind,
				name: collection.Name,
			}
		case couchbasev2.CollectionGroupCRDResourceKind:
			collectionGroup, found, err := v.Abstraction.GetCouchbaseCollectionGroup(namespace, resource.StrName())
			if err != nil {
				return err
			}

			if !found {
				break
			}

			for _, name := range collectionGroup.Spec.Names {
				if first, ok := names[string(name)]; ok {
					return fmt.Errorf("couchbase collection name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", name, kind, resourceName, couchbasev2.CollectionGroupCRDResourceKind, collectionGroup.Name, first.kind, first.name)
				}

				names[string(name)] = nameFirstDefinition{
					kind: couchbasev2.CollectionGroupCRDResourceKind,
					name: collectionGroup.Name,
				}
			}
		}
	}

	return nil
}

// checkScopeCollectionsUniqueImplicit checks collections included in a scope by label selector have
// unique names.
func checkScopeCollectionsUniqueImplicit(v *types.Validator, namespace, kind, resourceName string, selector *couchbasev2.CollectionSelector, names nameFirstDefinitionMap) error {
	if selector.Selector == nil {
		return nil
	}

	collections, err := v.Abstraction.GetCouchbaseCollections(namespace, selector.Selector)
	if err != nil {
		return err
	}

	for _, collection := range collections.Items {
		if first, ok := names[collection.CouchbaseName()]; ok {
			return fmt.Errorf("couchbase collection name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", collection.CouchbaseName(), kind, resourceName, couchbasev2.CollectionCRDResourceKind, collection.Name, first.kind, first.name)
		}

		names[collection.CouchbaseName()] = nameFirstDefinition{
			kind: couchbasev2.CollectionCRDResourceKind,
			name: collection.Name,
		}
	}

	collectionGroups, err := v.Abstraction.GetCouchbaseCollectionGroups(namespace, selector.Selector)
	if err != nil {
		return err
	}

	for _, collectionGroup := range collectionGroups.Items {
		for _, name := range collectionGroup.Spec.Names {
			if first, ok := names[string(name)]; ok {
				return fmt.Errorf("couchbase collection name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", name, kind, resourceName, couchbasev2.CollectionGroupCRDResourceKind, collectionGroup.Name, first.kind, first.name)
			}

			names[string(name)] = nameFirstDefinition{
				kind: couchbasev2.CollectionGroupCRDResourceKind,
				name: collectionGroup.Name,
			}
		}
	}

	return nil
}

// checkAllBucketScopesUnique is a somewhat heavyweight beast.  When we create a scope
// we check every bucket in the namespace in case a bucket references the scope, before validating
// each of those buckets individually.
func checkAllBucketScopesUnique(v *types.Validator, namespace string) error {
	buckets, err := v.Abstraction.GetCouchbaseBuckets(namespace, nil)
	if err != nil {
		return err
	}

	for _, bucket := range buckets.Items {
		if err := checkBucketScopesUnique(v, namespace, couchbasev2.BucketCRDResourceKind, bucket.Name, bucket.Spec.Scopes); err != nil {
			return err
		}
	}

	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(namespace, nil)
	if err != nil {
		return err
	}

	for _, ephemeralBucket := range ephemeralBuckets.Items {
		if err := checkBucketScopesUnique(v, namespace, couchbasev2.EphemeralBucketCRDResourceKind, ephemeralBucket.Name, ephemeralBucket.Spec.Scopes); err != nil {
			return err
		}
	}

	return nil
}

// checkBucketScopesUnique accepts a scope selector from either a bucket or ephemeral bucket
// and runs through every referenced scope and checks for duplicate names.
func checkBucketScopesUnique(v *types.Validator, namespace, kind, resourceName string, selector *couchbasev2.ScopeSelector) error {
	if selector == nil {
		return nil
	}

	names := nameFirstDefinitionMap{}

	if err := checkBucketScopesUniqueExplicit(v, namespace, kind, resourceName, selector, names); err != nil {
		return err
	}

	return checkBucketScopesUniqueImplicit(v, namespace, kind, resourceName, selector, names)
}

// checkBucketScopesUniqueExplicit checks scopes included in a bucket by reference have
// unique names.
func checkBucketScopesUniqueExplicit(v *types.Validator, namespace, kind, resourceName string, selector *couchbasev2.ScopeSelector, names nameFirstDefinitionMap) error {
	for _, resource := range selector.Resources {
		switch resource.Kind {
		case couchbasev2.ScopeCRDResourceKind:
			scope, found, err := v.Abstraction.GetCouchbaseScope(namespace, resource.StrName())
			if err != nil {
				return err
			}

			if !found {
				break
			}

			if first, ok := names[scope.CouchbaseName()]; ok {
				return fmt.Errorf("couchbase scope name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", scope.CouchbaseName(), kind, resourceName, couchbasev2.ScopeCRDResourceKind, scope.Name, first.kind, first.name)
			}

			names[scope.CouchbaseName()] = nameFirstDefinition{
				kind: couchbasev2.ScopeCRDResourceKind,
				name: scope.Name,
			}
		case couchbasev2.ScopeGroupCRDResourceKind:
			scopeGroup, found, err := v.Abstraction.GetCouchbaseScopeGroup(namespace, resource.StrName())
			if err != nil {
				return err
			}

			if !found {
				break
			}

			for _, name := range scopeGroup.Spec.Names {
				if first, ok := names[string(name)]; ok {
					return fmt.Errorf("couchbase scope name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", name, kind, resourceName, couchbasev2.ScopeGroupCRDResourceKind, scopeGroup.Name, first.kind, first.name)
				}

				names[string(name)] = nameFirstDefinition{
					kind: couchbasev2.ScopeGroupCRDResourceKind,
					name: scopeGroup.Name,
				}
			}
		}
	}

	return nil
}

// checkBucketScopesUniqueImplicit checks scopes included in a bucket by label selector have
// unique names.
func checkBucketScopesUniqueImplicit(v *types.Validator, namespace, kind, resourceName string, selector *couchbasev2.ScopeSelector, names nameFirstDefinitionMap) error {
	if selector.Selector == nil {
		return nil
	}

	scopes, err := v.Abstraction.GetCouchbaseScopes(namespace, selector.Selector)
	if err != nil {
		return err
	}

	for _, scope := range scopes.Items {
		if first, ok := names[scope.CouchbaseName()]; ok {
			return fmt.Errorf("couchbase scope name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", scope.CouchbaseName(), kind, resourceName, couchbasev2.ScopeCRDResourceKind, scope.Name, first.kind, first.name)
		}

		names[scope.CouchbaseName()] = nameFirstDefinition{
			kind: couchbasev2.ScopeCRDResourceKind,
			name: scope.Name,
		}
	}

	scopeGroups, err := v.Abstraction.GetCouchbaseScopeGroups(namespace, selector.Selector)
	if err != nil {
		return err
	}

	for _, scopeGroup := range scopeGroups.Items {
		for _, name := range scopeGroup.Spec.Names {
			if first, ok := names[string(name)]; ok {
				return fmt.Errorf("couchbase scope name `%v` in %s/%s redefined by %s/%s, first seen in %s/%s", name, kind, resourceName, couchbasev2.ScopeGroupCRDResourceKind, scopeGroup.Name, first.kind, first.name)
			}

			names[string(name)] = nameFirstDefinition{
				kind: couchbasev2.ScopeGroupCRDResourceKind,
				name: scopeGroup.Name,
			}
		}
	}

	return nil
}

// CheckImmutableFields checks whether the user is trying to change something
// that cannot be changed.  Think long and hard about adding stuff here... is it
// technically impossible to be updated?  Are you just being lazy?  History
// dictates that anything in here will soon not be because users will change it
// it won't work and they will complain at you.
func CheckImmutableFields(current, updated *couchbasev2.CouchbaseCluster) error {
	if checkAnnotationSkipValidation(updated.GetAnnotations()) {
		return nil
	}

	checks := []func(*couchbasev2.CouchbaseCluster, *couchbasev2.CouchbaseCluster) error{
		checkImmutableServerClass,
		checkImmutableIndexStorage,
		checkImmutableImage,
		checkImmutableVolumeTemplateSize,
	}

	var errs []error

	for _, check := range checks {
		if err := check(current, updated); err != nil {
			var composite *errors.CompositeError

			if ok := goerrors.As(err, &composite); ok {
				errs = append(errs, composite.Errors...)
				continue
			}

			errs = append(errs, err)
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkImmuatableServerClass checks that you aren't modifying that Couchbase cannot
// change about a pod, e.g. the set of services is it running.  This is set at pod
// creation time and cannot be updated.
func checkImmutableServerClass(current, updated *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for _, cur := range current.Spec.Servers {
		before76 := true

		tag, err := k8sutil.CouchbaseVersion(current.Spec.ServerClassCouchbaseImage(&cur))
		if err != nil {
			return err
		}

		if after76, err := couchbaseutil.VersionAfter(tag, "7.6.0"); after76 && err == nil {
			before76 = false
		}

		for i, up := range updated.Spec.Servers {
			if cur.Name == up.Name {
				if !util.StringArrayCompare(couchbasev2.ServiceList(cur.Services).StringSlice(), couchbasev2.ServiceList(up.Services).StringSlice()) {
					errs = append(errs, util.NewUpdateError(fmt.Sprintf("spec.servers[%d].services", i), "body"))
					continue
				}
			}

			if before76 {
				serviceList := couchbasev2.ServiceList(up.Services)
				if serviceList.Contains(couchbasev2.AdminService) || serviceList.Len() == 0 {
					errs = append(errs, fmt.Errorf("cannot enable admin service until cluster is upgraded to 7.6.x+"))
				}
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkImmutableIndexStorage checks you aren't trying to update the index storage
// setting when the index service is running.  This is again a limitation of Couchbase
// and is set on pod creation.  You can just balance out all your index pods, then
// change the setting and add them back again, which requires a maintenance window.
func checkImmutableIndexStorage(current, updated *couchbasev2.CouchbaseCluster) error {
	// Index storage modes are updated after the topology changes, therefore
	// we only care if the updated cluster has any index services running.
	if !updated.IsIndexerEnabled() {
		return nil
	}

	prev := current.Spec.ClusterSettings.IndexStorageSetting

	if current.Spec.ClusterSettings.Indexer != nil {
		prev = current.Spec.ClusterSettings.Indexer.StorageMode
	}

	curr := updated.Spec.ClusterSettings.IndexStorageSetting

	if updated.Spec.ClusterSettings.Indexer != nil {
		curr = updated.Spec.ClusterSettings.Indexer.StorageMode
	}

	if prev != curr {
		return fmt.Errorf("spec.cluster.indexStorageSetting/spec.cluster.indexer.storageMode in body cannot be modified if there are any nodes in the cluster running the index service")
	}

	return nil
}

// checkImmutableImage checks whether the image is immutable.  This is conditional,
// because we want to allow upgrades, while stopping ones that may cause problems.
// Couchbase cannot be downgraded (because network protocols may have changed).
// Upgrade is only allowed (aka tested) between N.x.x and N+1.x.x.  Finally we allow
// a special type of downgrade -- rollback -- but only to the original version.
func checkImmutableImage(current, updated *couchbasev2.CouchbaseCluster) error {
	if current.Spec.Image == updated.Spec.Image {
		return nil
	}

	updatedVersion, err := k8sutil.CouchbaseVersion(updated.Spec.Image)
	if err != nil {
		return err
	}

	upgradeCondition := current.Status.GetCondition(couchbasev2.ClusterConditionUpgrading)

	migratingCondition := current.Status.GetCondition(couchbasev2.ClusterConditionMigrating)

	fullyUpgraded, err := isFullyUpgraded(current)

	if err != nil {
		return err
	}

	// Condition is not set, therefore we are starting an upgrade.
	if (upgradeCondition == nil && migratingCondition == nil) && fullyUpgraded {
		if updatedVersion == "9.9.9" {
			// we have no idea what this is so we trust the user
			return nil
		}

		return checkClusterVersionUpgradePath(current, updated)
	}

	// Modification during upgrade, only allow rollback.
	if updatedVersion != current.Status.CurrentVersion {
		return util.NewUpdateError("spec.version", "body")
	}

	return nil
}

func isFullyUpgraded(c *couchbasev2.CouchbaseCluster) (bool, error) {
	imageVersion, err := k8sutil.CouchbaseVersion(c.Spec.Image)
	if err != nil {
		return false, err
	}

	return imageVersion == c.Status.CurrentVersion, nil
}

// checkImmutableVolumeTemplateSize checks that you aren't downscaling volumes
// as this is not allowed by the underlying platform.
func checkImmutableVolumeTemplateSize(current, updated *couchbasev2.CouchbaseCluster) error {
	if !updated.Spec.EnableOnlineVolumeExpansion {
		return nil
	}

	var errs []error

	for _, updatedClaimTemplate := range updated.Spec.VolumeClaimTemplates {
		currentClaimTemplate := current.Spec.GetVolumeClaimTemplate(updatedClaimTemplate.ObjectMeta.Name)
		if currentClaimTemplate == nil {
			// Claim is being added
			continue
		}

		// Compare storage requests
		currentQuantity, ok := currentClaimTemplate.Spec.Resources.Requests[v1.ResourceStorage]
		if !ok {
			continue
		}

		updatedQuantity, ok := updatedClaimTemplate.Spec.Resources.Requests[v1.ResourceStorage]
		if !ok {
			continue
		}

		if updatedQuantity.Cmp(currentQuantity) == -1 {
			errs = append(errs, fmt.Errorf("spec.volumeClaimTemplates.resources.requests.storage in body can not be less than previous value %s", currentQuantity.String()))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsBucket(prev, curr *couchbasev2.CouchbaseBucket) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckChangeConstraintsCluster(v *types.Validator, prev, curr *couchbasev2.CouchbaseCluster) error {
	err := annotations.Populate(&prev.Spec, prev.Annotations)
	if err != nil {
		return err
	}

	err = annotations.Populate(&curr.Spec, curr.Annotations)
	if err != nil {
		return err
	}

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	if err := checkConstraintPerServiceClassPDB(v, curr); err != nil {
		return err
	}

	// We check here if the change in default storage backend would result in a bucket backend
	// change for a bucket in a CB cluster < 7.6.0. The operator shouldn't actually attempt the
	// migration but we still want to keep the state of CB K8s resources and CB Server in sync
	// where we can.
	// This is a heavy check which is why we gate it behind if the default storage backend
	// actually changes
	if curr.GetDefaultBucketStorageBackend() != prev.GetDefaultBucketStorageBackend() {
		tag, err := k8sutil.CouchbaseVersion(curr.Spec.Image)
		if err != nil {
			return err
		}

		// If it's after 7.6.0 then backend can be changed so we don't need to check further
		if after76, err := couchbaseutil.VersionAfter(tag, "7.6.0"); after76 && err == nil {
			return nil
		}

		buckets, err := v.Abstraction.GetCouchbaseBuckets(curr.Namespace, curr.Spec.Buckets.Selector)

		if err != nil {
			return err
		}

		prevSelector := labels.Everything()
		if prev.Spec.Buckets.Selector != nil {
			prevSelector, err = metav1.LabelSelectorAsSelector(prev.Spec.Buckets.Selector)
			if err != nil {
				return nil
			}
		}

		var errs []error

		for _, bucket := range buckets.Items {
			if !prevSelector.Matches(labels.Set(bucket.Labels)) {
				continue
			}

			if bucket.GetStorageBackend(prev) != bucket.GetStorageBackend(curr) {
				errs = append(errs, fmt.Errorf("cannot change default bucket backend as %s backend would change, backend changes are only supported for server version >= 7.6.0", bucket.Name))
			}
		}

		if errs != nil {
			return errors.CompositeValidationError(errs...)
		}
	}

	// If the cluster is in hibernation, we should prohibit any changes
	if err := checkChangeConstraintsHibernate(prev, curr); err != nil {
		return err
	}

	if err := checkChangeConstraintsMigration(prev, curr); err != nil {
		return err
	}

	if err := checkChangeConstraintsBucketMigratingAnnotation(prev, curr); err != nil {
		return err
	}

	return nil
}

func checkClusterVersionUpgradePath(prev, curr *couchbasev2.CouchbaseCluster) error {
	oldImage, err := prev.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	newImage, err := curr.Spec.HighestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	oldVersion, err := k8sutil.CouchbaseVersion(oldImage)
	if err != nil {
		return err
	}

	if oldVersion == "9.9.9" && prev.Status.CurrentVersion != "" {
		// since we aren't upgrading the status should be what is actually running.
		oldVersion = prev.Status.CurrentVersion
	}

	newVersion, err := couchbaseutil.CouchbaseImageVersion(newImage)
	if err != nil {
		return err
	}

	versionBefore72, err := couchbaseutil.VersionBefore(oldVersion, "7.2.0")
	if err != nil {
		return err
	}

	versionAfter72, err := couchbaseutil.VersionAfter(newVersion, "7.2.0")
	if err != nil {
		return err
	}

	if *curr.Spec.UpgradeProcess == couchbasev2.InPlaceUpgrade && versionBefore72 && versionAfter72 {
		return fmt.Errorf("in-place upgrades not supported from pre-7.2.0 to versions 7.2.0 and above")
	}

	return couchbaseutil.CheckUpgradePath(oldVersion, newVersion)
}

func CheckChangeConstraintsBucket(v *types.Validator, prev, curr *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	err := annotations.Populate(&prev.Spec, prev.Annotations)
	if err != nil {
		return err
	}

	err = annotations.Populate(&curr.Spec, curr.Annotations)
	if err != nil {
		return err
	}

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	storageBackendEmptyOrCouchstore := func(prevStorageBackend, currStorageBackend couchbasev2.CouchbaseStorageBackend) bool {
		return prevStorageBackend == "" && currStorageBackend == "couchstore" || prevStorageBackend == "couchstore" && currStorageBackend == ""
	}

	// We want to validate whether a bucket can be migrated to a new storage backend if the storage backend has been explicitly changed on the CRD,
	// or it's changed from being a sample bucket and has magma in the CRD, in which case a migration will also take place.
	if (prev.Spec.StorageBackend != curr.Spec.StorageBackend && !storageBackendEmptyOrCouchstore(prev.Spec.StorageBackend, curr.Spec.StorageBackend)) || (prev.Spec.SampleBucket && !curr.Spec.SampleBucket && curr.Spec.StorageBackend == "magma") {
		if err := checkAllBucketsClustersValidForMigration(v, curr, cluster); err != nil {
			errs = append(errs, err)
		}

		if prev.Spec.StorageBackend == "magma" && curr.Spec.StorageBackend == "couchstore" {
			if err := CheckBucketHistoryDisabled(prev); err != nil {
				errs = append(errs, fmt.Errorf("spec.storageBackend backend can only be changed from magma to couchstore if history retention is first disabled on the bucket: %w", err))
			}
		}
	}

	if curr.Spec.StorageBackend == "magma" {
		if curr.Spec.EnableIndexReplica {
			errs = append(errs, fmt.Errorf("cannot set spec.enableIndexReplica to true for magma buckets"))
		}
	}

	if !prev.Spec.SampleBucket && curr.Spec.SampleBucket {
		errs = append(errs, fmt.Errorf("cao.couchbase.com/sampleBucket annotation cannot be added to an existing bucket"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckBucketHistoryDisabled(bucket *couchbasev2.CouchbaseBucket) error {
	// History retention labels have been deprecated, bucket.Spec.HistoryRetentionSettings resource object should be used instead
	if bucket.Spec.HistoryRetentionSettings != nil && bucket.Spec.HistoryRetentionSettings.CollectionDefault != nil {
		if *bucket.Spec.HistoryRetentionSettings.CollectionDefault {
			return fmt.Errorf("bucket doesn't have spec.historyRetention.collectionHistoryDefault set to false")
		} else {
			return nil
		}
	}

	if bucket.Annotations == nil {
		return fmt.Errorf("bucket doesn't have cao.couchbase.com/historyRetention.collectionHistoryDefault annotation set to false")
	}

	if val, ok := bucket.Annotations["cao.couchbase.com/historyRetention.collectionHistoryDefault"]; ok {
		if historyRetentionEnabled, err := strconv.ParseBool(val); err != nil {
			return err
		} else if historyRetentionEnabled {
			return fmt.Errorf("bucket doesn't have cao.couchbase.com/historyRetention.collectionHistoryDefault annotation set to false")
		}
	} else {
		return fmt.Errorf("bucket doesn't have cao.couchbase.com/historyRetention.collectionHistoryDefault annotation set to false")
	}

	return nil
}

//nolint:gocognit
func checkAllBucketsClustersValidForMigration(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	var clusters = new(couchbasev2.CouchbaseClusterList)

	if cluster != nil {
		clusters.Items = []couchbasev2.CouchbaseCluster{*cluster}
	} else {
		allClusters, err := v.Abstraction.GetCouchbaseClusters(bucket.Namespace)
		if err != nil {
			return err
		}

		clusters = allClusters
	}

	for _, cluster := range clusters.Items {
		clusterBucketSelector, err := metav1.LabelSelectorAsSelector(cluster.Spec.Buckets.Selector)
		if err != nil {
			return err
		}

		if cluster.Spec.Buckets.Selector == nil || clusterBucketSelector.Matches(labels.Set(bucket.Labels)) {
			if err := annotations.Populate(&cluster.Spec, cluster.Annotations); err != nil {
				return err
			}

			if !cluster.Spec.Buckets.EnableBucketMigrationRoutines {
				return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters have the enableBucketMigrationRoutines annotation set to true")
			}

			upgradeCondition := cluster.Status.GetCondition(couchbasev2.ClusterConditionUpgrading)

			if upgradeCondition != nil && upgradeCondition.Status == v1.ConditionTrue {
				return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters are not in an upgrade")
			}

			tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
			if err != nil {
				return err
			}

			after76, err := couchbaseutil.VersionAfter(tag, "7.6.0")

			if err != nil {
				return err
			}

			if cluster.Status.CurrentVersion == "" {
				return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters have current version 7.6.0 or greater")
			}

			currentVersionAfter76, err := couchbaseutil.VersionAfter(cluster.Status.CurrentVersion, "7.6.0")

			if err != nil {
				return err
			}

			if !after76 || !currentVersionAfter76 {
				return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters are version 7.6.0 or greater")
			}
		}
	}

	return nil
}

func CheckImmutableFieldsEphemeralBucket(prev, curr *couchbasev2.CouchbaseEphemeralBucket) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsMemcachedBucket(_, _ *couchbasev2.CouchbaseMemcachedBucket) error {
	return nil
}

func CheckImmutableFieldsReplication(prev, curr *couchbasev2.CouchbaseReplication) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	if prev.Spec.Bucket != curr.Spec.Bucket {
		errs = append(errs, util.NewUpdateError("spec.bucket", "body"))
	}

	if prev.Spec.RemoteBucket != curr.Spec.RemoteBucket {
		errs = append(errs, util.NewUpdateError("spec.remoteBucket", "body"))
	}

	if prev.Spec.FilterExpression != curr.Spec.FilterExpression {
		errs = append(errs, util.NewUpdateError("spec.filterExpression", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsBackup(prev, curr *couchbasev2.CouchbaseBackup) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	if prev.Spec.Strategy != curr.Spec.Strategy {
		errs = append(errs, util.NewUpdateError("spec.strategy", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsAutoscaler(prev, curr *couchbasev2.CouchbaseAutoscaler) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	// Referenced server group cannot be changed
	if prev.Spec.Servers != curr.Spec.Servers {
		errs = append(errs, util.NewUpdateError("spec.servers", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsCollectionGroup(prev, curr *couchbasev2.CouchbaseCollectionGroup) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	prevTTL := &metav1.Duration{}

	if prev.Spec.MaxTTL != nil {
		prevTTL = prev.Spec.MaxTTL
	}

	currTTL := &metav1.Duration{}

	if curr.Spec.MaxTTL != nil {
		currTTL = curr.Spec.MaxTTL
	}

	if prevTTL.Duration != currTTL.Duration {
		errs = append(errs, util.NewUpdateError("spec.maxTTL", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkClusterConstraintMagmaStorageBackend(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Buckets aren't managed by Cluster.
	if !cluster.Spec.Buckets.Managed {
		return nil
	}

	couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	var hasMagma bool

	// // storageBackend is only allowed above CB version 7.0.0.
	storageBackendSupported, err := cluster.IsAtLeastVersion("7.0.0")
	if err != nil {
		return err
	}

	// // magma storageBackend is only allowed above CB version 7.1.0.
	magmaStorageBackendSupported, err := cluster.IsAtLeastVersion("7.1.0")
	if err != nil {
		return err
	}

	if !magmaStorageBackendSupported {
		for _, cbBucket := range couchbaseBuckets.Items {
			if cbBucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackendMagma {
				return fmt.Errorf("magma storage backend requires Couchbase Server version 7.1.0 or later")
			}
		}
	}

	// find if any bucket has storage backend as "magma"
	for _, cbBucket := range couchbaseBuckets.Items {
		backend := k8sutil.GetBucketStorageBackend(&cbBucket, storageBackendSupported, magmaStorageBackendSupported, cluster)
		if backend == couchbasev2.CouchbaseStorageBackendMagma {
			hasMagma = true
			break
		}
	}

	if !hasMagma {
		return nil
	}

	return checkBucketConstraintMagmaStorageBackend(cluster)
}

func validateCloudNativeGatewayServerTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	serverSecretName := cluster.Spec.Networking.CloudNativeGateway.TLS.ServerSecretName

	secret, found, err := v.Abstraction.GetSecret(cluster.Namespace, serverSecretName)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("secret %s referenced by spec.networking.cloudNativeGateway.tls.serverSecretName must exist", serverSecretName)
	}

	_, ok := secret.Data[v1.TLSPrivateKeyKey]
	if !ok {
		return fmt.Errorf("tls secret %s must contain key %s", secret, v1.TLSPrivateKeyKey)
	}

	certData, ok := secret.Data[v1.TLSCertKey]
	if !ok {
		return fmt.Errorf("tls secret %s must contain cert %s", secret, v1.TLSCertKey)
	}

	cert, err := util_x509.ParseCertificate(certData)
	if err != nil {
		return fmt.Errorf("unable to parse server cert: %w", err)
	}

	if len(cert.DNSNames) == 0 {
		return fmt.Errorf("must provide the DNS name in Subject Alternate Name values in server cert needed for K8s Ingress or OC Routes")
	}

	return nil
}

// checkConstraintK8sSecurityContext validates the different combination of K8s SecurityContext options (pods and containers)
// being applied.
func checkConstraintK8sSecurityContext(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Security.PodSecurityContext != nil && cluster.Spec.SecurityContext != nil {
		if !reflect.DeepEqual(cluster.Spec.Security.PodSecurityContext, cluster.Spec.SecurityContext) {
			return fmt.Errorf("spec.Security.PodSecurityContext must be equal to spec.SecurityContext, if both present")
		}
	}

	return nil
}

// checkForClusterChangesDuringHibernation validates whether there have been any changes to a cluster while hibernate is enabled.
func checkChangeConstraintsHibernate(current, updated *couchbasev2.CouchbaseCluster) error {
	if current.Spec.Hibernate && updated.Spec.Hibernate && current.HasCondition(couchbasev2.ClusterConditionHibernating) {
		current.Spec.Hibernate = updated.Spec.Hibernate
		if !reflect.DeepEqual(updated.Spec, current.Spec) {
			return fmt.Errorf("cluster spec cannot be changed during hibernation")
		}
	}

	return nil
}

func checkAnnotationSkipValidation(annotations map[string]string) bool {
	value, found := annotations[constants.AnnotationDisableAdmissionController]
	if found {
		return strings.EqualFold(value, "true")
	}

	return false
}

func checkChangeConstraintsMigration(current, updated *couchbasev2.CouchbaseCluster) error {
	if current.Spec.Migration == nil && updated.Spec.Migration != nil {
		return fmt.Errorf("spec.migration cannot be added to a pre-existing cluster")
	}

	if current.Spec.Migration != nil {
		if updated.Spec.Migration != nil {
			if vc, err := checkForVersionChange(current, updated); err == nil && vc {
				return fmt.Errorf("couchbase version cannot be changed while in migration mode")
			} else if err != nil {
				return err
			}
		}

		if current.IsMigrating() {
			if updated.Spec.Migration == nil {
				return fmt.Errorf("spec.migration cannot be removed during migration")
			}

			if current.Spec.Migration.UnmanagedClusterHost != updated.Spec.Migration.UnmanagedClusterHost {
				return fmt.Errorf("spec.migration.unmanagedClusterHost cannot be changed during migration")
			}
		}
	}

	return nil
}

// Sample buckets have a number of defaults that are also immutable fields. We should check the bucket CRD
// either has these same defaults or omits them in order to avoid operator continuously trying to update the bucket.
func checkSampleBucketFieldPresets(resolution couchbasev2.CouchbaseBucketConflictResolution, enableIndexReplica bool) error {
	if resolution != couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber {
		return fmt.Errorf("spec.conflictResolution must be set to seqno for sample buckets")
	}

	if enableIndexReplica {
		return fmt.Errorf("spec.enableIndexReplica must be set to false for sample buckets")
	}

	return nil
}

func checkForVersionChange(current, updated *couchbasev2.CouchbaseCluster) (bool, error) {
	currentImage, err := current.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return false, err
	}

	updatedImage, err := updated.Spec.HighestInUseCouchbaseVersionImage()
	if err != nil {
		return false, err
	}

	currentVersion, err := k8sutil.CouchbaseVersion(currentImage)
	if err != nil {
		return false, err
	}

	updatedVersion, err := k8sutil.CouchbaseVersion(updatedImage)
	if err != nil {
		return false, err
	}

	return currentVersion != updatedVersion, nil
}

// checkBucketClustersMagmaStorageBackend checks all clusters that will use a given bucket for magma storage backend constraints.
func checkBucketClustersMagmaStorageBackend(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) error {
	clusters, err := v.Abstraction.GetCouchbaseClusters(bucket.Namespace)
	if err != nil {
		return err
	}

	for _, c := range clusters.Items {
		if !c.Spec.Buckets.Managed {
			return nil
		}

		clusterBucketSelector, err := metav1.LabelSelectorAsSelector(c.Spec.Buckets.Selector)
		if err != nil {
			return err
		}

		if c.Spec.Buckets.Selector == nil || clusterBucketSelector.Matches(labels.Set(bucket.Labels)) {
			if err := checkBucketConstraintMagmaStorageBackend(&c); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkConstraintBucketMagmaStorageBackend checks a cluster is able to support the magma storage backend.
func checkBucketConstraintMagmaStorageBackend(cluster *couchbasev2.CouchbaseCluster) error {
	lowestImageVer, err := cluster.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	srvImgTag, err := k8sutil.CouchbaseVersion(lowestImageVer)
	if err != nil {
		return err
	}

	srvVerAfter712, err := couchbaseutil.VersionAfter(srvImgTag, "7.1.2")
	if err != nil {
		return err
	}

	// We shouldn't allow FTS, Eventing or Analytics services on magma buckets when the cb version is < 7.1.2
	for _, config := range cluster.Spec.Servers {
		services := couchbasev2.ServiceList(config.Services)
		if !srvVerAfter712 && (services.Contains(couchbasev2.EventingService) || services.Contains(couchbasev2.AnalyticsService) || services.Contains(couchbasev2.SearchService)) {
			return fmt.Errorf("search, eventing or analytics services cannot be used with magma buckets below CB Server 7.1.2. One or more of those services has been used as server class: %v, for cluster: %s", services.StringSlice(), cluster.NamespacedName())
		}
	}

	return nil
}

// checkClusterRBACConstraints checks RBAC settings constraints for a cluster. Every CouchbaseGroup which qualifies for the RBAC selector for the cluster will be checked.
func checkClusterRBACConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	return checkClusterGroupRBACConstraints(v, cluster, nil)
}

// checkCouchbaseGroupRBACConstraints checks RBAC settings constraints, such as supported cluster versions, for a CouchbaseGroup. Every CouchbaseCluster which will reconcile the group will be checked.
func checkCouchbaseGroupRBACConstraints(v *types.Validator, group *couchbasev2.CouchbaseGroup) error {
	allClusters, err := v.Abstraction.GetCouchbaseClusters(group.Namespace)
	if err != nil {
		return err
	}

	for _, c := range allClusters.Items {
		return checkClusterGroupRBACConstraints(v, &c, group)
	}

	return nil
}

// checkClusterGroupRBACConstraints checks that the security_admin role is not used in any CouchbaseGroup for a cluster running a server version above 7.0.0. If a specific group is passed in, we will only validate against that group. If this is omitted, we will validate against every group for the cluster.
func checkClusterGroupRBACConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, group *couchbasev2.CouchbaseGroup) error {
	if !cluster.Spec.Security.RBAC.Managed {
		return nil
	}

	if v, err := isVersion7OrAbove(cluster); err != nil {
		return err
	} else if !v {
		return nil
	}

	couchbaseGroups := &couchbasev2.CouchbaseGroupList{}
	// If we pass a group into the func, we should validate against that group. If this is omitted, we will fetch and validate against every group for the cluster.
	if group != nil {
		couchbaseGroups.Items = []couchbasev2.CouchbaseGroup{*group}
	} else {
		groups, err := v.Abstraction.GetCouchbaseGroups(cluster.Namespace, cluster.Spec.Security.RBAC.Selector)
		if err != nil {
			return err
		}
		couchbaseGroups = groups
	}

	for _, g := range couchbaseGroups.Items {
		for _, r := range g.Spec.Roles {
			if r.Name == couchbasev2.RoleSecurityAdmin {
				return fmt.Errorf("security_admin role is configured in group %s and cannot be used with Couchbase Server 7.0.0 and above", g.Name)
			}
		}
	}

	return nil
}

func checkChangeConstraintsBucketMigratingAnnotation(prev, current *couchbasev2.CouchbaseCluster) error {
	if prev.Spec.Buckets.EnableBucketMigrationRoutines != current.Spec.Buckets.EnableBucketMigrationRoutines {
		if cond := prev.Status.GetCondition(couchbasev2.ClusterConditionBucketMigration); cond != nil && cond.Status == v1.ConditionTrue {
			return fmt.Errorf("cao.couchbase.com/buckets.enableBucketMigrationRoutines cannot be changed while a bucket migration is taking place")
		}
	}

	return nil
}

// checkConstraintServicelessOverAdminService returns a warning if the AdminService is used, recommending
// using a serviceless server class instead.
func checkConstraintServicelessOverAdminService(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	for _, server := range cluster.Spec.Servers {
		for _, service := range server.Services {
			if service == couchbasev2.AdminService {
				return []string{fmt.Sprintf("Server class %s uses the admin service - it is recommended to use a serviceless server class ('[]') instead", server.Name)}, nil
			}
		}
	}

	return nil, nil
}
