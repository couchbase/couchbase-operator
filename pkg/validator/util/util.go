package util

import (
	"fmt"
	"strconv"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func UniqueString(strList []string) bool {
	set := map[string]interface{}{}
	for _, str := range strList {
		set[str] = nil
	}

	return len(set) == len(strList)
}

// StringArrayCompare compares two arrays and ensure the elements are the same
// but unordered.
func StringArrayCompare(a1, a2 []string) bool {
	m := make(map[string]int)
	for _, val := range a1 {
		m[val]++
	}

	for _, val := range a2 {
		if _, ok := m[val]; ok {
			if m[val] > 0 {
				m[val]--
				continue
			}
		}

		return false
	}

	for _, cnt := range m {
		if cnt > 0 {
			return false
		}
	}

	return true
}

type UpdateError struct {
	field string
	in    string
}

func NewUpdateError(field, in string) error {
	return &UpdateError{field: field, in: in}
}

func (e *UpdateError) Error() string {
	return fmt.Sprintf("%s in %s cannot be updated", e.field, e.in)
}

// MergeAbstractBucketLists two lists of abstract bucket structs and joins them, with the l1 taking priority and overriding any buckets from l2 that share the same name.
func MergeAbstractBucketLists(l1, l2 []v2.AbstractBucket) []v2.AbstractBucket {
	names := make(map[string]struct{})

	for _, bucket := range l1 {
		names[bucket.GetCouchbaseName()] = struct{}{}
	}

	result := append([]v2.AbstractBucket{}, l1...)

	for _, bucket := range l2 {
		if _, exists := names[bucket.GetCouchbaseName()]; !exists {
			result = append(result, bucket)
		}
	}

	return result
}

func CheckDefaultStorageClassExists(scList *storagev1.StorageClassList) bool {
	if scList == nil {
		return false
	}

	for _, sc := range scList.Items {
		if val, ok := sc.Annotations["storageclass.kubernetes.io/is-default-class"]; ok {
			if d, err := strconv.ParseBool(val); err == nil && d {
				return true
			}
		}
	}

	return false
}

func GenerateMemcachedBucketWarning(cluster *v2.CouchbaseCluster, memcachedBucket *v2.CouchbaseMemcachedBucket) string {
	return fmt.Sprintf("memcached buckets are deprecated in Couchbase Server 8.0.0 and later, CouchbaseMemcachedBucket %s will be ignored for cluster %s", memcachedBucket.Name, cluster.NamespacedName())
}

func BucketStatusToCouchbaseBucket(status v2.BucketStatus) *v2.CouchbaseBucket {
	return &v2.CouchbaseBucket{
		Spec: v2.CouchbaseBucketSpec{
			Name:               v2.BucketName(status.BucketName),
			StorageBackend:     v2.CouchbaseStorageBackend(status.BucketStorageBackend),
			NumVBuckets:        status.NumVBuckets,
			MemoryQuota:        resource.NewQuantity(status.BucketMemoryQuota<<20, resource.BinarySI),
			Replicas:           status.BucketReplicas,
			IoPriority:         v2.CouchbaseBucketIOPriority(status.IoPriority),
			EvictionPolicy:     v2.CouchbaseBucketEvictionPolicy(status.EvictionPolicy),
			ConflictResolution: v2.CouchbaseBucketConflictResolution(status.ConflictResolution),
			EnableFlush:        status.EnableFlush,
			EnableIndexReplica: status.EnableIndexReplica,
			CompressionMode:    v2.CouchbaseBucketCompressionMode(status.CompressionMode),
		},
	}
}

func BucketStatusToEphemeralBucket(status v2.BucketStatus) *v2.CouchbaseEphemeralBucket {
	return &v2.CouchbaseEphemeralBucket{
		Spec: v2.CouchbaseEphemeralBucketSpec{
			Name:               v2.BucketName(status.BucketName),
			MemoryQuota:        resource.NewQuantity(status.BucketMemoryQuota<<20, resource.BinarySI),
			Replicas:           status.BucketReplicas,
			IoPriority:         v2.CouchbaseBucketIOPriority(status.IoPriority),
			EvictionPolicy:     v2.CouchbaseEphemeralBucketEvictionPolicy(status.EvictionPolicy),
			ConflictResolution: v2.CouchbaseBucketConflictResolution(status.ConflictResolution),
			EnableFlush:        status.EnableFlush,
			CompressionMode:    v2.CouchbaseBucketCompressionMode(status.CompressionMode),
		},
	}
}

func BucketStatusToMemcachedBucket(status v2.BucketStatus) *v2.CouchbaseMemcachedBucket {
	return &v2.CouchbaseMemcachedBucket{
		Spec: v2.CouchbaseMemcachedBucketSpec{
			Name:        v2.BucketName(status.BucketName),
			MemoryQuota: resource.NewQuantity(status.BucketMemoryQuota<<20, resource.BinarySI),
			EnableFlush: status.EnableFlush,
		},
	}
}

func GetCouchbaseBucketNameResourceNameMap(v *types.Validator, cluster *v2.CouchbaseCluster) map[string]string {
	couchbaseBuckets, err := v.Abstraction.GetCouchbaseBuckets(cluster.Namespace, cluster.Spec.Buckets.Selector)
	if err != nil {
		return nil
	}

	bucketNameResourceNameMap := make(map[string]string)

	for _, bucket := range couchbaseBuckets.Items {
		bucketNameResourceNameMap[bucket.GetCouchbaseName()] = bucket.GetResourceName()
	}

	return bucketNameResourceNameMap
}

func GetBucketsFromStatus(cluster *cluster.Cluster) ([]*v2.CouchbaseBucket, []*v2.CouchbaseMemcachedBucket, []*v2.CouchbaseEphemeralBucket) {
	statusBuckets := cluster.GetCouchbaseCluster().Status.Buckets

	var couchbaseBuckets []*v2.CouchbaseBucket
	var memcachedBuckets []*v2.CouchbaseMemcachedBucket
	var ephemeralBuckets []*v2.CouchbaseEphemeralBucket

	for _, b := range statusBuckets {
		switch b.BucketType {
		case constants.BucketTypeCouchbase:
			couchbaseBuckets = append(couchbaseBuckets, BucketStatusToCouchbaseBucket(b))
		case constants.BucketTypeEphemeral:
			ephemeralBuckets = append(ephemeralBuckets, BucketStatusToEphemeralBucket(b))
		case constants.BucketTypeMemcached:
			memcachedBuckets = append(memcachedBuckets, BucketStatusToMemcachedBucket(b))
		default:
			continue
		}
	}

	return couchbaseBuckets, memcachedBuckets, ephemeralBuckets
}
