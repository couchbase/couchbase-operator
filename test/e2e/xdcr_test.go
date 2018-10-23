package e2e

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Generic function to run rebalance out test case
// Rebalance out xdcrCluster nodes one by one for the provided clustersize
func rebalanceOutXdcrNodes(t *testing.T, cbCluster *api.CouchbaseCluster, clusterSize int, kubeName string, expectedEvents *e2eutil.EventValidator) error {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]
	nextNodeToBeAdded := clusterSize

	client, err := e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName), f.Namespace, f.PlatformType, targetKube.KubeClient, cbCluster)
	if err != nil {
		return errors.New("Failed to create cluster client: " + err.Error())
	}

	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		// Create node client
		clusterNodeName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		t.Logf("Rebalance-out %s", clusterNodeName)

		if err := e2eutil.RebalanceOutMember(t, client, cbCluster.Name, f.Namespace, memberIndex, true); err != nil {
			return errors.New("Rebalance-out failed: " + err.Error())
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIndex)

		event := e2eutil.NewMemberAddEvent(cbCluster, nextNodeToBeAdded)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 120); err != nil {
			return errors.New("Failed to add new member add: " + err.Error())
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", nextNodeToBeAdded)

		event = e2eutil.RebalanceStartedEvent(cbCluster)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 60); err != nil {
			return errors.New("Failed to start rebalance: " + err.Error())
		}

		event = e2eutil.RebalanceCompletedEvent(cbCluster)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
			return errors.New("Failed to rebalance: " + err.Error())
		}
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, cbCluster.Name, f.Namespace, clusterSize, constants.Retries5); err != nil {
			return errors.New("Cluster unhealthy: " + err.Error())
		}
		nextNodeToBeAdded++
	}
	return nil
}

// Generic function to kill xdcrCluster nodes
// Will kill all the nodes one by one for the given clusterSize number and wait for the new pod to get replaced
func killXdcrNodes(t *testing.T, cbCluster *api.CouchbaseCluster, clusterSize int, kubeName string, expectedEvents *e2eutil.EventValidator) error {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]
	nextNodeToBeAdded := clusterSize
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		memberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, memberName, f.Namespace); err != nil {
			return err
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", memberIndex)

		event := e2eutil.NewMemberFailedOverEvent(cbCluster, memberIndex)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 90); err != nil {
			return err
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", memberIndex)

		event = e2eutil.NewMemberAddEvent(cbCluster, nextNodeToBeAdded)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 180); err != nil {
			return err
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", nextNodeToBeAdded)

		event = e2eutil.RebalanceCompletedEvent(cbCluster)
		if err := e2eutil.WaitForClusterEvent(targetKube.KubeClient, cbCluster, event, 300); err != nil {
			return errors.New("Failed to rebalance: " + err.Error())
		}
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIndex)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, cbCluster.Name, f.Namespace, clusterSize, constants.Retries5); err != nil {
			return err
		}
		nextNodeToBeAdded++
	}
	return nil
}

// Generic function to resize the xdcrCluster to the given clusterSize value and wait for healthy cluster
func resizeXdcrCluster(t *testing.T, cbCluster *api.CouchbaseCluster, clusterSize int, kubeName string) error {
	f := framework.Global
	service := 0
	targetKube := f.ClusterSpec[kubeName]
	if err := e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, cbCluster); err != nil {
		return err
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, cbCluster.Name, f.Namespace, clusterSize, constants.Retries10); err != nil {
		return err
	}
	return nil
}

