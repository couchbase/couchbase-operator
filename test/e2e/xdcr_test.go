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
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// Generic function to run rebalance out test case
// Rebalance out xdcrCluster nodes one by one for the provided clustersize
func rebalanceOutXdcrNodes(t *testing.T, cbCluster *api.CouchbaseCluster, clusterSize int, k8s *types.Cluster, expectedEvents *e2eutil.EventValidator) error {
	f := framework.Global
	targetKube := k8s
	nextNodeToBeAdded := clusterSize

	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, cbCluster)
	defer cleanup()

	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		// Create node client
		clusterNodeName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		t.Logf("Rebalance-out %s", clusterNodeName)

		if err := e2eutil.RebalanceOutMember(t, client, cbCluster.Name, f.Namespace, memberIndex, true); err != nil {
			return errors.New("Rebalance-out failed: " + err.Error())
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIndex)

		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberAddEvent(cbCluster, nextNodeToBeAdded), 2*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceStartedEvent(cbCluster), time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", nextNodeToBeAdded)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 2*time.Minute)
		nextNodeToBeAdded++
	}
	return nil
}

// Generic function to kill xdcrCluster nodes
// Will kill all the nodes one by one for the given clusterSize number and wait for the new pod to get replaced
func killXdcrNodes(t *testing.T, cbCluster *api.CouchbaseCluster, clusterSize int, k8s *types.Cluster, expectedEvents *e2eutil.EventValidator) error {
	f := framework.Global
	targetKube := k8s
	nextNodeToBeAdded := clusterSize
	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		memberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberIndex)
		if err := e2eutil.DeletePod(t, targetKube.KubeClient, memberName, f.Namespace); err != nil {
			return err
		}
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberDown", memberIndex)

		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberFailedOverEvent(cbCluster, memberIndex), 2*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.NewMemberAddEvent(cbCluster, nextNodeToBeAdded), 3*time.Minute)
		e2eutil.MustWaitForClusterEvent(t, targetKube, cbCluster, e2eutil.RebalanceCompletedEvent(cbCluster), 5*time.Minute)

		expectedEvents.AddClusterPodEvent(cbCluster, "FailedOver", memberIndex)
		expectedEvents.AddClusterPodEvent(cbCluster, "AddNewMember", nextNodeToBeAdded)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceStarted")
		expectedEvents.AddClusterPodEvent(cbCluster, "MemberRemoved", memberIndex)
		expectedEvents.AddClusterEvent(cbCluster, "RebalanceCompleted")

		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, cbCluster, 2*time.Minute)
		nextNodeToBeAdded++
	}
	return nil
}

// Generic function to resize the xdcrCluster to the given clusterSize value and wait for healthy cluster
func resizeXdcrCluster(t *testing.T, cbCluster *api.CouchbaseCluster, clusterSize int, k8s *types.Cluster) *api.CouchbaseCluster {
	service := 0
	targetKube := k8s
	return e2eutil.MustResizeCluster(t, service, clusterSize, targetKube, cbCluster, 5*time.Minute)
}

