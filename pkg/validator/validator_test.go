package validator

import (
	"fmt"
	"io/ioutil"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/decoder"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"github.com/go-openapi/errors"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
)

type testDef struct {
	// name is the test name.
	name string
	// path is the path to the YAML to be applied.
	path string
	// existingPath is the path to the existing YAML CR in the system.
	existingPath string
	// description describes what the test is testing for.
	description string
	// expectedErr is the expected set of errors detected by CRD validation,
	// custom validation and immutability tests.
	expectedErr *errors.CompositeError
	// secretMissing indicates that the operator admin secret isn't present.
	secretMissing bool
	// storageClassMissing indicates that a requested storage class isn't present.
	storageClassMissing bool
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
			errors.Required("spec.baseImage", "body"),
			errors.Required("spec.version", "body"),
			errors.Required("spec.authSecret", "body"),
			errors.Required("spec.cluster", "body"),
			errors.Required("spec.servers", "body"),
		),
	},
	{
		name:        "TestBaseImageCustomValue",
		path:        "tests/0003.yaml",
		description: "Tests a config with a non-default value for the baseImage field",
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
	},
	{
		name:        "TestPausedNonBoolean",
		path:        "tests/0006.yaml",
		description: "Tests a config with paused set to a non-boolean value",
		expectedErr: errors.CompositeValidationError(
			errors.InvalidType("spec.paused", "body", "boolean", "string"),
		),
	},
	{
		name:        "TestAntiAffinityTrue",
		path:        "tests/0007.yaml",
		description: "Tests a config with antiAffinity set to true",
	},
	{
		name:        "TestAntiAffinityNonBoolean",
		path:        "tests/0008.yaml",
		description: "Tests a config with antiAffinity set to a non-boolean value",
		expectedErr: errors.CompositeValidationError(
			errors.InvalidType("spec.antiAffinity", "body", "boolean", "string"),
		),
	},
	{
		name:        "TestMissingAuthSecret",
		path:        "tests/0009.yaml",
		description: "Tests a config with no authSecret fails",
		expectedErr: errors.CompositeValidationError(
			errors.Required("spec.authSecret", "body"),
		),
	},
	{
		name:        "TestExposeAdminConsoleTrue",
		path:        "tests/0010.yaml",
		description: "Tests a config with exposeAdminConsole set to true",
	},
	{
		name:        "TestSetAdminConsoleServicesToDataService",
		path:        "tests/0011.yaml",
		description: "Tests a config with adminConsoleService set to data",
	},
	{
		name:        "TestSetAdminConsoleServicesAll",
		path:        "tests/0012.yaml",
		description: "Tests a config with all adminConsoleService services set",
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
		expectedErr: errors.CompositeValidationError(
			errors.InvalidType("spec.adminConsoleServices", "body", "array", "string"),
		),
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
	},
	{
		name:        "TestEphemeralBucketCreation",
		path:        "tests/0020.yaml",
		description: "Tests a valid ephemeral bucket configuration",
	},
	{
		name:        "TestMemcachedBucketCreation",
		path:        "tests/0021.yaml",
		description: "Tests a valid memcached bucket configuration",
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
			errors.InvalidType("replicas", "spec.buckets[0]", "nil", "Bucket type is memcached"),
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
			errors.Required("spec.servers.size", "body"),
			errors.Required("spec.servers.name", "body"),
			errors.Required("spec.servers.services", "body"),
		),
	},
	{
		name:        "TestServersNoDataService",
		path:        "tests/0035.yaml",
		description: "Tests at least one servers definition has the data service",
		expectedErr: errors.CompositeValidationError(
			errors.Required("at least one \"data\" service", "spec.servers[*].services"),
		),
	},
	{
		name:        "TestMultipleServerSections",
		path:        "tests/0036.yaml",
		description: "Tests multiple servers definitions",
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
		name:         "TestUpdateToAuthSecret",
		existingPath: "tests/0001.yaml",
		path:         "tests/0040.yaml",
		description:  "Tests updating the authSecret fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.authSecret", "body"},
		),
	},
	{
		name:         "TestUpdateToBucketType",
		existingPath: "tests/0030.yaml",
		path:         "tests/0041.yaml",
		description:  "Tests updating the bucket type fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.buckets[0].type", "body"},
		),
	},
	{
		name:         "TestUpdateToBucketConflictResolution",
		existingPath: "tests/0030.yaml",
		path:         "tests/0042.yaml",
		description:  "Tests updating the bucket conflict resolution fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.buckets[0].conflictResolution", "body"},
		),
	},
	{
		name:         "TestInvalidUpdateToServices",
		existingPath: "tests/0001.yaml",
		path:         "tests/0043.yaml",
		description:  "Tests updating the services (with different services) fails",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[0].services", "body"},
		),
	},
	{
		name:         "TestUpdateToServicesDifferentOrder",
		existingPath: "tests/0001.yaml",
		path:         "tests/0044.yaml",
		description:  "Tests updating the services in a different order passes",
	},
	{
		name:         "TestUpdateClusterSettings",
		existingPath: "tests/0047.yaml",
		path:         "tests/0048.yaml",
	},
	{
		name:         "TestUpdateClusterSettingsInvalidOld",
		existingPath: "tests/0001.yaml",
		path:         "tests/0048.yaml",
		description:  "Tests updating cluster settings when the old config has an index service",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[0].services", "body"},
			&UpdateError{"spec.cluster.indexStorageSetting", "body"},
		),
	},
	{
		name:         "TestUpdateClusterSettingsInvalidNew",
		existingPath: "tests/0048.yaml",
		path:         "tests/0001.yaml",
		description:  "Tests updating cluster settings when the new config has an index service",
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
		name:         "TestUpdateAddedVolumeMountSpec",
		existingPath: "tests/0001.yaml",
		path:         "tests/0050.yaml",
		description:  "Tests that VolumeMounts cannot be added after pod creation",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[*].Pod.VolumeMounts", "body"},
		),
	},
	{
		name:         "TestUpdateAddIndexVolumeMount",
		existingPath: "tests/0050.yaml",
		path:         "tests/0053.yaml",
		description:  "Tests that index volume mount cannot be added to list of VolumeMounts",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"index", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
	{
		name:         "TestUpdateRemoveVolumeMountSpec",
		existingPath: "tests/0050.yaml",
		path:         "tests/0001.yaml",
		description:  "Tests that VolumeMounts cannot be removed after pod creation",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.servers[*].Pod.VolumeMounts", "body"},
		),
	},
	{
		name:         "TestUpdateRemoveDataVolumeMount",
		existingPath: "tests/0050.yaml",
		path:         "tests/0054.yaml",
		description:  "Tests that data volume mount cannot be removed from list of VolumeMounts",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"data", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
	{
		name:         "TestChangeVolumeMountClaimTemplate",
		existingPath: "tests/0050.yaml",
		path:         "tests/0055.yaml",
		description:  "Tests that name of claim template of a volume cannot be changed",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"data", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
	{
		name:         "TestChangeClaimTemplateName",
		existingPath: "tests/0050.yaml",
		path:         "tests/0056.yaml",
		description:  "Tests that name of claim template cannot be changed",
		expectedErr: errors.CompositeValidationError(
			errors.Required(`"couchbase"`, "spec.volumeClaimTemplates[*].metadata.name"),
			errors.Required(`"couchbase"`, "spec.volumeClaimTemplates[*].metadata.name"),
		),
	},
	{
		name:         "TestChangeStorageClass",
		existingPath: "tests/0050.yaml",
		path:         "tests/0057.yaml",
		description:  "Tests that storage class cannot be changed",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{`"storageClassName"`, "spec.volumeClaimTemplates[*]"},
		),
	},
	{
		name:         "TestChangeStorageSize",
		existingPath: "tests/0050.yaml",
		path:         "tests/0058.yaml",
		description:  "Tests that storage size cannot be changed",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{`"storage"`, "spec.volumeClaimTemplates[*].resources.requests"},
		),
	},
	{
		name:        "TestClaimTemplateRequiresStorageClass",
		path:        "tests/0059.yaml",
		description: "Tests that storage class is specified",
		expectedErr: errors.CompositeValidationError(
			errors.Required("spec.volumeClaimTemplates.spec.storageClassName", "body"),
		),
	},
	{
		name:         "TestClaimTemplateRequiresStorageRequests",
		existingPath: "tests/0050.yaml",
		path:         "tests/0060.yaml",
		description:  "Tests that storage request size is specified",
		expectedErr: errors.CompositeValidationError(
			errors.Required("spec.volumeClaimTemplates.spec.resources", "body"),
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
		name:         "TestUpdateAnalyticsVolumes",
		existingPath: "tests/0065.yaml",
		path:         "tests/0066.yaml",
		description:  "Tests updating analytics volumes",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"analytics", "spec.servers[*].Pod.VolumeMounts"},
		),
	},
	{
		name:         "TestUpdateAntiAffinity",
		existingPath: "tests/0007.yaml",
		path:         "tests/0067.yaml",
		description:  "Tests anti affinity is immutable",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.antiAffinity", "body"},
		),
	},
	{
		name:        "TestBucketNamesUnique",
		path:        "tests/0068.yaml",
		description: "Tests bucket names are unique",
		expectedErr: errors.CompositeValidationError(
			errors.DuplicateItems("spec.buckets.name", "body"),
		),
	},
	{
		name:        "TestPodEnvironmentIsArray",
		path:        "tests/0069.yaml",
		description: "Tests pod environment is specified as an array",
		expectedErr: errors.CompositeValidationError(
			errors.InvalidType("spec.servers.pod.couchbaseEnv", "body", "array", "object"),
		),
	},
	{
		name:        "TestPodEnvironmentValueIsString",
		path:        "tests/0070.yaml",
		description: "Tests pod environment values are strings",
		expectedErr: errors.CompositeValidationError(
			errors.InvalidType("spec.servers.pod.couchbaseEnv.value", "body", "string", "number"),
		),
	},
	{
		name:        "TestPodEnvironmentValid",
		path:        "tests/0071.yaml",
		description: "Tests pod environment values are strings",
	},
	{
		name:        "TestMissingAdminConsoleServices",
		path:        "tests/0072.yaml",
		description: "Tests exposing the admin console without services is illegal",
		expectedErr: errors.CompositeValidationError(
			errors.Required("spec.adminConsoleServices", "body"),
		),
	},
	{
		name:        "TestLogsDefaultExclusive",
		path:        "tests/0073.yaml",
		description: "Tests that default and logs claim are mutually exclusive",
		expectedErr: errors.CompositeValidationError(
			errors.PropertyNotAllowed("spec.servers[0].pod.volumeMounts", "", "default"),
			errors.PropertyNotAllowed("spec.servers[0].pod.volumeMounts", "", "data"),
			errors.PropertyNotAllowed("spec.servers[0].pod.volumeMounts", "", "index"),
			errors.PropertyNotAllowed("spec.servers[0].pod.volumeMounts", "", "analytics"),
		),
	},
	{
		name:        "TestOnlyDataMountNotAllowed",
		path:        "tests/0075.yaml",
		description: "Tests specifying only data mount is not allowed",
		expectedErr: errors.CompositeValidationError(
			errors.Required("default", "spec.servers[0].pod.volumeMounts"),
		),
	},
	{
		name:        "TestSupportableCluster",
		path:        "tests/0076.yaml",
		description: "Tests a supportable configuration with stateful and stateless classes",
	},
	{
		name:        "TestUnsupportableCluster",
		path:        "tests/0077.yaml",
		description: "Tests an unsupportable configuration with stateful and stateless classes",
		expectedErr: errors.CompositeValidationError(
			errors.Required("volumeMounts", "spec.servers[1].pod"),
		),
	},
	{
		name:        "TestUnsupportableClusterStatefulServices",
		path:        "tests/0078.yaml",
		description: "Tests an unsupportable configuration whose stateful services don't have a default mount",
		expectedErr: errors.CompositeValidationError(
			errors.Required("default", "spec.servers[0].pod.volumeMounts"),
		),
	},
	{
		name:        "TestDataMountWithoutDataService",
		path:        "tests/0079.yaml",
		description: "Tests a server class with the data mount set but no data service enabled",
		expectedErr: errors.CompositeValidationError(
			errors.Required("data", "spec.servers[1].services"),
		),
	},
	{
		name:        "TestIndexMountWithoutIndexService",
		path:        "tests/0080.yaml",
		description: "Tests a server class with the index mount set but no index service enabled",
		expectedErr: errors.CompositeValidationError(
			errors.Required("index", "spec.servers[1].services"),
		),
	},
	{
		name:        "TestAnalyticsMountWithoutAnalyticsService",
		path:        "tests/0081.yaml",
		description: "Tests a server class with an analyitics mount set but no analytics service enabled",
		expectedErr: errors.CompositeValidationError(
			errors.Required("analytics", "spec.servers[1].services"),
		),
	},
	{
		name:        "TestSecretDoesNotExistInK8s",
		path:        "tests/0001.yaml",
		description: "Tests a config where the specified secret does not exist in k8s",
		expectedErr: errors.CompositeValidationError(
			fmt.Errorf("secret cb-example-auth must exist"),
		),
		secretMissing: true,
	},
	{
		name:        "TestStorageClassMissingInK8s",
		path:        "tests/0054.yaml",
		description: "Tests a config where one or more storage classes does not exist in k8s",
		expectedErr: errors.CompositeValidationError(
			fmt.Errorf("storage class standard must exist"),
		),
		storageClassMissing: true,
	},
	{
		name:        "TestVolumeClaimsUnique",
		path:        "tests/0086.yaml",
		description: "Tests a config where two volume claims have the same metadata.name",
		expectedErr: errors.CompositeValidationError(
			errors.DuplicateItems("spec.volumeClaimTemplates[1].metadata.name", "body"),
		),
	},
	{
		name:         "TestIllegalDowngrade",
		existingPath: "tests/0001.yaml",
		path:         "tests/0082.yaml",
		description:  "Tests downgrades are rejected",
		expectedErr: errors.CompositeValidationError(
			fmt.Errorf("spec.Version in body should be greater than 5.5.0"),
		),
	},
	{
		name:         "TestillegalUpgrade",
		existingPath: "tests/0001.yaml",
		path:         "tests/0083.yaml",
		description:  "Tests illegal upgrades are rejected",
		expectedErr: errors.CompositeValidationError(
			fmt.Errorf("spec.Version in body should be less than 7.0.0"),
		),
	},
	{
		name:         "TestRollback",
		existingPath: "tests/0084.yaml",
		path:         "tests/0001.yaml",
		description:  "Tests rollback",
	},
	{
		name:         "TestIllegalRollback",
		existingPath: "tests/0084.yaml",
		path:         "tests/0085.yaml",
		description:  "Tests rollback to an illegal version is rejected",
		expectedErr: errors.CompositeValidationError(
			&UpdateError{"spec.version", "body"},
		),
	},
}

