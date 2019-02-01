package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

func TestPodResourcesBasic(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(0.7 * maxMem))
	t.Logf("Mem Request: %s MB", memReq)
	memLimit := strconv.Itoa(int(0.8 * maxMem))
	t.Logf("Mem Limit: %s MB", memLimit)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"resourceMemRequest": memReq,
		"resourceMemLimit":   memLimit,
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=%sMB... \n Pod Policy Resource Memory Limit=%sMB... \n attempting to create 1 node cluster", memReq, memLimit)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, false)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestNegPodResourcesBasic(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(int(0.8 * maxMem))
	t.Logf("Mem Request: %s MB", memReq)
	memLimit := strconv.Itoa(int(0.7 * maxMem))
	t.Logf("Mem Limit: %s MB", memLimit)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"resourceMemRequest": memReq,
		"resourceMemLimit":   memLimit,
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=%sMB... \n Pod Policy Resource Memory Limit=%sMB... \n attempting to create 1 node cluster", memReq, memLimit)
	cluster, err := e2eutil.NewClusterMultiNoWait(t, targetKube, f.Namespace, configMap)
	if err != nil {
		t.Fatalf("failed to create cluster")
	}

	// Expect the cluster to enter a failed state
	if err := e2eutil.WaitClusterPhaseFailed(t, targetKube.CRClient, cluster, 10); err != nil {
		t.Fatalf("cluster failed to enter failed state")
	}
	t.Logf("Pod not placed")
}

func TestPodResourcesHigh(t *testing.T) {
	t.Skip("test not fully implemented...")
}

func TestPodResourcesLow(t *testing.T) {
	t.Skip("test not fully implemented...")
}

// TestPodResourcesCannotBePlaced tests for additional pods failing creation due to
// resource starvation.
// 1. Get the minimum memory on any node, set pods to reserve this amount
// 2. Calculate the maximum number of pods which can be allocated on the k8s cluster
// 3. Create a Couchbase cluster with this number of pods, saturating memory
// 4. Try scaling up by one pod
// 5. Expect to see an event indicating that pod creation failed
func TestPodResourcesCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	minMem := e2eutil.MustGetMinNodeMem(t, targetKube)
	reqMem := minMem * 0.9
	scaleNum := e2eutil.MustGetMaxScale(t, targetKube, reqMem)

	memReq := strconv.Itoa(int(reqMem))
	t.Logf("Mem Request: %s MB", memReq)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               strconv.Itoa(scaleNum),
		"name":               "test_config_1",
		"services":           "data",
		"resourceMemRequest": memReq,
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

	// Scale the cluster up to just below the memory threshold
	t.Logf("Pod Policy Resource Memory Request=%s MB...\n Cluster Capacity=%d  \n scaling until pods cannot be placed", memReq, scaleNum)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, false)

	// Add in a new node which should cause a memory allocation error
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, scaleNum+1, targetKube, testCouchbase)

	// Wait for the creation failure event to be raised
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, scaleNum), time.Minute)

	// Check the event stream is as expected
	expectedEvents := e2eutil.EventList{}
	for i := 0; i < scaleNum; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}
	if scaleNum > 1 {
		expectedEvents.AddRebalanceStartedEvent(testCouchbase)
		expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	}
	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, scaleNum)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestFirstNodePodResourcesCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	maxMem := e2eutil.MustGetMaxNodeMem(t, targetKube)
	memReq := strconv.Itoa(2 * int(maxMem))
	t.Logf("Mem Request: %s MB", memReq)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"resourceMemRequest": memReq,
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=%sMB... \n attempting to create 1 pod cluster with max allocatable memory of %dMB", memReq, int(maxMem))

	// Asynchronously create the cluster
	cluster, err := e2eutil.NewClusterMultiNoWait(t, targetKube, f.Namespace, configMap)
	if err != nil {
		t.Fatalf("failed to create cluster")
	}

	// Expect the cluster to enter a failed state
	if err := e2eutil.WaitClusterPhaseFailed(t, targetKube.CRClient, cluster, 10); err != nil {
		t.Fatalf("cluster failed to enter failed state")
	}
	t.Logf("Cluster failed, pod not scheduled")
}

func TestAntiAffinityOn(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	numNodes := e2eutil.MustNumNodes(t, targetKube)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":     strconv.Itoa(numNodes),
		"name":     "test_config_1",
		"services": "data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create %d pod cluster with %d nodes", numNodes, numNodes)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, false)

	expectedEvents := e2eutil.EventList{}
	for i := 0; i < numNodes; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	t.Logf("cluster created")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestAntiAffinityOnCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	numNodes := e2eutil.MustNumNodes(t, targetKube)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":     strconv.Itoa(numNodes + 1),
		"name":     "test_config_1",
		"services": "data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create %d pod cluster with %d nodes", numNodes+1, numNodes)
	testCouchbase, err := e2eutil.NewClusterMultiNoWait(t, targetKube, f.Namespace, configMap)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	expectedEvents := e2eutil.EventList{}

	var memberId int
	for memberId = 0; memberId < numNodes; memberId++ {
		e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberAddEvent(testCouchbase, memberId), 3*time.Minute)
		expectedEvents.AddMemberAddEvent(testCouchbase, memberId)
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, memberId), 5*time.Minute)

	t.Logf("Failed to add extra cluster node: %v", err)
	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, memberId)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestAntiAffinityOnCannotBeScaled(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	numNodes := e2eutil.MustNumNodes(t, targetKube)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":     strconv.Itoa(numNodes),
		"name":     "test_config_1",
		"services": "data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create %d pod cluster with %d nodes", numNodes, numNodes)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, false)
	t.Logf("Cluster created")

	expectedEvents := e2eutil.EventList{}
	for i := 0; i < numNodes; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}

	// For single node cluster
	if numNodes > 1 {
		expectedEvents.AddRebalanceStartedEvent(testCouchbase)
		expectedEvents.AddRebalanceCompletedEvent(testCouchbase)
	}

	t.Logf("Attempting to add a node")
	testCouchbase = e2eutil.MustResizeClusterNoWait(t, 0, numNodes+1, targetKube, testCouchbase)

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, e2eutil.NewMemberCreationFailedEvent(testCouchbase, numNodes), 5*time.Minute)
	expectedEvents.AddMemberCreationFailedEvent(testCouchbase, numNodes)
	t.Logf("Node not added")

	t.Logf("Reverting add")
	var err error
	testCouchbase, err = e2eutil.ResizeCluster(t, 0, numNodes, targetKube, testCouchbase, constants.Retries30)
	if err != nil {
		t.Fatalf("cluster failed to revert, fail: %v", err)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestAntiAffinityOff(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	numNodes := e2eutil.MustNumNodes(t, targetKube)
	scaleToNum := numNodes + 1
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":     strconv.Itoa(scaleToNum),
		"name":     "test_config_1",
		"services": "data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "off",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}

	t.Logf("AntiAffinity=off... \n attempting to create %d pod cluster with %d nodes", scaleToNum, numNodes)
	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, false)
	t.Logf("cluster created")

	expectedEvents := e2eutil.EventList{}
	for i := 0; i < scaleToNum; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	t.Logf("Attempting to add a node")
	var err error
	testCouchbase, err = e2eutil.ResizeCluster(t, 0, scaleToNum+1, targetKube, testCouchbase, constants.Retries30)
	if err != nil {
		t.Fatalf("cluster failed to scale to 5 nodes: %v", err)
	}
	t.Logf("Node added")

	expectedEvents.AddMemberAddEvent(testCouchbase, scaleToNum)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}