// Generic function to run all pod removal/resize tests
// Remove nodes using diff ways based on operationType value
// This will in-turn call rebalanceOutXdcrNodes / killXdcrNodes / resizeXdcrCluster function appropiately
// targetClusterNodes will be source / destination to decide target cluster from xdcrCluster1 / xdccrCluster2
func XdcrClusterRemoveNode(t *testing.T, cluster1, cluster2 *types.Cluster, targetClusterNodes, operationType string) {
	f := framework.Global
	xdcr1Kube := cluster1
	xdcr2Kube := cluster2

	clusterSize := constants.Size3

	// Cluster 1
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, xdcr1Kube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

	// Cluster 2
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, xdcr2Kube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

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

	hostUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcr1Kube, xdcrCluster1)
	defer cleanup()

	destUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcr2Kube, xdcrCluster2)
	defer cleanup()

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	e2eutil.CreateDestClusterReference(t, hostUrl, xdcr2Kube, xdcrCluster2, cbUsername, cbPassword)

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
			if err := rebalanceOutXdcrNodes(t, xdcrCluster1, clusterSize, xdcr1Kube, &expectedCluster1Events); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := rebalanceOutXdcrNodes(t, xdcrCluster2, clusterSize, xdcr2Kube, &expectedCluster2Events); err != nil {
				t.Fatal(err)
			}
		}
	case "killNodes":
		if targetClusterNodes == "source" {
			if err := killXdcrNodes(t, xdcrCluster1, clusterSize, xdcr1Kube, &expectedCluster1Events); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := killXdcrNodes(t, xdcrCluster2, clusterSize, xdcr2Kube, &expectedCluster2Events); err != nil {
				t.Fatal(err)
			}
		}
	case "resizeOut":
		if targetClusterNodes == "source" {
			xdcrCluster1 = resizeXdcrCluster(t, xdcrCluster1, constants.Size1, xdcr1Kube)
			expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", 1)
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", 2)
			expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
		} else {
			xdcrCluster2 = resizeXdcrCluster(t, xdcrCluster2, constants.Size1, xdcr2Kube)
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
	ValidateEvents(t, xdcr1Kube, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, xdcr2Kube, xdcrCluster2, expectedCluster2Events)
}

// Generic test case for creating Xdcr clusters
// Can support 2 clusters within same K8S kube or
// different kube based on the kubeNames provided in kubeNameList
func CreateXdcrCluster(t *testing.T, cluster1, cluster2 *types.Cluster) {
	f := framework.Global
	xdcr1Kube := cluster1
	xdcr2Kube := cluster2

	clusterSize := constants.Size3

	// Cluster 1
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, xdcr1Kube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

	// Cluster 2
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, xdcr2Kube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

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

	hostUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcr1Kube, xdcrCluster1)
	defer cleanup()

	destUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcr2Kube, xdcrCluster2)
	defer cleanup()

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	e2eutil.CreateDestClusterReference(t, hostUrl, xdcr2Kube, xdcrCluster2, cbUsername, cbPassword)

	if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, xdcrCluster2.Name, srcBucketName, destBucketName, versionType); err != nil {
		t.Fatal(err)
	}

	if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 10, 1); err != nil {
		t.Fatal(err)
	}

	if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 10, constants.Retries10); err != nil {
		t.Fatal(err)
	}
	ValidateEvents(t, xdcr1Kube, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, xdcr2Kube, xdcrCluster2, expectedCluster2Events)
}

// Generic testcase to run NodeDown test cases on kubeNames given by kubeNameList
// This can support bringing down a cluster node either during XDCR configration / post XDCR configuration
func ClusterNodeDownWithXdcr(t *testing.T, triggerDuring string, cluster1, cluster2 *types.Cluster) {
	f := framework.Global
	defKube := cluster1
	xdcrKube := cluster2

	xdcrCluster1Size := constants.Size5
	xdcrCluster2Size := constants.Size2

	// Cluster 1
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, defKube, f.Namespace, xdcrCluster1Size, constants.WithBucket, constants.AdminExposed)

	// Cluster 2
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, xdcrKube, f.Namespace, xdcrCluster2Size, constants.WithBucket, constants.AdminExposed)

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

	hostUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, defKube, xdcrCluster1)
	defer cleanup()

	destUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcrKube, xdcrCluster2)
	defer cleanup()

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	e2eutil.CreateDestClusterReference(t, hostUrl, xdcrKube, xdcrCluster2, cbUsername, cbPassword)

	errChan := make(chan error)
	nodeDownFunc := func(nodeIndex int) {
		// Kill first Pod of cluster-1
		if err := e2eutil.KillPodForMember(defKube.KubeClient, xdcrCluster1, nodeIndex); err != nil {
			errChan <- err
			return
		}
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberDown", nodeIndex)

		event := e2eutil.NewMemberFailedOverEvent(xdcrCluster1, nodeIndex)
		if err := e2eutil.WaitForClusterEvent(defKube.KubeClient, xdcrCluster1, event, 2*time.Minute); err != nil {
			errChan <- err
			return
		}
		expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "FailedOver", nodeIndex)

		event = e2eutil.NewMemberAddEvent(xdcrCluster1, 5)
		if err := e2eutil.WaitForClusterEvent(defKube.KubeClient, xdcrCluster1, event, 2*time.Minute); err != nil {
			errChan <- err
			return
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
	ValidateEvents(t, defKube, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, xdcrKube, xdcrCluster2, expectedCluster2Events)
}

