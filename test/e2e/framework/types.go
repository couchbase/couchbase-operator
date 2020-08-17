package framework

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// Main framework structure
type Framework struct {
	// CbopinfoPath is the absolute path to the cbopinfo binary
	CbopinfoPath           string
	OpImage                string
	SyncGatewayImage       string
	KubeType               string
	KubeVersion            string
	ClusterSpec            types.ClusterMap
	LogDir                 string
	SkipTeardown           bool
	SuiteYmlData           SuiteData
	ClusterConfFile        string
	CollectLogs            bool
	CouchbaseExporterImage string
	CouchbaseBackupImage   string
	// TestClusters is the current set of clusters to use for a test. This
	// list is derived from the TestCaseGroup and used by individual
	// tests to select the cluster configuration to use.
	TestClusters []string
	// CouchbaseServerImage is the image of Couchbase server we are running with
	CouchbaseServerImage string
	// CouchbaseServerImageUpgrade is the image of Couchbase server we are upgrading to
	CouchbaseServerImageUpgrade string
	StorageClassName            string
	// TestRetries allows you to retry a test N times before giving up.
	TestRetries int `yaml:"testRetries"`
	// PodCreateTimeout is the time we expect to wait when pods are failing to be
	// created.
	PodCreateTimeout time.Duration
	// tracks what clusters have already been initialized by the testing framework
	initializedClusters initializedClusterList
}

type initializedCluster struct {
	host       string
	namespaces []string
}

type initializedClusterList []initializedCluster

// Runtime configuration
type KubeConfData struct {
	ClusterName   string `yaml:"name"`
	ClusterConfig string `yaml:"config"`
	Context       string `yaml:"context"`
	Namespace     string `yaml:"namespace"`
}

// RegistryConfig defines a container image registry.  Registry configurations will
// automatically be added to all Operator/DAC deployments as image pull secrets.  They
// will be added to all Couchbase clusters also.  This allows testing of all assets
// from any private repository.
type RegistryConfig struct {
	// Server is the registry server to use e.g. "https://index.docker.io/v1/".
	Server string `json:"server"`

	// Username is the user/organization to authenticate as.
	Username string `json:"username"`

	// Passowrd is the authentication password for the organization.
	Password string `json:"password"`
}

// Struct to read and store test_config yaml passed by the user during testing
type TestRunParam struct {
	KubeType                    string `yaml:"kube-type"`
	OperatorImage               string `yaml:"operator-image"`
	AdmissionControllerImage    string `yaml:"admission-controller-image"`
	SyncGatewayImage            string `yaml:"sync-gateway-image"`
	CouchbaseServerImage        string `yaml:"couchbase-server-image"`
	CouchbaseServerImageUpgrade string `yaml:"couchbase-server-image-upgrade"`
	CouchbaseExporterImage      string `yaml:"couchbase-exporter-image"`
	CouchbaseBackupImage        string `yaml:"couchbase-backup-image"`
	SuiteToRun                  string `yaml:"suite"`

	ServiceAccountName string `yaml:"serviceAccountName"`
	StorageClassName   string `yaml:"StorageClassName"`
	ClusterConfFile    string `yaml:"cluster-config"`

	KubeVersion string         `yaml:"kube-version"`
	KubeConfig  []KubeConfData `yaml:"kube-config"`

	ForceKubeCreation    bool `yaml:"forceKubeCreation"`
	SkipTearDown         bool `yaml:"skip-tear-down"`
	CollectLogsOnFailure bool `yaml:"collectLogsOnFailure"`

	// RegistryConfigs define private container registries that need to be defined
	// as docker pull secrets in order to access private container images.
	RegistryConfigs []RegistryConfig `yaml:"registries"`

	// TestRetries allows you to retry a test N times before giving up.
	TestRetries *int `yaml:"testRetries"`

	// Platform allows you to explicitly specify the platform type.  DO NOT USE THIS
	// 99% of the time, your tests should not rely on platform behaviour and be generic,
	// unless absolutely necessary.
	Platform couchbasev2.PlatformType `yanl:"platform"`
}

/************************************************
 Following types are used for testing framework
************************************************/

// TestFunc defines the test function type
type TestFunc func(*testing.T)

// DecoratorArgs will be used to pass arguments to decorators
type DecoratorArgs struct {
	KubeNames []string
}

// Map to store Testcase name to their respective Function objects
type FuncMap map[string]TestFunc

// TestResult simply maps a test name to a pass/fail flag
type TestResult struct {
	Name     string
	Result   bool
	Unstable bool
}

// To decode test-suite yaml file
type SuiteData struct {
	SuiteName     string `yaml:"suite"`
	Timeout       string `yaml:"timeout"`
	TestCaseGroup []struct {
		GroupName   string   `yaml:"name"`
		ClusterName []string `yaml:"clusters"`
		TestCase    []struct {
			TcName string `yaml:"name"`
		} `yaml:"testcases"`
	} `yaml:"tcGroups"`
}
