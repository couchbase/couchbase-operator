// package conversion contains routines to convert between various resource types.
package conversion

import (
	"fmt"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// BucketNameGenerator is an abstract method for generating unique bucket
// resource names when using conversion.
type BucketNameGenerator interface {
	// GenerateBucketName generates a unique bucket name.
	GenerateBucketName(*couchbaseutil.Bucket) string

	// GenerateBucketName generates a unique ephemeral bucket name.
	GenerateEphemeralBucketName(*couchbaseutil.Bucket) string

	// GenerateBucketName generates a unique memcached bucket name.
	GenerateMemcachedBucketName(*couchbaseutil.Bucket) string

	// GenerateScopeName generates a unique scope name.
	GenerateScopeName(*couchbaseutil.Bucket, *couchbaseutil.Scope) string

	// GenerateCollectionName generates a unique collection name.
	GenerateCollectionName(*couchbaseutil.Bucket, *couchbaseutil.Scope, *couchbaseutil.Collection) string
}

// ConvertAbstractBucketToAPIBucket converts from an abstract API bucket to a concrete
// resource type.
func ConvertAbstractBucketToAPIBucket(bucket *couchbaseutil.Bucket, namer BucketNameGenerator) (runtime.Object, error) {
	switch bucket.BucketType {
	case "couchbase":
		return convertCouchbaseBucketToAPIBucket(bucket, namer), nil
	case "ephemeral":
		return convertCouchbaseEphemeralBucketToAPIBucket(bucket, namer), nil
	case "memcached":
		return convertCouchbaseMemcachedBucketToAPIBucket(bucket, namer), nil
	}

	return nil, fmt.Errorf("%w: unexpected bucket type %s", errors.NewStackTracedError(errors.ErrInternalError), bucket.BucketType)
}

// convertCouchbaseBucketToAPIBucket takes a raw bucket from the Couchbase API and converts
// it into a Kubernetes API equivalent.
func convertCouchbaseBucketToAPIBucket(bucket *couchbaseutil.Bucket, namer BucketNameGenerator) runtime.Object {
	cbBucket := &couchbasev2.CouchbaseBucket{
		TypeMeta: metav1.TypeMeta{
			APIVersion: couchbasev2.Group,
			Kind:       couchbasev2.BucketCRDResourceKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namer.GenerateBucketName(bucket),
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			Name:               couchbasev2.BucketName(bucket.BucketName),
			StorageBackend:     couchbasev2.CouchbaseStorageBackend(bucket.BucketStorageBackend),
			MemoryQuota:        resource.NewQuantity(bucket.BucketMemoryQuota<<20, resource.BinarySI),
			Replicas:           bucket.BucketReplicas,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriority(bucket.IoPriority),
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicy(bucket.EvictionPolicy),
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolution(bucket.ConflictResolution),
			EnableFlush:        bucket.EnableFlush,
			EnableIndexReplica: bucket.EnableIndexReplica,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionMode(bucket.CompressionMode),
			MinimumDurability:  couchbasev2.CouchbaseBucketMinimumDurability(bucket.DurabilityMinLevel),
			MaxTTL:             &metav1.Duration{Duration: time.Duration(bucket.MaxTTL) * time.Second},
			Scopes: &couchbasev2.ScopeSelector{
				Managed:   true,
				Resources: []couchbasev2.ScopeLocalObjectReference{},
			},
			DurabilityImpossibleFallback: couchbasev2.DurabilityImpossibleFallback(bucket.DurabilityImpossibleFallback),
		},
	}

	if bucket.BucketStorageBackend == "magma" {
		cbBucket.Spec.NumVBuckets = bucket.NumVBuckets
		cbBucket.Spec.HistoryRetentionSettings = &couchbasev2.HistoryRetentionSettings{
			CollectionDefault: bucket.HistoryRetentionCollectionDefault,
			Seconds:           bucket.HistoryRetentionSeconds,
			Bytes:             bucket.HistoryRetentionBytes,
		}
	}

	return cbBucket
}

// convertCouchbaseEphemeralBucketToAPIBucket takes a raw bucket from the Couchbase API and converts
// it into a Kubernetes API equivalent.
func convertCouchbaseEphemeralBucketToAPIBucket(bucket *couchbaseutil.Bucket, namer BucketNameGenerator) runtime.Object {
	return &couchbasev2.CouchbaseEphemeralBucket{
		TypeMeta: metav1.TypeMeta{
			APIVersion: couchbasev2.Group,
			Kind:       couchbasev2.EphemeralBucketCRDResourceKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namer.GenerateEphemeralBucketName(bucket),
		},
		Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
			Name:               couchbasev2.BucketName(bucket.BucketName),
			MemoryQuota:        resource.NewQuantity(bucket.BucketMemoryQuota<<20, resource.BinarySI),
			Replicas:           bucket.BucketReplicas,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriority(bucket.IoPriority),
			EvictionPolicy:     couchbasev2.CouchbaseEphemeralBucketEvictionPolicy(bucket.EvictionPolicy),
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolution(bucket.ConflictResolution),
			EnableFlush:        bucket.EnableFlush,
			CompressionMode:    couchbasev2.CouchbaseBucketCompressionMode(bucket.CompressionMode),
			MinimumDurability:  couchbasev2.CouchbaseEphemeralBucketMinimumDurability(bucket.DurabilityMinLevel),
			MaxTTL:             &metav1.Duration{Duration: time.Duration(bucket.MaxTTL) * time.Second},
			Scopes: &couchbasev2.ScopeSelector{
				Managed:   true,
				Resources: []couchbasev2.ScopeLocalObjectReference{},
			},
			DurabilityImpossibleFallback: couchbasev2.DurabilityImpossibleFallback(bucket.DurabilityImpossibleFallback),
		},
	}
}

