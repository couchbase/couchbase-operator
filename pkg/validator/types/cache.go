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
	InitEphemeralBucketCache(namespace string, load func() (*couchbasev2.CouchbaseEphemeralBucketList, error)) error
	InitMemcachedBucketCache(namespace string, load func() (*couchbasev2.CouchbaseMemcachedBucketList, error)) error
	InitScopeCache(namespace string, load func() (*couchbasev2.CouchbaseScopeList, error)) error
	InitScopeGroupCache(namespace string, load func() (*couchbasev2.CouchbaseScopeGroupList, error)) error
	InitReplicationCache(namespace string, load func() (*couchbasev2.CouchbaseReplicationList, error)) error
	InitUserCache(namespace string, load func() (*couchbasev2.CouchbaseUserList, error)) error
	InitGroupCache(namespace string, load func() (*couchbasev2.CouchbaseGroupList, error)) error
	InitRoleBindingCache(namespace string, load func() (*couchbasev2.CouchbaseRoleBindingList, error)) error
	InitBackupCache(namespace string, load func() (*couchbasev2.CouchbaseBackupList, error)) error
	InitBackupRestoreCache(namespace string, load func() (*couchbasev2.CouchbaseBackupRestoreList, error)) error
	InitMigrationReplicationCache(namespace string, load func() (*couchbasev2.CouchbaseMigrationReplicationList, error)) error
	InitEncryptionKeyCache(namespace string, load func() (*couchbasev2.CouchbaseEncryptionKeyList, error)) error
	GetCollection(namespace, name string) (*couchbasev2.CouchbaseCollection, bool, error)
	GetCollections(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionList, error)
	GetCollectionGroup(namespace, name string) (*couchbasev2.CouchbaseCollectionGroup, bool, error)
	GetCollectionGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionGroupList, error)
	GetBucket(namespace, name string) (*couchbasev2.CouchbaseBucket, bool, error)
	GetBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBucketList, error)
	GetEphemeralBucket(namespace, name string) (*couchbasev2.CouchbaseEphemeralBucket, bool, error)
	GetEphemeralBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error)
	GetMemcachedBucket(namespace, name string) (*couchbasev2.CouchbaseMemcachedBucket, bool, error)
	GetMemcachedBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error)
	GetScope(namespace, name string) (*couchbasev2.CouchbaseScope, bool, error)
	GetScopes(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeList, error)
	GetScopeGroup(namespace, name string) (*couchbasev2.CouchbaseScopeGroup, bool, error)
	GetScopeGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeGroupList, error)
	GetReplication(namespace, name string) (*couchbasev2.CouchbaseReplication, bool, error)
	GetReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error)
	GetUser(namespace, name string) (*couchbasev2.CouchbaseUser, bool, error)
	GetUsers(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error)
	GetGroup(namespace, name string) (*couchbasev2.CouchbaseGroup, bool, error)
	GetGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseGroupList, error)
	GetRoleBinding(namespace, name string) (*couchbasev2.CouchbaseRoleBinding, bool, error)
	GetRoleBindings(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error)
	GetBackup(namespace, name string) (*couchbasev2.CouchbaseBackup, bool, error)
	GetBackups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupList, error)
	GetBackupRestore(namespace, name string) (*couchbasev2.CouchbaseBackupRestore, bool, error)
	GetBackupRestores(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupRestoreList, error)
	GetMigrationReplication(namespace, name string) (*couchbasev2.CouchbaseMigrationReplication, bool, error)
	GetMigrationReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMigrationReplicationList, error)
	GetEncryptionKey(namespace, name string) (*couchbasev2.CouchbaseEncryptionKey, bool, error)
	GetEncryptionKeys(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEncryptionKeyList, error)
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
	collections           *SimpleIndexCache[*couchbasev2.CouchbaseCollection]
	collectionGroups      *SimpleIndexCache[*couchbasev2.CouchbaseCollectionGroup]
	buckets               *SimpleIndexCache[*couchbasev2.CouchbaseBucket]
	ephemeralBuckets      *SimpleIndexCache[*couchbasev2.CouchbaseEphemeralBucket]
	memcachedBuckets      *SimpleIndexCache[*couchbasev2.CouchbaseMemcachedBucket]
	scopes                *SimpleIndexCache[*couchbasev2.CouchbaseScope]
	scopeGroups           *SimpleIndexCache[*couchbasev2.CouchbaseScopeGroup]
	replications          *SimpleIndexCache[*couchbasev2.CouchbaseReplication]
	users                 *SimpleIndexCache[*couchbasev2.CouchbaseUser]
	groups                *SimpleIndexCache[*couchbasev2.CouchbaseGroup]
	roleBindings          *SimpleIndexCache[*couchbasev2.CouchbaseRoleBinding]
	backups               *SimpleIndexCache[*couchbasev2.CouchbaseBackup]
	backupRestores        *SimpleIndexCache[*couchbasev2.CouchbaseBackupRestore]
	migrationReplications *SimpleIndexCache[*couchbasev2.CouchbaseMigrationReplication]
	encryptionKeys        *SimpleIndexCache[*couchbasev2.CouchbaseEncryptionKey]
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

