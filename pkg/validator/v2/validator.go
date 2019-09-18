package v2

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"
	"github.com/couchbase/gocbmgr"

	"github.com/go-openapi/errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	defaultIndexStorageSetting                    = "memory_optimized"
	defaultAutoFailoverTimeout                    = 120
	defaultAutoFailoverMaxCount                   = 3
	defaultAutoFailoverOnDataDiskIssuesTimePeriod = 120
	defaultServiceMemQuota                        = 256
	defaultAnalyticsServiceMemQuota               = 1024
	defaultFSGroup                                = 1000
)

func ApplyDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	emptyObject := struct{}{}

	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "dataServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/dataServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "indexServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/indexServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "searchServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/searchServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "eventingServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/eventingServiceMemoryQuota", Value: defaultServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "analyticsServiceMemoryQuota"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/analyticsServiceMemoryQuota", Value: defaultAnalyticsServiceMemQuota})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "indexStorageSetting"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/indexStorageSetting", Value: defaultIndexStorageSetting})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoFailoverTimeout"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoFailoverTimeout", Value: defaultAutoFailoverTimeout})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoFailoverMaxCount"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoFailoverMaxCount", Value: defaultAutoFailoverMaxCount})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoFailoverOnDataDiskIssuesTimePeriod"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", Value: defaultAutoFailoverOnDataDiskIssuesTimePeriod})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "databaseFragmentationThreshold"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/databaseFragmentationThreshold", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "databaseFragmentationThreshold", "percent"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", Value: 30})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "viewFragmentationThreshold"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/viewFragmentationThreshold", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "viewFragmentationThreshold", "percent"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/viewFragmentationThreshold/percent", Value: 30})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "cluster", "autoCompaction", "tombstonePurgeInterval"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/cluster/autoCompaction/tombstonePurgeInterval", Value: "72h"})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "networking"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/networking", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "networking", "adminConsoleServiceType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/networking/adminConsoleServiceType", Value: corev1.ServiceTypeNodePort})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "networking", "exposedFeatureServiceType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/networking/exposedFeatureServiceType", Value: corev1.ServiceTypeNodePort})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "securityContext"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/securityContext", Value: emptyObject})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "securityContext", "fsGroup"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/securityContext/fsGroup", Value: defaultFSGroup})
	}

	return patch
}

func ApplyBucketDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "compressionMode"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/compressionMode", Value: cbmgr.CompressionModePassive})
	}

	return patch
}

func ApplyEphemeralBucketDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "compressionMode"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/compressionMode", Value: cbmgr.CompressionModePassive})
	}

	return patch
}

func ApplyMemcachedBucketDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	return nil
}

func ApplyReplicationDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "compressionType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/compressionType", Value: couchbasev2.CompressionTypeAuto})
	}

	return patch
}

func ApplyRoleDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList
	roles, _, _ := unstructured.NestedSlice(object.Object, "spec", "roles")
	for i, role := range roles {
		if r, ok := role.(map[string]interface{}); ok {
			// Apply bucket role to all buckets by default
			if _, ok := r["bucket"]; !ok {
				if couchbasev2.IsBucketRole(r["name"].(string)) {
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
	if !util.UniqueString(customResource.Spec.Networking.AdminConsoleServices.StringSlice()) {
		errs = append(errs, errors.DuplicateItems("spec.networking.adminConsoleServices", "body"))
	}
	if !util.UniqueString(customResource.Spec.Networking.ExposedFeatures) {
		errs = append(errs, errors.DuplicateItems("spec.networking.exposedFeatures", "body"))
	}
	if !util.UniqueString(customResource.Spec.ServerGroups) {
		errs = append(errs, errors.DuplicateItems("spec.serverGroups", "body"))
	}
	for i, class := range customResource.Spec.Servers {
		if !util.UniqueString(class.Services.StringSlice()) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].services", i), "body"))
		}
		if !util.UniqueString(class.ServerGroups) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].serverGroups", i), "body"))
		}
	}
	if customResource.Spec.Networking.ExposeAdminConsole && len(customResource.Spec.Networking.AdminConsoleServices) == 0 {
		errs = append(errs, errors.Required("spec.networking.adminConsoleServices", "body"))
	}
	if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssues && customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod == 0 {
		errs = append(errs, errors.Required("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod", "body"))
	}

	// Referenced object validation
	if secret, err := v.Abstraction.GetSecret(customResource.Namespace, customResource.Spec.Security.AdminSecret); err != nil {
		// Silently ignore permissions errors, some users may not want us seeing these resources.
		if !apierrors.IsForbidden(err) {
			errs = append(errs, err)
		}
	} else if secret == nil {
		errs = append(errs, fmt.Errorf("secret %s referenced by spec.security.adminSecret must exist", customResource.Spec.Security.AdminSecret))
	}
	if customResource.Spec.XDCR.Managed {
		for i, remoteCluster := range customResource.Spec.XDCR.RemoteClusters {
			if secret, err := v.Abstraction.GetSecret(customResource.Namespace, remoteCluster.AuthenticationSecret); err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if !apierrors.IsForbidden(err) {
					errs = append(errs, err)
				}
			} else if secret == nil {
				errs = append(errs, fmt.Errorf("secret %s referenced by spec.xdcr.remoteClusters[%d].authenticationSecret must exist", remoteCluster.AuthenticationSecret, i))
			}

			replications, err := v.Abstraction.GetCouchbaseReplications(customResource.Namespace, remoteCluster.Replications.Selector)
			if err != nil {
				errs = append(errs, err)
			}

			for _, replication := range replications.Items {
				if err := validateBucketExists(v, customResource, replication.Spec.Bucket); err != nil {
					errs = append(errs, fmt.Errorf("bucket %s referenced by spec.bucket in couchbasereplications.couchbase.com/%s must exist", replication.Spec.Bucket, replication.Name))
				}
			}
		}
	}

	// Check to make sure:
	// 1. Server names are unique
	// 2. The data service is specified on at least one node
	unique := make(map[string]bool)
	hasDataService := false
	for i := range customResource.Spec.Servers {
		if _, ok := unique[customResource.Spec.Servers[i].Name]; ok {
			errs = append(errs, errors.DuplicateItems("spec.servers.name", "body"))
		}

		for _, svc := range customResource.Spec.Servers[i].Services {
			if svc == "data" {
				hasDataService = true
			}
		}

		unique[customResource.Spec.Servers[i].Name] = true
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
		if class.Pod != nil && class.Pod.VolumeMounts != nil {
			if class.Pod.VolumeMounts.DefaultClaim != "" || class.Pod.VolumeMounts.LogsClaim != "" {
				anySupportable = true
			}
		}
	}

	if anySupportable {
		for index, class := range customResource.Spec.Servers {
			// Volume mounts must be specified if any others are supportable
			if class.Pod == nil || class.Pod.VolumeMounts == nil {
				errs = append(errs, errors.Required("volumeMounts", fmt.Sprintf("spec.servers[%d].pod", index)))
			} else {
				// These stateful services must have a "default" mount
				if class.Services.ContainsAny(couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.AnalyticsService) &&
					class.Pod.VolumeMounts.DefaultClaim == "" {
					errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].pod.volumeMounts", index)))
				}
			}
		}
	}

	// validate persistent volume spec such that when volumeMounts are specified, claim for
	// `default` must be provided, and all mounts much pair to associated persistentVolumeClaims.
	// `logs` claim cannot be used in conjunction with `default` claim.
	for index, config := range customResource.Spec.Servers {
		if config.Pod != nil && config.Pod.VolumeMounts != nil {
			mounts := config.Pod.VolumeMounts

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
			if mounts.DataClaim != "" && !config.Services.Contains(couchbasev2.DataService) {
				errs = append(errs, errors.Required(string(couchbasev2.DataService), fmt.Sprintf("spec.servers[%d].services", index)))
			}
			if mounts.IndexClaim != "" && !config.Services.Contains(couchbasev2.IndexService) {
				errs = append(errs, errors.Required(string(couchbasev2.IndexService), fmt.Sprintf("spec.servers[%d].services", index)))
			}
			if mounts.AnalyticsClaims != nil && !config.Services.Contains(couchbasev2.AnalyticsService) {
				errs = append(errs, errors.Required(string(couchbasev2.AnalyticsService), fmt.Sprintf("spec.servers[%d].services", index)))
			}

			templateNames := customResource.Spec.GetVolumeClaimTemplateNames()
			templateNamesEnum := []interface{}{}
			for _, name := range templateNames {
				templateNamesEnum = append(templateNamesEnum, name)
			}

			if mounts.LogsOnly() {
				if template := customResource.Spec.GetVolumeClaimTemplate(mounts.LogsClaim); template == nil {
					errs = append(errs, errors.EnumFail(fmt.Sprintf("spec.servers[%d].logs", index), "", mounts.LogsClaim, templateNamesEnum))
				}
				if mounts.DefaultClaim != "" || hasSecondaryMounts {
					if mounts.DefaultClaim != "" {
						errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.servers[%d].pod.volumeMounts", index), "", "default"))
					}
					for _, secondaryMount := range secondaryMounts {
						errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.servers[%d].pod.volumeMounts", index), "", secondaryMount))
					}
				}
			} else if mounts.DefaultClaim != "" {
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
			} else if hasSecondaryMounts {
				errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].pod.volumeMounts", index)))
			}
		}
	}

	// validate claim templates such that storage class is provided along with valid request
	pvcMap := map[string]bool{}
	for i, pvc := range customResource.Spec.VolumeClaimTemplates {
		hasStorageQuantity := false
		if quantity, ok := pvc.Spec.Resources.Requests["storage"]; ok {
			hasStorageQuantity = hasStorageQuantity || !quantity.IsZero()
		}
		if quantity, ok := pvc.Spec.Resources.Limits["storage"]; ok {
			hasStorageQuantity = hasStorageQuantity || !quantity.IsZero()
		}
		if !hasStorageQuantity {
			err := errors.Required(`"storage"`, "spec.volumeClaimTemplates[*].resources.requests|limits")
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
		if pvc.Spec.StorageClassName != nil {
			storageClass, err := v.Abstraction.GetStorageClass(*pvc.Spec.StorageClassName)
			if err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if !apierrors.IsForbidden(err) {
					errs = append(errs, err)
				}
			} else if storageClass == nil {
				errs = append(errs, fmt.Errorf("storage class %s must exist", *pvc.Spec.StorageClassName))
			}
		}
	}

	// version check
	_, err := k8sutil.CouchbaseVersion(customResource.Spec.Image)
	if err != nil {
		errs = append(errs, fmt.Errorf("unsupported Couchbase version"))
	}

	// Record the zones that the server certificate needs to support as we look at the network configuration.
	zones := []string{
		customResource.Name + "." + customResource.Namespace + ".svc",
	}
	if customResource.Spec.Networking.DNS != nil {
		zone := customResource.Name + "." + customResource.Spec.Networking.DNS.Domain
		zones = append(zones, zone)
	}

	// Check TLS
	errs = append(errs, validateTLS(v, customResource, zones)...)
	errs = append(errs, validateTLSXDCR(v, customResource)...)

	// Require that publically visible service ports have DNS information available.
	if customResource.Spec.IsExposedFeatureServiceTypePublic() || customResource.Spec.IsAdminConsoleServiceTypePublic() {
		if customResource.Spec.Networking.TLS == nil {
			errs = append(errs, errors.Required("spec.tls", "body"))
		}
		if customResource.Spec.Networking.DNS == nil {
			errs = append(errs, errors.Required("spec.dns", "body"))
		}
	}

	if err := validateMemoryConstraints(v, customResource); err != nil {
		errs = append(errs, err)
	}

	// Check mutual verification
	if customResource.Spec.Networking.TLS.ClientCertificatePolicy != nil {
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

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func CheckConstraintsBucket(v *types.Validator, bucket *couchbasev2.CouchbaseBucket) error {
	errs := []error{}

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
	domain := cbmgr.AuthDomain(user.Spec.AuthDomain)
	if domain == cbmgr.InternalAuthDomain {
		// password is required for internal auth domain
		authSecretName := user.Spec.AuthSecret
		if authSecretName == "" {
			emsg := fmt.Sprintf("spec.authSecret for `%s` domain", domain)
			errs = append(errs, errors.Required(emsg, user.Name))
		} else {
			// Check the ldap auth secret exists and has the correct keys
			authSecret, err := v.Abstraction.GetSecret(user.Namespace, authSecretName)
			if err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if apierrors.IsForbidden(err) {
					return nil
				}
				errs = append(errs, err)
			} else {
				if _, ok := authSecret.Data["password"]; !ok {
					errs = append(errs, fmt.Errorf("ldap auth secret %s must contain password", authSecretName))
				}
			}
		}
	} else if domain != cbmgr.LDAPAuthDomain {
		return fmt.Errorf("unsupported auth domain: %s", user.Spec.AuthDomain)
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}
	return nil
}

