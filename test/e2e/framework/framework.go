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
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
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
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	autoscalev2 "k8s.io/client-go/kubernetes/typed/autoscaling/v2beta2"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"
)

// Init performs one time only initialization of the framework.  Dynamic calls to these
// functions will result in race conditions and spurious failures.
func Init() error {
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

	if err := setup(); err != nil {
		return err
	}

	return nil
}

// ClusterConfigValue allows multiple cluster configurations to be passed on the command line
// e.g. --cluster ~/.kube/conf,,default --cluster ~/kubeconfig,,remote.
type ClusterConfigValue struct {
	values []ClusterConfig
}

func (v *ClusterConfigValue) Set(value string) error {
	fields := strings.Split(value, ",")

	num := len(fields)
	if num > 2 {
		return fmt.Errorf("invalid cluster config value, expected FILE(,CONTEXT)")
	}

	config := ClusterConfig{
		Config: fields[0],
	}

	if num >= 2 {
		config.Context = fields[1]
	}

	v.values = append(v.values, config)

	return nil
}

func (v *ClusterConfigValue) String() string {
	return ""
}

// RegistryConfigValue allows multiple container image registries to be passed on the command
// line e.g:
// --registry https://index.docker.io/v1/,organization,password.
type RegistryConfigValue struct {
	values []RegistryConfig
}

func (v *RegistryConfigValue) Set(value string) error {
	fields := strings.Split(value, ",")
	if len(fields) != 3 {
		return fmt.Errorf("invalid cluster config value, expected SERVER,USERNAME,PASSWORD")
	}

	config := RegistryConfig{
		Server:   fields[0],
		Username: fields[1],
		Password: fields[2],
	}

	v.values = append(v.values, config)

	return nil
}

func (v *RegistryConfigValue) String() string {
	return ""
}

// TestConfigValue represents an explicit set of tests to run, so you can choose to
// not run a 12h suite and only a single 5m test.
type TestConfigValue struct {
	values []string
}

func (v *TestConfigValue) Set(value string) error {
	v.values = append(v.values, value)
	return nil
}

func (v *TestConfigValue) String() string {
	return ""
}

// SuiteConfigValue represents an explcit set of suites to run.
type SuiteConfigValue struct {
	values []string
}

func (v *SuiteConfigValue) Set(value string) error {
	v.values = append(v.values, value)
	return nil
}

func (v *SuiteConfigValue) String() string {
	return ""
}

type durationVar struct {
	value time.Duration
}

func (v *durationVar) Set(s string) error {
	value, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("duration invalid: %w", err)
	}

	v.value = value

	return nil
}

func (v *durationVar) Type() string {
	return "string"
}

func (v *durationVar) String() string {
	return v.value.String()
}