// Generic function to run all pod removal/resize tests
// Remove nodes using diff ways based on operationType value
// This will in-turn call rebalanceOutXdcrNodes / killXdcrNodes / resizeXdcrCluster function appropiately
// targetClusterNodes will be source / destination to decide target cluster from xdcrCluster1 / xdccrCluster2
func XdcrClusterRemoveNode(t *testing.T, kubeNameList []string, targetClusterNodes, operationType string) {
	f := framework.Global
	xdcr1KubeName := kubeNameList[0]
	xdcr2KubeName := kubeNameList[1]
	xdcr1Kube := f.ClusterSpec[xdcr1KubeName]
	xdcr2Kube := f.ClusterSpec[xdcr2KubeName]

	var xdcrCluster1 *api.CouchbaseCluster
	errChan := make(chan error)
	clusterSize := constants.Size3

	go func() {
		// Cluster 1
		var err error
		xdcrCluster1, err = e2eutil.NewXdcrClusterBasic(t, xdcr1Kube.KubeClient, xdcr1Kube.CRClient, f.Namespace, xdcr1Kube.DefaultSecret.Name, clusterSize, constants.WithBucket, constants.AdminExposed)
		errChan <- err
	}()

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcr2Kube.KubeClient, xdcr2Kube.CRClient, f.Namespace, xdcr2Kube.DefaultSecret.Name, clusterSize, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	if err := <-errChan; err != nil {
		t.Fatal(err)
	}

	expectedCluster1Events := e2eutil.EventValidator{}
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", memberIndex)
	}
	expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
	expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

	expectedCluster2Events := e2eutil.EventValidator{}
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "AddNewMember", memberIndex)
	}
	expectedCluster2Events.AddClusterNodeServiceEvent(xdcrCluster2, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceStarted")
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceCompleted")
	expectedCluster2Events.AddClusterBucketEvent(xdcrCluster2, "Create", "default")

	//xdcr1KubeHost, err := f.GetKubeHostname(xdcr1KubeName)
	//if err != nil {
	//	t.Fatal(err)
	//}
	//xdcr2KubeHost, err := f.GetKubeHostname(xdcr2KubeName)
	//if err != nil {
	//	t.Fatal(err)
	//}
	//hostUrl := xdcr1KubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	//destUrl := xdcr2KubeHost + ":" + xdcrCluster2.Status.AdminConsolePort

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(xdcr1KubeName), f.Namespace, f.PlatformType, xdcr1Kube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(xdcr2KubeName), f.Namespace, f.PlatformType, xdcr2Kube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	xdcr1KubeHost, err := f.GetKubeHostname(xdcr1KubeName)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr1KubeHost, f.Namespace, f.PlatformType, xdcr1Kube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatal(err)
	}

	xdcr2KubeHost, err := f.GetKubeHostname(xdcr2KubeName)
	if err != nil {
		t.Fatal(err)
	}

	destUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr2KubeHost, f.Namespace, f.PlatformType, xdcr2Kube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatal(err)
	}

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err := e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	switch operationType {
	case "rebalanceOutNodes":
		if targetClusterNodes == "source" {
			if err := rebalanceOutXdcrNodes(t, xdcrCluster1, clusterSize, xdcr1KubeName, &expectedCluster1Events); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := rebalanceOutXdcrNodes(t, xdcrCluster2, clusterSize, xdcr2KubeName, &expectedCluster2Events); err != nil {
				t.Fatal(err)
			}
		}
	case "killNodes":
		if targetClusterNodes == "source" {
			if err := killXdcrNodes(t, xdcrCluster1, clusterSize, xdcr1KubeName, &expectedCluster1Events); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := killXdcrNodes(t, xdcrCluster2, clusterSize, xdcr2KubeName, &expectedCluster2Events); err != nil {
				t.Fatal(err)
			}
		}
	case "resizeOut":
		if targetClusterNodes == "source" {
			if err := resizeXdcrCluster(t, xdcrCluster1, constants.Size1, xdcr1KubeName); err != nil {
				t.Fatal(err)
			}
			expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", 1)
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", 2)
			expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
		} else {
			if err := resizeXdcrCluster(t, xdcrCluster2, constants.Size1, xdcr2KubeName); err != nil {
				t.Fatal(err)
			}
			expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceStarted")
			expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "MemberRemoved", 1)
			expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "MemberRemoved", 2)
			expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceCompleted")
		}
	default:
		t.Fatalf("Unsupported operation: %s", operationType)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	// Sleep to resume xdcr replication after cluster resize
	time.Sleep(5 * time.Minute)

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, constants.Retries60); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, xdcr1Kube.KubeClient, f.Namespace, xdcrCluster1.Name, expectedCluster1Events)
	ValidateEvents(t, xdcr2Kube.KubeClient, f.Namespace, xdcrCluster2.Name, expectedCluster2Events)
}

