package e2e

import (
	"os"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// Generic function to run rebalance out test case
// Rebalance out xdcrCluster nodes one by one for the provided clustersize
func rebalanceOutXdcrNodes(t *testing.T, k8s *types.Cluster, couchbase *api.CouchbaseCluster, clusterSize int, expectedEvents *e2eutil.EventValidator) {
	nextNodeToBeAdded := clusterSize

	for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
		// Create node client
		e2eutil.MustEjectMember(t, k8s, couchbase, memberIndex, 3*time.Minute)
		expectedEvents.AddClusterPodEvent(couchbase, "MemberRemoved", memberIndex)

		e2eutil.MustWaitForClusterEvent(t, k8s, couchbase, e2eutil.RebalanceCompletedEvent(couchbase), 5*time.Minute)

		expectedEvents.AddClusterPodEvent(couchbase, "AddNewMember", nextNodeToBeAdded)
		expectedEvents.AddClusterEvent(couchbase, "RebalanceStarted")
		expectedEvents.AddClusterEvent(couchbase, "RebalanceCompleted")

		// These tests fail intermitently due to something going screwy with server,
		// however it seems to rebalance again and save itself.  Would be nice to remove
		// this and run the test in a loop until it does fail and gather server logs...
		*expectedEvents = append(*expectedEvents, eventschema.Optional{
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: "RebalanceStarted"},
					eventschema.Event{Reason: "RebalanceCompleted"},
				},
			},
		})

		e2eutil.MustWaitClusterStatusHealthy(t, k8s, couchbase, 2*time.Minute)
		nextNodeToBeAdded++
	}
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
func XdcrClusterRemoveNode(t *testing.T, k8s1, k8s2 *types.Cluster, targetClusterNodes, operationType string) {
	// Platform configuration.
	f := framework.Global

	// Static configuration.
	clusterSize := constants.Size3
	srcBucketName := "default"
	destBucketName := "default"
	cbUsername := "Administrator"
	cbPassword := "password"

	// Create the clusters.
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

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

	e2eutil.MustCreateDestClusterReference(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword)
	e2eutil.MustCreateXdcrBucketReplication(t, k8s1, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword, srcBucketName, destBucketName)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 10, 10*time.Minute)

	switch operationType {
	case "rebalanceOutNodes":
		if targetClusterNodes == "source" {
			rebalanceOutXdcrNodes(t, k8s1, xdcrCluster1, clusterSize, &expectedCluster1Events)
		} else {
			rebalanceOutXdcrNodes(t, k8s2, xdcrCluster2, clusterSize, &expectedCluster2Events)
		}
	case "killNodes":
		if targetClusterNodes == "source" {
			if err := killXdcrNodes(t, xdcrCluster1, clusterSize, k8s1, &expectedCluster1Events); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := killXdcrNodes(t, xdcrCluster2, clusterSize, k8s2, &expectedCluster2Events); err != nil {
				t.Fatal(err)
			}
		}
	case "resizeOut":
		if targetClusterNodes == "source" {
			xdcrCluster1 = resizeXdcrCluster(t, xdcrCluster1, constants.Size1, k8s1)
			expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", 1)
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", 2)
			expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
		} else {
			xdcrCluster2 = resizeXdcrCluster(t, xdcrCluster2, constants.Size1, k8s2)
			expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceStarted")
			expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "MemberRemoved", 1)
			expectedCluster2Events.AddClusterPodEvent(xdcrCluster2, "MemberRemoved", 2)
			expectedCluster2Events.AddClusterEvent(xdcrCluster2, "RebalanceCompleted")
		}
	default:
		t.Fatalf("Unsupported operation: %s", operationType)
	}

	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 20, 10*time.Minute)

	ValidateEvents(t, k8s1, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedCluster2Events)
}

