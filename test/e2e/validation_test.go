package e2e

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	other_jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

var (
	unavailableStorageClass       = "unavailableStorageClass"
	defaultFSGroup          int64 = 1000
)

// Resources are stored in a list in order to maintain ordering.  Do not try to use a map
// or things will happen in a random order.
type resourceList [][]byte

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
		j, err := yaml.YAMLToJSON([]byte(part))
		if err != nil {
			return nil, err
		}
		res = append(res, j)
	}

	return res, nil
}

// getResourceMeta takes raw JSON and returns the resource namespace and name.
func getResourceMeta(resource []byte) (string, string, error) {
	object := &unstructured.Unstructured{}
	if err := json.Unmarshal(resource, object); err != nil {
		return "", "", err
	}
	return object.GetNamespace(), object.GetName(), nil
}

var (
	// restMapper is cached because the calls to create it are very expensive.
	restMapper meta.RESTMapper
)

// getResource takes raw JSON and returns the resource type (used by the raw API),
// the API version and the Kind (POST and PUT methods actually strip this from
// the status response so we have to replopulate it).
func getResource(k8s *types.Cluster, object *unstructured.Unstructured) (*schema.GroupVersionResource, error) {
	if restMapper == nil {
		discoveryClient, err := discovery.NewDiscoveryClientForConfig(k8s.Config)
		if err != nil {
			return nil, err
		}
		groupresources, err := restmapper.GetAPIGroupResources(discoveryClient)
		if err != nil {
			return nil, err
		}
		restMapper = restmapper.NewDiscoveryRESTMapper(groupresources)
	}

	gvk := object.GroupVersionKind()
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	return &mapping.Resource, nil
}

// createResources iterates over every resource and creates them in the requested namespace.
func createResources(k8s *types.Cluster, namespace string, resources resourceList) error {
	for i, resource := range resources {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(resource, object); err != nil {
			return err
		}

		groupVersion, err := getResource(k8s, object)
		if err != nil {
			return err
		}

		client, err := dynamic.NewForConfig(k8s.Config)
		if err != nil {
			return err
		}
		res, err := client.Resource(*groupVersion).Namespace(framework.Global.Namespace).Create(object, metav1.CreateOptions{})
		if err != nil {
			return err
		}

		// Stupid API
		res.SetAPIVersion(object.GetAPIVersion())
		res.SetKind(object.GetKind())

		raw, err := json.Marshal(res)
		if err != nil {
			return err
		}

		resources[i] = raw
	}
	return nil
}

