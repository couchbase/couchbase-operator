package v1

import (
	"fmt"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	validationv1 "github.com/couchbase/couchbase-operator/pkg/util/k8sutil/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"
	"github.com/couchbase/gocbmgr"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/go-openapi/errors"
)

const (
	defaultBaseImage                              = "couchbase/server"
	defaultIndexStorageSetting                    = "memory_optimized"
	defaultAutoFailoverTimeout                    = 120
	defaultAutoFailoverMaxCount                   = 3
	defaultAutoFailoverOnDataDiskIssuesTimePeriod = 120
	defaultServiceMemQuota                        = 256
	defaultAnalyticsServiceMemQuota               = 1024
)

func ApplyDefaults(object *unstructured.Unstructured) jsonpatch.PatchList {
	var patch jsonpatch.PatchList

	if _, found, _ := unstructured.NestedString(object.Object, "spec", "clusterSettings", "clusterName"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/clusterSettings/clusterName", Value: object.GetName()})
	}
	if _, found, _ := unstructured.NestedString(object.Object, "spec", "bsaeImage"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/baseImage", Value: defaultBaseImage})
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
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "adminConsoleServiceType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/adminConsoleServiceType", Value: corev1.ServiceTypeNodePort})
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(object.Object, "spec", "exposedFeatureServiceType"); !found {
		patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: "/spec/exposedFeatureServiceType", Value: corev1.ServiceTypeNodePort})
	}
	if buckets, found, _ := unstructured.NestedSlice(object.Object, "spec", "buckets"); !found {
		for i, bucket := range buckets {
			b, ok := bucket.(map[string]interface{})
			if !ok {
				continue
			}
			if _, found, _ := unstructured.NestedFieldNoCopy(b, "compressionMode"); !found {
				patch = append(patch, jsonpatch.Patch{Op: jsonpatch.Add, Path: fmt.Sprintf("/spec/buckets/%d/compressionMode", i), Value: cbmgr.CompressionModePassive})
			}
		}
	}

	return patch
}

