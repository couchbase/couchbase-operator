package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	other_jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	unavailableStorageClass = "unavailableStorageClass"
	emptyObject             = struct{}{}
)

// Resources are stored in a list in order to maintain ordering.  Do not try to use a map
// or things will happen in a random order.
type resourceList [][]byte

func (in resourceList) DeepCopy() [][]byte {
	out := make([][]byte, len(in))

	for i := range in {
		out[i] = make([]byte, len(in[i]))

		copy(out[i], in[i])
	}

	return out
}

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
		e2eutil.Die(t, fmt.Errorf("failures in test"))
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

// getResource takes raw JSON and returns the resource type (used by the raw API),
// the API version and the Kind (POST and PUT methods actually strip this from
// the status response so we have to replopulate it).
func getResource(k8s *types.Cluster, object *unstructured.Unstructured) (*schema.GroupVersionResource, error) {
	gvk := object.GroupVersionKind()

	mapping, err := k8s.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}

	return &mapping.Resource, nil
}

// createResources iterates over every resource and creates them in the requested namespace.
func createResources(k8s *types.Cluster, resources resourceList) error {
	for i, resource := range resources {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(resource, object); err != nil {
			return err
		}

		groupVersion, err := getResource(k8s, object)
		if err != nil {
			return err
		}

		res, err := k8s.DynamicClient.Resource(*groupVersion).Namespace(k8s.Namespace).Create(context.Background(), object, metav1.CreateOptions{})
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

		res, err := k8s.DynamicClient.Resource(*groupVersion).Namespace(k8s.Namespace).Update(context.Background(), object, metav1.UpdateOptions{})
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

		if _, err := k8s.DynamicClient.Resource(*groupVersion).Namespace(k8s.Namespace).Get(context.Background(), object.GetName(), metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				continue
			}

			return err
		}

		if err := k8s.DynamicClient.Resource(*groupVersion).Namespace(k8s.Namespace).Delete(context.Background(), object.GetName(), *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}

	return nil
}

