package e2e

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestFunc defines the test function type
type TestFunc func(*testing.T)

// TestSuite defines a suite of tests
type TestSuite []TestFunc

// TestDecorator decorates a test function.  This is used to augment an
// existing test usually to perform setup and tear-down tasks e.g.
// initializing and deleting a cluster or applying TLS configuration
type TestDecorator func(TestFunc) TestFunc

// TestResult simply maps a test name to a pass/fail flag
type TestResult struct {
	name   string
	result bool
}

// Variable to store the results globally
var results = []TestResult{}

// String variable to store the random suffix used for couchbase-server
// name and tls certificates
var RandomNameSuffix string

// Test suite definitions
//
// TODO: We should start thinking about a sane hierarchy here e.g. all devs want to
// run a set of tests which will work on a single node minikube cluster.  For
// example in TestSanity, TestAntiAffinityOn fails all the time. Something
// like the following would be cool, then filter what we want to run based on path.
//
//     TestAll
//       TestSingleNode
//         TestBasic
//         TestPod
//         TestNode
//         TestCluster
//       TestMultiNode
//         TestAntiAffinity
//       TestAWS
//         TestServerGroups
//       TestGoogle
//         TestServerGroups
//
var testSuiteSanity = TestSuite{
	TestCreateCluster,
	TestCreateBucketCluster,
	TestBucketAddRemoveBasic,
	TestEditBucket,
	TestResizeCluster,
	TestEditClusterSettings,
	TestRecoveryAfterOnePodFailureNoBucket,
	TestAntiAffinityOn,
	TestPodResourcesBasic,
}

var testSuiteP0 = TestSuite{
	TestCreateCluster,
	TestCreateBucketCluster,
	TestBucketAddRemoveBasic,
	TestNegBucketAdd,
	TestEditBucket,
	TestNegBucketEdit,
	TestResizeCluster,
	TestResizeClusterWithBucket,
	TestEditServiceConfig,
	TestNegEditServiceConfig,
	TestRecoveryAfterOnePodFailureNoBucket,
	TestRecoveryAfterTwoPodFailureNoBucket,
	TestRecoveryAfterOnePodFailureBucketOneReplica,
	TestRecoveryAfterTwoPodFailureBucketOneReplica,
	TestRecoveryAfterOnePodFailureBucketTwoReplica,
	TestRecoveryAfterTwoPodFailureBucketTwoReplica,
	TestPodResourcesBasic,
	TestPodResourcesCannotBePlaced,
	TestFirstNodePodResourcesCannotBePlaced,
	TestAntiAffinityOn,
	TestAntiAffinityOnCannotBePlaced,
	TestAntiAffinityOff,
	TestEditClusterSettings,
	TestNegEditClusterSettings,
	TestBasicMDSScaling,
	TestSwapNodesBetweenServices,
	TestCreateClusterWithoutDataService,
	TestCreateClusterDataServiceNotFirst,
	TestRemoveLastDataService,
	TestKillOperator,
	TestKillOperatorAndUpdateClusterConfig,
}

var testSuiteP1 = TestSuite{
	TestBucketAddRemoveExtended,
	TestRevertExternalBucketUpdates,
	TestInvalidAuthSecret,
	TestInvalidBaseImage,
	TestInvalidVersion,
	TestNodeUnschedulable,
	//TestNodeServiceDownRecovery,
	TestNodeServiceDownDuringRebalance,
	TestReplaceManuallyRemovedNode,
	TestManageMultipleClusters,
	TestNodeManualFailover,
	TestNodeRecoveryAfterMemberAdd,
	TestNodeRecoveryKilledNewMember,
	//TestKillNodesAfterRebalanceAndFailover,
	TestRemoveForeignNode,
	TestRecoveryAfterOneNsServerFailureBucketOneReplica,
	TestRecoveryAfterOneNodeUnreachableBucketOneReplica,
	TestRecoveryNodeTmpUnreachableBucketOneReplica,
	TestPauseOperator,
	TestNegPodResourcesBasic,
	TestPodResourcesHigh,
	TestPodResourcesLow,
	TestAntiAffinityOnCannotBeScaled,
}

