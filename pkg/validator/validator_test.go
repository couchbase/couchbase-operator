package validator

import (
	"fmt"
	"io/ioutil"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/decoder"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/go-openapi/errors"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
)

type testDef struct {
	name        string
	path        string
	updatePath  string
	description string
	typeFail    bool
	expectedErr *errors.CompositeError
}

var testDefs = []testDef{
	{
		name:        "TestExampleConfig",
		path:        "tests/0001.yaml",
		description: "Tests a basic 3 node cluster config",
		expectedErr: nil,
	},
	{
		name:        "TestNoSpecSection",
		path:        "tests/0002.yaml",
		description: "Tests that a config with no spec section fails",
		expectedErr: errors.CompositeValidationError(
			errors.Required("spec.version", "body"),
			errors.Required("spec.servers", "body"),
			errors.TooShort("spec.authSecret", "body", 1),
			errors.EnumFail("spec.cluster.indexStorageSetting", "body", nil, []interface{}{"plasma", "memory_optimized"}),
		),
	},
	{
		name:        "TestBaseImageCustomValue",
		path:        "tests/0003.yaml",
		description: "Tests a config with a non-default value for the baseImage field",
		expectedErr: nil,
	},
	{
		name:        "TestMissingVersion",
		path:        "tests/0004.yaml",
		description: "Tests a config with no version fails",
		expectedErr: errors.CompositeValidationError(
			errors.Required("spec.version", "body"),
		),
	},
	{
		name:        "TestPausedTrue",
		path:        "tests/0005.yaml",
		description: "Tests a config with paused set to true",
		expectedErr: nil,
	},
	{
		name:        "TestPausedNonBoolean",
		path:        "tests/0006.yaml",
		description: "Tests a config with paused set to a non-boolean value",
		typeFail:    true,
		expectedErr: nil,
	},
	{
		name:        "TestAntiAffinityTrue",
		path:        "tests/0007.yaml",
		description: "Tests a config with antiAffinity set to true",
		expectedErr: nil,
	},
	{
		name:        "TestAntiAffinityNonBoolean",
		path:        "tests/0008.yaml",
		description: "Tests a config with antiAffinity set to a non-boolean value",
		typeFail:    true,
		expectedErr: nil,
	},
	{
		name:        "TestMissingAuthSecret",
		path:        "tests/0009.yaml",
		description: "Tests a config with no authSecret fails",
		expectedErr: errors.CompositeValidationError(
			errors.TooShort("spec.authSecret", "body", 1),
		),
	},
	{
		name:        "TestExposeAdminConsoleTrue",
		path:        "tests/0010.yaml",
		description: "Tests a config with exposeAdminConsole set to true",
		expectedErr: nil,
	},
	{
		name:        "TestSetAdminConsoleServicesToDataService",
		path:        "tests/0011.yaml",
		description: "Tests a config with adminConsoleService set to data",
		expectedErr: nil,
	},
	{
		name:        "TestSetAdminConsoleServicesAll",
		path:        "tests/0012.yaml",
		description: "Tests a config with all adminConsoleService services set",
		expectedErr: nil,
	},
	{
		name:        "TestSetAdminConsoleServicesTooMany",
		path:        "tests/0013.yaml",
		description: "Tests a config with too many adminConsoleService services",
		expectedErr: errors.CompositeValidationError(
			errors.DuplicateItems("spec.adminConsoleServices", "body"),
		),
	},
	{
		name:        "TestSetAdminConsoleServicesInvalid",
		path:        "tests/0014.yaml",
		description: "Tests a config with an invalid adminConsoleService services",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("spec.adminConsoleServices", "body", "invalid",
				[]interface{}{"data", "index", "query", "search", "eventing", "analytics"}),
		),
	},
	{
		name:        "TestSetAdminConsoleServicesNotArray",
		path:        "tests/0015.yaml",
		description: "Tests a config where adminConsoleService is not an array",
		typeFail:    true,
		expectedErr: nil,
	},
	{
		name:        "TestDataServiceMemoryQuotaTooSmall",
		path:        "tests/0016.yaml",
		description: "Tests a config where dataServiceMemoryQuota is too small",
		expectedErr: errors.CompositeValidationError(
			errors.ExceedsMinimumInt("spec.cluster.dataServiceMemoryQuota", "body", 256, false),
		),
	},
	{
		name:        "TestIndexServiceMemoryQuotaTooSmall",
		path:        "tests/0017.yaml",
		description: "Tests a config where indexServiceMemoryQuota is too small",
		expectedErr: errors.CompositeValidationError(
			errors.ExceedsMinimumInt("spec.cluster.indexServiceMemoryQuota", "body", 256, false),
		),
	},
	{
		name:        "TestSearchServiceMemoryQuotaTooSmall",
		path:        "tests/0018.yaml",
		description: "Tests a config where searchServiceMemoryQuota is too small",
		expectedErr: errors.CompositeValidationError(
			errors.ExceedsMinimumInt("spec.cluster.searchServiceMemoryQuota", "body", 256, false),
		),
	},
	{
		name:        "TestIndexStorageMode",
		path:        "tests/0038.yaml",
		description: "Tests a config with a valid index storage mode",
		expectedErr: nil,
	},
	{
		name:        "TestIndexStorageModeInvalid",
		path:        "tests/0039.yaml",
		description: "Tests a config with an invalid index storage mode",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("spec.cluster.indexStorageSetting", "body", nil, []interface{}{"plasma", "memory_optimized"}),
		),
	},
	{
		name:        "TestCouchbaseBucketCreation",
		path:        "tests/0019.yaml",
		description: "Tests a valid couchbase bucket configuration",
		expectedErr: nil,
	},
	{
		name:        "TestEphemeralBucketCreation",
		path:        "tests/0020.yaml",
		description: "Tests a valid ephemeral bucket configuration",
		expectedErr: nil,
	},
	{
		name:        "TestMemcachedBucketCreation",
		path:        "tests/0021.yaml",
		description: "Tests a valid memcached bucket configuration",
		expectedErr: nil,
	},
	{
		name:        "TestEphemeralBucketCreationInvalidParameters",
		path:        "tests/0022.yaml",
		description: "Tests a valid ephemeral bucket configuration with invalid parameters",
		expectedErr: errors.CompositeValidationError(
			errors.InvalidType("enableReplicaIndex", "spec.buckets[0]", "nil", "Bucket type is ephemeral"),
		),
	},
	{
		name:        "TestMemcachedBucketCreationInvalidParameters",
		path:        "tests/0023.yaml",
		description: "Tests a valid memcached bucket configuration with invalid parameters",
		expectedErr: errors.CompositeValidationError(
			errors.InvalidType("enableReplicaIndex", "spec.buckets[0]", "nil", "Bucket type is memcached"),
			errors.InvalidType("bucketReplicas", "spec.buckets[0]", "nil", "Bucket type is memcached"),
			errors.InvalidType("conflictResolution", "spec.buckets[0]", "nil", "Bucket type is memcached"),
			errors.InvalidType("evictionPolicy", "spec.buckets[0]", "nil", "Bucket type is memcached"),
			errors.InvalidType("ioPriority", "spec.buckets[0]", "nil", "Bucket type is memcached"),
		),
	},
	{
		name:        "TestBucketWithInvalidName",
		path:        "tests/0024.yaml",
		description: "Tests a config with a bucket with an invalid name",
		expectedErr: errors.CompositeValidationError(
			errors.FailedPattern("spec.buckets.name", "body", `^[a-zA-Z0-9._\-%]*$`),
		),
	},
	{
		name:        "TestBucketWithInvalidType",
		path:        "tests/0025.yaml",
		description: "Tests a config with a bucket with an invalid type",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("spec.buckets.type", "body", nil, []interface{}{"couchbase", "ephemeral", "memcached"}),
		),
	},
	{
		name:        "TestBucketWithMemoryQuotaTooLow",
		path:        "tests/0026.yaml",
		description: "Tests a config with a bucket with too little memory",
		expectedErr: errors.CompositeValidationError(
			errors.ExceedsMinimumInt("spec.buckets.memoryQuota", "body", 100, false),
		),
	},
	{
		name:        "TestBucketWithReplicasTooLow",
		path:        "tests/0027.yaml",
		description: "Tests a config with a bucket with too few replicas",
		expectedErr: errors.CompositeValidationError(
			errors.ExceedsMinimumInt("spec.buckets.replicas", "body", 0, false),
		),
	},
	{
		name:        "TestBucketWithReplicasTooHigh",
		path:        "tests/0028.yaml",
		description: "Tests a config with a bucket with too many replicas",
		expectedErr: errors.CompositeValidationError(
			errors.ExceedsMaximumInt("spec.buckets.replicas", "body", 3, false),
		),
	},
	{
		name:        "TestBucketWithInvalidIoPriority",
		path:        "tests/0029.yaml",
		description: "Tests a config with a bucket with invalid ioPriority",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("spec.buckets.ioPriority", "body", nil, []interface{}{"high", "low"}),
		),
	},
	{
		name:        "TestBucketWithInvalidEphemeralEvictionPolicy",
		path:        "tests/0030.yaml",
		description: "Tests a config with a ephemeral bucket with invalid eviction policy",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("evictionPolicy", "spec.buckets[0]", nil, []interface{}{"noEviction", "nruEviction"}),
		),
	},
	{
		name:        "TestBucketWithInvalidCouchbaseEvictionPolicy",
		path:        "tests/0031.yaml",
		description: "Tests a config with a couchbase bucket with invalid eviction policy",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("evictionPolicy", "spec.buckets[0]", nil, []interface{}{"valueOnly", "fullEviction"}),
		),
	},
	{
		name:        "TestBucketWithInvalidConflictResolution",
		path:        "tests/0032.yaml",
		description: "Tests a config with a bucket with invalid conflict resolution",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("spec.buckets.conflictResolution", "body", nil, []interface{}{"seqno", "lww"}),
		),
	},
	{
		name:        "TestNoServersSection",
		path:        "tests/0033.yaml",
		description: "Tests a config with no servers section",
		expectedErr: errors.CompositeValidationError(
			errors.Required("spec.servers", "body"),
		),
	},
	{
		name:        "TestServersRequiredParameters",
		path:        "tests/0034.yaml",
		description: "Tests the required parameters in the servers section",
		expectedErr: errors.CompositeValidationError(
			errors.TooShort("spec.servers.name", "body", 1),
			errors.ExceedsMinimumInt("spec.servers.size", "body", 1, false),
			errors.InvalidType("spec.servers.services", "body", "array", "null"),
		),
	},
	{
		name:        "TestServersNoDataService",
		path:        "tests/0035.yaml",
		description: "Tests at least one servers definition has the data service",
		expectedErr: errors.CompositeValidationError(
			errors.Required("at least on \"data\" service", "spec.servers[*].services"),
		),
	},
	{
		name:        "TestMultipleServerSections",
		path:        "tests/0036.yaml",
		description: "Tests multiple servers definitions",
		expectedErr: nil,
	},
	{
		name:        "TestMultipleServerSectionsWithoutUniqueNames",
		path:        "tests/0037.yaml",
		description: "Tests multiple servers definitions where the names are not unique",
		expectedErr: errors.CompositeValidationError(
			errors.DuplicateItems("spec.servers.name", "body"),
		),
	},
	{
		name:        "TestValidIndexStorageSetting",
		path:        "tests/0038.yaml",
		description: "Tests setting indexStorageSetting to plasma",
		expectedErr: nil,
	},
	{
		name:        "TestInvalidIndexStorageSetting",
		path:        "tests/0039.yaml",
		description: "Tests setting indexStorageSetting to an invalid value",
		expectedErr: errors.CompositeValidationError(
			errors.EnumFail("spec.cluster.indexStorageSetting", "body", nil, []interface{}{"plasma", "memory_optimized"}),
		),
	},
	{
		name:        "TestUpdateToAuthSecret",
		path:        "tests/0001.yaml",
		updatePath:  "tests/0040.yaml",
		description: "Tests updating the authSecret fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.authSecret", "body"},
		),
	},
	{
		name:        "TestUpdateToBucketType",
		path:        "tests/0030.yaml",
		updatePath:  "tests/0041.yaml",
		description: "Tests updating the bucket type fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.buckets[0].type", "body"},
		),
	},
	{
		name:        "TestUpdateToBucketConflictResolution",
		path:        "tests/0030.yaml",
		updatePath:  "tests/0042.yaml",
		description: "Tests updating the bucket conflict resolution fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.buckets[0].conflictResolution", "body"},
		),
	},
	{
		name:        "TestInvalidUpdateToServices",
		path:        "tests/0001.yaml",
		updatePath:  "tests/0043.yaml",
		description: "Tests updating the services (with different services) fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[0].services", "body"},
		),
	},
	{
		name:        "TestUpdateToServicesDifferentOrder",
		path:        "tests/0001.yaml",
		updatePath:  "tests/0044.yaml",
		description: "Tests updating the services in a different order passes",
		expectedErr: nil,
	},
	{
		name:        "TestUpdateClusterSettings",
		path:        "tests/0047.yaml",
		updatePath:  "tests/0048.yaml",
		expectedErr: nil,
	},
	{
		name:        "TestUpdateClusterSettingsInvalidOld",
		path:        "tests/0001.yaml",
		updatePath:  "tests/0048.yaml",
		description: "Tests updating cluster settings when the old config has an index service",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[0].services", "body"},
			&UpdateError{"spec.cluster.indexStorageSetting", "body"},
		),
	},
	{
		name:        "TestUpdateClusterSettingsInvalidNew",
		path:        "tests/0048.yaml",
		updatePath:  "tests/0001.yaml",
		description: "Tests updating cluster settings when the new config has an index service",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[0].services", "body"},
			&UpdateError{"spec.cluster.indexStorageSetting", "body"},
		),
	},
	{
		name:        "TestUpdateClusterSettingsInvalidNew",
		path:        "tests/0049.yaml",
		description: "Tests that the sum of all bucket quotas is less than the data service quota",
		expectedErr: errors.CompositeValidationError(
			errors.ExceedsMaximumInt("spec.buckets[*].memoryQuota", "body", int64(256), false),
		),
	},
	{
		name:        "TestSpecWithVolumes",
		path:        "tests/0050.yaml",
		description: "Tests that spec with volume mounts is valid",
		expectedErr: nil,
	},
	{
		name:        "TestMissingDefaultMount",
		path:        "tests/0051.yaml",
		description: "Tests that default volume mount is specified",
		expectedErr: errors.CompositeValidationError(
			errors.Required(`"default"`, "spec.servers[*].Pod.VolumeMounts"),
		),
	},
	{
		name:        "TestMissingClaimTemplate",
		path:        "tests/0052.yaml",
		description: "Tests that template for volume mount is specified",
		expectedErr: errors.CompositeValidationError(
			errors.Required(`"couchbase"`, "spec.volumeClaimTemplates[*].metadata.name"),
		),
	},
	{
		name:        "TestUpdateAddedVolumeMountSpec",
		path:        "tests/0001.yaml",
		updatePath:  "tests/0050.yaml",
		description: "Tests that VolumeMounts cannot be added after pod creation",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[*].Pod.VolumeMounts", "body"},
		),
	},
	{
		name:        "TestUpdateAddIndexVolumeMount",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0053.yaml",
		description: "Tests that index volume mount cannot be added to list of VolumeMounts",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"index", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
	{
		name:        "TestUpdateRemoveVolumeMountSpec",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0001.yaml",
		description: "Tests that VolumeMounts cannot be removed after pod creation",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[*].Pod.VolumeMounts", "body"},
		),
	},
	{
		name:        "TestUpdateRemoveDataVolumeMount",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0054.yaml",
		description: "Tests that data volume mount cannot be removed from list of VolumeMounts",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"data", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
	{
		name:        "TestChangeVolumeMountClaimTemplate",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0055.yaml",
		description: "Tests that name of claim template of a volume cannot be changed",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"data", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
	{
		name:        "TestChangeClaimTemplateName",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0056.yaml",
		description: "Tests that name of claim template cannot be changed",
		expectedErr: errors.CompositeValidationError(
			errors.Required(`"couchbase"`, "spec.volumeClaimTemplates[*].metadata.name"),
			errors.Required(`"couchbase"`, "spec.volumeClaimTemplates[*].metadata.name"),
		),
	},
	{
		name:        "TestChangeStorageClass",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0057.yaml",
		description: "Tests that storage class cannot be changed",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{`"storageClassName"`, "spec.volumeClaimTemplates[*]"},
		),
	},
	{
		name:        "TestChangeStorageSize",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0058.yaml",
		description: "Tests that storage size cannot be changed",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{`"storage"`, "spec.volumeClaimTemplates[*].resources.requests"},
		),
	},
	{
		name:        "TestClaimTemplateRequiresStorageClass",
		path:        "tests/0059.yaml",
		description: "Tests that storage class is specified",
		expectedErr: errors.CompositeValidationError(
			errors.Required(`"storageClassName"`, "spec.volumeClaimTemplates[*]"),
		),
	},
	{
		name:        "TestClaimTemplateRequiresStorageRequests",
		path:        "tests/0050.yaml",
		updatePath:  "tests/0060.yaml",
		description: "Tests that storage request size is specified",
		expectedErr: errors.CompositeValidationError(
			errors.Required(`"storage"`, "spec.volumeClaimTemplates[*].resources.requests|limits"),
		),
	},
	{
		name:        "TestVersionMin",
		path:        "tests/0061.yaml",
		description: "Tests a config with version < min fails",
		expectedErr: errors.CompositeValidationError(
			errors.FailedPattern("spec.version", "body", k8sutil.VersionPattern),
		),
	},
	{
		name:        "TestVersionInvalid",
		path:        "tests/0062.yaml",
		description: "Tests a config with invalid",
		expectedErr: errors.CompositeValidationError(
			errors.FailedPattern("spec.version", "body", k8sutil.VersionPattern),
		),
	},
	{
		name:        "TestVersionUpgrade",
		path:        "tests/0001.yaml",
		updatePath:  "tests/0063.yaml",
		description: "Tests version upgrades are rejected",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.version", "body"},
		),
	},
	{
		name:        "TestAnalyticsMounts",
		path:        "tests/0064.yaml",
		description: "Tests mounts with analytics claims",
		expectedErr: nil,
	},
	{
		name:        "TestAnalyticsMissingClaim",
		path:        "tests/0065.yaml",
		description: "Tests analytics specify missing claim",
		expectedErr: errors.CompositeValidationError(
			errors.Required(`"couchbase2"`, "spec.volumeClaimTemplates[*].metadata.name"),
			errors.Required(`"couchbase2"`, "spec.volumeClaimTemplates[*].metadata.name"),
		),
	},
	{
		name:        "TestUpdateAnalyticsVolumes",
		path:        "tests/0065.yaml",
		updatePath:  "tests/0066.yaml",
		description: "Tests updating analytics volumes",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"analytics", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
}

