package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	pkg_constants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
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
	targetKube := f.GetCluster(0)

	bucket1 := api.BucketConfig{
		BucketName:         "default1",
		BucketType:         pkg_constants.BucketTypeCouchbase,
		BucketMemoryQuota:  constants.Mem256Mb,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: constants.IndexReplicaEnabled,
	}
	bucket2 := api.BucketConfig{
		BucketName:        "default2",
		BucketType:        pkg_constants.BucketTypeMemcached,
		BucketMemoryQuota: constants.Mem256Mb,
		EnableFlush:       constants.BucketFlushDisabled,
	}
	bucket3 := api.BucketConfig{
		BucketName:         "default3",
		BucketType:         pkg_constants.BucketTypeEphemeral,
		BucketMemoryQuota:  101,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyNoEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionTimestamp,
		EnableFlush:        constants.BucketFlushEnabled,
	}
	bucket4 := api.BucketConfig{
		BucketName:         "default4",
		BucketType:         pkg_constants.BucketTypeEphemeral,
		BucketMemoryQuota:  101,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyNRUEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
	}
	bucketSettingsList := []api.BucketConfig{bucket1, bucket2, bucket3, bucket4}

	clusterConfig := e2eutil.BasicClusterConfig2
	serviceConfig1 := e2eutil.GetServiceConfigMap(3, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
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
		testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries10, updateFunc)
		if err != nil {
			t.Fatal(err)
		}

		buckets = append(buckets, bucketSetting.BucketName)

		t.Logf("Waiting For Bucket To Be Created \n")
		err = e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, buckets, constants.Retries20, testCouchbase)
		if err != nil {
			t.Logf("status: %v+", testCouchbase.Status)
			t.Fatalf("failed to create bucket %v", err)
		}

		expectedEvents.AddBucketCreateEvent(testCouchbase, bucketSetting.BucketName)

		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

		currentBuckets, err := client.GetBuckets()
		if err != nil && len(currentBuckets) != i+1 {
			t.Fatalf("failed to see all buckets from client")
		}
	}
	// delete all buckets
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = []api.BucketConfig{} }
	t.Logf("Removing Bucket From Cluster \n")
	testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries10, updateFunc)
	if err != nil {
		t.Fatal(err)
	}
	err = e2eutil.WaitUntilBucketsNotExists(t, targetKube.CRClient, []string{"default1", "default2", "default3", "default4"}, constants.Retries10, testCouchbase)
	if err != nil {
		t.Fatalf("failed to delete bucket %v", err)
	}

	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default1")
	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default2")
	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default3")
	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default4")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

	currentBuckets, err := client.GetBuckets()
	if err != nil && len(currentBuckets) != 0 {
		t.Fatalf("failed to see no buckets from client")
	}

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestBucketAddRemoveExtended(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	t.Logf("Creating New Couchbase Cluster...\n")
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size3, constants.WithoutBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddMemberAddEvent(testCouchbase, 1)
	expectedEvents.AddMemberAddEvent(testCouchbase, 2)
	expectedEvents.AddRebalanceStartedEvent(testCouchbase)
	expectedEvents.AddRebalanceCompletedEvent(testCouchbase)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
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
		testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries10, updateFunc)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("Waiting For Bucket To Be Created \n")
		err = e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{bucketSetting.BucketName}, constants.Retries10, testCouchbase)
		if err != nil {
			t.Fatalf("failed to create bucket %v", err)
		}

		expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

		currentBuckets, err := client.GetBuckets()
		if err != nil && len(currentBuckets) != 1 {
			t.Fatalf("failed to see all buckets from client")
		}

		ramQuota := strconv.Itoa(bucketSetting.BucketMemoryQuota)
		err = e2eutil.VerifyBucketInfo(t, client, constants.Retries5, bucketSetting.BucketName, "BucketMemoryQuota", ramQuota, e2eutil.BucketInfoVerifier)
		if err != nil {
			t.Fatalf("failed to verify default bucket ram quota: %v", err)
		}

		// delete bucket
		updateFunc = func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = []api.BucketConfig{} }
		t.Logf("Removing Bucket From Cluster \n")
		testCouchbase, err = e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries10, updateFunc)
		if err != nil {
			t.Fatal(err)
		}
		err = e2eutil.WaitUntilBucketsNotExists(t, targetKube.CRClient, []string{bucketSetting.BucketName}, constants.Retries10, testCouchbase)
		if err != nil {
			t.Fatalf("failed to delete bucket %v", err)
		}

		expectedEvents.AddBucketDeleteEvent(testCouchbase, "default")

		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

		currentBuckets, err = client.GetBuckets()
		if err != nil && len(currentBuckets) != 0 {
			t.Fatalf("failed to see no buckets from client")
		}
	}

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithoutBucket, constants.AdminHidden)

	bucketSettings := api.BucketConfig{
		BucketName:         "default",
		BucketType:         pkg_constants.BucketTypeEphemeral,
		BucketMemoryQuota:  constants.Mem256Mb,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyFullEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
		EnableIndexReplica: constants.IndexReplicaDisabled,
	}
	bucketConfig := []api.BucketConfig{bucketSettings}

	// add bucket
	t.Logf("Desired Bucket Properties: %v\n", bucketConfig)
	updateFunc := func(cl *api.CouchbaseCluster) { cl.Spec.BucketSettings = bucketConfig }
	t.Logf("Adding Bucket To Cluster \n")
	testCouchbase, err := e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, updateFunc)
	if err == nil {
		t.Fatal("successful update to bucket with invalid eviction settings")
	}

	expectedEvents := e2eutil.EventList{}
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

