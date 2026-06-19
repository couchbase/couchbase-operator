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
	collections           *client.CouchbaseCollectionCache
	collectionGroups      *client.CouchbaseCollectionGroupCache
	buckets               *client.CouchbaseBucketCache
	ephemeralBuckets      *client.CouchbaseEphemeralBucketCache
	memcachedBuckets      *client.CouchbaseMemcachedBucketCache
	scopes                *client.CouchbaseScopeCache
	scopeGroups           *client.CouchbaseScopeGroupCache
	replications          *client.CouchbaseReplicationCache
	users                 *client.CouchbaseUserCache
	groups                *client.CouchbaseGroupCache
	roleBindings          *client.CouchbaseRoleBindingCache
	backups               *client.CouchbaseBackupCache
	backupRestores        *client.CouchbaseBackupRestoreCache
	migrationReplications *client.CouchbaseMigrationReplicationCache
	encryptionKeys        *client.CouchbaseEncryptionKeyCache
}

func newOperatorCacheAdapter(k8s *client.Client) *operatorCacheAdapter {
	return &operatorCacheAdapter{
		collections:           k8s.CouchbaseCollections,
		collectionGroups:      k8s.CouchbaseCollectionGroups,
		buckets:               k8s.CouchbaseBuckets,
		ephemeralBuckets:      k8s.CouchbaseEphemeralBuckets,
		memcachedBuckets:      k8s.CouchbaseMemcachedBuckets,
		scopes:                k8s.CouchbaseScopes,
		scopeGroups:           k8s.CouchbaseScopeGroups,
		replications:          k8s.CouchbaseReplications,
		users:                 k8s.CouchbaseUsers,
		groups:                k8s.CouchbaseGroups,
		roleBindings:          k8s.CouchbaseRoleBindings,
		backups:               k8s.CouchbaseBackups,
		backupRestores:        k8s.CouchbaseBackupRestores,
		migrationReplications: k8s.CouchbaseMigrationReplications,
		encryptionKeys:        k8s.CouchbaseEncryptionKeys,
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

// listFiltered filters a slice of pointer objects by our custom object selector and returns them dereferenced.
// T must be a pointer to E.
//
//nolint:gocognit
func listFilteredByObjectSelector[T interface {
	*E
	GetLabels() map[string]string
	GetName() string
}, E any](items []T, selector *couchbasev2.ObjectSelector) ([]E, error) {
	if selector.IsNil() {
		return returnAll(items), nil
	}

	matcher, err := selector.AsMatcher()
	if err != nil {
		return nil, err
	}

	out := make([]E, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}

		if matcher.Matches(item.GetName(), item.GetLabels()) {
			out = append(out, *item)
		}
	}

	return out, nil
}

func returnAll[T interface {
	*E
	GetLabels() map[string]string
	GetName() string
}, E any](items []T) []E {
	out := make([]E, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, *item)
		}
	}
	return out
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

