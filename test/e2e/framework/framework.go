package framework

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/analyzer"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"github.com/couchbase/couchbase-operator/test/e2e/util"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
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

	if err := apiextensionsv1beta1.SchemeBuilder.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := readYamlData(); err != nil {
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

// RegistryConfigValue allows multiple container image registries to be passed on the command line
// e.g. --registry https://index.docker.io/v1/,organization,password
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

func readYamlData() (err error) {
	// Provide some sane defaults.
	params := TestRunParam{
		ServiceAccountName: "couchbase-operator",
	}

	var platform string

	var clusters ClusterConfigValue

	var registries RegistryConfigValue

	var tests TestConfigValue

	var suites SuiteConfigValue

	// CLI based configuration (CI/computer friendly)
	flag.StringVar(&params.KubeType, "platform-type", "kubernetes", "Either kubernetes or openshift")
	flag.StringVar(&platform, "platform-vendor", "", "Either aws, gce or azure")
	flag.StringVar(&params.OperatorImage, "operator-image", "couchbase/couchbase-operator:v1", "Docker image to use for the operator")
	flag.StringVar(&params.AdmissionControllerImage, "admission-image", "couchbase/couchbase-operator-admission:v1", "Docker image to use for the admission controller")
	flag.StringVar(&params.SyncGatewayImage, "mobile-image", "couchbase/sync-gateway:2.8.0-enterprise", "Docker image to use for couchbase mobile")
	flag.StringVar(&params.CouchbaseServerImage, "server-image", "couchbase/server:6.5.1", "Docker image to use for couchbase server")
	flag.StringVar(&params.CouchbaseServerImageUpgrade, "server-image-upgrade", "couchbase/server:6.6.0", "Docker image to use for couchbase server upgrades")
	flag.StringVar(&params.CouchbaseExporterImage, "exporter-image", "couchbase/exporter:1.0.0", "Docker image to use for the couchbase exporter")
	flag.StringVar(&params.CouchbaseExporterImageUpgrade, "exporter-image-upgrade", "couchbase/exporter:1.0.2", "Docker image to use for couchbase exporter upgrades")
	flag.StringVar(&params.CouchbaseBackupImage, "backup-image", "couchbase/operator-backup:6.5.1-111", "Docker image to use for couchbase backup")
	flag.StringVar(&params.StorageClassName, "storage-class", "", "Storage class to use")
	flag.StringVar(&params.BucketType, "bucket-type", "couchbase", "Bucket type to use")
	flag.StringVar(&params.CompressionMode, "compression-mode", "passive", "Compression mode to use")
	flag.BoolVar(&params.CollectLogsOnFailure, "collect-logs", false, "Whether to collect logs on failure")
	flag.BoolVar(&params.CollectServerLogsOnFailure, "collect-server-logs", false, "Whether to collect logs on failure")
	flag.Var(&clusters, "cluster", "Kubernetes cluster configuration e.g. FILE,CONTEXT,NAMESPACE")
	flag.Var(&registries, "registry", "Container image registry configuration e.g. SERVER,USERNAME,PASSWORD")
	flag.Var(&suites, "suite", "Test suites to run")
	flag.Var(&tests, "test", "Individual test to run")
	flag.BoolVar(&util.UseANSIColor, "color", false, "Prettify output")

	// File based configuration (meat-space friendly)
	testConfigFilePath := flag.String("testconfig", "resources/test_config.yaml", "test_config.yaml path. eg: $HOME/test_config.yaml")

	flag.Parse()

	if util.UseANSIColor {
		logrus.SetFormatter(&logrus.TextFormatter{ForceColors: true})
	}

	params.ClusterConfigs = clusters.values
	params.RegistryConfigs = registries.values

	// We are using the CLI to configure if the suite or tests are explcitly stated.
	useCLI := len(suites.values) > 0 || len(tests.values) > 0

	// Use either the CLI parameters, or the YAML file.  I suspect the YAML
	// method will suffer a quick death...
	if useCLI {
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
		runtimeParams = params
	} else {
		logrus.Info("Using test_config file ", *testConfigFilePath)

		runtimeParams, err = readRuntimeConfig(*testConfigFilePath)
		if err != nil {
			return err
		}
	}

	for index, config := range runtimeParams.ClusterConfigs {
		if strings.HasPrefix(config.Config, "~/") {
			runtimeParams.ClusterConfigs[index].Config = strings.Replace(config.Config, "~", os.Getenv("HOME"), 1)
		}
	}

	// When using an explcit list of tests, fake a suite... otherwise load one
	// up from disk.
	if useCLI {
		// These values are way more friendly for CI, so it doesn't need to know
		// about internal file names.
		mapping := map[string]string{
			"validation": "TestCRDValidation",
			"sanity":     "TestSanity",
			"p0":         "TestP0",
			"p1":         "TestP1",
		}

		SuiteName = "custom"

		// For every suite that has been defined, buffer it up and add it
		// to our list.
		for _, suite := range suites.values {
			// For backwards compatibility, QE have hard coded file names
			// so default to this.  If this isn't one of those names, then
			// implicitly do the filename conversion.
			// TODO: There is literally no reason for this to be a file,
			// you may as well just make it a slice in code...
			suiteName := suite

			if !strings.HasPrefix(suite, "Test") {
				if _, ok := mapping[suite]; !ok {
					return fmt.Errorf("unable to find suite %s", suite)
				}

				suiteName = mapping[suite]
			}

			suiteFilePath := fmt.Sprintf("./resources/suites/%s.yaml", suiteName)

			data, err := getSuiteDataFromYml(suiteFilePath)
			if err != nil {
				return err
			}

			suiteData.TestCase = append(suiteData.TestCase, data.TestCase...)

			// Register tests with a suite in the analyzer.
			for _, test := range data.TestCase {
				analyzer.RegisterTest(suite, test)
			}
		}

		// For every test that has been defined, buffer it up and add it
		// to our list.
		suiteData.TestCase = append(suiteData.TestCase, tests.values...)

		// Register tests with a suite in the analyzer.
		for _, test := range tests.values {
			analyzer.RegisterTest("custom", test)
		}
	} else {
		suiteFilePath := "./resources/suites/" + runtimeParams.SuiteToRun + ".yaml"

		logrus.Info("Using suite file ", suiteFilePath)

		SuiteName = runtimeParams.SuiteToRun
		suiteData, err = getSuiteDataFromYml(suiteFilePath)
		if err != nil {
			return err
		}
	}

	return nil
}

// Returs time.Duration from given string
// Default return value: "2h0m0s".
func GetDuration(timeoutStr string) time.Duration {
	// Default timeout to 2 hours
	durationToReturn := (2 * time.Hour)

	pattern := regexp.MustCompile("^([0-9]+)([mhd])$")

	// Calculates only if valid pattern exists
	if pattern.MatchString(timeoutStr) {
		match := pattern.FindStringSubmatch(timeoutStr)

		timeoutVal, err := strconv.Atoi(match[1])
		if err != nil {
			return durationToReturn
		}

		timeoutDuration := time.Duration(timeoutVal)

		switch match[2] {
		case "m":
			durationToReturn = timeoutDuration * time.Minute
		case "h":
			durationToReturn = timeoutDuration * time.Hour
		case "d":
			durationToReturn = timeoutDuration * (time.Hour * 24)
		}
	}

	return durationToReturn
}

func createOperatorDeployment(k8s *types.Cluster, operatorImage string, podCreateTimeout fmt.Stringer) *appsv1.Deployment {
	deployment := config.GetOperatorDeployment("", operatorImage, k8s.PullSecrets, false, podCreateTimeout, "--zap-level", "debug")

	return deployment
}

// Setup setups a test framework and points "Global" to it.
func Setup() (err error) {
	// Initialize Global from runtime info
	Global = &Framework{
		KubeType:                      runtimeParams.KubeType,
		OpImage:                       runtimeParams.OperatorImage,
		SkipTeardown:                  runtimeParams.SkipTearDown,
		CollectLogs:                   runtimeParams.CollectLogsOnFailure,
		CollectServerLogsOnFailure:    runtimeParams.CollectServerLogsOnFailure,
		SuiteYmlData:                  suiteData,
		CouchbaseServerImage:          runtimeParams.CouchbaseServerImage,
		CouchbaseServerImageUpgrade:   runtimeParams.CouchbaseServerImageUpgrade,
		PodCreateTimeout:              5 * time.Minute,
		SyncGatewayImage:              runtimeParams.SyncGatewayImage,
		CouchbaseExporterImage:        runtimeParams.CouchbaseExporterImage,
		CouchbaseExporterImageUpgrade: runtimeParams.CouchbaseExporterImageUpgrade,
		CouchbaseBackupImage:          runtimeParams.CouchbaseBackupImage,
		BucketType:                    runtimeParams.BucketType,
		CompressionMode:               runtimeParams.CompressionMode,
		EnableIstio:                   runtimeParams.EnableIstio,
	}

	if runtimeParams.StorageClassName != "" {
		Global.StorageClassName = &runtimeParams.StorageClassName
	}

	Global.LogDir, err = makeLogDir()
	if err != nil {
		return err
	}

	Global.ClusterSpec = make([]*types.Cluster, len(runtimeParams.ClusterConfigs))

	for i, kubeConf := range runtimeParams.ClusterConfigs {
		clusterSpec, cerr := createKubeClusterObject(kubeConf)
		if cerr != nil {
			return cerr
		}

		Global.ClusterSpec[i] = clusterSpec
	}

	// Set any defaults.
	if Global.SyncGatewayImage == "" {
		Global.SyncGatewayImage = "couchbase/sync-gateway:2.7.0-enterprise"
	}

	// Setting required spec values from test_config yaml
	e2espec.SetStorageClassName(Global.StorageClassName)
	e2espec.SetCouchbaseServerImage(runtimeParams.CouchbaseServerImage)
	e2espec.SetPlatform(runtimeParams.Platform)
	e2espec.SetIstio(runtimeParams.EnableIstio)

	logrus.Info(util.PrettyHeading("Docker Registries"))

	for _, registry := range runtimeParams.RegistryConfigs {
		logrus.Info(" →  server: " + registry.Server)
		logrus.Info("    username: " + registry.Username)
		logrus.Info("    password: " + strings.Repeat("*", len(registry.Password)))
	}

	logrus.Info(util.PrettyHeading("Container Images"))
	logrus.Info(" →  couchbase operator: " + runtimeParams.OperatorImage)
	logrus.Info(" →  couchbase admission controller: " + runtimeParams.AdmissionControllerImage)
	logrus.Info(" →  couchbase server: " + runtimeParams.CouchbaseServerImage)
	logrus.Info(" →  couchbase server upgrade: " + runtimeParams.CouchbaseServerImageUpgrade)
	logrus.Info(" →  couchbase sync gateway: " + Global.SyncGatewayImage)
	logrus.Info(" →  couchbase exporter: " + runtimeParams.CouchbaseExporterImage)
	logrus.Info(" →  couchbase exporter upgrade: " + runtimeParams.CouchbaseExporterImageUpgrade)
	logrus.Info(" →  couchbase backup: " + runtimeParams.CouchbaseBackupImage)
	logrus.Info(" →  Bucket Type: " + runtimeParams.BucketType)
	logrus.Info(" →  Compression Mode: " + runtimeParams.CompressionMode)

	logrus.Info(util.PrettyHeading("Clusters"))

	for _, config := range Global.ClusterSpec {
		logrus.Info(" →  path: " + config.KubeConfPath)
		logrus.Info("    context: " + config.Context)
	}

	logrus.Info(util.PrettyHeading("Kubernetes"))
	logrus.Info(" →  storage class: " + runtimeParams.StorageClassName)
	logrus.Info(util.PrettyHeading("Logs"))
	logrus.Info(" →  directory: " + Global.LogDir)

	// Setup the cbopinfo absolute path so it will not change if we move directories
	wd, oserr := os.Getwd()
	if oserr != nil {
		return oserr
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
		return fmt.Errorf("failed to create clientset object: %v", err)
	}

	crds, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list CRDs: %v", err)
	}

	for _, crd := range crds.Items {
		if crd.Spec.Group == "couchbase.com" {
			if err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crd.Name, metav1.NewDeleteOptions(0)); err != nil {
				return fmt.Errorf("failed to delete CRD: %v", err)
			}

			// wait for crd delete
			if err := e2eutil.WaitForCRDDeletion(clientSet, crd.Name, time.Minute); err != nil {
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

		crd := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := yaml.Unmarshal([]byte(crdYAML), crd); err != nil {
			return err
		}

		if _, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd); err != nil {
			return err
		}
	}

	return nil
}

