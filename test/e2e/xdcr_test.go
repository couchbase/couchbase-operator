package e2e

import (
	"errors"
	"os"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Generic function to run rebalance out test case
// Rebalance out xdcrCluster nodes one by one for the provided clustersize
func rebalanceOutXdcrNodes(t *testing.T, xdcrCluster *api.CouchbaseCluster, clusterSize int, kubeName string) error {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		// Create node client
		clusterNodeName := couchbaseutil.CreateMemberName(xdcrCluster.Name, memberIndex)
		t.Logf("Rebalance-out %s", clusterNodeName)

		nodePortService := e2espec.NewNodePortService(f.Namespace)
		nodePortService.Spec.Selector["couchbase_node"] = clusterNodeName
		service, err := e2eutil.CreateService(t, targetKube.KubeClient, f.Namespace, nodePortService)
		if err != nil {
			return errors.New("Failed to create service: %s" + err.Error())
		}

		serviceUrl, err := e2eutil.NodePortServiceClient(f.ApiServerHost(kubeName), service)
		if err != nil {
			return errors.New("failed to get cluster url: %s" + err.Error())
		}

		client, err := e2eutil.NewClient(t, targetKube.KubeClient, xdcrCluster, []string{serviceUrl})
		if err != nil {
			return errors.New("failed to create cluster client %s" + err.Error())
		}

		if err = e2eutil.RebalanceOutMember(t, client, xdcrCluster.Name, f.Namespace, memberIndex, true); err != nil {
			return errors.New("Rebalance-out failed: %s" + err.Error())
		}

		err = e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, xdcrCluster.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
		if err != nil {
			return errors.New(err.Error())
		}
	}
	return nil
}

// Generic function to kill xdcrCluster nodes
// Will kill all the nodes one by one for the given clusterSize number and wait for the new pod to get replaced
func killXdcrNodes(t *testing.T, xdcrCluster *api.CouchbaseCluster, clusterSize int, kubeName string) error {
	f := framework.Global
	targetKube := f.ClusterSpec[kubeName]
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		memberName := couchbaseutil.CreateMemberName(xdcrCluster.Name, memberIndex)
		if _, err := f.ExecShellInPod(kubeName, memberName, "mv /etc/service/couchbase-server /tmp/"); err != nil {
			return err
		}

		if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, xdcrCluster.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10); err != nil {
			return err
		}
	}
	return nil
}

