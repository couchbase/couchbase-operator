package e2e

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// Variable to store random suffix for couchbase-server name & tls certificates.
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
		"TestIndexerSettings":                                 TestIndexerSettings,
		"TestQuerySettings":                                   TestQuerySettings,
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

		"TestNegValidationCreateCouchbaseCluster":                      TestNegValidationCreateCouchbaseCluster,
		"TestNegValidationCreateCouchbaseClusterNetworking":            TestNegValidationCreateCouchbaseClusterNetworking,
		"TestNegValidationCreateCouchbaseClusterNetworkingTLSStandard": TestNegValidationCreateCouchbaseClusterNetworkingTLSStandard,
		"TestNegValidationCreateCouchbaseClusterNetworkingTLSLegacy":   TestNegValidationCreateCouchbaseClusterNetworkingTLSLegacy,
		"TestNegValidationCreateCouchbaseClusterServers":               TestNegValidationCreateCouchbaseClusterServers,
		"TestNegValidationCreateCouchbaseClusterPersistentVolumes":     TestNegValidationCreateCouchbaseClusterPersistentVolumes,
		"TestNegValidationCreateCouchbaseClusterLogging":               TestNegValidationCreateCouchbaseClusterLogging,
		"TestNegValidationCreateCouchbaseClusterSettings":              TestNegValidationCreateCouchbaseClusterSettings,
		"TestNegValidationCreateCouchbaseClusterSecurity":              TestNegValidationCreateCouchbaseClusterSecurity,
		"TestNegValidationCreateCouchbaseClusterXDCR":                  TestNegValidationCreateCouchbaseClusterXDCR,
		"TestNegValidationCreateCouchbaseBucket":                       TestNegValidationCreateCouchbaseBucket,
		"TestNegValidationCreateCouchbaseEphemeralBucket":              TestNegValidationCreateCouchbaseEphemeralBucket,
		"TestNegValidationCreateCouchbaseMemcachedBucket":              TestNegValidationCreateCouchbaseMemcachedBucket,
		"TestNegValidationCreateCouchbaseReplication":                  TestNegValidationCreateCouchbaseReplication,
		"TestNegValidationCreateCouchbaseBackup":                       TestNegValidationCreateCouchbaseBackup,
		"TestNegValidationCreateCouchbaseBackupRestore":                TestNegValidationCreateCouchbaseBackupRestore,

		"TestValidationDefaultCreate":          TestValidationDefaultCreate,
		"TestNegValidationDefaultCreate":       TestNegValidationDefaultCreate,
		"TestNegValidationConstraintsCreate":   TestNegValidationConstraintsCreate,
		"TestValidationApply":                  TestValidationApply,
		"TestNegValidationApply":               TestNegValidationApply,
		"TestValidationDefaultApply":           TestValidationDefaultApply,
		"TestNegValidationConstraintsApply":    TestNegValidationConstraintsApply,
		"TestNegValidationImmutableApply":      TestNegValidationImmutableApply,
		"TestTaintK8SNodeAndRemoveTaint":       TestTaintK8SNodeAndRemoveTaint,
		"TestDenyCommunityEdition":             TestDenyCommunityEdition,
		"TestRemoveServerClassWithNodeService": TestRemoveServerClassWithNodeService,
		"TestAutoCompactionUpdate":             TestAutoCompactionUpdate,
		"TestModifyDataServiceSettings":        TestModifyDataServiceSettings,

		// System testing cases
		"TestFeaturesAll": TestFeaturesAll,

		// TLS cases
		"TestTLSCreateCluster":                             TestTLSCreateCluster,
		"TestTLSKillClusterNode":                           TestTLSKillClusterNode,
		"TestTLSResizeCluster":                             TestTLSResizeCluster,
		"TestTLSRemoveOperatorCertificateAndAddBack":       TestTLSRemoveOperatorCertificateAndAddBack,
		"TestTLSRemoveClusterCertificateAndAddBack":        TestTLSRemoveClusterCertificateAndAddBack,
		"TestTLSRemoveOperatorCertificateAndResizeCluster": TestTLSRemoveOperatorCertificateAndResizeCluster,
		"TestTLSRemoveClusterCertificateAndResizeCluster":  TestTLSRemoveClusterCertificateAndResizeCluster,
		"TestTLSNegRSACertificateDnsName":                  TestTLSNegRSACertificateDnsName,
		"TestTLSCertificateExpiry":                         TestTLSCertificateExpiry,
		"TestTLSNegCertificateExpiredBeforeDeployment":     TestTLSNegCertificateExpiredBeforeDeployment,
		"TestTLSCertificateDeployedBeforeValidity":         TestTLSCertificateDeployedBeforeValidity,
		"TestTLSGenerateWrongCACertType":                   TestTLSGenerateWrongCACertType,
		"TestTLSRotate":                                    TestTLSRotate,
		"TestTLSRotateWithShadowing":                       TestTLSRotateWithShadowing,
		"TestTLSRotateChain":                               TestTLSRotateChain,
		"TestTLSRotateCA":                                  TestTLSRotateCA,
		"TestTLSRotateCAWithShadowing":                     TestTLSRotateCAWithShadowing,
		"TestTLSRotateCAAndScale":                          TestTLSRotateCAAndScale,
		"TestTLSRotateCAAndKillOperator":                   TestTLSRotateCAAndKillOperator,
		"TestTLSRotateCAKillPodAndKillOperator":            TestTLSRotateCAKillPodAndKillOperator,
		"TestTLSRotateInvalid":                             TestTLSRotateInvalid,
		"TestTLSEditSettings":                              TestTLSEditSettings,
		"TestTLSCreateClusterWithShadowing":                TestTLSCreateClusterWithShadowing,

		// mTLS test cases
		"TestMutualTLSCreateCluster":              TestMutualTLSCreateCluster,
		"TestMutualTLSCreateClusterWithShadowing": TestMutualTLSCreateClusterWithShadowing,
		"TestMutualTLSEnable":                     TestMutualTLSEnable,
		"TestMutualTLSDisable":                    TestMutualTLSDisable,
		"TestMutualTLSRotateClient":               TestMutualTLSRotateClient,
		"TestMutualTLSRotateClientWithShadowing":  TestMutualTLSRotateClientWithShadowing,
		"TestMutualTLSRotateClientChain":          TestMutualTLSRotateClientChain,
		"TestMutualTLSRotateCA":                   TestMutualTLSRotateCA,
		"TestMutualTLSRotateCAWithShadowing":      TestMutualTLSRotateCAWithShadowing,
		"TestMutualTLSRotateInvalid":              TestMutualTLSRotateInvalid,

		"TestMandatoryMutualTLSCreateCluster":     TestMandatoryMutualTLSCreateCluster,
		"TestMandatoryMutualTLSEnable":            TestMandatoryMutualTLSEnable,
		"TestMandatoryMutualTLSDisable":           TestMandatoryMutualTLSDisable,
		"TestMandatoryMutualTLSRotateClient":      TestMandatoryMutualTLSRotateClient,
		"TestMandatoryMutualTLSRotateClientChain": TestMandatoryMutualTLSRotateClientChain,
		"TestMandatoryMutualTLSRotateCA":          TestMandatoryMutualTLSRotateCA,
		"TestMandatoryMutualTLSRotateInvalid":     TestMandatoryMutualTLSRotateInvalid,

		// XDCR cases
		"TestXDCRCreateCluster":                         TestXDCRCreateCluster,
		"TestXDCRPauseReplication":                      TestXDCRPauseReplication,
		"TestXDCRSourceNodeDown":                        TestXDCRSourceNodeDown,
		"TestXDCRSourceNodeAdd":                         TestXDCRSourceNodeAdd,
		"TestXDCRTargetNodeServiceDelete":               TestXDCRTargetNodeServiceDelete,
		"TestXDCRRebalanceOutSourceClusterNodes":        TestXDCRRebalanceOutSourceClusterNodes,
		"TestXDCRRebalanceOutTargetClusterNodes":        TestXDCRRebalanceOutTargetClusterNodes,
		"TestXDCRRemoveSourceClusterNodes":              TestXDCRRemoveSourceClusterNodes,
		"TestXDCRRemoveTargetClusterNodes":              TestXDCRRemoveTargetClusterNodes,
		"TestXDCRResizedOutSourceClusterNodes":          TestXDCRResizedOutSourceClusterNodes,
		"TestXDCRResizedOutTargetClusterNodes":          TestXDCRResizedOutTargetClusterNodes,
		"TestXDCRCreateClusterLocal":                    TestXDCRCreateClusterLocal,
		"TestXDCRCreateClusterLocalTLS":                 TestXDCRCreateClusterLocalTLS,
		"TestXDCRCreateClusterLocalMutualTLS":           TestXDCRCreateClusterLocalMutualTLS,
		"TestXDCRCreateClusterLocalMandatoryMutualTLS":  TestXDCRCreateClusterLocalMandatoryMutualTLS,
		"TestXDCRCreateClusterRemote":                   TestXDCRCreateClusterRemote,
		"TestXDCRCreateClusterRemoteTLS":                TestXDCRCreateClusterRemoteTLS,
		"TestXDCRCreateClusterRemoteMutualTLS":          TestXDCRCreateClusterRemoteMutualTLS,
		"TestXDCRCreateClusterRemoteMandatoryMutualTLS": TestXDCRCreateClusterRemoteMandatoryMutualTLS,
		"TestXDCRDeleteReplication":                     TestXDCRDeleteReplication,
		"TestXDCRFilterExp":                             TestXDCRFilterExp,
		"TestXDCRRotatePassword":                        TestXDCRRotatePassword,
		"TestXDCRRotateClientMutualTLS":                 TestXDCRRotateClientMutualTLS,
		"TestXDCRRotateCAMutualTLS":                     TestXDCRRotateCAMutualTLS,
		"TestXDCRRotateClientMandatoryMutualTLS":        TestXDCRRotateClientMandatoryMutualTLS,
		"TestXDCRRotateCAMandatoryMutualTLS":            TestXDCRRotateCAMandatoryMutualTLS,

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
		"TestLDAPCreateAdminUser":       TestLDAPCreateAdminUser,
		"TestLDAPDeleteUser":            TestLDAPDeleteUser,
		"TestLDAPDeleteRole":            TestLDAPDeleteRole,
		"TestLDAPUpdateRole":            TestLDAPUpdateRole,
		"TestLDAPRemoveUserFromBinding": TestLDAPRemoveUserFromBinding,
		"TestLDAPDeleteBinding":         TestLDAPDeleteBinding,

		// Autoscaler cases
		"TestAutoscalerValidation":                               TestAutoscalerValidation,
		"TestAutoscaleEnabled":                                   TestAutoscaleEnabled,
		"TestAutoscaleDisabled":                                  TestAutoscaleDisabled,
		"TestAutoscalerDeleted":                                  TestAutoscalerDeleted,
		"TestAutoscaleSelectiveMDS":                              TestAutoscaleSelectiveMDS,
		"TestAutoscaleUp":                                        TestAutoscaleUp,
		"TestAutoscaleUpTLS":                                     TestAutoscaleUpTLS,
		"TestAutoscaleUpMutualTLS":                               TestAutoscaleUpMutualTLS,
		"TestAutoscaleUpMandatoryMutualTLS":                      TestAutoscaleUpMandatoryMutualTLS,
		"TestAutoscaleDown":                                      TestAutoscaleDown,
		"TestAutoscaleDownTLS":                                   TestAutoscaleDownTLS,
		"TestAutoscaleDownMutualTLS":                             TestAutoscaleDownMutualTLS,
		"TestAutoscaleDownMandatoryMutualTLS":                    TestAutoscaleDownMandatoryMutualTLS,
		"TestAutoscaleMultiConfigs":                              TestAutoscaleMultiConfigs,
		"TestAutoscaleConflict":                                  TestAutoscaleConflict,
		"TestAutoScalingDisabledOnData":                          TestAutoScalingDisabledOnData,
		"TestAutoScalingDisabledOnCouchbaseBucket":               TestAutoScalingDisabledOnCouchbaseBucket,
		"TestPreviewModeAllowsEphemeral":                         TestPreviewModeAllowsEphemeral,
		"TestPreviewModeAllowsEphemeralTLS":                      TestPreviewModeAllowsEphemeralTLS,
		"TestPreviewModeAllowsEphemeralMutualTLS":                TestPreviewModeAllowsEphemeralMutualTLS,
		"TestPreviewModeAllowsEphemeralMandatoryMutualTLS":       TestPreviewModeAllowsEphemeralMandatoryMutualTLS,
		"TestPreviewModeEnabledAllowsStateful":                   TestPreviewModeEnabledAllowsStatefulTLS,
		"TestPreviewModeEnabledAllowsStatefulTLS":                TestPreviewModeEnabledAllowsStatefulMutualTLS,
		"TestPreviewModeEnabledAllowsStatefulMutualTLS":          TestPreviewModeEnabledAllowsStateful,
		"TestPreviewModeEnabledAllowsStatefulMandatoryMutualTLS": TestPreviewModeEnabledAllowsStatefulMandatoryMutualTLS,

		// Server groups / RZA cases
		"TestRzaCreateClusterWithStaticConfig":     TestRzaCreateClusterWithStaticConfig,
		"TestRzaCreateClusterWithClassBasedConfig": TestRzaCreateClusterWithClassBasedConfig,
		"TestRzaResizeCluster":                     TestRzaResizeCluster,
		"TestRzaAntiAffinityOn":                    TestRzaAntiAffinityOn,
		"TestRzaAntiAffinityOff":                   TestRzaAntiAffinityOff,
		"TestServerGroupEnable":                    TestServerGroupEnable,
		"TestServerGroupDisable":                   TestServerGroupDisable,
		"TestServerGroupAddGroup":                  TestServerGroupAddGroup,
		"TestServerGroupRemoveGroup":               TestServerGroupRemoveGroup,
		"TestServerGroupReplaceGroup":              TestServerGroupReplaceGroup,

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
		"TestPersistentVolumeKillAllPodsTLS":           TestPersistentVolumeKillAllPodsTLS,
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
		"TestLogsMetadata":                                 TestLogsMetadata,

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
		"TestUpgradeToMandatoryMutualTLS":                   TestUpgradeToMandatoryMutualTLS,
		"TestUpgradePVC":                                    TestUpgradePVC,
		"TestUpgradeImmediate":                              TestUpgradeImmediate,
		"TestUpgradeConstrained":                            TestUpgradeConstrained,
		"TestUpgradeBucketDurability":                       TestUpgradeBucketDurability,

		// Networking tests
		"TestExposedFeatureIP":                   TestExposedFeatureIP,
		"TestExposedFeatureDNS":                  TestExposedFeatureDNS,
		"TestExposedFeatureDNSModify":            TestExposedFeatureDNSModify,
		"TestExposedFeatureServiceTypeModify":    TestExposedFeatureServiceTypeModify,
		"TestConsoleServiceDNS":                  TestConsoleServiceDNS,
		"TestConsoleServiceDNSModify":            TestConsoleServiceDNSModify,
		"TestConsoleServiceTypeModify":           TestConsoleServiceTypeModify,
		"TestExposedFeatureTrafficPolicyCluster": TestExposedFeatureTrafficPolicyCluster,
		"TestLoadBalancerSourceRanges":           TestLoadBalancerSourceRanges,

		// Status tests
		"TestStatusRecovery":  TestStatusRecovery,
		"TestStatusStability": TestStatusStability,

		// Monitoring tests
		"TestPrometheusMetrics":                         TestPrometheusMetrics,
		"TestPrometheusMetricsTLS":                      TestPrometheusMetricsTLS,
		"TestPrometheusMetricsMutualTLS":                TestPrometheusMetricsMutualTLS,
		"TestPrometheusMetricsMandatoryMutualTLS":       TestPrometheusMetricsMandatoryMutualTLS,
		"TestPrometheusMetricsEnable":                   TestPrometheusMetricsEnable,
		"TestPrometheusMetricsEnableTLS":                TestPrometheusMetricsEnableTLS,
		"TestPrometheusMetricsEnableMutualTLS":          TestPrometheusMetricsEnableMutualTLS,
		"TestPrometheusMetricsEnableMandatoryMutualTLS": TestPrometheusMetricsEnableMandatoryMutualTLS,
		"TestPrometheusMetricsEnableAndPerformOps":      TestPrometheusMetricsEnableAndPerformOps,
		"TestPrometheusMetricsBearerTokenAuth":          TestPrometheusMetricsBearerTokenAuth,
		"TestPrometheusMetricsEnableAndUpgrade":         TestPrometheusMetricsEnableAndUpgrade,

		// Backup tests
		"TestFullIncremental":                TestFullIncremental,
		"TestFullIncrementalS3":              TestFullIncrementalS3,
		"TestFullOnly":                       TestFullOnly,
		"TestFullOnlyS3":                     TestFullOnlyS3,
		"TestFailedBackupBehaviour":          TestFailedBackupBehaviour,
		"TestFailedBackupBehaviourS3":        TestFailedBackupBehaviourS3,
		"TestBackupPVCReconcile":             TestBackupPVCReconcile,
		"TestBackupPVCResize":                TestBackupPVCResize,
		"TestBackupPVCResizeS3":              TestBackupPVCResizeS3,
		"TestBackupPVCReconcileS3":           TestBackupPVCReconcileS3,
		"TestReplaceFullOnlyBackup":          TestReplaceFullOnlyBackup,
		"TestReplaceFullOnlyBackupS3":        TestReplaceFullOnlyBackupS3,
		"TestReplaceFullIncrementalBackup":   TestReplaceFullIncrementalBackup,
		"TestReplaceFullIncrementalBackupS3": TestReplaceFullIncrementalBackupS3,
		"TestBackupAndRestore":               TestBackupAndRestore,
		"TestBackupAndRestoreS3":             TestBackupAndRestoreS3,
		"TestUpdateBackupStatus":             TestUpdateBackupStatus,
		"TestUpdateBackupStatusS3":           TestUpdateBackupStatusS3,
		"TestMultipleBackups":                TestMultipleBackups,
		"TestMultipleBackupsS3":              TestMultipleBackupsS3,
		"TestFullIncrementalOverTLS":         TestFullIncrementalOverTLS,
		"TestFullIncrementalOverTLSS3":       TestFullIncrementalOverTLSS3,
		"TestFullOnlyOverTLS":                TestFullOnlyOverTLS,
		"TestFullOnlyOverTLSS3":              TestFullOnlyOverTLSS3,
		"TestBackupRetention":                TestBackupRetention,
		"TestBackupRetentionS3":              TestBackupRetentionS3,
		"TestBackupAutoscaling":              TestBackupAutoscaling,

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
		"TestCreateClusterWithTLSAndControlPlaneNodeToNodeAndRotateCA":                 TestCreateClusterWithTLSAndControlPlaneNodeToNodeAndRotateCA,
		"TestCreateClusterWithTLSAndFullNodeToNodeAndRotateCA":                         TestCreateClusterWithTLSAndFullNodeToNodeAndRotateCA,
		"TestCreateClusterThenEnableControlPlaneNodeToNode":                            TestCreateClusterThenEnableControlPlaneNodeToNode,
		"TestCreateClusterThenEnableFullNodeToNode":                                    TestCreateClusterThenEnableFullNodeToNode,

		// Lights-out Recovery
		"TestLightsOutEphemeral":  TestLightsOutEphemeral,
		"TestLightsOutPersistent": TestLightsOutPersistent,

		// Epehemeral Recovery
		"TestAutoRecoveryEpehemeralWithNoAutofailover": TestAutoRecoveryEpehemeralWithNoAutofailover,

		// Security tests
		"TestRotateAdminPassword":                                TestRotateAdminPassword,
		"TestRotateAdminPasswordTLS":                             TestRotateAdminPasswordTLS,
		"TestRotateAdminPasswordMutualTLS":                       TestRotateAdminPasswordMutualTLS,
		"TestRotateAdminPasswordMandatoryMutualTLS":              TestRotateAdminPasswordMandatoryMutualTLS,
		"TestRotateAdminPasswordAndRestart":                      TestRotateAdminPasswordAndRestart,
		"TestRotateAdminPasswordAndRestartTLS":                   TestRotateAdminPasswordAndRestartTLS,
		"TestRotateAdminPasswordAndRestartMutualTLS":             TestRotateAdminPasswordAndRestartMutualTLS,
		"TestRotateAdminPasswordAndRestartMandatoryMutualTLS":    TestRotateAdminPasswordAndRestartMandatoryMutualTLS,
		"TestRotateAdminPasswordDuringRestart":                   TestRotateAdminPasswordDuringRestart,
		"TestRotateAdminPasswordDuringRestartTLS":                TestRotateAdminPasswordDuringRestartTLS,
		"TestRotateAdminPasswordDuringRestartMutualTLS":          TestRotateAdminPasswordDuringRestartMutualTLS,
		"TestRotateAdminPasswordDuringRestartMandatoryMutualTLS": TestRotateAdminPasswordDuringRestartMandatoryMutualTLS,

		// Kubernetes Rolling Upgrade
		"TestPodReadiness":             TestPodReadiness,
		"TestKubernetesRollingUpgrade": TestKubernetesRollingUpgrade,

		// Kubernetes Scheduling Tests
		"TestScheduleEvacuateAllPersistent":   TestScheduleEvacuateAllPersistent,
		"TestScheduleCleanupUninitializedPod": TestScheduleCleanupUninitializedPod,

		// SDK testing
		"TestSDK": TestSDK,

		// Hibernation tests
		"TestHibernateEphemeralImmediate":   TestHibernateEphemeralImmediate,
		"TestHibernateSupportableImmediate": TestHibernateSupportableImmediate,

		// Durability tests
		"TestCreateDurableBucket": TestCreateDurableBucket,
		"TestEditDurableBucket":   TestEditDurableBucket,
		"TestLoadDurableBucket":   TestLoadDurableBucket,

		// Expiry tests
		"TestBucketTTL":       TestBucketTTL,
		"TestBucketTTLUpdate": TestBucketTTLUpdate,

		// Logging tests
		"TestNoLogOrAuditConfig":         TestNoLogOrAuditConfig,
		"TestLoggingAndAuditingDefaults": TestLoggingAndAuditingDefaults,
		"TestAuditingNoLogging":          TestAuditingNoLogging,
		"TestCustomLogging":              TestCustomLogging,
		"TestChangeLogShipperImage":      TestChangeLogShipperImage,
	}
)

