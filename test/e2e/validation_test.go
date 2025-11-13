package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/couchbase-operator/test/e2e/util"

	other_jsonpatch "github.com/evanphx/json-patch"
	"github.com/ghodss/yaml"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	utilErrors "github.com/couchbase/couchbase-operator/pkg/errors"
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
	// expectedWarnings is a list of expected warnings for the test. Warnings are only collected for the resources that are being patched.
	expectedWarnings []string
	deleteTargets    []string
}

type (
	failureList []failure
	failure     struct {
		testName  string
		testError error
	}
)

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
	data, err := os.ReadFile(path)
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
func getResourceMeta(resource []byte) (string, error) {
	object := &unstructured.Unstructured{}
	if err := json.Unmarshal(resource, object); err != nil {
		return "", err
	}

	return object.GetName(), nil
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
func createResources(k8s *types.Cluster, resources resourceList, wc *types.KubeWarningCollector) error {
	for i, resource := range resources {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(resource, object); err != nil {
			return err
		}

		if wc != nil {
			wc.SetResource(object.GetName())
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
func updateResources(k8s *types.Cluster, resources resourceList, wc *types.KubeWarningCollector) error {
	for i, resource := range resources {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(resource, object); err != nil {
			return err
		}

		// If we have a warning collector, set the resource name so we can collect warnings.
		if wc != nil {
			wc.SetResource(object.GetName())
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

func deleteSelectedResources(k8s *types.Cluster, resources resourceList, deleteTargetNames []string) error {
	deleteTargets := resourceList{}

	for _, resource := range resources {
		name, err := getResourceMeta(resource)
		if err != nil {
			return err
		}

		if slices.Contains(deleteTargetNames, name) {
			deleteTargets = append(deleteTargets, resource)
		}
	}

	return deleteResources(k8s, deleteTargets)
}

// deleteResources deletes all defined resources.
func deleteResources(k8s *types.Cluster, resources resourceList) error {
	encryptionKeyResources := []*unstructured.Unstructured{}
	encryptionKeyGroupVersion := couchbasev2.SchemeGroupVersion.WithResource("couchbaseencryptionkeys")

	for _, resource := range resources {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(resource, object); err != nil {
			return err
		}

		groupVersion, err := getResource(k8s, object)
		if err != nil {
			return err
		}

		// We need to delete encryption keys last as they are dependent on other resources.
		if *groupVersion == encryptionKeyGroupVersion {
			encryptionKeyResources = append(encryptionKeyResources, object)
			continue
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

	for _, resource := range encryptionKeyResources {
		if _, err := k8s.DynamicClient.Resource(encryptionKeyGroupVersion).Namespace(k8s.Namespace).Get(context.Background(), resource.GetName(), metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				continue
			}

			return err
		}

		if err := k8s.DynamicClient.Resource(encryptionKeyGroupVersion).Namespace(k8s.Namespace).Delete(context.Background(), resource.GetName(), *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}

	return nil
}

// patchResources applies JSON patches to all defined resources.
func patchResources(resources resourceList, patches patchMap) error {
	for i, resource := range resources {
		name, err := getResourceMeta(resource)
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

	// operationDelete applies patches to objects, creates them and then deletes them.
	operationDelete
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

	// validationFile may be set, if left zero, then it will default to validation.yaml.
	validationFile string
}

func runValidationTest(t *testing.T, testDefs []testDef, validation validationContext) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t, framework.NoOperator)
	defer cleanup()

	if validation.validationFile == "" {
		validation.validationFile = "validation.yaml"
	}
	// Clean up resources that may have been left behind by a job that was interrupted.
	objectsPristine, err := loadResources("/resources/validation/" + validation.validationFile)
	if err != nil {
		e2eutil.Die(t, err)
	}

	// This is slow (entropy and modular exponentiation) so cache where possible,
	// this will make tests 4x faster!
	tlsCache := map[string]*e2eutil.TLSContext{}

	for i := range testDefs {
		test := testDefs[i]

		// Run each test case defined as a separate test so we have a way
		// of running them individually.

		var wc *types.KubeWarningCollector
		if len(test.expectedWarnings) > 0 {
			wc = types.NewKubeWarningCollector()
			kubernetes.Config.WarningHandler = wc
			kubernetes.DynamicClient = dynamic.NewForConfigOrDie(kubernetes.Config)
		}

		// nil checks in the validation test.

		t.Run(test.name, func(t *testing.T) {
			objects := objectsPristine.DeepCopy()

			// Delete anything we created.
			defer func() {
				_ = deleteResources(kubernetes, objects)

				// We initialise a new warning collector for each test, but let's reset it here to be safe.
				if wc != nil {
					wc.Reset()
				}
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
				object.SetNamespace(kubernetes.Namespace)

				// Do dynamic environment configuration.
				if err := unstructured.SetNestedField(object.Object, kubernetes.DefaultSecret.Name, "spec", "security", "adminSecret"); err != nil {
					e2eutil.Die(t, err)
				}

				// Do dynamic TLS configuration.
				ctx, ok := tlsCache[object.GetName()]
				if !ok {
					tlsOpts := &e2eutil.TLSOpts{
						ClusterName: object.GetName(),
						AltNames:    util_x509.MandatorySANs(object.GetName(), kubernetes.Namespace, true),
					}
					tlsOpts.AltNames = append(tlsOpts.AltNames, "*.example.com")

					if validation.tls == tlsStandard {
						tlsOpts.Source = e2eutil.TLSSourceCertManagerSecret
					}

					ctx = e2eutil.MustInitClusterTLS(t, kubernetes, tlsOpts)

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
				if found {
					if err != nil {
						e2eutil.Die(t, err)
					}
					for _, remoteCluster := range remoteClusters {
						rc, ok := remoteCluster.(map[string]interface{})
						if !ok {
							e2eutil.Die(t, fmt.Errorf("unexpected data type"))
						}
						rc["authenticationSecret"] = kubernetes.DefaultSecret.Name
					}
					if err := unstructured.SetNestedField(object.Object, remoteClusters, "spec", "xdcr", "remoteClusters"); err != nil {
						e2eutil.Die(t, err)
					}
				}

				// Do PVC configuration.
				pvcTemplates, found, err := unstructured.NestedSlice(object.Object, "spec", "volumeClaimTemplates")
				if found {
					if err != nil {
						e2eutil.Die(t, err)
					}
					for _, pvcTemplate := range pvcTemplates {
						pvct, ok := pvcTemplate.(map[string]interface{})
						if !ok {
							e2eutil.Die(t, fmt.Errorf("unexpected data type"))
						}
						if err := unstructured.SetNestedField(pvct, getStorageClass(t, kubernetes), "spec", "storageClassName"); err != nil {
							e2eutil.Die(t, err)
						}
					}
					if err := unstructured.SetNestedField(object.Object, pvcTemplates, "spec", "volumeClaimTemplates"); err != nil {
						e2eutil.Die(t, err)
					}
				}
				// Turn back into JSON.
				raw, err := json.Marshal(object)
				if err != nil {
					e2eutil.Die(t, err)
				}

				objects[i] = raw
			}

			// If we are applying a change or deleting a cluster we first need to create it. We don't need to set a warning collector here as we don't want to collect warnings for the initial resource creation.
			if validation.operation == operationApply {
				if err := createResources(kubernetes, objects, nil); err != nil {
					e2eutil.Die(t, err)
				}
			}

			// Patch the cluster specification
			if test.mutations != nil {
				if err := patchResources(objects, test.mutations); err != nil {
					e2eutil.Die(t, err)
				}

				// If we have a warning collector, track the resources that are being patched so we collect warnings for them.
				if wc != nil {
					for resource := range test.mutations {
						wc.TrackResource(resource)
					}
				}
			}

			// Execute the main test, update the new resource for verification.
			switch validation.operation {
			case operationCreate:
				err = createResources(kubernetes, objects, wc)
			case operationApply:
				err = updateResources(kubernetes, objects, wc)
			case operationDelete:
				err = createResources(kubernetes, objects, wc)
				err = utilErrors.Join(err, deleteSelectedResources(kubernetes, objects, test.deleteTargets))
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
						// match all expected errors as regex strings
						re := regexp.MustCompile(message)

						if !re.Match([]byte(err.Error())) {
							t.Logf("expected message: %v", message)
							t.Logf("actual message: %v", err)
							e2eutil.Die(t, fmt.Errorf("expected message not encountered"))
						}
					}
				}
			}

			// If we have initialised a warning collector, check we have received the expected warnings.
			if wc != nil {
				validateWarnings(t, wc, test.expectedWarnings)
			}
		})
	}
}

func validateWarnings(t *testing.T, wc *types.KubeWarningCollector, expectedWarnings []string) {
	actualWarnings := wc.Warnings()

	for _, expectedWarning := range expectedWarnings {
		re := regexp.MustCompile(expectedWarning)
		if !hasMatchingWarning(actualWarnings, re) {
			t.Logf("expected warning message: %v", expectedWarning)
			t.Logf("actual warnings: %v", actualWarnings)
			e2eutil.Die(t, fmt.Errorf("expected warning not encountered"))
		}
	}
}

// Helper function to check if any actual warning matches the expected pattern.
func hasMatchingWarning(actualWarnings []string, pattern *regexp.Regexp) bool {
	for _, actual := range actualWarnings {
		if pattern.MatchString(actual) {
			return true
		}
	}

	return false
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

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-701.yaml"})
}

func TestNegValidationCreateCouchbaseCluster(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateDifferentSecurityContexts",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Replace("/spec/securityContext",
						&corev1.PodSecurityContext{
							FSGroup: func(fsGrp int) *int64 {
								fsGrpInt64 := int64(fsGrp)
								return &fsGrpInt64
							}(133),
						}).
					Replace("/spec/security/podSecurityContext",
						&corev1.PodSecurityContext{
							FSGroup: func(fsGrp int) *int64 {
								fsGrpInt64 := int64(fsGrp)
								return &fsGrpInt64
							}(123),
						}),
			},
			shouldFail: true,
		},
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
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgrade", &couchbasev2.UpgradeSpec{RollingUpgrade: &couchbasev2.RollingUpgradeConstraints{MaxUpgradable: -1}})},
			shouldFail:     true,
			expectedErrors: []string{`spec.upgrade.rollingUpgrade.maxUpgradable`},
		},
		{
			name:           "TestValidateRollingUpgradeMaxUpgradablePercentUnderflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgrade", &couchbasev2.UpgradeSpec{RollingUpgrade: &couchbasev2.RollingUpgradeConstraints{MaxUpgradablePercent: "0%"}})},
			shouldFail:     true,
			expectedErrors: []string{`spec.upgrade.rollingUpgrade.maxUpgradablePercent`},
		},
		{
			name:           "TestValidateRollingUpgradeMaxUpgradablePercentOverflow",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgrade", &couchbasev2.UpgradeSpec{RollingUpgrade: &couchbasev2.RollingUpgradeConstraints{MaxUpgradablePercent: "9001%"}})}, // OVER 9000!!!
			shouldFail:     true,
			expectedErrors: []string{`spec.upgrade.rollingUpgrade.maxUpgradablePercent`},
		},
		{
			name:           "TestValidateUpgradeStrategyMutuallyExclusiveWithUpgradeProcess",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgradeStrategy", "ImmediateUpgrade").Replace("/spec/upgradeProcess", "DeltaRecovery")},
			shouldFail:     true,
			expectedErrors: []string{"cannot set spec.upgrade.upgradeStrategy to ImmediateUpgrade when spec.upgrade.upgradeProcess is set to InPlaceUpgrade"},
		},
		{
			name:           "TestValidateUpgradeUpgradeStrategyMutuallyExclusiveWithUpgradeProcess",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgrade", &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.InPlaceUpgrade, UpgradeStrategy: couchbasev2.ImmediateUpgrade})},
			shouldFail:     true,
			expectedErrors: []string{"cannot set spec.upgrade.upgradeStrategy to ImmediateUpgrade when spec.upgrade.upgradeProcess is set to InPlaceUpgrade"},
		},
		{
			name:             "TestValidateDeltaRecoveryDeprecated",
			mutations:        patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgradeProcess", "DeltaRecovery")},
			shouldFail:       false,
			expectedWarnings: []string{"DeltaRecovery is deprecated, please use InPlaceUpgrade instead"},
		},
		{
			name:             "TestValidateUpgradeDeltaRecoveryDeprecated",
			mutations:        patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgrade", &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.DeltaRecovery})},
			shouldFail:       false,
			expectedWarnings: []string{"DeltaRecovery is deprecated, please use InPlaceUpgrade instead"},
		},
		{
			name: "TestValidateInPlaceUpgradeWithOneDataNodeMultiNodeCluster",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgradeProcess", "InPlaceUpgrade").
				Replace("/spec/servers", []couchbasev2.ServerConfig{
					{
						Name:     "data_only",
						Services: []couchbasev2.Service{couchbasev2.DataService},
						Size:     1,
					},
					{
						Name:     "query_only",
						Services: []couchbasev2.Service{couchbasev2.QueryService},
						Size:     1,
					},
				})},
			shouldFail:     true,
			expectedErrors: []string{"cannot enable InPlaceUpgrade with one data service node in a multi-node cluster"},
		},
		{
			name: "TestValidateUpgradeInPlaceUpgradeWithOneDataNodeMultiNodeCluster",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgrade", &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.InPlaceUpgrade}).
				Replace("/spec/servers", []couchbasev2.ServerConfig{
					{
						Name:     "data_only",
						Services: []couchbasev2.Service{couchbasev2.DataService},
						Size:     1,
					},
					{
						Name:     "query_only",
						Services: []couchbasev2.Service{couchbasev2.QueryService},
						Size:     1,
					},
				})},
			shouldFail:     true,
			expectedErrors: []string{"cannot enable InPlaceUpgrade with one data service node in a multi-node cluster"},
		},
		{
			name: "TestValidateInPlaceUpgradeWithOneDataNodeMultiNodeClusterWarning",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgradeProcess", "InPlaceUpgrade").
				Replace("/spec/servers", []couchbasev2.ServerConfig{
					{
						Name:     "data_only",
						Services: []couchbasev2.Service{couchbasev2.DataService},
						Size:     1,
					},
				})},
			shouldFail:       false,
			expectedWarnings: []string{"It is not possible to perform an online In-place Upgrade for a single-node cluster, the cluster will be offline while being upgraded. Please use the Swap Rebalance method to keep the cluster online."},
		},
		{
			name: "TestValidateUpgradeInPlaceUpgradeWithOneDataNodeMultiNodeClusterWarning",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/upgrade", &couchbasev2.UpgradeSpec{UpgradeProcess: couchbasev2.InPlaceUpgrade}).
				Replace("/spec/servers", []couchbasev2.ServerConfig{
					{
						Name:     "data_only",
						Services: []couchbasev2.Service{couchbasev2.DataService},
						Size:     1,
					},
				})},
			shouldFail:       false,
			expectedWarnings: []string{"It is not possible to perform an online In-place Upgrade for a single-node cluster, the cluster will be offline while being upgraded. Please use the Swap Rebalance method to keep the cluster online."},
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
		{
			name: "TestValidateCouchbaseServerVersionNegBelowMin",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:6.6.5")},
			shouldFail:     true,
			expectedErrors: []string{"unsupported Couchbase version: 6.6.5, minimum version required: 7.0.0"},
		},
		{
			name: "TestInvalidCouchbaseClusterNameTooLong",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/metadata/name", "this-cluster-name-is-43-characters-long-now")},
			shouldFail:     true,
			expectedErrors: []string{"cluster name this-cluster-name-is-43-characters-long-now cannot be longer than 42 characters"},
		},
		{
			name: "TestInvalidCouchbaseClusterGenerateNameTooLong",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Remove("/metadata/name").Add("/metadata/generateName", "this-generated-name-will-be-too-long-now")},
			shouldFail:     true,
			expectedErrors: []string{" cannot be longer than 42 characters"},
		},
		{
			name: "TestInvalidCouchbaseClusterBackupImageEmptyWhenManaged",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/backup", couchbasev2.Backup{
					Managed: true,
					Image:   "",
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"spec.backup.image cannot be empty when spec.backup.managed is true"},
		},
		{
			name: "TestValidateDeprecatedNetworkingOptionsIPv4",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/addressFamily", couchbasev2.IPv4)},
			shouldFail:       false,
			expectedWarnings: []string{"IPv4Only should be used in place of IPv4 for spec.networking.addressFamily. IPv4 is deprecated and will be removed in a future release"},
		},
		{
			name: "TestValidateDeprecatedNetworkingOptionsIPv6",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/networking/addressFamily", couchbasev2.IPv6)},
			shouldFail:       false,
			expectedWarnings: []string{"IPv6Only should be used in place of IPv6 for spec.networking.addressFamily. IPv6 is deprecated and will be removed in a future release"},
		},
		{
			name: "TestValidateLoggingTLSCRDFieldsNotYetImplemented",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/logging", &couchbasev2.CouchbaseClusterLoggingSpec{
					Server: &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
						Sidecar: &couchbasev2.LogShipperSidecarSpec{
							TLS: &couchbasev2.LogShipperSidecarTLSSpec{
								MountPath:   "some-value",
								SecretNames: []string{"some-value", "some-other-value"},
							},
						},
					},
				})},
			shouldFail:       false,
			expectedWarnings: []string{"CouchbaseCluster spec.logging.server.sidecar.tls is not implemented in this operator version."},
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
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths(\[0\])?.path`},
		},
		{
			name:           "TestValidateTLSClientCertificatePathRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/networking/tls/clientCertificatePaths/0/path")},
			shouldFail:     true,
			expectedErrors: []string{`spec.networking.tls.clientCertificatePaths(\[0\])?.path`},
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
		{
			name:           "TestValidateNewTLSVersionsPre71Illegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/tlsMinimumVersion", "TLS1.3").Replace("/spec/image", "couchbase/server:7.0.1")},
			shouldFail:     true,
			expectedErrors: []string{`tls1.3 is only supported for Couchbase`},
		},
		{
			name:           "TestValidateOldTLSVersionsPost76Illegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/networking/tls/tlsMinimumVersion", "TLS1.0").Replace("/spec/image", "couchbase/server:7.6.1")},
			shouldFail:     true,
			expectedErrors: []string{`tls1.0 and tls1.1 are not supported`},
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
			expectedErrors: []string{`spec.servers(\[0\])?.services(\[1\])?`},
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
			expectedErrors: []string{`spec.servers(\[0\])?.services`},
		},
		{
			name:           "TestServerSizeRangeInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", -2)},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers(\[0\])?.size`},
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
			expectedErrors: []string{`spec.servers(\[2\])?.serverGroups`},
		},
		{
			name:           "TestNoServicelessClassBelow76",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/services", []string{})},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers(\[3\])?.services requires atleast one service`},
		},
		{
			name: "TestAdminServiceWithAnotherService",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/services", couchbasev2.ServiceList{couchbasev2.AdminService, couchbasev2.DataService}).
				Replace("/spec/image", "couchbase/server:7.6.2")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers(\[3\])?.services cannot contain the admin service and other services`},
		},
		{
			name:             "TestServerClassImageDeprecated",
			mutations:        patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/image", "couchbase/server:7.1.0")},
			expectedWarnings: []string{"spec.servers.image"},
			shouldFail:       false,
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
				`spec.servers(\[0\])?.default`,
				`spec.servers(\[1\])?.default`,
				`spec.servers(\[1\])?.data`,
				`spec.servers(\[1\])?.index`,
				`spec.servers(\[1\])?.analytics(\[0\])?`,
				`spec.servers(\[1\])?.analytics(\[1\])?`,
				`spec.servers(\[2\])?.default`,
				`spec.servers(\[4\])?.default`,
			},
		},
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_1",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/volumeMounts/logs", "couchbase")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers(\[0\])?.volumeMounts.default`},
		},
		{
			name:           "TestValidateLogsVolumeMountMutuallyExclusive_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/3/volumeMounts/default", "couchbase-log-pv")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers(\[3\])?.volumeMounts.default`},
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
			expectedErrors: []string{`spec.servers(\[1\])?.volumeMounts.data`},
		},
		{
			name:           "TestValidateServerServiceRequiredForVolumeMountIndex",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/servers/1/services/3").Remove("/spec/servers/1/services/1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers(\[1\])?.volumeMounts.index`},
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
			expectedErrors: []string{`spec.servers(\[1\])?.volumeMounts.analytics`},
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
			expectedErrors: []string{`spec.servers(\[1\])?.` + mntName},
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
			expectedErrors: []string{`spec.servers(\[0\])?.volumeMounts`},
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
		expectedErrors: []string{`spec.servers(\[0\])?.volumeMounts`},
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
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Add("/spec/logging/server", &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
						Enabled: true,
					}).Remove("/spec/servers/0/volumeMounts"), // only need to remove first as we validate only on that
			},
			shouldFail: true,
		},
		{
			name: "TestValidateLoggingIgnoresGCWithNoAudit",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
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
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
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
			name: "TestValidateLoggingIgnoresNativeGCWithNoAudit",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
						Enabled: false,
						Rotation: &couchbasev2.CouchbaseClusterLogRotationSpec{
							PruneAge: k8sutil.NewDurationS(30),
						},
					}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateLoggingFailsAuditNativeGCUnsupportedVersion",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
						Enabled: true,
						Rotation: &couchbasev2.CouchbaseClusterLogRotationSpec{
							PruneAge: k8sutil.NewDurationS(30),
						},
					}).
					Replace("/spec/image", "couchbase/server:7.2.0"),
			},
			shouldFail:     true,
			expectedErrors: []string{`only supported for server version 7.2.4`},
		},
		{
			name: "TestValidateLoggingFailsAuditNativeAndSidecarGC",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
						Enabled: true,
						GarbageCollection: &couchbasev2.CouchbaseClusterAuditGarbageCollectionSpec{
							Sidecar: &couchbasev2.CouchbaseClusterAuditCleanupSidecarSpec{
								Enabled: true,
							},
						},
						Rotation: &couchbasev2.CouchbaseClusterLogRotationSpec{
							PruneAge: k8sutil.NewDurationS(30),
						},
					}).
					Replace("/spec/image", "couchbase/server:7.2.4"),
			},
			shouldFail:     true,
			expectedErrors: []string{`mutually exclusive`},
		},
		{
			name: "TestValidateLoggingFailsAuditNativeGCWithNoPersistentVolume",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
						Enabled: true,
						Rotation: &couchbasev2.CouchbaseClusterLogRotationSpec{
							PruneAge: k8sutil.NewDurationS(30),
						},
					}).
					Replace("/spec/image", "couchbase/server:7.2.4").
					Remove("/spec/servers/0/volumeMounts"),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers.volumeMounts`},
		},
		{
			name: "TestValidateLoggingFailsAuditNativeGCOverMaximum",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Add("/spec/logging/audit", &couchbasev2.CouchbaseClusterAuditLoggingSpec{
						Enabled: true,
						Rotation: &couchbasev2.CouchbaseClusterLogRotationSpec{
							PruneAge: k8sutil.NewDurationS(35791395),
						},
					}).
					Replace("/spec/image", "couchbase/server:7.2.4"),
			},
			shouldFail:     true,
			expectedErrors: []string{`has a maximum of`},
		},
		{
			name: "TestValidateLoggingFailsForInvalidAuditUser",
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
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
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
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
			expectedErrors: []string{`autoCompaction.databaseFragmentationThreshold.percent`},
		},
		{
			name:           "TestValidateAutoCompactionMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", 101)},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.databaseFragmentationThreshold.percent`},
		},
		{
			name:           "TestValidateStartTimeIllegal",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/timeWindow/start", "26:00")},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.timeWindow.start`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMinimum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/tombstonePurgeInterval", "1m")},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.tombstonePurgeInterval`},
		},
		{
			name:           "TestValidateTombstonePurgeIntervalMaximum",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/tombstonePurgeInterval", "2400h")},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.tombstonePurgeInterval`},
		},
		{
			name:           "TestValidateAutoCompactionInvalidStartEqualsEnd",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/timeWindow/start", "07:30")},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.timeWindow.start cannot be the same as autoCompaction.timeWindow.end`},
		},
		{
			name:           "TestValidateAutoCompactionInvalidStartMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/autoCompaction/timeWindow/start")},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.timeWindow.start`},
		},
		{
			name:           "TestValidateAutoCompactionInvalidEndMissing",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/autoCompaction/timeWindow/end")},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.timeWindow.end`},
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
			name:           "TestValidateAutoFailoverTimeoutUnderflowWithCB76",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", "0s").Replace("/spec/image", "couchbase/server:7.6.4")},
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
			name:           "TestValidateAutoFailoverMaxCountLessThan4",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverMaxCount", 4).Replace("/spec/image", "couchbase/server:7.0.4")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoFailoverMaxCount`},
		},
		{
			name:           "TestValidateAutoFailoverMaxCountOver4On71+",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverMaxCount", 4).Replace("/spec/image", "couchbase/server:7.2.6")},
			shouldFail:     false,
			expectedErrors: []string{},
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
		{
			name:           "TestValidateSynchronizationNoLabel",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/buckets/managed", false).Add("/spec/buckets/synchronize", true)},
			shouldFail:     true,
			expectedErrors: []string{`spec.buckets.selector`},
		},
		{
			name:           "TestValidateIndexerNumberOfReplicaInValid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/numReplica", "-1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.numReplica`},
		},
		{
			name:           "TestValidateIndexerNumberOfReplicaGreaterThanIndexPods",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/numReplica", 10)},
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.indexer.numReplica 10 cannot be greater or equal to the number of index pods 3"},
		},
		{
			name:           "TestValidateIndexerNumberOfReplicaEqualIndexPods",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/numReplica", 3)},
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.indexer.numReplica 3 cannot be greater or equal to the number of index pods 3"},
		},
		{
			name:           "TestValidateEnablePageBloomFilterPre71Error",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/enablePageBloomFilter", true).Replace("/spec/image", "couchbase/server:7.0.1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.enablePageBloomFilter requires Couchbase Server version 7.1.0 or later`},
		},
		{
			name:           "TestValidateEnableShardAffinityPre76Error",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/enableShardAffinity", true).Replace("/spec/image", "couchbase/server:7.1.0")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.enableShardAffinity requires Couchbase Server version 7.6.0 or later`},
		},
		{
			name:           "TestValidateDeferBuildPre80Error",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/deferBuild", true).Replace("/spec/image", "couchbase/server:7.6.3")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.deferBuild requires Couchbase Server version 8.0.0 or later`},
		},
		{
			name:           "TestValidateIndexerRedistributeIndexesInValid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/redistributeIndexes", "slim-shady")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.indexer.redistributeIndexes`},
		},
		{
			name: "TestValidateAutoCompactionMagmaFragmentationPercentageMinimum",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.1.0").
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/autoCompaction.magmaFragmentationPercentage": "1",
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.magmaFragmentationPercentage must be between 10 and 100`},
		},
		{
			name: "TestValidateAutoCompactionMagmaFragmentationPercentageMaximum",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.1.0").
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/autoCompaction.magmaFragmentationPercentage": "101",
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.magmaFragmentationPercentage must be between 10 and 100`},
		},
		{
			name: "TestValidateAutoCompactionMagmaFragmentationPercentageUnsupportedVersion",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/autoCompaction.magmaFragmentationPercentage": "15",
				}).
				Replace("/spec/image", "couchbase/server:7.0.1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.magmaFragmentationPercentage is only supported for Couchbase 7.1.0+`},
		},
		{
			name: "TestValidateAutoCompactionMagmaFragmentationPercentageCRDFieldMinimum",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.1.0").
				Replace("/spec/cluster/autoCompaction/magmaFragmentationPercentage", 1)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.magmaFragmentationPercentage`},
		},
		{
			name: "TestValidateAutoCompactionMagmaFragmentationPercentageCRDFieldMaximum",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.1.0").
				Replace("/spec/cluster/autoCompaction/magmaFragmentationPercentage", 101)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.magmaFragmentationPercentage`},
		},
		{
			name: "TestValidateAutoCompactionMagmaFragmentationPercentageCRDFieldUnsupportedVersion",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.0.1").
				Replace("/spec/cluster/autoCompaction/magmaFragmentationPercentage", 15)},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.autoCompaction.magmaFragmentationPercentage is only supported for Couchbase 7.1.0+`},
		},
		{
			name: "TestValidateAutoCompactionMagmaFragmentationPercentageCRDFieldValid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.1.0").
				Replace("/spec/cluster/autoCompaction/magmaFragmentationPercentage", 50)},
			shouldFail: false,
		},
		{
			name: "TestValidateDiskUsageLimitUnsupportedVersion",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.6.3").
				Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{
					DiskUsageLimit: &couchbasev2.DiskUsageLimit{
						Enabled: util.BoolPtr(true),
					},
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.data.diskUsageLimit requires Couchbase Server version 8.0.0 or later`},
		},
		{
			name: "TestValidateDataThreadSettingsFixedValueInvalid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.0.3").
				Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{
					ReaderThreads: util.IntOrStringPtr("1"),
					WriterThreads: util.IntOrStringPtr("65"),
				})},
			shouldFail: true,
			expectedErrors: []string{`spec.cluster.data.readerThreads must either be between 4 and 64 or one of default, disk_io_optimized for Couchbase server version 7.0.3`,
				`spec.cluster.data.writerThreads must either be between 4 and 64 or one of default, disk_io_optimized for Couchbase server version 7.0.3`},
		},
		{
			name: "TestValidateDataThreadSettingsFixedValueTooHigh",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.6.5").
				Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{
					ReaderThreads: util.IntOrStringPtr("1"),
					WriterThreads: util.IntOrStringPtr("65"),
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.data.writerThreads must either be between 1 and 64 or one of default, disk_io_optimized for Couchbase server version 7.6.5`},
		},
		{
			name: "TestValidateDataThreadSettingsSettingInvalidSettingsFor70",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.6.3").
				Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{
					ReaderThreads: util.IntOrStringPtr("balanced"),
					WriterThreads: util.IntOrStringPtr("disk_io_optimized"),
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.data.readerThreads must either be between 1 and 64 or one of default, disk_io_optimized for Couchbase server version 7.6.3`},
		},
		{
			name: "TestValidateDataThreadSettingsSettingInvalidSettingsFor80",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0").
				Add("/spec/cluster/data", &couchbasev2.CouchbaseClusterDataSettings{
					ReaderThreads: util.IntOrStringPtr("default"),
					WriterThreads: util.IntOrStringPtr("0"),
				})},
			shouldFail: true,
			expectedErrors: []string{`spec.cluster.data.readerThreads must either be between 1 and 64 or one of balanced, disk_io_optimized for Couchbase server version 8.0.0`,
				`spec.cluster.data.writerThreads must either be between 1 and 64 or one of balanced, disk_io_optimized for Couchbase server version 8.0.0`},
		},
		{
			name: "TestValidateAppTelemetryInvalidMaxScrapeClientsPerNode",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0").
				Add("/spec/cluster/appTelemetry", &couchbasev2.CouchbaseClusterAppTelemetrySettings{
					MaxScrapeClientsPerNode: -1,
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.appTelemetry.maxScrapeClientsPerNode`},
		},
		{
			name: "TestValidateAppTelemetryMaxScrapeClientsPerNodeExceedsMaximum",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0").
				Add("/spec/cluster/appTelemetry", &couchbasev2.CouchbaseClusterAppTelemetrySettings{
					MaxScrapeClientsPerNode: 1025,
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.appTelemetry.maxScrapeClientsPerNode`},
		},
		{
			name: "TestValidateAppTelemetryInvalidScrapeIntervalSeconds",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0").
				Add("/spec/cluster/appTelemetry", &couchbasev2.CouchbaseClusterAppTelemetrySettings{
					ScrapeIntervalSeconds: -1,
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.appTelemetry.scrapeIntervalSeconds`},
		},
		{
			name: "TestValidateAppTelemetryScrapeIntervalSecondsBelowMinimum",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0").
				Add("/spec/cluster/appTelemetry", &couchbasev2.CouchbaseClusterAppTelemetrySettings{
					ScrapeIntervalSeconds: 59,
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.appTelemetry.scrapeIntervalSeconds`},
		},
		{
			name: "TestValidateAppTelemetryScrapeIntervalSecondsExceedsMaximum",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0").
				Add("/spec/cluster/appTelemetry", &couchbasev2.CouchbaseClusterAppTelemetrySettings{
					ScrapeIntervalSeconds: 601,
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.appTelemetry.scrapeIntervalSeconds`},
		},
		{
			name: "TestValidateAppTelemetryBothFieldsInvalid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0").
				Add("/spec/cluster/appTelemetry", &couchbasev2.CouchbaseClusterAppTelemetrySettings{
					Enabled:                 true,
					MaxScrapeClientsPerNode: 2000,
					ScrapeIntervalSeconds:   30,
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.cluster.appTelemetry.maxScrapeClientsPerNode`, `spec.cluster.appTelemetry.scrapeIntervalSeconds`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestCBVersionSpecificPosValidationsCreateCouchbaseClusterSettings(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateAutoFailoverTimeoutNoUnderflowWithCB76",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoFailoverTimeout", "3s").Replace("/spec/image", "couchbase/server:7.6.4")},
			shouldFail:     false,
			expectedErrors: []string{`spec.cluster.autoFailoverTimeout`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestValidationCreateCouchbaseBackup(t *testing.T) {
	useIAM := true
	testDefs := []testDef{
		{
			name: "TestValidateBackupSecretS3",
			mutations: patchMap{
				"backup0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", &couchbasev2.ObjectStoreSpec{
						URI:    "s3://blah",
						Secret: "test-example-s3",
					}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateBackupSecretAzure",
			mutations: patchMap{
				"backup0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", couchbasev2.ObjectStoreSpec{
						URI:    "az://blah",
						Secret: "test-example-az",
					}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateBackupSecretGCP",
			mutations: patchMap{
				"backup0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", couchbasev2.ObjectStoreSpec{
						URI:    "gs://blah",
						Secret: "test-example-gs",
					}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateBackupSecretS3Region",
			mutations: patchMap{
				"backup0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", couchbasev2.ObjectStoreSpec{
						URI:    "s3://blah",
						Secret: "test-example-s3-region-only",
						UseIAM: &useIAM,
					}),
			},
			shouldFail: false,
		},
		{
			name:       "TestValidateBackupS3BucketSubpath",
			mutations:  patchMap{"backup0": jsonpatch.NewPatchSet().Replace("/spec/s3bucket", "s3://hello/beans")},
			shouldFail: false,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}
func TestValidationCreateCouchbaseBackupRestore(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateBackupRestoreSecretS3",
			mutations: patchMap{
				"restore0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", &couchbasev2.ObjectStoreSpec{
						URI:    "s3://blah",
						Secret: "test-example-s3",
					}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateBackupRestoreSecretAzure",
			mutations: patchMap{
				"restore0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", couchbasev2.ObjectStoreSpec{
						URI:    "az://blah",
						Secret: "test-example-az",
					}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateBackupRestoreSecretGCP",
			mutations: patchMap{
				"restore0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", couchbasev2.ObjectStoreSpec{
						URI:    "gs://blah",
						Secret: "test-example-gs",
					}),
			},
			shouldFail: false,
		},
		{
			name:       "TestValidateBackupS3BucketSubpath",
			mutations:  patchMap{"restore0": jsonpatch.NewPatchSet().Replace("/spec/s3bucket", "s3://hello/beans")},
			shouldFail: false,
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
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.uuid`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidName",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "illegal_dns")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "starsky-and-hutch.tv:huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidIpv6",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "0[2001:0000:1234:0000:0000:C1C0:ABCD:0876]")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidPrefix",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "hoopy://127.0.0.1")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameOnlyHttpPrefix",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameOnlyHttpsPrefix",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameOnlyCouchbasePrefix",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameOnlyCouchbasesPrefix",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
		},
		{
			name:           "TestValidateXDCRHostnameInvalidIpv6BadCompression", // can only use one set of :: in an ipv6 address
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[3ffe:b00::1::a]")},
			shouldFail:     true,
			expectedErrors: []string{`spec.xdcr.remoteClusters(\[0\])?.hostname`},
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

func TestValidationXDCRConflictLogging(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateXDCRConflictLoggingInvalidVersion",
			mutations:      patchMap{"cluster1": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.0").Replace("/status/clusterVersion", "7.6.0")},
			shouldFail:     true,
			expectedErrors: []string{`conflict logging requires cluster version 8.0.0 or greater`},
		},
		{
			name:           "TestValidateXDCRConflictLoggingCrossClusterVersioningDisabled",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/enableCrossClusterVersioning", false)},
			shouldFail:     true,
			expectedErrors: []string{`must have cross cluster versioning enabled to enable conflict logging`},
		},
		{
			name:           "TestValidateXDCRConflictLoggingInvalidBucket",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/conflictLogging/logCollection/bucket", "bucket8001")},
			shouldFail:     true,
			expectedErrors: []string{`bucket bucket8001 not found`},
		},
		{
			name: "TestValidateXDCRConflictLoggingManagedBucketsDisabled",
			mutations: patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/conflictLogging/logCollection/bucket", "bucket8001"),
				"cluster1": jsonpatch.NewPatchSet().Replace("/spec/buckets/managed", false)},
			shouldFail: false,
		},
		{
			name:       "TestValidateXDCRConflictLoggingMissingScope",
			mutations:  patchMap{"replication0": jsonpatch.NewPatchSet().Remove("/spec/conflictLogging/logCollection/scope")},
			shouldFail: true,
		},
		{
			name:       "TestValidateXDCRConflictLoggingMissingCollection",
			mutations:  patchMap{"replication0": jsonpatch.NewPatchSet().Remove("/spec/conflictLogging/logCollection/collection")},
			shouldFail: true,
		},
		{
			name:       "TestValidateXDCRConflictLoggingRuleCollectionMissingScope",
			mutations:  patchMap{"replication0": jsonpatch.NewPatchSet().Remove("/spec/conflictLogging/loggingRules/customCollectionRules/0/logCollection/scope")},
			shouldFail: true,
		},
		{
			name:       "TestValidateXDCRConflictLoggingRuleCollectionMissingCollection",
			mutations:  patchMap{"replication0": jsonpatch.NewPatchSet().Remove("/spec/conflictLogging/loggingRules/customCollectionRules/0/logCollection/collection")},
			shouldFail: true,
		},
		{
			name:       "TestValidateXDCRConflictLoggingRuleWithoutCollection",
			mutations:  patchMap{"replication0": jsonpatch.NewPatchSet().Remove("/spec/conflictLogging/loggingRules/customCollectionRules/0/collection")},
			shouldFail: false,
		},
		{
			name:       "TestValidateXDCRConflictLoggingRuleWithoutScope",
			mutations:  patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/conflictLogging/loggingRules/customCollectionRules/0/scope", "")},
			shouldFail: true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-80.yaml"})
}
func TestValidationCreateCouchbaseClusterXDCR(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateXDCRHostnameHttpIpv4",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://127.0.0.1")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv4WithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://127.0.0.1:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv4WithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://127.0.0.1:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv4",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://127.0.0.1")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv4WithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://127.0.0.1:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv4WithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://127.0.0.1:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv4",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://127.0.0.1")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv4WithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://127.0.0.1:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv4WithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://127.0.0.1:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv4",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://127.0.0.1")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv4WithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://127.0.0.1:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv4WithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://127.0.0.1:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv4",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "127.0.0.1")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv4WithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "127.0.0.1:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpHostname",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://abc.example.com")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpHostnameWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://abc.example.com:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpHostnameWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://abc.example.com:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsHostname",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://abc.example.com")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsHostnameWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://abc.example.com:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsHostnameWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://abc.example.com:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseHostname",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://abc.example.com")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseHostnameWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://abc.example.com:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseHostnameWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://abc.example.com:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesHostname",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://abc.example.com")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesHostnameWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://abc.example.com:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesHostnameWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://abc.example.com:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawHostname",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "abc.example.com")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawHostnameWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "abc.example.com:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawHostnameWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "abc.example.com:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6Http",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6HttpWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6HttpWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv6",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6HttpsWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6HttpsWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv6",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6CouchbaseWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6CouchbaseWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv6",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6CouchbasesWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6CouchbasesWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv6",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6RawWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameIpv6RawWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[2001:0000:0dea:C1AB:0000:00D0:ABCD:004E]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv6OmitPreceedingZeroes",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[1050:0:0:0:5:600:300c:326b]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv6OmitPreceedingZeroesWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[1050:0:0:0:5:600:300c:326b]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv6OmitPreceedingZeroesWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[1050:0:0:0:5:600:300c:326b]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv6OmitPreceedingZeroes",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[1050:0:0:0:5:600:300c:326b]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv6OmitPreceedingZeroesWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[1050:0:0:0:5:600:300c:326b]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv6OmitPreceedingZeroesWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[1050:0:0:0:5:600:300c:326b]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv6OmitPreceedingZeroes",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[1050:0:0:0:5:600:300c:326b]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv6OmitPreceedingZeroesWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[1050:0:0:0:5:600:300c:326b]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv6OmitPreceedingZeroesWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[1050:0:0:0:5:600:300c:326b]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv6OmitPreceedingZeroes",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[1050:0:0:0:5:600:300c:326b]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv6OmitPreceedingZeroesWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[1050:0:0:0:5:600:300c:326b]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv6OmitPreceedingZeroesWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[1050:0:0:0:5:600:300c:326b]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv6OmitPreceedingZeroes",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[1050:0:0:0:5:600:300c:326b]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv6OmitPreceedingZeroesWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[1050:0:0:0:5:600:300c:326b]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv6OmitPreceedingZeroesWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[1050:0:0:0:5:600:300c:326b]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv6Compact",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[ff06::c3]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv6CompactWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[ff06::c3]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpIpv6CombactWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "http://[ff06::c3]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv6Compact",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[ff06::c3]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv6CompactWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[ff06::c3]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameHttpsIpv6CombactWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "https://[ff06::c3]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv6Compact",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[ff06::c3]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv6CompactWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[ff06::c3]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbaseIpv6CombactWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbase://[ff06::c3]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv6Compact",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[ff06::c3]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv6CompactWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[ff06::c3]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameCouchbasesIpv6CombactWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "couchbases://[ff06::c3]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv6Compact",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[ff06::c3]")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv6CompactWithPort",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[ff06::c3]:8091")},
			shouldFail:     false,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateXDCRHostnameRawIpv6CombactWithPortAndNetwork",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/xdcr/remoteClusters/0/hostname", "[ff06::c3]:8091?network=abc")},
			shouldFail:     false,
			expectedErrors: []string{},
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
			expectedErrors: []string{`bucket memory allocation \(1001Mi\) exceeds data service quota \(600Mi\) on cluster cluster`},
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
		{
			name:           "TestValidateBucketInvalidCouchbaseStorageBackend",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "abracadabra")},
			shouldFail:     true,
			expectedErrors: []string{`spec.storageBackend`},
		},
		{
			name: "TestValidateBucketStorageBackendMagmaNeedsMoreThanDefaultMemoryQuotaPre80",
			mutations: patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma"),
				"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.1.2")},
			shouldFail:     true,
			expectedErrors: []string{`spec.memoryQuota must be greater than or equal to 1024Mi for magma buckets pre Couchbase Server 8.0.0`},
		},
		{
			name: "TestValidateBucketStorageBackendMagmaNeedsMoreThanOrEqualTo1024MiPre80",
			mutations: patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma").Replace("/spec/memoryQuota", "1023Mi"),
				"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.7")},
			shouldFail:     true,
			expectedErrors: []string{`spec.memoryQuota must be greater than or equal to 1024Mi for magma buckets pre Couchbase Server 8.0.0`},
		},
		{
			name:           "TestValidateBucketStorageBackendMagmaInvalidEvictionPolicy",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma").Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)},
			shouldFail:     true,
			expectedErrors: []string{`spec.evictionPolicy must be fullEviction for magma buckets`},
		},
		{
			name: "TestValidateHistoryRetentionBytesBelowWorkingRange",
			mutations: patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma").Add("/spec/historyRetention", &couchbasev2.HistoryRetentionSettings{
				Bytes: uint64(2147483647),
			})},
			shouldFail:     true,
			expectedErrors: []string{`historyRetention.bytes value 2147483647 is less than minimum working value of 2147483648`},
		},
		{
			name:           "TestValidateBucketAutoCompactionPurgeIntervalTooShort",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{TombstonePurgeInterval: &metav1.Duration{Duration: time.Duration(59) * time.Minute}})},
			shouldFail:     true,
			expectedErrors: []string{"autoCompaction.tombstonePurgeInterval in body should be greater than or equal to 1h"},
		},
		{
			name:           "TestValidateBucketAutoCompactionPurgeIntervalTooLong",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{TombstonePurgeInterval: &metav1.Duration{Duration: time.Duration(1441) * time.Hour}})},
			shouldFail:     true,
			expectedErrors: []string{"autoCompaction.tombstonePurgeInterval in body should be less than or equal to 60d"},
		},
		{
			name:           "TestValidateBucketAutoCompactionStartTimeIllegal",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{TimeWindow: &couchbasev2.TimeWindow{Start: util.StrPtr("26:00")}})},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.timeWindow.start: Invalid value`},
		},
		{
			name:           "TestValidateBucketAutoCompactionDatabaseFragmentationThresholdSizeZero",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{DatabaseFragmentationThreshold: &couchbasev2.DatabaseFragmentationThresholdBucket{Size: k8sutil.NewResourceQuantityMi(0)}})},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.databaseFragmentationThreshold.size should be greater than 0Mi`},
		},
		{
			name:           "TestValidateBucketAutoCompactionDatabaseFragmentationThresholdSizeNegative",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{DatabaseFragmentationThreshold: &couchbasev2.DatabaseFragmentationThresholdBucket{Size: k8sutil.NewResourceQuantityMi(-1)}})},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.databaseFragmentationThreshold.size should be greater than 0Mi`},
		},
		{
			name:           "TestValidateBucketAutoCompactionViewFragmentationThresholdSizeZero",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{ViewFragmentationThreshold: &couchbasev2.ViewFragmentationThresholdBucket{Size: k8sutil.NewResourceQuantityMi(0)}})},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.viewFragmentationThreshold.size should be greater than 0Mi`},
		},
		{
			name:           "TestValidateBucketAutoCompactionViewFragmentationThresholdSizeNegative",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{ViewFragmentationThreshold: &couchbasev2.ViewFragmentationThresholdBucket{Size: k8sutil.NewResourceQuantityMi(-1)}})},
			shouldFail:     true,
			expectedErrors: []string{`autoCompaction.viewFragmentationThreshold.size should be greater than 0Mi`},
		},
		{
			name: "TestValidateSampleBucketConflictResolutionInvalid",
			mutations: patchMap{"bucket1": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/sampleBucket": "true",
				}).Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionTimestamp)},
			shouldFail:     true,
			expectedErrors: []string{`spec.conflictResolution must be set to seqno for sample buckets`},
		},
		{
			name: "TestValidateSampleBucketEnableIndexReplicaInvalid",
			mutations: patchMap{"bucket1": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/sampleBucket": "true",
				}).Replace("/spec/enableIndexReplica", true)},
			shouldFail:     true,
			expectedErrors: []string{`spec.enableIndexReplica must be set to false for sample buckets`},
		},
		{
			name: "TestValidateSampleBucketMemoryQuotaExceeded",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Add("/metadata/annotations", map[string]string{
						"cao.couchbase.com/sampleBucket": "true",
					}),
				"bucket0": jsonpatch.NewPatchSet().
					Add("/metadata/annotations", map[string]string{
						"cao.couchbase.com/sampleBucket": "true",
					}),
				"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/dataServiceMemoryQuota", "256Mi")},
			shouldFail:     true,
			expectedErrors: []string{`sample buckets have a memory quota of 200Mi`},
		},
		{
			name: "TestValidateCreateMagmaBucketInvalidClusterSupport",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/storageBackend", "magma").
					Replace("/spec/memoryQuota", "1024Mi").
					Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyFullEviction),
				"cluster": jsonpatch.NewPatchSet().
					Replace("/spec/image", "couchbase/server:7.1.1").
					Replace("/spec/cluster/dataServiceMemoryQuota", "2Gi"),
			},
			shouldFail:     true,
			expectedErrors: []string{`search, eventing or analytics services cannot be used with magma buckets below CB Server 7.1.2`},
		},
		{
			name: "TestValidateCreateMagmaBucketInvalidClusterVersion",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/storageBackend", "magma").
					Replace("/spec/memoryQuota", "1024Mi").
					Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyFullEviction),
				"cluster": jsonpatch.NewPatchSet().
					Replace("/spec/image", "couchbase/server:7.0.0").
					Replace("/spec/cluster/dataServiceMemoryQuota", "2Gi"),
			},
			shouldFail:     true,
			expectedErrors: []string{`magma storage backend requires Couchbase Server version 7.1.0 or later`},
		},
		{
			name: "TestValidateMultipleBucketsSameName",
			mutations: patchMap{
				"bucket2": jsonpatch.NewPatchSet().Add("/spec/name", "bucket0"),
			},
			shouldFail:     true,
			expectedErrors: []string{"defined multiple times for cluster"},
		},
		{
			name: "TestValidateMultipleBucketTypesSameName",
			mutations: patchMap{
				"bucket3": jsonpatch.NewPatchSet().Add("/spec/name", "bucket0"),
			},
			shouldFail:     true,
			expectedErrors: []string{"defined multiple times for cluster"},
		},
		{
			name: "TestValidateCreateMagmaBucketInvalidVersion",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/storageBackend", "magma").
					Replace("/spec/memoryQuota", "1024Mi").
					Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyFullEviction),
				"cluster": jsonpatch.NewPatchSet().
					Replace("/spec/image", "couchbase/server:7.0.0").
					Replace("/spec/cluster/dataServiceMemoryQuota", "2Gi"),
			},
			shouldFail:     true,
			expectedErrors: []string{`magma storage backend requires Couchbase Server version 7.1.0 or later`},
		},
		{
			name: "TestValidateBucketAutoCompactionMagmaFragmentationPercentageMinimum",
			mutations: patchMap{"bucket1": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/autoCompaction.magmaFragmentationPercentage": "1",
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.autoCompaction.magmaFragmentationPercentage must be between 10 and 100`},
		},
		{
			name: "TestValidateBucketAutoCompactionMagmaFragmentationPercentageMaximum",
			mutations: patchMap{"bucket1": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/autoCompaction.magmaFragmentationPercentage": "101",
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.autoCompaction.magmaFragmentationPercentage must be between 10 and 100`},
		},
		{
			name: "TestValidateBucketAutoCompactionMagmaFragmentationPercentageCRDFieldMinimum",
			mutations: patchMap{"bucket1": jsonpatch.NewPatchSet().
				Add("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{MagmaFragmentationThresholdPercentage: util.IntPtr(1)})},
			shouldFail:     true,
			expectedErrors: []string{`spec.autoCompaction.magmaFragmentationPercentage in body should be greater than or equal to 10`},
		},
		{
			name: "TestValidateBucketAutoCompactionMagmaFragmentationPercentageCRDFieldMaximum",
			mutations: patchMap{"bucket1": jsonpatch.NewPatchSet().
				Add("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{MagmaFragmentationThresholdPercentage: util.IntPtr(101)})},
			shouldFail:     true,
			expectedErrors: []string{`spec.autoCompaction.magmaFragmentationPercentage in body should be less than or equal to 100`},
		},
		{
			name: "TestValidateBucketAutoCompactionMagmaFragmentationPercentageCRDFieldValid",
			mutations: patchMap{"bucket1": jsonpatch.NewPatchSet().
				Add("/spec/autoCompaction", couchbasev2.AutoCompactionSpecBucket{MagmaFragmentationThresholdPercentage: util.IntPtr(50)})},
			shouldFail: false,
		},
		{
			name: "TestValidateBucketEnableCrossClusterVersioningInvalidVersionAnnotation",
			mutations: patchMap{"bucket7": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/enableCrossClusterVersioning": "false",
				}),
				"cluster": jsonpatch.NewPatchSet().
					Replace("/spec/image", "couchbase/server:7.2.0"),
			},
			shouldFail:     true,
			expectedErrors: []string{"enableCrossClusterVersioning requires Couchbase Server version 7.6.0 or later"},
		},
		{
			name: "TestValidateBucketInvalidMemoryWatermarks",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/memoryLowWatermark", 74).
					Replace("/spec/memoryHighWatermark", 62),
			},
			shouldFail:     true,
			expectedErrors: []string{"spec.memoryLowWatermark"},
		},
		{
			name: "TestValidateBucketInvalidLowMemoryWatermark",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/memoryLowWatermark", 12),
			},
			shouldFail: true,
		},
		{
			name: "TestValidateBucketInvalidHighMemoryWatermark",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/memoryHighWatermark", 1212),
			},
			shouldFail: true,
		},
		{
			name: "TestValidateBucketInvalidWarmupBehavior",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/warmupBehavior", "invalid"),
			},
			shouldFail:     true,
			expectedErrors: []string{"spec.warmupBehavior"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestBucketCrossClusterVersioningChangeConstraints(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateBucketCrossClusterVersioningChangeConstraints",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().
					Add("/metadata/annotations", map[string]string{
						"cao.couchbase.com/enableCrossClusterVersioning": "true",
					}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateBucketCrossClusterVersioningChangeConstraintsNeg",
			mutations: patchMap{
				"bucket2": jsonpatch.NewPatchSet().
					Add("/metadata/annotations", map[string]string{
						"cao.couchbase.com/enableCrossClusterVersioning": "false",
					}),
			},
			shouldFail:     true,
			expectedErrors: []string{"enableCrossClusterVersioning cannot be disabled once enabled"},
		},
	}
	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "validation-76.yaml"})
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

func TestWarningDeprecatedMemcachedBucket(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateDeprecatedMemcachedBucket",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0"),
				// Empty patch to ensure the bucket is tracked by the warning collector
				"bucket3": jsonpatch.NewPatchSet(),
			},
			shouldFail:       false,
			expectedWarnings: []string{`memcached buckets are deprecated`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-76.yaml"})
}

func TestUpgradeInvalidMemcachedBucket(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateDeprecatedMemcachedBucket",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:8.0.0"),
			},
			shouldFail:     true,
			expectedErrors: []string{`cluster has memcached buckets`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "validation-76.yaml"})
}

func TestNegValidationCreateCouchbaseReplication(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestValidateXDCRReplicationBucketExists",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/bucket", "huggy-bear")},
			shouldFail:     true,
			expectedErrors: []string{`bucket huggy-bear referenced by spec.bucket in couchbasereplications.couchbase.com/replication0 must be valid: bucket huggy-bear not found`},
		},
		{
			name:           "TestValidateXDCRReplicationBucketTypeInvalid",
			mutations:      patchMap{"replication0": jsonpatch.NewPatchSet().Replace("/spec/bucket", "bucket2")},
			shouldFail:     true,
			expectedErrors: []string{`bucket bucket2 referenced by spec.bucket in couchbasereplications.couchbase.com/replication0 must be valid: memcached bucket bucket2 cannot be replicated`},
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
		// Scopes and collections tests - this is only validation checks so does not actually need server 7 to run.
		// For replication we can source and target either a scope or a scope.collection which is
		// referred to as a keyspace. We use the same validation as the ScopeOrCollectionName type
		// but also support usage of _default scopes and collections.
		// https://docs.couchbase.com/server/current/learn/clusters-and-availability/xdcr-with-scopes-and-collections.html
		//
		// This means the following are allowed:
		// _default._default
		// _default
		// scope.collection
		// scope
		// scope._default
		// Whilst the following are not:
		// _scopefail
		// scope._collectionfail
		// scopefail.
		//
		// See: https://regex101.com/r/6b4mKs/1
		{
			name:           "TestValidateXDCRKeyspaceTooShort",
			mutations:      patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().Replace("/explicitMapping/allowRules/0/sourceKeyspace/scope", "")},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[0\])?.sourceKeyspace`}, // the regex validation does not give you the array index
		},
		{
			name:           "TestValidateXDCRKeyspaceTooLong",
			mutations:      patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().Replace("/explicitMapping/allowRules/0/targetKeyspace/collection", "123456789012345678901234567890.1234567890123456789012345678901")},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[0\])?.targetKeyspace`},
		},
		{
			name:           "TestValidateXDCRKeyspaceInvalidFirstCharacter",
			mutations:      patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().Replace("/explicitMapping/allowRules/0/targetKeyspace/scope", "_scopefail")},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[0\])?.targetKeyspace`},
		},
		{
			name: "TestValidateXDCRKeyspaceInvalidFirstCharacterCollection",
			mutations: patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().
				Replace("/explicitMapping/allowRules/0/targetKeyspace/scope", "scope").
				Replace("/explicitMapping/allowRules/0/targetKeyspace/collection", "_collectionfail")},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[0\])?.targetKeyspace`},
		},
		// Test that source and target keyspaces are the same size, i.e. either both contain collections or scopes only or neither.
		{
			name: "TestValidateXDCRReplicationKeyspaceInvalidSource",
			mutations: patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().
				Replace("/explicitMapping/allowRules/0/sourceKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope: "scopeonly",
				}).
				Replace("/explicitMapping/allowRules/0/targetKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scopeand",
					Collection: "collection",
				})},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[0\])?.sourceKeyspace`},
		},
		// Test that a source collection is not permitted to be mapped (by means of multiple rules) to multiple target collections.
		{
			name: "TestValidateXDCRReplicationMultipleSourceRulesInvalid",
			mutations: patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().
				Replace("/explicitMapping/allowRules/0/sourceKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope0",
					Collection: "bugs",
				}).
				Replace("/explicitMapping/allowRules/0/targetKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope0",
					Collection: "lola",
				}).
				Replace("/explicitMapping/allowRules/1/sourceKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope0",
					Collection: "bugs",
				}).
				Replace("/explicitMapping/allowRules/1/targetKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope0",
					Collection: "collection0",
				})},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[1\])?.sourceKeyspace`},
		},
		// Test that multiple source collections are not permitted to be mapped (by means of multiples rules) to a single target collection.
		{
			name: "TestValidateXDCRReplicationMultipleTargetRulesInvalid",
			mutations: patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().
				Replace("/explicitMapping/allowRules/0/sourceKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope1",
					Collection: "bugs",
				}).
				Replace("/explicitMapping/allowRules/0/targetKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope0",
					Collection: "lola",
				}).
				Replace("/explicitMapping/allowRules/1/sourceKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope0",
					Collection: "collection0",
				}).
				Replace("/explicitMapping/allowRules/1/targetKeyspace", &couchbasev2.CouchbaseReplicationKeyspace{
					Scope:      "scope0",
					Collection: "lola",
				})},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[1\])?.targetKeyspace`},
		},
		// Test that if you have an allow rule for a scope you do not have a more specific allow rule for a
		// collection inside it, but you can have a deny rule for a collection.
		{
			name: "TestValidateXDCRMultipleMoreSpecificRulesInvalid",
			mutations: patchMap{"replicationscopesandcollections": jsonpatch.NewPatchSet().
				Replace("/explicitMapping/allowRules/1/sourceKeyspace/scope", "scope0")},
			shouldFail:     true,
			expectedErrors: []string{`explicitMapping.allowRules(\[1\])?.sourceKeyspace`},
		},
		// Test that for migration you can only have one rule if using the _default collection as the source.
		{
			name: "TestValidateXDCRMigrationRulesInvalid",
			mutations: patchMap{"migrationscopesandcollections": jsonpatch.NewPatchSet().
				Replace("/migrationMapping/mappings/0/filter", "_default._default")},
			shouldFail:     true,
			expectedErrors: []string{`migrationMapping.mappings(\[0\])?.filter`},
		},
		// Test that you cannot have migration and replication rules together in the same definition
		{
			name: "TestValidateXDCRMigrationRulesMutualExclusionReplicationInvalid",
			mutations: patchMap{"migrationscopesandcollections": jsonpatch.NewPatchSet().
				Replace("/spec/bucket", "bucket0").
				Replace("/spec/remoteBucket", "bucket0")},
			shouldFail:     true,
			expectedErrors: []string{`duplicate rule`},
		},
		// Test that mobile active bucket cannot be used with cluster version < 7.6.4
		{
			name: "TestValidateMobileActiveBucketInvalidVersion",
			mutations: patchMap{"cluster0": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.2.6"),
				"replication0": jsonpatch.NewPatchSet().
					Add("/metadata/annotations", map[string]string{
						"cao.couchbase.com/mobile": "Active",
					})},
			shouldFail:     true,
			expectedErrors: []string{`bucket bucket0 referenced by spec.bucket in couchbasereplications.couchbase.com/replication0 must be valid: mobile replication requires cluster version 7.6.4 or greater`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestCreateCouchbaseReplication(t *testing.T) {
	testDefs := []testDef{
		// Test that buckets referenced by a replication must have cross cluster versioning enabled
		{
			name: "TestValidateXDCRBucketCrossClusterVersioningDisabled",
			mutations: patchMap{"cluster2": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.6.4").
				Replace("/status/currentVersion", "7.6.4"),
				"replication0": jsonpatch.NewPatchSet().
					Add("/metadata/annotations", map[string]string{
						"cao.couchbase.com/mobile": "Active",
					})},
			shouldFail:     true,
			expectedErrors: []string{`bucket bucket1 referenced by spec.bucket in couchbasereplications.couchbase.com/replication0 must be valid: bucket bucket1 must have cross cluster versioning enabled to be used with mobile replication`},
		},
		{
			name: "TestValidateXDCRWithMobileActive",
			mutations: patchMap{"cluster2": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.6.4").
				Replace("/status/currentVersion", "7.6.4"),
				"replication0": jsonpatch.NewPatchSet().
					Add("/metadata/annotations", map[string]string{
						"cao.couchbase.com/mobile": "Active",
					}),
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/metadata/annotations", map[string]string{
						"cao.couchbase.com/enableCrossClusterVersioning": "true",
					})},
			shouldFail: false,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-76.yaml"})
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
			name:           "TestValidateBackupStagingVolueWithoutCloudOrPersistentVolume",
			mutations:      patchMap{"backup2": jsonpatch.NewPatchSet().Remove("/spec/objectStore/uri").Replace("/spec/ephemeralVolume", true)},
			shouldFail:     true,
			expectedErrors: []string{`spec.ephemeralVolume`},
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
		{
			name: "TestValidateBackupInvalidSecret",
			mutations: patchMap{
				"backup0": jsonpatch.NewPatchSet().
					Add("/spec/objectStore", couchbasev2.ObjectStoreSpec{
						URI:    "s3://blah",
						Secret: "test-example-s3-region-only",
					}),
			},
			shouldFail:     true,
			expectedErrors: []string{},
		},
		{
			name:           "TestValidateBackupCloudWithBackupMerge",
			mutations:      patchMap{"backup0": jsonpatch.NewPatchSet().Replace("/spec/s3bucket", "s3://hello/beans").Replace("/spec/strategy", "periodic_merge")},
			shouldFail:     true,
			expectedErrors: []string{`spec.strategy cannot be periodicMerge when using a cloud object store`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationCreateCouchbaseBackupRestore(t *testing.T) {
	testDefs := []testDef{
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
			mutations:      patchMap{"scope0": jsonpatch.NewPatchSet().Add("/spec/name", "000000000011111111112222222222333333333344444444445555555555666666666677777777778888888888999999999900000000001111111111222222222233333333334444444444555555555566666666667777777777888888888899999999990000000000111111111122222222223333333333444444444455")},
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
			expectedErrors: []string{`spec.collections.resources(\[0\])?.kind`},
		},
		// Check that bucket scope collection is working.  This shared between
		// couchbase and ephemeral buckets, so we only need to do this once.
		{
			name:           "TestValidateBucketSelectorTypeIllegal",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/scopes/resources/0/kind", "CouchbaseCollection")},
			shouldFail:     true,
			expectedErrors: []string{`spec.scopes.resources(\[0\])?.kind`},
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
			mutations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
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
			validations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Test("/spec/cluster/indexServiceMemoryQuota", "256Mi").
					Test("/spec/cluster/searchServiceMemoryQuota", "256Mi").
					Test("/spec/cluster/eventingServiceMemoryQuota", "256Mi").
					Test("/spec/cluster/analyticsServiceMemoryQuota", "1Gi").
					Test("/spec/cluster/indexStorageSetting", couchbasev2.CouchbaseClusterIndexStorageSettingMemoryOptimized).
					Test("/spec/cluster/autoFailoverTimeout", "120s").
					Test("/spec/cluster/autoFailoverMaxCount", 1).
					Test("/spec/cluster/autoFailoverOnDataDiskIssuesTimePeriod", "120s"),
			},
		},
		{
			name:      "TestValidateIndexerDefault",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer", emptyObject)},
			validations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
					Test("/spec/cluster/indexer/logLevel", "info").
					Test("/spec/cluster/indexer/maxRollbackPoints", 2).
					Test("/spec/cluster/indexer/memorySnapshotInterval", "200ms").
					Test("/spec/cluster/indexer/stableSnapshotInterval", "5s").
					Test("/spec/cluster/indexer/storageMode", "memory_optimized"),
			},
		},
		{
			name: "TestValidateCouchbaseBucketDefault",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().
					Remove("/spec/memoryQuota").
					Remove("/spec/replicas").
					Remove("/spec/ioPriority").
					Remove("/spec/evictionPolicy").
					Remove("/spec/conflictResolution").
					Remove("/spec/compressionMode"),
			},
			validations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().
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
			mutations: patchMap{
				"bucket3": jsonpatch.NewPatchSet().
					Remove("/spec/memoryQuota").
					Remove("/spec/replicas").
					Remove("/spec/ioPriority").
					Remove("/spec/evictionPolicy").
					Remove("/spec/conflictResolution").
					Remove("/spec/compressionMode"),
			},
			validations: patchMap{
				"bucket3": jsonpatch.NewPatchSet().
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
			mutations: patchMap{
				"bucket2": jsonpatch.NewPatchSet().
					Remove("/spec/memoryQuota"),
			},
			validations: patchMap{
				"bucket2": jsonpatch.NewPatchSet().
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
			validations: patchMap{
				"cluster": jsonpatch.NewPatchSet().
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
			expectedErrors: []string{`bucket memory allocation \([0-9]*Mi\) exceeds data service quota \(256Mi\) on cluster cluster`},
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
		// Check Collection mutability.
		{
			name:       "TestValidateCollectionTTLMutable",
			mutations:  patchMap{"collection0": jsonpatch.NewPatchSet().Add("/spec/maxTTL", "30s")},
			shouldFail: false,
		},
		{
			name:       "TestValidateUpdateBucketMemoryQuota",
			mutations:  patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", "100Mi")},
			shouldFail: false,
		},
		{
			name: "TestNegValidationUpdateClusterBackupImageLeftEmpty",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/backup", couchbasev2.Backup{
				Image:   "",
				Managed: true,
			})},
			shouldFail:     true,
			expectedErrors: []string{"spec.backup.image cannot be empty when spec.backup.managed is true"},
		},
		{
			name: "TestValidationUpdateClusterBackupImageLeftEmptyWhenNotManaged",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/backup", couchbasev2.Backup{
				Image:   "",
				Managed: false,
			})},
			shouldFail: false,
		},
	}

	// Cases to verify supported time units for Spec.LogRetentionTime
	for _, timeUnit := range supportedTimeUnits {
		testDefCase := testDef{
			name:      "TestValidateApplyLogRetentionTime_" + timeUnit,
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
			expectedErrors: []string{`spec.servers(\[0\])?.services`},
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
		{
			name:           "TestValidateUpdateBucketMemoryQuotaOverflow",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/memoryQuota", "601Mi")},
			shouldFail:     true,
			expectedErrors: []string{`bucket memory allocation \(1101Mi\) exceeds data service quota \(600Mi\) on cluster cluster`},
		},
		{
			name: "TestValidateAddSampleBucketAnnotation",
			mutations: patchMap{"bucket0": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/sampleBucket": "true",
				})},
			shouldFail:     true,
			expectedErrors: []string{`cao.couchbase.com/sampleBucket annotation cannot be added to an existing bucket`},
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
			expectedErrors: []string{`spec.servers(\[1\])?.services`},
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
			expectedErrors: []string{`spec.servers(\[3\])?.services`},
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
		{
			name: "TestEnableOnlineVolumeClaimExpansionRejectedNoTemplates",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/enableOnlineVolumeExpansion", true).
				Remove("/spec/volumeClaimTemplates")},
			shouldFail:     true,
			expectedErrors: []string{"spec.cluster.enableOnlineVolumeExpansion cannot be enabled since no volume claim templates have been definied"},
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
			expectedErrors: []string{`spec.servers(\[0\])?.services`},
		},
		{
			name:           "TestValidateApplyServerServicesImmutable_2",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/servers/0/services", couchbasev2.ServiceList{couchbasev2.DataService, couchbasev2.DataService})},
			shouldFail:     true,
			expectedErrors: []string{`spec.servers(\[0\])?.services`},
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
			name:           "TestValidateBucketInvalidCouchbaseStorageBackendChange",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Add("/spec/storageBackend", "magma")},
			shouldFail:     true,
			expectedErrors: []string{`spec.storageBackend`},
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
			expectedErrors: []string{`spec.roles(\[0\])?.bucket`},
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
			expectedErrors: []string{`spec.roles(\[0\])?.bucket`},
		},
		// RBAC role-version specific tests were moved to TestRBACRoleValidation76 and TestRBACRoleValidation80.
		{
			name: "TestWarnDuplicateUserNameInSpec",
			mutations: patchMap{
				"user1": jsonpatch.NewPatchSet().Replace("/spec/name", "duplicate-user"),
				"user2": jsonpatch.NewPatchSet().Replace("/spec/name", "duplicate-user"),
			},
			shouldFail:       false,
			expectedWarnings: []string{"user name duplicate-user is already in use"},
		},
		{
			name: "TestWarnDuplicateUserNameInSpecAndMetadata",
			mutations: patchMap{
				"user2": jsonpatch.NewPatchSet().Replace("/spec/name", "user1"),
			},
			shouldFail:       false,
			expectedWarnings: []string{"user name user1 is already in use"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

// TestRBACRoleValidation80 tests RBAC role validation for 8.0+ server.
func TestRBACRoleValidation80(t *testing.T) {
	testDefs := []testDef{
		// RBAC Role validation tests for 8.0+ server (uses validation.yaml with cluster image patched to 8.0.0).
		{
			name: "TestAcceptSecurityAdminOn80",
			mutations: patchMap{
				"cluster":     jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:8.0.0").Replace("/status/currentVersion", "8.0.0"),
				"admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "security_admin"}),
			},
			shouldFail: false,
		},
		{
			name: "TestWarnSecurityAdminLocalOn80",
			mutations: patchMap{
				"cluster":     jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:8.0.0").Replace("/status/currentVersion", "8.0.0"),
				"admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "security_admin_local"}),
			},
			shouldFail:       false,
			expectedWarnings: []string{`Group admin-group uses deprecated role security_admin_local - will be automatically migrated to security_admin \+ user_admin_\*`},
		},
		{
			name: "TestWarnSecurityAdminExternalOn80",
			mutations: patchMap{
				"cluster":     jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:8.0.0").Replace("/status/currentVersion", "8.0.0"),
				"admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "security_admin_external"}),
			},
			shouldFail:       false,
			expectedWarnings: []string{`Group admin-group uses deprecated role security_admin_external - will be automatically migrated to security_admin \+ user_admin_\*`},
		},
		{
			name: "TestAcceptUserAdminLocalOn80",
			mutations: patchMap{
				"cluster":     jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:8.0.0").Replace("/status/currentVersion", "8.0.0"),
				"admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "user_admin_local"}),
			},
			shouldFail: false,
		},
		{
			name: "TestAcceptUserAdminExternalOn80",
			mutations: patchMap{
				"cluster":     jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:8.0.0").Replace("/status/currentVersion", "8.0.0"),
				"admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "user_admin_external"}),
			},
			shouldFail: false,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

// TestRBACRoleValidation76 tests RBAC role validation for 7.6.x server.
func TestRBACRoleValidation76(t *testing.T) {
	testDefs := []testDef{
		// RBAC Role validation tests for 7.x server (uses validation.yaml default image 7.1.4).
		{
			name:           "TestRejectSecurityAdminRoleOn7x",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.4").Replace("/status/currentVersion", "7.6.4"), "admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "security_admin"})},
			shouldFail:     true,
			expectedErrors: []string{`security_admin role is configured in group admin-group and cannot be used with Couchbase Server 7.0\+`},
		},
		{
			name:       "TestAcceptSecurityAdminLocalOn7x",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.4").Replace("/status/currentVersion", "7.6.4"), "admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "security_admin_local"})},
			shouldFail: false,
		},
		{
			name:       "TestAcceptSecurityAdminExternalOn7x",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.4").Replace("/status/currentVersion", "7.6.4"), "admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "security_admin_external"})},
			shouldFail: false,
		},
		{
			name:           "TestRejectUserAdminLocalOn7x",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.4").Replace("/status/currentVersion", "7.6.4"), "admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "user_admin_local"})},
			shouldFail:     true,
			expectedErrors: []string{`role user_admin_local in group admin-group requires Couchbase Server 8.0\+`},
		},
		{
			name:           "TestRejectUserAdminExternalOn7x",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.4").Replace("/status/currentVersion", "7.6.4"), "admin-group": jsonpatch.NewPatchSet().Replace("/spec/roles/0", couchbasev2.Role{Name: "user_admin_external"})},
			shouldFail:     true,
			expectedErrors: []string{`role user_admin_external in group admin-group requires Couchbase Server 8.0\+`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestRBACScopeValidationCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestRBACScopeGroupAllowed",
			mutations: patchMap{"scoped-group": jsonpatch.NewPatchSet().
				Replace("/spec/roles/0/scopes/resources/0/name", "scopegroup0").
				Replace("/spec/roles/0/scopes/resources/0/kind", "CouchbaseScopeGroup")},
			shouldFail: false,
		},
		{
			name: "TestRBACScopeGroupBadKind",
			mutations: patchMap{"scoped-group": jsonpatch.NewPatchSet().
				Replace("/spec/roles/0/scopes/resources/0/kind", "UnknownScopeGroup")},
			shouldFail: true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestRBACCollectionValidationCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestRBACCollectionGroupAllowed",
			mutations: patchMap{"scoped-group": jsonpatch.NewPatchSet().
				Replace("/spec/roles/0/collections/resources/0/name", "collectiongroup0").
				Replace("/spec/roles/0/collections/resources/0/kind", "CouchbaseCollectionGroup")},
			shouldFail: false,
		},
		{
			name: "TestRBACCollectionGroupBadKind",
			mutations: patchMap{"scoped-group": jsonpatch.NewPatchSet().
				Replace("/spec/roles/0/scopes/resources/0/kind", "UnknownCollectionGroup")},
			shouldFail: true,
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
		},
		{
			name:           "TestValidateCaCertNotRequiredRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.1.0").Remove("/spec/security/ldap/tlsSecret")},
			shouldFail:     true,
			expectedErrors: []string{"spec.security.ldap.tlsSecret"},
		},
		{
			name:           "TestValidateGroupRequired",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/security/ldap/groupsQuery")},
			shouldFail:     true,
			expectedErrors: []string{"security.ldap.groupsQuery in body is required"},
		},
		{
			name:           "TestValidateAuthSecretRejected",
			mutations:      patchMap{"user2": jsonpatch.NewPatchSet().Add("/spec/authSecret", "auth-secret")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authSecret auth-secret not allowed for LDAP user `user2`"},
		},
		{
			name:           "TestValidateAuthDomain",
			mutations:      patchMap{"user1": jsonpatch.NewPatchSet().Replace("/spec/authDomain", "upnorth")},
			shouldFail:     true,
			expectedErrors: []string{"spec.authDomain"},
		},
		{
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

func TestCNGVersionValidation(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestMinimunCBVersionForCNGSupport",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/networking/cloudNativeGateway", couchbasev2.CloudNativeGateway{
					Image:    "ghcr.io/cb-vanilla/cloud-native-gateway:0.1.0-137",
					LogLevel: "debug",
				}).
				Replace("/spec/image", "couchbase/server:7.2.0")},
			expectedErrors: []string{"for cloud native gateway support"},
			shouldFail:     true,
		},
		{
			name: "TestRestrictedCNGVersion",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/networking/cloudNativeGateway", couchbasev2.CloudNativeGateway{
					Image:    "ghcr.io/cb-vanilla/cloud-native-gateway:0.2.0-136",
					LogLevel: "debug",
				}).
				Replace("/spec/image", "couchbase/server:7.2.2")},
			expectedErrors: []string{"to support cloud native gateway versio"},
			shouldFail:     true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestBucketMigrationPre76Invalid(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestBucketMigrationToCouchstoreInvalid",
			mutations:      patchMap{"bucket2": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "couchstore")},
			expectedErrors: []string{"spec.storageBackend backend can only be changed if all referencing clusters are version 7.6.0 or greater"},
			shouldFail:     true,
		},
		{
			name:           "TestBucketMigrationToMagmaPre76Invalid",
			mutations:      patchMap{"bucket1": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma")},
			expectedErrors: []string{"spec.storageBackend backend can only be changed if all referencing clusters are version 7.6.0 or greater"},
			shouldFail:     true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "bucket-migration.yaml"})
}

