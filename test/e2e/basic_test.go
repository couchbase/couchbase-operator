package e2e

import (
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// Tests creation of a 3 node cluster with 0 buckets
// 1. Create a 3 node Couchbase cluster with no buckets.
// 2. Check the events to make sure the operator took the correct actions.
// 3. Verifies that the cluster is balanced and all data is available.
func TestCreateCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, targetKube.Namespace, clusterSize)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// Tests creation of a 3 node cluster with 1 bucket
// 1. Create a 3 node Couchbase cluster with 1 bucket.
// 2. Check the events to make sure the operator took the correct actions.
// 3. Verifies that the cluster is balanced and all data is available.
func TestCreateBucketCluster(t *testing.T) {
	// Plaform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	e2eutil.MustNewBucket(t, targetKube, targetKube.Namespace, e2espec.DefaultBucket)
	testCouchbase := e2eutil.MustNewClusterBasic(t, targetKube, targetKube.Namespace, clusterSize)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}