// Generic function to resize the xdcrCluster to the given clusterSize value and wait for healthy cluster
func resizeXdcrCluster(t *testing.T, xdcrCluster *api.CouchbaseCluster, clusterSize int, kubeName string) error {
	f := framework.Global
	service := 0
	targetKube := f.ClusterSpec[kubeName]
	if err := e2eutil.ResizeCluster(t, service, clusterSize, targetKube.CRClient, xdcrCluster); err != nil {
		return err
	}

	if err := e2eutil.WaitClusterStatusHealthy(t, targetKube.CRClient, xdcrCluster.Name, f.Namespace, clusterSize, e2eutil.Retries10); err != nil {
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

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, xdcr1Kube.KubeClient, xdcr1Kube.CRClient, f.Namespace, xdcr1Kube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, xdcr1Kube.KubeClient, xdcr1Kube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster1Events := e2eutil.EventList{}
	expectedXdcrCluster1Events.AddAdminConsoleSvcCreateEvent(xdcrCluster1)
	for nodeIndex := 0; nodeIndex < e2eutil.Size3; nodeIndex++ {
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 0)
	}
	expectedXdcrCluster1Events.AddRebalanceStartedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddRebalanceCompletedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddBucketCreateEvent(xdcrCluster1, "default")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "admin")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "data")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "view")

	_, err = xdcr1Kube.KubeClient.CoreV1().Events(f.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcr2Kube.KubeClient, xdcr2Kube.CRClient, f.Namespace, xdcr2Kube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, xdcr2Kube.KubeClient, xdcr2Kube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster2Events := e2eutil.EventList{}
	expectedXdcrCluster2Events.AddAdminConsoleSvcCreateEvent(xdcrCluster2)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster2Events.AddMemberAddEvent(xdcrCluster2, 0)
	}
	expectedXdcrCluster1Events.AddRebalanceStartedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddRebalanceCompletedEvent(xdcrCluster1)
	expectedXdcrCluster2Events.AddBucketCreateEvent(xdcrCluster2, "default")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "admin")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "data")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "view")

	xdcr1KubeHost, err := f.GetKubeHostname(xdcr1KubeName)
	if err != nil {
		t.Fatal(err)
	}
	xdcr2KubeHost, err := f.GetKubeHostname(xdcr2KubeName)
	if err != nil {
		t.Fatal(err)
	}
	hostUrl := xdcr1KubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := xdcr2KubeHost + ":" + xdcrCluster2.Status.AdminConsolePort
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err = e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	switch operationType {
	case "rebalanceOutNodes":
		if targetClusterNodes == "source" {
			rebalanceOutXdcrNodes(t, xdcrCluster1, e2eutil.Size3, xdcr1KubeName)
		} else {
			rebalanceOutXdcrNodes(t, xdcrCluster2, e2eutil.Size3, xdcr2KubeName)
		}
	case "killNodes":
		if targetClusterNodes == "source" {
			killXdcrNodes(t, xdcrCluster1, e2eutil.Size3, xdcr1KubeName)
		} else {
			killXdcrNodes(t, xdcrCluster2, e2eutil.Size3, xdcr2KubeName)
		}
	case "resizeOut":
		if targetClusterNodes == "source" {
			resizeXdcrCluster(t, xdcrCluster1, e2eutil.Size1, xdcr1KubeName)
		} else {
			resizeXdcrCluster(t, xdcrCluster2, e2eutil.Size1, xdcr2KubeName)
		}
	default:
		t.Fatalf("Unsupported operation: %s", operationType)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	ValidateClusterEvents(t, xdcr1Kube.KubeClient, xdcrCluster1.Name, f.Namespace, expectedXdcrCluster1Events)
	ValidateClusterEvents(t, xdcr2Kube.KubeClient, xdcrCluster2.Name, f.Namespace, expectedXdcrCluster2Events)
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

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, xdcr1Kube.KubeClient, xdcr1Kube.CRClient, f.Namespace, xdcr1Kube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, xdcr1Kube.KubeClient, xdcr1Kube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster1Events := e2eutil.EventList{}
	expectedXdcrCluster1Events.AddAdminConsoleSvcCreateEvent(xdcrCluster1)
	for nodeIndex := 0; nodeIndex < e2eutil.Size3; nodeIndex++ {
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, nodeIndex)
	}
	expectedXdcrCluster1Events.AddRebalanceStartedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddRebalanceCompletedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddBucketCreateEvent(xdcrCluster1, "default")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "admin")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "data")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "view")

	_, err = xdcr1Kube.KubeClient.CoreV1().Events(f.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcr2Kube.KubeClient, xdcr2Kube.CRClient, f.Namespace, xdcr2Kube.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, xdcr2Kube.KubeClient, xdcr2Kube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster2Events := e2eutil.EventList{}
	expectedXdcrCluster2Events.AddAdminConsoleSvcCreateEvent(xdcrCluster2)
	for nodeIndex := 0; nodeIndex < e2eutil.Size3; nodeIndex++ {
		expectedXdcrCluster2Events.AddMemberAddEvent(xdcrCluster2, nodeIndex)
	}
	expectedXdcrCluster2Events.AddRebalanceStartedEvent(xdcrCluster2)
	expectedXdcrCluster2Events.AddRebalanceCompletedEvent(xdcrCluster2)
	expectedXdcrCluster2Events.AddBucketCreateEvent(xdcrCluster2, "default")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "admin")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "data")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "view")

	xdcr1KubeHost, err := f.GetKubeHostname(xdcr1KubeName)
	if err != nil {
		t.Fatal(err)
	}

	xdcr2KubeHost, err := f.GetKubeHostname(xdcr1KubeName)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl := xdcr1KubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := xdcr2KubeHost + ":" + xdcrCluster2.Status.AdminConsolePort

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err = e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	ValidateClusterEvents(t, xdcr1Kube.KubeClient, xdcrCluster1.Name, f.Namespace, expectedXdcrCluster1Events)
	ValidateClusterEvents(t, xdcr2Kube.KubeClient, xdcrCluster2.Name, f.Namespace, expectedXdcrCluster2Events)
}

