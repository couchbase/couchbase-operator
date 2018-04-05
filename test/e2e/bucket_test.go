package e2e

import (
	"os"
	"strconv"
	"testing"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/gocbmgr"
)

func TestBucketAddRemoveBasic(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	bucket1 := api.BucketConfig{
		BucketName:         "default1",
		BucketType:         constants.BucketTypeCouchbase,
		BucketMemoryQuota:  256,
		BucketReplicas:     &constants.BucketReplicasOne,
		IoPriority:         &constants.BucketIoPriorityHigh,
		EvictionPolicy:     &constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: &constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: &constants.BucketIndexReplicasEnabled,
	}
	bucket2 := api.BucketConfig{
		BucketName:        "default2",
		BucketType:        constants.BucketTypeMemcached,
		BucketMemoryQuota: 256,
		EnableFlush:       constants.BucketFlushDisabled,
	}
	bucket3 := api.BucketConfig{
		BucketName:         "default3",
		BucketType:         constants.BucketTypeEphemeral,
		BucketMemoryQuota:  101,
		BucketReplicas:     &constants.BucketReplicasOne,
		IoPriority:         &constants.BucketIoPriorityHigh,
		EvictionPolicy:     &constants.BucketEvictionPolicyNoEviction,
		ConflictResolution: &constants.BucketConflictResolutionTimestamp,
		EnableFlush:        constants.BucketFlushEnabled,
	}
	bucket4 := api.BucketConfig{
		BucketName:         "default4",
		BucketType:         constants.BucketTypeEphemeral,
		BucketMemoryQuota:  101,
		BucketReplicas:     &constants.BucketReplicasOne,
		IoPriority:         &constants.BucketIoPriorityHigh,
		EvictionPolicy:     &constants.BucketEvictionPolicyNRUEviction,
		ConflictResolution: &constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
	}
	bucketSettingsList := []api.BucketConfig{bucket1, bucket2, bucket3, bucket4}

	clusterConfig := e2eutil.BasicClusterConfig2
	serviceConfig1 := e2eutil.BasicServiceThreeDataNode
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase, err := e2eutil.NewClusterMulti(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, configMap, e2eutil.AdminExposed)
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	bucketConfigs := []api.BucketConfig{}
	buckets := []string{}

	for i, bucketSetting := range bucketSettingsList {
		bucketConfigs = append(bucketConfigs, bucketSetting)

		// add bucket
		t.Logf("Desired Bucket Properties: %v\n", bucketSetting)
		t.Logf("Spec Buckets: %v\n", bucketConfigs)
		updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfigs }
		t.Logf("Adding Bucket To Cluster \n")
		testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries10, updateFunc)
		if err != nil {
			t.Fatal(err)
		}

		buckets = append(buckets, bucketSetting.BucketName)

		t.Logf("Waiting For Bucket To Be Created \n")
		err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, buckets, e2eutil.Retries20, testCouchbase)
		if err != nil {
			t.Logf("status: %v+", testCouchbase.Status)
			t.Fatalf("failed to create bucket %v", err)
		}

		expectedEvents.AddBucketCreateEvent(testCouchbase, bucketSetting.BucketName)

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
		if err != nil {
			t.Fatal(err.Error())
		}

		currentBuckets, err := client.GetBuckets()
		if err != nil && len(currentBuckets) != i+1 {
			t.Fatalf("failed to see all buckets from client")
		}
	}
	// delete all buckets
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = []api.BucketConfig{} }
	t.Logf("Removing Bucket From Cluster \n")
	testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.WaitUntilBucketsNotExists(t, f.CRClient, []string{"default1", "default2", "default3", "default4"}, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to delete bucket %v", err)
	}

	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default1")
	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default2")
	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default3")
	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default4")

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	currentBuckets, err := client.GetBuckets()
	if err != nil && len(currentBuckets) != 0 {
		t.Fatalf("failed to see no buckets from client")
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

func TestBucketAddRemoveExtended(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size3, e2eutil.WithoutBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceEvent(testCouchbase)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	bucketTypes := []string{"couchbase", "memcached", "ephemeral"}
	bucketSettingsList := e2espec.GenerateValidBucketSettings(bucketTypes)
	for _, bucketSetting := range bucketSettingsList {
		newConfig := []api.BucketConfig{bucketSetting}

		// add bucket
		t.Logf("Desired Bucket Properties: %v\n", newConfig)
		updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = newConfig }
		t.Logf("Adding Bucket To Cluster \n")
		testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries10, updateFunc)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("Waiting For Bucket To Be Created \n")
		err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{bucketSetting.BucketName}, e2eutil.Retries10, testCouchbase)
		if err != nil {
			t.Fatalf("failed to create bucket %v", err)
		}

		expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
		if err != nil {
			t.Fatal(err.Error())
		}

		currentBuckets, err := client.GetBuckets()
		if err != nil && len(currentBuckets) != 1 {
			t.Fatalf("failed to see all buckets from client")
		}

		ramQuota := strconv.Itoa(bucketSetting.BucketMemoryQuota)
		err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, bucketSetting.BucketName, "BucketMemoryQuota", ramQuota, e2eutil.BucketInfoVerifier)
		if err != nil {
			t.Fatalf("failed to verify default bucket ram quota: %v", err)
		}

		// delete bucket
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = []api.BucketConfig{} }
		t.Logf("Removing Bucket From Cluster \n")
		testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries10, updateFunc)
		if err != nil {
			t.Fatal(err)
		}
		err = e2eutil.WaitUntilBucketsNotExists(t, f.CRClient, []string{bucketSetting.BucketName}, e2eutil.Retries10, testCouchbase)
		if err != nil {
			t.Fatalf("failed to delete bucket %v", err)
		}

		expectedEvents.AddBucketDeleteEvent(testCouchbase, "default")

		err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size3, e2eutil.Retries10)
		if err != nil {
			t.Fatal(err.Error())
		}

		currentBuckets, err = client.GetBuckets()
		if err != nil && len(currentBuckets) != 0 {
			t.Fatalf("failed to see no buckets from client")
		}
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Logf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Creates a cluster and adds a bucket with an invalid configuration
// 1. Create a single node cluster with no buckets
// 2. Creates an ephemeral bucket with replicaIndexes enabled (replicaIndexes
//    are not supported on ephemeral buckets so creation should fail)
// 3. Wait for 90 seconds to ensure that the bucket is not created
// 4. (TODO) Check to make sure that an error is reported in the conditions section
func TestNegBucketAdd(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithoutBucket, e2eutil.AdminHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	bucketSettings := api.BucketConfig{
		BucketName:         "default",
		BucketType:         constants.BucketTypeEphemeral,
		BucketMemoryQuota:  256,
		BucketReplicas:     &constants.BucketReplicasOne,
		IoPriority:         &constants.BucketIoPriorityHigh,
		EvictionPolicy:     &constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: &constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: &constants.BucketIndexReplicasDisabled,
	}
	bucketConfig := []api.BucketConfig{bucketSettings}

	// add bucket
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig)
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase, err = e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries5, updateFunc)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Waiting For Bucket To Be Created \n")
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{bucketSettings.BucketName}, e2eutil.Retries10, testCouchbase)
	if err == nil {
		t.Fatalf("failed to NOT create bucket %v", err)
	}

	message := "[evictionPolicy - Eviction policy must be either 'noEviction' or 'nruEviction' for ephemeral buckets replicaNumber - Warning: you do not have enough data servers to support this number of replicas."
	err = e2eutil.WaitForConditionMessage(t, f.CRClient, 10, testCouchbase, api.ClusterConditionManageBuckets, message)
	if err != nil {
		t.Fatalf("failed to verify condition: %v", err)
	}

	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Fatalf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}