func ValidateEvents(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, events []eventschema.Validatable) {
	// On dynamic clusters that scale and reorganize deployments to save money this is
	// practically meaningless at the moment as we see loads of creations failing then
	// passing on retry.  The operator does tend to get moved at the most inopportune
	// of moments so events go missing.  From what I've see everything is actually is
	// beign, and it's working as designed in the face of shifting sands.
	if k8s.Platform == "gke-autopilot" {
		return
	}

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

	if err := retryutil.RetryFor(time.Minute, callback); err != nil {
		e2eutil.Die(t, fmt.Errorf(out.String()))
	}
}

// skipEnterpriseOnlyPlatform skips the test if it's Enterprise Edition only e.g.
// RedHat Openshift, as it doesn't have community edition binaries.
func skipEnterpriseOnlyPlatform(t *testing.T) {
	if framework.Global.KubeType == "openshift" {
		t.Skip("unsupported on platform")
	}
}

// clusterOptions collates options from the CLI and bundles them up to be propagated
// to the CR generation stuff.
func clusterOptions() *e2eutil.ClusterOptions {
	return &e2eutil.ClusterOptions{
		Options: &e2espec.ClusterOptions{
			Image:               framework.Global.CouchbaseServerImage,
			AutoFailoverTimeout: e2espec.NewDurationS(30),
			MonitoringImage:     framework.Global.CouchbaseExporterImage,
			BackupImage:         framework.Global.CouchbaseBackupImage,
			StorageClass:        framework.Global.StorageClassName,
			Platform:            framework.Global.Platform,
			Istio:               framework.Global.EnableIstio,
		},
	}
}

// clusterOptionsUpgrade does the same as above, but replaces the default image
// with the one to upgrade from.
func clusterOptionsUpgrade() *e2eutil.ClusterOptions {
	options := clusterOptions()
	options.Options.Image = framework.Global.CouchbaseServerImageUpgrade

	return options
}

// clusterOptionsUpgrade does the same as above, but replaces the default image
// with the one to upgrade from.
func clusterOptionsUpgradeMonitoring() *e2eutil.ClusterOptions {
	options := clusterOptions()
	options.Options.MonitoringImage = framework.Global.CouchbaseExporterImageUpgrade

	return options
}