// Generic test case for creating Xdcr clusters
// Can support 2 clusters within same K8S kube or
// different kube based on the kubeNames provided in kubeNameList
func CreateXdcrCluster(t *testing.T, kubeNameList []string) {
	f := framework.Global
	xdcr1KubeName := kubeNameList[0]
	xdcr2KubeName := kubeNameList[1]
	xdcr1Kube := f.ClusterSpec[xdcr1KubeName]
	xdcr2Kube := f.ClusterSpec[xdcr2KubeName]

	var xdcrCluster1 *api.CouchbaseCluster
	errChan := make(chan error)
	clusterSize := constants.Size3

	go func() {
		var err error
		// Cluster 1
		xdcrCluster1, err = e2eutil.NewXdcrClusterBasic(t, xdcr1Kube.KubeClient, xdcr1Kube.CRClient, f.Namespace, xdcr1Kube.DefaultSecret.Name, clusterSize, constants.WithBucket, constants.AdminExposed)
		errChan <- err
	}()

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcr2Kube.KubeClient, xdcr2Kube.CRClient, f.Namespace, xdcr2Kube.DefaultSecret.Name, clusterSize, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	if err := <-errChan; err != nil {
		t.Fatal(err)
	}

	expectedCluster1Events := e2eutil.EventValidator{}
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", memberIndex)
	}
	expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
	expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

	expectedCluster2Events := e2eutil.EventValidator{}
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "AddNewMember", memberIndex)
	}
	expectedCluster2Events.AddClusterNodeServiceEvent(xdcrCluster2, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceStarted")
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceCompleted")
	expectedCluster2Events.AddClusterBucketEvent(xdcrCluster2, "Create", "default")

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(xdcr1KubeName), f.Namespace, f.PlatformType, xdcr1Kube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(xdcr2KubeName), f.Namespace, f.PlatformType, xdcr2Kube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	xdcr1KubeHost, err := f.GetKubeHostname(xdcr1KubeName)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr1KubeHost, f.Namespace, f.PlatformType, xdcr1Kube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatal(err)
	}

	xdcr2KubeHost, err := f.GetKubeHostname(xdcr2KubeName)
	if err != nil {
		t.Fatal(err)
	}

	destUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr2KubeHost, f.Namespace, f.PlatformType, xdcr2Kube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatal(err)
	}

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err := e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, xdcr1Kube.KubeClient, f.Namespace, xdcrCluster1.Name, expectedCluster1Events)
	ValidateEvents(t, xdcr2Kube.KubeClient, f.Namespace, xdcrCluster2.Name, expectedCluster2Events)
}

