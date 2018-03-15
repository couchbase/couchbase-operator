package e2e

import (
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"strconv"
	"testing"
)

func TestPodResourcesBasic(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	maxMem, err := e2eutil.GetMaxNodeMem(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get max node memory: %s", err)
	}
	memReq := strconv.Itoa(int(0.7 * maxMem))
	t.Logf("Mem Request: %s MB", memReq)
	memLimit := strconv.Itoa(int(0.8 * maxMem))
	t.Logf("Mem Limit: %s MB", memLimit)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"dataPath":           "/opt/couchbase/var/lib/couchbase/data",
		"indexPath":          "/opt/couchbase/var/lib/couchbase/data",
		"resourceMemRequest": memReq,
		"resourceMemLimit":   memLimit,
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=%sMB... \n Pod Policy Resource Memory Limit=%sMB... \n attempting to create 1 node cluster", memReq, memLimit)
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("failed to place first pod: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestNegPodResourcesBasic(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	maxMem, err := e2eutil.GetMaxNodeMem(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get max node memory: %s", err)
	}
	memReq := strconv.Itoa(int(0.8 * maxMem))
	t.Logf("Mem Request: %s MB", memReq)
	memLimit := strconv.Itoa(int(0.7 * maxMem))
	t.Logf("Mem Limit: %s MB", memLimit)

	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"dataPath":           "/opt/couchbase/var/lib/couchbase/data",
		"indexPath":          "/opt/couchbase/var/lib/couchbase/data",
		"resourceMemRequest": memReq,
		"resourceMemLimit":   memLimit,
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=%sMB... \n Pod Policy Resource Memory Limit=%sMB... \n attempting to create 1 node cluster", memReq, memLimit)
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

		t.Fatalf("pod placed with invalid resource list, fail: %v", err)
	}
	t.Logf("Pod not placed")
}

func TestPodResourcesHigh(t *testing.T) {
	t.Skip("test not fully implemented...")
}

func TestPodResourcesLow(t *testing.T) {
	t.Skip("test not fully implemented...")
}

func TestPodResourcesCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	minMem, err := e2eutil.GetMinNodeMem(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get min node memory: %s", err)
	}
	reqMem := minMem * 0.9
	scaleNum, err := e2eutil.GetMaxScale(f.KubeClient, reqMem)
	if err != nil {
		t.Fatalf("failed to get max scale: %s", err)
	}

	memReq := strconv.Itoa(int(reqMem))
	t.Logf("Mem Request: %d MB", memReq)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"dataPath":           "/opt/couchbase/var/lib/couchbase/data",
		"indexPath":          "/opt/couchbase/var/lib/couchbase/data",
		"resourceMemRequest": memReq,
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}

	t.Logf("Pod Policy Resource Memory Request=%d MB...\n Cluster Capacity=%d  \n scaling until pods cannot be placed", memReq, scaleNum)
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("failed to place first pod: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	clusterSize := 1
	for err == nil {
		clusterSize = clusterSize + 1
		err = e2eutil.ResizeCluster(t, 0, clusterSize, f.CRClient, testCouchbase)
		if err != nil {
			t.Logf("failed to place pod: %v", clusterSize)
			break
		}

		expectedEvents.AddMemberAddEvent(testCouchbase, clusterSize-1)
		expectedEvents.AddRebalanceStartedEvent(testCouchbase)
		expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

		t.Logf("Pods placed: %v", clusterSize)
	}
	actualSize := clusterSize - 1

	t.Logf("Cluster size: %v", actualSize)

	if actualSize != scaleNum {
		t.Fatalf("failed to saturate cluster memory: %v", err)
	}

	t.Logf("Cluster memory saturated with cluster size: %v", actualSize)

	if scaleNum != 1 {
		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, actualSize, e2eutil.Retries5)
		if err != nil {
			t.Fatalf("failed to see healthy and balanced cluster: %v", err)
		}
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestFirstNodePodResourcesCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	maxMem, err := e2eutil.GetMaxNodeMem(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get max node memory: %s", err)
	}
	memReq := strconv.Itoa(2 * int(maxMem))
	t.Logf("Mem Request: %s MB", memReq)
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"dataPath":           "/opt/couchbase/var/lib/couchbase/data",
		"indexPath":          "/opt/couchbase/var/lib/couchbase/data",
		"resourceMemRequest": memReq,
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=%sMB... \n attempting to create 1 pod cluster with max allocatable memory of %dMB", memReq, int(maxMem))
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
		t.Fatalf("%dMB request placed on node with max allocatable memory of %dMB, fail: %v", memReq, int(maxMem), err)
	}
	t.Logf("Pod not placed")
}