func (f *Framework) RemoveK8SNodeTaints(kubeClient kubernetes.Interface) error {
	logrus.Info("Marking all nodes as schedulable")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
		nodeTaintList := []v1.Taint{}

		k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to get node list: %v", err)
		}

		for nodeIndex := range k8sNodeList.Items {
			if err := e2eutil.SetNodeTaintAndSchedulableProperty(kubeClient, false, nodeTaintList, nodeIndex); err != nil {
				return fmt.Errorf("failed to update node taint: %v", err)
			}
		}

		return nil
	})
}

const (
	// namespacePrefix is used to denote namespaces owned by this application.
	namespacePrefix = "test-"
)

// tells us if the underlying physical cluster on a host exists.
func (f *Framework) SetupFramework(k8s *types.Cluster) error {
	if err := f.RemoveK8SNodeTaints(k8s.KubeClient); err != nil {
		return err
	}

	logrus.Info("Cleaning-Up Namespaces")

	namespaces, err := k8s.KubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
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

		if err := k8s.KubeClient.CoreV1().Namespaces().Delete(namespace.Name, metav1.NewDeleteOptions(0)); err != nil {
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

	if err := deleteAdmissionController(k8s.KubeClient); err != nil {
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

func (f *Framework) SetupCouchbaseOperator(k8s *types.Cluster) error {
	if _, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(k8s.OperatorDeployment); err != nil {
		return err
	}

	return e2eutil.WaitUntilOperatorReady(k8s, 5*time.Minute)
}

func (f *Framework) GetOperatorRestartCount(k8s *types.Cluster) (int32, error) {
	operatorPodName, err := e2eutil.GetOperatorName(k8s)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var operatorPod *v1.Pod

	err = retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
		operatorPod, err = k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Get(operatorPodName, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		return true, nil
	})
	if err != nil {
		return 0, err
	}

	return operatorPod.Status.ContainerStatuses[0].RestartCount, nil
}

func DeleteOperatorCompletely(k8s *types.Cluster, deploymentName string) error {
	if err := deleteOperator(k8s, deploymentName); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// On k8s 1.6.1, grace period isn't accurate. It took ~10s for operator pod to completely disappear.
	// We work around by increasing the wait time. Revisit this later.
	return retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
		_, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Get(deploymentName, metav1.GetOptions{})
		if err == nil {
			return false, err
		}

		if k8sutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}

		return false, err
	})
}

