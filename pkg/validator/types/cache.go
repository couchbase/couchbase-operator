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

// ValidationCache is a one off, in memory cache to avoid hitting rate limits during validation when querying K8S api server.
// Resources need to be explicitly loaded into the cache by type.
// This is not a general purpose cache, does not inform/watch for resource changes and should only be used during validation.
type ValidationCache struct {
	collections      *SimpleIndexCache[*couchbasev2.CouchbaseCollection]
	collectionGroups *SimpleIndexCache[*couchbasev2.CouchbaseCollectionGroup]
}

func NewValidationCache() *ValidationCache {
	return &ValidationCache{}
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

// LoadCollections resets and fills the collections cache for the given namespace.
func (c *ValidationCache) LoadCollections(namespace string, collections *couchbasev2.CouchbaseCollectionList) {
	ptrs := make([]*couchbasev2.CouchbaseCollection, 0, len(collections.Items))
	for i := range collections.Items {
		ptrs = append(ptrs, &collections.Items[i])
	}

	c.collections = NewSimpleIndexCache[*couchbasev2.CouchbaseCollection]()
	c.collections.Load(namespace, ptrs)
}

// GetCollection retrieves a collection by name. The boolean return values indicate (in order) whether the collection was found in the cache and whether the cache was initialised for the namespace.
// If the cache was not initialised for the namespace, the calling method should fallback to direct API lookup.
func (c *ValidationCache) GetCollection(namespace, name string) (*couchbasev2.CouchbaseCollection, bool, bool) {
	if c.collections == nil || !c.collections.Initialised(namespace) {
		return nil, false, false
	}

	collection, ok := c.collections.Get(name)
	return collection, ok, true
}

func (c *ValidationCache) GetCollections(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionList, bool) {
	if c.collections == nil || !c.collections.Initialised(namespace) {
		return nil, false
	}

	collections := &couchbasev2.CouchbaseCollectionList{}
	cols, err := c.collections.List(selector)
	if err != nil {
		// If there's an error listing from the cache, treat it as a cache miss and fallback to direct API lookup.
		return collections, false
	}

	for _, c := range cols {
		if c != nil {
			collections.Items = append(collections.Items, *c)
		}
	}

	return collections, true
}

// LoadCollectionGroups resets and fills the collection groups cache for the given namespace.
func (c *ValidationCache) LoadCollectionGroups(namespace string, groups *couchbasev2.CouchbaseCollectionGroupList) {
	ptrs := make([]*couchbasev2.CouchbaseCollectionGroup, 0, len(groups.Items))
	for i := range groups.Items {
		ptrs = append(ptrs, &groups.Items[i])
	}

	c.collectionGroups = NewSimpleIndexCache[*couchbasev2.CouchbaseCollectionGroup]()
	c.collectionGroups.Load(namespace, ptrs)
}

func (c *ValidationCache) GetCollectionGroup(namespace, name string) (*couchbasev2.CouchbaseCollectionGroup, bool, bool) {
	if c.collectionGroups == nil || !c.collectionGroups.Initialised(namespace) {
		return nil, false, false
	}

	collectionGroup, ok := c.collectionGroups.Get(name)
	return collectionGroup, ok, true
}

func (c *ValidationCache) GetCollectionGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionGroupList, bool) {
	if c.collectionGroups == nil || !c.collectionGroups.Initialised(namespace) {
		return nil, false
	}

	groups := &couchbasev2.CouchbaseCollectionGroupList{}
	cols, err := c.collectionGroups.List(selector)
	if err != nil {
		// If there's an error listing from the cache, treat it as a cache miss and fallback to direct API lookup.
		return groups, false
	}

	for _, g := range cols {
		if g != nil {
			groups.Items = append(groups.Items, *g)
		}
	}

	return groups, true
}