// Tests editing a bucket by changing various bucket parameters
// 1. Create a one node cluster with one bucket
// 2. Change the bucket memory quota to 128MB
// 3. Verify that the memory quota was in the describe output and in the actual cluster
// 4. Change the bucket memory quota back to 256MB
// 5. Change the bucket replicas to 2
// 6. Verify that the bucket replicas was in the describe output and in the actual cluster
// 7. Change the bucket replicas back to 1
// 8. Change enable flush to true
// 9. Verify that the memory quota was in the describe output and in the actual cluster
// 10. Change enable flush back to false
func TestEditBucket(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	t.Logf("Waiting For Bucket To Be Created \n")
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to create bucket %v", err)
	}

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// change memory quota
	_, err = e2eutil.UpdateBucketSpec("default", "BucketMemoryQuota", "128", f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketMemoryQuota == 128
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket ram quota %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketMemoryQuota", "128", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}

	// change memory quota back
	_, err = e2eutil.UpdateBucketSpec("default", "BucketMemoryQuota", "256", f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketMemoryQuota == 256
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket ram quota back %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketMemoryQuota", "256", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket ram quota: %v", err)
	}

	// change replica count
	_, err = e2eutil.UpdateBucketSpec("default", "BucketReplicas", "2", f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return *bucket.BucketReplicas == 2
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket replica count %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketReplicas", "2", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket replica count: %v", err)
	}

	// change replica count back
	_, err = e2eutil.UpdateBucketSpec("default", "BucketReplicas", "1", f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return *bucket.BucketReplicas == 1
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket replcia count back %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketReplicas", "1", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket replcia count back: %v", err)
	}

	// change eviction policy
	_, err = e2eutil.UpdateBucketSpec("default", "EnableFlush", "false", f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.EnableFlush == constants.BucketFlushDisabled
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket flush policy %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "EnableFlush", "false", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket flush policy: %v", err)
	}

	// change eviction policy back
	_, err = e2eutil.UpdateBucketSpec("default", "EnableFlush", "true", f.CRClient, testCouchbase, e2eutil.Retries5)
	if err != nil {
		t.Fatal(err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// verify
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.EnableFlush == constants.BucketFlushEnabled
		}
		return false
	}
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries10, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to change default bucket flush policy back %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "EnableFlush", "true", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify default bucket flush policy back: %v", err)
	}

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