func TestBlockChangingMigrationProcessDuringMigration(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestBlockChangingMigrationProcessDuringMigration",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/metadata/annotations", map[string]string{"cao.couchbase.com/buckets.enableBucketMigrationRoutines": "false"})},
			expectedErrors: []string{"cao.couchbase.com/buckets.enableBucketMigrationRoutines cannot be changed while a bucket migration is taking place"},
			shouldFail:     true,
		}, {
			name:           "TestBlockRemovingMigrationProcessDuringMigration",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/metadata/annotations", map[string]string{})},
			expectedErrors: []string{"cao.couchbase.com/buckets.enableBucketMigrationRoutines cannot be changed while a bucket migration is taking place"},
			shouldFail:     true,
		}, {
			name:           "TestBlockRemovingAnnotationsMigrationProcessDuringMigration",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/metadata/annotations")},
			expectedErrors: []string{"cao.couchbase.com/buckets.enableBucketMigrationRoutines cannot be changed while a bucket migration is taking place"},
			shouldFail:     true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "bucket-migration-in-process.yaml"})
}

func TestBucketMigrationPost76Validation(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestBucketMigrationToCouchstoreValid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.enableBucketMigrationRoutines": "true",
				}),
				"bucket2": jsonpatch.NewPatchSet().
					Replace("/spec/storageBackend", "couchstore"),
			},
			shouldFail: false,
		},
		{
			name: "TestBucketMigrationToCouchstoreInvalid",
			mutations: patchMap{"bucket3": jsonpatch.NewPatchSet().
				Replace("/spec/storageBackend", "couchstore")},
			expectedErrors: []string{"can only be changed from magma to couchstore if history retention is first disabled on the bucket"},
			shouldFail:     true,
		},
		{
			name: "TestBucketMigrationToMagmaValid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.enableBucketMigrationRoutines": "true",
				}),
				"bucket1": jsonpatch.NewPatchSet().
					Replace("/spec/storageBackend", "magma")},
			shouldFail: false,
		},
		{
			name: "TestBucketMigrationToCouchstoreInvalidAnnotation",
			mutations: patchMap{
				"bucket2": jsonpatch.NewPatchSet().
					Replace("/spec/storageBackend", "couchstore")},
			expectedErrors: []string{"spec.storageBackend backend can only be changed if all referencing clusters have the enableBucketMigrationRoutines annotation set to true"},
			shouldFail:     true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "bucket-migration-76.yaml"})
}

