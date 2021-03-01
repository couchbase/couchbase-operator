package e2e

import (
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestBucketTTL tests a bucket created with a TTL can create documents, and they eventually
// are deleted when the exipry period is up.
func TestBucketTTL(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// This actually means don't run for memcached buckets...
	skipEditBucket(t)

	// Static configuration.
	clusterSize := 1
	numOfDocs := f.DocsCount

	// Create the cluster
	bucket := e2eutil.GetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustSetBucketTTL(t, bucket, time.Minute)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Insert some docs into the bucket and verify, then expect them to be deleted
	// in a minute.
	e2eutil.MustInsertJSONDocsIntoBucket(t, kubernetes, cluster, bucket.GetName(), 0, numOfDocs)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// In order to expire a document it must be read, compacted, or expired.  It's easiest to
	// just compact the bucket manually.
	time.Sleep(2 * time.Minute)
	e2eutil.MustCompactBucket(t, kubernetes, cluster, bucket)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 0, time.Minute)
}

// TestBucketTTLUpdate test creating a bucket, adding a document exipry, updaing it and finally
// removing it.
func TestBucketTTLUpdate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// This actually means don't run for memcached buckets...
	skipEditBucket(t)

	// Static configuration.
	clusterSize := 1

	// Create the cluster
	bucket := e2eutil.GetBucket(f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Add TTL and verify, update the TTL and verify, delete the TTL and verify.
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Add("/spec/maxTTL", &metav1.Duration{Duration: time.Minute}), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/MaxTTL", 60), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Replace("/spec/maxTTL", &metav1.Duration{Duration: time.Hour}), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/MaxTTL", 3600), time.Minute)
	bucket = e2eutil.MustPatchBucket(t, kubernetes, bucket, jsonpatch.NewPatchSet().Remove("/spec/maxTTL"), time.Minute)
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, bucket.GetName(), jsonpatch.NewPatchSet().Test("/MaxTTL", 0), time.Minute)
}