// attempt to change bucket type to ephemeral
func TestNegBucketEdit(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// edit bucket type
	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketType = "ephemeral"
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries5, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	// verify type did not change
	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketType == "ephemeral"
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc)
	if _, allowed := err.(cberrors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketType", "ephemeral", e2eutil.BucketInfoVerifier)
	if err == nil {
		t.Fatalf("failed to verify prevent changing bucket type: %v", err)
	}

	message := "Bucket: default cannot change (default) bucket param='type' from 'couchbase' to 'ephemeral'"
	err = e2eutil.WaitForConditionMessage(t, f.CRClient, 10, testCouchbase, api.ClusterConditionManageBuckets, message)
	if err != nil {
		t.Fatalf("failed to verify condition: %v", err)
	}

	// revert edit bucket type
	t.Logf("reverting bucket type edit")
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketType = "couchbase"
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries5, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketType == "couchbase"
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc)
	if err != nil {
		t.Fatalf("failed to revert failed bucket edit: %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketType", "membase", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify bucket type: %v", err)
	}

	// edit memory quota
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketMemoryQuota = 9999
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries5, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	// verify type did not change
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketMemoryQuota == 9999
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc)
	if _, allowed := err.(cberrors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket memory quota: %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketMemoryQuota", "9999", e2eutil.BucketInfoVerifier)
	if err == nil {
		t.Fatalf("failed to verify prevent changing bucket memory quota: %v", err)
	}

	message = "ramQuotaMB - RAM quota specified is too large to be provisioned into this cluster"
	err = e2eutil.WaitForConditionMessage(t, f.CRClient, 20, testCouchbase, api.ClusterConditionManageBuckets, message)
	if err != nil {
		t.Fatalf("failed to verify condition: %v", err)
	}

	// revert edit memory quota
	t.Logf("reverting memory quota edit")
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketMemoryQuota = 256
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries5, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return bucket.BucketMemoryQuota == 256
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc)
	if err != nil {
		t.Fatalf("failed to revert bucket edit: %c", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketMemoryQuota", "256", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify bucket: %v", err)
	}

	// edit conflict resolution
	timestamp := "timestamp"
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].ConflictResolution = &timestamp
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries5, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return *bucket.ConflictResolution == timestamp
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc)
	if _, allowed := err.(cberrors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing conflict resolution: %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "ConflictResolution", "timestamp", e2eutil.BucketInfoVerifier)
	if err == nil {
		t.Fatalf("failed to verify prevent changing conflict resolution: %v", err)
	}

	message = "Bucket: default cannot change (default) bucket param='conflictResolution' from 'seqno' to 'timestamp'"
	err = e2eutil.WaitForConditionMessage(t, f.CRClient, 10, testCouchbase, api.ClusterConditionManageBuckets, message)
	if err != nil {
		t.Fatalf("failed to verify condition: %v", err)
	}

	// revert edit conflict resolution
	t.Logf("reverting conflict resoltuion edit")
	seqno := "seqno"
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].ConflictResolution = &seqno
	}

	if _, err := e2eutil.UpdateCluster(f.CRClient, testCouchbase, e2eutil.Retries5, updateFunc); err != nil {
		t.Fatalf("failed to post updated cluster spec: %v", err)
	}

	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			return *bucket.ConflictResolution == seqno
		}
		return false
	}
	err = e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc)
	if err != nil {
		t.Fatalf("failed to revert bucket edit: %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "ConflictResolution", "seqno", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify bucket: %v", err)
	}

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