func TestAnnotationValidation(t *testing.T) {
	testDefs := []testDef{
		{
			name:       "TestInvalidCAOClusterAnnotation",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Add("/metadata/annotations", map[string]string{"cao.couchbase.com/invalidAnnotation": "v"})},
			shouldFail: false,
		},
		{
			name: "TestValidCAOClusterAnnotations",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.defaultStorageBackend":               "magma",
					"cao.couchbase.com/buckets.targetUnmanagedBucketStorageBackend": "magma",
				})},
			shouldFail: false,
		},
		{
			name: "TestClusterAnnotationsDefaultBackendMagmaInvalidVersion",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Replace("/spec/image", "couchbase/server:7.0.0").
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.defaultStorageBackend": "magma",
				})},
			shouldFail:     true,
			expectedErrors: []string{"magma storage backend requires Couchbase Server version 7.1.0 or later"},
		},
		{
			name: "TestClusterAnnotationsBackendValuesInvalid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.defaultStorageBackend":               "supamagma",
					"cao.couchbase.com/buckets.targetUnmanagedBucketStorageBackend": "coldmagma",
				})},
			shouldFail:     true,
			expectedErrors: []string{"must be a valid storage backend"},
		},
		{
			name: "TestClusterAnnotationsEnableBucketMigrationRoutinesInvalid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.enableBucketMigrationRoutines": "NaN",
				})},
			shouldFail:     true,
			expectedErrors: []string{"invalid syntax"},
		},
		{
			name: "TestClusterAnnotationsMaxConcurrentPodSwapsInvalid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.maxConcurrentPodSwaps": "NaN",
				})},
			shouldFail:     true,
			expectedErrors: []string{"invalid syntax"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "bucket-migration-76.yaml"})
}

