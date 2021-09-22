package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

func TestFTSCreateCluster(t *testing.T) {
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

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.SearchService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// insert docs in the bucket
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, 2*time.Minute)

	// CB cluster instance
	host := e2eutil.MustGetCBInstance(t, kubernetes, cluster)

	// Index Manager.
	searchManager := host.SearchIndexes()
	searchIndex := e2eutil.NewFTSIndex(bucket.GetName())

	query := `SELECT COUNT(*) FROM default USE INDEX(USING FTS) where key1="dummyVal1";`

	e2eutil.MustExecuteFTSOps(t, searchIndex, searchManager, host, query)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestFTSResizeCluster(t *testing.T) {
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

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.SearchService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// insert docs in the bucket
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, 2*time.Minute)

	host := e2eutil.MustGetCBInstance(t, kubernetes, cluster)

	searchManager := host.SearchIndexes()
	searchIndex := e2eutil.NewFTSIndex(bucket.GetName())

	query := `SELECT COUNT(*) FROM default USE INDEX(USING FTS) where key1="dummyVal1";`

	e2eutil.MustExecuteFTSOps(t, searchIndex, searchManager, host, query)

	stop := e2eutil.MustGenerateWorkload(t, kubernetes, cluster, f.CouchbaseServerImage, sourceBucket.Name)
	defer stop()

	for _, newClusterSize := range []int{2, 3, 2} {
		cluster = e2eutil.MustResizeCluster(t, 0, newClusterSize, kubernetes, cluster, 20*time.Minute)
	}

	stop()

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     2,
			Validator: e2eutil.ClusterScaleUpSequence(1)},
		e2eutil.ClusterScaleDownSequence(1),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestFTSWithScopesAndCollections(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	numOfDocs := f.DocsCount

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Servers[0].Services = append(cluster.Spec.Servers[0].Services, couchbasev2.SearchService)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

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

	host := e2eutil.MustGetCBInstance(t, kubernetes, cluster)

	// Instance to manage FTS.
	searchManager := host.SearchIndexes()
	searchIndex := e2eutil.NewFTSIndexWithCollections(bucket.GetName(), scopeName, collectionName)

	query := "SELECT COUNT(*) FROM `default`" + "." + scopeName + "." + collectionName + ` USE INDEX(USING FTS) where key1="dummyVal1";`

	// create FTS Index and execute a query against it.
	e2eutil.MustExecuteFTSOps(t, searchIndex, searchManager, host, query)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
