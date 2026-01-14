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
	"k8s.io/apimachinery/pkg/util/intstr"
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
		checkConstraintDataServiceMemoryQuota,
		checkConstraintDataServiceMemcachedThreadCounts,
		checkConstraintIndexServiceMemoryQuota,
		checkConstraintSearchServiceMemoryQuota,
		checkConstraintEventingServiceMemoryQuota,
		checkConstraintAnalyticsServiceMemoryQuota,
		checkConstraintPerServiceClassPDB,
		checkConstraintQueryTemporarySpace,
		checkConstraintQueryCompletedStreamSize,
		checkConstraintAutoFailoverTimeout,
		checkConstraintAutoFailoverMaxCount,
		checkConstraintAutoFailoverEphemeral,
		checkConstraintAutoFailoverOnDataDiskIssuesTimePeriod,
		checkConstraintIndexerMemorySnapshotInterval,
		checkConstraintIndexerStableSnapshotInterval,
		checkConstraintAdminSecret,
		checkConstraintLoggingPermissible,
		checkConstraintAuditLoggingPermissible,
		checkConstraintXDCRRemoteAuthentication,
		checkConstraintXDCRReplicationBuckets,
		checkConstraintXDCRReplicationScopesAndCollectionsSupported,
		checkConstraintXDCRReplicationRules,
		checkConstraintServerClassContainsDataService,
		checkoutConstraintNoArbiterClassBelow76,
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
		checkAdminServiceConstraints,
		checkClusterRBACConstraints,
		checkClusterBackupConstraints,
		checkConstraintsDataSettings,
		checkConstraintsIndexerSettings,
		checkConstraintEncryptionKeys,
		checkConstraintsPasswordPolicy,
		checkConstraintsUpgrade,
	}

	warningChecks := []func(*types.Validator, *couchbasev2.CouchbaseCluster) ([]string, error){
		checkConstraintTwoDataNodesForDeltaRecovery,
		checkConstraintUpgradeFieldsDeprecated,
		checkConstraintArbiterOverAdminService,
		checkMigrationConstraints,
		checkConstraintMemcachedBucketDeprecated,
		checkServerClassImageDeprecated,
		checkConstraintsDeprecatedNetworkingOptions,
		checkConstraintDeprecatedAnnotations,
		checkConstraintDeprecatedRBACRoles,
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

func checkConstraintsUpgrade(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Upgrade == nil {
		return nil
	}

	var errs []error

	errs = append(errs, checkUpgradeOrderEntriesExist(cluster.Spec.Upgrade.UpgradeOrderType, cluster)...)

	uniqueMap := make(map[string]bool)

	for _, entry := range cluster.Spec.Upgrade.UpgradeOrder {
		if _, exists := uniqueMap[entry]; !exists {
			uniqueMap[entry] = true
			continue
		}

		errs = append(errs, fmt.Errorf("upgrade order contains duplicate entry: %s", entry))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkConstraintClusterName(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if checkAnnotationSkipClusterNameLengthValidation(cluster.Annotations) {
		return nil
	}

	if len(cluster.Name) > 42 {
		return fmt.Errorf("cluster name %s cannot be longer than 42 characters", cluster.Name)
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
	if (cluster.GetUpgradeProcess() == couchbasev2.InPlaceUpgrade || cluster.GetUpgradeProcess() == couchbasev2.DeltaRecovery) && cluster.GetUpgradeStrategy() == couchbasev2.ImmediateUpgrade {
		return fmt.Errorf("cannot set spec.upgrade.upgradeStrategy to ImmediateUpgrade when spec.upgrade.upgradeProcess is set to InPlaceUpgrade or DeltaRecovery")
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

func checkMigrationConstraints(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if cluster.Spec.Migration == nil {
		return nil, nil
	}

	if cluster.Spec.Hibernate {
		return nil, fmt.Errorf("spec.hibernate cannot be enabled when spec.migration is configured")
	}

	if cluster.Spec.Migration.NumUnmanagedNodes >= cluster.Spec.TotalSize() {
		return nil, fmt.Errorf("spec.migration.numUnmanagedNodes must be less than the total size of the cluster")
	}

	warnings := []string{}

	if ws, err := checkMigrationOrderOverride(cluster); err != nil {
		return nil, err
	} else {
		warnings = append(warnings, ws...)
	}

	if cluster.Spec.Buckets.Managed {
		warnings = append(warnings, "spec.Buckets.Managed is true. Any bucket not defined as a kubernetes resource will be deleted once migration is completed")
	}

	if cluster.Spec.Security.RBAC.Managed {
		warnings = append(warnings, "spec.Security.RBAC.Managed is true. Any user, group or role not defined as a kubernetes resource will be deleted once migration is completed")
	}

	return warnings, nil
}

func checkMigrationOrderOverride(cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if cluster.Spec.Migration == nil || cluster.Spec.Migration.MigrationOrderOverride == nil {
		return nil, nil
	}

	warnings := []string{}

	if len(cluster.Spec.Migration.MigrationOrderOverride.NodeOrder) > 0 &&
		cluster.Spec.Migration.MigrationOrderOverride.MigrationOrderOverrideStrategy != couchbasev2.ByNode {
		warnings = append(warnings, "spec.migration.migrationOrderOverride.nodeOrder will be ignored when using ByServerClass or ByServerGroup strategy")
	}

	if len(cluster.Spec.Migration.MigrationOrderOverride.ServerGroupOrder) > 0 &&
		cluster.Spec.Migration.MigrationOrderOverride.MigrationOrderOverrideStrategy != couchbasev2.ByServerGroup {
		warnings = append(warnings, "spec.migration.migrationOrderOverride.serverGroupOrder will be ignored when using ByNode or ByServerClass strategy")
	}

	if len(cluster.Spec.Migration.MigrationOrderOverride.ServerClassOrder) > 0 &&
		cluster.Spec.Migration.MigrationOrderOverride.MigrationOrderOverrideStrategy != couchbasev2.ByServerClass {
		warnings = append(warnings, "spec.migration.migrationOrderOverride.serverClassOrder will be ignored when using ByNode or ByServerGroup strategy")
	}

	if len(cluster.Spec.Migration.MigrationOrderOverride.ServerClassOrder) != 0 {
		validServerConfigs := []string{}

		for _, server := range cluster.Spec.Servers {
			validServerConfigs = append(validServerConfigs, server.Name)
		}

		for _, sc := range cluster.Spec.Migration.MigrationOrderOverride.ServerClassOrder {
			if !slices.Contains(validServerConfigs, sc) {
				return warnings, fmt.Errorf("spec.migration.migrationOrderOverride.serverClassOrder contains an invalid server class: %s", sc)
			}
		}
	}

	return warnings, nil
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

// checkConstraintDataServiceMemcachedThreadCounts checks the reader/writer thread settings for a specific server version.
func checkConstraintDataServiceMemcachedThreadCounts(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.ClusterSettings.Data == nil {
		return nil
	}

	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	minThreads := int32(1)
	maxThreads := int32(64)
	allowedSettings := []string{}

	if after71, err := couchbaseutil.VersionAfter(tag, "7.1.0"); !after71 && err == nil {
		minThreads = int32(4)

		allowedSettings = append(allowedSettings, "default", "disk_io_optimized")
	} else if after80, err := couchbaseutil.VersionAfter(tag, "8.0.0"); !after80 && err == nil {
		allowedSettings = append(allowedSettings, "default", "disk_io_optimized")
	} else {
		allowedSettings = append(allowedSettings, "balanced", "disk_io_optimized")
	}

	validateThreadSetting := func(setting *intstr.IntOrString, minThreads, maxThreads int32, allowedSettings []string, tag, path string) error {
		if setting == nil {
			return nil
		}

		if (setting.Type == intstr.Int && (setting.IntVal < minThreads || setting.IntVal > maxThreads)) ||
			(setting.Type == intstr.String && !slices.Contains(allowedSettings, setting.StrVal)) {
			return fmt.Errorf("%s must either be between %d and %d or one of %v for Couchbase server version %s", path, minThreads, maxThreads, strings.Join(allowedSettings, ", "), tag)
		}

		return nil
	}

	var errs []error
	if err := validateThreadSetting(cluster.Spec.ClusterSettings.Data.ReaderThreads, minThreads, maxThreads, allowedSettings, tag, "spec.cluster.data.readerThreads"); err != nil {
		errs = append(errs, err)
	}

	if err := validateThreadSetting(cluster.Spec.ClusterSettings.Data.WriterThreads, minThreads, maxThreads, allowedSettings, tag, "spec.cluster.data.writerThreads"); err != nil {
		errs = append(errs, err)
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

// checkConstraintQueryCompletedStreamSize ensures completedStreamSize is only set for Couchbase Server 8.0.0+.
func checkConstraintQueryCompletedStreamSize(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// CompletedStreamSize is now a cluster-level (spec.cluster.query.completedStreamSize) field.
	if cluster.Spec.ClusterSettings.Query == nil || cluster.Spec.ClusterSettings.Query.CompletedStreamSize == nil {
		return nil
	}

	streamSize := cluster.Spec.ClusterSettings.Query.CompletedStreamSize

	// Ensure numeric value is not negative.
	if *streamSize < 0 {
		return fmt.Errorf("spec.cluster.query.completedStreamSize must be >= 0")
	}

	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	after800, err := couchbaseutil.VersionAfter(tag, "8.0.0")
	if err != nil {
		return err
	}

	if !after800 {
		return fmt.Errorf("spec.cluster.query.completedStreamSize requires Couchbase Server version 8.0.0 or greater")
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

func checkConstraintAutoFailoverEphemeral(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	tag, err := k8sutil.CouchbaseVersion(cluster.Spec.Image)
	if err != nil {
		return err
	}

	if before80, err := couchbaseutil.VersionBefore(tag, "8.0.0"); before80 && err == nil {
		if cluster.Spec.ClusterSettings.AllowFailoverEphemeralNoReplicas != nil {
			return fmt.Errorf("spec.cluster.allowFailoverEphemeralNoReplicas is not supported in Couchbase Server versions lower than 8.0.0")
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

// checkConstraintLoggingSidecarTLS checks that if enabled, the logging sidecar TLS configs passed.
func checkConstraintLoggingSidecarTLS(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	fbs := cluster.Spec.Logging.Server
	if fbs == nil {
		return nil
	}

	if fbs.Sidecar == nil || fbs.Sidecar.TLS == nil {
		return nil
	}

	// In operator 2.9.0 the `spec.logging.server.sidecar.tls` field is not
	// implemented. Reject usage via the admission controller so users cannot
	// rely on it until mounting/rotation is implemented in 2.9.1.
	return fmt.Errorf("spec.logging.server.sidecar.tls is not implemented in this operator version; available in 2.9.1+")
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

// checkHlvPruningWindowSecCompatibility checks if HlvPruningWindowSec is compatible with the cluster version.
func checkHlvPruningWindowSecCompatibility(cluster *couchbasev2.CouchbaseCluster, resourceName string, fieldValue *int32) error {
	if fieldValue == nil {
		return nil
	}

	isAtLeast76, err := cluster.IsAtLeastVersion("7.6.0")
	if err != nil {
		return fmt.Errorf("unable to check cluster version for HlvPruningWindowSec validation: %w", err)
	}

	if isAtLeast76 {
		// HlvPruningWindowSec is not supported in Couchbase Server 7.6+
		return fmt.Errorf("HlvPruningWindowSec is not supported in Couchbase Server 7.6+, field will be ignored in %s", resourceName)
	}

	return nil
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

			if err := checkConstraintXDCRReplicationMappings(replication); err != nil {
				errs = append(errs, fmt.Errorf("invalid rule for XDCR replication in couchbasereplications.couchbase.com/%s: %w", replication.Name, err))
			}

			if err := checkHlvPruningWindowSecCompatibility(cluster, fmt.Sprintf("couchbasereplications.couchbase.com/%s", replication.Name), replication.Spec.HlvPruningWindowSec); err != nil {
				errs = append(errs, err)
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
				errs = append(errs, fmt.Errorf("duplicate rule (%s) for XDCR migration in couchbasemigrations.couchbase.com/%s", hash, migration.Name))
				continue
			}

			currentRules[hash] = true

			if err := checkConstraintXDCRMigrationMappings(migration); err != nil {
				errs = append(errs, fmt.Errorf("invalid rule for XDCR migration in couchbasemigrations.couchbase.com/%s: %w", migration.Name, err))
			}

			if err := checkHlvPruningWindowSecCompatibility(cluster, fmt.Sprintf("couchbasemigrationreplications.couchbase.com/%s", migration.Name), migration.Spec.HlvPruningWindowSec); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
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
			err = annotations.Populate(&replication.Spec, replication.Annotations)
			if err != nil {
				errs = append(errs, err)
			}

			if err := validateReplicationBucketValid(v, cluster, &replication.Spec); err != nil {
				errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasereplications.couchbase.com/%s must be valid: %w", replication.Spec.Bucket, replication.Name, err))
			}

			if err := validateReplicationConflictLogging(v, cluster, &replication.Spec); err != nil {
				errs = append(errs, fmt.Errorf("couchbasereplications.couchbase.com/%s has an invalid conflict logging configuration: %w", replication.Name, err))
			}
		}

		migrations, err := v.Abstraction.GetCouchbaseMigrationReplications(cluster.Namespace, remoteCluster.Replications.Selector)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, migration := range migrations.Items {
			err = annotations.Populate(&migration.Spec, migration.Annotations)
			if err != nil {
				errs = append(errs, err)
			}

			if err := validateReplicationBucketValid(v, cluster, &migration.Spec); err != nil {
				errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasemigrationreplications.couchbase.com/%s must be valid: %w", migration.Spec.Bucket, migration.Name, err))
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

func checkoutConstraintNoArbiterClassBelow76(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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

func checkConstraintUpgradeFieldsDeprecated(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	var warnings []string

	if cluster.GetUpgradeProcess() == couchbasev2.DeltaRecovery {
		warnings = append(warnings, "DeltaRecovery is deprecated, please use InPlaceUpgrade instead")
	}

	if cluster.Spec.UpgradeProcess != nil {
		warnings = append(warnings, "spec.upgradeProcess is deprecated, please use spec.upgrade.upgradeStrategy instead")
	}

	if cluster.Spec.UpgradeStrategy != nil {
		warnings = append(warnings, "spec.upgradeStrategy is deprecated, please use spec.upgrade.upgradeStrategy instead")
	}

	if len(warnings) > 0 {
		return warnings, nil
	}

	return nil, nil
}

func checkConstraintMoreThanTwoDataNodesMultiNodeClusterInPlaceUpgrade(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if (cluster.GetUpgradeProcess() == couchbasev2.DeltaRecovery || cluster.GetUpgradeProcess() == couchbasev2.InPlaceUpgrade) && (cluster.GetNumberOfDataServiceNodes() < 2 && len(cluster.Spec.Servers) > 1) {
		return fmt.Errorf("cannot enable InPlaceUpgrade with one data service node in a multi-node cluster")
	}

	return nil
}

// checkConstraintTwoDataNodesForDeltaRecovery checks there are at least two data nodes when trying to use InPlaceUpgrade.
func checkConstraintTwoDataNodesForDeltaRecovery(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if cluster.GetUpgradeProcess() == couchbasev2.DeltaRecovery || cluster.GetUpgradeProcess() == couchbasev2.InPlaceUpgrade {
		if cluster.GetNumberOfDataServiceNodes() < 2 {
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

// checkConstraintTLS checks that the X.509v3 SANs are correctly configured for use with
// Couchbase on Kubernetes, that the referenced secret exists and is correctly formatted.
func checkConstraintTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsTLSEnabled() {
		return nil
	}

	// Determine whether to include bare hostnames from the cluster spec; default to true.
	includeBareHostnames := true
	if cluster.Spec.Networking.TLS != nil {
		includeBareHostnames = cluster.Spec.Networking.TLS.ValidateBareHostnames
	}

	subjectAltNames := util_x509.MandatorySANs(cluster.Name, cluster.Namespace, includeBareHostnames)

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
	if cluster.Spec.ClusterSettings.AutoCompaction.MagmaFragmentationThresholdPercentage != nil {
		magmaFragmentationPercentageSupported, err := cluster.IsAtLeastVersion("7.1.0")
		if err != nil {
			return err
		}

		if !magmaFragmentationPercentageSupported {
			return fmt.Errorf("spec.cluster.autoCompaction.magmaFragmentationPercentage is only supported for Couchbase 7.1.0+")
		}

		if *cluster.Spec.ClusterSettings.AutoCompaction.MagmaFragmentationThresholdPercentage < 10 || *cluster.Spec.ClusterSettings.AutoCompaction.MagmaFragmentationThresholdPercentage > 100 {
			return fmt.Errorf("spec.cluster.autoCompaction.magmaFragmentationPercentage must be between 10 and 100")
		}
	}

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

	if autoCompaction.MagmaFragmentationThresholdPercentage != nil {
		if *autoCompaction.MagmaFragmentationThresholdPercentage < 10 || *autoCompaction.MagmaFragmentationThresholdPercentage > 100 {
			return fmt.Errorf("spec.autoCompaction.magmaFragmentationPercentage must be between 10 and 100")
		}
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

func checkBucketHistoryRetentionSettings(bucket *couchbasev2.CouchbaseBucket, storageBackend couchbasev2.CouchbaseStorageBackend) error {
	if bucket.Spec.HistoryRetentionSettings != nil {
		if storageBackend == couchbasev2.CouchbaseStorageBackendCouchstore {
			return fmt.Errorf("historyRetentionSettings can only be used with magma storage backend")
		}

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

//nolint:gocognit,gocyclo
func CheckConstraintsBucket(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	var errs []error

	err := annotations.Populate(&bucket.Spec, bucket.Annotations)

	if err != nil {
		return nil, err
	}

	if checkAnnotationSkipValidation(bucket.Annotations) {
		return nil, nil
	}
	if err := validateBucketStorageBackendAndOnlineEvictionPolicyConstraints(v, bucket, cluster); err != nil {
		errs = append(errs, err)
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

	if err := checkConstraintAutoCompactionBucket(bucket.Spec.AutoCompaction); err != nil {
		errs = append(errs, err)
	}

	errs = append(errs, checkBucketAnnotations(bucket)...)

	if bucket.Spec.SampleBucket {
		if err := checkSampleBucketFieldPresets(bucket.Spec.ConflictResolution, bucket.Spec.EnableIndexReplica); err != nil {
			errs = append(errs, err)
		}
	}

	if err := checkBucketCrossClusterVersioning(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if err := checkBucketMemoryWatermarkSettings(bucket); err != nil {
		errs = append(errs, err)
	}

	if err := checkBucketEncryptionAtRestSettings(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if err := checkBucketUnsupportedFields(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	var warnings []string

	w, err := checkBucketCRDFieldsForNonDefaultUnsupportedFields(v, bucket, cluster)
	if err != nil {
		return nil, err
	}

	if len(w) > 0 {
		warnings = append(warnings, w...)
	}

	if errs != nil {
		return warnings, errors.CompositeValidationError(errs...)
	}

	return warnings, nil
}

func checkBucketMemoryWatermarkSettings(bucket *couchbasev2.CouchbaseBucket) error {
	if bucket.Spec.MemoryLowWatermark != nil || bucket.Spec.MemoryHighWatermark != nil {
		low := constants.MemoryLowWatermarkDefault
		high := constants.MemoryHighWatermarkDefault

		if bucket.Spec.MemoryLowWatermark != nil {
			low = *bucket.Spec.MemoryLowWatermark
		}

		if bucket.Spec.MemoryHighWatermark != nil {
			high = *bucket.Spec.MemoryHighWatermark
		}

		if low >= high {
			return fmt.Errorf("spec.memoryLowWatermark (%d) must be less than spec.memoryHighWatermark (%d)", low, high)
		}
	}

	return nil
}

func checkBucketEncryptionAtRestSettings(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) error {
	config := bucket.Spec.EncryptionAtRest
	if config == nil {
		return nil
	}

	// Check rotation interval is at least 7 days if set
	if config.RotationInterval != nil {
		rotationInterval := config.RotationInterval.Duration
		if rotationInterval.Hours() < 7*24 {
			return fmt.Errorf("rotation interval must be at least 7 days, got %v", rotationInterval)
		}
	}

	// Check key lifetime is at least 30 days if set
	if config.KeyLifetime != nil {
		keyLifetime := config.KeyLifetime.Duration
		if keyLifetime.Hours() < 30*24 {
			return fmt.Errorf("key lifetime must be at least 30 days, got %v", keyLifetime)
		}
	}

	keyName := config.KeyName
	if keyName == "" {
		return nil
	}

	clusters, err := getBucketsRelatedClusters(v, bucket)
	if err != nil {
		return err
	}

	for _, cluster := range clusters {
		if !cluster.IsEncryptionAtRestManaged() {
			continue
		}

		encryptionAtRest := cluster.Spec.Security.EncryptionAtRest

		// Get all encryption keys using the selector
		encryptionKeys, err := v.Abstraction.GetCouchbaseEncryptionKeys(cluster.Namespace, encryptionAtRest.Selector)
		if err != nil {
			return fmt.Errorf("failed to get encryption keys: %w", err)
		}

		// Create a map of key names to their specs for easy lookup
		keyMap := make(map[string]*couchbasev2.CouchbaseEncryptionKey)

		for i := range encryptionKeys.Items {
			key := &encryptionKeys.Items[i]
			keyMap[key.Name] = key
		}

		key, ok := keyMap[keyName]
		if !ok {
			return fmt.Errorf("spec.encryptionAtRest.keyName %s does not exist for cluster %s", keyName, cluster.Name)
		}

		if !key.Spec.Usage.AllBuckets {
			return fmt.Errorf("spec.encryptionAtRest.keyName %s does not have bucket encryption usage for cluster %s", keyName, cluster.Name)
		}
	}
	return nil
}

func checkMagmaBucketRequiredSettings(bucket *couchbasev2.CouchbaseBucket) error {
	if bucket.Spec.EnableIndexReplica {
		return fmt.Errorf("cannot set spec.enableIndexReplica to true for magma buckets")
	}
	return nil
}

func checkMagmaBucketClusterVersionSettings(v *types.Validator, c *couchbasev2.CouchbaseCluster, bucket *couchbasev2.CouchbaseBucket) error {
	// If explicitly set to magma, check if the cluster version supports it.
	after71, err := c.IsAtLeastVersion("7.1.0")
	if err != nil {
		return err
	}

	if !after71 {
		return fmt.Errorf("magma storage backend requires Couchbase Server version 7.1.0 or later for cluster: %s", c.NamespacedName())
	}

	after712, err := c.IsAtLeastVersion("7.1.2")
	if err != nil {
		return err
	}

	if !after712 {
		// We shouldn't allow FTS, Eventing or Analytics services on magma buckets when the cb version is < 7.1.2
		for _, config := range c.Spec.Servers {
			services := couchbasev2.ServiceList(config.Services)
			if services.Contains(couchbasev2.EventingService) || services.Contains(couchbasev2.AnalyticsService) || services.Contains(couchbasev2.SearchService) {
				return fmt.Errorf("search, eventing or analytics services cannot be used with magma buckets below CB Server 7.1.2. One or more of those services has been used as server class: %v, for cluster: %s", services.StringSlice(), c.NamespacedName())
			}
		}
	}

	after80, err := c.RunningVersion("8.0.0")
	if err != nil {
		return err
	}

	if after80 {
		return checkMagmaVBucketsSettings(bucket, c.NamespacedName())
	}

	if bucket.Spec.NumVBuckets != nil {
		return fmt.Errorf("spec.numVBuckets cannot be set for buckets pre Couchbase Server 8.0.0")
	}

	// MemoryQuota defaults to 100Mi, and pre-8.0 magma buckets require a minimum of 1024Mi.
	if bucket.Spec.MemoryQuota == nil || bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
		return fmt.Errorf("spec.memoryQuota must be greater than or equal to 1024Mi for magma buckets pre Couchbase Server 8.0.0 for cluster: %s", c.NamespacedName())
	}

	return nil
}

// checkMagmaVBucketsSettings checks if the bucket's numVBuckets is set to a valid value for the cluster.
// This method is not dependent on cluster version and assumes 8.0.0 or later.
func checkMagmaVBucketsSettings(bucket *couchbasev2.CouchbaseBucket, cName string) error {
	if bucket.Spec.NumVBuckets == nil {
		return nil
	}

	if *bucket.Spec.NumVBuckets != 1024 && *bucket.Spec.NumVBuckets != 128 {
		return fmt.Errorf("spec.numVBuckets can only be set to either 128 or 1024 for cluster: %s", cName)
	}

	// If numVBuckets is 128, either explicitly or by defaulting to that value by omission, we don't need to check memory quota as the minimum required is 100Mi which is already a constraint.
	if *bucket.Spec.NumVBuckets == 1024 && bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
		return fmt.Errorf("spec.memoryQuota must be greater than or equal to 1024Mi when numVBuckets is 1024 for cluster: %s", cName)
	}

	return nil
}

// checkCrossClusterVersioning checks if the bucket's cross cluster versioning is compatible with the cluster version.
func checkCrossClusterVersioning(v *types.Validator, namespace string, bucketLabels map[string]string, enableCrossClusterVersioning *bool) error {
	if enableCrossClusterVersioning == nil {
		return nil
	}

	clusters, err := v.Abstraction.GetCouchbaseClusters(namespace)
	if err != nil {
		return err
	}

	for _, cluster := range clusters.Items {
		clusterBucketSelector, err := metav1.LabelSelectorAsSelector(cluster.Spec.Buckets.Selector)
		if err != nil {
			return err
		}

		if cluster.Spec.Buckets.Selector == nil || clusterBucketSelector.Matches(labels.Set(bucketLabels)) {
			srvVerAfter76, err := cluster.IsAtLeastVersion("7.6.0")
			if err != nil {
				return err
			}

			if !srvVerAfter76 {
				return fmt.Errorf("enableCrossClusterVersioning requires Couchbase Server version 7.6.0 or later")
			}
		}
	}

	return nil
}

func checkBucketCrossClusterVersioning(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) error {
	return checkCrossClusterVersioning(v, bucket.Namespace, bucket.Labels, bucket.Spec.EnableCrossClusterVersioning)
}

func checkEphemeralBucketCrossClusterVersioning(v *types.Validator, bucket *couchbasev2.CouchbaseEphemeralBucket) error {
	return checkCrossClusterVersioning(v, bucket.Namespace, bucket.Labels, bucket.Spec.EnableCrossClusterVersioning)
}

func checkBucketReplicasCount(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	clusters := []*couchbasev2.CouchbaseCluster{}
	if cluster != nil {
		clusters = []*couchbasev2.CouchbaseCluster{cluster}
	} else {
		bClusters, err := getBucketsRelatedClusters(v, bucket)
		if err != nil {
			return err
		}

		clusters = append(clusters, bClusters...)
	}

	for _, cluster := range clusters {
		if cluster.Spec.ClusterSettings.Data != nil && cluster.Spec.ClusterSettings.Data.MinReplicasCount > bucket.Spec.Replicas {
			return fmt.Errorf("spec.replicas (%v) should be atleast %v (by %s, spec.cluster.data.minReplicasCount)",
				bucket.Spec.Replicas, cluster.Spec.ClusterSettings.Data.MinReplicasCount, cluster.Name)
		}
	}

	return nil
}

func checkEphemeralBucketReplicasCount(v *types.Validator, bucket *couchbasev2.CouchbaseEphemeralBucket, cluster *couchbasev2.CouchbaseCluster) error {
	clusters := []*couchbasev2.CouchbaseCluster{}
	if cluster != nil {
		clusters = []*couchbasev2.CouchbaseCluster{cluster}
	} else {
		bClusters, err := getEphemeralBucketsRelatedClusters(v, bucket)
		if err != nil {
			return err
		}

		clusters = append(clusters, bClusters...)
	}

	for _, cluster := range clusters {
		if cluster.Spec.ClusterSettings.Data != nil && cluster.Spec.ClusterSettings.Data.MinReplicasCount > bucket.Spec.Replicas {
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

	if err := checkEphemeralBucketReplicasCount(v, bucket, cluster); err != nil {
		errs = append(errs, err)
	}

	if bucket.Spec.SampleBucket {
		if err := checkSampleBucketFieldPresets(bucket.Spec.ConflictResolution, false); err != nil {
			errs = append(errs, err)
		}
	}

	if err := checkEphemeralBucketCrossClusterVersioning(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsMemcachedBucket(v *types.Validator, bucket *couchbasev2.CouchbaseMemcachedBucket, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	var errs []error

	if checkAnnotationSkipValidation(bucket.Annotations) {
		return nil, nil
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

	warnings, err := validateMemcachedBucketSupported(v, bucket, cluster)
	if err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return warnings, errors.CompositeValidationError(errs...)
	}

	return warnings, nil
}

func CheckConstraintsReplication(_ *types.Validator, r *couchbasev2.CouchbaseReplication) error {
	if checkAnnotationSkipValidation(r.Annotations) {
		return nil
	}

	err := annotations.Populate(&r.Spec, r.Annotations)
	if err != nil {
		return err
	}

	mobileValue := ""
	if r.Spec.Mobile != nil {
		mobileValue = *r.Spec.Mobile
	}

	switch mobileValue {
	case "", "Active", "Off":
	default:
		return fmt.Errorf("spec.mobile must be either 'Active' or 'Off'")
	}

	return nil
}

func CheckConstraintsCouchbaseUser(v *types.Validator, user *couchbasev2.CouchbaseUser) ([]string, error) {
	var errs []error

	if checkAnnotationSkipValidation(user.Annotations) {
		return nil, nil
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
		}
	case couchbasev2.LDAPAuthDomain:
		// authSecret not accepted for LDAP user
		if authSecretName := user.Spec.AuthSecret; authSecretName != "" {
			errs = append(errs, fmt.Errorf("spec.authSecret %s not allowed for LDAP user `%s`", authSecretName, user.Name))
		}

		if user.Spec.Password != nil {
			errs = append(errs, fmt.Errorf("spec.password not allowed for LDAP user `%s`", user.Name))
		}

		if user.Spec.Locked != nil && *user.Spec.Locked {
			errs = append(errs, fmt.Errorf("spec.locked not allowed for LDAP user `%s`", user.Name))
		}
	default:
		return nil, fmt.Errorf("unknown auth domain: %s", user.Spec.AuthDomain)
	}

	errs = append(errs, checkClusterUserConstraints(v, user, nil)...)

	if errs != nil {
		return nil, errors.CompositeValidationError(errs...)
	}

	warnings, err := validateCouchbaseUserNameConstraints(v, user)
	if err != nil {
		return nil, err
	}

	return warnings, nil
}

func checkPasswordAuthSecret(v *types.Validator, user *couchbasev2.CouchbaseUser) (string, error) {
	secret, found, err := v.Abstraction.GetSecret(user.Namespace, user.Spec.AuthSecret)
	if err != nil {
		return "", err
	}

	if !found {
		return "", fmt.Errorf("secret %s referenced by user.spec.authSecret for user `%s` must exist", user.Spec.AuthSecret, user.Name)
	}

	pw, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("secret %s referenced by user.spec.authSecret for user `%s` must contain password", user.Spec.AuthSecret, user.Name)
	}

	return string(pw), nil
}

// checkUserPasswordForCluster checks that a given password is valid for a CouchbaseCluster to create a new CouchbaseUser.
// This will return nil if the user is not managed by the cluster and therefore not subject to the password policy, or if the user's authSecret password complies with the cluster's password policy.
func checkUserPasswordForCluster(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, user *couchbasev2.CouchbaseUser) error {
	if cluster.Spec.Security.PasswordPolicy == nil {
		return nil
	}

	// We can ignore users that already exist on a cluster as subsequent changes to the password secret are irrelevant.
	if slices.Contains(cluster.Status.Users, user.GetUserID()) {
		return nil
	}

	pw, err := checkPasswordAuthSecret(v, user)
	if err != nil {
		return err
	}

	if !k8sutil.PasswordCompliesWithCouchbasePasswordPolicy(cluster.Spec.Security.PasswordPolicy, pw) {
		return fmt.Errorf("initial password for user `%s` does not comply with the password policy of cluster `%s`", user.Name, cluster.NamespacedName())
	}

	return nil
}

// validateCouchbaseUserNameConstraints checks that no other CouchbaseUser resources
// share the same name as the provided user.
func validateCouchbaseUserNameConstraints(v *types.Validator, user *couchbasev2.CouchbaseUser) ([]string, error) {
	// Get all CouchbaseUsers in the same namespace
	existingUsers, err := v.Abstraction.GetCouchbaseUsers(user.Namespace, nil)
	if err != nil {
		return nil, err
	}

	userName := user.GetUserID()

	// Check each existing user
	for _, existingUser := range existingUsers.Items {
		// Skip comparing against self
		if existingUser.Name == user.Name {
			continue
		}

		// Check if names match
		if existingUser.GetUserID() == userName {
			return []string{fmt.Sprintf("user name %s is already in use by CouchbaseUser %s", userName, existingUser.GetUserID())}, nil
		}
	}

	return nil, nil
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

	if backup.HasCloudStore() && backup.Spec.Strategy == couchbasev2.PeriodicMerge {
		errs = append(errs, fmt.Errorf("spec.strategy cannot be periodicMerge when using a cloud object store"))
	}

	if !backup.HasCloudStore() && backup.Spec.EphemeralVolume {
		errs = append(errs, fmt.Errorf("spec.ephemeralVolume is only useable with spec.objectStore.uri or spec.s3Bucket"))
	}

	if backup.Spec.EphemeralVolume && backup.Spec.DefaultRecoveryMethod == couchbasev2.DefaultRecoveryTypeResume {
		errs = append(errs, fmt.Errorf("spec.defaultRecoveryMethod cannot be resume when using ephemeral volume"))
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

	errs = append(errs, checkConstraintsEnvVarsBackup(backup)...)

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkConstraintsEnvVarsBackup(backup *couchbasev2.CouchbaseBackup) []error {
	var errs []error

	for _, env := range backup.Spec.Env {
		if env.Value != "" && env.ValueFrom != nil {
			errs = append(errs, fmt.Errorf("spec.env[%s] cannot have both value and valueFrom set", env.Name))
		}

		if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
			match, err := regexp.MatchString(`[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*`, env.ValueFrom.ConfigMapKeyRef.Name)
			if err != nil {
				errs = append(errs, err)
			}

			if !match {
				errs = append(errs, fmt.Errorf("spec.env[%s].valueFrom.configMapKeyRef.name %s is not a valid config map name", env.Name, env.ValueFrom.ConfigMapKeyRef.Name))
			}
		}
	}

	return errs
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

func getClusterBucketsByType(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) (map[string]*couchbasev2.CouchbaseBucket, map[string]*couchbasev2.CouchbaseEphemeralBucket, error) {
	couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, nil, err
	}

	couchbaseBucketsMap := make(map[string]*couchbasev2.CouchbaseBucket)
	for i := range couchbaseBuckets.Items {
		couchbaseBucketsMap[couchbaseBuckets.Items[i].GetCouchbaseName()] = &couchbaseBuckets.Items[i]
	}

	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, nil, err
	}

	ephemeralBucketsMap := make(map[string]*couchbasev2.CouchbaseEphemeralBucket)
	for i := range ephemeralBuckets.Items {
		ephemeralBucketsMap[ephemeralBuckets.Items[i].GetCouchbaseName()] = &ephemeralBuckets.Items[i]
	}

	return couchbaseBucketsMap, ephemeralBucketsMap, nil
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

func validateMemcachedBucketSupported(v *types.Validator, memcachedBucket *couchbasev2.CouchbaseMemcachedBucket, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	clusters, err := v.Abstraction.GetCouchbaseClusters(memcachedBucket.Namespace)
	if err != nil {
		return nil, err
	}

	checkCluster := func(cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
		if cluster == nil {
			return []string{}, nil
		}

		memcachedDeprecated, err := cluster.IsAtLeastVersion("8.0.0")
		if err != nil {
			return nil, err
		}

		if memcachedDeprecated {
			return []string{util.GenerateMemcachedBucketWarning(cluster, memcachedBucket)}, nil
		}

		return []string{}, nil
	}

	if cluster != nil {
		return checkCluster(cluster)
	}

	warnings := []string{}
	errs := []error{}

	for _, cluster := range clusters.Items {
		warns, err := checkCluster(&cluster)
		if err != nil {
			errs = append(errs, err)
		}

		warnings = append(warnings, warns...)
	}

	if len(errs) > 0 {
		return warnings, errors.CompositeValidationError(errs...)
	}

	return warnings, nil
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
			if sb {
				return fmt.Errorf("bucket memory allocation (%v) exceeds data service quota (%v) on cluster %s sample buckets have a memory quota of 200Mi", allocated, cluster.Spec.ClusterSettings.DataServiceMemQuota, cluster.Name)
			}

			return fmt.Errorf("bucket memory allocation (%v) exceeds data service quota (%v) on cluster %s", allocated, cluster.Spec.ClusterSettings.DataServiceMemQuota, cluster.Name)
		}
	}

	return nil
}

// validateReplicationBucketValid ensures the specified Couchbase bucket exists.
func validateReplicationBucketValid(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, rs *couchbasev2.CouchbaseReplicationSpec) error {
	if !cluster.Spec.Buckets.Managed {
		return nil
	}

	buckets, err := v.Abstraction.GetBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	bucketName := string(rs.Bucket)

	conflictLoggingEnabled := rs.ConflictLogging != nil && rs.ConflictLogging.Enabled

	for _, bucket := range buckets {
		if bucket.GetCouchbaseName() != bucketName {
			continue
		}

		if bucket.GetType() == couchbasev2.BucketTypeMemcached {
			return fmt.Errorf("memcached bucket %s cannot be replicated", bucketName)
		}

		if rs.Mobile != nil && *rs.Mobile == "Active" {
			mobileSupported, err := cluster.IsAtLeastVersion("7.6.4")
			if err != nil {
				return err
			}

			if !mobileSupported {
				return fmt.Errorf("mobile replication requires cluster version 7.6.4 or greater")
			}

			if !bucket.HasCrossClusterVersioningEnabled() {
				return fmt.Errorf("bucket %s must have cross cluster versioning enabled to be used with mobile replication", bucketName)
			}
		}

		if conflictLoggingEnabled && !bucket.HasCrossClusterVersioningEnabled() {
			return fmt.Errorf("bucket %s must have cross cluster versioning enabled to enable conflict logging", bucketName)
		}

		return nil
	}

	return fmt.Errorf("bucket %s not found", bucketName)
}

func validateReplicationConflictLogging(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, replication *couchbasev2.CouchbaseReplicationSpec) error {
	if replication.ConflictLogging == nil || !replication.ConflictLogging.Enabled {
		return nil
	}

	if conflictLoggingSupported, err := cluster.IsAtLeastVersion("8.0.0"); err != nil {
		return err
	} else if !conflictLoggingSupported {
		return fmt.Errorf("conflict logging requires cluster version 8.0.0 or greater")
	}

	if !cluster.Spec.Buckets.Managed {
		return nil
	}

	buckets, err := v.Abstraction.GetBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	bucketExists := func(bucketName string) bool {
		for _, b := range buckets {
			if b.GetCouchbaseName() == bucketName {
				return true
			}
		}

		return false
	}

	if !bucketExists(string(replication.ConflictLogging.LogCollection.Bucket)) {
		return fmt.Errorf("bucket %s not found", replication.ConflictLogging.LogCollection.Bucket)
	}

	if replication.ConflictLogging.LogCollection.Scope == "" {
		return fmt.Errorf("spec.conflictLogging.logCollection.scope is required for conflict logging")
	}

	if replication.ConflictLogging.LogCollection.Collection == "" {
		return fmt.Errorf("spec.conflictLogging.logCollection.collection is required for conflict logging")
	}

	for _, rule := range replication.ConflictLogging.LoggingRules.CustomCollectionRules {
		if !bucketExists(string(rule.LogCollection.Bucket)) {
			return fmt.Errorf("bucket %s not found", rule.LogCollection.Bucket)
		}

		if rule.LogCollection.Scope == "" {
			return fmt.Errorf("spec.conflictLogging.logCollection.scope is required for conflict logging")
		}

		if rule.LogCollection.Collection == "" {
			return fmt.Errorf("spec.conflictLogging.logCollection.collection is required for conflict logging")
		}
	}

	return nil
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
		// No validation needed - these run immediately
	case couchbasev2.PeriodicMerge:
		if err := validateCronJobString(backup.Spec.Incremental, "spec.incremental"); err != nil {
			errs = append(errs, err)
		}

		if err := validateCronJobString(backup.Spec.Merge, "spec.merge"); err != nil {
			errs = append(errs, err)
		}
	default:
		errs = append(errs, fmt.Errorf("spec.strategy %s not valid, must be one of %s | %s | %s | %s",
			backup.Spec.Strategy, couchbasev2.FullIncremental, couchbasev2.FullOnly, couchbasev2.ImmediateFull, couchbasev2.ImmediateIncremental))
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

func CheckConstraintsEncryptionKey(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey) error {
	// If the key is being deleted, we don't need to validate it
	if key.DeletionTimestamp != nil {
		return nil
	}

	var errs []error

	// If usage is not set then we default it to all true
	usage := key.GetUsage()

	// Check that at least one of the usage values is true
	if !usage.Configuration && !usage.Key && !usage.Log && !usage.Audit && !usage.AllBuckets {
		errs = append(errs, fmt.Errorf("at least one usage field must be set to true in spec.usage"))
	}

	if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated {
		if key.Spec.AutoGenerated != nil && key.Spec.AutoGenerated.Rotation != nil && key.Spec.AutoGenerated.Rotation.IntervalDays != 0 && key.Spec.AutoGenerated.Rotation.StartTime == nil {
			errs = append(errs, fmt.Errorf("spec.autoGenerated.rotation.startTime must be set when spec.autoGenerated.rotation.intervalDays is set"))
		}
	}

	if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAWS {
		if key.Spec.AWSKey == nil {
			errs = append(errs, fmt.Errorf("spec.awsKey is required when spec.keyType is AWS"))
		} else {
			if err := validateAWSKeyARN(key.Spec.AWSKey.KeyARN); err != nil {
				errs = append(errs, err)
			}

			if err := validateAWSKeyCredentialsSecret(v, key, key.Spec.AWSKey.CredentialsSecret); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeKMIP {
		if err := validateKMIPKey(v, key); err != nil {
			errs = append(errs, err)
		}
	}

	errs = append(errs, checkEncryptionKeyUsage(v, key)...)

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkEncryptionKeyUsage(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey) []error {
	errs := []error{}

	clusters, err := v.Abstraction.GetCouchbaseClusters(key.Namespace)
	if err != nil {
		return []error{err}
	}

	for _, cluster := range clusters.Items {
		errs = append(errs, checkEncryptionKeyUsageForCluster(v, key, &cluster)...)
	}

	return errs
}

func checkEncryptionKeyUsageForCluster(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey, cluster *couchbasev2.CouchbaseCluster) []error {
	usage := key.GetUsage()

	if !cluster.IsEncryptionAtRestManaged() {
		return nil
	}

	earConf := cluster.Spec.Security.EncryptionAtRest
	if earConf == nil {
		return nil
	}

	selector := labels.Everything()

	if cluster.Spec.Security.EncryptionAtRest.Selector != nil {
		var err error
		selector, err = metav1.LabelSelectorAsSelector(cluster.Spec.Security.EncryptionAtRest.Selector)

		if err != nil {
			return []error{err}
		}
	}

	if !selector.Matches(labels.Set(key.Labels)) {
		return nil
	}

	errs := []error{}
	checkUsageConf := func(config *couchbasev2.EncryptionAtRestUsageConfiguration, usage bool, usageType string) error {
		if config == nil {
			return nil
		}

		if config.Enabled && config.KeyName == key.Name {
			if usage == false {
				errs = append(errs, fmt.Errorf("spec.usage.%s is false but encryption key %s is used for %s encryption on cluster %s", usageType, key.Name, usageType, cluster.Name))
			}
		}

		return nil
	}

	if err := checkUsageConf(earConf.Configuration, usage.Configuration, "configuration"); err != nil {
		errs = append(errs, err)
	}

	if err := checkUsageConf(earConf.Audit, usage.Audit, "audit"); err != nil {
		errs = append(errs, err)
	}

	if err := checkUsageConf(earConf.Log, usage.Log, "log"); err != nil {
		errs = append(errs, err)
	}

	if !usage.AllBuckets {
		errs = append(errs, checkEncryptionKeyUsageForClusterBuckets(v, key, cluster)...)
	}

	if !usage.Key {
		errs = append(errs, checkEncryptionKeyUsageForClusterKeys(v, key, cluster)...)
	}

	return errs
}

func checkEncryptionKeyUsageForClusterBuckets(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey, cluster *couchbasev2.CouchbaseCluster) []error {
	couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return []error{err}
	}

	errs := []error{}
	for _, bucket := range couchbaseBuckets.Items {
		if bucket.Spec.EncryptionAtRest != nil && bucket.Spec.EncryptionAtRest.KeyName == key.Name {
			errs = append(errs, fmt.Errorf("spec.usage.allBuckets is false but encryption key %s is used for encryption at rest on bucket %s", key.Name, bucket.Name))
		}
	}

	return errs
}

func checkEncryptionKeyUsageForClusterKeys(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey, cluster *couchbasev2.CouchbaseCluster) []error {
	keys, err := v.Abstraction.GetCouchbaseEncryptionKeys(cluster.Namespace, cluster.Spec.Security.EncryptionAtRest.Selector)
	if err != nil {
		return []error{err}
	}

	errs := []error{}
	for _, k := range keys.Items {
		if k.Spec.AutoGenerated != nil && k.Spec.AutoGenerated.EncryptWithKey == key.Name {
			errs = append(errs, fmt.Errorf("spec.usage.key is false but encryption key %s is used for encryption at rest on key %s", key.Name, k.Name))
		}
	}

	return errs
}

func validateAWSKeyCredentialsSecret(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey, credentialsSecret string) error {
	if credentialsSecret == "" {
		return nil
	}

	secret, found, err := v.Abstraction.GetSecret(key.Namespace, credentialsSecret)

	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("secret %s referenced by spec.awsKey.credentialsSecret must exist", credentialsSecret)
	}

	if secret.Data[constants.AWSCredentialsSecretKey] == nil {
		return fmt.Errorf("spec.awsKey.credentialsSecret must contain %s", constants.AWSCredentialsSecretKey)
	}

	return nil
}

func validateAWSKeyARN(keyARN string) error {
	keyARNRegex := regexp.MustCompile(`^arn:(aws|aws-us-gov|aws-cn):kms:\w+(?:-\w+)+:\d{12}:key\/[A-Za-z0-9-]+$`)
	if !keyARNRegex.MatchString(keyARN) {
		return fmt.Errorf("spec.awsKey.keyARN must match AWS KMS key ARN format")
	}

	return nil
}

func validateKMIPKey(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey) error {
	if key.Spec.KMIPKey == nil {
		return fmt.Errorf("spec.kmipKey is required when spec.keyType is KMIP")
	}

	if key.Spec.KMIPKey.ClientSecret == "" {
		return fmt.Errorf("spec.kmipKey.clientSecret is required when spec.keyType is KMIP")
	}

	secret, found, err := v.Abstraction.GetSecret(key.Namespace, key.Spec.KMIPKey.ClientSecret)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("secret %s referenced by spec.kmipKey.clientSecret must exist", key.Spec.KMIPKey.ClientSecret)
	}

	if secret.Data[constants.KMIPClientSecretCertKey] == nil {
		return fmt.Errorf("spec.kmipKey.clientSecret must contain %s", constants.KMIPClientSecretCertKey)
	}

	if secret.Data[constants.KMIPClientSecretKeyKey] == nil {
		return fmt.Errorf("spec.kmipKey.clientSecret must contain %s", constants.KMIPClientSecretKeyKey)
	}

	if secret.Data[constants.KMIPClientSecretPassphraseKey] == nil {
		return fmt.Errorf("spec.kmipKey.clientSecret must contain %s", constants.KMIPClientSecretPassphraseKey)
	}

	if err := util_x509.ValidateKMIPKeyAndPassphrase(secret.Data[constants.KMIPClientSecretKeyKey], secret.Data[constants.KMIPClientSecretPassphraseKey]); err != nil {
		return fmt.Errorf("spec.kmipKey.clientSecret must contain a valid key and passphrase: %w", err)
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

	// The index storage mode cannot be changed unless the cluster is in an index mismatch error state, which can be caused when starting a migration.
	if prev != curr && !current.IsInIndexMismatchErrorState() {
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

	fullyUpgraded, err := isFullyUpgraded(current)
	if err != nil {
		return err
	}

	// Not currently upgrading/migrating and cluster is fully upgraded, therefore we are starting an upgrade.
	isUpgrading := current.HasCondition(couchbasev2.ClusterConditionUpgrading)
	isMigrating := current.HasCondition(couchbasev2.ClusterConditionMigrating)

	if !isUpgrading && !isMigrating && fullyUpgraded {
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

	if imageVersion == "9.9.9" {
		// we have no idea what this is so we trust the user
		return true, nil
	}

	// Spec must match status
	if imageVersion != c.Status.CurrentVersion {
		return false, nil
	}

	// If MixedMode condition exists and is true, cluster is not fully upgraded
	if c.HasCondition(couchbasev2.ClusterConditionMixedMode) {
		return false, nil
	}

	return true, nil
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

func CheckChangeConstraintsCluster(v *types.Validator, prev, curr *couchbasev2.CouchbaseCluster) (bool, error) {
	err := annotations.Populate(&prev.Spec, prev.Annotations)
	if err != nil {
		return false, err
	}

	err = annotations.Populate(&curr.Spec, curr.Annotations)
	if err != nil {
		return false, err
	}

	if checkAnnotationSkipValidation(curr.Annotations) {
		return false, nil
	}

	// Check if only the status has updated
	if reflect.DeepEqual(prev.Spec, curr.Spec) && !reflect.DeepEqual(prev.Status, curr.Status) {
		return true, nil
	}

	var errs []error

	if err := checkConstraintPerServiceClassPDB(v, curr); err != nil {
		errs = append(errs, err)
	}

	// We check here if the change in default storage backend would result in a bucket backend
	// change for a bucket in a CB cluster < 7.6.0. The operator shouldn't actually attempt the
	// migration but we still want to keep the state of CB K8s resources and CB Server in sync
	// where we can.
	// This is a heavy check which is why we gate it behind if the default storage backend
	// actually changes
	if err := checkDefaultBucketStorageBackendConstraint(v, prev, curr); err != nil {
		errs = append(errs, err)
	}

	// If the cluster is in hibernation, we should prohibit any changes
	if err := checkChangeConstraintsHibernate(prev, curr); err != nil {
		errs = append(errs, err)
	}

	if err := checkChangeConstraintsMigration(v, prev, curr); err != nil {
		errs = append(errs, err)
	}

	if err := checkChangeConstraintsBucketMigratingAnnotation(prev, curr); err != nil {
		errs = append(errs, err)
	}

	if err := checkClusterUpgradePrerequisites(v, prev, curr); err != nil {
		errs = append(errs, err)
	}

	if err := checkChangeConstraintsSidecar(prev, curr); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return false, errors.CompositeValidationError(errs...)
	}

	return false, nil
}

func checkChangeConstraintsSidecar(prev, curr *couchbasev2.CouchbaseCluster) error {
	upgradeCondition := curr.HasCondition(couchbasev2.ClusterConditionUpgrading)
	mixedModeCondition := curr.HasCondition(couchbasev2.ClusterConditionMixedMode)

	if upgradeCondition || mixedModeCondition {
		if !reflect.DeepEqual(prev.Spec.Logging, curr.Spec.Logging) {
			return fmt.Errorf("server logging is immutable in mixed mode or during upgrade")
		}

		if !reflect.DeepEqual(prev.Spec.Networking.CloudNativeGateway, curr.Spec.Networking.CloudNativeGateway) {
			return fmt.Errorf("cloud native gateway spec cannot be changed in mixed mode or during upgrade")
		}
	}

	return nil
}

// nolint:gocognit
func checkDefaultBucketStorageBackendConstraint(v *types.Validator, prev, curr *couchbasev2.CouchbaseCluster) error {
	currDefault, _ := curr.GetDefaultBucketStorageBackend()
	prevDefault, _ := prev.GetDefaultBucketStorageBackend()
	if currDefault == prevDefault {
		return nil
	}

	// If the default storage backend is changing, we need to check the managed bucket's numVBuckets if the version is also changing
	// from pre 8.0.0 to post 8.0.0.
	checkVBuckets := false

	// If it's after 7.6.0 then backend can be changed
	currAbove76, err := curr.IsAtLeastVersion("7.6.0")
	if err != nil {
		return nil
	}

	if currAbove76 {
		currAbove80, err := curr.IsAtLeastVersion("8.0.0")
		if err != nil {
			return err
		}

		if currAbove80 {
			prevAbove80, err := prev.IsAtLeastVersion("8.0.0")
			if err != nil {
				return err
			}

			checkVBuckets = currAbove80 && !prevAbove80
		}
	}

	// If we don't need to check v buckets and the cluster is after 7.6.0, we can return early as the backend change is allowed.
	if !checkVBuckets && currAbove76 {
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

		if checkVBuckets {
			if err := checkNumVBucketsChangeConstraint(&bucket, &bucket, prev, curr); err != nil {
				errs = append(errs, err)
			}
		}

		// If the cluster is before 7.6.0, we need to check if the backend change would change the bucket backend.
		if !currAbove76 {
			prevBackend, _ := bucket.GetStorageBackend(prev)
			currBackend, _ := bucket.GetStorageBackend(curr)

			if prevBackend != currBackend {
				errs = append(errs, fmt.Errorf("cannot change default bucket backend as %s backend would change, backend changes are only supported for server version >= 7.6.0", bucket.Name))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkClusterUpgradePrerequisites(v *types.Validator, prev, curr *couchbasev2.CouchbaseCluster) error {
	if !curr.Spec.Buckets.Managed {
		return nil
	}

	if (curr.HasCondition(couchbasev2.ClusterConditionBucketMigration) || prev.HasCondition(couchbasev2.ClusterConditionBucketMigration)) && prev.Spec.Image != curr.Spec.Image {
		return fmt.Errorf("cannot upgrade cluster while bucket migration is in progress")
	}

	startVersion, err := k8sutil.CouchbaseVersion(prev.Spec.CouchbaseImage())
	if err != nil {
		return err
	}

	targetVersion, err := k8sutil.CouchbaseVersion(curr.Spec.CouchbaseImage())
	if err != nil {
		return err
	}

	if startBefore8, err := couchbaseutil.VersionBefore(startVersion, "8.0.0"); err != nil {
		return err
	} else if !startBefore8 {
		return nil
	}

	if targetAfter8, err := couchbaseutil.VersionAfter(targetVersion, "8.0.0"); err != nil {
		return err
	} else if !targetAfter8 {
		return nil
	}

	buckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(curr.Namespace, curr.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	memcachedBuckets := []string{}
	for _, bucket := range buckets.Items {
		memcachedBuckets = append(memcachedBuckets, bucket.Name)
	}

	if len(memcachedBuckets) > 0 {
		return fmt.Errorf("cluster has memcached buckets (%s) which are not supported in Couchbase Server version 8.0.0 and above, please remove them before upgrading", strings.Join(memcachedBuckets, ", "))
	}

	return nil
}

func checkClusterVersionUpgradePath(prev, curr *couchbasev2.CouchbaseCluster) error {
	oldImage := prev.Spec.CouchbaseImage()

	newImage := curr.Spec.CouchbaseImage()

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

	inPlaceUpgrade := (curr.GetUpgradeProcess() == couchbasev2.InPlaceUpgrade || curr.GetUpgradeProcess() == couchbasev2.DeltaRecovery)

	if inPlaceUpgrade && versionBefore72 && versionAfter72 {
		return fmt.Errorf("in-place upgrades not supported from pre-7.2.0 to versions 7.2.0 and above")
	}

	return couchbaseutil.CheckUpgradePath(oldVersion, newVersion)
}

// nolint:gocognit,gocyclo
func CheckChangeConstraintsBucket(v *types.Validator, prev, curr *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	var errs []error

	var warnings []string

	err := annotations.Populate(&prev.Spec, prev.Annotations)
	if err != nil {
		return nil, err
	}

	err = annotations.Populate(&curr.Spec, curr.Annotations)
	if err != nil {
		return nil, err
	}

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil, nil
	}

	if !prev.Spec.SampleBucket && curr.Spec.SampleBucket {
		errs = append(errs, fmt.Errorf("cao.couchbase.com/sampleBucket annotation cannot be added to an existing bucket"))
	}

	if prev.Spec.EnableCrossClusterVersioning != nil && *prev.Spec.EnableCrossClusterVersioning {
		if curr.Spec.EnableCrossClusterVersioning == nil || !*curr.Spec.EnableCrossClusterVersioning {
			errs = append(errs, fmt.Errorf("enableCrossClusterVersioning cannot be disabled once enabled"))
		}
	}

	clusters := []*couchbasev2.CouchbaseCluster{}
	if cluster != nil {
		clusters = []*couchbasev2.CouchbaseCluster{cluster}
	} else {
		bClusters, err := getBucketsRelatedClusters(v, curr)
		if err != nil {
			return nil, err
		}

		clusters = append(clusters, bClusters...)
	}

	for _, c := range clusters {
		if err := annotations.Populate(&c.Spec, c.Annotations); err != nil {
			return nil, err
		}

		// currBackend is the desired backend
		currBackend, _ := curr.GetStorageBackend(c)

		// prevBackend is the one currently in use. If it isn't explicitly set, we'll check the cluster status.
		prevBackend, explicit := prev.GetStorageBackend(c)
		if !explicit {
			if statusBackend := c.Status.GetBucketStorageBackendFromStatus(prev.GetCouchbaseName()); statusBackend != "" {
				prevBackend = statusBackend
			}
		}

		if prevBackend != currBackend || prev.IsSampleBucket() && !curr.IsSampleBucket() {
			if err := checkClusterValidForBucketMigration(v, curr, c); err != nil {
				errs = append(errs, err)
			}

			if prevBackend == couchbasev2.CouchbaseStorageBackendMagma && currBackend == couchbasev2.CouchbaseStorageBackendCouchstore {
				// Bucket history must have been disabled on the previous bucket spec. Cannot be done as part of the same change operation.
				if err := checkBucketHistoryDisabled(prev); err != nil {
					errs = append(errs, err)
				}

				warnings = append(warnings, "Changing bucket storagebackend could have associated collections with history retention enabled. This must be disabled to prevent errors.")
			}
		}

		after80, err := c.RunningVersion("8.0.0")
		if err != nil {
			return nil, err
		}

		if !after80 {
			continue
		}

		if !c.Spec.Buckets.EnableBucketMigrationRoutines {
			if curr.Spec.EvictionPolicy != prev.Spec.EvictionPolicy && curr.Spec.OnlineEvictionPolicyChange {
				errs = append(errs, fmt.Errorf("spec.evictionPolicy cannot be changed unless all referencing clusters have spec.buckets.enableBucketMigrationRoutines set to true"))
			}
		}

		if err := checkNumVBucketsChangeConstraint(prev, curr, c, c); err != nil {
			errs = append(errs, err)
		}
	}

	if errs != nil {
		return nil, errors.CompositeValidationError(errs...)
	}

	if len(warnings) > 0 {
		return warnings, nil
	}

	return nil, nil
}

// We need to validate numVBucket constraints when a bucket is changed, or if a cluster is upgraded from pre 8.0 to post 8.0 (due to default changes).
// If a cluster is changed, we need to run this against all managed buckets.
// If we are validating cluster changes, pass prevBucket and currBucket as the same bucket.
// If we are validating bucket changes, pass prevCluster and currCluster as the same cluster.
func checkNumVBucketsChangeConstraint(prevBucket, currBucket *couchbasev2.CouchbaseBucket, prevCluster, currCluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	prevVBuckets := prevBucket.GetNumVBuckets(prevCluster)
	currVBuckets := currBucket.GetNumVBuckets(currCluster)
	if prevVBuckets != currVBuckets {
		errs = append(errs, fmt.Errorf("spec.numVBuckets is immutable for bucket %s and cluster %s", currBucket.GetCouchbaseName(), currCluster.NamespacedName()))
	}

	// If the storage backend is magma and the numVBuckets is 1024, we need to check if the memory quota is greater than or equal to 1024Mi.
	storageBackend, _ := currBucket.GetStorageBackend(currCluster)
	if storageBackend == couchbasev2.CouchbaseStorageBackendMagma && currVBuckets == 1024 && currBucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
		errs = append(errs, fmt.Errorf("spec.memoryQuota must be greater than or equal to 1024Mi when numVBuckets is 1024 for bucket %s and cluster %s", currBucket.GetCouchbaseName(), currCluster.NamespacedName()))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckChangeConstraintsEphemeralBucket(v *types.Validator, prev, curr *couchbasev2.CouchbaseEphemeralBucket, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	err := annotations.Populate(&prev.Spec, prev.Annotations)
	if err != nil {
		return err
	}

	err = annotations.Populate(&curr.Spec, curr.Annotations)
	if err != nil {
		return err
	}

	if prev.Spec.EnableCrossClusterVersioning != nil && *prev.Spec.EnableCrossClusterVersioning {
		if curr.Spec.EnableCrossClusterVersioning == nil || !*curr.Spec.EnableCrossClusterVersioning {
			errs = append(errs, fmt.Errorf("enableCrossClusterVersioning cannot be disabled once enabled"))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// CheckImmutableFieldsEncryptionKey checks for field changes that are invalid.
func CheckImmutableFieldsEncryptionKey(prev, current *couchbasev2.CouchbaseEncryptionKey) error {
	if prev.Spec.KeyType != current.Spec.KeyType {
		return fmt.Errorf("encryption key type cannot be changed")
	}

	if current.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAWS && current.Spec.AWSKey != nil && prev.Spec.AWSKey != nil {
		if current.Spec.AWSKey.KeyARN != prev.Spec.AWSKey.KeyARN {
			return fmt.Errorf("encryption key ARN cannot be changed")
		}

		if current.Spec.AWSKey.KeyRegion != prev.Spec.AWSKey.KeyRegion {
			return fmt.Errorf("encryption key region cannot be changed")
		}
	}

	return nil
}

func checkBucketHistoryDisabled(bucket *couchbasev2.CouchbaseBucket) error {
	// History retention labels have been deprecated, bucket.Spec.HistoryRetentionSettings resource object should be used instead
	if bucket.Spec.HistoryRetentionSettings != nil && bucket.Spec.HistoryRetentionSettings.CollectionDefault != nil && !*bucket.Spec.HistoryRetentionSettings.CollectionDefault {
		return nil
	}

	return fmt.Errorf("spec.storageBackend can only be changed from magma to couchstore if spec.historyRetention.collectionHistoryDefault is first disabled on the bucket")
}

//nolint:gocognit
func checkClusterValidForBucketMigration(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.Buckets.EnableBucketMigrationRoutines {
		return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters have spec.buckets.enableBucketMigrationRoutines set to true")
	}

	if cluster.HasCondition(couchbasev2.ClusterConditionUpgrading) {
		return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters are not in an upgrade")
	}

	if cluster.HasCondition(couchbasev2.ClusterConditionMixedMode) {
		return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters are not running in mixed mode")
	}

	after76, err := cluster.RunningVersion("7.6.0")
	if err != nil {
		return err
	}

	if !after76 {
		return fmt.Errorf("spec.storageBackend backend can only be changed if all referencing clusters are version 7.6.0 or greater")
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

// CheckImmutableFieldsUser checks for field changes that are invalid for CouchbaseUser.
// The authDomain field is immutable because changing it would require deleting the existing
// user and creating a new one with a different authentication domain.
func CheckImmutableFieldsUser(prev, curr *couchbasev2.CouchbaseUser) error {
	var errs []error

	if checkAnnotationSkipValidation(curr.Annotations) {
		return nil
	}

	// authDomain cannot be changed as external and local users are separate entities
	if prev.Spec.AuthDomain != curr.Spec.AuthDomain {
		errs = append(errs, util.NewUpdateError("spec.authDomain", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkClusterConstraintMagmaStorageBackend checks if any buckets selected by the cluster have a magma storage backend
// and if so, checks if the cluster is able to support it.
func checkClusterConstraintMagmaStorageBackend(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Buckets aren't managed by Cluster.
	if !cluster.Spec.Buckets.Managed {
		return nil
	}

	couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	for _, cbBucket := range couchbaseBuckets.Items {
		if err := annotations.Populate(&cbBucket.Spec, cbBucket.Annotations); err != nil {
			return err
		}

		if err := validateBucketStorageBackendAndOnlineEvictionPolicyConstraints(v, &cbBucket, cluster); err != nil {
			return err
		}
	}

	return nil
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
	return checkAnnotationTrue(annotations, constants.AnnotationDisableAdmissionController)
}

func checkAnnotationSkipClusterNameLengthValidation(annotations map[string]string) bool {
	return checkAnnotationTrue(annotations, constants.AnnotationSkipClusterNameLengthValidation)
}

func checkAnnotationTrue(annotations map[string]string, annotation string) bool {
	value, found := annotations[annotation]
	if found {
		return strings.EqualFold(value, "true")
	}

	return false
}

//nolint:gocognit
func checkChangeConstraintsMigration(v *types.Validator, current, updated *couchbasev2.CouchbaseCluster) error {
	if current.Spec.Migration == nil && updated.Spec.Migration != nil {
		return fmt.Errorf("spec.migration cannot be added to a pre-existing cluster")
	}

	if current.Spec.Migration != nil {
		if updated.Spec.Migration != nil {
			if vc, err := checkForVersionChange(current, updated); err == nil && vc && current.Status.GetCondition(couchbasev2.ClusterConditionMigrating) != nil {
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

	if current.Spec.Migration != nil && updated.Spec.Migration == nil {
		// Put this here to allow the rest of our tests to run.
		// We can't import the validator package here as it would cause a circular dependency in the test.
		if v == nil {
			return nil
		}

		users, err := v.Abstraction.GetCouchbaseUsers(updated.Namespace, updated.Spec.Security.RBAC.Selector)
		if err != nil {
			return err
		}

		if len(users.Items) == 0 && updated.Spec.Security.RBAC.Managed {
			return fmt.Errorf("cannot remove migration spec: create user CRDs or disable user management")
		}

		if err := checkChangeConstraintsBucketLeavingMigrationMode(v, current, updated); err != nil {
			return err
		}
	}

	return nil
}

// checkChangeConstraintsBucketLeavingMigrationMode validates all CRD resources are valid
// for buckets that already exist in the cluster after a cluster migration has completed.
// This method should only be called when leaving migration mode.
func checkChangeConstraintsBucketLeavingMigrationMode(v *types.Validator, current *couchbasev2.CouchbaseCluster, updated *couchbasev2.CouchbaseCluster) error {
	if !current.Spec.Buckets.Managed {
		return nil
	}

	requestedCBuckets, requestedEBuckets, err := getClusterBucketsByType(v, updated)
	if err != nil {
		return err
	}

	if len(requestedCBuckets) == 0 && len(requestedEBuckets) == 0 {
		return fmt.Errorf("cannot remove migration spec: create bucket CRDs or disable bucket management")
	}

	errs := []error{}
	actualBuckets := current.Status.Buckets
	for _, statusBucket := range actualBuckets {
		switch statusBucket.BucketType {
		case couchbasev2.BucketTypeCouchbase:
			if requested, ok := requestedCBuckets[statusBucket.BucketName]; ok {
				actualAsCRD := util.BucketStatusToCouchbaseBucket(statusBucket)
				if err := CheckImmutableFieldsBucket(actualAsCRD, requested); err != nil {
					errs = append(errs, err)
				}

				if _, err = CheckChangeConstraintsBucket(v, actualAsCRD, requested, updated); err != nil {
					errs = append(errs, err)
				}
			}
		case couchbasev2.BucketTypeEphemeral:
			if requested, ok := requestedEBuckets[statusBucket.BucketName]; ok {
				actualAsCRD := util.BucketStatusToEphemeralBucket(statusBucket)
				if err := CheckImmutableFieldsEphemeralBucket(actualAsCRD, requested); err != nil {
					errs = append(errs, err)
				}

				if err := CheckChangeConstraintsEphemeralBucket(v, actualAsCRD, requested, updated); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
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
	currentImage := current.Spec.CouchbaseImage()

	updatedImage := updated.Spec.CouchbaseImage()

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

// checkClusterRBACConstraints checks RBAC settings constraints for a cluster. Every CouchbaseGroup which qualifies for the RBAC selector for the cluster will be checked.
func checkClusterRBACConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if err := checkClusterGroupRBACConstraints(v, cluster, nil); err != nil {
		return err
	}

	if err := checkClusterUserRbacConstraints(v, cluster, nil); err != nil {
		return err
	}

	return nil
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

// checkClusterGroupRBACConstraints checks RBAC role constraints based on server version.
func checkClusterGroupRBACConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, group *couchbasev2.CouchbaseGroup) error {
	if !cluster.Spec.Security.RBAC.Managed {
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

	// Check if we're on 8.0+ for role validation
	is8Plus, err := cluster.RunningVersion("8.0.0")
	if err != nil {
		return err
	}

	// Check if we're on 7.0+ for security_admin validation
	is7Plus, err := isVersion7OrAbove(cluster)
	if err != nil {
		return err
	}

	for _, g := range couchbaseGroups.Items {
		for _, r := range g.Spec.Roles {
			// security_admin is invalid on 7.0+ but valid again on 8.0+
			if r.Name == couchbasev2.RoleSecurityAdmin && is7Plus && !is8Plus {
				return fmt.Errorf("security_admin role is configured in group %s and cannot be used with Couchbase Server 7.0+", g.Name)
			}

			// New roles on older versions should error
			if !is8Plus {
				if r.Name == couchbasev2.RoleUserAdminLocal || r.Name == couchbasev2.RoleUserAdminExternal ||
					r.Name == couchbasev2.RoleReadOnlySecurityAdmin || r.Name == couchbasev2.RoleQueryListIndex ||
					r.Name == couchbasev2.RoleQueryManageSystemCatalog || r.Name == couchbasev2.RoleApplicationTelemetryWriter {
					return fmt.Errorf("role %s in group %s requires Couchbase Server 8.0+", r.Name, g.Name)
				}
			}
		}
	}

	return nil
}

func checkChangeConstraintsBucketMigratingAnnotation(prev, current *couchbasev2.CouchbaseCluster) error {
	if prev.Spec.Buckets.EnableBucketMigrationRoutines != current.Spec.Buckets.EnableBucketMigrationRoutines {
		if cond := prev.Status.GetCondition(couchbasev2.ClusterConditionBucketMigration); cond != nil && cond.Status == v1.ConditionTrue {
			return fmt.Errorf("spec.buckets.enableBucketMigrationRoutines cannot be changed while a bucket migration is taking place")
		}
	}

	return nil
}

// checkConstraintArbiterOverAdminService returns a warning if the AdminService is used, recommending
// using an arbiter server class instead.
func checkConstraintArbiterOverAdminService(_ *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	for _, server := range cluster.Spec.Servers {
		for _, service := range server.Services {
			if service == couchbasev2.AdminService {
				return []string{fmt.Sprintf("Server class %s uses the admin service - it is recommended to use an arbiter server class ('[]') instead", server.Name)}, nil
			}
		}
	}

	return nil, nil
}

// checkConstraintDeprecatedRBACRoles returns warnings for deprecated RBAC roles on 8.0+.
func checkConstraintDeprecatedRBACRoles(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if !cluster.Spec.Security.RBAC.Managed {
		return nil, nil
	}

	is8Plus, err := cluster.IsAtLeastVersion("8.0.0")
	if err != nil {
		return nil, err
	}

	if !is8Plus {
		return nil, nil
	}

	groups, err := v.Abstraction.GetCouchbaseGroups(cluster.Namespace, cluster.Spec.Security.RBAC.Selector)
	if err != nil {
		return nil, err
	}

	var warnings []string

	for _, g := range groups.Items {
		for _, r := range g.Spec.Roles {
			//nolint:staticcheck // Deprecated roles referenced intentionally for upgrade/migration warnings.
			if r.Name == couchbasev2.RoleSecurityAdminLocal || r.Name == couchbasev2.RoleSecurityAdminExternal {
				warnings = append(warnings, fmt.Sprintf("Group %s uses deprecated role %s - will be automatically migrated to security_admin + user_admin_*", g.Name, r.Name))
			}
		}
	}

	return warnings, nil
}

func checkClusterBackupConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.Backup.Managed {
		return nil
	}

	if cluster.Spec.Backup.Image == "" {
		return fmt.Errorf("spec.backup.image cannot be empty when spec.backup.managed is true")
	}

	return nil
}

func checkConstraintsIndexerSettings(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.ClusterSettings.Indexer == nil {
		return nil
	}

	if cluster.Spec.ClusterSettings.Indexer.EnablePageBloomFilter {
		pageBloomFilterSupported, err := cluster.IsAtLeastVersion("7.1.0")
		if err != nil {
			return err
		}

		if !pageBloomFilterSupported {
			return fmt.Errorf("spec.cluster.indexer.enablePageBloomFilter requires Couchbase Server version 7.1.0 or later")
		}
	}

	if cluster.Spec.ClusterSettings.Indexer.EnableShardAffinity {
		shardAffinitySupported, err := cluster.IsAtLeastVersion("7.6.0")
		if err != nil {
			return err
		}

		if !shardAffinitySupported {
			return fmt.Errorf("spec.cluster.indexer.enableShardAffinity requires Couchbase Server version 7.6.0 or later")
		}
	}

	if cluster.Spec.ClusterSettings.Indexer.DeferBuild {
		deferBuildSupported, err := cluster.IsAtLeastVersion("8.0.0")
		if err != nil {
			return err
		}

		if !deferBuildSupported {
			return fmt.Errorf("spec.cluster.indexer.deferBuild requires Couchbase Server version 8.0.0 or later")
		}
	}

	if cluster.Spec.ClusterSettings.Indexer.NumberOfReplica > 0 {
		totalIndexPodsAvailable := 0

		for _, serverClass := range cluster.Spec.Servers {
			if slices.Contains(serverClass.Services, couchbasev2.IndexService) {
				totalIndexPodsAvailable += serverClass.Size
			}
		}

		if cluster.Spec.ClusterSettings.Indexer.NumberOfReplica >= totalIndexPodsAvailable {
			return fmt.Errorf("spec.cluster.indexer.numReplica %d cannot be greater or equal to the number of index pods %d", cluster.Spec.ClusterSettings.Indexer.NumberOfReplica, totalIndexPodsAvailable)
		}
	}

	return nil
}

func checkConstraintsDataSettings(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	dataSettings := cluster.Spec.ClusterSettings.Data
	if dataSettings == nil {
		return nil
	}

	atLeast80, err := cluster.IsAtLeastVersion("8.0.0")
	if err != nil {
		return err
	}

	if atLeast80 {
		return nil
	}

	if dataSettings.DiskUsageLimit != nil {
		return fmt.Errorf("spec.cluster.data.diskUsageLimit can only be set for Couchbase Server 8.0.0+")
	}

	if dataSettings.TCPUserTimeout != nil {
		return fmt.Errorf("spec.cluster.data.tcpUserTimeout can only be set for Couchbase Server 8.0.0+")
	}

	if dataSettings.TCPKeepAliveProbes != nil {
		return fmt.Errorf("spec.cluster.data.tcpKeepAliveProbes can only be set for Couchbase Server 8.0.0+")
	}

	if dataSettings.TCPKeepAliveInterval != nil {
		return fmt.Errorf("spec.cluster.data.tcpKeepAliveInterval can only be set for Couchbase Server 8.0.0+")
	}

	if dataSettings.TCPKeepAliveIdle != nil {
		return fmt.Errorf("spec.cluster.data.tcpKeepAliveIdle can only be set for Couchbase Server 8.0.0+")
	}

	return nil
}

// checkConstraintsPasswordPolicy validates passwordPolicy fields that are only supported on 8.0.0+.
func checkConstraintsPasswordPolicy(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Security.PasswordPolicy == nil {
		return nil
	}

	atleast80, err := cluster.IsAtLeastVersion("8.0.0")
	if err != nil {
		return err
	}

	if !atleast80 {
		pp := cluster.Spec.Security.PasswordPolicy
		if pp.RequirePasswordResetOnPolicyChange != nil {
			return fmt.Errorf("spec.security.passwordPolicy.requirePasswordResetOnPolicyChange requires Couchbase Server version 8.0.0 or later")
		}

		if len(pp.PasswordResetOnPolicyChangeExemptUsers) > 0 {
			return fmt.Errorf("spec.security.passwordPolicy.passwordResetOnPolicyChangeExemptUsers requires Couchbase Server version 8.0.0 or later")
		}
	}

	return nil
}

func checkConstraintMemcachedBucketDeprecated(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	memcachedDeprecated, err := cluster.IsAtLeastVersion("8.0.0")
	if err != nil {
		return nil, err
	}

	if !memcachedDeprecated {
		return nil, nil
	}

	buckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, err
	}

	warnings := []string{}
	for _, bucket := range buckets.Items {
		warnings = append(warnings, util.GenerateMemcachedBucketWarning(cluster, &bucket))
	}

	return warnings, nil
}

func checkServerClassImageDeprecated(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	for _, serverClass := range cluster.Spec.Servers {
		if serverClass.Image == "" {
			continue
		}

		return []string{fmt.Sprintf("spec.servers.image is deprecated use spec.image and spec.upgrade for granular upgrades instead")}, nil
	}

	return nil, nil
}

func checkConstraintsDeprecatedNetworkingOptions(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if cluster.Spec.Networking.AddressFamily == nil {
		return nil, nil
	}

	var warnings []string

	// Map of deprecated address families to their replacements
	deprecatedToReplacement := map[couchbasev2.AddressFamily]couchbasev2.AddressFamily{
		couchbasev2.IPv4: couchbasev2.IPv4Only,
		couchbasev2.IPv6: couchbasev2.IPv6Only,
	}

	addressFamily := *cluster.Spec.Networking.AddressFamily
	if replacement, isDeprecated := deprecatedToReplacement[addressFamily]; isDeprecated {
		warning := fmt.Sprintf("%s should be used in place of %s for spec.networking.addressFamily. %s is deprecated and will be removed in a future release", replacement, addressFamily, addressFamily)
		warnings = append(warnings, warning)
	}

	return warnings, nil
}

func checkConstraintDeprecatedAnnotations(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	if len(cluster.Annotations) == 0 {
		return nil, nil
	}

	deprecatedAnnotations := map[string]string{
		"cao.couchbase.com/buckets.enableBucketMigrationRoutines": "spec.buckets.enableBucketMigrationRoutines",
	}

	for annotation, newField := range deprecatedAnnotations {
		if _, ok := cluster.Annotations[annotation]; ok {
			return []string{fmt.Sprintf("%s is deprecated, please use %s field instead", annotation, newField)}, nil
		}
	}

	return nil, nil
}

func checkConstraintEncryptionKeys(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.Security.EncryptionAtRest == nil || !cluster.Spec.Security.EncryptionAtRest.Managed {
		return nil
	}

	if encryptionAtRestSupported, err := cluster.IsAtLeastVersion("8.0.0"); err != nil {
		return err
	} else if !encryptionAtRestSupported {
		return fmt.Errorf("encryption at rest requires Couchbase Server version 8.0.0 or later")
	}

	// Encryption at rest cannot be enabled in mixed mode as it requires all nodes to be running Couchbase Server 8.0.0+
	if cluster.HasCondition(couchbasev2.ClusterConditionMixedMode) {
		return fmt.Errorf("encryption at rest cannot be enabled while cluster is in mixed mode")
	}

	encryptionAtRest := cluster.Spec.Security.EncryptionAtRest

	// Get all encryption keys using the selector
	encryptionKeys, err := v.Abstraction.GetCouchbaseEncryptionKeys(cluster.Namespace, encryptionAtRest.Selector)
	if err != nil {
		return fmt.Errorf("failed to get encryption keys: %w", err)
	}

	// Create a map of key names to their specs for easy lookup
	keyMap := make(map[string]*couchbasev2.CouchbaseEncryptionKey)

	for i := range encryptionKeys.Items {
		key := &encryptionKeys.Items[i]
		keyMap[key.Name] = key
	}

	var errs []error

	// Check configuration encryption
	if err := validateEncryptionSettings(keyMap, encryptionAtRest.Configuration, constants.EncryptionAtRestUsageConfiguration); err != nil {
		errs = append(errs, fmt.Errorf("configuration encryption key validation failed: %w", err))
	}

	// Check audit encryption
	if err := validateEncryptionSettings(keyMap, encryptionAtRest.Audit, constants.EncryptionAtRestUsageAudit); err != nil {
		errs = append(errs, fmt.Errorf("audit encryption key validation failed: %w", err))
	}

	// Check log encryption
	if err := validateEncryptionSettings(keyMap, encryptionAtRest.Log, constants.EncryptionAtRestUsageLog); err != nil {
		errs = append(errs, fmt.Errorf("log encryption key validation failed: %w", err))
	}

	// Currently, encryption at rest of logs will break fluent-bit log streaming.
	loggingEnabled := cluster.Spec.Logging.Server != nil && cluster.Spec.Logging.Server.Enabled
	if encryptionAtRest.Log != nil && encryptionAtRest.Log.Enabled && loggingEnabled {
		errs = append(errs, fmt.Errorf("encryption at rest of logs will break fluent-bit log streaming"))
	}

	// Check configuration encryption at rest constraints
	if err := validateConfigurationEncryptionAtRest(cluster, keyMap); err != nil {
		errs = append(errs, fmt.Errorf("configuration encryption at rest validation failed: %w", err))
	}

	// Check for circular dependencies between encryption keys
	if err := validateEncryptionKeyCircularDependencies(keyMap); err != nil {
		errs = append(errs, fmt.Errorf("encryption key circular dependency validation failed: %w", err))
	}

	// Check the encryption keyys used on buckets all exist and have the appropriate usage settings
	if err := validateEncryptionKeysUsedOnBuckets(v, cluster, keyMap); err != nil {
		errs = append(errs, fmt.Errorf("encryption key used on bucket validation failed: %w", err))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// validateEncryptionKey validates that an encryption key exists in the map and has the appropriate usage setting.
func validateEncryptionSettings(keyMap map[string]*couchbasev2.CouchbaseEncryptionKey, config *couchbasev2.EncryptionAtRestUsageConfiguration, usageType string) error {
	// Check if configuration is nil or not enabled
	if config == nil || !config.Enabled {
		return nil
	}

	// Check rotation interval is at least 7 days if set
	if config.RotationInterval != nil {
		rotationInterval := config.RotationInterval.Duration
		if rotationInterval.Hours() < 7*24 {
			return fmt.Errorf("rotation interval must be at least 7 days, got %v", rotationInterval)
		}
	}

	// Check key lifetime is at least 30 days if set
	if config.KeyLifetime != nil {
		keyLifetime := config.KeyLifetime.Duration
		if keyLifetime.Hours() < 30*24 {
			return fmt.Errorf("key lifetime must be at least 30 days, got %v", keyLifetime)
		}
	}

	// If key name is empty, master password will be used for encryption - key validation needed
	if config.KeyName == "" {
		return nil
	}

	// Get the encryption key from the map
	encryptionKey, found := keyMap[config.KeyName]
	if !found {
		return fmt.Errorf("encryption key %s does not exist or is not selected by the encryption at rest selector", config.KeyName)
	}

	// Check the appropriate usage setting based on the usage type
	usage := encryptionKey.Spec.Usage

	switch usageType {
	case constants.EncryptionAtRestUsageConfiguration:
		if !usage.Configuration {
			return fmt.Errorf("encryption key %s does not have configuration usage enabled", config.KeyName)
		}
	case constants.EncryptionAtRestUsageAudit:
		if !usage.Audit {
			return fmt.Errorf("encryption key %s does not have audit usage enabled", config.KeyName)
		}
	case constants.EncryptionAtRestUsageLog:
		if !usage.Log {
			return fmt.Errorf("encryption key %s does not have log usage enabled", config.KeyName)
		}
	default:
		return fmt.Errorf("invalid usage type: %s", usageType)
	}

	return nil
}

// validateConfigurationEncryptionAtRest checks that if configuration Encryption At rest is disabled
// then there are no existing autoGenerated encryption keys that use masterPassword encryption.
func validateConfigurationEncryptionAtRest(cluster *couchbasev2.CouchbaseCluster, keyMap map[string]*couchbasev2.CouchbaseEncryptionKey) error {
	encryptionAtRest := cluster.Spec.Security.EncryptionAtRest

	// Check if configuration encryption is enabled
	configEnabled := encryptionAtRest.Configuration != nil && encryptionAtRest.Configuration.Enabled

	// Configuration encryption is enabled, no need to check
	if configEnabled {
		return nil
	}

	var invalidKeys []string

	for _, key := range keyMap {
		// Check if it's an auto-generated key
		if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated {
			// Check if it uses master password encryption (no EncryptWithKey specified)
			if key.Spec.AutoGenerated == nil || key.Spec.AutoGenerated.EncryptWithKey == "" {
				invalidKeys = append(invalidKeys, key.Name)
			}
		}
	}

	if len(invalidKeys) > 0 {
		return fmt.Errorf("auto-generated encryption keys use master password encryption and require configuration encryption at rest to be enabled: %s", strings.Join(invalidKeys, ", "))
	}

	return nil
}

// validateEncryptionKeyCircularDependencies checks for circular dependencies in encryption key relationships.
func validateEncryptionKeyCircularDependencies(keyMap map[string]*couchbasev2.CouchbaseEncryptionKey) error {
	// Build dependency graph: key -> key that encrypts it
	dependencies := make(map[string]string)

	for keyName, key := range keyMap {
		// Only auto-generated keys can have encryptWithKey dependencies
		if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated &&
			key.Spec.AutoGenerated != nil &&
			key.Spec.AutoGenerated.EncryptWithKey != "" {
			dependencies[keyName] = key.Spec.AutoGenerated.EncryptWithKey
		}
	}

	// If no dependencies exist, no cycles are possible
	if len(dependencies) == 0 {
		return nil
	}

	// Use DFS to detect cycles
	visited := make(map[string]bool)
	inPath := make(map[string]bool)

	for keyName := range dependencies {
		if !visited[keyName] {
			if cycle := detectCycleDFS(keyName, dependencies, keyMap, visited, inPath, []string{}); cycle != nil {
				return fmt.Errorf("circular dependency detected in encryption keys: %s", formatCycle(cycle))
			}
		}
	}

	return nil
}

// detectCycleDFS performs depth-first search to detect cycles in the dependency graph.
func detectCycleDFS(keyName string, dependencies map[string]string, keyMap map[string]*couchbasev2.CouchbaseEncryptionKey,
	visited, inPath map[string]bool, path []string) []string {
	visited[keyName] = true
	inPath[keyName] = true

	path = append(path, keyName)

	// Check if this key has a dependency
	if encryptingKey, hasDependency := dependencies[keyName]; hasDependency {
		if inPath[encryptingKey] {
			// Found a cycle - find the cycle start and return the cycle path
			cycleStart := -1

			for i, key := range path {
				if key == encryptingKey {
					cycleStart = i
					break
				}
			}

			if cycleStart >= 0 {
				cycle := make([]string, 0, len(path[cycleStart:])+1)
				cycle = append(cycle, path[cycleStart:]...)
				cycle = append(cycle, encryptingKey) // Complete the cycle

				return cycle
			}
		} else if !visited[encryptingKey] {
			// Continue DFS if the encrypting key is also in our key map
			if _, exists := keyMap[encryptingKey]; exists {
				if cycle := detectCycleDFS(encryptingKey, dependencies, keyMap, visited, inPath, path); cycle != nil {
					return cycle
				}
			}
		}
	}

	inPath[keyName] = false

	return nil
}

// formatCycle formats a cycle path into a readable string.
func formatCycle(cycle []string) string {
	if len(cycle) == 0 {
		return ""
	}

	return strings.Join(cycle, " -> ")
}

func validateEncryptionKeysUsedOnBuckets(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, keyMap map[string]*couchbasev2.CouchbaseEncryptionKey) error {
	var errs []error

	couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	// First check that the bucket is using an encryption key that exists
	for _, bucket := range couchbaseBuckets.Items {
		if bucket.Spec.EncryptionAtRest == nil || bucket.Spec.EncryptionAtRest.KeyName == "" {
			continue
		}

		if bucket.Spec.EncryptionAtRest.RotationInterval != nil {
			rotationInterval := bucket.Spec.EncryptionAtRest.RotationInterval.Duration
			if rotationInterval.Hours() < 7*24 {
				return fmt.Errorf("rotation interval must be at least 7 days, got %v", rotationInterval)
			}
		}

		// Check key lifetime is at least 30 days if set
		if bucket.Spec.EncryptionAtRest.KeyLifetime != nil {
			keyLifetime := bucket.Spec.EncryptionAtRest.KeyLifetime.Duration
			if keyLifetime.Hours() < 30*24 {
				return fmt.Errorf("key lifetime must be at least 30 days, got %v", keyLifetime)
			}
		}

		key, found := keyMap[bucket.Spec.EncryptionAtRest.KeyName]
		if !found {
			errs = append(errs, fmt.Errorf("encryption key %s does not exist or is not selected by the encryption at rest selector", bucket.Spec.EncryptionAtRest.KeyName))
			continue
		}

		if !key.Spec.Usage.AllBuckets {
			errs = append(errs, fmt.Errorf("encryption key %s does not have bucket usage enabled", bucket.Spec.EncryptionAtRest.KeyName))
			continue
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// nolint:gocognit
func CheckDeleteConstraintsEncryptionKey(v *types.Validator, key *couchbasev2.CouchbaseEncryptionKey) error {
	// Get the clusters that are using the encryption key
	clusters, err := v.Abstraction.GetCouchbaseClusters(key.Namespace)
	if err != nil {
		return err
	}

	var errs []error

	for _, cluster := range clusters.Items {
		if encryptionAtRestSupported, err := cluster.IsAtLeastVersion("8.0.0"); err != nil {
			return err
		} else if !encryptionAtRestSupported {
			continue
		}

		if cluster.Spec.Security.EncryptionAtRest == nil || !cluster.Spec.Security.EncryptionAtRest.Managed {
			continue
		}

		// If the encryption key is not selected by the cluster then we can skip the checks
		if cluster.Spec.Security.EncryptionAtRest.Selector != nil {
			encryptionKeySelector, err := metav1.LabelSelectorAsSelector(cluster.Spec.Security.EncryptionAtRest.Selector)
			if err != nil {
				return err
			}

			if !encryptionKeySelector.Matches(labels.Set(key.Labels)) {
				continue
			}
		}

		if cluster.Spec.Security.EncryptionAtRest.Configuration != nil && cluster.Spec.Security.EncryptionAtRest.Configuration.KeyName == key.Name {
			errs = append(errs, fmt.Errorf("encryption key %s is currently being used for configuration encryption on cluster %s", key.Name, cluster.Name))
		}

		if cluster.Spec.Security.EncryptionAtRest.Audit != nil && cluster.Spec.Security.EncryptionAtRest.Audit.KeyName == key.Name {
			errs = append(errs, fmt.Errorf("encryption key %s is currently being used for audit encryption on cluster %s", key.Name, cluster.Name))
		}

		if cluster.Spec.Security.EncryptionAtRest.Log != nil && cluster.Spec.Security.EncryptionAtRest.Log.KeyName == key.Name {
			errs = append(errs, fmt.Errorf("encryption key %s is currently being used for log encryption on cluster %s", key.Name, cluster.Name))
		}

		if !cluster.Spec.Buckets.Managed {
			continue
		}

		couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
		if err != nil {
			return err
		}

		for _, bucket := range couchbaseBuckets.Items {
			if bucket.Spec.EncryptionAtRest != nil && bucket.Spec.EncryptionAtRest.KeyName == key.Name {
				errs = append(errs, fmt.Errorf("encryption key %s is currently being used for encryption at rest on bucket %s on cluster %s", key.Name, bucket.Name, cluster.Name))
			}
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func getBucketsRelatedClusters(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) ([]*couchbasev2.CouchbaseCluster, error) {
	clusters, err := v.Abstraction.GetCouchbaseClusters(bucket.Namespace)
	if err != nil {
		return nil, err
	}

	relatedClusters := []*couchbasev2.CouchbaseCluster{}
	for _, cluster := range clusters.Items {
		if !cluster.Spec.Buckets.Managed {
			continue
		}

		if cluster.Spec.Buckets.Selector == nil {
			relatedClusters = append(relatedClusters, &cluster)
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(cluster.Spec.Buckets.Selector)
		if err != nil {
			return nil, err
		}

		if selector.Matches(labels.Set(bucket.Labels)) {
			relatedClusters = append(relatedClusters, &cluster)
		}
	}

	return relatedClusters, nil
}

func getEphemeralBucketsRelatedClusters(v *types.Validator, bucket *couchbasev2.CouchbaseEphemeralBucket) ([]*couchbasev2.CouchbaseCluster, error) {
	clusters, err := v.Abstraction.GetCouchbaseClusters(bucket.Namespace)
	if err != nil {
		return nil, err
	}

	relatedClusters := []*couchbasev2.CouchbaseCluster{}
	for _, cluster := range clusters.Items {
		if !cluster.Spec.Buckets.Managed {
			continue
		}

		if cluster.Spec.Buckets.Selector == nil {
			relatedClusters = append(relatedClusters, &cluster)
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(cluster.Spec.Buckets.Selector)
		if err != nil {
			return nil, err
		}

		if selector.Matches(labels.Set(bucket.Labels)) {
			relatedClusters = append(relatedClusters, &cluster)
		}
	}

	return relatedClusters, nil
}

func getUserRelatedClusters(v *types.Validator, user *couchbasev2.CouchbaseUser) ([]*couchbasev2.CouchbaseCluster, error) {
	clusters, err := v.Abstraction.GetCouchbaseClusters(user.Namespace)
	if err != nil {
		return nil, err
	}

	relatedClusters := []*couchbasev2.CouchbaseCluster{}
	for _, cluster := range clusters.Items {
		if !cluster.Spec.Security.RBAC.Managed {
			continue
		}

		selector := labels.Everything()
		if cluster.Spec.Security.RBAC.Selector != nil {
			selector, err = metav1.LabelSelectorAsSelector(cluster.Spec.Security.RBAC.Selector)
			if err != nil {
				return nil, err
			}
		}

		if !selector.Matches(labels.Set(user.Labels)) {
			continue
		}

		relatedClusters = append(relatedClusters, &cluster)
	}

	return relatedClusters, nil
}

//nolint:gocognit
func validateBucketStorageBackendAndOnlineEvictionPolicyConstraints(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	clusters := []*couchbasev2.CouchbaseCluster{}
	if cluster != nil {
		clusters = []*couchbasev2.CouchbaseCluster{cluster}
	} else {
		bClusters, err := getBucketsRelatedClusters(v, bucket)
		if err != nil {
			return err
		}

		clusters = append(clusters, bClusters...)
	}

	// If we don't find any clusters that will manage the bucket, but the storage backend is explicitly set on the bucket,
	// we can still validate the bucket settings that are not dependent on the cluster version.
	if len(clusters) == 0 && bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackendMagma {
		if err := checkMagmaBucketRequiredSettings(bucket); err != nil {
			return err
		}
	}

	if len(clusters) == 0 && bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackendCouchstore {
		if err := checkBucketHistoryRetentionSettings(bucket, couchbasev2.CouchbaseStorageBackendCouchstore); err != nil {
			return err
		}
	}

	for _, c := range clusters {
		if err := annotations.Populate(&c.Spec, c.Annotations); err != nil {
			return err
		}

		// There are a number of ways a bucket storagebackend can be set. By order of precedence:
		// 1. Explicitly set in the bucket spec
		// 2. Cluster default overridden by the defaultStorageBackend annotation
		// 3. Defaulted by the server version (magma for 8.0.0+, couchstore for earlier)
		// The operator will use GetStorageBackend, which in some cases will return a different value for compatibility. Therefore we'll run these checks
		// if it's set specifically on the bucket as well so we can validate values that the operator would override.
		storageBackend, _ := bucket.GetStorageBackend(c)

		if err := checkBucketHistoryRetentionSettings(bucket, storageBackend); err != nil {
			return err
		}

		if storageBackend == couchbasev2.CouchbaseStorageBackendMagma || bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackendMagma {
			if err := checkMagmaBucketRequiredSettings(bucket); err != nil {
				return err
			}

			if err := checkMagmaBucketClusterVersionSettings(v, c, bucket); err != nil {
				return err
			}
		} else if bucket.Spec.NumVBuckets != nil && *bucket.Spec.NumVBuckets != 1024 {
			// We need to allow numVBuckets to be set for couchstore buckets to aid with migration from couchstore to magma.
			// This isn't used by anything in the cluster, so we can allow it to be set to 1024 for couchstore buckets.
			return fmt.Errorf("spec.numVBuckets can only be set to 1024 for couchstore buckets for cluster: %s", c.NamespacedName())
		}

		atLeast80, err := c.IsAtLeastVersion("8.0.0")
		if err != nil {
			return err
		}

		if !atLeast80 {
			if bucket.Spec.OnlineEvictionPolicyChange != false {
				return fmt.Errorf("spec.onlineEvictionPolicyChange can only be set for Couchbase Server 8.0.0+")
			}
		}
	}

	return nil
}

func checkUpgradeOrderEntriesExist(t couchbasev2.UpgradeOrderType, cluster *couchbasev2.CouchbaseCluster) []error {
	var errs []error

	servers := cluster.Spec.Servers

	switch t {
	case couchbasev2.UpgradeOrderTypeServices:
		servicesMap := make(map[string]bool)

		for _, server := range servers {
			if len(server.Services) == 0 {
				servicesMap["arbiter"] = true
			}

			for _, item := range server.Services {
				servicesMap[item.String()] = true
			}
		}

		for _, uOService := range cluster.Spec.Upgrade.UpgradeOrder {
			if !servicesMap[uOService] {
				errs = append(errs, fmt.Errorf("upgrade order contains service %s not found in spec.servers", uOService))
			}
		}
	case couchbasev2.UpgradeOrderTypeServerClasses:
		var serverNames []string

		for _, server := range servers {
			serverNames = append(serverNames, server.Name)
		}

		for _, uOServer := range cluster.Spec.Upgrade.UpgradeOrder {
			if !slices.Contains(serverNames, uOServer) {
				errs = append(errs, fmt.Errorf("upgrade order contains serverclass %s not found in spec.servers", uOServer))
			}
		}
	case couchbasev2.UpgradeOrderTypeServerGroups:
		for _, uOServerGroup := range cluster.Spec.Upgrade.UpgradeOrder {
			if !slices.Contains(cluster.Spec.ServerGroups, uOServerGroup) {
				errs = append(errs, fmt.Errorf("upgrade order contains servergroup %s not found in the cluster spec", uOServerGroup))
			}
		}
	case couchbasev2.UpgradeOrderTypeNodes:
	// We don't generate node names from the spec as they are ephemeral, so we'll skip validation here for now.
	default:
		errs = append(errs, fmt.Errorf("unknown upgrade order type %v", t))
	}

	return errs
}

// There are several 8.0 only fields that were added with kubebuilder:default annotations. We can't remove these from the CRD's without breaking backwards compatibility,
// and we can't warn if they are the defaults or we'll barrage users with warnings.
// The compromise is to warn if the fields have been changed from the default.
// checkBucketCRDFieldsForNonDefaultUnsupportedFields handles the bucket fields.
func checkBucketCRDFieldsForNonDefaultUnsupportedFields(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	clusters := []*couchbasev2.CouchbaseCluster{}
	if cluster != nil {
		clusters = []*couchbasev2.CouchbaseCluster{cluster}
	} else {
		bClusters, err := getBucketsRelatedClusters(v, bucket)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, bClusters...)
	}

	warnings := []string{}
	for _, c := range clusters {
		atLeast80, err := c.IsAtLeastVersion("8.0.0")
		if err != nil {
			return nil, err
		}

		if atLeast80 {
			continue
		}

		if err := annotations.Populate(&c.Spec, c.Annotations); err != nil {
			return nil, err
		}

		fields := []string{}

		if bucket.Spec.AccessScannerEnabled != nil && !*bucket.Spec.AccessScannerEnabled {
			fields = append(fields, "spec.accessScannerEnabled")
		}

		if bucket.Spec.MemoryLowWatermark != nil && *bucket.Spec.MemoryLowWatermark != 75 {
			fields = append(fields, "spec.memoryLowWatermark")
		}

		if bucket.Spec.MemoryHighWatermark != nil && *bucket.Spec.MemoryHighWatermark != 85 {
			fields = append(fields, "spec.memoryHighWatermark")
		}

		if warmupBehavior := bucket.Spec.WarmupBehavior; warmupBehavior != couchbasev2.CouchbaseBucketWarmupBehaviorBackground {
			fields = append(fields, "spec.warmupBehavior")
		}

		for _, field := range fields {
			warnings = append(warnings, fmt.Sprintf("CouchbaseBucket %s has been configured for cluster %s. This will be ignored for Couchbase Server versions below 8.0.0.", field, c.NamespacedName()))
		}
	}

	return warnings, nil
}

func checkBucketUnsupportedFields(v *types.Validator, bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	clusters := []*couchbasev2.CouchbaseCluster{}
	if cluster != nil {
		clusters = []*couchbasev2.CouchbaseCluster{cluster}
	} else {
		bClusters, err := getBucketsRelatedClusters(v, bucket)
		if err != nil {
			return err
		}

		clusters = append(clusters, bClusters...)
	}

	for _, c := range clusters {
		if err := annotations.Populate(&c.Spec, c.Annotations); err != nil {
			return err
		}

		atLeast80, err := c.IsAtLeastVersion("8.0.0")
		if err != nil {
			return err
		}

		if atLeast80 {
			continue
		}

		if bucket.Spec.DurabilityImpossibleFallback != "" {
			return fmt.Errorf("spec.durabilityImpossibleFallback can only be set for Couchbase Server 8.0.0+")
		}
	}

	return nil
}

func checkClusterUserRbacConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, user *couchbasev2.CouchbaseUser) error {
	if !cluster.Spec.Security.RBAC.Managed {
		return nil
	}

	users := []*couchbasev2.CouchbaseUser{}
	if user != nil {
		users = append(users, user)
	} else {
		allUsers, err := v.Abstraction.GetCouchbaseUsers(cluster.Namespace, cluster.Spec.Security.RBAC.Selector)
		if err != nil {
			return err
		}

		for _, user := range allUsers.Items {
			selector := labels.Everything()
			if cluster.Spec.Security.RBAC.Selector != nil {
				s, err := metav1.LabelSelectorAsSelector(cluster.Spec.Security.RBAC.Selector)
				if err != nil {
					return err
				}
				selector = s
			}

			// Current user pw and version validation currently only applies to internal users.
			if selector.Matches(labels.Set(user.Labels)) && user.Spec.AuthDomain == couchbasev2.InternalAuthDomain {
				users = append(users, &user)
			}
		}
	}

	for _, user := range users {
		if errs := checkClusterUserConstraints(v, user, cluster); errs != nil {
			return errors.CompositeValidationError(errs...)
		}
	}

	return nil
}

// checkClusterUserConstraints validates user constraints that are cluster specific, e.g. versions.
// A user must be provided and if a cluster is provided, we will validate against that cluster. If not, we will fetch all clusters that have an rbac selector
// that matches the user. The Operator uses a tree of groups/users/rolebindings to determine which clusters are relevant to a given user,
// but for validation this becomes quite a heavy check, so until we can correctly cache the tree, we'll just do a more lightweight check.
func checkClusterUserConstraints(v *types.Validator, user *couchbasev2.CouchbaseUser, cluster *couchbasev2.CouchbaseCluster) []error {
	var errs []error
	if user == nil {
		return errs
	}

	var clusters []*couchbasev2.CouchbaseCluster
	if cluster != nil {
		clusters = []*couchbasev2.CouchbaseCluster{cluster}
	} else {
		allClusters, err := getUserRelatedClusters(v, user)
		if err != nil {
			return []error{err}
		}

		clusters = allClusters
	}

	for _, cluster := range clusters {
		if !cluster.Spec.Security.RBAC.Managed {
			continue
		}

		if v.Options.ValidateSecrets {
			if err := checkUserPasswordForCluster(v, cluster, user); err != nil {
				errs = append(errs, err)
			}
		}

		if err := checkUserClusterVersionConstraints(user, cluster); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

func checkUserClusterVersionConstraints(user *couchbasev2.CouchbaseUser, cluster *couchbasev2.CouchbaseCluster) error {
	atLeast80, err := cluster.IsAtLeastVersion("8.0.0")
	if err != nil {
		return err
	}

	if atLeast80 {
		return nil
	}

	errs := []error{}
	if user.Spec.Password != nil {
		errs = append(errs, fmt.Errorf("spec.password is only supported for Couchbase Server versions 8.0.0+ for user `%s`", user.Name))
	}

	if user.Spec.Locked != nil && *user.Spec.Locked {
		errs = append(errs, fmt.Errorf("spec.locked is only supported for Couchbase Server versions 8.0.0+ for user `%s`", user.Name))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}
