package e2eutil

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"k8s.io/apimachinery/pkg/api/resource"
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

	// memoryQuota is the desired memory quota for the bucket.
	memoryQuota int64

	// replicas is the number of replicas for the bucket.
	replicas int

	// ioPriority is the priority for the bucket, affecting the number of threads for the bucket.
	ioPriority couchbasev2.CouchbaseBucketIOPriority

	// evictionPolicy (or ejection policy) changes the policy for how docs are removed from memory.
	evictionPolicy string

	// conflictResolution is the methodology used for solving conflicts from XDCR replication.
	conflictResolution couchbasev2.CouchbaseBucketConflictResolution

	// flush allows the bucket to be flushed.
	flush bool

	// indexReplica causes indexes to be replicated.
	indexReplica bool

	// compressionMode is the compression mode to use.
	// If not specified this defaults to "passive".
	compressionMode couchbasev2.CouchbaseBucketCompressionMode

	// durabilityMinLevel defines the minimum level at which all writes to the bucket must occur.
	durabilityMinLevel couchbaseutil.Durability

	// maxTTL sets a maximum lifespam on items in the bucket.
	maxTTL int

	// scopes is a slice containing the scopes to be added.
	scopes []metav1.Object

	// CouchbaseStorageBackend can either be "couchstore" or "magma"
	storageBackend couchbasev2.CouchbaseStorageBackend
}

// NewBucket creates a bucket with any required parameters.
func NewBucket(kind BucketType) *Bucket {
	return &Bucket{
		kind: kind,
	}
}

func (b *Bucket) WithMemoryQuota(memory int) *Bucket {
	b.memoryQuota = int64(memory)

	return b
}

func (b *Bucket) WithReplicas(replicas int) *Bucket {
	b.replicas = replicas

	return b
}

func (b *Bucket) WithIOPriority(priority couchbasev2.CouchbaseBucketIOPriority) *Bucket {
	b.ioPriority = priority

	return b
}

func (b *Bucket) WithEvictionPolicy(evictionPolicy string) *Bucket {
	b.evictionPolicy = evictionPolicy

	return b
}

func (b *Bucket) WithConflictResolution(conflictResolution couchbasev2.CouchbaseBucketConflictResolution) *Bucket {
	b.conflictResolution = conflictResolution

	return b
}

// WithFlush allows the bucket to be flushed.
func (b *Bucket) WithFlush() *Bucket {
	b.flush = true

	return b
}

func (b *Bucket) WithIndexReplica() *Bucket {
	b.indexReplica = true

	return b
}

// WithCompressionMode allows the bucket's compression mode to be specified.
func (b *Bucket) WithCompressionMode(compressionMode couchbasev2.CouchbaseBucketCompressionMode) *Bucket {
	b.compressionMode = compressionMode

	return b
}

func (b *Bucket) WithDurability(durabilityMinLevel couchbaseutil.Durability) *Bucket {
	b.durabilityMinLevel = durabilityMinLevel

	return b
}

func (b *Bucket) WithTTL(ttl int) *Bucket {
	b.maxTTL = ttl

	return b
}

func (b *Bucket) WithStorageBackend(backend couchbasev2.CouchbaseStorageBackend) *Bucket {
	b.storageBackend = backend

	return b
}

// WithScopes takes a variable amount of scope objects, and adds them to the bucket.
func (b *Bucket) WithScopes(scopes ...metav1.Object) *Bucket {
	b.scopes = append(b.scopes, scopes...)

	return b
}