var testCRDValidation = TestSuite{
	TestValidationCreate,
	TestNegValidationCreate,
	TestValidationDefaultCreate,
	TestNegValidationDefaultCreate,
	TestNegValidationConstraintsCreate,
	TestValidationApply,
	TestNegValidationApply,
	TestValidationDefaultApply,
	TestNegValidationDefaultApply,
	TestNegValidationConstraintsApply,
	TestNegValidationImmutableApply,
	TestValidationDelete,
	TestNegValidationDelete,
}

var testSuiteAll = TestSuite{
	// basic tests
	TestCreateCluster,
	TestCreateBucketCluster,

	// bucket tests
	TestBucketAddRemoveBasic,
	TestBucketAddRemoveExtended,
	TestNegBucketAdd,
	TestEditBucket,
	TestNegBucketEdit,
	TestRevertExternalBucketUpdates,

	// cluster tests
	TestResizeCluster,
	TestResizeClusterWithBucket,
	TestEditClusterSettings,
	TestNegEditClusterSettings,
	TestInvalidAuthSecret,
	TestInvalidBaseImage,
	TestInvalidVersion,
	TestNodeUnschedulable,
	TestNodeServiceDownDuringRebalance,
	TestReplaceManuallyRemovedNode,
	TestBasicMDSScaling,
	TestSwapNodesBetweenServices,
	TestCreateClusterWithoutDataService,
	TestCreateClusterDataServiceNotFirst,
	TestRemoveLastDataService,
	TestManageMultipleClusters,

	// node tests
	TestEditServiceConfig,
	TestNegEditServiceConfig,
	TestNodeManualFailover,
	TestNodeRecoveryAfterMemberAdd,
	TestNodeRecoveryKilledNewMember,
	TestKillNodesAfterRebalanceAndFailover,
	TestRemoveForeignNode,
	TestRecoveryAfterOnePodFailureNoBucket,
	TestRecoveryAfterTwoPodFailureNoBucket,
	TestRecoveryAfterOnePodFailureBucketOneReplica,
	TestRecoveryAfterTwoPodFailureBucketOneReplica,
	TestRecoveryAfterOnePodFailureBucketTwoReplica,
	TestRecoveryAfterTwoPodFailureBucketTwoReplica,
	TestRecoveryAfterOneNsServerFailureBucketOneReplica,
	TestRecoveryAfterOneNodeUnreachableBucketOneReplica,
	TestRecoveryNodeTmpUnreachableBucketOneReplica,

	// operator tests
	TestPauseOperator,
	TestKillOperator,
	TestKillOperatorAndUpdateClusterConfig,

	// pod tests
	TestPodResourcesBasic,
	TestNegPodResourcesBasic,
	TestPodResourcesHigh,
	TestPodResourcesLow,
	TestPodResourcesCannotBePlaced,
	TestFirstNodePodResourcesCannotBePlaced,
	TestAntiAffinityOn,
	TestAntiAffinityOnCannotBePlaced,
	TestAntiAffinityOnCannotBeScaled,
	TestAntiAffinityOff,
}

var testSuiteTLSP0 = TestSuite{
	// Basic tests
	TestTlsCreateCluster,
	TestTlsKillClusterNode,
	TestTlsResizeCluster,
}

var testSuiteTLSP1 = TestSuite{
	// Certificate remove tests
	TestTlsRemoveOperatorCertificateAndAddBack,
	TestTlsRemoveClusterCertificateAndAddBack,
	TestTlsRemoveOperatorCertificateAndResizeCluster,
	TestTlsRemoveClusterCertificateAndResizeCluster,
}

var testSuiteTLSCustCert = TestSuite{
	// Customized certificate cases
	TestTlsNegRSACertificateDnsName,
	TestTlsCertificateExpiry,
	TestTlsNegCertificateExpiredBeforeDeployment,
	TestTlsCertificateDeployedBeforeValidity,
	TestTlsGenerateWrongCACertType,
}

