package framework

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/version"
	"github.com/couchbase/couchbase-operator/test/e2e/analyzer"
	"github.com/couchbase/couchbase-operator/test/e2e/clustercapabilities"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/couchbase-operator/test/e2e/util"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	autoscalev2 "k8s.io/client-go/kubernetes/typed/autoscaling/v2beta2"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
)

// Init performs one time only initialization of the framework.  Dynamic calls to these
// functions will result in race conditions and spurious failures.
func Init() error {
	fmt.Println("couchbase-operator-certification", version.WithBuildNumber(), "revision:", revision.Revision())

	// Register CouchbaseCluster and CustomResourceDefinition types with the main library.
	if err := v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := couchbasev2.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := apiextensionsv1.SchemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := configure(); err != nil {
		return err
	}

	if err := preflight(); err != nil {
		return err
	}

	if err := setup(); err != nil {
		return err
	}

	return nil
}

const (
	// Not defined by the library, usually due to ordering mismatches across
	// architectures. This is the number of processes/PIDs/TIDs that are allowed
	// within the container.
	RLIMIT_NPROC = 6 // nolint:golint,stylecheck,revive
)

// rlimitCheck defines an rlimit check.
type rlimitCheck struct {
	// metric is a textual representation of the metric.
	metric string

	// resource is the resource number as Linux understands it.  See:
	// /usr/include/asm-generic/resource.h
	// /usr/include/x86_64-linux-gnu/bits/resource.h
	resource int

	// threhold is the minimum resource requirements that Couchbase server
	// needs to function.
	threshold uint64
}

// rlimitChecks is the set of preflight rlimit checks we need to perform before
// allowing the test to proceed.  If these fail, then your platform is not going
// to be able to run Couchbase reliably, let alone pass any tests.
var rlimitChecks = []rlimitCheck{
	// https://docs.couchbase.com/server/7.0/install/non-root.html
	{
		metric:    "Number of processes",
		resource:  RLIMIT_NPROC,
		threshold: 10000,
	},
	{
		metric:    "Number of open files",
		resource:  syscall.RLIMIT_NOFILE,
		threshold: 70000,
	},
}

// preflight checks the platform is capable of running Couchbase before allowing
// the test framework to run.
func preflight() error {
	logrus.Info(util.PrettyHeading("Platform Preflight Checks"))

	fails := 0

	for _, check := range rlimitChecks {
		limit := &syscall.Rlimit{}

		if err := syscall.Getrlimit(check.resource, limit); err != nil {
			return err
		}

		pass := limit.Max >= check.threshold

		value := strconv.FormatUint(limit.Max, 10)
		if limit.Max == ^uint64(0) {
			value = "unlimited"
		}

		result := types.ResultTypePass

		if !pass {
			result = types.ResultTypeFail
			fails++
		}

		logrus.Infof("%s = %s (>= %d) %s", check.metric, value, check.threshold, util.PrettyResult(result))
	}

	if fails > 0 {
		return fmt.Errorf("%d preflight checks failed", fails)
	}

	return nil
}