// TestEditBucket tests modifying various bucket parameters and reverting them.
func TestEditBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	kubernetes := f.GetCluster(0)

	// Constants
	bucketName := "default"
	enabled := true
	disabled := false

	// Create the cluster.
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

	// Create a direct connection to a couchbase node.
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, kubernetes, cluster)
	defer cleanup()

	// When healthy change the memory quota, replicas, whether flushes are allowed and the compression mode.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, constants.Retries10)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketMemoryQuota", 128), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", 128), constants.Retries5)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketMemoryQuota", 256), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", 256), constants.Retries5)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketReplicas", 2), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 2), constants.Retries5)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketReplicas", 1), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), constants.Retries5)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EnableFlush", disabled), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", &disabled), constants.Retries5)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EnableFlush", enabled), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", &enabled), constants.Retries5)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/CompressionMode", cbmgr.CompressionModeActive), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModeActive), constants.Retries5)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/CompressionMode", cbmgr.CompressionModeOff), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModeOff), constants.Retries5)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/CompressionMode", cbmgr.CompressionModePassive), constants.Retries5)
	e2eutil.MustPatchBucketInfo(t, client, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModePassive), constants.Retries5)

	// Avoid a race where Couchbase has been updated but the event not raise yet.
	time.Sleep(10 * time.Second)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 9, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited}},
	}

	ValidateEvents(t, kubernetes, f.Namespace, cluster.Name, expectedEvents)
}

// attempt to change bucket type to ephemeral
func TestNegBucketEdit(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
	if err != nil {
		t.Fatalf("failed to get cluster info %v", err)
	}
	t.Logf("cluster info: %v", clusterInfo)

	// edit bucket type
	updateFunc := func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketType = "ephemeral"
	}

	if _, err := e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, updateFunc); err == nil {
		t.Fatal("successful update to bucket type")
	}

	// edit memory quota
	updateFunc = func(cl *api.CouchbaseCluster) {
		cl.Spec.BucketSettings[0].BucketMemoryQuota = 9999
	}

	if _, err := e2eutil.UpdateCluster(targetKube.CRClient, testCouchbase, constants.Retries5, updateFunc); err == nil {
		t.Fatal("successful update to bucket memory quota")
	}

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}

