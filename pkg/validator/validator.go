package validator

import (
	"fmt"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/go-openapi/errors"
)

const (
	DefaultBaseImage                              = "couchbase/server"
	DefaultIndexStorageSetting                    = "default"
	DefaultAutoFailoverTimeout                    = 120
	DefaultAutoFailoverMaxCount                   = 3
	DefaultAutoFailoverOnDataDiskIssuesTimePeriod = 120
	DefaultServiceMemQuota                        = 256
	DefaultAnalyticsServiceMemQuota               = 1024
)

type Warning string

func (w Warning) String() string {
	return fmt.Sprintf("Warning: %s", string(w))
}

type EnumList []string

func (e EnumList) Contains(s string) bool {
	for _, element := range e {
		if element == s {
			return true
		}
	}
	return false
}

func (e EnumList) Interfaces() []interface{} {
	i := []interface{}{}
	for _, element := range e {
		i = append(i, element)
	}
	return i
}

func BoundedErrorUint(name, in string, value, min, max uint64) error {
	if value < min {
		return errors.ExceedsMinimumUint(name, in, min, false)
	} else if value > max {
		return errors.ExceedsMaximumUint(name, in, max, false)
	}
	return nil
}

func Create(resource *api.CouchbaseCluster) error {
	applyDefaults(resource)

	if err := k8sutil.ValidateCRD(resource); err != nil {
		return err
	}

	if err := checkConstraints(resource); err != nil {
		return err
	}

	return nil
}

func Update(current, updated *api.CouchbaseCluster) (error, []Warning) {
	applyDefaults(updated)

	err, warn := checkImmutableFields(current, updated)
	if err != nil {
		return err, warn
	}

	if err := k8sutil.ValidateCRD(updated); err != nil {
		return err, warn
	}

	if err := checkConstraints(updated); err != nil {
		return err, warn
	}

	updated.ResourceVersion = current.ResourceVersion
	return nil, warn
}

func applyDefaults(customResource *api.CouchbaseCluster) {
	if customResource.Spec.BaseImage == "" {
		customResource.Spec.BaseImage = DefaultBaseImage
	}

	if customResource.Spec.ExposedFeatures == nil {
		customResource.Spec.ExposedFeatures = []string{}
	}

	if customResource.Spec.AdminConsoleServices == nil {
		customResource.Spec.AdminConsoleServices = []string{}
	}

	if customResource.Spec.ClusterSettings.DataServiceMemQuota == 0 {
		customResource.Spec.ClusterSettings.DataServiceMemQuota = DefaultServiceMemQuota
	}

	if customResource.Spec.ClusterSettings.IndexServiceMemQuota == 0 {
		customResource.Spec.ClusterSettings.IndexServiceMemQuota = DefaultServiceMemQuota
	}

	if customResource.Spec.ClusterSettings.SearchServiceMemQuota == 0 {
		customResource.Spec.ClusterSettings.SearchServiceMemQuota = DefaultServiceMemQuota
	}

	if customResource.Spec.ClusterSettings.EventingServiceMemQuota == 0 {
		customResource.Spec.ClusterSettings.EventingServiceMemQuota = DefaultServiceMemQuota
	}

	if customResource.Spec.ClusterSettings.AnalyticsServiceMemQuota == 0 {
		customResource.Spec.ClusterSettings.AnalyticsServiceMemQuota = DefaultAnalyticsServiceMemQuota
	}

	if customResource.Spec.ClusterSettings.IndexStorageSetting == "" {
		customResource.Spec.ClusterSettings.IndexStorageSetting = DefaultIndexStorageSetting
	}

	if customResource.Spec.ClusterSettings.AutoFailoverTimeout == 0 {
		customResource.Spec.ClusterSettings.AutoFailoverTimeout = DefaultAutoFailoverTimeout
	}

	if customResource.Spec.ClusterSettings.AutoFailoverMaxCount == 0 {
		customResource.Spec.ClusterSettings.AutoFailoverMaxCount = DefaultAutoFailoverMaxCount
	}

	if customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod == 0 {
		customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod = DefaultAutoFailoverOnDataDiskIssuesTimePeriod
	}
}