func configure() (err error) {
	// Provide some sane defaults.
	params := &Framework{
		ServiceAccountName: "couchbase-operator",
		MinioAccessKey:     e2eutil.RandomString(8),
		MinioSecretID:      e2eutil.RandomString(8),
		MinioRegion:        "us-west-1",
	}

	var platform string

	var registries RegistryConfigValue

	var tests TestConfigValue

	var suites SuiteConfigValue

	var suiteSuffix string

	podCreateTimeout := durationVar{
		value: 5 * time.Minute,
	}

	compression := bucketCompression{
		value: couchbasev2.CouchbaseBucketCompressionModePassive,
	}

	bucket := bucketType{
		value: e2eutil.BucketTypeCouchbase,
	}

	istioMode := newIstioMTLSModeFlag()

	// CLI based configuration (CI/computer friendly)
	flag.StringVar(&params.KubeType, "platform-type",
		"kubernetes",
		"Controls handling of security settings. Either 'kubernetes' or 'openshift'.")
	flag.StringVar(&platform, "platform-vendor",
		"",
		"Controls handling of platform specific behavior. Either 'aws', 'gce' or 'azure'.")
	flag.StringVar(&params.OpImage, "operator-image",
		operatorImageDefault,
		"Docker image to use for the operator.")
	flag.StringVar(&params.AdmissionControllerImage, "admission-image",
		admissionImageDefault,
		"Docker image to use for the admission controller.")
	flag.StringVar(&params.SyncGatewayImage, "mobile-image",
		mobileImageDefault,
		"Docker image to use for couchbase mobile.")
	flag.StringVar(&params.CouchbaseServerImage, "server-image",
		serverImageDefault,
		"Docker image to use for couchbase server.")
	flag.StringVar(&params.CouchbaseServerImageUpgrade, "server-image-upgrade",
		serverImageUpgradeFromDefault,
		"Docker image to use for couchbase server upgrades to upgrade from.")
	flag.StringVar(&params.CouchbaseExporterImage, "exporter-image",
		exporterImageDefault,
		"Docker image to use for the couchbase exporter.")
	flag.StringVar(&params.CouchbaseExporterImageUpgrade, "exporter-image-upgrade",
		exporterImageUpgradeFromDefault,
		"Docker image to use for couchbase exporter upgrades to upgrade from.")
	flag.StringVar(&params.CouchbaseBackupImage, "backup-image",
		backupImageDefault,
		"Docker image to use for couchbase backup.")
	flag.StringVar(&params.CouchbaseLoggingImage, "logging-image",
		loggingImageDefault,
		"Docker image to use for couchbase log shipping.")
	flag.StringVar(&params.CouchbaseLoggingImageUpgrade, "logging-image-upgrade",
		loggingImageUpgradeFromDefault,
		"Docker image to use for couchbase log shipping upgrades to upgrade from.")
	flag.StringVar(&params.StorageClassName, "storage-class",
		"",
		"Storage class to use, platform default if not specified.")
	flag.Var(&bucket, "bucket-type",
		"Bucket type to use.  Either 'couchbase', 'ephemeral' or 'memcached'.")
	flag.Var(&compression, "compression-mode",
		"Compression mode to use.  Either 'off', 'passive' or 'active'.")
	flag.StringVar(&params.S3Region, "s3-region",
		"us-west-2",
		"S3 region to use for backup.")
	flag.StringVar(&params.S3AccessKey, "s3-access-key",
		"",
		"S3 access key to use for backup.")
	flag.StringVar(&params.S3SecretID, "s3-secret-id",
		"",
		"S3 secret ID to use for backup.")

	flag.StringVar(&suiteSuffix, "suite-suffix",
		"",
		"Suffix to apply to suite name in JUnit results, useful when running multiple versions of the same suite in parallel.")
	flag.BoolVar(&params.CollectLogs, "collect-logs",
		false,
		"Whether to collect logs on failure.  These will be saved in the /logs directory.")
	flag.BoolVar(&params.CollectServerLogsOnFailure, "collect-server-logs",
		false,
		"Whether to collect Couchbase Server logs on failure.")
	flag.Var(&registries, "registry",
		"Container image registry configuration e.g. SERVER,USERNAME,PASSWORD.  This will be added as an image pull secret.  May be specified multiple times.")
	flag.Var(&suites, "suite",
		"Test suites to run.  Either 'validation', 'sanity', 'p0' or 'p1'.  May be specified more than once.")
	flag.Var(&tests, "test",
		"Individual test to run.  May be specified more than once.")
	flag.BoolVar(&util.UseANSIColor, "color",
		false,
		"Prettify output.")
	flag.IntVar(&params.DocsCount, "docs",
		10,
		"The number of Documents created during tests.")
	flag.StringVar(&params.LogLevel, "log-level",
		"debug",
		"Log level to use.  This affects Couchbase Autonomous Operator logs.  Either 'info', or 'debug'")
	flag.Var(&podCreateTimeout, "pod-creation-timeout",
		"Time before giving up on pod creation.  Platforms with cluster autoscaling may require a larger value e.g. 15m.")
	flag.BoolVar(&params.EnableIstio, "istio",
		false,
		"Enable istio injection.  This annotates per-test namespaces with Istio injection, and enables any Operator specific workarounds.")
	flag.Var(&istioMode, "istio-mtls-mode",
		"Select the istio mTLS policy.  Can be 'PERMISSIVE' or 'STRICT'.")
	flag.BoolVar(&params.DynamicPlatform, "dynamic-platform",
		false,
		"Enable dynamic platform support e.g. GKE Autopilot, AWS Fargate or anything with cluster autoscaling enabled.")
	flag.BoolVar(&params.IPv6, "ipv6",
		false,
		"Force the use use of IPv6 with Couchbase Server.")
	flag.Var(&params.PodImagePullPolicy, "image-pull-policy",
		"Image pull policy to use for Operator and Admission Pods.")

	flag.Parse()

	if util.UseANSIColor {
		logrus.SetFormatter(&logrus.TextFormatter{ForceColors: true})
	}

	params.RegistryConfigs = registries.values
	params.PodCreateTimeout = podCreateTimeout.value
	params.Platform = couchbasev2.PlatformType(platform)
	params.CompressionMode = compression.value
	params.BucketType = bucket.value
	params.IstioTLSMode = istioMode.String()

	// Hack... if nothing is specified then it's likely the user will
	// want platform certification.
	if len(suites.values) == 0 && len(tests.values) == 0 {
		suites.values = append(suites.values, "platform")
	}

	SelectedTests = TestDefinitions.Select(suites.values, tests.values)
	for _, test := range SelectedTests {
		analyzerSuiteName := test.selectedTag

		if suiteSuffix != "" {
			analyzerSuiteName += "-" + suiteSuffix
		}

		analyzer.RegisterTest(analyzerSuiteName, test.Name())
	}

	Global = params

	return nil
}

