package e2e

import (
	"errors"
	"os"
	"testing"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type GroupSetupFunction map[string]func(*testing.T, []framework.ClusterInfo) error

// Variable to store random suffix for couchbase-server name & tls certificates
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
		"TestResizeClusterWithBucket":                         TestResizeClusterWithBucket,
		"TestEditServiceConfig":                               TestEditServiceConfig,
		"TestRecoveryAfterTwoPodFailureNoBucket":              TestRecoveryAfterTwoPodFailureNoBucket,
		"TestRecoveryAfterOnePodFailureBucketOneReplica":      TestRecoveryAfterOnePodFailureBucketOneReplica,
		"TestRecoveryAfterTwoPodFailureBucketOneReplica":      TestRecoveryAfterTwoPodFailureBucketOneReplica,
		"TestRecoveryAfterOnePodFailureBucketTwoReplica":      TestRecoveryAfterOnePodFailureBucketTwoReplica,
		"TestRecoveryAfterTwoPodFailureBucketTwoReplica":      TestRecoveryAfterTwoPodFailureBucketTwoReplica,
		"TestPodResourcesCannotBePlaced":                      TestPodResourcesCannotBePlaced,
		"TestFirstNodePodResourcesCannotBePlaced":             TestFirstNodePodResourcesCannotBePlaced,
		"TestAntiAffinityOnCannotBePlaced":                    TestAntiAffinityOnCannotBePlaced,
		"TestAntiAffinityOff":                                 TestAntiAffinityOff,
		"TestBasicMDSScaling":                                 TestBasicMDSScaling,
		"TestSwapNodesBetweenServices":                        TestSwapNodesBetweenServices,
		"TestCreateClusterDataServiceNotFirst":                TestCreateClusterDataServiceNotFirst,
		"TestRemoveLastDataService":                           TestRemoveLastDataService,
		"TestKillOperator":                                    TestKillOperator,
		"TestKillOperatorAndUpdateClusterConfig":              TestKillOperatorAndUpdateClusterConfig,
		"TestBucketAddRemoveExtended":                         TestBucketAddRemoveExtended,
		"TestRevertExternalBucketUpdates":                     TestRevertExternalBucketUpdates,
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
		"TestAntiAffinityOnCannotBeScaled":                    TestAntiAffinityOnCannotBeScaled,
		"TestValidationCreate":                                TestValidationCreate,
		"TestNegValidationCreate":                             TestNegValidationCreate,
		"TestValidationDefaultCreate":                         TestValidationDefaultCreate,
		"TestNegValidationDefaultCreate":                      TestNegValidationDefaultCreate,
		"TestNegValidationConstraintsCreate":                  TestNegValidationConstraintsCreate,
		"TestValidationApply":                                 TestValidationApply,
		"TestNegValidationApply":                              TestNegValidationApply,
		"TestValidationDefaultApply":                          TestValidationDefaultApply,
		"TestNegValidationConstraintsApply":                   TestNegValidationConstraintsApply,
		"TestNegValidationImmutableApply":                     TestNegValidationImmutableApply,
		"TestValidationDelete":                                TestValidationDelete,
		"TestNegValidationDelete":                             TestNegValidationDelete,
		"TestTaintK8SNodeAndRemoveTaint":                      TestTaintK8SNodeAndRemoveTaint,
		"TestDenyCommunityEdition":                            TestDenyCommunityEdition,
		"TestRemoveServerClassWithNodeService":                TestRemoveServerClassWithNodeService,

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
		"TestTLSRotate":                                    TestTLSRotate,
		"TestTLSRotateChain":                               TestTLSRotateChain,
		"TestTLSRotateCA":                                  TestTLSRotateCA,
		"TestTLSRotateCAAndScale":                          TestTLSRotateCAAndScale,
		"TestTLSRotateCAAndKillOperator":                   TestTLSRotateCAAndKillOperator,
		"TestTLSRotateCAKillPodAndKillOperator":            TestTLSRotateCAKillPodAndKillOperator,
		"TestTLSRotateInvalid":                             TestTLSRotateInvalid,

		// XDCR cases
		"TestXdcrCreateCluster":                      TestXdcrCreateCluster,
		"TestXdcrCreateTlsCluster":                   TestXdcrCreateTlsCluster,
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
		"TestRzaNegScaleupCluster":                 TestRzaNegScaleupCluster,
		"TestRzaServerGroupDown":                   TestRzaServerGroupDown,
		"TestRzaAntiAffinityOn":                    TestRzaAntiAffinityOn,
		"TestRzaAntiAffinityOff":                   TestRzaAntiAffinityOff,
		"TestRzaUpdateK8SNodeLabelAndCrd":          TestRzaUpdateK8SNodeLabelAndCrd,
		"TestRzaRemoveK8SNodeLabel":                TestRzaRemoveK8SNodeLabel,

		// 5.5 feature - Eventing cases
		"TestEventingCreateEventingCluster": TestEventingCreateEventingCluster,
		"TestEventingResizeCluster":         TestEventingResizeCluster,
		"TestEventingKillEventingPods":      TestEventingKillEventingPods,

		// 5.5 feature - Analytics cases
		"TestAnalyticsCreateDataSet":   TestAnalyticsCreateDataSet,
		"TestAnalyticsResizeCluster":   TestAnalyticsResizeCluster,
		"TestAnalyticsKillPods":        TestAnalyticsKillPods,
		"TestAnalyticsKillPodsWithPVC": TestAnalyticsKillPodsWithPVC,

		// 5.5 feature - Node Failover cases
		"TestServerGroupAutoFailover":                         TestServerGroupAutoFailover,
		"TestServerGroupWithSingleServiceNodeInFailoverGroup": TestServerGroupWithSingleServiceNodeInFailoverGroup,
		"TestDiskFailureAutoFailover":                         TestDiskFailureAutoFailover,
		"TestMultiNodeAutoFailover":                           TestMultiNodeAutoFailover,

		// Persistent Volume cases
		"TestPersistentVolumeCreateCluster":          TestPersistentVolumeCreateCluster,
		"TestPersistentVolumeAutoFailover":           TestPersistentVolumeAutoFailover,
		"TestPersistentVolumeNodeFailover":           TestPersistentVolumeNodeFailover,
		"TestPersistentVolumeKillAllPodsDeletePod":   TestPersistentVolumeKillAllPodsDeletePod,
		"TestPersistentVolumeKillAllPodsKillService": TestPersistentVolumeKillAllPodsKillService,
		"TestPersistentVolumeRemoveVolume":           TestPersistentVolumeRemoveVolume,
		"TestPersistentVolumeKillPodAndOperator":     TestPersistentVolumeKillPodAndOperator,
		"TestPersistentVolumeKillAllPodsAndOperator": TestPersistentVolumeKillAllPodsAndOperator,
		"TestPersistentVolumeRzaNodesKilled":         TestPersistentVolumeRzaNodesKilled,
		"TestPersistentVolumeRzaFailover":            TestPersistentVolumeRzaFailover,
		"TestPersistentVolumeWithSingleNodeService":  TestPersistentVolumeWithSingleNodeService,
		"TestPersistentVolumeResizeCluster":          TestPersistentVolumeResizeCluster,

		// Supportability cases
		"TestLogCollectValidateArguments":            TestLogCollectValidateArguments,
		"TestNegLogCollectValidateArgs":              TestNegLogCollectValidateArgs,
		"TestLogCollectUsingClusterNameAndNamespace": TestLogCollectUsingClusterNameAndNamespace,
		"TestLogCollectRbacPermission":               TestLogCollectRbacPermission,
		// Extended log collection cases
		"TestExtendedDebugWithDefaultValues":               TestExtendedDebugWithDefaultValues,
		"TestExtendedDebugWithNonDefaultValues":            TestExtendedDebugWithNonDefaultValues,
		"TestExtendedDebugWithInvalidValues":               TestExtendedDebugWithInvalidValues,
		"TestExtendedDebugKillOperatorDuringLogCollection": TestExtendedDebugKillOperatorDuringLogCollection,
		"TestLogCollectListJson":                           TestLogCollectListJson,

		// Log collection from Ephmeral pods
		"TestCollectLogFromEphemeralPodsUsingLogPV":                    TestCollectLogFromEphemeralPodsUsingLogPV,
		"TestCollectLogFromEphemeralPodsUsingLogPVKillProcess":         TestCollectLogFromEphemeralPodsUsingLogPVKillProcess,
		"TestCollectLogFromEphemeralPodsWithOperatorKilled":            TestCollectLogFromEphemeralPodsWithOperatorKilled,
		"TestCollectLogFromEphemeralPodsWithOperatorKilledKillProcess": TestCollectLogFromEphemeralPodsWithOperatorKilledKillProcess,
		"TestEphemeralLogCollectResizeCluster":                         TestEphemeralLogCollectResizeCluster,
		"TestLogCollectWithClusterResizeAndServerPodKilled":            TestLogCollectWithClusterResizeAndServerPodKilled,
		"TestLogCollectWithClusterResizeAndOperatorPodKilled":          TestLogCollectWithClusterResizeAndOperatorPodKilled,
		"TestLogCollectWithDefaultRetentionAndSize":                    TestLogCollectWithDefaultRetentionAndSize,
		"TestLogCollectWithCustomRetentionAndSize":                     TestLogCollectWithCustomRetentionAndSize,
		// Log collection from Persistent pods
		"TestLogCollectClusterWithPVC":                TestLogCollectClusterWithPVC,
		"TestCollectLogFromPvPodRecoveredDeletePod":   TestCollectLogFromPvPodRecoveredDeletePod,
		"TestCollectLogFromPvPodRecoveredKillService": TestCollectLogFromPvPodRecoveredKillService,
		// Log redaction cases
		"TestLogRedactionVerify":       TestLogRedactionVerify,
		"TestLogRedactionWithPvVerify": TestLogRedactionWithPvVerify,

		// Log retention regression tests
		"TestLogRetentionMultiCluster": TestLogRetentionMultiCluster,

		// Upgrade tests
		"TestUpgrade":                                       TestUpgrade,
		"TestUpgradeRollback":                               TestUpgradeRollback,
		"TestUpgradeKillPodOnCreate":                        TestUpgradeKillPodOnCreate,
		"TestUpgradeInvalidUpgrade":                         TestUpgradeInvalidUpgrade,
		"TestUpgradeInvalidDowngrade":                       TestUpgradeInvalidDowngrade,
		"TestUpgradeInvalidRollback":                        TestUpgradeInvalidRollback,
		"TestUpgradeSupportable":                            TestUpgradeSupportable,
		"TestUpgradeSupportableKillStatefulPodOnCreate":     TestUpgradeSupportableKillStatefulPodOnCreate,
		"TestUpgradeSupportableKillStatefulPodOnRebalance":  TestUpgradeSupportableKillStatefulPodOnRebalance,
		"TestUpgradeSupportableKillStatelessPodOnCreate":    TestUpgradeSupportableKillStatelessPodOnCreate,
		"TestUpgradeSupportableKillStatelessPodOnRebalance": TestUpgradeSupportableKillStatelessPodOnRebalance,

		// Networking tests
		"TestExposedFeatureIP":                   TestExposedFeatureIP,
		"TestExposedFeatureDNS":                  TestExposedFeatureDNS,
		"TestExposedFeatureDNSModify":            TestExposedFeatureDNSModify,
		"TestExposedFeatureServiceTypeModify":    TestExposedFeatureServiceTypeModify,
		"TestConsoleServiceDNS":                  TestConsoleServiceDNS,
		"TestConsoleServiceDNSModify":            TestConsoleServiceDNSModify,
		"TestConsoleServiceTypeModify":           TestConsoleServiceTypeModify,
		"TestExposedFeatureTrafficPolicyCluster": TestExposedFeatureTrafficPolicyCluster,
	}

	DecoratorFuncMap = framework.DecoratorMap{
		"recoverDecorator": framework.RecoverDecorator,
	}
)

