package framework

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
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

	// Start the timeout timer, after reading in suite configuration.
	startTimeoutTimer()

	return nil
}

// ClusterConfigValue allows multiple cluster configurations to be passed on the command line
// e.g. --cluster ~/.kube/conf,,default --cluster ~/kubeconfig,,remote
type ClusterConfigValue struct {
	values []ClusterConfig
}

func (v *ClusterConfigValue) Set(value string) error {
	fields := strings.Split(value, ",")
	if len(fields) != 3 {
		return fmt.Errorf("invalid cluster config value, expected FILE,CONTEXT,NAMESPACE")
	}

	config := ClusterConfig{
		Config:    fields[0],
		Context:   fields[1],
		Namespace: fields[2],
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

var useANSIColor bool

func readYamlData() (err error) {
	// Provide some sane defaults.
	params := TestRunParam{
		ServiceAccountName: "couchbase-operator",
	}

	var platform string

	var clusters ClusterConfigValue

	var registries RegistryConfigValue

	var tests TestConfigValue

	// CLI based configuration (CI/computer friendly)
	flag.StringVar(&params.KubeType, "platform-type", "kubernetes", "Either kubernetes or openshift")
	flag.StringVar(&platform, "platform-vendor", "", "Either aws, gce or azure")
	flag.StringVar(&params.OperatorImage, "operator-image", "couchbase/couchbase-operator:v1", "Docker image to use for the operator")
	flag.StringVar(&params.AdmissionControllerImage, "admission-image", "couchbase/couchbase-operator-admission:v1", "Docker image to use for the admission controller")
	flag.StringVar(&params.SyncGatewayImage, "mobile-image", "ouchbase/sync-gateway:2.7.0-enterprise", "Docker image to use for couchbase mobile")
	flag.StringVar(&params.CouchbaseServerImage, "server-image", "couchbase/server:6.5.0", "Docker image to use for couchbase server")
	flag.StringVar(&params.CouchbaseServerImageUpgrade, "server-image-upgrade", "couchbase/server:6.5.1", "Docker image to use for couchbase server upgrades")
	flag.StringVar(&params.CouchbaseExporterImage, "exporter-image", "couchbase/exporter:1.0.0", "Docker image to use for the couchbase exporter")
	flag.StringVar(&params.CouchbaseExporterImageUpgrade, "exporter-image-upgrade", "couchbase/exporter:1.0.2", "Docker image to use for couchbase exporter upgrades")
	flag.StringVar(&params.CouchbaseBackupImage, "backup-image", "couchbase/operator-backup:6.5.0", "Docker image to use for couchbase backup")
	flag.StringVar(&params.SuiteToRun, "suite", "", "Test suite to run")
	flag.StringVar(&params.StorageClassName, "storage-class", "", "Storage class to use")
	flag.BoolVar(&params.CollectLogsOnFailure, "collect-logs", false, "Whether to collect logs on failure")
	flag.Var(&clusters, "cluster", "Kubernetes cluster configuration e.g. FILE,CONTEXT,NAMESPACE")
	flag.Var(&registries, "registry", "Container image registry configuration e.g. SERVER,USERNAME,PASSWORD")
	flag.Var(&tests, "test", "Individual test to run, overrides -suite if specified")
	flag.BoolVar(&useANSIColor, "color", false, "Prettify output")

	// File based configuration (meat-space friendly)
	testConfigFilePath := flag.String("testconfig", "resources/test_config.yaml", "test_config.yaml path. eg: $HOME/test_config.yaml")

	flag.Parse()

	if useANSIColor {
		logrus.SetFormatter(&logrus.TextFormatter{ForceColors: true})
	}

	params.ClusterConfigs = clusters.values
	params.RegistryConfigs = registries.values

	// We are using the CLI to configure if the suite or tests are explcitly stated.
	withExplicitTests := len(tests.values) > 0
	useCLI := params.SuiteToRun != "" || withExplicitTests

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
					Config:    "~/.kube/config",
					Namespace: "default",
				},
				{
					Config:    "~/.kube/config",
					Namespace: "remote",
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
	if withExplicitTests {
		SuiteName = "custom"
		suiteData = SuiteData{
			// Timeout is pointless, expect users to specify -test.timeout instead.
			Timeout:  "24h",
			TestCase: tests.values,
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

// startTimeoutTimer starts timeout trigger based on given value in suiteData.Timeout.
func startTimeoutTimer() {
	go func() {
		timeoutDuration := GetDuration(suiteData.Timeout)

		logrus.Infof("Setting timeout of %v from %v", timeoutDuration, time.Now())

		<-time.After(timeoutDuration)

		logrus.Infof("Timeout happened at %v", time.Now())

		// Dump out backtraces in case this is a deadlock situation.
		profile := pprof.Lookup("goroutine")
		if profile != nil {
			buffer := &bytes.Buffer{}
			_ = profile.WriteTo(buffer, 2)
			logrus.Info(buffer.String())
		}

		panic("Test timed out..")
	}()
}

func CreateDeploymentObject(k8s *types.Cluster, operatorImage string, operatorPort int, podCreateTimeout fmt.Stringer) *appsv1.Deployment {
	// This just picks the first, so ordering is important unless we sort out
	// cbopcfg...
	var pullSecret string

	if k8s.PullSecrets != nil && k8s.PullSecrets[k8s.Namespace] != nil && len(k8s.PullSecrets[k8s.Namespace]) > 0 {
		pullSecret = k8s.PullSecrets[k8s.Namespace][0]
	}

	deployment := config.GetOperatorDeployment("", operatorImage, pullSecret, podCreateTimeout, "--zap-level", "debug")

	// Manually set the HTTP port.
	if operatorPort != 0 {
		listerAddrArg := "--listen-addr=0.0.0.0:" + strconv.Itoa(operatorPort)
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, listerAddrArg)
	}

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
		SuiteYmlData:                  suiteData,
		CouchbaseServerImage:          runtimeParams.CouchbaseServerImage,
		CouchbaseServerImageUpgrade:   runtimeParams.CouchbaseServerImageUpgrade,
		PodCreateTimeout:              5 * time.Minute,
		SyncGatewayImage:              runtimeParams.SyncGatewayImage,
		CouchbaseExporterImage:        runtimeParams.CouchbaseExporterImage,
		CouchbaseExporterImageUpgrade: runtimeParams.CouchbaseExporterImageUpgrade,
		CouchbaseBackupImage:          runtimeParams.CouchbaseBackupImage,
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

	logrus.Info(PrettyHeading("Docker Registries"))

	for _, registry := range runtimeParams.RegistryConfigs {
		logrus.Info(" →  server: " + registry.Server)
		logrus.Info("    username: " + registry.Username)
		logrus.Info("    password: " + strings.Repeat("*", len(registry.Password)))
	}

	logrus.Info(PrettyHeading("Container Images"))
	logrus.Info(" →  couchbase operator: " + runtimeParams.OperatorImage)
	logrus.Info(" →  couchbase admission controller: " + runtimeParams.AdmissionControllerImage)
	logrus.Info(" →  couchbase server: " + runtimeParams.CouchbaseServerImage)
	logrus.Info(" →  couchbase server upgrade: " + runtimeParams.CouchbaseServerImageUpgrade)
	logrus.Info(" →  couchbase sync gateway: " + Global.SyncGatewayImage)
	logrus.Info(" →  couchbase exporter: " + runtimeParams.CouchbaseExporterImage)
	logrus.Info(" →  couchbase exporter upgrade: " + runtimeParams.CouchbaseExporterImageUpgrade)
	logrus.Info(" →  couchbase backup: " + runtimeParams.CouchbaseBackupImage)

	logrus.Info(PrettyHeading("Clusters"))

	for _, config := range Global.ClusterSpec {
		logrus.Info(" →  path: " + config.KubeConfPath)
		logrus.Info("    context: " + config.Context)
		logrus.Info("    namespace: " + config.Namespace)
	}

	logrus.Info(PrettyHeading("Kubernetes"))
	logrus.Info(" →  storage class: " + runtimeParams.StorageClassName)
	logrus.Info(PrettyHeading("Logs"))
	logrus.Info(" →  directory: " + Global.LogDir)

	// Setup the cbopinfo absolute path so it will not change if we move directories
	wd, oserr := os.Getwd()
	if oserr != nil {
		return oserr
	}

	Global.CbopinfoPath = wd + "/../../build/bin/cbopinfo"

	for _, k8s := range Global.ClusterSpec {
		if err = Global.SetupFramework(k8s); err != nil {
			return err
		}
	}

	return nil
}

func cleanUpNamespace() (err error) {
	for _, k8s := range Global.ClusterSpec {
		logrus.Infof("Cleaning up namespace %s", k8s.Namespace)

		// Remove secrets
		if k8s.DefaultSecret != nil {
			if err := e2eutil.DeleteSecret(k8s, k8s.DefaultSecret.Name, &metav1.DeleteOptions{}); err != nil {
				if !k8sutil.IsKubernetesResourceNotFoundError(err) {
					return fmt.Errorf("unable to delete the default secret: %v", err)
				}
			}
		}

		// Clean-up Deployments and pods
		if err := DeleteOperatorCompletely(k8s, config.OperatorResourceName); err != nil {
			return err
		}

		// Blow away any couchbase cluster resources (and friends)
		e2eutil.CleanK8sCluster(k8s)
	}

	// TODO: check all deleted and wait
	Global = nil

	logrus.Info("Namespace cleaned-up successfully")

	return
}

func Teardown() error {
	if Global == nil {
		return fmt.Errorf("framework is uninitialized")
	}

	if Global.SkipTeardown {
		return nil
	}

	if err := cleanUpNamespace(); err != nil {
		return err
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
		Config:        config,
		CRClient:      client.MustNew(config),
		KubeClient:    kubernetes.NewForConfigOrDie(config),
		DynamicClient: dynamic.NewForConfigOrDie(config),
		RESTMapper:    restMapper,
		KubeConfPath:  c.Config,
		Context:       c.Context,
		Namespace:     c.Namespace,
	}, nil
}

func (f *Framework) CreateSecretInKubeCluster(k8s *types.Cluster) error {
	secret, err := e2eutil.CreateSecret(k8s, e2espec.NewDefaultSecret(k8s.Namespace))
	if err != nil {
		err = fmt.Errorf("failed to create default couchbase secret: %v", err)
		return err
	}

	k8s.DefaultSecret = secret

	return err
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

// tells us if the underlying physical cluster on a host exists.
func (l initializedClusterList) isClusterInitialized(host string) bool {
	for _, cluster := range l {
		if cluster.host == host {
			return true
		}
	}

	return false
}

// check that the namespace for this host is already initialized.
func (l initializedClusterList) isClusterNamespaceInitialized(k8s *types.Cluster) bool {
	for _, cluster := range l {
		if cluster.host == k8s.Config.Host {
			for _, ns := range cluster.namespaces {
				if ns == k8s.Namespace {
					return true
				}
			}
		}
	}

	return false
}

// add initializedCluster to the initializedClusterList.
func (l initializedClusterList) initializeClusterNamespace(k8s *types.Cluster) (initializedClusterList, error) {
	for _, cluster := range l {
		if cluster.host == k8s.Config.Host {
			for _, ns := range cluster.namespaces {
				if ns == k8s.Namespace {
					return nil, fmt.Errorf("requested namespace already exists on the requested host")
				}
			}

			cluster.namespaces = append(cluster.namespaces, k8s.Namespace)

			return l, nil
		}
	}

	return append(l, initializedCluster{
		host:       k8s.Config.Host,
		namespaces: []string{k8s.Namespace},
	}), nil
}

func (f *Framework) SetupFramework(k8s *types.Cluster) error {
	if !f.initializedClusters.isClusterInitialized(k8s.Config.Host) {
		if err := f.RemoveK8SNodeTaints(k8s.KubeClient); err != nil {
			return err
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

		// Creating required namespaces and cluster roles before deploying the operator
		if err := createK8SNamespace(k8s); err != nil {
			return err
		}

		// re-creating docker secrets
		logrus.Info("Recreating docker auth secret in default namespace")

		if err := recreateDockerAuthSecret(k8s, "default"); err != nil {
			return err
		}

		if k8s.Namespace != "default" {
			logrus.Infof("Recreating docker auth secret in %v namespace", k8s.Namespace)

			if err := recreateDockerAuthSecret(k8s, k8s.Namespace); err != nil {
				return err
			}
		}

		// creating DAC
		logrus.Infof("Creating admission controller")

		if err := createAdmissionController(k8s); err != nil {
			return err
		}
	}

	if f.initializedClusters.isClusterNamespaceInitialized(k8s) {
		return nil
	}

	// Creating required namespaces and cluster roles before deploying the operator
	if err := createK8SNamespace(k8s); err != nil {
		return err
	}

	e2eutil.CleanK8sCluster(k8s)

	// Clean up any state locls that may prevent the framework working
	// across operator versions.
	logrus.Info("Cleaning stale locks...")

	if err := k8s.KubeClient.CoreV1().ConfigMaps(k8s.Namespace).Delete("couchbase-operator", metav1.NewDeleteOptions(0)); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}

	logrus.Info("Deleting orphaned pods")

	pods, err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if err := k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Delete(pod.Name, metav1.NewDeleteOptions(0)); err != nil {
			if errors.IsNotFound(err) {
				continue
			}

			return err
		}

		logrus.Infof("Pod deleted: %v", pod.Name)
	}

	endpoints, err := k8s.KubeClient.CoreV1().Endpoints(k8s.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
	if err != nil {
		return err
	}

	for _, endpoint := range endpoints.Items {
		if err := k8s.KubeClient.CoreV1().Endpoints(k8s.Namespace).Delete(endpoint.Name, metav1.NewDeleteOptions(0)); err != nil {
			return err
		}

		logrus.Infof("Endpoint deleted: %v", endpoint.Name)
	}

	logrus.Info("Recreating role")

	if err := recreateRoles(k8s, config.OperatorResourceName); err != nil {
		return err
	}

	logrus.Info("Recreating service account")

	if err := RecreateServiceAccount(k8s, config.OperatorResourceName); err != nil {
		return err
	}

	logrus.Info("Recreating role binding")

	if err := recreateRoleBindings(k8s); err != nil {
		return err
	}

	// deleting secrets
	logrus.Info("Deleting secrets")

	if err := e2eutil.DeleteSecret(k8s, "basic-test-secret", &metav1.DeleteOptions{}); err == nil {
		logrus.Infof("Secret deleted: %v", "basic-test-secret")
	}

	logrus.Info("Creating secret")

	if err = f.CreateSecretInKubeCluster(k8s); err != nil {
		return err
	}

	// delete and create operator
	logrus.Infof("Cleaning up namespace %s before deployment", k8s.Namespace)

	if err := DeleteOperatorCompletely(k8s, config.OperatorResourceName); err != nil {
		return fmt.Errorf("failed to delete operator: %v", err)
	}

	logrus.Info("Setting up operator")

	if err := f.SetupCouchbaseOperator(k8s); err != nil {
		return fmt.Errorf("failed to setup couchbase operator: %v", err)
	}

	logrus.Info("Couchbase operator created successfully")

	f.initializedClusters, err = f.initializedClusters.initializeClusterNamespace(k8s)
	if err != nil {
		return err
	}

	logrus.Info("E2E setup successfully")

	return nil
}

func (f *Framework) SetupCouchbaseOperator(k8s *types.Cluster) error {
	deployment := CreateDeploymentObject(k8s, f.OpImage, 0, f.PodCreateTimeout)

	if _, err := k8s.KubeClient.AppsV1().Deployments(k8s.Namespace).Create(deployment); err != nil {
		return err
	}

	return e2eutil.WaitUntilOperatorReady(k8s, constants.CouchbaseOperatorLabel)
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

// GetCluster allocates a cluster for a tests and records it as in-use
// for later debug analysis and output if needed.
func (f *Framework) GetCluster(index int) *types.Cluster {
	cluster := f.ClusterSpec[index]

	f.TestClusters = append(f.TestClusters, cluster)

	return cluster
}

// Reset preforms any per-test clean up operations.
func (f *Framework) Reset() {
	f.TestClusters = []*types.Cluster{}
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