func CheckConstraints(v *types.Validator, customResource *couchbasev1.CouchbaseCluster) error {
	// Custom validation
	errs := []error{}

	// Ensure secret exists
	if secret, err := v.Abstraction.GetSecret(customResource.Namespace, customResource.Spec.AuthSecret); err != nil {
		// Silently ignore permissions errors, some users may not want us seeing these resources.
		if !apierrors.IsForbidden(err) {
			errs = append(errs, err)
		}
	} else if secret == nil {
		errs = append(errs, fmt.Errorf("secret %s must exist", customResource.Spec.AuthSecret))
	}

	// Ensure one service is specified when the admin console is exposed
	if customResource.Spec.ExposeAdminConsole && len(customResource.Spec.AdminConsoleServices) == 0 {
		errs = append(errs, errors.Required("spec.adminConsoleServices", "body"))
	}

	// Uniqueness, although in the schema types, the API denies it.
	if !util.UniqueString(customResource.Spec.AdminConsoleServices.StringSlice()) {
		errs = append(errs, errors.DuplicateItems("spec.adminConsoleServices", "body"))
	}
	if !util.UniqueString(customResource.Spec.ExposedFeatures) {
		errs = append(errs, errors.DuplicateItems("spec.exposedFeatures", "body"))
	}
	if !util.UniqueString(customResource.Spec.ServerGroups) {
		errs = append(errs, errors.DuplicateItems("spec.serverGroups", "body"))
	}
	for i, class := range customResource.Spec.ServerSettings {
		if !util.UniqueString(class.Services.StringSlice()) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].services", i), "body"))
		}
		if !util.UniqueString(class.ServerGroups) {
			errs = append(errs, errors.DuplicateItems(fmt.Sprintf("spec.servers[%d].serverGroups", i), "body"))
		}
	}

	// Check optional parameters
	if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssues && customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod == 0 {
		// If we want to auto failover on disk issues, we must specify a time period.  CRD validation
		// will catch where it is specified and out of bounds. We can catch the fact it is unspecified
		// by checking for the zero value
		errs = append(errs, errors.Required("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod", "body"))
	}

	// Ensure buckets are named uniquely
	bucketNames := []string{}
	for _, bucket := range customResource.Spec.BucketSettings {
		bucketNames = append(bucketNames, bucket.BucketName)
	}
	if !util.UniqueString(bucketNames) {
		errs = append(errs, errors.DuplicateItems("spec.buckets.name", "body"))
	}

	// Ensure unnecessary settings in memcached and ephemeral buckets are nil
	for i := range customResource.Spec.BucketSettings {
		if customResource.Spec.BucketSettings[i].BucketType == constants.BucketTypeCouchbase {
			continue
		}

		if customResource.Spec.BucketSettings[i].EnableIndexReplica {
			err := errors.InvalidType("enableReplicaIndex", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].BucketType == constants.BucketTypeEphemeral {
			continue
		}

		if customResource.Spec.BucketSettings[i].BucketReplicas != 0 {
			err := errors.InvalidType("replicas", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].ConflictResolution != "" {
			err := errors.InvalidType("conflictResolution", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].EvictionPolicy != "" {
			err := errors.InvalidType("evictionPolicy", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].IoPriority != "" {
			err := errors.InvalidType("ioPriority", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].CompressionMode != "" {
			err := errors.InvalidType("compressionMode", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}
	}

	// Check bucket parameter constraints
	for i, bucket := range customResource.Spec.BucketSettings {
		if bucket.BucketType == constants.BucketTypeMemcached {
			continue
		}

		if customResource.Spec.BucketSettings[i].ConflictResolution == "" {
			errs = append(errs, errors.Required("conflictResolution", fmt.Sprintf("spec.buckets[%d]", i)))
		}

		if customResource.Spec.BucketSettings[i].EvictionPolicy == "" {
			errs = append(errs, errors.Required("evictionPolicy", fmt.Sprintf("spec.buckets[%d]", i)))
		}

		if customResource.Spec.BucketSettings[i].IoPriority == "" {
			errs = append(errs, errors.Required("ioPriority", fmt.Sprintf("spec.buckets[%d]", i)))
		}

		if customResource.Spec.BucketSettings[i].CompressionMode == "" {
			errs = append(errs, errors.Required("compressionMode", fmt.Sprintf("spec.buckets[%d]", i)))
		}

		switch bucket.BucketType {
		case constants.BucketTypeEphemeral:
			evictionPolicies := util.EnumList{
				constants.BucketEvictionPolicyNoEviction,
				constants.BucketEvictionPolicyNRUEviction,
			}
			if !evictionPolicies.Contains(bucket.EvictionPolicy) {
				errs = append(errs, errors.EnumFail("evictionPolicy", fmt.Sprintf("spec.buckets[%d]", i), nil, evictionPolicies.Interfaces()))
			}
		case constants.BucketTypeCouchbase:
			evictionPolicies := util.EnumList{
				constants.BucketEvictionPolicyValueOnly,
				constants.BucketEvictionPolicyFullEviction,
			}
			if !evictionPolicies.Contains(bucket.EvictionPolicy) {
				errs = append(errs, errors.EnumFail("evictionPolicy", fmt.Sprintf("spec.buckets[%d]", i), nil, evictionPolicies.Interfaces()))
			}
		}
	}

	// Check that the total memory quota is valid
	var totalBucketMemory uint64
	for _, bucket := range customResource.Spec.BucketSettings {
		totalBucketMemory += uint64(bucket.BucketMemoryQuota)
	}

	maxBucketQuota := customResource.Spec.ClusterSettings.DataServiceMemQuota
	if totalBucketMemory > maxBucketQuota {
		err := errors.ExceedsMaximumInt("spec.buckets[*].memoryQuota", "body", int64(maxBucketQuota), false)
		errs = append(errs, err)
	}

	// Check to make sure:
	// 1. Server names are unique
	// 2. The data service is specified on at least one node
	unique := make(map[string]bool)
	hasDataService := false
	for i := range customResource.Spec.ServerSettings {
		if _, ok := unique[customResource.Spec.ServerSettings[i].Name]; ok {
			errs = append(errs, errors.DuplicateItems("spec.servers.name", "body"))
		}

		for _, svc := range customResource.Spec.ServerSettings[i].Services {
			if svc == "data" {
				hasDataService = true
			}
		}

		unique[customResource.Spec.ServerSettings[i].Name] = true
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
	for _, class := range customResource.Spec.ServerSettings {
		if class.Pod != nil && class.Pod.VolumeMounts != nil {
			if class.Pod.VolumeMounts.DefaultClaim != "" || class.Pod.VolumeMounts.LogsClaim != "" {
				anySupportable = true
			}
		}
	}

	if anySupportable {
		for index, class := range customResource.Spec.ServerSettings {
			// Volume mounts must be specified if any others are supportable
			if class.Pod == nil || class.Pod.VolumeMounts == nil {
				errs = append(errs, errors.Required("volumeMounts", fmt.Sprintf("spec.servers[%d].pod", index)))
			} else {
				// These stateful services must have a "default" mount
				if class.Services.ContainsAny(couchbasev1.DataService, couchbasev1.IndexService, couchbasev1.AnalyticsService) &&
					class.Pod.VolumeMounts.DefaultClaim == "" {
					errs = append(errs, errors.Required("default", fmt.Sprintf("spec.servers[%d].pod.volumeMounts", index)))
				}
			}
		}
	}

	// validate persistent volume spec such that when volumeMounts are specified, claim for
	// `default` must be provided, and all mounts much pair to associated persistentVolumeClaims.
	// `logs` claim cannot be used in conjunction with `default` claim.
	for index, config := range customResource.Spec.ServerSettings {
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
			if mounts.DataClaim != "" && !config.Services.Contains(couchbasev1.DataService) {
				errs = append(errs, errors.Required(string(couchbasev1.DataService), fmt.Sprintf("spec.servers[%d].services", index)))
			}
			if mounts.IndexClaim != "" && !config.Services.Contains(couchbasev1.IndexService) {
				errs = append(errs, errors.Required(string(couchbasev1.IndexService), fmt.Sprintf("spec.servers[%d].services", index)))
			}
			if mounts.AnalyticsClaims != nil && !config.Services.Contains(couchbasev1.AnalyticsService) {
				errs = append(errs, errors.Required(string(couchbasev1.AnalyticsService), fmt.Sprintf("spec.servers[%d].services", index)))
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
	if err := couchbaseutil.VerifyVersion(customResource.Spec.Version); err != nil {
		err := errors.FailedPattern("spec.version", "body", validationv1.VersionPattern)
		errs = append(errs, err)
	}

	// Record the zones that the server certificate needs to support as we look at the network configuration.
	zones := []string{
		customResource.Name + "." + customResource.Namespace + ".svc",
	}
	if customResource.Spec.DNS != nil {
		zone := customResource.Name + "." + customResource.Spec.DNS.Domain
		zones = append(zones, zone)
	}

	// Check TLS
	errs = append(errs, validateTLS(v, customResource, zones)...)

	// Require that publically visible service ports have DNS information available.
	if customResource.Spec.IsExposedFeatureServiceTypePublic() || customResource.Spec.IsAdminConsoleServiceTypePublic() {
		if customResource.Spec.TLS == nil {
			errs = append(errs, errors.Required("spec.tls", "body"))
		}
		if customResource.Spec.DNS == nil {
			errs = append(errs, errors.Required("spec.dns", "body"))
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
func validateTLS(v *types.Validator, cluster *couchbasev1.CouchbaseCluster, zones []string) (errs []error) {
	if cluster.Spec.TLS != nil {
		// CRD validation requires all the necessary fields are populated
		operatorSecretName := cluster.Spec.TLS.Static.OperatorSecret
		serverSecretName := cluster.Spec.TLS.Static.Member.ServerSecret

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
			errs = append(errs, fmt.Errorf("tls operator secret %s must exist", operatorSecretName))
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
			errs = append(errs, fmt.Errorf("tls server secret %s must exist", serverSecretName))
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

func CheckImmutableFields(current, updated *couchbasev1.CouchbaseCluster) error {
	errs := []error{}

	if current.Spec.AntiAffinity != updated.Spec.AntiAffinity {
		errs = append(errs, util.NewUpdateError("spec.antiAffinity", "body"))
	}

	if current.Spec.AuthSecret != updated.Spec.AuthSecret {
		err := util.NewUpdateError("spec.authSecret", "body")
		errs = append(errs, err)
	}

	if !util.StringArrayCompare(current.Spec.ServerGroups, updated.Spec.ServerGroups) {
		errs = append(errs, util.NewUpdateError("spec.serverGroups", "body"))
	}

	for _, cur := range current.Spec.BucketSettings {
		for i, up := range updated.Spec.BucketSettings {
			if cur.BucketName == up.BucketName {
				if cur.BucketType != up.BucketType {
					err := util.NewUpdateError(fmt.Sprintf("spec.buckets[%d].type", i), "body")
					errs = append(errs, err)
				}

				if cur.ConflictResolution != up.ConflictResolution {
					err := util.NewUpdateError(fmt.Sprintf("spec.buckets[%d].conflictResolution", i), "body")
					errs = append(errs, err)
				}
			}
		}
	}

	for _, cur := range current.Spec.ServerSettings {
		for i, up := range updated.Spec.ServerSettings {
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
	for _, cur := range current.Spec.ServerSettings {
		for _, svc := range cur.Services {
			if svc == couchbasev1.IndexService {
				hasIndexSvc = true
			}
		}
	}

	for _, up := range updated.Spec.ServerSettings {
		for _, svc := range up.Services {
			if svc == couchbasev1.IndexService {
				hasIndexSvc = true
			}
		}
	}

	if hasIndexSvc && updated.Spec.ClusterSettings.IndexStorageSetting != current.Spec.ClusterSettings.IndexStorageSetting {
		err := util.NewUpdateError("spec.cluster.indexStorageSetting", "body")
		errs = append(errs, err)
	}

	// volume mounts cannot be added/removed nor can they specify different claim templates
	for _, cur := range current.Spec.ServerSettings {
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
	upgradeCondition := current.Status.GetCondition(couchbasev1.ClusterConditionUpgrading)
	if upgradeCondition == nil && current.Spec.Version != updated.Spec.Version {
		src, err := couchbaseutil.NewVersion(current.Spec.Version)
		if err != nil {
			errs = append(errs, err)
		}
		dst, err := couchbaseutil.NewVersion(updated.Spec.Version)
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
	if upgradeCondition != nil && current.Spec.Version != updated.Spec.Version {
		if updated.Spec.Version != current.Status.CurrentVersion {
			errs = append(errs, util.NewUpdateError("spec.version", "body"))
		}
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}