func ValidateEvents(t *testing.T, k8s *types.Cluster, couchbase *couchbasev1.CouchbaseCluster, events e2eutil.EventValidator) {
	clusterEvents, err := e2eutil.GetCouchbaseEvents(k8s.KubeClient, couchbase.Name, couchbase.Namespace)
	if err != nil {
		t.Error(err)
		return
	}
	eventSeq := &eventschema.Sequence{Validators: events}
	v := &eventschema.Validator{Events: clusterEvents, Schema: eventSeq}
	if err := v.Validate(os.Stdout); err != nil {
		t.Error(err)
	}
}

// Remove specified label from all k8s nodes identified by kubeName
func K8SNodesRemoveLabel(nodeLabelName string, kubeClient kubernetes.Interface) error {
	k8sNodeList, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.New("Failed to get k8s nodes " + err.Error())
	}
	for _, k8sNode := range k8sNodeList.Items {
		nodeLabels := k8sNode.GetLabels()
		delete(nodeLabels, nodeLabelName)
		k8sNode.SetLabels(nodeLabels)
		if _, err = kubeClient.CoreV1().Nodes().Update(&k8sNode); err != nil {
			return errors.New("Failed to delete label for node " + k8sNode.Name + ": " + err.Error())
		}
	}
	return nil
}

// skipEnterpriseOnlyPlatform skips the test if it's Enterprise Edition only e.g.
// RedHat Openshift, as it doesn't have community edition binaries.
func skipEnterpriseOnlyPlatform(t *testing.T) {
	if framework.Global.KubeType == "openshift" {
		t.Skip("unsupported on platform")
	}
}