func createOperatorDeployment(k8s *types.Cluster, operatorImage string, podCreateTimeout time.Duration, logLevel string) *appsv1.Deployment {
	deployment := config.GetOperatorDeployment(operatorImage, k8s.PullSecrets, podCreateTimeout, logLevel)

	return deployment
}

// setup setups a test framework and points "Global" to it.
func setup() error {
	logDir, err := makeLogDir()
	if err != nil {
		return err
	}

	Global.LogDir = logDir

	cluster, err := createKubeClusterObject()
	if err != nil {
		return err
	}

	Global.ClusterSpec = []*types.Cluster{
		cluster,
	}

	Global.CbopinfoPath = "/cao"

	if len(Global.RegistryConfigs) > 0 {
		logrus.Info(util.PrettyHeading("Docker Registries"))

		for _, registry := range Global.RegistryConfigs {
			logrus.Info(" →  server: " + registry.Server)
			logrus.Info("    username: " + registry.Username)
			logrus.Info("    password: " + strings.Repeat("*", len(registry.Password)))
		}
	}

	logrus.Info(util.PrettyHeading("Container Images"))
	logrus.Info(" →  couchbase operator: " + Global.OpImage)
	logrus.Info(" →  couchbase admission controller: " + Global.AdmissionControllerImage)
	logrus.Info(" →  couchbase server: " + Global.CouchbaseServerImage)
	logrus.Info(" →  couchbase server upgrade: " + Global.CouchbaseServerImageUpgrade)
	logrus.Info(" →  couchbase sync gateway: " + Global.SyncGatewayImage)
	logrus.Info(" →  couchbase exporter: " + Global.CouchbaseExporterImage)
	logrus.Info(" →  couchbase exporter upgrade: " + Global.CouchbaseExporterImageUpgrade)
	logrus.Info(" →  couchbase backup: " + Global.CouchbaseBackupImage)
	logrus.Info(" →  couchbase logging: " + Global.CouchbaseLoggingImage)
	logrus.Info(" →  couchbase logging upgrade: " + Global.CouchbaseLoggingImageUpgrade)

	logrus.Info(util.PrettyHeading("Framework Configuration"))
	logrus.Info(" →  Bucket Type: " + Global.BucketType)
	logrus.Info(" →  Bucket Compression Mode: " + Global.CompressionMode)
	logrus.Info(" →  Documents: " + strconv.Itoa(Global.DocsCount))
	logrus.Info(" →  Logging Level: " + Global.LogLevel)

	logrus.Info(util.PrettyHeading("Kubernetes"))
	logrus.Info(" →  storage class: " + Global.StorageClassName)

	logrus.Info(util.PrettyHeading("Logs"))
	logrus.Info(" →  directory: " + Global.LogDir)

	for i, k8s := range Global.ClusterSpec {
		logrus.Info(util.PrettyHeading(fmt.Sprintf("Configuring Cluster %d", i)))

		if err = Global.SetupFramework(k8s); err != nil {
			return err
		}

		if err := updateKubernetesClusterDynamicClient(k8s); err != nil {
			return err
		}
	}

	return nil
}