func TestAnnotationVersionValidation(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestTargetUnmanagedBackendAnnotationInvalidVersion",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.targetUnmanagedBucketStorageBackend": "couchstore",
				})},
			shouldFail:     true,
			expectedErrors: []string{"server version 7.6.0 or larger"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "bucket-migration.yaml"})
}

func TestDefaultStorageBackendChangeValidation(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestDefaultBackendChangeCausingInvalingBackendMigration",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.defaultStorageBackend": "magma",
				})},
			shouldFail:     true,
			expectedErrors: []string{"backend changes are only supported for server version >= 7.6.0"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "bucket-migration.yaml"})
}

func TestBucketMinReplicasCountApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:       "TestBucketEnoughReplicas",
			shouldFail: false,
		},
		{
			name:           "TestBucketNotEnoughReplicas",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/replicas", 1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.replicas"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "bucket-replicas.yaml"})
}

func TestBucketMinReplicasCountCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestBucketNotEnoughReplicas",
			mutations:      patchMap{"bucket0": jsonpatch.NewPatchSet().Replace("/spec/replicas", 1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.replicas"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "bucket-replicas.yaml"})
}

func TestVersionUpgradePath(t *testing.T) {
	testDefs := []testDef{
		{
			name:       "ValidUpgradePath",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.2.3")},
			shouldFail: false,
		},
		{
			name:           "InvalidUpgradePath",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.2.4")},
			shouldFail:     true,
			expectedErrors: []string{"cannot upgrade"},
		},
		{
			name:       "InvalidUpgradePathDowngrade",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.0.0")},
			shouldFail: true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "validation-701.yaml"})
}

func TestCouchbaseClusterWarnings(t *testing.T) {
	testDefs := []testDef{
		{
			name:             "TestAutoCompactionDefaultsWarningWhenRemoved",
			mutations:        patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/cluster/autoCompaction")},
			shouldFail:       false,
			expectedWarnings: []string{"CouchbaseCluster spec.cluster.autoCompaction settings have been left as their defaults. It is recommended these are tuned for production clusters."},
		},
		{
			name:             "TestAutoCompactionDefaultsWarningWhenSetToEmpty",
			mutations:        patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction", &couchbasev2.AutoCompaction{})},
			shouldFail:       false,
			expectedWarnings: []string{"CouchbaseCluster spec.cluster.autoCompaction settings have been left as their defaults. It is recommended these are tuned for production clusters."},
		},
		{
			name: "TestAutoCompactionDefaultsNoWarningWhenMagmaSet",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction", &couchbasev2.AutoCompaction{
				MagmaFragmentationThresholdPercentage: util.IntPtr(10),
			})},
			shouldFail:       false,
			expectedWarnings: []string{},
		},
		{
			name: "TestPasswordPolicyZeroMinLengthWarning",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
				MinLength: util.IntPtr(0),
			})},
			shouldFail:       false,
			expectedWarnings: []string{"CouchbaseCluster spec.security.passwordPolicy.minLength is set to 0. This is highly insecure and should not be used for production clusters."},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestAnnotationWarnings(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestHistoryRetentionSecondsOverwrite",
			mutations: patchMap{"bucket0": jsonpatch.NewPatchSet().
				Add("/spec/historyRetention", couchbasev2.HistoryRetentionSettings{
					Seconds: 1000,
				}).
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/historyRetention.seconds": "4",
				})},
			shouldFail:       false,
			expectedWarnings: []string{"Overwriting existing value for annotation"},
		},
		{
			name: "TestNoTargetFoundForAnnotation",
			mutations: patchMap{"bucket0": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/nonexistent.field": "value",
				})},
			shouldFail:       false,
			expectedWarnings: []string{"No target found for annotation"},
		},
		{
			name: "TestDeprecatedAnnotation",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.enableBucketMigrationRoutines": "true",
				})},
			shouldFail:       false,
			expectedWarnings: []string{"cao.couchbase.com/buckets.enableBucketMigrationRoutines is deprecated, please use spec.buckets.enableBucketMigrationRoutines field instead"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestHibernationChangeConstraints(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "ChangesDuringHibernationAreInvalid",
			mutations:      patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/servers/0/size", 2)},
			shouldFail:     true,
			expectedErrors: []string{"cluster spec cannot be changed during hibernation"},
		},
		{
			name:       "ChangesWhenDisablingHibernationAreValid",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Add("/spec/servers/0/size", 2).Remove("/spec/hibernate")},
			shouldFail: false,
		},
		{
			name:             "HibernateDuringUpgradeExpectsWarning",
			mutations:        patchMap{"cluster-upgrading": jsonpatch.NewPatchSet().Replace("/spec/hibernate", true)},
			shouldFail:       false,
			expectedWarnings: []string{"but the cluster cannot enter hibernation: Cluster is upgrading"},
		},
		{
			name:           "HibernateOnMigrationClusterExpectsWarning",
			mutations:      patchMap{"cluster-migrating": jsonpatch.NewPatchSet().Replace("/spec/hibernate", true)},
			shouldFail:     true,
			expectedErrors: []string{"spec.hibernate cannot be enabled when spec.migration is configured"},
		},
		{
			name:             "HibernateDuringBucketMigrationExpectsWarning",
			mutations:        patchMap{"cluster-bucket-migrating": jsonpatch.NewPatchSet().Replace("/spec/hibernate", true)},
			shouldFail:       false,
			expectedWarnings: []string{"but the cluster cannot enter hibernation: Cluster is migrating buckets"},
		},
		{
			name:             "HibernateDuringScalingExpectsWarning",
			mutations:        patchMap{"cluster-scaling": jsonpatch.NewPatchSet().Replace("/spec/hibernate", true)},
			shouldFail:       false,
			expectedWarnings: []string{"but the cluster cannot enter hibernation: Cluster is scaling"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "validation-hibernate.yaml"})
}