func TestRevertExternalBucketAdd(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithoutBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	fullEvictionPolicy := "fullEviction"
	timestampConflictResolution := "lww"
	indexReplicaOff := false
	disableFlush := false
	newBucket := cbmgr.Bucket{
		BucketName:         "default",
		BucketType:         "couchbase",
		BucketMemoryQuota:  101,
		BucketReplicas:     0,
		EvictionPolicy:     &fullEvictionPolicy,
		ConflictResolution: &timestampConflictResolution,
		EnableFlush:        &disableFlush,
		EnableIndexReplica: &indexReplicaOff,
	}

	err = client.CreateBucket(&newBucket)
	if err != nil {
		t.Fatalf("failed to create bucket externally: %v", err)
	}
	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "BucketMemoryQuota", "101", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify create default bucket: %v", err)
	}
	err = e2eutil.VerifyBucketDeleted(t, client, e2eutil.Retries10, "default")
	if err != nil {
		t.Fatalf("failed to delete bucket %v", err)
	}

	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default")

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

// Tests that the operator reverts bucket edits not made by the operator
// 1. Create a one node cluster with one bucket
// 2. Create a node port service so we can access the cluster externally
// 3. Use an external client to update the bucket flush enabled parameter to false
// 4. Verify that the operator reverts the change
// 5. Use an external client to update the bucket replicas parameter to 3
// 6. Verify that the operator reverts the change
// 7. Use an external client to update the bucket IO priority parameter to default
// 8. Verify that the operator reverts the change
// 9. Check the events to make sure the operator took the correct actions
func TestRevertExternalBucketUpdates(t *testing.T) {

	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	testCouchbase, err := e2eutil.NewClusterBasic(t, f.KubeClient, f.CRClient, f.Namespace, f.DefaultSecret.Name, e2eutil.Size1, e2eutil.WithBucket, e2eutil.AdminExposed)
	if err != nil {
		t.Fatal(err)
	}
	defer e2eutil.CleanUpCluster(t, f.KubeClient, f.CRClient, f.Namespace, f.LogDir)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase, testCouchbase.Name+"-ui")
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	consoleURL, err := e2eutil.AdminConsoleURL(f.ApiServerHost(), testCouchbase.Status.AdminConsolePort)
	if err != nil {
		t.Fatalf("failed to get cluster url %v", err)
	}
	client, err := e2eutil.NewClient(t, f.KubeClient, testCouchbase, []string{consoleURL})
	if err != nil {
		t.Fatalf("failed to create cluster client %v", err)
	}
	clusterInfo, err := e2eutil.GetClusterInfo(t, client, e2eutil.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// bucket should exist with flush enabled
	acceptsBucketFunc := func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			t.Logf("enabled bucket flush: %t", bucket.EnableFlush)
			return bucket.EnableFlush == constants.BucketFlushEnabled
		}
		return false
	}

	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to create default bucket with flush enabled %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, e2eutil.Retries5, "default", "EnableFlush", "true", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify create default bucket with flush enabled: %v", err)
	}

	// make a bucket spec with flush disabled
	t.Logf("externally changing bucket flush to: false")
	bucket, err := e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		b.EnableFlush = false
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	// edit bucket and verify change is reflected in cluster.
	err = e2eutil.EditBucketAndVerify(t, client, bucket, e2eutil.Retries5, e2eutil.FlushDisabledVerifier)
	if err != nil {
		t.Fatalf("error occurred editing cluster bucket %v", err)
	}

	if _, allowed := err.(cberrors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	// verify that the operator has reverted the change
	// and re-enabled bucket flush
	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to enable bucket flush %v", err)
	}

	// make a bucket spec with bucket replicas = 3
	t.Logf("externally changing bucket replicas to: 3")
	bucket, err = e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		b.BucketReplicas = &constants.BucketReplicasThree
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	// edit bucket and verify change is reflected in cluster.
	err = e2eutil.EditBucketAndVerify(t, client, bucket, e2eutil.Retries5, e2eutil.ThreeReplicaVerifier)
	if err != nil {
		t.Fatalf("error occurred editing cluster bucket %v", err)
	}

	if _, allowed := err.(cberrors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	// verify that the operator has reverted the change
	// and reverted bucket replicas to 1
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			t.Logf("bucket replicas: %v", bucket.BucketReplicas)
			if *bucket.BucketReplicas == 1 {
				return true
			}
		}
		return false
	}

	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to revert bucket replicas to 1 %v", err)
	}

	// make a bucket spec with io priority = "default"
	t.Logf("externally changing bucket io priority to: default")
	bucket, err = e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		b.IoPriority = &constants.BucketIoPriorityLow
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	// edit bucket and verify change is reflected in cluster.
	err = e2eutil.EditBucketAndVerify(t, client, bucket, e2eutil.Retries5, e2eutil.DefaultIoPriorityVerifier)
	if err != nil {
		t.Fatalf("error occurred editing cluster bucket %v", err)
	}

	if _, allowed := err.(cberrors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	// verify that the operator has reverted the change
	// and reverted io priority to "high"
	acceptsBucketFunc = func(c *api.CouchbaseCluster) bool {
		if bucket, ok := c.Status.Buckets["default"]; ok {
			t.Logf("io priority: %v", bucket.IoPriority)
			if *bucket.IoPriority == "high" {
				return true
			}
		}
		return false
	}

	if err := e2eutil.WaitUntilBucketsExists(t, f.CRClient, []string{"default"}, e2eutil.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to revert bucket io prioritys to high %v", err)
	}

	err = e2eutil.WaitClusterStatusHealthy(t, f.CRClient, testCouchbase.Name, f.Namespace, e2eutil.Size1, e2eutil.Retries10)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Event checking
	events, err := e2eutil.GetCouchbaseEvents(f.KubeClient, testCouchbase.Name, f.Namespace)
	if err != nil {
		t.Fatalf("failed to get coucbase cluster events: %v", err)
	}
	if !expectedEvents.Compare(events) {
		t.Logf(e2eutil.EventListCompareFailedString(expectedEvents, events))
	}
}