// loadCustomResource loads a custom resource.  If the load failed we check against
// expected errors that may be triggered by CRD validation.  The return bool indicates
// whether we need to continue (e.g. a CRD validation failure that was expected, we can
// just stop the test now).
func loadCustomResource(t *testing.T, tc testDef, path string) (*api.CouchbaseCluster, bool) {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The decode does an unstructured CRD validation before decoding, as such if we
	// encounter an error this may be expected and we should check this.
	resource, err := decoder.DecodeCouchbaseCluster(raw)
	if err != nil {
		checkErrors(t, tc, err)
		return nil, false
	}
	return resource, true

}

type kubeAbstractionTestImpl struct {
	testCase *testDef
}

func (ab *kubeAbstractionTestImpl) secretExists(string, string) (bool, error) {
	return !ab.testCase.secretMissing, nil
}

func (ab *kubeAbstractionTestImpl) storageClassExists(string) (bool, error) {
	return !ab.testCase.storageClassMissing, nil
}

func newTestValidator(testCase *testDef) Validator {
	return Validator{&kubeAbstractionTestImpl{testCase}}
}

func TestValidation(t *testing.T) {
	err := v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Failed to register CRD scheme due to %v", err)
	}

	err = api.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Failed to register CouchbaseCluster scheme due to %v", err)
	}

	for _, tc := range testDefs {
		t.Run(tc.name, func(t *testing.T) {
			// Load the inbound YAML file.
			current, cont := loadCustomResource(t, tc, tc.path)
			if !cont {
				return
			}

			v := newTestValidator(&tc)

			// If this is a pure input test run a Create command.
			if tc.existingPath == "" {
				checkErrors(t, tc, v.Create(current))
				return
			}

			// Otherwise it's an update test, load the existing resource and run an Update command.
			previous, cont := loadCustomResource(t, tc, tc.existingPath)
			if !cont {
				t.Fatal("unexpected load failure of existing resource")
			}
			err, _ = v.Update(previous, current)
			checkErrors(t, tc, err)
		})
	}
}

func printError(tc testDef, actual *errors.CompositeError) {
	fmt.Println("Description:", tc.description)
	if tc.existingPath != "" {
		fmt.Println("Existing CR:", tc.existingPath)
	}
	fmt.Println("Current CR:", tc.path)
	fmt.Println("Expected Errors:")
	fmt.Println(tc.expectedErr)
	fmt.Println("Actual Errors:")
	fmt.Println(actual)
}

func checkErrors(t *testing.T, tc testDef, err error) {
	// If there are no errors and some are expected, then raise an error.
	if err == nil {
		if tc.expectedErr != nil {
			printError(tc, nil)
			t.FailNow()
		}
		return
	}

	// All of our expected error types are composite errors, either from
	// CRD validation, Create or Delete operations.  Anything else is
	// unexpected.
	switch typed := err.(type) {
	case *errors.CompositeError:
		if !compositeErrorEqual(tc.expectedErr, typed) {
			printError(tc, typed)
			t.FailNow()
		}
	default:
		printError(tc, errors.CompositeValidationError(err))
		t.FailNow()
	}
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
