package util

import (
	"reflect"
	"testing"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMergeAbstractBucketLists(t *testing.T) {
	newCouchbaseBucket := func(name v2.BucketName, memoryQuota int64) *v2.CouchbaseBucket {
		return &v2.CouchbaseBucket{
			Spec: v2.CouchbaseBucketSpec{
				Name:        name,
				MemoryQuota: resource.NewQuantity(memoryQuota, resource.BinarySI),
			},
		}
	}

	newMemcachedBucket := func(name v2.BucketName, memoryQuota int64) *v2.CouchbaseMemcachedBucket {
		return &v2.CouchbaseMemcachedBucket{
			Spec: v2.CouchbaseMemcachedBucketSpec{
				Name:        name,
				MemoryQuota: resource.NewQuantity(memoryQuota, resource.BinarySI),
			},
		}
	}

	cbBucket := newCouchbaseBucket("cb-bucket", 100)
	memcachedBucket := newMemcachedBucket("memcached-bucket", 100)

	testcases := []struct {
		l1       []v2.AbstractBucket
		l2       []v2.AbstractBucket
		expected []v2.AbstractBucket
	}{
		{
			l1:       []v2.AbstractBucket{},
			l2:       []v2.AbstractBucket{},
			expected: []v2.AbstractBucket{},
		},
		{
			l1: []v2.AbstractBucket{
				cbBucket,
				memcachedBucket,
			},
			l2: []v2.AbstractBucket{},
			expected: []v2.AbstractBucket{
				cbBucket,
				memcachedBucket,
			},
		},
		{
			l1: []v2.AbstractBucket{},
			l2: []v2.AbstractBucket{
				cbBucket,
				memcachedBucket,
			},
			expected: []v2.AbstractBucket{
				cbBucket,
				memcachedBucket,
			},
		},
		{
			l1: []v2.AbstractBucket{
				cbBucket,
				newMemcachedBucket("some-memcached-bucket", 0),
			},
			l2: []v2.AbstractBucket{
				newCouchbaseBucket("cb-bucket", 200),
				memcachedBucket,
			},
			expected: []v2.AbstractBucket{
				cbBucket,
				newMemcachedBucket("some-memcached-bucket", 0),
				memcachedBucket,
			},
		},
	}

	for _, testcase := range testcases {
		result := MergeAbstractBucketLists(testcase.l1, testcase.l2)
		if !reflect.DeepEqual(result, testcase.expected) {
			t.Errorf("abstract bucket lists were not merged as expected")
		}
	}
}