func TestValiation(t *testing.T) {
	err := v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Failed to register CRD scheme due to %v", err)
	}

	err = api.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Failed to register CouchbaseCluster scheme due to %v", err)
	}

	for _, tc := range testDefs {
		raw, err := ioutil.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("%s failed due to %v", tc.name, err)
		}

		resource, err := decoder.DecodeCouchbaseCluster(raw)
		if err != nil {
			if !tc.typeFail {
				fmt.Printf("Failed: %s %T\n", tc.name, err)
			}
			continue
		}

		if tc.typeFail {
			fmt.Printf("Name: %s\nFile: %s\nDesc: %s\n", tc.name, tc.path, tc.description)
			fmt.Printf("Expected decoding to fail due to invalid type\n")
			t.Fail()
			continue
		}

		if tc.updatePath == "" {
			err = Create(resource)
			if !checkErrors(tc, err) {
				t.Fail()
			}
		} else {
			raw, err = ioutil.ReadFile(tc.updatePath)
			if err != nil {
				t.Fatalf("%s failed due to %v", tc.name, err)
			}

			updated, err := decoder.DecodeCouchbaseCluster(raw)
			if err != nil {
				if !tc.typeFail {
					fmt.Printf("Failed: %s %T\n", tc.name, err)
				}
				continue
			}

			err, _ = Update(resource, updated)
			if !checkErrors(tc, err) {
				t.Fail()
			}
		}
	}
}