func createKubeClusterObject() (*types.Cluster, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	// Clients are rate limited to 5 queries per-second by default, override the
	// defaults or whatever the provider has specified :D
	config.QPS = 1000
	config.Burst = 1000

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}

	// Istio hack, because it's rubbish and starts the container before networking works.
	callback := func() error {
		if _, err := discoveryClient.ServerVersion(); err != nil {
			return err
		}

		return nil
	}

	if err := retryutil.RetryFor(time.Minute, callback); err != nil {
		return nil, fmt.Errorf("unable to poll kubernetes version, check authentication tokens are enabled, any network sidecars are running and no firewalls are preventing communication")
	}

	groupresources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, err
	}

	restMapper := restmapper.NewDiscoveryRESTMapper(groupresources)

	cluster := &types.Cluster{
		Config:          config,
		CRClient:        client.MustNew(config),
		KubeClient:      kubernetes.NewForConfigOrDie(config),
		DynamicClient:   dynamic.NewForConfigOrDie(config),
		AutoscaleClient: autoscalev2.NewForConfigOrDie(config),
		APIRegClient:    apiregistrationv1.NewForConfigOrDie(config),
		RESTMapper:      restMapper,
		Platform:        string(Global.Platform),
		PlatformType:    Global.KubeType,
		IPv6:            Global.IPv6,
		DynamicPlatform: Global.DynamicPlatform,
	}

	return cluster, nil
}

// updateKubernetesCluster is called *after* the cluster has been initialized (e.g. after
// CRDs have been installed initially using the typed clients).  This allows the dynamic
// client to pickup the new custom resource types via the discocvery API.
func updateKubernetesClusterDynamicClient(k8s *types.Cluster) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(k8s.Config)
	if err != nil {
		return err
	}

	groupresources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return err
	}

	k8s.RESTMapper = restmapper.NewDiscoveryRESTMapper(groupresources)

	return nil
}

func recreateCRDs(k8s *types.Cluster) error {
	clientSet, err := clientset.NewForConfig(k8s.Config)
	if err != nil {
		return fmt.Errorf("failed to create clientset object: %w", err)
	}

	crds, err := clientSet.ApiextensionsV1().CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list CRDs: %w", err)
	}

	for i := range crds.Items {
		crd := &crds.Items[i]

		if crd.Spec.Group == "couchbase.com" {
			if err := clientSet.ApiextensionsV1().CustomResourceDefinitions().Delete(context.Background(), crd.Name, *metav1.NewDeleteOptions(0)); err != nil {
				return fmt.Errorf("failed to delete CRD: %w", err)
			}

			// wait for crd delete
			if err := retryutil.RetryFor(time.Minute, e2eutil.ResourceDeleted(k8s, crd)); err != nil {
				return err
			}
		}
	}

	crdsRaw, err := ioutil.ReadFile("/crd.yaml")
	if err != nil {
		return err
	}

	crdYAMLs := strings.Split(string(crdsRaw), "---\n")

	for _, crdYAML := range crdYAMLs {
		if strings.TrimSpace(crdYAML) == "" {
			continue
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal([]byte(crdYAML), crd); err != nil {
			return err
		}

		if _, err := clientSet.ApiextensionsV1().CustomResourceDefinitions().Create(context.Background(), crd, metav1.CreateOptions{}); err != nil {
			return err
		}

		if err := retryutil.RetryFor(time.Minute, e2eutil.ResourceCondition(k8s, crd, string(apiextensionsv1.Established), string(apiextensionsv1.ConditionTrue))); err != nil {
			return err
		}

		if err := retryutil.RetryFor(time.Minute, e2eutil.ResourceCondition(k8s, crd, string(apiextensionsv1.NamesAccepted), string(apiextensionsv1.ConditionTrue))); err != nil {
			return err
		}

		if err := e2eutil.ResourceCondition(k8s, crd, string(apiextensionsv1.NonStructuralSchema), string(apiextensionsv1.ConditionTrue)); err == nil {
			return fmt.Errorf("CRD %s reports as non-structural", crd.Name)
		}
	}

	return nil
}

const (
	// namespacePrefix is used to denote namespaces owned by this application.
	namespacePrefix = "test-"
)

// tells us if the underlying physical cluster on a host exists.
func (f *Framework) SetupFramework(k8s *types.Cluster) error {
	if !Global.DynamicPlatform {
		logrus.Info("Removing node taints")

		if err := e2eutil.UntaintAll(k8s); err != nil {
			return err
		}
	}

	logrus.Info("Cleaning-Up Namespaces")

	namespaces, err := k8s.KubeClient.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, namespace := range namespaces.Items {
		if !strings.HasPrefix(namespace.Name, namespacePrefix) {
			continue
		}

		if namespace.DeletionTimestamp != nil {
			continue
		}

		if err := k8s.KubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace.Name, *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}

	// delete and recreate CRDs
	logrus.Info("Recreating CRD")

	if err := recreateCRDs(k8s); err != nil {
		return err
	}

	// delete DAC
	logrus.Infof("Deleting admission controller")

	if err := deleteAdmissionController(k8s); err != nil {
		return err
	}

	// re-creating docker secrets
	logrus.Info("Recreating docker auth secret in default namespace")

	secrets, err := recreateDockerAuthSecret(k8s, "default")
	if err != nil {
		return err
	}

	// creating DAC
	logrus.Infof("Creating admission controller")

	if err := createAdmissionController(k8s, secrets); err != nil {
		return err
	}

	return nil
}