func CheckConstraintsCouchbaseRole(v *types.Validator, role *couchbasev2.CouchbaseRole) error {
	errs := []error{}

	for index, role := range role.Spec.Roles {
		// role itself must be valid
		isCluterRole := couchbasev2.IsClusterRole(role.Name)
		// Bucket cannot be used with cluster role
		if role.Bucket != "" && isCluterRole {
			errs = append(errs, errors.PropertyNotAllowed(fmt.Sprintf("spec.roles[%d].bucket for cluster role", index), "", role.Name))
		}

	}
	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}
	return nil
}

// validateTLS checks TLS configuration exists and is valid
// * correct secrets exist
// * correct keys exist in the secrets
// * cerificate chain validates with the CA
// * certificates are
//   * in date
//   * have the correct attributes
// * leaf certificate has the correct SANs
func validateTLS(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, zones []string) (errs []error) {
	if cluster.Spec.Networking.TLS != nil {
		// CRD validation requires all the necessary fields are populated
		operatorSecretName := cluster.Spec.Networking.TLS.Static.OperatorSecret
		serverSecretName := cluster.Spec.Networking.TLS.Static.Member.ServerSecret

		var key []byte
		var chain []byte
		var ca []byte
		var ok bool

		// Check the operator secret exists and has the correct keys
		operatorSecret, err := v.Abstraction.GetSecret(cluster.Namespace, operatorSecretName)
		if err != nil {
			// Silently ignore permissions errors, some users may not want us seeing these resources.
			if apierrors.IsForbidden(err) {
				return
			}
			errs = append(errs, err)
		} else if operatorSecret == nil {
			errs = append(errs, fmt.Errorf("secret %s referenced by spec.networking.tls.static.operatorSecret must exist", operatorSecretName))
		} else {
			if ca, ok = operatorSecret.Data["ca.crt"]; !ok {
				errs = append(errs, fmt.Errorf("tls operator secret %s must contain ca.crt", operatorSecretName))
			}
		}

		// Check the server secret exists and has the correct keys
		serverSecret, err := v.Abstraction.GetSecret(cluster.Namespace, serverSecretName)
		if err != nil {
			// Silently ignore permissions errors, some users may not want us seeing these resources.
			if apierrors.IsForbidden(err) {
				return
			}
			errs = append(errs, err)
		} else if serverSecret == nil {
			errs = append(errs, fmt.Errorf("secret %s referenced by spec.networking.tls.static.member.serverSecret must exist", serverSecretName))
		} else {
			if chain, ok = serverSecret.Data["chain.pem"]; !ok {
				errs = append(errs, fmt.Errorf("tls server secret %s must contain chain.pem", serverSecretName))
			}
			if key, ok = serverSecret.Data["pkey.key"]; !ok {
				errs = append(errs, fmt.Errorf("tls server secret %s must contain pkey.key", serverSecretName))
			}
		}

		// Something is wrong, bomb out now
		if len(errs) > 0 {
			return
		}

		// Validate the TLS configuration is going to work
		errs = x509.Verify(ca, chain, key, zones)
		return
	}
	return
}