func (b *Bucket) GetBucketType() BucketType {
	return b.kind
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
		}

		if b.memoryQuota != 0 {
			bucket.Spec.MemoryQuota = resource.NewQuantity(b.memoryQuota, resource.BinarySI)
		}

		if b.replicas != 0 {
			bucket.Spec.Replicas = b.replicas
		}

		if b.ioPriority != "" {
			bucket.Spec.IoPriority = b.ioPriority
		}

		if b.evictionPolicy != "" {
			bucket.Spec.EvictionPolicy = couchbasev2.CouchbaseBucketEvictionPolicy(b.evictionPolicy)
		}

		if b.conflictResolution != "" {
			bucket.Spec.ConflictResolution = b.conflictResolution
		}

		if b.flush {
			bucket.Spec.EnableFlush = b.flush
		}

		if b.indexReplica {
			bucket.Spec.EnableIndexReplica = b.indexReplica
		}

		if b.compressionMode != "" {
			bucket.Spec.CompressionMode = b.compressionMode
		}

		if b.durabilityMinLevel != "" {
			bucket.Spec.MinimumDurability = couchbasev2.CouchbaseBucketMinimumDurability(b.durabilityMinLevel)
		}

		if b.maxTTL != 0 {
			bucket.Spec.MaxTTL = e2espec.NewDurationS(int64(b.maxTTL))
		}

		if b.storageBackend != "" {
			bucket.Spec.StorageBackend = b.storageBackend
		}

		if b.scopes != nil {
			if bucket.Spec.Scopes == nil {
				bucket.Spec.Scopes = &couchbasev2.ScopeSelector{}
			}

			bucket.Spec.Scopes.Managed = true

			for _, scope := range b.scopes {
				switch s := scope.(type) {
				case *couchbasev2.CouchbaseScope:
					bucket.Spec.Scopes.Resources = append(bucket.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
						Kind: couchbasev2.ScopeCRDResourceKind,
						Name: couchbasev2.ScopeOrCollectionName(s.Name),
					})
				case *couchbasev2.CouchbaseScopeGroup:
					bucket.Spec.Scopes.Resources = append(bucket.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
						Kind: couchbasev2.ScopeGroupCRDResourceKind,
						Name: couchbasev2.ScopeOrCollectionName(s.Name),
					})
				}
			}
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
		}

		if b.memoryQuota != 0 {
			bucket.Spec.MemoryQuota = resource.NewQuantity(b.memoryQuota, resource.BinarySI)
		}

		if b.replicas != 0 {
			bucket.Spec.Replicas = b.replicas
		}

		if b.ioPriority != "" {
			bucket.Spec.IoPriority = b.ioPriority
		}

		if b.evictionPolicy != "" {
			bucket.Spec.EvictionPolicy = couchbasev2.CouchbaseEphemeralBucketEvictionPolicy(b.evictionPolicy)
		}

		if b.conflictResolution != "" {
			bucket.Spec.ConflictResolution = b.conflictResolution
		}

		if b.flush {
			bucket.Spec.EnableFlush = b.flush
		}

		if b.compressionMode != "" {
			bucket.Spec.CompressionMode = b.compressionMode
		}

		if b.durabilityMinLevel != "" {
			bucket.Spec.MinimumDurability = couchbasev2.CouchbaseEphemeralBucketMinimumDurability(b.durabilityMinLevel)
		}

		if b.maxTTL != 0 {
			bucket.Spec.MaxTTL = e2espec.NewDurationS(int64(b.maxTTL))
		}

		if b.scopes != nil {
			if bucket.Spec.Scopes == nil {
				bucket.Spec.Scopes = &couchbasev2.ScopeSelector{}
			}

			bucket.Spec.Scopes.Managed = true

			for _, scope := range b.scopes {
				switch s := scope.(type) {
				case *couchbasev2.CouchbaseScope:
					bucket.Spec.Scopes.Resources = append(bucket.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
						Kind: couchbasev2.ScopeCRDResourceKind,
						Name: couchbasev2.ScopeOrCollectionName(s.Name),
					})
				case *couchbasev2.CouchbaseScopeGroup:
					bucket.Spec.Scopes.Resources = append(bucket.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
						Kind: couchbasev2.ScopeGroupCRDResourceKind,
						Name: couchbasev2.ScopeOrCollectionName(s.Name),
					})
				}
			}
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
		}

		if b.memoryQuota != 0 {
			bucket.Spec.MemoryQuota = resource.NewQuantity(b.memoryQuota, resource.BinarySI)
		}

		if b.flush {
			bucket.Spec.EnableFlush = b.flush
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

func (b *Bucket) toBucketForAPI(name string) *couchbaseutil.Bucket {
	bucket := &couchbaseutil.Bucket{
		BucketName:         name,
		BucketType:         string(b.kind),
		BucketMemoryQuota:  100,
		BucketReplicas:     1,
		IoPriority:         couchbaseutil.IoPriorityTypeLow,
		EvictionPolicy:     "valueOnly",
		ConflictResolution: "seqno",
		EnableFlush:        false,
		EnableIndexReplica: false,
		CompressionMode:    couchbaseutil.CompressionModePassive,
		DurabilityMinLevel: "none",
		MaxTTL:             0,
	}

	if b.memoryQuota != 0 {
		bucket.BucketMemoryQuota = b.memoryQuota
	}

	if b.replicas != 0 {
		bucket.BucketReplicas = b.replicas
	}

	if b.ioPriority != "" {
		bucket.IoPriority = couchbaseutil.IoPriorityType(b.ioPriority)
	}

	if b.evictionPolicy != "" {
		bucket.EvictionPolicy = b.evictionPolicy
	}

	if b.conflictResolution != "" {
		bucket.ConflictResolution = string(b.conflictResolution)
	}

	if b.flush {
		bucket.EnableFlush = b.flush
	}

	if b.indexReplica {
		bucket.EnableIndexReplica = b.indexReplica
	}

	if b.compressionMode != "" {
		bucket.CompressionMode = couchbaseutil.CompressionMode(b.compressionMode)
	}

	if b.durabilityMinLevel != "" {
		bucket.DurabilityMinLevel = b.durabilityMinLevel
	}

	if b.maxTTL != 0 {
		bucket.MaxTTL = b.maxTTL
	}

	if b.storageBackend != "" {
		bucket.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(b.storageBackend)
	}

	return bucket
}

func (b *Bucket) MustCreateManually(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, name string) *couchbaseutil.Bucket {
	client, err := CreateAdminConsoleClient(kubernetes, cluster)
	if err != nil {
		Die(t, err)
	}

	bucket := b.toBucketForAPI(name)

	if len(b.scopes) > 0 {
		Die(t, fmt.Errorf("cannot use builder to manually create scopes"))
	}

	err = retryutil.RetryFor(30*time.Second, func() error {
		return couchbaseutil.CreateBucket(bucket).On(client.client, client.host)
	})

	if err != nil {
		Die(t, err)
	}

	return bucket
}

func (b *Bucket) MustUpdateManually(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, name string, timeout time.Duration) *couchbaseutil.Bucket {
	client, err := CreateAdminConsoleClient(kubernetes, cluster)
	if err != nil {
		Die(t, err)
	}

	bucket := b.toBucketForAPI(name)

	if len(b.scopes) > 0 {
		Die(t, fmt.Errorf("cannot use builder to manually create scopes"))
	}

	err = retryutil.RetryFor(timeout, func() error {
		return couchbaseutil.UpdateBucket(bucket).On(client.client, client.host)
	})

	if err != nil {
		Die(t, err)
	}

	return bucket
}

// MustFlushBucket flushes all documents from a flushable bucket.
func MustFlushBucket(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket metav1.Object, timeout time.Duration) {
	callback := func() error {
		client, err := CreateAdminConsoleClient(kubernetes, cluster)
		if err != nil {
			return err
		}

		request := newRequest(fmt.Sprintf("/pools/default/buckets/%s/controller/doFlush", bucket.GetName()), nil, nil)

		return client.client.Post(request, client.host)
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}

func MustAssertBucket(t *testing.T, actual interface{}, expected *couchbaseutil.Bucket) {
	switch expected.BucketType {
	case string(BucketTypeCouchbase):
		typedActual, ok := actual.(*couchbasev2.CouchbaseBucket)
		if !ok {
			Die(t, fmt.Errorf("could not assert couchbasebucket type"))
		}

		mustAssertCouchbaseBucket(t, typedActual, expected)
	case string(BucketTypeEphemeral):
		typedActual, ok := actual.(*couchbasev2.CouchbaseEphemeralBucket)
		if !ok {
			Die(t, fmt.Errorf("could not assert ephemeralbucket type"))
		}

		mustAssertEphemeralBucket(t, typedActual, expected)
	case string(BucketTypeMemcached):
		typedActual, ok := actual.(*couchbasev2.CouchbaseMemcachedBucket)
		if !ok {
			Die(t, fmt.Errorf("could not assert memcachedbucket type"))
		}

		mustAssertMemcachedBucket(t, typedActual, expected)
	default:
		Die(t, fmt.Errorf("unexpected bucket type"))
	}
}

func mustAssertCouchbaseBucket(t *testing.T, got *couchbasev2.CouchbaseBucket, expected *couchbaseutil.Bucket) {
	// bucket name
	if got.Spec.Name != couchbasev2.BucketName(expected.BucketName) {
		Die(t, fmt.Errorf("bucket name does not match: expected %s, got %s", expected.BucketName, got.Spec.Name))
	}
	// memory quota
	if got.Spec.MemoryQuota.Equal(*resource.NewMilliQuantity(expected.BucketMemoryQuota, resource.BinarySI)) {
		Die(t, fmt.Errorf("memory quota does not match: expected %d, got %d", expected.BucketMemoryQuota, got.Spec.MemoryQuota.MilliValue()))
	}
	// replicas
	if got.Spec.Replicas != expected.BucketReplicas {
		Die(t, fmt.Errorf("bucket replicas does not match: expected %d, got %d", expected.BucketReplicas, got.Spec.Replicas))
	}
	// io prio
	if got.Spec.IoPriority != couchbasev2.CouchbaseBucketIOPriority(expected.IoPriority) {
		Die(t, fmt.Errorf("io priority does not match: expected %s, got %s", expected.IoPriority, got.Spec.IoPriority))
	}
	// eviction
	if got.Spec.EvictionPolicy != couchbasev2.CouchbaseBucketEvictionPolicy(expected.EvictionPolicy) {
		Die(t, fmt.Errorf("eviction policy does not match: expected %s, got %s", expected.EvictionPolicy, got.Spec.EvictionPolicy))
	}
	// conflict resolution
	if got.Spec.ConflictResolution != couchbasev2.CouchbaseBucketConflictResolution(expected.ConflictResolution) {
		Die(t, fmt.Errorf("conflict resolution does not match: expected %s, got %s", expected.IoPriority, got.Spec.IoPriority))
	}
	// flush
	if got.Spec.EnableFlush != expected.EnableFlush {
		Die(t, fmt.Errorf("enable flush does not match: expected %t, got %t", expected.EnableFlush, got.Spec.EnableFlush))
	}
	// index replica
	if got.Spec.EnableIndexReplica != expected.EnableIndexReplica {
		Die(t, fmt.Errorf("enable index replica does not match: expected %t, got %t", expected.EnableIndexReplica, got.Spec.EnableIndexReplica))
	}
	// compression
	if got.Spec.CompressionMode != couchbasev2.CouchbaseBucketCompressionMode(expected.CompressionMode) {
		Die(t, fmt.Errorf("compression mode does not match: expected %s, got %s", expected.CompressionMode, got.Spec.CompressionMode))
	}
	// durability
	if got.Spec.MinimumDurability != couchbasev2.CouchbaseBucketMinimumDurability(expected.DurabilityMinLevel) {
		Die(t, fmt.Errorf("minimum durability does not match: expected %s, got %s", expected.DurabilityMinLevel, got.Spec.MinimumDurability))
	}
	// ttl
	if got.Spec.MaxTTL.Duration != time.Second*time.Duration(expected.MaxTTL) {
		Die(t, fmt.Errorf("max ttl does not match: expected %s, got %s", time.Second*time.Duration(expected.MaxTTL), got.Spec.MaxTTL.Duration))
	}

	if got.Spec.StorageBackend != couchbasev2.CouchbaseStorageBackend(expected.BucketStorageBackend) {
		Die(t, fmt.Errorf("storage backend does not match: expected %s, got %s", expected.BucketStorageBackend, got.Spec.StorageBackend))
	}
}

func mustAssertEphemeralBucket(t *testing.T, got *couchbasev2.CouchbaseEphemeralBucket, expected *couchbaseutil.Bucket) {
	// bucket name
	if got.Spec.Name != couchbasev2.BucketName(expected.BucketName) {
		Die(t, fmt.Errorf("bucket name does not match: expected %s, got %s", expected.BucketName, got.Spec.Name))
	}
	// memory quota
	if got.Spec.MemoryQuota.Equal(*resource.NewMilliQuantity(expected.BucketMemoryQuota, resource.BinarySI)) {
		Die(t, fmt.Errorf("memory quota does not match: expected %d, got %d", expected.BucketMemoryQuota, got.Spec.MemoryQuota.MilliValue()))
	}
	// replicas
	if got.Spec.Replicas != expected.BucketReplicas {
		Die(t, fmt.Errorf("bucket replicas does not match: expected %d, got %d", expected.BucketReplicas, got.Spec.Replicas))
	}
	// io prio
	if got.Spec.IoPriority != couchbasev2.CouchbaseBucketIOPriority(expected.IoPriority) {
		Die(t, fmt.Errorf("io priority does not match: expected %s, got %s", expected.IoPriority, got.Spec.IoPriority))
	}
	// eviction
	if got.Spec.EvictionPolicy != couchbasev2.CouchbaseEphemeralBucketEvictionPolicy(expected.EvictionPolicy) {
		Die(t, fmt.Errorf("eviction policy does not match: expected %s, got %s", expected.EvictionPolicy, got.Spec.EvictionPolicy))
	}
	// conflict resolution
	if got.Spec.ConflictResolution != couchbasev2.CouchbaseBucketConflictResolution(expected.ConflictResolution) {
		Die(t, fmt.Errorf("conflict resolution does not match: expected %s, got %s", expected.IoPriority, got.Spec.IoPriority))
	}
	// flush
	if got.Spec.EnableFlush != expected.EnableFlush {
		Die(t, fmt.Errorf("enable flush does not match: expected %t, got %t", expected.EnableFlush, got.Spec.EnableFlush))
	}
	// compression
	if got.Spec.CompressionMode != couchbasev2.CouchbaseBucketCompressionMode(expected.CompressionMode) {
		Die(t, fmt.Errorf("compression mode does not match: expected %s, got %s", expected.CompressionMode, got.Spec.CompressionMode))
	}
	// durability
	if got.Spec.MinimumDurability != couchbasev2.CouchbaseEphemeralBucketMinimumDurability(expected.DurabilityMinLevel) {
		Die(t, fmt.Errorf("minimum durability does not match: expected %s, got %s", expected.DurabilityMinLevel, got.Spec.MinimumDurability))
	}
	// ttl
	if got.Spec.MaxTTL.Duration != time.Second*time.Duration(expected.MaxTTL) {
		Die(t, fmt.Errorf("max ttl does not match: expected %s, got %s", time.Second*time.Duration(expected.MaxTTL), got.Spec.MaxTTL.Duration))
	}
}

func mustAssertMemcachedBucket(t *testing.T, got *couchbasev2.CouchbaseMemcachedBucket, expected *couchbaseutil.Bucket) {
	// bucket name
	if got.Spec.Name != couchbasev2.BucketName(expected.BucketName) {
		Die(t, fmt.Errorf("bucket name does not match: expected %s, got %s", expected.BucketName, got.Spec.Name))
	}
	// memory quota
	if got.Spec.MemoryQuota.Equal(*resource.NewMilliQuantity(expected.BucketMemoryQuota, resource.BinarySI)) {
		Die(t, fmt.Errorf("memory quota does not match: expected %d, got %d", expected.BucketMemoryQuota, got.Spec.MemoryQuota.MilliValue()))
	}
	// flush
	if got.Spec.EnableFlush != expected.EnableFlush {
		Die(t, fmt.Errorf("enable flush does not match: expected %t, got %t", expected.EnableFlush, got.Spec.EnableFlush))
	}
}

// MustAssertBucketStorageBackend checks that the status version is as we expect.
func MustAssertBucketStorageBackend(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucketName string, bucketStorageBackend couchbaseutil.CouchbaseStorageBackend, timeout time.Duration) {
	callback := func() error {
		cls, err := k8s.CRClient.CouchbaseV2().CouchbaseClusters(cluster.Namespace).Get(context.Background(), cluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, bucket := range cls.Status.Buckets {
			if bucket.BucketName == bucketName && couchbaseutil.CouchbaseStorageBackend(bucket.BucketStorageBackend) != bucketStorageBackend {
				return fmt.Errorf("expected bucket storage backend %s, got %s", bucketStorageBackend, bucket.BucketStorageBackend)
			}
		}

		return nil
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}

// getBucketInfo returns information of the bucket.
func getBucketInfo(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string) (*couchbaseutil.BucketStatus, error) {
	client := MustCreateAdminConsoleClient(t, k8s, cluster)

	info := &couchbaseutil.BucketStatus{}

	request := newRequest("/pools/default/buckets/"+bucket, nil, info)

	if err := client.client.Get(request, client.host); err != nil {
		return info, err
	}

	return info, nil
}

// verifyDocCountInBucket polls the Couchbase API for the named bucket and checks whether the
// document count matches the expected number of items.
func verifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
			return err
		}

		if info.BasicStats.ItemCount != items {
			return fmt.Errorf("document count %d, expected %d", info.BasicStats.ItemCount, items)
		}

		return nil
	})
}