// Generic testcase to run AddNode test cases on kubeNames given by kubeNameList
// This can support adding new node to the cluster either during XDCR configration / post XDCR configuration
func ClusterAddNodeWithXdcr(t *testing.T, triggerDuring string, cluster1, cluster2 *types.Cluster) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	defKube := cluster1
	xdcrKube := cluster2

	// Cluster 1
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, defKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

	// Cluster 2
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, xdcrKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

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

	errChan := make(chan error)
	resizeFunction := func() {
		service := 0
		clusterSize := constants.Size3
		var err error
		xdcrCluster1, err = e2eutil.ResizeCluster(t, service, clusterSize, defKube, xdcrCluster1, 5*time.Minute)
		if err != nil {
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

	hostUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, defKube, xdcrCluster1)
	defer cleanup()

	destUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcrKube, xdcrCluster2)
	defer cleanup()

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	e2eutil.CreateDestClusterReference(t, hostUrl, xdcrKube, xdcrCluster2, cbUsername, cbPassword)

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
	ValidateEvents(t, defKube, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, xdcrKube, xdcrCluster2, expectedCluster2Events)
}

// Generic testcase to kill the XDCR exposed service test cases on kubeNames given by kubeNameList
// This supports killing the xdcr port service either during XDCR configration / post XDCR configuration
func ClusterNodeXdcrServiceKill(t *testing.T, triggerDuring string, cluster1, cluster2 *types.Cluster) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	defKube := cluster1
	xdcrKube := cluster2

	// Cluster 1
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, defKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

	// Cluster 2
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, xdcrKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

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

	errChan := make(chan error)
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

	hostUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, defKube, xdcrCluster1)
	defer cleanup()

	destUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcrKube, xdcrCluster2)
	defer cleanup()

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	e2eutil.CreateDestClusterReference(t, hostUrl, xdcrKube, xdcrCluster2, cbUsername, cbPassword)

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
	ValidateEvents(t, defKube, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, xdcrKube, xdcrCluster2, expectedCluster2Events)
}

// Create XDCR cluster within same k8s cluster
func TestXdcrCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	CreateXdcrCluster(t, cluster1, cluster2)
}

// TLSClusterMap maps the Kubernetes cluster to its TLS configuration.
type TLSClusterMap struct {
	cluster *types.Cluster
	context *e2eutil.TlsContext
}

