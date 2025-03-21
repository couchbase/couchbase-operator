package framework

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/certification"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// Main framework structure.
type Framework struct {
	certification.SharedTestFlags

	// CbopinfoPath is the absolute path to the cbopinfo binary
	CbopinfoPath                     string
	OpImage                          string
	AdmissionControllerImage         string
	SyncGatewayImage                 string
	KubeType                         string
	KubeVersion                      string
	ClusterSpec                      []*types.Cluster
	LogDir                           string
	SkipTeardown                     bool
	ClusterConfFile                  string
	CollectLogs                      bool
	CollectServerLogsOnFailure       bool
	CouchbaseExporterImage           string
	CouchbaseExporterImageUpgrade    string
	CouchbaseBackupImage             string
	CouchbaseLoggingImage            string
	CouchbaseLoggingImageUpgrade     string
	CouchbaseCloudNativeGatewayImage string
	BucketType                       e2eutil.BucketType
	CompressionMode                  couchbasev2.CouchbaseBucketCompressionMode
	EnableIstio                      bool
	IstioTLSMode                     string
	// AWS Access
	AWSAccountID    string
	AWSOIDCProvider string
	IAMAccessKey    string
	IAMSecretID     string
	S3Region        string
	S3AccessKey     string
	S3SecretID      string
	// Azure Access
	AZAccountName string
	AZAccountKey  string
	// GCP Access
	GCPClientID     string
	GCPClientSecret string
	GCPRefreshToken string

	MinioRegion        string
	MinioAccessKey     string
	MinioSecretID      string
	DocsCount          int
	LogLevel           string
	PodImagePullPolicy PullPolicyFlag
	CollectedLogLevel  int
	TLSVersion         couchbasev2.TLSVersion

	// TestClusters is the current set of clusters to use for a test. This
	// list is derived from the TestCaseGroup and used by individual
	// tests to select the cluster configuration to use.
	TestClusters []*types.Cluster

	// CouchbaseServerImage is the image of Couchbase server we are running with
	CouchbaseServerImage string

	CouchbaseServerImageVersion string

	// CouchbaseServerImageUpgrade is the image of Couchbase server we are upgrading to
	CouchbaseServerImageUpgrade        string
	CouchbaseServerImageUpgradeVersion string
	ServiceAccountName                 string
	// PodCreateTimeout is the time we expect to wait when pods are failing to be
	// created.
	PodCreateTimeout time.Duration
	// PodDeleteDelay is the time we expect to wait before deleting a pod after determining
	// it must be deleted.
	PodDeleteDelay time.Duration
	// PodReadinessDelay is the time we wait before starting readiness probes
	PodReadinessDelay time.Duration
	// PodReadinessPeriod is the time between readiness probes
	PodReadinessPeriod time.Duration
	Platform           couchbasev2.PlatformType
	// RegistryConfigs define private container registries that need to be defined
	// as docker pull secrets in order to access private container images.
	RegistryConfigs []RegistryConfig

	// DynamicPlatform is your GKE Autopilots or anything with cluster autoscaling
	// enabled.
	DynamicPlatform bool

	// IgnoreErrors tells the application not to fatally die on errors.
	IgnoreErrors bool

	// Parallelism is essentially a copy of -test.parallel that we can acually see.
	Parallelism int

	// BackupStorageClassName is the name of the storage class to use for test volumes.
	BackupStorageClassName string
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

/************************************************
 Following types are used for testing framework
************************************************/

// Taglist is a wrapper for an ordered list of tags.
type TagList []string

// contains checks to see if the given tag is in the tag list.
func (l TagList) contains(tag string) bool {
	for _, t := range l {
		if t == tag {
			return true
		}
	}

	return false
}

// containsAny checks to see if any of the tags are in the tag list, returning
// the first tag that matched.
func (l TagList) containsAny(tags []string) (string, bool) {
	for _, tag := range tags {
		if l.contains(tag) {
			return tag, true
		}
	}

	return "", false
}

// TestFunc defines the test function type.
type TestFunc func(*testing.T)

// TestDef allows us to describe a test.
type TestDef struct {
	// function is a function pointer to the test itself.
	function TestFunc

	// tags are a set of textual groups a test belongs to e.g. 'p0', 'tls' etc.
	tags TagList

	// selectedTag is used to mark the first tag that matches a test definition,
	// this in turn is used to assign the tests to a JUnit test suite.
	selectedTag string
}

// Run kicks off the test.
func (t *TestDef) Run(tt *testing.T) {
	tt.Run(t.Name(), t.function)
}

// Name returns the test function name.
func (t *TestDef) Name() string {
	v := reflect.ValueOf(t.function)
	name := runtime.FuncForPC(v.Pointer()).Name()
	parts := strings.Split(name, ".")

	return parts[len(parts)-1]
}

// NewTestDef is a wrapper to turn struct initialization into a oneliner in an
// extensible way.
func NewTestDef(function TestFunc) *TestDef {
	return &TestDef{
		function: function,
	}
}

// WithTags adds a set of tags to the test.
func (t *TestDef) WithTags(tags ...string) *TestDef {
	t.tags = TagList(tags)

	return t
}

// TestDefList is a container that holds test definitions.
type TestDefList []*TestDef

// Select returns the set of test definitions that match at least one of the provided
// tags (a union of all tests that match any tag).  Precedence is important here however
// as the first matched tag will be added to the selected test definition in order to
// define a JUnit suite for the test.  For backward compatibility, after tags have been
// selected, individual tests can be matched too, and placed in the custom suite.
func (l TestDefList) Select(tags []string, tests []string) TestDefList {
	selected := TestDefList{}

	for _, test := range l {
		tag, ok := test.tags.containsAny(tags)
		if ok {
			selectedTestDef := test
			selectedTestDef.selectedTag = tag
			selected = append(selected, selectedTestDef)

			continue
		}

		if TagList(tests).contains(test.Name()) {
			selectedTestDef := test
			selectedTestDef.selectedTag = "custom"
			selected = append(selected, selectedTestDef)

			continue
		}
	}

	return selected
}
