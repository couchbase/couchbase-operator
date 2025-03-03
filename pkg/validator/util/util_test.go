package util

import (
	"reflect"
	"testing"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestCheckDefaultStorageClassExists(t *testing.T) {
	testcases := []struct {
		name           string
		storageClasses []storagev1.StorageClass
		expected       bool
	}{
		{
			name: "No Annotation Set",
			storageClasses: []storagev1.StorageClass{
				{
					ObjectMeta: metav1.ObjectMeta{},
				},
			},
			expected: false,
		},
		{
			name: "True Annotation Exists",
			storageClasses: []storagev1.StorageClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"storageclass.kubernetes.io/is-default-class": "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
			expected: true,
		},
		{
			name: "False Annotation Exists",
			storageClasses: []storagev1.StorageClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"storageclass.kubernetes.io/is-default-class": "false",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Default configured on one class",
			storageClasses: []storagev1.StorageClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"storageclass.kubernetes.io/is-default-class": "false",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"storageclass.kubernetes.io/is-default-class": "true",
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, testcase := range testcases {
		scl := storagev1.StorageClassList{Items: testcase.storageClasses}
		result := CheckDefaultStorageClassExists(&scl)

		if result != testcase.expected {
			t.Errorf("default storage class existence check failed, expected %v but got %v in test %v", testcase.expected, result, testcase.name)
		}
	}
}
