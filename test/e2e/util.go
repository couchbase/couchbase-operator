package e2e

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"k8s.io/client-go/kubernetes"
)

// Variable to store random suffix for couchbase-server name & tls certificates
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
		"TestTaintK8SNodeAndRemoveTaint":                      TestTaintK8SNodeAndRemoveTaint,

		// System testing cases
		"TestFeaturesAll": TestFeaturesAll,

		// Tls cases
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

		// XDCR cases
		"TestXdcrCreateCluster":                      TestXdcrCreateCluster,
		"TestXdcrCreateTlsCluster":                   TestXdcrCreateTlsCluster,
		"TestXdcrCreateInterCluster":                 TestXdcrCreateInterCluster,
		"TestXdcrCreateK8SVMCluster":                 TestXdcrCreateK8SVMCluster,
		"TestXdcrNodeDownDuringSetupDuringConfigure": TestXdcrNodeDownDuringSetupDuringConfigure,
		"TestXdcrNodeDownDuringSetupAfterConfigure":  TestXdcrNodeDownDuringSetupAfterConfigure,
		"TestXdcrNodeAddDuringSetupDuringConfigure":  TestXdcrNodeAddDuringSetupDuringConfigure,
		"TestXdcrNodeAddDuringSetupAfterConfigure":   TestXdcrNodeAddDuringSetupAfterConfigure,
		"TestXdcrNodeServiceKilledDuringConfigure":   TestXdcrNodeServiceKilledDuringConfigure,
		"TestXdcrNodeServiceKilledAfterConfigure":    TestXdcrNodeServiceKilledAfterConfigure,
		"TestXdcrRebalanceOutSourceClusterNodes":     TestXdcrRebalanceOutSourceClusterNodes,
		"TestXdcrRebalanceOutTargetClusterNodes":     TestXdcrRebalanceOutTargetClusterNodes,
		"TestXdcrRemoveSourceClusterNodes":           TestXdcrRemoveSourceClusterNodes,
		"TestXdcrRemoveTargetClusterNodes":           TestXdcrRemoveTargetClusterNodes,
		"TestXdcrResizedOutSourceClusterNodes":       TestXdcrResizedOutSourceClusterNodes,
		"TestXdcrResizedOutTargetClusterNodes":       TestXdcrResizedOutTargetClusterNodes,

		// Server groups / RZA cases
		"TestRzaCreateClusterWithStaticConfig":     TestRzaCreateClusterWithStaticConfig,
		"TestRzaCreateClusterWithClassBasedConfig": TestRzaCreateClusterWithClassBasedConfig,
		"TestRzaResizeCluster":                     TestRzaResizeCluster,
		"TestRzaServerGroupRemoval":                TestRzaServerGroupRemoval,
		"TestRzaServerGroupAddition":               TestRzaServerGroupAddition,
		"TestRzaKillServerPods":                    TestRzaKillServerPods,
		"TestRzaNegScaleupCluster":                 TestRzaNegScaleupCluster,
		"TestRzaServerGroupDown":                   TestRzaServerGroupDown,
		"TestRzaAntiAffinityOn":                    TestRzaAntiAffinityOn,
		"TestRzaAntiAffinityOff":                   TestRzaAntiAffinityOff,
		"TestRzaUpdateK8SNodeLabelAndCrd":          TestRzaUpdateK8SNodeLabelAndCrd,
		"TestRzaRemoveK8SNodeLabel":                TestRzaRemoveK8SNodeLabel,

		// 5.5 feature - Eventing cases
		//"TestEventingOnK8sBasic": TestEventingOnK8sBasic,

		// 5.5 feature - Analytics cases
		//"TestAnalyticsCreateDataSet":   TestAnalyticsCreateDataSet,
		//"TestAnalyticsResizeCluster":   TestAnalyticsResizeCluster,
		//"TestAnalyticsKillPods":        TestAnalyticsKillPods,
		//"TestAnalyticsKillPodsWithPVC": TestAnalyticsKillPodsWithPVC,

		// 5.5 feature - Node Failover cases
		"TestServerGroupAutoFailover":                         TestServerGroupAutoFailover,
		"TestServerGroupWithSingleServiceNodeInFailoverGroup": TestServerGroupWithSingleServiceNodeInFailoverGroup,
		"TestDiskFailureAutoFailover":                         TestDiskFailureAutoFailover,
		"TestMultiNodeAutoFailover":                           TestMultiNodeAutoFailover,

		// Persistent Volume cases
		"TestPersistentVolumeCreateCluster":          TestPersistentVolumeCreateCluster,
		"TestPersistentVolumeAutoFailover":           TestPersistentVolumeAutoFailover,
		"TestPersistentVolumeNodeFailover":           TestPersistentVolumeNodeFailover,
		"TestPersistentVolumeKillAllPods":            TestPersistentVolumeKillAllPods,
		"TestPersistentVolumeRemoveVolume":           TestPersistentVolumeRemoveVolume,
		"TestPersistentVolumeKillPodAndOperator":     TestPersistentVolumeKillPodAndOperator,
		"TestPersistentVolumeKillAllPodsAndOperator": TestPersistentVolumeKillAllPodsAndOperator,
		"TestPersistentVolumeRzaNodesKilled":         TestPersistentVolumeRzaNodesKilled,
		"TestPersistentVolumeRzaFailover":            TestPersistentVolumeRzaFailover,
		"TestPersistentVolumeWithSingleNodeService":  TestPersistentVolumeWithSingleNodeService,
		"TestPersistentVolumeResizeCluster":          TestPersistentVolumeResizeCluster,
		"TestNegPersistentVolumeCreateCluster":       TestNegPersistentVolumeCreateCluster,
	}
	DecoratorFuncMap = framework.DecoratorMap{
		"rsaDecorator":     rsaDecorator,
		"rzaNodeLabeller":  rzaNodeLabeller,
		"recoverDecorator": framework.RecoverDecorator,
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