// convertCouchbaseMemcachedBucketToAPIBucket takes a raw bucket from the Couchbase API and converts
// it into a Kubernetes API equivalent.
func convertCouchbaseMemcachedBucketToAPIBucket(bucket *couchbaseutil.Bucket, namer BucketNameGenerator) runtime.Object {
	return &couchbasev2.CouchbaseMemcachedBucket{
		TypeMeta: metav1.TypeMeta{
			APIVersion: couchbasev2.Group,
			Kind:       couchbasev2.MemcachedBucketCRDResourceKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namer.GenerateMemcachedBucketName(bucket),
		},
		Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
			Name:        couchbasev2.BucketName(bucket.BucketName),
			MemoryQuota: resource.NewQuantity(bucket.BucketMemoryQuota<<20, resource.BinarySI),
			EnableFlush: bucket.EnableFlush,
		},
	}
}

// ConvertCouchbaseScopeToAPIScope takes a raw scope from the Couchbase API and converts
// it into a Kubernetes API equivalent.
func ConvertCouchbaseScopeToAPIScope(bucket *couchbaseutil.Bucket, scope *couchbaseutil.Scope, namer BucketNameGenerator) *couchbasev2.CouchbaseScope {
	s := &couchbasev2.CouchbaseScope{
		TypeMeta: metav1.TypeMeta{
			APIVersion: couchbasev2.Group,
			Kind:       couchbasev2.ScopeCRDResourceKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namer.GenerateScopeName(bucket, scope),
		},
		Spec: couchbasev2.CouchbaseScopeSpec{
			CouchbaseScopeSpecCommon: couchbasev2.CouchbaseScopeSpecCommon{
				Collections: &couchbasev2.CollectionSelector{
					Managed:   true,
					Resources: []couchbasev2.CollectionLocalObjectReference{},
				},
			},
		},
	}

	if scope.Name == constants.DefaultScopeOrCollectionName {
		s.Spec.DefaultScope = true
	} else {
		s.Spec.Name = couchbasev2.ScopeOrCollectionName(scope.Name)
	}

	return s
}

// ConvertCouchbaseCollectionToAPICollection takes a raw collection from the Couchbase API and converts
// it into a Kubernetes API equivalent.
func ConvertCouchbaseCollectionToAPICollection(bucket *couchbaseutil.Bucket, scope *couchbaseutil.Scope, collection *couchbaseutil.Collection, namer BucketNameGenerator) *couchbasev2.CouchbaseCollection {
	return &couchbasev2.CouchbaseCollection{
		TypeMeta: metav1.TypeMeta{
			APIVersion: couchbasev2.Group,
			Kind:       couchbasev2.CollectionCRDResourceKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namer.GenerateCollectionName(bucket, scope, collection),
		},
		Spec: couchbasev2.CouchbaseCollectionSpec{
			Name: couchbasev2.ScopeOrCollectionName(collection.Name),
			CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{
				MaxTTLWithNegativeOverride: couchbasev2.MaxTTLWithNegativeOverride{MaxTTL: &metav1.Duration{Duration: time.Duration(*collection.MaxTTL) * time.Second}},
				History:                    collection.History,
			},
		},
	}
}