func printError(name, file, description string, expected, actual *errors.CompositeError) {
	fmt.Printf("----\n")
	fmt.Printf("Name: %s\nFile: %s\nDesc: %s\n", name, file, description)
	fmt.Printf("Expected:\n%v\n", expected)
	fmt.Printf("Actual:\n%v\n", actual)
	fmt.Printf("----\n")
}

func checkErrors(tc testDef, err error) bool {
	if actual, ok := err.(*errors.CompositeError); ok {
		if !compositeErrorEqual(tc.expectedErr, actual) {
			printError(tc.name, tc.path, tc.description, tc.expectedErr, actual)
			return false
		}
	} else if err != nil {
		fmt.Printf("%s failed due to %v\n", tc.name, err)
		return false
	} else {
		if tc.expectedErr != nil {
			printError(tc.name, tc.path, tc.description, tc.expectedErr, actual)
			return false
		}
	}

	return true
}

func compositeErrorEqual(e1, e2 *errors.CompositeError) bool {
	if e1 == nil && e2 == nil {
		return true
	} else if e2 == nil {
		return false
	} else if e1 == nil {
		return false
	}

	if len(e1.Errors) != len(e2.Errors) {
		return false
	}

	for _, err1 := range e1.Errors {
		found := false
		for _, err2 := range e2.Errors {
			if err1.Error() == err2.Error() {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}
