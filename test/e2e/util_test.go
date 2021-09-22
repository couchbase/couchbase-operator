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

const (
	// Main test suites.  Validation is to test the CRDs against the Kubernetes API
	// Sanity is a short dirty test for only the most invasive of changes, P0 gives
	// far better test coverage with more targeted tests, P1 is the whole shebang.
	TagSuiteValidation = "validation"
	TagSuiteSanity     = "sanity"
	TagSuiteP0         = "p0"
	TagSuiteP1         = "p1"
	TagSuiteSystem     = "system"
	TagSuitePlatform   = "platform"

	// Functional test areas.  These allow you to do targeted runs without invoking
	// a broad test suite.
	TagFeatureRBAC              = "rbac"
	TagFeatureLDAP              = "ldap"
	TagFeatureLogging           = "logging"
	TagFeatureMetrics           = "metrics"
	TagFeatureTLS               = "tls"
	TagFeaturePersistentVolumes = "persistentvolumes"
	TagFeatureUpgrade           = "upgrade"
	TagFeatureSyncGateway       = "syncgateway"
	TagFeatureXDCR              = "xdcr"
	TagFeatureServerGroups      = "servergroups"
	TagFeatureSupportability    = "supportability"
	TagFeatureRecovery          = "recovery"
	TagFeatureScheduling        = "scheduling"
	TagFeatureAutoScaling       = "autoscaling"
	TagFeatureBackup            = "backup"
	TagFeatureReconcile         = "reconcile"
	TagFeatureNetwork           = "network"
	TagFeatureCollections       = "collections"
)