// Create cb clusters on top of TLS certificates
func TestXdcrCreateTlsCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global

	tlsMap := []TLSClusterMap{}

	defKube := f.GetCluster(0)
	xdcrKube := f.GetCluster(1)

	// Create secrets in all k8s clusters
	for index := 0; index < 2; index++ {
		cluster := f.GetCluster(index)
		ctx, teardown, err := e2eutil.InitClusterTLS(cluster.KubeClient, f.Namespace, &e2eutil.TlsOpts{})
		if err != nil {
			t.Fatal(err)
		}
		defer teardown()
		tlsMap = append(tlsMap, TLSClusterMap{cluster: cluster, context: ctx})
	}

	// Cluster 1
	xdcrCluster1 := e2eutil.MustNewTlsXdcrClusterBasic(t, defKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed, tlsMap[0].context)

	// Cluster 2
	xdcrCluster2 := e2eutil.MustNewTlsXdcrClusterBasic(t, xdcrKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed, tlsMap[1].context)

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

	hostUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, defKube, xdcrCluster1)
	defer cleanup()

	destUrl, cleanup := e2eutil.GetAdminConsoleHostURL(t, xdcrKube, xdcrCluster2)
	defer cleanup()

	srcBucketName := "default"
	destBucketName := "default"
	versionType := "xmem"
	cbUsername := "Administrator"
	cbPassword := "password"

	e2eutil.CreateDestClusterReference(t, hostUrl, xdcrKube, xdcrCluster2, cbUsername, cbPassword)

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
	for index, tlsMapping := range tlsMap {
		t.Logf("Verifying TLS for cluster %d", index)
		pods, err := tlsMapping.cluster.KubeClient.CoreV1().Pods(f.Namespace).List(metav1.ListOptions{LabelSelector: constants.CouchbaseLabel})
		if err != nil {
			t.Fatal("Unable to get couchbase pods:", err)
		}

		for _, pod := range pods.Items {
			if err := e2eutil.TlsCheckForPod(t, tlsMapping.cluster, f.Namespace, pod.GetName(), tlsMapping.context); err != nil {
				t.Fatal("TLS verification failed:", err)
			}
		}
	}
	ValidateEvents(t, defKube, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, xdcrKube, xdcrCluster2, expectedCluster2Events)
}

// Create two clusters one on k8s using operator
// and one directly running on usual VMs and verify
func TestXdcrCreateK8SVMCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	t.Skip("Work out how to make me work")

	/*
		f := framework.Global
		defKube := f.GetCluster(0)

		// Cluster 1
		clusterSize := constants.Size2
		xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, defKube, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

		expectedCluster1Events := e2eutil.EventValidator{}
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
		for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", memberIndex)
		}
		expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
		expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

		defKubeHost, err := defKube.APIHostname()
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

		e2eutil.CreateDestClusterReference(t, hostUrl, xdcrKube,xdcrCluster2, cbUsername, cbPassword)

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
		ValidateEvents(t, defKube, xdcrCluster1, expectedCluster1Events)
	*/
}

// Create two clusters and while trying to configure XDCR,
// couchbase pod goes down
func TestXdcrNodeDownDuringSetupDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	ClusterNodeDownWithXdcr(t, "duringXdcrSetup", cluster1, cluster2)
}

// Create two clusters and XDCR is configured between the two clusters
// Couchbase pod goes down after setup is successful, replaced by new node
func TestXdcrNodeDownDuringSetupAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	ClusterNodeDownWithXdcr(t, "afterXdcrSetup", cluster1, cluster2)
}

// Create two clusters and while trying to configure XDCR,
// new couchbase pod is added to the existing cluster
func TestXdcrNodeAddDuringSetupDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	ClusterAddNodeWithXdcr(t, "duringXdcrSetup", cluster1, cluster2)
}

// Create two clusters and XDCR is configured between the two clusters
// New couchbase pod is added to the existing cluster
func TestXdcrNodeAddDuringSetupAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	ClusterAddNodeWithXdcr(t, "afterXdcrSetup", cluster1, cluster2)
}

// Create two clusters and while trying to configure XDCR,
// XDCR exposed node port service is killed
func TestXdcrNodeServiceKilledDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	ClusterNodeXdcrServiceKill(t, "duringXdcrSetup", cluster1, cluster2)
}

// Create two clusters and XDCR is configured between the two clusters
// XDCR exposed node port service is killed after the replication is progress
func TestXdcrNodeServiceKilledAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	ClusterNodeXdcrServiceKill(t, "afterXdcrSetup", cluster1, cluster2)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, cluster1, cluster2, "source", "rebalanceOutNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, cluster1, cluster2, "remote", "rebalanceOutNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, cluster1, cluster2, "source", "killNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, cluster1, cluster2, "remote", "killNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of source bucket cluster is resized to single node cluster
func TestXdcrResizedOutSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, cluster1, cluster2, "source", "resizeOut")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of destination bucket cluster is resized to single node cluster
func TestXdcrResizedOutTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	cluster1 := f.GetCluster(0)
	cluster2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, cluster1, cluster2, "remote", "resizeOut")
}
