package e2e

import (
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/decoder"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

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

func ExpectedClusterSize(cluster *api.CouchbaseCluster) int {
	clusterSize := 0
	for _, server := range cluster.Spec.ServerSettings {
		clusterSize = clusterSize + server.Size
	}
	return clusterSize
}

// camelify turns plan english names into more standard looking test names.
// TODO: Just make the names sensible to start with...
func camelify(name string) string {
	// Replace non text characters with white space
	re := regexp.MustCompile(`[^\w]`)
	name = re.ReplaceAllString(name, " ")

	// Transform each word into bumpy caps
	words := strings.Split(name, " ")
	for i, word := range words {
		if len(word) != 0 {
			words[i] = strings.ToUpper(string(word[0])) + word[1:]
		}
	}

	// And for the sake of convention tack on a test prefix
	return "Test" + strings.Join(words, "")
}

func runValidationTest(t *testing.T, testDefs []testDef, kubeName, command string) {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]
	for _, test := range testDefs {
		// Run each test case defined as a separate test so we have a way
		// of running them individually.
		name := camelify(test.name)
		t.Run(name, func(t *testing.T) {
			testCouchbase, err := YAMLToCluster("./resources/validation/validation.yaml")
			if err != nil {
				t.Fatal(err)
			}

			testCouchbase.Spec.AuthSecret = targetKube.DefaultSecret.Name
			testCouchbase.ObjectMeta.Namespace = f.Namespace

			// Removing previous deployment if any
			e2eutil.CleanUpCluster(t, targetKube.KubeClient, targetKube.CRClient, f.Namespace, f.LogDir, kubeName, t.Name())

			// If we are applying a change or deleting a cluster we first need to create it...
			if command == "apply" || command == "delete" {
				if _, err := k8sutil.CreateCouchbaseCluster(targetKube.CRClient, testCouchbase); err != nil && !test.shouldFail {
					t.Fatal(err)
				}

				clusterSize := ExpectedClusterSize(testCouchbase)
				if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, testCouchbase.Name, f.Namespace, clusterSize, constants.Retries60); err != nil {
					t.Fatal(err)
				}

				// Update to the latest revision so and update is less likely to fail with a CAS collision
				var err error
				if testCouchbase, err = k8sutil.GetCouchbaseCluster(targetKube.CRClient, f.Namespace, testCouchbase.Name); err != nil {
					t.Fatal(err)
				}
			}

			// Patch the cluster specification
			if err := jsonpatch.Apply(testCouchbase, test.mutations.Patches()); err != nil {
				t.Fatal(err)
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
			// If the command was a create of apply ensure that any validation checks pass.
			if err == nil {
				if test.shouldFail {
					t.Fatal("test unexpectedly succeeded")
				}
				if command == "create" || command == "apply" {
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
			name: "create default yaml",
		},
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name:      "Apply spec.logRetentionTime with time duration in '" + timeUnit + "'",
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
			name:           "Validate spec.exposedFeatures field values",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ExposedFeatures", api.ExposedFeatureList{api.FeatureAdmin, "cleint", api.FeatureXDCR}),
			shouldFail:     true,
			expectedErrors: []string{"spec.exposedFeatures in body should be one of [admin xdcr client]"},
		},
		{
			name:           "Validate spec.exposedFeatures fields uniqueness",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ExposedFeatures", api.ExposedFeatureList{api.FeatureAdmin, api.FeatureClient, api.FeatureXDCR, api.FeatureAdmin}),
			shouldFail:     true,
			expectedErrors: []string{"spec.exposedFeatures in body shouldn't contain duplicates"},
		},

		// Bucket validation test
		{
			name:           "create invalid bucket name",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "invalid!bucket!name"),
			shouldFail:     true,
			expectedErrors: []string{`spec.buckets.name in body should match '^[a-zA-Z0-9._\-%]*$'`},
		},
		{
			name:           "create invalid bucket type",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketType", "invalid-bucket-type"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.type in body should be one of [couchbase ephemeral memcached]"},
		},
		{
			name:       "create invalid bucket name and bucket type",
			mutations:  jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "invalid!bucket!name").Replace("/Spec/BucketSettings/0/BucketType", "invalid-bucket-type"),
			shouldFail: true,
			expectedErrors: []string{
				`spec.buckets.type in body should be one of [couchbase ephemeral memcached]`,
				`spec.buckets.name in body should match '^[a-zA-Z0-9._\-%]*$'`,
			},
		},
		{
			name:           "Validate spec.bucket.enableIndexReplica with memcached bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "Validate spec.bucket.enableIndexReplica with ephemeral bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[3] must be of type nil: "Bucket type is ephemeral"`},
		},
		{
			name:           "Validate spec.bucket.replicas with memcached bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/BucketReplicas", 1),
			shouldFail:     true,
			expectedErrors: []string{`replicas in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "Validate spec.bucket.conflictResolution with memcached bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{`conflictResolution in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "Validate spec.bucket.evictionPolicy with memcached bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{`evictionPolicy in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "Validate spec.bucket.ioPriority with memcached bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/IoPriority", "low"),
			shouldFail:     true,
			expectedErrors: []string{`ioPriority in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "Validate spec.bucket.conflictResolution with couchbase bucket",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/0/ConflictResolution"),
			shouldFail:     true,
			expectedErrors: []string{"conflictResolution in spec.buckets[0] is required"},
		},
		{
			name:       "Validate spec.bucket.evictionPolicy with couchbase bucket",
			mutations:  jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/0/EvictionPolicy"),
			shouldFail: true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[0] is required",
				"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
		{
			name:           "Validate spec.bucket.ioPriority with couchbase bucket",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/0/IoPriority"),
			shouldFail:     true,
			expectedErrors: []string{"ioPriority in spec.buckets[0] is required"},
		},
		{
			name:           "Validate spec.bucket.conflictResolution with ephemeral bucket",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/3/ConflictResolution"),
			shouldFail:     true,
			expectedErrors: []string{"conflictResolution in spec.buckets[3] is required"},
		},
		{
			name:       "Validate spec.bucket.evictionPolicy with ephemeral bucket",
			mutations:  jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/3/EvictionPolicy"),
			shouldFail: true,
			expectedErrors: []string{
				"evictionPolicy in spec.buckets[3] is required",
				"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]",
			},
		},
		{
			name:           "Validate spec.bucket.ioPriority with ephemeral bucket",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings/3/IoPriority"),
			shouldFail:     true,
			expectedErrors: []string{"ioPriority in spec.buckets[3] is required"},
		},
		{
			name:           "Validate spec.buckets.evictionPolicy as valueOnly for ephemeral bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name:           "Validate spec.buckets.evictionPolicy as fullEviction for ephemeral bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "fullEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name:           "Validate spec.buckets.evictionPolicy as noEviction for couchbase bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EvictionPolicy", "noEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
		{
			name:           "Validate spec.buckets.evictionPolicy as nruEviction for couchbase bucket",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EvictionPolicy", "nruEviction"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[0] should be one of [valueOnly fullEviction]"},
		},
		{
			name:           "Validate spec.buckets.name for duplicate values",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "default3"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.name in body shouldn't contain duplicates"},
		},
		{
			name:           "Validate spec.buckets.quota with more than declared",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketMemoryQuota", 601),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[*].memoryQuota in body should be less than or equal to 600"},
		},

		// Server settings validation
		{
			name:           "Validate server-settings services fields",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.Service("indxe"), api.QueryService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.services in body should be one of [data index query search eventing analytics]"},
		},
		{
			name:           "create invalid server name",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Name", "data_only"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.name in body shouldn't contain duplicates"},
		},
		{
			name:           "Validate spec.adminConsoleServices fields uniqueness",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.IndexService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "Validate spec.serverSettings.services fields for uniqueness",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.IndexService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body shouldn't contain duplicates"},
		},

		// ServerGroups list validation
		{
			name:           "Validate spec.serverGroups uniqueness",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-1"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body shouldn't contain duplicates"},
		},
		{
			name:           "Validate spec.servers.serverGroups list uniqueness",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/2/ServerGroups", []string{"us-east-1a", "us-east-1b", "us-east-1a"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body shouldn't contain duplicates"},
		},

		// Admin console services field validation
		{
			name:           "Validate AdminConsoleService fields",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.Service("indxe"), api.QueryService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},

		// Persistent volume claim cases
		{
			name:           "Validate spec.volumeClaimTemplates.Spec.storageClassName to be defined",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/StorageClassName", &unavailableStorageClass),
			shouldFail:     true,
			expectedErrors: []string{"storage class unavailableStorageClass must exist"},
		},
		{
			name:       "Create PVC cluster with unavailable volume claim template defined in spec",
			mutations:  jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/ObjectMeta/Name", "InvalidVolumeClaim"),
			shouldFail: true,
			expectedErrors: []string{
				`"couchbase" in spec.volumeClaimTemplates[*].metadata.name is required`,
				`"couchbase" in spec.volumeClaimTemplates[*].metadata.name is required`,
				`"couchbase" in spec.volumeClaimTemplates[*].metadata.name is required`,
				`"couchbase" in spec.volumeClaimTemplates[*].metadata.name is required`,
				`"couchbase" in spec.volumeClaimTemplates[*].metadata.name is required`,
			},
		},

		// Verify for Default/Log volume mounts mutual exclusion
		{
			name:           "Log volume defined on top of default volume mount in spec.servers.pod.volumeMounts.properties",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/LogsClaim", "couchbase"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "default volume defined on top of log volume mounts specified in spec.servers.pod.volumeMounts.properties",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Pod/VolumeMounts/DefaultClaim", "couchbase-log-pv"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[3].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "Create with data service on top of stateless services",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Services", api.ServiceList{api.DataService, api.QueryService, api.SearchService, api.EventingService, api.AnalyticsService}),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "Invalid value for spec.logRetentionTime",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionTime", "1"),
			shouldFail:     true,
			expectedErrors: []string{`spec.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "Negative value for spec.logRetentionCount",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionCount", -1),
			shouldFail:     true,
			expectedErrors: []string{"spec.logRetentionCount in body should be greater than or equal to 0"},
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
			name:           "Validate spec.servers.pod.volumeMounts.properties." + mntName + " values to be defined",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/"+mntField, "invalidClaim"),
			shouldFail:     true,
			expectedErrors: []string{`"invalidClaim" in spec.volumeClaimTemplates[*].metadata.name is required`},
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
			name:           "Validate spec.servers.services with all persistent mount defined, but " + string(serviceToSkip) + "service missing in config",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Services", fieldValueToUse),
			shouldFail:     true,
			expectedErrors: []string{string(serviceToSkip) + " in spec.servers[1].services is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV only defined,but one of stateful service is included
	for _, statefulService := range constants.StatefulCbServiceList {
		testCase := testDef{
			name:           "Validate spec.servers.services with only log volume mount defined, with " + string(statefulService) + " service included",
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
			name:           "Validate by defining " + claimField + " without Default volume mount",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/"+claimField, "couchbase").Remove("/Spec/ServerSettings/0/Pod/VolumeMounts/DefaultClaim"),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[0].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims is an array value
	testCase := testDef{
		name:           "Validate by defining AnalyticsClaims without Default volume mount",
		mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/AnalyticsClaims", []string{"couchbase"}).Remove("/Spec/ServerSettings/0/Pod/VolumeMounts/DefaultClaim"),
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
			name:        "create:default:Spec.BaseImage",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/BaseImage"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/BaseImage", "couchbase/server"),
		},
		{
			name:        "create:default:Spec.ClusterSettings.IndexServiceMemQuota",
			mutations:   jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexServiceMemQuota", 0),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexServiceMemQuota", 256),
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
			name:           "create:default:Spec.ClusterSettings.DataServiceMemQuota",
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
			name:           "more than 4 adminConsoleServices",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.SearchService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "invalid adminConsoleService",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.Service("xxxxx")}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},
		{
			name:           "ephemeral bucket with index replicas enabled",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[3] must be of type nil: "Bucket type is ephemeral"`},
		},
		{
			name:           "memcached bucket with seqno conflict resolution",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{`conflictResolution in spec.buckets[2] must be of type nil: "Bucket type is memcached"`},
		},
		{
			name:           "couchbase bucket with invalid io priority",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/IoPriority", "lighow"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.ioPriority in body should be one of [high low]"},
		},
		{
			name:           "couchbase bucket with invalid conflict resolution",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/ConflictResolution", "selwwno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.conflictResolution in body should be one of [seqno lww]"},
		},
		{
			name:           "ephemeral bucket with invalid eviction policy",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},
		{
			name:           "couchbase bucket with invalid eviction policy",
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
			name: "Apply without any changes in cluster yaml",
		},
		{
			name:        "Apply: update the bucket[0].name",
			mutations:   jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "newNameForBucket"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/BucketSettings/0/BucketName", "newNameForBucket"),
		},
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name:      "Apply spec.logRetentionTime with time duration in '" + timeUnit + "'",
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
			name:           "apply invalid changes to bucket name",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "invalid!bucket!name"),
			shouldFail:     true,
			expectedErrors: []string{`spec.buckets.name in body should match '^[a-zA-Z0-9._\-%]*$'`},
		},
		{
			name:           "apply invalid changes to bucket type",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketType", "invalid-bucket-type"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.type in body should be one of [couchbase ephemeral memcached]"},
		},
		{
			name:           "apply invalid changes to server name",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Name", "data_only"),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		// Verify for Default/Log volume mounts mutual exclusion
		{
			name:           "Log volume defined on top of default volume mount in spec.servers.pod.volumeMounts.properties",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/LogsClaim", "couchbase"),
			shouldFail:     true,
			expectedErrors: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "Default volume defined on top of log volume mount in spec.servers.pod.volumeMounts.properties",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/3/Pod/VolumeMounts/DefaultClaim", "couchbase-log-pv"),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "Log volume defined on top of default volume mount in spec.servers.pod.volumeMounts.properties",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/2/Pod/VolumeMounts/LogsClaim", "couchbase-log-pv"),
			shouldFail:     true,
			expectedErrors: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "Invalid value for spec.logRetentionTime",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/LogRetentionTime", "1"),
			shouldFail:     true,
			expectedErrors: []string{`spec.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "Negative value for spec.logRetentionCount",
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
			name:           "Validate spec.servers.services with all persistent mount defined, but " + string(serviceToSkip) + "service missing in config",
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
			name:           "Validate spec.servers.services with only log volume mount defined, with " + string(serviceName) + " service included",
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
			name:           "Validate by defining " + mntField + " without Default volume mount",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Pod/VolumeMounts/"+mntField, "couchbase").Remove("/Spec/ServerSettings/0/Pod/VolumeMounts/DefaultClaim"),
			shouldFail:     true,
			expectedErrors: []string{mntName + " in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims in an array parameter
	testCase := testDef{
		name:           "Validate by defining AnalyticsClaims without Default volume mount",
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
			name:        "apply defaults to base image",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/BaseImage"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/BaseImage", "couchbase/server"),
		},
		{
			name:        "apply defaults to indexServiceMemQuota",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/IndexServiceMemQuota"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexServiceMemQuota", 256),
		},
		{
			name:        "apply defaults to searchServiceMemQuota",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/SearchServiceMemQuota"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/SearchServiceMemQuota", 256),
		},
		{
			name:        "apply defaults to autofailover timeout",
			mutations:   jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverTimeout"),
			validations: jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverTimeout", 120),
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationDefaultApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:           "create:default:Spec.ClusterSettings.DataServiceMemQuota",
			mutations:      jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/DataServiceMemQuota"),
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationConstraintsApply(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	testDefs := []testDef{
		{
			name:           "more than 4 adminConsoleServices",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.SearchService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "invalid adminConsoleService",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AdminConsoleServices", api.ServiceList{api.DataService, api.IndexService, api.QueryService, api.Service("xxxxx")}),
			shouldFail:     true,
			expectedErrors: []string{"spec.adminConsoleServices in body should be one of [data index query search eventing analytics]"},
		},
		{
			name:           "ephemeral bucket with index replicas enabled",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EnableIndexReplica", true),
			shouldFail:     true,
			expectedErrors: []string{`enableReplicaIndex in spec.buckets[3] must be of type nil: "Bucket type is ephemeral"`},
		},
		{
			name:           "couchbase bucket with invalid io priority",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/IoPriority", "lighow"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.ioPriority in body should be one of [high low]"},
		},
		{
			name:           "ephemeral bucket with invalid eviction policy",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/3/EvictionPolicy", "valueOnly"),
			shouldFail:     true,
			expectedErrors: []string{"evictionPolicy in spec.buckets[3] should be one of [noEviction nruEviction]"},
		},

		{
			name:           "couchbase bucket with invalid eviction policy",
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
			name:           "ephemeral bucket with seqno conflict resolution",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/4/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[4].conflictResolution in body cannot be updated"},
		},
		{
			name:           "memcached bucket with seqno conflict resolution",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/2/ConflictResolution", "seqno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[2].conflictResolution in body cannot be updated"},
		},
		{
			name:           "couchbase bucket with invalid conflict resolution",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/ConflictResolution", "selwwno"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets.conflictResolution in body should be one of [seqno lww]"},
		},
		{
			name:           "Update spec.buckets.type value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/Type", "memcached"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[0].type in body cannot be updated"},
		},
		{
			name:           "Update spec.buckets.conflictResolution value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/ConflictResolution", "lww"),
			shouldFail:     true,
			expectedErrors: []string{"spec.buckets[0].conflictResolution in body cannot be updated"},
		},
		{
			name:      "Update spec.buckets.ioPriority value",
			mutations: jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/IoPriority", "low"),
		},
		{
			name:      "Update spec.buckets.evictionPolicy value",
			mutations: jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EvictionPolicy", "valueOnly"),
		},
		// ServerSettings service update
		{
			name:           "Update spec.servers.services list",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.IndexService, api.SearchService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "Update spec.serverSettings.services values for uniqueness",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/0/Services", api.ServiceList{api.DataService, api.DataService}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "Update AntiAffinity settings",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AntiAffinity", true),
			shouldFail:     true,
			expectedErrors: []string{"spec.antiAffinity in body cannot be updated"},
		},
		{
			name:           "Update AuthSecret value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/AuthSecret", "auth-secret-update"),
			shouldFail:     true,
			expectedErrors: []string{"spec.authSecret in body cannot be updated"},
		},
		{
			name:           "change index storage mode",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexStorageSetting", "plasma"),
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.indexStorageSetting in body cannot be updated"},
		},
		// Server groups updates
		{
			name:           "Update server groups value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-2"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body cannot be updated"},
		},
		{
			name:           "Update spec.servers.serverGroups list",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/2/ServerGroups", []string{"us-east-1a", "us-east-1b", "us-east-1c"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body cannot be updated"},
		},
		// Persistent volume spec updation
		{
			name:           "Update spec.servers.pod.volumeMounts.properties.data value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/DataClaim", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"data in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "Update spec.servers.pod.volumeMounts.properties.default value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/DefaultClaim", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "Update spec.servers.pod.volumeMounts.properties.index value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/IndexClaim", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"index in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "Update spec.servers.pod.volumeMounts.properties.analytics value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerSettings/1/Pod/VolumeMounts/AnalyticsClaims/0", "newVolumeMount"),
			shouldFail:     true,
			expectedErrors: []string{"analytics in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "Apply: Remove default volume claim template name",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/StorageClassName", &unavailableStorageClass),
			shouldFail:     true,
			expectedErrors: []string{`"storageClassName" in spec.volumeClaimTemplates[*] cannot be updated`},
		},
		{
			name:           "Update spec.volumeClaimTemplates.spec.resources.requests.storage value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/Resources/Requests", 10),
			shouldFail:     true,
			expectedErrors: []string{`"storage" in spec.volumeClaimTemplates[*].resources.requests cannot be updated`},
		},
		{
			name:           "Update spec.volumeClaimTemplates.spec.resources.limits.storage value",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/Resources/Limits", apiresource.NewQuantity(int64(6)*1024*1024*1024, apiresource.BinarySI)),
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
			name:      "delete after modifying",
			mutations: jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketName", "newBucketName"),
		},
		{
			name: "delete",
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
		// SM: This is testing kubernetes, not anything to do with us, delete me
		{
			name:           "delete",
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
			name:           "Creating cluster with incorrect server-group in static config",
			mutations:      jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"InvalidGroup-1", "InvalidGroup-2"}),
			shouldFail:     true,
			expectedErrors: []string{"spec.servergroups in body is invalid"},
		},
		{
			name:           "Create cluster with server-group missing in default and class specific config",
			shouldFail:     true,
			expectedErrors: []string{"spec.servergroups missing for "},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}