// patchResources applies JSON patches to all defined resources.
func patchResources(resources resourceList, patches patchMap) error {
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

// getStorageClass selects either the framework specified storage class name
// (so as to maintain backwards compatibility) or selects one from the system.
// We should just use the system default anyway as that makes all the tests
// use the same configuration!
func getStorageClass(t *testing.T, cluster *types.Cluster) string {
	f := framework.Global

	if f.StorageClassName != "" {
		return f.StorageClassName
	}

	scs, err := cluster.KubeClient.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	if len(scs.Items) == 0 {
		t.Skip("test requires a storage class on the platform")
	}

	return scs.Items[0].Name
}

// validationOperationType determines when to apply patches to objects and what to do.
type validationOperationType int

const (
	// operationCreate applies patches to objects and creates them.
	operationCreate validationOperationType = iota

	// operationApply creates objects, applies patches and updates them.
	operationApply
)

// validationTLSType determines the TLS strategy to use.
type validationTLSType int

const (
	// tlsLegacy uses the old (pre-2.2) TLS layout.
	tlsLegacy validationTLSType = iota

	// tlsStandard uses Kubernetes TLS secrets.
	tlsStandard
)

// validationContext controls things about the validation environment.
type validationContext struct {
	// operation must be set.
	operation validationOperationType

	// tls may be set, if left zero, then it will default to legacy mode.
	tls validationTLSType
}

func runValidationTest(t *testing.T, testDefs []testDef, validation validationContext) {
	f := framework.Global

	targetKube, cleanup := f.SetupTest(t, framework.NoOperator)
	defer cleanup()

	// Clean up resources that may have been left behind by a job that was interrupted.
	objectsPristine, err := loadResources("/resources/validation/validation.yaml")
	if err != nil {
		e2eutil.Die(t, err)
	}

	// This is slow (entropy and modular exponetiation) so cache where possible,
	// this will make tests 4x faster!
	tlsCache := map[string]*e2eutil.TLSContext{}

	for i := range testDefs {
		test := testDefs[i]

		// Run each test case defined as a separate test so we have a way
		// of running them individually.
		t.Run(test.name, func(t *testing.T) {
			objects := objectsPristine.DeepCopy()

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
				object.SetNamespace(targetKube.Namespace)

				// Do dynamic environment configuration.
				if err := unstructured.SetNestedField(object.Object, targetKube.DefaultSecret.Name, "spec", "security", "adminSecret"); err != nil {
					e2eutil.Die(t, err)
				}

				// Do dynamic TLS configuration.
				ctx, ok := tlsCache[object.GetName()]
				if !ok {
					tlsOpts := &e2eutil.TLSOpts{
						ClusterName: object.GetName(),
						AltNames:    util_x509.MandatorySANs(object.GetName(), targetKube.Namespace),
					}
					tlsOpts.AltNames = append(tlsOpts.AltNames, "*.example.com")

					if validation.tls == tlsStandard {
						tlsOpts.Source = e2eutil.TLSSourceTLSSecret
					}

					ctx = e2eutil.MustInitClusterTLS(t, targetKube, tlsOpts)

					tlsCache[object.GetName()] = ctx
				}

				if validation.tls == tlsStandard {
					if err := unstructured.SetNestedField(object.Object, ctx.ClusterSecretName, "spec", "networking", "tls", "secretSource", "serverSecretName"); err != nil {
						e2eutil.Die(t, err)
					}
					if err := unstructured.SetNestedField(object.Object, ctx.OperatorSecretName, "spec", "networking", "tls", "secretSource", "clientSecretName"); err != nil {
						e2eutil.Die(t, err)
					}
				} else {
					if err := unstructured.SetNestedField(object.Object, ctx.ClusterSecretName, "spec", "networking", "tls", "static", "serverSecret"); err != nil {
						e2eutil.Die(t, err)
					}
					if err := unstructured.SetNestedField(object.Object, ctx.OperatorSecretName, "spec", "networking", "tls", "static", "operatorSecret"); err != nil {
						e2eutil.Die(t, err)
					}
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
					if err := unstructured.SetNestedField(pvct, getStorageClass(t, targetKube), "spec", "storageClassName"); err != nil {
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
			if validation.operation == operationApply {
				if err := createResources(targetKube, objects); err != nil {
					e2eutil.Die(t, err)
				}
			}

			// Patch the cluster specification
			if test.mutations != nil {
				if err := patchResources(objects, test.mutations); err != nil {
					e2eutil.Die(t, err)
				}
			}

			// Execute the main test, update the new resource for verification.
			switch validation.operation {
			case operationCreate:
				err = createResources(targetKube, objects)
			case operationApply:
				err = updateResources(targetKube, objects)
			}

			// Handle successes when it shoud have failed.
			// Also if any validations are defined then ensure the updated CR matches.
			if err == nil {
				if test.shouldFail {
					e2eutil.Die(t, fmt.Errorf("test unexpectedly succeeded"))
				}
				if test.validations != nil {
					if err := patchResources(objects, test.validations); err != nil {
						e2eutil.Die(t, err)
					}
				}
			}

			// When it did fail, handle when it shouldn't have done so.
			// If there were any errors to expect look for them.
			if err != nil {
				if !test.shouldFail {
					e2eutil.Die(t, fmt.Errorf("test unexpectedly failed: %w", err))
				}

				if len(test.expectedErrors) > 0 {
					for _, message := range test.expectedErrors {
						if !strings.Contains(err.Error(), message) || message == "" {
							t.Logf("expected message: %v", message)
							t.Logf("actual message: %v", err)
							e2eutil.Die(t, fmt.Errorf("expected message not encountered"))
						}
					}
				}
			}
		})
	}
}

func TestValidationCreate(t *testing.T) {
	supportedTimeUnits := []string{"ns", "us", "ms", "s", "m", "h"}

	supportedImagePatterns := []string{
		"registry.tld/org/image:7.0.0",
		"registry.tld:1234/org/image:7.0.0",
		"registry.tld:1234/org/image:prefix-7.0.0",
		"registry.tld:1234/org/image:7.0.0-s",
		"192.168.0.1/org/image:7.0.0",
		"192.168.0.1:1234/org/image:7.0.0",
		"org/image@sha256:43a5c1abd5f85ec09019b8794e082e28ae2562195046660d2ead7f059ba67f64",
		"registry.tld/org/image@sha256:43a5c1abd5f85ec09019b8794e082e28ae2562195046660d2ead7f059ba67f64",
		"registry.tld:1234/org/image@sha256:43a5c1abd5f85ec09019b8794e082e28ae2562195046660d2ead7f059ba67f64",
		"192.168.0.1/org/image@sha256:43a5c1abd5f85ec09019b8794e082e28ae2562195046660d2ead7f059ba67f64",
		"192.168.0.1:1234/org/image@sha256:43a5c1abd5f85ec09019b8794e082e28ae2562195046660d2ead7f059ba67f64",
		"192.168.0.1:1234/org/image@sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

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

	// Cases to verify valid image patterns
	for i, pattern := range supportedImagePatterns {
		testDefCase := testDef{
			name:       fmt.Sprintf("TestValidateImagePattern_%d", i),
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", pattern)},
			shouldFail: false,
		}
		testDefs = append(testDefs, testDefCase)
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseCluster(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateServerGroupsUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/serverGroups", []string{"NewGroupUpdate-1", "NewGroupUpdate-1"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.serverGroups"},
		},
		{
			name:           "TestValidateRecoveryPolicyIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/recoveryPolicy", "fryUp")},
			shouldFail:     true,
			expectedErrors: []string{`spec.recoveryPolicy`},
		},
		{
			name:           "TestValidateUpgradeStrategyIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgradeStrategy", "bigBang")},
			shouldFail:     true,
			expectedErrors: []string{`spec.upgradeStrategy`},
		},
		{
			name:           "TestValidateHibernationStrategyIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/hibernationStrategy", "chloroform")},
			shouldFail:     true,
			expectedErrors: []string{`spec.hibernationStrategy`},
		},
		{
			name:           "TestValidateRollingUpgradeMaxUpgradableUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/rollingUpgrade/maxUpgradable", 0)},
			shouldFail:     true,
			expectedErrors: []string{`spec.rollingUpgrade.maxUpgradable`},
		},
		{
			name:           "TestValidateRollingUpgradeMaxUpgradablePercentUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/rollingUpgrade/maxUpgradablePercent", "0%")},
			shouldFail:     true,
			expectedErrors: []string{`spec.rollingUpgrade.maxUpgradablePercent`},
		},
		{
			name:           "TestValidateRollingUpgradeMaxUpgradablePercentOverflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/rollingUpgrade/maxUpgradablePercent", "9001%")}, // OVER 9000!!!
			shouldFail:     true,
			expectedErrors: []string{`spec.rollingUpgrade.maxUpgradablePercent`},
		},
		{
			name: "TestValidateVolumeClaimTemplatesNegRejected",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", "0")},
			shouldFail:     true,
			expectedErrors: []string{"spec.volumeClaimTemplates.resources.requests"},
		},
		{
			name: "TestValidateVolumeClaimTemplatesNegRejected",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", "-1")},
			shouldFail:     true,
			expectedErrors: []string{"spec.volumeClaimTemplates.resources.requests"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseClusterNetworking(t *testing.T) {
	invalidExternalTraficPolicy := corev1.ServiceExternalTrafficPolicyType("Donkey")

	testDefs := []testDef{
		{
			name:           "TestValidateExposedFeaturesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatures", couchbasev2.ExposedFeatureList{couchbasev2.FeatureAdmin, "cleint", couchbasev2.FeatureXDCR})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.exposedFeatures"},
		},
		{
			name:           "TestValidateExposedFeaturesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatures", couchbasev2.ExposedFeatureList{couchbasev2.FeatureAdmin, couchbasev2.FeatureClient, couchbasev2.FeatureXDCR, couchbasev2.FeatureAdmin})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.exposedFeatures"},
		},
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.IndexService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices"},
		},
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.Service("indxe"), couchbasev2.QueryService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices"},
		},
		{
			name:           "TestValidateTLSClientCertificatePolicyInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/clientCertificatePolicy", "invalid")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePolicy`},
		},
		{
			name:           "TestValidateTLSClientCertificatePathInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/clientCertificatePaths/0/path", "invalid")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths.path`},
		},
		{
			name:           "TestValidateTLSClientCertificatePathRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/networking/tls/clientCertificatePaths/0/path")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths.path`},
		},
		{
			name:           "TestValidateTLSClientCertificateNoPaths",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/networking/tls/clientCertificatePaths")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths`},
		},
		{
			name: "TestValidateDNSReqiredWithPublicAdminConsoleService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/adminConsoleServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.dns`},
		},
		{
			name: "TestValidateDNSReqiredWithPublicExposedFeatureService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/exposedFeatureServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.dns`},
		},
		{
			name: "TestValidateTLSRequiredWithPublicAdminConsoleService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/adminConsoleServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/tls")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls`},
		},
		{
			name: "TestValidateTLSRequiredWithPublicExposedFeatureService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/exposedFeatureServiceType", corev1.ServiceTypeLoadBalancer).
				Remove("/spec/networking/tls")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls`},
		},
		{
			name:       "TestValidateMissingDNSSubjectAltName",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/dns/domain", "acme.com")},
			shouldFail: true,
		},
		{
			name:           "TestValidateExposedFeatureTrafficPolicyInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatureTrafficPolicy", &invalidExternalTraficPolicy)},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.exposedFeatureTrafficPolicy`},
		},
		{
			name:           "TestValidateLoadBalancerSourceRanges",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/networking/loadBalancerSourceRanges/-", "192.168.0.1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.loadBalancerSourceRanges`},
		},
		{
			name:           "TestValidateN2NEncryptionIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/nodeToNodeEncryption", "illegal")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.nodeToNodeEncryption`},
		},
		{
			name:           "TestValidateTLSMinimumVersionIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/tlsMinimumVersion", "SSL1.3")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.tlsMinimumVersion`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseClusterNetworkingTLSStandard(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateTLSStandardServerSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/secretSource/serverSecretName", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.secretSource.serverSecretName must exist"},
		},
		{
			name:           "TestValidateTLSStandardClientSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/secretSource/clientSecretName", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.secretSource.clientSecretName must exist"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, tls: tlsStandard})
}

