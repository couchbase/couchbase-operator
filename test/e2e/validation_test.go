package e2e

import (
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/gocbmgr"

	"github.com/ghodss/yaml"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

var (
	unavailableStorageClass       = "unavailableStorageClass"
	defaultFSGroup          int64 = 1000
)

// Resources are stored in a list in order to maintain ordering.  Do not try to use a map
// or things will happen in a random order.
type resourceList []runtime.Object

// We validate operations on multiple objects concurrently, so need a way to discriminate
// which patches to pply to which resources.  At present we key on name, so resources must
// be uniquely named.
type patchMap map[string]jsonpatch.PatchSet

type testDef struct {
	name           string
	mutations      patchMap
	validations    patchMap
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

// loadResources loads all defined resources from a static YAML file.
// As we use multiple resources to create and manage a cluster we need to pass things
// around in a generic way.  To this end we use the object interface which all API
// types implement.
func loadResources(path string) (resourceList, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	res := resourceList{}

	parts := strings.Split(string(data), "---\n")
	for _, part := range parts {
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(part), u); err != nil {
			return nil, err
		}

		object, err := scheme.Scheme.New(u.GetObjectKind().GroupVersionKind())
		if err != nil {
			return nil, err
		}

		decoder := scheme.Codecs.UniversalDeserializer()
		if object, _, err = decoder.Decode([]byte(part), nil, object); err != nil {
			return nil, err
		}
		res = append(res, object)
	}

	return res, nil
}

// createResources iterates over every resource and creates them in the requested namespace.
func createResources(k8s *types.Cluster, namespace string, resources resourceList) error {
	for i, resource := range resources {
		switch k := resource.GetObjectKind().GroupVersionKind().Kind; k {
		case couchbasev2.ClusterCRDResourceKind:
			cluster := resource.(*couchbasev2.CouchbaseCluster)
			cluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(namespace).Create(cluster)
			if err != nil {
				return err
			}
			resources[i] = cluster
		case couchbasev2.BucketCRDResourceKind:
			bucket := resource.(*couchbasev2.CouchbaseBucket)
			bucket, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(namespace).Create(bucket)
			if err != nil {
				return err
			}
			resources[i] = bucket
		case couchbasev2.EphemeralBucketCRDResourceKind:
			bucket := resource.(*couchbasev2.CouchbaseEphemeralBucket)
			bucket, err := k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).Create(bucket)
			if err != nil {
				return err
			}
			resources[i] = bucket
		case couchbasev2.MemcachedBucketCRDResourceKind:
			bucket := resource.(*couchbasev2.CouchbaseMemcachedBucket)
			bucket, err := k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).Create(bucket)
			if err != nil {
				return err
			}
			resources[i] = bucket
		case couchbasev2.ReplicationCRDResourceKind:
			replication := resource.(*couchbasev2.CouchbaseReplication)
			replication, err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(namespace).Create(replication)
			if err != nil {
				return err
			}
			resources[i] = replication
		default:
			return fmt.Errorf("create: unsupported kind: %v", k)
		}
	}
	return nil
}

// updateResources updates all defined resources.
func updateResources(k8s *types.Cluster, resources resourceList) error {
	for i, resource := range resources {
		switch t := resource.(type) {
		case *couchbasev2.CouchbaseCluster:
			cluster, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(t.Namespace).Update(t)
			if err != nil {
				return err
			}
			resources[i] = cluster
		case *couchbasev2.CouchbaseBucket:
			bucket, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(t.Namespace).Update(t)
			if err != nil {
				return err
			}
			resources[i] = bucket
		case *couchbasev2.CouchbaseEphemeralBucket:
			bucket, err := k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(t.Namespace).Update(t)
			if err != nil {
				return err
			}
			resources[i] = bucket
		case *couchbasev2.CouchbaseMemcachedBucket:
			bucket, err := k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(t.Namespace).Update(t)
			if err != nil {
				return err
			}
			resources[i] = bucket
		case *couchbasev2.CouchbaseReplication:
			replication, err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(t.Namespace).Update(t)
			if err != nil {
				return err
			}
			resources[i] = replication
		default:
			return fmt.Errorf("update: unsupported kind: %v", t)
		}
	}
	return nil
}