func configure() (err error) {
	// Provide some sane defaults.
	params := &Framework{
		ServiceAccountName: "couchbase-operator",
	}

	var platform string

	var clusters ClusterConfigValue

	var registries RegistryConfigValue

	var tests TestConfigValue

	var suites SuiteConfigValue

	var suiteSuffix string

	podCreateTimeout := durationVar{
		value: 5 * time.Minute,
	}

	// CLI based configuration (CI/computer friendly)
	flag.StringVar(&params.KubeType, "platform-type", "kubernetes", "Either kubernetes or openshift")
	flag.StringVar(&platform, "platform-vendor", "", "Either aws, gce or azure")
	flag.StringVar(&params.OpImage, "operator-image", "couchbase/couchbase-operator:v1", "Docker image to use for the operator")
	flag.StringVar(&params.AdmissionControllerImage, "admission-image", "couchbase/couchbase-operator-admission:v1", "Docker image to use for the admission controller")
	flag.StringVar(&params.SyncGatewayImage, "mobile-image", "couchbase/sync-gateway:2.8.2-enterprise", "Docker image to use for couchbase mobile")
	flag.StringVar(&params.CouchbaseServerImage, "server-image", "couchbase/server:6.6.2", "Docker image to use for couchbase server")
	flag.StringVar(&params.CouchbaseServerImageUpgrade, "server-image-upgrade", "couchbase/server:6.6.1", "Docker image to use for couchbase server upgrades to upgrade from")
	flag.StringVar(&params.CouchbaseExporterImage, "exporter-image", "couchbase/exporter:1.0.5", "Docker image to use for the couchbase exporter")
	flag.StringVar(&params.CouchbaseExporterImageUpgrade, "exporter-image-upgrade", "couchbase/exporter:1.0.3", "Docker image to use for couchbase exporter upgrades to upgrade from")
	flag.StringVar(&params.CouchbaseBackupImage, "backup-image", "couchbase/operator-backup:1.1.0", "Docker image to use for couchbase backup")
	flag.StringVar(&params.CouchbaseLoggingImage, "logging-image", "couchbase/fluent-bit:1.0.0", "Docker image to use for couchbase log shipping")
	flag.StringVar(&params.StorageClassName, "storage-class", "", "Storage class to use")
	flag.StringVar(&params.BucketType, "bucket-type", "couchbase", "Bucket type to use")
	flag.StringVar(&params.CompressionMode, "compression-mode", "passive", "Compression mode to use")
	flag.StringVar(&params.S3Region, "s3-region", "us-west-2", "S3 Region to use")
	flag.StringVar(&params.S3AccessKey, "s3-access-key", "", "S3 Access Key")
	flag.StringVar(&params.S3SecretID, "s3-secret-id", "", "S3 Secret ID")
	flag.StringVar(&suiteSuffix, "suite-suffix", "", "Suffix to apply to suite name in JUnit results, useful when running multiple versions of the same suite in parallel")
	flag.BoolVar(&params.CollectLogs, "collect-logs", false, "Whether to collect logs on failure")
	flag.BoolVar(&params.CollectServerLogsOnFailure, "collect-server-logs", false, "Whether to collect logs on failure")
	flag.Var(&clusters, "cluster", "Kubernetes cluster configuration e.g. FILE,CONTEXT,NAMESPACE")
	flag.Var(&registries, "registry", "Container image registry configuration e.g. SERVER,USERNAME,PASSWORD")
	flag.Var(&suites, "suite", "Test suites to run")
	flag.Var(&tests, "test", "Individual test to run")
	flag.BoolVar(&util.UseANSIColor, "color", false, "Prettify output")
	flag.IntVar(&params.DocsCount, "docs", 10, "The amount of Documents created during tests")
	flag.StringVar(&params.LogLevel, "log-level", "debug", "Log Level to use")
	flag.Var(&podCreateTimeout, "pod-creation-timeout", "Time before giving up on pod creation")
	flag.BoolVar(&params.EnableIstio, "istio", false, "Enable istio injection")

	flag.Parse()

	if util.UseANSIColor {
		logrus.SetFormatter(&logrus.TextFormatter{ForceColors: true})
	}

	params.ClusterConfigs = clusters.values
	params.RegistryConfigs = registries.values
	params.PodCreateTimeout = podCreateTimeout.value
	params.Platform = couchbasev2.PlatformType(platform)

	// If no cluster configurations were specified, then provide some
	// sane defaults.  At most we need two clusters in different namespaces
	// to run XDCR/client tests.
	if len(params.ClusterConfigs) == 0 {
		params.ClusterConfigs = []ClusterConfig{
			{
				Config: "~/.kube/config",
			},
		}
	}

	// Heretical use of global variables alert.
	Global = params

	for index, config := range Global.ClusterConfigs {
		if strings.HasPrefix(config.Config, "~/") {
			Global.ClusterConfigs[index].Config = strings.Replace(config.Config, "~", os.Getenv("HOME"), 1)
		}
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

	Global.ClusterSpec = make([]*types.Cluster, len(Global.ClusterConfigs))

	for i, kubeConf := range Global.ClusterConfigs {
		clusterSpec, err := createKubeClusterObject(kubeConf)
		if err != nil {
			return err
		}

		Global.ClusterSpec[i] = clusterSpec
	}

	// Set any defaults.
	if Global.SyncGatewayImage == "" {
		Global.SyncGatewayImage = "couchbase/sync-gateway:2.7.0-enterprise"
	}

	logrus.Info(util.PrettyHeading("Docker Registries"))

	for _, registry := range Global.RegistryConfigs {
		logrus.Info(" →  server: " + registry.Server)
		logrus.Info("    username: " + registry.Username)
		logrus.Info("    password: " + strings.Repeat("*", len(registry.Password)))
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

	logrus.Info(util.PrettyHeading("Framework Configuration"))
	logrus.Info(" →  Bucket Type: " + Global.BucketType)
	logrus.Info(" →  Compression Mode: " + Global.CompressionMode)
	logrus.Info(" →  Documents: " + strconv.Itoa(Global.DocsCount))
	logrus.Info(" →  Logging Level: " + Global.LogLevel)

	logrus.Info(util.PrettyHeading("Clusters"))

	for _, config := range Global.ClusterSpec {
		logrus.Info(" →  path: " + config.KubeConfPath)
		logrus.Info("    context: " + config.Context)
	}

	logrus.Info(util.PrettyHeading("Kubernetes"))
	logrus.Info(" →  storage class: " + Global.StorageClassName)
	logrus.Info(util.PrettyHeading("Logs"))
	logrus.Info(" →  directory: " + Global.LogDir)

	// Setup the cbopinfo absolute path so it will not change if we move directories
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	Global.CbopinfoPath = wd + "/../../build/bin/cbopinfo"

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

func createKubeClusterObject(c ClusterConfig) (*types.Cluster, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: c.Config},
		&clientcmd.ConfigOverrides{CurrentContext: c.Context},
	).ClientConfig()
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

	groupresources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, err
	}

	restMapper := restmapper.NewDiscoveryRESTMapper(groupresources)

	return &types.Cluster{
		Config:          config,
		CRClient:        client.MustNew(config),
		KubeClient:      kubernetes.NewForConfigOrDie(config),
		DynamicClient:   dynamic.NewForConfigOrDie(config),
		AutoscaleClient: autoscalev2.NewForConfigOrDie(config),
		APIRegClient:    apiregistrationv1.NewForConfigOrDie(config),
		RESTMapper:      restMapper,
		KubeConfPath:    c.Config,
		Context:         c.Context,
		Platform:        string(Global.Platform),
		PlatformType:    Global.KubeType,
	}, nil
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

	crdsRaw, err := ioutil.ReadFile("../../example/crd.yaml")
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
	if Global.Platform != "gke-autopilot" {
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
				"istio-injection": "enabled",
			},
		},
	}

	var err error

	namespace, err = cluster.KubeClient.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	cluster.Namespace = namespace.Name

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

		if _, err := exec.Command("../../build/bin/cbopcfg", args...).CombinedOutput(); err != nil {
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

		// Collect operator logs
		if err := e2eutil.WriteLogs(cluster, logDir, ""); err != nil {
			t.Logf("Error: %v", err)
		}

		// Collect any kubernetes/server logs.
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
	dir, err := generateLogDir()
	if err != nil {
		return "", err
	}

	return dir, os.MkdirAll(dir, os.ModePerm)
}

func generateLogDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	t := time.Now()
	ts := t.Format(time.RFC3339)

	return filepath.Join(cwd, "logs", ts), nil
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
	if Global.Platform == "gke-autopilot" {
		r.t.Skip("GKE Autopilot implements cluster autoscaling")
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

// DefaultAndExplicitStorageClass does what it says, looks for an implicit storage class
// and that an explcit named one is configured.
func (r *TestRequirement) DefaultAndExplicitStorageClass() *TestRequirement {
	if Global.StorageClassName == "" {
		r.t.Skip("No storage class name configured")
	}

	scs, err := r.kubernetes.KubeClient.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		r.t.Skip(fmt.Sprintf("Unable to list storage classes: %v", err))
	}

	for _, sc := range scs.Items {
		if _, ok := sc.Annotations["storageclass.kubernetes.io/is-default-class"]; ok {
			return r
		}
	}

	r.t.Skip("No default storage class configured for platform")

	return r
}

// ExpandableStorage skips the test if the storage class does not have allowVolumeExpansion set to True.
func (r *TestRequirement) ExpandableStorage() *TestRequirement {
	sc, err := r.kubernetes.KubeClient.StorageV1().StorageClasses().Get(context.Background(), Global.StorageClassName, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(r.t, err)
	}

	if sc.AllowVolumeExpansion == nil || !*sc.AllowVolumeExpansion {
		r.t.Skip("Storage Class does not have allowVolumeExpansion=true")
	}

	return r
}