func TestNegValidationCreateCouchbaseClusterNetworkingTLSLegacy(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateTLSLegacyServerSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/static/serverSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.static.serverSecret must exist"},
		},
		{
			name:           "TestValidateTLSLegacyOperatorSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/static/operatorSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.networking.tls.static.operatorSecret must exist"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, tls: tlsLegacy})
}

func TestNegValidationCreateCouchbaseClusterServers(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateServerServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.Service("indxe"), couchbasev2.QueryService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.services"},
		},
		{
			name:           "TestValidateServerNameUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/name", "data_only")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers"},
		},
		{
			name:           "TestValidateServerServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services"},
		},
		{
			name:           "TestServerSizeRangeInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", -2)},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers.size"},
		},
		{
			name:           "TestNoDataService",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers", []couchbasev2.ServerConfig{{Name: "test", Size: 1, Services: couchbasev2.ServiceList{couchbasev2.IndexService}}})},
			shouldFail:     true,
			expectedErrors: []string{`at least one "data" service in spec.servers.services is required`},
		},
		{
			name:           "TestValidateServerServerGroupsUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/2/serverGroups", []string{"us-east-1a", "us-east-1b", "us-east-1a"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[2].serverGroups"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseClusterPersistentVolumes(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateVolumeClaimTemplatesStorageClassMustExist",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/spec/storageClassName", &unavailableStorageClass)},
			shouldFail:     true,
			expectedErrors: []string{"storage class"},
		},
		{
			name:       "TestValidateVolumeClaimTemplateMustExist",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/volumeClaimTemplates/0/metadata/name", "InvalidVolumeClaim")},
			shouldFail: true,
			expectedErrors: []string{
				"spec.servers[0].default",
				"spec.servers[1].default",
				"spec.servers[1].data",
				"spec.servers[1].index",
				"spec.servers[1].analytics[0]",
				"spec.servers[1].analytics[1]",
				"spec.servers[2].default",
				"spec.servers[4].default",
			},
		},
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/volumeMounts/logs", "couchbase")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].volumeMounts.default"},
		},
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/volumeMounts/default", "couchbase-log-pv")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[3].volumeMounts.default"},
		},
		{
			name:       "TestValidateDefaultVolumeMountRequiredForStatefulServices",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.EventingService, couchbasev2.AnalyticsService})},
			shouldFail: true,
		},
		{
			name:           "TestValidateServerServiceRequiredForVolumeMountData",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/0")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers[1].volumeMounts.data`},
		},
		{
			name:           "TestValidateServerServiceRequiredForVolumeMountIndex",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/3").Remove("/spec/servers/1/services/1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers[1].volumeMounts.index`},
		},
		{
			name:       "TestValidateServerServiceRequiredForVolumeMountIndexSearchOnly",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/1")},
			shouldFail: false,
		},
		{
			name:           "TestValidateServerServiceRequiredForVolumeMountAnalytics",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/2")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers[1].volumeMounts.analytics`},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/1/volumeMounts/"+mntField, "invalidClaim")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[1]." + mntName},
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases to validate with Log PV only defined,but one of stateful service is included
	for _, statefulService := range constants.StatefulCbServiceList {
		testCase := testDef{
			name:       "TestValidateDefaultVolumeMountRequiredForStatefulService_" + string(statefulService),
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/services", couchbasev2.ServiceList{couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.EventingService, statefulService})},
			shouldFail: true,
		}
		testDefs = append(testDefs, testCase)
	}

	// Cases for defining Stateful claims without specifying Default volume mounts
	claimFieldNames := []string{"data", "index"}
	for _, claimField := range claimFieldNames {
		testCase := testDef{
			name: "TestValidateDefaultVolumeMountRequiredForServiceClaim_" + claimField,
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/servers/0/volumeMounts/"+claimField, "couchbase").
				Remove("/spec/servers/0/volumeMounts/default")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].volumeMounts"},
		}
		testDefs = append(testDefs, testCase)
	}
	// Analytics is an array value
	testCase := testDef{
		name: "TestValidateDefaultVolumeMountRequiredForAnalytics",
		mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
			Replace("/spec/servers/0/volumeMounts/analytics", []string{"couchbase"}).
			Remove("/spec/servers/0/volumeMounts/default")},
		shouldFail:     true,
		expectedErrors: []string{"spec.servers[0].volumeMounts"},
	}
	testDefs = append(testDefs, testCase)
	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseClusterLogging(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateLogRetentionTimeInvalidPattern",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionTime", "1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.logging.logRetentionTime`},
		},
		{
			name:           "TestValidateLogRetentionCountInvalidRange",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionCount", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.logging.logRetentionCount"},
		},
		{
			name: "TestValidateLoggingFailsForNoPersistentVolume",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/logging/server", &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
					Enabled: true,
				}).Remove("/spec/servers/0/volumeMounts"), // only need to remove first as we validate only on that
			},
			shouldFail: true,
		},
		{
			name: "TestValidateLoggingIgnoresGCWithNoAudit",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
					Enabled: false,
					GarbageCollection: &couchbasev2.CouchbaseClusterAuditGarbageCollectionSpec{
						Sidecar: &couchbasev2.CouchbaseClusterAuditCleanupSidecarSpec{
							Enabled: true,
						},
					},
				}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateLoggingFailsAuditGCWithNoPersistentVolume",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
					Enabled: true,
					GarbageCollection: &couchbasev2.CouchbaseClusterAuditGarbageCollectionSpec{
						Sidecar: &couchbasev2.CouchbaseClusterAuditCleanupSidecarSpec{
							Enabled: true,
						},
					},
				}).
				Remove("/spec/servers/0/volumeMounts"),
			},
			shouldFail: true,
		},
		{
			name: "TestValidateLoggingFailsForInvalidAuditUser",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
					DisabledUsers: []couchbasev2.AuditDisabledUser{
						"Cthulu",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"spec.logging.audit.disabledUsers"},
		},
		{
			name: "TestValidateLoggingAllowsAuditUser",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
					DisabledUsers: []couchbasev2.AuditDisabledUser{
						"localusername/local",
						"externalusername/external",
						"@internalusername/local",
					},
				}),
			},
			shouldFail: false,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseClusterSettings(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateAutoCompactionMinimum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", 1)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.databaseFragmentationThreshold.percent`},
		},
		{
			name:           "TestValidateAutoCompactionMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", 101)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.databaseFragmentationThreshold.percent`},
		},
		{
			name:           "TestValidateStartTimeIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/timeWindow/start", "26:00")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.timeWindow.start`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMinimum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/tombstonePurgeInterval", "1m")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.tombstonePurgeInterval`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/tombstonePurgeInterval", "2400h")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.tombstonePurgeInterval`},
		},
		{
			name:           "TestValidateDataServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/dataServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.dataServiceMemoryQuota`},
		},
		{
			name:           "TestValidateIndexServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexServiceMemoryQuota`},
		},
		{
			name:           "TestValidateSearchServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/searchServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.searchServiceMemoryQuota`},
		},
		{
			name:           "TestValidateEventingServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/eventingServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.eventingServiceMemoryQuota`},
		},
		{
			name:           "TestValidateAnalyticsServiceMemoryQuotaUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/analyticsServiceMemoryQuota", "0Mi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.analyticsServiceMemoryQuota`},
		},
		{
			name:           "TestValidateAutoFailoverTimeoutUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", "0s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverTimeout`},
		},
		{
			name:           "TestValidateAutoFailoverTimeoutOverflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", "2h")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverTimeout`},
		},
		{
			name:           "TestValidateAutoFailoverOnDataDiskIssuesTimePeriodUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", "0s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod`},
		},
		{
			name:           "TestValidateAutoFailoverOnDataDiskIssuesTimePeriodOverflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", "2h")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverOnDataDiskIssuesTimePeriod`},
		},
		{
			name:           "TestValidateIndexerThreadsUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/threads", "-1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.threads`},
		},
		{
			name:           "TestValidateIndexerLogLevelInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/logLevel", "scots-pine")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.logLevel`},
		},
		{
			name:           "TestValidateIndexerMaxRollbackPointsUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/maxRollbackPoints", "0")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.maxRollbackPoints`},
		},
		{
			name:           "TestValidateIndexerMemorySnapshotIntervalUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/memorySnapshotInterval", "999us")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.memorySnapshotInterval`},
		},
		{
			name:           "TestValidateIndexerStableSnapshotIntervalUndeflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/stableSnapshotInterval", "999us")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.stableSnapshotInterval`},
		},
		{
			name:           "TestValidateIndexerStorageModeInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/storageMode", "ntfs")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.storageMode`},
		},
		{
			name:           "TestValidateQueryTemporarySpaceUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/query/temporarySpace", "-2Gi")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.query.temporarySpace`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseClusterSecurity(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateAuthSecretMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/security/adminSecret", "does-not-exist")},
			shouldFail:     true,
			expectedErrors: []string{"secret does-not-exist referenced by spec.security.adminSecret must exist"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseClusterXDCR(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateXDCRUUIDInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/uuid", "cat")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.uuid`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidName",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "illegal_dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "starsky-and-hutch.tv:huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters.hostname`},
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
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseBucket(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateBucketEvictionPolicyNoEvicitionInvalidInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", "noEviction")},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyNRUEvictionInvalidInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},
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
			expectedErrors: []string{"spec.compressionMode"},
		},
		{
			name:           "TestValidateExplicitBucketNameIllegalCharacter",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Add("/spec/name", "!@#$%^&*()")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{
			name:           "TestValidateExplicitBucketNameIllegalLength",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Add("/spec/name", "000000000011111111111222222222223333333333344444444444455555555555666666666667777777777778888888888889999999999OVERFLOW")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{
			name:           "TestValidateBucketDurabilityIllegal",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/minimumDurability", "flimsy")},
			shouldFail:     true,
			expectedErrors: []string{`spec.minimumDurability`},
		},
		{
			name:           "TestValidateBucketMaxTTLUnderflow",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/maxTTL", "-1s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.maxTTL`},
		},
		{
			name:           "TestValidateBucketMaxTTLOverflow",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/maxTTL", "2147483648s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.maxTTL`},
		},
		{
			name:           "TestValidateBucketMetadataNameCollision",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/metadata/name", "bucket0")},
			shouldFail:     true,
			expectedErrors: []string{`bucket0`},
		},
		{
			name:           "TestValidateBucketSpecNameCollision",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/name", "bucket0")},
			shouldFail:     true,
			expectedErrors: []string{`bucket0`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseEphemeralBucket(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateBucketEvictionPolicyValueOnlyInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyFullEvicitonInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyFullEviction)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},
		{
			name:           "TestValidateBucketCompressionModeInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/compressionMode", couchbasev2.CouchbaseBucketCompressionMode("invalid"))},
			shouldFail:     true,
			expectedErrors: []string{"spec.compressionMode"},
		},
		{
			name:           "TestValidateExplicitEphemeralBucketNameIllegalCharacter",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Add("/spec/name", "!@#$%^&*()")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{
			name:           "TestValidateExplicitEphemeralBucketNameIllegalLength",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Add("/spec/name", "000000000011111111111222222222223333333333344444444444455555555555666666666667777777777778888888888889999999999OVERFLOW")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{
			name:           "TestValidateEphemeralBucketDurabilityIllegal",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/minimumDurability", "flimsy")},
			shouldFail:     true,
			expectedErrors: []string{`spec.minimumDurability`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseMemcachedBucket(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateExplicitMemcachedBucketNameIllegalCharacter",
			mutations:      patchMap{"bucket2": jsonpatch.NewPatchSet().Add("/spec/name", "!@#$%^&*()")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{
			name:           "TestValidateExplicitMemcachedBucketNameIllegalLength",
			mutations:      patchMap{"bucket2": jsonpatch.NewPatchSet().Add("/spec/name", "000000000011111111111222222222223333333333344444444444455555555555666666666667777777777778888888888889999999999OVERFLOW")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseReplication(t *testing.T) {
	testDefs := []testDef{
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
			expectedErrors: []string{`spec.compressionType`},
		},
		{
			name:           "TestValidateXDCRSourceNamePrecedence",
			mutations:      patchMap{"replication1": jsonpatch.NewPatchSet().Replace("/spec/bucket", "bucket1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.bucket`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseBackup(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateBackupInvalidCronSchedule",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Replace("/spec/full/schedule", "*7 * * * *")},
			shouldFail:     true,
			expectedErrors: []string{`spec.full.schedule`},
		},
		{
			name:           "TestValidateBackupInvalidStrategy",
			mutations:      patchMap{"backup1": jsonpatch.NewPatchSet().Replace("/spec/strategy", "tumbleweed")},
			shouldFail:     true,
			expectedErrors: []string{`spec.strategy`},
		},
		{
			name:           "TestValidateBackupSizeZero",
			mutations:      patchMap{"backup1": jsonpatch.NewPatchSet().Replace("/spec/size", "0")},
			shouldFail:     true,
			expectedErrors: []string{`spec.size`},
		},
		{
			name:           "TestValidateBackupSizeNegative",
			mutations:      patchMap{"backup1": jsonpatch.NewPatchSet().Replace("/spec/size", "-2")},
			shouldFail:     true,
			expectedErrors: []string{`spec.size`},
		},
		{
			name:           "TestValidateBackupS3Bucket",
			mutations:      patchMap{"backup1": jsonpatch.NewPatchSet().Replace("/spec/s3bucket", "hellobeans")},
			shouldFail:     true,
			expectedErrors: []string{`spec.s3bucket`},
		},
		{
			name:           "TestValidateBackupMissingCronSchedule",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Remove("/spec/incremental")},
			shouldFail:     true,
			expectedErrors: []string{`spec.incremental`},
		},
		{
			name:           "TestValidateBackupMissingCronSchedule2",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Replace("/spec/incremental/schedule", "")},
			shouldFail:     true,
			expectedErrors: []string{`spec.incremental`},
		},
		{
			name:           "TestValidateBackupIncludeInvalidBucketName",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "buck^et")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateBackupIncludeInvalidScopeName",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket._scope")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateBackupIncludeInvalidCollectionName",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket.scope._collection")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateBackupIncludeInvalidFormat",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket.nested.scope.collection")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateBackupIncludeInvalidDefault",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket.scope._default")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateBackupIncludeInvalidOverlap",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket1.scope")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateBackupIncludeExcludeMutuallyExclusive",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Add("/spec/data/exclude", []string{"anything"})},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`, `spec.data.exclude`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseBackupRestore(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateBackupRestoreMissingBackupField",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Remove("/spec/backup")},
			shouldFail:     true,
			expectedErrors: []string{`spec.backup`},
		},
		{
			name:           "TestValidateBackupRestoreStartPositiveInteger",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/start/int", 0)},
			shouldFail:     true,
			expectedErrors: []string{`spec.start.int`},
		},
		{
			name:           "TestValidateBackupRestoreEndPositiveInteger",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/end/int", -27)},
			shouldFail:     true,
			expectedErrors: []string{`spec.end.int`},
		},
		{
			name:           "TestValidateBackupRestoreStartIntWithString",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/start/int", "20")},
			shouldFail:     true,
			expectedErrors: []string{`spec.start.int`},
		},
		{
			name:           "TestValidateBackupRestoreEndIntWithString",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/end/int", "latest")},
			shouldFail:     true,
			expectedErrors: []string{`spec.end.int`},
		},
		{
			name:           "TestValidateBackupRestoreStartStringWithInt",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/start/str", 2)},
			shouldFail:     true,
			expectedErrors: []string{`spec.start.str`},
		},
		{
			name:           "TestValidateBackupRestoreEndStringWithInt",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/end/str", 17)},
			shouldFail:     true,
			expectedErrors: []string{`spec.end.str`},
		},
		{
			name:           "TestValidateRestoreIncludeInvalidBucketName",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "buck^et")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateRestoreIncludeInvalidScopeName",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket._scope")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateRestoreIncludeInvalidCollectionName",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket.scope._collection")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateRestoreIncludeInvalidFormat",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket.nested.scope.collection")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateRestoreIncludeInvalidDefault",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket.scope._default")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateRestoreIncludeInvalidOverlap",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/include/-", "bucket1.scope")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`},
		},
		{
			name:           "TestValidateRestoreIncludeExcludeMutuallyExclusive",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/exclude", []string{"anything"})},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.include`, `spec.data.exclude`},
		},
		{
			name:           "TestValidateRestoreMapSameSourceInvalid",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/map/-", map[string]string{"source": "bucket1", "target": "bucket99"})},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.map`},
		},
		{
			name:           "TestValidateRestoreMapSourceInvalidOverlap",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Add("/spec/data/map/-", map[string]string{"source": "bucket1.scope", "target": "bucket99"})},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.map`},
		},
		{
			name:           "TestValidateRestoreMapMixedScopeInvalid",
			mutations:      patchMap{"restore1": jsonpatch.NewPatchSet().Replace("/spec/data/map/0/target", "bucket2.scope")},
			shouldFail:     true,
			expectedErrors: []string{`spec.data.map`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseScopesAndCollections(t *testing.T) {
	testDefs := []testDef{
		// The name type is shared across all scopes and collections types, so
		// adding explicit tests for each type should be a waste of time and effort.
		{
			name:           "TestValidateScopeNameTooShort",
			mutations:      patchMap{"scope0": jsonpatch.NewPatchSet().Add("/spec/name", "")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{
			name:           "TestValidateScopeNameTooLong",
			mutations:      patchMap{"scope0": jsonpatch.NewPatchSet().Add("/spec/name", "1234567890123456789012345678901")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{

			name:           "TestValidateScopeNameIllegalCharacter",
			mutations:      patchMap{"scope0": jsonpatch.NewPatchSet().Add("/spec/name", "abc&")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		{

			name:           "TestValidateScopeNameIllegalFirstCharacter",
			mutations:      patchMap{"scope0": jsonpatch.NewPatchSet().Add("/spec/name", "%pewpew")},
			shouldFail:     true,
			expectedErrors: []string{`spec.name`},
		},
		// Check that array names are working as designed as they are slightly
		// different from above.
		{
			name:           "TestValidateScopeGroupEmptyNamesIllegal",
			mutations:      patchMap{"scopegroup0": jsonpatch.NewPatchSet().Remove("/spec/names")},
			shouldFail:     true,
			expectedErrors: []string{`spec.names`},
		},
		{
			name:           "TestValidateScopeGroupNameIllegal",
			mutations:      patchMap{"scopegroup0": jsonpatch.NewPatchSet().Replace("/spec/names/0", "_default")},
			shouldFail:     true,
			expectedErrors: []string{`spec.names`},
		},
		{
			name:           "TestValidateScopeGroupNamesUnique",
			mutations:      patchMap{"scopegroup0": jsonpatch.NewPatchSet().Add("/spec/names/-", "daffy")},
			shouldFail:     true,
			expectedErrors: []string{`spec.names`},
		},
		// Check that scope collection selection is working.  This is shared between
		// scopes and scope groups, so we only need to do this once.
		{
			name:           "TestValidateScopeSelectorTypeIllegal",
			mutations:      patchMap{"scope0": jsonpatch.NewPatchSet().Replace("/spec/collections/resources/0/kind", "CouchbaseScope")},
			shouldFail:     true,
			expectedErrors: []string{`spec.collections.resources.kind`},
		},
		// Check that bucket scope collection is working.  This shared between
		// couchbase and ephemeral buckets, so we only need to do this once.
		{
			name:           "TestValidateBucketSelectorTypeIllegal",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/scopes/resources/0/kind", "CouchbaseCollection")},
			shouldFail:     true,
			expectedErrors: []string{`spec.scopes.resources.kind`},
		},
		// Check TTLs are working.
		{
			name:           "TestCollectionTTLUnderflow",
			mutations:      patchMap{"collectiongroup0": jsonpatch.NewPatchSet().Add("/spec/maxTTL", "-15s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.maxTTL`},
		},
		{
			name:           "TestCollectionTTLOverflow",
			mutations:      patchMap{"collectiongroup0": jsonpatch.NewPatchSet().Add("/spec/maxTTL", "2147483648s")},
			shouldFail:     true,
			expectedErrors: []string{`spec.maxTTL`},
		},
		// Name aliasing testing... if etcd explodes, don't blame me.
		{
			name:           "TestCollectionNameAlias",
			mutations:      patchMap{"collection0": jsonpatch.NewPatchSet().Add("/spec", emptyObject).Add("/spec/name", "bugs")},
			shouldFail:     true,
			expectedErrors: []string{"couchbase collection name `bugs` in CouchbaseScope/scope0 redefined by CouchbaseCollectionGroup/collectiongroup0, first seen in CouchbaseCollection/collection0"},
		},
		{
			name:           "TestScopeNameAlias",
			mutations:      patchMap{"scope0": jsonpatch.NewPatchSet().Add("/spec/name", "daffy")},
			shouldFail:     true,
			expectedErrors: []string{"couchbase scope name `daffy` in CouchbaseBucket/bucket0 redefined by CouchbaseScopeGroup/scopegroup0, first seen in CouchbaseScope/scope0"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
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
			name:      "TestValidateIndexerDefault",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer", emptyObject)},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Test("/spec/cluster/indexer/logLevel", "info").
				Test("/spec/cluster/indexer/maxRollbackPoints", 2).
				Test("/spec/cluster/indexer/memorySnapshotInterval", "200ms").
				Test("/spec/cluster/indexer/stableSnapshotInterval", "5s").
				Test("/spec/cluster/indexer/storageMode", "memory_optimized"),
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
			name:       "TestValidateSecurityContextfsgroup",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/securityContext/fsGroup")},
			shouldFail: false,
		},
		{
			name:        "TestValidatefsgroup",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/securityContext/fsGroup", 1234)},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/spec/securityContext/fsGroup", 1234)},
			shouldFail:  false,
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
		{
			name:        "TestScopeCollectionReferenceKindDefault",
			mutations:   patchMap{"scope0": jsonpatch.NewPatchSet().Remove("/spec/collections/resources/0/kind")},
			validations: patchMap{"scope0": jsonpatch.NewPatchSet().Test("/spec/collections/resources/0/kind", "CouchbaseCollection")},
		},
		{
			name:        "TestBucketScopeReferenceKindDefault",
			mutations:   patchMap{"bucket0": jsonpatch.NewPatchSet().Remove("/spec/scopes/resources/0/kind")},
			validations: patchMap{"bucket0": jsonpatch.NewPatchSet().Test("/spec/scopes/resources/0/kind", "CouchbaseScope")},
		},
	}
	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
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
	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationConstraintsCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices"},
		},
		{
			name:           "TestValidateAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.Service("xxxxx")})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices"},
		},
		{
			name:           "TestValidateBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/ioPriority", "lighow")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority"},
		},
		{
			name:           "TestValidateBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", "selwwno")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},
		{
			name:           "TestValidateBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},
	}
	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

