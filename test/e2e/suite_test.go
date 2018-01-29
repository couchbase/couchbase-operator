package e2e

import (
	"testing"
)

type testResult struct {
	testName   string
	testResult bool
}

func TestSanity(t *testing.T) {
	testResults := []testResult{}
	testResults = append(testResults, testResult{"TestCreateCluster", t.Run("TestCreateCluster", TestCreateCluster)})
	testResults = append(testResults, testResult{"TestCreateBucketCluster", t.Run("TestCreateBucketCluster", TestCreateBucketCluster)})
	testResults = append(testResults, testResult{"TestBucketAddRemoveBasic", t.Run("TestBucketAddRemoveBasic", TestBucketAddRemoveBasic)})
	testResults = append(testResults, testResult{"TestEditBucket", t.Run("TestEditBucket", TestEditBucket)})
	testResults = append(testResults, testResult{"TestResizeCluster", t.Run("TestResizeCluster", TestResizeCluster)})
	testResults = append(testResults, testResult{"TestEditClusterSettings", t.Run("TestEditClusterSettings", TestEditClusterSettings)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOnePodFailureNoBucket", t.Run("TestRecoveryAfterOnePodFailureNoBucket", TestRecoveryAfterOnePodFailureNoBucket)})
	testResults = append(testResults, testResult{"TestAntiAffinityOn", t.Run("TestAntiAffinityOn", TestAntiAffinityOn)})
	testResults = append(testResults, testResult{"TestPodResourcesBasic", t.Run("TestPodResourcesBasic", TestPodResourcesBasic)})

	if AnalyzeResults(t, testResults) {
		t.Fatalf("suite contains failures")
	}
}

// 30 tests
func TestP0(t *testing.T) {
	testResults := []testResult{}
	testResults = append(testResults, testResult{"TestCreateCluster", t.Run("TestCreateCluster", TestCreateCluster)})
	testResults = append(testResults, testResult{"TestCreateBucketCluster", t.Run("TestCreateBucketCluster", TestCreateBucketCluster)})
	testResults = append(testResults, testResult{"TestBucketAddRemoveBasic", t.Run("TestBucketAddRemoveBasic", TestBucketAddRemoveBasic)})
	testResults = append(testResults, testResult{"TestNegBucketAdd", t.Run("TestNegBucketAdd", TestNegBucketAdd)})
	testResults = append(testResults, testResult{"TestEditBucket", t.Run("TestEditBucket", TestEditBucket)})
	testResults = append(testResults, testResult{"TestNegBucketEdit", t.Run("TestNegBucketEdit", TestNegBucketEdit)})
	testResults = append(testResults, testResult{"TestResizeCluster", t.Run("TestResizeCluster", TestResizeCluster)})
	testResults = append(testResults, testResult{"TestResizeClusterWithBucket", t.Run("TestResizeClusterWithBucket", TestResizeClusterWithBucket)})
	testResults = append(testResults, testResult{"TestEditServiceConfig", t.Run("TestEditServiceConfig", TestEditServiceConfig)})
	//testResults = append(testResults, testResult{"TestNegEditServiceConfig", t.Run("TestNegEditServiceConfig", TestNegEditServiceConfig)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOnePodFailureNoBucket", t.Run("TestRecoveryAfterOnePodFailureNoBucket", TestRecoveryAfterOnePodFailureNoBucket)})
	testResults = append(testResults, testResult{"TestRecoveryAfterTwoPodFailureNoBucket", t.Run("TestRecoveryAfterTwoPodFailureNoBucket", TestRecoveryAfterTwoPodFailureNoBucket)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOnePodFailureBucketOneReplica", t.Run("TestRecoveryAfterOnePodFailureBucketOneReplica", TestRecoveryAfterOnePodFailureBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterTwoPodFailureBucketOneReplica", t.Run("TestRecoveryAfterTwoPodFailureBucketOneReplica", TestRecoveryAfterTwoPodFailureBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOnePodFailureBucketTwoReplica", t.Run("TestRecoveryAfterOnePodFailureBucketTwoReplica", TestRecoveryAfterOnePodFailureBucketTwoReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterTwoPodFailureBucketTwoReplica", t.Run("TestRecoveryAfterTwoPodFailureBucketTwoReplica", TestRecoveryAfterTwoPodFailureBucketTwoReplica)})
	testResults = append(testResults, testResult{"TestPodResourcesBasic", t.Run("TestPodResourcesBasic", TestPodResourcesBasic)})
	testResults = append(testResults, testResult{"TestPodResourcesCannotBePlaced", t.Run("TestPodResourcesCannotBePlaced", TestPodResourcesCannotBePlaced)})
	testResults = append(testResults, testResult{"TestFirstNodePodResourcesCannotBePlaced", t.Run("TestFirstNodePodResourcesCannotBePlaced", TestFirstNodePodResourcesCannotBePlaced)})
	testResults = append(testResults, testResult{"TestAntiAffinityOn", t.Run("TestAntiAffinityOn", TestAntiAffinityOn)})
	testResults = append(testResults, testResult{"TestAntiAffinityOnCannotBePlaced", t.Run("TestAntiAffinityOnCannotBePlaced", TestAntiAffinityOnCannotBePlaced)})
	testResults = append(testResults, testResult{"TestAntiAffinityOff", t.Run("TestAntiAffinityOff", TestAntiAffinityOff)})
	testResults = append(testResults, testResult{"TestEditClusterSettings", t.Run("TestEditClusterSettings", TestEditClusterSettings)})
	testResults = append(testResults, testResult{"TestNegEditClusterSettings", t.Run("TestNegEditClusterSettings", TestNegEditClusterSettings)})
	testResults = append(testResults, testResult{"TestBasicMDSScaling", t.Run("TestBasicMDSScaling", TestBasicMDSScaling)})
	testResults = append(testResults, testResult{"TestSwapNodesBetweenServices", t.Run("TestSwapNodesBetweenServices", TestSwapNodesBetweenServices)})
	testResults = append(testResults, testResult{"TestCreateClusterWithoutDataService", t.Run("TestCreateClusterWithoutDataService", TestCreateClusterWithoutDataService)})
	testResults = append(testResults, testResult{"TestCreateClusterDataServiceNotFirst", t.Run("TestCreateClusterDataServiceNotFirst", TestCreateClusterDataServiceNotFirst)})
	testResults = append(testResults, testResult{"TestRemoveLastDataService", t.Run("TestRemoveLastDataService", TestRemoveLastDataService)})
	testResults = append(testResults, testResult{"TestKillOperator", t.Run("TestKillOperator", TestKillOperator)})
	testResults = append(testResults, testResult{"TestKillOperatorAndUpdateClusterConfig", t.Run("TestKillOperatorAndUpdateClusterConfig", TestKillOperatorAndUpdateClusterConfig)})

	if AnalyzeResults(t, testResults) {
		t.Fatalf("suite contains failures")
	}
}

// 21 tests
func TestP1(t *testing.T) {
	testResults := []testResult{}
	testResults = append(testResults, testResult{"TestBucketAddRemoveExtended", t.Run("TestBucketAddRemoveExtended", TestBucketAddRemoveExtended)})
	testResults = append(testResults, testResult{"TestRevertExternalBucketUpdates", t.Run("TestRevertExternalBucketUpdates", TestRevertExternalBucketUpdates)})
	testResults = append(testResults, testResult{"TestInvalidAuthSecret", t.Run("TestInvalidAuthSecret", TestInvalidAuthSecret)})
	testResults = append(testResults, testResult{"TestInvalidBaseImage", t.Run("TestInvalidBaseImage", TestInvalidBaseImage)})
	testResults = append(testResults, testResult{"TestInvalidVersion", t.Run("TestInvalidVersion", TestInvalidVersion)})
	//testResults = append(testResults, testResult{"TestNodeUnschedulable", t.Run("TestNodeUnschedulable", TestNodeUnschedulable)})
	testResults = append(testResults, testResult{"TestNodeServiceDownRecovery", t.Run("TestNodeServiceDownRecovery", TestNodeServiceDownRecovery)})
	// (TODO: fix K8S-113), testResults = append(testResults, testResult{"TestNodeServiceDownDuringRebalance", t.Run("TestNodeServiceDownDuringRebalance", TestNodeServiceDownDuringRebalance)})
	testResults = append(testResults, testResult{"TestReplaceManuallyRemovedNode", t.Run("TestReplaceManuallyRemovedNode", TestReplaceManuallyRemovedNode)})
	testResults = append(testResults, testResult{"TestManageMultipleClusters", t.Run("TestManageMultipleClusters", TestManageMultipleClusters)})
	//testResults = append(testResults, testResult{"TestNodeManualFailover", t.Run("TestNodeManualFailover", TestNodeManualFailover)})
	testResults = append(testResults, testResult{"TestNodeRecoveryAfterMemberAdd", t.Run("TestNodeRecoveryAfterMemberAdd", TestNodeRecoveryAfterMemberAdd)})
	testResults = append(testResults, testResult{"TestNodeRecoveryKilledNewMember", t.Run("TestNodeRecoveryKilledNewMember", TestNodeRecoveryKilledNewMember)})
	//testResults = append(testResults, testResult{"TestKillNodesAfterRebalanceAndFailover", t.Run("TestKillNodesAfterRebalanceAndFailover", TestKillNodesAfterRebalanceAndFailover)})
	//testResults = append(testResults, testResult{"TestRemoveForeignNode", t.Run("TestRemoveForeignNode", TestRemoveForeignNode)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOneNsServerFailureBucketOneReplica", t.Run("TestRecoveryAfterOneNsServerFailureBucketOneReplica", TestRecoveryAfterOneNsServerFailureBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOneNodeUnreachableBucketOneReplica", t.Run("TestRecoveryAfterOneNodeUnreachableBucketOneReplica", TestRecoveryAfterOneNodeUnreachableBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryNodeTmpUnreachableBucketOneReplica", t.Run("TestRecoveryNodeTmpUnreachableBucketOneReplica", TestRecoveryNodeTmpUnreachableBucketOneReplica)})
	testResults = append(testResults, testResult{"TestPauseOperator", t.Run("TestPauseOperator", TestPauseOperator)})
	testResults = append(testResults, testResult{"TestNegPodResourcesBasic", t.Run("TestNegPodResourcesBasic", TestNegPodResourcesBasic)})
	testResults = append(testResults, testResult{"TestPodResourcesHigh", t.Run("TestPodResourcesHigh", TestPodResourcesHigh)})
	testResults = append(testResults, testResult{"TestPodResourcesLow", t.Run("TestPodResourcesLow", TestPodResourcesLow)})
	testResults = append(testResults, testResult{"TestAntiAffinityOnCannotBeScaled", t.Run("TestAntiAffinityOnCannotBeScaled", TestAntiAffinityOnCannotBeScaled)})

	if AnalyzeResults(t, testResults) {
		t.Fatalf("suite contains failures")
	}
}

func TestAll(t *testing.T) {
	testResults := []testResult{}
	// basic tests
	testResults = append(testResults, testResult{"TestCreateCluster", t.Run("TestCreateCluster", TestCreateCluster)})
	testResults = append(testResults, testResult{"TestCreateBucketCluster", t.Run("TestCreateBucketCluster", TestCreateBucketCluster)})
	// bucket tests
	testResults = append(testResults, testResult{"TestBucketAddRemoveBasic", t.Run("TestBucketAddRemoveBasic", TestBucketAddRemoveBasic)})
	testResults = append(testResults, testResult{"TestBucketAddRemoveExtended", t.Run("TestBucketAddRemoveExtended", TestBucketAddRemoveExtended)})
	testResults = append(testResults, testResult{"TestNegBucketAdd", t.Run("TestNegBucketAdd", TestNegBucketAdd)})
	testResults = append(testResults, testResult{"TestEditBucket", t.Run("TestEditBucket", TestEditBucket)})
	testResults = append(testResults, testResult{"TestNegBucketEdit", t.Run("TestNegBucketEdit", TestNegBucketEdit)})
	testResults = append(testResults, testResult{"TestRevertExternalBucketUpdates", t.Run("TestRevertExternalBucketUpdates", TestRevertExternalBucketUpdates)})
	// cluster tests
	testResults = append(testResults, testResult{"TestResizeCluster", t.Run("TestResizeCluster", TestResizeCluster)})
	testResults = append(testResults, testResult{"TestResizeClusterWithBucket", t.Run("TestResizeClusterWithBucket", TestResizeClusterWithBucket)})
	testResults = append(testResults, testResult{"TestEditClusterSettings", t.Run("TestEditClusterSettings", TestEditClusterSettings)})
	testResults = append(testResults, testResult{"TestNegEditClusterSettings", t.Run("TestNegEditClusterSettings", TestNegEditClusterSettings)})
	testResults = append(testResults, testResult{"TestInvalidAuthSecret", t.Run("TestInvalidAuthSecret", TestInvalidAuthSecret)})
	testResults = append(testResults, testResult{"TestInvalidBaseImage", t.Run("TestInvalidBaseImage", TestInvalidBaseImage)})
	testResults = append(testResults, testResult{"TestInvalidVersion", t.Run("TestInvalidVersion", TestInvalidVersion)})
	//testResults = append(testResults, testResult{"TestNodeUnschedulable", t.Run("TestNodeUnschedulable", TestNodeUnschedulable)})
	//testResults = append(testResults, testResult{"TestNodeServiceDownDuringRebalance", t.Run("TestNodeServiceDownDuringRebalance", TestNodeServiceDownDuringRebalance)})
	testResults = append(testResults, testResult{"TestReplaceManuallyRemovedNode", t.Run("TestReplaceManuallyRemovedNode", TestReplaceManuallyRemovedNode)})
	testResults = append(testResults, testResult{"TestBasicMDSScaling", t.Run("TestBasicMDSScaling", TestBasicMDSScaling)})
	testResults = append(testResults, testResult{"TestSwapNodesBetweenServices", t.Run("TestSwapNodesBetweenServices", TestSwapNodesBetweenServices)})
	testResults = append(testResults, testResult{"TestCreateClusterWithoutDataService", t.Run("TestCreateClusterWithoutDataService", TestCreateClusterWithoutDataService)})
	testResults = append(testResults, testResult{"TestCreateClusterDataServiceNotFirst", t.Run("TestCreateClusterDataServiceNotFirst", TestCreateClusterDataServiceNotFirst)})
	testResults = append(testResults, testResult{"TestRemoveLastDataService", t.Run("TestRemoveLastDataService", TestRemoveLastDataService)})
	testResults = append(testResults, testResult{"TestManageMultipleClusters", t.Run("TestManageMultipleClusters", TestManageMultipleClusters)})
	// node tests
	testResults = append(testResults, testResult{"TestEditServiceConfig", t.Run("TestEditServiceConfig", TestEditServiceConfig)})
	testResults = append(testResults, testResult{"TestNegEditServiceConfig", t.Run("TestNegEditServiceConfig", TestNegEditServiceConfig)})
	testResults = append(testResults, testResult{"TestNodeManualFailover", t.Run("TestNodeManualFailover", TestNodeManualFailover)})
	testResults = append(testResults, testResult{"TestNodeRecoveryAfterMemberAdd", t.Run("TestNodeRecoveryAfterMemberAdd", TestNodeRecoveryAfterMemberAdd)})
	testResults = append(testResults, testResult{"TestNodeRecoveryKilledNewMember", t.Run("TestNodeRecoveryKilledNewMember", TestNodeRecoveryKilledNewMember)})
	testResults = append(testResults, testResult{"TestKillNodesAfterRebalanceAndFailover", t.Run("TestKillNodesAfterRebalanceAndFailover", TestKillNodesAfterRebalanceAndFailover)})
	testResults = append(testResults, testResult{"TestRemoveForeignNode", t.Run("TestRemoveForeignNode", TestRemoveForeignNode)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOnePodFailureNoBucket", t.Run("TestRecoveryAfterOnePodFailureNoBucket", TestRecoveryAfterOnePodFailureNoBucket)})
	testResults = append(testResults, testResult{"TestRecoveryAfterTwoPodFailureNoBucket", t.Run("TestRecoveryAfterTwoPodFailureNoBucket", TestRecoveryAfterTwoPodFailureNoBucket)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOnePodFailureBucketOneReplica", t.Run("TestRecoveryAfterOnePodFailureBucketOneReplica", TestRecoveryAfterOnePodFailureBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterTwoPodFailureBucketOneReplica", t.Run("TestRecoveryAfterTwoPodFailureBucketOneReplica", TestRecoveryAfterTwoPodFailureBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOnePodFailureBucketTwoReplica", t.Run("TestRecoveryAfterOnePodFailureBucketTwoReplica", TestRecoveryAfterOnePodFailureBucketTwoReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterTwoPodFailureBucketTwoReplica", t.Run("TestRecoveryAfterTwoPodFailureBucketTwoReplica", TestRecoveryAfterTwoPodFailureBucketTwoReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOneNsServerFailureBucketOneReplica", t.Run("TestRecoveryAfterOneNsServerFailureBucketOneReplica", TestRecoveryAfterOneNsServerFailureBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryAfterOneNodeUnreachableBucketOneReplica", t.Run("TestRecoveryAfterOneNodeUnreachableBucketOneReplica", TestRecoveryAfterOneNodeUnreachableBucketOneReplica)})
	testResults = append(testResults, testResult{"TestRecoveryNodeTmpUnreachableBucketOneReplica", t.Run("TestRecoveryNodeTmpUnreachableBucketOneReplica", TestRecoveryNodeTmpUnreachableBucketOneReplica)})
	// operator tests
	testResults = append(testResults, testResult{"TestPauseOperator", t.Run("TestPauseOperator", TestPauseOperator)})
	testResults = append(testResults, testResult{"TestKillOperator", t.Run("TestKillOperator", TestKillOperator)})
	testResults = append(testResults, testResult{"TestKillOperatorAndUpdateClusterConfig", t.Run("TestKillOperatorAndUpdateClusterConfig", TestKillOperatorAndUpdateClusterConfig)})
	// pod tests
	testResults = append(testResults, testResult{"TestPodResourcesBasic", t.Run("TestPodResourcesBasic", TestPodResourcesBasic)})
	testResults = append(testResults, testResult{"TestNegPodResourcesBasic", t.Run("TestNegPodResourcesBasic", TestNegPodResourcesBasic)})
	testResults = append(testResults, testResult{"TestPodResourcesHigh", t.Run("TestPodResourcesHigh", TestPodResourcesHigh)})
	testResults = append(testResults, testResult{"TestPodResourcesLow", t.Run("TestPodResourcesLow", TestPodResourcesLow)})
	testResults = append(testResults, testResult{"TestPodResourcesCannotBePlaced", t.Run("TestPodResourcesCannotBePlaced", TestPodResourcesCannotBePlaced)})
	testResults = append(testResults, testResult{"TestFirstNodePodResourcesCannotBePlaced", t.Run("TestFirstNodePodResourcesCannotBePlaced", TestFirstNodePodResourcesCannotBePlaced)})
	testResults = append(testResults, testResult{"TestAntiAffinityOn", t.Run("TestAntiAffinityOn", TestAntiAffinityOn)})
	testResults = append(testResults, testResult{"TestAntiAffinityOnCannotBePlaced", t.Run("TestAntiAffinityOnCannotBePlaced", TestAntiAffinityOnCannotBePlaced)})
	testResults = append(testResults, testResult{"TestAntiAffinityOnCannotBeScaled", t.Run("TestAntiAffinityOnCannotBeScaled", TestAntiAffinityOnCannotBeScaled)})
	testResults = append(testResults, testResult{"TestAntiAffinityOff", t.Run("TestAntiAffinityOff", TestAntiAffinityOff)})

	if AnalyzeResults(t, testResults) {
		t.Fatalf("suite contains failures")
	}
}

func AnalyzeResults(t *testing.T, testResults []testResult) bool {
	t.Logf("Suite Test Results: \n")

	failures := []string{}
	for i, result := range testResults {
		if result.testResult {
			t.Logf("%d: %s...PASS", i+1, result.testName)
		}
		if !result.testResult {
			t.Logf("%d: %s...FAIL", i+1, result.testName)
			failures = append(failures, result.testName)

		}
	}

	pass := float64(len(testResults) - len(failures))
	fail := float64(len(failures))
	total := float64(len(testResults))
	passRate := float64((pass / total) * 100.0)

	if fail > 0 {
		t.Logf("Failures: ")
		for i, test := range failures {
			t.Logf("%d: %s", i+1, test)
		}
	}

	t.Logf("\n Pass: %f \n Fail: %f \n Pass Rate: %f", pass, fail, passRate)
	containsFailure := fail > 0
	return containsFailure
}
