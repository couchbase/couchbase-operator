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
	TagFeatureLPV               = "lpv"
	TagFeatureUpgrade           = "upgrade"
	TagFeatureSyncGateway       = "syncgateway"
	TagFeatureXDCR              = "xdcr"
	TagFeatureServerGroups      = "servergroups"
	TagFeatureSupportability    = "supportability"
	TagFeatureRecovery          = "recovery"
	TagFeatureScheduling        = "scheduling"
	TagFeatureAutoScaling       = "autoscaling"
	TagFeatureBackup            = "backup"
	TagFeatureBackupCloud       = "backupcloud"
	TagFeatureReconcile         = "reconcile"
	TagFeatureNetwork           = "network"
	TagFeatureCollections       = "collections"
	TagFeatureSynchronization   = "synchronization"
	TagFeatureDeleteDelay       = "deletedelay"
	TagFeatureCNG               = "cng"
	TagFeatureBucketMigration   = "bucketmigration"
	TagFeatureAssimilation      = "assimilation"
	TagFeatureAdmission         = "admission"
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
		framework.NewTestDef(TestCBVersionSpecificPosValidationsCreateCouchbaseClusterSettings).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterSecurity).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseClusterXDCR).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestValidationCreateCouchbaseClusterXDCR).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseBucket).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseEphemeralBucket).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseMemcachedBucket).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseReplication).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestValidationCreateCouchbaseBackup).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestValidationCreateCouchbaseBackupRestore).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseBackup).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseBackupRestore).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationCreateCouchbaseScopesAndCollections).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestValidationDefaultCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationDefaultCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationConstraintsCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestValidationApply).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationApply).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestValidationDefaultApply).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationConstraintsApply).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationImmutableApply).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestRBACValidationCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestRBACScopeValidationCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestRBACCollectionValidationCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestRBACValidationLDAP).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestAutoscalerValidation).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestCNGVersionValidation).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestBucketMigrationPre76Invalid).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestBucketMigrationPost76Validation).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestAnnotationValidation).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestAnnotationVersionValidation).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestDefaultStorageBackendChangeValidation).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestBucketMinReplicasCountApply).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestBucketMinReplicasCountCreate).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestVersionUpgradePath).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestClusterChangesDuringHibernation).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestClusterMigrationAddition).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestClusterMigrationInvalidMigration).WithTags(TagSuiteValidation, TagSuitePlatform),
		framework.NewTestDef(TestNegValidationClusterMigrationApply).WithTags(TagSuiteValidation, TagSuitePlatform),

		// Smoke tests.
		framework.NewTestDef(TestCreateCNG).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureCNG),
		framework.NewTestDef(TestCNGLiveConfigReload).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureCNG),
		framework.NewTestDef(TestCNGDataAPI).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureCNG),
		framework.NewTestDef(TestCNGDataAPIConfigChangeRestart).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureCNG),
		framework.NewTestDef(TestCNGBucketOps).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureCNG),
		framework.NewTestDef(TestCngOtlp).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureCNG),
		framework.NewTestDef(TestCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestCLIParametersCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
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
		framework.NewTestDef(TestPKCS12CreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMutualTLSCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestXDCRCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterRemote).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestServerGroupShuffling).WithTags(TagSuiteSanity, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupRescheduling).WithTags(TagSuiteSanity, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupReschedulingInitialNode).WithTags(TagSuiteSanity, TagFeatureServerGroups),
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
		framework.NewTestDef(TestBackupFullIncrementalS3).WithTags(TagSuiteSanity, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullIncrementalAzure).WithTags(TagSuiteSanity, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullIncrementalGCP).WithTags(TagSuiteSanity, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestRBACCreateAdminUser).WithTags(TagSuiteSanity, TagFeatureRBAC),
		framework.NewTestDef(TestRBACCreateUserClusterRoles).WithTags(TagSuiteSanity, TagFeatureRBAC),
		framework.NewTestDef(TestRotateAdminPassword).WithTags(TagSuiteSanity),
		framework.NewTestDef(TestNoLogOrAuditConfig).WithTags(TagSuiteSanity, TagFeatureLogging),
		framework.NewTestDef(TestLoggingAndAuditingDefaults).WithTags(TagSuiteSanity, TagFeatureLogging, TagSuitePlatform),
		framework.NewTestDef(TestLoggingAndAuditingNativeCleanup).WithTags(TagSuiteSanity, TagFeatureLogging, TagSuitePlatform),
		framework.NewTestDef(TestViewsCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestFTSCreateCluster).WithTags(TagSuiteSanity, TagSuitePlatform),
		framework.NewTestDef(TestSyncGatewayCreateLocal).WithTags(TagSuiteSanity, TagSuitePlatform, TagFeatureSyncGateway),
		framework.NewTestDef(TestMovePod).WithTags(TagSuiteSanity),
		framework.NewTestDef(TestServicelessClass).WithTags(TagSuiteSanity),

		// High priority tests.
		framework.NewTestDef(TestBucketHistoryRetentionWithAnnotations).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestBucketHistoryRetentionRetentionSettingsInCRD).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestResizeClusterWithBucket).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestBasicMDSScaling).WithTags(TagSuiteP0),
		framework.NewTestDef(TestDenyCommunityEdition).WithTags(TagSuiteP0),
		framework.NewTestDef(TestAutoCompactionUpdate).WithTags(TagSuiteP0),
		framework.NewTestDef(TestModifyDataServiceSettings).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketUnmanaged).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketSelection).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketWithExplicitName).WithTags(TagSuiteP0),
		framework.NewTestDef(TestBucketWithSameExplicitNameAndDifferentType).WithTags(TagSuiteP0),
		framework.NewTestDef(TestCouchbaseBucketStorageBackendMagmaInvalidForFtsAnalyticsEventing).WithTags(TagSuiteP0),
		framework.NewTestDef(TestSampleBucket).WithTags(TagSuiteP0),
		framework.NewTestDef(TestUpdateSampleBucket).WithTags(TagSuiteP0),
		framework.NewTestDef(TestDeltaRecovery).WithTags(TagSuiteP0),
		framework.NewTestDef(TestPartialUpgrade).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestDeltaRecoveryWithoutDataService).WithTags(TagSuiteP0),
		framework.NewTestDef(TestResilientDeltaRecovery).WithTags(TagSuiteP0),
		framework.NewTestDef(TestDeltaRecoveryWithVariousServices).WithTags(TagSuiteP0),
		framework.NewTestDef(TestEditServiceConfig).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestCreateClusterDataServiceNotFirst).WithTags(TagSuiteP0),
		framework.NewTestDef(TestRemoveLastDataService).WithTags(TagSuiteP0),
		framework.NewTestDef(TestSwapNodesBetweenServices).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestPodResourcesCannotBePlaced).WithTags(TagSuiteP0, TagFeatureScheduling),
		framework.NewTestDef(TestFirstNodePodResourcesCannotBePlaced).WithTags(TagSuiteP0, TagFeatureScheduling),
		framework.NewTestDef(TestRemoveServerClassWithNodeService).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestRecoveryAfterTwoPodFailureNoBucket).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterOnePodFailureBucketOneReplica).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterTwoPodFailureBucketOneReplica).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterOnePodFailureBucketTwoReplica).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestRecoveryAfterTwoPodFailureBucketTwoReplica).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestLightsOutPersistent).WithTags(TagSuiteP0, TagFeaturePersistentVolumes, TagFeatureRecovery),
		framework.NewTestDef(TestAutoRecoveryEphemeralWithNoAutofailover).WithTags(TagSuiteP0, TagFeatureRecovery),
		framework.NewTestDef(TestAntiAffinityOff).WithTags(TagSuiteP0, TagFeatureScheduling),
		framework.NewTestDef(TestAntiAffinityOnCannotBePlaced).WithTags(TagSuiteP0, TagFeatureScheduling),
		framework.NewTestDef(TestKillOperator).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestKillOperatorAndUpdateClusterConfig).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestImageVersionDefaultPrecedence).WithTags(TagSuiteP0, TagSuitePlatform),
		framework.NewTestDef(TestImageVersionEnvPrecedence).WithTags(TagSuiteP0, TagSuitePlatform),
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
		framework.NewTestDef(TestMultipleCAsAddAndRemove).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSEditSettings).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSCreateClusterWithShadowing).WithTags(TagSuiteP0, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestTLSWithMultipleServerCerts).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSScriptPassphrase).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateScriptPassphrase).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateAndChangeScriptPassphrase).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSRestPassphrase).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSRotateRestToScriptPassphrase).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSRestCreateRestRotate).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestTLSScriptCreateRestRotate).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestXDCRCreateClusterLocal).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterLocalTLS).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterLocalMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterRemoteTLS).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterRemoteMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRSourceNodeDown).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRSourceNodeAdd).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRTargetNodeServiceDelete).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRRotatePassword).WithTags(TagSuiteP0, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRReplicateLocalScopesAndCollections).WithTags(TagSuiteP0, TagFeatureXDCR, TagFeatureCollections, TagSuitePlatform),
		framework.NewTestDef(TestXDCRMigrationLocalScopesAndCollections).WithTags(TagSuiteP0, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRReplicateLocalScopesAndCollectionsImplicit).WithTags(TagSuiteP0, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestSyncGatewayCreateLocalTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateLocalMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayRBAC).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemote).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemoteTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemoteMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestRzaResizeCluster).WithTags(TagSuiteP0, TagFeatureServerGroups, TagSuitePlatform),
		framework.NewTestDef(TestServerGroupEnable).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupDisable).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupAddGroup).WithTags(TagSuiteP0, TagFeatureServerGroups, TagSuitePlatform),
		framework.NewTestDef(TestServerGroupRemoveGroup).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServerGroupReplaceGroup).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestGlobalServerGroupsAddsToPodNodeSelector).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestServersServerGroupsAddsToPodNodeSelector).WithTags(TagSuiteP0, TagFeatureServerGroups),
		framework.NewTestDef(TestPersistentVolumeAutoFailover).WithTags(TagSuiteP0, TagFeaturePersistentVolumes),
		framework.NewTestDef(TestPersistentVolumeAutoRecovery).WithTags(TagSuiteP0, TagSuitePlatform, TagFeaturePersistentVolumes, TagFeatureRecovery),
		framework.NewTestDef(TestPersistentVolumeKillAllPodsTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeaturePersistentVolumes, TagSuitePlatform),
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
		framework.NewTestDef(TestUpgradePersistent).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeSupportableKillStatefulPodOnCreate).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeSupportableKillStatefulPodOnRebalance).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeSupportableKillExistingStatefulPodOnRebalance).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeImmediate).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeConstrained).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeBucketDurability).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestUpgradeWithTLS).WithTags(TagSuiteP0, TagFeatureUpgrade),
		framework.NewTestDef(TestExposedFeatureIP).WithTags(TagSuiteP0, TagFeatureNetwork, TagSuitePlatform),
		framework.NewTestDef(TestExposedFeatureDNS).WithTags(TagSuiteP0, TagFeatureNetwork, TagSuitePlatform),
		framework.NewTestDef(TestExposedFeatureDNSModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestExposedFeatureServiceTypeModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceDNS).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceDNSModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceTypeModify).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceBootstrapingClient).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestConsoleServiceBootstrapingXDCR).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestExposedFeatureTrafficPolicyCluster).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestNetworkAddressFamily).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestLoadBalancerSourceRanges).WithTags(TagSuiteP0, TagFeatureNetwork),
		framework.NewTestDef(TestRBACDeleteUser).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC),
		framework.NewTestDef(TestRBACUpdateRole).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC),
		framework.NewTestDef(TestRBACDeleteRole).WithTags(TagSuiteP0, TagFeatureRBAC),
		framework.NewTestDef(TestLDAPCreateAdminUser).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLDAPDeleteUser).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLDAPDeleteRole).WithTags(TagSuiteP0, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLDAPUpdateRole).WithTags(TagSuiteP0, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestVerifyLDAPConfigRetention).WithTags(TagSuiteP0, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestVerifyLDAPManualManagement).WithTags(TagSuiteP0, TagFeatureRBAC, TagFeatureLDAP),
		framework.NewTestDef(TestLdapSettings).WithTags(TagSuiteP0, TagFeatureLDAP),
		framework.NewTestDef(TestBackupFullOnly).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestore).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullIncrementalOverTLS).WithTags(TagSuiteP0, TagFeatureTLS),
		framework.NewTestDef(TestBackupFullOnlyOverTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverTLSStandard).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverTLSKubernetes).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestBackupFullOnlyOverMutualTLS).WithTags(TagSuiteP0, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestMultipleBackups).WithTags(TagSuiteP0, TagFeatureBackup, TagSuitePlatform),
		framework.NewTestDef(TestBackupRetention).WithTags(TagSuiteP0, TagFeatureBackup, TagSuitePlatform),
		framework.NewTestDef(TestBackupPVCResize).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableEventing).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableGSI).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableAnalytics).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreDisableData).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreEnableBucketConfig).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreMapBuckets).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreIncludeBuckets).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreExcludeBuckets).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreNodeSelector).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestPrometheusMetricsTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsMutualTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsEnable).WithTags(TagSuiteP0, TagFeatureMetrics),
		framework.NewTestDef(TestPrometheusMetricsEnableTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsEnableMutualTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestPrometheusMetricsEnableShadowTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsEnableMutualShadowTLS).WithTags(TagSuiteP0, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsPerformOps).WithTags(TagSuiteP0, TagFeatureMetrics, TagSuitePlatform),
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
		framework.NewTestDef(TestCreateClusterWithTLSAndFullNodeToNode).WithTags(TagSuiteP0, TagFeatureTLS, TagSuitePlatform),
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
		framework.NewTestDef(TestScopeCreateExplicit).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureCollections),
		framework.NewTestDef(TestScopeCreateImplicit).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeCreateMixed).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeDelete).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureCollections),
		framework.NewTestDef(TestScopeUnmanaged).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureCollections),
		framework.NewTestDef(TestCollectionCreateExplicit).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureCollections),
		framework.NewTestDef(TestCollectionCreateImplicit).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionWithAnnotations).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionCreateMixed).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionDelete).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionUnmanaged).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestDefaultCollectionDeletion).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopesAndCollectionsSharedScopeTopology).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopesAndCollectionsSharedCollectionTopology).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopesAndCollectionsCascadingScopeDeletion).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestScopeOverflow).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestCollectionOverflow).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestViewsResizeCluster).WithTags(TagSuiteP0),
		framework.NewTestDef(TestViewsWithScopesAndCollections).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestFTSResizeCluster).WithTags(TagSuiteP0),
		framework.NewTestDef(TestFTSWithScopesAndCollections).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestAnalyticsCreateDataSetWithCollections).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestGSIWithCollections).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestUpdateCollection).WithTags(TagSuiteP0, TagFeatureCollections),
		framework.NewTestDef(TestBackupAndRestoreCollections).WithTags(TagSuiteP0, TagFeatureCollections, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreScopesAndCollections).WithTags(TagSuiteP0, TagSuitePlatform, TagFeatureCollections),
		framework.NewTestDef(TestBackupAndRestoreScope).WithTags(TagSuiteP0, TagFeatureCollections, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreCollection).WithTags(TagSuiteP0, TagFeatureCollections, TagFeatureBackup),
		framework.NewTestDef(TestBackupCustomObjEndpoint).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupCustomObjEndpointWithCert).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupLegacyCustomObjEndpoint).WithTags(TagSuiteP0, TagFeatureBackup),
		framework.NewTestDef(TestBackupLegacyCustomObjEndpointWithCert).WithTags(TagSuiteP0, TagFeatureBackup),

		// Old P0s now P1s
		framework.NewTestDef(TestAutoscaleUpMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureAutoScaling, TagSuitePlatform),
		framework.NewTestDef(TestAutoscaleDownMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureAutoScaling),
		framework.NewTestDef(TestPrometheusMetricsEnableMandatoryMutualShadowTLS).WithTags(TagSuiteP1, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsEnableMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestPrometheusMetricsMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureMetrics, TagFeatureTLS),
		framework.NewTestDef(TestBackupFullOnlyOverMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup),
		framework.NewTestDef(TestSyncGatewayCreateRemoteMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateRemoteMandatoryMutualTLSWithMultipleCAs).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestSyncGatewayCreateLocalMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureSyncGateway),
		framework.NewTestDef(TestXDCRCreateClusterRemoteMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRCreateClusterLocalMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestMandatoryMutualTLSRotateClientExpiring).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateCAExpiring).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSWithMultipleCAsAndRotateServerPKI).WithTags(TagSuiteP1, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestMandatoryMutualTLSWithMultipleCAsAndRotateServerPKIWithOperatorDown).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSWithMultipleCAsAndRotateClientPKI).WithTags(TagSuiteP1, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestMandatoryMutualTLSWithMultipleCAsAndRotateClientPKIWithOperatorDown).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSCreateCluster).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSEnable).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSDisable).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateClient).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateClientChain).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateCA).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSRotateInvalid).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSWithMultipleCAsAndKubernetesSecrets).WithTags(TagSuiteP1, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestMandatoryMutualTLSWithSingleSecretMultipleCAsAndKubernetesSecrets).WithTags(TagSuiteP1, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestMandatoryMutualTLSWithMultipleCAsAndCertManagerSecrets).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestMandatoryMutualTLSWithSingleSecretMultipleCAsAndCertManagerSecrets).WithTags(TagSuiteP1, TagFeatureTLS, TagSuitePlatform),
		framework.NewTestDef(TestBackupAndRestoreCollectionS3).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreCollectionAzure).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreCollectionGCP).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreScopeS3).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreScopeAzure).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreScopeGCP).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreScopesAndCollectionsS3).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreScopesAndCollectionsAzure).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreScopesAndCollectionsGCP).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreCollectionsS3).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreCollectionsAzure).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreCollectionsGCP).WithTags(TagSuiteP1, TagFeatureCollections, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreNodeSelectorS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreNodeSelectorAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreNodeSelectorGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreExcludeBucketsS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreExcludeBucketsAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreExcludeBucketsGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreIncludeBucketsS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreIncludeBucketsAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreIncludeBucketsGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreMapBucketsS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreMapBucketsAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreMapBucketsGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreEnableBucketConfigS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreEnableBucketConfigAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreEnableBucketConfigGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableDataS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableDataAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableDataGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableAnalyticsS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableAnalyticsAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableAnalyticsGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableGSIS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableGSIAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableGSIGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupPVCResizeS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupPVCResizeGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupPVCResizeAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupRetentionS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupRetentionAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupRetentionGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestMultipleBackupsS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestMultipleBackupsAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestMultipleBackupsGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyOverTLSS3).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyOverTLSAzure).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyOverTLSGCP).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyOverTLSS3Standard).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyOverTLSAzureStandard).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyOverTLSGCPStandard).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreS3).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreAzure).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreGCP).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableEventingS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableEventingAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreDisableEventingGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullOnlyGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullIncrementalOverTLSS3).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullIncrementalOverTLSAzure).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupFullIncrementalOverTLSGCP).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestCAODacValidation).WithTags(TagSuiteP1, TagFeatureAdmission),
		framework.NewTestDef(TestCAODacValidationDisabled).WithTags(TagSuiteP1, TagFeatureAdmission),
		framework.NewTestDef(TestCAOValidationUnreconcilable).WithTags(TagSuiteP1, TagFeatureAdmission),
		framework.NewTestDef(TestDisableAllValidation).WithTags(TagSuiteP1, TagFeatureAdmission),

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
		framework.NewTestDef(TestAntiAffinityOnCannotBeScaled).WithTags(TagSuiteP1, TagFeatureScheduling),
		framework.NewTestDef(TestBucketAddRemoveExtended).WithTags(TagSuiteP1),
		framework.NewTestDef(TestRevertExternalBucketUpdates).WithTags(TagSuiteP1),
		framework.NewTestDef(TestPauseOperator).WithTags(TagSuiteP1),
		framework.NewTestDef(TestServicesRunningAfterFailover).WithTags(TagSuiteP1),
		framework.NewTestDef(TestNegPodResourcesBasic).WithTags(TagSuiteP1, TagFeatureScheduling),
		framework.NewTestDef(TestTLSRemoveOperatorCertificateAndAddBack).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureTLS),
		framework.NewTestDef(TestTLSRemoveClusterCertificateAndAddBack).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRemoveOperatorCertificateAndResizeCluster).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSRemoveClusterCertificateAndResizeCluster).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSNegRSACertificateDnsName).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSNegCertificateExpiredBeforeDeployment).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSCertificateDeployedBeforeValidity).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSGenerateWrongCACertType).WithTags(TagSuiteP1, TagFeatureTLS),
		framework.NewTestDef(TestTLSGenerateWrongCertType).WithTags(TagSuiteP1, TagFeatureTLS),
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
		framework.NewTestDef(TestXDCRRotateCAMandatoryMutualTLS).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR),
		framework.NewTestDef(TestXDCRReplicateLocalScopesAndCollectionsWithDeny).WithTags(TagSuiteP1, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRReplicateLocalScopesAndCollectionsMultipleRules).WithTags(TagSuiteP1, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRReplicateLocalScopesAndCollectionsReuseSpec).WithTags(TagSuiteP1, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRReplicateLocalScopesAndCollectionsToUnmanaged).WithTags(TagSuiteP1, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRMigrationLocalScopesAndCollectionsMultipleRules).WithTags(TagSuiteP1, TagFeatureXDCR, TagFeatureCollections),
		framework.NewTestDef(TestXDCRWithMandatoryTLSAndMultipleCAs).WithTags(TagSuiteP1, TagFeatureTLS, TagFeatureXDCR),
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
		framework.NewTestDef(TestLogCollectWithClusterResizeAndOperatorPodKilled).WithTags(TagSuiteP1, TagFeatureSupportability),
		framework.NewTestDef(TestLogRetentionMultiCluster).WithTags(TagSuiteP1, TagFeatureSupportability),
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
		framework.NewTestDef(TestLoggingMemoryBufferLimits).WithTags(TagSuiteP1, TagFeatureLogging),
		framework.NewTestDef(TestPodDeleteDelayRespected).WithTags(TagSuiteP1, TagSuiteSystem, TagFeatureDeleteDelay),
		framework.NewTestDef(TestNoPodDeleteDelayRespected).WithTags(TagSuiteP1, TagSuiteSystem, TagFeatureDeleteDelay),
		framework.NewTestDef(TestPodDeletedAfterExpectedDelay).WithTags(TagSuiteP1, TagSuiteSystem, TagFeatureDeleteDelay),
		framework.NewTestDef(TestAnalyticsKillPods).WithTags(TagSuiteP1),
		framework.NewTestDef(TestAnalyticsKillPodsWithPVC).WithTags(TagSuiteP1),
		framework.NewTestDef(TestMagmaBucketToCouchstoreMigration).WithTags(TagSuiteP1, TagFeatureUpgrade, TagFeatureBucketMigration),
		framework.NewTestDef(TestMultipleCouchstoreBucketsToMagmaMigration).WithTags(TagSuiteP1, TagFeatureUpgrade, TagFeatureBucketMigration),
		framework.NewTestDef(TestCouchstoreBucketToMagmaMigrationUnmanagedBucket).WithTags(TagSuiteP1, TagFeatureUpgrade, TagFeatureBucketMigration),
		framework.NewTestDef(TestCouchstoreBucketToCouchstoreMigrationFromDefault).WithTags(TagSuiteP1, TagFeatureUpgrade, TagFeatureBucketMigration),
		framework.NewTestDef(TestCouchstoreBucketToMagmaUpdateUnmanagedBucket).WithTags(TagSuiteP1, TagFeatureUpgrade, TagFeatureBucketMigration),
		framework.NewTestDef(TestCouchstoreBucketsToMagmaMigrationWithMultiMigration).WithTags(TagSuiteP1, TagFeatureUpgrade, TagFeatureBucketMigration),
		framework.NewTestDef(TestPodDisruptionBudgets).WithTags(TagSuiteP1),

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
		framework.NewTestDef(TestFailedBackupBehaviourS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestFailedBackupBehaviourAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestFailedBackupBehaviourGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupPVCReconcile).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupPVCReconcileS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupPVCReconcileAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupPVCReconcileGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestReplaceFullOnlyBackup).WithTags(TagSuiteP1, TagSuitePlatform, TagFeatureBackup),
		framework.NewTestDef(TestReplaceFullOnlyBackupS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestReplaceFullOnlyBackupAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestReplaceFullOnlyBackupGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestReplaceFullIncrementalBackup).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestReplaceFullIncrementalBackupS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestReplaceFullIncrementalBackupAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestReplaceFullIncrementalBackupGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestUpdateBackupStatus).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestUpdateBackupStatusS3).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestUpdateBackupStatusAzure).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestUpdateBackupStatusGCP).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
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
		framework.NewTestDef(TestAutoscaleDown).WithTags(TagSuiteP1, TagFeatureAutoScaling),
		framework.NewTestDef(TestAutoscaleMultiConfigs).WithTags(TagSuiteP1, TagFeatureAutoScaling),
		framework.NewTestDef(TestScheduleEvacuateAllPersistent).WithTags(TagSuiteP1, TagFeaturePersistentVolumes, TagFeatureScheduling),
		framework.NewTestDef(TestScheduleCleanupUninitializedPod).WithTags(TagSuiteP1),
		framework.NewTestDef(TestCustomAnnotationsAndLabelsStayAfterReconcile).WithTags(TagSuiteP1, TagFeatureReconcile, TagSuitePlatform),
		framework.NewTestDef(TestBackupBucketInclusion).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupBucketExclusion).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupThenDelete).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreS3WithIAMRole).WithTags(TagSuiteP1, TagFeatureBackup, TagSuitePlatform, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndForcedRestore).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreServices).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreToSubPath).WithTags(TagSuiteP1, TagFeatureBackup),
		framework.NewTestDef(TestBackupAndRestoreEphemeralVolume).WithTags(TagSuiteP1, TagFeatureBackup, TagFeatureBackupCloud),
		framework.NewTestDef(TestBackupAndRestoreUsers).WithTags(TagSuiteP1, TagFeatureBackup),

		// Synchronization tests.
		framework.NewTestDef(TestDataSynchronizationBasic).WithTags(TagSuiteP1, TagFeatureSynchronization, TagFeatureCollections, TagSuitePlatform),
		framework.NewTestDef(TestDataSynchronizationUpdate).WithTags(TagSuiteP1, TagFeatureSynchronization, TagFeatureCollections),
		framework.NewTestDef(TestDataSynchronizationCouchbaseBucketConfig).WithTags(TagSuiteP1, TagFeatureSynchronization, TagSuitePlatform),
		framework.NewTestDef(TestDataSynchronizationEphemeralBucketConfig).WithTags(TagSuiteP1, TagFeatureSynchronization),
		framework.NewTestDef(TestDataSynchronizationMemcachedBucketConfig).WithTags(TagSuiteP1, TagFeatureSynchronization),
		framework.NewTestDef(TestDataSynchronizationDefaultCollectionDeleted).WithTags(TagSuiteP1, TagFeatureSynchronization, TagFeatureCollections),
		framework.NewTestDef(TestDataSynchronizationErrorTopologyChange).WithTags(TagSuiteP1, TagFeatureSynchronization),
		framework.NewTestDef(TestDataSynchronizationOperatorRestart).WithTags(TagSuiteP1, TagFeatureSynchronization),

		// System tests.
		framework.NewTestDef(TestFeaturesAll).WithTags(TagSuiteSystem),

		// Local Persistent Volume tests
		framework.NewTestDef(TestLocalVolumeMountReuse).WithTags(TagFeatureLPV),
		framework.NewTestDef(TestMixedVolumeMountReuse).WithTags(TagFeatureLPV),
		framework.NewTestDef(TestLocalVolumeAutoFailover).WithTags(TagFeatureLPV),

		// Graceful Failover test
		framework.NewTestDef(TestGracefulShutdown).WithTags(TagSuiteP1),

		// Migration tests
		framework.NewTestDef(TestMigrateCluster).WithTags(TagSuiteP1, TagFeatureAssimilation),
		framework.NewTestDef(TestMigrateLeaveUnmanagedCluster).WithTags(TagSuiteP1, TagFeatureAssimilation),
		framework.NewTestDef(TestPremigrationNodes).WithTags(TagSuiteP1, TagFeatureAssimilation),
		framework.NewTestDef(TestStabilizationPeriod).WithTags(TagSuiteP1, TagFeatureAssimilation),
		framework.NewTestDef(TestMaxConcurrency).WithTags(TagSuiteP1, TagFeatureAssimilation),

		// Bucket auto-compaction settings tests
		framework.NewTestDef(TestCreateEditDeleteCouchbaseBucketAutoCompactionSettings).WithTags(TagSuiteSanity),
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

		return v.Validate(out)
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
