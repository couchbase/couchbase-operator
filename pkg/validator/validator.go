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

type CRDValidator struct {
}

func (c *CRDValidator) Create(resource *api.CouchbaseCluster) error {
	applyDefaults(resource)

	if err := k8sutil.ValidateCRD(resource); err != nil {
		return err
	}

	if err := checkConstraints(resource); err != nil {
		return err
	}

	return nil
}

func applyDefaults(customResource *api.CouchbaseCluster) {
	if customResource.Spec.BaseImage == "" {
		customResource.Spec.BaseImage = DefaultBaseImage
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

	for i, _ := range customResource.Spec.ServerSettings {
		if customResource.Spec.ServerSettings[i].DataPath == "" {
			customResource.Spec.ServerSettings[i].DataPath = constants.DefaultDataPath
		}

		if customResource.Spec.ServerSettings[i].IndexPath == "" {
			customResource.Spec.ServerSettings[i].IndexPath = constants.DefaultIndexPath
		}
	}
}

func checkConstraints(customResource *api.CouchbaseCluster) error {
	errs := []error{}

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

	// Ensure unneccessary settings in memcached and ephemeral buckets are nil
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