// Generic testcase to run NodeDown test cases on kubeNames given by kubeNameList
// This can support bringing down a cluster node either during XDCR configration / post XDCR configuration
func ClusterNodeDownWithXdcr(t *testing.T, triggerDuring string, kubeNameList []string) {
	f := framework.Global
	defKubeName := kubeNameList[0]
	defKube := f.ClusterSpec[defKubeName]

	xdcrKubeName := kubeNameList[1]
	xdcrKube := f.ClusterSpec[xdcrKubeName]

	xdcrCluster1Size := constants.Size5
	xdcrCluster2Size := constants.Size2

	var xdcrCluster1 *api.CouchbaseCluster
	errChan := make(chan error)

	go func() {
		var err error
		// Cluster 1
		xdcrCluster1, err = e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, xdcrCluster1Size, constants.WithBucket, constants.AdminExposed)
		errChan <- err
	}()

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, xdcrCluster2Size, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	if err := <-errChan; err != nil {
		t.Fatal(err)
	}

	expectedCluster1Events := e2eutil.EventValidator{}
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < xdcrCluster1Size; memberIndex++ {
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", memberIndex)
	}
	expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
	expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

	expectedCluster2Events := e2eutil.EventValidator{}
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < xdcrCluster2Size; memberIndex++ {
		expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "AddNewMember", memberIndex)
	}
	expectedCluster2Events.AddClusterNodeServiceEvent(xdcrCluster2, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceStarted")
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceCompleted")
	expectedCluster2Events.AddClusterBucketEvent(xdcrCluster2, "Create", "default")

	//defKubeHost, err := f.GetKubeHostname(defKubeName)
	//if err != nil {
	//	t.Fatal(err)
	//}

	//xdcrKubeHost, err := f.GetKubeHostname(xdcrKubeName)
	//if err != nil {
	//	t.Fatal(err)
	//}

	//hostUrl := defKubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	//destUrl := xdcrKubeHost + ":" + xdcrCluster2.Status.AdminConsolePort

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(defKubeName), f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(xdcrKubeName), f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	xdcr1KubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr1KubeHost, f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatal(err)
	}

	xdcr2KubeHost, err := f.GetKubeHostname(xdcrKubeName)
	if err != nil {
		t.Fatal(err)
	}

	destUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr2KubeHost, f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatal(err)
	}

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err := e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	errChan = make(chan error)
	nodeDownFunc := func(nodeIndex int) {
		// Kill first Pod of cluster-1
		if err := e2eutil.KillPodForMember(defKube.KubeClient, xdcrCluster1, nodeIndex); err != nil {
			errChan <- err
		}
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberDown", nodeIndex)

		event := e2eutil.NewMemberFailedOverEvent(xdcrCluster1, nodeIndex)
		if err := e2eutil.WaitForClusterEvent(defKube.KubeClient, xdcrCluster1, event, 90); err != nil {
			errChan <- err
		}
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "FailedOver", nodeIndex)

		event = e2eutil.NewMemberAddEvent(xdcrCluster1, 5)
		if err := e2eutil.WaitForClusterEvent(defKube.KubeClient, xdcrCluster1, event, 120); err != nil {
			errChan <- err
		}
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", 5)
		errChan <- nil
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	nodeToKill := 1
	if triggerDuring == "duringXdcrSetup" {
		go nodeDownFunc(nodeToKill)
	}

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, constants.Retries60); err != nil {
		t.Fatal(err)
	}

	if triggerDuring == "afterXdcrSetup" {
		go nodeDownFunc(nodeToKill)
	}

	if err := <-errChan; err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	// Sleep to resume xdcr replication after cluster resize
	time.Sleep(5 * time.Minute)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", nodeToKill)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, defKube.KubeClient, f.Namespace, xdcrCluster1.Name, expectedCluster1Events)
	ValidateEvents(t, xdcrKube.KubeClient, f.Namespace, xdcrCluster2.Name, expectedCluster2Events)
}

// Generic testcase to run AddNode test cases on kubeNames given by kubeNameList
// This can support adding new node to the cluster either during XDCR configration / post XDCR configuration
func ClusterAddNodeWithXdcr(t *testing.T, triggerDuring string, kubeNameList []string) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	defKubeName := kubeNameList[0]
	defKube := f.ClusterSpec[defKubeName]

	xdcrKubeName := kubeNameList[1]
	xdcrKube := f.ClusterSpec[xdcrKubeName]

	var xdcrCluster1 *api.CouchbaseCluster
	errChan := make(chan error)

	go func() {
		var err error
		// Cluster 1
		xdcrCluster1, err = e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminExposed)
		errChan <- err
	}()

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	if err := <-errChan; err != nil {
		t.Fatal(err)
	}

	expectedCluster1Events := e2eutil.EventValidator{}
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", 0)
	expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

	expectedCluster2Events := e2eutil.EventValidator{}
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "AdminConsoleServiceCreate")
	expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "AddNewMember", 0)
	expectedCluster2Events.AddClusterNodeServiceEvent(xdcrCluster2, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster2Events.AddClusterBucketEvent(xdcrCluster2, "Create", "default")

	errChan = make(chan error)
	resizeFunction := func() {
		service := 0
		clusterSize := constants.Size3
		if err := e2eutil.ResizeCluster(t, service, clusterSize, defKube.CRClient, xdcrCluster1); err != nil {
			errChan <- err
			return
		}

		if err := e2eutil.WaitClusterStatusHealthy(t, defKube.CRClient, xdcrCluster1.Name, f.Namespace, clusterSize, constants.Retries10); err != nil {
			errChan <- err
			return
		}

		for memberIndex := 1; memberIndex < clusterSize; memberIndex++ {
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", memberIndex)
		}
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
		errChan <- nil
	}

	if triggerDuring == "duringXdcrSetup" {
		go resizeFunction()
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(defKubeName), f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(xdcrKubeName), f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	xdcr1KubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr1KubeHost, f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatal(err)
	}

	xdcr2KubeHost, err := f.GetKubeHostname(xdcrKubeName)
	if err != nil {
		t.Fatal(err)
	}

	destUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr2KubeHost, f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatal(err)
	}
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err := e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	if triggerDuring == "afterXdcrSetup" {
		go resizeFunction()
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Failed to resize cluster: %s", err.Error())
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	// Sleep to resume xdcr replication after cluster resize
	time.Sleep(5 * time.Minute)

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, constants.Retries30); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, defKube.KubeClient, f.Namespace, xdcrCluster1.Name, expectedCluster1Events)
	ValidateEvents(t, xdcrKube.KubeClient, f.Namespace, xdcrCluster2.Name, expectedCluster2Events)
}

