package e2e

import (
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

func TestViewsCreateCluster(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3
	numOfDocs := f.DocsCount

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// insert docs in the bucket
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, 2*time.Minute)

	host, sdkCleanup := e2eutil.MustGetCouchbaseClientSDK(t, kubernetes, cluster)
	defer sdkCleanup()

	// Design Doc definition.
	designDoc := e2eutil.NewViewsDesignDoc()

	// Views Manager.
	viewManager := host.Bucket(bucket.GetName()).ViewIndexes()

	// create design docs.
	e2eutil.MustUpsertViewsDesignDocs(t, designDoc, viewManager)

	// drop design docs.
	e2eutil.MustDropViewsDesignDocs(t, designDoc, viewManager)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestViewsResizeCluster(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1
	numOfDocs := f.DocsCount

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// insert docs in the bucket
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, 2*time.Minute)

	host, sdkCleanup := e2eutil.MustGetCouchbaseClientSDK(t, kubernetes, cluster)
	defer sdkCleanup()

	// Design Doc definition.
	designDoc := e2eutil.NewViewsDesignDoc()

	// Views Manager.
	viewManager := host.Bucket(bucket.GetName()).ViewIndexes()

	// create design docs.
	e2eutil.MustUpsertViewsDesignDocs(t, designDoc, viewManager)

	stop := e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, sourceBucket.Name)
	defer stop()

	for _, newClusterSize := range []int{2, 3, 2} {
		cluster = e2eutil.MustResizeCluster(t, 0, newClusterSize, kubernetes, cluster, 20*time.Minute)
	}

	// drop design docs.
	e2eutil.MustDropViewsDesignDocs(t, designDoc, viewManager)

	stop()

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     2,
			Validator: e2eutil.ClusterScaleUpSequence(1),
		},
		e2eutil.ClusterScaleDownSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestViewsWithScopesAndCollections(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	// Static configuration.
	clusterSize := 1
	numOfDocs := f.DocsCount

	scopeName := "pinky"
	collectionName := "brain"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs in the created scope.collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName, numOfDocs, 10*time.Minute)

	// insert docs in the default_scope.default_collection.
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 2*numOfDocs, 5*time.Minute)

	host, sdkCleanup := e2eutil.MustGetCouchbaseClientSDK(t, kubernetes, cluster)
	defer sdkCleanup()

	// Design Doc definition.
	designDoc := e2eutil.NewViewsDesignDoc()

	// Views Manager.
	viewManager := host.Bucket(bucket.GetName()).ViewIndexes()

	// create design docs.
	e2eutil.MustUpsertViewsDesignDocs(t, designDoc, viewManager)

	// drop design docs.
	e2eutil.MustDropViewsDesignDocs(t, designDoc, viewManager)
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
