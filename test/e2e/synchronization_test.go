/*
Copyright 2022-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestDataSynchronizationBasic is a very basic smoke test of synchronization.  It creates a
// bucket, scope, and collection, does a synchronization and expects those resources to still
// be there, along with the default scope and collection, after we switch to managed.
func TestDataSynchronizationBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := 1
	bucketName := "pale"
	scopeName := "telescope"
	collectionName := "copernicus"
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"foo": "bar",
		},
	}

	// Create a cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.Managed = false
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Add some data topology.
	e2eutil.NewBucket(f.BucketType).MustCreateManually(t, kubernetes, cluster, bucketName)
	e2eutil.MustCreateScopeManually(t, kubernetes, cluster, bucketName, scopeName)
	e2eutil.MustCreateCollectionManually(t, kubernetes, cluster, bucketName, scopeName, collectionName)

	// Start synchronization.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/selector", labelSelector).Add("/spec/buckets/synchronize", true), time.Minute)

	// Wait for completion.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, 3*time.Minute)

	// Go managed.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", true), time.Minute)

	// Check things are alive still.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection().WithScopes(scopeName).WithCollection(collectionName)
	e2eutil.MustAssertScopesAndCollectionsFor(t, kubernetes, cluster, bucketName, expected, 3*time.Minute)
}

// TestDataSyncronizationUpdate sets up a managed cluster, disables managed, externally
// modifies topology, synchronizes, returns to managed, and checks that the new topology
// has been successfully brought in to managed.
func TestDataSynchronizationUpdate(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName1 := "brain"
	collectionName2 := "mindy"
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"foo": "bar",
		},
	}

	// Create a collection and scope.
	collection := e2eutil.NewCollection(collectionName1).MustCreate(t, kubernetes)
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.NewBucket(f.BucketType).WithScopes(scope).MustCreate(t, kubernetes)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection().WithScope(scopeName).WithCollection(collectionName1)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Go unmanaged.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", false), time.Minute)
	time.Sleep(30 * time.Second)

	// Externally add a collection.
	e2eutil.MustCreateCollectionManually(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName2)

	// Start synchronization.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/selector", labelSelector).Add("/spec/buckets/synchronize", true), time.Minute)

	// Wait for completion.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, 3*time.Minute)

	// Go managed.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", true), time.Minute)

	// Check things are alive still.
	expected = e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection().WithScope(scopeName).WithCollections(collectionName1, collectionName2)
	e2eutil.MustAssertScopesAndCollectionsFor(t, kubernetes, cluster, bucket.GetName(), expected, 3*time.Minute)
}

func testDataSynchronizationBucketConfig(t *testing.T, bucket *e2eutil.Bucket) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0")

	// Static configuration.
	clusterSize := 3
	bucketName := "pale"
	labelSelectorString := "foo=bar"
	labelSelector := metav1.LabelSelector{
		MatchLabels:      map[string]string{"foo": "bar"},
		MatchExpressions: nil,
	}

	// Create a cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.Managed = false
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Add some data topology.
	expectedBucket := bucket.MustCreateManually(t, kubernetes, cluster, bucketName)

	// Start synchronization.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/selector", labelSelector).Add("/spec/buckets/synchronize", true), time.Minute)

	// Wait for completion.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, 3*time.Minute)

	// Go managed.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", true), time.Minute)

	// Check that the expected and actual buckets are the same.
	var actualBucket interface{}

	switch bucket.GetBucketType() {
	case e2eutil.BucketTypeCouchbase:
		actualBucket = e2eutil.MustRetrieveCouchbaseBucketByLabel(t, kubernetes, labelSelectorString)
	case e2eutil.BucketTypeEphemeral:
		actualBucket = e2eutil.MustRetrieveEphemeralBucketByLabel(t, kubernetes, labelSelectorString)
	case e2eutil.BucketTypeMemcached:
		actualBucket = e2eutil.MustRetrieveMemcachedBucketByLabel(t, kubernetes, labelSelectorString)
	}

	e2eutil.MustAssertBucket(t, actualBucket, expectedBucket)
}

// TestDataSynchronizationCouchbaseBucketConfig ensures that the sync tool correctly
// picks up and sets the various options available for couchbase bucket configuration.
func TestDataSynchronizationCouchbaseBucketConfig(t *testing.T) {
	bucketType := e2eutil.BucketTypeCouchbase
	memoryQuota := 200
	replicas := 2
	ioPrio := couchbasev2.CouchbaseBucketIOPriorityHigh
	eviction := "fullEviction"
	resolution := couchbasev2.CouchbaseBucketConflictResolutionTimestamp
	compression := couchbasev2.CouchbaseBucketCompressionModeActive
	durability := couchbaseutil.DurabilityMajority
	ttl := 600

	manualBucket := e2eutil.NewBucket(bucketType).WithMemoryQuota(memoryQuota).WithReplicas(replicas).WithIOPriority(ioPrio).WithEvictionPolicy(eviction).WithConflictResolution(resolution).WithFlush().WithIndexReplica().WithCompressionMode(compression).WithDurability(durability).WithTTL(ttl)

	testDataSynchronizationBucketConfig(t, manualBucket)
}

// TestDataSynchronizationEphemeralBucketConfig ensures that the sync tool correctly
// picks up and sets the various options available for ephemeral bucket configuration.
func TestDataSynchronizationEphemeralBucketConfig(t *testing.T) {
	bucketType := e2eutil.BucketTypeEphemeral
	memoryQuota := 200
	replicas := 2
	ioPrio := couchbasev2.CouchbaseBucketIOPriorityHigh
	eviction := "nruEviction"
	resolution := couchbasev2.CouchbaseBucketConflictResolutionTimestamp
	compression := couchbasev2.CouchbaseBucketCompressionModeActive
	durability := couchbaseutil.DurabilityMajority
	ttl := 600

	manualBucket := e2eutil.NewBucket(bucketType).WithMemoryQuota(memoryQuota).WithReplicas(replicas).WithIOPriority(ioPrio).WithEvictionPolicy(eviction).WithConflictResolution(resolution).WithFlush().WithCompressionMode(compression).WithDurability(durability).WithTTL(ttl)

	testDataSynchronizationBucketConfig(t, manualBucket)
}

// TestDataSynchronizationMemcachedBucketConfig ensures that the sync tool correctly
// picks up and sets the few options available for memcached bucket configuration.
func TestDataSynchronizationMemcachedBucketConfig(t *testing.T) {
	bucketType := e2eutil.BucketTypeMemcached
	memoryQuota := 200

	manualBucket := e2eutil.NewBucket(bucketType).WithMemoryQuota(memoryQuota).WithFlush()

	testDataSynchronizationBucketConfig(t, manualBucket)
}

// TestDataSynchronizationDefaultCollectionDeleted checks that the default collection is
// not re-created through a synchronization, should it have been deleted beforehand.
func TestDataSynchronizationDefaultCollectionDeleted(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := 1
	bucketName := "pale"
	scopeName := "telescope"
	collectionName := "copernicus"
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"foo": "bar",
		},
	}

	// Create a cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.Managed = false
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Add some data topology.
	e2eutil.NewBucket(f.BucketType).MustCreateManually(t, kubernetes, cluster, bucketName)
	e2eutil.MustCreateScopeManually(t, kubernetes, cluster, bucketName, scopeName)
	e2eutil.MustCreateCollectionManually(t, kubernetes, cluster, bucketName, scopeName, collectionName)

	// Delete the default collection
	e2eutil.MustDeleteCollectionManually(t, kubernetes, cluster, bucketName, couchbasev2.DefaultScopeOrCollection, couchbasev2.DefaultScopeOrCollection)

	// Start synchronization.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/selector", labelSelector).Add("/spec/buckets/synchronize", true), time.Minute)

	// Wait for completion.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, 3*time.Minute)

	// Go managed.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", true), time.Minute)

	// Check things are alive still - and the default collection hasn't been reanimated.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScope().WithScopes(scopeName).WithCollection(collectionName)
	e2eutil.MustAssertScopesAndCollectionsFor(t, kubernetes, cluster, bucketName, expected, 3*time.Minute)
}

// TestDataSynchronizationErrorTopologyChange ensures that, should the topology be changed,
// the synchronization fails.
func TestDataSynchronizationErrorTopologyChange(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := 1
	bucketName := "pale"
	scopeName := "telescope"
	collectionName := "copernicus"
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"foo": "bar",
		},
	}

	// Create a cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.Managed = false
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Add some data topology.
	e2eutil.NewBucket(f.BucketType).MustCreateManually(t, kubernetes, cluster, bucketName)
	e2eutil.MustCreateScopeManually(t, kubernetes, cluster, bucketName, scopeName)
	e2eutil.MustCreateCollectionManually(t, kubernetes, cluster, bucketName, scopeName, collectionName)

	// Start synchronization.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/selector", labelSelector).Add("/spec/buckets/synchronize", true), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, 3*time.Minute)

	// Edit the bucket, expecting failure.
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucketName, jsonpatch.NewPatchSet().Add("/BucketMemoryQuota", int64(256)), time.Minute)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionFalse, cluster, time.Minute)
	e2eutil.AssertClusterConditionFor(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionFalse, cluster, time.Minute)
}

// TestDataSynchronizationOperatorRestart restarts the operator mid-sync, checking that the
// synchronization is successful regardless.
func TestDataSynchronizationOperatorRestart(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := 1
	bucketName := "pale"
	scopeName := "telescope"
	collectionName := "copernicus"
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"foo": "bar",
		},
	}

	// Create a cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Buckets.Managed = false
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Add some data topology.
	e2eutil.NewBucket(f.BucketType).MustCreateManually(t, kubernetes, cluster, bucketName)
	e2eutil.MustCreateScopeManually(t, kubernetes, cluster, bucketName, scopeName)
	e2eutil.MustCreateCollectionManually(t, kubernetes, cluster, bucketName, scopeName, collectionName)

	// Start synchronization, and kill the operator.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/selector", labelSelector).Add("/spec/buckets/synchronize", true), time.Minute)
	e2eutil.MustKillOperatorAndWaitForRecovery(t, kubernetes)

	// Wait for completion.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, 3*time.Minute)

	// Go managed, and kill the operator (again!).
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", true), time.Minute)
	e2eutil.MustKillOperatorAndWaitForRecovery(t, kubernetes)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, time.Minute)

	// Check things are alive still.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection().WithScopes(scopeName).WithCollection(collectionName)
	e2eutil.MustAssertScopesAndCollectionsFor(t, kubernetes, cluster, bucketName, expected, 3*time.Minute)
}
