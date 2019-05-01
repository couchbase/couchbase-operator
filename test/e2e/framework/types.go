package framework

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
)

// Main framework structure
type Framework struct {
	// CbopinfoPath is the absolute path to the cbopinfo binary
	CbopinfoPath    string
	OpImage         string
	Deployment      *appsv1.Deployment
	Namespace       string
	KubeType        string
	KubeVersion     string
	ClusterSpec     types.ClusterMap
	LogDir          string
	SkipTeardown    bool
	SuiteYmlData    SuiteData
	ClusterConfFile string
	CollectLogs     bool
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
}

// To decode cluster yaml file
type ClusterInfo struct {
	ClusterName                  string `yaml:"name"`
	StorageClassType             string `yaml:"storageClassType"`
	SupportsMultipleVolumeClaims bool   `yaml:"supportsMultipleVolumeClaims"`
	MasterNodeList               []struct {
		Ip        string `yaml:"ip"`
		NodeLabel string `yaml:"label"`
	} `yaml:"master"`
	WorkerNodeList []struct {
		Ip        string `yaml:"ip"`
		NodeLabel string `yaml:"label"`
	} `yaml:"worker"`
}

type ClusterConfig struct {
	ClusterInfo []struct {
		Type        string        `yaml:"type"`
		ClusterList []ClusterInfo `yaml:"clusters"`
	} `yaml:"types"`
}

// Runtime configuration
type KubeConfData struct {
	ClusterName   string `yaml:"name"`
	ClusterConfig string `yaml:"config"`
	Context       string `yaml:"context"`
}

// Struct to read and store test_config yaml passed by the user during testing
type TestRunParam struct {
	KubeType                    string `yaml:"kube-type"`
	Namespace                   string `yaml:"namespace"`
	OperatorImage               string `yaml:"operator-image"`
	AdmissionControllerImage    string `yaml:"admission-controller-image"`
	CouchbaseServerImage        string `yaml:"couchbase-server-image"`
	CouchbaseServerImageUpgrade string `yaml:"couchbase-server-image-upgrade"`
	SuiteToRun                  string `yaml:"suite"`

	ServiceAccountName string `yaml:"serviceAccountName"`
	StorageClassName   string `yaml:"StorageClassName"`
	ClusterConfFile    string `yaml:"cluster-config"`

	KubeVersion string         `yaml:"kube-version"`
	KubeConfig  []KubeConfData `yaml:"kube-config"`

	ForceKubeCreation    bool `yaml:"forceKubeCreation"`
	SkipTearDown         bool `yaml:"skip-tear-down"`
	CollectLogsOnFailure bool `yaml:"collectLogsOnFailure"`

	// DockerServer, if defined, creates a pull secret and associates
	// it with Operator and Admission Controller deployments.
	DockerServer string `yaml:"docker-server"`
	// DockerUsername is the docker registry username to use, required when
	// DockerServer is specified.
	DockerUsername string `yaml:"docker-username"`
	// DockerPassword is the docker registry password to use, required when
	// DockerServer is specified.
	DockerPassword string `yaml:"docker-password"`

	// TestRetries allows you to retry a test N times before giving up.
	TestRetries *int `yaml:"testRetries"`
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

// TestDecorator decorates a test function.  This is used to augment an
// existing test usually to perform setup and tear-down tasks e.g.
// initializing and deleting a cluster or applying TLS configuration
type TestDecorator func(TestFunc, DecoratorArgs) TestFunc

// TestSuite defines a suite of tests
type TestSuite map[string]TestFunc
type TestSuiteDecorator map[string]TestDecorator

// Map to store Testcase name to their respective Function objects
type FuncMap map[string]func(*testing.T)
type DecoratorMap map[string]TestDecorator

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
		GroupName     string   `yaml:"name"`
		GroupSetup    []string `yaml:"groupSetup"`
		GroupTeardown []string `yaml:"groupTearDown"`
		ClusterName   []string `yaml:"clusters"`
		TestCase      []struct {
			TcName     string   `yaml:"name"`
			Decorators []string `yaml:"decorators"`
		} `yaml:"testcases"`
	} `yaml:"tcGroups"`
}
