package e2e

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"k8s.io/client-go/kubernetes"
)

// String variable to store the random suffix used for couchbase-server
// name and tls certificates
var RandomNameSuffix string

var (
	envParallelTest     = "PARALLEL_TEST"
	envParallelTestTrue = "true"
	TestFuncMap         = framework.FuncMap{
		"TestCreateCluster":                                   TestCreateCluster,
		"TestCreateBucketCluster":                             TestCreateBucketCluster,
		"TestBucketAddRemoveBasic":                            TestBucketAddRemoveBasic,
		"TestEditBucket":                                      TestEditBucket,
		"TestResizeCluster":                                   TestResizeCluster,
		"TestEditClusterSettings":                             TestEditClusterSettings,
		"TestRecoveryAfterOnePodFailureNoBucket":              TestRecoveryAfterOnePodFailureNoBucket,
		"TestAntiAffinityOn":                                  TestAntiAffinityOn,
		"TestPodResourcesBasic":                               TestPodResourcesBasic,
		"TestNegBucketAdd":                                    TestNegBucketAdd,
		"TestNegBucketEdit":                                   TestNegBucketEdit,
		"TestResizeClusterWithBucket":                         TestResizeClusterWithBucket,
		"TestEditServiceConfig":                               TestEditServiceConfig,
		"TestNegEditServiceConfig":                            TestNegEditServiceConfig,
		"TestRecoveryAfterTwoPodFailureNoBucket":              TestRecoveryAfterTwoPodFailureNoBucket,
		"TestRecoveryAfterOnePodFailureBucketOneReplica":      TestRecoveryAfterOnePodFailureBucketOneReplica,
		"TestRecoveryAfterTwoPodFailureBucketOneReplica":      TestRecoveryAfterTwoPodFailureBucketOneReplica,
		"TestRecoveryAfterOnePodFailureBucketTwoReplica":      TestRecoveryAfterOnePodFailureBucketTwoReplica,
		"TestRecoveryAfterTwoPodFailureBucketTwoReplica":      TestRecoveryAfterTwoPodFailureBucketTwoReplica,
		"TestPodResourcesCannotBePlaced":                      TestPodResourcesCannotBePlaced,
		"TestFirstNodePodResourcesCannotBePlaced":             TestFirstNodePodResourcesCannotBePlaced,
		"TestAntiAffinityOnCannotBePlaced":                    TestAntiAffinityOnCannotBePlaced,
		"TestAntiAffinityOff":                                 TestAntiAffinityOff,
		"TestNegEditClusterSettings":                          TestNegEditClusterSettings,
		"TestBasicMDSScaling":                                 TestBasicMDSScaling,
		"TestSwapNodesBetweenServices":                        TestSwapNodesBetweenServices,
		"TestCreateClusterWithoutDataService":                 TestCreateClusterWithoutDataService,
		"TestCreateClusterDataServiceNotFirst":                TestCreateClusterDataServiceNotFirst,
		"TestRemoveLastDataService":                           TestRemoveLastDataService,
		"TestKillOperator":                                    TestKillOperator,
		"TestKillOperatorAndUpdateClusterConfig":              TestKillOperatorAndUpdateClusterConfig,
		"TestBucketAddRemoveExtended":                         TestBucketAddRemoveExtended,
		"TestRevertExternalBucketUpdates":                     TestRevertExternalBucketUpdates,
		"TestInvalidAuthSecret":                               TestInvalidAuthSecret,
		"TestInvalidBaseImage":                                TestInvalidBaseImage,
		"TestInvalidVersion":                                  TestInvalidVersion,
		"TestNodeUnschedulable":                               TestNodeUnschedulable,
		"TestNodeServiceDownRecovery":                         TestNodeServiceDownRecovery,
		"TestNodeServiceDownDuringRebalance":                  TestNodeServiceDownDuringRebalance,
		"TestReplaceManuallyRemovedNode":                      TestReplaceManuallyRemovedNode,
		"TestManageMultipleClusters":                          TestManageMultipleClusters,
		"TestNodeManualFailover":                              TestNodeManualFailover,
		"TestNodeRecoveryAfterMemberAdd":                      TestNodeRecoveryAfterMemberAdd,
		"TestNodeRecoveryKilledNewMember":                     TestNodeRecoveryKilledNewMember,
		"TestKillNodesAfterRebalanceAndFailover":              TestKillNodesAfterRebalanceAndFailover,
		"TestRemoveForeignNode":                               TestRemoveForeignNode,
		"TestRecoveryAfterOneNsServerFailureBucketOneReplica": TestRecoveryAfterOneNsServerFailureBucketOneReplica,
		"TestRecoveryAfterOneNodeUnreachableBucketOneReplica": TestRecoveryAfterOneNodeUnreachableBucketOneReplica,
		"TestRecoveryNodeTmpUnreachableBucketOneReplica":      TestRecoveryNodeTmpUnreachableBucketOneReplica,
		"TestPauseOperator":                                   TestPauseOperator,
		"TestNegPodResourcesBasic":                            TestNegPodResourcesBasic,
		"TestPodResourcesHigh":                                TestPodResourcesHigh,
		"TestPodResourcesLow":                                 TestPodResourcesLow,
		"TestAntiAffinityOnCannotBeScaled":                    TestAntiAffinityOnCannotBeScaled,
		"TestValidationCreate":                                TestValidationCreate,
		"TestNegValidationCreate":                             TestNegValidationCreate,
		"TestValidationDefaultCreate":                         TestValidationDefaultCreate,
		"TestNegValidationDefaultCreate":                      TestNegValidationDefaultCreate,
		"TestNegValidationConstraintsCreate":                  TestNegValidationConstraintsCreate,
		"TestValidationApply":                                 TestValidationApply,
		"TestNegValidationApply":                              TestNegValidationApply,
		"TestValidationDefaultApply":                          TestValidationDefaultApply,
		"TestNegValidationDefaultApply":                       TestNegValidationDefaultApply,
		"TestNegValidationConstraintsApply":                   TestNegValidationConstraintsApply,
		"TestNegValidationImmutableApply":                     TestNegValidationImmutableApply,
		"TestValidationDelete":                                TestValidationDelete,
		"TestNegValidationDelete":                             TestNegValidationDelete,

		"TestTlsCreateCluster":                             TestTlsCreateCluster,
		"TestTlsKillClusterNode":                           TestTlsKillClusterNode,
		"TestTlsResizeCluster":                             TestTlsResizeCluster,
		"TestTlsRemoveOperatorCertificateAndAddBack":       TestTlsRemoveOperatorCertificateAndAddBack,
		"TestTlsRemoveClusterCertificateAndAddBack":        TestTlsRemoveClusterCertificateAndAddBack,
		"TestTlsRemoveOperatorCertificateAndResizeCluster": TestTlsRemoveOperatorCertificateAndResizeCluster,
		"TestTlsRemoveClusterCertificateAndResizeCluster":  TestTlsRemoveClusterCertificateAndResizeCluster,
		"TestTlsNegRSACertificateDnsName":                  TestTlsNegRSACertificateDnsName,
		"TestTlsCertificateExpiry":                         TestTlsCertificateExpiry,
		"TestTlsNegCertificateExpiredBeforeDeployment":     TestTlsNegCertificateExpiredBeforeDeployment,
		"TestTlsCertificateDeployedBeforeValidity":         TestTlsCertificateDeployedBeforeValidity,
		"TestTlsGenerateWrongCACertType":                   TestTlsGenerateWrongCACertType,

		//"TestXdcrCreateCluster": TestXdcrCreateCluster,
	}
	DecoratorFuncMap = framework.DecoratorMap{
		"rsaDecorator": rsaDecorator,
	}
)

func ValidateClusterEvents(t *testing.T, kubeClient kubernetes.Interface, clusterName, namespace string, expectedEvents e2eutil.EventList) {
	events, err := e2eutil.GetCouchbaseEvents(kubeClient, clusterName, namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}