// TestFuncName return the name of a test function
func getTestFuncName(f TestFunc) string {
	// Use reflection and symbol lookups to determine the function name.
	// This returns library_path.name, so extract the bit we are interested in
	nameRaw := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	nameParts := strings.Split(nameRaw, ".")
	return nameParts[len(nameParts)-1]
}

// analyzeResults accepts a list of test results and displays success rates
func analyzeResults(t *testing.T) {
	t.Logf("Suite Test Results: \n")

	failures := []string{}
	for i, result := range results {
		if result.result {
			t.Logf("%d: %s...PASS", i+1, result.name)
		}
		if !result.result {
			t.Logf("%d: %s...FAIL", i+1, result.name)
			failures = append(failures, result.name)

		}
	}

	pass := float64(len(results) - len(failures))
	fail := float64(len(failures))
	total := float64(len(results))
	passRate := float64((pass / total) * 100.0)

	if fail > 0 {
		t.Logf("Failures: ")
		for i, test := range failures {
			t.Logf("%d: %s", i+1, test)
		}
	}

	t.Logf("\n Pass: %f \n Fail: %f \n Pass Rate: %f", pass, fail, passRate)
	if fail > 0 {
		t.Fatalf("suite contains failures")
	}
}

// runSuite takes a TestSuite and runs each test one at a time.  An optional list
// of decorators may be applied to each test in the suite.  Decorators are
// applied in order, so the first to be specified is the last to be run before
// the test is run, and the first to be run after the test completes
//
// TODO: we may in future want to apply a decorator at a specific level in
// the test hierarchy e.g. for a specific cloud provider we may need to
// perform an admin option only once during a set of tests, others e.g.
// static TLS, need to be performed on a per-test basis.  The distinction
// is decorate immediately for a superset, or defer until we have an actual
// set of tests
func runSuite(t *testing.T, suite TestSuite, decorators ...TestDecorator) {
	for _, test := range suite {
		// Grab the test name before we possibly alter the test
		// function pointer with decorators
		name := getTestFuncName(test)

		// Apply per-test decorations
		for _, decorator := range decorators {
			test = decorator(test)
		}

		// Run the test
		res := t.Run(name, test)
		results = append(results, TestResult{name, res})
	}
}

// Top-level test sets
func TestSanity(t *testing.T) {
	runSuite(t, testSuiteSanity)
	analyzeResults(t)
}

func TestP0(t *testing.T) {
	runSuite(t, testSuiteP0)
	analyzeResults(t)
}

func TestP1(t *testing.T) {
	runSuite(t, testSuiteP1)
	analyzeResults(t)
}

func TestCRDValidation(t *testing.T) {
	runSuite(t, testCRDValidation)
	analyzeResults(t)
}

func TestAll(t *testing.T) {
	runSuite(t, testSuiteAll)
	analyzeResults(t)
}

func TestRSA(t *testing.T) {
	runSuite(t, testSuiteTLSP0, rsaDecorator)
	runSuite(t, testSuiteTLSP1, rsaDecorator)
	runSuite(t, testSuiteTLSCustCert)
	analyzeResults(t)
}

func TestEllipticP224(t *testing.T) {
	runSuite(t, testSuiteTLSP0, ellipticP224Decorator)
	analyzeResults(t)
}

func TestEllipticP256(t *testing.T) {
	runSuite(t, testSuiteTLSP0, ellipticP256Decorator)
	analyzeResults(t)
}

func TestEllipticP384(t *testing.T) {
	runSuite(t, testSuiteTLSP0, ellipticP384Decorator)
	analyzeResults(t)
}

func TestEllipticP521(t *testing.T) {
	runSuite(t, testSuiteTLSP0, ellipticP521Decorator)
	analyzeResults(t)
}

func TestTLS(t *testing.T) {
	suite := TestSuite{
		TestRSA,
		// These don't work: see MB-28680
		TestEllipticP224,
		TestEllipticP256,
		TestEllipticP384,
		TestEllipticP521,
	}
	runSuite(t, suite)
}
