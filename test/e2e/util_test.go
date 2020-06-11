package e2e

import (
	"bytes"
	"context"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// Variable to store random suffix for couchbase-server name & tls certificates
var (
	TestFuncMap = framework.FuncMap{
		"TestCreateCluster":                                   TestCreateCluster,
		"TestCreateBucketCluster":                             TestCreateBucketCluster,
		"TestBucketAddRemoveBasic":                            TestBucketAddRemoveBasic,
		"TestEditBucket":                                      TestEditBucket,
		"TestBucketUnmanaged":                                 TestBucketUnmanaged,
		"TestBucketSelection":                                 TestBucketSelection,
		"TestBucketWithExplicitName":                          TestBucketWithExplicitName,
		"TestBucketWithSameExplicitNameAndDifferentType":      TestBucketWithSameExplicitNameAndDifferentType,
		"TestDeltaRecoveryImpossible":                         TestDeltaRecoveryImpossible,
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
		"TestTaintK8SNodeAndRemoveTaint":                      TestTaintK8SNodeAndRemoveTaint,
		"TestDenyCommunityEdition":                            TestDenyCommunityEdition,
		"TestRemoveServerClassWithNodeService":                TestRemoveServerClassWithNodeService,
		"TestAutoCompactionUpdate":                            TestAutoCompactionUpdate,

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

		// mTLS test cases
		"TestMutualTLSCreateCluster":     TestMutualTLSCreateCluster,
		"TestMutualTLSEnable":            TestMutualTLSEnable,
		"TestMutualTLSDisable":           TestMutualTLSDisable,
		"TestMutualTLSRotateClient":      TestMutualTLSRotateClient,
		"TestMutualTLSRotateClientChain": TestMutualTLSRotateClientChain,
		"TestMutualTLSRotateCA":          TestMutualTLSRotateCA,
		"TestMutualTLSRotateInvalid":     TestMutualTLSRotateInvalid,

		"TestMandatoryMutualTLSCreateCluster":     TestMandatoryMutualTLSCreateCluster,
		"TestMandatoryMutualTLSEnable":            TestMandatoryMutualTLSEnable,
		"TestMandatoryMutualTLSDisable":           TestMandatoryMutualTLSDisable,
		"TestMandatoryMutualTLSRotateClient":      TestMandatoryMutualTLSRotateClient,
		"TestMandatoryMutualTLSRotateClientChain": TestMandatoryMutualTLSRotateClientChain,
		"TestMandatoryMutualTLSRotateCA":          TestMandatoryMutualTLSRotateCA,
		"TestMandatoryMutualTLSRotateInvalid":     TestMandatoryMutualTLSRotateInvalid,

		// XDCR cases
		"TestXdcrCreateCluster":                         TestXdcrCreateCluster,
		"TestXDCRPauseReplication":                      TestXDCRPauseReplication,
		"TestXdcrSourceNodeDown":                        TestXdcrSourceNodeDown,
		"TestXdcrSourceNodeAdd":                         TestXdcrSourceNodeAdd,
		"TestXdcrTargetNodeServiceDelete":               TestXdcrTargetNodeServiceDelete,
		"TestXdcrRebalanceOutSourceClusterNodes":        TestXdcrRebalanceOutSourceClusterNodes,
		"TestXdcrRebalanceOutTargetClusterNodes":        TestXdcrRebalanceOutTargetClusterNodes,
		"TestXdcrRemoveSourceClusterNodes":              TestXdcrRemoveSourceClusterNodes,
		"TestXdcrRemoveTargetClusterNodes":              TestXdcrRemoveTargetClusterNodes,
		"TestXdcrResizedOutSourceClusterNodes":          TestXdcrResizedOutSourceClusterNodes,
		"TestXdcrResizedOutTargetClusterNodes":          TestXdcrResizedOutTargetClusterNodes,
		"TestXdcrCreateClusterLocal":                    TestXdcrCreateClusterLocal,
		"TestXdcrCreateClusterLocalTLS":                 TestXdcrCreateClusterLocalTLS,
		"TestXdcrCreateClusterLocalMutualTLS":           TestXdcrCreateClusterLocalMutualTLS,
		"TestXdcrCreateClusterLocalMandatoryMutualTLS":  TestXdcrCreateClusterLocalMandatoryMutualTLS,
		"TestXdcrCreateClusterRemote":                   TestXdcrCreateClusterRemote,
		"TestXdcrCreateClusterRemoteTLS":                TestXdcrCreateClusterRemoteTLS,
		"TestXdcrCreateClusterRemoteMutualTLS":          TestXdcrCreateClusterRemoteMutualTLS,
		"TestXdcrCreateClusterRemoteMandatoryMutualTLS": TestXdcrCreateClusterRemoteMandatoryMutualTLS,
		"TestXDCRDeleteReplication":                     TestXDCRDeleteReplication,
		"TestXDCRFilterExp":                             TestXDCRFilterExp,

		// SGW tests
		"TestSyncGatewayCreateLocal":                    TestSyncGatewayCreateLocal,
		"TestSyncGatewayCreateLocalTLS":                 TestSyncGatewayCreateLocalTLS,
		"TestSyncGatewayCreateLocalMutualTLS":           TestSyncGatewayCreateLocalMutualTLS,
		"TestSyncGatewayCreateLocalMandatoryMutualTLS":  TestSyncGatewayCreateLocalMandatoryMutualTLS,
		"TestSyncGatewayCreateRemote":                   TestSyncGatewayCreateRemote,
		"TestSyncGatewayCreateRemoteTLS":                TestSyncGatewayCreateRemoteTLS,
		"TestSyncGatewayCreateRemoteMutualTLS":          TestSyncGatewayCreateRemoteMutualTLS,
		"TestSyncGatewayCreateRemoteMandatoryMutualTLS": TestSyncGatewayCreateRemoteMandatoryMutualTLS,
		"TestSyncGatewayRBAC":                           TestSyncGatewayRBAC,

		// RBAC cases
		"TestRBACValidationCreate":      TestRBACValidationCreate,
		"TestRBACValidationLDAP":        TestRBACValidationLDAP,
		"TestRBACCreateAdminUser":       TestRBACCreateAdminUser,
		"TestRBACUpdateRole":            TestRBACUpdateRole,
		"TestRBACDeleteUser":            TestRBACDeleteUser,
		"TestRBACDeleteRole":            TestRBACDeleteRole,
		"TestRBACRemoveUserFromBinding": TestRBACRemoveUserFromBinding,
		"TestRBACDeleteBinding":         TestRBACDeleteBinding,
		"TestRBACWithLDAPAuth":          TestRBACWithLDAPAuth,
		"TestRBACSelection":             TestRBACSelection,

		// LDAP cases
		"TestLDAPCreateAdminUser": TestLDAPCreateAdminUser,
		"TestLDAPCDeleteUser":     TestLDAPCDeleteUser,
		"TestLDAPDeleteRole":      TestLDAPDeleteRole,
		"TestLDAPUpdateRole":      TestLDAPUpdateRole,
		"TestLDAPRemoveUserFrom":  TestLDAPRemoveUserFromBinding,
		"TestLDAPDeleteBinding":   TestLDAPDeleteBinding,

		// Server groups / RZA cases
		"TestRzaCreateClusterWithStaticConfig":     TestRzaCreateClusterWithStaticConfig,
		"TestRzaCreateClusterWithClassBasedConfig": TestRzaCreateClusterWithClassBasedConfig,
		"TestRzaResizeCluster":                     TestRzaResizeCluster,
		"TestRzaAntiAffinityOn":                    TestRzaAntiAffinityOn,
		"TestRzaAntiAffinityOff":                   TestRzaAntiAffinityOff,

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
		"TestServerGroupAutoFailover": TestServerGroupAutoFailover,
		"TestMultiNodeAutoFailover":   TestMultiNodeAutoFailover,

		// Persistent Volume cases
		"TestPersistentVolumeCreateCluster":            TestPersistentVolumeCreateCluster,
		"TestPersistentVolumeAutoFailover":             TestPersistentVolumeAutoFailover,
		"TestPersistentVolumeAutoRecovery":             TestPersistentVolumeAutoRecovery,
		"TestPersistentVolumeKillAllPods":              TestPersistentVolumeKillAllPods,
		"TestPersistentVolumeKillPodAndOperator":       TestPersistentVolumeKillPodAndOperator,
		"TestPersistentVolumeKillAllPodsAndOperator":   TestPersistentVolumeKillAllPodsAndOperator,
		"TestPersistentVolumeRzaNodesKilled":           TestPersistentVolumeRzaNodesKilled,
		"TestPersistentVolumeRzaNodesKilledUnbalanced": TestPersistentVolumeRzaNodesKilledUnbalanced,
		"TestPersistentVolumeRzaFailover":              TestPersistentVolumeRzaFailover,
		"TestPersistentVolumeResizeCluster":            TestPersistentVolumeResizeCluster,

		// Supportability cases
		"TestLogCollectValidateArguments": TestLogCollectValidateArguments,
		"TestNegLogCollectValidateArgs":   TestNegLogCollectValidateArgs,
		"TestLogCollect":                  TestLogCollect,
		"TestLogCollectRbacPermission":    TestLogCollectRbacPermission,
		// Extended log collection cases
		"TestExtendedDebugWithDefaultValues":               TestExtendedDebugWithDefaultValues,
		"TestExtendedDebugWithNonDefaultValues":            TestExtendedDebugWithNonDefaultValues,
		"TestLogCollectInvalid":                            TestLogCollectInvalid,
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
		"TestUpgradeEnv":                                    TestUpgradeEnv,
		"TestUpgradeToSupportable":                          TestUpgradeToSupportable,
		"TestUpgradeToTLS":                                  TestUpgradeToTLS,
		"TestUpgradePVC":                                    TestUpgradePVC,

		// Networking tests
		"TestExposedFeatureIP":                   TestExposedFeatureIP,
		"TestExposedFeatureDNS":                  TestExposedFeatureDNS,
		"TestExposedFeatureDNSModify":            TestExposedFeatureDNSModify,
		"TestExposedFeatureServiceTypeModify":    TestExposedFeatureServiceTypeModify,
		"TestConsoleServiceDNS":                  TestConsoleServiceDNS,
		"TestConsoleServiceDNSModify":            TestConsoleServiceDNSModify,
		"TestConsoleServiceTypeModify":           TestConsoleServiceTypeModify,
		"TestExposedFeatureTrafficPolicyCluster": TestExposedFeatureTrafficPolicyCluster,

		// Status tests
		"TestStatusRecovery": TestStatusRecovery,

		// Monitoring tests
		"TestPrometheusMetrics":                    TestPrometheusMetrics,
		"TestPrometheusMetricsEnable":              TestPrometheusMetricsEnable,
		"TestPrometheusMetricsEnableAndPerformOps": TestPrometheusMetricsEnableAndPerformOps,
		"TestPrometheusMetricsBearerTokenAuth":     TestPrometheusMetricsBearerTokenAuth,
		"TestPrometheusMetricsEnableAndUpgrade":    TestPrometheusMetricsEnableAndUpgrade,

		// Backup tests
		"TestFullIncremental":              TestFullIncremental,
		"TestFullOnly":                     TestFullOnly,
		"TestFailedBackupBehaviour":        TestFailedBackupBehaviour,
		"TestBackupPVCReconcile":           TestBackupPVCReconcile,
		"TestReplaceFullOnlyBackup":        TestReplaceFullOnlyBackup,
		"TestReplaceFullIncrementalBackup": TestReplaceFullIncrementalBackup,
		"TestBackupAndRestore":             TestBackupAndRestore,
		"TestUpdateBackupStatus":           TestUpdateBackupStatus,
		"TestMultipleBackups":              TestMultipleBackups,
		"TestFullIncrementalOverTLS":       TestFullIncrementalOverTLS,
		"TestFullOnlyOverTLS":              TestFullOnlyOverTLS,

		// Node-to-node Encryption
		"TestCreateClusterWithTLSAndControlPlaneNodeToNode":                            TestCreateClusterWithTLSAndControlPlaneNodeToNode,
		"TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenScale":                   TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenScale,
		"TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenKillPod":                 TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenKillPod,
		"TestCreateClusterWithTLSThenEnableControlPlaneNodeToNode":                     TestCreateClusterWithTLSThenEnableControlPlaneNodeToNode,
		"TestCreateClusterWithTLSAnControlPlanedNodeToNodeThenDisableNodeToNode":       TestCreateClusterWithTLSAnControlPlanedNodeToNodeThenDisableNodeToNode,
		"TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenChangeToFullNodeToNode":  TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenChangeToFullNodeToNode,
		"TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenRotateServerCertificate": TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenRotateServerCertificate,
		"TestCreateClusterWithTLSAndFullNodeToNode":                                    TestCreateClusterWithTLSAndFullNodeToNode,
		"TestCreateClusterWithTLSAndFullNodeToNodeThenScale":                           TestCreateClusterWithTLSAndFullNodeToNodeThenScale,
		"TestCreateClusterWithTLSAndFullNodeToNodeThenKillPod":                         TestCreateClusterWithTLSAndFullNodeToNodeThenKillPod,
		"TestCreateClusterWithTLSThenEnableFullNodeToNode":                             TestCreateClusterWithTLSThenEnableFullNodeToNode,
		"TestCreateClusterWithTLSAndFullNodeToNodeThenDisableNodeToNode":               TestCreateClusterWithTLSAndFullNodeToNodeThenDisableNodeToNode,
		"TestCreateClusterWithTLSAndFullNodeToNodeThenChangeToControlPlaneNodeToNode":  TestCreateClusterWithTLSAndFullNodeToNodeThenChangeToControlPlaneNodeToNode,
		"TestCreateClusterWithTLSAndFullNodeToNodeThenRotateServerCertificate":         TestCreateClusterWithTLSAndFullNodeToNodeThenRotateServerCertificate,
	}
)

func ValidateEvents(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, events []eventschema.Validatable) {
	eventSeq := &eventschema.Sequence{Validators: events}

	out := &bytes.Buffer{}

	// Wrap the check in a retry, to avoid race conditions when any synchronization
	// we do have, isn't enough to guarantee the state of the event stream.
	callback := func() error {
		clusterEvents, err := e2eutil.GetCouchbaseEvents(k8s.KubeClient, couchbase)
		if err != nil {
			return err
		}

		out.Reset()

		v := &eventschema.Validator{Events: clusterEvents, Schema: eventSeq}
		if err := v.Validate(out); err != nil {
			return err
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		t.Log(out.String())
		t.Error(err)
	}
}

// skipEnterpriseOnlyPlatform skips the test if it's Enterprise Edition only e.g.
// RedHat Openshift, as it doesn't have community edition binaries.
func skipEnterpriseOnlyPlatform(t *testing.T) {
	if framework.Global.KubeType == "openshift" {
		t.Skip("unsupported on platform")
	}
}