// Generic testcase to kill the XDCR exposed service test cases on kubeNames given by kubeNameList
// This supports killing the xdcr port service either during XDCR configration / post XDCR configuration
func ClusterNodeXdcrServiceKill(t *testing.T, triggerDuring string, kubeNameList []string) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	defKubeName := kubeNameList[0]
	defKube := f.ClusterSpec[defKubeName]

	xdcrKubeName := kubeNameList[1]
	xdcrKube := f.ClusterSpec[xdcrKubeName]

	var xdcrCluster1 *api.CouchbaseCluster
	errChan := make(chan error)

	go func() {
		var err error
		// Cluster 1
		xdcrCluster1, err = e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminExposed)
		errChan <- err
	}()

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	if err := <-errChan; err != nil {
		t.Fatal(err)
	}

	expectedCluster1Events := e2eutil.EventValidator{}
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", 0)
	expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

	expectedCluster2Events := e2eutil.EventValidator{}
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "AdminConsoleServiceCreate")
	expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "AddNewMember", 0)
	expectedCluster2Events.AddClusterNodeServiceEvent(xdcrCluster2, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster2Events.AddClusterBucketEvent(xdcrCluster2, "Create", "default")

	errChan = make(chan error)
	serviceKillFunc := func() {
		services, err := defKube.KubeClient.CoreV1().Services(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseServerPodLabelStr + xdcrCluster1.Name})
		if err != nil {
			errChan <- err
			return
		}
		for _, service := range services.Items {
			if strings.HasSuffix(service.Name, "-exposed-ports") {
				if err := e2eutil.DeleteService(defKube.KubeClient, f.Namespace, service.Name, metav1.NewDeleteOptions(0)); err != nil {
					errChan <- err
					return
				}
			}
		}
		errChan <- nil
	}

	if triggerDuring == "duringXdcrSetup" {
		go serviceKillFunc()
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(defKubeName), f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(xdcrKubeName), f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	xdcr1KubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr1KubeHost, f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatal(err)
	}

	xdcr2KubeHost, err := f.GetKubeHostname(xdcrKubeName)
	if err != nil {
		t.Fatal(err)
	}

	destUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr2KubeHost, f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatal(err)
	}
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err := e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	if triggerDuring == "afterXdcrSetup" {
		go serviceKillFunc()
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Failed to remove Xdcr services: %s", err.Error())
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, defKube.KubeClient, f.Namespace, xdcrCluster1.Name, expectedCluster1Events)
	ValidateEvents(t, xdcrKube.KubeClient, f.Namespace, xdcrCluster2.Name, expectedCluster2Events)
}

// Create XDCR cluster within same k8s cluster
func TestXdcrCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	// Create Intra XDCR clusters
	CreateXdcrCluster(t, []string{"BasicCluster", "BasicCluster"})
}

// Create XDCR cluster using two distinct k8s clusters
func TestXdcrCreateInterCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	CreateXdcrCluster(t, []string{"BasicCluster", "NewCluster1"})
}