// Generic testcase to run NodeDown test cases on kubeNames given by kubeNameList
// This can support bringing down a cluster node either during XDCR configration / post XDCR configuration
func ClusterNodeDownWithXdcr(t *testing.T, triggerDuring string, kubeNameList []string) {
	f := framework.Global
	defKubeName := kubeNameList[0]
	defKube := f.ClusterSpec[defKubeName]

	xdcrKubeName := kubeNameList[1]
	xdcrKube := f.ClusterSpec[xdcrKubeName]

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, e2eutil.Size2, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster1Events := e2eutil.EventList{}
	expectedXdcrCluster1Events.AddAdminConsoleSvcCreateEvent(xdcrCluster1)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 0)
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 1)
	}
	expectedXdcrCluster1Events.AddRebalanceStartedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddRebalanceCompletedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddBucketCreateEvent(xdcrCluster1, "default")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "admin")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "data")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "view")

	_, err = defKube.KubeClient.CoreV1().Events(f.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, e2eutil.Size2, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster2Events := e2eutil.EventList{}
	expectedXdcrCluster2Events.AddAdminConsoleSvcCreateEvent(xdcrCluster2)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster2Events.AddMemberAddEvent(xdcrCluster2, 0)
	}
	expectedXdcrCluster2Events.AddBucketCreateEvent(xdcrCluster2, "default")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "admin")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "data")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "view")

	defKubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}

	xdcrKubeHost, err := f.GetKubeHostname(xdcrKubeName)
	if err != nil {
		t.Fatal(err)
	}

	hostUrl := defKubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := xdcrKubeHost + ":" + xdcrCluster2.Status.AdminConsolePort
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err = e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	errChan := make(chan error)
	nodeDownFunc := func(nodeIndex int) {
		// Kill first Pod of cluster-1
		memberName := couchbaseutil.CreateMemberName(xdcrCluster1.Name, nodeIndex)
		_, err := f.ExecShellInPod(defKubeName, memberName, "mv /etc/service/couchbase-server /tmp/")
		errChan <- err
	}

	if triggerDuring == "duringXdcrSetup" {
		go nodeDownFunc(0)
	}

	if _, err = e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	if triggerDuring == "afterXdcrSetup" {
		go nodeDownFunc(0)
	}

	err = <-errChan
	if err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	ValidateClusterEvents(t, defKube.KubeClient, xdcrCluster1.Name, f.Namespace, expectedXdcrCluster1Events)
	ValidateClusterEvents(t, xdcrKube.KubeClient, xdcrCluster2.Name, f.Namespace, expectedXdcrCluster2Events)
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

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster1Events := e2eutil.EventList{}
	expectedXdcrCluster1Events.AddAdminConsoleSvcCreateEvent(xdcrCluster1)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 0)
	}
	expectedXdcrCluster1Events.AddBucketCreateEvent(xdcrCluster1, "default")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "admin")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "data")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "view")

	_, err = defKube.KubeClient.CoreV1().Events(f.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster2Events := e2eutil.EventList{}
	expectedXdcrCluster2Events.AddAdminConsoleSvcCreateEvent(xdcrCluster2)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster2Events.AddMemberAddEvent(xdcrCluster2, 0)
	}
	expectedXdcrCluster2Events.AddBucketCreateEvent(xdcrCluster2, "default")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "admin")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "data")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "view")

	errChan := make(chan error)
	resizeFunction := func() {
		service := 0
		clusterSize := e2eutil.Size3
		if err = e2eutil.ResizeCluster(t, service, clusterSize, defKube.CRClient, xdcrCluster1); err != nil {
			errChan <- err
			return
		}

		if err = e2eutil.WaitClusterStatusHealthy(t, defKube.CRClient, xdcrCluster1.Name, f.Namespace, clusterSize, e2eutil.Retries10); err != nil {
			errChan <- err
			return
		}

		for memberIndex := 1; memberIndex < clusterSize; memberIndex++ {
			expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, memberIndex)
		}
		expectedXdcrCluster1Events.AddRebalanceStartedEvent(xdcrCluster1)
		expectedXdcrCluster1Events.AddRebalanceCompletedEvent(xdcrCluster1)
		errChan <- nil
	}

	if triggerDuring == "duringXdcrSetup" {
		go resizeFunction()
	}

	defKubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}
	xdcrKubeHost, err := f.GetKubeHostname(xdcrKubeName)
	if err != nil {
		t.Fatal(err)
	}
	hostUrl := defKubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := xdcrKubeHost + ":" + xdcrCluster2.Status.AdminConsolePort
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err = e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	if triggerDuring == "afterXdcrSetup" {
		go resizeFunction()
	}

	err = <-errChan
	if err != nil {
		t.Fatalf("Failed to resize cluster: %s", err.Error())
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	ValidateClusterEvents(t, defKube.KubeClient, xdcrCluster1.Name, f.Namespace, expectedXdcrCluster1Events)
	ValidateClusterEvents(t, xdcrKube.KubeClient, xdcrCluster2.Name, f.Namespace, expectedXdcrCluster2Events)
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

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster1Events := e2eutil.EventList{}
	expectedXdcrCluster1Events.AddAdminConsoleSvcCreateEvent(xdcrCluster1)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 0)
	}
	expectedXdcrCluster1Events.AddBucketCreateEvent(xdcrCluster1, "default")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "admin")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "data")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "view")

	_, err = defKube.KubeClient.CoreV1().Events(f.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster2Events := e2eutil.EventList{}
	expectedXdcrCluster2Events.AddAdminConsoleSvcCreateEvent(xdcrCluster2)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster2Events.AddMemberAddEvent(xdcrCluster2, 0)
	}
	expectedXdcrCluster2Events.AddBucketCreateEvent(xdcrCluster2, "default")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "admin")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "data")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "view")

	errChan := make(chan error)
	serviceKillFunc := func() {
		services, err := defKube.KubeClient.CoreV1().Services(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase,couchbase_cluster=" + xdcrCluster1.Name})
		if err != nil {
			errChan <- err
			return
		}
		for _, service := range services.Items {
			if strings.HasSuffix(service.Name, "-exposed-ports") {
				if err := e2eutil.DeleteService(t, defKube.KubeClient, f.Namespace, service.Name, metav1.NewDeleteOptions(0)); err != nil {
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

	defKubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}
	xdcrKubeHost, err := f.GetKubeHostname(xdcrKubeName)
	if err != nil {
		t.Fatal(err)
	}
	hostUrl := defKubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := xdcrKubeHost + ":" + xdcrCluster2.Status.AdminConsolePort
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err = e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	if triggerDuring == "afterXdcrSetup" {
		go serviceKillFunc()
	}

	err = <-errChan
	if err != nil {
		t.Fatalf("Failed to remove Xdcr services: %s", err.Error())
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 11); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 20, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	ValidateClusterEvents(t, defKube.KubeClient, xdcrCluster1.Name, f.Namespace, expectedXdcrCluster1Events)
	ValidateClusterEvents(t, xdcrKube.KubeClient, xdcrCluster2.Name, f.Namespace, expectedXdcrCluster2Events)
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

	// Create secrets in both the clusters
	RandomNameSuffix = e2eutil.RandomSuffix()
	decoratorObj := &TlsDecorator{}
	decoratorObj.Init(RandomNameSuffix, f.Namespace, e2eutil.KeyTypeRSA)
	decoratorObj.CreateCaRootCert(t)

	for _, kubeName := range []string{kubeName1, kubeName2} {
		targetKube := f.ClusterSpec[kubeName]
		operatorSecret := decoratorObj.CreateOperatorSecret(t, f, kubeName)
		defer e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, operatorSecret.Name, &metav1.DeleteOptions{})

		clusterSecret := decoratorObj.CreateClusterSecret(t, f, kubeName)
		defer e2eutil.DeleteSecret(targetKube.KubeClient, f.Namespace, clusterSecret.Name, &metav1.DeleteOptions{})

		// Update cluster parameters
		e2espec.SetClusterName(decoratorObj.clusterName)
		defer e2espec.ResetClusterName()

		decoratorObj.SetTlsForTesting(operatorSecret, clusterSecret)
		defer e2espec.ResetTLS()
	}

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster1Events := e2eutil.EventList{}
	expectedXdcrCluster1Events.AddAdminConsoleSvcCreateEvent(xdcrCluster1)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 0)
	}
	expectedXdcrCluster1Events.AddBucketCreateEvent(xdcrCluster1, "default")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "admin")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "data")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "view")

	_, err = defKube.KubeClient.CoreV1().Events(f.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Cluster 2
	xdcrCluster2, err := e2eutil.NewXdcrClusterBasic(t, xdcrKube.KubeClient, xdcrKube.CRClient, f.Namespace, xdcrKube.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster2Events := e2eutil.EventList{}
	expectedXdcrCluster2Events.AddAdminConsoleSvcCreateEvent(xdcrCluster2)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster2Events.AddMemberAddEvent(xdcrCluster2, 0)
	}
	expectedXdcrCluster2Events.AddBucketCreateEvent(xdcrCluster2, "default")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "admin")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "data")
	expectedXdcrCluster2Events.NodeServiceCreateEvent(xdcrCluster2, "view")

	defKubeHost, err := f.GetKubeHostname(kubeName1)
	if err != nil {
		t.Fatal(err)
	}
	xdcrKubeHost, err := f.GetKubeHostname(kubeName2)
	if err != nil {
		t.Fatal(err)
	}
	hostUrl := defKubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := xdcrKubeHost + ":" + xdcrCluster2.Status.AdminConsolePort
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	if _, err = e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, xdcrCluster2.Status.ClusterID, xdcrCluster2.Name, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	// TLS handshake with pods
	for _, kubeName := range []string{kubeName1, kubeName2} {
		t.Logf("Verifying TLS for kube: %s", kubeName)
		targetKube := f.ClusterSpec[kubeName]
		pods, err := targetKube.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: "app=couchbase"})
		if err != nil {
			t.Fatal("Unable to get couchbase pods:", err)
		}

		for _, pod := range pods.Items {
			err = e2eutil.TlsCheckForPod(t, f.Namespace, pod.GetName(), targetKube.Config)
			if err != nil {
				t.Fatal("TLS verification failed:", err)
			}
		}
	}

	ValidateClusterEvents(t, defKube.KubeClient, xdcrCluster1.Name, f.Namespace, expectedXdcrCluster1Events)
	ValidateClusterEvents(t, xdcrKube.KubeClient, xdcrCluster2.Name, f.Namespace, expectedXdcrCluster2Events)
}