// CRD apply tests.
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

	runValidationTest(t, testDefs, validationContext{operation: operationApply})
}

func TestNegValidationApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateServerServicesImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services/0", "analytics")},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services"},
		},
		// Validation for logRetentionTime and logRetentionCount field
		{
			name:           "TestValidateApplyLogRetentionTimeInvalidPattern",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionTime", "1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.logging.logRetentionTime`},
		},
		{
			name:           "TestValidateApplyLogRetentionCountInvalidRange",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/logging/logRetentionCount", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.logging.logRetentionCount"},
		},
		{
			name:           "TestValidateBackupRestoreStartGreaterThanEnd",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/start/int", 27)},
			shouldFail:     true,
			expectedErrors: []string{`start integer cannot be larger than end integer`},
		},
		{
			name:           "TestValidateBackupRestoreStartBothStringAndInt",
			mutations:      patchMap{"restore0": jsonpatch.NewPatchSet().Add("/spec/start/str", "oldest")},
			shouldFail:     true,
			expectedErrors: []string{`specify just one value, either Str or Int`},
		},
		// Check Collection immutability.
		{
			name:           "TestValidateCollectionTTLImmutable",
			mutations:      patchMap{"collection0": jsonpatch.NewPatchSet().Add("/spec/maxTTL", "30s")},
			shouldFail:     true,
			expectedErrors: []string{"spec.maxTTL"},
		},
		{
			name:           "TestValidateCollectionGroupTTLImmutable",
			mutations:      patchMap{"collectiongroup0": jsonpatch.NewPatchSet().Add("/spec/maxTTL", "30s")},
			shouldFail:     true,
			expectedErrors: []string{"spec.maxTTL"},
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
			expectedErrors: []string{"spec.servers[1].services"},
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
			expectedErrors: []string{"spec.servers[3].services"},
		}
		testDefs = append(testDefs, testCase)
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply})
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
	runValidationTest(t, testDefs, validationContext{operation: operationApply})
}

func TestNegValidationConstraintsApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateApplyAdminConsoleServicesUnique",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.SearchService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices"},
		},
		{
			name:           "TestValidateApplyAdminConsoleServicesEnumInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServices", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.QueryService, couchbasev2.Service("xxxxx")})},
			shouldFail:     true,
			expectedErrors: []string{"spec.networking.adminConsoleServices"},
		},
		{
			name:           "TestValidateApplyBucketIOPriorityEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/ioPriority", "lighow")},
			shouldFail:     true,
			expectedErrors: []string{"spec.ioPriority"},
		},
		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForEphemeral",
			mutations:      patchMap{"bucket3": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},

		{
			name:           "TestValidateApplyBucketEvictionPolicyEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNRUEviction)},
			shouldFail:     true,
			expectedErrors: []string{"spec.evictionPolicy"},
		},
		{
			name: "TestValidateVolumeClaimTemplatesShrinkRejected",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/enableOnlineVolumeExpansion", true).
				Replace("/spec/volumeClaimTemplates/0/spec/resources/requests/storage", "1Gi")},
			shouldFail:     true,
			expectedErrors: []string{"spec.volumeClaimTemplates.resources.requests"},
		},
	}
	runValidationTest(t, testDefs, validationContext{operation: operationApply})
}

func TestNegValidationImmutableApply(t *testing.T) {
	testDefs := []testDef{
		// Bucket spec updation
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForEphemeral",
			mutations:      patchMap{"bucket4": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber)},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionEnumInvalidForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", "selwwno")},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution"},
		},
		{
			name:           "TestValidateApplyBucketConflictResolutionImmutableForCouchbase",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionTimestamp)},
			shouldFail:     true,
			expectedErrors: []string{"spec.conflictResolution"},
		},
		// servers service update
		{
			name:           "TestValidateApplyServerServicesImmutable_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.IndexService, couchbasev2.SearchService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services"},
		},
		{
			name:           "TestValidateApplyServerServicesImmutable_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servers[0].services"},
		},
		{
			name:           "TestValidateApplyIndexStorageSettingsImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/storageMode", couchbasev2.CouchbaseClusterIndexStorageSettingStandard)},
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.indexer.storageMode"},
		},
		{
			name:           "TestValidateBackupStrategyImmutable",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Replace("/spec/strategy", "full_only")},
			shouldFail:     true,
			expectedErrors: []string{"spec.strategy"},
		},
		{
			name:           "TestValidateReplicationBucketImmutable",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/bucket", "tinkywinky")},
			shouldFail:     true,
			expectedErrors: []string{"spec.bucket"},
		},
		{
			name:           "TestValidateReplicationRemoteBucketImmutable",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/remoteBucket", "dipsy")},
			shouldFail:     true,
			expectedErrors: []string{"spec.remoteBucket"},
		},
		{
			name:           "TestValidateReplicationFilterExpressionImmutable",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/filterExpression", "lala")},
			shouldFail:     true,
			expectedErrors: []string{"spec.filterExpression"},
		},
		// Poor po!
		{
			name:           "TestValidateNetworkAddressFamilyImmutable",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/addressFamily", couchbasev2.AFInet6)},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.addressFamily`},
		},
	}
	runValidationTest(t, testDefs, validationContext{operation: operationApply})
}