func TestClusterMigrationAddition(t *testing.T) {
	testDefs := []testDef{
		{
			name: "AddingMigrationToExistingCluster",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/migration", couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "unmanaged-cluster.cbnet",
			})},
			shouldFail:     true,
			expectedErrors: []string{"spec.migration cannot be added"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply})
}

func TestClusterMigrationInvalidMigration(t *testing.T) {
	testDefs := []testDef{
		{
			name: "AddingMigrationToExistingCluster",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/migration", couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "unmanaged-cluster.cbnet",
				NumUnmanagedNodes:    199,
			})},
			shouldFail:     true,
			expectedErrors: []string{"spec.migration.numUnmanagedNodes must be less than"},
		},
		{
			name: "HibernateEnabledDuringMigrationInvalid",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/migration", couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "unmanaged-cluster.cbnet",
			}).Add("/spec/hibernate", true)},
			shouldFail:     true,
			expectedErrors: []string{"spec.hibernate cannot be enabled when spec.migration is configured"},
		},
		{
			name: "AddMigrationWithInvalidServerClasses",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/migration", couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "unmanaged-cluster.cbnet",
				NumUnmanagedNodes:    1,
				MigrationOrderOverride: &couchbasev2.MigrationOrderOverrideSpec{
					MigrationOrderOverrideStrategy: couchbasev2.ByServerClass,
					ServerClassOrder:               []string{"all_services", "totally_real_service"},
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"contains an invalid server class"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestNegValidationClusterMigrationApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:       "UpdateImageDuringMigration",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.2")},
			shouldFail: true,
		},
		{
			name:       "RemoveMigrationSpecWithNoBuckets",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/migration")},
			shouldFail: true,
		},
		{
			name:       "RemoveMigrationSpecWithNoUsers",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Remove("/spec/migration")},
			shouldFail: true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "validation-migration.yaml"})
}

