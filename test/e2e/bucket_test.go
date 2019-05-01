package e2e

import (
	"testing"
	"time"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
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
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration
	clusterSize := 3

	bucket1 := couchbasev1.BucketConfig{
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
	bucket2 := couchbasev1.BucketConfig{
		BucketName:        "default2",
		BucketType:        pkg_constants.BucketTypeMemcached,
		BucketMemoryQuota: constants.Mem256Mb,
		EnableFlush:       constants.BucketFlushDisabled,
	}
	bucket3 := couchbasev1.BucketConfig{
		BucketName:         "default3",
		BucketType:         pkg_constants.BucketTypeEphemeral,
		BucketMemoryQuota:  101,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyNoEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionTimestamp,
		EnableFlush:        constants.BucketFlushEnabled,
	}
	bucket4 := couchbasev1.BucketConfig{
		BucketName:         "default4",
		BucketType:         pkg_constants.BucketTypeEphemeral,
		BucketMemoryQuota:  101,
		BucketReplicas:     pkg_constants.BucketReplicasOne,
		IoPriority:         pkg_constants.BucketIoPriorityHigh,
		EvictionPolicy:     pkg_constants.BucketEvictionPolicyNRUEviction,
		ConflictResolution: pkg_constants.BucketConflictResolutionSeqno,
		EnableFlush:        constants.BucketFlushEnabled,
	}
	bucketSettingsList := []couchbasev1.BucketConfig{bucket1, bucket2, bucket3, bucket4}

	clusterConfig := e2eutil.BasicClusterConfig2
	serviceConfig1 := e2eutil.GetServiceConfigMap(clusterSize, "test_config_1", []string{"data"})
	configMap := map[string]map[string]string{
		"cluster":  clusterConfig,
		"service1": serviceConfig1}

	testCouchbase := e2eutil.MustNewClusterMulti(t, targetKube, f.Namespace, configMap, constants.AdminHidden)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.MustCreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	bucketConfigs := []couchbasev1.BucketConfig{}
	buckets := []string{}

	for i, bucketSetting := range bucketSettingsList {
		bucketConfigs = append(bucketConfigs, bucketSetting)

		// add bucket
		t.Logf("Desired Bucket Properties: %v\n", bucketSetting)
		testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings", bucketConfigs), time.Minute)

		buckets = append(buckets, bucketSetting.BucketName)

		t.Logf("Waiting For Bucket To Be Created \n")
		e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, buckets, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

		currentBuckets, err := client.GetBuckets()
		if err != nil && len(currentBuckets) != i+1 {
			t.Fatalf("failed to see all buckets from client")
		}
	}
	// delete all buckets
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings"), time.Minute)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket1.BucketName, 2*time.Minute)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket2.BucketName, 2*time.Minute)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket3.BucketName, 2*time.Minute)
	e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucket4.BucketName, 2*time.Minute)

	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	currentBuckets, err := client.GetBuckets()
	if err != nil && len(currentBuckets) != 0 {
		t.Fatalf("failed to see no buckets from client")
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets added seqentially
	// * Buckets removed
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: len(bucketSettingsList), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated}},
		eventschema.Repeat{Times: len(bucketSettingsList), Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted}},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

func TestBucketAddRemoveExtended(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, clusterSize, constants.WithoutBucket, constants.AdminHidden)

	// create connection to couchbase nodes
	client, cleanup := e2eutil.MustCreateAdminConsoleClient(t, targetKube, testCouchbase)
	defer cleanup()

	bucketTypes := []string{"couchbase", "memcached", "ephemeral"}
	bucketSettingsList := e2espec.GenerateValidBucketSettings(bucketTypes)
	for _, bucketSetting := range bucketSettingsList {
		newConfig := []couchbasev1.BucketConfig{bucketSetting}

		// add bucket
		testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings", newConfig), time.Minute)
		e2eutil.MustWaitUntilBucketsExists(t, targetKube, testCouchbase, []string{bucketSetting.BucketName}, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

		currentBuckets, err := client.GetBuckets()
		if err != nil && len(currentBuckets) != 1 {
			t.Fatalf("failed to see all buckets from client")
		}

		e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, "default", jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", bucketSetting.BucketMemoryQuota), time.Minute)

		// delete bucket
		testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/Spec/BucketSettings"), time.Minute)
		e2eutil.MustWaitUntilBucketNotExists(t, targetKube, testCouchbase, bucketSetting.BucketName, 2*time.Minute)
		e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

		currentBuckets, err = client.GetBuckets()
		if err != nil && len(currentBuckets) != 0 {
			t.Fatalf("failed to see no buckets from client")
		}
	}

	// Check the events match what we expect:
	// * Cluster created
	// * Buckets added then removed
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{
			Times: len(bucketSettingsList),
			Validator: eventschema.Sequence{
				Validators: []eventschema.Validatable{
					eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
					eventschema.Event{Reason: k8sutil.EventReasonBucketDeleted},
				},
			},
		},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
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
	// When healthy change the memory quota, replicas, whether flushes are allowed and the compression mode.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketMemoryQuota", 128), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", 128), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketMemoryQuota", 256), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketMemoryQuota", 256), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketReplicas", 2), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 2), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/BucketReplicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EnableFlush", disabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", &disabled), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/EnableFlush", enabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", &enabled), time.Minute)

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/CompressionMode", cbmgr.CompressionModeActive), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModeActive), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/CompressionMode", cbmgr.CompressionModeOff), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModeOff), time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/Spec/BucketSettings/0/CompressionMode", cbmgr.CompressionModePassive), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Test("/CompressionMode", cbmgr.CompressionModePassive), time.Minute)

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

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
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
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	bucketName := "default"
	enabled := true
	disabled := false

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, f.Namespace, constants.Size1, constants.WithBucket, constants.AdminExposed)

	// Once ready, alter a few parameters and ensure they are reverted by the operator.
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/EnableFlush", &disabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", &disabled), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent("default", testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/EnableFlush", &enabled), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/BucketReplicas", 3), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 3), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent("default", testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/BucketReplicas", 1), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Replace("/IoPriority", cbmgr.IoPriorityTypeLow), time.Minute)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/IoPriority", cbmgr.IoPriorityTypeLow), time.Minute)
	e2eutil.MustWaitForClusterEvent(t, targetKube, testCouchbase, k8sutil.BucketEditEvent("default", testCouchbase), 30*time.Second)
	e2eutil.MustPatchBucketInfo(t, targetKube, testCouchbase, bucketName, jsonpatch.NewPatchSet().Test("/IoPriority", cbmgr.IoPriorityTypeHigh), time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, targetKube, testCouchbase, 2*time.Minute)

	// Check the events match what we expect:
	// * Admin console service created
	// * Cluster created
	// * Bucket edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonServiceCreated},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{Times: 3, Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited}},
	}

	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