func (f *Framework) GetOperatorRestartCount(k8s *types.Cluster) (int32, error) {
	operatorPodName, err := e2eutil.GetOperatorName(k8s)
	if err != nil {
		return 0, err
	}

	var operatorPod *v1.Pod

	err = retryutil.RetryFor(time.Minute, func() error {
		operatorPod, err = k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(context.Background(), operatorPodName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return operatorPod.Status.ContainerStatuses[0].RestartCount, nil
}

// allocationLock is used to control access to consurrent cluster use.
var allocationLock sync.Mutex

// allocations is an array mapping cluster index to number of tests allocated.
var allocations []int

// isAllocationExcluded is used to force scheduling of a cluster onto one different
// from the list of exclusions.
func isAllocationExcluded(index int, exclusions []int) bool {
	for _, exclusion := range exclusions {
		if index == exclusion {
			return true
		}
	}

	return false
}

// allocate returns the least busy cluster.
func (f *Framework) allocate(exclusions ...int) int {
	allocationLock.Lock()
	defer allocationLock.Unlock()

	if allocations == nil {
		allocations = make([]int, len(f.ClusterSpec))
	}

	smallest := 1000
	smallestIndex := 0

	for index, value := range allocations {
		if isAllocationExcluded(index, exclusions) {
			continue
		}

		if value >= smallest {
			continue
		}

		smallest = value
		smallestIndex = index
	}

	allocations[smallestIndex]++

	return smallestIndex
}

// deallocate updates the allocator by freeing resources.
func (f *Framework) deallocate(index int) {
	allocationLock.Lock()
	defer allocationLock.Unlock()

	allocations[index]--
}

// TestOption allows individual tests to perform/inhibit certain actions explicitly.
type TestOption int

// NoOperator tells the framework not to provision an operator, validation tests don't
// need one for example.
const (
	NoOperator TestOption = 1 << iota
)

// optionSet determines whether an option is defined or not.
func optionSet(options []TestOption, option TestOption) bool {
	for _, o := range options {
		if o == option {
			return true
		}
	}

	return false
}

// SetupTest is called by parallelizable tests that require a single cluster
// to run in.
func (f *Framework) SetupTest(t *testing.T, o ...TestOption) (*types.Cluster, func()) {
	// This will stop execution of the test here, it will allow the underlying
	// go testing framework to release jobs based on the requested parallelism.
	t.Parallel()

	// Schedule a cluster to run on.
	index1 := f.allocate()

	cluster1, cleanup1 := f.setupCluster(t, index1, o)

	// Start the test.
	reporter := analyzer.New()

	cleanup := func() {
		// Report the test status.
		reporter.Report(t, recover())

		cleanup1()
	}

	return cluster1, cleanup
}

// SetupTestExclusive is called by non-parallelizable tests that require a single cluster
// to run in.
func (f *Framework) SetupTestExclusive(t *testing.T, o ...TestOption) (*types.Cluster, func()) {
	// Schedule a cluster to run on.
	index1 := f.allocate()

	cluster1, cleanup1 := f.setupCluster(t, index1, o)

	// Start the test.
	reporter := analyzer.New()

	cleanup := func() {
		// Report the test status.
		reporter.Report(t, recover())

		cleanup1()
	}

	return cluster1, cleanup
}

// SetupTestRemote is called by parallelizable tests that require a local and a
// remote cluster to run in.
func (f *Framework) SetupTestRemote(t *testing.T, o ...TestOption) (*types.Cluster, *types.Cluster, func()) {
	// This will stop execution of the test here, it will allow the underlying
	// go testing framework to release jobs based on the requested parallelism.
	t.Parallel()

	// Schedule clusters to run on.
	index1 := f.allocate()
	index2 := f.allocate(index1)

	cluster1, cleanup1 := f.setupCluster(t, index1, o)
	cluster2, cleanup2 := f.setupCluster(t, index2, o)

	// Start the test.
	reporter := analyzer.New()

	cleanup := func() {
		// Report the test status.
		reporter.Report(t, recover())

		cleanup1()
		cleanup2()
	}

	return cluster1, cluster2, cleanup
}

func (f *Framework) SetupSubTest(t *testing.T) func() {
	analyzer.RegisterSubTest(t.Name())

	reporter := analyzer.New()

	return func() {
		reporter.Report(t, recover())
	}
}

// setupCluster takes an allocated cluster and makes a virtual cluster i.e.
// unique namespace to run in, it then configures and starts the operator.
// It returns a test-ready cluster and a clean up function to perform logging
// and namespace deletion.
func (f *Framework) setupCluster(t *testing.T, index int, o []TestOption) (*types.Cluster, func()) {
	cluster := f.ClusterSpec[index].Copy()

	// Create a namespace.
	namespace := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namespacePrefix,
			Labels: map[string]string{
				"istio-injection": constants.EnabledValue,
			},
		},
	}

	var err error

	namespace, err = cluster.KubeClient.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	cluster.Namespace = namespace.Name

	if f.EnableIstio && f.IstioTLSMode != "" {
		policy := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "security.istio.io/v1beta1",
				"kind":       "PeerAuthentication",
				"metadata": map[string]interface{}{
					"name": "default",
				},
				"spec": map[string]interface{}{
					"mtls": map[string]interface{}{
						"mode": f.IstioTLSMode,
					},
				},
			},
		}

		gvr := schema.GroupVersionResource{
			Group:    "security.istio.io",
			Version:  "v1beta1",
			Resource: "peerauthentications",
		}

		if _, err := cluster.DynamicClient.Resource(gvr).Namespace(namespace.Name).Create(context.Background(), policy, metav1.CreateOptions{}); err != nil {
			e2eutil.Die(t, err)
		}
	}

	// Setup any pull secrets in the new namespace.
	secrets, err := recreateDockerAuthSecret(cluster, cluster.Namespace)
	if err != nil {
		e2eutil.Die(t, err)
	}

	cluster.PullSecrets = secrets

	// Create the operator.
	if !optionSet(o, NoOperator) {
		args := []string{
			"create",
			"operator",
			"--image=" + f.OpImage,
			"--pod-creation-timeout=" + f.PodCreateTimeout.String(),
			"--log-level=" + f.LogLevel,
			"--namespace=" + cluster.Namespace,
			"--kubeconfig=" + cluster.KubeConfPath,
		}

		if cluster.Context != "" {
			args = append(args, "--context="+cluster.Context)
		}

		for _, secret := range secrets {
			args = append(args, "--image-pull-secret="+secret)
		}

		if f.PodImagePullPolicy.String() != "" {
			args = append(args, "--image-pull-policy="+f.PodImagePullPolicy.String())
		}

		if _, err := exec.Command("/cao", args...).CombinedOutput(); err != nil {
			e2eutil.Die(t, err)
		}

		// For waiting and for re-creation, cache the deployment by calling directly
		// into the configuration code.  Remember to populate the namespace too as
		// that's what the framework uses.
		cluster.OperatorDeployment = createOperatorDeployment(cluster, f.OpImage, f.PodCreateTimeout, f.LogLevel)
		cluster.OperatorDeployment.Namespace = cluster.Namespace

		if err := e2eutil.WaitUntilOperatorReady(cluster, 5*time.Minute); err != nil {
			e2eutil.Die(t, err)
		}

		if err := CreateBackupStuff(cluster); err != nil {
			e2eutil.Die(t, err)
		}
	}

	// Create anything that's static across all tests (bad).
	secret, err := e2eutil.CreateSecret(cluster, e2espec.NewDefaultSecret(cluster.Namespace))
	if err != nil {
		e2eutil.Die(t, err)
	}

	cluster.DefaultSecret = secret

	// Generate the clean up closure.
	cleanup := func() {
		logDir := filepath.Join(f.LogDir, t.Name(), cluster.Namespace)

		// Collect operator and logging sidecar logs
		if err := e2eutil.WriteLogs(cluster, logDir, ""); err != nil {
			t.Logf("Error: %v", err)
		}

		// Collect any kubernetes/server logs using cbopinfo call.
		if t.Failed() && f.CollectLogs {
			e2eutil.CollectLogs(t, cluster, logDir, f.CbopinfoPath, f.OpImage, f.CollectServerLogsOnFailure)
		}

		// Cleanup, which is now trivial.
		if err := cluster.KubeClient.CoreV1().Namespaces().Delete(context.Background(), cluster.Namespace, *metav1.NewDeleteOptions(0)); err != nil {
			logrus.Warnf("failed to delete namespace %s", cluster.Namespace)
		}

		// Update the scheduler.
		f.deallocate(index)
	}

	return cluster, cleanup
}