func TestValidationClusterMigrationApply(t *testing.T) {
	testDefs := []testDef{
		{
			name:       "TestValidationUpdateIndexStorageModeWhenInErrorState",
			mutations:  patchMap{"cluster-index-mismatch": jsonpatch.NewPatchSet().Replace("/spec/cluster/indexer/storageMode", couchbasev2.CouchbaseClusterIndexStorageSettingStandard)},
			shouldFail: false,
		},
		{
			name: "TestMigrationOrderOverrideServerGroupOrder",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/migration", couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "unmanaged-cluster.cbnet",
				MigrationOrderOverride: &couchbasev2.MigrationOrderOverrideSpec{
					MigrationOrderOverrideStrategy: couchbasev2.ByNode,
					ServerGroupOrder:               []string{"sg-1", "sg-2"},
				},
			})},
			shouldFail:       false,
			expectedWarnings: []string{"spec.migration.migrationOrderOverride.serverGroupOrder will be ignored when using ByNode or ByServerClass strategy"},
		},
		{
			name: "TestMigrationOrderOverrideServerClassOrder",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/migration", couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "unmanaged-cluster.cbnet",
				MigrationOrderOverride: &couchbasev2.MigrationOrderOverrideSpec{
					MigrationOrderOverrideStrategy: couchbasev2.ByServerGroup,
					ServerClassOrder:               []string{"all_services"},
				},
			})},
			shouldFail:       false,
			expectedWarnings: []string{"spec.migration.migrationOrderOverride.serverClassOrder will be ignored when using ByNode or ByServerGroup strategy"},
		},
		{
			name: "TestMigrationOrderOverrideNodeOrder",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/migration", couchbasev2.ClusterAssimilationSpec{
				UnmanagedClusterHost: "unmanaged-cluster.cbnet",
				MigrationOrderOverride: &couchbasev2.MigrationOrderOverrideSpec{
					MigrationOrderOverrideStrategy: couchbasev2.ByServerClass,
					NodeOrder:                      []string{"node1", "node2"},
				},
			})},
			shouldFail:       false,
			expectedWarnings: []string{"spec.migration.migrationOrderOverride.nodeOrder will be ignored when using ByServerClass or ByServerGroup strategy"},
		},
		{
			name: "TestMigrationBucketsManaged",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/buckets", couchbasev2.Buckets{
				Managed: true,
			},
			)},
			shouldFail:       false,
			expectedWarnings: []string{"spec.Buckets.Managed is true. Any bucket not defined as a kubernetes resource will be deleted once migration is completed"},
		},
		{
			name:             "TestMigrationRBACManaged",
			mutations:        patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/security/rbac", couchbasev2.RBAC{Managed: true})},
			shouldFail:       false,
			expectedWarnings: []string{"spec.Security.RBAC.Managed is true. Any user, group or role not defined as a kubernetes resource will be deleted once migration is completed"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "validation-migration.yaml"})
}