// deleteResources deletes all defined resources.
func deleteResources(k8s *types.Cluster, resources resourceList) error {
	options := metav1.NewDeleteOptions(0)
	for _, resource := range resources {
		switch t := resource.(type) {
		case *couchbasev2.CouchbaseCluster:
			if err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(t.Namespace).Delete(t.Name, options); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseBucket:
			if err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(t.Namespace).Delete(t.Name, options); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseEphemeralBucket:
			if err := k8s.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(t.Namespace).Delete(t.Name, options); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseMemcachedBucket:
			if err := k8s.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(t.Namespace).Delete(t.Name, options); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseReplication:
			if err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(t.Namespace).Delete(t.Name, options); err != nil {
				return err
			}
		default:
			return fmt.Errorf("delete: unsupported kind: %v", t)
		}
	}
	return nil
}

// patchResources applies JSON patches to all defined resources.
func patchResources(resources resourceList, patches patchMap) error {
	for _, resource := range resources {
		switch t := resource.(type) {
		case *couchbasev2.CouchbaseCluster:
			patchset, ok := patches[t.Name]
			if !ok {
				break
			}
			if err := jsonpatch.Apply(t, patchset.Patches()); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseBucket:
			patchset, ok := patches[t.Name]
			if !ok {
				break
			}
			if err := jsonpatch.Apply(t, patchset.Patches()); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseEphemeralBucket:
			patchset, ok := patches[t.Name]
			if !ok {
				break
			}
			if err := jsonpatch.Apply(t, patchset.Patches()); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseMemcachedBucket:
			patchset, ok := patches[t.Name]
			if !ok {
				break
			}
			if err := jsonpatch.Apply(t, patchset.Patches()); err != nil {
				return err
			}
		case *couchbasev2.CouchbaseReplication:
			patchset, ok := patches[t.Name]
			if !ok {
				break
			}
			if err := jsonpatch.Apply(t, patchset.Patches()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("patch: unsupported kind: %v", t)
		}
	}
	return nil

}

func runValidationTest(t *testing.T, testDefs []testDef, kubeName, command string) {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]

	// Stop the operator, we don't actually need it to validate the API and the tests will take forever.
	_ = framework.DeleteOperatorCompletely(targetKube.KubeClient, f.Deployment.Name, f.Namespace)
	defer func() { _ = f.SetupCouchbaseOperator(targetKube) }()

	for _, test := range testDefs {
		// Run each test case defined as a separate test so we have a way
		// of running them individually.
		t.Run(test.name, func(t *testing.T) {
			objects, err := loadResources("./resources/validation/validation.yaml")
			if err != nil {
				t.Fatal(err)
			}

			for _, object := range objects {
				if object.GetObjectKind().GroupVersionKind().Kind != "CouchbaseCluster" {
					continue
				}

				cluster := object.(*couchbasev2.CouchbaseCluster)

				tlsOpts := &e2eutil.TlsOpts{
					ClusterName: cluster.Name,
					AltNames: []string{
						"*." + cluster.Name + "." + f.Namespace + ".svc",
						"*." + cluster.Name + ".example.com",
					},
				}
				ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, tlsOpts)
				defer teardown()

				cluster.Spec.Security.AdminSecret = targetKube.DefaultSecret.Name
				for i := range cluster.Spec.XDCR.RemoteClusters {
					cluster.Spec.XDCR.RemoteClusters[i].AuthenticationSecret = targetKube.DefaultSecret.Name
				}
				cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
					Static: &couchbasev2.StaticTLS{
						Member: &couchbasev2.MemberSecret{
							ServerSecret: ctx.ClusterSecretName,
						},
						OperatorSecret: ctx.OperatorSecretName,
					},
				}
				cluster.ObjectMeta.Name = ctx.ClusterName
				cluster.ObjectMeta.Namespace = f.Namespace

				for i := range cluster.Spec.VolumeClaimTemplates {
					cluster.Spec.VolumeClaimTemplates[i].Spec.StorageClassName = &f.StorageClassName
				}
			}

			// Removing previous deployment if any
			e2eutil.CleanUpCluster(t, targetKube, f.Namespace, f.LogDir, kubeName, t.Name())

			// If we are applying a change or deleting a cluster we first need to create it...
			if command == "apply" || command == "delete" {
				if err := createResources(targetKube, f.Namespace, objects); err != nil {
					e2eutil.Die(t, err)
				}
			}

			// Patch the cluster specification
			if test.mutations != nil {
				if err := patchResources(objects, test.mutations); err != nil {
					t.Fatal(err)
				}
			}

			// Execute the main test, update the new resource for verification.
			switch command {
			case "create":
				err = createResources(targetKube, f.Namespace, objects)
			case "apply":
				err = updateResources(targetKube, objects)
			case "delete":
				err = deleteResources(targetKube, objects)
			}

			// Handle successes when it shoud have failed.
			// Also if any validations are defined then ensure the updated CR matches.
			if err == nil {
				if test.shouldFail {
					t.Fatal("test unexpectedly succeeded")
				}
				if test.validations != nil {
					if err := patchResources(objects, test.validations); err != nil {
						t.Fatal(err)
					}
				}
			}

			// When it did fail, handle when it shouldn't have done so.
			// If there were any errors to expect look for them.
			if err != nil {
				if !test.shouldFail {
					e2eutil.Die(t, fmt.Errorf("test unexpectedly failed: %v", err))
				}
				if len(test.expectedErrors) > 0 {
					for _, message := range test.expectedErrors {
						if !strings.Contains(err.Error(), message) || message == "" {
							e2eutil.Die(t, fmt.Errorf("expected message: %+v \n returned message: %v \n", message, err))
						}
					}
				}
			}
		})
	}

	// Removing deployment if any
	if !f.SkipTeardown {
		e2eutil.CleanUpCluster(t, targetKube, f.Namespace, f.LogDir, kubeName, t.Name())
	}
}