func MustVerifyDocCountInBucket(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, items int, timeout time.Duration) {
	if err := verifyDocCountInBucket(t, k8s, cluster, bucket, items, timeout); err != nil {
		Die(t, err)
	}
}

// getLatestMetric takes the 2D array that server returns - an array of [timestamp, value] - and gets the last/most recent one.
// Naturally, the returned value is a string, so we convert it to an int, then return it as the value we're looking for.
// Sorry.
func getLatestMetric(values [][]interface{}) (int, error) {
	return strconv.Atoi(values[len(values)-1][1].(string))
}

// verifyDocCountInBucketNonZero polls the Couchbase API for the named bucket and checks whether the
// document count is non-zero.
func verifyDocCountInBucketNonZero(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
			return err
		}

		if info.BasicStats.ItemCount == 0 {
			return fmt.Errorf("document count zero")
		}

		return nil
	})
}

func MustVerifyDocCountInBucketNonZero(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := verifyDocCountInBucketNonZero(t, k8s, cluster, bucket, timeout); err != nil {
		Die(t, err)
	}
}

func verifyBucketHistoryRetentionSettings(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, name string, seconds, bytes uint64, collectionDefault bool, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, name)
		if err != nil {
			return err
		}

		if info.HistoryRetentionBytes != bytes {
			return fmt.Errorf("history retention expected %d bytes but found %d", bytes, info.HistoryRetentionBytes)
		}
		if info.HistoryRetentionSeconds != seconds {
			return fmt.Errorf("history retention expected %d seconds but found %d", seconds, info.HistoryRetentionSeconds)
		}

		if *info.HistoryRetentionCollectionDefault != collectionDefault {
			return fmt.Errorf("history retention expected %t collection default but found %t", collectionDefault, *info.HistoryRetentionCollectionDefault)
		}
		return nil
	})
}