// Create cb clusters on top of TLS certificates
func TestXdcrCreateTlsCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	kubeName1 := "BasicCluster"
	kubeName2 := "NewCluster1"
	defKube := f.ClusterSpec[kubeName1]
	xdcrKube := f.ClusterSpec[kubeName2]

	tlsContexts := map[string]*e2eutil.TlsContext{}

	// Create secrets in all k8s clusters
	// TODO: This test is pointless unless you are going to specify the encrytionType=full parameter in
	// the remote cluster definition.  Additionally as the clusters are in separate kubernetes clusters
	// then TLS will not work as you'll be connecting via IP address and not DNS.
	for _, kubeName := range []string{kubeName1, kubeName2} {
		ctx, teardown, err := e2eutil.InitClusterTLS(f.ClusterSpec[kubeName].KubeClient, f.Namespace, &e2eutil.TlsOpts{})
		if err != nil {
			t.Fatal(err)
		}
		defer teardown()
		tlsContexts[kubeName] = ctx
	}

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewTlsXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminExposed, tlsContexts[kubeName1])
	if err != nil {
		t.Fatal(err)
	}

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewTlsXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, constants.Size1, constants.WithBucket, constants.AdminExposed, tlsContexts[kubeName2])
	if err != nil {
		t.Fatal(err)
	}

	expectedCluster1Events := e2eutil.EventValidator{}
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", 0)
	expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

	expectedCluster2Events := e2eutil.EventValidator{}
	expectedCluster2Events.AddClusterEvent(xdcrCluster2, "AdminConsoleServiceCreate")
	expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "AddNewMember", 0)
	expectedCluster2Events.AddClusterNodeServiceEvent(xdcrCluster2, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster2Events.AddClusterBucketEvent(xdcrCluster2, "Create", "default")

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName1), f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	_, err = e2eutil.CreateAdminConsoleClient(t, f.ApiServerHost(kubeName2), f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	xdcr1KubeHost, err := f.GetKubeHostname(kubeName1)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr1KubeHost, f.Namespace, f.PlatformType, defKube.KubeClient, xdcrCluster1)
	if err != nil {
		t.Fatal(err)
	}

	xdcr2KubeHost, err := f.GetKubeHostname(kubeName2)
	if err != nil {
		t.Fatal(err)
	}

	destUrl, err := e2eutil.GetAdminConsoleHostURL(xdcr2KubeHost, f.Namespace, f.PlatformType, xdcrKube.KubeClient, xdcrCluster2)
	if err != nil {
		t.Fatal(err)
	}

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err := e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	// TLS handshake with pods
	for _, kubeName := range []string{kubeName1, kubeName2} {
		t.Logf("Verifying TLS for kube: %s", kubeName)
		targetKube := f.ClusterSpec[kubeName]
		pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
		if err != nil {
			t.Fatal("Unable to get couchbase pods:", err)
		}

		ctx := tlsContexts[kubeName]
		for _, pod := range pods.Items {
			if err := e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config, ctx.CA); err != nil {
				t.Fatal("TLS verification failed:", err)
			}
		}
	}
	ValidateEvents(t, defKube.KubeClient, f.Namespace, xdcrCluster1.Name, expectedCluster1Events)
	ValidateEvents(t, xdcrKube.KubeClient, f.Namespace, xdcrCluster2.Name, expectedCluster2Events)
}

