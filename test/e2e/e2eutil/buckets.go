package e2eutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BucketType defines the bucket type for the bucket builder.
type BucketType string

const (
	// BucketTypeCouchbase is the base bucket type, that does all the things.
	BucketTypeCouchbase BucketType = "couchbase"

	// BucketTypeEphemeral is the same as above with no backing storage other than memory.
	BucketTypeEphemeral BucketType = "ephemeral"

	// BucketTypeMemcached no one uses...
	BucketTypeMemcached BucketType = "memcached"
)

// Bucket is an abstract builder type used to concisely create bucket resources.
type Bucket struct {
	// kind is the type of bucket to create.
	kind BucketType

	// compresisonMode is the compression mode to use.
	// If not specified this defaults to "passive".
	compressionMode couchbasev2.CouchbaseBucketCompressionMode

	// flush allows the bucket to be flushed.
	flush bool
}

// NewBucket creates a bucket with any required parameters.
func NewBucket(kind BucketType) *Bucket {
	return &Bucket{
		kind: kind,
	}
}

// WithCompressionMode allows the bucket's compression mode to be specified.
func (b *Bucket) WithCompressionMode(compressionMode couchbasev2.CouchbaseBucketCompressionMode) *Bucket {
	b.compressionMode = compressionMode

	return b
}

// WithFlush allows the bucket to be flushed.
func (b *Bucket) WithFlush() *Bucket {
	b.flush = true

	return b
}

// MustCreate takes the abstract bucket definition and creates it in Kubernetes, returning the
// concrete resource type.
func (b *Bucket) MustCreate(t *testing.T, kubernetes *types.Cluster) metav1.Object {
	generateName := "bucket-"

	switch b.kind {
	case BucketTypeCouchbase:
		bucket := &couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: generateName,
			},
			Spec: couchbasev2.CouchbaseBucketSpec{
				EnableFlush: b.flush,
			},
		}

		if b.compressionMode != "" {
			bucket.Spec.CompressionMode = b.compressionMode
		}

		newBucket, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBuckets(kubernetes.Namespace).Create(context.Background(), bucket, metav1.CreateOptions{})
		if err != nil {
			Die(t, err)
		}

		return newBucket
	case BucketTypeEphemeral:
		bucket := &couchbasev2.CouchbaseEphemeralBucket{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: generateName,
			},
			Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
				EnableFlush: b.flush,
			},
		}

		if b.compressionMode != "" {
			bucket.Spec.CompressionMode = b.compressionMode
		}

		newBucket, err := kubernetes.CRClient.CouchbaseV2().CouchbaseEphemeralBuckets(kubernetes.Namespace).Create(context.Background(), bucket, metav1.CreateOptions{})
		if err != nil {
			Die(t, err)
		}

		return newBucket
	case BucketTypeMemcached:
		bucket := &couchbasev2.CouchbaseMemcachedBucket{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: generateName,
			},
			Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
				EnableFlush: b.flush,
			},
		}

		newBucket, err := kubernetes.CRClient.CouchbaseV2().CouchbaseMemcachedBuckets(kubernetes.Namespace).Create(context.Background(), bucket, metav1.CreateOptions{})
		if err != nil {
			Die(t, err)
		}

		return newBucket
	}

	Die(t, fmt.Errorf("bucket builder creation failure"))

	return nil
}

// MustFlushBucket flushes all documents from a flushable bucket.
func MustFlushBucket(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket metav1.Object, timeout time.Duration) {
	callback := func() error {
		client, err := CreateAdminConsoleClient(kubernetes, cluster)
		if err != nil {
			return err
		}

		request := &couchbaseutil.Request{
			Path: fmt.Sprintf("/pools/default/buckets/%s/controller/doFlush", bucket.GetName()),
		}

		return client.client.Post(request, client.host)
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}