// validateTLSXDCR checks that TLS configuration for a remote cluster is valid.
// * if set the secret must exist
// * if set the secret must contain a CA
func validateTLSXDCR(v *types.Validator, cluster *couchbasev2.CouchbaseCluster) (errs []error) {
	for _, remoteCluster := range cluster.Spec.XDCR.RemoteClusters {
		if remoteCluster.TLS == nil {
			continue
		}
		if remoteCluster.TLS.Secret != nil {
			secret, err := v.Abstraction.GetSecret(cluster.Namespace, *remoteCluster.TLS.Secret)
			if err != nil {
				// Silently ignore permissions errors, some users may not want us seeing these resources.
				if apierrors.IsForbidden(err) {
					return
				}
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

// validateMemoryConstraints works in two different ways:
// * If a cluster is specified we are creating or updating cluster. Look up all buckets selected by it
//   and ensure the total memory requirements do not surpass the data service memory quota.
// * If a bucket is specified then a bucket is being created or updated.  Look up all clusters that
//   may select the bucket and ensure the total memory requirements do not surpass the data service memory
//   quota for each cluster.
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

	for _, cluster := range clusters.Items {
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

	buckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}
	ephemeralBuckets, err := v.Abstraction.GetCouchbaseEphemeralBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}
	memcachedBuckets, err := v.Abstraction.GetCouchbaseMemcachedBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	allocated := uint64(0)
	for _, bucket := range buckets.Items {
		allocated += uint64(bucket.Spec.MemoryQuota)
	}
	for _, bucket := range ephemeralBuckets.Items {
		allocated += uint64(bucket.Spec.MemoryQuota)
	}
	for _, bucket := range memcachedBuckets.Items {
		allocated += uint64(bucket.Spec.MemoryQuota)
	}

	if allocated > cluster.Spec.ClusterSettings.DataServiceMemQuota {
		return fmt.Errorf("bucket memory allocation (%v) exceeds data service quota (%v) on cluster %s", allocated, cluster.Spec.ClusterSettings.DataServiceMemQuota, cluster.Name)
	}

	return nil
}

// validateBucketExists ensures the specified Couchbase bucket exists.
func validateBucketExists(v *types.Validator, cluster *couchbasev2.CouchbaseCluster, name string) error {
	buckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return err
	}

	for _, bucket := range buckets.Items {
		if bucket.Name == name {
			return nil
		}
	}

	return fmt.Errorf("bucket %s not found", name)
}

func CheckImmutableFields(current, updated *couchbasev2.CouchbaseCluster) error {
	errs := []error{}

	if current.Spec.AntiAffinity != updated.Spec.AntiAffinity {
		errs = append(errs, util.NewUpdateError("spec.antiAffinity", "body"))
	}

	if current.Spec.Security.AdminSecret != updated.Spec.Security.AdminSecret {
		err := util.NewUpdateError("spec.authSecret", "body")
		errs = append(errs, err)
	}

	if !util.StringArrayCompare(current.Spec.ServerGroups, updated.Spec.ServerGroups) {
		errs = append(errs, util.NewUpdateError("spec.serverGroups", "body"))
	}

	for _, cur := range current.Spec.Servers {
		for i, up := range updated.Spec.Servers {
			if cur.Name == up.Name {
				if !util.StringArrayCompare(cur.ServerGroups, up.ServerGroups) {
					errs = append(errs, util.NewUpdateError(fmt.Sprintf("spec.servers[%d].serverGroups", i), "body"))
				}
				if !util.StringArrayCompare(cur.Services.StringSlice(), up.Services.StringSlice()) {
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

	if hasIndexSvc && updated.Spec.ClusterSettings.IndexStorageSetting != current.Spec.ClusterSettings.IndexStorageSetting {
		err := util.NewUpdateError("spec.cluster.indexStorageSetting", "body")
		errs = append(errs, err)
	}

	// volume mounts cannot be added/removed nor can they specify different claim templates
	for _, cur := range current.Spec.Servers {
		// If the current isn't in the updated it's being deleted, ignore
		up := updated.Spec.GetServerConfigByName(cur.Name)
		if up == nil {
			continue
		}

		// If neither have volume mounts specified, ignore
		curPersisted := cur.Pod != nil && cur.Pod.VolumeMounts != nil
		upPersisted := up.Pod != nil && up.Pod.VolumeMounts != nil
		if !curPersisted && !upPersisted {
			continue
		}

		// If one does and the other doesn't raise an error
		if curPersisted != upPersisted {
			errs = append(errs, util.NewUpdateError("spec.servers[*].Pod.VolumeMounts", "body"))
			continue
		}

		// Check the claims are the same
		if cur.Pod.VolumeMounts.DefaultClaim != up.Pod.VolumeMounts.DefaultClaim {
			errs = append(errs, util.NewUpdateError("default", "spec.servers[*].Pod.VolumeMounts"))
		}
		if cur.Pod.VolumeMounts.DataClaim != up.Pod.VolumeMounts.DataClaim {
			errs = append(errs, util.NewUpdateError("data", "spec.servers[*].Pod.VolumeMounts"))
		}
		if cur.Pod.VolumeMounts.IndexClaim != up.Pod.VolumeMounts.IndexClaim {
			errs = append(errs, util.NewUpdateError("index", "spec.servers[*].Pod.VolumeMounts"))
		}
		if cur.Pod.VolumeMounts.LogsClaim != up.Pod.VolumeMounts.LogsClaim {
			errs = append(errs, util.NewUpdateError("logs", "spec.servers[*].Pod.VolumeMounts"))
		}
		if !util.StringArrayCompareOrdered(cur.Pod.VolumeMounts.AnalyticsClaims, up.Pod.VolumeMounts.AnalyticsClaims) {
			errs = append(errs, util.NewUpdateError("analytics", "spec.servers[*].Pod.VolumeMounts"))
		}
	}

	// persistent volume claim templates are immutable
	for _, cur := range current.Spec.VolumeClaimTemplates {
		for _, up := range updated.Spec.VolumeClaimTemplates {
			if cur.Name == up.Name {
				if !util.StringPtrEquals(cur.Spec.StorageClassName, up.Spec.StorageClassName) {
					err := util.NewUpdateError(`"storageClassName"`, "spec.volumeClaimTemplates[*]")
					errs = append(errs, err)
				}

				// cannot change storage requests or limits
				for resource, curQuantity := range cur.Spec.Resources.Requests {
					if string(resource) == "storage" {
						upStorageQuantity, ok := up.Spec.Resources.Requests[resource]
						if ok {
							if curQuantity.Cmp(upStorageQuantity) != 0 {
								err := util.NewUpdateError(`"storage"`, "spec.volumeClaimTemplates[*].resources.requests")
								errs = append(errs, err)
							}
						}
					}
				}
				for resource, curQuantity := range cur.Spec.Resources.Limits {
					if string(resource) == "storage" {
						upStorageQuantity, ok := up.Spec.Resources.Limits[resource]
						if ok {
							if curQuantity.Cmp(upStorageQuantity) != 0 {
								err := util.NewUpdateError(`"storage"`, "spec.volumeClaimTemplates[*].resources.limits")
								errs = append(errs, err)
							}
						}
					}
				}
			}
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
	// Todo validate all clusters referencing the replication also reference the
	// source bucket.
	return nil
}