func TestValidationCreate(t *testing.T) {
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
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Logging/LogRetentionTime", "1"+timeUnit)},
		}
		testDefs = append(testDefs, testDefCase)
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationCreate(t *testing.T) {
	oneMinute, _ := time.ParseDuration("1m")
	tombstonePurgeIntervalUnderflow := metav1.Duration{
		Duration: oneMinute,
	}

	oneHundredDays, _ := time.ParseDuration("2400h")
	tombstonePurgeIntervalOverflow := metav1.Duration{
		Duration: oneHundredDays,
	}

	one := 1
	oneHundredAndOne := 101

	testDefs := []testDef{
		// Spec.ExposedFeatures list validation
		{
			name:           "TestValidateExposedFeaturesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/ExposedFeatures", couchbasev2.ExposedFeatureList{couchbasev2.FeatureAdmin, "cleint", couchbasev2.FeatureXDCR})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.exposedFeatures in body should match '^admin|xdcr|client$'"},
		},
		{
			name:           "TestValidateExposedFeaturesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/ExposedFeatures", couchbasev2.ExposedFeatureList{couchbasev2.FeatureAdmin, couchbasev2.FeatureClient, couchbasev2.FeatureXDCR, couchbasev2.FeatureAdmin})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.exposedFeatures in body shouldn't contain duplicates"},
		},

		// Bucket validation test
		{
			name:       "TestValidateBucketNameInvalid",
			mutations:  patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Name", "invalid!bucket!name")},
			shouldFail: true,
		},
		{
			name:           "TestValidateBucketConflictResolutionRequiredForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Remove("/Spec/ConflictResolution")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body should match '^seqno|lww$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyRequiredForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Remove("/Spec/EvictionPolicy")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^valueOnly|fullEviction$'"},
		},
		{
			name:           "TestValidateBucketIOPriorityRequiredForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Remove("/Spec/IoPriority")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority in body should match '^high|low$'"},
		},
		{
			name:           "TestValidateBucketConflictResolutionRequiredForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Remove("/Spec/ConflictResolution")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body should match '^seqno|lww$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyRequiredForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Remove("/Spec/EvictionPolicy")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},
		{
			name:           "TestValidateBucketIOPriorityRequiredForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Remove("/Spec/IoPriority")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority in body should match '^high|low$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral_1",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "valueOnly")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral_2",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "fullEviction")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidInvalidForCouchbase_1",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "noEviction")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^valueOnly|fullEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidInvalidForCouchbase_2",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "nruEviction")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^valueOnly|fullEviction$'"},
		},
		// Hard problem.
		//		{
		//			name:       "TestValidateBucketNameUnique",
		//			mutations:  patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/ObjectMeta/Name", "bucket3")},
		//			shouldFail: true,
		//		},
		{
			name:           "TestValidateBucketQuotaOverflow",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/MemoryQuota", 601)},
			shouldFail:     true,
			expectedErrors: []string{"bucket memory allocation (1001) exceeds data service quota (600) on cluster cluster"},
		},
		{
			name:           "TestValidateBucketCompressionModeInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/CompressionMode", cbmgr.CompressionMode("invalid"))},
			shouldFail:     true,
			expectedErrors: []string{"spec.compressionMode in body should match '^off|passive|active$'"},
		},
		{
			name:           "TestValidateBucketCompressionModeInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/Spec/CompressionMode", cbmgr.CompressionMode("invalid"))},
			shouldFail:     true,
			expectedErrors: []string{"spec.compressionMode in body should match '^off|passive|active$'"},
		},

		// Server settings validation
		{
			name:           "TestValidateServerServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.Service("indxe"), couchbasev2.QueryService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.services in body should match '^data|index|query|search|eventing|analytics$'"},
		},
		{
			name:           "TestValidateServerNameUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Name", "data_only")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.name in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.IndexService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateServerServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body shouldn't contain duplicates"},
		},
		{
			name:           "TestServerSizeRangeInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Size", -2)},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.size in body should be greater than or equal to 1"},
		},
		{
			name:           "TestNoDataService",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers", []couchbasev2.ServerConfig{{Name: "test", Size: 1, Services: couchbasev2.ServiceList{couchbasev2.IndexService}}})},
			shouldFail:     true,
			expectedErrors: []string{`at least one "data" service in spec.servers[*].services is required`},
		},

		// ServerGroups list validation
		{
			name:           "TestValidateServerGroupsUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-1"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateServerServerGroupsUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/2/ServerGroups", []string{"us-east-1a", "us-east-1b", "us-east-1a"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body shouldn't contain duplicates"},
		},

		// Admin console services field validation
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.Service("indxe"), couchbasev2.QueryService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body should match '^data|index|query|search|eventing|analytics$'"},
		},

		// Persistent volume claim cases
		{
			name:           "TestValidateVolumeClaimTemplatesStorageClassMustExist",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/StorageClassName", &unavailableStorageClass)},
			shouldFail:     true,
			expectedErrors: []string{"storage class unavailableStorageClass must exist"},
		},
		{
			name:       "TestValidateVolumeClaimTemplateMustExist",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/ObjectMeta/Name", "InvalidVolumeClaim")},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Pod/VolumeMounts/LogsClaim", "couchbase")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/3/Pod/VolumeMounts/DefaultClaim", "couchbase-log-pv")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[3].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "TestValidateDefaultVolumeMountRequiredForStatefulServices",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/3/Services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.EventingService, couchbasev2.AnalyticsService})},
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "TestValidateLogRetentionTimeInvalidPattern",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Logging/LogRetentionTime", "1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.logging.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "TestValidateLogRetentionCountInvalidRange",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Logging/LogRetentionCount", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.logging.logRetentionCount in body should be greater than or equal to 0"},
		},
		// Missing referenced resources
		{
			name:           "TestValidateAuthSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Security/AdminSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.security.adminSecret must exist"},
		},
		{
			name:           "TestValidateTLSServerSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/TLS/Static/Member/ServerSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.static.member.serverSecret must exist"},
		},
		{
			name:           "TestValidateTLSOperatorSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/TLS/Static/OperatorSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.static.operatorSecret must exist"},
		},
		// Missing Network configuration
		{
			name: "TestValidateDNSReqiredWithPublicAdminConsoleService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/Spec/Networking/AdminConsoleServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/Spec/Networking/DNS")},
			shouldFail:     true,
			expectedErrors: []string{`spec.dns in body is required`},
		},
		{
			name: "TestValidateDNSReqiredWithPublicExposedFeatureService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/Spec/Networking/ExposedFeatureServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/Spec/Networking/DNS")},
			shouldFail:     true,
			expectedErrors: []string{`spec.dns in body is required`},
		},
		{
			name: "TestValidateTLSRequiredWithPublicAdminConsoleService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/Spec/Networking/AdminConsoleServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/Spec/Networking/TLS")},
			shouldFail:     true,
			expectedErrors: []string{`spec.tls in body is required`},
		},
		{
			name: "TestValidateTLSRequiredWithPublicExposedFeatureService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/Spec/Networking/ExposedFeatureServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/Spec/Networking/TLS")},
			shouldFail:     true,
			expectedErrors: []string{`spec.tls in body is required`},
		},
		{
			name:           "TestValidateMissingDNSSubjectAltName",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/DNS/Domain", "acme.com")},
			shouldFail:     true,
			expectedErrors: []string{`certificate is valid for *.cluster.default.svc, *.cluster.example.com, not verify.cluster.acme.com`},
		},
		// Auto compaction
		{
			name:           "TestValidateAutoCompactionMinimum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/DatabaseFragmentationThreshold/Percent", &one)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.databaseFragmentationThreshold.percent in body should be greater than or equal to 2`},
		},
		{
			name:           "TestValidateAutoCompactionMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/DatabaseFragmentationThreshold/Percent", &oneHundredAndOne)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.databaseFragmentationThreshold.percent in body should be less than or equal to 100`},
		},
		{
			name:           "TestValidateStartTimeIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/TimeWindow/Start", "26:00")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.timeWindow.start in body should match '^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$'`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMinimum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/TombstonePurgeInterval", tombstonePurgeIntervalUnderflow)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.tombstonePurgeInterval in body should be greater than or equal to 1h`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/TombstonePurgeInterval", tombstonePurgeIntervalOverflow)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.tombstonePurgeInterval in body should be less than or equal to 60d`},
		},
		// XDCR
		{
			name:           "TestValidateXDCRUUIDInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/XDCR/RemoteClusters/0/UUID", "cat")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.uuid in body should match '^[0-9a-f]{32}$'`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidName",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/XDCR/RemoteClusters/0/Hostname", "illegal_dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.hostname in body should match '^[0-9a-zA-Z\-\.]+(:\d+)?$'`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/XDCR/RemoteClusters/0/Hostname", "starsky-and-hutch.tv:huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.hostname in body should match '^[0-9a-zA-Z\-\.]+(:\d+)?$'`},
		},
		{
			name:           "TestValidateXDCRAuthenticationSecretExists",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/XDCR/RemoteClusters/0/AuthenticationSecret", "huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`secret huggy-bear referenced by spec.xdcr.remoteClusters[0].authenticationSecret must exist`},
		},
		{
			name:           "TestValidateXDCRReplicationBucketExists",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/Spec/Bucket", "huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`bucket huggy-bear referenced by spec.bucket in couchbasereplications.couchbase.com/replication0 must exist`},
		},
		{
			name:           "TestValidateXDCRReplicationCompressionTypeInvalid",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/Spec/CompressionType", couchbasev2.CompressionType("huggy-bear"))},
			shouldFail:     true,
			expectedErrors: []string{`spec.compressionType in body should match '^none|auto|snappy$'`},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/1/Pod/VolumeMounts/"+mntField, "invalidClaim")},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/1/Services", fieldValueToUse)},
			shouldFail:     true,
			expectedErrors: []string{string(serviceToSkip) + " in spec.servers[1].services is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV only defined,but one of stateful service is included
	for _, statefulService := range constants.StatefulCbServiceList {
		testCase := testDef{
			name:           "TestValidateDefaultVolumeMountRequiredForStatefulService_" + string(statefulService),
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/3/Services", couchbasev2.ServiceList{couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.EventingService, statefulService})},
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
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/Spec/Servers/0/Pod/VolumeMounts/"+claimField, "couchbase").
				Remove("/Spec/Servers/0/Pod/VolumeMounts/DefaultClaim")},
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[0].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims is an array value
	testCase := testDef{
		name: "TestValidateDefaultVolumeMountRequiredForAnalyticsClaim",
		mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
			Replace("/Spec/Servers/0/Pod/VolumeMounts/AnalyticsClaims", []string{"couchbase"}).
			Remove("/Spec/Servers/0/Pod/VolumeMounts/DefaultClaim")},
		shouldFail:     true,
		expectedErrors: []string{"default in spec.servers[0].pod.volumeMounts is required"},
	}
	testDefs = append(testDefs, testCase)

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestValidationDefaultCreate(t *testing.T) {
	defaultAutoCompationThresholdPercent := 30

	threeDays, _ := time.ParseDuration("72h")
	defaultTombstonePurgeInterval := metav1.Duration{
		Duration: threeDays,
	}

	testDefs := []testDef{
		{
			name:        "TestValidateIndexServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexServiceMemQuota", uint64(0))},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexServiceMemQuota", uint64(256))},
		},
		{
			name:        "TestValidateSearchServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/SearchServiceMemQuota", uint64(0))},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/SearchServiceMemQuota", uint64(256))},
		},
		{
			name:        "TestValidateEventingServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/EventingServiceMemQuota")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/EventingServiceMemQuota", uint64(256))},
		},
		{
			name:        "TestValidateAnalyticsServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AnalyticsServiceMemQuota")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AnalyticsServiceMemQuota", uint64(1024))},
		},
		{
			name:        "TestValidateIndexStorageSetting",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/IndexStorageSetting")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexStorageSetting", "memory_optimized")},
		},
		{
			name:        "TestValidateAutoFailoverTimeoutDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverTimeout")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverTimeout", uint64(120))},
		},
		{
			name:        "TestValidateAutoFailoverMaxCountDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverMaxCount")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverMaxCount", uint64(3))},
		},
		{
			name:        "TestValidateAutoFailoverOnDataDiskIssuesPeriodDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverOnDataDiskIssuesTimePeriod")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverOnDataDiskIssuesTimePeriod", uint64(120))},
		},
		{
			name:        "TestValidateAdminConsoleServiceTypeDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/Networking/AdminConsoleServiceType")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/Networking/AdminConsoleServiceType", corev1.ServiceTypeNodePort)},
		},
		{
			name:        "TestValidateExposedFeatureServiceTypeDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/Networking/ExposedFeatureServiceType")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/Networking/ExposedFeatureServiceType", corev1.ServiceTypeNodePort)},
		},
		{
			name:        "TestValidateBucketCompressionModeDefaultForCouchbase",
			mutations:   patchMap{"bucket0": jsonpatch.NewPatchSet().Remove("/Spec/CompressionMode")},
			validations: patchMap{"bucket0": jsonpatch.NewPatchSet().Test("/Spec/CompressionMode", cbmgr.CompressionModePassive)},
		},
		{
			name:        "TestValidateBucketCompressionModeDefaultForEphemeral",
			mutations:   patchMap{"bucket3": jsonpatch.NewPatchSet().Remove("/Spec/CompressionMode")},
			validations: patchMap{"bucket3": jsonpatch.NewPatchSet().Test("/Spec/CompressionMode", cbmgr.CompressionModePassive)},
		},
		{
			name:        "TestValidateFSGroupDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/SecurityContext/FSGroup")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/SecurityContext/FSGroup", &defaultFSGroup)},
		},
		{
			name:        "TestAutoCompactionDatabaseThresholdPercentDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoCompaction/DatabaseFragmentationThreshold/Percent")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoCompaction/DatabaseFragmentationThreshold/Percent", &defaultAutoCompationThresholdPercent)},
		},
		{
			name:        "TestAutoCompactionViewThresholdPercentDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoCompaction/ViewFragmentationThreshold/Percent")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoCompaction/ViewFragmentationThreshold/Percent", &defaultAutoCompationThresholdPercent)},
		},
		{
			name:        "TestAutoCompactionTombstonePurgeIntervalDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoCompaction/TombstonePurgeInterval")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoCompaction/TombstonePurgeInterval", defaultTombstonePurgeInterval)},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationDefaultCreate(t *testing.T) {
	testDefs := []testDef{
		{
			// DataServiceMemQuota will get mutated to the default of 256, but we have
			// 500 worth of buckets defined.
			name:           "TestValidateDataServiceMemoryQuotaDefault",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/DataServiceMemQuota", uint64(0))},
			shouldFail:     true,
			expectedErrors: []string{"bucket memory allocation (500) exceeds data service quota (256) on cluster cluster"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationConstraintsCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.Service("xxxxx")})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body should match '^data|index|query|search|eventing|analytics$'"},
		},
		{
			name:           "TestValidateBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/IoPriority", "lighow")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority in body should match '^high|low$'"},
		},
		{
			name:           "TestValidateBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/ConflictResolution", "selwwno")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body should match '^seqno|lww$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "valueOnly")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "nruEviction")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^valueOnly|fullEviction$'"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

// CRD apply tests
func TestValidationApply(t *testing.T) {
	supportedTimeUnits := []string{"ns", "us", "ms", "s", "m", "h"}

	testDefs := []testDef{
		{
			name: "TestValidateDefault",
		},
		{
			name:        "TestValidateApplyBucketName",
			mutations:   patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/ObjectMeta/Name", "newNameForBucket")},
			validations: patchMap{"bucket0": jsonpatch.NewPatchSet().Test("/ObjectMeta/Name", "newNameForBucket")},
			shouldFail:  true,
		},
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name:      "TestValidateApplyLogRestentionTime_" + timeUnit,
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Logging/LogRetentionTime", "100"+timeUnit)},
		}
		testDefs = append(testDefs, testDefCase)
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateServerServicesImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Name", "data_only")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		// Verify for Default/Log volume mounts mutual exclusion
		{
			name:           "TestValidateVolumeMountsImmutable_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Pod/VolumeMounts/LogsClaim", "couchbase")},
			shouldFail:     true,
			expectedErrors: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateVolumeMountsImmutable_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/3/Pod/VolumeMounts/DefaultClaim", "couchbase-log-pv")},
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateVolumeMountsImmutable_3",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/2/Pod/VolumeMounts/LogsClaim", "couchbase-log-pv")},
			shouldFail:     true,
			expectedErrors: []string{"logs in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "TestValidateApplyLogRetentionTimeInvalidPattern",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Logging/LogRetentionTime", "1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.logging.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "TestValidateApplyLogRetentionCountInvalidRange",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Logging/LogRetentionCount", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.logging.logRetentionCount in body should be greater than or equal to 0"},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/1/Services", fieldValueToUse)},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/3/Services", fieldValueToUse)},
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
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/Spec/Servers/0/Pod/VolumeMounts/"+mntField, "couchbase").
				Remove("/Spec/Servers/0/Pod/VolumeMounts/DefaultClaim")},
			shouldFail:     true,
			expectedErrors: []string{mntName + " in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}
	// AnalyticsClaims in an array parameter
	testCase := testDef{
		name:           "TestValidateApplyServerVolumeMountsImmutable_analytics",
		mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Pod/VolumeMounts/AnalyticsClaims", []string{"couchbase"}).Remove("/Spec/Servers/0/Pod/VolumeMounts/DefaultClaim")},
		shouldFail:     true,
		expectedErrors: []string{"analytics in spec.servers[*].Pod.VolumeMounts cannot be updated"},
	}
	testDefs = append(testDefs, testCase)

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestValidationDefaultApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:        "TestValidateApplyIndexServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/IndexServiceMemQuota")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/IndexServiceMemQuota", uint64(256))},
		},
		{
			name:        "TestValidateApplySearchServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/SearchServiceMemQuota")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/SearchServiceMemQuota", uint64(256))},
		},
		{
			name:        "TestValidateApplyAutoFailoverTimeoutDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/Spec/ClusterSettings/AutoFailoverTimeout")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/Spec/ClusterSettings/AutoFailoverTimeout", uint64(120))},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationConstraintsApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateApplyAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateApplyAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.Service("xxxxx")})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body should match '^data|index|query|search|eventing|analytics$'"},
		},
		{
			name:           "TestValidateApplyBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/IoPriority", "lighow")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority in body should match '^high|low$'"},
		},
		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "valueOnly")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},

		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/EvictionPolicy", "nruEviction")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^valueOnly|fullEviction$'"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationImmutableApply(t *testing.T) {
	testDefs := []testDef{
		// Bucket spec updation
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForEphemeral",
			mutations:      patchMap{"bucket4": jsonpatch.NewPatchSet().Replace("/Spec/ConflictResolution", "seqno")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/ConflictResolution", "selwwno")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body should match '^seqno|lww$'"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/Spec/ConflictResolution", "lww")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body cannot be updated"},
		},
		// Servers service update
		{
			name:           "TestValidateApplyServerServicesImmutable_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyServerServicesImmutable_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/0/Services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyAntiAffinityImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/AntiAffinity", true)},
			shouldFail:     true,
			expectedErrors: []string{"spec.antiAffinity in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyAuthSecretImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Security/AdminSecret", "auth-secret-update")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authSecret in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyIndexStorageSettingsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/IndexStorageSetting", "plasma")},
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.indexStorageSetting in body cannot be updated"},
		},
		// Server groups updates
		{
			name:           "TestValidateApplyServerGroupsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-2"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyServersServerGroupsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/2/ServerGroups", []string{"us-east-1a", "us-east-1b", "us-east-1c"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body cannot be updated"},
		},
		// Persistent volume spec updation
		{
			name:           "TestValidateApplyVolumeMountsImmutable_data",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/1/Pod/VolumeMounts/DataClaim", "newVolumeMount")},
			shouldFail:     true,
			expectedErrors: []string{"data in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeMountsImmutable_default",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/1/Pod/VolumeMounts/DefaultClaim", "newVolumeMount")},
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeMountsImmutable_index",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/1/Pod/VolumeMounts/IndexClaim", "newVolumeMount")},
			shouldFail:     true,
			expectedErrors: []string{"index in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeMountsImmutable_analytics",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/Servers/1/Pod/VolumeMounts/AnalyticsClaims/0", "newVolumeMount")},
			shouldFail:     true,
			expectedErrors: []string{"analytics in spec.servers[*].Pod.VolumeMounts cannot be updated"},
		},
		{
			name:           "TestValidateApplyVolumeTemplatesStorageClassImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/StorageClassName", &unavailableStorageClass)},
			shouldFail:     true,
			expectedErrors: []string{`"storageClassName" in spec.volumeClaimTemplates[*] cannot be updated`},
		},
		{
			name:           "TestValidateApplyVolumeTemplatesResourcesRequestsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/Resources/Requests", corev1.ResourceList{corev1.ResourceStorage: *apiresource.NewScaledQuantity(10, 30)})},
			shouldFail:     true,
			expectedErrors: []string{`"storage" in spec.volumeClaimTemplates[*].resources.requests cannot be updated`},
		},
		{
			name:           "TestValidateApplyVolumeTemplatesResourcesLimitsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/VolumeClaimTemplates/0/Spec/Resources/Limits", corev1.ResourceList{corev1.ResourceStorage: *apiresource.NewScaledQuantity(10, 30)})},
			shouldFail:     true,
			expectedErrors: []string{`"storage" in spec.volumeClaimTemplates[*].resources.limits cannot be updated`},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

// CRD delete tests
func TestValidationDelete(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateDelete",
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "delete")
}

func TestNegValidationDelete(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateDeleteRenamedCluster",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/ObjectMeta/Name", "cb-example1")},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/Spec/ServerGroups", []string{"InvalidGroup-1", "InvalidGroup-2"})},
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