func makeLogDir() (string, error) {
	dir := generateLogDir()
	return dir, os.MkdirAll(dir, os.ModePerm)
}

func generateLogDir() string {
	t := time.Now()
	ts := t.Format(time.RFC3339)

	return filepath.Join("/artifacts", "logs", ts)
}

// TestRequirement is a type used to check if a cluster has the ability to run a test.
type TestRequirement struct {
	t          *testing.T
	kubernetes *types.Cluster
}

// Requires is the constructor for TestRequirement.
func Requires(t *testing.T, kubernetes *types.Cluster) *TestRequirement {
	return &TestRequirement{
		t:          t,
		kubernetes: kubernetes,
	}
}

// StaticCluster is a cluster that has a constant size, and you can actually
// test filling it up.  This is as opposed to a dynamic cluster that will keep
// on growing to fufil your capacity needs.
func (r *TestRequirement) StaticCluster() *TestRequirement {
	if Global.DynamicPlatform {
		r.t.Skip("Test unsupported on dynamic platform")
	}

	return r
}

// CouchbaseBucket is a non-memcached bucket in essence.
func (r *TestRequirement) CouchbaseBucket() *TestRequirement {
	if Global.BucketType == "memcached" {
		r.t.Skip("Memcached buckets unsupported")
	}

	return r
}