// Noop.
func (a *operatorCacheAdapter) InitEphemeralBucketCache(_ string, _ func() (*couchbasev2.CouchbaseEphemeralBucketList, error)) error {
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

func (a *operatorCacheAdapter) GetBuckets(_ string, selector *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseBucketList, error) {
	items, err := listFilteredByObjectSelector(a.buckets.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseBucketList{Items: items}, nil
}

func (a *operatorCacheAdapter) GetEphemeralBucket(_ string, name string) (*couchbasev2.CouchbaseEphemeralBucket, bool, error) {
	bucket, ok := a.ephemeralBuckets.Get(name)
	return bucket, ok, nil
}

func (a *operatorCacheAdapter) GetEphemeralBuckets(_ string, selector *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error) {
	items, err := listFilteredByObjectSelector(a.ephemeralBuckets.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseEphemeralBucketList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitMemcachedBucketCache(_ string, _ func() (*couchbasev2.CouchbaseMemcachedBucketList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetMemcachedBucket(_ string, name string) (*couchbasev2.CouchbaseMemcachedBucket, bool, error) {
	bucket, ok := a.memcachedBuckets.Get(name)
	return bucket, ok, nil
}

func (a *operatorCacheAdapter) GetMemcachedBuckets(_ string, selector *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error) {
	items, err := listFilteredByObjectSelector(a.memcachedBuckets.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseMemcachedBucketList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitScopeCache(_ string, _ func() (*couchbasev2.CouchbaseScopeList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetScope(_ string, name string) (*couchbasev2.CouchbaseScope, bool, error) {
	scope, ok := a.scopes.Get(name)
	return scope, ok, nil
}

func (a *operatorCacheAdapter) GetScopes(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeList, error) {
	items, err := listFiltered(a.scopes.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseScopeList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitScopeGroupCache(_ string, _ func() (*couchbasev2.CouchbaseScopeGroupList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetScopeGroup(_ string, name string) (*couchbasev2.CouchbaseScopeGroup, bool, error) {
	group, ok := a.scopeGroups.Get(name)
	return group, ok, nil
}

func (a *operatorCacheAdapter) GetScopeGroups(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeGroupList, error) {
	items, err := listFiltered(a.scopeGroups.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseScopeGroupList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitReplicationCache(_ string, _ func() (*couchbasev2.CouchbaseReplicationList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetReplication(_ string, name string) (*couchbasev2.CouchbaseReplication, bool, error) {
	replication, ok := a.replications.Get(name)
	return replication, ok, nil
}

func (a *operatorCacheAdapter) GetReplications(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error) {
	items, err := listFiltered(a.replications.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseReplicationList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitUserCache(_ string, _ func() (*couchbasev2.CouchbaseUserList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetUser(_ string, name string) (*couchbasev2.CouchbaseUser, bool, error) {
	user, ok := a.users.Get(name)
	return user, ok, nil
}

func (a *operatorCacheAdapter) GetUsers(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error) {
	items, err := listFiltered(a.users.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseUserList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitGroupCache(_ string, _ func() (*couchbasev2.CouchbaseGroupList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetGroup(_ string, name string) (*couchbasev2.CouchbaseGroup, bool, error) {
	group, ok := a.groups.Get(name)
	return group, ok, nil
}

func (a *operatorCacheAdapter) GetGroups(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseGroupList, error) {
	items, err := listFiltered(a.groups.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseGroupList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitRoleBindingCache(_ string, _ func() (*couchbasev2.CouchbaseRoleBindingList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetRoleBinding(_ string, name string) (*couchbasev2.CouchbaseRoleBinding, bool, error) {
	roleBinding, ok := a.roleBindings.Get(name)
	return roleBinding, ok, nil
}

func (a *operatorCacheAdapter) GetRoleBindings(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error) {
	items, err := listFiltered(a.roleBindings.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseRoleBindingList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitBackupCache(_ string, _ func() (*couchbasev2.CouchbaseBackupList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetBackup(_ string, name string) (*couchbasev2.CouchbaseBackup, bool, error) {
	backup, ok := a.backups.Get(name)
	return backup, ok, nil
}

func (a *operatorCacheAdapter) GetBackups(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupList, error) {
	items, err := listFiltered(a.backups.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseBackupList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitBackupRestoreCache(_ string, _ func() (*couchbasev2.CouchbaseBackupRestoreList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetBackupRestore(_ string, name string) (*couchbasev2.CouchbaseBackupRestore, bool, error) {
	restore, ok := a.backupRestores.Get(name)
	return restore, ok, nil
}

func (a *operatorCacheAdapter) GetBackupRestores(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupRestoreList, error) {
	items, err := listFiltered(a.backupRestores.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseBackupRestoreList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitMigrationReplicationCache(_ string, _ func() (*couchbasev2.CouchbaseMigrationReplicationList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetMigrationReplication(_ string, name string) (*couchbasev2.CouchbaseMigrationReplication, bool, error) {
	replication, ok := a.migrationReplications.Get(name)
	return replication, ok, nil
}

func (a *operatorCacheAdapter) GetMigrationReplications(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMigrationReplicationList, error) {
	items, err := listFiltered(a.migrationReplications.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseMigrationReplicationList{Items: items}, nil
}

// Noop.
func (a *operatorCacheAdapter) InitEncryptionKeyCache(_ string, _ func() (*couchbasev2.CouchbaseEncryptionKeyList, error)) error {
	return nil
}

func (a *operatorCacheAdapter) GetEncryptionKey(_ string, name string) (*couchbasev2.CouchbaseEncryptionKey, bool, error) {
	key, ok := a.encryptionKeys.Get(name)
	return key, ok, nil
}

func (a *operatorCacheAdapter) GetEncryptionKeys(_ string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEncryptionKeyList, error) {
	items, err := listFiltered(a.encryptionKeys.List(), selector)
	if err != nil {
		return nil, err
	}
	return &couchbasev2.CouchbaseEncryptionKeyList{Items: items}, nil
}