func TestRevertExternalBucketAdd(t *testing.T) {
	if os.Getenv(envParallelTest) == envParallelTestTrue {
		t.Parallel()
	}
	f := framework.Global
	targetKube := f.GetCluster(0)

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithoutBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
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
	err = e2eutil.VerifyBucketInfo(t, client, constants.Retries5, "default", "BucketMemoryQuota", "101", e2eutil.BucketInfoVerifier)
	if err != nil {
		t.Fatalf("failed to verify create default bucket: %v", err)
	}
	err = e2eutil.VerifyBucketDeleted(t, client, constants.Retries10, "default")
	if err != nil {
		t.Fatalf("failed to delete bucket %v", err)
	}

	expectedEvents.AddBucketDeleteEvent(testCouchbase, "default")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
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
	targetKube := f.GetCluster(0)

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

	expectedEvents := e2eutil.EventList{}
	expectedEvents.AddAdminConsoleSvcCreateEvent(testCouchbase)
	expectedEvents.AddMemberAddEvent(testCouchbase, 0)
	expectedEvents.AddBucketCreateEvent(testCouchbase, "default")

	// create connection to couchbase nodes
	client, cleanup := e2eutil.CreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	clusterInfo, err := e2eutil.GetClusterInfo(t, client, constants.Retries5)
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

	if err := e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{"default"}, constants.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to create default bucket with flush enabled %v", err)
	}

	err = e2eutil.VerifyBucketInfo(t, client, constants.Retries5, "default", "EnableFlush", "true", e2eutil.BucketInfoVerifier)
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
	err = e2eutil.EditBucketAndVerify(t, client, bucket, constants.Retries5, e2eutil.FlushDisabledVerifier)
	if err != nil {
		t.Fatalf("error occurred editing cluster bucket %v", err)
	}

	if _, allowed := err.(cberrors.ErrInvalidBucketParamChange); allowed {
		t.Fatalf("failed to prevent changing bucket type: %v", err)
	}

	// verify that the operator has reverted the change
	// and re-enabled bucket flush
	event := k8sutil.BucketEditEvent("default", testCouchbase)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, 30)
	if err := e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{"default"}, constants.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to enable bucket flush %v", err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// Bucket edits can be consumed by a prior reconcile interestingly due to
	// a race somewhere in Couchbase server, so ensure we wait for things to
	// settle
	time.Sleep(5 * time.Second)

	// make a bucket spec with bucket replicas = 3
	t.Logf("externally changing bucket replicas to: 3")
	bucket, err = e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		b.BucketReplicas = pkg_constants.BucketReplicasThree
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	// edit bucket and verify change is reflected in cluster.
	err = e2eutil.EditBucketAndVerify(t, client, bucket, constants.Retries5, e2eutil.ThreeReplicaVerifier)
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
			if bucket.BucketReplicas == 1 {
				return true
			}
		}
		return false
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, 30)
	if err := e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{"default"}, constants.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to revert bucket replicas to 1 %v", err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	// Bucket edits can be consumed by a prior reconcile interestingly due to
	// a race somewhere in Couchbase server, so ensure we wait for things to
	// settle
	time.Sleep(5 * time.Second)

	// make a bucket spec with io priority = "low"
	t.Logf("externally changing bucket io priority to: low")
	bucket, err = e2eutil.SpecToApiBucket("default", testCouchbase, func(b *api.BucketConfig) {
		b.IoPriority = pkg_constants.BucketIoPriorityLow
	})
	if err != nil {
		t.Fatalf("error occurred converting bucket spec %v", err)
	}

	// edit bucket and verify change is reflected in cluster.
	err = e2eutil.EditBucketAndVerify(t, client, bucket, constants.Retries5, e2eutil.DefaultIoPriorityVerifier)
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
			if bucket.IoPriority == "high" {
				return true
			}
		}
		return false
	}

	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, event, 30)
	if err := e2eutil.WaitUntilBucketsExists(t, targetKube.CRClient, []string{"default"}, constants.Retries5, testCouchbase, acceptsBucketFunc); err != nil {
		t.Fatalf("failed to revert bucket io prioritys to high %v", err)
	}

	expectedEvents.AddBucketEditEvent(testCouchbase, "default")

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, constants.Retries10)

	// Event checking
	ValidateClusterEvents(t, targetKube, testCouchbase.Name, f.Namespace, expectedEvents)
}