// Test cases for RBAC testing.
func TestRBACValidationCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name:       "TestValidateLDAPDomain",
			mutations:  patchMap{"user2": jsonpatch.NewPatchSet().Replace("/spec/authDomain", "external")},
			shouldFail: false,
		},
		{
			name:        "TestValidateClusterRole",
			mutations:   patchMap{"admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0/name", "cluster_admin")},
			validations: patchMap{"admin-group": jsonpatch.NewPatchSet().Test("/spec/roles/0/name", "cluster_admin")},
			shouldFail:  false,
		},
		{
			name:           "TestRejectBucketForClusterRole",
			mutations:      patchMap{"admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0/bucket", "default")},
			validations:    patchMap{"admin-group": jsonpatch.NewPatchSet().Test("/spec/roles/0/bucket", "default")},
			shouldFail:     true,
			expectedErrors: []string{"spec.roles[0].bucket"},
		},
		{
			name:           "TestValidateUnkownDomain",
			mutations:      patchMap{"user1": jsonpatch.NewPatchSet().Replace("/spec/authDomain", "unknown")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authDomain"},
		},
		{
			name:           "TestValidateSecretRequired",
			mutations:      patchMap{"user1": jsonpatch.NewPatchSet().Remove("/spec/authSecret")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authSecret"},
		},
		{
			name:       "TestValidateBucketName",
			mutations:  patchMap{"data-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0/bucket", "99.buckets-0nthe_w%ll")},
			shouldFail: false,
		},
		{
			name:       "TestValidateAllBucketSelector",
			mutations:  patchMap{"data-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0/bucket", "*")},
			shouldFail: false,
		},
		{
			name:           "TestRejectInvalidBucketName",
			mutations:      patchMap{"data-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0/bucket", "default#bucket")},
			shouldFail:     true,
			expectedErrors: []string{"spec.roles.bucket"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

// Test cases for LDAP Validation.
func TestRBACValidationLDAP(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateHostnameRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/hosts/0")},
			shouldFail:     true,
			expectedErrors: []string{"spec.security.ldap.hosts"},
		},
		{
			name:           "TestValidateCaCertRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/tlsSecret")},
			shouldFail:     true,
			expectedErrors: []string{"spec.security.ldap.tlsSecret"},
		}, {
			name:           "TestValidateGroupRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/groupsQuery")},
			shouldFail:     true,
			expectedErrors: []string{"security.ldap.groupsQuery in body is required"},
		}, {
			name:           "TestValidateAuthSecretRejected",
			mutations:      patchMap{"user2": jsonpatch.NewPatchSet().Add("/spec/authSecret", "auth-secret")},
			shouldFail:     true,
			expectedErrors: []string{"secret auth-secret not allowed for LDAP user `user2`"},
		}, {
			name:           "TestValidateAuthDomain",
			mutations:      patchMap{"user1": jsonpatch.NewPatchSet().Replace("/spec/authDomain", "upnorth")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authDomain"},
		}, {
			name:        "TestValidateAuthenticationDefault",
			mutations:   patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/authenticationEnabled")},
			validations: patchMap{"cluster": jsonpatch.NewPatchSet().Test("/spec/security/ldap/authenticationEnabled", true)},
			shouldFail:  false,
		},
		{
			name:      "TestValidateCertValidationFails",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/security/ldap/encryption", "None")},
			// Please fix me...
			expectedErrors: []string{"encryption must be one of TLS | StartTLSExtension, when serverCertValidation is enabled"},
			shouldFail:     true,
		},
		{
			name: "TestUserDNQuery",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/security/ldap/userDNMapping/query", "users=%u,dc=example,dc=com").
				Remove("/spec/security/ldap/userDNMapping/template")},
			shouldFail: false,
		},
		{
			name: "TestUserDNQueryAndTemplate",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/security/ldap/userDNMapping/query", "users=%u,dc=example,dc=com")},
			expectedErrors: []string{"ldap.userDNMapping"},
			shouldFail:     true,
		},
		{
			name: "TestUserDNQueryWithoutMapping",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Remove("/spec/security/ldap/userDNMapping/template")},
			expectedErrors: []string{"spec.security.ldap.userDNMapping"},
			shouldFail:     true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

