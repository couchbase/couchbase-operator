package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestDataSynchronizationBasic is a very basic smoke test of synchronization.  It creates a
// bucket, scope, and collection, does a synchronization and expects those resources to still
// be there after we switch to managed.
func TestDataSynchronizationBasic(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

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
	e2eutil.MustCreateBucketManually(t, kubernetes, cluster, bucketName)
	e2eutil.MustCreateScopeManually(t, kubernetes, cluster, bucketName, scopeName)
	e2eutil.MustCreateCollectionManually(t, kubernetes, cluster, bucketName, scopeName, collectionName)

	// Start synchronization.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/selector", labelSelector).Add("/spec/buckets/synchronize", true), time.Minute)

	// Wait for completion.
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionSynchronized, corev1.ConditionTrue, cluster, time.Minute)

	// Go managed.
	e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/buckets/managed", true), time.Minute)

	// Check things are alive still.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection().WithScopes(scopeName).WithCollection(collectionName)
	e2eutil.MustAssertScopesAndCollectionsFor(t, kubernetes, cluster, bucketName, expected, time.Minute)
}