func TestMidUpgradeImageValidations(t *testing.T) {
	midUpgradeTestDefs := []testDef{
		{
			name:       "UpgradeDuringGranularUpgrade",
			mutations:  patchMap{"cluster": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.4")},
			shouldFail: true,
		},
	}

	runValidationTest(t, midUpgradeTestDefs, validationContext{operation: operationApply, validationFile: "mid-upgrade.yaml"})
}

func TestValidationEncryptionKey(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestEncryptionKeyRotationIntervalDays",
			mutations:      patchMap{"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/rotation/intervalDays", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.autoGenerated.rotation.intervalDays"},
		},
		{
			name: "TestEncryptionKeyEmptyUsage",
			mutations: patchMap{"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/usage", couchbasev2.CouchbaseEncryptionKeyUsage{
				Configuration: false,
				Key:           false,
				Log:           false,
				Audit:         false,
				AllBuckets:    false,
			})},
			shouldFail:     true,
			expectedErrors: []string{"at least one usage field must be set to true in spec.usage"},
		},
		{
			name:           "TestEncryptionKeyTypeChange",
			mutations:      patchMap{"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/keyType", couchbasev2.CouchbaseEncryptionKeyTypeAWS)},
			shouldFail:     true,
			expectedErrors: []string{"encryption key type cannot be changed"},
		},
		{
			name:       "TestEncryptionKeyAWSInvalidARNChange",
			mutations:  patchMap{"aws-key-1": jsonpatch.NewPatchSet().Replace("/spec/awsKey/keyARN", "arn:aws:kms:eu-west-2:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab")},
			shouldFail: true,
		},
		{
			name:       "TestEncryptionKeyAWSInvalidRegionChange",
			mutations:  patchMap{"aws-key-1": jsonpatch.NewPatchSet().Replace("/spec/awsKey/keyRegion", "eu-west-2")},
			shouldFail: true,
		},
		{
			name: "TestRemoveKeyUsageWhenInUseByCluster",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-4",
					},
					Audit: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-4",
					},
					Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-4",
					},
				}),
				"auto-generated-key-4": jsonpatch.NewPatchSet().Replace("/spec/usage", couchbasev2.CouchbaseEncryptionKeyUsage{
					Configuration: false,
					Key:           true,
					Log:           false,
					Audit:         false,
					AllBuckets:    true,
				})},
			shouldFail:     true,
			expectedErrors: []string{"spec.usage.configuration", "spec.usage.audit", "spec.usage.log"},
		},
		{
			name: "TestRemoveKeyUsageWhenInUseByKey",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
				}),
				"auto-generated-key-1": jsonpatch.NewPatchSet().Add("/spec/autoGenerated/encryptWithKey", "auto-generated-key-4"),
				"auto-generated-key-4": jsonpatch.NewPatchSet().Replace("/spec/usage", couchbasev2.CouchbaseEncryptionKeyUsage{
					Configuration: true,
					Key:           false,
					Log:           true,
					Audit:         true,
					AllBuckets:    true,
				})},
			shouldFail:     true,
			expectedErrors: []string{"spec.usage.key"},
		},
		{
			name: "TestRemoveKeyUsageWhenInUseByKey",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
				}),
				"bucket1": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					KeyName: "auto-generated-key-4",
				}),
				"auto-generated-key-4": jsonpatch.NewPatchSet().Replace("/spec/usage", couchbasev2.CouchbaseEncryptionKeyUsage{
					Configuration: true,
					Key:           true,
					Log:           true,
					Audit:         true,
					AllBuckets:    false,
				})},
			shouldFail:     true,
			expectedErrors: []string{"spec.usage.allBuckets"},
		},
		{
			name: "TestInvalidKeyNameOnBucket",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
				}),
				"bucket3": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					KeyName: "non-existent-key",
				})},
			shouldFail:     true,
			expectedErrors: []string{"spec.encryptionAtRest.keyName non-existent-key does not exist"},
		},
		{
			name: "TestInvalidKeyNoBucketUsage",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
				}),
				"auto-generated-key-4": jsonpatch.NewPatchSet().Replace("/spec/usage", couchbasev2.CouchbaseEncryptionKeyUsage{
					Configuration: true,
					Key:           true,
					Log:           true,
					Audit:         true,
					AllBuckets:    false,
				}),
				"bucket3": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					KeyName: "auto-generated-key-4",
				})},
			shouldFail:     true,
			expectedErrors: []string{"spec.encryptionAtRest.keyName auto-generated-key-4 does not have bucket encryption"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationApply, validationFile: "validation-80.yaml"})
}

func TestValidationEncryptionKeyCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name:           "TestEncryptionKeyRotationIntervalDays",
			mutations:      patchMap{"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/rotation/intervalDays", -1)},
			shouldFail:     true,
			expectedErrors: []string{"spec.autoGenerated.rotation.intervalDays"},
		},
		{
			name: "TestEncryptionKeyEmptyUsage",
			mutations: patchMap{"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/usage", couchbasev2.CouchbaseEncryptionKeyUsage{
				Configuration: false,
				Key:           false,
				Log:           false,
				Audit:         false,
				AllBuckets:    false,
			})},
			shouldFail:     true,
			expectedErrors: []string{"at least one usage field must be set to true in spec.usage"},
		},
		{
			name: "TestEncryptionKeyEmptyUsageNoStartTime",
			mutations: patchMap{"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated", couchbasev2.CouchbaseEncryptionKeyAutoGenerated{
				Rotation: &couchbasev2.CouchbaseEncryptionKeyRotation{
					IntervalDays: 7,
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"spec.autoGenerated.rotation.startTime"},
		},
		{
			name:           "TestEncryptionKeyAWSInvalidARN",
			mutations:      patchMap{"aws-key-1": jsonpatch.NewPatchSet().Replace("/spec/awsKey/keyARN", "definitely not an ARN")},
			shouldFail:     true,
			expectedErrors: []string{"spec.awsKey.keyARN"},
		},
		{
			name:           "TestEncryptionKeyAWSNonexistentCredentialsSecret",
			mutations:      patchMap{"aws-key-1": jsonpatch.NewPatchSet().Replace("/spec/awsKey/credentialsSecret", "non-existent-secret")},
			shouldFail:     true,
			expectedErrors: []string{"secret non-existent-secret referenced by spec.awsKey.credentialsSecret must exist"},
		},
		{
			name:           "TestEncryptionKeyAWSInvalidCredentialsSecret",
			mutations:      patchMap{"aws-key-secret": jsonpatch.NewPatchSet().Replace("/data", map[string][]byte{"config": []byte("non-existent-credentials")})},
			shouldFail:     true,
			expectedErrors: []string{"spec.awsKey.credentialsSecret"},
		},
		{
			name:           "TestKMIPEncryptionKeyInvalidPort",
			mutations:      patchMap{"kmip-key-1": jsonpatch.NewPatchSet().Replace("/spec/kmipKey/port", 65537)},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.port"},
		},
		{
			name:           "TestKMIPEncryptionKeyInvalidTimeoutInMs",
			mutations:      patchMap{"kmip-key-1": jsonpatch.NewPatchSet().Replace("/spec/kmipKey/timeoutInMs", 0)},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.timeoutInMs"},
		},
		{
			name:           "TestKMIPEncryptionKeyEmptyClientSecret",
			mutations:      patchMap{"kmip-key-1": jsonpatch.NewPatchSet().Replace("/spec/kmipKey/clientSecret", "")},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.clientSecret"},
		},
		{
			name:           "TestKMIPEncryptionKeyMissingClientSecret",
			mutations:      patchMap{"kmip-key-1": jsonpatch.NewPatchSet().Remove("/spec/kmipKey/clientSecret")},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.clientSecret"},
		},
		{
			name:           "TestKMIPEncryptionKeyClientSecretNotFound",
			mutations:      patchMap{"kmip-key-1": jsonpatch.NewPatchSet().Replace("/spec/kmipKey/clientSecret", "non-existent-kmip-secret")},
			shouldFail:     true,
			expectedErrors: []string{"secret non-existent-kmip-secret referenced by spec.kmipKey.clientSecret must exist"},
		},
		{
			name:           "TestKMIPEncryptionKeyClientSecretMissingPassphrase",
			mutations:      patchMap{"kmip-client-secret": jsonpatch.NewPatchSet().Remove("/data/passphrase")},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.clientSecret must contain passphrase"},
		},
		{
			name:           "TestKMIPEncryptionKeyClientSecretInvalidPassphrase",
			mutations:      patchMap{"kmip-client-secret": jsonpatch.NewPatchSet().Replace("/data/passphrase", []byte("invalid-passphrase"))},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.clientSecret must contain a valid key and passphrase"},
		},
		{
			name:           "TestKMIPEncryptionKeyClientSecretInvalidKey",
			mutations:      patchMap{"kmip-client-secret": jsonpatch.NewPatchSet().Replace("/data/tls.key", []byte("invalid-key"))},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.clientSecret must contain a valid key and passphrase"},
		},
		{
			name:           "TestKMIPEncryptionKeyClientMissingKey",
			mutations:      patchMap{"kmip-client-secret": jsonpatch.NewPatchSet().Remove("/data/tls.key")},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.clientSecret must contain tls.key"},
		},
		{
			name:           "TestKMIPEncryptionKeyClientMissingCert",
			mutations:      patchMap{"kmip-client-secret": jsonpatch.NewPatchSet().Remove("/data/tls.crt")},
			shouldFail:     true,
			expectedErrors: []string{"spec.kmipKey.clientSecret must contain tls.crt"},
		},
		{
			name:       "TestKMIPEncryptionKeyValid",
			mutations:  patchMap{"kmip-key-1": jsonpatch.NewPatchSet()},
			shouldFail: false,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-80.yaml"})
}
func TestValidationEncryptionAtRest(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestEncryptionAtRestNonExistentConfigurationKey",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "non-existent-key",
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"configuration encryption key validation failed", "encryption key non-existent-key does not exist or is not selected by the encryption at rest selector"},
		},
		{
			name: "TestEncryptionAtRestNonExistentAuditKey",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Audit: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "non-existent-key",
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"audit encryption key validation failed", "encryption key non-existent-key does not exist or is not selected by the encryption at rest selector"},
		},
		{
			name: "TestEncryptionAtRestNonExistentLogKey",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "non-existent-key",
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"log encryption key validation failed", "encryption key non-existent-key does not exist or is not selected by the encryption at rest selector"},
		},
		{
			name: "TestEncryptionAtRestKeyWithoutConfigurationUsage",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/usage/configuration", false),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-1",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"configuration encryption key validation failed", "encryption key auto-generated-key-1 does not have configuration usage enabled"},
		},
		{
			name: "TestEncryptionAtRestKeyWithoutAuditUsage",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/usage/audit", false),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Audit: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-1",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"audit encryption key validation failed", "encryption key auto-generated-key-1 does not have audit usage enabled"},
		},
		{
			name: "TestEncryptionAtRestKeyWithoutLogUsage",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/usage/log", false),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-1",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"log encryption key validation failed", "encryption key auto-generated-key-1 does not have log usage enabled"},
		},
		{
			name: "TestEncryptionAtRestValidKeys",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
				Audit: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
				Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
			})},
			shouldFail: false,
		},
		{
			name: "TestEncryptionAtRestEmptyKeys",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "",
				},
				Audit: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "",
				},
				Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "",
				},
			})},
			shouldFail: false, // Empty keys are valid - uses master password
		},
		{
			name: "TestEncryptionAtRestAllTypesValid",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
				Audit: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
				Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
			})},
			shouldFail: false,
		},
		{
			name: "TestEncryptionAtRestRotationIntervalTooShort",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
					RotationInterval: &metav1.Duration{
						Duration: 1 * time.Hour,
					},
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"rotation interval must be at least 7 days"},
		},
		{
			name: "TestEncryptionAtRestWithoutKeyRotationIntervalTooShort",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					RotationInterval: &metav1.Duration{
						Duration: 1 * time.Hour,
					},
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"rotation interval must be at least 7 days"},
		},
		{
			name: "TestEncryptionAtRestKeyLifetimeTooShort",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
					KeyLifetime: &metav1.Duration{
						Duration: 1 * time.Hour,
					},
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"key lifetime must be at least 30 days"},
		},
		{
			name: "TestEncryptionAtRestWithoutKeyLifetimeTooShort",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyLifetime: &metav1.Duration{
						Duration: 1 * time.Hour,
					},
				},
			})},
			shouldFail:     true,
			expectedErrors: []string{"key lifetime must be at least 30 days"},
		},
		{
			name: "TestEncryptionAtRestSimpleCircularDependency",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-2"),
				"auto-generated-key-2": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-1"),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-1",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"encryption key circular dependency validation failed", "circular dependency detected in encryption keys"},
		},
		{
			name: "TestEncryptionAtRestComplexCircularDependency",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-2"),
				"auto-generated-key-2": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-3"),
				"auto-generated-key-3": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-1"),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-1",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"encryption key circular dependency validation failed", "circular dependency detected in encryption keys"},
		},
		{
			name: "TestEncryptionAtRestSelfReferenceCircularDependency",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-1"),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-1",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"encryption key circular dependency validation failed", "circular dependency detected in encryption keys"},
		},
		{
			name: "TestEncryptionAtRestValidChainDependency",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-2"),
				"auto-generated-key-2": jsonpatch.NewPatchSet().Replace("/spec/autoGenerated/encryptWithKey", "auto-generated-key-3"),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
					Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
						Enabled: true,
						KeyName: "auto-generated-key-1",
					},
				}),
			},
			shouldFail: false, // Valid chain without cycle
		},
		{
			name: "TestEncryptionAtRestWithUnsupportedVersion",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().
					Replace("/spec/image", "couchbase/server:7.6.0").
					Replace("/status/currentVersion", "7.6.0").
					Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
						Managed: true,
						Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
							Enabled: true,
							KeyName: "auto-generated-key-1",
						},
					}),
			},
			shouldFail:     true,
			expectedErrors: []string{"encryption at rest requires Couchbase Server version 8.0.0 or later"},
		},
		{
			name: "TestEncryptionAtRestWithLoggingEnabled",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().
					Add("/spec/logging", couchbasev2.CouchbaseClusterLoggingSpec{
						Server: &couchbasev2.CouchbaseClusterLoggingConfigurationSpec{
							Enabled: true,
						},
					}).
					Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
						Managed: true,
						Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
							Enabled: true,
						},
					}),
			},
			shouldFail: true,
		},
		{
			name: "TestAutoGeneratedKeysWithoutConfigurationEncryption",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().
					Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
						Managed: true,
						// Configuration encryption is not enabled (nil/disabled)
						Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
							Enabled: false,
						},
					}),
				"auto-generated-key-1": jsonpatch.NewPatchSet().
					Add("/spec/autoGenerated", couchbasev2.CouchbaseEncryptionKeyAutoGenerated{
						EncryptWithKey: "",
					}),
			},
			shouldFail:     true,
			expectedErrors: []string{"auto-generated encryption keys use master password encryption and require configuration encryption at rest to be enabled"},
		},
		{
			name: "TestEncryptionAtRestKeyWithoutBucketUsage",
			mutations: patchMap{
				"auto-generated-key-1": jsonpatch.NewPatchSet().Replace("/spec/usage/allBuckets", false),
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
				}),
				"bucket1": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					KeyName: "auto-generated-key-1",
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"encryption key auto-generated-key-1 does not have bucket usage enabled"},
		},
		{
			name: "TestEncryptionAtRestKeyWithNonexistentKey",
			mutations: patchMap{
				"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
					Managed: true,
				}),
				"bucket1": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					KeyName: "non-existent-key",
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"encryption key non-existent-key does not exist"},
		},
		{
			name: "TestBucketEncryptionAtRestRotationIntervalTooShort",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					RotationInterval: &metav1.Duration{
						Duration: 1 * time.Hour,
					},
					KeyName: "auto-generated-key-1",
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"rotation interval must be at least 7 days"},
		},
		{
			name: "TestBucketEncryptionAtRestKeyLifetimeTooShort",
			mutations: patchMap{
				"bucket1": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					KeyLifetime: &metav1.Duration{
						Duration: 1 * time.Hour,
					},
					KeyName: "auto-generated-key-1",
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{"key lifetime must be at least 30 days"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-80.yaml"})
}

// TestAutoResourceAllocationValidation tests all scenarios for autoResourceAllocation
// overhead validation including mutual exclusivity and individual field acceptance.
func TestAutoResourceAllocationValidation(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateAutoResourceAllocationOverheadPercentOnly",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/autoResourceAllocation", &couchbasev2.AutoResourceAllocation{
					Enabled:         true,
					OverheadPercent: util.IntPtr(20),
				})},
			shouldFail: false,
		},
		{
			name: "TestValidateAutoResourceAllocationOverheadMemoryOnly",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/autoResourceAllocation", &couchbasev2.AutoResourceAllocation{
					Enabled:        true,
					OverheadMemory: resource.NewQuantity(1*1024*1024*1024, resource.BinarySI), // 1Gi
				})},
			shouldFail: false,
		},
		{
			name: "TestValidateAutoResourceAllocationNeitherSpecified",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/autoResourceAllocation", &couchbasev2.AutoResourceAllocation{
					Enabled: true,
				})},
			shouldFail: false,
		},
		{
			name: "TestValidateAutoResourceAllocationOverheadPercentAndOverheadMemorySet",
			mutations: patchMap{"cluster": jsonpatch.NewPatchSet().
				Add("/spec/autoResourceAllocation", &couchbasev2.AutoResourceAllocation{
					Enabled:         true,
					OverheadPercent: util.IntPtr(0),
					OverheadMemory:  resource.NewQuantity(1*1024*1024*1024, resource.BinarySI), // 1Gi
				})},
			shouldFail:     true,
			expectedErrors: []string{`at most one of spec.autoResourceAllocation.overheadPercent or spec.autoResourceAllocation.overheadMemory can be set`},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate})
}