func (c *AdmissionControlCache) InitEphemeralBucketCache(namespace string, load func() (*couchbasev2.CouchbaseEphemeralBucketList, error)) error {
	if c.ephemeralBuckets != nil && c.ephemeralBuckets.Initialised(namespace) {
		return nil
	}

	buckets, err := load()
	if err != nil || buckets == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseEphemeralBucket, 0, len(buckets.Items))
	for i := range buckets.Items {
		ptrs = append(ptrs, &buckets.Items[i])
	}

	c.ephemeralBuckets = NewSimpleIndexCache[*couchbasev2.CouchbaseEphemeralBucket]()
	c.ephemeralBuckets.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetEphemeralBucket(namespace, name string) (*couchbasev2.CouchbaseEphemeralBucket, bool, error) {
	bucket, ok := c.ephemeralBuckets.Get(name)
	return bucket, ok, nil
}

func (c *AdmissionControlCache) GetEphemeralBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error) {
	items, err := listDeref(c.ephemeralBuckets, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseEphemeralBucketList{Items: items}, nil
}

func (c *AdmissionControlCache) InitMemcachedBucketCache(namespace string, load func() (*couchbasev2.CouchbaseMemcachedBucketList, error)) error {
	if c.memcachedBuckets != nil && c.memcachedBuckets.Initialised(namespace) {
		return nil
	}

	buckets, err := load()
	if err != nil || buckets == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseMemcachedBucket, 0, len(buckets.Items))
	for i := range buckets.Items {
		ptrs = append(ptrs, &buckets.Items[i])
	}

	c.memcachedBuckets = NewSimpleIndexCache[*couchbasev2.CouchbaseMemcachedBucket]()
	c.memcachedBuckets.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetMemcachedBucket(namespace, name string) (*couchbasev2.CouchbaseMemcachedBucket, bool, error) {
	bucket, ok := c.memcachedBuckets.Get(name)
	return bucket, ok, nil
}

func (c *AdmissionControlCache) GetMemcachedBuckets(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error) {
	items, err := listDeref(c.memcachedBuckets, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseMemcachedBucketList{Items: items}, nil
}

func (c *AdmissionControlCache) InitScopeCache(namespace string, load func() (*couchbasev2.CouchbaseScopeList, error)) error {
	if c.scopes != nil && c.scopes.Initialised(namespace) {
		return nil
	}

	scopes, err := load()
	if err != nil || scopes == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseScope, 0, len(scopes.Items))
	for i := range scopes.Items {
		ptrs = append(ptrs, &scopes.Items[i])
	}

	c.scopes = NewSimpleIndexCache[*couchbasev2.CouchbaseScope]()
	c.scopes.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetScope(namespace, name string) (*couchbasev2.CouchbaseScope, bool, error) {
	scope, ok := c.scopes.Get(name)
	return scope, ok, nil
}

func (c *AdmissionControlCache) GetScopes(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeList, error) {
	items, err := listDeref(c.scopes, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseScopeList{Items: items}, nil
}

func (c *AdmissionControlCache) InitScopeGroupCache(namespace string, load func() (*couchbasev2.CouchbaseScopeGroupList, error)) error {
	if c.scopeGroups != nil && c.scopeGroups.Initialised(namespace) {
		return nil
	}

	groups, err := load()
	if err != nil || groups == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseScopeGroup, 0, len(groups.Items))
	for i := range groups.Items {
		ptrs = append(ptrs, &groups.Items[i])
	}

	c.scopeGroups = NewSimpleIndexCache[*couchbasev2.CouchbaseScopeGroup]()
	c.scopeGroups.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetScopeGroup(namespace, name string) (*couchbasev2.CouchbaseScopeGroup, bool, error) {
	group, ok := c.scopeGroups.Get(name)
	return group, ok, nil
}

func (c *AdmissionControlCache) GetScopeGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeGroupList, error) {
	items, err := listDeref(c.scopeGroups, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseScopeGroupList{Items: items}, nil
}

func (c *AdmissionControlCache) InitReplicationCache(namespace string, load func() (*couchbasev2.CouchbaseReplicationList, error)) error {
	if c.replications != nil && c.replications.Initialised(namespace) {
		return nil
	}

	replications, err := load()
	if err != nil || replications == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseReplication, 0, len(replications.Items))
	for i := range replications.Items {
		ptrs = append(ptrs, &replications.Items[i])
	}

	c.replications = NewSimpleIndexCache[*couchbasev2.CouchbaseReplication]()
	c.replications.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetReplication(namespace, name string) (*couchbasev2.CouchbaseReplication, bool, error) {
	replication, ok := c.replications.Get(name)
	return replication, ok, nil
}

func (c *AdmissionControlCache) GetReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error) {
	items, err := listDeref(c.replications, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseReplicationList{Items: items}, nil
}

func (c *AdmissionControlCache) InitUserCache(namespace string, load func() (*couchbasev2.CouchbaseUserList, error)) error {
	if c.users != nil && c.users.Initialised(namespace) {
		return nil
	}

	users, err := load()
	if err != nil || users == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseUser, 0, len(users.Items))
	for i := range users.Items {
		ptrs = append(ptrs, &users.Items[i])
	}

	c.users = NewSimpleIndexCache[*couchbasev2.CouchbaseUser]()
	c.users.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetUser(namespace, name string) (*couchbasev2.CouchbaseUser, bool, error) {
	user, ok := c.users.Get(name)
	return user, ok, nil
}

func (c *AdmissionControlCache) GetUsers(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error) {
	items, err := listDeref(c.users, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseUserList{Items: items}, nil
}

func (c *AdmissionControlCache) InitGroupCache(namespace string, load func() (*couchbasev2.CouchbaseGroupList, error)) error {
	if c.groups != nil && c.groups.Initialised(namespace) {
		return nil
	}

	groups, err := load()
	if err != nil || groups == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseGroup, 0, len(groups.Items))
	for i := range groups.Items {
		ptrs = append(ptrs, &groups.Items[i])
	}

	c.groups = NewSimpleIndexCache[*couchbasev2.CouchbaseGroup]()
	c.groups.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetGroup(namespace, name string) (*couchbasev2.CouchbaseGroup, bool, error) {
	group, ok := c.groups.Get(name)
	return group, ok, nil
}

func (c *AdmissionControlCache) GetGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseGroupList, error) {
	items, err := listDeref(c.groups, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseGroupList{Items: items}, nil
}

func (c *AdmissionControlCache) InitRoleBindingCache(namespace string, load func() (*couchbasev2.CouchbaseRoleBindingList, error)) error {
	if c.roleBindings != nil && c.roleBindings.Initialised(namespace) {
		return nil
	}

	roleBindings, err := load()
	if err != nil || roleBindings == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseRoleBinding, 0, len(roleBindings.Items))
	for i := range roleBindings.Items {
		ptrs = append(ptrs, &roleBindings.Items[i])
	}

	c.roleBindings = NewSimpleIndexCache[*couchbasev2.CouchbaseRoleBinding]()
	c.roleBindings.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetRoleBinding(namespace, name string) (*couchbasev2.CouchbaseRoleBinding, bool, error) {
	roleBinding, ok := c.roleBindings.Get(name)
	return roleBinding, ok, nil
}

func (c *AdmissionControlCache) GetRoleBindings(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error) {
	items, err := listDeref(c.roleBindings, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseRoleBindingList{Items: items}, nil
}

func (c *AdmissionControlCache) InitBackupCache(namespace string, load func() (*couchbasev2.CouchbaseBackupList, error)) error {
	if c.backups != nil && c.backups.Initialised(namespace) {
		return nil
	}

	backups, err := load()
	if err != nil || backups == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseBackup, 0, len(backups.Items))
	for i := range backups.Items {
		ptrs = append(ptrs, &backups.Items[i])
	}

	c.backups = NewSimpleIndexCache[*couchbasev2.CouchbaseBackup]()
	c.backups.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetBackup(namespace, name string) (*couchbasev2.CouchbaseBackup, bool, error) {
	backup, ok := c.backups.Get(name)
	return backup, ok, nil
}

func (c *AdmissionControlCache) GetBackups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupList, error) {
	items, err := listDeref(c.backups, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseBackupList{Items: items}, nil
}

func (c *AdmissionControlCache) InitBackupRestoreCache(namespace string, load func() (*couchbasev2.CouchbaseBackupRestoreList, error)) error {
	if c.backupRestores != nil && c.backupRestores.Initialised(namespace) {
		return nil
	}

	restores, err := load()
	if err != nil || restores == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseBackupRestore, 0, len(restores.Items))
	for i := range restores.Items {
		ptrs = append(ptrs, &restores.Items[i])
	}

	c.backupRestores = NewSimpleIndexCache[*couchbasev2.CouchbaseBackupRestore]()
	c.backupRestores.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetBackupRestore(namespace, name string) (*couchbasev2.CouchbaseBackupRestore, bool, error) {
	restore, ok := c.backupRestores.Get(name)
	return restore, ok, nil
}

func (c *AdmissionControlCache) GetBackupRestores(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupRestoreList, error) {
	items, err := listDeref(c.backupRestores, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseBackupRestoreList{Items: items}, nil
}

func (c *AdmissionControlCache) InitMigrationReplicationCache(namespace string, load func() (*couchbasev2.CouchbaseMigrationReplicationList, error)) error {
	if c.migrationReplications != nil && c.migrationReplications.Initialised(namespace) {
		return nil
	}

	replications, err := load()
	if err != nil || replications == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseMigrationReplication, 0, len(replications.Items))
	for i := range replications.Items {
		ptrs = append(ptrs, &replications.Items[i])
	}

	c.migrationReplications = NewSimpleIndexCache[*couchbasev2.CouchbaseMigrationReplication]()
	c.migrationReplications.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetMigrationReplication(namespace, name string) (*couchbasev2.CouchbaseMigrationReplication, bool, error) {
	replication, ok := c.migrationReplications.Get(name)
	return replication, ok, nil
}

func (c *AdmissionControlCache) GetMigrationReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMigrationReplicationList, error) {
	items, err := listDeref(c.migrationReplications, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseMigrationReplicationList{Items: items}, nil
}

func (c *AdmissionControlCache) InitEncryptionKeyCache(namespace string, load func() (*couchbasev2.CouchbaseEncryptionKeyList, error)) error {
	if c.encryptionKeys != nil && c.encryptionKeys.Initialised(namespace) {
		return nil
	}

	keys, err := load()
	if err != nil || keys == nil {
		return err
	}

	ptrs := make([]*couchbasev2.CouchbaseEncryptionKey, 0, len(keys.Items))
	for i := range keys.Items {
		ptrs = append(ptrs, &keys.Items[i])
	}

	c.encryptionKeys = NewSimpleIndexCache[*couchbasev2.CouchbaseEncryptionKey]()
	c.encryptionKeys.Load(namespace, ptrs)

	return nil
}

func (c *AdmissionControlCache) GetEncryptionKey(namespace, name string) (*couchbasev2.CouchbaseEncryptionKey, bool, error) {
	key, ok := c.encryptionKeys.Get(name)
	return key, ok, nil
}

func (c *AdmissionControlCache) GetEncryptionKeys(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEncryptionKeyList, error) {
	items, err := listDeref(c.encryptionKeys, selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseEncryptionKeyList{Items: items}, nil
}