func TestAntiAffinityOn(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	numNodes, err := e2eutil.NumK8Workers(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get number of kubernetes nodes: %v", err)
	}
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":      strconv.Itoa(numNodes),
		"name":      "test_config_1",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create %d pod cluster with %d nodes", numNodes, numNodes)
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	for i := 0; i < numNodes; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	t.Logf("cluster created")

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, numNodes, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestAntiAffinityOnCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	numNodes, err := e2eutil.NumK8Workers(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get number of kubernetes nodes: %v", err)
	}
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":      strconv.Itoa(numNodes + 1),
		"name":      "test_config_1",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create %d pod cluster with %d nodes", numNodes + 1, numNodes)
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
		t.Fatalf("cluster created despite anti affinity status... fail: %v", err)
	}
	t.Logf("Cluster creation failed, anti affinty works")
}

func TestAntiAffinityOnCannotBeScaled(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	numNodes, err := e2eutil.NumK8Workers(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get number of kubernetes nodes: %v", err)
	}
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":      strconv.Itoa(numNodes),
		"name":      "test_config_1",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create %d pod cluster with %d nodes", numNodes, numNodes)
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	t.Logf("Cluster created")

	expectedEvents := e2eutil.EventList{}
	for i := 0; i < numNodes; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	t.Logf("Attempting to add a node")
	err = e2eutil.ResizeCluster(t, 0, numNodes + 1, f.CRClient, testCouchbase)
	if err == nil {
		t.Fatalf("cluster scaled to %d pods on %d nodes, fail: %v", numNodes + 1, numNodes, err)
	}
	t.Logf("Node not added")

	t.Logf("Reverting add")
	err = e2eutil.ResizeCluster(t, 0, numNodes, f.CRClient, testCouchbase)
	if err != nil {
		t.Fatalf("cluster failed to revert, fail: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, numNodes, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestAntiAffinityOff(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	numNodes, err := e2eutil.NumK8Workers(f.KubeClient)
	if err != nil {
		t.Fatalf("failed to get number of kubernetes nodes: %v", err)
	}
	scaleToNum := numNodes + 1
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":      strconv.Itoa(scaleToNum),
		"name":      "test_config_1",
		"services":  "data",
		"dataPath":  "/opt/couchbase/var/lib/couchbase/data",
		"indexPath": "/opt/couchbase/var/lib/couchbase/data"}
	otherConfig1 := map[string]string{
		"antiAffinity": "off",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}

	t.Logf("AntiAffinity=off... \n attempting to create %s pod cluster with %s nodes", scaleToNum, numNodes)
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	t.Logf("cluster created")

	expectedEvents := e2eutil.EventList{}
	for i := 0; i < scaleToNum; i++ {
		expectedEvents.AddMemberAddEvent(testCouchbase, i)
	}
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	t.Logf("Attempting to add a node")
	err = e2eutil.ResizeCluster(t, 0, scaleToNum+1, f.CRClient, testCouchbase)
	if err != nil {
		t.Fatalf("cluster failed to scale to 5 nodes: %v", err)
	}
	t.Logf("Node added")

	expectedEvents.AddMemberAddEvent(testCouchbase, scaleToNum)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, scaleToNum + 1, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}