func TestDeleteInUseEncryptionKey(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestDeleteNotInUseEncryptionKey",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
			})},
			deleteTargets: []string{"auto-generated-key-1"},
			shouldFail:    false,
		},
		{
			name: "TestDeleteInUseEncryptionKeyConfigEncryption",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Configuration: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
			})},
			deleteTargets: []string{"auto-generated-key-1"},
			shouldFail:    true,
		},
		{
			name: "TestDeleteInUseEncryptionKeyAuditEncryption",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Audit: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
			})},
			deleteTargets: []string{"auto-generated-key-1"},
			shouldFail:    true,
		},
		{
			name: "TestDeleteInUseEncryptionKeyLogEncryption",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
				Log: &couchbasev2.EncryptionAtRestUsageConfiguration{
					Enabled: true,
					KeyName: "auto-generated-key-1",
				},
			})},
			deleteTargets: []string{"auto-generated-key-1"},
			shouldFail:    true,
		},
		{
			name: "TestDeleteInUseEncryptionKeyBucket",
			mutations: patchMap{"cluster1": jsonpatch.NewPatchSet().Add("/spec/security/encryptionAtRest", &couchbasev2.EncryptionAtRestSpec{
				Managed: true,
			}),
				"bucket1": jsonpatch.NewPatchSet().Add("/spec/encryptionAtRest", &couchbasev2.BucketEncryptionAtRestConfiguration{
					KeyName: "auto-generated-key-1",
				})},
			deleteTargets: []string{"auto-generated-key-1"},
			shouldFail:    true,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationDelete, validationFile: "validation-80.yaml"})
}

func TestUserPasswordPolicyValidation(t *testing.T) {
	testDefs := []testDef{
		{
			// The test-user-pw-policy will be the selected user and without mutations has a 4 length pw.
			name: "TestNegUserPasswordTooShort",
			mutations: patchMap{"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
				MinLength: util.IntPtr(8),
			})},
			shouldFail:     true,
			expectedErrors: []string{"initial password for user `test-user-pw-policy` does not comply with the password policy of cluster"},
		},
		{
			name: "TestUserPasswordValid",
			mutations: patchMap{"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
				MinLength: util.IntPtr(4),
			})},
			shouldFail: false,
		},
		{
			name: "TestNegUserPasswordNoUppercase",
			mutations: patchMap{
				"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
					EnforceUppercase: util.BoolPtr(true)}),
				"test-user-pw-policy": jsonpatch.NewPatchSet().Replace("/spec/authSecret", "user-password-nouppercase")},
			shouldFail:     true,
			expectedErrors: []string{"initial password for user `test-user-pw-policy` does not comply with the password policy of cluster"},
		},
		{
			name: "TestUserPasswordNoUppercaseValid",
			mutations: patchMap{
				"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
					EnforceUppercase: util.BoolPtr(false)}),
				"test-user-pw-policy": jsonpatch.NewPatchSet().Replace("/spec/authSecret", "user-password-nouppercase")},
			shouldFail: false,
		},
		{
			name: "TestNegUserPasswordNoLowercase",
			mutations: patchMap{
				"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
					EnforceLowercase: util.BoolPtr(true)}),
				"test-user-pw-policy": jsonpatch.NewPatchSet().Replace("/spec/authSecret", "user-password-nolowercase")},
			shouldFail:     true,
			expectedErrors: []string{"initial password for user `test-user-pw-policy` does not comply with the password policy of cluster"},
		},
		{
			name: "TestNegUserPasswordNoDigits",
			mutations: patchMap{
				"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
					EnforceDigits: util.BoolPtr(true)}),
				"test-user-pw-policy": jsonpatch.NewPatchSet().Replace("/spec/authSecret", "user-password-nodigits")},
			shouldFail:     true,
			expectedErrors: []string{"initial password for user `test-user-pw-policy` does not comply with the password policy of cluster"},
		},
		{
			name: "TestNegUserPasswordNoSpecialChars	",
			mutations: patchMap{
				"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
					EnforceSpecialChars: util.BoolPtr(true)}),
				"test-user-pw-policy": jsonpatch.NewPatchSet().Replace("/spec/authSecret", "user-password-nospecialchars")},
			shouldFail:     true,
			expectedErrors: []string{"initial password for user `test-user-pw-policy` does not comply with the password policy of cluster"},
		},
		{
			name: "TestPasswordPolicyMinLengthInvalidUserNotSelected",
			mutations: patchMap{
				"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/rbac/selector", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"someLabel": "some-value",
					},
				}).Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
					EnforceDigits: util.BoolPtr(true)}),
				"test-user-pw-policy": jsonpatch.NewPatchSet().Replace("/spec/authSecret", "user-password-nodigits"),
			},
			shouldFail: false,
		},
		{
			name: "TestNegPasswordPolicyMinLengthInvalidUserNotSelected",
			mutations: patchMap{
				"password-policy-test-cluster": jsonpatch.NewPatchSet().Add("/spec/security/rbac/selector", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"someLabel": "some-value",
					},
				}).Add("/spec/security/passwordPolicy", &couchbasev2.PasswordPolicySpec{
					EnforceDigits: util.BoolPtr(true)}),
				"test-user-pw-policy": jsonpatch.NewPatchSet().Replace("/spec/authSecret", "user-password-short").Add("/metadata/labels", map[string]string{
					"someLabel": "some-value",
				})},
			shouldFail:     true,
			expectedErrors: []string{"initial password for user `test-user-pw-policy` does not comply with the password policy of cluster"},
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-user.yaml"})
}

func TestBucketStorageBackendValidationCreate(t *testing.T) {
	testDefs := []testDef{
		{
			name: "TestValidateMagma",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma"),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateCouchstore",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "couchstore"),
			},
			shouldFail: false,
		},
		{
			name: "TestNegValidateMagmaPre71",
			mutations: patchMap{
				"bucket0":  jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma"),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.0.0"),
			},
			shouldFail:     true,
			expectedErrors: []string{`magma storage backend requires Couchbase Server version 7.1.0 or later`},
		},
		{
			name: "TestValidateMagmaPre80",
			mutations: patchMap{
				"bucket0":  jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma").Replace("/spec/memoryQuota", "1024Mi"),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.5"),
			},
			shouldFail: false,
		},
		{
			name: "TestNegValidateMagmaPre80MemQuotaDefault",
			mutations: patchMap{
				"bucket0":  jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma"),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.5"),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.memoryQuota must be greater than or equal to 1024Mi for magma buckets pre Couchbase Server 8.0.0`},
		},
		{
			name: "TestNegValidateMagmaPre80MemoryQuota",
			mutations: patchMap{
				"bucket0":  jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma").Replace("/spec/memoryQuota", "1023Mi"),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.5"),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.memoryQuota must be greater than or equal to 1024Mi for magma buckets pre Couchbase Server 8.0.0`},
		},
		{
			name: "TestNegValidateMagmaPre80MemoryQuotaSelector",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma").Replace("/spec/memoryQuota", "512Mi").Add("/metadata/labels", map[string]string{
					"someLabel": "some-value",
				}),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.5").Add("/spec/buckets/selector", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"someLabel": "some-value",
					},
				}),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.memoryQuota must be greater than or equal to 1024Mi for magma buckets pre Couchbase Server 8.0.0`},
		},
		{
			name: "TestValidateMagmaPre80MemoryQuotaSelector",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "magma").Replace("/spec/memoryQuota", "1Gi").Add("/metadata/labels", map[string]string{
					"someLabel": "some-value",
				}),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.5").Add("/spec/buckets/selector", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"someLabel": "some-value",
					},
				}),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateCouchstoreValidPre80",
			mutations: patchMap{
				"bucket0":  jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "couchstore"),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.5"),
			},
			shouldFail: false,
		},
		{
			name: "TestNegValidateCouchstoreNumVBuckets",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "couchstore").Add("/spec/numVBuckets", 1024),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.numVBuckets can only be set for magma buckets`},
		},
		{
			name: "TestNegValidateCouchstoreFromClusterDefaultVBuckets",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Add("/spec/numVBuckets", 1024),
				"cluster0": jsonpatch.NewPatchSet().Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.defaultStorageBackend": "couchstore",
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.numVBuckets can only be set for magma buckets`},
		},
		{
			name: "TestNegValidateMagmaEvictionPolicy",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/evictionPolicy", couchbasev2.CouchbaseBucketEvictionPolicyValueOnly),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.evictionPolicy must be fullEviction for magma buckets`},
		},
		{
			name: "TestNegValidateMagmaEnableIndexReplica",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/enableIndexReplica", true),
			},
			shouldFail:     true,
			expectedErrors: []string{`cannot set spec.enableIndexReplica to true for magma buckets`},
		},
		{
			name: "TestNegValidateMagmaClusterDefaultNumVBucketsOnPre80",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/numVBuckets", 1024).Replace("/spec/memoryQuota", "1024Mi"),
				"cluster0": jsonpatch.NewPatchSet().Replace("/spec/image", "couchbase/server:7.6.5").Add("/metadata/annotations", map[string]string{
					"cao.couchbase.com/buckets.defaultStorageBackend": "magma",
				})},
			shouldFail:     true,
			expectedErrors: []string{`spec.numVBuckets cannot be set for magma buckets pre Couchbase Server 8.0.0`},
		},
		{
			name: "TestNegValidateCouchstoreNumVBuckets",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/storageBackend", "couchstore").Add("/spec/numVBuckets", 1024),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.numVBuckets can only be set for magma buckets`},
		},
		{
			name: "TestNegValidateMagmaNumVBuckets",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/numVBuckets", 1024),
			},
			shouldFail:     true,
			expectedErrors: []string{`spec.memoryQuota must be greater than or equal to 1024Mi when numVBuckets is 1024`},
		},
		{
			name: "TestValidateMagmaNumVBuckets1024",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/numVBuckets", 1024).Replace("/spec/memoryQuota", "1024Mi"),
			},
			shouldFail: false,
		},
		{
			name: "TestValidateMagmaNumVBuckets128",
			mutations: patchMap{
				"bucket0": jsonpatch.NewPatchSet().Replace("/spec/numVBuckets", 128),
			},
			shouldFail: false,
		},
	}

	runValidationTest(t, testDefs, validationContext{operation: operationCreate, validationFile: "validation-storagebackend.yaml"})
}