// NotVersion skips the test if the Couchbase version is buggy.
func (r *TestRequirement) NotVersion(v ...string) *TestRequirement {
	parts := strings.Split(Global.CouchbaseServerImage, ":")
	if len(parts) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseServerImage))
	}

	v1, err := couchbaseutil.NewVersion(parts[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts[1], err))
	}

	for _, version := range v {
		v2, err := couchbaseutil.NewVersion(version)
		if err != nil {
			r.t.Skip(fmt.Sprintf("malformed version: %s: %v", version, err))
		}

		if v1.Equal(v2) {
			r.t.Skip("Couchbase server image version not supported (defective)")
		}
	}

	return r
}

// HasS3Parameters skips the S3 tests if S3 parameters are not provided.
func (r *TestRequirement) HasS3Parameters() *TestRequirement {
	if Global.S3AccessKey == "" || Global.S3SecretID == "" {
		r.t.Skip("S3 Config parameters are not provided")
	}

	return r
}

// AtLeastVersion skips the test for Couchbase versions before this threshold.
func (r *TestRequirement) AtLeastVersion(v string) *TestRequirement {
	parts := strings.Split(Global.CouchbaseServerImage, ":")
	if len(parts) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseServerImage))
	}

	v1, err := couchbaseutil.NewVersion(parts[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts[1], err))
	}

	v2, err := couchbaseutil.NewVersion(v)
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", v, err))
	}

	if v1.Less(v2) {
		r.t.Skip("Couchbase Server Image version not supported (geriatric)")
	}

	return r
}

func (r *TestRequirement) BeforeVersion(v string) *TestRequirement {
	parts := strings.Split(Global.CouchbaseServerImage, ":")
	if len(parts) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseServerImage))
	}

	v1, err := couchbaseutil.NewVersion(parts[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts[1], err))
	}

	v2, err := couchbaseutil.NewVersion(v)
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", v, err))
	}

	if v2.Less(v1) {
		r.t.Skip("Couchbase Server Image version not supported (immature)")
	}

	return r
}

// Upgradable skips the test if the upgrade version is greater than or equal to the
// test version.
func (r *TestRequirement) Upgradable() *TestRequirement {
	if Global.CouchbaseServerImageUpgrade == "" {
		r.t.Skip("Upgrade version not specified")
	}

	parts1 := strings.Split(Global.CouchbaseServerImage, ":")
	if len(parts1) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseServerImage))
	}

	parts2 := strings.Split(Global.CouchbaseServerImageUpgrade, ":")
	if len(parts2) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseServerImage))
	}

	version, err := couchbaseutil.NewVersion(parts1[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts1[1], err))
	}

	upgrade, err := couchbaseutil.NewVersion(parts2[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts2[1], err))
	}

	if upgrade.GreaterEqual(version) {
		r.t.Skip("Upgrade from version greater than or equal to upgrade to version")
	}

	return r
}

