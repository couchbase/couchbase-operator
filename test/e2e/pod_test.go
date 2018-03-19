package e2e

import (
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"os"
	"strconv"
	"testing"
)

//Assume each node has 7.6GB of memory
func TestPodResourcesBasic(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"dataPath":           "/opt/couchbase/var/lib/couchbase/data",
		"indexPath":          "/opt/couchbase/var/lib/couchbase/data",
		"resourceMemRequest": "400",
		"resourceMemLimit":   "500",
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=0.4GB... \n Pod Policy Resource Memory Limit=0.5GB... \n attempting to create 2 node cluster")
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
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"dataPath":           "/opt/couchbase/var/lib/couchbase/data",
		"indexPath":          "/opt/couchbase/var/lib/couchbase/data",
		"resourceMemRequest": "500",
		"resourceMemLimit":   "400",
	}

	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=0.5GB... \n Pod Policy Resource Memory Limit=0.4GB... \n attempting to create 1 node cluster")
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
		expectedEvents.AddRebalanceEvent(testCouchbase)

		t.Logf("Pods placed: %v", clusterSize)
	}
	actualSize := clusterSize - 1

	t.Logf("Cluster size: %v", actualSize)

	// TODO calculate cluster memory total and use to assert correct cluster size
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

//Assume each node has 7.6GB of memory
func TestFirstNodePodResourcesCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := map[string]string{
		"size":               "1",
		"name":               "test_config_1",
		"services":           "data",
		"dataPath":           "/opt/couchbase/var/lib/couchbase/data",
		"indexPath":          "/opt/couchbase/var/lib/couchbase/data",
		"resourceMemRequest": "16000",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
	}
	t.Logf("Pod Policy Resource Memory Request=16GB... \n attempting to create 1 pod cluster with 3 nodes of 8GB")
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

		t.Fatalf("16GB request placed on node with 8GB, fail: %v", err)
	}
	t.Logf("Pod not placed")
}

//this test will assume a 3 node kubernetes cluster
func TestAntiAffinityOn(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceThreeDataNode
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create 3 pod cluster with 3 nodes")
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	t.Logf("cluster created")

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
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

//this test will assume a 3 node kubernetes cluster
func TestAntiAffinityOnCannotBePlaced(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFourDataNode
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create 4 pod cluster with 3 nodes")
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err == nil {
		defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
		t.Fatalf("cluster created despite anti affinity status... fail: %v", err)
	}
	t.Logf("Cluster creation failed, anti affinty works")
}

//this test will assume a 3 node kubernetes cluster
func TestAntiAffinityOnCannotBeScaled(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceThreeDataNode
	otherConfig1 := map[string]string{
		"antiAffinity": "on",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=on... \n attempting to create 3 pod cluster with 3 nodes")
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	t.Logf("Cluster created")

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	t.Logf("Attempting to add a node")
	err = e2eutil.ResizeCluster(t, 0, 4, f.CRClient, testCouchbase)
	if err == nil {
		t.Fatalf("cluster scaled to 4 pods on 3 nodes, fail: %v", err)
	}
	t.Logf("Node not added")

	t.Logf("Reverting add")
	err = e2eutil.ResizeCluster(t, 0, 3, f.CRClient, testCouchbase)
	if err != nil {
		t.Fatalf("cluster failed to revert, fail: %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
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

//this test will assume a 3 node kubernetes cluster
func TestAntiAffinityOff(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	clusterConfig := e2eutil.BasicClusterConfig
	serviceConfig1 := e2eutil.BasicServiceFourDataNode
	otherConfig1 := map[string]string{
		"antiAffinity": "off",
	}
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1,
		"other1":   otherConfig1,
	}
	t.Logf("AntiAffinity=off... \n attempting to create 4 pod cluster with 3 nodes")
	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, "basic-test-secret", configMap, false)
	if err != nil {
		t.Fatalf("cluster creation failed: %v", err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir, testCouchbase)
	t.Logf("cluster created")

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddMemberAddEvent(testCouchbase, 3)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	t.Logf("Attempting to add a node")
	err = e2eutil.ResizeCluster(t, 0, 5, f.CRClient, testCouchbase)
	if err != nil {
		t.Fatalf("cluster failed to scale to 5 nodes: %v", err)
	}
	t.Logf("Node added")

	expectedEvents.AddMemberAddEvent(testCouchbase, 4)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size5, e2eutil.Retries10)
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
