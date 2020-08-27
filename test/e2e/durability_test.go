package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestCreateDurableBucket tests the framework and operator can make a bucket
// with durability enabled, and also the framework can observe it.
func TestCreateDurableBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	e2eutil.SkipVersionsBefore(t, framework.Global.CouchbaseServerImage, "6.6.0")
	skipEditBucket(t)

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	bucket := e2eutil.GetDurableBucket(f.BucketType, f.CompressionMode, couchbasev2.CouchbaseBucketMinimumDurabilityMajority)

	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, constants.Size1)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/DurabilityMinLevel", couchbaseutil.DurabilityMajority), time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestEditDurableBucket tests the operator can edit bucket durability settings.
func TestEditDurableBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	e2eutil.SkipVersionsBefore(t, framework.Global.CouchbaseServerImage, "6.6.0")
	skipEditBucket(t)

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	bucket := e2eutil.GetBucket(f.BucketType, f.CompressionMode)

	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, constants.Size1)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Add("/Spec/MinimumDurability", couchbasev2.CouchbaseBucketMinimumDurabilityMajority), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/DurabilityMinLevel", couchbaseutil.DurabilityMajority), time.Minute)

	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/Spec/MinimumDurability", couchbasev2.CouchbaseBucketMinimumDurabilityPersistToMajority), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/DurabilityMinLevel", couchbaseutil.DurabilityPersistToMajority), time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketEdited},
		},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestLoadDurableBucket(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	e2eutil.SkipVersionsBefore(t, framework.Global.CouchbaseServerImage, "6.6.0")
	skipEditBucket(t)

	// Static configuration.
	clusterSize := 3
	numOfDocs := 500

	// Create the cluster.
	bucket := e2eutil.GetDurableBucket(f.BucketType, f.CompressionMode, couchbasev2.CouchbaseBucketMinimumDurabilityMajority)

	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := e2eutil.MustNewClusterBasic(t, kubernetes, clusterSize)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	e2eutil.MustInsertJSONDocsIntoBucket(t, kubernetes, cluster, bucket.GetName(), 1, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