// Test cases for Autoscaler Validation.
func TestAutoscalerValidation(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestAutoscalerMissingServers",
			mutations:      patchMap{"scaler": jsonpatch.NewPatchSet().Remove("/spec/servers")},
			expectedErrors: []string{"spec.servers"},
			shouldFail:     true,
		},
		{
			name:           "TestAutoscalerMissingSize",
			mutations:      patchMap{"scaler": jsonpatch.NewPatchSet().Remove("/spec/size")},
			expectedErrors: []string{"spec.size"},
			shouldFail:     true,
		},
		{
			name:           "TestAutoscalerNegStabilizationPeriod",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/autoscaleStabilizationPeriod", "-1s")},
			expectedErrors: []string{"spec.autoscaleStabilizationPeriod"},
			shouldFail:     true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

// Test cases for RZA / Server group testing.
// Deploy couchbase cluster over non existent server group.
func TestRzaNegCreateCluster(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateServerGroupsInvalid_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/serverGroups", []string{"InvalidGroup-1", "InvalidGroup-2"})},
			shouldFail:     true,
			expectedErrors: []string{"spec.servergroups"},
		},
		{
			name:           "TestValidateServerGroupsInvalid_2",
			shouldFail:     true,
			expectedErrors: []string{"spec.servergroups"},
		},
	}
	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}