func checkConstraints(customResource *api.CouchbaseCluster) error {
	errs := []error{}

	if customResource.Spec.ExposedFeatures != nil {
		if len(customResource.Spec.ExposedFeatures) > len(api.SupportedFeatures) {
			err := errors.TooManyItems("spec.exposedFeatures", "body", int64(len(api.SupportedFeatures)))
			errs = append(errs, err)
		}

		for _, feature := range customResource.Spec.ExposedFeatures {
			found := false
			for _, f := range api.SupportedFeatures {
				if feature == f {
					found = true
					break
				}
			}
			if !found {
				enums := make([]interface{}, len(api.SupportedFeatures))
				for idx, enum := range api.SupportedFeatures {
					enums[idx] = enum
				}
				err := errors.EnumFail("spec.exposedFeatures", "body", nil, enums)
				errs = append(errs, err)
			}
		}
	}

	if customResource.Spec.AdminConsoleServices != nil {
		services := EnumList{"data", "index", "query", "search", "eventing", "analytics"}

		if len(customResource.Spec.AdminConsoleServices) > len(services) {
			err := errors.TooManyItems("spec.adminConsoleServices", "body", int64(len(services)))
			errs = append(errs, err)
		}

		for _, svc := range customResource.Spec.AdminConsoleServices {
			if !services.Contains(svc) {
				err := errors.EnumFail("spec.adminConsoleServices", "body", nil,
					services.Interfaces())
				errs = append(errs, err)
				continue
			}
		}
	}

	// Check memory quota limits
	if customResource.Spec.ClusterSettings.DataServiceMemQuota < DefaultServiceMemQuota {
		errs = append(errs, errors.ExceedsMinimumUint("spec.cluster.dataServiceMemoryQuota", "body", DefaultServiceMemQuota, false))
	}
	if customResource.Spec.ClusterSettings.IndexServiceMemQuota < DefaultServiceMemQuota {
		errs = append(errs, errors.ExceedsMinimumUint("spec.cluster.indexServiceMemoryQuota", "body", DefaultServiceMemQuota, false))
	}
	if customResource.Spec.ClusterSettings.SearchServiceMemQuota < DefaultServiceMemQuota {
		errs = append(errs, errors.ExceedsMinimumUint("spec.cluster.searchServiceMemoryQuota", "body", DefaultServiceMemQuota, false))
	}
	if customResource.Spec.ClusterSettings.EventingServiceMemQuota < DefaultServiceMemQuota {
		errs = append(errs, errors.ExceedsMinimumUint("spec.cluster.eventingServiceMemoryQuota", "body", DefaultServiceMemQuota, false))
	}
	if customResource.Spec.ClusterSettings.AnalyticsServiceMemQuota < DefaultAnalyticsServiceMemQuota {
		errs = append(errs, errors.ExceedsMinimumUint("spec.cluster.analyticsServiceMemoryQuota", "body", DefaultAnalyticsServiceMemQuota, false))
	}

	// Ensure unnecessary settings in memcached and ephemeral buckets are nil
	for i, _ := range customResource.Spec.BucketSettings {
		if customResource.Spec.BucketSettings[i].BucketType == constants.BucketTypeCouchbase {
			continue
		}

		if customResource.Spec.BucketSettings[i].EnableIndexReplica != nil {
			err := errors.InvalidType("enableReplicaIndex", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].BucketType == constants.BucketTypeEphemeral {
			continue
		}

		if customResource.Spec.BucketSettings[i].BucketReplicas != nil {
			err := errors.InvalidType("bucketReplicas", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].ConflictResolution != nil {
			err := errors.InvalidType("conflictResolution", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].EvictionPolicy != nil {
			err := errors.InvalidType("evictionPolicy", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].IoPriority != nil {
			err := errors.InvalidType("ioPriority", fmt.Sprintf("spec.buckets[%d]", i),
				"nil", fmt.Sprintf("Bucket type is %s", customResource.Spec.BucketSettings[i].BucketType))
			errs = append(errs, err)
		}
	}

	// Check bucket parameter constraints
	for _, bucket := range customResource.Spec.BucketSettings {
		if bucket.BucketType == constants.BucketTypeMemcached {
			continue
		}

		if bucket.BucketReplicas == nil {
			errs = append(errs, errors.Required("spec.buckets.replicas", "body"))
		} else if *bucket.BucketReplicas < 0 {
			errs = append(errs, errors.ExceedsMinimumInt("spec.buckets.replicas", "body", int64(constants.BucketReplicasZero), false))
		} else if *bucket.BucketReplicas > 3 {
			errs = append(errs, errors.ExceedsMaximumInt("spec.buckets.replicas", "body", int64(constants.BucketReplicasThree), false))
		}

		ioPriorities := EnumList{
			constants.BucketIoPriorityHigh,
			constants.BucketIoPriorityLow,
		}
		if bucket.IoPriority == nil || !ioPriorities.Contains(*bucket.IoPriority) {
			errs = append(errs, errors.EnumFail("spec.buckets.ioPriority", "body", nil, ioPriorities.Interfaces()))
		}

		conflictResolutions := EnumList{
			constants.BucketConflictResolutionSeqno,
			constants.BucketConflictResolutionTimestamp,
		}
		if bucket.ConflictResolution == nil || !conflictResolutions.Contains(*bucket.ConflictResolution) {
			errs = append(errs, errors.EnumFail("spec.buckets.conflictResolution", "body", nil, conflictResolutions.Interfaces()))
		}

		switch bucket.BucketType {
		case constants.BucketTypeEphemeral:
			evictionPolicies := EnumList{
				constants.BucketEvictionPolicyNoEviction,
				constants.BucketEvictionPolicyNRUEviction,
			}
			if bucket.EvictionPolicy == nil || !evictionPolicies.Contains(*bucket.EvictionPolicy) {
				errs = append(errs, errors.EnumFail("spec.buckets.evictionPolicy", "body", nil, evictionPolicies.Interfaces()))
			}
		case constants.BucketTypeCouchbase:
			evictionPolicies := EnumList{
				constants.BucketEvictionPolicyValueOnly,
				constants.BucketEvictionPolicyFullEviction,
			}
			if bucket.EvictionPolicy == nil || !evictionPolicies.Contains(*bucket.EvictionPolicy) {
				errs = append(errs, errors.EnumFail("spec.buckets.evictionPolicy", "body", nil, evictionPolicies.Interfaces()))
			}

			if bucket.EnableIndexReplica == nil {
				errs = append(errs, errors.Required("spec.buckets.enableIndexReplica", "body"))
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

	// Check auto failover settings
	timeout := customResource.Spec.ClusterSettings.AutoFailoverTimeout
	if err := BoundedErrorUint("spec.cluster.autoFailoverTimeout", "body", timeout, constants.AutoFailoverTimeoutMin, constants.AutoFailoverTimeoutMax); err != nil {
		errs = append(errs, err)
	}

	maxCount := customResource.Spec.ClusterSettings.AutoFailoverMaxCount
	if err := BoundedErrorUint("spec.cluster.autoFailoverMaxCount", "body", maxCount, constants.AutoFailoverMaxCountMin, constants.AutoFailoverMaxCountMax); err != nil {
		errs = append(errs, err)
	}

	dataDiskIssuesTimePeriod := customResource.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod
	if err := BoundedErrorUint("spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod", "body", dataDiskIssuesTimePeriod, constants.AutoFailoverOnDataDiskIssuesTimePeriodMin, constants.AutoFailoverOnDataDiskIssuesTimePeriodMax); err != nil {
		errs = append(errs, err)
	}

	// Check to make sure:
	// 1. Server names are unique
	// 2. The data service is specified on at least one node
	unique := make(map[string]bool)
	hasDataService := false
	for i, _ := range customResource.Spec.ServerSettings {
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
		err := errors.Required("at least on \"data\" service", "spec.servers[*].services")
		errs = append(errs, err)
	}

	// validate persistent volume spec such that when volumeMounts are specified, claim for
	// `default` must be provided, and all mounts much pair to associated persistentVolumeClaims
	for _, config := range customResource.Spec.ServerSettings {
		if config.Pod != nil && config.Pod.VolumeMounts != nil {
			mounts := config.Pod.VolumeMounts
			if mounts.DefaultClaim == nil {
				err := errors.Required(`"default"`, "spec.servers[*].Pod.VolumeMounts")
				errs = append(errs, err)
			} else {
				if template := customResource.Spec.GetVolumeClaimTemplate(*mounts.DefaultClaim); template == nil {
					err := errors.Required(fmt.Sprintf(`"%s"`, *mounts.DefaultClaim), "spec.volumeClaimTemplates[*].metadata.name")
					errs = append(errs, err)
				}
				if mounts.DataClaim != nil {
					if template := customResource.Spec.GetVolumeClaimTemplate(*mounts.DataClaim); template == nil {
						err := errors.Required(fmt.Sprintf(`"%s"`, *mounts.DataClaim), "spec.volumeClaimTemplates[*].metadata.name")
						errs = append(errs, err)
					}
				}
				if mounts.IndexClaim != nil {
					if template := customResource.Spec.GetVolumeClaimTemplate(*mounts.IndexClaim); template == nil {
						err := errors.Required(fmt.Sprintf(`"%s"`, *mounts.IndexClaim), "spec.volumeClaimTemplates[*].metadata.name")
						errs = append(errs, err)
					}
				}
			}
		}
	}

	// validate claim templates such that storage class is provided along with valid request
	for _, pvc := range customResource.Spec.VolumeClaimTemplates {

		if pvc.Spec.StorageClassName == nil {
			err := errors.Required(`"storageClassName"`, "spec.volumeClaimTemplates[*]")
			errs = append(errs, err)
		}

		// requests or limits required
		hasStorageQuantity := false
		for resource, quantity := range pvc.Spec.Resources.Requests {
			if string(resource) == "storage" {
				if !quantity.IsZero() {
					hasStorageQuantity = true
				}
			}
		}
		for resource, quantity := range pvc.Spec.Resources.Limits {
			if string(resource) == "storage" {
				if !quantity.IsZero() {
					hasStorageQuantity = true
				}
			}
		}
		if !hasStorageQuantity {
			err := errors.Required(`"storage"`, "spec.volumeClaimTemplates[*].resources.requests|limits")
			errs = append(errs, err)
		}
	}

	// version check
	err := couchbaseutil.VerifyVersion(customResource.Spec.Version)
	if err != nil {
		err := errors.FailedPattern("version", "spec.Version", `i.e "enterprise-5.5.0"`)
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

type UpdateError struct {
	field string
	in    string
}

func (e *UpdateError) Error() string {
	return fmt.Sprintf("%s in %s cannot be updated", e.field, e.in)
}

func checkImmutableFields(current, updated *api.CouchbaseCluster) (error, []Warning) {
	warns := []Warning{}
	errs := []error{}
	if current.Spec.AuthSecret != updated.Spec.AuthSecret {
		err := &UpdateError{"spec.authSecret", "body"}
		errs = append(errs, err)
	}

	for _, cur := range current.Spec.BucketSettings {
		for i, up := range updated.Spec.BucketSettings {
			if cur.BucketName == up.BucketName {
				if cur.BucketType != up.BucketType {
					err := &UpdateError{fmt.Sprintf("spec.buckets[%d].type", i), "body"}
					errs = append(errs, err)
				}

				if !stringPtrEquals(cur.ConflictResolution, up.ConflictResolution) {
					err := &UpdateError{fmt.Sprintf("spec.buckets[%d].conflictResolution", i), "body"}
					errs = append(errs, err)
				}

				if !stringPtrEquals(cur.IoPriority, up.IoPriority) {
					warn := Warning(fmt.Sprintf("Changing the IO Priority will cause the bucket %s to be temporarily unavailable", cur.BucketName))
					warns = append(warns, warn)
				}

				if !stringPtrEquals(cur.EvictionPolicy, up.EvictionPolicy) {
					warn := Warning(fmt.Sprintf("Changing the Eviction Policy will cause the bucket %s to be temporarily unavailable", cur.BucketName))
					warns = append(warns, warn)
				}
			}
		}
	}

	for _, cur := range current.Spec.ServerSettings {
		for i, up := range updated.Spec.ServerSettings {
			if cur.Name == up.Name {
				if !stringArrayCompare(cur.Services, up.Services) {
					err := &UpdateError{fmt.Sprintf("spec.servers[%d].services", i), "body"}
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
			if svc == constants.ServiceIndex {
				hasIndexSvc = true
			}
		}
	}

	for _, up := range updated.Spec.ServerSettings {
		for _, svc := range up.Services {
			if svc == constants.ServiceIndex {
				hasIndexSvc = true
			}
		}
	}

	if hasIndexSvc && updated.Spec.ClusterSettings.IndexStorageSetting != current.Spec.ClusterSettings.IndexStorageSetting {
		err := &UpdateError{"spec.cluster.indexStorageSetting", "body"}
		errs = append(errs, err)
	}

	// volume mounts cannot be added/removed nor can they specify different claim templates
	for _, cur := range current.Spec.ServerSettings {
		for _, up := range updated.Spec.ServerSettings {
			curPersisted := cur.Pod != nil && cur.Pod.VolumeMounts != nil
			upPersisted := up.Pod != nil && up.Pod.VolumeMounts != nil
			if curPersisted != upPersisted {
				err := &UpdateError{"spec.servers[*].Pod.VolumeMounts", "body"}
				errs = append(errs, err)
			}
			if curPersisted && upPersisted {
				if !stringPtrEquals(cur.Pod.VolumeMounts.DefaultClaim, up.Pod.VolumeMounts.DefaultClaim) {
					err := &UpdateError{"default", "spec.servers[*].Pod.VolumeMounts"}
					errs = append(errs, err)
				}
				if !stringPtrEquals(cur.Pod.VolumeMounts.DataClaim, up.Pod.VolumeMounts.DataClaim) {
					err := &UpdateError{"data", "spec.servers[*].Pod.VolumeMounts"}
					errs = append(errs, err)
				}
				if !stringPtrEquals(cur.Pod.VolumeMounts.IndexClaim, up.Pod.VolumeMounts.IndexClaim) {
					err := &UpdateError{"index", "spec.servers[*].Pod.VolumeMounts"}
					errs = append(errs, err)
				}
			}
		}
	}

	// persistent volume claim templates are immutable
	for _, cur := range current.Spec.VolumeClaimTemplates {
		for _, up := range updated.Spec.VolumeClaimTemplates {
			if cur.Name == up.Name {
				if !stringPtrEquals(cur.Spec.StorageClassName, up.Spec.StorageClassName) {
					err := &UpdateError{`"storageClassName"`, "spec.volumeClaimTemplates[*]"}
					errs = append(errs, err)
				}

				// cannot change storage requests or limits
				for resource, curQuantity := range cur.Spec.Resources.Requests {
					if string(resource) == "storage" {
						upStorageQuantity, ok := up.Spec.Resources.Requests[resource]
						if ok {
							if curQuantity.Cmp(upStorageQuantity) != 0 {
								err := &UpdateError{`"storage"`, "spec.volumeClaimTemplates[*].resources.requests"}
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
								err := &UpdateError{`"storage"`, "spec.volumeClaimTemplates[*].resources.limits"}
								errs = append(errs, err)
							}
						}
					}
				}
			}
		}
	}

	if len(warns) == 0 {
		warns = nil
	}

	if len(errs) > 0 {
		return errors.CompositeValidationError(errs...), warns
	}

	return nil, warns
}

func stringArrayCompare(a1, a2 []string) bool {
	m := make(map[string]int)
	for _, val := range a1 {
		m[val]++
	}

	for _, val := range a2 {
		if _, ok := m[val]; ok {
			if m[val] > 0 {
				m[val]--
				continue
			}
		}
		return false
	}

	for _, cnt := range m {
		if cnt > 0 {
			return false
		}
	}

	return true
}

func stringPtrEquals(p1, p2 *string) bool {
	return (p1 == nil && p2 == nil) || (p1 != nil && p2 != nil && *p1 == *p2)
}