// Generic test case for creating Xdcr clusters
// Can support 2 clusters within same K8S kube or
// different kube based on the kubeNames provided in kubeNameList
func CreateXdcrCluster(t *testing.T, k8s1, k8s2 *types.Cluster) {
	// Platform configuration.
	f := framework.Global

	// Static configuration.
	clusterSize := constants.Size3
	srcBucketName := "default"
	destBucketName := "default"
	cbUsername := "Administrator"
	cbPassword := "password"

	// Create the clusters.
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

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

	e2eutil.MustCreateDestClusterReference(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword)
	e2eutil.MustCreateXdcrBucketReplication(t, k8s1, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword, srcBucketName, destBucketName)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 10, 10*time.Minute)

	ValidateEvents(t, k8s1, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedCluster2Events)
}

// Generic testcase to run NodeDown test cases on kubeNames given by kubeNameList
// This can support bringing down a cluster node either during XDCR configration / post XDCR configuration
func ClusterNodeDownWithXdcr(t *testing.T, triggerDuring string, k8s1, k8s2 *types.Cluster) {
	// Platform configuration.
	f := framework.Global

	// Static configuration.
	xdcrCluster1Size := constants.Size5
	xdcrCluster2Size := constants.Size2
	srcBucketName := "default"
	destBucketName := "default"
	cbUsername := "Administrator"
	cbPassword := "password"

	// Create the clusters.
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, xdcrCluster1Size, constants.WithBucket, constants.AdminExposed)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, xdcrCluster2Size, constants.WithBucket, constants.AdminExposed)

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

	e2eutil.MustCreateDestClusterReference(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)

	nodeToKill := 1
	if triggerDuring == "duringXdcrSetup" {
		e2eutil.MustKillPodForMember(t, k8s1, xdcrCluster1, nodeToKill, true)
	}

	e2eutil.MustCreateXdcrBucketReplication(t, k8s1, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword, srcBucketName, destBucketName)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 10, 10*time.Minute)

	if triggerDuring == "afterXdcrSetup" {
		e2eutil.MustKillPodForMember(t, k8s1, xdcrCluster1, nodeToKill, true)
	}

	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustWaitClusterStatusHealthy(t, k8s1, xdcrCluster1, 5*time.Minute)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 20, 10*time.Minute)

	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberDown", nodeToKill)
	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "FailedOver", nodeToKill)
	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", 5)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
	expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "MemberRemoved", nodeToKill)
	expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")

	ValidateEvents(t, k8s1, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedCluster2Events)
}

// Generic testcase to run AddNode test cases on kubeNames given by kubeNameList
// This can support adding new node to the cluster either during XDCR configration / post XDCR configuration
func ClusterAddNodeWithXdcr(t *testing.T, triggerDuring string, k8s1, k8s2 *types.Cluster) {
	// Platform configuration.
	f := framework.Global

	// Static configuration.
	clusterSize := constants.Size1
	srcBucketName := "default"
	destBucketName := "default"
	cbUsername := "Administrator"
	cbPassword := "password"

	// Create the clusters.
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

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
		xdcrCluster1, err = e2eutil.ResizeCluster(t, service, clusterSize, k8s1, xdcrCluster1, 5*time.Minute)
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

	e2eutil.MustCreateDestClusterReference(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword)
	e2eutil.MustCreateXdcrBucketReplication(t, k8s1, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword, srcBucketName, destBucketName)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 10, 10*time.Minute)

	if triggerDuring == "afterXdcrSetup" {
		go resizeFunction()
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Failed to resize cluster: %s", err.Error())
	}

	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 20, 10*time.Minute)

	ValidateEvents(t, k8s1, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedCluster2Events)
}