func MustVerifyBucketHistoryRetentionSettings(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, name string, seconds, bytes uint64, collectionDefault bool, timeout time.Duration) {
	if err := verifyBucketHistoryRetentionSettings(t, k8s, cluster, name, seconds, bytes, collectionDefault, timeout); err != nil {
		Die(t, err)
	}
}

func verifyMagmaBucketBlockSizeSettings(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucketName string, seqTreeDataBlockSizeBytes, keyTreeDataBlockSize uint64, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucketName)
		if err != nil {
			return err
		}

		if *info.MagmaSeqTreeDataBlockSize != seqTreeDataBlockSizeBytes {
			return fmt.Errorf("magma seqIndex block size expected %d bytes but found %d", seqTreeDataBlockSizeBytes, info.MagmaSeqTreeDataBlockSize)
		}

		if *info.MagmaKeyTreeDataBlockSize != keyTreeDataBlockSize {
			return fmt.Errorf("magma keyIndex block size expected %d bytes but found %d", keyTreeDataBlockSize, *info.MagmaKeyTreeDataBlockSize)
		}

		return nil
	})
}

func MustVerifyMagmaBucketBlockSizeSettings(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucketName string, seqTreeDataBlockSizeBytes, keyTreeDataBlockSize uint64, timeout time.Duration) {
	if err := verifyMagmaBucketBlockSizeSettings(t, k8s, cluster, bucketName, seqTreeDataBlockSizeBytes, keyTreeDataBlockSize, timeout); err != nil {
		Die(t, err)
	}
}

