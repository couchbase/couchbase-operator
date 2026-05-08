/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package types

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// ResourceCacheProvider abstracts cache implementation for resource validation between the Operator and the DAC. The Operator uses a watcher/informer cache, whereas the DAC uses a simple in-memory cache populated at the start of each admission request.
type ResourceCacheProvider interface {
	InitCollectionCache(namespace string, load func() (*couchbasev2.CouchbaseCollectionList, error)) error
	InitCollectionGroupCache(namespace string, load func() (*couchbasev2.CouchbaseCollectionGroupList, error)) error
	InitBucketCache(namespace string, load func() (*couchbasev2.CouchbaseBucketList, error)) error
	GetCollection(namespace, name string) (*couchbasev2.CouchbaseCollection, bool, error)
	GetCollections(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionList, error)
	GetCollectionGroup(namespace, name string) (*couchbasev2.CouchbaseCollectionGroup, bool, error)
	GetCollectionGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionGroupList, error)
	GetBucket(namespace, name string) (*couchbasev2.CouchbaseBucket, bool, error)
	GetBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error)
}

// SimpleIndexCache is a typed, single namespace cache backed by a client-go Indexer.
// T should be a pointer to a Kubernetes runtime.Object (e.g. *couchbasev2.CouchbaseCollection).
type SimpleIndexCache[T runtime.Object] struct {
	namespace string
	index     cache.Indexer
}

func NewSimpleIndexCache[T runtime.Object]() *SimpleIndexCache[T] {
	return &SimpleIndexCache[T]{
		index: cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
	}
}

func (c *SimpleIndexCache[T]) Initialised(namespace string) bool {
	return c.namespace != "" && c.namespace == namespace
}

// Load replaces the cache contents for the given namespace.
func (c *SimpleIndexCache[T]) Load(namespace string, items []T) {
	c.namespace = namespace
	for _, obj := range c.index.List() {
		_ = c.index.Delete(obj)
	}
	for _, obj := range items {
		_ = c.index.Add(obj)
	}
}

// Get retrieves a single typed object by name.
func (c *SimpleIndexCache[T]) Get(name string) (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	key := c.namespace + "/" + name
	obj, exists, err := c.index.GetByKey(key)
	if err != nil || !exists || obj == nil {
		return zero, false
	}

	t, ok := obj.(T)
	if !ok {
		return zero, false
	}

	return t, true
}

// List returns all typed objects that match the optional label selector.
func (c *SimpleIndexCache[T]) List(selector *metav1.LabelSelector) ([]T, error) {
	if c == nil {
		return nil, nil
	}
	out := []T{}
	if selector == nil {
		for _, o := range c.index.List() {
			if t, ok := o.(T); ok {
				out = append(out, t)
			}
		}

		return out, nil
	}

	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	for _, o := range c.index.List() {
		ro, rok := o.(runtime.Object)
		if !rok {
			continue
		}
		acc, err := meta.Accessor(ro)
		if err != nil {
			continue
		}
		if sel.Matches(labels.Set(acc.GetLabels())) {
			if t, ok := o.(T); ok {
				out = append(out, t)
			}
		}
	}

	return out, nil
}

// listDeref lists items from a pointer cache and returns them dereferenced.
// The T is required to be a pointer to E, and T must implement runtime.Object.
func listDeref[T interface {
	*E
	runtime.Object
}, E any](c *SimpleIndexCache[T], selector *metav1.LabelSelector) ([]E, error) {
	if c == nil {
		return nil, nil
	}
	// Load from the cache.
	ptrs, err := c.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]E, 0, len(ptrs))
	for _, p := range ptrs {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out, nil
}

// AdmissionControlCache is an in-memory cache used by the DAC to avoid
// hitting rate limits when querying the K8s API server during admission validation.
// It is not a general-purpose cache, does not watch for resource changes and must
// not be used outside of the admission controller path.
// Cache contents are loaded on the first Get or List request for a given namespace.
// This implements the ResourceCacheProvider interface.
// To add a new resource type, add Get/List/Init methods to the ResourceCacheProvider and implement here.
// Then update the KubeAbstractionImpl Get/List methods to warm the cache before returning results.
type AdmissionControlCache struct {
	collections      *SimpleIndexCache[*couchbasev2.CouchbaseCollection]
	collectionGroups *SimpleIndexCache[*couchbasev2.CouchbaseCollectionGroup]
	buckets          *SimpleIndexCache[*couchbasev2.CouchbaseBucket]
}

func NewAdmissionControlCache() *AdmissionControlCache {
	return &AdmissionControlCache{}
}

func (c *AdmissionControlCache) InitCollectionCache(namespace string, load func() (*couchbasev2.CouchbaseCollectionList, error)) error {
	if c.collections != nil && c.collections.Initialised(namespace) {
		return nil
	}

	collections, err := load()
	if err != nil || collections == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseCollection, 0, len(collections.Items))
	for i := range collections.Items {
		ptrs = append(ptrs, &collections.Items[i])
	}

	c.collections = NewSimpleIndexCache[*couchbasev2.CouchbaseCollection]()
	c.collections.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetCollection(namespace, name string) (*couchbasev2.CouchbaseCollection, bool, error) {
	col, ok := c.collections.Get(name)
	return col, ok, nil
}

func (c *AdmissionControlCache) GetCollections(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionList, error) {
	items, err := listDeref(c.collections, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseCollectionList{Items: items}, nil
}

func (c *AdmissionControlCache) InitCollectionGroupCache(namespace string, load func() (*couchbasev2.CouchbaseCollectionGroupList, error)) error {
	if c.collectionGroups != nil && c.collectionGroups.Initialised(namespace) {
		return nil
	}

	groups, err := load()
	if err != nil || groups == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseCollectionGroup, 0, len(groups.Items))
	for i := range groups.Items {
		ptrs = append(ptrs, &groups.Items[i])
	}

	c.collectionGroups = NewSimpleIndexCache[*couchbasev2.CouchbaseCollectionGroup]()
	c.collectionGroups.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetCollectionGroup(namespace, name string) (*couchbasev2.CouchbaseCollectionGroup, bool, error) {
	cg, ok := c.collectionGroups.Get(name)
	return cg, ok, nil
}

func (c *AdmissionControlCache) GetCollectionGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionGroupList, error) {
	items, err := listDeref(c.collectionGroups, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseCollectionGroupList{Items: items}, nil
}

func (c *AdmissionControlCache) InitBucketCache(namespace string, load func() (*couchbasev2.CouchbaseBucketList, error)) error {
	if c.buckets != nil && c.buckets.Initialised(namespace) {
		return nil
	}

	buckets, err := load()
	if err != nil || buckets == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseBucket, 0, len(buckets.Items))
	for i := range buckets.Items {
		ptrs = append(ptrs, &buckets.Items[i])
	}

	c.buckets = NewSimpleIndexCache[*couchbasev2.CouchbaseBucket]()
	c.buckets.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetBucket(namespace, name string) (*couchbasev2.CouchbaseBucket, bool, error) {
	bucket, ok := c.buckets.Get(name)
	return bucket, ok, nil
}

func (c *AdmissionControlCache) GetBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error) {
	items, err := listDeref(c.buckets, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseBucketList{Items: items}, nil
}