func (r *TestRequirement) LoggingUpgradable() *TestRequirement {
	if Global.CouchbaseLoggingImageUpgrade == "" {
		r.t.Skip("Logging image upgrade version not specified")
	}

	if Global.CouchbaseLoggingImageUpgrade == "latest" {
		r.t.Skip("Cannot upgrade logging image from latest version")
	}

	parts1 := strings.Split(Global.CouchbaseLoggingImage, ":")
	if len(parts1) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseLoggingImage))
	}

	parts2 := strings.Split(Global.CouchbaseLoggingImageUpgrade, ":")
	if len(parts2) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseLoggingImageUpgrade))
	}

	if parts1[1] == parts2[1] {
		r.t.Skip("Logging image upgrade and base version are the same")
	}

	if parts1[1] == "latest" {
		return r
	}

	version, err := couchbaseutil.NewVersion(parts1[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts1[1], err))
	}

	upgrade, err := couchbaseutil.NewVersion(parts2[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts2[1], err))
	}

	if upgrade.GreaterEqual(version) {
		r.t.Skip(fmt.Sprintf("Logging image upgrade version %s greater than or equal to base version %s", parts2[1], parts1[1]))
	}

	return r
}

func (r *TestRequirement) ExporterUpgradable() *TestRequirement {
	if Global.CouchbaseExporterImageUpgrade == "" {
		r.t.Skip("Exporter upgrade version not specified")
	}

	if Global.CouchbaseExporterImageUpgrade == "latest" {
		r.t.Skip("Cannot upgrade Exporter from latest version")
	}

	parts1 := strings.Split(Global.CouchbaseExporterImage, ":")
	if len(parts1) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseExporterImage))
	}

	parts2 := strings.Split(Global.CouchbaseExporterImageUpgrade, ":")
	if len(parts2) != 2 {
		r.t.Skip(fmt.Sprintf("malformed image: %v", Global.CouchbaseExporterImageUpgrade))
	}

	if parts1[1] == parts2[1] {
		r.t.Skip("Exporter upgrade and base version are the same")
	}

	if parts1[1] == "latest" {
		return r
	}

	version, err := couchbaseutil.NewVersion(parts1[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts1[1], err))
	}

	upgrade, err := couchbaseutil.NewVersion(parts2[1])
	if err != nil {
		r.t.Skip(fmt.Sprintf("malformed version: %s: %v", parts2[1], err))
	}

	if upgrade.GreaterEqual(version) {
		r.t.Skip(fmt.Sprintf("Exporter upgrade version %s greater than or equal to base version %s", parts2[1], parts1[1]))
	}

	return r
}

// ServerGroups skips the test is there aren't enough server groups to play with.
func (r *TestRequirement) ServerGroups(i int) *TestRequirement {
	capabilities := clustercapabilities.MustNewCapabilities(r.t, r.kubernetes.KubeClient)

	if len(capabilities.AvailabilityZones) < i {
		r.t.Skip("Required number of availability zones not found")
	}

	return r
}

func (r *TestRequirement) getDefaultStorageClassName() (string, error) {
	scs, err := r.kubernetes.KubeClient.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, sc := range scs.Items {
		if _, ok := sc.Annotations["storageclass.kubernetes.io/is-default-class"]; ok {
			return sc.Name, nil
		}
	}

	return "", fmt.Errorf("no default storage class specified")
}

// DefaultAndExplicitStorageClass does what it says, looks for an implicit storage class
// and that an explcit named one is configured.
func (r *TestRequirement) DefaultAndExplicitStorageClass() *TestRequirement {
	if Global.StorageClassName == "" {
		r.t.Skip("No storage class name configured")
	}

	if _, err := r.getDefaultStorageClassName(); err != nil {
		r.t.Skip("No default storage class configured for platform")
	}

	return r
}

// ExpandableStorage skips the test if the storage class does not have allowVolumeExpansion set to True.
func (r *TestRequirement) ExpandableStorage() *TestRequirement {
	storageClassName := Global.StorageClassName

	if storageClassName == "" {
		name, err := r.getDefaultStorageClassName()
		if err != nil {
			e2eutil.Die(r.t, err)
		}

		storageClassName = name
	}

	sc, err := r.kubernetes.KubeClient.StorageV1().StorageClasses().Get(context.Background(), storageClassName, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(r.t, err)
	}

	if sc.AllowVolumeExpansion == nil || !*sc.AllowVolumeExpansion {
		r.t.Skip("Storage Class does not have allowVolumeExpansion=true")
	}

	return r
}

// Rethink a tongue in cheek way of saying the test doesn't work.  For example, if we were to
// simulate a rolling cluster upgrade, the test container would get killed!
func (r *TestRequirement) Rethink() *TestRequirement {
	r.t.Skip("test unstable")

	return r
}