// updateResources updates all defined resources.
func updateResources(k8s *types.Cluster, resources resourceList) error {
	for i, resource := range resources {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(resource, object); err != nil {
			return err
		}

		groupVersion, err := getResource(k8s, object)
		if err != nil {
			return err
		}

		client, err := dynamic.NewForConfig(k8s.Config)
		if err != nil {
			return err
		}
		res, err := client.Resource(*groupVersion).Namespace(object.GetNamespace()).Update(object, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		// Stupid API
		res.SetAPIVersion(object.GetAPIVersion())
		res.SetKind(object.GetKind())

		raw, err := json.Marshal(res)
		if err != nil {
			return err
		}
		resources[i] = raw
	}
	return nil
}

// deleteResources deletes all defined resources.
func deleteResources(k8s *types.Cluster, resources resourceList) error {
	for _, resource := range resources {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(resource, object); err != nil {
			return err
		}

		groupVersion, err := getResource(k8s, object)
		if err != nil {
			return err
		}

		client, err := dynamic.NewForConfig(k8s.Config)
		if err != nil {
			return err
		}
		if err := client.Resource(*groupVersion).Namespace(object.GetNamespace()).Delete(object.GetName(), metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}
	return nil
}

// patchResources applies JSON patches to all defined resources.
func patchResources(k8s *types.Cluster, resources resourceList, patches patchMap) error {
	for i, resource := range resources {
		_, name, err := getResourceMeta(resource)
		if err != nil {
			return err
		}

		patchset, ok := patches[name]
		if !ok {
			continue
		}

		patchJSON, err := json.Marshal(patchset.Patches())
		if err != nil {
			return err
		}

		patch, err := other_jsonpatch.DecodePatch(patchJSON)
		if err != nil {
			return err
		}

		resource, err := patch.Apply(resource)
		if err != nil {
			return err
		}

		resources[i] = resource
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
				e2eutil.Die(t, err)
			}

			// Delete anything we created.
			defer func() {
				_ = deleteResources(targetKube, objects)
			}()

			for i, resource := range objects {
				object := &unstructured.Unstructured{}
				if err := json.Unmarshal(resource, object); err != nil {
					e2eutil.Die(t, err)
				}

				if object.GetKind() != "CouchbaseCluster" {
					continue
				}

				// Do static environment configuration.
				object.SetNamespace(f.Namespace)

				// Do dynamic environment configuration.
				if err := unstructured.SetNestedField(object.Object, targetKube.DefaultSecret.Name, "spec", "security", "adminSecret"); err != nil {
					e2eutil.Die(t, err)
				}

				// Do dynamic TLS configuration.
				tlsOpts := &e2eutil.TLSOpts{
					ClusterName: object.GetName(),
					AltNames: []string{
						"*." + object.GetName() + "." + f.Namespace + ".svc",
						"*." + object.GetName() + ".example.com",
					},
				}
				ctx, teardown := e2eutil.MustInitClusterTLS(t, targetKube, f.Namespace, tlsOpts)
				defer teardown()

				if err := unstructured.SetNestedField(object.Object, ctx.ClusterSecretName, "spec", "networking", "tls", "static", "serverSecret"); err != nil {
					e2eutil.Die(t, err)
				}
				if err := unstructured.SetNestedField(object.Object, ctx.OperatorSecretName, "spec", "networking", "tls", "static", "operatorSecret"); err != nil {
					e2eutil.Die(t, err)
				}

				// Do XDCR configuration.
				remoteClusters, found, err := unstructured.NestedSlice(object.Object, "spec", "xdcr", "remoteClusters")
				if !found || err != nil {
					e2eutil.Die(t, err)
				}
				for _, remoteCluster := range remoteClusters {
					rc, ok := remoteCluster.(map[string]interface{})
					if !ok {
						e2eutil.Die(t, fmt.Errorf("unexpected data type"))
					}
					rc["authenticationSecret"] = targetKube.DefaultSecret.Name
				}
				if err := unstructured.SetNestedField(object.Object, remoteClusters, "spec", "xdcr", "remoteClusters"); err != nil {
					e2eutil.Die(t, err)
				}

				// Do PVC configuration.
				pvcTemplates, found, err := unstructured.NestedSlice(object.Object, "spec", "volumeClaimTemplates")
				if !found || err != nil {
					e2eutil.Die(t, err)
				}
				for _, pvcTemplate := range pvcTemplates {
					pvct, ok := pvcTemplate.(map[string]interface{})
					if !ok {
						e2eutil.Die(t, fmt.Errorf("unexpected data type"))
					}
					if err := unstructured.SetNestedField(pvct, f.StorageClassName, "spec", "storageClassName"); err != nil {
						e2eutil.Die(t, err)
					}
				}
				if err := unstructured.SetNestedField(object.Object, pvcTemplates, "spec", "volumeClaimTemplates"); err != nil {
					e2eutil.Die(t, err)
				}

				// Turn back into JSON.
				raw, err := json.Marshal(object)
				if err != nil {
					e2eutil.Die(t, err)
				}
				objects[i] = raw
			}

			// If we are applying a change or deleting a cluster we first need to create it...
			if command == "apply" {
				if err := createResources(targetKube, f.Namespace, objects); err != nil {
					e2eutil.Die(t, err)
				}
			}

			// Patch the cluster specification
			if test.mutations != nil {
				if err := patchResources(targetKube, objects, test.mutations); err != nil {
					e2eutil.Die(t, err)
				}
			}

			// Execute the main test, update the new resource for verification.
			switch command {
			case "create":
				err = createResources(targetKube, f.Namespace, objects)
			case "apply":
				err = updateResources(targetKube, objects)
			}

			// Handle successes when it shoud have failed.
			// Also if any validations are defined then ensure the updated CR matches.
			if err == nil {
				if test.shouldFail {
					e2eutil.Die(t, fmt.Errorf("test unexpectedly succeeded"))
				}
				if test.validations != nil {
					if err := patchResources(targetKube, objects, test.validations); err != nil {
						e2eutil.Die(t, err)
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
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionTime", "1"+timeUnit)},
		}
		testDefs = append(testDefs, testDefCase)
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationCreate(t *testing.T) {
	invalidExternalTraficPolicy := corev1.ServiceExternalTrafficPolicyType("Donkey")
	testDefs := []testDef{
		{
			name:           "TestValidateExposedFeaturesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatures", couchbasev2.ExposedFeatureList{couchbasev2.FeatureAdmin, "cleint", couchbasev2.FeatureXDCR})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.exposedFeatures in body should match '^admin|xdcr|client$'"},
		},
		{
			name:           "TestValidateExposedFeaturesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatures", couchbasev2.ExposedFeatureList{couchbasev2.FeatureAdmin, couchbasev2.FeatureClient, couchbasev2.FeatureXDCR, couchbasev2.FeatureAdmin})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.exposedFeatures in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral_1",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral_2",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyFullEviction)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidInvalidForCouchbase_1",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", "noEviction")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^valueOnly|fullEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidInvalidForCouchbase_2",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^valueOnly|fullEviction$'"},
		},
		// Hard problem.
		//		{
		//			name:       "TestValidateBucketNameUnique",
		//			mutations:  patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/name", "bucket3")},
		//			shouldFail: true,
		//		},
		{
			name:           "TestValidateBucketQuotaOverflow",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", "601Mi")},
			shouldFail:     true,
			expectedErrors: []string{"bucket memory allocation (1001Mi) exceeds data service quota (600Mi) on cluster cluster"},
		},
		{
			name:           "TestValidateBucketCompressionModeInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionMode("invalid"))},
			shouldFail:     true,
			expectedErrors: []string{"spec.compressionMode in body should match '^off|passive|active$'"},
		},
		{
			name:           "TestValidateBucketCompressionModeInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionMode("invalid"))},
			shouldFail:     true,
			expectedErrors: []string{"spec.compressionMode in body should match '^off|passive|active$'"},
		},
		{
			name:           "TestValidateServerServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.Service("indxe"), couchbasev2.QueryService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.services in body should match '^data|index|query|search|eventing|analytics$'"},
		},
		{
			name:           "TestValidateServerNameUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/name", "data_only")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.name in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.IndexService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateServerServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body shouldn't contain duplicates"},
		},
		{
			name:           "TestServerSizeRangeInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", -2)},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.size in body should be greater than or equal to 1"},
		},
		{
			name:           "TestNoDataService",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers", []couchbasev2.ServerConfig{{Name: "test", Size: 1, Services: couchbasev2.ServiceList{couchbasev2.IndexService}}})},
			shouldFail:     true,
			expectedErrors: []string{`at least one "data" service in spec.servers[*].services is required`},
		},
		{
			name:           "TestValidateServerGroupsUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/serverGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-1"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateServerServerGroupsUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/2/serverGroups", []string{"us-east-1a", "us-east-1b", "us-east-1a"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.Service("indxe"), couchbasev2.QueryService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body should match '^data|index|query|search|eventing|analytics$'"},
		},
		{
			name:           "TestValidateVolumeClaimTemplatesStorageClassMustExist",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/storageClassName", &unavailableStorageClass)},
			shouldFail:     true,
			expectedErrors: []string{"storage class unavailableStorageClass must exist"},
		},
		{
			name:       "TestValidateVolumeClaimTemplateMustExist",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/metadata/name", "InvalidVolumeClaim")},
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
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/pod/volumeMounts/logs", "couchbase")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/pod/volumeMounts/default", "couchbase-log-pv")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[3].pod.volumeMounts.default is a forbidden property"},
		},
		{
			name:           "TestValidateDefaultVolumeMountRequiredForStatefulServices",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.EventingService, couchbasev2.AnalyticsService})},
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		},
		{
			name:           "TestValidateLogRetentionTimeInvalidPattern",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionTime", "1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.logging.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "TestValidateLogRetentionCountInvalidRange",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionCount", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.logging.logRetentionCount in body should be greater than or equal to 0"},
		},
		{
			name:           "TestValidateAuthSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/security/adminSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.security.adminSecret must exist"},
		},
		{
			name:           "TestValidateTLSServerSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/static/serverSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.static.serverSecret must exist"},
		},
		{
			name:           "TestValidateTLSOperatorSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/static/operatorSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.static.operatorSecret must exist"},
		},
		{
			name:           "TestValidateTLSClientCertificatePolicyInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/clientCertificatePolicy", "invalid")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePolicy in body should match '^enable|mandatory$'`},
		},
		{
			name:           "TestValidateTLSClientCertificatePathInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/clientCertificatePaths/0/path", "invalid")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths.path in body should match '^subject\.cn|san\.uri|san\.dnsname|san\.email$'`},
		},
		{
			name:           "TestValidateTLSClientCertificatePathRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/networking/tls/clientCertificatePaths/0/path")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths.path in body is required`},
		},
		{
			name:           "TestValidateTLSClientCertificateNoPaths",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/networking/tls/clientCertificatePaths")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths should have at least 1 items`},
		},
		{
			name: "TestValidateDNSReqiredWithPublicAdminConsoleService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/adminConsoleServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.dns in body is required`},
		},
		{
			name: "TestValidateDNSReqiredWithPublicExposedFeatureService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/exposedFeatureServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.dns in body is required`},
		},
		{
			name: "TestValidateTLSRequiredWithPublicAdminConsoleService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/adminConsoleServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/tls")},
			shouldFail:     true,
			expectedErrors: []string{`spec.tls in body is required`},
		},
		{
			name: "TestValidateTLSRequiredWithPublicExposedFeatureService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/exposedFeatureServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/tls")},
			shouldFail:     true,
			expectedErrors: []string{`spec.tls in body is required`},
		},
		{
			name:           "TestValidateMissingDNSSubjectAltName",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/dns/domain", "acme.com")},
			shouldFail:     true,
			expectedErrors: []string{`certificate is valid for *.cluster.default.svc, *.cluster.example.com, not verify.cluster.acme.com`},
		},
		{
			name:           "TestValidateAutoCompactionMinimum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", 1)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.databaseFragmentationThreshold.percent in body should be greater than or equal to 2`},
		},
		{
			name:           "TestValidateAutoCompactionMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", 101)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.databaseFragmentationThreshold.percent in body should be less than or equal to 100`},
		},
		{
			name:           "TestValidateStartTimeIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/timeWindow/start", "26:00")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.timeWindow.start in body should match '^(2[0-3]|[01]?[0-9]):([0-5]?[0-9])$'`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMinimum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/tombstonePurgeInterval", "1m")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.tombstonePurgeInterval in body should be greater than or equal to 1h`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/tombstonePurgeInterval", "2400h")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.tombstonePurgeInterval in body should be less than or equal to 60d`},
		},
		{
			name:           "TestValidateXDCRUUIDInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/uuid", "cat")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.uuid in body should match '^[0-9a-f]{32}$'`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidName",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "illegal_dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.hostname in body should match '^[0-9a-zA-Z\-\.]+(:\d+)?$'`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "starsky-and-hutch.tv:huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.hostname in body should match '^[0-9a-zA-Z\-\.]+(:\d+)?$'`},
		},
		{
			name:           "TestValidateXDCRReplicationBucketExists",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/bucket", "huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`bucket huggy-bear referenced by spec.bucket in couchbasereplications.couchbase.com/replication0 must exist: bucket huggy-bear not found`},
		},
		{
			name:           "TestValidateXDCRReplicationBucketTypeInvalid",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/bucket", "bucket2")},
			shouldFail:     true,
			expectedErrors: []string{`bucket bucket2 referenced by spec.bucket in couchbasereplications.couchbase.com/replication0 must exist: memcached bucket bucket2 cannot be replicated`},
		},
		{
			name:           "TestValidateXDCRReplicationCompressionTypeInvalid",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/compressionType", couchbasev2.CompressionType("huggy-bear"))},
			shouldFail:     true,
			expectedErrors: []string{`spec.compressionType in body should match '^None|Auto|Snappy$'`},
		},
		{
			name:           "TestValidateXDCRTLSSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/tls/secret", "huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`xdcr tls secret huggy-bear for remote cluster starsky must exist`},
		},
		{
			name:           "TestValidateXDCRTLSCAMissing",
			mutations:      patchMap{"xdcr-tls-secret": jsonpatch.NewPatchSet().Remove("/data/ca")},
			shouldFail:     true,
			expectedErrors: []string{`xdcr tls secret xdcr-tls-secret for remote cluster starsky must contain key 'ca'`},
		},
		{
			name:           "TestValidateExposedFeatureTrafficPolicyInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatureTrafficPolicy", &invalidExternalTraficPolicy)},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.exposedFeatureTrafficPolicy in body should match '^Cluster|Local$'`},
		},
		{
			name:           "TestValidateLoadBalancerSourceRanges",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/networking/loadBalancerSourceRanges/-", "192.168.0.1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.loadBalancerSourceRanges in body should match '^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/\d{1,2}$'`},
		},
		{
			name:           "TestValidateMonitoringInvalidImage",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/monitoring/prometheus/image", "mr-tickle")},
			shouldFail:     true,
			expectedErrors: []string{`spec.monitoring.prometheus.image in body should match '^[\w_\-/]+:([\w\d]+-)?\d+\.\d+.\d+(-[\w\d]+)?$'`},
		},
		{
			name:           "TestValidateDataServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/dataServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.dataServiceMemoryQuota in body should be greater than or equal to 256Mi`},
		},
		{
			name:           "TestValidateIndexServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexServiceMemoryQuota in body should be greater than or equal to 256Mi`},
		},
		{
			name:           "TestValidateSearchServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/searchServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.searchServiceMemoryQuota in body should be greater than or equal to 256Mi`},
		},
		{
			name:           "TestValidateEventingServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/eventingServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.eventingServiceMemoryQuota in body should be greater than or equal to 256Mi`},
		},
		{
			name:           "TestValidateAnalyticsServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/analyticsServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.analyticsServiceMemoryQuota in body should be greater than or equal to 1Gi`},
		},
		{
			name:           "TestValidateAutoFailoverTimeoutUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", "0s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverTimeout in body should be greater than or equal to 5s`},
		},
		{
			name:           "TestValidateAutoFailoverTimeoutOverflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", "2h")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverTimeout in body should be less than or equal to 1h`},
		},
		{
			name:           "TestValidateAutoFailoverOnDataDiskIssuesTimePeriodUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", "0s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be greater than or equal to 5s`},
		},
		{
			name:           "TestValidateAutoFailoverOnDataDiskIssuesTimePeriodOverflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", "2h")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod in body should be less than or equal to 1h`},
		},
		{
			name:           "TestValidateServerServiceRequiredForVaolumeMountData",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/0")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers[1].pod.volumeMounts.data requires the data service to be enabled`},
		},
		{
			name:           "TestValidateServerServiceRequiredForVaolumeMountIndex",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers[1].pod.volumeMounts.index requires the index service to be enabled`},
		},
		{
			name:           "TestValidateServerServiceRequiredForVaolumeMountAnalytics",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/2")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers[1].pod.volumeMounts.analytics requires the analytics service to be enabled`},
		},
	}

	// Cases to validate with invalidClaim name given in Pod.VolumeMounts.[Claims]
	volMountsMap := map[string]string{
		"default": "default",
		"data":    "data",
		"index":   "index",
	}
	for mntField, mntName := range volMountsMap {
		testCase := testDef{
			name:           "TestValidateVolumeClaimTemplateMustExist_" + mntName,
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/1/pod/volumeMounts/"+mntField, "invalidClaim")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[1]." + mntName + " should be one of [couchbase couchbase-log-pv]"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV only defined,but one of stateful service is included
	for _, statefulService := range constants.StatefulCbServiceList {
		testCase := testDef{
			name:           "TestValidateDefaultVolumeMountRequiredForStatefulService_" + string(statefulService),
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/services", couchbasev2.ServiceList{couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.EventingService, statefulService})},
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[3].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases for defining Stateful claims without specifying Default volume mounts
	claimFieldNames := []string{"data", "index"}
	for _, claimField := range claimFieldNames {
		testCase := testDef{
			name: "TestValidateDefaultVolumeMountRequiredForServiceClaim_" + claimField,
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/servers/0/pod/volumeMounts/"+claimField, "couchbase").
				Remove("/spec/servers/0/pod/volumeMounts/default")},
			shouldFail:     true,
			expectedErrors: []string{"default in spec.servers[0].pod.volumeMounts is required"},
		}
		testDefs = append(testDefs, testCase)
	}
	// Analytics is an array value
	testCase := testDef{
		name: "TestValidateDefaultVolumeMountRequiredForAnalytics",
		mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
			Replace("/spec/servers/0/pod/volumeMounts/analytics", []string{"couchbase"}).
			Remove("/spec/servers/0/pod/volumeMounts/default")},
		shouldFail:     true,
		expectedErrors: []string{"default in spec.servers[0].pod.volumeMounts is required"},
	}
	testDefs = append(testDefs, testCase)

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestValidationDefaultCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateClusterDefault",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				// Data cannot be tested as we require at least 500Mi of buckets
				Remove("/spec/cluster/indexServiceMemoryQuota").
				Remove("/spec/cluster/searchServiceMemoryQuota").
				Remove("/spec/cluster/eventingServiceMemoryQuota").
				Remove("/spec/cluster/analyticsServiceMemoryQuota").
				Remove("/spec/cluster/indexStorageSetting").
				Remove("/spec/cluster/autoFailoverTimeout").
				Remove("/spec/cluster/autoFailoverMaxCount").
				Remove("/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod"),
			},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Test("/spec/cluster/indexServiceMemoryQuota", "256Mi").
				Test("/spec/cluster/searchServiceMemoryQuota", "256Mi").
				Test("/spec/cluster/eventingServiceMemoryQuota", "256Mi").
				Test("/spec/cluster/analyticsServiceMemoryQuota", "1Gi").
				Test("/spec/cluster/indexStorageSetting", couchbasev2.CouchbaseClusterIndexStorageSettingMemoryOptimized).
				Test("/spec/cluster/autoFailoverTimeout", "120s").
				Test("/spec/cluster/autoFailoverMaxCount", 3).
				Test("/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", "120s"),
			},
		},
		{
			name:      "TestValidateNetworkingDefault",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/networking")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Test("/spec/networking/adminConsoleServiceType", corev1.ServiceTypeNodePort).
				Test("/spec/networking/exposedFeatureServiceType", corev1.ServiceTypeNodePort),
			},
		},
		{
			name: "TestValidateCouchbaseBucketDefault",
			mutations: patchMap{"bucket0": jsonpatch.NewPatchSet().
				Remove("/spec/memoryQuota").
				Remove("/spec/replicas").
				Remove("/spec/ioPriority").
				Remove("/spec/evictionPolicy").
				Remove("/spec/conflictResolution").
				Remove("/spec/compressionMode"),
			},
			validations: patchMap{"bucket0": jsonpatch.NewPatchSet().
				Test("/spec/memoryQuota", "100Mi").
				Test("/spec/replicas", 1).
				Test("/spec/ioPriority", couchbasev2.CouchbaseBucketIOPriorityLow).
				Test("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly).
				Test("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber).
				Test("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionModePassive),
			},
		},
		{
			name: "TestValidateEphemeralBucketDefault",
			mutations: patchMap{"bucket3": jsonpatch.NewPatchSet().
				Remove("/spec/memoryQuota").
				Remove("/spec/replicas").
				Remove("/spec/ioPriority").
				Remove("/spec/evictionPolicy").
				Remove("/spec/conflictResolution").
				Remove("/spec/compressionMode"),
			},
			validations: patchMap{"bucket3": jsonpatch.NewPatchSet().
				Test("/spec/memoryQuota", "100Mi").
				Test("/spec/replicas", 1).
				Test("/spec/ioPriority", couchbasev2.CouchbaseBucketIOPriorityLow).
				Test("/spec/evictionPolicy", couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNoEviction).
				Test("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber).
				Test("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionModePassive),
			},
		},
		{
			name: "TestValidateMemcachedBucketDefault",
			mutations: patchMap{"bucket2": jsonpatch.NewPatchSet().
				Remove("/spec/memoryQuota"),
			},
			validations: patchMap{"bucket2": jsonpatch.NewPatchSet().
				Test("/spec/memoryQuota", "100Mi"),
			},
		},
		{
			name:        "TestValidateSecurityContextDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/securityContext")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/spec/securityContext/fsGroup", &defaultFSGroup)},
		},
		{
			name:      "TestAutoCompactionDefault",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/autoCompaction")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Test("/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", 30).
				Test("/spec/cluster/autoCompaction/viewFragmentationThreshold/percent", 30).
				Test("/spec/cluster/autoCompaction/tombstonePurgeInterval", "72h"),
			},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationDefaultCreate(t *testing.T) {
	testDefs := []testDef{
		{
			// dataServiceMemoryQuota will get mutated to the default of 256, but we have
			// 500 worth of buckets defined.
			name:           "TestValidateDataServiceMemoryQuotaDefault",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/dataServiceMemoryQuota")},
			shouldFail:     true,
			expectedErrors: []string{"bucket memory allocation (500Mi) exceeds data service quota (256Mi) on cluster cluster"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

func TestNegValidationConstraintsCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.Service("xxxxx")})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body should match '^data|index|query|search|eventing|analytics$'"},
		},
		{
			name:           "TestValidateBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/ioPriority", "lighow")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority in body should match '^high|low$'"},
		},
		{
			name:           "TestValidateBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", "selwwno")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body should match '^seqno|lww$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction)},
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
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name:      "TestValidateApplyLogRestentionTime_" + timeUnit,
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionTime", "100"+timeUnit)},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/name", "data_only")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "TestValidateApplyLogRetentionTimeInvalidPattern",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionTime", "1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.logging.logRetentionTime in body should match '^\d+(ns|us|ms|s|m|h)$'`},
		},
		{
			name:           "TestValidateApplyLogRetentionCountInvalidRange",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionCount", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.logging.logRetentionCount in body should be greater than or equal to 0"},
		},
	}

	// Cases to validate with all volume mounts present in Pod.VolumeMounts but one of the Service missing
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/1/services", fieldValueToUse)},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/services", fieldValueToUse)},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[3].services in body cannot be updated"},
		}
		testDefs = append(testDefs, testCase)
	}

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestValidationDefaultApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:        "TestValidateApplyIndexServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/indexServiceMemoryQuota")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/spec/cluster/indexServiceMemoryQuota", "256Mi")},
		},
		{
			name:        "TestValidateApplySearchServiceMemoryQuotaDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/searchServiceMemoryQuota")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/spec/cluster/searchServiceMemoryQuota", "256Mi")},
		},
		{
			name:        "TestValidateApplyAutoFailoverTimeoutDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/autoFailoverTimeout")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/spec/cluster/autoFailoverTimeout", "120s")},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

func TestNegValidationConstraintsApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateApplyAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body shouldn't contain duplicates"},
		},
		{
			name:           "TestValidateApplyAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.Service("xxxxx")})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices in body should match '^data|index|query|search|eventing|analytics$'"},
		},
		{
			name:           "TestValidateApplyBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/ioPriority", "lighow")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority in body should match '^high|low$'"},
		},
		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy in body should match '^noEviction|nruEviction$'"},
		},

		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction)},
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
			mutations:      patchMap{"bucket4": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber)},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", "selwwno")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body should match '^seqno|lww$'"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionTimestamp)},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution in body cannot be updated"},
		},
		// servers service update
		{
			name:           "TestValidateApplyServerServicesImmutable_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyServerServicesImmutable_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyAntiAffinityImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/antiAffinity", true)},
			shouldFail:     true,
			expectedErrors: []string{"spec.antiAffinity in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyAuthSecretImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/security/adminSecret", "auth-secret-update")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authSecret in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyIndexStorageSettingsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexStorageSetting", couchbasev2.CouchbaseClusterIndexStorageSettingStandard)},
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.indexStorageSetting in body cannot be updated"},
		},
		// Server groups updates
		{
			name:           "TestValidateApplyServerGroupsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/serverGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-2"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups in body cannot be updated"},
		},
		{
			name:           "TestValidateApplyServerSserverGroupsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/2/serverGroups", []string{"us-east-1a", "us-east-1b", "us-east-1c"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups in body cannot be updated"},
		},
	}
	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "apply")
}

/*******************************************************************
************ Test cases for RBAC testing *************
********************************************************************/
func TestRBACValidationCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name:        "TestValidateLDAPDomain",
			mutations:   patchMap{"user1": jsonpatch.NewPatchSet().Replace("/spec/authDomain", "ldap")},
			validations: patchMap{"user1": jsonpatch.NewPatchSet().Test("/spec/authDomain", "ldap")},
			shouldFail:  false,
		}, {
			name:        "TestValidateClusterRole",
			mutations:   patchMap{"role": jsonpatch.NewPatchSet().Replace("/spec/roles/0/name", "cluster_admin")},
			validations: patchMap{"role": jsonpatch.NewPatchSet().Test("/spec/roles/0/name", "cluster_admin")},
			shouldFail:  false,
		},
		{
			name:           "TestRejectBucketForClusterRole",
			mutations:      patchMap{"role": jsonpatch.NewPatchSet().Replace("/spec/roles/0/bucket", "default")},
			validations:    patchMap{"role": jsonpatch.NewPatchSet().Test("/spec/roles/0/bucket", "default")},
			shouldFail:     true,
			expectedErrors: []string{"spec.roles[0].bucket for cluster role.admin is a forbidden property"},
		},
		/* Unknown role is caught by CRD validation which returns expectError that isn't being matched
		{
			name:           "TestUnknownRole",
			mutations:      patchMap{"role": jsonpatch.NewPatchSet().Replace("/spec/roles/0/name", "jabroni")},
			shouldFail:     true,
			expectedErrors: []string{"spec.roles[0].name in body should match '" + couchbasev2.ValidRolePattern() + "'"},
		}*/
		{
			name:           "TestValidateUnkownDomain",
			mutations:      patchMap{"user1": jsonpatch.NewPatchSet().Replace("/spec/authDomain", "unknown")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authDomain in body should match '^local|ldap$'"},
		},
		{
			name:           "TestValidateSecretRequired",
			mutations:      patchMap{"user1": jsonpatch.NewPatchSet().Remove("/spec/authSecret")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authSecret for `local` domain in user1 is required"},
		},
		{
			name:        "TestValidateDefaultBucketRole",
			validations: patchMap{"bucket-role": jsonpatch.NewPatchSet().Test("/spec/roles/0/bucket", "*")},
			shouldFail:  false,
		},
	}

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

/*******************************************************************
************ Test cases for LDAP Validation *************
********************************************************************/
func TestRBACValidationLDAP(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateHostnameRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/hosts/0")},
			shouldFail:     true,
			expectedErrors: []string{"spec.security.ldap.hosts should have at least 1 items"},
		},
		{
			name:           "TestValidateCaCertRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/tlsSecret")},
			shouldFail:     true,
			expectedErrors: []string{"spec.security.ldap.tlsSecret in body is required"},
		},
		{
			name:        "TestValidateDefaultRegex",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/user_dn_mapping/0/re")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/spec/security/ldap/user_dn_mapping/0/re", "(.+)")},
			shouldFail:  false,
		}, {
			name:           "TestValidateGroupRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/groups_query")},
			shouldFail:     true,
			expectedErrors: []string{"security.ldap.groups_query in body is required"},
		},
	}

	kubeName := framework.Global.TestClusters[0]
	runValidationTest(t, testDefs, kubeName, "create")
}

/*******************************************************************
************ Test cases for RZA / Server group testing *************
********************************************************************/

// Deploy couchbase cluster over non existent server group
func TestRzaNegCreateCluster(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateServerGroupsInvalid_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/serverGroups", []string{"InvalidGroup-1", "InvalidGroup-2"})},
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
