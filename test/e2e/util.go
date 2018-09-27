package e2e

import (
	"errors"
	"os"
	"testing"

	pkg_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type GroupSetupFunction map[string]func(*testing.T, []framework.ClusterInfo) error

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
		"TestPersistentVolumeKillAllPods":            TestPersistentVolumeKillAllPods,
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
		"TestLogCollectClusterWithPVC":               TestLogCollectClusterWithPVC,
	}

	DecoratorFuncMap = framework.DecoratorMap{
		"rsaDecorator":     rsaDecorator,
		"rzaNodeLabeller":  rzaNodeLabeller,
		"recoverDecorator": framework.RecoverDecorator,
	}

	TestGroupSetupFuncMap = GroupSetupFunction{
		"AddServerGroupLabelToNodes":      AddServerGroupLabelToNodes,
		"RemoveServerGroupLabelFromNodes": RemoveServerGroupLabelFromNodes,
	}
)

func ValidateEvents(t *testing.T, kubeClient kubernetes.Interface, namespace, cbClusterName string, events e2eutil.EventValidator) {
	clusterEvents, err := e2eutil.GetCouchbaseEvents(kubeClient, cbClusterName, namespace)
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

func ValidateClusterEvents(t *testing.T, kubeClient kubernetes.Interface, clusterName, namespace string, expectedEvents e2eutil.EventList) {
	events, err := e2eutil.GetCouchbaseEvents(kubeClient, clusterName, namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func ValidateClusterEventsWithoutError(t *testing.T, kubeClient kubernetes.Interface, clusterName, namespace string, expectedEvents e2eutil.EventList) {
	events, err := e2eutil.GetCouchbaseEvents(kubeClient, clusterName, namespace)
	if err != nil {
		t.Logf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Logf(e2eutil.EventListCompareFailedString(expectedEvents, events))
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

// Function to Label the K8S nodes with server group
func AddServerGroupLabelToNodes(t *testing.T, clusterInfoList []framework.ClusterInfo) (err error) {
	f := framework.Global

	for _, clusterInfo := range clusterInfoList {
		kubeName := clusterInfo.ClusterName
		targetKube := f.ClusterSpec[kubeName]

		k8sNodesData, err := framework.GetClusterConfigFromYml(f.ClusterConfFile, f.KubeType, []string{kubeName})
		if err != nil {
			return errors.New("Failed to read cluster yaml data: " + err.Error())
		}

		for retryCount := 0; retryCount < constants.Retries5; retryCount++ {
			t.Logf("Update node label count: %d", retryCount)
			// Label K8S nodes based on the labels present in the cluster conf yaml file
			if err = K8SNodesAddLabel(pkg_constants.ServerGroupLabel, targetKube.KubeClient, k8sNodesData[0]); err == nil {
				break
			}
		}
	}
	return err
}

// Function to remove the server group specific labels from K8S nodes
func RemoveServerGroupLabelFromNodes(t *testing.T, clusterInfoList []framework.ClusterInfo) (err error) {
	f := framework.Global
	for _, clusterInfo := range clusterInfoList {
		kubeName := clusterInfo.ClusterName
		targetKube := f.ClusterSpec[kubeName]
		for retryCount := 0; retryCount < constants.Retries5; retryCount++ {
			t.Logf("Update node label count: %d", retryCount)
			if err = K8SNodesRemoveLabel(pkg_constants.ServerGroupLabel, targetKube.KubeClient); err == nil {
				break
			}
		}
	}
	return err
}
