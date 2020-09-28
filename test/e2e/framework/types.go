package framework

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// Main framework structure.
type Framework struct {
	// CbopinfoPath is the absolute path to the cbopinfo binary
	CbopinfoPath                  string
	OpImage                       string
	SyncGatewayImage              string
	KubeType                      string
	KubeVersion                   string
	ClusterSpec                   []*types.Cluster
	LogDir                        string
	SkipTeardown                  bool
	SuiteYmlData                  SuiteData
	ClusterConfFile               string
	CollectLogs                   bool
	CollectServerLogsOnFailure    bool
	CouchbaseExporterImage        string
	CouchbaseExporterImageUpgrade string
	CouchbaseBackupImage          string
	BucketType                    string
	CompressionMode               string
	EnableIstio                   bool
	S3Bucket                      string
	S3Region                      string
	S3AccessKey                   string
	S3SecretID                    string

	// TestClusters is the current set of clusters to use for a test. This
	// list is derived from the TestCaseGroup and used by individual
	// tests to select the cluster configuration to use.
	TestClusters []*types.Cluster
	// CouchbaseServerImage is the image of Couchbase server we are running with
	CouchbaseServerImage string
	// CouchbaseServerImageUpgrade is the image of Couchbase server we are upgrading to
	CouchbaseServerImageUpgrade string
	StorageClassName            *string
	// PodCreateTimeout is the time we expect to wait when pods are failing to be
	// created.
	PodCreateTimeout time.Duration
}

// ClusterConfig holds configuration data about a cluster to use for
// testing.
type ClusterConfig struct {
	// Config is the path to a Kubernetes configuration file, typically ~/.kube/conf.
	Config string `yaml:"config"`

	// Context is the context within the Kubernetes configuration file to use.
	// This is the mechanism Kubernetes uses for defining multiple users or clusters
	// in the same configuration file.
	Context string `yaml:"context"`
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

// Struct to read and store test_config yaml passed by the user during testing.
type TestRunParam struct {
	KubeType                      string `yaml:"kube-type"`
	OperatorImage                 string `yaml:"operator-image"`
	AdmissionControllerImage      string `yaml:"admission-controller-image"`
	SyncGatewayImage              string `yaml:"sync-gateway-image"`
	CouchbaseServerImage          string `yaml:"couchbase-server-image"`
	CouchbaseServerImageUpgrade   string `yaml:"couchbase-server-image-upgrade"`
	CouchbaseExporterImage        string `yaml:"couchbase-exporter-image"`
	CouchbaseExporterImageUpgrade string `yaml:"couchbase-exporter-image-upgrade"`
	CouchbaseBackupImage          string `yaml:"couchbase-backup-image"`
	SuiteToRun                    string `yaml:"suite"`
	BucketType                    string `yaml:"bucket-type"`
	CompressionMode               string `yaml:"compression-mode"`
	EnableIstio                   bool   `yaml:"enable-istio"`
	S3Bucket                      string `yaml:"s3-bucket"`
	S3Region                      string `yaml:"s3-region"`
	S3AccessKey                   string `yaml:"s3-access-key"`
	S3SecretID                    string `yaml:"s3-secret-id"`

	ServiceAccountName string `yaml:"serviceAccountName"`
	StorageClassName   string `yaml:"StorageClassName"`

	// Cluster configs are named virtual clusters (i.e. they may be resident on the
	// same physical cluster, but use a different namespace).  Tests are run against
	// these virtual clusters, and are explicitly referenced by test suites.
	// Note: When all clusters are homogeneous, then you can just pick one.  Better
	// still you can run single-cluster tests concurrently and gate execution based
	// on allocating from the available pool.  The net result, you go a lot faster.
	ClusterConfigs []ClusterConfig `yaml:"kube-config"`

	SkipTearDown               bool `yaml:"skip-tear-down"`
	CollectLogsOnFailure       bool `yaml:"collectLogsOnFailure"`
	CollectServerLogsOnFailure bool `json:"collectServerLogsOnFailure"`

	// RegistryConfigs define private container registries that need to be defined
	// as docker pull secrets in order to access private container images.
	RegistryConfigs []RegistryConfig `yaml:"registries"`

	// Platform allows you to explicitly specify the platform type.  DO NOT USE THIS
	// 99% of the time, your tests should not rely on platform behaviour and be generic,
	// unless absolutely necessary.
	Platform couchbasev2.PlatformType `yanl:"platform"`
}

/************************************************
 Following types are used for testing framework
************************************************/

// TestFunc defines the test function type.
type TestFunc func(*testing.T)

// Map to store Testcase name to their respective Function objects.
type FuncMap map[string]TestFunc

// To decode test-suite yaml file.
type SuiteData struct {
	Timeout  string   `yaml:"timeout"`
	TestCase []string `yaml:"testcases"`
}