// registerTests does what it says on the tin.  As we can see both the framework and all the
// tests in this package scope, we need to manually tell the framework about them so it can
// make decisions based on the provided metadata.
// Rules:
// * Every test definition MUST have at least one tag so it is selectable.
// * Every test definition MUST have a suite associated with it and covered by QE.
func registerTests() {
	framework.TestDefinitions = framework.TestDefList{
		// Validation tests.
		framework.NewTestDef(TestValidationCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseCluster).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterNetworking).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterNetworkingTLSStandard).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterNetworkingTLSLegacy).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterServers).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterPersistentVolumes).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterLogging).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterSettings).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterSecurity).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterXDCR).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseBucket).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseEphemeralBucket).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseMemcachedBucket).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseReplication).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseBackup).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseBackupRestore).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseScopesAndCollections).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestValidationDefaultCreate).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestNegValidationDefaultCreate).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestNegValidationConstraintsCreate).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestValidationApply).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestNegValidationApply).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestValidationDefaultApply).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestNegValidationConstraintsApply).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestNegValidationImmutableApply).WithTags(TagSuiteValidation),
		framework.NewTestDef(TestRBACValidationCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestRBACScopeValidationCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestRBACCollectionValidationCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestRBACValidationLDAP).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestAutoscalerValidation).WithTags(TagSuiteValidation),

		// Smoke tests.
		framework.NewTestDef(TestCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestCreateBucketCluster).WithTags(TagSuiteSanity),
		framework.NewTestDef(TestResizeCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestEditClusterSettings).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestBucketAddRemoveBasic).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestEditBucket).WithTags(TagSuiteSanity),
		framework.NewTestDef(TestRecoveryAfterOnePodFailureNoBucket).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureRecovery),
		framework.NewTestDef(TestLightsOutEphemeral).WithTags(TagSuiteSanity, TagFeatureRecovery),
		framework.NewTestDef(TestAntiAffinityOn).WithTags(TagSuiteSanity, TagFeatureScheduling),
		framework.NewTestDef(TestPodResourcesBasic).WithTags(TagSuiteSanity, TagFeatureScheduling),
		framework.NewTestDef(TestTLSCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestXDCRCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterRemote).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestRzaCreateClusterWithStaticConfig).WithTags(TagSuiteSanity, TagFeatureServerGroups),
		framework.NewTestDef(TestRzaCreateClusterWithClassBasedConfig).WithTags(TagSuiteSanity, TagFeatureServerGroups),
		framework.NewTestDef(TestPersistentVolumeCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestLogCollectValidateArguments).WithTags(TagSuiteSanity, TagFeatureSupportability),
		framework.NewTestDef(TestExtendedDebugWithDefaultValues).WithTags(TagSuiteSanity, TagFeatureSupportability),
		framework.NewTestDef(TestLogRedactionVerify).WithTags(TagSuiteSanity, TagFeatureSupportability),
		framework.NewTestDef(TestAnalyticsCreateDataSet).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestEventingCreateEventingCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestPrometheusMetrics).WithTags(TagSuiteSanity, TagFeatureMetrics, TagSuitePlatform),
		framework.NewTestDef(TestBackupFullIncremental).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullIncrementalS3).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestRBACCreateAdminUser).WithTags(TagSuiteSanity, TagFeatureRBAC),
		framework.NewTestDef(TestRotateAdminPassword).WithTags(TagSuiteSanity),
		framework.NewTestDef(TestNoLogOrAuditConfig).WithTags(TagSuiteSanity, TagFeatureLogging),
		framework.NewTestDef(TestLoggingAndAuditingDefaults).WithTags(TagSuiteSanity, TagFeatureLogging, TagSuitePlatform),
		framework.NewTestDef(TestViewsCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestFTSCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform),

		// High priority tests.
		framework.NewTestDef(TestResizeClusterWithBucket).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestBasicMDSScaling).WithTags(TagSuiteP0),
		framework.NewTestDef(TestDenyCommunityEdition).WithTags(TagSuiteP0),
		framework.NewTestDef(TestAutoCompactionUpdate).WithTags(TagSuiteP0),
		framework.NewTestDef(TestModifyDataServiceSettings).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketUnmanaged).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketSelection).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketWithExplicitName).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketWithSameExplicitNameAndDifferentType).WithTags(TagSuiteP0),
		framework.NewTestDef(TestEditServiceConfig).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestCreateClusterDataServiceNotFirst).WithTags(TagSuiteP0),
		framework.NewTestDef(TestRemoveLastDataService).WithTags(TagSuiteP0),
		framework.NewTestDef(TestSwapNodesBetweenServices).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestPodResourcesCannotBePlaced).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureScheduling),
		framework.NewTestDef(TestFirstNodePodResourcesCannotBePlaced).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureScheduling),
		framework.NewTestDef(TestRemoveServerClassWithNodeService).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestRecoveryAfterTwoPodFailureNoBucket).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterOnePodFailureBucketOneReplica).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterTwoPodFailureBucketOneReplica).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterOnePodFailureBucketTwoReplica).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterTwoPodFailureBucketTwoReplica).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestLightsOutPersistent).WithTags(TagSuiteP0, TagFeaturePersistentVolumes, TagFeatureRecovery),
		framework.NewTestDef(TestAutoRecoveryEpehemeralWithNoAutofailover).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestAntiAffinityOff).WithTags(TagSuiteP0, TagFeatureScheduling),
		framework.NewTestDef(TestAntiAffinityOnCannotBePlaced).WithTags(TagSuiteP0, TagFeatureScheduling),
		framework.NewTestDef(TestKillOperator).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestKillOperatorAndUpdateClusterConfig).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestTLSKillClusterNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSResizeCluster).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotate).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateChain).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateCA).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateInvalid).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSEnable).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSDisable).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSRotateClient).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSRotateClientChain).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSRotateCA).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSRotateInvalid).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSCreateCluster).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSEnable).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSDisable).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateClient).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateClientChain).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateCA).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateInvalid).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSEditSettings).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSCreateClusterWithShadowing).WithTags(TagSuiteP0, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestXDCRCreateClusterLocal).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterLocalTLS).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterLocalMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterLocalMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterRemoteTLS).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterRemoteMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterRemoteMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRSourceNodeDown).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRSourceNodeAdd).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRTargetNodeServiceDelete).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRotatePassword).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestSyncGatewayCreateLocal).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateLocalTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateLocalMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateLocalMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayRBAC).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemote).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemoteTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemoteMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemoteMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestRzaResizeCluster).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupEnable).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupDisable).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupAddGroup).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupRemoveGroup).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupReplaceGroup).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestPersistentVolumeAutoFailover).WithTags(TagSuiteP0, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeAutoRecovery).WithTags(TagSuiteP0, TagSuitePlatform, TagFeaturePersistentVolumes, TagFeatureRecovery),
		framework.NewTestDef(TestPersistentVolumeKillAllPodsTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestServerGroupAutoFailover).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestMultiNodeAutoFailover).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestAnalyticsResizeCluster).WithTags(TagSuiteP0),
		framework.NewTestDef(TestNegLogCollectValidateArgs).WithTags(TagSuiteP0, TagFeatureSupportability),
		framework.NewTestDef(TestLogCollect).WithTags(TagSuiteP0, TagFeatureSupportability),
		framework.NewTestDef(TestExtendedDebugWithNonDefaultValues).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureSupportability),
		framework.NewTestDef(TestCollectLogFromEphemeralPodsUsingLogPVKillProcess).WithTags(TagSuiteP0, TagFeatureSupportability),
		framework.NewTestDef(TestLogRedactionWithPvVerify).WithTags(TagSuiteP0, TagFeatureSupportability),
		framework.NewTestDef(TestLogCollectListJson).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureSupportability),
		framework.NewTestDef(TestCollectLogFromEphemeralPodsUsingLogPV).WithTags(TagSuiteP0, TagFeatureSupportability),
		framework.NewTestDef(TestLogCollectWithDefaultRetentionAndSize).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureSupportability),
		framework.NewTestDef(TestLogsMetadata).WithTags(TagSuiteP0, TagFeatureSupportability),
		framework.NewTestDef(TestUpgradeInvalidDowngrade).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeInvalidUpgrade).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgrade).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeSupportableKillStatefulPodOnCreate).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeSupportableKillStatefulPodOnRebalance).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeImmediate).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeConstrained).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeBucketDurability).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestExposedFeatureIP).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestExposedFeatureDNS).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestExposedFeatureDNSModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestExposedFeatureServiceTypeModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceDNS).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceDNSModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceTypeModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestExposedFeatureTrafficPolicyCluster).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestRBACDeleteUser).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC),
		framework.NewTestDef(TestRBACUpdateRole).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC),
		framework.NewTestDef(TestRBACDeleteRole).WithTags(TagSuiteP0, TagFeatureRBAC),
		framework.NewTestDef(TestLDAPCreateAdminUser).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLDAPDeleteUser).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLDAPDeleteRole).WithTags(TagSuiteP0, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLDAPUpdateRole).WithTags(TagSuiteP0, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestVerifyLDAPConfigRetention).WithTags(TagSuiteP0, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestBackupFullOnly).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestore).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreS3).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullIncrementalOverTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullIncrementalOverTLSS3).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverTLSStandard).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverTLSS3).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverTLSS3Standard).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestMultipleBackups).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestMultipleBackupsS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupRetention).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupRetentionS3).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestBackupPVCResize).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableEventing).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableEventingS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableGSI).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableGSIS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableAnalytics).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableAnalyticsS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableData).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableDataS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreEnableBucketConfig).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreEnableBucketConfigS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreMapBuckets).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreMapBucketsS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreIncludeBuckets).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreIncludeBucketsS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreExcludeBuckets).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreExcludeBucketsS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreNodeSelector).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreNodeSelectorS3).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestPrometheusMetricsTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsMutualTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsEnable).WithTags(TagSuiteP0, TagFeatureMetrics),
		framework.NewTestDef(TestPrometheusMetricsEnableTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsEnableMutualTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsEnableMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsPerformOps).WithTags(TagSuiteP0, TagFeatureMetrics),
		framework.NewTestDef(TestPrometheusMetricsBearerTokenAuth).WithTags(TagSuiteP0, TagFeatureMetrics),
		framework.NewTestDef(TestPrometheusMetricsUpgrade).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureUpgrade),
		framework.NewTestDef(TestPrometheusMetricsOperator).WithTags(TagSuiteP0, TagFeatureMetrics),
		framework.NewTestDef(TestCouchbaseMetricsDocumentCount).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureCollections),
		framework.NewTestDef(TestCreateClusterWithTLSAndControlPlaneNodeToNode).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenScale).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenKillPod).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSThenEnableControlPlaneNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAnControlPlanedNodeToNodeThenDisableNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenChangeToFullNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndControlPlaneNodeToNodeThenRotateServerCertificate).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNodeThenScale).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNodeThenKillPod).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSThenEnableFullNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNodeThenDisableNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNodeThenChangeToControlPlaneNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNodeThenRotateServerCertificate).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndControlPlaneNodeToNodeAndRotateCA).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNodeAndRotateCA).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestCreateClusterThenEnableControlPlaneNodeToNode).WithTags(TagSuiteP0),
		framework.NewTestDef(TestCreateClusterThenEnableFullNodeToNode).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestRotateAdminPasswordAndRestart).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestRotateAdminPasswordDuringRestart).WithTags(TagSuiteP0),
		framework.NewTestDef(TestPodReadiness).WithTags(TagSuiteP0),
		framework.NewTestDef(TestSDK).WithTags(TagSuiteP0),
		framework.NewTestDef(TestHibernateEphemeralImmediate).WithTags(TagSuiteP0),
		framework.NewTestDef(TestHibernateSupportableImmediate).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestCreateDurableBucket).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestEditDurableBucket).WithTags(TagSuiteP0),
		framework.NewTestDef(TestLoadDurableBucket).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketTTL).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestBucketTTLUpdate).WithTags(TagSuiteP0),
		framework.NewTestDef(TestAutoscaleEnabled).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleDisabled).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscalerDeleted).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleSelectiveMDS).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleUpMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleDownMandatoryMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleMultiConfigs).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleConflict).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscalerValidation).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleVerifyMaintenanceMode).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleEnabledAllowsStatefulTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleManualMaintenanceMode).WithTags(TagSuiteP0, TagFeatureAutoScaling),
		framework.NewTestDef(TestAuditingNoLogging).WithTags(TagSuiteP0, TagFeatureLogging),
		framework.NewTestDef(TestCustomLogging).WithTags(TagSuiteP0, TagFeatureLogging),
		framework.NewTestDef(TestChangeLogShipperImage).WithTags(TagSuiteP0, TagFeatureLogging),
		framework.NewTestDef(TestInflightLogRedaction).WithTags(TagSuiteP0, TagFeatureLogging, TagSuitePlatform),
		framework.NewTestDef(TestRebalanceLogProcessing).WithTags(TagSuiteP0, TagFeatureLogging),
		framework.NewTestDef(TestLoggingDynamicConfigReload).WithTags(TagSuiteP0, TagFeatureLogging),
		framework.NewTestDef(TestLoggingUpgrade).WithTags(TagSuiteP0, TagFeatureLogging, TagFeatureUpgrade),
		framework.NewTestDef(TestScopeCreateExplicit).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeCreateImplicit).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeCreateMixed).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeDelete).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeUnmanaged).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionCreateExplicit).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionCreateImplicit).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionCreateMixed).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionDelete).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionUnmanaged).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestDefaultCollectionDeletion).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopesAndCollectionsSharedScopeTopology).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopesAndCollectionsSharedCollectionTopology).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopesAndCollectionsCascadingScopeDeletion).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeOverflow).WithTags(TagSuiteP1, TagFeatureCollections),
		framework.NewTestDef(TestCollectionOverflow).WithTags(TagSuiteP1, TagFeatureCollections),
		framework.NewTestDef(TestViewsResizeCluster).WithTags(TagSuiteP0),
		framework.NewTestDef(TestViewsWithScopesAndCollections).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestFTSResizeCluster).WithTags(TagSuiteP0),
		framework.NewTestDef(TestFTSWithScopesAndCollections).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestAnalyticsCreateDataSetWithCollections).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestGSIWithCollections).WithTags(TagSuiteP0, TagFeatureCollections),

		// Low priority tests.
		framework.NewTestDef(TestInvalidBaseImage).WithTags(TagSuiteP1, TagSuitePlatform),
		framework.NewTestDef(TestInvalidVersion).WithTags(TagSuiteP1, TagSuitePlatform),
		framework.NewTestDef(TestManageMultipleClusters).WithTags(TagSuiteP1),
		framework.NewTestDef(TestIndexerSettings).WithTags(TagSuiteP1, TagSuitePlatform),
		framework.NewTestDef(TestQuerySettings).WithTags(TagSuiteP1),
		framework.NewTestDef(TestNodeUnschedulable).WithTags(TagSuiteP1),
		framework.NewTestDef(TestNodeServiceDownRecovery).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureRecovery),
		framework.NewTestDef(TestNodeServiceDownDuringRebalance).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestNodeManualFailover).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestKillNodesAfterRebalanceAndFailover).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestRemoveForeignNode).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestReplaceManuallyRemovedNode).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestNodeRecoveryAfterMemberAdd).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureRecovery),
		framework.NewTestDef(TestNodeRecoveryKilledNewMember).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterOneNsServerFailureBucketOneReplica).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterOneNodeUnreachableBucketOneReplica).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryNodeTmpUnreachableBucketOneReplica).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestAntiAffinityOnCannotBeScaled).WithTags(TagSuiteP1, TagFeatureScheduling),
		framework.NewTestDef(TestBucketAddRemoveExtended).WithTags(TagSuiteP1),
		framework.NewTestDef(TestRevertExternalBucketUpdates).WithTags(TagSuiteP1),
		framework.NewTestDef(TestDeltaRecoveryImpossible).WithTags(TagSuiteP1, TagFeatureRecovery),
		framework.NewTestDef(TestPauseOperator).WithTags(TagSuiteP1),
		framework.NewTestDef(TestNegPodResourcesBasic).WithTags(TagSuiteP1, TagFeatureScheduling),
		framework.NewTestDef(TestTLSRemoveOperatorCertificateAndAddBack).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestTLSRemoveClusterCertificateAndAddBack).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRemoveOperatorCertificateAndResizeCluster).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRemoveClusterCertificateAndResizeCluster).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSNegRSACertificateDnsName).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSCertificateExpiry).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSNegCertificateExpiredBeforeDeployment).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSCertificateDeployedBeforeValidity).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSGenerateWrongCACertType).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateCAAndScale).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateCAAndKillOperator).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateWithShadowing).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateCAWithShadowing).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSCreateClusterWithShadowing).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSRotateClientWithShadowing).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSRotateCAWithShadowing).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateCAKillPodAndKillOperator).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestXDCRPauseReplication).WithTags(TagSuiteP1, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRebalanceOutSourceClusterNodes).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRebalanceOutTargetClusterNodes).WithTags(TagSuiteP1, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRemoveSourceClusterNodes).WithTags(TagSuiteP1, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRemoveTargetClusterNodes).WithTags(TagSuiteP1, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRResizedOutSourceClusterNodes).WithTags(TagSuiteP1, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRResizedOutTargetClusterNodes).WithTags(TagSuiteP1, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRDeleteReplication).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRFilterExp).WithTags(TagSuiteP1, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRotateClientMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRotateCAMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRotateClientMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRReplicateLocalScopesAndCollections).WithTags(TagSuiteP1, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRMigrationLocalScopesAndCollections).WithTags(TagSuiteP1, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRRotateCAMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR, TagSuitePlatform),
		framework.NewTestDef(TestRzaAntiAffinityOn).WithTags(TagSuiteP1, TagFeatureServerGroups, TagFeatureScheduling),
		framework.NewTestDef(TestRzaAntiAffinityOff).WithTags(TagSuiteP1, TagFeatureServerGroups, TagFeatureScheduling),
		framework.NewTestDef(TestPersistentVolumeKillAllPods).WithTags(TagSuiteP1, TagSuitePlatform, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeKillPodAndOperator).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeKillAllPodsAndOperator).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeRzaNodesKilled).WithTags(TagSuiteP1, TagFeatureServerGroups, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeRzaNodesKilledUnbalanced).WithTags(TagSuiteP1, TagFeatureServerGroups, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeRzaFailover).WithTags(TagSuiteP1, TagFeatureServerGroups, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeResizeCluster).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestOnlinePersistentVolumeResize).WithTags(TagSuiteP1, TagFeaturePersistentVolumes, TagSuitePlatform),
		framework.NewTestDef(TestOnlinePersistentVolumeResizeMDS).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestOnlinePersistentVolumeResizeMixedClaims).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestOnlinePersistentVolumeResizeWithDocs).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestOnlinePersistentVolumeResizeNop).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestOnlinePersistentVolumeResizeWhenPodKilled).WithTags(TagSuiteP1, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPrometheusRotateAdminPassword).WithTags(TagSuiteP1, TagFeatureMetrics),
		framework.NewTestDef(TestPrometheusRotateCA).WithTags(TagSuiteP1, TagFeatureMetrics),
		framework.NewTestDef(TestEventingResizeCluster).WithTags(TagSuiteP1),
		framework.NewTestDef(TestEventingKillEventingPods).WithTags(TagSuiteP1),
		framework.NewTestDef(TestLogCollectRbacPermission).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureSupportability),
		framework.NewTestDef(TestLogCollectInvalid).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureSupportability),
		framework.NewTestDef(TestExtendedDebugKillOperatorDuringLogCollection).WithTags(TagSuiteP1, TagFeatureSupportability),
		framework.NewTestDef(TestCollectLogFromEphemeralPodsWithOperatorKilled).WithTags(TagSuiteP1, TagFeatureSupportability),
		framework.NewTestDef(TestCollectLogFromEphemeralPodsWithOperatorKilledKillProcess).WithTags(TagSuiteP1, TagFeatureSupportability),
		framework.NewTestDef(TestEphemeralLogCollectResizeCluster).WithTags(TagSuiteP1, TagFeatureSupportability),
		framework.NewTestDef(TestLogCollectWithClusterResizeAndServerPodKilled).WithTags(TagSuiteP1, TagFeatureSupportability),
		framework.NewTestDef(TestLogCollectWithCustomRetentionAndSize).WithTags(TagSuiteP1, TagFeatureSupportability),
		framework.NewTestDef(TestUpgradeSupportableKillStatelessPodOnCreate).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeSupportableKillStatelessPodOnRebalance).WithTags(TagSuiteP1, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeRollback).WithTags(TagSuiteP1, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeKillPodOnCreate).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeInvalidRollback).WithTags(TagSuiteP1, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeSupportable).WithTags(TagSuiteP1, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeEnv).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeToSupportable).WithTags(TagSuiteP1, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeToTLS).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeToMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradePVC).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradePVCStorageClass).WithTags(TagSuiteP1, TagFeatureUpgrade),
		framework.NewTestDef(TestStatusRecovery).WithTags(TagSuiteP1),
		framework.NewTestDef(TestStatusStability).WithTags(TagSuiteP1),

		// RBAC Tests
		framework.NewTestDef(TestRBACRemoveUserFromBinding).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureRBAC),
		framework.NewTestDef(TestRBACDeleteBinding).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithLDAPAuth).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureRBAC, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestRBACSelection).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureRBAC),
		framework.NewTestDef(TestLDAPRemoveUserFromBinding).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLDAPDeleteBinding).WithTags(TagSuiteP1, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestRBACWithBucketScopedRolePost7).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithBucketScopedRolePre7).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithScopeScopedRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithCollectionScopedRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleScopedRolesViaScopeGroup).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleCollectionScopedRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleCollectionAndMultipleScopesScopedRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithCollectionScopedRoleBySelector).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithCollectionScopedRoleBySelectorForGroups).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithCollectionScopedRoleBySelectorForMultipleScopesAndCollections).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithWillNotGetPermissionSetWithInvalidSelector).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithScopeScopedRoleNoPermissionWhenNoScopeCreated).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithCollectionScopedRoleDoesNotAddPermissionDuetoNoCollection).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleBucketRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleBucketSingleScopeRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleBucketMultiScopeRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleBucketMultiScopeSingleCollectionRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithMultipleBucketMultiScopeMultiCollectionRole).WithTags(TagSuiteP1, TagFeatureRBAC),
		framework.NewTestDef(TestRBACWithBucketSelector).WithTags(TagSuiteP1, TagFeatureRBAC),
		// end RBAC

		framework.NewTestDef(TestFailedBackupBehaviour).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestFailedBackupBehaviourS3).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupPVCReconcile).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupPVCReconcileS3).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestReplaceFullOnlyBackup).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestReplaceFullOnlyBackupS3).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestReplaceFullIncrementalBackup).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestReplaceFullIncrementalBackupS3).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestUpdateBackupStatus).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestUpdateBackupStatusS3).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupAutoscaling).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestKubernetesRollingUpgrade).WithTags(TagSuiteP1),
		framework.NewTestDef(TestRotateAdminPasswordTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordAndRestartTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordAndRestartMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordAndRestartMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordDuringRestartTLS).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordDuringRestartMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestRotateAdminPasswordDuringRestartMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestAutoscaleUp).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleUpMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleDown).WithTags(TagSuiteP1, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleDownMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleMultiConfigs).WithTags(TagSuiteP1, TagFeatureAutoScaling),
		framework.NewTestDef(TestScheduleEvacuateAllPersistent).WithTags(TagSuiteP1, TagFeaturePersistentVolumes, TagFeatureScheduling),
		framework.NewTestDef(TestScheduleCleanupUninitializedPod).WithTags(TagSuiteP1),
		framework.NewTestDef(TestCustomAnnotationsAndLabelsStayAfterReconcile).WithTags(TagSuiteP1, TagFeatureReconcile),
		framework.NewTestDef(TestBackupBucketInclusion).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupBucketExclusion).WithTags(TagSuiteP1, TagFeatureBackup),

		// System tests.
		framework.NewTestDef(TestFeaturesAll).WithTags(TagSuiteSystem),
	}
}

func ValidateEvents(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, events []eventschema.Validatable) {
	// On dynamic clusters that scale and reorganize deployments to save money this is
	// practically meaningless at the moment as we see loads of creations failing then
	// passing on retry.  The operator does tend to get moved at the most inopportune
	// of moments so events go missing.  From what I've see everything is actually is
	// beign, and it's working as designed in the face of shifting sands.
	if k8s.DynamicPlatform {
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
			LoggingImage:        framework.Global.CouchbaseLoggingImage,
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

func clusterOptionsUpgradeLogging() *e2eutil.ClusterOptions {
	options := clusterOptions()
	options.Options.LoggingImage = framework.Global.CouchbaseLoggingImageUpgrade

	return options
}