// verifyReplicaCount polls the Couchbase API for the named bucket and checks whether the
// Replica number matches the expected replicaNumber.
func verifyReplicaCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
			return err
		}

		if info.ReplicaNumber != 1 {
			return fmt.Errorf("replica Number %d, expected %d", info.ReplicaNumber, 1)
		}

		return nil
	})
}

func MustVerifyReplicaCount(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, timeout time.Duration) {
	if err := verifyReplicaCount(t, k8s, cluster, bucket, timeout); err != nil {
		Die(t, err)
	}
}

// storageBackendUnchanged polls the Couchbase API for the named bucket to verify the storage backend is unchanged.
func storageBackendUnchanged(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, expectedStorageBackendType couchbaseutil.CouchbaseStorageBackend, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
			return err
		}

		if info.StorageBackend != expectedStorageBackendType {
			return fmt.Errorf("storage backend from API %v, expected %v", info.StorageBackend, expectedStorageBackendType)
		}

		return nil
	})
}

func MustVerifyStorageBackendUnchanged(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, storageBackendType couchbaseutil.CouchbaseStorageBackend, timeout time.Duration) {
	if err := storageBackendUnchanged(t, k8s, cluster, bucket, storageBackendType, timeout); err != nil {
		Die(t, err)
	}
}

func MustVerifyCouchbaseBucketAutoCompactionSettings(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, expectedSettings couchbaseutil.BucketAutoCompactionSettings, expectedPurgeInterval *float64, timeout time.Duration) {
	if err := verifyBuckAutoCompactionSettings(t, k8s, cluster, bucket, expectedSettings, expectedPurgeInterval, timeout); err != nil {
		Die(t, err)
	}
}

func verifyBuckAutoCompactionSettings(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket string, expectedSettings couchbaseutil.BucketAutoCompactionSettings, expectedPurgeInterval *float64, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		info, err := getBucketInfo(t, k8s, cluster, bucket)
		if err != nil {
			return err
		}

		if !reflect.DeepEqual(info.AutoCompactionSettings, expectedSettings) {
			return fmt.Errorf("bucket auto compaction settings from api %v, expected %v", info.AutoCompactionSettings.Settings, expectedSettings.Settings)
		}

		if !reflect.DeepEqual(info.PurgeInterval, expectedPurgeInterval) {
			return fmt.Errorf("bucket auto compaction purge interval from api %v, expected %v", info.PurgeInterval, expectedPurgeInterval)
		}

		return nil
	})
}