// Create two clusters one on k8s using operator
// and one directly running on usual VMs and verify
func TestXdcrCreateK8SVMCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	kubeNameList := []string{"BasicCluster", "ExternalVMs"}

	defKubeName := kubeNameList[0]
	defKube := f.ClusterSpec[defKubeName]

	// Cluster 1
	xdcrCluster1, err := e2eutil.NewXdcrClusterBasic(t, defKube.KubeClient, defKube.CRClient, f.Namespace, defKube.DefaultSecret.Name, e2eutil.Size2, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, defKube.KubeClient, defKube.CRClient, f.Namespace, f.LogDir)

	expectedXdcrCluster1Events := e2eutil.EventList{}
	expectedXdcrCluster1Events.AddAdminConsoleSvcCreateEvent(xdcrCluster1)
	for nodeIndex := 0; nodeIndex < e2eutil.Size1; nodeIndex++ {
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 0)
		expectedXdcrCluster1Events.AddMemberAddEvent(xdcrCluster1, 1)
	}
	expectedXdcrCluster1Events.AddRebalanceStartedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddRebalanceCompletedEvent(xdcrCluster1)
	expectedXdcrCluster1Events.AddBucketCreateEvent(xdcrCluster1, "default")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "admin")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "data")
	expectedXdcrCluster1Events.NodeServiceCreateEvent(xdcrCluster1, "view")

	defKubeHost, err := f.GetKubeHostname(defKubeName)
	if err != nil {
		t.Fatal(err)
	}
	externalVmClusterData, err := f.GetHostNameFromUrl(kubeNameList[1])
	if err != nil {
		t.Fatal(err)
	}

	hostUrl := defKubeHost + ":" + xdcrCluster1.Status.AdminConsolePort
	destUrl := externalVmClusterData + ":8091"
	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"
	externalCbClusterName := kubeNameList[1]

	uuid, err := e2eutil.GetRemoteUuid(destUrl, cbUsername, cbPassword)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.CreateDestClusterReference(hostUrl, cbUsername, cbPassword, uuid, externalCbClusterName, destUrl, cbUsername, cbPassword); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, externalCbClusterName, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err = e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 100, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 100, e2eutil.Retries10); err != nil {
		t.Fatal(err)
	}

	ValidateClusterEvents(t, defKube.KubeClient, xdcrCluster1.Name, f.Namespace, expectedXdcrCluster1Events)
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

	kubeNameList := []string{"BasicCluster", "BasicCluster"}
	XdcrClusterRemoveNode(t, kubeNameList, "source", "rebalanceOutXdcrNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	kubeNameList := []string{"BasicCluster", "NewCluster1"}
	XdcrClusterRemoveNode(t, kubeNameList, "remote", "rebalanceOutXdcrNodes")
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
