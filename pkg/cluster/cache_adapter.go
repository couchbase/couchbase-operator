/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	vtypes "github.com/couchbase/couchbase-operator/pkg/validator/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// operatorCacheAdapter bridges the operator's watch-backed informer caches to the
// vtypes.ResourceCacheProvider interface. The informer caches are always warm so
// every call is a cache hit and the Init* methods are no-ops.
type operatorCacheAdapter struct {
	collections      *client.CouchbaseCollectionCache
	collectionGroups *client.CouchbaseCollectionGroupCache
	buckets          *client.CouchbaseBucketCache
}

func newOperatorCacheAdapter(k8s *client.Client) *operatorCacheAdapter {
	return &operatorCacheAdapter{
		collections:      k8s.CouchbaseCollections,
		collectionGroups: k8s.CouchbaseCollectionGroups,
		buckets:          k8s.CouchbaseBuckets,
	}
}

// newOperatorValidator creates a Validator backed by the operator's watch-backed
// informer caches.
func newOperatorValidator(k8s *client.Client) *vtypes.Validator {
	opts := &vtypes.ValidatorOptions{
		ValidateSecrets:        false,
		ValidateStorageClasses: false,
		DefaultFileSystemGroup: false,
	}
	return vtypes.NewWithCache(k8s.KubeClient, k8s.CouchbaseClient, opts, newOperatorCacheAdapter(k8s))
}

// listFiltered filters a slice of pointer objects by label selector and returns them dereferenced.
// T must be a pointer to E, and E must have a GetLabels() method.
func listFiltered[T interface {
	*E
	GetLabels() map[string]string
}, E any](items []T, selector *metav1.LabelSelector) ([]E, error) {
	sel := labels.Everything()
	if selector != nil {
		var err error
		sel, err = metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return nil, err
		}
	}
	out := make([]E, 0, len(items))
	for _, item := range items {
		if item == nil || !sel.Matches(labels.Set(item.GetLabels())) {
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

// Noop.
func (a *operatorCacheAdapter) InitCollectionCache(_ string, _ func() (*couchbasev2.CouchbaseCollectionList, error)) error {
	return nil
}

// Noop.
func (a *operatorCacheAdapter) InitCollectionGroupCache(_ string, _ func() (*couchbasev2.CouchbaseCollectionGroupList, error)) error {
	return nil
}

// Noop.
func (a *operatorCacheAdapter) InitBucketCache(_ string, _ func() (*couchbasev2.CouchbaseBucketList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetCollection(_ string, name string) (*couchbasev2.CouchbaseCollection, bool, error) {
	col, ok := a.collections.Get(name)
	return col, ok, nil
}

func (a *operatorCacheAdapter) GetCollections(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionList, error) {
	items, err := listFiltered(a.collections.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseCollectionList{Items: items}, nil
}

func (a *operatorCacheAdapter) GetCollectionGroup(_ string, name string) (*couchbasev2.CouchbaseCollectionGroup, bool, error) {
	cg, ok := a.collectionGroups.Get(name)
	return cg, ok, nil
}

func (a *operatorCacheAdapter) GetCollectionGroups(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionGroupList, error) {
	items, err := listFiltered(a.collectionGroups.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseCollectionGroupList{Items: items}, nil
}

func (a *operatorCacheAdapter) GetBucket(_ string, name string) (*couchbasev2.CouchbaseBucket, bool, error) {
	bucket, ok := a.buckets.Get(name)
	return bucket, ok, nil
}

func (a *operatorCacheAdapter) GetBuckets(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error) {
	items, err := listFiltered(a.buckets.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseBucketList{Items: items}, nil
}