func deleteOperator(k8s *types.Cluster, deploymentName string) error {
	deletePropagation := metav1.DeletePropagationForeground

	deleteOpts := metav1.NewDeleteOptions(0)
	deleteOpts.PropagationPolicy = &deletePropagation

	if err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Delete(deploymentName, deleteOpts); err != nil {
		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}

	return nil
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
		reporter.Report(t)

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
		reporter.Report(t)

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
		reporter.Report(t)

		cleanup1()
		cleanup2()
	}

	return cluster1, cluster2, cleanup
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

	namespace, err = cluster.KubeClient.CoreV1().Namespaces().Create(namespace)
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
		cluster.OperatorDeployment = createOperatorDeployment(cluster, f.OpImage, f.PodCreateTimeout)

		if err := RecreateServiceAccount(cluster, cluster.OperatorDeployment.Name); err != nil {
			e2eutil.Die(t, err)
		}

		if err := recreateRoles(cluster, cluster.OperatorDeployment.Name); err != nil {
			e2eutil.Die(t, err)
		}

		if err := recreateRoleBindings(cluster); err != nil {
			e2eutil.Die(t, err)
		}

		if err := f.SetupCouchbaseOperator(cluster); err != nil {
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
		if err := cluster.KubeClient.CoreV1().Namespaces().Delete(cluster.Namespace, metav1.NewDeleteOptions(0)); err != nil {
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