// Generic testcase to kill the XDCR exposed service test cases on kubeNames given by kubeNameList
// This supports killing the xdcr port service either during XDCR configration / post XDCR configuration
func ClusterNodeXdcrServiceKill(t *testing.T, triggerDuring string, k8s1, k8s2 *types.Cluster) {
	// Platform configuration.
	f := framework.Global

	// Static configuration.
	clusterSize := constants.Size1
	srcBucketName := "default"
	destBucketName := "default"
	cbUsername := "Administrator"
	cbPassword := "password"

	// Create the clusters.
	xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)
	xdcrCluster2 := e2eutil.MustNewXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

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

	if triggerDuring == "duringXdcrSetup" {
		e2eutil.MustDeletePodServices(t, k8s1, xdcrCluster1)
	}

	e2eutil.MustCreateDestClusterReference(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword)
	e2eutil.MustCreateXdcrBucketReplication(t, k8s1, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword, srcBucketName, destBucketName)
	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 10, 10*time.Minute)

	if triggerDuring == "afterXdcrSetup" {
		e2eutil.MustDeletePodServices(t, k8s1, xdcrCluster1)
	}

	e2eutil.MustPopulateBucket(t, k8s1, xdcrCluster1, srcBucketName, 10)
	e2eutil.MustVerifyDocCountInBucket(t, k8s2, xdcrCluster2, destBucketName, 20, 10*time.Minute)

	ValidateEvents(t, k8s1, xdcrCluster1, expectedCluster1Events)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedCluster2Events)
}

// Create XDCR cluster within same k8s cluster
func TestXdcrCreateCluster(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	CreateXdcrCluster(t, k8s1, k8s2)
}

// Create cb clusters on top of TLS certificates
func TestXdcrCreateTlsCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)

	// Static configuration.
	clusterSize := constants.Size1
	srcBucketName := "default"
	destBucketName := "default"
	cbUsername := "Administrator"
	cbPassword := "password"

	tls1, teardown1 := e2eutil.MustInitClusterTLS(t, k8s1, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown1()
	tls2, teardown2 := e2eutil.MustInitClusterTLS(t, k8s2, f.Namespace, &e2eutil.TlsOpts{})
	defer teardown2()

	// Create the clusters.
	xdcrCluster1 := e2eutil.MustNewTlsXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden, tls1)
	xdcrCluster2 := e2eutil.MustNewTlsXdcrClusterBasic(t, k8s2, f.Namespace, clusterSize, constants.WithBucket, constants.AdminHidden, tls2)

	// Create the XDCR connection.  Ensure TLS is enabled.
	e2eutil.MustCreateDestClusterReference(t, k8s1, k8s2, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword)
	e2eutil.MustCreateXdcrBucketReplication(t, k8s1, xdcrCluster1, xdcrCluster2, cbUsername, cbPassword, srcBucketName, destBucketName)
	e2eutil.MustCheckClusterTLS(t, k8s1, f.Namespace, tls1)
	e2eutil.MustCheckClusterTLS(t, k8s1, f.Namespace, tls2)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonNodeServiceCreated}},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, k8s1, xdcrCluster1, expectedEvents)
	ValidateEvents(t, k8s2, xdcrCluster2, expectedEvents)
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
		k8s1 := f.GetCluster(0)

		// Cluster 1
		clusterSize := constants.Size2
		xdcrCluster1 := e2eutil.MustNewXdcrClusterBasic(t, k8s1, f.Namespace, clusterSize, constants.WithBucket, constants.AdminExposed)

		expectedCluster1Events := e2eutil.EventValidator{}
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "AdminConsoleServiceCreate")
		for memberIndex := 0; memberIndex < clusterSize; memberIndex++ {
			expectedCluster1Events.AddClusterPodEvent(xdcrCluster1, "AddNewMember", memberIndex)
		}
		expectedCluster1Events.AddClusterNodeServiceEvent(xdcrCluster1, "Create", api.AdminService, api.DataService, api.IndexService)
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceStarted")
		expectedCluster1Events.AddClusterEvent(xdcrCluster1, "RebalanceCompleted")
		expectedCluster1Events.AddClusterBucketEvent(xdcrCluster1, "Create", "default")

		k8s1Host, err := k8s1.APIHostname()
		if err != nil {
			t.Fatal(err)
		}

		// Deploy a stand alone cluster using the same host where K8S is running
		hostUrl := k8s1Host + ":" + xdcrCluster1.Status.AdminConsolePort
		destUrl := k8s1Host + ":8091"
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

		e2eutil.CreateDestClusterReference(t, hostUrl, k8s2,xdcrCluster2, cbUsername, cbPassword)

		if _, err := e2eutil.CreateXdcrBucketReplication(hostUrl, cbUsername, cbPassword, externalCbClusterName, srcBucketName, destBucketName, versionType); err != nil {
			t.Fatal(err)
		}

		// Validate bucket replication from K8S -> external cluster
		if _, err := e2eutil.PopulateBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 100, 1); err != nil {
			t.Fatal(err)
		}

		if err := e2eutil.VerifyDocCountInBucket(destUrl, destBucketName, cbUsername, cbPassword, 100, 10*time.Minute); err != nil {
			t.Fatal(err)
		}

			// Enable replication from External CB cluster -> K8S CB cluster
			xdcrRefList, err := e2eutil.GetXdcrClusterReferences(destUrl, cbUsername, cbPassword)
			if err != nil {
				t.Fatal(err)
			}

			for _, xdcrRef := range xdcrRefList {
				if err := e2eutil.DeleteXdcrClusterReferences(cbUsername, cbPassword, xdcrRef); err != nil {
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

			if err := e2eutil.VerifyDocCountInBucket(hostUrl, srcBucketName, cbUsername, cbPassword, 200, 10*time.Minute); err != nil {
				t.Fatal(err)
			}
		ValidateEvents(t, k8s1, xdcrCluster1, expectedCluster1Events)
	*/
}

// Create two clusters and while trying to configure XDCR,
// couchbase pod goes down
func TestXdcrNodeDownDuringSetupDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	ClusterNodeDownWithXdcr(t, "duringXdcrSetup", k8s1, k8s2)
}

