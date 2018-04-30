package validator

import (
	"fmt"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/go-openapi/errors"
)

const (
	DefaultBaseImage           = "couchbase/server"
	DefaultIndexStorageSetting = "default"
	DefaultAutoFailoverTimeout = 30
	DefaultServiceMemQuota     = 256
)

type Warning string

func (w Warning) String() string {
	return fmt.Sprintf("Warning: %s", string(w))
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

	if customResource.Spec.ClusterSettings.IndexStorageSetting == "" {
		customResource.Spec.ClusterSettings.IndexStorageSetting = DefaultIndexStorageSetting
	}

	if customResource.Spec.ClusterSettings.AutoFailoverTimeout == 0 {
		customResource.Spec.ClusterSettings.AutoFailoverTimeout = DefaultAutoFailoverTimeout
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
		if len(customResource.Spec.AdminConsoleServices) > 4 {
			err := errors.TooManyItems("spec.adminConsoleServices", "body", 4)
			errs = append(errs, err)
		}

		for _, svc := range customResource.Spec.AdminConsoleServices {
			if svc != "data" && svc != "index" && svc != "query" && svc != "search" {
				err := errors.EnumFail("spec.adminConsoleServices", "body", nil,
					[]interface{}{"data", "index", "query", "search"})
				errs = append(errs, err)
				continue
			}
		}
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
	for i, _ := range customResource.Spec.BucketSettings {
		if customResource.Spec.BucketSettings[i].BucketType == constants.BucketTypeMemcached {
			continue
		}

		if *customResource.Spec.BucketSettings[i].BucketReplicas < 0 {
			err := errors.ExceedsMinimumInt("spec.buckets.replicas", "body", int64(constants.BucketReplicasZero), false)
			errs = append(errs, err)
		} else if *customResource.Spec.BucketSettings[i].BucketReplicas > 3 {
			err := errors.ExceedsMaximumInt("spec.buckets.replicas", "body", int64(constants.BucketReplicasThree), false)
			errs = append(errs, err)
		}

		if *customResource.Spec.BucketSettings[i].IoPriority != constants.BucketIoPriorityHigh &&
			*customResource.Spec.BucketSettings[i].IoPriority != constants.BucketIoPriorityLow {
			err := errors.EnumFail("spec.buckets.ioPriority", "body", nil,
				[]interface{}{constants.BucketIoPriorityHigh, constants.BucketIoPriorityLow})
			errs = append(errs, err)
		}

		if *customResource.Spec.BucketSettings[i].ConflictResolution != constants.BucketConflictResolutionSeqno &&
			*customResource.Spec.BucketSettings[i].ConflictResolution != constants.BucketConflictResolutionTimestamp {
			err := errors.EnumFail("spec.buckets.conflictResolution", "body", nil,
				[]interface{}{constants.BucketConflictResolutionSeqno, constants.BucketConflictResolutionTimestamp})
			errs = append(errs, err)
		}

		if customResource.Spec.BucketSettings[i].BucketType == constants.BucketTypeEphemeral {
			if *customResource.Spec.BucketSettings[i].EvictionPolicy != constants.BucketEvictionPolicyNoEviction &&
				*customResource.Spec.BucketSettings[i].EvictionPolicy != constants.BucketEvictionPolicyNRUEviction {
				err := errors.EnumFail("spec.buckets.evictionPolicy", "body", nil,
					[]interface{}{constants.BucketEvictionPolicyNoEviction, constants.BucketEvictionPolicyNRUEviction})
				errs = append(errs, err)
			}
			continue
		}

		if *customResource.Spec.BucketSettings[i].EvictionPolicy != constants.BucketEvictionPolicyValueOnly &&
			*customResource.Spec.BucketSettings[i].EvictionPolicy != constants.BucketEvictionPolicyFullEviction {
			err := errors.EnumFail("spec.buckets.evictionPolicy", "body", nil,
				[]interface{}{constants.BucketEvictionPolicyValueOnly, constants.BucketEvictionPolicyFullEviction})
			errs = append(errs, err)
		}
	}

	// Check that the total memory quota is valid
	totalBucketMemory := 0
	for _, bucket := range customResource.Spec.BucketSettings {
		totalBucketMemory += bucket.BucketMemoryQuota
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
