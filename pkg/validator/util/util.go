package util

import (
	"fmt"
	"strconv"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	storagev1 "k8s.io/api/storage/v1"
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