// Create two clusters and XDCR is configured between the two clusters
// Couchbase pod goes down after setup is successful, replaced by new node
func TestXdcrNodeDownDuringSetupAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	ClusterNodeDownWithXdcr(t, "afterXdcrSetup", k8s1, k8s2)
}

// Create two clusters and while trying to configure XDCR,
// new couchbase pod is added to the existing cluster
func TestXdcrNodeAddDuringSetupDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	ClusterAddNodeWithXdcr(t, "duringXdcrSetup", k8s1, k8s2)
}

// Create two clusters and XDCR is configured between the two clusters
// New couchbase pod is added to the existing cluster
func TestXdcrNodeAddDuringSetupAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	ClusterAddNodeWithXdcr(t, "afterXdcrSetup", k8s1, k8s2)
}

// Create two clusters and while trying to configure XDCR,
// XDCR exposed node port service is killed
func TestXdcrNodeServiceKilledDuringConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	ClusterNodeXdcrServiceKill(t, "duringXdcrSetup", k8s1, k8s2)
}

// Create two clusters and XDCR is configured between the two clusters
// XDCR exposed node port service is killed after the replication is progress
func TestXdcrNodeServiceKilledAfterConfigure(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	ClusterNodeXdcrServiceKill(t, "afterXdcrSetup", k8s1, k8s2)
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, k8s1, k8s2, "source", "rebalanceOutNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are rebalanced out
// one by one until there is only one node in cluster
func TestXdcrRebalanceOutTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, k8s1, k8s2, "remote", "rebalanceOutNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the source bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, k8s1, k8s2, "source", "killNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes from the destination bucket cluster are killed one by one
// At the end all nodes are replaced by new nodes in the cluster
func TestXdcrRemoveTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, k8s1, k8s2, "remote", "killNodes")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of source bucket cluster is resized to single node cluster
func TestXdcrResizedOutSourceClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, k8s1, k8s2, "source", "resizeOut")
}

// Create two clusters and while trying to configure XDCR
// Cluster nodes of destination bucket cluster is resized to single node cluster
func TestXdcrResizedOutTargetClusterNodes(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}

	f := framework.Global
	k8s1 := f.GetCluster(0)
	k8s2 := f.GetCluster(1)
	XdcrClusterRemoveNode(t, k8s1, k8s2, "remote", "resizeOut")
}
