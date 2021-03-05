package v2

import (
	"crypto/x509"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"

	"github.com/go-openapi/errors"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/robfig/cron/v3"
)

const (
	defaultFSGroup              = 1000
	defaultMetricsImage         = "couchbase/exporter:1.0.3"
	redhatMetricsImage          = "registry.connect.redhat.com/couchbase/exporter:1.0.3-1"
	defaultBackupImage          = "couchbase/operator-backup:6.5.0"
	redhatBackupImage           = "registry.connect.redhat.com/couchbase/operator-backup:6.5.0-5"
	defaultBackupServiceAccount = "couchbase-backup"
	bucketTTLMax                = (1 << 31) - 1 // Puny 32 bit signed integers
)

var (
	emptyObject = struct{}{}
)

func ApplyDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	// SM: This may not actually be needed any more on OCP.  I say may, as I don't trust
	// customers, however, I think it prudent to make this behaviour optional so support
	// can turn it off if it becomes a problem.  It has certainly prevented a lot of
	// support cases in the past few years!
	if v.Options.DefaultFileSystemGroup {
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "securityContext"); !found {
			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/securityContext", Value: emptyObject})
		}

		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "securityContext", "fsGroup"); !found {
			fsgroup := defaultFSGroup

			// OCP specific hack, set the fsGroup to that defined in the namespace.
			// Otherwise default to the default for the dockerhub container.
			namespace, err := v.Abstraction.GetNamespace(object.GetNamespace())
			if !apierrors.IsForbidden(err) {
				if namespace.Annotations != nil {
					if groups, ok := namespace.Annotations["openshift.io/sa.scc.supplemental-groups"]; ok {
						// This may either look like 1000140000/10000
						// or 1000140000-1000150000, just pick the first
						// group.
						i := strings.Index(groups, "-")
						if i == -1 {
							i = strings.Index(groups, "/")
						}

						if i != -1 {
							if val, err := strconv.Atoi(groups[:i]); err == nil {
								fsgroup = val
							}
						}
					}
				}
			}

			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/securityContext/fsGroup", Value: fsgroup})
		}
	}

	if managedBackup, found, _ := unstructured.NestedBool(object.Object, "spec", "backup", "managed"); found && managedBackup {
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "backup", "image"); !found {
			backupImage := defaultBackupImage

			namespace, err := v.Abstraction.GetNamespace(object.GetNamespace())
			if !apierrors.IsForbidden(err) {
				if namespace.Annotations != nil {
					for annotation := range namespace.Annotations {
						if strings.HasPrefix(annotation, "openshift.io") {
							backupImage = redhatBackupImage
							break
						}
					}
				}
			}

			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/backup/image", Value: backupImage})
		}

		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "backup", "serviceAccountName"); !found {
			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/backup/serviceAccountName", Value: defaultBackupServiceAccount})
		}
	}

	if enableMonitoring, found, _ := unstructured.NestedBool(object.Object, "spec", "monitoring", "prometheus", "enabled"); found && enableMonitoring {
		if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "monitoring", "prometheus", "image"); !found {
			metricsImage := defaultMetricsImage

			namespace, err := v.Abstraction.GetNamespace(object.GetNamespace())
			if !apierrors.IsForbidden(err) {
				if namespace.Annotations != nil {
					for annotation := range namespace.Annotations {
						if strings.HasPrefix(annotation, "openshift.io") {
							metricsImage = redhatMetricsImage
							break
						}
					}
				}
			}

			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/monitoring/prometheus/image", Value: metricsImage})
		}
	}

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "security", "ldap"); found {
		// enable authorization if not specified but groupsQuery also exists.
		// otherwise this can remain false since authorization can still be
		// done for external users with names that already exist in the cluster
		if _, found, _ := unstructured.NestedBool(object.Object, "spec", "security", "ldap", "authorizationEnabled"); !found {
			if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "security", "ldap", "groupsQuery"); found {
				patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/security/ldap/authorizationEnabled", Value: true})
			}
		}

		// enable cert validation if encryption type is not None
		if encryption, found, _ := unstructured.NestedFieldCopy(object.Object, "spec", "security", "ldap", "encryption"); found {
			if encryption != string(couchbasev2.LDAPEncryptionNone) {
				if _, found, _ := unstructured.NestedBool(object.Object, "spec", "security", "ldap", "serverCertValidation"); !found {
					patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/security/ldap/serverCertValidation", Value: true})
				}
			}
		} else {
			// encryption is disabled by default
			patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/security/ldap/encryption", Value: couchbasev2.LDAPEncryptionNone})
		}
	}

	return patch
}

func ApplyGroupDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	roles, _, _ := unstructured.NestedSlice(object.Object, "spec", "roles")
	for i, role := range roles {
		if r, ok := role.(map[string]interface{}); ok {
			// Apply bucket role to all buckets by default
			bucket, ok := r["bucket"]
			if !ok || (bucket == "") {
				if couchbasev2.IsBucketRole(couchbasev2.RoleName(r["name"].(string))) {
					path := fmt.Sprintf("/spec/roles/%d/bucket", i)

					patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: path, Value: "*"})
				}
			}
		}
	}

	return patch
}

func CheckConstraints(v *types.Validator, customResource *couchbasev2.CouchbaseCluster) error {
	errs := []error{}

	// Basic schema openapi v3 validation (not provided by structural schema)
	// DELETE ME AFTER 1.17
	if !util.UniqueString(couchbasev2.ServiceList(customResource.Spec.Networking.AdminConsoleServices).StringSlice()) {
		errs = append(errs, errors.DuplicateItems("spec.networking.adminConsoleServices", "body"))
	}

	if !util.UniqueString(couchbasev2.ExposedFeatureList(customResource.Spec.Networking.ExposedFeatures).StringSlice()) {
		errs = append(errs, errors.DuplicateItems("spec.networking.exposedFeatures", "body"))
	}

	if !util.UniqueString(customResource.Spec.ServerGroups) {
		errs = append(errs, errors.DuplicateItems("spec.serverGroups", "body"))
	}

	for i, class := range customResource.Spec.Servers {
		if !util.UniqueString(couchbasev2.ServiceList(class.Services).StringSlice()) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].services", i), "body"))
		}

		if !util.UniqueString(class.ServerGroups) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].serverGroups", i), "body"))
		}
	}

	// Cluster validation
	if customResource.Spec.ClusterSettings.DataServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.DataServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}

	if customResource.Spec.ClusterSettings.IndexServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.IndexServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.indexServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}

	if customResource.Spec.ClusterSettings.SearchServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.SearchServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.searchServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}

	if customResource.Spec.ClusterSettings.EventingServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.EventingServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(256)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.eventingServiceMemoryQuota in body should be greater than or equal to 256Mi"))
		}
	}

	if customResource.Spec.ClusterSettings.AnalyticsServiceMemQuota != nil {
		if customResource.Spec.ClusterSettings.AnalyticsServiceMemQuota.Cmp(*k8sutil.NewResourceQuantityMi(1024)) < 0 {
			errs = append(errs, fmt.Errorf("spec.cluster.analyticsServiceMemoryQuota in body should be greater than or equal to 1Gi"))
		}
	}

	if customResource.Spec.ClusterSettings.AutoFailoverTimeout != nil {
		if customResource.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() < 5.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be greater than or equal to 5s"))
		}

		if customResource.Spec.ClusterSettings.AutoFailoverTimeout.Seconds() > 3600.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverTimeout in body should be less than or equal to 1h"))
		}
	}

	if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod != nil {
		if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod.Seconds() < 5.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be greater than or equal to 5s"))
		}

		if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod.Seconds() > 3600.0 {
			errs = append(errs, fmt.Errorf("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be less than or equal to 1h"))
		}
	}

	if customResource.Spec.ClusterSettings.Indexer != nil {
		if int(customResource.Spec.ClusterSettings.Indexer.MemorySnapshotInterval.Milliseconds()) < 1 {
			errs = append(errs, fmt.Errorf("spec.cluster.indexer.memorySnapshotInterval in body must be greater than or equal to 1ms"))
		}

		if int(customResource.Spec.ClusterSettings.Indexer.StableSnapshotInterval.Milliseconds()) < 1 {
			errs = append(errs, fmt.Errorf("spec.cluster.indexer.stableSnapshotInterval in body must be greater than or equal to 1ms"))
		}
	}

	// Referenced object validation
	if v.Options.ValidateSecrets {
		secret, err := v.Abstraction.GetSecret(customResource.Namespace, customResource.Spec.Security.AdminSecret)

		switch {
		case err != nil:
			errs = append(errs, err)
		case secret == nil:
			errs = append(errs, fmt.Errorf("secret %s referenced by spec.security.adminSecret must exist", customResource.Spec.Security.AdminSecret))
		default:
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
		}
	}

	// Referenced object validation
	if customResource.Spec.Monitoring != nil && customResource.Spec.Monitoring.Prometheus != nil {
		if v.Options.ValidateSecrets {
			authSecret := customResource.Spec.Monitoring.Prometheus.AuthorizationSecret
			if authSecret != nil {
				if secret, err := v.Abstraction.GetSecret(customResource.Namespace, *authSecret); err != nil {
					errs = append(errs, err)
				} else if secret == nil {
					errs = append(errs, fmt.Errorf("secret %s referenced by spec.monitoring.prometheus.authorizationSecret must exist", *customResource.Spec.Monitoring.Prometheus.AuthorizationSecret))
				} else if _, ok := secret.Data["token"]; !ok {
					errs = append(errs, fmt.Errorf("monitoring authorization secret %s must contain key 'token'", *authSecret))
				}
			}
		}
	}

	if customResource.Spec.Logging.Server != nil && customResource.Spec.Logging.Server.Enabled {
		// If we want to use the sidecars then a PV must be present to use.
		// See separate PV validation but if any server class has a log volume or a default volume they all should.
		// Just checking the first server with some extra nil pointer checks
		// No checks are required for empty strings for configurationName, configurationMountPath or image as this will be omitted and defaulted then.
		if len(customResource.Spec.Servers) == 0 {
			errs = append(errs, fmt.Errorf("spec.logging.server requires spec.servers to be populated to enable logging sidecar"))
		} else {
			mounts := customResource.Spec.Servers[0].VolumeMounts
			if mounts == nil || (mounts.DefaultClaim == "" && mounts.LogsClaim == "") {
				errs = append(errs, fmt.Errorf("spec.logging.server requires a default or logs volume to enable logging sidecar"))
			}
		}
	} else {
		// Checks for server logging being disabled (not enabled) and any that require it (audit).
		// Ignore any settings if audit not enabled.
		auditSettings := customResource.Spec.Logging.Audit
		if auditSettings != nil && auditSettings.Enabled {
			// We are not allowing audit garbage collection without some form of server log shipping.
			gc := auditSettings.GarbageCollection
			if gc != nil && gc.Sidecar != nil && gc.Sidecar.Enabled {
				errs = append(errs, fmt.Errorf("spec.logging.audit.garbageCollection.sidecar.enabled requires spec.logging.server configured for log shipping"))
			}
		}
	}

	if customResource.Spec.XDCR.Managed {
		for i, remoteCluster := range customResource.Spec.XDCR.RemoteClusters {
			if remoteCluster.AuthenticationSecret != nil {
				if v.Options.ValidateSecrets {
					if secret, err := v.Abstraction.GetSecret(customResource.Namespace, *remoteCluster.AuthenticationSecret); err != nil {
						errs = append(errs, err)
					} else if secret == nil {
						errs = append(errs, fmt.Errorf("secret %s referenced by spec.xdcr.remoteClusters[%d].authenticationSecret must exist", *remoteCluster.AuthenticationSecret, i))
					}
				}
			}

			replications, err := v.Abstraction.GetCouchbaseReplications(customResource.Namespace, remoteCluster.Replications.Selector)
			if err != nil {
				errs = append(errs, err)
			}

			for _, replication := range replications.Items {
				if err := validateBucketExists(v, customResource, replication.Spec.Bucket); err != nil {
					errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasereplications.couchbase.com/%s must exist: %w", replication.Spec.Bucket, replication.Name, err))
				}
			}
		}
	}

	// Check to make sure:
	// 1. Server names are unique
	// 2. The data service is specified on at least one node
	// 3. The derived autoscaler name will be unique
	unique := make(map[string]bool)
	hasDataService := false

	for i, config := range customResource.Spec.Servers {
		// DELETE ME AFTER 1.17
		if _, ok := unique[customResource.Spec.Servers[i].Name]; ok {
			errs = append(errs, errors.DuplicateItems("spec.servers.name", "body"))
		}

		for _, svc := range customResource.Spec.Servers[i].Services {
			if svc == "data" {
				hasDataService = true
			}
		}

		if _, ok := unique[config.AutoscalerName(customResource.Name)]; ok {
			errs = append(errs, errors.DuplicateItems("spec.servers.autoscaler.name", "body"))
		}

		unique[customResource.Spec.Servers[i].Name] = true
		unique[config.AutoscalerName(customResource.Name)] = true
	}

	if !hasDataService {
		err := errors.Required("at least one \"data\" service", "spec.servers[*].services")
		errs = append(errs, err)
	}

	// Validate the cluster is supportable.
	// 1. If any server class has a log volume or a default volume they all should.
	// 2. Log volumes can only be used on server classes containing query, search and eventing services.
	//    Data, index and analytics volumes must use the default mount for data persistence.
	anySupportable := false

	for _, class := range customResource.Spec.Servers {
		if class.VolumeMounts != nil {
			if class.VolumeMounts.DefaultClaim != "" || class.VolumeMounts.LogsClaim != "" {
				anySupportable = true
			}
		}
	}

	if anySupportable {
		for index, class := range customResource.Spec.Servers {
			// Volume mounts must be specified if any others are supportable
			if class.VolumeMounts == nil {
				errs = append(errs, errors.Required("volumeMounts", fmt.Sprintf("spec.servers[%d]", index)))
			} else if couchbasev2.ServiceList(class.Services).ContainsAny(couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.AnalyticsService) && class.VolumeMounts.DefaultClaim == "" {
				// These stateful services must have a "default" mount
				errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].volumeMounts", index)))
				// Note that we don't test for search here but we do allow the Index mount to be used for it later though for performance reasons.
			}
		}
	}

	// validate persistent volume spec such that when volumeMounts are specified, claim for
	// `default` must be provided, and all mounts much pair to associated persistentVolumeClaims.
	// `logs` claim cannot be used in conjunction with `default` claim.
	for index, config := range customResource.Spec.Servers {
		if config.VolumeMounts != nil {
			mounts := config.VolumeMounts

			secondaryMounts := []string{}

			if mounts.DataClaim != "" {
				secondaryMounts = append(secondaryMounts, "data")
			}

			if mounts.IndexClaim != "" {
				secondaryMounts = append(secondaryMounts, "index")
			}

			if mounts.AnalyticsClaims != nil {
				secondaryMounts = append(secondaryMounts, "analytics")
			}

			hasSecondaryMounts := len(secondaryMounts) > 0

			// Check the associated service is enabled
			if mounts.DataClaim != "" && !couchbasev2.ServiceList(config.Services).Contains(couchbasev2.DataService) {
				errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.data requires the data service to be enabled", index))
			}

			if mounts.IndexClaim != "" && !couchbasev2.ServiceList(config.Services).ContainsAny(couchbasev2.IndexService, couchbasev2.SearchService) {
				errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.index requires the index or search service to be enabled", index))
			}

			if mounts.AnalyticsClaims != nil && !couchbasev2.ServiceList(config.Services).Contains(couchbasev2.AnalyticsService) {
				errs = append(errs, fmt.Errorf("spec.servers[%d].volumeMounts.analytics requires the analytics service to be enabled", index))
			}

			templateNamesEnum := []interface{}{}

			templateNames := customResource.Spec.GetVolumeClaimTemplateNames()
			for _, name := range templateNames {
				templateNamesEnum = append(templateNamesEnum, name)
			}

			switch {
			case mounts.LogsOnly():
				if template := customResource.Spec.GetVolumeClaimTemplate(mounts.LogsClaim); template == nil {
					errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].logs", index), "", mounts.LogsClaim, templateNamesEnum))
				}

				if mounts.DefaultClaim != "" || hasSecondaryMounts {
					if mounts.DefaultClaim != "" {
						errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.servers[%d].volumeMounts", index), "", "default"))
					}

					for _, secondaryMount := range secondaryMounts {
						errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.servers[%d].volumeMounts", index), "", secondaryMount))
					}
				}
			case mounts.DefaultClaim != "":
				if template := customResource.Spec.GetVolumeClaimTemplate(mounts.DefaultClaim); template == nil {
					errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].default", index), "", mounts.DefaultClaim, templateNamesEnum))
				}

				if mounts.DataClaim != "" {
					if template := customResource.Spec.GetVolumeClaimTemplate(mounts.DataClaim); template == nil {
						errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].data", index), "", mounts.DataClaim, templateNamesEnum))
					}
				}

				if mounts.IndexClaim != "" {
					if template := customResource.Spec.GetVolumeClaimTemplate(mounts.IndexClaim); template == nil {
						errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].index", index), "", mounts.IndexClaim, templateNamesEnum))
					}
				}

				if len(mounts.AnalyticsClaims) > 0 {
					for analyticsIndex, claim := range mounts.AnalyticsClaims {
						if template := customResource.Spec.GetVolumeClaimTemplate(claim); template == nil {
							errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].analytics[%d]", index, analyticsIndex), "", claim, templateNamesEnum))
						}
					}
				}
			case hasSecondaryMounts:
				errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].volumeMounts", index)))
			}
		}
	}

	// validate claim templates such that storage class is provided along with valid request
	pvcMap := map[string]bool{}

	for i, pvc := range customResource.Spec.VolumeClaimTemplates {
		hasStorageQuantity := false

		// Request quantity cannot be negative or zero
		if quantity, ok := pvc.Spec.Resources.Requests[v1.ResourceStorage]; ok {
			hasStorageQuantity = hasStorageQuantity || (quantity.Sign() == 1)
		}

		// There is no such thing as a limit in regard to storage request
		// but the k8s api allows a value here since it is a Resource kind.
		// It's not an error to provide this, but it cannot be standalone
		// since the volume capacity only requires a request value.
		if quantity, ok := pvc.Spec.Resources.Limits[v1.ResourceStorage]; ok {
			hasStorageQuantity = hasStorageQuantity && (quantity.Sign() == 1)
		}

		if !hasStorageQuantity {
			err := errors.Required(string(v1.ResourceStorage), "spec.volumeClaimTemplates[*].resources.requests|limits")
			errs = append(errs, err)
		}

		pvcName := pvc.ObjectMeta.Name
		if pvcMap[pvcName] {
			err := errors.DuplicateItems(fmt.Sprintf("spec.volumeClaimTemplates[%d].metadata.name", i), "body")
			errs = append(errs, err)
		} else {
			pvcMap[pvcName] = true
		}

		// Ensure storageClass exists
		if pvc.Spec.StorageClassName != nil && v.Options.ValidateStorageClasses {
			storageClass, err := v.Abstraction.GetStorageClass(*pvc.Spec.StorageClassName)

			switch {
			case err != nil:
				errs = append(errs, err)
			case storageClass == nil:
				errs = append(errs, fmt.Errorf("storage class %q must exist", *pvc.Spec.StorageClassName))
			case customResource.Spec.EnableOnlineVolumeExpansion:
				// StorageClass must allow volume expansion when feature is enabled
				volumeExpansionAllowed := false
				if val := storageClass.AllowVolumeExpansion; val != nil {
					volumeExpansionAllowed = *val
				}

				if !volumeExpansionAllowed {
					errs = append(errs, fmt.Errorf("spec.cluster.enableOnlineVolumeExpansion cannot be enabled since storage class %q does not specify `allowVolumeExpansion=true`", *pvc.Spec.StorageClassName))
				}
			}
		}
	}

	// version check
	currentVersionString, err := k8sutil.CouchbaseVersion(customResource.Spec.Image)
	if err != nil {
		errs = append(errs, fmt.Errorf("unsupported Couchbase version: %w", err))
	}

	currentVersion, err := couchbaseutil.NewVersion(currentVersionString)
	if err != nil {
		errs = append(errs, fmt.Errorf("unsupported Couchbase version: %w", err))
	} else {
		// current version must be equal or greater than min version
		minVersion, _ := couchbaseutil.NewVersion(constants.CouchbaseVersionMin)
		if currentVersion.Less(minVersion) {
			errs = append(errs, fmt.Errorf("unsupported Couchbase version: %s, minimum version required: %s", currentVersion, constants.CouchbaseVersionMin))
		}
	}

	// Record the zones that the server certificate needs to support as we look at the network configuration.
	subjectAltNames := util_x509.MandatorySANs(customResource.Name, customResource.Namespace)

	if customResource.Spec.Networking.DNS != nil {
		subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", customResource.Spec.Networking.DNS.Domain))
	}

	// Check TLS
	errs = append(errs, validateTLS(v, customResource, subjectAltNames)...)
	errs = append(errs, validateTLSXDCR(v, customResource)...)

	// Require that publically visible service ports have DNS information available.
	if customResource.Spec.IsExposedFeatureServiceTypePublic() || customResource.Spec.IsAdminConsoleServiceTypePublic() {
		if customResource.Spec.Networking.TLS == nil {
			errs = append(errs, errors.Required("spec.networking.tls", "body"))
		}

		if customResource.Spec.Networking.DNS == nil {
			errs = append(errs, errors.Required("spec.networking.dns", "body"))
		}
	}

	if err := validateBucketNameConstraints(v, customResource); err != nil {
		errs = append(errs, err)
	}

	if err := validateMemoryConstraints(v, customResource); err != nil {
		errs = append(errs, err)
	}

	// Check mutual verification
	if customResource.Spec.Networking.TLS != nil && customResource.Spec.Networking.TLS.ClientCertificatePolicy != nil {
		if len(customResource.Spec.Networking.TLS.ClientCertificatePaths) == 0 {
			errs = append(errs, errors.TooFewItems("spec.networking.tls.clientCertificatePaths", "", 1))
		}
	}

	// Check auto compaction
	purgeInterval := customResource.Spec.ClusterSettings.AutoCompaction.TombstonePurgeInterval.Duration.Hours()
	if purgeInterval < 1.0 {
		errs = append(errs, fmt.Errorf("spec.cluster.autoCompaction.tombstonePurgeInterval in body should be greater than or equal to 1h"))
	}

	if purgeInterval > 60.0*24.0 {
		errs = append(errs, fmt.Errorf("spec.cluster.autoCompaction.tombstonePurgeInterval in body should be less than or equal to 60d"))
	}

	// Check LDAP Settings
	if ldap := customResource.Spec.Security.LDAP; ldap != nil {
		if len(ldap.Hosts) == 0 {
			errs = append(errs, errors.TooFewItems("spec.security.ldap.hosts", "", 1))
		}

		// If authentication enabled then require username mapping
		if ldap.AuthenticationEnabled {
			// Both mapping options cannot be empty
			if (ldap.UserDNMapping.Template == "") && (ldap.UserDNMapping.Query == "") {
				errs = append(errs, errors.Required("spec.security.ldap.userDNMapping", "body"))
			}
			// Only 1 mapping option allowed
			if (ldap.UserDNMapping.Template != "") && (ldap.UserDNMapping.Query != "") {
				errs = append(errs, fmt.Errorf("ldap.userDNMapping must contain either query or template"))
			}
		}

		// ca is required when tls is enabled
		if ldap.EnableCertValidation {
			// encryption type must be set
			if ldap.Encryption == couchbasev2.LDAPEncryptionNone {
				errs = append(errs, fmt.Errorf("encryption must be one of %s | %s, when serverCertValidation is enabled",
					couchbasev2.LDAPEncryptionTLS, couchbasev2.LDAPEncryptionStartTLS))
			}

			// If encryption enabled then require cert secret
			tlsSecretName := customResource.Spec.Security.LDAP.TLSSecret
			if tlsSecretName == "" {
				errs = append(errs, errors.Required("spec.security.ldap.tlsSecret", "body"))
			}

			// secret containing ldap ca must exist
			if v.Options.ValidateSecrets {
				tlsSecret, err := v.Abstraction.GetSecret(customResource.Namespace, tlsSecretName)
				if err != nil {
					errs = append(errs, err)
				} else if tlsSecret == nil {
					errs = append(errs, fmt.Errorf("secret %s referenced by security.ldap.tlsSecret must exist", tlsSecretName))
				} else if _, ok := tlsSecret.Data["ca.crt"]; !ok {
					errs = append(errs, fmt.Errorf("ldap tls secret %s must contain key 'ca.crt'", tlsSecretName))
				}
			}
		}

		// require groups query when group auth enabled
		if ldap.AuthorizationEnabled {
			if ldap.GroupsQuery == "" {
				errs = append(errs, errors.Required("security.ldap.groupsQuery", "body"))
			}
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsBucket(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) error {
	errs := []error{}

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

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsEphemeralBucket(v *types.Validator, bucket *couchbasev2.CouchbaseEphemeralBucket) error {
	errs := []error{}

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

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsMemcachedBucket(v *types.Validator, bucket *couchbasev2.CouchbaseMemcachedBucket) error {
	errs := []error{}

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

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsReplication(v *types.Validator, replication *couchbasev2.CouchbaseReplication) error {
	return nil
}

func CheckConstraintsCouchbaseUser(v *types.Validator, user *couchbasev2.CouchbaseUser) error {
	errs := []error{}

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

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsBackup(v *types.Validator, backup *couchbasev2.CouchbaseBackup) error {
	errs := []error{}

	if err := validateBackupCronSchedules(backup); err != nil {
		errs = err
	}

	if backup.Spec.Size.Value() <= 0 {
		errs = append(errs, fmt.Errorf("spec.size %d must be greater than 0", backup.Spec.Size.Value()))
	}

	if len(backup.Spec.S3Bucket) != 0 && !strings.HasPrefix(backup.Spec.S3Bucket, "s3://") {
		errs = append(errs, fmt.Errorf("spec.s3bucket %s is not a valid S3 bucket URI format", backup.Spec.S3Bucket))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsBackupRestore(v *types.Validator, restore *couchbasev2.CouchbaseBackupRestore) error {
	errs := []error{}

	if len(restore.Spec.Backup) == 0 && len(restore.Spec.Repo) == 0 {
		errs = append(errs, fmt.Errorf("both spec.backup and spec.repo fields are empty. Please supply a value for at least one"))
	}

	// start is required
	start := restore.Spec.Start
	if start == nil {
		errs = append(errs, fmt.Errorf("specify a start point or backup"))
	} else if start.Str != nil && start.Int != nil {
		// both str and int are specified
		errs = append(errs, fmt.Errorf("specify just one value, either Str or Int"))
	}

	// if end has been specified
	end := restore.Spec.End
	if start != nil && end != nil {
		// both str and int are specified
		if end.Str != nil && end.Int != nil {
			errs = append(errs, fmt.Errorf("specify just one value, either Str or Int"))
		}

		// end and start differ
		if end != start {
			// start and end are using string arguments
			if end.Str != nil && start.Str != nil {
				if *end.Str == "oldest" && *start.Str == "newest" {
					errs = append(errs, fmt.Errorf("start point %s is after end point %s", *start.Str, *end.Str))
				}
			}

			// start and end are using integer arguments
			if start.Int != nil && end.Int != nil {
				if *start.Int > *end.Int {
					errs = append(errs, fmt.Errorf("start integer cannot be larger than end integer"))
				}
			}
		}
	}

	if len(restore.Spec.Buckets.Exclude) != 0 {
	outer:
		for _, b := range restore.Spec.Buckets.Exclude {
			if len(restore.Spec.Buckets.Include) != 0 {
				for _, b1 := range restore.Spec.Buckets.Include {
					if strings.TrimSpace(b1) == strings.TrimSpace(b) {
						errs = append(errs, fmt.Errorf("bucket to be excluded cannot also be in bucket include list"))
						break outer
					}
				}
			}
			if len(restore.Spec.Buckets.BucketMap) != 0 {
				for _, m := range restore.Spec.Buckets.BucketMap {
					if strings.TrimSpace(m.Source) == strings.TrimSpace(b) {
						errs = append(errs, fmt.Errorf("bucket to be excluded cannot also be in a source field of the bucketMap"))
						break outer
					}
				}
			}
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsCouchbaseGroup(v *types.Validator, group *couchbasev2.CouchbaseGroup) error {
	errs := []error{}

	for index, role := range group.Spec.Roles {
		// role itself must be valid
		isCluterRole := couchbasev2.IsClusterRole(role.Name)
		// Bucket cannot be used with cluster role
		if role.Bucket != "" && isCluterRole {
			errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.roles[%d].bucket for cluster role", index), "", string(role.Name)))
		}
	}

	if len(errs) > 0 {
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
	buckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	for _, bucket := range buckets.Items {
		if bucket.Spec.Name != "" {
			if bucket.Spec.Name == name {
				return nil
			}
		} else {
			if bucket.Name == name {
				return nil
			}
		}
	}

	for _, bucket := range ephemeralBuckets.Items {
		if bucket.Spec.Name != "" {
			if bucket.Spec.Name == name {
				return nil
			}
		} else {
			if bucket.Name == name {
				return nil
			}
		}
	}

	memcachedBuckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	for _, bucket := range memcachedBuckets.Items {
		if bucket.Spec.Name != "" {
			if bucket.Spec.Name == name {
				return fmt.Errorf("memcached bucket %s cannot be replicated", name)
			}
		} else {
			if bucket.Name == name {
				return fmt.Errorf("memcached bucket %s cannot be replicated", name)
			}
		}
	}

	return fmt.Errorf("bucket %s not found", name)
}

// validateBackupCronSchedules ensures that the correct cronjob schedules are valid for the desired backup strategy.
func validateBackupCronSchedules(backup *couchbasev2.CouchbaseBackup) []error {
	errs := []error{}

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

func CheckImmutableFields(current, updated *couchbasev2.CouchbaseCluster) error {
	errs := []error{}

	if !reflect.DeepEqual(current.Spec.Networking.AddressFamily, updated.Spec.Networking.AddressFamily) {
		errs = append(errs, util.NewUpdateError(`spec.networking.addressFamily`, `body`))
	}

	for _, cur := range current.Spec.Servers {
		for i, up := range updated.Spec.Servers {
			if cur.Name == up.Name {
				if !util.StringArrayCompare(couchbasev2.ServiceList(cur.Services).StringSlice(), couchbasev2.ServiceList(up.Services).StringSlice()) {
					err := util.NewUpdateError(fmt.Sprintf("spec.servers[%d].services", i), "body")
					errs = append(errs, err)
				}
			}
		}
	}

	// Check to see if either the old or new specification have the the index
	// service defined. If they do then we cannot change the indexStorageSetting.
	hasIndexSvc := false

	for _, cur := range current.Spec.Servers {
		for _, svc := range cur.Services {
			if svc == couchbasev2.IndexService {
				hasIndexSvc = true
			}
		}
	}

	for _, up := range updated.Spec.Servers {
		for _, svc := range up.Services {
			if svc == couchbasev2.IndexService {
				hasIndexSvc = true
			}
		}
	}

	if hasIndexSvc {
		prev := current.Spec.ClusterSettings.IndexStorageSetting

		if current.Spec.ClusterSettings.Indexer != nil {
			prev = current.Spec.ClusterSettings.Indexer.StorageMode
		}

		curr := updated.Spec.ClusterSettings.IndexStorageSetting

		if updated.Spec.ClusterSettings.Indexer != nil {
			curr = updated.Spec.ClusterSettings.Indexer.StorageMode
		}

		if prev != curr {
			errs = append(errs, fmt.Errorf("spec.cluster.indexStorageSetting/spec.cluster.indexer.storageMode in body cannot be modified if there are any nodes in the cluster running the index service"))
		}
	}

	// Upgrade validation
	// * Deny downgrades if no upgrade in progress
	// * Deny upgrade if across major versions
	// * Deny rollback if it doesn't match the current version
	currentVersion, err := k8sutil.CouchbaseVersion(current.Spec.Image)
	if err != nil {
		errs = append(errs, err)
	}

	updatedVersion, err := k8sutil.CouchbaseVersion(updated.Spec.Image)
	if err != nil {
		errs = append(errs, err)
	}

	upgradeCondition := current.Status.GetCondition(couchbasev2.ClusterConditionUpgrading)
	if upgradeCondition == nil && currentVersion != updatedVersion {
		src, err := couchbaseutil.NewVersion(currentVersion)
		if err != nil {
			errs = append(errs, err)
		}

		dst, err := couchbaseutil.NewVersion(updatedVersion)
		if err != nil {
			errs = append(errs, err)
		}

		if dst.Less(src) {
			errs = append(errs, fmt.Errorf("spec.Version in body should be greater than %s", src.Semver()))
		}

		if dst.Major() > src.Major()+1 {
			max, _ := couchbaseutil.NewVersion(fmt.Sprintf("%d.0.0", src.Major()+2))
			errs = append(errs, fmt.Errorf("spec.Version in body should be less than %s", max.Semver()))
		}
	}

	if upgradeCondition != nil && currentVersion != updatedVersion {
		if updatedVersion != current.Status.CurrentVersion {
			errs = append(errs, util.NewUpdateError("spec.version", "body"))
		}
	}

	if updated.Spec.EnableOnlineVolumeExpansion {
		// Online Persistent Volumes cannot be down-scaled since the current
		// action is provided by a Filesystem 'expansion' storage driver
		for _, updatedClaimTemplate := range updated.Spec.VolumeClaimTemplates {
			currentClaimTemplate := current.Spec.GetVolumeClaimTemplate(updatedClaimTemplate.ObjectMeta.Name)
			if currentClaimTemplate == nil {
				// Claim is being added
				continue
			}

			// Compare storage requests
			currentQuantity, cOk := currentClaimTemplate.Spec.Resources.Requests[v1.ResourceStorage]
			updatedQuantity, uOk := updatedClaimTemplate.Spec.Resources.Requests[v1.ResourceStorage]

			if cOk && uOk && updatedQuantity.Cmp(currentQuantity) == -1 {
				errs = append(errs, fmt.Errorf("spec.volumeClaimTemplates[*].resources.requests[storage] in body can not be less than previous value %s", currentQuantity.String()))
			}
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsBucket(prev, curr *couchbasev2.CouchbaseBucket) error {
	errs := []error{}

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsEphemeralBucket(prev, curr *couchbasev2.CouchbaseEphemeralBucket) error {
	errs := []error{}

	if prev.Spec.ConflictResolution != curr.Spec.ConflictResolution {
		errs = append(errs, util.NewUpdateError("spec.conflictResolution", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsMemcachedBucket(prev, curr *couchbasev2.CouchbaseMemcachedBucket) error {
	return nil
}

func CheckImmutableFieldsReplication(prev, curr *couchbasev2.CouchbaseReplication) error {
	errs := []error{}

	if prev.Spec.Bucket != curr.Spec.Bucket {
		errs = append(errs, util.NewUpdateError("spec.bucket", "body"))
	}

	if prev.Spec.RemoteBucket != curr.Spec.RemoteBucket {
		errs = append(errs, util.NewUpdateError("spec.remoteBucket", "body"))
	}

	if prev.Spec.FilterExpression != curr.Spec.FilterExpression {
		errs = append(errs, util.NewUpdateError("spec.filterExpression", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsBackup(prev, curr *couchbasev2.CouchbaseBackup) error {
	errs := []error{}

	if prev.Spec.Strategy != curr.Spec.Strategy {
		errs = append(errs, util.NewUpdateError("spec.strategy", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckImmutableFieldsAutoscaler(prev, curr *couchbasev2.CouchbaseAutoscaler) error {
	errs := []error{}

	if prev.Spec.Servers != curr.Spec.Servers {
		errs = append(errs, util.NewUpdateError("spec.servers", "body"))
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}
