package e2e

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/decoder"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
)

var (
	unavailableStorageClass = "unavailableStorageClass"
)

type testDef struct {
	name           string
	mutations      jsonpatch.PatchSet
	validations    jsonpatch.PatchSet
	shouldFail     bool
	expectedErrors []string
}

type failureList []failure
type failure struct {
	testName  string
	testError error
}

func (failures *failureList) AppendFailure(name string, err error) {
	newFailure := failure{
		testName:  name,
		testError: err,
	}
	*failures = append(*failures, newFailure)
}

func (failures *failureList) PrintFailures(t *testing.T) bool {
	failureExists := false
	for i, failure := range *failures {
		t.Logf("Failure %d: %s \n Error: %v \n", i+1, failure.testName, failure.testError)
		failureExists = true
	}
	return failureExists
}

func (failures *failureList) CheckFailures(t *testing.T) {
	failureExists := failures.PrintFailures(t)
	if failureExists {
		t.Fatal("failures in test")
	}
}

func YAMLToCluster(yamlPath string) (*api.CouchbaseCluster, error) {
	raw, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	cluster, err := decoder.DecodeCouchbaseCluster(raw)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

func runValidationTest(t *testing.T, testDefs []testDef, kubeName, command string) {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]

	// Stop the operator, we don't actually need it to validate the API and the tests will take forever.
	framework.DeleteOperatorCompletely(targetKube.KubeClient, f.Deployment.Name, f.Namespace)
	defer f.SetupCouchbaseOperator(targetKube)

	for _, test := range testDefs {
		// Run each test case defined as a separate test so we have a way
		// of running them individually.
		t.Run(test.name, func(t *testing.T) {
			testCouchbase, err := YAMLToCluster("./resources/validation/validation.yaml")
			if err != nil {
				t.Fatal(err)
			}

			ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, &e2eutil.TlsOpts{})
			defer teardown()

			testCouchbase.Spec.AuthSecret = targetKube.DefaultSecret.Name
			testCouchbase.Spec.TLS = &api.TLSPolicy{
				Static: &api.StaticTLS{
					Member: &api.MemberSecret{
						ServerSecret: ctx.ClusterSecretName,
					},
					OperatorSecret: ctx.OperatorSecretName,
				},
			}
			testCouchbase.ObjectMeta.Name = ctx.ClusterName
			testCouchbase.ObjectMeta.Namespace = f.Namespace

			for i, _ := range testCouchbase.Spec.VolumeClaimTemplates {
				testCouchbase.Spec.VolumeClaimTemplates[i].Spec.StorageClassName = &f.StorageClassName
			}

			// Removing previous deployment if any
			e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, kubeName, t.Name())

			// If we are applying a change or deleting a cluster we first need to create it...
			if command == "apply" || command == "delete" {
				testCouchbase, err = k8sutil.CreateCouchbaseCluster(targetKube.CRClient, testCouchbase)
				if err != nil {
					t.Fatal(err)
				}
			}

			// Patch the cluster specification
			if test.mutations != nil {
				if err := jsonpatch.Apply(testCouchbase, test.mutations.Patches()); err != nil {
					t.Fatal(err)
				}
			}

			// Execute the main test, update the new resource for verification.
			switch command {
			case "create":
				testCouchbase, err = k8sutil.CreateCouchbaseCluster(targetKube.CRClient, testCouchbase)
			case "apply":
				testCouchbase, err = k8sutil.UpdateCouchbaseCluster(targetKube.CRClient, testCouchbase)
			case "delete":
				err = k8sutil.DeleteCouchbaseCluster(targetKube.CRClient, testCouchbase)
			}

			// Handle successes when it shoud have failed.
			// Also if any validations are defined then ensure the updated CR matches.
			if err == nil {
				if test.shouldFail {
					t.Fatal("test unexpectedly succeeded")
				}
				if test.validations != nil {
					if err := jsonpatch.Apply(testCouchbase, test.validations.Patches()); err != nil {
						t.Fatal(err)
					}
				}
			}

			// When it did fail, handle when it shouldn't have done so.
			// If there were any errors to expect look for them.
			if err != nil {
				if !test.shouldFail {
					t.Fatalf("test unexpectedly failed: %v", err)
				}
				if len(test.expectedErrors) > 0 {
					for _, message := range test.expectedErrors {
						if !strings.Contains(err.Error(), message) || message == "" {
							t.Fatalf("expected message: %+v \n returned message: %v \n", message, err)
						}
					}
				}
			}
		})
	}

	// Removing deployment if any
	if !f.SkipTeardown {
		e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, kubeName, t.Name())
	}
}

func TestValidationCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	supportedTimeUnits := []string{"ns", "us", "ms", "s", "m", "h"}

	testDefs := []testDef{
		{
			name: "TestValidateDefault",
		},
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name:      "TestValidateLogRetentionTime_" + timeUnit,
			mutations: jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionTime", "1"+timeUnit),
		}
		testDefs = append(testDefs, testDefCase)
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		// Spec.ExposedFeatures list validation
		{
			name:           "TestValidateExposedFeaturesEnumInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ExposedFeatures", api.ExposedFeatureList{api.FeatureAdmin, "cleint", api.FeatureXDCR}),
			shouldFail:     true,
			expectedErrors: []string{"spec.exposedFeatures in body should be one of [admin xdcr client]"},
		},
		{
			name:           "TestValidateExposedFeaturesUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ExposedFeatures", api.ExposedFeatureList{api.FeatureAdmin, api.FeatureClient, api.FeatureXDCR, api.FeatureAdmin}),
			shouldFail:     true,
			expectedErrors: []string{"spec.exposedFeatures in body shouldn't contain duplicates"},
		},

		// Bucket validation test
		{
			name:           "TestValidateBucketNameInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "invalid!bucket!name"),
			shouldFail:     true,
			expectedErrors: []string{`spec.buckets.name in body should match '^[a-zA-Z0-9._\-%]*$'`},
		},
		{
			name:           "TestValidateBucketTypeEnumInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketType", "invalid-bucket-type"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.type in body should be one of [couchbase ephemeral memcached]"},
		},
		{
			name: "TestValidateBucketNameInvalidBucketTypeEnumInvalid",
			mutations: jsonpatch.NewPatchSet().
				Replace("/Spec/BucketSettings/0/BucketName", "invalid!bucket!name").
				Replace("/Spec/BucketSettings/0/BucketType", "invalid-bucket-type"),
			shouldFail: true,
			expectedErrors: []string{
				`spec.buckets.type in body should be one of [couchbase ephemeral memcached]`,
				`spec.buckets.name in body should match '^[a-zA-Z0-9._\-%]*$'`,
			},
		},
		{
			name:           "TestValidateBucketIndexReplicaInvalidForMemcached",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "TestValidateBucketIndexReplicaInvalidForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[3] must be of type nil: "Bucket type is ephemeral"`},
		},
		{
			name:           "TestValidateBucketReplicasInvalidForMemcached",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/BucketReplicas", 1),
			shouldFail:     true,
			expectedErrors: []string{`replicas in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "TestValidateBucketConflictResolutionInvalidForMemcached",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{`conflictResolution in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "TestValidateBucketEvictionPolicyInvalidForMemcached",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{`evictionPolicy in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "TestValidateBucketIOPriorityInvalidForMemcached",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/IoPriority", "low"),
			shouldFail:     true,
			expectedErrors: []string{`ioPriority in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "TestValidateBucketConflictResolutionRequiredForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/0/ConflictResolution"),
			shouldFail:     true,
			expectedErrors: []string{"conflictResolution in spec.buckets[0] is required"},
		},
		{
			name:       "TestValidateBucketEvictionPolicyRequiredForCouchbase",
			mutations:  jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/0/EvictionPolicy"),
			shouldFail: true,
			expectedErrors: []string{
				"evictionPolicy in spec.buckets[0] is required",
				"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]",
			},
		},
		{
			name:           "TestValidateBucketIOPriorityRequiredForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/0/IoPriority"),
			shouldFail:     true,
			expectedErrors: []string{"ioPriority in spec.buckets[0] is required"},
		},
		{
			name:           "TestValidateBucketConflictResolutionRequiredForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/3/ConflictResolution"),
			shouldFail:     true,
			expectedErrors: []string{"conflictResolution in spec.buckets[3] is required"},
		},
		{
			name:       "TestValidateBucketEvictionPolicyRequiredForEphemeral",
			mutations:  jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/3/EvictionPolicy"),
			shouldFail: true,
			expectedErrors: []string{
				"evictionPolicy in spec.buckets[3] is required",
				"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]",
			},
		},
		{
			name:           "TestValidateBucketIOPriorityRequiredForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/3/IoPriority"),
			shouldFail:     true,
			expectedErrors: []string{"ioPriority in spec.buckets[3] is required"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral_1",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral_2",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "fullEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidInvalidForCouchbase_1",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EvictionPolicy", "noEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidInvalidForCouchbase_2",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EvictionPolicy", "nruEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
		{
			name:           "TestValidateBucketNameUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "default3"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.name in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateBucketQuotaOverflow",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketMemoryQuota", 601),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 600"},
		},

		// Server settings validation
		{
			name:           "TestValidateServerServicesEnumInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.Service("indxe"), api.QueryService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.services in body should be one of [data index query search eventing analytics]"},
		},
		{
			name:           "TestValidateServerNameUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Name", "data_only"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.name in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.IndexService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateServerServicesUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.IndexService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body shouldn't contain duplicates"},
		},

		// ServerGroups list validation
		{
			name:           "TestValidateServerGroupsUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-1"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateServerServerGroupsUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/2/ServerGroups", []string{"us-east-1a", "us-east-1b", "us-east-1a"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body shouldn't contain duplicates"},
		},

		// Admin console services field validation
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.Service("indxe"), api.QueryService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},

		// Persistent volume claim cases
		{
			name:           "TestValidateVolumeClaimTemplatesStorageClassMustExist",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/StorageClassName", &unavailableStorageClass),
			shouldFail:     true,
			expectedErrors: []string{"storage class unavailableStorageClass must exist"},
		},
		{
			name:       "TestValidateVolumeClaimTemplateMustExist",
			mutations:  jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/ObjectMeta/Name", "InvalidVolumeClaim"),
			shouldFail: true,
			expectedErrors: []string{
				"spec.servers[0].default should be one of [InvalidVolumeClaim couchbase-log-pv]",
				"spec.servers[1].default should be one of [InvalidVolumeClaim couchbase-log-pv]",
				"spec.servers[1].data should be one of [InvalidVolumeClaim couchbase-log-pv]",
				"spec.servers[1].index should be one of [InvalidVolumeClaim couchbase-log-pv]",
				"spec.servers[1].analytics[0] should be one of [InvalidVolumeClaim couchbase-log-pv]",
				"spec.servers[1].analytics[1] should be one of [InvalidVolumeClaim couchbase-log-pv]",
				"spec.servers[2].default should be one of [InvalidVolumeClaim couchbase-log-pv]",
				"spec.servers[4].default should be one of [InvalidVolumeClaim couchbase-log-pv]",
			},
		},

		// Verify for Default/Log volume mounts mutual exclusion
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_1",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/LogsClaim", "couchbase"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_2",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Pod/VolumeMounts/DefaultClaim", "couchbase-log-pv"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[3].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "TestValidateDefaultVolumeMountRequiredForStatefulServices",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Services", api.ServiceList{api.DataService, api.QueryService, api.SearchService, api.EventingService, api.AnalyticsService}),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "TestValidateLogRetentionTimeInvalidPattern",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionTime", "1"),
			shouldFail:     true,
			expectedErrors: []string{`spec.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "TestValidateLogRetentionCountInvalidRange",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionCount", -1),
			shouldFail:     true,
			expectedErrors: []string{"spec.logRetentionCount in body should be greater than or equal to 0"},
		},
		// Missing referenced resources
		{
			name:           "TestValidateAuthSecretMissing",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AuthSecret", "does-not-exist"),
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist must exist"},
		},
		{
			name:           "TestValidateTLSServerSecretMissing",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/TLS/Static/Member/ServerSecret", "does-not-exist"),
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist must exist"},
		},
		{
			name:           "TestValidateTLSOperatorSecretMissing",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/TLS/Static/OperatorSecret", "does-not-exist"),
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist must exist"},
		},
	}

	// Cases to validate with invalidClaim name given in Pod.VolumeMounts.[Claims]
	volMountsMap := map[string]string{
		"DefaultClaim": "default",
		"DataClaim":    "data",
		"IndexClaim":   "index",
	}
	for mntField, mntName := range volMountsMap {
		testCase := testDef{
			name:           "TestValidateVolumeClaimTemplateMustExist_" + mntName,
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/"+mntField, "invalidClaim"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[1]." + mntName + " should be one of [couchbase couchbase-log-pv]"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with all volume mounts present in Pod.VolumeMounts but one of the Service missing in ServersSettings
	for _, serviceToSkip := range constants.StatefulCbServiceList {
		fieldValueToUse := constants.StatelessCbServiceList
		for _, statefulService := range constants.StatefulCbServiceList {
			if statefulService == serviceToSkip {
				continue
			}
			fieldValueToUse = append(fieldValueToUse, statefulService)
		}
		testCase := testDef{
			name:           "TestValidateServerServicesRequiredForVolumeMount_" + string(serviceToSkip),
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Services", fieldValueToUse),
			shouldFail:     true,
			expectedErrors: []string{string(serviceToSkip) + " in spec.servers[1].services is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV only defined,but one of stateful service is included
	for _, statefulService := range constants.StatefulCbServiceList {
		testCase := testDef{
			name:           "TestValidateDefaultVolumeMountRequiredForStatefulService_" + string(statefulService),
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Services", api.ServiceList{api.QueryService, api.SearchService, api.EventingService, statefulService}),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases for defining Stateful claims without specifying Default volume mounts
	claimFieldNames := []string{"DataClaim", "IndexClaim"}
	for _, claimField := range claimFieldNames {
		testCase := testDef{
			name: "TestValidateDefaultVolumeMountRequiredForServiceClaim_" + claimField,
			mutations: jsonpatch.NewPatchSet().
				Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/"+claimField, "couchbase").
				Remove("/Spec/ServerSettings/0/Pod/VolumeMounts/DefaultClaim"),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[0].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims is an array value
	testCase := testDef{
		name: "TestValidateDefaultVolumeMountRequiredForAnalyticsClaim",
		mutations: jsonpatch.NewPatchSet().
			Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/AnalyticsClaims", []string{"couchbase"}).
			Remove("/Spec/ServerSettings/0/Pod/VolumeMounts/DefaultClaim"),
		shouldFail:     true,
		expectedErrors: []string{"default in spec.servers[0].pod.volumeMounts is required"},
	}
	testDefs = append(testDefs, testCase)

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestValidationDefaultCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:        "TestValidateBaseImageDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/BaseImage"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/BaseImage", "couchbase/server"),
		},
		{
			name:        "TestValidateIndexServiceMemoryQuotaDefault",
			mutations:   jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexServiceMemQuota", uint64(0)),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexServiceMemQuota", uint64(256)),
		},
		{
			name:        "TestValidateSearchServiceMemoryQuotaDefault",
			mutations:   jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/SearchServiceMemQuota", uint64(0)),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/SearchServiceMemQuota", uint64(256)),
		},
		{
			name:        "TestValidateEventingServiceMemoryQuotaDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/EventingServiceMemQuota"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/EventingServiceMemQuota", uint64(256)),
		},
		{
			name:        "TestValidateAnalyticsServiceMemoryQuotaDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AnalyticsServiceMemQuota"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AnalyticsServiceMemQuota", uint64(1024)),
		},
		{
			name:        "TestValidateIndexStorageSetting",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/IndexStorageSetting"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexStorageSetting", "memory_optimized"),
		},
		{
			name:        "TestValidateAutoFailoverTimeoutDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverTimeout"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverTimeout", uint64(120)),
		},
		{
			name:        "TestValidateAutoFailoverMaxCountDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverMaxCount"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverMaxCount", uint64(3)),
		},
		{
			name:        "TestValidateAutoFailoverOnDataDiskIssuesPeriodDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverOnDataDiskIssuesTimePeriod"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverOnDataDiskIssuesTimePeriod", uint64(120)),
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationDefaultCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			// DataServiceMemQuota will get mutated to the default of 256, but we have
			// 500 worth of buckets defined.
			name:           "TestValidateDataServiceMemoryQuotaDefault",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/DataServiceMemQuota", uint64(0)),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 256"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationConstraintsCreate(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.SearchService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.Service("xxxxx")}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},
		{
			name:           "TestValidateBucketIndexReplicasInvalidForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[3] must be of type nil: "Bucket type is ephemeral"`},
		},
		{
			name:           "TestValidateBucketConflictResolutionInvalidForMemcached",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{`conflictResolution in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "TestValidateBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/IoPriority", "lighow"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.ioPriority in body should be one of [high low]"},
		},
		{
			name:           "TestValidateBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/ConflictResolution", "selwwno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.conflictResolution in body should be one of [seqno lww]"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EvictionPolicy", "nruEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

// cbopctl apply tests
func TestValidationApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	supportedTimeUnits := []string{"ns", "us", "ms", "s", "m", "h"}

	testDefs := []testDef{
		{
			name: "TestValidateDefault",
		},
		{
			name:        "TestValidateApplyBucketName",
			mutations:   jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "newNameForBucket"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/BucketSettings/0/BucketName", "newNameForBucket"),
		},
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name:      "TestValidateApplyLogRestentionTime_" + timeUnit,
			mutations: jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionTime", "100"+timeUnit),
		}
		testDefs = append(testDefs, testDefCase)
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	testDefs := []testDef{
		{
			name:           "TestValidateApplyBucketNameInvalidPattern",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "invalid!bucket!name"),
			shouldFail:     true,
			expectedErrors: []string{`spec.buckets.name in body should match '^[a-zA-Z0-9._\-%]*$'`},
		},
		{
			name:           "TestValidateApplyBucketTypeEnumInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketType", "invalid-bucket-type"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.type in body should be one of [couchbase ephemeral memcached]"},
		},
		{
			name:           "TestValidateServerServicesImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Name", "data_only"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		// Verify for Default/Log volume mounts mutual exclusion
		{
			name:           "TestValidateVolumeMountsImmutable_1",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/LogsClaim", "couchbase"),
			shouldFail:     true,
			expectedErrors: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateVolumeMountsImmutable_2",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Pod/VolumeMounts/DefaultClaim", "couchbase-log-pv"),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateVolumeMountsImmutable_3",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/2/Pod/VolumeMounts/LogsClaim", "couchbase-log-pv"),
			shouldFail:     true,
			expectedErrors: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "TestValidateApplyLogRetentionTimeInvalidPattern",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionTime", "1"),
			shouldFail:     true,
			expectedErrors: []string{`spec.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "TestValidateApplyLogRetentionCountInvalidRange",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionCount", -1),
			shouldFail:     true,
			expectedErrors: []string{"spec.logRetentionCount in body should be greater than or equal to 0"},
		},
	}

	// Cases to validate with all volume mounts present in Pod.VolumeMounts but one of the Service missing in ServersSettings
	for _, serviceToSkip := range constants.StatefulCbServiceList {
		fieldValueToUse := constants.StatelessCbServiceList
		for _, statefulService := range constants.StatefulCbServiceList {
			if statefulService == serviceToSkip {
				continue
			}
			fieldValueToUse = append(fieldValueToUse, statefulService)
		}
		testCase := testDef{
			name:           "TestValidateApplyServerServicesImmutable_" + string(serviceToSkip),
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Services", fieldValueToUse),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[1].services in body cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV defined along with stateful services
	for _, serviceName := range constants.StatefulCbServiceList {
		fieldValueToUse := constants.StatelessCbServiceList
		fieldValueToUse = append(fieldValueToUse, serviceName)
		testCase := testDef{
			name:           "TestValidateApplyServerServicesImmutable_" + string(serviceName),
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Services", fieldValueToUse),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[3].services in body cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate by defining Stateful volumes without defining DefaultClaim
	volMountsMap := map[string]string{
		"DataClaim":  "data",
		"IndexClaim": "index",
	}
	for mntField, mntName := range volMountsMap {
		testCase := testDef{
			name: "TestValidateApplyServerVolumeMountsImmutable_" + mntName,
			mutations: jsonpatch.NewPatchSet().
				Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/"+mntField, "couchbase").
				Remove("/Spec/ServerSettings/0/Pod/VolumeMounts/DefaultClaim"),
			shouldFail:     true,
			expectedErrors: []string{mntName + " in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims in an array parameter
	testCase := testDef{
		name:           "TestValidateApplyServerVolumeMountsImmutable_analytics",
		mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/AnalyticsClaims", []string{"couchbase"}).Remove("/Spec/ServerSettings/0/Pod/VolumeMounts/DefaultClaim"),
		shouldFail:     true,
		expectedErrors: []string{"analytics in spec.servers[*].Pod.VolumeMounts cannot be updated"},
	}
	testDefs = append(testDefs, testCase)

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestValidationDefaultApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:        "TestValidateApplyBaseImageDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/BaseImage"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/BaseImage", "couchbase/server"),
		},
		{
			name:        "TestValidateApplyIndexServiceMemoryQuotaDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/IndexServiceMemQuota"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexServiceMemQuota", uint64(256)),
		},
		{
			name:        "TestValidateApplySearchServiceMemoryQuotaDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/SearchServiceMemQuota"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/SearchServiceMemQuota", uint64(256)),
		},
		{
			name:        "TestValidateApplyAutoFailoverTimeoutDefault",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverTimeout"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverTimeout", uint64(120)),
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationConstraintsApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:           "TestValidateApplyAdminConsoleServicesUnique",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.SearchService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateApplyAdminConsoleServicesEnumInvalid",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.Service("xxxxx")}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},
		{
			name:           "TestValidateApplyBucketIndexReplicasInvalidForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[3] must be of type nil: "Bucket type is ephemeral"`},
		},
		{
			name:           "TestValidateApplyBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/IoPriority", "lighow"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.ioPriority in body should be one of [high low]"},
		},
		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},

		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EvictionPolicy", "nruEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationImmutableApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		// Bucket spec updation
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForEphemeral",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/4/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[4].conflictResolution in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForMemcached",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[2].conflictResolution in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/ConflictResolution", "selwwno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.conflictResolution in body should be one of [seqno lww]"},
		},
		{
			name:           "TestValidateApplyBucketTypeImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketType", "memcached"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[0].type in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForCouchbase",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/ConflictResolution", "lww"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[0].conflictResolution in body cannot be updated"},
		},
		// ServerSettings service update
		{
			name:           "TestValidateApplyServerServicesImmutable_1",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.IndexService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyServerServicesImmutable_2",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyAntiAffinityImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AntiAffinity", true),
			shouldFail:     true,
			expectedErrors: []string{"spec.antiAffinity in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyAuthSecretImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AuthSecret", "auth-secret-update"),
			shouldFail:     true,
			expectedErrors: []string{"spec.authSecret in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyIndexStorageSettingsImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexStorageSetting", "plasma"),
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.indexStorageSetting in body cannot be updated"},
		},
		// Server groups updates
		{
			name:           "TestValidateApplyServerGroupsImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-2"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyServersServerGroupsImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/2/ServerGroups", []string{"us-east-1a", "us-east-1b", "us-east-1c"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body cannot be updated"},
		},
		// Persistent volume spec updation
		{
			name:           "TestValidateApplyVolumeMountsImmutable_data",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/DataClaim", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"data in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeMountsImmutable_default",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/DefaultClaim", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeMountsImmutable_index",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/IndexClaim", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"index in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeMountsImmutable_analytics",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/AnalyticsClaims/0", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"analytics in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeTemplatesStorageClassImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/StorageClassName", &unavailableStorageClass),
			shouldFail:     true,
			expectedErrors: []string{`"storageClassName" in spec.volumeClaimTemplates[*] cannot be updated`},
		},
		{
			name:           "TestValidateApplyVolumeTemplatesResourcesRequestsImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/Resources/Requests", corev1.ResourceList{corev1.ResourceStorage: *apiresource.NewScaledQuantity(10, 30)}),
			shouldFail:     true,
			expectedErrors: []string{`"storage" in spec.volumeClaimTemplates[*].resources.requests cannot be updated`},
		},
		{
			name:           "TestValidateApplyVolumeTemplatesResourcesLimitsImmutable",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/Resources/Limits", corev1.ResourceList{corev1.ResourceStorage: *apiresource.NewScaledQuantity(10, 30)}),
			shouldFail:     true,
			expectedErrors: []string{`"storage" in spec.volumeClaimTemplates[*].resources.limits cannot be updated`},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

//cbopctl delete tests
func TestValidationDelete(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:      "TestValidateDeleteAfterApplyBucketName",
			mutations: jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "newBucketName"),
		},
		{
			name: "TestValidateDelete",
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "delete")
}

func TestNegValidationDelete(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:           "TestValidateDeleteRenamedCluster",
			mutations:      jsonpatch.NewPatchSet().Replace("/ObjectMeta/Name", "cb-example1"),
			shouldFail:     true,
			expectedErrors: []string{`couchbaseclusters.couchbase.com "cb-example1" not found`},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "delete")
}

/*******************************************************************
************ Test cases for RZA / Server group testing *************
********************************************************************/

// Deploy couchbase cluster over non existent server group
func TestRzaNegCreateCluster(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateServerGroupsInvalid_1",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"InvalidGroup-1", "InvalidGroup-2"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servergroups in body is invalid"},
		},
		{
			name:           "TestValidateServerGroupsInvalid_2",
			shouldFail:     true,
			expectedErrors: []string{"spec.servergroups missing for "},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}