// Create two clusters one on k8s using operator
// and one directly running on usual VMs and verify
func TestXdcrCreateK8SVMCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	defKubeName := "BasicCluster"
	defKube := f.ClusterSpec[defKubeName]

	// Cluster 1
	clusterSize := constants.Size2
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, clusterSize, constants.WithBucket, constants.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}

	expectedCluster1Events := e2eutil.EventValidator{}
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", memberIndex)
	}
	expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
	expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

	defKubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}

	// Deploy a stand alone cluster using the same host where K8S is running
	hostUrl := defKubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := defKubeHost + ":8091"
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"
	externalCbClusterName := "externalCluster"

	if responseData, err := e2eutil.FlushBucket(destUrl, destBucketName, cbUsername, cbPassword); err != nil {
		t.Logf("Response from http call: %v", string(responseData))
		t.Fatal(err)
	}

	// Enable replication from K8S CB cluster -> External CB cluster
	uuid, err := e2eutil.GetRemoteUuid(destUrl, cbUsername, cbPassword)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, uuid, externalCbClusterName, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, externalCbClusterName, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	// Validate bucket replication from K8S -> external cluster
	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 100, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 100, constants.Retries10); err != nil {
		t.Fatal(err)
	}

	/*
		// Enable replication from External CB cluster -> K8S CB cluster
		xdcrRefList, err := e2eutil.GetXdcrClusterReferences(destUrl, cbUsername, cbPassword)
		if err != nil {
			t.Fatal(err)
		}

		for _, xdcrRef := range xdcrRefList {
			if err := e2eutil.DeleteXdcrClusterReferences(destUrl, cbUsername, cbPassword, xdcrRef); err != nil {
				t.Fatal(err)
			}
		}

		uuid, err = e2eutil.GetRemoteUuid(hostUrl, cbUsername, cbPassword)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e2eutil.CreateDestClusterReference(destUrl, cbUsername, cbPassword, uuid, xdcrCluster1.Name, hostUrl, cbUsername, cbPassword); err != nil {
			t.Fatal(err)
		}

		if _, err = e2eutil.CreateXdcrBucketReplication(destUrl, cbUsername, cbPassword, xdcrCluster1.Name, destBucketName, srcBucketName, versionType); err != nil {
			t.Fatal(err)
		}

		// Validate bucket replication from external -> K8S cluster
		if _, err = e2eutil.PopulateBucket(destUrl, destBucketName, cbUsername, cbPassword, 100, 101); err != nil {
			t.Fatal(err)
		}

		if err := e2eutil.VerifyDocCountInBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 200, constants.Retries10); err != nil {
			t.Fatal(err)
		}
	*/
	ValidateEvents(t, defKube.KubeClient, f.Namespace, xdcrCluster1.Name, expectedCluster1Events)
}

// Create two clusters and while trying to configure XDCR,
// couchbase pod goes down
func TestXdcrNodeDownDuringSetupDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	ClusterNodeDownWithXdcr(t, "duringXdcrSetup", kubeNameList)
}

// Create two clusters and XDCR is configured between the two clusters
// Couchbase pod goes down after setup is successful, replaced by new node
func TestXdcrNodeDownDuringSetupAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	ClusterNodeDownWithXdcr(t, "afterXdcrSetup", kubeNameList)
}

// Create two clusters and while trying to configure XDCR,
// new couchbase pod is added to the existing cluster
func TestXdcrNodeAddDuringSetupDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	ClusterAddNodeWithXdcr(t, "duringXdcrSetup", kubeNameList)
}

// Create two clusters and XDCR is configured between the two clusters
// New couchbase pod is added to the existing cluster
func TestXdcrNodeAddDuringSetupAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	ClusterAddNodeWithXdcr(t, "afterXdcrSetup", kubeNameList)
}

// Create two clusters and while trying to configure XDCR,
// XDCR exposed node port service is killed
func TestXdcrNodeServiceKilledDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	ClusterNodeXdcrServiceKill(t, "duringXdcrSetup", kubeNameList)
}

// Create two clusters and XDCR is configured between the two clusters
// XDCR exposed node port service is killed after the replication is progress
func TestXdcrNodeServiceKilledAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	ClusterNodeXdcrServiceKill(t, "afterXdcrSetup", kubeNameList)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	XdcrClusterRemoveNode(t, kubeNameList, "source", "rebalanceOutNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	XdcrClusterRemoveNode(t, kubeNameList, "remote", "rebalanceOutNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	XdcrClusterRemoveNode(t, kubeNameList, "source", "killNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	XdcrClusterRemoveNode(t, kubeNameList, "remote", "killNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of source bucket cluster is resized to single node cluster
func TestXdcrResizedOutSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	XdcrClusterRemoveNode(t, kubeNameList, "source", "resizeOut")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of destination bucket cluster is resized to single node cluster
func TestXdcrResizedOutTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	XdcrClusterRemoveNode(t, kubeNameList, "remote", "resizeOut")
}
