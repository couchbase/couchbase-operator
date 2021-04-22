package v2

import (
	"crypto/x509"
	goerrors "errors"
	"fmt"
	"reflect"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"

	"github.com/go-openapi/errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/robfig/cron/v3"
)

const (
	bucketTTLMax = (1 << 31) - 1 // Puny 32 bit signed integers
)

// CheckConstraints does domain specific validation for a Couchbase cluster.
// NOTE: philosophically, we shouldn't need this many, and my gut feeling is that
// through clever API design we can cull some of them.  Obviously this is a
// CRDv3 thing.
func CheckConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	checks := []func(*types.Validator, *couchbasev2.CouchbaseCluster) error{
		checkConstraintDataServiceMemoryQuota,
		checkConstraintIndexServiceMemoryQuota,
		checkConstraintSearchServiceMemoryQuota,
		checkConstraintEventingServiceMemoryQuota,
		checkConstraintAnalyticsServiceMemoryQuota,
		checkConstraintQueryTemporarySpace,
		checkConstraintAutoFailoverTimeout,
		checkConstraintAutoFailoverOnDataDiskIssuesTimePeriod,
		checkConstraintIndexerMemorySnapshotInterval,
		checkConstraintIndexerStableSnapshotInterval,
		checkConstraintAdminSecret,
		checkConstraintPrometheusAuthorizationSecret,
		checkConstraintLoggingPermissible,
		checkConstraintAuditLoggingPermissible,
		checkConstraintXDCRRemoteAuthentication,
		checkConstraintXDCRReplicationBuckets,
		checkConstraintServerClassContainsDataService,
		checkConstraintClusterSupportable,
		checkConstraintServiceEnabledForVolumeMount,
		checkConstraintDefaultAndLogVolumesMututallyExclusive,
		checkConstraintServiceMountsUsedWithDefaultMount,
		checkConstraintTemplateExistsForMount,
		checkConstraintVolumeTemplateNameUnique,
		checkConstraintVolumeTemplateSize,
		checkConstraintVolumeTemplateStorageClass,
		checkConstraintServerMinimumVersion,
		checkConstraintTLS,
		checkConstraintXDCRConnectionTLS,
		checkConstraintPublicNetworking,
		checkConstraintBucketNames,
		checkConstraintMemoryAllocations,
		checkConstraintMTLSPaths,
		checkConstraintAutoCompactionTombstonePurgeInterval,
		checkConstraintLDAPAuthentication,
		checkConstraintLDAPConnectionTLS,
		checkConstraintLDAPAuthorization,
		checkConstraintAutoscalingStabilizationPeriod,
	}

	var errs []error

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

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintDataServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintDataServiceMemoryQuota(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.DataServiceMemQuota == nil {
		return errors.Required("spec.cluster.dataServiceMemoryQuota", "body")
	}

	if cluster.Spec.ClusterSettings.DataServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintIndexServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintIndexServiceMemoryQuota(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.IndexServiceMemQuota == nil {
		return errors.Required("spec.cluster.indexServiceMemoryQuota", "body")
	}

	if cluster.Spec.ClusterSettings.IndexServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.indexServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintSearchServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintSearchServiceMemoryQuota(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.SearchServiceMemQuota == nil {
		return errors.Required("spec.cluster.searchServiceMemoryQuota", "body")
	}

	if cluster.Spec.ClusterSettings.SearchServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.searchServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintEventingServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintEventingServiceMemoryQuota(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.EventingServiceMemQuota == nil {
		return errors.Required("spec.cluster.eventingServiceMemoryQuota", "body")
	}

	if cluster.Spec.ClusterSettings.EventingServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
		return fmt.Errorf("spec.cluster.eventingServiceMemoryQuota in body should be greater than or equal to 256Mi")
	}

	return nil
}

// checkConstraintAnalyticsServiceMemoryQuota checks the service memory resource lower limit.
func checkConstraintAnalyticsServiceMemoryQuota(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota == nil {
		return errors.Required("spec.cluster.anayticsServiceMemoryQuota", "body")
	}

	if cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
		return fmt.Errorf("spec.cluster.analyticsServiceMemoryQuota in body should be greater than or equal to 1Gi")
	}

	return nil
}

// checkConstraintQueryTemporarySpace checks the query temporary space is higher than the lower limit.
func checkConstraintQueryTemporarySpace(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.ClusterSettings.Query == nil {
		return nil
	}

	if cluster.Spec.ClusterSettings.Query.TemporarySpace.Cmp(*k8sutil.NewResourceQuantityMi(0)) <= 0 {
		return fmt.Errorf("spec.cluster.query.temporarySpace in body should be greater than 0Mi")
	}

	return nil
}

// checkConstraintAutoFailoverTimeout checks the autofailover timeout is within range.
func checkConstraintAutoFailoverTimeout(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.AutoFailoverTimeout == nil {
		return errors.Required("spec.cluster.autoFailoverTimeout", "body")
	}

	if cluster.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() < 5.0 {
		return fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be greater than or equal to 5s")
	}

	if cluster.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() > 3600.0 {
		return fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be less than or equal to 1h")
	}

	return nil
}

// checkConstraintAutoFailoverOnDataDiskIssuesTimePeriod checks the auto failover timeout is within range.
func checkConstraintAutoFailoverOnDataDiskIssuesTimePeriod(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	// Should be filled in by CRD defaulting.
	if cluster.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod == nil {
		return errors.Required("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod", "body")
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
func checkConstraintIndexerMemorySnapshotInterval(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.ClusterSettings.Indexer == nil {
		return nil
	}

	if int(cluster.Spec.ClusterSettings.Indexer.MemorySnapshotInterval.Milliseconds()) < 1 {
		return fmt.Errorf("spec.cluster.indexer.memorySnapshotInterval in body must be greater than or equal to 1ms")
	}

	return nil
}

// checkConstraintIndexerStableSnapshotInterval checks the indexer snapshot interval is within range.
func checkConstraintIndexerStableSnapshotInterval(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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

	secret, err := v.Abstraction.GetSecret(cluster.Namespace, cluster.Spec.Security.AdminSecret)
	if err != nil {
		return err
	}

	if secret == nil {
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
			errs = append(errs, errors.TooShort("password", "spec.security.adminSecret", 6))
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

	secret, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return err
	}

	if secret == nil {
		return fmt.Errorf("secret %s referenced by spec.monitoring.prometheus.authorizationSecret must exist", secretName)
	}

	if _, ok := secret.Data["token"]; !ok {
		return fmt.Errorf("monitoring authorization secret %s must contain key 'token'", secretName)
	}

	return nil
}

// checkConstraintLoggingPermissible checks persistent volumes are being used.
func checkConstraintLoggingPermissible(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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
func checkConstraintAuditLoggingPermissible(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsAuditGarbageCollectionSidecarEnabled() {
		return nil
	}

	if !cluster.IsSupportable() {
		return fmt.Errorf("server audit logging requires 'spec.servers.volumeMounts' to be configured")
	}

	if !cluster.IsAuditLoggingEnabled() {
		return fmt.Errorf("server audit logging requires 'spec.logging.audit.garbageCollection.sidecar.enabled' to be set")
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

			secret, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
			if err != nil {
				errs = append(errs, err)
			}

			if secret == nil {
				errs = append(errs, fmt.Errorf("secret %s referenced by spec.xdcr.remoteClusters[%d].authenticationSecret must exist", secretName, i))
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
			if err := validateBucketExists(v, cluster, replication.Spec.Bucket); err != nil {
				errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasereplications.couchbase.com/%s must exist: %w", replication.Spec.Bucket, replication.Name, err))
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintServerClassContainsDataService checks are least one class has the data service enabled.
func checkConstraintServerClassContainsDataService(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	for _, config := range cluster.Spec.Servers {
		if couchbasev2.ServiceList(config.Services).Contains(couchbasev2.DataService) {
			return nil
		}
	}

	return errors.Required("at least one \"data\" service", "spec.servers.services")
}

// checkConstraintClusterSupportable checks that if you have one supportable class, they all are.
func checkConstraintClusterSupportable(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.AnySupportable() && !cluster.IsSupportable() {
		return fmt.Errorf("all server classes must have volumes defined, all classes with stateful services must have the 'default' mount defined")
	}

	return nil
}

// checkConstraintServiceEnabledForVolumeMount checks that volume mounts are only used with the correct services.
func checkConstraintServiceEnabledForVolumeMount(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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
func checkConstraintDefaultAndLogVolumesMututallyExclusive(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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
func checkConstraintServiceMountsUsedWithDefaultMount(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for index, config := range cluster.Spec.Servers {
		if !config.VolumeMounts.HasVolumeMounts() {
			continue
		}

		if config.VolumeMounts.HasSubMounts() && !config.VolumeMounts.HasDefaultMount() {
			errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].volumeMounts", index)))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintTemplateExistsForMount checks that volume mounts are only specified when the
// corresponding service is enabled.
func checkConstraintTemplateExistsForMount(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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
func checkConstraintVolumeTemplateNameUnique(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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
func checkConstraintVolumeTemplateSize(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for _, template := range cluster.Spec.VolumeClaimTemplates {
		if template.Spec.Resources.Requests == nil {
			errs = append(errs, errors.Required(string(v1.ResourceStorage), "spec.volumeClaimTemplates.resources.requests"))
			continue
		}

		value, ok := template.Spec.Resources.Requests[v1.ResourceStorage]
		if !ok {
			errs = append(errs, errors.Required(string(v1.ResourceStorage), "spec.volumeClaimTemplates.resources.requests"))
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

	for _, template := range cluster.Spec.VolumeClaimTemplates {
		if template.Spec.StorageClassName == nil {
			// Not so fast skippy, you can lookup the default storage class
			// and continue with the checks...
			continue
		}

		storageClassName := *template.Spec.StorageClassName

		storageClass, err := v.Abstraction.GetStorageClass(storageClassName)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if storageClass == nil {
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
func checkConstraintServerMinimumVersion(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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
func checkConstraintPublicNetworking(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.Spec.IsExposedFeatureServiceTypePublic() && !cluster.Spec.IsAdminConsoleServiceTypePublic() {
		return nil
	}

	var errs []error

	if cluster.Spec.Networking.TLS == nil {
		errs = append(errs, errors.Required("spec.networking.tls", "body"))
	}

	if cluster.Spec.Networking.DNS == nil {
		errs = append(errs, errors.Required("spec.networking.dns", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// checkConstraintBucketNames checks that all buckets referenced by this cluster have
// unique names.
func checkConstraintBucketNames(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	return validateBucketNameConstraints(v, cluster)
}

// checkConstraintMemoryAllocations checks that all buckets referenced by this cluster
// have total memory requirements less than or equal to the data service memory quota.
func checkConstraintMemoryAllocations(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	return validateMemoryConstraints(v, cluster)
}

// checkConstraintMTLSPaths checks that when mTLS is enabled, then paths are specified
// in order to extract user identinty from the X.509 client certificate.
func checkConstraintMTLSPaths(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	if !cluster.IsMutualTLSEnabled() {
		return nil
	}

	if len(cluster.Spec.Networking.TLS.ClientCertificatePaths) == 0 {
		return errors.TooFewItems("spec.networking.tls.clientCertificatePaths", "", 1)
	}

	return nil
}

// checkConstraintAutoCompactionTombstonePurgeInterval checks that the tombstone purge
// interval is in range.
func checkConstraintAutoCompactionTombstonePurgeInterval(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	purgeInterval := cluster.Spec.ClusterSettings.AutoCompaction.TombstonePurgeInterval.Duration.Hours()
	if purgeInterval < 1.0 {
		return fmt.Errorf("spec.cluster.autoCompaction.tombstonePurgeInterval in body should be greater than or equal to 1h")
	}

	if purgeInterval > 60.0*24.0 {
		return fmt.Errorf("spec.cluster.autoCompaction.tombstonePurgeInterval in body should be less than or equal to 60d")
	}

	return nil
}

// checkConstraintLDAPAuthentication checks that when enabled, either a template or
// query are provided for LDAP authentication.
func checkConstraintLDAPAuthentication(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	ldap := cluster.Spec.Security.LDAP

	if ldap == nil {
		return nil
	}

	if !ldap.AuthenticationEnabled {
		return nil
	}

	if ldap.UserDNMapping.Template == "" && ldap.UserDNMapping.Query == "" {
		return errors.Required("spec.security.ldap.userDNMapping", "body")
	}

	if ldap.UserDNMapping.Template != "" && ldap.UserDNMapping.Query != "" {
		return fmt.Errorf("ldap.userDNMapping must contain either query or template")
	}

	return nil
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
	if tlsSecretName == "" {
		return errors.Required("spec.security.ldap.tlsSecret", "body")
	}

	if v.Options.ValidateSecrets {
		tlsSecret, err := v.Abstraction.GetSecret(cluster.Namespace, tlsSecretName)
		if err != nil {
			return err
		}

		if tlsSecret == nil {
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
func checkConstraintLDAPAuthorization(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	ldap := cluster.Spec.Security.LDAP

	if ldap == nil {
		return nil
	}

	// require groups query when group auth enabled
	if !ldap.AuthorizationEnabled {
		return nil
	}

	if ldap.GroupsQuery == "" {
		return errors.Required("security.ldap.groupsQuery", "body")
	}

	return nil
}

// checkConstraintAutoscalingStabilizationPeriod checks the autoscaling stablization period is greater than the lower limit.
func checkConstraintAutoscalingStabilizationPeriod(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
	stabilizationPeriod := cluster.Spec.AutoscaleStabilizationPeriod

	if stabilizationPeriod == nil {
		return nil
	}

	if stabilizationPeriod.Duration < 0 {
		return fmt.Errorf("spec.autoscaleStabilizationPeriod must be greater than or equal to 0s")
	}

	return nil
}

func CheckConstraintsBucket(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) error {
	var errs []error

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if bucket.Spec.MaxTTL != nil {
		timeout := int(bucket.Spec.MaxTTL.Duration.Seconds())

		if timeout < 0 {
			errs = append(errs, errors.ExceedsMinimumInt("spec.maxTTL", "body", 0, false))
		}

		if timeout > bucketTTLMax {
			errs = append(errs, errors.ExceedsMaximumInt("spec.maxTTL", "body", bucketTTLMax, false))
		}
	}

	if err := validateBucketNameConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if err := validateMemoryConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsEphemeralBucket(v *types.Validator, bucket *couchbasev2.CouchbaseEphemeralBucket) error {
	var errs []error

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if bucket.Spec.MaxTTL != nil {
		timeout := int(bucket.Spec.MaxTTL.Duration.Seconds())

		if timeout < 0 {
			errs = append(errs, errors.ExceedsMinimumInt("spec.maxTTL", "body", 0, false))
		}

		if timeout > bucketTTLMax {
			errs = append(errs, errors.ExceedsMaximumInt("spec.maxTTL", "body", bucketTTLMax, false))
		}
	}

	if err := validateBucketNameConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if err := validateMemoryConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsMemcachedBucket(v *types.Validator, bucket *couchbasev2.CouchbaseMemcachedBucket) error {
	var errs []error

	if bucket.Spec.MemoryQuota != nil {
		if bucket.Spec.MemoryQuota.Cmp(*k8sutil.NewResourceQuantityMi(100)) < 0 {
			errs = append(errs, fmt.Errorf("spec.memoryQuota in body should be greater than or equal to 100Mi"))
		}
	}

	if err := validateBucketNameConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if err := validateMemoryConstraints(v, bucket); err != nil {
		errs = append(errs, err)
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsReplication(v *types.Validator, replication *couchbasev2.CouchbaseReplication) error {
	return nil
}

func CheckConstraintsCouchbaseUser(v *types.Validator, user *couchbasev2.CouchbaseUser) error {
	var errs []error

	// only 'local' and 'ldap' auth domains accepted
	domain := user.Spec.AuthDomain
	switch domain {
	case couchbasev2.InternalAuthDomain:
		// password is required for internal auth domain
		authSecretName := user.Spec.AuthSecret
		if authSecretName == "" {
			emsg := fmt.Sprintf("spec.authSecret for `%s` domain", domain)
			errs = append(errs, errors.Required(emsg, user.Name))
		} else if v.Options.ValidateSecrets {
			// Check the ldap auth secret exists and has the correct keys
			authSecret, err := v.Abstraction.GetSecret(user.Namespace, authSecretName)
			if err != nil {
				errs = append(errs, err)
			} else if authSecret == nil {
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

func CheckConstraintsBackup(v *types.Validator, backup *couchbasev2.CouchbaseBackup) error {
	var errs []error

	if err := validateBackupCronSchedules(backup); err != nil {
		errs = err
	}

	if backup.Spec.Size.Value() <= 0 {
		errs = append(errs, fmt.Errorf("spec.size %d must be greater than 0", backup.Spec.Size.Value()))
	}

	if len(backup.Spec.S3Bucket) != 0 && !strings.HasPrefix(backup.Spec.S3Bucket, "s3://") {
		errs = append(errs, fmt.Errorf("spec.s3bucket %s is not a valid S3 bucket URI format", backup.Spec.S3Bucket))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsBackupRestore(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	checks := []func(*types.Validator, *couchbasev2.CouchbaseBackupRestore) error{
		checkContraintRestoreStart,
		checkContraintRestoreEnd,
		checkContraintRestoreRange,
		checkContraintRestoreBucketsMutuallyExclusive,
		checkConstraintRestoreMappedBucketNotExcluded,
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

func checkContraintRestoreStart(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	start := restore.Spec.Start

	if start.Str != nil && start.Int != nil {
		return fmt.Errorf("specify just one value, either Str or Int")
	}

	return nil
}

func checkContraintRestoreEnd(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
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

func checkContraintRestoreRange(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	start := restore.Spec.Start
	end := restore.Spec.End

	if end == nil {
		return nil
	}

	if end.Str != nil && start.Str != nil {
		if *end.Str == "oldest" && *start.Str == "newest" {
			return fmt.Errorf("start point %s is after end point %s", *start.Str, *end.Str)
		}
	}

	// start and end are using integer arguments
	if start.Int != nil && end.Int != nil {
		if *start.Int > *end.Int {
			return fmt.Errorf("start integer cannot be larger than end integer")
		}
	}

	return nil
}

func checkContraintRestoreBucketsMutuallyExclusive(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	var errs []error

	for _, b := range restore.Spec.Buckets.Exclude {
		for _, b1 := range restore.Spec.Buckets.Include {
			if b1 == b {
				errs = append(errs, fmt.Errorf("bucket to be excluded cannot also be in bucket include list"))
				break
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func checkConstraintRestoreMappedBucketNotExcluded(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	var errs []error

	for _, m := range restore.Spec.Buckets.BucketMap {
		for _, b := range restore.Spec.Buckets.Exclude {
			if m.Source == b {
				errs = append(errs, fmt.Errorf("bucket to be excluded cannot also be in a source field of the bucketMap"))
				break
			}
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsCouchbaseGroup(v *types.Validator, group *couchbasev2.CouchbaseGroup) error {
	var errs []error

	for index, role := range group.Spec.Roles {
		// role itself must be valid
		isCluterRole := couchbasev2.IsClusterRole(role.Name)
		// Bucket cannot be used with cluster role
		if role.Bucket != "" && isCluterRole {
			errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.roles[%d].bucket for cluster role", index), "", string(role.Name)))
		}
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

// getCA returns the CA, whether there was a soft error, or whether there was a hard error.
func getCA(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]byte, bool, error) {
	var secretPath string

	var secretName string

	caKey := "ca.crt"

	if cluster.IsTLSShadowed() {
		secretPath = "spec.networking.tls.secretSource.serverSecretName"
		secretName = cluster.Spec.Networking.TLS.SecretSource.ServerSecretName
	} else {
		secretPath = "spec.networking.tls.static.operatorSecret"
		secretName = cluster.Spec.Networking.TLS.Static.OperatorSecret
	}

	secret, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return nil, false, err
	}

	if secret == nil {
		return nil, false, fmt.Errorf("secret %s referenced by %s must exist", secretName, secretPath)
	}

	ca, ok := secret.Data[caKey]
	if !ok {
		return nil, false, fmt.Errorf("tls secret %s must contain %s", secretName, caKey)
	}

	return ca, true, nil
}

// getServerTLS returns the server key and chain, whether there was a soft error, or whether there was a hard error.
func getServerTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) ([]byte, []byte, bool, error) {
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

	secret, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return nil, nil, false, err
	}

	if secret == nil {
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

// getClientTLS returns the client key and chain, whether there was a soft error, or whether there was a hard error.
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

	secret, err := v.Abstraction.GetSecret(cluster.Namespace, secretName)
	if err != nil {
		return nil, nil, false, err
	}

	if secret == nil {
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
//   * in date
//   * have the correct attributes
// * leaf certificate has the correct SANs.
func validateTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, subjectAltNames []string) []error {
	if !cluster.IsTLSEnabled() || !v.Options.ValidateSecrets {
		return nil
	}

	// Get the CA for verification.
	ca, ok, err := getCA(v, cluster)
	if err != nil {
		return []error{err}
	}

	if !ok {
		return nil
	}

	// Check server certificates.
	key, chain, ok, err := getServerTLS(v, cluster)
	if err != nil {
		return []error{err}
	}

	if !ok {
		return nil
	}

	errs := util_x509.Verify(ca, chain, key, x509.ExtKeyUsageServerAuth, subjectAltNames, !cluster.IsTLSShadowed())
	if errs != nil {
		return errs
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

	errs = util_x509.Verify(ca, chain, key, x509.ExtKeyUsageClientAuth, nil, false)
	if errs != nil {
		return errs
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
			secret, err := v.Abstraction.GetSecret(cluster.Namespace, *remoteCluster.TLS.Secret)
			if err != nil {
				errs = append(errs, err)

				continue
			}

			if secret == nil {
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
		buckets = append(buckets, &couchbaseBuckets.Items[i])
	}

	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, err
	}

	for i := range ephemeralBuckets.Items {
		buckets = append(buckets, &ephemeralBuckets.Items[i])
	}

	memcachedBuckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil, err
	}

	for i := range memcachedBuckets.Items {
		buckets = append(buckets, &memcachedBuckets.Items[i])
	}

	return buckets, nil
}

// validateBucketNameConstraints takes a cluster and finds all buckets, or
// a bucket and finds all clusters referencing it, checking that bucket names
// are not reused.
func validateBucketNameConstraints(v *types.Validator, object runtime.Object) error {
	// Gather the clusters affected by this change (either adding a cluster or
	// a bucket -- bucket names are immutable).
	clusters := []*couchbasev2.CouchbaseCluster{}

	switch t := object.(type) {
	case *couchbasev2.CouchbaseBucket, *couchbasev2.CouchbaseEphemeralBucket, *couchbasev2.CouchbaseMemcachedBucket:
		bucket, ok := object.(metav1.Object)
		if !ok {
			return fmt.Errorf("failed to type assert bucket to meta object")
		}

		namespacedClusters, err := v.Abstraction.GetCouchbaseClusters(bucket.GetNamespace())
		if err != nil {
			return err
		}

		for i := range namespacedClusters.Items {
			clusters = append(clusters, &namespacedClusters.Items[i])
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

		// Gather the names in an associative array (a set essentially) and look for duplicates.
		names := map[string]interface{}{}

		for _, bucket := range buckets {
			name := bucket.GetName()

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
func validateMemoryConstraints(v *types.Validator, object runtime.Object) error {
	var namespace string

	switch t := object.(type) {
	case *couchbasev2.CouchbaseCluster:
		return validateClusterMemoryConstraints(v, t)
	case *couchbasev2.CouchbaseBucket:
		namespace = t.Namespace
	case *couchbasev2.CouchbaseEphemeralBucket:
		namespace = t.Namespace
	case *couchbasev2.CouchbaseMemcachedBucket:
		namespace = t.Namespace
	default:
		return fmt.Errorf("validate memory constraints: unsupported type")
	}

	clusters, err := v.Abstraction.GetCouchbaseClusters(namespace)
	if err != nil {
		return err
	}

	for i := range clusters.Items {
		cluster := clusters.Items[i]

		if err := validateClusterMemoryConstraints(v, &cluster); err != nil {
			return err
		}
	}

	return nil
}

// validateClusterMemoryConstraints given a cluster loads all buckets associated with it and
// validates that the allocated memory does not exceed the memory allocated for the
// data service.
func validateClusterMemoryConstraints(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) error {
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

	for _, bucket := range buckets {
		allocated.Add(*bucket.GetMemoryQuota())
	}

	if cluster.Spec.ClusterSettings.DataServiceMemQuota != nil {
		if allocated.Cmp(*cluster.Spec.ClusterSettings.DataServiceMemQuota) > 0 {
			return fmt.Errorf("bucket memory allocation (%v) exceeds data service quota (%v) on cluster %s", allocated, cluster.Spec.ClusterSettings.DataServiceMemQuota, cluster.Name)
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
		if bucket.GetName() != name {
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

// CheckImmutableFields checks whether the user is trying to change something
// that cannot be changed.  Think long and hard about adding stuff here... is it
// technically impossible to be updated?  Are you just being lazy?  History
// dictates that anything in here will soon not be because users will change it
// it won't work and they will complain at you.
func CheckImmutableFields(current, updated *couchbasev2.CouchbaseCluster) error {
	checks := []func(*couchbasev2.CouchbaseCluster, *couchbasev2.CouchbaseCluster) error{
		checkImmutableAddressFamily,
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

// checkImmutableAddressFamily checks that you aren't doing something silly like trying
// to migrate from IPv4 to IPv6.  Couchbase doesn't do dual stack, it's one or the other
// and set at pod creation time.
func checkImmutableAddressFamily(current, updated *couchbasev2.CouchbaseCluster) error {
	if !reflect.DeepEqual(current.Spec.Networking.AddressFamily, updated.Spec.Networking.AddressFamily) {
		return util.NewUpdateError(`spec.networking.addressFamily`, `body`)
	}

	return nil
}

// checkImmuatableServerClass checks that you aren't modifying that Couchbase cannot
// change about a pod, e.g. the set of services is it running.  This is set at pod
// creation time and cannot be updated.
func checkImmutableServerClass(current, updated *couchbasev2.CouchbaseCluster) error {
	var errs []error

	for _, cur := range current.Spec.Servers {
		for i, up := range updated.Spec.Servers {
			if cur.Name == up.Name {
				if !util.StringArrayCompare(couchbasev2.ServiceList(cur.Services).StringSlice(), couchbasev2.ServiceList(up.Services).StringSlice()) {
					errs = append(errs, util.NewUpdateError(fmt.Sprintf("spec.servers[%d].services", i), "body"))
					continue
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

	currentVersion, err := k8sutil.CouchbaseVersion(current.Spec.Image)
	if err != nil {
		return err
	}

	updatedVersion, err := k8sutil.CouchbaseVersion(updated.Spec.Image)
	if err != nil {
		return err
	}

	upgradeCondition := current.Status.GetCondition(couchbasev2.ClusterConditionUpgrading)

	// Condition is not set, therefore we are starting an upgrade.
	if upgradeCondition == nil {
		src, err := couchbaseutil.NewVersion(currentVersion)
		if err != nil {
			return err
		}

		dst, err := couchbaseutil.NewVersion(updatedVersion)
		if err != nil {
			return err
		}

		if dst.Less(src) {
			return fmt.Errorf("spec.Version in body should be greater than %s", src.Semver())
		}

		if dst.Major() > src.Major()+1 {
			max, _ := couchbaseutil.NewVersion(fmt.Sprintf("%d.0.0", src.Major()+2))
			return fmt.Errorf("spec.Version in body should be less than %s", max.Semver())
		}

		return nil
	}

	// Modification during upgrade, only allow rollback.
	if updatedVersion != current.Status.CurrentVersion {
		return util.NewUpdateError("spec.version", "body")
	}

	return nil
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

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsEphemeralBucket(prev, curr *couchbasev2.CouchbaseEphemeralBucket) error {
	var errs []error

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsMemcachedBucket(prev, curr *couchbasev2.CouchbaseMemcachedBucket) error {
	return nil
}

func CheckImmutableFieldsReplication(prev, curr *couchbasev2.CouchbaseReplication) error {
	var errs []error

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

	// Referenced server group cannot be changed
	if prev.Spec.Servers != curr.Spec.Servers {
		errs = append(errs, util.NewUpdateError("spec.servers", "body"))
	}

	if errs != nil {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}
