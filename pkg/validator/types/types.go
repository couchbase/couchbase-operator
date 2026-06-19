/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package types

import (
	"context"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubeAbstraction contains methods and data that help facilitate
// the discovery of objects that already exist or not in K8s, so
// we can validate our cbc YAML file against secrets or storage classes
// that may or may not exist before being accepted by K8s itself.
type KubeAbstraction interface { //nolint: interfacebloat
	// GetSecret checks whether the named secret exists in the specified namespace.
	GetSecret(string, string) (*corev1.Secret, bool, error)
	// GetStorageClass checks whether the named stoage class exists.
	GetStorageClass(string) (*storagev1.StorageClass, bool, error)
	// GetStorageClasses returns all storage classes in the cluster.
	GetStorageClasses() (*storagev1.StorageClassList, error)
	// GetCouchbaseClusters returns all clusters in the specified namespace.
	GetCouchbaseClusters(string) (*couchbasev2.CouchbaseClusterList, error)
	// GetCouchbaseBuckets returns all couchbase buckets for a specified selector.
	GetCouchbaseBuckets(string, *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseBucketList, error)
	// GetCouchbaseEphemeralBuckets returns all ephemeral buckets for a specified selector.
	GetCouchbaseEphemeralBuckets(string, *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error)
	// GetCouchbaseMemcachedBuckets returns all memcached buckets for a specified selector.
	GetCouchbaseMemcachedBuckets(string, *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error)
	// GetBuckets returns all abstract buckets for a specified selector.
	GetBuckets(string, *couchbasev2.ObjectSelector) ([]couchbasev2.AbstractBucket, error)
	// GetCouchbaseReplications returns all replications for a specified selector.
	GetCouchbaseReplications(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error)
	// GetCouchbaseUsers returns all users for a specified selector
	GetCouchbaseUsers(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error)
	// GetCouchbaseGroups returns all groups for a specified selector
	GetCouchbaseGroups(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseGroupList, error)
	// GetCouchbaseRoleBindings returns all user role bindings for a specified selector
	GetCouchbaseRoleBindings(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error)
	// GetCouchbaseBackups returns all backups for a specified selector.
	GetCouchbaseBackups(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupList, error)
	// GetCouchbaseBackups returns all backups for a specified selector.
	GetCouchbaseBackupRestores(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupRestoreList, error)
	// GetCouchbaseCollection returns the named collection.
	GetCouchbaseCollection(string, string) (*couchbasev2.CouchbaseCollection, bool, error)
	// GetCouchbaseCollectionGroup returns the named collection group.
	GetCouchbaseCollectionGroup(string, string) (*couchbasev2.CouchbaseCollectionGroup, bool, error)
	// GetCouchbaseCollections returns the selected collections.
	GetCouchbaseCollections(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionList, error)
	// GetCouchbaseCollectionGroups returns the selected collection groups.
	GetCouchbaseCollectionGroups(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionGroupList, error)
	// GetCouchbaseScope returns the named scope.
	GetCouchbaseScope(string, string) (*couchbasev2.CouchbaseScope, bool, error)
	// GetCouchbaseScopeGroup returns the named scope group.
	GetCouchbaseScopeGroup(string, string) (*couchbasev2.CouchbaseScopeGroup, bool, error)
	// GetCouchbaseScopes returns the selected scopes.
	GetCouchbaseScopes(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeList, error)
	// GetCouchbaseScopeGroups returns the selected scope groups.
	GetCouchbaseScopeGroups(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeGroupList, error)
	// GetCouchbaseEncryptionKeys returns all encryption keys for a specified selector.
	GetCouchbaseEncryptionKeys(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseEncryptionKeyList, error)
	// GetCouchbaseMigrationeplications returns all migration replications for a specified selector.
	GetCouchbaseMigrationReplications(string, *metav1.LabelSelector) (*couchbasev2.CouchbaseMigrationReplicationList, error)
}

// kubeAbstractionImpl Implements KubeAbstraction, operating on a real kubernetes cluster.
type kubeAbstractionImpl struct {
	client          kubernetes.Interface
	couchbaseClient versioned.Interface
	cache           ResourceCacheProvider
}

// secretExists checks whether the named secret exists in the specified namespace.
func (ab *kubeAbstractionImpl) GetSecret(namespace, name string) (*corev1.Secret, bool, error) {
	// Warning, this returns a valid pointer on error
	secret, err := ab.client.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	return secret, true, nil
}

// GetStorageClass checks whether the named stoage class exists.
func (ab *kubeAbstractionImpl) GetStorageClass(name string) (*storagev1.StorageClass, bool, error) {
	// Warning, this returns a valid pointer on error
	storageClass, err := ab.client.StorageV1().StorageClasses().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	return storageClass, true, nil
}

// GetStorageClasses returns all storage classes in the cluster.
func (ab *kubeAbstractionImpl) GetStorageClasses() (*storagev1.StorageClassList, error) {
	return ab.client.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
}

// GetCouchbaseClusters returns all clusters in the specified namespace.
func (ab *kubeAbstractionImpl) GetCouchbaseClusters(namespace string) (*couchbasev2.CouchbaseClusterList, error) {
	return ab.couchbaseClient.CouchbaseV2().CouchbaseClusters(namespace).List(context.Background(), metav1.ListOptions{})
}

// GetCouchbaseBuckets returns all couchbase buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseBuckets(namespace string, selector *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseBucketList, error) {
	if err := ab.cache.InitBucketCache(namespace, func() (*couchbasev2.CouchbaseBucketList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseBuckets(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetBuckets(namespace, selector)
}

// GetCouchbaseEphemeralBuckets returns all ephemeral buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseEphemeralBuckets(namespace string, selector *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseEphemeralBucketList, error) {
	if err := ab.cache.InitEphemeralBucketCache(namespace, func() (*couchbasev2.CouchbaseEphemeralBucketList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetEphemeralBuckets(namespace, selector)
}

// GetCouchbaseMemcachedBuckets returns all memcached buckets for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseMemcachedBuckets(namespace string, selector *couchbasev2.ObjectSelector) (*couchbasev2.CouchbaseMemcachedBucketList, error) {
	if err := ab.cache.InitMemcachedBucketCache(namespace, func() (*couchbasev2.CouchbaseMemcachedBucketList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetMemcachedBuckets(namespace, selector)
}

func (ab *kubeAbstractionImpl) GetBuckets(namespace string, selector *couchbasev2.ObjectSelector) ([]couchbasev2.AbstractBucket, error) {
	buckets, err := ab.GetCouchbaseBuckets(namespace, selector)
	if err != nil {
		return nil, err
	}

	ephemeral, err := ab.GetCouchbaseEphemeralBuckets(namespace, selector)
	if err != nil {
		return nil, err
	}

	memcached, err := ab.GetCouchbaseMemcachedBuckets(namespace, selector)
	if err != nil {
		return nil, err
	}

	abstract := []couchbasev2.AbstractBucket{}

	for i := range buckets.Items {
		abstract = append(abstract, &buckets.Items[i])
	}

	for i := range ephemeral.Items {
		abstract = append(abstract, &ephemeral.Items[i])
	}

	for i := range memcached.Items {
		abstract = append(abstract, &memcached.Items[i])
	}

	return abstract, nil
}

// GetCouchbaseReplications returns all replications for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseReplicationList, error) {
	if err := ab.cache.InitReplicationCache(namespace, func() (*couchbasev2.CouchbaseReplicationList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseReplications(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetReplications(namespace, selector)
}

func (ab *kubeAbstractionImpl) GetCouchbaseMigrationReplications(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseMigrationReplicationList, error) {
	if err := ab.cache.InitMigrationReplicationCache(namespace, func() (*couchbasev2.CouchbaseMigrationReplicationList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseMigrationReplications(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetMigrationReplications(namespace, selector)
}

// GetCouchbaseUsers returns all users for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseUsers(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseUserList, error) {
	if err := ab.cache.InitUserCache(namespace, func() (*couchbasev2.CouchbaseUserList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseUsers(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetUsers(namespace, selector)
}

// GetCouchbaseGroups returns all groups for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseGroupList, error) {
	if err := ab.cache.InitGroupCache(namespace, func() (*couchbasev2.CouchbaseGroupList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseGroups(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetGroups(namespace, selector)
}

// GetCouchbaseRoleBindings returns all user role bindings for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseRoleBindings(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseRoleBindingList, error) {
	if err := ab.cache.InitRoleBindingCache(namespace, func() (*couchbasev2.CouchbaseRoleBindingList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseRoleBindings(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetRoleBindings(namespace, selector)
}

func (ab *kubeAbstractionImpl) GetCouchbaseBackups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupList, error) {
	if err := ab.cache.InitBackupCache(namespace, func() (*couchbasev2.CouchbaseBackupList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseBackups(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetBackups(namespace, selector)
}

func (ab *kubeAbstractionImpl) GetCouchbaseBackupRestores(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseBackupRestoreList, error) {
	if err := ab.cache.InitBackupRestoreCache(namespace, func() (*couchbasev2.CouchbaseBackupRestoreList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseBackupRestores(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetBackupRestores(namespace, selector)
}

func (ab *kubeAbstractionImpl) GetCouchbaseCollection(namespace, name string) (*couchbasev2.CouchbaseCollection, bool, error) {
	if err := ab.cache.InitCollectionCache(namespace, func() (*couchbasev2.CouchbaseCollectionList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseCollections(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, false, err
	}

	return ab.cache.GetCollection(namespace, name)
}

func (ab *kubeAbstractionImpl) GetCouchbaseCollectionGroup(namespace, name string) (*couchbasev2.CouchbaseCollectionGroup, bool, error) {
	if err := ab.cache.InitCollectionGroupCache(namespace, func() (*couchbasev2.CouchbaseCollectionGroupList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseCollectionGroups(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, false, err
	}

	return ab.cache.GetCollectionGroup(namespace, name)
}

func (ab *kubeAbstractionImpl) GetCouchbaseCollections(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionList, error) {
	if err := ab.cache.InitCollectionCache(namespace, func() (*couchbasev2.CouchbaseCollectionList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseCollections(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetCollections(namespace, selector)
}

func (ab *kubeAbstractionImpl) GetCouchbaseCollectionGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseCollectionGroupList, error) {
	if err := ab.cache.InitCollectionGroupCache(namespace, func() (*couchbasev2.CouchbaseCollectionGroupList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseCollectionGroups(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetCollectionGroups(namespace, selector)
}

// GetCouchbaseScope returns the named scope.
func (ab *kubeAbstractionImpl) GetCouchbaseScope(namespace, name string) (*couchbasev2.CouchbaseScope, bool, error) {
	if err := ab.cache.InitScopeCache(namespace, func() (*couchbasev2.CouchbaseScopeList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseScopes(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, false, err
	}

	return ab.cache.GetScope(namespace, name)
}

// GetCouchbaseScopeGroup returns the named scope group.
func (ab *kubeAbstractionImpl) GetCouchbaseScopeGroup(namespace, name string) (*couchbasev2.CouchbaseScopeGroup, bool, error) {
	if err := ab.cache.InitScopeGroupCache(namespace, func() (*couchbasev2.CouchbaseScopeGroupList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseScopeGroups(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, false, err
	}

	return ab.cache.GetScopeGroup(namespace, name)
}

// GetCouchbaseScopes returns the selected scopes.
func (ab *kubeAbstractionImpl) GetCouchbaseScopes(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeList, error) {
	if err := ab.cache.InitScopeCache(namespace, func() (*couchbasev2.CouchbaseScopeList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseScopes(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetScopes(namespace, selector)
}

// GetCouchbaseScopeGroups returns the selected scope groups.
func (ab *kubeAbstractionImpl) GetCouchbaseScopeGroups(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseScopeGroupList, error) {
	if err := ab.cache.InitScopeGroupCache(namespace, func() (*couchbasev2.CouchbaseScopeGroupList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseScopeGroups(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetScopeGroups(namespace, selector)
}

// GetCouchbaseEncryptionKeys returns all encryption keys for a specified selector.
func (ab *kubeAbstractionImpl) GetCouchbaseEncryptionKeys(namespace string, selector *metav1.LabelSelector) (*couchbasev2.CouchbaseEncryptionKeyList, error) {
	if err := ab.cache.InitEncryptionKeyCache(namespace, func() (*couchbasev2.CouchbaseEncryptionKeyList, error) {
		return ab.couchbaseClient.CouchbaseV2().CouchbaseEncryptionKeys(namespace).List(context.Background(), metav1.ListOptions{})
	}); err != nil {
		return nil, err
	}

	return ab.cache.GetEncryptionKeys(namespace, selector)
}

// ValidatorOptions are configurable, as opposed to required, bits of the
// validator.
type ValidatorOptions struct {
	// Check whether referenced secrets exist, potentially whether the keys
	// are defined correctly, and certs/keys are valid in the case of TLS.
	ValidateSecrets bool

	// Check whether referenced storage classes exist.
	ValidateStorageClasses bool

	// Fill in the default fs group for a pod in order to write persistent
	// volumes.
	DefaultFileSystemGroup bool
}

// Validator is an abstraction layer for communicating with kubernetes
// to sanity check resources.
type Validator struct {
	Abstraction KubeAbstraction

	Options *ValidatorOptions
}

// New instantiates a new Validator backed by a AdmissionControlCache (on-demand API loading).
func New(client kubernetes.Interface, couchbaseClient versioned.Interface, options *ValidatorOptions) *Validator {
	return NewWithCache(client, couchbaseClient, options, NewAdmissionControlCache())
}

// NewWithCache instantiates a new Validator with the provided ResourceCacheProvider.
// Use this when the caller already has a warm cache (e.g. the operator's informer caches)
// to avoid redundant API calls during validation.
func NewWithCache(client kubernetes.Interface, couchbaseClient versioned.Interface, options *ValidatorOptions, cache ResourceCacheProvider) *Validator {
	abs := kubeAbstractionImpl{
		client:          client,
		couchbaseClient: couchbaseClient,
		cache:           cache,
	}

	// The main validator code expects this to be populated in order to
	// avoid mad levels of control flow.
	if options == nil {
		options = &ValidatorOptions{}
	}

	return &Validator{
		Abstraction: &abs,
		Options:     options,
	}
}
